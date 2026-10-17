// Package conformance — see test/conformance/doc.go for the package contract.
package conformance

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/core/prospection"
	"github.com/rengo/nooma/internal/ports"
	"github.com/rengo/nooma/test/support/memrepo"
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

// TestI37_SupersededHeldWhenItCannotBeReplanned: with no channel a superseded
// armed reminder is not sent and not expired: nothing could replace it, so it
// is left for the first pass that can.
func TestI37_SupersededHeldWhenItCannotBeReplanned(t *testing.T) {
	v := newI37Vault(t)
	v.silent = true
	corrected := i37Day(20, 9, 0)
	v.event("u", &corrected)
	v.row("old", "u", "dentist", i37Now.Add(-time.Minute), modern(i37Day(15, 11, 0), 0))

	report := v.check(false)

	if got := v.armed("u"); len(got) != 1 || report.TriggersExpired != 0 || report.TriggersFired != 0 {
		t.Errorf("armed = %+v, report %+v; want the row held armed", got, report)
	}
}

// TestI37_ASupersededRowWhoseNewEventPassesIsRetiredAsPassed: with no channel
// nothing can re-plan, so a moved reminder stays held. It must not stay due
// forever: once the unit's new event is past too, the row is retired as passed,
// armed or fired alike, the fired one by the next digest due (the next
// morning: past the noon cutoff, none is due that day).
func TestI37_ASupersededRowWhoseNewEventPassesIsRetiredAsPassed(t *testing.T) {
	moved := i37Day(14, 12, 0)
	for _, fired := range []bool{false, true} {
		v := newI37Vault(t)
		// A pass that cannot re-plan: no channel at all, or one with nowhere to
		// push to (the digest still lapses what has passed).
		v.silent, v.noConversation = !fired, fired
		v.event("u", &moved)
		if fired {
			v.fired("old", "u", "dentist", i37Day(14, 8, 0), modern(i37Day(15, 11, 0), 27*60))
		} else {
			v.row("old", "u", "dentist", i37Day(14, 8, 0), modern(i37Day(15, 11, 0), 27*60))
		}

		for _, at := range []time.Time{i37Day(14, 10, 0), i37Day(14, 10, 5), i37Day(14, 10, 10)} {
			if r := v.checkAt(at, false); r.TriggersExpired+r.TriggersLapsed != 0 {
				t.Fatalf("fired=%v: retired at %v while the new event is ahead: %+v", fired, at, r)
			}
		}
		report := v.checkAt(i37Day(15, prospection.DigestHour, 5), false)

		if report.TriggersExpired+report.TriggersLapsed != 1 || len(v.armed("u")) != 0 || v.undelivered()["old"] {
			t.Errorf("fired=%v: report %+v, armed %d, undelivered %v; want the row retired once the new event passed", fired, report, len(v.armed("u")), v.undelivered())
		}
		for _, action := range []ports.DecisionAction{ports.ActionCheckTriggerExpired, ports.ActionCheckTriggerLapsed} {
			for _, reason := range v.reasons(action) {
				if reason != string(prospection.LapsePassed) {
					t.Errorf("fired=%v: %s reason %q, want passed", fired, action, reason)
				}
			}
		}
	}
}

// TestI37_TodayHoldsSupersededAndLegacyRows: Today mirrors the digest's skip.
// A reminder whose event moved, and a legacy one its unit has since replaced,
// are neither counted nor listed, and Today writes nothing.
func TestI37_TodayHoldsSupersededAndLegacyRows(t *testing.T) {
	v := newI37Vault(t)
	event := i37Day(14, 20, 0)
	recent := i37Now.Add(-time.Hour)
	v.event("u-ok", &event)
	v.fired("ok", "u-ok", "dentist", recent, modern(event, 120))
	v.event("u-moved", &event)
	v.fired("moved", "u-moved", "flight", recent, modern(i37Day(15, 20, 0), 120))
	v.event("u-legacy", &event)
	v.fired("legacy", "u-legacy", "visa", recent, nil)
	v.row("newer", "u-legacy", "visa", i37Day(14, 18, 0), modern(event, 120))

	svc := newI37Today(v)
	today, err := svc.Today(context.Background())
	if err != nil {
		t.Fatalf("Today: %v", err)
	}

	if today.Status.Undelivered != 1 || len(today.Digest.Items) != 1 || today.Digest.Items[0].TriggerID != "ok" {
		t.Errorf("Today = undelivered %d, items %+v; want only the healthy reminder", today.Status.Undelivered, today.Digest.Items)
	}
	if len(v.undelivered()) != 3 {
		t.Errorf("undelivered after viewing = %v, want all three untouched", v.undelivered())
	}
}

// TestI37_ADryRunWritesNothingForAnUnreadableUnit: a damaged unit row is
// recorded once by the real pass, and a dry run, which writes nothing, records
// nothing.
func TestI37_ADryRunWritesNothingForAnUnreadableUnit(t *testing.T) {
	seed := func() *i37Vault {
		v := newI37Vault(t)
		v.units = i37BrokenUnits{memrepo.NewUnits()}
		v.row("modern", "u1", "dentist", i37Now.Add(-time.Minute), func(tr *ports.Trigger) {
			tr.Payload.About, tr.Payload.LeadMinutes = i37Day(15, 9, 0), 0
		})
		return v
	}
	dry, wet := seed(), seed()

	dry.check(true)
	wet.check(false)

	if got := dry.rows(ports.ActionCheckDeliveryFailed); len(got) != 0 {
		t.Errorf("a dry run recorded %+v", got)
	}
	if got := wet.rows(ports.ActionCheckDeliveryFailed); len(got) != 1 {
		t.Errorf("the real pass recorded %+v, want one", got)
	}
}

// TestI37_AnArmedRecurringReminderDoesNotSupersedeALegacyRow: the unit's
// replacement must be a one-shot reminder. A birthday the unit also carries is
// recurring and says nothing about the legacy one-shot, which is still pushed.
func TestI37_AnArmedRecurringReminderDoesNotSupersedeALegacyRow(t *testing.T) {
	v := newI37Vault(t)
	yearly := prospection.RuleYearly
	event := i37Day(14, 20, 0)
	v.event("u", &event)
	v.fired("legacy", "u", "dentist", i37Now.Add(-time.Hour), nil)
	v.row("birthday", "u", "birthday", i37Day(20, 12, 0), func(tr *ports.Trigger) {
		tr.RecurrenceRule, tr.RecurrenceAnchor = &yearly, &prospection.Anchor{Month: time.October, Day: 27}
	})

	report := v.check(false)

	if report.TriggersDelivered != 1 || report.TriggersLapsed != 0 || len(v.ch.texts) != 1 || !strings.Contains(v.ch.texts[0], "dentist") {
		t.Errorf("report = %+v (sent %q); want the legacy reminder pushed", report, v.ch.texts)
	}
}
