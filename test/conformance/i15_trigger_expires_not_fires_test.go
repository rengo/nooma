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

// i15SchemaStatuses are the status strings the schema itself owns —
// triggers ("armed", "fired", "dismissed", "expired", doc 02 §7) and
// timers ("pending", "fired", "cancelled", doc 02 §8). prospection.Verdict
// must never surface one of these directly: brain names the transition
// (design §3.3), core states only its own neutral vocabulary.
//
// "pending" is the one schema status deliberately EXCLUDED from the list.
// The timers table owns it (doc 02 §8) and Verdict has a same-named member
// of its own, but the two are homonyms rather than the same fact: the
// schema's is a persisted timer row's state, VerdictPending is "fire_at is
// still in the future", which TriggerVerdict returns legitimately and this
// sweep sees on every not-yet-due offset. Listing it would fail the test
// against correct behaviour. The collision is a coincidence of vocabulary,
// and this comment exists so the next reader does not "complete" the list.
var i15SchemaStatuses = []prospection.Verdict{"armed", "fired", "dismissed", "expired", "cancelled"}

// TestI15_TriggerOverdueExpiresNeverFires proves invariant I15
// (docs/06-harness.md §4, ADR-0009): a trigger overdue past
// TriggerStalenessHours is always VerdictStale, never VerdictDeliver, and
// TriggerVerdict never surfaces a status the schema itself owns —
// core says "stale", not "expired"; brain owns that mapping.
//
// The sweep is deliberately NOT built by recomputing TriggerVerdict's own
// overdue formula from prospection.TriggerStalenessHours: an L2 gate that
// reimplements the L1 function it is guarding agrees with a broken
// implementation by construction, whatever the constant says (the defect
// class test/conformance/i16_quiet_hours_test.go's own sweep was
// corrected for, commit 5da3bd4). Instead this sweep fixes fireAt
// candidates at fixed wall-clock offsets before now — 0 through 47 hours,
// half-hour steps — asserts the two-sided property (Stale strictly past
// the threshold, never Stale at or under it) against those literal
// offsets, and separately requires the sweep to have actually observed
// both a Stale and a Deliver case. A mutation that deletes the Stale
// branch, inverts its comparison, or collapses the range this sweep
// covers is caught by the offsets themselves, not by a formula shared
// with the code under test.
func TestI15_TriggerOverdueExpiresNeverFires(t *testing.T) {
	loc := time.FixedZone("UTC+3", 3*60*60)
	now := time.Date(2026, 8, 10, 14, 0, 0, 0, loc)

	if prospection.InQuietHours(now) {
		t.Fatalf("test fixture is broken: now (%v) must be outside quiet hours, so this sweep "+
			"tests staleness alone — the Defer gate's own property is "+
			"TestTriggerVerdict_NeverStaleAtQuietHoursEnd (internal/core/prospection/staleness_test.go)",
			now)
	}

	threshold := time.Duration(prospection.TriggerStalenessHours) * time.Hour

	var staleSeen, deliverSeen int
	for halfHours := 0; halfHours <= 94; halfHours++ { // 0 .. 47h, 30-minute steps
		fireAt := now.Add(-time.Duration(halfHours) * 30 * time.Minute)
		if prospection.InQuietHours(fireAt) {
			// Quiet hours interacting with staleness is a different
			// property, already proven by
			// TestTriggerVerdict_NeverStaleAtQuietHoursEnd; skipping keeps
			// this sweep about I15 alone.
			continue
		}

		overdue := now.Sub(fireAt)
		got := prospection.TriggerVerdict(fireAt, now)

		switch {
		case overdue > threshold:
			if got != prospection.VerdictStale {
				t.Fatalf("fireAt %v is %v overdue (threshold %v): TriggerVerdict = %v, want "+
					"VerdictStale — ADR-0009 requires a trigger past its staleness window to "+
					"expire, never fire", fireAt, overdue, threshold, got)
			}
			staleSeen++
		default:
			if got == prospection.VerdictStale {
				t.Fatalf("fireAt %v is %v overdue (threshold %v, at or under it): TriggerVerdict "+
					"= VerdictStale, want VerdictDeliver — a trigger inside its staleness window "+
					"must still be delivered", fireAt, overdue, threshold)
			}
			if got == prospection.VerdictDeliver {
				deliverSeen++
			}
		}

		for _, schemaStatus := range i15SchemaStatuses {
			if got == schemaStatus {
				t.Fatalf("TriggerVerdict(%v, %v) = %q, a status the schema itself owns — core "+
					"must speak Verdict's own neutral vocabulary, never brain's mapped status "+
					"string (design §3.3)", fireAt, now, got)
			}
		}
	}

	if staleSeen == 0 {
		t.Fatalf("sweep never produced a VerdictStale case — the swept range does not cross "+
			"TriggerStalenessHours (%dh); this guard would pass even if the Stale branch were "+
			"deleted entirely", prospection.TriggerStalenessHours)
	}
	if deliverSeen == 0 {
		t.Fatalf("sweep never produced a VerdictDeliver case — this guard would pass even if " +
			"TriggerVerdict always returned VerdictStale")
	}
}

// TestI15_OverdueTriggerExpiresAndNeverFiresThroughAScan is I15's
// behavioural half: the pure sweep above proves core says "stale", this
// proves brain acts on it.
//
// It sweeps the window rather than sampling it. Sampling at one overdue
// instant would pass against an implementation that expired only at the
// boundary, or only far past it; a sweep fails at every instant an
// implementation gets wrong, which is what makes the failure point at the
// shape of the bug rather than at one unlucky offset. The offsets are
// multiples of prospection.TriggerStalenessHours, so a recalibration needs
// no edit here.
//
// **Two claims over two domains, because overdue does not always mean
// expired.** verdict evaluates quiet hours BEFORE staleness on purpose —
// an item is never declared stale during a window in which it was refused
// delivery — so a trigger that is hours overdue at 06:00 is deferred, not
// expired, and will expire once the window ends. Writing this as a single
// "every overdue instant expires" sweep would have asserted a bug into
// existence, and it is only because the sweep covered the whole range
// that the interaction showed up at all.
//
// So: outside quiet hours, an overdue trigger expires. At EVERY overdue
// instant, quiet hours or not, it never fires. The second claim is I15's
// own sentence and holds unconditionally; the first is I15 as I16 leaves
// it. prospection.InQuietHours is the gate the runner itself consults —
// naming it here is using the same documented rule, not reimplementing it.
func TestI15_OverdueTriggerExpiresAndNeverFiresThroughAScan(t *testing.T) {
	// A Wednesday noon.
	fireAt := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	staleness := time.Duration(prospection.TriggerStalenessHours) * time.Hour

	sweptOutsideQuietHours := 0

	for offset := staleness + time.Hour; offset <= 4*staleness; offset += time.Hour {
		now := fireAt.Add(offset)
		t.Run(offset.String()+" overdue", func(t *testing.T) {
			triggers := memrepo.NewTriggers()
			timers := memrepo.NewTimers()
			decisions := memrepo.NewDecisionLog()

			ctx := context.Background()
			at := fireAt
			if err := triggers.Create(ctx, ports.Trigger{
				ID:        "trg-overdue",
				Kind:      ports.TriggerKindTimeBased,
				FireAt:    &at,
				CreatedAt: fireAt.Add(-24 * time.Hour),
			}); err != nil {
				t.Fatalf("Create: %v", err)
			}

			svc := brain.NewCheckService(fixedClock{now: now}, triggers, timers, &counterIDs{}, decisions, nil, nil, nil)
			report, err := svc.Check(ctx, brain.CheckRequest{})
			if err != nil {
				t.Fatalf("Check: %v", err)
			}

			rows, err := decisions.Since(ctx, fireAt.Add(-time.Hour), -1)
			if err != nil {
				t.Fatalf("decisions.Since: %v", err)
			}

			// I15's own sentence, unconditional: never fired, at any
			// overdue instant, in or out of quiet hours.
			for _, row := range rows {
				if row.Action == ports.ActionCheckTimerFired {
					t.Fatalf("an overdue trigger produced %q — it must expire, never fire late", row.Action)
				}
			}
			if report.TimersFired != 0 {
				t.Fatalf("TimersFired = %d for a scan with no timers at all", report.TimersFired)
			}

			if prospection.InQuietHours(now) {
				// Deferred: nothing written, still armed, and next scan
				// outside the window will expire it by arithmetic alone.
				if report.TriggersExpired != 0 || len(rows) != 0 {
					t.Fatalf("inside quiet hours the scan expired %d trigger(s) and wrote %d row(s), want none — quiet hours are evaluated before staleness so an item is never declared stale during a window in which it was refused delivery",
						report.TriggersExpired, len(rows))
				}
				stillDue, err := triggers.Due(ctx, now)
				if err != nil {
					t.Fatalf("Due: %v", err)
				}
				if len(stillDue) != 1 {
					t.Fatalf("a deferred trigger left Due (%d rows) — it must stay armed and resurface next pass", len(stillDue))
				}
				return
			}

			sweptOutsideQuietHours++

			if report.TriggersExpired != 1 {
				t.Fatalf("TriggersExpired = %d, want 1", report.TriggersExpired)
			}
			if len(rows) != 1 {
				t.Fatalf("decision_log has %d rows, want exactly 1: %+v", len(rows), rows)
			}
			if rows[0].Action != ports.ActionCheckTriggerExpired {
				t.Fatalf("Action = %q, want %q", rows[0].Action, ports.ActionCheckTriggerExpired)
			}

			// It left Due, which for an armed-only read means it is no
			// longer armed. That it is expired and not fired is what the
			// row above says, and what L3 asserts against the column.
			stillDue, err := triggers.Due(ctx, now.Add(24*time.Hour))
			if err != nil {
				t.Fatalf("Due: %v", err)
			}
			if len(stillDue) != 0 {
				t.Fatalf("the trigger is still armed after the scan: %+v", stillDue)
			}
		})
	}

	if sweptOutsideQuietHours == 0 {
		t.Fatal("every swept instant fell inside quiet hours — the expiry half of this test checked nothing")
	}
}
