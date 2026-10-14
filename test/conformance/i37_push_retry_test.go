// Package conformance — see test/conformance/doc.go for the package contract.
package conformance

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/core/prospection"
	"github.com/rengo/nooma/internal/ports"
	"github.com/rengo/nooma/test/support/memrepo"
)

// pushes counts the Send attempts that were a pushed reminder saying text, not
// a digest that happens to mention it.
func pushes(ch *i37Channel, text string) int {
	n := 0
	for _, a := range ch.attempts {
		if strings.Contains(a, text) {
			n++
		}
	}
	return n
}

// dueNow stores a reminder about an event at 20:00 that came due a minute ago.
func (v *i37Vault) dueNow(id, unitID, text string) {
	v.t.Helper()
	event := i37Day(14, 20, 0)
	v.event(unitID, &event)
	v.row(id, unitID, text, i37Now.Add(-time.Minute), modern(event, 605))
}

// TestI37_FailedPushRetriedOncePerPass: a push the transport refused stays
// undelivered, and each later pass tries it again, once. The pass that fired it
// does not try it a second time.
func TestI37_FailedPushRetriedOncePerPass(t *testing.T) {
	v := newI37Vault(t)
	v.dueNow("r", "u", "dentist")
	v.ch.failIf = func(string) bool { return true }

	v.checkAt(i37Now, false)
	if got := pushes(v.ch, "dentist"); got != 1 {
		t.Fatalf("the pass that fired the reminder pushed it %d times, want 1", got)
	}
	v.checkAt(i37Now.Add(5*time.Minute), false)
	if got := pushes(v.ch, "dentist"); got != 2 {
		t.Fatalf("after the next pass the reminder was pushed %d times, want one retry", got)
	}

	v.ch.failIf = nil
	v.checkAt(i37Now.Add(10*time.Minute), false)
	if v.undelivered()["r"] || pushes(v.ch, "dentist") != 3 {
		t.Fatalf("undelivered %v, pushes %d; want the third pass to deliver it", v.undelivered(), pushes(v.ch, "dentist"))
	}
	v.checkAt(i37Now.Add(15*time.Minute), false)
	if got := pushes(v.ch, "dentist"); got != 3 {
		t.Errorf("a delivered reminder was pushed again: %d pushes", got)
	}
}

// surfaceFails is a trigger store whose Surface of the named ids fails once,
// with err.
type surfaceFails struct {
	ports.TriggerRepo
	err  map[string]error
	left map[string]int
}

func newSurfaceFails(inner ports.TriggerRepo, err error, times int, ids ...string) *surfaceFails {
	s := &surfaceFails{TriggerRepo: inner, err: map[string]error{}, left: map[string]int{}}
	for _, id := range ids {
		s.err[id], s.left[id] = err, times
	}
	return s
}

func (s *surfaceFails) Surface(ctx context.Context, id string, at time.Time) error {
	if s.left[id] != 0 {
		s.left[id]--
		return s.err[id]
	}
	return s.TriggerRepo.Surface(ctx, id, at)
}

// TestI37_SurfaceFailureIsNotResentByTheProcess: the message went out and the
// row could not say so. The pass fails loudly, and the process never sends that
// reminder again, by push or by digest; only a restart, which has lost the
// memory, tries it once more.
func TestI37_SurfaceFailureIsNotResentByTheProcess(t *testing.T) {
	v := newI37Vault(t)
	v.dueNow("r", "u", "dentist")
	v.repo = newSurfaceFails(v.triggers, errors.New("disk full"), 1, "r")

	if _, err := v.tryCheckAt(i37Now, false); err == nil {
		t.Fatal("the pass that sent a reminder it could not record did not fail")
	}
	for i := 1; i <= 3; i++ {
		if _, err := v.tryCheckAt(i37Now.Add(time.Duration(i)*5*time.Minute), false); err != nil {
			t.Fatalf("pass %d: %v", i, err)
		}
	}
	mentions := 0
	for _, text := range v.ch.texts {
		if strings.Contains(text, "dentist") {
			mentions++
		}
	}
	if mentions != 1 {
		t.Fatalf("the user got the reminder %d times in one process, want 1: %q", mentions, v.ch.texts)
	}

	v.svc = nil
	v.checkAt(i37Now.Add(20*time.Minute), false)
	if pushes(v.ch, "dentist") != 2 || v.undelivered()["r"] {
		t.Errorf("pushes %d, undelivered %v; want a restart to send it once more and record it", pushes(v.ch, "dentist"), v.undelivered())
	}
}

// failedRows counts the check.delivery.failed rows that name each reminder.
func (v *i37Vault) failedRows() map[string]int {
	v.t.Helper()
	out := map[string]int{}
	for _, r := range v.rows(ports.ActionCheckDeliveryFailed) {
		out[r.ID]++
	}
	return out
}

// TestI37_OutageWritesOneRowPerReminder: a transport that stays down writes one
// check.delivery.failed row per reminder, not one per pass. A send that gets
// through ends the outage, so the next failure is a new one and is said again.
func TestI37_OutageWritesOneRowPerReminder(t *testing.T) {
	v := newI37Vault(t)
	v.dueNow("alpha", "ua", "alpha")
	v.dueNow("bravo", "ub", "bravo")
	v.ch.failIf = func(string) bool { return true }

	for i := 0; i < 3; i++ {
		v.checkAt(i37Now.Add(time.Duration(i)*5*time.Minute), false)
	}
	if got := v.failedRows(); got["alpha"] != 1 || got["bravo"] != 1 {
		t.Fatalf("three passes of an outage wrote %v, want one row per reminder", got)
	}

	v.ch.failIf = func(text string) bool { return strings.Contains(text, "alpha") }
	v.checkAt(i37Now.Add(15*time.Minute), false)
	v.checkAt(i37Now.Add(20*time.Minute), false)
	if got := v.failedRows(); got["alpha"] != 2 || got["bravo"] != 1 || v.undelivered()["bravo"] {
		t.Errorf("rows %v, undelivered %v; want bravo delivered and alpha's new outage said once", got, v.undelivered())
	}
}

// TestDueScanConcurrent_SurfaceConflictIsBenign: a Surface that finds the row
// already moved on lost a race with another pass. It is recorded and skipped,
// the reminders behind it are still delivered, and the pass does not fail.
func TestDueScanConcurrent_SurfaceConflictIsBenign(t *testing.T) {
	conflict := fmt.Errorf("surface: %w", ports.ErrTriggerStatusConflict)
	for _, retry := range []bool{false, true} {
		t.Run(fmt.Sprintf("retry=%v", retry), func(t *testing.T) {
			v := newI37Vault(t)
			event := i37Day(14, 20, 0)
			v.event("ua", &event)
			v.event("ub", &event)
			if retry {
				v.fired("a", "ua", "alpha", i37Now.Add(-10*time.Minute), modern(event, 605))
				v.fired("b", "ub", "bravo", i37Now.Add(-5*time.Minute), modern(event, 605))
			} else {
				v.row("a", "ua", "alpha", i37Now.Add(-10*time.Minute), modern(event, 605))
				v.row("b", "ub", "bravo", i37Now.Add(-5*time.Minute), modern(event, 605))
			}
			v.repo = newSurfaceFails(v.triggers, conflict, 1, "a")

			if _, err := v.tryCheckAt(i37Now, false); err != nil {
				t.Fatalf("a lost Surface race failed the pass: %v", err)
			}
			if v.undelivered()["b"] {
				t.Error("the reminder behind the conflict was not delivered")
			}
			if got := v.rows(ports.ActionCheckConflictSkipped); len(got) != 1 || got[0].ID != "a" {
				t.Errorf("check.conflict_skipped rows = %+v, want one naming a", got)
			}
		})
	}
}

// TestI37_DigestCarriesRecurringOnly: a one-shot reminder that is still
// undelivered belongs to the push path, which retries it; the digest carries
// only the recurring ones waiting for it.
func TestI37_DigestCarriesRecurringOnly(t *testing.T) {
	v := newI37Vault(t)
	v.dueNow("a", "u", "alpha")
	yearly := prospection.RuleYearly
	v.event("bd", i37At(1, 12))
	v.fired("b", "bd", "Leo's birthday", i37Day(13, 16, 0), func(tr *ports.Trigger) {
		tr.RecurrenceRule, tr.RecurrenceAnchor = &yearly, &prospection.Anchor{Month: time.October, Day: 21}
	})
	v.ch.failIf = func(text string) bool { return strings.Contains(text, "alpha") }

	report := v.check(false)

	if report.DigestCarried != 1 || len(v.ch.texts) != 1 || !strings.Contains(v.ch.texts[0], "Leo") || strings.Contains(v.ch.texts[0], "alpha") {
		t.Errorf("report = %+v, sent %q; want a digest of the birthday alone", report, v.ch.texts)
	}
	if got := v.undelivered(); !got["a"] || got["b"] {
		t.Errorf("undelivered = %v, want only the one-shot reminder still waiting for its retry", got)
	}
}

// TestI37_QuietHoursHoldARetryUntilQuietEnd: a push that failed before the night
// is not retried in it, and the first pass after the user's quiet end sends it
// once. The quiet end is 03:00 here, so the retry is still inside ADR-0009's
// window when it is let go.
func TestI37_QuietHoursHoldARetryUntilQuietEnd(t *testing.T) {
	v := newI37Vault(t)
	v.cfg = memrepo.NewConfig()
	quietEnd := "03:00"
	v.cfg.(*memrepo.Config).SeedConfig(t, ports.VaultConfig{QuietHoursEnd: &quietEnd})
	event := i37Day(15, 20, 0)
	v.event("u", &event)
	v.row("r", "u", "dentist", i37Day(14, 23, 50), modern(event, 1210))
	v.ch.failIf = func(string) bool { return true }

	v.checkAt(i37Day(14, 23, 55), false)
	v.ch.failIf = nil
	night := v.checkAt(i37Day(15, 1, 0), false)
	if got := pushes(v.ch, "dentist"); got != 1 || night.TriggersDelivered != 0 || !v.undelivered()["r"] {
		t.Fatalf("pushes %d, report %+v, undelivered %v; want nothing sent in the night", got, night, v.undelivered())
	}
	if got := v.failedRows()["r"]; got != 1 {
		t.Errorf("failure rows for r = %d, want the one from before the night", got)
	}

	v.checkAt(i37Day(15, 3, 5), false)
	v.checkAt(i37Day(15, 3, 10), false)
	if got := pushes(v.ch, "dentist"); got != 2 || v.undelivered()["r"] {
		t.Errorf("pushes %d, undelivered %v; want one push at quiet end and none after", got, v.undelivered())
	}
}

// TestI37_DryRunCountsARetryAndSendsNothing: a dry run reports the retry the
// real pass will make, writes nothing and sends nothing.
func TestI37_DryRunCountsARetryAndSendsNothing(t *testing.T) {
	seed := func(v *i37Vault) {
		event := i37Day(14, 20, 0)
		v.event("u", &event)
		v.fired("r", "u", "dentist", i37Day(14, 9, 0), modern(event, 660))
	}
	dry, real := newI37Vault(t), newI37Vault(t)
	seed(dry)
	seed(real)

	preview, report := dry.check(true), real.check(false)

	if preview != report || report.TriggersDelivered != 1 {
		t.Errorf("dry run reported %+v, the real pass %+v; want the same, with the retry delivered", preview, report)
	}
	if len(dry.ch.attempts) != 0 || !dry.undelivered()["r"] {
		t.Errorf("the dry run sent %q, undelivered %v; want it to touch nothing", dry.ch.attempts, dry.undelivered())
	}
}
