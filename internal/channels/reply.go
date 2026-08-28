package channels

import (
	"fmt"

	"github.com/rengo/nooma/internal/brain"
	"github.com/rengo/nooma/internal/core/phrase"
	"github.com/rengo/nooma/internal/core/prospection"
)

// RenderReply turns what a capture did into what the person is told.
//
// It renders brain.CaptureResult and nothing else: no second
// classification, no second recall, no opinion about what the outcome
// meant. The capture already decided; this says so.
//
// **It chooses which sentence, never what the sentence says.** The words
// live in internal/core/phrase, keyed by the language the classification
// read off the message (ADR-0022). That split is what lets this switch
// stay a routing table — one line per outcome — while the vocabulary
// grows a language without this file changing at all. It also moves the
// sentences somewhere they can be tested at L1, which they could not be
// while they were string literals in a channel.
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
	say := phrase.For(result.Language)

	switch result.Outcome {
	case brain.OutcomeStored:
		return say.Noted

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
			return fmt.Sprintf(say.TimerSet, say.Time(result.Armed.FireAt))
		case prospection.ArmRecurring:
			return fmt.Sprintf(say.RecurringSet, say.Time(result.Armed.About))
		default:
			// Immediate is the plan's own fact, never re-derived from the
			// two instants: subtracting them gives a true duration and a
			// false promise — 41 hours reads as "the day before" for a
			// reminder that arrives at once.
			lead := say.Lead(result.Armed.Immediate, result.Armed.About.Sub(result.Armed.FireAt))
			return fmt.Sprintf(say.NotedFor, say.Time(result.Armed.About), lead)
		}

	case brain.OutcomeArmRefused:
		if result.ArmRefused == nil {
			return ""
		}
		// **The reason travels, and now it travels typed.** A refusal a
		// person cannot act on is a refusal they will send again, so the
		// reason still reaches them — but as prospection.Refusal rather
		// than as brain's English sentence. That sentence still exists
		// and still goes to the trail, where its audience is an auditor
		// and English is correct (ADR-0022).
		return fmt.Sprintf(say.NotScheduled, refusalReason(say, result.ArmRefused))

	case brain.OutcomeConversed:
		// The model's own sentence, verbatim. This is the one reply in
		// this switch Nooma did not write, and that is the point: it comes
		// back in the language the message was written in, which is the
		// same property the table above buys for everything else.
		if result.Reply == "" {
			// A documented state, not a fallback for an unknown outcome:
			// the chat task did not answer.
			return say.NoAnswer
		}
		return result.Reply

	case brain.OutcomeOutOfScope:
		return say.OutOfScope

	case brain.OutcomeRecalled:
		// "No results" is an answer; an empty message is not.
		if len(result.Recalled) == 0 {
			return say.NothingFound
		}
		contents := make([]string, len(result.Recalled))
		for i, u := range result.Recalled {
			contents[i] = u.Content
		}
		return say.List(contents)

	case brain.OutcomeCorrected:
		return say.Corrected

	case brain.OutcomeAsked:
		return say.AskWhichOne
	}
	return ""
}

// refusalReason renders why nothing was scheduled, in the person's
// language.
//
// An unrecognised refusal falls back to brain's own English sentence
// rather than to silence. That is a deliberate ordering of two bad
// outcomes: a reason in the wrong language is still actionable, and a
// refusal carrying no reason is one the person will simply send again.
func refusalReason(say phrase.Set, refused *brain.ArmRefused) string {
	switch refused.Why {
	case prospection.RefusalNoDate:
		return say.RefusalNoDate
	case prospection.RefusalAlreadyPast:
		return say.RefusalPast
	default:
		return refused.Message
	}
}
