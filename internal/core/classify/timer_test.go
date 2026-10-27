package classify

import (
	"strings"
	"testing"
	"time"
)

// TestAllTimerRequests pins the vocabulary and its order, which the prompt
// renders verbatim.
func TestAllTimerRequests(t *testing.T) {
	want := []TimerRequest{TimerRequestNone, TimerRequestList, TimerRequestCancel}
	got := AllTimerRequests()
	if len(got) != len(want) {
		t.Fatalf("AllTimerRequests() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("AllTimerRequests() = %v, want %v", got, want)
		}
	}
	if string(TimerRequestNone) != "none" || string(TimerRequestList) != "list" || string(TimerRequestCancel) != "cancel" {
		t.Errorf("the wire words changed: %v", got)
	}
}

// TestDecode_TimerRequest reads what a message asks of the live timers
// (ADR-0051): a list, a cancel, or none. An unknown word degrades to absent.
func TestDecode_TimerRequest(t *testing.T) {
	for _, want := range AllTimerRequests() {
		c, _ := Decode(reminderPayload(`,"timer_request":"`+string(want)+`"`), testNow)
		if len(c.Degradations) != 0 || c.TimerRequest == nil || *c.TimerRequest != want {
			t.Errorf("timer_request %q = %v degraded %v; want it clean", want, c.TimerRequest, c.Degradations)
		}
	}
	c, _ := Decode(reminderPayload(`,"timer_request":"snooze"`), testNow)
	assertOnlyDegraded(t, c, "timer_request", ReasonUnknownEnum)
	if c.TimerRequest != nil {
		t.Errorf("an unknown timer_request decoded %q, want nil", *c.TimerRequest)
	}
	if c, _ := Decode(reminderPayload(`,"timer_request":null`), testNow); len(c.Degradations) != 0 || c.TimerRequest != nil {
		t.Errorf("a null timer_request = %v degraded %v; want absent and clean", c.TimerRequest, c.Degradations)
	}
}

// TestDecode_TimerTarget reads which listed timer a cancel names: a position
// the list showed, or unclear. A position past the list's bound is not read.
func TestDecode_TimerTarget(t *testing.T) {
	for _, tc := range []struct {
		raw  string
		want ReminderTarget
	}{{`1`, ReminderTarget{Position: 1}}, {`5`, ReminderTarget{Position: MaxReminderChoices}}, {`"unclear"`, ReminderTarget{Unclear: true}}} {
		c, _ := Decode(reminderPayload(`,"timer_target":`+tc.raw), testNow)
		if len(c.Degradations) != 0 || c.TimerTarget == nil || *c.TimerTarget != tc.want {
			t.Errorf("timer_target %s = %v degraded %v; want %+v clean", tc.raw, c.TimerTarget, c.Degradations, tc.want)
		}
	}
	for _, tc := range []struct {
		raw    string
		reason Reason
	}{{`0`, ReasonBadFormat}, {`6`, ReasonBadFormat}, {`"2"`, ReasonUnknownEnum}, {`true`, ReasonWrongType}} {
		c, _ := Decode(reminderPayload(`,"timer_target":`+tc.raw), testNow)
		assertOnlyDegraded(t, c, "timer_target", tc.reason)
		if c.TimerTarget != nil {
			t.Errorf("timer_target %s decoded %+v, want nil", tc.raw, *c.TimerTarget)
		}
	}
}

// TestRenderTimerList numbers the live timers as the prompt shows them: the
// instant in the prompt's own form, then the text, at most
// MaxReminderChoices, a text flattened to one line. The reply lists the same
// entries in the same order, so a position means the timer the user saw.
func TestRenderTimerList(t *testing.T) {
	art := time.FixedZone("ART", -3*60*60)
	pizza := "Sacar la pizza\ndel horno"
	var entries []TimerEntry
	for i := range MaxReminderChoices + 1 {
		entries = append(entries, TimerEntry{At: time.Date(2026, 10, 26, 10, 28+i, 0, 0, art)})
	}
	entries[0].Text = &pizza
	got := RenderTimerList(entries)
	lines := strings.Split(strings.TrimSuffix(got, "\n"), "\n")
	if len(lines) != MaxReminderChoices {
		t.Fatalf("RenderTimerList rendered %d lines, want %d:\n%s", len(lines), MaxReminderChoices, got)
	}
	if want := "1) 2026-10-26 10:28 (Monday) — Sacar la pizza del horno"; lines[0] != want {
		t.Errorf("line 1 = %q, want %q", lines[0], want)
	}
	if want := "2) 2026-10-26 10:29 (Monday)"; lines[1] != want {
		t.Errorf("a timer with no text = %q, want %q", lines[1], want)
	}
}

// TestBuildPrompt_ReadsWhatAMessageAsksOfTheTimers: "¿qué temporizadores
// tengo?" came back a recall over notes, because nothing told the model that
// timers are not memory, and "cancelá el del horno" had no field to say it in
// (ADR-0051). Every prompt defines both fields; the list of live timers is
// shown when there is one, and its absence is said rather than left to be
// noticed.
func TestBuildPrompt_ReadsWhatAMessageAsksOfTheTimers(t *testing.T) {
	art := time.FixedZone("ART", -3*60*60)
	now := time.Date(2026, 10, 26, 10, 10, 0, 0, art)
	pizza := "Sacar la pizza del horno"
	entries := []TimerEntry{{At: time.Date(2026, 10, 26, 10, 28, 0, 0, art), Text: &pizza}}

	for _, want := range []string{
		"timer_request one of: none | list | cancel",
		"Timers are kept apart from memory: a recall never finds one.",
		`list: the user asks which timers or alarms are set ("¿qué temporizadores tengo?", "what timers do I have?"). That asks about the timers, so it is not a recall.`,
		`cancel: the user asks to cancel or remove a timer that is set ("cancelá el del horno", "sacá la alarma de mamá").`,
		"none for everything else, a new timer included.",
		`timer_target for a cancel: the position, 1 to 5, of the timer listed below that the user means, by what it says or when it rings, or "unclear" when you cannot tell which. Never invent a position`,
	} {
		flat := strings.Join(strings.Fields(BuildPrompt("x", nil, now, 0.5, ReminderContext{})), " ")
		if !strings.Contains(flat, want) {
			t.Errorf("the prompt does not state %q", want)
		}
	}

	with := BuildPrompt("cancelá el del horno", nil, now, 0.5, ReminderContext{Timers: entries})
	if !strings.Contains(with, timersHeading+"\n"+RenderTimerList(entries)) {
		t.Errorf("the prompt does not list the live timers under %q:\n%s", timersHeading, with)
	}
	without := BuildPrompt("cancelá el del horno", nil, now, 0.5, ReminderContext{})
	if strings.Contains(without, timersHeading) || !strings.Contains(without, "  No timer is set.\n") {
		t.Errorf("with no live timer the prompt must say none is set and list nothing:\n%s", without)
	}
}
