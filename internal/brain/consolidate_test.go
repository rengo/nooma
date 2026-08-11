package brain

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/core/consolidation"
	"github.com/rengo/nooma/internal/core/recall"
	"github.com/rengo/nooma/internal/core/selfmodel"
	"github.com/rengo/nooma/internal/core/unit"
	"github.com/rengo/nooma/internal/core/weight"
	"github.com/rengo/nooma/internal/ports"
	"github.com/rengo/nooma/test/support/fakeprovider"
	"github.com/rengo/nooma/test/support/goldenset"
	"github.com/rengo/nooma/test/support/memrepo"
)

// testRecall returns a *RecallService wired over empty fakes — every
// existing test in this file predates connect (PR 9), so none of them
// exercises recall at all; this is the minimal non-nil value
// NewConsolidateService now requires (design §7.1's own widened
// constructor). Tests that DO exercise connect's own recall step build
// their own RecallService instead, over fixtures that give it something to
// find — testRecallOver below, when the fixture's own units store already
// exists.
func testRecall() *RecallService {
	return testRecallOver(memrepo.NewUnits())
}

// testRecallOver is testRecall's own parametrized form: a *RecallService
// over units — a fixture's own already-seeded ports.UnitRepo — with an
// empty lexical index and index (design §7.1's own recall step degrades
// gracefully to "no candidates found" when nothing is seeded there, the
// same product rule every other recall caller relies on).
func testRecallOver(units ports.UnitRepo) *RecallService {
	return NewRecallService(NewIndex(recall.VectorIndex{Model: "test-model"}), memrepo.NewLexical(), units, fakeprovider.NewEmbeddingFake("test-model"))
}

// noJudge is fakeprovider.New with zero scripted cases — the value every
// fixture that predates PR 9b's judge call, or whose recall step finds no
// candidates to judge, passes to NewConsolidateService's own widened judge
// parameter (design §7.1). An unexpected Complete call fails the test
// loudly (fakeprovider's own unscripted-call guard, fakeprovider.go) rather
// than reaching a real provider (CLAUDE.md non-negotiable #5) or panicking
// on a nil ports.LLMProvider.
func noJudge(t *testing.T) *fakeprovider.Fake {
	return fakeprovider.New(t, "")
}

// testdataLLMCasesDir returns testdata/llm/cases, resolved from the repo
// root — the identical idiom test/conformance/capture_clock_test.go's own
// repoRootFromCaller uses, restated here rather than imported: package
// brain and package conformance are siblings two directories below the
// repo root (internal/brain, test/conformance), so the same
// runtime.Caller(0)-plus-three-Dir() walk resolves correctly from either,
// and internal/brain has no dependency on test/conformance to import from
// (docs/06-harness.md §1's package boundary). Exists so connect's own
// judge fixtures (below) can replay the EXACT decision-table cases
// capture's own relation-judge tests already prove (task 9.4's own
// instruction: not a new decision table), rather than inventing a second,
// parallel set of scripted responses.
func testdataLLMCasesDir(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller(0) failed to report this test file's path")
	}
	// thisFile is .../internal/brain/consolidate_test.go
	repoRoot := filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
	return filepath.Join(repoRoot, "testdata", "llm", "cases")
}

// writeDeriveCase writes a throwaway belief_derivation golden case under
// dir (a t.TempDir(), never testdata/llm/cases/) — derive's own judge
// fixtures do not join the shared corpus PR 9b's own connect fixtures
// reuse (testdataLLMCasesDir, above): belief_derivation is not one of
// cmd/nooma/doctor.go's jsonTasks (capture_processing, relation_evaluation
// only), so a case added to the shared corpus would sit there unchecked by
// the live quality gate and outside this PR's own declared diff scope
// (tasks.md's PR-level Verify: internal/brain/consolidate.go, its test,
// and doc 02 §13 only). fakeprovider_test.go's own writeCase draws the
// identical distinction for its own throwaway fixtures — restated here
// rather than imported, since that helper lives in package
// fakeprovider_test, not exported for another package to call.
func writeDeriveCase(t *testing.T, dir, id, response string) {
	t.Helper()
	ex := goldenset.LLMExample{
		ID:       id,
		Provider: "test",
		Model:    "test-model",
		Task:     "belief_derivation",
		Message:  "derive fixture — Message only feeds nooma doctor's live gate, which does not cover belief_derivation; Complete replays strictly by case id (fakeprovider.go)",
		Response: response,
	}
	data, err := json.Marshal(ex)
	if err != nil {
		t.Fatalf("marshal derive case %q: %v", id, err)
	}
	if err := os.WriteFile(filepath.Join(dir, id+".json"), data, 0o644); err != nil {
		t.Fatalf("write derive case %q: %v", id, err)
	}
}

// This file lives inside package brain (white-box), not test/conformance,
// matching correction_test.go's own precedent (tasks.md Conflicts §C5):
// consolidateRunner and its unexported helpers have no caller outside this
// package, so a test proving I11's behavioural half (spec R4.1) has to sit
// beside the code it drives. design.md §9's test matrix names
// "test/conformance/i11_..." for this same property; tasks.md 7a.1 assigns
// it to this file instead, and this file follows tasks.md — the executable
// instruction — disclosed here rather than silently reconciled.
//
// ConsolidateReport.phasesRun (consolidate.go) is this PR's own answer to
// spec R4.1's "spy recording each phase's invocation" requirement: no
// production case in runPhase's switch calls any port this PR wires yet
// (every arm is a placeholder — design §3.3(b)), so there is nothing else
// for a spy to observe. Unexported deliberately, per Judgment Day/verify
// feedback on this PR: this file is white-box (package brain), so an
// unexported field gives identical observability without committing
// public API surface design.md never named — PR 12's own report-rendering
// shape is a future decision, not this PR's to make.

// fixedClock is a deterministic ports.Clock for this file only, mirroring
// correction_test.go's fakeIDs precedent for a small package-local test
// double.
type fixedClock struct{ now time.Time }

func (c fixedClock) Now() time.Time { return c.now }

// spyConfig wraps memrepo.Config to count ports.ConfigRepo.Load and
// RecordConsolidationRun calls. "since read once" and
// "consolidation_last_run_at written once" (spec R5.3, R5.4; design
// §3.3(c)-(d)) are call-count claims, not stored-value claims — a plain
// memrepo.Config fixture cannot prove either by itself.
type spyConfig struct {
	*memrepo.Config
	loadCalls   int
	recordCalls int
	recordAts   []time.Time
}

func newSpyConfig() *spyConfig {
	return &spyConfig{Config: memrepo.NewConfig()}
}

func (s *spyConfig) Load(ctx context.Context) (ports.VaultConfig, error) {
	s.loadCalls++
	return s.Config.Load(ctx)
}

func (s *spyConfig) RecordConsolidationRun(ctx context.Context, at time.Time) error {
	s.recordCalls++
	s.recordAts = append(s.recordAts, at)
	return s.Config.RecordConsolidationRun(ctx, at)
}

// TestConsolidate_WholePassReachesEveryPhaseInOrder is spec R4.1's own
// scenario (I11's behavioural half, design §3.3(b)): a whole pass
// (ConsolidateRequest's zero value) reaches every consolidation.Order()
// member, in that exact sequence, with PhaseLearn's slot reached last.
func TestConsolidate_WholePassReachesEveryPhaseInOrder(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 8, 10, 3, 0, 0, 0, time.UTC)
	svc := NewConsolidateService(fixedClock{now}, memrepo.NewConfig(), memrepo.NewUnits(), memrepo.NewRelations(), &fakeIDs{}, memrepo.NewDecisionLog(), testRecall(), noJudge(t), memrepo.NewSelfModel(), memrepo.NewState())

	report, err := svc.Consolidate(ctx, ConsolidateRequest{})
	if err != nil {
		t.Fatalf("Consolidate: %v", err)
	}

	want := consolidation.Order()
	if len(report.phasesRun) != len(want) {
		t.Fatalf("PhasesRun = %v, want %v", report.phasesRun, want)
	}
	for i, p := range want {
		if report.phasesRun[i] != p {
			t.Errorf("PhasesRun[%d] = %s, want %s", i, report.phasesRun[i], p)
		}
	}
	if last := report.phasesRun[len(report.phasesRun)-1]; last != consolidation.PhaseLearn {
		t.Errorf("last phase run = %s, want PhaseLearn (I11)", last)
	}
}

// TestConsolidate_PerPhase is design §3.3(a)-(b)'s per-phase scope: a
// ConsolidateRequest naming one Phase reaches exactly that phase's arm and
// no other; a Phase value outside consolidation.Order()'s range errors
// through consolidation.ErrUnknownPhase rather than being silently
// skipped.
//
// The third subtest below was added in round 1 of this PR's Judgment Day:
// both blind judges independently confirmed that an out-of-range
// req.Phase reached svc.Consolidate as a silent no-op success (the filter
// loop's continue never matches it, so runPhase — the only place the old
// code produced an error — was never called), because the only prior
// coverage of "unknown phase" called runPhase directly and never drove
// the request through the real entry point. Fixed in consolidate.go's
// `at` by checking for an empty phasesRun after a non-nil req.Phase.
func TestConsolidate_PerPhase(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 8, 10, 3, 0, 0, 0, time.UTC)

	t.Run("reaches exactly the requested phase", func(t *testing.T) {
		svc := NewConsolidateService(fixedClock{now}, memrepo.NewConfig(), memrepo.NewUnits(), memrepo.NewRelations(), &fakeIDs{}, memrepo.NewDecisionLog(), testRecall(), noJudge(t), memrepo.NewSelfModel(), memrepo.NewState())
		phase := consolidation.PhaseArchive

		report, err := svc.Consolidate(ctx, ConsolidateRequest{Phase: &phase})
		if err != nil {
			t.Fatalf("Consolidate: %v", err)
		}
		if len(report.phasesRun) != 1 || report.phasesRun[0] != consolidation.PhaseArchive {
			t.Fatalf("PhasesRun = %v, want exactly [PhaseArchive]", report.phasesRun)
		}
	})

	t.Run("an unknown phase errors through runPhase's default", func(t *testing.T) {
		r := consolidateRunner{}
		if err := r.runPhase(ctx, consolidation.Phase(99), passContext{}, &ConsolidateReport{}); err == nil {
			t.Fatal("runPhase(99) error = nil, want an error naming the unhandled phase")
		}
	})

	t.Run("an unknown phase errors through Consolidate itself, not just runPhase", func(t *testing.T) {
		svc := NewConsolidateService(fixedClock{now}, memrepo.NewConfig(), memrepo.NewUnits(), memrepo.NewRelations(), &fakeIDs{}, memrepo.NewDecisionLog(), testRecall(), noJudge(t), memrepo.NewSelfModel(), memrepo.NewState())
		unknown := consolidation.Phase(99)

		report, err := svc.Consolidate(ctx, ConsolidateRequest{Phase: &unknown})
		if !errors.Is(err, consolidation.ErrUnknownPhase) {
			t.Fatalf("Consolidate(Phase(99)) error = %v, want errors.Is(err, consolidation.ErrUnknownPhase)", err)
		}
		if len(report.phasesRun) != 0 {
			t.Fatalf("PhasesRun = %v, want empty — an unknown phase must run nothing", report.phasesRun)
		}
	})
}

// TestConsolidate_SinceReadOnceBeforeAnyPhase is spec R5.3 and design
// §3.3(c): passContext.since is cfg.ConsolidationLastRunAt, read from
// ports.ConfigRepo.Load exactly once per invocation — whole pass or single
// phase — before any phase runs. This PR ships no phase consumer of since
// yet (PR 8/9 add Strengthen/SelectConnectSources), so the assertion is
// against buildPassContext's own return value and against Load's call
// count, not against a phase's observed behaviour.
func TestConsolidate_SinceReadOnceBeforeAnyPhase(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 8, 10, 3, 0, 0, 0, time.UTC)

	t.Run("nil since with no config row", func(t *testing.T) {
		r := consolidateRunner{cfg: memrepo.NewConfig()}
		pass, err := r.buildPassContext(ctx, now)
		if err != nil {
			t.Fatalf("buildPassContext: %v", err)
		}
		if pass.since != nil {
			t.Errorf("since = %v, want nil with no config row", pass.since)
		}
	})

	t.Run("since mirrors the stored ConsolidationLastRunAt", func(t *testing.T) {
		cfg := memrepo.NewConfig()
		last := now.Add(-48 * time.Hour)
		if err := cfg.RecordConsolidationRun(ctx, last); err != nil {
			t.Fatalf("seed RecordConsolidationRun: %v", err)
		}
		r := consolidateRunner{cfg: cfg}

		pass, err := r.buildPassContext(ctx, now)
		if err != nil {
			t.Fatalf("buildPassContext: %v", err)
		}
		if pass.since == nil || !pass.since.Equal(last) {
			t.Errorf("since = %v, want %v", pass.since, last)
		}
	})

	t.Run("Load is called exactly once for a whole pass", func(t *testing.T) {
		cfg := newSpyConfig()
		svc := NewConsolidateService(fixedClock{now}, cfg, memrepo.NewUnits(), memrepo.NewRelations(), &fakeIDs{}, memrepo.NewDecisionLog(), testRecall(), noJudge(t), memrepo.NewSelfModel(), memrepo.NewState())
		if _, err := svc.Consolidate(ctx, ConsolidateRequest{}); err != nil {
			t.Fatalf("Consolidate: %v", err)
		}
		if cfg.loadCalls != 1 {
			t.Errorf("Load calls = %d, want exactly 1", cfg.loadCalls)
		}
	})

	t.Run("Load is called exactly once for a per-phase run too", func(t *testing.T) {
		cfg := newSpyConfig()
		svc := NewConsolidateService(fixedClock{now}, cfg, memrepo.NewUnits(), memrepo.NewRelations(), &fakeIDs{}, memrepo.NewDecisionLog(), testRecall(), noJudge(t), memrepo.NewSelfModel(), memrepo.NewState())
		phase := consolidation.PhaseStrengthen
		if _, err := svc.Consolidate(ctx, ConsolidateRequest{Phase: &phase}); err != nil {
			t.Fatalf("Consolidate: %v", err)
		}
		if cfg.loadCalls != 1 {
			t.Errorf("Load calls = %d, want exactly 1", cfg.loadCalls)
		}
	})
}

// TestConsolidate_RecordsConsolidationRunOnce is spec R5.4 and design
// §3.3(d): RecordConsolidationRun is called exactly once per whole pass,
// with the pass's own now, and never for a per-phase run — one call site,
// gated on the same field (req.Phase) that selected the scope.
func TestConsolidate_RecordsConsolidationRunOnce(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 8, 10, 3, 0, 0, 0, time.UTC)

	t.Run("whole pass records exactly once with the pass's own now", func(t *testing.T) {
		cfg := newSpyConfig()
		svc := NewConsolidateService(fixedClock{now}, cfg, memrepo.NewUnits(), memrepo.NewRelations(), &fakeIDs{}, memrepo.NewDecisionLog(), testRecall(), noJudge(t), memrepo.NewSelfModel(), memrepo.NewState())
		if _, err := svc.Consolidate(ctx, ConsolidateRequest{}); err != nil {
			t.Fatalf("Consolidate: %v", err)
		}
		if cfg.recordCalls != 1 {
			t.Fatalf("RecordConsolidationRun calls = %d, want exactly 1", cfg.recordCalls)
		}
		if !cfg.recordAts[0].Equal(now) {
			t.Errorf("RecordConsolidationRun at = %v, want the pass's own now %v", cfg.recordAts[0], now)
		}
	})

	t.Run("per-phase run never records", func(t *testing.T) {
		cfg := newSpyConfig()
		svc := NewConsolidateService(fixedClock{now}, cfg, memrepo.NewUnits(), memrepo.NewRelations(), &fakeIDs{}, memrepo.NewDecisionLog(), testRecall(), noJudge(t), memrepo.NewSelfModel(), memrepo.NewState())
		phase := consolidation.PhaseLearn
		if _, err := svc.Consolidate(ctx, ConsolidateRequest{Phase: &phase}); err != nil {
			t.Fatalf("Consolidate: %v", err)
		}
		if cfg.recordCalls != 0 {
			t.Errorf("RecordConsolidationRun calls = %d, want 0 for a per-phase run", cfg.recordCalls)
		}
	})
}

// TestConsolidateRunner_Record is spec R4.2's I12 direction 1, exercised at
// the one call site design §3.3(e) names — record — rather than through
// runPhase's dispatch: every arm runPhase's switch declares is still a
// placeholder (design §3.3(b), PR 7a), so there is no real per-phase
// "invocation slot" for a spy to observe yet. This PR's own honest
// substitute (disclosed here rather than silently reconciled, tasks.md
// 7b.2's own framing): one record call stands in for one phase's one
// persistable effect, using each of the ten new ports.DecisionAction
// members design §7.5 enumerates as the ten "invocation slots" — proving
// record itself persists exactly one decision_log row per call, with an
// Action that distinguishes which slot produced it. PR 8-11 wire the real
// per-phase call sites that reach record through runPhase; I11's own
// whole-pass ordering property (TestConsolidate_WholePassReachesEveryPhaseInOrder,
// above) already proves the runner reaches every slot in order.
func TestConsolidateRunner_Record(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 8, 10, 3, 0, 0, 0, time.UTC)

	slots := []struct {
		action ports.DecisionAction
		detail any
	}{
		{ports.ActionExpireIncompleteTransitioned, struct {
			UnitID string `json:"unit_id"`
		}{"u-expire"}},
		{ports.ActionArchiveArchived, struct {
			UnitID string `json:"unit_id"`
		}{"u-archive"}},
		{ports.ActionStrengthenApplied, struct {
			RelationID string `json:"relation_id"`
		}{"r-strengthen"}},
		{ports.ActionConnectRelationPersisted, struct {
			RelationID string `json:"relation_id"`
		}{"r-connect"}},
		{ports.ActionDeriveBeliefCreated, struct {
			TopicKey string `json:"topic_key"`
		}{"tk-created"}},
		{ports.ActionDeriveBeliefReinforced, struct {
			TopicKey string `json:"topic_key"`
		}{"tk-reinforced"}},
		{ports.ActionReweightBoostApplied, struct {
			UnitID string `json:"unit_id"`
		}{"u-reweight"}},
		{ports.ActionPatternEvalStagnationFound, struct {
			GoalUnitID string `json:"goal_unit_id"`
		}{"u-stagnant"}},
		{ports.ActionPatternEvalLoadHypothesisOpened, struct {
			LastHypothesisAt *time.Time `json:"last_hypothesis_at"`
		}{nil}},
		{ports.ActionArchiveConflictSkipped, struct {
			UnitID string `json:"unit_id"`
		}{"u-skipped"}},
	}

	log := memrepo.NewDecisionLog()
	r := consolidateRunner{ids: &fakeIDs{}, log: log}

	for i, slot := range slots {
		rationale := "slot " + string(slot.action) + " persisted"
		if err := r.record(ctx, now, slot.action, rationale, slot.detail); err != nil {
			t.Fatalf("record(%d, %s): %v", i, slot.action, err)
		}
	}

	rows, err := log.Since(ctx, now.Add(-time.Second), len(slots)+1)
	if err != nil {
		t.Fatalf("Since: %v", err)
	}
	if len(rows) != len(slots) {
		t.Fatalf("decision_log rows = %d, want exactly %d — one per persisted effect (spec R4.2)", len(rows), len(slots))
	}

	seen := make(map[ports.DecisionAction]int)
	for _, row := range rows {
		seen[row.Action]++
		if row.Rationale == "" {
			t.Errorf("row %s: empty Rationale, want a legible sentence (spec R6.4)", row.Action)
		}
	}
	for _, slot := range slots {
		if seen[slot.action] != 1 {
			t.Errorf("Action %s appeared %d times, want exactly 1 — a reader must be able to tell which slot wrote which row (spec R4.2)", slot.action, seen[slot.action])
		}
	}
}

// TestConsolidate_NoEffects is spec R4.2's I12 direction 2: a whole pass
// where no phase has qualifying input completes and decision_log gains
// zero rows. Every arm runPhase's switch declares is still a placeholder
// (design §3.3(b), PR 7a) — none of them calls record unconditionally
// today, so this passes immediately rather than failing first
// (tasks.md 7b.4's own framing: "disclosed as the actual regression this
// test guards, not a hypothetical" — the m2a C9 precedent this PR chain
// already established for a stated Red that is a proof, not a discovered
// break). It stays in this suite as the regression guard PR 8-11's real
// per-phase wiring must keep green: a phase fed nothing must still write
// nothing.
func TestConsolidate_NoEffects(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 8, 10, 3, 0, 0, 0, time.UTC)
	log := memrepo.NewDecisionLog()
	svc := NewConsolidateService(fixedClock{now}, memrepo.NewConfig(), memrepo.NewUnits(), memrepo.NewRelations(), &fakeIDs{}, log, testRecall(), noJudge(t), memrepo.NewSelfModel(), memrepo.NewState())

	report, err := svc.Consolidate(ctx, ConsolidateRequest{})
	if err != nil {
		t.Fatalf("Consolidate: %v", err)
	}
	if len(report.phasesRun) != len(consolidation.Order()) {
		t.Fatalf("PhasesRun = %v, want every phase reached even with nothing to do", report.phasesRun)
	}

	rows, err := log.Since(ctx, now.Add(-time.Hour), 100)
	if err != nil {
		t.Fatalf("Since: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("decision_log rows = %d, want 0 — a phase fed nothing must write nothing (spec R4.2)", len(rows))
	}
}

// TestConsolidateReport_CorruptedNeverLogged is spec R4.2's own MUST NOT,
// decided uniformly across every corrupted-capable phase (design §3.3(e)):
// a corrupted entry from any of archive, strengthen, reweight or derive is
// surfaced in ConsolidateReport and never in decision_log, because a
// refusal had no vault effect. Exercised against two of the four real core
// producers that already share the (effects, corrupted []string) shape —
// consolidation.Archive and consolidation.Strengthen — feeding a fixture
// engineered to refuse one entry through each straight into
// report.reportCorrupted, the one place this PR routes a corrupted id.
// reweight (PR 11) and derive (PR 10b) wire their own real corrupted-
// producing calls later, through this exact same mechanism: it takes only
// ids, so it structurally cannot itself become a call site into record —
// disclosed here rather than claimed as coverage this PR does not have.
func TestConsolidateReport_CorruptedNeverLogged(t *testing.T) {
	now := time.Date(2026, 8, 10, 3, 0, 0, 0, time.UTC)

	t.Run("archive", func(t *testing.T) {
		cs := []consolidation.Cold{
			{UnitID: "u-good", Weight: 0.1, DecayRate: 0.01, LastTouchedAt: now.Add(-24 * time.Hour)},
			{UnitID: "u-nan", Weight: math.NaN(), DecayRate: 0.01, LastTouchedAt: now},
		}
		// Both units start pool; the fixture needs Status set for Archive
		// to consider them at all.
		for i := range cs {
			cs[i].Status = unit.StatusPool
		}
		_, corrupted := consolidation.Archive(cs, consolidation.DefaultWeightThreshold, now)
		if len(corrupted) != 1 || corrupted[0] != "u-nan" {
			t.Fatalf("fixture corrupted = %v, want exactly [u-nan]", corrupted)
		}

		log := memrepo.NewDecisionLog()
		var report ConsolidateReport
		report.reportCorrupted(corrupted)

		if len(report.corrupted) != 1 || report.corrupted[0] != "u-nan" {
			t.Errorf("report.corrupted = %v, want [u-nan]", report.corrupted)
		}
		rows, err := log.Since(context.Background(), now.Add(-time.Hour), 10)
		if err != nil {
			t.Fatalf("Since: %v", err)
		}
		if len(rows) != 0 {
			t.Errorf("decision_log rows = %d, want 0 for a refused archive entry", len(rows))
		}
	})

	t.Run("strengthen", func(t *testing.T) {
		since := now.Add(-48 * time.Hour)
		es := []consolidation.RelationEvidence{
			{RelationID: "r-good", Strength: 0.2, FromLastTouchedAt: now, ToLastTouchedAt: now},
			{RelationID: "r-bad", Strength: 7, FromLastTouchedAt: now, ToLastTouchedAt: now},
		}
		_, corrupted := consolidation.Strengthen(es, &since)
		if len(corrupted) != 1 || corrupted[0] != "r-bad" {
			t.Fatalf("fixture corrupted = %v, want exactly [r-bad]", corrupted)
		}

		log := memrepo.NewDecisionLog()
		var report ConsolidateReport
		report.reportCorrupted(corrupted)

		if len(report.corrupted) != 1 || report.corrupted[0] != "r-bad" {
			t.Errorf("report.corrupted = %v, want [r-bad]", report.corrupted)
		}
		rows, err := log.Since(context.Background(), now.Add(-time.Hour), 10)
		if err != nil {
			t.Fatalf("Since: %v", err)
		}
		if len(rows) != 0 {
			t.Errorf("decision_log rows = %d, want 0 for a refused strengthen entry", len(rows))
		}
	})
}

// TestConsolidateRunner_Record_EncodesDetailIntoContext proves record's own
// marshalling contract (design §3.3(e)): detail lands in Decision.Context
// verbatim, decodable by the caller that wrote it.
func TestConsolidateRunner_Record_EncodesDetailIntoContext(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 8, 10, 3, 0, 0, 0, time.UTC)
	log := memrepo.NewDecisionLog()
	r := consolidateRunner{ids: &fakeIDs{}, log: log}

	type detail struct {
		UnitID string `json:"unit_id"`
	}
	if err := r.record(ctx, now, ports.ActionArchiveArchived, `archived unit "u-1"`, detail{UnitID: "u-1"}); err != nil {
		t.Fatalf("record: %v", err)
	}

	rows, err := log.Since(ctx, now.Add(-time.Second), 1)
	if err != nil {
		t.Fatalf("Since: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("decision_log rows = %d, want 1", len(rows))
	}

	var got detail
	if err := json.Unmarshal(rows[0].Context, &got); err != nil {
		t.Fatalf("decode Context: %v", err)
	}
	if got.UnitID != "u-1" {
		t.Errorf("Context.unit_id = %q, want %q", got.UnitID, "u-1")
	}
}

// spyUnits wraps memrepo.Units to record every IncompleteOlderThan cutoff
// argument it receives — spec R5.1, design §4.1's own duplicated-predicate
// risk note: expire_incomplete's cutoff must be derived from
// consolidation.IncompleteExpiryHours, never a literal, and a spy on the
// argument is the only way to prove which one produced it (the return
// value alone cannot tell a correct cutoff from a coincidentally-matching
// one on an empty fixture).
type spyUnits struct {
	*memrepo.Units
	incompleteOlderThanCalls []time.Time
}

func (u *spyUnits) IncompleteOlderThan(ctx context.Context, cutoff time.Time) ([]consolidation.Incomplete, error) {
	u.incompleteOlderThanCalls = append(u.incompleteOlderThanCalls, cutoff)
	return u.Units.IncompleteOlderThan(ctx, cutoff)
}

// TestConsolidateRunner_ExpireIncomplete_DerivesCutoffFromConstant is spec
// R5.1 and design §4.1: the cutoff expire_incomplete reads by is
// now.Add(-consolidation.IncompleteExpiryHours * time.Hour), computed in
// brain, never a literal duplicating that same 24h window.
func TestConsolidateRunner_ExpireIncomplete_DerivesCutoffFromConstant(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 8, 10, 3, 0, 0, 0, time.UTC)
	units := &spyUnits{Units: memrepo.NewUnits()}
	phase := consolidation.PhaseExpireIncomplete

	svc := NewConsolidateService(fixedClock{now}, memrepo.NewConfig(), units, memrepo.NewRelations(), &fakeIDs{}, memrepo.NewDecisionLog(), testRecall(), noJudge(t), memrepo.NewSelfModel(), memrepo.NewState())
	if _, err := svc.Consolidate(ctx, ConsolidateRequest{Phase: &phase}); err != nil {
		t.Fatalf("Consolidate: %v", err)
	}

	if len(units.incompleteOlderThanCalls) != 1 {
		t.Fatalf("IncompleteOlderThan calls = %d, want exactly 1", len(units.incompleteOlderThanCalls))
	}
	want := now.Add(-consolidation.IncompleteExpiryHours * time.Hour)
	if got := units.incompleteOlderThanCalls[0]; !got.Equal(want) {
		t.Errorf("IncompleteOlderThan cutoff = %v, want %v (now - consolidation.IncompleteExpiryHours)", got, want)
	}
}

// TestConsolidateRunner_ExpireIncomplete_TransitionsAndRecords proves the
// GREEN half of the wiring: a promoted and an archived Incomplete each
// persist through SetStatus and record exactly one decision_log row
// (ActionExpireIncompleteTransitioned), spec R4.2/R5.1.
func TestConsolidateRunner_ExpireIncomplete_TransitionsAndRecords(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 8, 10, 3, 0, 0, 0, time.UTC)
	units := memrepo.NewUnits()
	old := now.Add(-25 * time.Hour)
	for _, seed := range []struct {
		id         string
		unresolved bool
	}{
		{"u-promote", false},
		{"u-archive", true},
	} {
		if err := units.Create(ctx, unit.Unit{
			ID: seed.id, Type: unit.TypeKnowledge, Status: unit.StatusIncomplete,
			Content: "seed", Source: "chat", CreatedAt: old, UpdatedAt: old,
			StructuredData: json.RawMessage(`{}`),
		}); err != nil {
			t.Fatalf("seed %s: %v", seed.id, err)
		}
	}
	// unresolved has no column yet (design §4.2, spec R2.1's own stated
	// gap) — this fixture proves the wiring over ExpireIncomplete's pure
	// output, not a producer of Unresolved: true, so both seeded units
	// promote under this PR's real IncompleteOlderThan read.

	log := memrepo.NewDecisionLog()
	phase := consolidation.PhaseExpireIncomplete
	svc := NewConsolidateService(fixedClock{now}, memrepo.NewConfig(), units, memrepo.NewRelations(), &fakeIDs{}, log, testRecall(), noJudge(t), memrepo.NewSelfModel(), memrepo.NewState())

	if _, err := svc.Consolidate(ctx, ConsolidateRequest{Phase: &phase}); err != nil {
		t.Fatalf("Consolidate: %v", err)
	}

	for _, id := range []string{"u-promote", "u-archive"} {
		got, err := units.ByID(ctx, id)
		if err != nil {
			t.Fatalf("ByID(%s): %v", id, err)
		}
		if got.Status != unit.StatusPool {
			t.Errorf("%s status = %s, want %s (no Unresolved producer exists yet)", id, got.Status, unit.StatusPool)
		}
	}

	rows, err := log.Since(ctx, now.Add(-time.Hour), 10)
	if err != nil {
		t.Fatalf("Since: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("decision_log rows = %d, want 2 — one per transitioned unit (spec R4.2)", len(rows))
	}
	for _, row := range rows {
		if row.Action != ports.ActionExpireIncompleteTransitioned {
			t.Errorf("row Action = %s, want %s", row.Action, ports.ActionExpireIncompleteTransitioned)
		}
	}
}

// conflictingUnits wraps memrepo.Units, forcing exactly the conflictOn'th
// SetStatus call to fail with ports.ErrStatusConflict regardless of the
// unit's own stored status — spec R4.3's fixture needs one deterministic
// conflict, not one incidentally derived from seeded state.
type conflictingUnits struct {
	*memrepo.Units
	conflictOn int // 1-based call index that fails
	calls      int
}

func (u *conflictingUnits) SetStatus(ctx context.Context, id string, from, to unit.Status, at time.Time) error {
	u.calls++
	if u.calls == u.conflictOn {
		return ports.ErrStatusConflict
	}
	return u.Units.SetStatus(ctx, id, from, to, at)
}

// TestConsolidateRunner_PersistArchiveTransitions_SkipsAndLogsConflict is
// spec R4.3's own fixture (tasks.md 7b.8's shape): three units planned for
// archival, the second's SetStatus call returns ports.ErrStatusConflict — a
// concurrent capture or correction revived it between archive's read and
// this write. The pass does not fail: the first and third are archived,
// the second is skipped and the skip itself is logged, and no error
// propagates.
//
// Forward-looking scaffold (design §3.3(e), tasks.md 7b.8): exercised
// directly against persistArchiveTransitions with a fake phase's own
// planned transitions, not through runPhase's still-placeholder archive
// arm — PR 8's own task 8.9 re-runs this identical shape against the real
// archive wiring, proving the real phase uses the exact mechanism proved
// here in isolation.
func TestConsolidateRunner_PersistArchiveTransitions_SkipsAndLogsConflict(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 8, 10, 3, 0, 0, 0, time.UTC)

	units := &conflictingUnits{Units: memrepo.NewUnits(), conflictOn: 2}
	for _, id := range []string{"u-1", "u-2", "u-3"} {
		if err := units.Create(ctx, unit.Unit{
			ID: id, Type: unit.TypeKnowledge, Status: unit.StatusPool,
			Content: "seed", Source: "chat", CreatedAt: now, UpdatedAt: now,
		}); err != nil {
			t.Fatalf("seed %s: %v", id, err)
		}
	}

	ts := []consolidation.Transition{
		{UnitID: "u-1", From: unit.StatusPool, To: unit.StatusArchived, Reason: consolidation.ReasonBelowWeightThreshold},
		{UnitID: "u-2", From: unit.StatusPool, To: unit.StatusArchived, Reason: consolidation.ReasonBelowWeightThreshold},
		{UnitID: "u-3", From: unit.StatusPool, To: unit.StatusArchived, Reason: consolidation.ReasonBelowWeightThreshold},
	}

	log := memrepo.NewDecisionLog()
	r := consolidateRunner{ids: &fakeIDs{}, log: log}

	if err := r.persistArchiveTransitions(ctx, ts, units, now); err != nil {
		t.Fatalf("persistArchiveTransitions: %v", err)
	}

	for id, want := range map[string]unit.Status{
		"u-1": unit.StatusArchived,
		"u-2": unit.StatusPool,
		"u-3": unit.StatusArchived,
	} {
		got, err := units.ByID(ctx, id)
		if err != nil {
			t.Fatalf("ByID(%s): %v", id, err)
		}
		if got.Status != want {
			t.Errorf("%s status = %s, want %s", id, got.Status, want)
		}
	}

	rows, err := log.Since(ctx, now.Add(-time.Hour), 10)
	if err != nil {
		t.Fatalf("Since: %v", err)
	}
	var archived, skipped int
	for _, row := range rows {
		switch row.Action {
		case ports.ActionArchiveArchived:
			archived++
		case ports.ActionArchiveConflictSkipped:
			skipped++
		default:
			t.Errorf("unexpected Action %s", row.Action)
		}
	}
	if archived != 2 {
		t.Errorf("ActionArchiveArchived rows = %d, want 2", archived)
	}
	if skipped != 1 {
		t.Errorf("ActionArchiveConflictSkipped rows = %d, want 1", skipped)
	}
	if len(rows) != 3 {
		t.Errorf("decision_log rows = %d, want 3 — one per unit, including the skip (spec R4.3)", len(rows))
	}
}

// TestConsolidateRunner_Archive_ResolvesConfiguredThreshold is spec R5.2
// and design §3.3(c): archive's own weight.Effective comparison must run
// against ConfigRepo's configured WeightThreshold, resolved through
// consolidation.ResolveWeightThreshold — never DefaultWeightThreshold read
// directly, which would silently ignore a user's own configuration. A unit
// at effective weight 0.6 sits ABOVE the 0.5 default (would stay pool under
// a default-only read) but BELOW a configured 0.8 threshold (must archive)
// — the two readings disagree, so the outcome proves which one the runner
// actually used.
func TestConsolidateRunner_Archive_ResolvesConfiguredThreshold(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 8, 10, 3, 0, 0, 0, time.UTC)

	units := memrepo.NewUnits()
	if err := units.Create(ctx, unit.Unit{
		ID: "u-mid", Type: unit.TypeKnowledge, Status: unit.StatusPool,
		Content: "seed", Source: "chat", Weight: 0.6, WeightDecayRate: 0,
		LastTouchedAt: now, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("seed u-mid: %v", err)
	}

	cfg := memrepo.NewConfig()
	configured := 0.8
	cfg.SeedConfig(t, ports.VaultConfig{WeightThreshold: &configured})

	log := memrepo.NewDecisionLog()
	phase := consolidation.PhaseArchive
	svc := NewConsolidateService(fixedClock{now}, cfg, units, memrepo.NewRelations(), &fakeIDs{}, log, testRecall(), noJudge(t), memrepo.NewSelfModel(), memrepo.NewState())

	if _, err := svc.Consolidate(ctx, ConsolidateRequest{Phase: &phase}); err != nil {
		t.Fatalf("Consolidate: %v", err)
	}

	got, err := units.ByID(ctx, "u-mid")
	if err != nil {
		t.Fatalf("ByID(u-mid): %v", err)
	}
	if got.Status != unit.StatusArchived {
		t.Errorf("u-mid status = %s, want %s — 0.6 < configured threshold 0.8; a default-only read (0.5) would have left this unit pool, so this outcome proves the configured value was used", got.Status, unit.StatusArchived)
	}
}

// TestConsolidateRunner_Archive_RefusesNonFiniteBeforeArchiveSees is design
// §8.1: consolidateRunner partitions the LiveDecayStates read into usable
// and refused, using Archive's own non-finite predicate, BEFORE any of it
// reaches consolidation.Archive — three units so removing the guard would
// change the fixture's outcome (two would-be archivals plus one refusal,
// not indistinguishable from Archive's own internal check alone).
func TestConsolidateRunner_Archive_RefusesNonFiniteBeforeArchiveSees(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 8, 10, 3, 0, 0, 0, time.UTC)

	units := memrepo.NewUnits()
	seeds := []struct {
		id     string
		weight float64
	}{
		{"u-cold-1", 0.1},
		{"u-nan", math.NaN()},
		{"u-cold-2", 0.05},
	}
	for _, s := range seeds {
		if err := units.Create(ctx, unit.Unit{
			ID: s.id, Type: unit.TypeKnowledge, Status: unit.StatusPool,
			Content: "seed", Source: "chat", Weight: s.weight, WeightDecayRate: 0.01,
			LastTouchedAt: now.Add(-48 * time.Hour), CreatedAt: now, UpdatedAt: now,
		}); err != nil {
			t.Fatalf("seed %s: %v", s.id, err)
		}
	}

	log := memrepo.NewDecisionLog()
	phase := consolidation.PhaseArchive
	svc := NewConsolidateService(fixedClock{now}, memrepo.NewConfig(), units, memrepo.NewRelations(), &fakeIDs{}, log, testRecall(), noJudge(t), memrepo.NewSelfModel(), memrepo.NewState())

	report, err := svc.Consolidate(ctx, ConsolidateRequest{Phase: &phase})
	if err != nil {
		t.Fatalf("Consolidate: %v", err)
	}

	if len(report.corrupted) != 1 || report.corrupted[0] != "u-nan" {
		t.Fatalf("report.corrupted = %v, want exactly [u-nan]", report.corrupted)
	}

	for _, id := range []string{"u-cold-1", "u-cold-2"} {
		got, err := units.ByID(ctx, id)
		if err != nil {
			t.Fatalf("ByID(%s): %v", id, err)
		}
		if got.Status != unit.StatusArchived {
			t.Errorf("%s status = %s, want %s", id, got.Status, unit.StatusArchived)
		}
	}

	nanUnit, err := units.ByID(ctx, "u-nan")
	if err != nil {
		t.Fatalf("ByID(u-nan): %v", err)
	}
	if nanUnit.Status != unit.StatusPool {
		t.Errorf("u-nan status = %s, want unchanged %s — a refused entry must never reach Archive/SetStatus", nanUnit.Status, unit.StatusPool)
	}

	rows, err := log.Since(ctx, now.Add(-time.Hour), 10)
	if err != nil {
		t.Fatalf("Since: %v", err)
	}
	for _, row := range rows {
		var detail struct {
			UnitID string `json:"UnitID"`
		}
		_ = json.Unmarshal(row.Context, &detail)
		if detail.UnitID == "u-nan" {
			t.Errorf("decision_log row %+v names the refused unit u-nan — a refusal must never be logged (spec R4.2's MUST NOT)", row)
		}
	}
}

// TestConsolidateRunner_Archive_RealWiringSkipsAndLogsConflict re-runs spec
// R4.3's exact fixture (PR 7b task 7b.8's shape, proven in isolation there
// against persistArchiveTransitions directly) through this PR's real
// archive wiring: three units planned for archival via the real
// LiveDecayStates -> Archive path, the second's SetStatus call conflicts.
// The pass completes, the first and third archive, the second is skipped
// and logged, and no error propagates — proving the real phase uses the
// exact mechanism PR 7b already built, not a second one.
func TestConsolidateRunner_Archive_RealWiringSkipsAndLogsConflict(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 8, 10, 3, 0, 0, 0, time.UTC)

	base := memrepo.NewUnits()
	for _, id := range []string{"u-1", "u-2", "u-3"} {
		if err := base.Create(ctx, unit.Unit{
			ID: id, Type: unit.TypeKnowledge, Status: unit.StatusPool,
			Content: "seed", Source: "chat", Weight: 0.1, WeightDecayRate: 0.01,
			LastTouchedAt: now.Add(-48 * time.Hour), CreatedAt: now, UpdatedAt: now,
		}); err != nil {
			t.Fatalf("seed %s: %v", id, err)
		}
	}
	units := &conflictingUnits{Units: base, conflictOn: 2}

	log := memrepo.NewDecisionLog()
	phase := consolidation.PhaseArchive
	svc := NewConsolidateService(fixedClock{now}, memrepo.NewConfig(), units, memrepo.NewRelations(), &fakeIDs{}, log, testRecall(), noJudge(t), memrepo.NewSelfModel(), memrepo.NewState())

	if _, err := svc.Consolidate(ctx, ConsolidateRequest{Phase: &phase}); err != nil {
		t.Fatalf("Consolidate: %v", err)
	}

	for id, want := range map[string]unit.Status{
		"u-1": unit.StatusArchived,
		"u-2": unit.StatusPool,
		"u-3": unit.StatusArchived,
	} {
		got, err := units.ByID(ctx, id)
		if err != nil {
			t.Fatalf("ByID(%s): %v", id, err)
		}
		if got.Status != want {
			t.Errorf("%s status = %s, want %s", id, got.Status, want)
		}
	}

	rows, err := log.Since(ctx, now.Add(-time.Hour), 10)
	if err != nil {
		t.Fatalf("Since: %v", err)
	}
	var archived, skipped int
	for _, row := range rows {
		switch row.Action {
		case ports.ActionArchiveArchived:
			archived++
		case ports.ActionArchiveConflictSkipped:
			skipped++
		default:
			t.Errorf("unexpected Action %s", row.Action)
		}
	}
	if archived != 2 {
		t.Errorf("ActionArchiveArchived rows = %d, want 2", archived)
	}
	if skipped != 1 {
		t.Errorf("ActionArchiveConflictSkipped rows = %d, want 1", skipped)
	}
}

// TestConsolidateRunner_Strengthen_SincePropagatesAndPersists is spec R5.3
// and R4.2/R5.3's persist half combined: strengthen's own Evidence() read
// feeds Strengthen(es, pass.since) with pass.since unmodified — proven
// behaviourally (two relations, one qualifying at since, one excluded
// before it, since Strengthen is a pure function with no seam to spy on
// directly) — and each resulting StrengthChange persists through
// RelationRepo.Upsert with the ORIGINAL relation's FromUnitID/ToUnitID/Type
// (Upsert's own conflict target, design §4.2) and Confidence preserved,
// never zeroed.
func TestConsolidateRunner_Strengthen_SincePropagatesAndPersists(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 8, 10, 3, 0, 0, 0, time.UTC)
	since := now.Add(-48 * time.Hour)

	cfg := memrepo.NewConfig()
	if err := cfg.RecordConsolidationRun(ctx, since); err != nil {
		t.Fatalf("seed RecordConsolidationRun: %v", err)
	}

	rels := memrepo.NewRelations()
	for _, id := range []string{"u-a", "u-b", "u-c", "u-d"} {
		rels.EnsureUnit(t, id)
	}
	rels.SetLastTouchedAt(t, "u-a", now)
	rels.SetLastTouchedAt(t, "u-b", now)
	rels.SetLastTouchedAt(t, "u-c", since.Add(-time.Hour))
	rels.SetLastTouchedAt(t, "u-d", now)

	qualifying := ports.Relation{
		ID: "r-qualify", FromUnitID: "u-a", ToUnitID: "u-b", Type: "reference",
		Strength: 0.2, Confidence: 0.9, CreatedBy: "system", CreatedAt: now,
	}
	excluded := ports.Relation{
		ID: "r-exclude", FromUnitID: "u-c", ToUnitID: "u-d", Type: "reference",
		Strength: 0.2, Confidence: 0.9, CreatedBy: "system", CreatedAt: now,
	}
	if err := rels.Upsert(ctx, qualifying); err != nil {
		t.Fatalf("seed qualifying: %v", err)
	}
	if err := rels.Upsert(ctx, excluded); err != nil {
		t.Fatalf("seed excluded: %v", err)
	}

	log := memrepo.NewDecisionLog()
	phase := consolidation.PhaseStrengthen
	svc := NewConsolidateService(fixedClock{now}, cfg, memrepo.NewUnits(), rels, &fakeIDs{}, log, testRecall(), noJudge(t), memrepo.NewSelfModel(), memrepo.NewState())

	if _, err := svc.Consolidate(ctx, ConsolidateRequest{Phase: &phase}); err != nil {
		t.Fatalf("Consolidate: %v", err)
	}

	byRelationID := func(unitID string) map[string]ports.Relation {
		out, err := rels.ByUnit(ctx, unitID)
		if err != nil {
			t.Fatalf("ByUnit(%s): %v", unitID, err)
		}
		byID := make(map[string]ports.Relation, len(out))
		for _, r := range out {
			byID[r.ID] = r
		}
		return byID
	}

	afterQualify, ok := byRelationID("u-a")["r-qualify"]
	if !ok {
		t.Fatal("r-qualify not found after Consolidate — the runner must never drop a persisted relation")
	}
	wantStrength := 0.2 + consolidation.StrengthenGain*(1-0.2)
	if afterQualify.Strength != wantStrength {
		t.Errorf("r-qualify Strength = %v, want %v (StrengthenGain's own formula)", afterQualify.Strength, wantStrength)
	}
	if afterQualify.Confidence != 0.9 {
		t.Errorf("r-qualify Confidence = %v, want 0.9 unchanged — Upsert must not corrupt it to the zero value", afterQualify.Confidence)
	}
	if afterQualify.FromUnitID != "u-a" || afterQualify.ToUnitID != "u-b" || afterQualify.Type != "reference" {
		t.Errorf("r-qualify identity = (%s, %s, %s), want (u-a, u-b, reference) unchanged", afterQualify.FromUnitID, afterQualify.ToUnitID, afterQualify.Type)
	}

	afterExclude, ok := byRelationID("u-c")["r-exclude"]
	if !ok {
		t.Fatal("r-exclude not found after Consolidate")
	}
	if afterExclude.Strength != 0.2 {
		t.Errorf("r-exclude Strength = %v, want unchanged 0.2 — its endpoint u-c was touched before since, so it must not qualify (spec R5.3)", afterExclude.Strength)
	}

	rows, err := log.Since(ctx, now.Add(-time.Hour), 10)
	if err != nil {
		t.Fatalf("Since: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("decision_log rows = %d, want exactly 1 — only the qualifying relation changed (spec R4.2)", len(rows))
	}
	if rows[0].Action != ports.ActionStrengthenApplied {
		t.Errorf("row Action = %s, want %s", rows[0].Action, ports.ActionStrengthenApplied)
	}
}

// TestConsolidateRunner_Connect_CallsRecallServiceScoredFor is spec R5.5's
// own MUST (design §7.1): the candidate search behind connect calls
// brain.RecallService's existing fused ranking — it does not implement a
// second fusion. RecallService has no interface seam today (a concrete
// struct, not a port), so this is proven the way tasks.md 9.1 names as the
// fallback: a spy on the embedding fake ScoredFor's own first step always
// calls (recall.go's ScoredFor calls s.embed.Embed before anything else) —
// counting embed calls is counting ScoredFor calls, without RecallService
// needing a seam it does not have.
//
// Red, before consolidate.go's PhaseConnect arm does anything but read and
// select sources: the placeholder arm calls nothing recall-related, so
// EmbedCalls() stays 0 and this fails first, for the right reason.
func TestConsolidateRunner_Connect_CallsRecallServiceScoredFor(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 8, 10, 3, 0, 0, 0, time.UTC)

	units := memrepo.NewUnits()
	if err := units.Create(ctx, unit.Unit{
		ID: "u-source", Type: unit.TypeKnowledge, Status: unit.StatusPool,
		Content: "seed content for connect's own recall step", Source: "chat",
		Weight: 1.0, WeightDecayRate: 0, LastTouchedAt: now, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("seed u-source: %v", err)
	}

	embed := fakeprovider.NewEmbeddingFake("test-model")
	rec := NewRecallService(NewIndex(recall.VectorIndex{Model: "test-model"}), memrepo.NewLexical(), units, embed)

	log := memrepo.NewDecisionLog()
	phase := consolidation.PhaseConnect
	svc := NewConsolidateService(fixedClock{now}, memrepo.NewConfig(), units, memrepo.NewRelations(), &fakeIDs{}, log, rec, noJudge(t), memrepo.NewSelfModel(), memrepo.NewState())

	if _, err := svc.Consolidate(ctx, ConsolidateRequest{Phase: &phase}); err != nil {
		t.Fatalf("Consolidate: %v", err)
	}

	if got := embed.EmbedCalls(); got != 1 {
		t.Fatalf("EmbedCalls() = %d, want exactly 1 — connect's own candidate search must call RecallService.ScoredFor (spec R5.5), never a second fusion implementation", got)
	}
}

// TestConsolidateRunner_Connect_ExcludesExistingPairsAndCandidateSelf is
// design §7.1/§4.2's own triangulation over the same wiring: two live
// sources exist, u-source has an existing relation to u-existing (excluded
// by ExistingPairs' canonical lookup) and finds u-new (kept). u-source must
// never appear as its own candidate either (ConnectPairs' own MUST,
// unreachable here since the fixture excludes self from the fake lexical
// index, but pinned anyway as a second, independently-seeded case so this
// test is not a trivial single-assertion rerun of the one above).
func TestConsolidateRunner_Connect_ExcludesExistingPairsAndCandidateSelf(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 8, 10, 3, 0, 0, 0, time.UTC)

	units := memrepo.NewUnits()
	for _, seed := range []struct {
		id, content string
	}{
		{"u-source", "plan the quarterly offsite in Lisbon"},
		{"u-existing", "quarterly offsite venue shortlist"},
		{"u-new", "quarterly offsite travel budget"},
	} {
		if err := units.Create(ctx, unit.Unit{
			ID: seed.id, Type: unit.TypeKnowledge, Status: unit.StatusPool,
			Content: seed.content, Source: "chat",
			Weight: 1.0, WeightDecayRate: 0, LastTouchedAt: now, CreatedAt: now, UpdatedAt: now,
		}); err != nil {
			t.Fatalf("seed %s: %v", seed.id, err)
		}
	}

	lex := memrepo.NewLexical()
	lex.SeedLexical(t, "u-source", "quarterly offsite Lisbon")
	lex.SeedLexical(t, "u-existing", "quarterly offsite venue")
	lex.SeedLexical(t, "u-new", "quarterly offsite budget")

	rels := memrepo.NewRelations()
	for _, id := range []string{"u-source", "u-existing", "u-new"} {
		rels.EnsureUnit(t, id)
	}
	if err := rels.Upsert(ctx, ports.Relation{
		ID: "r-existing", FromUnitID: "u-source", ToUnitID: "u-existing", Type: "same_topic",
		Strength: 0.4, Confidence: 0.7, CreatedBy: "system", CreatedAt: now,
	}); err != nil {
		t.Fatalf("seed existing relation: %v", err)
	}

	rec := NewRecallService(NewIndex(recall.VectorIndex{Model: "test-model"}), lex, units, fakeprovider.NewEmbeddingFake("test-model"))

	r := consolidateRunner{units: units, rels: rels, recall: rec}
	pairs, err := r.connectSources(ctx, []string{"u-source"})
	if err != nil {
		t.Fatalf("connectSources: %v", err)
	}

	if len(pairs) != 1 || pairs[0] != (consolidation.Pair{From: "u-source", To: "u-new"}) {
		t.Fatalf("connectSources() = %v, want exactly [{u-source u-new}] — u-existing is already related (excluded by ExistingPairs), and u-source is never its own candidate", pairs)
	}
}

// TestConnect_RefusesNonFiniteSources re-runs PR 8's own task 8.7 fixture
// shape (TestConsolidateRunner_Archive_RefusesNonFiniteBeforeArchiveSees,
// above) against connect's own LiveDecayStates consumption, from the
// identical seeded state — design §8.1's own claim: "archive at slot 2 and
// connect/derive at slots 4/5 therefore refuse the identical set of rows"
// is a claim about ONE guard function reused across phases, not two
// independently-correct implementations that happen to agree.
//
// Every finite weight here sits well above the default weight threshold,
// so archive's own run leaves both finite units live — the second
// (connect) phase run below reads the same seeded state the first phase
// did, not state the first phase's own archival already changed.
func TestConnect_RefusesNonFiniteSources(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 8, 10, 3, 0, 0, 0, time.UTC)

	units := memrepo.NewUnits()
	seeds := []struct {
		id     string
		weight float64
	}{
		{"u-cold-1", 0.9},
		{"u-nan", math.NaN()},
		{"u-cold-2", 0.8},
	}
	for _, s := range seeds {
		if err := units.Create(ctx, unit.Unit{
			ID: s.id, Type: unit.TypeKnowledge, Status: unit.StatusPool,
			Content: "seed content for " + s.id, Source: "chat",
			Weight: s.weight, WeightDecayRate: 0,
			LastTouchedAt: now, CreatedAt: now, UpdatedAt: now,
		}); err != nil {
			t.Fatalf("seed %s: %v", s.id, err)
		}
	}

	archivePhase := consolidation.PhaseArchive
	archiveSvc := NewConsolidateService(fixedClock{now}, memrepo.NewConfig(), units, memrepo.NewRelations(), &fakeIDs{}, memrepo.NewDecisionLog(), testRecallOver(units), noJudge(t), memrepo.NewSelfModel(), memrepo.NewState())
	archiveReport, err := archiveSvc.Consolidate(ctx, ConsolidateRequest{Phase: &archivePhase})
	if err != nil {
		t.Fatalf("Consolidate(PhaseArchive): %v", err)
	}
	if len(archiveReport.corrupted) != 1 || archiveReport.corrupted[0] != "u-nan" {
		t.Fatalf("archive report.corrupted = %v, want exactly [u-nan]", archiveReport.corrupted)
	}
	for _, id := range []string{"u-cold-1", "u-cold-2"} {
		got, err := units.ByID(ctx, id)
		if err != nil {
			t.Fatalf("ByID(%s): %v", id, err)
		}
		if got.Status != unit.StatusPool {
			t.Fatalf("%s status = %s after archive, want unchanged %s — this fixture's weights must stay above the default threshold so connect reads the same seeded state", id, got.Status, unit.StatusPool)
		}
	}

	connectPhase := consolidation.PhaseConnect
	connectSvc := NewConsolidateService(fixedClock{now}, memrepo.NewConfig(), units, memrepo.NewRelations(), &fakeIDs{}, memrepo.NewDecisionLog(), testRecallOver(units), noJudge(t), memrepo.NewSelfModel(), memrepo.NewState())
	connectReport, err := connectSvc.Consolidate(ctx, ConsolidateRequest{Phase: &connectPhase})
	if err != nil {
		t.Fatalf("Consolidate(PhaseConnect): %v", err)
	}
	if len(connectReport.corrupted) != 1 || connectReport.corrupted[0] != "u-nan" {
		t.Errorf("connect report.corrupted = %v, want exactly [u-nan] — the identical guard, reused (design §8.1)", connectReport.corrupted)
	}
}

// TestConsolidate_WholePassReportsEachCorruptedIDOnce is the whole-pass
// companion TestConnect_RefusesNonFiniteSources (above) deliberately does
// not provide: that fixture drives archive and connect as two SEPARATE
// per-phase calls, so each gets its own fresh ConsolidateReport and neither
// can observe what happens when both phases fold into the SAME report.
//
// They do both fold into one report on a whole pass, and both read
// LiveDecayStates independently: archive refuses a non-finite row at slot
// 2, and connect reads the same row again at slot 4 and refuses it again,
// because archive's refusal is not a vault write — nothing removes the row
// between the two reads. Without dedup the same id lands in corrupted
// twice, which PR 12's report rendering would then print twice, reading as
// two distinct corrupt units rather than one seen by two phases.
//
// Found by Judgment Day on PR 9a (Judge A, SUGGESTION); fixed here rather
// than carried to 9b because 9b extends this exact call path.
func TestConsolidate_WholePassReportsEachCorruptedIDOnce(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 8, 10, 3, 0, 0, 0, time.UTC)

	units := memrepo.NewUnits()
	seeds := []struct {
		id     string
		weight float64
	}{
		{"u-cold-1", 0.9},
		{"u-nan", math.NaN()},
		{"u-inf", math.Inf(1)},
	}
	for _, s := range seeds {
		if err := units.Create(ctx, unit.Unit{
			ID: s.id, Type: unit.TypeKnowledge, Status: unit.StatusPool,
			Content: "seed content for " + s.id, Source: "chat",
			Weight: s.weight, WeightDecayRate: 0,
			LastTouchedAt: now, CreatedAt: now, UpdatedAt: now,
		}); err != nil {
			t.Fatalf("seed %s: %v", s.id, err)
		}
	}

	// u-cold-1 is a live, finite source — derive's own call is never
	// skipped for it (unlike connect's own zero candidates here, since
	// testRecallOver's index/lexical stores are empty): one scripted case
	// covers it, connect's own zero calls need none.
	dir := t.TempDir()
	writeDeriveCase(t, dir, "derive-corrupted-fixture", `{"beliefs":[]}`)
	judge := fakeprovider.New(t, dir, "derive-corrupted-fixture")

	svc := NewConsolidateService(fixedClock{now}, memrepo.NewConfig(), units, memrepo.NewRelations(), &fakeIDs{}, memrepo.NewDecisionLog(), testRecallOver(units), judge, memrepo.NewSelfModel(), memrepo.NewState())
	report, err := svc.Consolidate(ctx, ConsolidateRequest{})
	if err != nil {
		t.Fatalf("Consolidate(whole pass): %v", err)
	}

	seen := make(map[string]int, len(report.corrupted))
	for _, id := range report.corrupted {
		seen[id]++
	}
	for id, n := range seen {
		if n != 1 {
			t.Errorf("corrupted contains %q %d times, want exactly 1 — archive (slot 2) and connect (slot 4) both refuse it from the same unchanged rows, but one corrupt unit is one entry", id, n)
		}
	}
	if len(seen) != 2 || seen["u-nan"] != 1 || seen["u-inf"] != 1 {
		t.Errorf("corrupted = %v, want exactly [u-inf u-nan] in some order", report.corrupted)
	}
}

// TestConsolidateRunner_Connect_PersistsAcceptedJudgmentThroughRealDispatch
// is spec R5.5's own persist scenario, driven through the real PhaseConnect
// arm (svc.Consolidate) rather than connectSources or connectPairsForSource
// directly — PR 9a's own Judgment Day carried this forward (Judge A,
// WARNING): the exclusion property (design §4.2) had previously been proven
// only against the unexported connectSources helper, never through the arm
// that actually judges and persists. u-existing already has a relation to
// u-source, so ExistingPairs excludes it before the judge is ever asked —
// the fake judge below is scripted with exactly ONE case, so a second,
// unscripted Complete call (which a broken exclusion would trigger) fails
// this test loudly via fakeprovider's own guard, proving exclusion holds
// through the real dispatch path, not merely through connectSources in
// isolation.
//
// Reuses "relation-related-uncertain-band" verbatim — the identical
// decision-table fixture capture_relation_judge_test.go's own
// TestCapture_RelationJudgePersistsOutcomeMatchingConfidenceBand (I07)
// already proves (confidence 0.40, type "same_topic", target
// 3527ca73-93c4-4688-a680-145243ce1e04) — task 9.4's own instruction: not a
// new decision table, only that connect's wiring routes through the same
// relation.Resolve/Decide function.
func TestConsolidateRunner_Connect_PersistsAcceptedJudgmentThroughRealDispatch(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 8, 1, 9, 30, 0, 0, time.UTC)
	since := now.Add(-time.Hour)
	const candidateID = "3527ca73-93c4-4688-a680-145243ce1e04"

	// SelectConnectSources treats every live unit as a candidate SOURCE, not
	// only the one this fixture means by "the source" — u-existing and
	// candidateID must sit before since (excluded as sources, spec R5.3's
	// own "since" gate) while remaining ordinary live units recall can
	// still find AS candidates, since ScoredFor applies no since filter of
	// its own.
	units := memrepo.NewUnits()
	for _, seed := range []struct {
		id, content   string
		lastTouchedAt time.Time
	}{
		{"u-source", "plan the quarterly offsite in Lisbon", now},
		{"u-existing", "quarterly offsite venue shortlist", since.Add(-time.Hour)},
		{candidateID, "quarterly offsite travel budget", since.Add(-time.Hour)},
	} {
		if err := units.Create(ctx, unit.Unit{
			ID: seed.id, Type: unit.TypeKnowledge, Status: unit.StatusPool,
			Content: seed.content, Source: "chat",
			Weight: 1.0, WeightDecayRate: 0, LastTouchedAt: seed.lastTouchedAt, CreatedAt: now, UpdatedAt: now,
		}); err != nil {
			t.Fatalf("seed %s: %v", seed.id, err)
		}
	}

	lex := memrepo.NewLexical()
	lex.SeedLexical(t, "u-source", "quarterly offsite Lisbon")
	lex.SeedLexical(t, "u-existing", "quarterly offsite venue")
	lex.SeedLexical(t, candidateID, "quarterly offsite budget")

	rels := memrepo.NewRelations()
	for _, id := range []string{"u-source", "u-existing", candidateID} {
		rels.EnsureUnit(t, id)
	}
	if err := rels.Upsert(ctx, ports.Relation{
		ID: "r-existing", FromUnitID: "u-source", ToUnitID: "u-existing", Type: "same_topic",
		Strength: 0.4, Confidence: 0.7, CreatedBy: "system", CreatedAt: now,
	}); err != nil {
		t.Fatalf("seed existing relation: %v", err)
	}

	cfg := memrepo.NewConfig()
	if err := cfg.RecordConsolidationRun(ctx, since); err != nil {
		t.Fatalf("seed since: %v", err)
	}

	rec := NewRecallService(NewIndex(recall.VectorIndex{Model: "test-model"}), lex, units, fakeprovider.NewEmbeddingFake("test-model"))
	log := memrepo.NewDecisionLog()
	judge := fakeprovider.New(t, testdataLLMCasesDir(t), "relation-related-uncertain-band")
	phase := consolidation.PhaseConnect
	svc := NewConsolidateService(fixedClock{now}, cfg, units, rels, &fakeIDs{}, log, rec, judge, memrepo.NewSelfModel(), memrepo.NewState())

	if _, err := svc.Consolidate(ctx, ConsolidateRequest{Phase: &phase}); err != nil {
		t.Fatalf("Consolidate(PhaseConnect): %v", err)
	}

	got, err := rels.ByUnit(ctx, "u-source")
	if err != nil {
		t.Fatalf("relations.ByUnit(u-source): %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("relations.ByUnit(u-source) = %v, want exactly 2 (the pre-existing one, unchanged, plus the newly persisted one)", got)
	}
	var persisted *ports.Relation
	for i := range got {
		if got[i].ToUnitID == candidateID {
			persisted = &got[i]
		}
	}
	if persisted == nil {
		t.Fatalf("no relation to %q found among %+v", candidateID, got)
	}
	if persisted.Type != "same_topic" {
		t.Errorf("Type = %q, want the judge's own %q", persisted.Type, "same_topic")
	}
	if persisted.Confidence != 0.40 {
		t.Errorf("Confidence = %v, want the judge's recorded 0.40", persisted.Confidence)
	}
	if persisted.CreatedBy != "consolidation" {
		t.Errorf("CreatedBy = %q, want %q — an automatic consolidation decision, never capture's own %q (spec R5.5)", persisted.CreatedBy, "consolidation", "system")
	}

	rows, err := log.Since(ctx, now.Add(-time.Hour), 10)
	if err != nil {
		t.Fatalf("log.Since: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("decision_log rows = %d, want exactly 1 (consolidate.connect.relation_persisted): %+v", len(rows), rows)
	}
	if rows[0].Action != ports.ActionConnectRelationPersisted {
		t.Errorf("row Action = %s, want %s", rows[0].Action, ports.ActionConnectRelationPersisted)
	}
	if rows[0].Rationale == "" {
		t.Error("consolidate.connect.relation_persisted Rationale is empty — doc 02 §11 requires a legible sentence")
	}
}

// TestConsolidateRunner_Connect_DiscardWritesNoDecisionLogRow proves design
// §7.1's own stated divergence from capture (spec R4.2's second MUST): a
// judgment consolidation.ProposeRelation refuses persists no relation AND
// writes no decision_log row for connect — deliberately unlike capture's
// own ActionRelationDiscarded row.
//
// Reuses "relation-discard-low-confidence" verbatim — the identical
// below-min_confidence_to_persist fixture (I08) capture's own
// TestCapture_RelationJudgeDiscardsBelowMinConfidenceToPersist already
// proves, confidence 0.10.
//
// Red against PR 9a's own placeholder arm, but not for the reason this
// test's own body is written to prove: the arm never calls the judge at
// all yet, so fakeprovider's own scripted-and-never-called guard fails
// this test in Cleanup before the "zero relations, zero decision_log rows"
// assertions below are ever reached. That failure is a real one — it
// means the case 9.5 wires the judge call to consume is dead code so far —
// but it does not yet exercise "a discard writes no row" specifically.
// Non-vacuity for THAT property is established once GREEN lands (task
// 9.5's own commit message): a reversion probe that made the arm call
// record unconditionally on every judged pair, regardless of
// consolidation.ProposeRelation's own ok return, observed this exact
// test's own assertions fail before the probe was reverted.
func TestConsolidateRunner_Connect_DiscardWritesNoDecisionLogRow(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 8, 1, 9, 30, 0, 0, time.UTC)
	since := now.Add(-time.Hour)
	const candidateID = "ce8d8460-dfb3-42bf-9cd0-0fc74f3dab42"

	// candidateID sits before since (excluded as a connect SOURCE, spec
	// R5.3) so it is discoverable only as u-source's own recall candidate —
	// the persist test's own fixture, above, explains why this matters.
	units := memrepo.NewUnits()
	for _, seed := range []struct {
		id, content   string
		lastTouchedAt time.Time
	}{
		{"u-source", "thinking about repainting the kitchen this weekend", now},
		{candidateID, "kitchen renovation ideas from last spring", since.Add(-time.Hour)},
	} {
		if err := units.Create(ctx, unit.Unit{
			ID: seed.id, Type: unit.TypeKnowledge, Status: unit.StatusPool,
			Content: seed.content, Source: "chat",
			Weight: 1.0, WeightDecayRate: 0, LastTouchedAt: seed.lastTouchedAt, CreatedAt: now, UpdatedAt: now,
		}); err != nil {
			t.Fatalf("seed %s: %v", seed.id, err)
		}
	}

	lex := memrepo.NewLexical()
	lex.SeedLexical(t, "u-source", "kitchen repaint weekend")
	lex.SeedLexical(t, candidateID, "kitchen renovation")

	rels := memrepo.NewRelations()
	for _, id := range []string{"u-source", candidateID} {
		rels.EnsureUnit(t, id)
	}

	cfg := memrepo.NewConfig()
	if err := cfg.RecordConsolidationRun(ctx, since); err != nil {
		t.Fatalf("seed since: %v", err)
	}

	rec := NewRecallService(NewIndex(recall.VectorIndex{Model: "test-model"}), lex, units, fakeprovider.NewEmbeddingFake("test-model"))
	log := memrepo.NewDecisionLog()
	judge := fakeprovider.New(t, testdataLLMCasesDir(t), "relation-discard-low-confidence")
	phase := consolidation.PhaseConnect
	svc := NewConsolidateService(fixedClock{now}, cfg, units, rels, &fakeIDs{}, log, rec, judge, memrepo.NewSelfModel(), memrepo.NewState())

	if _, err := svc.Consolidate(ctx, ConsolidateRequest{Phase: &phase}); err != nil {
		t.Fatalf("Consolidate(PhaseConnect): %v", err)
	}

	got, err := rels.ByUnit(ctx, "u-source")
	if err != nil {
		t.Fatalf("relations.ByUnit(u-source): %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("relations.ByUnit(u-source) = %v, want none — a Discard verdict persists nothing (I08)", got)
	}

	rows, err := log.Since(ctx, now.Add(-time.Hour), 10)
	if err != nil {
		t.Fatalf("log.Since: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("decision_log rows = %+v, want none — a judgment ProposeRelation refuses writes no row for connect, unlike capture's own relation.discarded (design §7.1, spec R4.2)", rows)
	}
}

// TestConsolidateRunner_Derive_PromptIncludesActiveBeliefsOrNamesEmptyState
// is spec R5.6's own dedup-defense-1 wiring proof (design §6.3 slot 5,
// §7.3): derive's belief_derivation call always sends
// consolidation.BuildDerivePrompt's own rendering — every active belief's
// TopicKey/Content when SelfModelRepo.ActiveBeliefs returns non-empty, or
// (PR 10a's own writeExistingBeliefs) the plain empty-state sentence when
// it returns none — never a skipped call either way (R5.6's second MUST).
//
// Red against PR 9/10a's own placeholder derive arm: the arm calls
// nothing, so the scripted judge case below is never consumed and
// fakeprovider's own under-run guard fails this test in Cleanup, before
// either subtest's own prompt-content assertion is ever reached.
func TestConsolidateRunner_Derive_PromptIncludesActiveBeliefsOrNamesEmptyState(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 8, 1, 9, 30, 0, 0, time.UTC)
	since := now.Add(-time.Hour)

	seedSourceUnit := func(t *testing.T, units *memrepo.Units) {
		t.Helper()
		if err := units.Create(ctx, unit.Unit{
			ID: "u-source", Type: unit.TypeKnowledge, Status: unit.StatusPool,
			Content: "training for a half marathon in October", Source: "chat",
			Weight: 1.0, WeightDecayRate: 0, LastTouchedAt: now, CreatedAt: now, UpdatedAt: now,
		}); err != nil {
			t.Fatalf("seed source unit: %v", err)
		}
	}

	runDerive := func(t *testing.T, selfModel ports.SelfModelRepo, judge *fakeprovider.Fake) []string {
		t.Helper()
		units := memrepo.NewUnits()
		seedSourceUnit(t, units)
		cfg := memrepo.NewConfig()
		if err := cfg.RecordConsolidationRun(ctx, since); err != nil {
			t.Fatalf("seed since: %v", err)
		}
		phase := consolidation.PhaseDerive
		svc := NewConsolidateService(fixedClock{now}, cfg, units, memrepo.NewRelations(), &fakeIDs{}, memrepo.NewDecisionLog(), testRecallOver(units), judge, selfModel, memrepo.NewState())

		if _, err := svc.Consolidate(ctx, ConsolidateRequest{Phase: &phase}); err != nil {
			t.Fatalf("Consolidate(PhaseDerive): %v", err)
		}
		return judge.SeenPrompts()
	}

	t.Run("with active beliefs, every one's topic_key and content appear in the prompt", func(t *testing.T) {
		selfModel := memrepo.NewSelfModel()
		if err := selfModel.UpsertByTopicKey(ctx, ports.Belief{
			ID: "b-1", Facet: selfmodel.FacetGoal, TopicKey: "derived/goal/fitness",
			Content: "wants to run more consistently", Confidence: 0.6, Status: "active",
			LastReinforcedAt: now, CreatedAt: now, UpdatedAt: now,
		}); err != nil {
			t.Fatalf("seed belief: %v", err)
		}

		dir := t.TempDir()
		writeDeriveCase(t, dir, "derive-with-beliefs", `{"beliefs":[]}`)
		judge := fakeprovider.New(t, dir, "derive-with-beliefs")

		prompts := runDerive(t, selfModel, judge)
		if len(prompts) != 1 {
			t.Fatalf("SeenPrompts() = %d, want exactly 1", len(prompts))
		}
		if !strings.Contains(prompts[0], "derived/goal/fitness") || !strings.Contains(prompts[0], "wants to run more consistently") {
			t.Errorf("prompt = %q, want it to contain the active belief's topic_key and content", prompts[0])
		}
	})

	t.Run("with no active beliefs, the call still sends and names the empty state", func(t *testing.T) {
		dir := t.TempDir()
		writeDeriveCase(t, dir, "derive-no-beliefs", `{"beliefs":[]}`)
		judge := fakeprovider.New(t, dir, "derive-no-beliefs")

		prompts := runDerive(t, memrepo.NewSelfModel(), judge)
		if len(prompts) != 1 {
			t.Fatalf("SeenPrompts() = %d, want exactly 1 — the call is never skipped (spec R5.6's second MUST)", len(prompts))
		}
		if !strings.Contains(prompts[0], "There are no existing self-beliefs for this user yet.") {
			t.Errorf("prompt = %q, want the empty-state sentence BuildDerivePrompt renders", prompts[0])
		}
	})
}

// TestConsolidateRunner_Derive_EmbedsExactlyOncePerActiveBelief is spec
// R5.7's own cost proof (design §6.3 slot 5): the runner embeds every
// active belief exactly once, in memory, per derive phase run — never
// more, never fewer — and no new port or store method persists the
// result (owner ruling Q2, option A; task 10b.7's own source-tree scan
// covers the second half separately).
//
// The scripted belief_derivation response below decodes to zero proposed
// beliefs, so this fixture isolates the EXISTING side of R5.7's embedding
// cost from the PROPOSED side task 10b.5/10b.6's own merge-routing fixture
// exercises — MergeProposals's "proposed" vectors are embedded too
// (task 10b.4's own GREEN), but only when the judge actually proposes
// something, which this fixture deliberately does not.
//
// Red against task 10b.2's own placeholder-embedding derive: it never
// calls EmbeddingProvider at all, so EmbedCalls() is 0 against a fixture
// seeding 2 active beliefs.
func TestConsolidateRunner_Derive_EmbedsExactlyOncePerActiveBelief(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 8, 1, 9, 30, 0, 0, time.UTC)
	since := now.Add(-time.Hour)

	units := memrepo.NewUnits()
	if err := units.Create(ctx, unit.Unit{
		ID: "u-source", Type: unit.TypeKnowledge, Status: unit.StatusPool,
		Content: "training for a half marathon in October", Source: "chat",
		Weight: 1.0, WeightDecayRate: 0, LastTouchedAt: now, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("seed source unit: %v", err)
	}

	selfModel := memrepo.NewSelfModel()
	for _, b := range []ports.Belief{
		{ID: "b-1", Facet: selfmodel.FacetGoal, TopicKey: "derived/goal/fitness", Content: "wants to run more consistently", Confidence: 0.6, Status: "active", LastReinforcedAt: now, CreatedAt: now, UpdatedAt: now},
		{ID: "b-2", Facet: selfmodel.FacetValue, TopicKey: "derived/value/health", Content: "values staying active", Confidence: 0.5, Status: "active", LastReinforcedAt: now, CreatedAt: now, UpdatedAt: now},
	} {
		if err := selfModel.UpsertByTopicKey(ctx, b); err != nil {
			t.Fatalf("seed belief %s: %v", b.ID, err)
		}
	}

	cfg := memrepo.NewConfig()
	if err := cfg.RecordConsolidationRun(ctx, since); err != nil {
		t.Fatalf("seed since: %v", err)
	}

	embed := fakeprovider.NewEmbeddingFake("test-model")
	rec := NewRecallService(NewIndex(recall.VectorIndex{Model: "test-model"}), memrepo.NewLexical(), units, embed)

	dir := t.TempDir()
	writeDeriveCase(t, dir, "derive-embed-count", `{"beliefs":[]}`)
	judge := fakeprovider.New(t, dir, "derive-embed-count")

	phase := consolidation.PhaseDerive
	svc := NewConsolidateService(fixedClock{now}, cfg, units, memrepo.NewRelations(), &fakeIDs{}, memrepo.NewDecisionLog(), rec, judge, selfModel, memrepo.NewState())

	if _, err := svc.Consolidate(ctx, ConsolidateRequest{Phase: &phase}); err != nil {
		t.Fatalf("Consolidate(PhaseDerive): %v", err)
	}

	if got, want := embed.EmbedCalls(), 2; got != want {
		t.Fatalf("EmbedCalls() = %d, want exactly len(activeBeliefs) = %d (spec R5.7)", got, want)
	}
}

// spySelfModel counts UpsertByTopicKey/ReinforceByID calls on top of a
// real memrepo.SelfModel — task 10b.5's own "exactly one of each" proof
// needs call counts, not merely final state, the same reason spyConfig
// (above) wraps memrepo.Config instead of reading RecordConsolidationRun's
// own side effect back out.
type spySelfModel struct {
	*memrepo.SelfModel
	upsertCalls    int
	reinforceCalls int
}

func newSpySelfModel() *spySelfModel {
	return &spySelfModel{SelfModel: memrepo.NewSelfModel()}
}

func (s *spySelfModel) UpsertByTopicKey(ctx context.Context, b ports.Belief) error {
	s.upsertCalls++
	return s.SelfModel.UpsertByTopicKey(ctx, b)
}

func (s *spySelfModel) ReinforceByID(ctx context.Context, id string, confidence float64, at time.Time) error {
	s.reinforceCalls++
	return s.SelfModel.ReinforceByID(ctx, id, confidence, at)
}

// TestConsolidateRunner_Derive_RoutesCreateAndMergeToTheirOwnWrite is spec
// R5.8's own split: one create-decision and one merge-decision from the
// same derive run produce exactly one SelfModelRepo.UpsertByTopicKey call
// and exactly one ReinforceByID call, each with the correct target.
//
// fakeprovider.NewEmbeddingFake derives a deterministic vector from text
// alone (test/support/fakeprovider/embed.go): a proposed belief whose
// Content is byte-identical to an existing one embeds to the identical
// vector (cosine similarity 1.0, well above BeliefMergeCosine's 0.85) and
// therefore merges; a proposed belief with unrelated text embeds far
// enough away to create instead. No hand-derived vector geometry is
// needed to land on the 0.85 boundary — reusing the existing belief's own
// Content verbatim is exact by construction.
//
// Red against task 10b.4's own derive: decisions is computed and then
// discarded (`_ = decisions`), so neither write ever happens.
func TestConsolidateRunner_Derive_RoutesCreateAndMergeToTheirOwnWrite(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 8, 1, 9, 30, 0, 0, time.UTC)
	since := now.Add(-time.Hour)
	const existingContent = "wants to run more consistently"

	units := memrepo.NewUnits()
	if err := units.Create(ctx, unit.Unit{
		ID: "u-source", Type: unit.TypeKnowledge, Status: unit.StatusPool,
		Content: "training log entry", Source: "chat",
		Weight: 1.0, WeightDecayRate: 0, LastTouchedAt: now, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("seed source unit: %v", err)
	}

	selfModel := newSpySelfModel()
	if err := selfModel.UpsertByTopicKey(ctx, ports.Belief{
		ID: "b-existing", Facet: selfmodel.FacetGoal, TopicKey: "derived/goal/fitness",
		Content: existingContent, Confidence: 0.5, Status: "active",
		LastReinforcedAt: now, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("seed existing belief: %v", err)
	}
	selfModel.upsertCalls = 0 // reset: only derive's own calls count below

	cfg := memrepo.NewConfig()
	if err := cfg.RecordConsolidationRun(ctx, since); err != nil {
		t.Fatalf("seed since: %v", err)
	}

	embed := fakeprovider.NewEmbeddingFake("test-model")
	rec := NewRecallService(NewIndex(recall.VectorIndex{Model: "test-model"}), memrepo.NewLexical(), units, embed)

	dir := t.TempDir()
	response := `{"beliefs":[` +
		`{"facet":"goal","topic_key":"fitness-again","content":"` + existingContent + `","confidence":0.55},` +
		`{"facet":"goal","topic_key":"hiking","content":"enjoys weekend hiking trips","confidence":0.7}` +
		`]}`
	writeDeriveCase(t, dir, "derive-create-and-merge", response)
	judge := fakeprovider.New(t, dir, "derive-create-and-merge")

	phase := consolidation.PhaseDerive
	svc := NewConsolidateService(fixedClock{now}, cfg, units, memrepo.NewRelations(), &fakeIDs{}, memrepo.NewDecisionLog(), rec, judge, selfModel, memrepo.NewState())

	if _, err := svc.Consolidate(ctx, ConsolidateRequest{Phase: &phase}); err != nil {
		t.Fatalf("Consolidate(PhaseDerive): %v", err)
	}

	if selfModel.upsertCalls != 1 {
		t.Errorf("UpsertByTopicKey calls = %d, want exactly 1 (the hiking belief)", selfModel.upsertCalls)
	}
	if selfModel.reinforceCalls != 1 {
		t.Errorf("ReinforceByID calls = %d, want exactly 1 (b-existing)", selfModel.reinforceCalls)
	}

	active, err := selfModel.ActiveBeliefs(ctx)
	if err != nil {
		t.Fatalf("ActiveBeliefs: %v", err)
	}
	if len(active) != 2 {
		t.Fatalf("ActiveBeliefs() = %d, want exactly 2 (b-existing reinforced, hiking created)", len(active))
	}
	var reinforced ports.Belief
	var created ports.Belief
	for _, b := range active {
		if b.ID == "b-existing" {
			reinforced = b
		} else {
			created = b
		}
	}
	if reinforced.Confidence <= 0.5 {
		t.Errorf("b-existing Confidence = %v, want raised above its seeded 0.5 (consolidation.Reinforce)", reinforced.Confidence)
	}
	if created.Content != "enjoys weekend hiking trips" {
		t.Errorf("created belief Content = %q, want the hiking proposal's own content", created.Content)
	}
}

// TestDecodeDerivedBeliefs_DedupsByTopicKey pins the collision Judgment Day
// found on this PR (Judge B: CRITICAL, Judge A: SUGGESTION — escalated and
// ruled a defect by the owner).
//
// A belief_derivation response is free to propose the same (facet,
// topic_key) twice with different content — nothing in the prompt forbids
// it and a degraded judge will do it. Without dedup both proposals reached
// createDerivedBelief, so each wrote its own ActionDeriveBeliefCreated row
// while UpsertByTopicKey (INSERT ... ON CONFLICT(topic_key) DO UPDATE)
// silently overwrote the first in self_beliefs. decision_log then claimed
// two beliefs were created when the vault held one, and the losing
// proposal's content vanished with no refusal and no corrupted entry.
//
// doc 05's own M2 demo bullet is "the decision_log tells the story", so an
// audit trail that reports an effect the vault never kept is not a
// documented-behavior footnote — it is the one thing that phase must not
// do. First proposal wins; later collisions are dropped at decode, before
// any write or any row exists.
func TestDecodeDerivedBeliefs_DedupsByTopicKey(t *testing.T) {
	raw := `{"beliefs":[
		{"facet":"preference","topic_key":"ui-theme","content":"Prefers dark mode.","confidence":0.8},
		{"facet":"preference","topic_key":"ui-theme","content":"Prefers light mode.","confidence":0.7},
		{"facet":"identity","topic_key":"role","content":"Backend engineer.","confidence":0.9}
	]}`

	got := decodeDerivedBeliefs(raw)

	if len(got) != 2 {
		t.Fatalf("decodeDerivedBeliefs returned %d proposals, want 2 — the duplicate (preference, ui-theme) must be dropped at decode: %+v", len(got), got)
	}
	if got[0].Facet != "preference" || got[0].Key != "ui-theme" || got[0].Content != "Prefers dark mode." {
		t.Errorf("first proposal = %+v, want the FIRST (preference, ui-theme) entry — first wins, later collisions drop", got[0])
	}
	if got[1].Facet != "identity" || got[1].Key != "role" {
		t.Errorf("second proposal = %+v, want the (identity, role) entry — a different topic_key is not a collision", got[1])
	}
}

// TestConsolidateRunner_Reweight_PersistsBoostAndOmitsCorruptedFromLog is
// the exact m2b spec R3.3 scenario (internal/core/consolidation's own
// TestReweight_UnitMayAppearInBothBoostsAndCorrupted) re-run through the
// wired PhaseReweight arm (design §6.3 slot 6, spec R5.9): unit "x" gains a
// clean edge from origin "a" (Strength 0.9) and, in the SAME call, a
// corrupt-strength edge from origin "b" (NaN). "x" is boosted through the
// clean edge and refused through the corrupt one — both facts hold at once
// (spec R3.3's own "neither suppresses the other") — and only the boost
// reaches decision_log: R4.2's MUST NOT says a Reweight corrupted entry has
// no vault effect, so it is never logged. "a" is boosted too (Resurface's
// own hop-0 self-effect over its one valid outgoing edge); "b" is refused
// and gets no boost at all.
//
// newRelationEdges is set directly on the report (white-box, package
// brain) rather than produced by driving PhaseConnect first: connect's own
// persisted relations always carry a finite, [0,1]-bounded Strength
// (relation.Resolve's own contract), so the "corrupted" half of this
// scenario cannot arise from a real judge response — the identical
// reasoning TestConsolidateRunner_Record (above) already applies to
// exercise record's ten slots directly rather than through ten scripted
// whole passes.
func TestConsolidateRunner_Reweight_PersistsBoostAndOmitsCorruptedFromLog(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 8, 7, 3, 0, 0, 0, time.UTC)

	units := memrepo.NewUnits()
	for _, id := range []string{"a", "b", "x"} {
		if err := units.Create(ctx, unit.Unit{
			ID: id, Type: unit.TypeKnowledge, Status: unit.StatusPool,
			Content: "seed content for " + id, Source: "chat",
			Weight: 0, WeightDecayRate: 0,
			LastTouchedAt: now, CreatedAt: now, UpdatedAt: now,
		}); err != nil {
			t.Fatalf("seed %s: %v", id, err)
		}
	}

	log := memrepo.NewDecisionLog()
	r := consolidateRunner{units: units, ids: &fakeIDs{}, log: log}
	report := &ConsolidateReport{
		newRelationEdges: []weight.Edge{
			{From: "a", To: "x", Strength: 0.9},
			{From: "b", To: "x", Strength: math.NaN()},
		},
	}

	if err := r.runPhase(ctx, consolidation.PhaseReweight, passContext{now: now}, report); err != nil {
		t.Fatalf("runPhase(PhaseReweight): %v", err)
	}

	gotA, err := units.ByID(ctx, "a")
	if err != nil {
		t.Fatalf("ByID(a): %v", err)
	}
	if gotA.Weight != 0.315 {
		t.Errorf("a weight = %v, want 0.315 (boosted through its own valid a->x edge)", gotA.Weight)
	}
	if !gotA.LastTouchedAt.Equal(now) {
		t.Errorf("a LastTouchedAt = %v, want %v", gotA.LastTouchedAt, now)
	}

	gotX, err := units.ByID(ctx, "x")
	if err != nil {
		t.Fatalf("ByID(x): %v", err)
	}
	if gotX.Weight != 0.315 {
		t.Errorf("x weight = %v, want 0.315 (the boosted value ApplyBoosts must persist, despite also being corrupted)", gotX.Weight)
	}
	if !gotX.LastTouchedAt.Equal(now) {
		t.Errorf("x LastTouchedAt = %v, want %v", gotX.LastTouchedAt, now)
	}

	gotB, err := units.ByID(ctx, "b")
	if err != nil {
		t.Fatalf("ByID(b): %v", err)
	}
	if gotB.Weight != 0 {
		t.Errorf("b weight = %v, want unchanged 0 — b is refused, never boosted", gotB.Weight)
	}

	corruptedSeen := make(map[string]bool, len(report.corrupted))
	for _, id := range report.corrupted {
		corruptedSeen[id] = true
	}
	if len(corruptedSeen) != 2 || !corruptedSeen["b"] || !corruptedSeen["x"] {
		t.Errorf("report.corrupted = %v, want exactly [b x] in some order", report.corrupted)
	}

	rows, err := log.Since(ctx, now.Add(-time.Hour), 10)
	if err != nil {
		t.Fatalf("log.Since: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("decision_log rows = %d, want exactly 2 (one per boosted unit: a, x) — none for the corrupted half", len(rows))
	}
	for _, row := range rows {
		if row.Action != ports.ActionReweightBoostApplied {
			t.Errorf("row Action = %s, want %s", row.Action, ports.ActionReweightBoostApplied)
		}
	}
}

// TestConsolidateRunner_PatternEval_StagnationFindingsEachProduceOneRow is
// spec R5.10's first MUST (design §6.3 slot 7): every StagnationFinding
// consolidation.EvaluateStagnation returns is recorded to decision_log,
// correctly attributed. Three beliefs: one goal-facet belief stagnant well
// past DefaultGoalStagnationDays (21), one goal-facet belief reinforced
// today (not stagnant), and one preference-facet belief stagnant for the
// same span as the first — EvaluateStagnation's own facet filter (only
// selfmodel.FacetGoal) excludes it, so it must never produce a finding
// however stagnant.
//
// Red: the placeholder pattern_eval arm calls nothing — decision_log stays
// empty.
func TestConsolidateRunner_PatternEval_StagnationFindingsEachProduceOneRow(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 8, 10, 3, 0, 0, 0, time.UTC)
	stagnantSince := now.Add(-30 * 24 * time.Hour)

	selfModel := memrepo.NewSelfModel()
	seeds := []struct {
		id, topicKey     string
		facet            selfmodel.Facet
		lastReinforcedAt time.Time
	}{
		{"b-stagnant-goal", "derived/goal/fitness", selfmodel.FacetGoal, stagnantSince},
		{"b-fresh-goal", "derived/goal/reading", selfmodel.FacetGoal, now},
		{"b-stagnant-preference", "derived/preference/coffee", selfmodel.FacetPreference, stagnantSince},
	}
	for _, s := range seeds {
		if err := selfModel.UpsertByTopicKey(ctx, ports.Belief{
			ID: s.id, Facet: s.facet, TopicKey: s.topicKey,
			Content: "seed content for " + s.id, Confidence: 0.5, Status: "active",
			LastReinforcedAt: s.lastReinforcedAt, CreatedAt: now, UpdatedAt: now,
		}); err != nil {
			t.Fatalf("seed %s: %v", s.id, err)
		}
	}

	log := memrepo.NewDecisionLog()
	r := consolidateRunner{selfModel: selfModel, units: memrepo.NewUnits(), state: memrepo.NewState(), ids: &fakeIDs{}, log: log}
	pass := passContext{now: now}

	if err := r.runPhase(ctx, consolidation.PhasePatternEval, pass, &ConsolidateReport{}); err != nil {
		t.Fatalf("runPhase(PhasePatternEval): %v", err)
	}

	rows, err := log.Since(ctx, now.Add(-time.Hour), 10)
	if err != nil {
		t.Fatalf("log.Since: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("decision_log rows = %d, want exactly 1 (only b-stagnant-goal: a fresh goal belief and a stagnant NON-goal belief must not fire, and no mental_load units exist so the load half stays silent)", len(rows))
	}
	if rows[0].Action != ports.ActionPatternEvalStagnationFound {
		t.Errorf("row Action = %s, want %s", rows[0].Action, ports.ActionPatternEvalStagnationFound)
	}
	var detail consolidation.StagnationFinding
	if err := json.Unmarshal(rows[0].Context, &detail); err != nil {
		t.Fatalf("unmarshal row Context: %v", err)
	}
	if detail.BeliefID != "b-stagnant-goal" {
		t.Errorf("row Context.BeliefID = %q, want %q — correctly attributed to the stagnant goal belief", detail.BeliefID, "b-stagnant-goal")
	}
}

// TestConsolidateRunner_PatternEval_LoadFiringOpensHypothesisAndLogs is
// spec R5.10's second MUST (design §3.2 Q6, §6.3 slot 7): EvaluateLoad
// firing appends exactly one current_state row through StateRepo's own
// OpenHypothesis, plus one decision_log row whose Context states the
// lastHypothesisAt mapping this phase uses (m2b design §9 Q6's own
// undecided question, mapped here: lastHypothesisAt is
// StateRepo.LastHypothesisAt's own return, the previous hypothesis's own
// recorded_at). Not firing (below threshold) writes zero of both.
//
// Red: the placeholder pattern_eval arm's load half does not exist yet —
// no current_state row is ever appended and no
// ActionPatternEvalLoadHypothesisOpened row is ever logged.
func TestConsolidateRunner_PatternEval_LoadFiringOpensHypothesisAndLogs(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 8, 10, 3, 0, 0, 0, time.UTC)

	t.Run("firing: count at or above threshold opens one hypothesis and logs its lastHypothesisAt mapping", func(t *testing.T) {
		units := memrepo.NewUnits()
		for i := 0; i < consolidation.DefaultMentalLoadThreshold; i++ {
			id := "u-load-" + string(rune('0'+i))
			if err := units.Create(ctx, unit.Unit{
				ID: id, Type: unit.TypeMentalLoad, Status: unit.StatusPool,
				Content: "open loop " + id, Source: "chat",
				LastTouchedAt: now, CreatedAt: now, UpdatedAt: now,
			}); err != nil {
				t.Fatalf("seed %s: %v", id, err)
			}
		}

		state := memrepo.NewState()
		log := memrepo.NewDecisionLog()
		r := consolidateRunner{units: units, state: state, selfModel: memrepo.NewSelfModel(), ids: &fakeIDs{}, log: log}

		if err := r.runPhase(ctx, consolidation.PhasePatternEval, passContext{now: now}, &ConsolidateReport{}); err != nil {
			t.Fatalf("runPhase(PhasePatternEval): %v", err)
		}

		lastAt, err := state.LastHypothesisAt(ctx)
		if err != nil {
			t.Fatalf("LastHypothesisAt: %v", err)
		}
		if lastAt == nil || !lastAt.Equal(now) {
			t.Fatalf("LastHypothesisAt = %v, want %v — OpenHypothesis must append exactly one current_state row", lastAt, now)
		}

		rows, err := log.Since(ctx, now.Add(-time.Hour), 10)
		if err != nil {
			t.Fatalf("log.Since: %v", err)
		}
		var loadRow *ports.Decision
		for i := range rows {
			if rows[i].Action == ports.ActionPatternEvalLoadHypothesisOpened {
				loadRow = &rows[i]
			}
		}
		if loadRow == nil {
			t.Fatalf("decision_log rows = %+v, want one %s row", rows, ports.ActionPatternEvalLoadHypothesisOpened)
		}
		if !strings.Contains(string(loadRow.Context), "recorded_at") {
			t.Errorf("row Context = %s, want it to state the lastHypothesisAt mapping (m2b design §9 Q6's own instruction: \"m2c must map it and say so in the decision_log context\")", loadRow.Context)
		}
	})

	t.Run("not firing: below threshold appends no current_state row and logs nothing", func(t *testing.T) {
		units := memrepo.NewUnits()
		state := memrepo.NewState()
		log := memrepo.NewDecisionLog()
		r := consolidateRunner{units: units, state: state, selfModel: memrepo.NewSelfModel(), ids: &fakeIDs{}, log: log}

		if err := r.runPhase(ctx, consolidation.PhasePatternEval, passContext{now: now}, &ConsolidateReport{}); err != nil {
			t.Fatalf("runPhase(PhasePatternEval): %v", err)
		}

		lastAt, err := state.LastHypothesisAt(ctx)
		if err != nil {
			t.Fatalf("LastHypothesisAt: %v", err)
		}
		if lastAt != nil {
			t.Errorf("LastHypothesisAt = %v, want nil — below threshold must open no hypothesis", lastAt)
		}

		rows, err := log.Since(ctx, now.Add(-time.Hour), 10)
		if err != nil {
			t.Fatalf("log.Since: %v", err)
		}
		for _, row := range rows {
			if row.Action == ports.ActionPatternEvalLoadHypothesisOpened {
				t.Errorf("decision_log gained a %s row, want none below threshold", ports.ActionPatternEvalLoadHypothesisOpened)
			}
		}
	})
}
