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
// RecordConsolidationRun calls. The "since read once" and
// "consolidation_last_run_at written once" properties (spec R5.3, R5.4;
// design §3.3(c)-(d)) are call-count claims, not stored-value claims, so a
// plain memrepo.Config fixture cannot prove them by itself.
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
