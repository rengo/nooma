// Package conformance — see test/conformance/doc.go for the package contract.
package conformance

import (
	"context"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/brain"
	"github.com/rengo/nooma/internal/core/prospection"
	"github.com/rengo/nooma/internal/ports"
	"github.com/rengo/nooma/test/support/memrepo"
)

// TestCheckEffectCompleteness is I12 in both directions, swept over
// prospection.AllVerdicts() crossed with {trigger, timer}.
//
// I12 is usually read one way — every effect writes a row — and the other
// half is the one that rots: a pass that decided nothing must write
// nothing. A scan runs every few minutes forever, so a spurious row per
// deferred item is not a cosmetic defect; it is dozens of rows a night
// per item, and a glass box nobody can read is a glass box that is not
// there.
//
// The sweep is over the vocabulary, not over a list of cases, so a fifth
// Verdict fails here rather than quietly acquiring whatever behaviour the
// runner's default branch happens to have.
func TestCheckEffectCompleteness(t *testing.T) {
	verdicts := prospection.AllVerdicts()
	if len(verdicts) == 0 {
		t.Fatal("prospection.AllVerdicts() is empty — this sweep proves nothing")
	}

	// fireAt sits at a Wednesday noon; each verdict is reached by moving
	// now relative to it, never by constructing the verdict directly —
	// what is under test is the runner reached by the real gate.
	fireAt := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	nowFor := map[prospection.Verdict]time.Time{
		prospection.VerdictPending: fireAt.Add(-time.Hour),
		prospection.VerdictDeliver: fireAt.Add(time.Minute),
		prospection.VerdictStale:   fireAt.Add(time.Duration(prospection.TriggerStalenessHours+1) * time.Hour),
		prospection.VerdictDefer:   time.Date(2026, 8, 5, prospection.QuietHoursStartHour, 30, 0, 0, time.UTC),
	}

	// wantRows is how many decision_log rows each cell must produce.
	// Written as expectations about EFFECTS, not about verdicts: a cell
	// writes a row exactly when it changed a row.
	wantTriggerRows := map[prospection.Verdict]int{
		prospection.VerdictPending: 0,
		prospection.VerdictDefer:   0,
		prospection.VerdictStale:   1,
		// Nothing in this change can surface a fired trigger, so there is
		// no effect to record and no row. This is the cell that would
		// silently gain a row the day someone "completed" the mapping.
		prospection.VerdictDeliver: 0,
	}
	wantTimerRows := map[prospection.Verdict]int{
		prospection.VerdictPending: 0,
		prospection.VerdictDefer:   0,
		prospection.VerdictStale:   1,
		prospection.VerdictDeliver: 1,
	}

	for _, v := range verdicts {
		now, reachable := nowFor[v]
		if !reachable {
			t.Errorf("verdict %q has no instant that reaches it — a member was added and this sweep was not revisited", v)
			continue
		}

		t.Run("trigger/"+string(v), func(t *testing.T) {
			want, known := wantTriggerRows[v]
			if !known {
				t.Fatalf("verdict %q has no expected trigger row count", v)
			}

			triggers, timers, decisions := freshCheckFakes(t)
			at := fireAt
			if err := triggers.Create(context.Background(), ports.Trigger{
				ID: "trg", Kind: ports.TriggerKindTimeBased, FireAt: &at, CreatedAt: fireAt.Add(-time.Hour),
			}); err != nil {
				t.Fatalf("Create: %v", err)
			}

			report := runCheck(t, now, triggers, timers, decisions)
			assertRowCount(t, decisions, fireAt.Add(-24*time.Hour), want)

			// The report's own arithmetic must agree with the row count:
			// I12 stated twice, from two independent observations.
			if effects := report.TriggersExpired; effects != want {
				t.Errorf("report says %d trigger effect(s), decision_log says %d", effects, want)
			}
		})

		t.Run("timer/"+string(v), func(t *testing.T) {
			want, known := wantTimerRows[v]
			if !known {
				t.Fatalf("verdict %q has no expected timer row count", v)
			}

			triggers, timers, decisions := freshCheckFakes(t)
			if err := timers.Create(context.Background(), ports.Timer{
				ID: "tmr", FireAt: fireAt, CreatedAt: fireAt.Add(-time.Hour),
			}); err != nil {
				t.Fatalf("Create: %v", err)
			}

			// A timer never defers — TimerVerdict passes
			// deferInQuietHours = false, which is the one push exception
			// to quiet hours (doc 02 §7). At the Defer instant a timer is
			// judged on staleness alone, so its expected count is that
			// verdict's, not zero. Encoding it as "0 because defer" would
			// have been the mistake this comment exists to prevent.
			if v == prospection.VerdictDefer {
				want = wantTimerRows[prospection.TimerVerdict(fireAt, now)]
			}

			report := runCheck(t, now, triggers, timers, decisions)
			assertRowCount(t, decisions, fireAt.Add(-24*time.Hour), want)

			if effects := report.TimersFired + report.TimersCancelled; effects != want {
				t.Errorf("report says %d timer effect(s), decision_log says %d", effects, want)
			}
		})
	}
}

func freshCheckFakes(t *testing.T) (*memrepo.Triggers, *memrepo.Timers, *memrepo.DecisionLog) {
	t.Helper()
	return memrepo.NewTriggers(), memrepo.NewTimers(), memrepo.NewDecisionLog()
}

func runCheck(t *testing.T, now time.Time, triggers ports.TriggerRepo, timers ports.TimerRepo, decisions ports.DecisionLog) brain.CheckReport {
	t.Helper()

	report, err := brain.NewCheckService(fixedClock{now: now}, triggers, timers, &counterIDs{}, decisions).
		Check(context.Background(), brain.CheckRequest{})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	return report
}

func assertRowCount(t *testing.T, decisions ports.DecisionLog, since time.Time, want int) {
	t.Helper()

	rows, err := decisions.Since(context.Background(), since, -1)
	if err != nil {
		t.Fatalf("decisions.Since: %v", err)
	}
	if len(rows) != want {
		t.Fatalf("decision_log has %d rows, want %d: %+v", len(rows), want, rows)
	}
}
