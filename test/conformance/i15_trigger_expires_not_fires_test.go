// Package conformance — see test/conformance/doc.go for the package contract.
package conformance

import (
	"testing"
	"time"

	"github.com/rengo/nooma/internal/core/prospection"
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
