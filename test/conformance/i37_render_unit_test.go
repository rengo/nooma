// Package conformance — see test/conformance/doc.go for the package contract.
package conformance

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

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

// sameUnitPair stores one event with two reminders of it that are pushed
// together: the 180-minute lead fired earlier and never reached the user, the
// 61-minute lead is due now. It returns the pass's instant.
func (v *i37Vault) sameUnitPair() time.Time {
	v.t.Helper()
	event := i37Day(14, 20, 0)
	v.event("u", &event)
	v.fired("l180", "u", "dentist", i37Day(14, 17, 0), modern(event, 180))
	v.row("l61", "u", "dentist", i37Day(14, 18, 59), modern(event, 61))
	return i37Day(14, 19, 0)
}

// TestI37_SurfaceFailureOfAGroupIsNotResentByTheProcess: one message carried
// both reminders of a unit and the vault could not record the first as
// delivered. The sibling is still marked, the process never sends the first
// again, and a restart, which has forgotten, sends it once more.
func TestI37_SurfaceFailureOfAGroupIsNotResentByTheProcess(t *testing.T) {
	v := newI37Vault(t)
	at := v.sameUnitPair()
	v.repo = newSurfaceFails(v.triggers, errors.New("disk full"), 1, "l180")

	if _, err := v.tryCheckAt(at, false); err == nil {
		t.Fatal("the pass that sent reminders it could not record did not fail")
	}
	if got := v.undelivered(); len(got) != 1 || !got["l180"] {
		t.Errorf("undelivered = %v, want only the row whose Surface failed: its sibling went out in the same message", got)
	}
	for i := 1; i <= 3; i++ {
		if _, err := v.tryCheckAt(at.Add(time.Duration(i)*5*time.Minute), false); err != nil {
			t.Fatalf("pass %d: %v", i, err)
		}
	}
	if got := pushes(v.ch, "dentist"); got != 1 {
		t.Fatalf("the user got the reminder %d times in one process, want 1: %q", got, v.ch.texts)
	}

	v.svc = nil
	v.checkAt(at.Add(20*time.Minute), false)
	if got := pushes(v.ch, "dentist"); got != 2 || len(v.undelivered()) != 0 {
		t.Errorf("pushes %d, undelivered %v; want a restart to send the unrecorded one once more", got, v.undelivered())
	}
}

// TestI37_EverySurfaceFailureOfAGroupIsRemembered: when the vault could record
// neither row of a message, the process remembers both and resends neither.
func TestI37_EverySurfaceFailureOfAGroupIsRemembered(t *testing.T) {
	v := newI37Vault(t)
	at := v.sameUnitPair()
	v.repo = newSurfaceFails(v.triggers, errors.New("disk full"), 1, "l180", "l61")

	if _, err := v.tryCheckAt(at, false); err == nil {
		t.Fatal("the pass that sent reminders it could not record did not fail")
	}
	for i := 1; i <= 3; i++ {
		if _, err := v.tryCheckAt(at.Add(time.Duration(i)*5*time.Minute), false); err != nil {
			t.Fatalf("pass %d: %v", i, err)
		}
	}
	if got := pushes(v.ch, "dentist"); got != 1 {
		t.Errorf("the user got the reminder %d times in one process, want 1: %q", got, v.ch.texts)
	}
}

// TestI37_OutageWritesOneRowPerReminderOfAGroup: a send that fails for a
// message carrying two reminders writes a check.delivery.failed row for each.
func TestI37_OutageWritesOneRowPerReminderOfAGroup(t *testing.T) {
	v := newI37Vault(t)
	at := v.sameUnitPair()
	v.ch.failIf = func(string) bool { return true }

	v.checkAt(at, false)
	v.checkAt(at.Add(5*time.Minute), false)

	if got := v.failedRows(); got["l180"] != 1 || got["l61"] != 1 {
		t.Errorf("failure rows = %v, want one for each of the two reminders and none for the second pass", got)
	}
}

// failingUndelivered is a trigger store whose Undelivered read fails.
type failingUndelivered struct{ ports.TriggerRepo }

func (failingUndelivered) Undelivered(context.Context) ([]ports.DueTrigger, error) {
	return nil, errors.New("the store is unavailable")
}

// TestI37_AnAbortedPassStillSendsWhatItFired: the re-plan step fails after the
// due loop fired a reminder. The push goes out all the same, because left
// behind the error it would go stale, and the pass still returns the error.
func TestI37_AnAbortedPassStillSendsWhatItFired(t *testing.T) {
	v := newI37Vault(t)
	v.dueNow("r", "u", "dentist")
	v.repo = failingUndelivered{v.triggers}

	if _, err := v.tryCheckAt(i37Now, false); err == nil {
		t.Fatal("the pass did not return the re-plan error")
	}
	if got := pushes(v.ch, "dentist"); got != 1 || v.undelivered()["r"] {
		t.Errorf("pushes %d, undelivered %v; want the fired reminder sent and recorded despite the abort", got, v.undelivered())
	}
	if _, err := v.tryCheckAt(i37Now.Add(5*time.Minute), false); err == nil || pushes(v.ch, "dentist") != 1 {
		t.Errorf("a later aborted pass resent it (%d pushes) or hid the error: %v", pushes(v.ch, "dentist"), err)
	}
}

// TestDigestSaysOneLinePerUnitNotPerText: two distinct units that read the same
// are two lines, and the header counts the lines shown.
func TestDigestSaysOneLinePerUnitNotPerText(t *testing.T) {
	v := newI37Vault(t)
	yearly := prospection.RuleYearly
	for _, id := range []string{"a", "b"} {
		v.event("u"+id, i37At(1, 12))
		v.fired(id, "u"+id, "Call mom", i37Day(13, 16, 0), func(tr *ports.Trigger) {
			tr.RecurrenceRule, tr.RecurrenceAnchor = &yearly, &prospection.Anchor{Month: time.October, Day: 21}
		})
	}

	report := v.check(false)

	want := "Here are 2 things for today:\n• Call mom — Wed 21 Oct\n• Call mom — Wed 21 Oct"
	if report.DigestCarried != 2 || len(v.ch.texts) != 1 || v.ch.texts[0] != want {
		t.Errorf("report %+v, sent %q, want %q", report, v.ch.texts, want)
	}
}

// TestALongUnitCannotWedgeDelivery: a unit's text is unbounded and Telegram
// refuses a message over 4096 characters, which would fail every pass. The
// reminder says a bounded, rune-safe prefix and ends with an ellipsis.
func TestALongUnitCannotWedgeDelivery(t *testing.T) {
	long := strings.Repeat("é", 5000)
	v := newI37Vault(t)
	event := i37Day(15, 11, 0)
	v.event("u", &event)
	v.row("r", "u", "dentist", i37Now.Add(-time.Minute), modern(event, 1500))
	v.say("u", long)

	v.check(false)

	if len(v.ch.texts) != 1 {
		t.Fatalf("sent %d messages, want 1", len(v.ch.texts))
	}
	got := v.ch.texts[0]
	if n := len([]rune(got)); n > 600 || !utf8.ValidString(got) || !strings.Contains(got, "é… —") {
		t.Errorf("a %d-rune unit was sent as %d runes (valid %v): %.40q...", len([]rune(long)), n, utf8.ValidString(got), got)
	}
}

// TestRenderSaysTheLocalDayNotUTC: the clock's zone decides the day said. An
// event at 01:30 UTC is 22:30 the evening before at UTC-3.
func TestRenderSaysTheLocalDayNotUTC(t *testing.T) {
	zone := time.FixedZone("UTC-3", -3*3600)
	v := newI37Vault(t)
	event := i37Day(15, 1, 30)
	v.event("u", &event)
	now := i37Day(14, 13, 0).In(zone) // 10:00 local
	v.row("r", "u", "late dinner", now.Add(-time.Minute), modern(event, 930))

	v.checkAt(now, false)

	want := "Reminder: late dinner — Wed 14 Oct, 22:30"
	if len(v.ch.texts) != 1 || v.ch.texts[0] != want {
		t.Errorf("sent %q, want exactly [%q]", v.ch.texts, want)
	}
}

// TestSeveralUnreadableRowsSayEachTextOnce: a unit that cannot be read makes the
// message the texts the reminders were armed with, one line for each distinct
// one, in the order they came due. The second call is armed with the first
// call's text again and is said once.
func TestSeveralUnreadableRowsSayEachTextOnce(t *testing.T) {
	v := newI37Vault(t)
	event := i37Day(14, 20, 0)
	v.fired("a", "gone", "dentist, first call", i37Day(14, 17, 0), modern(event, 180))
	v.row("b", "gone", "dentist, first call", i37Day(14, 18, 58), modern(event, 62))
	v.row("c", "gone", "dentist, second call", i37Day(14, 18, 59), modern(event, 61))

	v.checkAt(i37Day(14, 19, 0), false)

	want := "dentist, first call\ndentist, second call"
	if len(v.ch.texts) != 1 || v.ch.texts[0] != want {
		t.Errorf("sent %q, want exactly [%q]", v.ch.texts, want)
	}
}

// TestBlankUnitTextFallsBackToAction: a unit whose text is blank says nothing a
// reminder could frame, so the reminder says what it was armed with.
func TestBlankUnitTextFallsBackToAction(t *testing.T) {
	v := newI37Vault(t)
	event := i37Day(15, 11, 0)
	v.event("u", &event)
	v.row("r", "u", "renew the passport", i37Now.Add(-time.Minute), modern(event, 1500))
	v.say("u", "   ")

	v.check(false)

	if len(v.ch.texts) != 1 || v.ch.texts[0] != "renew the passport" {
		t.Errorf("sent %q, want the frozen text alone", v.ch.texts)
	}
}

// TestALongTextCannotWedgeAnyPath: the bound holds on the paths that do not go
// through a readable unit's push: the text a reminder was armed with, and a
// digest line (which keeps its date after the cut).
func TestALongTextCannotWedgeAnyPath(t *testing.T) {
	long := strings.Repeat("é", 5000)

	armed := newI37Vault(t)
	armed.row("r", "gone", long, i37Now.Add(-time.Minute), modern(i37Day(15, 11, 0), 1500))
	armed.check(false)

	digest := newI37Vault(t)
	yearly := prospection.RuleYearly
	digest.event("bd", i37At(1, 12))
	digest.fired("b", "bd", "x", i37Day(13, 16, 0), func(tr *ports.Trigger) {
		tr.RecurrenceRule, tr.RecurrenceAnchor = &yearly, &prospection.Anchor{Month: time.October, Day: 21}
	})
	digest.say("bd", long)
	digest.check(false)

	gone := newI37Vault(t)
	gone.fired("g", "nobody", long, i37Day(13, 16, 0), func(tr *ports.Trigger) {
		tr.RecurrenceRule, tr.RecurrenceAnchor = &yearly, &prospection.Anchor{Month: time.October, Day: 21}
	})
	gone.check(false)

	for name, v := range map[string]*i37Vault{"armed text": armed, "digest line": digest, "digest armed text": gone} {
		if len(v.ch.texts) != 1 || len([]rune(v.ch.texts[0])) > 600 || !utf8.ValidString(v.ch.texts[0]) {
			t.Errorf("%s: sent %d message(s), first of %d runes; want one bounded message", name, len(v.ch.texts), len([]rune(strings.Join(v.ch.texts, ""))))
		}
	}
	if len(digest.ch.texts) == 1 && !strings.HasSuffix(digest.ch.texts[0], "… — Wed 21 Oct") {
		t.Errorf("digest line = %.20q...%q, want the ellipsis then the date", digest.ch.texts[0], digest.ch.texts[0][len(digest.ch.texts[0])-20:])
	}
}

// TestTheClipNeverSplitsAMultiByteCharacter: the cut is on runes. A 3-byte
// character is used because 500 is not a multiple of 3, so a byte cut lands inside
// one; with a 2-byte character it would land between two and pass.
func TestTheClipNeverSplitsAMultiByteCharacter(t *testing.T) {
	v := newI37Vault(t)
	event := i37Day(15, 11, 0)
	v.event("u", &event)
	v.row("r", "u", "dentist", i37Now.Add(-time.Minute), modern(event, 1500))
	v.say("u", strings.Repeat("\u20ac", 600))

	v.check(false)

	want := "Reminder: " + strings.Repeat("\u20ac", 500) + "\u2026 \u2014 Thu 15 Oct, 11:00"
	if len(v.ch.texts) != 1 || v.ch.texts[0] != want {
		got := ""
		if len(v.ch.texts) == 1 {
			got = v.ch.texts[0]
		}
		t.Errorf("sent %d message(s) of %d bytes, valid %v; want exactly %d bytes: 500 whole runes then an ellipsis", len(v.ch.texts), len(got), utf8.ValidString(got), len(want))
	}
}

// TestI37_AnAbortedPassReportsTheFlushFailureToo: the pass aborts and the push it
// still sends cannot be recorded; both failures reach the caller, so the one that
// caused the abort does not hide that a message went out unrecorded.
func TestI37_AnAbortedPassReportsTheFlushFailureToo(t *testing.T) {
	v := newI37Vault(t)
	v.dueNow("r", "u", "dentist")
	v.repo = failingUndelivered{newSurfaceFails(v.triggers, errors.New("disk full"), 1, "r")}

	_, err := v.tryCheckAt(i37Now, false)

	if err == nil || !strings.Contains(err.Error(), "the store is unavailable") || !strings.Contains(err.Error(), "disk full") {
		t.Errorf("error = %v, want both the abort's cause and the unrecorded send", err)
	}
}
