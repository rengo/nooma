package channels

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/rengo/nooma/internal/brain"
	"github.com/rengo/nooma/internal/core/prospection"
)

// RenderReply turns what a capture did into what the person is told.
//
// It renders brain.CaptureResult and nothing else: no second
// classification, no second recall, no opinion about what the outcome
// meant. The capture already decided; this says so.
//
// The switch has no default clause on purpose. A default would answer a
// new outcome with something plausible and wrong, which is exactly the
// failure test/conformance/channel_reply_totality_test.go exists to catch
// — and it can only catch it if an unhandled outcome produces an empty
// string rather than a fallback sentence.
//
// The sentences are plain and short because they are read on a phone, in a
// chat, by someone who just typed one line.
func RenderReply(result brain.CaptureResult) string {
	switch result.Outcome {
	case brain.OutcomeStored:
		return "Noted."

	case brain.OutcomeArmed:
		if result.Armed == nil {
			return ""
		}
		// **About, not FireAt.** The reply names the appointment, not the
		// nudge. A dated event's firing is a lead time before it and can
		// be clamped to the capture instant, so "Reminder set for <now>"
		// is what a correct reading of "el viernes a las 9am" looked
		// like — twice read as a misparse by the person who wrote it.
		switch result.Armed.What {
		case prospection.ArmTimer:
			// A timer fires AT its subject: one instant, said once.
			return fmt.Sprintf("Timer set for %s.", localTime(result.Armed.FireAt))
		case prospection.ArmRecurring:
			return fmt.Sprintf("Recurring reminder set for %s.", localTime(result.Armed.About))
		default:
			return fmt.Sprintf("Noted for %s. I will remind you%s.",
				localTime(result.Armed.About), nudgeWhen(result.Armed))
		}

	case brain.OutcomeArmRefused:
		if result.ArmRefused == nil {
			return ""
		}
		// The reason travels verbatim. A refusal a person cannot act on
		// is a refusal they will send again.
		return "I did not set that: " + result.ArmRefused.Message

	case brain.OutcomeConversed:
		// The model's own sentence, verbatim. This is the one reply in
		// this switch not written here, and that is the point: it comes
		// back in the language the message was written in, which no fixed
		// string in a Go file can do (ADR-0021).
		if result.Reply == "" {
			// A documented state, not a fallback for an unknown outcome:
			// the chat task did not answer. Saying so is the honest
			// version of the silence — and it is still English, which is
			// exactly the surface ADR-0021 leaves open.
			return "I could not answer that just now."
		}
		return result.Reply

	case brain.OutcomeOutOfScope:
		return "That is not something I can do."

	case brain.OutcomeRecalled:
		return renderRecall(result)

	case brain.OutcomeCorrected:
		return "Corrected."

	case brain.OutcomeAsked:
		return "I need one more thing before I can change that — which one did you mean?"
	}
	return ""
}

// localTime renders an instant the way a person reads one. It is the
// instant's own zone, not the process's: the classification carried the
// user's offset in, and re-rendering it in the server's zone would tell
// someone in Buenos Aires about a reminder at a time nobody set.
// nudgeWhen says when the reminder itself arrives, and says it in
// relation to now rather than as a second date. Two absolute times in one
// sentence is what made the original unreadable: the reader has to work
// out which one is theirs.
func nudgeWhen(a *brain.Armed) string {
	if a.Immediate {
		return " right away"
	}
	gap := a.About.Sub(a.FireAt)
	switch {
	case gap <= 0:
		return " right away"
	case gap < 24*time.Hour:
		return " a few hours before"
	case gap < 48*time.Hour:
		return " the day before"
	default:
		return fmt.Sprintf(" %d days before", int(gap.Hours()/24))
	}
}

func localTime(t time.Time) string {
	return t.Format("Mon 2 Jan, 15:04")
}

// renderRecall lists what recall found, or says plainly that it found
// nothing. "No results" is an answer; an empty message is not.
func renderRecall(result brain.CaptureResult) string {
	if len(result.Recalled) == 0 {
		return "I could not find anything about that."
	}

	var b strings.Builder
	b.WriteString("Found " + strconv.Itoa(len(result.Recalled)))
	if len(result.Recalled) == 1 {
		b.WriteString(" thing:")
	} else {
		b.WriteString(" things:")
	}
	for _, u := range result.Recalled {
		b.WriteString("\n• " + u.Content)
	}
	return b.String()
}
