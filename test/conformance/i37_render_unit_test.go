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

// say sets what the unit now says, as a correction of its text would.
func (v *i37Vault) say(unitID, content string) {
	v.t.Helper()
	if err := v.units.UpdateContent(context.Background(), unitID, content, i37Now); err != nil {
		v.t.Fatalf("say %s: %v", unitID, err)
	}
}

// TestRenderUsesCurrentContentAndInstant: a reminder says the unit as it is when
// it is sent (ADR-0032 point 4), not the text frozen when it was armed. The
// client's flight was corrected to 08:00 and the reminder still said 09:00.
func TestRenderUsesCurrentContentAndInstant(t *testing.T) {
	v := newI37Vault(t)
	event := i37Day(15, 8, 0)
	v.event("flight", &event)
	v.row("r", "flight", "Flight to Lisbon, 09:00", i37Now.Add(-time.Minute), modern(event, 1319))
	v.say("flight", "Flight to Lisbon, 08:00")

	v.check(false)

	want := "Reminder: Flight to Lisbon, 08:00 — Thu 15 Oct, 08:00"
	if len(v.ch.texts) != 1 || v.ch.texts[0] != want {
		t.Errorf("sent %q, want exactly [%q]", v.ch.texts, want)
	}
}

// TestRenderUsesTheUnitsInstantWhenThePayloadNamesNone: a reminder armed before
// its payload named an event (a legacy at-once one) has no instant of its own;
// it says the unit's.
func TestRenderUsesTheUnitsInstantWhenThePayloadNamesNone(t *testing.T) {
	v := newI37Vault(t)
	event := i37Day(15, 11, 0)
	v.event("dentist", &event)
	v.row("r", "dentist", "Dentist", i37Now.Add(-time.Minute), nil)

	v.check(false)

	want := "Reminder: Dentist \u2014 Thu 15 Oct, 11:00"
	if len(v.ch.texts) != 1 || v.ch.texts[0] != want {
		t.Errorf("sent %q, want exactly [%q]", v.ch.texts, want)
	}
}

// TestRenderUsesCurrentContentInTheDigest: a recurring reminder waits for the
// digest, and the digest says the unit's words and the occurrence, not the
// frozen text and not a clock time.
func TestRenderUsesCurrentContentInTheDigest(t *testing.T) {
	v := newI37Vault(t)
	yearly := prospection.RuleYearly
	v.event("bd", i37At(1, 12))
	v.fired("b", "bd", "Leo's birthday (old)", i37Day(13, 16, 0), func(tr *ports.Trigger) {
		tr.RecurrenceRule, tr.RecurrenceAnchor = &yearly, &prospection.Anchor{Month: time.October, Day: 21}
	})
	v.say("bd", "Leo's birthday")

	report := v.check(false)

	want := "Here is 1 thing for today:\n• Leo's birthday — Wed 21 Oct"
	if report.DigestCarried != 1 || len(v.ch.texts) != 1 || v.ch.texts[0] != want {
		t.Errorf("report %+v, sent %q, want the digest %q", report, v.ch.texts, want)
	}
}

// TestDateOnlyEventRendersWithoutATime: an event that is a bare date has no
// hour, and saying "00:00" would be a time nobody set.
func TestDateOnlyEventRendersWithoutATime(t *testing.T) {
	v := newI37Vault(t)
	event := i37Day(16, 0, 0)
	v.event("passport", &event)
	v.row("r", "passport", "Renew the passport", i37Now.Add(-time.Minute), modern(event, 14*60))

	v.check(false)

	want := "Reminder: Renew the passport — Fri 16 Oct"
	if len(v.ch.texts) != 1 || v.ch.texts[0] != want {
		t.Errorf("sent %q, want exactly [%q]", v.ch.texts, want)
	}
}

// TestOneMessagePerUnitArmedSiblingUntouched: an event with leads of 180, 60 and
// 15 minutes. The 180 push failed and is retried in the same pass in which the
// 60 fires: the user gets one message, both rows are marked delivered, and the
// 15-minute reminder still waits armed for its own moment (carried over from
// #351's review).
func TestOneMessagePerUnitArmedSiblingUntouched(t *testing.T) {
	v := newI37Vault(t)
	event := i37Day(14, 20, 0)
	v.event("u", &event)
	v.fired("l180", "u", "dentist", i37Day(14, 17, 0), modern(event, 180))
	v.row("l60", "u", "dentist", i37Day(14, 18, 59), modern(event, 61))
	v.row("l15", "u", "dentist", i37Day(14, 19, 45), modern(event, 15))

	report := v.checkAt(i37Day(14, 19, 0), false)

	want := "Reminder: dentist — Wed 14 Oct, 20:00"
	if len(v.ch.texts) != 1 || v.ch.texts[0] != want {
		t.Fatalf("sent %q, want exactly one message [%q]", v.ch.texts, want)
	}
	if left := v.undelivered(); len(left) != 0 || report.TriggersDelivered != 2 {
		t.Errorf("undelivered %v, delivered %d; want both rows marked delivered by the one message", left, report.TriggersDelivered)
	}
	if got := v.armed("u"); len(got) != 1 || got[0].ID != "l15" {
		t.Errorf("armed = %+v, want only the 15-minute sibling, untouched", got)
	}
	if got := len(v.rows(ports.ActionCheckTriggerDelivered)); got != 2 {
		t.Errorf("%d check.trigger.delivered rows, want one per reminder", got)
	}
}

// TestMissingUnitFallsBackToAction: a reminder whose unit cannot be read says
// what it was armed with, which beats saying nothing.
func TestMissingUnitFallsBackToAction(t *testing.T) {
	v := newI37Vault(t)
	v.row("r", "gone", "renew the passport", i37Now.Add(-time.Minute), modern(i37Day(15, 11, 0), 1500))

	v.check(false)

	if len(v.ch.texts) != 1 || v.ch.texts[0] != "renew the passport" {
		t.Errorf("sent %q, want the frozen text alone", v.ch.texts)
	}
	if strings.Contains(strings.Join(v.ch.texts, ""), "Reminder:") {
		t.Errorf("a reminder with no unit was framed from nothing: %q", v.ch.texts)
	}
}

// TestDigestSaysOneLinePerUnit: two recurring reminders of one unit are one
// line and count as one thing, and both are marked delivered.
func TestDigestSaysOneLinePerUnit(t *testing.T) {
	v := newI37Vault(t)
	yearly := prospection.RuleYearly
	v.event("bd", i37At(1, 12))
	for _, id := range []string{"b1", "b2"} {
		v.fired(id, "bd", "Leo's birthday", i37Day(13, 16, 0), func(tr *ports.Trigger) {
			tr.RecurrenceRule, tr.RecurrenceAnchor = &yearly, &prospection.Anchor{Month: time.October, Day: 21}
		})
	}

	report := v.check(false)

	want := "Here is 1 thing for today:\n• Leo's birthday — Wed 21 Oct"
	if len(v.ch.texts) != 1 || v.ch.texts[0] != want || len(v.undelivered()) != 0 {
		t.Errorf("report %+v, sent %q, undelivered %v; want %q and both rows delivered", report, v.ch.texts, v.undelivered(), want)
	}
}
