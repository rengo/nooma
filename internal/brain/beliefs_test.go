package brain

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/core/selfmodel"
	"github.com/rengo/nooma/internal/ports"
	"github.com/rengo/nooma/test/support/memrepo"
)

// This file holds the shared world of the BeliefsService tests (FX-B,
// design §6): beliefs with non-default confidence and timestamps in
// several facets, one retired belief in a touched facet, one facet empty,
// a seeded decision_log and learning_signals so that "nothing changed" is a
// comparison of full snapshots, never of counts from zero.

// tickClock advances one second per read, so a second Now() inside one
// operation shows as two different instants.
type tickClock struct{ t time.Time }

func (c *tickClock) Now() time.Time {
	c.t = c.t.Add(time.Second)
	return c.t
}

// selfModelFaults wraps a ports.SelfModelRepo, counting the two user
// writes and optionally failing them (the write is NOT passed through when
// it fails, so the belief stays as it was).
type selfModelFaults struct {
	ports.SelfModelRepo
	setErr, editErr     error
	setCalls, editCalls int
}

func (f *selfModelFaults) SetStatus(ctx context.Context, id string, from, to selfmodel.Status, at time.Time) error {
	f.setCalls++
	if f.setErr != nil {
		return f.setErr
	}
	return f.SelfModelRepo.SetStatus(ctx, id, from, to, at)
}

func (f *selfModelFaults) EditContent(ctx context.Context, id, from, to string, at time.Time) error {
	f.editCalls++
	if f.editErr != nil {
		return f.editErr
	}
	return f.SelfModelRepo.EditContent(ctx, id, from, to, at)
}

// logFaults counts and optionally fails decision_log writes.
type logFaults struct {
	ports.DecisionLog
	err   error
	calls int
}

func (f *logFaults) Record(ctx context.Context, d ports.Decision) error {
	f.calls++
	if f.err != nil {
		return f.err
	}
	return f.DecisionLog.Record(ctx, d)
}

// signalFaults counts and optionally fails learning_signals writes.
type signalFaults struct {
	ports.SignalRepo
	err   error
	calls int
}

func (f *signalFaults) Record(ctx context.Context, s ports.Signal) error {
	f.calls++
	if f.err != nil {
		return f.err
	}
	return f.SignalRepo.Record(ctx, s)
}

// beliefsWorld is one BeliefsService over memrepo, seeded with FX-B.
type beliefsWorld struct {
	t     *testing.T
	now   time.Time
	clock ports.Clock

	base        *memrepo.SelfModel
	baseLog     *memrepo.DecisionLog
	baseSignals *memrepo.Signals

	model   *selfModelFaults
	log     *logFaults
	signals *signalFaults

	seeded map[string]ports.Belief
	n      int
}

func newBeliefsWorld(t *testing.T) *beliefsWorld {
	t.Helper()
	now := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	w := &beliefsWorld{
		t: t, now: now, clock: fixedClock{now},
		base: memrepo.NewSelfModel(), baseLog: memrepo.NewDecisionLog(), baseSignals: memrepo.NewSignals(),
		seeded: map[string]ports.Belief{},
	}
	w.model = &selfModelFaults{SelfModelRepo: w.base}
	w.log = &logFaults{DecisionLog: w.baseLog}
	w.signals = &signalFaults{SignalRepo: w.baseSignals}

	ctx := context.Background()
	for i, id := range []string{"seed-d1", "seed-d2"} {
		d := ports.Decision{ID: id, Action: ports.ActionCaptureClassify, Rationale: "seeded", Context: json.RawMessage(`{"n":` + string(rune('1'+i)) + `}`), OccurredAt: now.Add(-time.Duration(i+1) * time.Hour)}
		if err := w.baseLog.Record(ctx, d); err != nil {
			t.Fatalf("seed decision: %v", err)
		}
	}
	if err := w.baseSignals.Record(ctx, ports.Signal{ID: "seed-s1", Type: ports.SignalCorrection, Valence: ports.ValenceNegative, Context: json.RawMessage(`{}`), OccurredAt: now.Add(-90 * time.Minute)}); err != nil {
		t.Fatalf("seed signal: %v", err)
	}

	// FX-B. The goal facet is the touched one and holds a retired belief;
	// value is populated and untouched; identity and social stay empty.
	w.seed("g1", selfmodel.FacetGoal, selfmodel.OriginDerived, selfmodel.StatusActive, 0.82, "run a marathon")
	w.seed("g2", selfmodel.FacetGoal, selfmodel.OriginUserStated, selfmodel.StatusActive, 0.61, "read every day")
	w.seed("gr", selfmodel.FacetGoal, selfmodel.OriginDerived, selfmodel.StatusRetired, 0.33, "learn latin")
	w.seed("v1", selfmodel.FacetValue, selfmodel.OriginDerived, selfmodel.StatusActive, 0.74, "line one\nline two")
	w.seed("v2", selfmodel.FacetValue, selfmodel.OriginSeed, selfmodel.StatusActive, 0.55, "honesty first")
	w.seed("p1", selfmodel.FacetPreference, selfmodel.OriginDerived, selfmodel.StatusActive, 0.91, "tea over coffee")
	return w
}

// seed stores one belief with non-default timestamps and returns it.
func (w *beliefsWorld) seed(id string, facet selfmodel.Facet, origin selfmodel.Origin, status selfmodel.Status, conf float64, content string) ports.Belief {
	w.t.Helper()
	w.n++
	age := time.Duration(w.n) * 24 * time.Hour
	b := ports.Belief{
		ID: id, Facet: facet, TopicKey: "derived/" + string(facet) + "/" + id, Content: content, Confidence: conf,
		Origin: origin, Status: status,
		LastReinforcedAt: w.now.Add(-age), CreatedAt: w.now.Add(-age - time.Hour), UpdatedAt: w.now.Add(-age / 2),
	}
	if err := w.base.UpsertByTopicKey(context.Background(), b); err != nil {
		w.t.Fatalf("seed belief %s: %v", id, err)
	}
	w.seeded[id] = b
	return b
}

// service builds the BeliefsService over whatever decorators the test set.
func (w *beliefsWorld) service() *BeliefsService {
	return NewBeliefsService(w.clock, &seqIDs{}, w.model, w.signals, w.log)
}

// worldSnapshot is every belief column, every decision_log row and every
// learning signal.
type worldSnapshot struct {
	Beliefs   []ports.Belief
	Decisions []ports.Decision
	Signals   []ports.Signal
}

func (w *beliefsWorld) snapshot() worldSnapshot {
	w.t.Helper()
	ctx := context.Background()
	active, err := w.base.ActiveBeliefs(ctx)
	if err != nil {
		w.t.Fatalf("ActiveBeliefs: %v", err)
	}
	retired, err := w.base.RetiredBeliefs(ctx)
	if err != nil {
		w.t.Fatalf("RetiredBeliefs: %v", err)
	}
	beliefs := append(active, retired...)
	slices.SortFunc(beliefs, func(a, b ports.Belief) int { return strings.Compare(a.ID, b.ID) })
	decisions, err := w.baseLog.Since(ctx, time.Time{}, 10000)
	if err != nil {
		w.t.Fatalf("decisions: %v", err)
	}
	signals, err := w.baseSignals.Since(ctx, time.Time{}, 10000)
	if err != nil {
		w.t.Fatalf("signals: %v", err)
	}
	return worldSnapshot{Beliefs: beliefs, Decisions: decisions, Signals: signals}
}

// wantNothingChanged fails when anything differs from before.
func (w *beliefsWorld) wantNothingChanged(before worldSnapshot) {
	w.t.Helper()
	after := w.snapshot()
	if !reflect.DeepEqual(before.Beliefs, after.Beliefs) {
		w.t.Errorf("beliefs changed:\n before %+v\n after  %+v", before.Beliefs, after.Beliefs)
	}
	if !reflect.DeepEqual(before.Decisions, after.Decisions) {
		w.t.Errorf("decision_log changed:\n before %+v\n after  %+v", before.Decisions, after.Decisions)
	}
	if !reflect.DeepEqual(before.Signals, after.Signals) {
		w.t.Errorf("learning_signals changed:\n before %+v\n after  %+v", before.Signals, after.Signals)
	}
}

func (w *beliefsWorld) belief(id string) ports.Belief {
	w.t.Helper()
	b, err := w.base.BeliefByID(context.Background(), id)
	if err != nil {
		w.t.Fatalf("BeliefByID(%s): %v", id, err)
	}
	return b
}

// newDecisions returns the rows written after the seeds, in order, with
// their contexts decoded.
func (w *beliefsWorld) newDecisions() []loggedDecision {
	w.t.Helper()
	ds, err := w.baseLog.Since(context.Background(), w.now.Add(-time.Minute), 100)
	if err != nil {
		w.t.Fatalf("decisions: %v", err)
	}
	out := make([]loggedDecision, len(ds))
	for i, d := range ds {
		out[i] = loggedDecision{Decision: d, Ctx: decodeContext(w.t, d.Context)}
	}
	return out
}

type loggedDecision struct {
	ports.Decision
	Ctx map[string]any
}

// newSignals returns the signals written after the seeds.
func (w *beliefsWorld) newSignals() []loggedSignal {
	w.t.Helper()
	ss, err := w.baseSignals.Since(context.Background(), w.now.Add(-time.Minute), 100)
	if err != nil {
		w.t.Fatalf("signals: %v", err)
	}
	out := make([]loggedSignal, len(ss))
	for i, s := range ss {
		out[i] = loggedSignal{Signal: s, Ctx: decodeContext(w.t, s.Context)}
	}
	return out
}

type loggedSignal struct {
	ports.Signal
	Ctx map[string]any
}

func decodeContext(t *testing.T, raw json.RawMessage) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("decode context: %v\n%s", err, raw)
	}
	return m
}

// ---------- ByFacet (B11, B11b-d) ----------

// stubActive returns exactly the beliefs it was given, in that order.
type stubActive struct {
	ports.SelfModelRepo
	beliefs []ports.Belief
	err     error
}

func (s stubActive) ActiveBeliefs(context.Context) ([]ports.Belief, error) {
	return slices.Clone(s.beliefs), s.err
}

func beliefIDs(bs []ports.Belief) []string {
	out := make([]string, len(bs))
	for i, b := range bs {
		out[i] = b.ID
	}
	return out
}

func facetsOf(gs []FacetBeliefs) []selfmodel.Facet {
	out := make([]selfmodel.Facet, len(gs))
	for i, g := range gs {
		out[i] = g.Facet
	}
	return out
}

func TestBeliefsByFacet_GroupsActiveBeliefsUnderAllFiveFacets(t *testing.T) {
	w := newBeliefsWorld(t)

	got, err := w.service().ByFacet(context.Background())
	if err != nil {
		t.Fatalf("ByFacet: %v", err)
	}

	if want := selfmodel.AllFacets(); !slices.Equal(facetsOf(got), want) {
		t.Fatalf("facets = %v, want all five in AllFacets order %v", facetsOf(got), want)
	}
	wantIDs := map[selfmodel.Facet][]string{
		selfmodel.FacetIdentity:   {},
		selfmodel.FacetValue:      {"v1", "v2"},
		selfmodel.FacetGoal:       {"g1", "g2"}, // gr is retired
		selfmodel.FacetSocial:     {},
		selfmodel.FacetPreference: {"p1"},
	}
	for _, g := range got {
		if !slices.Equal(beliefIDs(g.Beliefs), wantIDs[g.Facet]) {
			t.Errorf("facet %s = %v, want %v", g.Facet, beliefIDs(g.Beliefs), wantIDs[g.Facet])
		}
		for _, b := range g.Beliefs {
			if !reflect.DeepEqual(b, w.seeded[b.ID]) {
				t.Errorf("belief %s = %+v, want the stored row %+v", b.ID, b, w.seeded[b.ID])
			}
			if b.Facet != g.Facet {
				t.Errorf("belief %s (facet %s) listed under %s", b.ID, b.Facet, g.Facet)
			}
		}
	}
}

// orderFixture is one facet with a tie at each level of the order: two
// beliefs that differ in confidence, two that tie on confidence and differ
// in last_reinforced_at, three that tie on both and differ in id.
func orderFixture(now time.Time) (inputs []ports.Belief, wantIDs []string) {
	mk := func(id string, conf float64, lra time.Time) ports.Belief {
		return ports.Belief{ID: id, Facet: selfmodel.FacetGoal, TopicKey: "derived/goal/" + id, Content: id, Confidence: conf,
			Origin: selfmodel.OriginDerived, Status: selfmodel.StatusActive, LastReinforcedAt: lra}
	}
	t1, t2, t3 := now.Add(-3*time.Hour), now.Add(-2*time.Hour), now.Add(-time.Hour)
	expected := []ports.Belief{
		mk("top", 0.90, t1),
		mk("z-newer", 0.80, t3),
		mk("a-older", 0.80, t2),
		mk("id-a", 0.50, t1),
		mk("id-b", 0.50, t1),
		mk("id-c", 0.50, t1),
	}
	return expected, beliefIDs(expected)
}

func TestBeliefsByFacet_OrdersConfidenceThenLastReinforcedThenID(t *testing.T) {
	now := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	expected, wantIDs := orderFixture(now)
	reverse := slices.Clone(expected)
	slices.Reverse(reverse)
	byID := map[string]ports.Belief{}
	for _, b := range expected {
		byID[b.ID] = b
	}
	var shuffled []ports.Belief
	for _, id := range []string{"id-b", "a-older", "id-c", "top", "id-a", "z-newer"} {
		shuffled = append(shuffled, byID[id])
	}

	for name, input := range map[string][]ports.Belief{"reverse of the expected order": reverse, "second permutation": shuffled} {
		t.Run(name, func(t *testing.T) {
			svc := NewBeliefsService(fixedClock{now}, &seqIDs{}, stubActive{beliefs: input}, memrepo.NewSignals(), memrepo.NewDecisionLog())

			got, err := svc.ByFacet(context.Background())
			if err != nil {
				t.Fatalf("ByFacet: %v", err)
			}
			goal := groupFor(t, got, selfmodel.FacetGoal)
			if !slices.Equal(beliefIDs(goal.Beliefs), wantIDs) {
				t.Errorf("goal order = %v, want %v", beliefIDs(goal.Beliefs), wantIDs)
			}
		})
	}
}

func TestBeliefsByFacet_SortsEachFacetOnItsOwn(t *testing.T) {
	now := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	mk := func(id string, facet selfmodel.Facet, conf float64) ports.Belief {
		return ports.Belief{ID: id, Facet: facet, Confidence: conf, Status: selfmodel.StatusActive, LastReinforcedAt: now}
	}
	input := []ports.Belief{
		mk("v-low", selfmodel.FacetValue, 0.2), mk("g-low", selfmodel.FacetGoal, 0.1),
		mk("v-high", selfmodel.FacetValue, 0.9), mk("g-high", selfmodel.FacetGoal, 0.8),
	}
	svc := NewBeliefsService(fixedClock{now}, &seqIDs{}, stubActive{beliefs: input}, memrepo.NewSignals(), memrepo.NewDecisionLog())

	got, err := svc.ByFacet(context.Background())
	if err != nil {
		t.Fatalf("ByFacet: %v", err)
	}
	if len(got) != len(selfmodel.AllFacets()) {
		t.Fatalf("groups = %v, want one per facet", facetsOf(got))
	}
	for _, g := range got {
		switch g.Facet {
		case selfmodel.FacetValue:
			if want := []string{"v-high", "v-low"}; !slices.Equal(beliefIDs(g.Beliefs), want) {
				t.Errorf("value = %v, want %v", beliefIDs(g.Beliefs), want)
			}
		case selfmodel.FacetGoal:
			if want := []string{"g-high", "g-low"}; !slices.Equal(beliefIDs(g.Beliefs), want) {
				t.Errorf("goal = %v, want %v", beliefIDs(g.Beliefs), want)
			}
		default:
			if len(g.Beliefs) != 0 {
				t.Errorf("facet %s = %v, want empty", g.Facet, beliefIDs(g.Beliefs))
			}
		}
	}
}

// memrepo iterates a Go map, so without the sort this fails with high
// probability on some run; the stub test above is what makes the kill
// deterministic.
func TestBeliefsByFacet_MemrepoOrderIsStableAcrossRuns(t *testing.T) {
	now := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	expected, wantIDs := orderFixture(now)
	repo := memrepo.NewSelfModel()
	for _, b := range expected {
		if err := repo.UpsertByTopicKey(context.Background(), b); err != nil {
			t.Fatalf("seed %s: %v", b.ID, err)
		}
	}
	svc := NewBeliefsService(fixedClock{now}, &seqIDs{}, repo, memrepo.NewSignals(), memrepo.NewDecisionLog())

	for run := 0; run < 20; run++ {
		got, err := svc.ByFacet(context.Background())
		if err != nil {
			t.Fatalf("run %d: ByFacet: %v", run, err)
		}
		goal := groupFor(t, got, selfmodel.FacetGoal)
		if !slices.Equal(beliefIDs(goal.Beliefs), wantIDs) {
			t.Fatalf("run %d: goal order = %v, want %v", run, beliefIDs(goal.Beliefs), wantIDs)
		}
	}
}

func TestBeliefsByFacet_ReadErrorIsReturned(t *testing.T) {
	boom := errBoom("read failed")
	svc := NewBeliefsService(fixedClock{time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)}, &seqIDs{}, stubActive{err: boom}, memrepo.NewSignals(), memrepo.NewDecisionLog())

	got, err := svc.ByFacet(context.Background())
	if !errors.Is(err, boom) {
		t.Fatalf("ByFacet error = %v, want it to wrap the read failure", err)
	}
	if got != nil {
		t.Errorf("ByFacet returned %+v with an error, want nothing", got)
	}
}

// groupFor returns facet's group, failing (not panicking) when absent.
func groupFor(t *testing.T, gs []FacetBeliefs, facet selfmodel.Facet) FacetBeliefs {
	t.Helper()
	for _, g := range gs {
		if g.Facet == facet {
			return g
		}
	}
	t.Fatalf("no %s group among %v", facet, facetsOf(gs))
	return FacetBeliefs{}
}

type errBoom string

func (e errBoom) Error() string { return string(e) }
