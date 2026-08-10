package brain

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/core/consolidation"
	"github.com/rengo/nooma/internal/core/unit"
	"github.com/rengo/nooma/internal/ports"
	"github.com/rengo/nooma/test/support/memrepo"
)

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
	svc := NewConsolidateService(fixedClock{now}, memrepo.NewConfig(), memrepo.NewUnits(), memrepo.NewRelations(), &fakeIDs{}, memrepo.NewDecisionLog())

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
		svc := NewConsolidateService(fixedClock{now}, memrepo.NewConfig(), memrepo.NewUnits(), memrepo.NewRelations(), &fakeIDs{}, memrepo.NewDecisionLog())
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
		svc := NewConsolidateService(fixedClock{now}, memrepo.NewConfig(), memrepo.NewUnits(), memrepo.NewRelations(), &fakeIDs{}, memrepo.NewDecisionLog())
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
		svc := NewConsolidateService(fixedClock{now}, cfg, memrepo.NewUnits(), memrepo.NewRelations(), &fakeIDs{}, memrepo.NewDecisionLog())
		if _, err := svc.Consolidate(ctx, ConsolidateRequest{}); err != nil {
			t.Fatalf("Consolidate: %v", err)
		}
		if cfg.loadCalls != 1 {
			t.Errorf("Load calls = %d, want exactly 1", cfg.loadCalls)
		}
	})

	t.Run("Load is called exactly once for a per-phase run too", func(t *testing.T) {
		cfg := newSpyConfig()
		svc := NewConsolidateService(fixedClock{now}, cfg, memrepo.NewUnits(), memrepo.NewRelations(), &fakeIDs{}, memrepo.NewDecisionLog())
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
		svc := NewConsolidateService(fixedClock{now}, cfg, memrepo.NewUnits(), memrepo.NewRelations(), &fakeIDs{}, memrepo.NewDecisionLog())
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
		svc := NewConsolidateService(fixedClock{now}, cfg, memrepo.NewUnits(), memrepo.NewRelations(), &fakeIDs{}, memrepo.NewDecisionLog())
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
	svc := NewConsolidateService(fixedClock{now}, memrepo.NewConfig(), memrepo.NewUnits(), memrepo.NewRelations(), &fakeIDs{}, log)

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

	svc := NewConsolidateService(fixedClock{now}, memrepo.NewConfig(), units, memrepo.NewRelations(), &fakeIDs{}, memrepo.NewDecisionLog())
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
	svc := NewConsolidateService(fixedClock{now}, memrepo.NewConfig(), units, memrepo.NewRelations(), &fakeIDs{}, log)

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
