package classify

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"
)

// TimerRequest is what a message asks of the live timers (doc 02 §8,
// ADR-0051): to list them, to cancel one, or nothing. The model reads it in
// any language; the code lists or cancels.
type TimerRequest string

const (
	TimerRequestNone   TimerRequest = "none"
	TimerRequestList   TimerRequest = "list"
	TimerRequestCancel TimerRequest = "cancel"
)

// AllTimerRequests returns a fresh slice of the TimerRequest vocabulary.
func AllTimerRequests() []TimerRequest {
	return []TimerRequest{TimerRequestNone, TimerRequestList, TimerRequestCancel}
}

// TimerEntry is one live timer as the prompt lists it: when it rings and what
// it says, nil for a generic nudge.
type TimerEntry struct {
	At   time.Time
	Text *string
}

// RenderTimerList is the numbered list of live timers the prompt carries:
// "1) <date> <time> (<weekday>) — <text>", in the order given, at most
// MaxReminderChoices. The instant is in the form the rest of the prompt
// writes one, read in its own zone, so a cancel names a position the model
// can match against "el de las 10:28" as well as against "el del horno".
func RenderTimerList(entries []TimerEntry) string {
	var b strings.Builder
	for i, e := range entries {
		if i == MaxReminderChoices {
			break
		}
		b.WriteString(strconv.Itoa(i+1) + ") " + e.At.Format(localDateLayout+" "+localTimeLayout) + " (" + e.At.Weekday().String() + ")")
		if e.Text != nil {
			if text := clipLabel(oneLine(*e.Text)); text != "" {
				b.WriteString(" — " + text)
			}
		}
		b.WriteString("\n")
	}
	return b.String()
}

// timersHeading heads the numbered list of live timers in the prompt.
const timersHeading = "Timers set"

// writeTimers renders the timer fields (ADR-0051) and the live timers a
// cancel's position counts, or says there is none: an absent list asked the
// model to notice an absence.
func writeTimers(b *strings.Builder, timers []TimerEntry) {
	b.WriteString("Timers\n")
	b.WriteString("  Timers are kept apart from memory: a recall never finds one.\n")
	b.WriteString("  timer_request      one of: " + joinVocabulary(AllTimerRequests()) + "\n")
	b.WriteString("                     list: the user asks which timers or alarms are set (\"¿qué\n")
	b.WriteString("                     temporizadores tengo?\", \"what timers do I have?\"). That asks about\n")
	b.WriteString("                     the timers, so it is not a recall.\n")
	b.WriteString("                     cancel: the user asks to cancel or remove a timer that is set\n")
	b.WriteString("                     (\"cancelá el del horno\", \"sacá la alarma de mamá\").\n")
	b.WriteString("                     none for everything else, a new timer included.\n")
	b.WriteString("  timer_target       for a cancel: the position, 1 to " + strconv.Itoa(MaxReminderChoices) +
		", of the timer listed below that\n")
	b.WriteString("                     the user means, by what it says or when it rings, or \"" + ReminderTargetUnclear + "\"\n")
	b.WriteString("                     when you cannot tell which. Never invent a position\n")
	if len(timers) == 0 {
		b.WriteString("  No timer is set.\n\n")
		return
	}
	b.WriteString("\n" + timersHeading + "\n")
	b.WriteString(RenderTimerList(timers))
	b.WriteString("\n")
}

// assignTimerTarget reads timer_target: a position in the timer list the
// prompt carried, or the word unclear, as reminder_target is read.
func assignTimerTarget(raw json.RawMessage, c *Classification, _ time.Time) Reason {
	t, r := readTarget(raw)
	c.TimerTarget = t
	return r
}
