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
		switch result.Armed.What {
		case prospection.ArmTimer:
			return fmt.Sprintf("Timer set for %s.", localTime(result.Armed.FireAt))
		case prospection.ArmRecurring:
			return fmt.Sprintf("Recurring reminder set — the next one is %s.", localTime(result.Armed.FireAt))
		default:
			return fmt.Sprintf("Reminder set for %s.", localTime(result.Armed.FireAt))
		}

	case brain.OutcomeArmRefused:
		if result.ArmRefused == nil {
			return ""
		}
		// The reason travels verbatim. A refusal a person cannot act on
		// is a refusal they will send again.
		return "I did not set that: " + result.ArmRefused.Message

	case brain.OutcomeDiscarded:
		return "Nothing to keep there."

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
