package brain

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/core/consolidation"
	"github.com/rengo/nooma/internal/core/recall"
	"github.com/rengo/nooma/internal/core/selfmodel"
	"github.com/rengo/nooma/internal/core/unit"
	"github.com/rengo/nooma/internal/ports"
	"github.com/rengo/nooma/test/support/fakeprovider"
	"github.com/rengo/nooma/test/support/memrepo"
)

// This file is m4e PR 2's brain half: derive's retired shield, its
// conditional embedding and the retired-vector failure policy (design
// §3.3, §6 D1-D23). Every belief and proposal vector is hand-built through
// scriptedEmbedder, so every cosine in a fixture is exact.

// seqIDs hands out predictable ids. fakeIDs (correction_test.go) renders
// its counter as a single rune, which stops being a legible id past nine.
type seqIDs struct{ n int }

func (g *seqIDs) New() string {
	g.n++
	return fmt.Sprintf("gen-%03d", g.n)
}

// shieldWorld is one derive pass over memrepo: seeded beliefs, a scripted
// judge response and a scripted embedder.
type shieldWorld struct {
	t         *testing.T
	now       time.Time
	units     *memrepo.Units
	base      *memrepo.SelfModel
	selfModel ports.SelfModelRepo // what derive sees; base unless a test wraps it
	log       *memrepo.DecisionLog
	emb       *scriptedEmbedder
	provider  ports.EmbeddingProvider // emb unless a test substitutes another
	proposals []map[string]any
	seeded    map[string]ports.Belief
	noJudge   bool
	nSeeds    int
	judge     *fakeprovider.Fake // set by run when the judge is scripted
}

func newShieldWorld(t *testing.T) *shieldWorld {
	t.Helper()
	w := &shieldWorld{
		t:      t,
		now:    time.Date(2026, 8, 1, 9, 30, 0, 0, time.UTC),
		units:  memrepo.NewUnits(),
		base:   memrepo.NewSelfModel(),
		log:    memrepo.NewDecisionLog(),
		emb:    newScriptedEmbedder("test-model"),
		seeded: map[string]ports.Belief{},
	}
	w.selfModel = w.base
	w.provider = w.emb
	ctx := context.Background()
	if err := w.units.Create(ctx, unit.Unit{
		ID: "u-source", Type: unit.TypeKnowledge, Status: unit.StatusPool,
		Content: "training log entry", Source: "chat",
		Weight: 1.0, WeightDecayRate: 0, LastTouchedAt: w.now, CreatedAt: w.now, UpdatedAt: w.now,
	}); err != nil {
		t.Fatalf("seed source unit: %v", err)
	}
	return w
}

type seedBelief struct {
	id      string
	key     string // full topic_key, e.g. "derived/goal/swim"
	content string
	conf    float64
	origin  selfmodel.Origin
	retired bool
	vec     []float32 // scripted for content; nil leaves it to the test
}

// seed stores a belief with non-default confidence and timestamps (so a
// column written by mistake shows) and returns the row as stored.
func (w *shieldWorld) seed(s seedBelief) ports.Belief {
	w.t.Helper()
	ctx := context.Background()
	w.nSeeds++
	age := time.Duration(w.nSeeds) * 24 * time.Hour
	facet := selfmodel.FacetGoal
	if parts := strings.SplitN(s.key, "/", 3); len(parts) == 3 {
		if f, err := selfmodel.ParseFacet(parts[1]); err == nil {
			facet = f
		}
	}
	origin := s.origin
	if origin == "" {
		origin = selfmodel.OriginDerived
	}
	b := ports.Belief{
		ID: s.id, Facet: facet, TopicKey: s.key, Content: s.content, Confidence: s.conf,
		Origin: origin, Status: selfmodel.StatusActive,
		LastReinforcedAt: w.now.Add(-age), CreatedAt: w.now.Add(-age - time.Hour), UpdatedAt: w.now.Add(-age),
	}
	if err := w.base.UpsertByTopicKey(ctx, b); err != nil {
		w.t.Fatalf("seed belief %s: %v", s.id, err)
	}
	if s.retired {
		if err := w.base.SetStatus(ctx, s.id, selfmodel.StatusActive, selfmodel.StatusRetired, w.now.Add(-age/2)); err != nil {
			w.t.Fatalf("retire seed %s: %v", s.id, err)
		}
	}
	if s.vec != nil {
		w.emb.Vector(s.content, s.vec...)
	}
	stored, err := w.base.BeliefByID(ctx, s.id)
	if err != nil {
		w.t.Fatalf("read back seed %s: %v", s.id, err)
	}
	w.seeded[s.id] = stored
	return stored
}

// propose appends one judge proposal. facetKey is "facet/key" as the judge
// writes it; the stored topic_key is derived/{facet}/{key}.
func (w *shieldWorld) propose(facet, key, content string, vec []float32) {
	w.t.Helper()
	w.proposals = append(w.proposals, map[string]any{"facet": facet, "topic_key": key, "content": content, "confidence": 0.7})
	if vec != nil {
		w.emb.Vector(content, vec...)
	}
}

func (w *shieldWorld) run() (ConsolidateReport, error) {
	w.t.Helper()
	ctx := context.Background()
	return w.runCtx(ctx)
}

func (w *shieldWorld) runCtx(ctx context.Context) (ConsolidateReport, error) {
	w.t.Helper()
	cfg := memrepo.NewConfig()
	if err := cfg.RecordConsolidationRun(ctx, w.now.Add(-time.Hour)); err != nil {
		w.t.Fatalf("seed since: %v", err)
	}
	var judge ports.LLMProvider = noJudge(w.t)
	if !w.noJudge {
		raw, err := json.Marshal(map[string]any{"beliefs": w.proposals})
		if err != nil {
			w.t.Fatalf("marshal proposals: %v", err)
		}
		dir := w.t.TempDir()
		writeDeriveCase(w.t, dir, "shield", string(raw))
		w.judge = fakeprovider.New(w.t, dir, "shield")
		judge = w.judge
	}
	rec := NewRecallService(NewIndex(recall.VectorIndex{Model: "test-model"}), memrepo.NewLexical(), w.units, w.provider)
	svc := NewConsolidateService(fixedClock{w.now}, cfg, w.units, memrepo.NewRelations(), &seqIDs{}, w.log, rec, judge, w.selfModel, memrepo.NewState(), memrepo.NewPendingQuestions())
	phase := consolidation.PhaseDerive
	return svc.Consolidate(ctx, ConsolidateRequest{Phase: &phase})
}

func (w *shieldWorld) mustRun() {
	w.t.Helper()
	if _, err := w.run(); err != nil {
		w.t.Fatalf("Consolidate(PhaseDerive): %v", err)
	}
}

// row is one decision_log row with its context decoded.
type row struct {
	Action    ports.DecisionAction
	Rationale string
	Ctx       map[string]any
}

func (w *shieldWorld) rows(action ports.DecisionAction) []row {
	w.t.Helper()
	all, err := w.log.Since(context.Background(), w.now.Add(-time.Hour), 1000)
	if err != nil {
		w.t.Fatalf("log.Since: %v", err)
	}
	var out []row
	for _, d := range all {
		if d.Action != action {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal(d.Context, &m); err != nil {
			w.t.Fatalf("decode context of %s row: %v\n%s", d.Action, err, d.Context)
		}
		out = append(out, row{Action: d.Action, Rationale: d.Rationale, Ctx: m})
	}
	return out
}

func (w *shieldWorld) allRows() int {
	w.t.Helper()
	all, err := w.log.Since(context.Background(), w.now.Add(-time.Hour), 1000)
	if err != nil {
		w.t.Fatalf("log.Since: %v", err)
	}
	return len(all)
}

func (w *shieldWorld) belief(id string) ports.Belief {
	w.t.Helper()
	b, err := w.base.BeliefByID(context.Background(), id)
	if err != nil {
		w.t.Fatalf("BeliefByID(%s): %v", id, err)
	}
	return b
}

func (w *shieldWorld) wantUnchanged(id string) {
	w.t.Helper()
	if got, want := w.belief(id), w.seeded[id]; !reflect.DeepEqual(got, want) {
		w.t.Errorf("belief %s changed:\n got  %+v\n want %+v", id, got, want)
	}
}

func keysOf(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

func (w *shieldWorld) rowFor(rows []row, topicKey string) row {
	w.t.Helper()
	for _, r := range rows {
		if r.Ctx["topic_key"] == topicKey {
			return r
		}
	}
	w.t.Fatalf("no row for topic_key %q among %d rows: %+v", topicKey, len(rows), rows)
	return row{}
}

// ---------- FX-D: the shield end to end ----------

const fxdDim = 15

func fxdWorld(t *testing.T) *shieldWorld {
	t.Helper()
	w := newShieldWorld(t)
	a1, a2 := math.Acos(0.95), math.Acos(0.90)
	beta := a2 - a1
	inPlane := func(theta float64) []float32 { return mix(fxdDim, 0, math.Cos(theta), 1, math.Sin(theta)) }

	w.seed(seedBelief{id: "r2", key: "derived/goal/swim", content: "R2 content", conf: 0.61, retired: true, vec: axis(fxdDim, 0)})
	w.seed(seedBelief{id: "a", key: "derived/value/health", content: "A content", conf: 0.50, vec: inPlane(beta)})
	w.seed(seedBelief{id: "r1", key: "derived/goal/marathon", content: "R1 content", conf: 0.72, retired: true, vec: axis(fxdDim, 2)})
	w.seed(seedBelief{id: "u", key: "derived/goal/read", content: "U content", conf: 0.40, origin: selfmodel.OriginUserStated, vec: axis(fxdDim, 3)})
	w.seed(seedBelief{id: "u2", key: "derived/goal/write", content: "U2 content", conf: 0.45, origin: selfmodel.OriginUserStated, vec: axis(fxdDim, 4)})
	w.seed(seedBelief{id: "a2", key: "derived/value/sleep", content: "A2 content", conf: 0.55, vec: mix(fxdDim, 5, 0.9, 6, cosAt(0.9))})
	w.seed(seedBelief{id: "r3", key: "derived/goal/cycle", content: "R3 content", conf: 0.66, retired: true, vec: mix(fxdDim, 5, 0.9, 7, cosAt(0.9))})
	w.seed(seedBelief{id: "d", key: "derived/goal/code", content: "D content", conf: 0.58, vec: axis(fxdDim, 8)})
	w.seed(seedBelief{id: "a5", key: "derived/value/focus", content: "A5 content", conf: 0.52, vec: axis(fxdDim, 13)})

	w.propose("goal", "fresh", "P0 content", axis(fxdDim, 11))                       // p0 create
	w.propose("goal", "marathon", "P1 content", axis(fxdDim, 10))                    // p1 retired key, far
	w.propose("goal", "new2", "P2 content", inPlane(-a1))                            // p2 retired 0.95 vs active 0.90
	w.propose("goal", "new3", "P3 content", inPlane(a2))                             // p3 active 0.95 vs retired 0.90
	w.propose("goal", "read", "P4 content", axis(fxdDim, 9))                         // p4 user-stated key
	w.propose("goal", "code", "P5 content", axis(fxdDim, 12))                        // p5 active derived key
	w.propose("goal", "new6", "P6 content", axis(fxdDim, 5))                         // p6 exact tie
	w.propose("goal", "write", "P7 content", mix(fxdDim, 13, 0.95, 14, cosAt(0.95))) // p7 user-stated key, active 0.95
	return w
}

func TestDerive_FXD_RoutesEachProposalAndLogsEachSkip(t *testing.T) {
	w := fxdWorld(t)

	w.mustRun()

	// Retired beliefs are never written (D10): the three rows are byte-identical.
	for _, id := range []string{"r1", "r2", "r3"} {
		w.wantUnchanged(id)
	}

	skips := w.rows(ports.ActionDeriveBeliefSkipped)
	if len(skips) != 3 {
		t.Fatalf("belief_skipped rows = %d, want 3 (p1, p2, p6): %+v", len(skips), skips)
	}
	// D9: the exact key set per reason, similarity omitted on a key match.
	p1 := w.rowFor(skips, "derived/goal/marathon")
	if want := []string{"belief_id", "proposed_content", "reason", "topic_key"}; !slices.Equal(keysOf(p1.Ctx), want) {
		t.Errorf("p1 (retired key) context keys = %v, want %v", keysOf(p1.Ctx), want)
	}
	if p1.Ctx["reason"] != "retired_topic_key" || p1.Ctx["belief_id"] != "r1" || p1.Ctx["proposed_content"] != "P1 content" {
		t.Errorf("p1 context = %+v, want reason retired_topic_key, belief r1, the proposal's content", p1.Ctx)
	}
	p2 := w.rowFor(skips, "derived/goal/new2")
	if want := []string{"belief_id", "proposed_content", "reason", "similarity", "topic_key"}; !slices.Equal(keysOf(p2.Ctx), want) {
		t.Errorf("p2 (retired similar) context keys = %v, want %v", keysOf(p2.Ctx), want)
	}
	if p2.Ctx["reason"] != "retired_similar" || p2.Ctx["belief_id"] != "r2" {
		t.Errorf("p2 context = %+v, want reason retired_similar, belief r2", p2.Ctx)
	}
	if sim, _ := p2.Ctx["similarity"].(float64); math.Abs(sim-0.95) > 1e-4 {
		t.Errorf("p2 similarity = %v, want 0.95", p2.Ctx["similarity"])
	}
	p6 := w.rowFor(skips, "derived/goal/new6")
	if p6.Ctx["reason"] != "retired_similar" || p6.Ctx["belief_id"] != "r3" {
		t.Errorf("p6 (tie) context = %+v, want retired_similar against r3: a tie goes to retired", p6.Ctx)
	}
	if p6.Rationale == "" {
		t.Error("belief_skipped rationale is empty: doc 02 §11 wants a legible sentence")
	}

	// p0 created, p5 overwritten in place (m2c control).
	created := w.rows(ports.ActionDeriveBeliefCreated)
	if len(created) != 2 {
		t.Errorf("belief_created rows = %d, want 2 (p0, p5): %+v", len(created), created)
	}
	if d := w.belief("d"); d.Content != "P5 content" || d.Origin != selfmodel.OriginDerived {
		t.Errorf("belief d = %+v, want its content overwritten in place by p5 and still derived", d)
	}
	var fresh *ports.Belief
	active, _ := w.base.ActiveBeliefs(context.Background())
	for i := range active {
		if active[i].TopicKey == "derived/goal/fresh" {
			fresh = &active[i]
		}
	}
	if fresh == nil || fresh.Content != "P0 content" {
		t.Errorf("p0 was not created as an active belief: %+v", fresh)
	}

	// p3, p4, p7 reinforce; p4 and p7 never rewrite the user's text.
	if got := w.belief("a").Confidence; got <= 0.50 {
		t.Errorf("a confidence = %v, want it raised by p3", got)
	}
	for _, id := range []string{"u", "u2"} {
		got, seeded := w.belief(id), w.seeded[id]
		if got.Confidence <= seeded.Confidence {
			t.Errorf("%s confidence = %v, want it raised above %v", id, got.Confidence, seeded.Confidence)
		}
		if got.Content != seeded.Content || got.Origin != selfmodel.OriginUserStated {
			t.Errorf("%s = %+v, want content %q and origin user_stated untouched", id, got, seeded.Content)
		}
	}
	w.wantUnchanged("a5") // p7 reinforced U2, not the nearer-by-vector A5
	if n := len(w.rows(ports.ActionDeriveBeliefReinforced)); n != 3 {
		t.Errorf("belief_reinforced rows = %d, want 3 (p3 a, p4 u, p7 u2)", n)
	}

	// D17: active + retired + pending, and p1 (decided by key) is not embedded.
	if got, want := w.emb.EmbedCalls(), 6+3+7; got != want {
		t.Errorf("EmbedCalls() = %d, want %d (6 active + 3 retired + 7 pending; p1 is decided by key)", got, want)
	}
	if slices.Contains(w.emb.Embedded(), "P1 content") {
		t.Error("P1 content was embedded: a proposal decided by key needs no vector")
	}
}

// ---------- D15, D16, D17: the conditional embed ----------

// D17, the rewrite's sibling: two active, one retired and ONE pending
// proposal embed active + retired + pending = 4 times, where the old
// fixture could only ever see 2.
func TestDerive_OnePendingProposalEmbedsActiveRetiredAndPending(t *testing.T) {
	w := newShieldWorld(t)
	w.seed(seedBelief{id: "b-1", key: "derived/goal/fitness", content: "wants to run more consistently", conf: 0.6, vec: []float32{1, 0, 0}})
	w.seed(seedBelief{id: "b-2", key: "derived/value/health", content: "values staying active", conf: 0.5, vec: []float32{0, 1, 0}})
	w.seed(seedBelief{id: "b-3", key: "derived/goal/swim", content: "wanted to swim", conf: 0.4, retired: true, vec: []float32{0, 0, 1}})
	w.propose("goal", "hiking", "enjoys weekend hiking trips", []float32{0.5, 0.5, -0.7})

	w.mustRun()

	if got, want := w.emb.EmbedCalls(), 4; got != want {
		t.Errorf("EmbedCalls() = %d, want %d (2 active + 1 retired + 1 pending): %v", got, want, w.emb.Embedded())
	}
	if n := len(w.rows(ports.ActionDeriveBeliefCreated)); n != 1 {
		t.Errorf("belief_created rows = %d, want 1: the proposal is far from every belief", n)
	}
}

// D16: a proposal already decided by key needs no vector, and when every
// proposal is key-decided nothing is embedded at all.
func TestDerive_AllKeyDecidedMakesNoEmbedCalls(t *testing.T) {
	w := newShieldWorld(t)
	w.seed(seedBelief{id: "a", key: "derived/value/health", content: "A content", conf: 0.5, vec: []float32{1, 0, 0}})
	w.seed(seedBelief{id: "r1", key: "derived/goal/marathon", content: "R1 content", conf: 0.7, retired: true, vec: []float32{0, 1, 0}})
	w.propose("goal", "marathon", "P1 content", []float32{0, 0, 1})

	w.mustRun()

	if got := w.emb.EmbedCalls(); got != 0 {
		t.Errorf("EmbedCalls() = %d, want 0: the only proposal is decided by retired key: %v", got, w.emb.Embedded())
	}
	skips := w.rows(ports.ActionDeriveBeliefSkipped)
	if len(skips) != 1 || skips[0].Ctx["reason"] != "retired_topic_key" {
		t.Fatalf("belief_skipped rows = %+v, want exactly one retired_topic_key", skips)
	}
	w.wantUnchanged("r1")
}

// ---------- D8, D11, D12: reads and the race arms ----------

type failingRetiredRead struct {
	*memrepo.SelfModel
	err error
}

func (f failingRetiredRead) RetiredBeliefs(context.Context) ([]ports.Belief, error) {
	return nil, f.err
}

func TestDerive_RetiredReadErrorFailsPhase(t *testing.T) {
	w := newShieldWorld(t)
	w.seed(seedBelief{id: "a", key: "derived/value/health", content: "A content", conf: 0.5, vec: []float32{1, 0, 0}})
	sentinel := errors.New("retired read broke")
	w.selfModel = failingRetiredRead{SelfModel: w.base, err: sentinel}
	w.noJudge = true // the read fails before the judge is asked

	_, err := w.run()

	if !errors.Is(err, sentinel) {
		t.Fatalf("Consolidate error = %v, want the retired-read failure to abort the phase", err)
	}
}

// raceSelfModel runs a hook between derive's read and its write, the way a
// UI click lands between them.
type raceSelfModel struct {
	*memrepo.SelfModel
	beforeUpsert    func(b ports.Belief)
	beforeReinforce func(id string)
}

func (r *raceSelfModel) UpsertByTopicKey(ctx context.Context, b ports.Belief) error {
	if r.beforeUpsert != nil {
		r.beforeUpsert(b)
	}
	return r.SelfModel.UpsertByTopicKey(ctx, b)
}

func (r *raceSelfModel) ReinforceByID(ctx context.Context, id string, confidence float64, at time.Time) error {
	if r.beforeReinforce != nil {
		r.beforeReinforce(id)
	}
	return r.SelfModel.ReinforceByID(ctx, id, confidence, at)
}

// D11: between derive's read and its write the user edits an active derived
// belief. The upsert is refused (ErrBeliefProtected); derive records a
// changed_since_read skip with no belief_id and goes on to the next
// proposal. The mutant "the store's refusal aborts the pass" fails here.
func TestDerive_ProtectedRaceSkipsAndContinues(t *testing.T) {
	w := newShieldWorld(t)
	w.seed(seedBelief{id: "d", key: "derived/goal/code", content: "D content", conf: 0.58, vec: []float32{1, 0, 0}})
	w.propose("goal", "code", "P5 content", []float32{0, 1, 0}) // overwrites d, unless it was edited meanwhile
	w.propose("goal", "fresh", "P0 content", []float32{0, 0, 1})
	race := &raceSelfModel{SelfModel: w.base}
	raced := false
	race.beforeUpsert = func(b ports.Belief) {
		if b.TopicKey == "derived/goal/code" && !raced {
			raced = true
			if err := w.base.EditContent(context.Background(), "d", "D content", "the user's own words", w.now); err != nil {
				t.Fatalf("simulate user edit: %v", err)
			}
		}
	}
	w.selfModel = race

	if _, err := w.run(); err != nil {
		t.Fatalf("Consolidate(PhaseDerive) = %v, want the protected race to be a skip, not an abort", err)
	}

	if !raced {
		t.Fatal("the race hook never ran: the upsert for the raced key was not attempted")
	}
	skips := w.rows(ports.ActionDeriveBeliefSkipped)
	if len(skips) != 1 {
		t.Fatalf("belief_skipped rows = %+v, want exactly one", skips)
	}
	got := skips[0].Ctx
	if want := []string{"proposed_content", "reason", "topic_key"}; !slices.Equal(keysOf(got), want) {
		t.Errorf("race skip context keys = %v, want %v: no belief_id when the store refused without naming one", keysOf(got), want)
	}
	if got["reason"] != "changed_since_read" || got["topic_key"] != "derived/goal/code" {
		t.Errorf("race skip context = %+v, want reason changed_since_read for derived/goal/code", got)
	}
	if d := w.belief("d"); d.Content != "the user's own words" || d.Origin != selfmodel.OriginUserStated {
		t.Errorf("belief d = %+v, want the user's edit intact", d)
	}
	if n := len(w.rows(ports.ActionDeriveBeliefCreated)); n != 1 {
		t.Errorf("belief_created rows = %d, want 1: the proposal after the race is still created", n)
	}
}

// D12: between derive's read and its reinforce the user retires the target.
// The reinforce is refused (ErrBeliefStatusConflict): a changed_since_read
// skip naming the target, and the pass goes on.
func TestDerive_RetiredDuringReinforceSkips(t *testing.T) {
	w := newShieldWorld(t)
	w.seed(seedBelief{id: "a", key: "derived/value/health", content: "A content", conf: 0.5, vec: []float32{1, 0, 0}})
	w.propose("goal", "near-a", "P3 content", mix(3, 0, 0.95, 1, cosAt(0.95)))
	w.propose("goal", "fresh", "P0 content", []float32{0, 0, 1})
	race := &raceSelfModel{SelfModel: w.base}
	raced := false
	race.beforeReinforce = func(id string) {
		if id == "a" && !raced {
			raced = true
			if err := w.base.SetStatus(context.Background(), "a", selfmodel.StatusActive, selfmodel.StatusRetired, w.now); err != nil {
				t.Fatalf("simulate user retire: %v", err)
			}
		}
	}
	w.selfModel = race

	if _, err := w.run(); err != nil {
		t.Fatalf("Consolidate(PhaseDerive) = %v, want the retire race to be a skip, not an abort", err)
	}

	if !raced {
		t.Fatal("the race hook never ran: the reinforce was not attempted")
	}
	skips := w.rows(ports.ActionDeriveBeliefSkipped)
	if len(skips) != 1 {
		t.Fatalf("belief_skipped rows = %+v, want exactly one", skips)
	}
	got := skips[0].Ctx
	if want := []string{"belief_id", "proposed_content", "reason", "topic_key"}; !slices.Equal(keysOf(got), want) {
		t.Errorf("race skip context keys = %v, want %v", keysOf(got), want)
	}
	if got["reason"] != "changed_since_read" || got["belief_id"] != "a" || got["topic_key"] != "derived/goal/near-a" {
		t.Errorf("race skip context = %+v, want changed_since_read on belief a for derived/goal/near-a", got)
	}
	if a := w.belief("a"); a.Status != selfmodel.StatusRetired || a.Confidence != 0.5 {
		t.Errorf("belief a = %+v, want it retired with its confidence unchanged", a)
	}
	if n := len(w.rows(ports.ActionDeriveBeliefReinforced)); n != 0 {
		t.Errorf("belief_reinforced rows = %d, want 0: the write did not land", n)
	}
	if n := len(w.rows(ports.ActionDeriveBeliefCreated)); n != 1 {
		t.Errorf("belief_created rows = %d, want 1: the proposal after the race is still created", n)
	}
}

// ---------- FX-D2: a retired belief whose vector is unusable (D18-D22) ----------

const fxd2Dim = 5

// fxd2World: active A (axis 0, the reference dimension), retired R4 (key
// derived/goal/old, vector scripted by the caller), pA sharing R4's key and
// pB with a new key far from everything. Without pB the pending list would
// be empty, derive would embed nothing, and R4's embedding would never be
// attempted (D16).
func fxd2World(t *testing.T) *shieldWorld {
	t.Helper()
	w := newShieldWorld(t)
	w.seed(seedBelief{id: "a", key: "derived/value/health", content: "A content", conf: 0.5, vec: axis(fxd2Dim, 0)})
	w.seed(seedBelief{id: "r4", key: "derived/goal/old", content: "R4 content", conf: 0.7, retired: true})
	w.propose("goal", "old", "PA content", axis(fxd2Dim, 3))
	w.propose("goal", "brand-new", "PB content", axis(fxd2Dim, 4))
	return w
}

func (w *shieldWorld) wantDegradedToKeyOnly(retiredIDs []string, cause string, embeds int) {
	w.t.Helper()
	fails := w.rows(ports.ActionDeriveRetiredEmbedFailed)
	if len(fails) != len(retiredIDs) {
		w.t.Fatalf("retired_embed_failed rows = %+v, want %d", fails, len(retiredIDs))
	}
	for _, id := range retiredIDs {
		var found bool
		for _, f := range fails {
			if f.Ctx["belief_id"] != id {
				continue
			}
			found = true
			if want := []string{"belief_id", "cause", "topic_key"}; !slices.Equal(keysOf(f.Ctx), want) {
				w.t.Errorf("%s failure context keys = %v, want %v", id, keysOf(f.Ctx), want)
			}
			if f.Ctx["cause"] != cause || f.Ctx["topic_key"] != w.seeded[id].TopicKey {
				w.t.Errorf("%s failure context = %+v, want cause %q and its topic_key", id, f.Ctx, cause)
			}
			if f.Rationale == "" {
				w.t.Errorf("%s failure rationale is empty", id)
			}
		}
		if !found {
			w.t.Errorf("no retired_embed_failed row for %s: %+v", id, fails)
		}
	}
	// pA is still skipped by key (D20): the failed embedding never makes a
	// retired belief re-derivable.
	skips := w.rows(ports.ActionDeriveBeliefSkipped)
	if len(skips) != 1 || skips[0].Ctx["reason"] != "retired_topic_key" || skips[0].Ctx["belief_id"] != "r4" {
		w.t.Errorf("belief_skipped rows = %+v, want exactly pA skipped by key against r4", skips)
	}
	w.wantUnchanged("r4")
	created := w.rows(ports.ActionDeriveBeliefCreated)
	if len(created) != 1 || created[0].Ctx["TopicKey"] != "derived/goal/brand-new" {
		w.t.Errorf("belief_created rows = %+v, want exactly pB", created)
	}
	if got := w.emb.EmbedCalls(); got != embeds {
		w.t.Errorf("EmbedCalls() = %d, want %d: %v", got, embeds, w.emb.Embedded())
	}
}

func TestDerive_RetiredEmbedFailureDegradesToKeyOnly(t *testing.T) {
	w := fxd2World(t)
	w.emb.Fail("R4 content", errors.New("provider hiccup"))

	w.mustRun()

	// 1 active + 1 retired (attempted) + 1 pending (pB; pA is key-decided).
	w.wantDegradedToKeyOnly([]string{"r4"}, "embed_error", 3)
}

func TestDerive_RetiredNonFiniteVectorDegrades(t *testing.T) {
	w := fxd2World(t)
	w.emb.Vector("R4 content", float32(math.NaN()), 0, 0, 0, 0)

	w.mustRun()

	w.wantDegradedToKeyOnly([]string{"r4"}, "non_finite", 3)
}

func TestDerive_RetiredInfiniteVectorDegrades(t *testing.T) {
	w := fxd2World(t)
	w.emb.Vector("R4 content", float32(math.Inf(1)), 0, 0, 0, 0)

	w.mustRun()

	w.wantDegradedToKeyOnly([]string{"r4"}, "non_finite", 3)
}

func TestDerive_RetiredZeroVectorDegrades(t *testing.T) {
	for _, tc := range []struct {
		name string
		vec  []float32
	}{
		{"all zeros", make([]float32, fxd2Dim)},
		{"empty", []float32{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := fxd2World(t)
			w.emb.Vector("R4 content", tc.vec...)

			w.mustRun()

			w.wantDegradedToKeyOnly([]string{"r4"}, "zero_vector", 3)
		})
	}
}

func TestDerive_RetiredRaggedVectorDegrades(t *testing.T) {
	t.Run("one component short", func(t *testing.T) {
		w := fxd2World(t)
		w.emb.Vector("R4 content", 1, 0, 0, 0)

		w.mustRun()

		w.wantDegradedToKeyOnly([]string{"r4"}, "dimension_mismatch", 3)
	})

	t.Run("every retired vector consistently the wrong length", func(t *testing.T) {
		w := fxd2World(t)
		w.seed(seedBelief{id: "r5", key: "derived/goal/older", content: "R5 content", conf: 0.6, retired: true})
		w.emb.Vector("R4 content", 1, 0, 0)
		w.emb.Vector("R5 content", 0, 1, 0)

		w.mustRun()

		fails := w.rows(ports.ActionDeriveRetiredEmbedFailed)
		if len(fails) != 2 {
			t.Fatalf("retired_embed_failed rows = %+v, want one per retired belief", fails)
		}
		for _, f := range fails {
			if f.Ctx["cause"] != "dimension_mismatch" {
				t.Errorf("failure context = %+v, want dimension_mismatch: they agree with each other, not with the active vectors", f.Ctx)
			}
		}
		if n := len(w.rows(ports.ActionDeriveBeliefCreated)); n != 1 {
			t.Errorf("belief_created rows = %d, want pB created", n)
		}
	})
}

// One bad retired belief does not blind the shield to the healthy ones.
func TestDerive_RetiredUnusableVectorKeepsHealthyRetiredMatching(t *testing.T) {
	w := fxd2World(t)
	w.seed(seedBelief{id: "r6", key: "derived/goal/healthy", content: "R6 content", conf: 0.6, retired: true,
		vec: mix(fxd2Dim, 4, 0.95, 2, cosAt(0.95))}) // 0.95 to pB
	w.emb.Vector("R4 content", float32(math.NaN()), 0, 0, 0, 0)

	w.mustRun()

	fails := w.rows(ports.ActionDeriveRetiredEmbedFailed)
	if len(fails) != 1 || fails[0].Ctx["belief_id"] != "r4" {
		t.Fatalf("retired_embed_failed rows = %+v, want only r4", fails)
	}
	skips := w.rows(ports.ActionDeriveBeliefSkipped)
	pB := w.rowFor(skips, "derived/goal/brand-new")
	if pB.Ctx["reason"] != "retired_similar" || pB.Ctx["belief_id"] != "r6" {
		t.Errorf("pB skip = %+v, want retired_similar against the healthy r6", pB.Ctx)
	}
}

// D21: a cancelled context is not the degrade policy; it aborts like any
// other cancelled read.
func TestDerive_CancelledEmbedAbortsPhase(t *testing.T) {
	w := fxd2World(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w.emb.CancelOn("R4 content", cancel)

	_, err := w.runCtx(ctx)

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Consolidate error = %v, want context.Canceled: a cancelled embed must abort, not degrade", err)
	}
	if n := len(w.rows(ports.ActionDeriveRetiredEmbedFailed)); n != 0 {
		t.Errorf("retired_embed_failed rows = %d, want 0: a cancellation is not an embedding failure", n)
	}
}

// D22: an active belief that fails to embed still aborts (today's
// behaviour): the active vectors are the trusted reference set.
func TestDerive_ActiveEmbedFailureAbortsPhase(t *testing.T) {
	w := fxd2World(t)
	sentinel := errors.New("active embed broke")
	w.emb.Fail("A content", sentinel)

	_, err := w.run()

	if !errors.Is(err, sentinel) {
		t.Fatalf("Consolidate error = %v, want the active embed failure to abort", err)
	}
	if n := w.allRows(); n != 0 {
		t.Errorf("decision_log rows = %d, want 0 after an aborted derive", n)
	}
}

// ---------- D19d: a proposal whose own vector is unusable ----------

// An unusable proposed vector is excluded from both merges. It is created
// (or, for a user-stated key, reinforces) and its rationale says so. It is
// FIRST in the list with active beliefs present: the odd proposal must be
// the one excluded, not the one that sets the reference dimension (design
// §10 item 2).
func TestDerive_ProposedUnusableVectorCreatesAndSaysSo(t *testing.T) {
	for _, tc := range []struct {
		name  string
		vec   []float32
		cause string
	}{
		{"zero", []float32{0, 0, 0}, "zero_vector"},
		{"non-finite", []float32{float32(math.NaN()), 0, 0}, "non_finite"},
		{"wrong dimension", []float32{1, 0}, "dimension_mismatch"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := newShieldWorld(t)
			w.seed(seedBelief{id: "a", key: "derived/value/health", content: "A content", conf: 0.5, vec: []float32{1, 0, 0}})
			w.propose("goal", "odd", "ODD content", tc.vec)
			w.propose("goal", "near-a", "NEAR content", mix(3, 0, 0.95, 1, cosAt(0.95)))

			w.mustRun() // a zero or short vector must not abort the phase

			created := w.rows(ports.ActionDeriveBeliefCreated)
			if len(created) != 1 {
				t.Fatalf("belief_created rows = %+v, want exactly the odd proposal", created)
			}
			if !strings.Contains(created[0].Rationale, "semantic comparison skipped") || !strings.Contains(created[0].Rationale, tc.cause) {
				t.Errorf("created rationale = %q, want it to say the semantic comparison was skipped and name %q", created[0].Rationale, tc.cause)
			}
			// The healthy proposal after the odd one is still compared and merges.
			reinforced := w.rows(ports.ActionDeriveBeliefReinforced)
			if len(reinforced) != 1 || reinforced[0].Ctx["belief_id"] != "a" {
				t.Fatalf("belief_reinforced rows = %+v, want NEAR reinforcing a: the odd first proposal must not set the reference dimension", reinforced)
			}
			if strings.Contains(reinforced[0].Rationale, "semantic comparison skipped") {
				t.Errorf("reinforced rationale = %q, want no skipped-comparison note on a proposal that was compared", reinforced[0].Rationale)
			}
		})
	}
}

func TestDerive_ProposedUnusableVectorOnUserStatedKeyReinforcesAndSaysSo(t *testing.T) {
	w := newShieldWorld(t)
	w.seed(seedBelief{id: "a", key: "derived/value/health", content: "A content", conf: 0.5, vec: []float32{1, 0, 0}})
	w.seed(seedBelief{id: "u", key: "derived/goal/read", content: "U content", conf: 0.4, origin: selfmodel.OriginUserStated, vec: []float32{0, 1, 0}})
	w.propose("goal", "read", "READ content", []float32{0, 0, 0})

	w.mustRun()

	reinforced := w.rows(ports.ActionDeriveBeliefReinforced)
	if len(reinforced) != 1 || reinforced[0].Ctx["belief_id"] != "u" {
		t.Fatalf("belief_reinforced rows = %+v, want u reinforced", reinforced)
	}
	if !strings.Contains(reinforced[0].Rationale, "semantic comparison skipped") || !strings.Contains(reinforced[0].Rationale, "zero_vector") {
		t.Errorf("reinforced rationale = %q, want the skipped-comparison note naming zero_vector", reinforced[0].Rationale)
	}
	if u := w.belief("u"); u.Content != "U content" || u.Confidence <= 0.4 {
		t.Errorf("belief u = %+v, want its text untouched and its confidence raised", u)
	}
	if n := len(w.rows(ports.ActionDeriveBeliefCreated)); n != 0 {
		t.Errorf("belief_created rows = %d, want 0", n)
	}
}

// No active beliefs: the first USABLE proposal sets the reference
// dimension. A zero-vector proposal that is also the wrong length comes
// first and must not set it, or the healthy retired belief would be dropped
// as a mismatch.
func TestDerive_NoActiveBeliefsNextUsableProposalSetsDimension(t *testing.T) {
	w := newShieldWorld(t)
	w.seed(seedBelief{id: "r", key: "derived/goal/swim", content: "R content", conf: 0.6, retired: true, vec: []float32{1, 0, 0}})
	w.propose("goal", "odd", "ODD content", []float32{0, 0})
	w.propose("goal", "swim-again", "SWIM content", mix(3, 0, 0.95, 1, cosAt(0.95)))

	w.mustRun()

	if fails := w.rows(ports.ActionDeriveRetiredEmbedFailed); len(fails) != 0 {
		t.Fatalf("retired_embed_failed rows = %+v, want none: the retired vector agrees with the first usable proposal", fails)
	}
	skips := w.rows(ports.ActionDeriveBeliefSkipped)
	if len(skips) != 1 || skips[0].Ctx["reason"] != "retired_similar" || skips[0].Ctx["belief_id"] != "r" {
		t.Errorf("belief_skipped rows = %+v, want SWIM skipped as similar to the retired belief", skips)
	}
	created := w.rows(ports.ActionDeriveBeliefCreated)
	if len(created) != 1 || !strings.Contains(created[0].Rationale, "zero_vector") {
		t.Errorf("belief_created rows = %+v, want the odd proposal created with its cause in the rationale", created)
	}
}

// With no active beliefs the first usable proposal fixes the reference
// dimension for everything after it: a later proposal of another length is
// excluded, and so is a retired vector of another length.
func TestDerive_NoActiveBeliefsFirstUsableProposalFixesDimension(t *testing.T) {
	w := newShieldWorld(t)
	w.seed(seedBelief{id: "r", key: "derived/goal/swim", content: "R content", conf: 0.6, retired: true, vec: []float32{1, 0, 0, 0}})
	w.propose("goal", "first", "FIRST content", []float32{0, 1, 0})
	w.propose("goal", "short", "SHORT content", []float32{1, 0})

	w.mustRun()

	created := w.rows(ports.ActionDeriveBeliefCreated)
	if len(created) != 2 {
		t.Fatalf("belief_created rows = %+v, want both proposals created", created)
	}
	for _, c := range created {
		if c.Ctx["TopicKey"] == "derived/goal/short" && !strings.Contains(c.Rationale, "dimension_mismatch") {
			t.Errorf("short proposal rationale = %q, want the dimension_mismatch note", c.Rationale)
		}
		if c.Ctx["TopicKey"] == "derived/goal/first" && strings.Contains(c.Rationale, "skipped") {
			t.Errorf("first proposal rationale = %q, want no skipped-comparison note: it set the dimension", c.Rationale)
		}
	}
	fails := w.rows(ports.ActionDeriveRetiredEmbedFailed)
	if len(fails) != 1 || fails[0].Ctx["cause"] != "dimension_mismatch" {
		t.Errorf("retired_embed_failed rows = %+v, want r dropped as dimension_mismatch against the first proposal", fails)
	}
}

// When no proposal has a usable vector there is nothing to compare, so
// nothing is merged: retired vectors that disagree with each other (no
// reference exists to screen them by) must not fail a merge that has no
// query.
func TestDerive_NoUsableProposalSkipsTheMergeEntirely(t *testing.T) {
	w := newShieldWorld(t)
	w.seed(seedBelief{id: "r1", key: "derived/goal/swim", content: "R1 content", conf: 0.6, retired: true, vec: []float32{1, 0, 0}})
	w.seed(seedBelief{id: "r2", key: "derived/goal/cycle", content: "R2 content", conf: 0.5, retired: true, vec: []float32{1, 0, 0, 0}})
	w.propose("goal", "odd", "ODD content", []float32{0, 0, 0})

	w.mustRun()

	created := w.rows(ports.ActionDeriveBeliefCreated)
	if len(created) != 1 || !strings.Contains(created[0].Rationale, "zero_vector") {
		t.Errorf("belief_created rows = %+v, want the odd proposal created without a comparison", created)
	}
}

// ---------- what stays as it was ----------

// A proposal that fails to EMBED still aborts the phase: an embed error is
// a provider fault that clears on retry, unlike an unusable vector, which
// is deterministic per input (design RK-10).
func TestDerive_ProposedEmbedFailureAbortsPhase(t *testing.T) {
	w := newShieldWorld(t)
	w.seed(seedBelief{id: "a", key: "derived/value/health", content: "A content", conf: 0.5, vec: []float32{1, 0, 0}})
	sentinel := errors.New("proposal embed broke")
	w.propose("goal", "fresh", "P0 content", nil)
	w.emb.Fail("P0 content", sentinel)

	_, err := w.run()

	if !errors.Is(err, sentinel) {
		t.Fatalf("Consolidate error = %v, want the proposal's embed failure to abort", err)
	}
}

type failingWrites struct {
	*memrepo.SelfModel
	upsertErr, reinforceErr error
}

func (f failingWrites) UpsertByTopicKey(ctx context.Context, b ports.Belief) error {
	if f.upsertErr != nil {
		return f.upsertErr
	}
	return f.SelfModel.UpsertByTopicKey(ctx, b)
}

func (f failingWrites) ReinforceByID(ctx context.Context, id string, c float64, at time.Time) error {
	if f.reinforceErr != nil {
		return f.reinforceErr
	}
	return f.SelfModel.ReinforceByID(ctx, id, c, at)
}

// Only the two refusals that mean "the user changed it" are skips. Any
// other store error still aborts the pass, as it always did.
func TestDerive_OtherStoreErrorsStillAbort(t *testing.T) {
	boom := errors.New("disk on fire")
	t.Run("upsert", func(t *testing.T) {
		w := newShieldWorld(t)
		w.seed(seedBelief{id: "a", key: "derived/value/health", content: "A content", conf: 0.5, vec: []float32{1, 0, 0}})
		w.propose("goal", "fresh", "P0 content", []float32{0, 1, 0})
		w.selfModel = failingWrites{SelfModel: w.base, upsertErr: boom}

		if _, err := w.run(); !errors.Is(err, boom) {
			t.Fatalf("Consolidate error = %v, want the upsert failure to abort", err)
		}
		if n := len(w.rows(ports.ActionDeriveBeliefSkipped)); n != 0 {
			t.Errorf("belief_skipped rows = %d, want 0: only a refusal is a skip", n)
		}
	})
	t.Run("reinforce", func(t *testing.T) {
		w := newShieldWorld(t)
		w.seed(seedBelief{id: "a", key: "derived/value/health", content: "A content", conf: 0.5, vec: []float32{1, 0, 0}})
		w.propose("goal", "near-a", "P3 content", mix(3, 0, 0.95, 1, cosAt(0.95)))
		w.selfModel = failingWrites{SelfModel: w.base, reinforceErr: boom}

		if _, err := w.run(); !errors.Is(err, boom) {
			t.Fatalf("Consolidate error = %v, want the reinforce failure to abort", err)
		}
		if n := len(w.rows(ports.ActionDeriveBeliefSkipped)); n != 0 {
			t.Errorf("belief_skipped rows = %d, want 0: only a refusal is a skip", n)
		}
	})
}

// BuildDerivePrompt is unchanged: the judge is shown active beliefs only.
// The retired shield is code after the judge, not a request to it.
func TestDerive_PromptShowsActiveBeliefsOnly(t *testing.T) {
	w := newShieldWorld(t)
	w.seed(seedBelief{id: "a", key: "derived/value/health", content: "ACTIVE-BELIEF-TEXT", conf: 0.5, vec: []float32{1, 0, 0}})
	w.seed(seedBelief{id: "r", key: "derived/goal/swim", content: "RETIRED-BELIEF-TEXT", conf: 0.6, retired: true, vec: []float32{0, 1, 0}})

	w.mustRun()

	prompts := w.judge.SeenPrompts()
	if len(prompts) != 1 {
		t.Fatalf("SeenPrompts() = %d, want 1", len(prompts))
	}
	if !strings.Contains(prompts[0], "ACTIVE-BELIEF-TEXT") {
		t.Errorf("prompt does not show the active belief:\n%s", prompts[0])
	}
	if strings.Contains(prompts[0], "RETIRED-BELIEF-TEXT") || strings.Contains(prompts[0], "derived/goal/swim") {
		t.Errorf("prompt shows the retired belief; it must stay invisible to the judge:\n%s", prompts[0])
	}
}
