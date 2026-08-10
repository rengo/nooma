package brain

import (
	"context"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/core/consolidation"
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
// ConsolidateReport.PhasesRun (consolidate.go) is this PR's own answer to
// spec R4.1's "spy recording each phase's invocation" requirement: no
// production case in runPhase's switch calls any port this PR wires yet
// (every arm is a placeholder — design §3.3(b)), so there is nothing else
// for a spy to observe from outside the package. PhasesRun is real output,
// not a test-only seam — it is also what `nooma consolidate`'s eventual
// report rendering (PR 12) reads.

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
	svc := NewConsolidateService(fixedClock{now}, memrepo.NewConfig())

	report, err := svc.Consolidate(ctx, ConsolidateRequest{})
	if err != nil {
		t.Fatalf("Consolidate: %v", err)
	}

	want := consolidation.Order()
	if len(report.PhasesRun) != len(want) {
		t.Fatalf("PhasesRun = %v, want %v", report.PhasesRun, want)
	}
	for i, p := range want {
		if report.PhasesRun[i] != p {
			t.Errorf("PhasesRun[%d] = %s, want %s", i, report.PhasesRun[i], p)
		}
	}
	if last := report.PhasesRun[len(report.PhasesRun)-1]; last != consolidation.PhaseLearn {
		t.Errorf("last phase run = %s, want PhaseLearn (I11)", last)
	}
}

// TestConsolidate_PerPhase is design §3.3(a)-(b)'s per-phase scope: a
// ConsolidateRequest naming one Phase reaches exactly that phase's arm and
// no other; a Phase value outside consolidation.Order()'s range errors
// through runPhase's own default case rather than being silently skipped.
//
// Disclosed rather than presented as a literal RED: by the time this test
// is added, 7a.2's filter loop and switch already implement both
// properties in full (design §3.3(b)'s own code shape, quoted verbatim),
// so both subtests pass immediately — proof that the already-implemented
// filter and default are correct, not a behaviour change. Matches this
// change's own m2a C9/task-8.3 precedent for a stated "Red" that does not
// materialize as a missing-symbol or failing-assertion red once written.
func TestConsolidate_PerPhase(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 8, 10, 3, 0, 0, 0, time.UTC)

	t.Run("reaches exactly the requested phase", func(t *testing.T) {
		svc := NewConsolidateService(fixedClock{now}, memrepo.NewConfig())
		phase := consolidation.PhaseArchive

		report, err := svc.Consolidate(ctx, ConsolidateRequest{Phase: &phase})
		if err != nil {
			t.Fatalf("Consolidate: %v", err)
		}
		if len(report.PhasesRun) != 1 || report.PhasesRun[0] != consolidation.PhaseArchive {
			t.Fatalf("PhasesRun = %v, want exactly [PhaseArchive]", report.PhasesRun)
		}
	})

	t.Run("an unknown phase errors through runPhase's default", func(t *testing.T) {
		r := consolidateRunner{}
		if err := r.runPhase(ctx, consolidation.Phase(99), passContext{}); err == nil {
			t.Fatal("runPhase(99) error = nil, want an error naming the unhandled phase")
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
		svc := NewConsolidateService(fixedClock{now}, cfg)
		if _, err := svc.Consolidate(ctx, ConsolidateRequest{}); err != nil {
			t.Fatalf("Consolidate: %v", err)
		}
		if cfg.loadCalls != 1 {
			t.Errorf("Load calls = %d, want exactly 1", cfg.loadCalls)
		}
	})

	t.Run("Load is called exactly once for a per-phase run too", func(t *testing.T) {
		cfg := newSpyConfig()
		svc := NewConsolidateService(fixedClock{now}, cfg)
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
		svc := NewConsolidateService(fixedClock{now}, cfg)
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
		svc := NewConsolidateService(fixedClock{now}, cfg)
		phase := consolidation.PhaseLearn
		if _, err := svc.Consolidate(ctx, ConsolidateRequest{Phase: &phase}); err != nil {
			t.Fatalf("Consolidate: %v", err)
		}
		if cfg.recordCalls != 0 {
			t.Errorf("RecordConsolidationRun calls = %d, want 0 for a per-phase run", cfg.recordCalls)
		}
	})
}
