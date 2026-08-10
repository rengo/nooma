package brain

import (
	"context"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/core/consolidation"
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

// TestConsolidate_WholePassReachesEveryPhaseInOrder is spec R4.1's own
// scenario (I11's behavioural half, design §3.3(b)): a whole pass
// (ConsolidateRequest's zero value) reaches every consolidation.Order()
// member, in that exact sequence, with PhaseLearn's slot reached last.
func TestConsolidate_WholePassReachesEveryPhaseInOrder(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 8, 10, 3, 0, 0, 0, time.UTC)
	svc := NewConsolidateService(fixedClock{now})

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
		svc := NewConsolidateService(fixedClock{now})
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
