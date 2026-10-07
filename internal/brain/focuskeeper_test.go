package brain

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"

	"github.com/rengo/nooma/internal/core/focus"
	"github.com/rengo/nooma/internal/core/unit"
	"github.com/rengo/nooma/internal/core/weight"
	"github.com/rengo/nooma/internal/ports"
	"github.com/rengo/nooma/test/support/memrepo"
)

// errKeeperBoom is the one error every failing port below returns, so a test
// can assert it travelled through errors.Is.
var errKeeperBoom = errors.New("keeper test: port failed")

// fxH is design m4c §6's FX-H hysteresis fixture, and the reason it exists:
// Select keeps the top focus.DefaultSize (7) of a Kind, so with fewer than 8
// candidates nobody is displaced and a hold or a displacement proves
// nothing. Every test built on it therefore (1) puts at least DefaultSize+1
// candidates of the Kind in the pool (fillers that clearly outrank the
// contested units, plus the contested units, so the contest is slot 7
// against slot 8), (2) SEEDS the incumbent with a first request, and (3)
// only afterwards changes a contested unit's stored weight. Every unit is
// stamped todayNow, so a different clock instant would scale every score
// together and could not reorder them.
//
// With zero relations and zero age, a unit's Rank score is exactly its
// stored weight, so the arithmetic in each test is plain: against the
// default margin (5%) an incumbent of weight 1.0 is displaced by a
// challenger above 1.05 and keeps its slot against one at or below it.
type fxH struct {
	units     *memrepo.Units
	flaky     *flakyUnits
	rels      *memrepo.Relations
	cfg       *memrepo.Config
	cfgPort   *flakyConfig
	triggers  *memrepo.Triggers
	state     *memrepo.State
	log       *memrepo.DecisionLog
	ids       *countingIDs
	questions *openFailingQuestions
	keeper    *FocusKeeper
	svc       *TodayService
}

// flakyConfig makes ConfigRepo.Load fail on demand. The keeper reads it on
// every computation, and checkRunner holds no ConfigRepo, so toggling it
// fails exactly the keeper's compute.
type flakyConfig struct {
	*memrepo.Config
	fail bool
}

func (c *flakyConfig) Load(ctx context.Context) (ports.VaultConfig, error) {
	if c.fail {
		return ports.VaultConfig{}, errKeeperBoom
	}
	return c.Config.Load(ctx)
}

// openFailingQuestions makes PendingQuestionRepo.Open fail on demand: Open is
// the last read of a Today request, so a request that fails there has already
// computed its whole round.
type openFailingQuestions struct {
	*memrepo.PendingQuestions
	failOpen bool
}

func (q *openFailingQuestions) Open(ctx context.Context) ([]ports.RelationQuestion, error) {
	if q.failOpen {
		return nil, errKeeperBoom
	}
	return q.PendingQuestions.Open(ctx)
}

// flakyUnits makes the two reads a Today request makes for its focuses fail on
// demand: the keeper's candidate read, and the view's LiveByIDs read that
// follows it.
type flakyUnits struct {
	*memrepo.Units
	failCandidates bool
	failLive       bool
}

func (u *flakyUnits) LiveFocusCandidatesByType(ctx context.Context, types []unit.Type) ([]focus.Candidate, error) {
	if u.failCandidates {
		return nil, errKeeperBoom
	}
	return u.Units.LiveFocusCandidatesByType(ctx, types)
}

func (u *flakyUnits) LiveByIDs(ctx context.Context, ids []string) ([]unit.Unit, error) {
	if u.failLive {
		return nil, errKeeperBoom
	}
	return u.Units.LiveByIDs(ctx, ids)
}

func newFxH(t *testing.T) *fxH {
	t.Helper()
	f := &fxH{
		units:     memrepo.NewUnits(),
		rels:      memrepo.NewRelations(),
		cfg:       memrepo.NewConfig(),
		triggers:  memrepo.NewTriggers(),
		state:     memrepo.NewState(),
		log:       memrepo.NewDecisionLog(),
		ids:       &countingIDs{},
		questions: &openFailingQuestions{PendingQuestions: memrepo.NewPendingQuestions()},
	}
	f.flaky = &flakyUnits{Units: f.units}
	f.cfgPort = &flakyConfig{Config: f.cfg}
	f.keeper = NewFocusKeeper(f.flaky, f.cfgPort, f.rels)
	f.svc = NewTodayService(fixedClock{now: todayNow}, f.flaky, f.cfgPort, f.state, f.triggers, f.questions, f.log, f.keeper)
	return f
}

// fillers seeds n units of typ named prefix+1..prefix+n at a weight no
// contested unit comes near. Equal weights tie, so Rank orders them by id.
func (f *fxH) fillers(t *testing.T, typ unit.Type, prefix string, n int) []string {
	t.Helper()
	ids := make([]string, n)
	for i := range ids {
		ids[i] = fmt.Sprintf("%s%d", prefix, i+1)
		seedTodayUnit(t, f.units, ids[i], typ, 10)
	}
	return ids
}

// taskContest seeds the task Kind: six fillers F1..F6, the incumbent-to-be A
// and the challenger B, so A and B contest slot 7 against slot 8.
func (f *fxH) taskContest(t *testing.T, a, b float64) {
	t.Helper()
	f.fillers(t, unit.TypeTask, "F", 6)
	seedTodayUnit(t, f.units, "A", unit.TypeTask, a)
	seedTodayUnit(t, f.units, "B", unit.TypeTask, b)
}

// loadContest is taskContest for the load Kind: G1..G6, C and D.
func (f *fxH) loadContest(t *testing.T, c, d float64) {
	t.Helper()
	f.fillers(t, unit.TypeMentalLoad, "G", 6)
	seedTodayUnit(t, f.units, "C", unit.TypeMentalLoad, c)
	seedTodayUnit(t, f.units, "D", unit.TypeMentalLoad, d)
}

func (f *fxH) setWeight(t *testing.T, id string, w float64) {
	t.Helper()
	if err := f.units.ApplyBoosts(context.Background(), []weight.Boost{{UnitID: id, Weight: w, LastTouchedAt: todayNow}}, todayNow); err != nil {
		t.Fatalf("set weight of %s: %v", id, err)
	}
}

// relate stores one relation with distinct strength and confidence.
func (f *fxH) relate(t *testing.T, from, to string, strength, confidence float64) {
	t.Helper()
	if err := f.rels.Upsert(context.Background(), ports.Relation{
		ID: from + ">" + to, FromUnitID: from, ToUnitID: to, Type: "related",
		Strength: strength, Confidence: confidence, CreatedBy: "test", CreatedAt: todayNow,
	}); err != nil {
		t.Fatalf("relate %s -> %s: %v", from, to, err)
	}
}

func (f *fxH) request(t *testing.T) Today {
	t.Helper()
	today, err := f.svc.Today(context.Background())
	if err != nil {
		t.Fatalf("Today: %v", err)
	}
	return today
}

func ids(prefix string, n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = fmt.Sprintf("%s%d", prefix, i+1)
	}
	return out
}

func assertIDs(t *testing.T, what string, got, want []string) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%s = %v, want %v", what, got, want)
	}
}

// TestFocusKeeper_PublishReplacesBothKinds pins R5's "never a mix": publish
// swaps in the whole snapshot, so a round whose load focus is empty leaves no
// trace of the previous round's load focus (design m4c §3.2).
func TestFocusKeeper_PublishReplacesBothKinds(t *testing.T) {
	k := NewFocusKeeper(memrepo.NewUnits(), memrepo.NewConfig(), memrepo.NewRelations())

	first := focusRound{next: &incumbent{byKind: map[focus.Kind]focus.Selection{
		focus.KindTask: {Kind: focus.KindTask, Members: []string{"t1"}},
		focus.KindLoad: {Kind: focus.KindLoad, Members: []string{"l1"}},
	}}}
	k.publish(first)
	if k.held.Load() != first.next {
		t.Fatal("publish did not store the round's snapshot")
	}

	second := focusRound{next: &incumbent{byKind: map[focus.Kind]focus.Selection{
		focus.KindTask: {Kind: focus.KindTask, Members: []string{"t2"}},
		focus.KindLoad: {Kind: focus.KindLoad},
	}}}
	k.publish(second)

	got := k.held.Load()
	if got != second.next {
		t.Fatalf("held = %+v, want exactly the second round's snapshot", got)
	}
	assertIDs(t, "held task members", got.byKind[focus.KindTask].Members, []string{"t2"})
	if m := got.byKind[focus.KindLoad].Members; len(m) != 0 {
		t.Fatalf("held load members = %v, want none — the previous round's load focus survived a publish", m)
	}
}

// TestFocusKeeper_ComputeDoesNotPublish: computing is a read. Only a writer
// that has earned it (a request that succeeded, a digest that was sent)
// publishes.
func TestFocusKeeper_ComputeDoesNotPublish(t *testing.T) {
	f := newFxH(t)
	f.taskContest(t, 1.0, 0.99)

	round, err := f.keeper.compute(context.Background(), todayNow)
	if err != nil {
		t.Fatalf("compute: %v", err)
	}
	assertIDs(t, "computed task members", rankedIDs(round.members[focus.KindTask]), append(ids("F", 6), "A"))
	if f.keeper.held.Load() != nil {
		t.Fatal("compute stored an incumbent; only publish may")
	}
}

func rankedIDs(rs []focus.Ranked) []string {
	out := make([]string, len(rs))
	for i, r := range rs {
		out[i] = r.Candidate.ID
	}
	return out
}

type failingConfig struct{ ports.ConfigRepo }

func (failingConfig) Load(context.Context) (ports.VaultConfig, error) {
	return ports.VaultConfig{}, errKeeperBoom
}

type failingCandidates struct{ ports.UnitRepo }

func (failingCandidates) LiveFocusCandidatesByType(context.Context, []unit.Type) ([]focus.Candidate, error) {
	return nil, errKeeperBoom
}

type failingRelations struct{ ports.RelationRepo }

func (failingRelations) ByUnit(context.Context, string) ([]ports.Relation, error) {
	return nil, errKeeperBoom
}

func TestFocusKeeper_ConfigErrorPropagates(t *testing.T) {
	k := NewFocusKeeper(memrepo.NewUnits(), failingConfig{memrepo.NewConfig()}, memrepo.NewRelations())
	if _, err := k.compute(context.Background(), todayNow); !errors.Is(err, errKeeperBoom) {
		t.Fatalf("compute err = %v, want the config error", err)
	}
}

func TestFocusKeeper_CandidatesErrorPropagates(t *testing.T) {
	k := NewFocusKeeper(failingCandidates{memrepo.NewUnits()}, memrepo.NewConfig(), memrepo.NewRelations())
	if _, err := k.compute(context.Background(), todayNow); !errors.Is(err, errKeeperBoom) {
		t.Fatalf("compute err = %v, want the candidates error", err)
	}
}

// TestFocusKeeper_RelationsErrorPropagates needs a held incumbent: with none
// there are no members to read relations for.
func TestFocusKeeper_RelationsErrorPropagates(t *testing.T) {
	k := NewFocusKeeper(memrepo.NewUnits(), memrepo.NewConfig(), failingRelations{memrepo.NewRelations()})
	k.held.Store(&incumbent{byKind: map[focus.Kind]focus.Selection{
		focus.KindTask: {Kind: focus.KindTask, Members: []string{"A"}},
	}})
	if _, err := k.compute(context.Background(), todayNow); !errors.Is(err, errKeeperBoom) {
		t.Fatalf("compute err = %v, want the relations error", err)
	}
}
