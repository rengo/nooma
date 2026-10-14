// Package conformance — see test/conformance/doc.go for the package contract.
package conformance

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/core/prospection"
	"github.com/rengo/nooma/internal/ports"
)

// armed is unitID's armed reminders, by fire_at.
func (v *i37Vault) armed(unitID string) []ports.DueTrigger {
	v.t.Helper()
	got, err := v.triggers.ArmedForUnit(context.Background(), unitID)
	if err != nil {
		v.t.Fatal(err)
	}
	return got
}

// modern shapes a row as a capture since ADR-0029 arms it: it names its event
// and the lead it serves.
func modern(about time.Time, leadMinutes int) func(*ports.Trigger) {
	return func(tr *ports.Trigger) {
		tr.Payload.About, tr.Payload.LeadMinutes, tr.Payload.LeadDays = about, leadMinutes, 0
		tr.Payload.UserLeadsMinutes = []int{leadMinutes}
	}
}

func (v *i37Vault) reasons(action ports.DecisionAction) []string {
	var out []string
	for _, r := range v.rows(action) {
		out = append(out, r.Reason)
	}
	return out
}

// TestI37_SupersededRowIsHeldNotRetired: a correction moved the event ahead,
// so the reminder about the old date is never sent. It is not lapsed either:
// the unit still needs the reminders of its new date, and only the re-plan step
// arms them before it retires the old row. Until then the row is held untouched
// and the pass writes nothing.
func TestI37_SupersededRowIsHeldNotRetired(t *testing.T) {
	v := newI37Vault(t)
	corrected := i37Day(16, 11, 0)
	v.event("u", &corrected)
	v.fired("old", "u", "dentist", i37Day(14, 8, 0), modern(i37Day(15, 11, 0), 27*60))

	report := v.check(false)

	if all, _ := v.log.Since(context.Background(), time.Time{}, -1); len(all) != 0 {
		t.Errorf("the pass wrote %+v, want nothing", all)
	}
	if !v.undelivered()["old"] || report.DigestCarried != 0 || report.TriggersLapsed != 0 || len(v.ch.texts) != 0 {
		t.Errorf("report = %+v (sent %d, undelivered %v); want the superseded row held", report, len(v.ch.texts), v.undelivered())
	}
}

// TestI37_BirthdayWaitingForTheDigestIsCarried: a recurring reminder is waiting
// for the digest, not late. Fired eighteen hours before it, it is carried.
func TestI37_BirthdayWaitingForTheDigestIsCarried(t *testing.T) {
	v := newI37Vault(t)
	yearly := prospection.RuleYearly
	v.event("u", i37At(1, 12))
	v.fired("b", "u", "Leo's birthday", i37Day(13, 16, 0), func(tr *ports.Trigger) {
		tr.RecurrenceRule, tr.RecurrenceAnchor = &yearly, &prospection.Anchor{Month: time.October, Day: 21}
	})

	report := v.check(false)

	if report.DigestCarried != 1 || report.TriggersLapsed != 0 || len(v.ch.texts) != 1 || !strings.Contains(v.ch.texts[0], "Leo") {
		t.Errorf("report = %+v (sent %q); want the birthday carried", report, v.ch.texts)
	}
}

// TestI37_LegacyRowSupersededByNewerArming (D8): a row that names no event
// cannot be compared with its unit, but a unit that has since armed a reminder
// of its own has moved on from the legacy one. Though it is not late it is not
// sent, and it is held, not lapsed, for the re-plan step; the newer reminder is
// untouched.
func TestI37_LegacyRowSupersededByNewerArming(t *testing.T) {
	v := newI37Vault(t)
	event := i37Day(14, 20, 0)
	v.event("u", &event)
	v.fired("legacy", "u", "dentist", i37Day(14, 9, 0), nil)
	v.row("l2", "u", "dentist", i37Day(14, 18, 0), modern(event, 120))

	report := v.check(false)

	if report.TriggersLapsed != 0 || report.DigestCarried != 0 || len(v.ch.texts) != 0 || !v.undelivered()["legacy"] {
		t.Errorf("report = %+v (sent %d, undelivered %v); want the legacy row held", report, len(v.ch.texts), v.undelivered())
	}
	if got := v.armed("u"); len(got) != 1 || got[0].ID != "l2" {
		t.Errorf("armed = %+v; want l2 untouched", got)
	}
}

// TestI37_CannotDeliverExpiresAsToday: an overdue armed reminder expires as it
// always has, on a pass with no channel as on one with.
func TestI37_CannotDeliverExpiresAsToday(t *testing.T) {
	for _, silent := range []bool{true, false} {
		v := newI37Vault(t)
		v.silent = silent
		event := i37Day(14, 20, 0)
		v.event("u", &event)
		v.row("a24", "u", "dentist", i37Day(13, 20, 0), modern(event, 24*60))

		report := v.check(false)

		if got := v.armed("u"); len(got) != 0 || report.TriggersExpired != 1 {
			t.Errorf("silent=%v: armed = %+v, report %+v; want it expired", silent, got, report)
		}
	}
}
