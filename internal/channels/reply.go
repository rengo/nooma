package channels

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/rengo/nooma/internal/brain"
	"github.com/rengo/nooma/internal/core/correction"
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
		// Asked and not armed: the unit is kept, and the reply says why no
		// reminder was set (ADR-0031 point 7).
		if result.ArmRefused != nil {
			return fmt.Sprintf(say.NotedNotSet, refusalReason(say, result.ArmRefused))
		}
		// A dated task arms nothing: the digest of its day names it (doc 02 §7).
		if result.DueInDigest != nil {
			return fmt.Sprintf(say.NotedForDigest, say.Date(*result.DueInDigest))
		}
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
			// When it rings every time, not its next ring alone (ADR-0031 point 7).
			return say.Every(result.Armed.Rule, result.Armed.Anchor, result.Armed.About)
		default:
			fires := append([]time.Time{result.Armed.FireAt}, result.Armed.Later...)
			if a := result.Armed.About; result.Armed.DayOnly || (a.Hour() == 0 && a.Minute() == 0 && a.Second() == 0 && a.Nanosecond() == 0) {
				// Every ring is named: an event on a day alone is also
				// reminded the day before (ADR-0029, ADR-0031 point 2). An
				// unasked one is a local midnight, how a bare date is stored
				// (ADR-0029 point 2): its date alone is named, not "00:00".
				return fmt.Sprintf(say.NotedFor, say.Date(result.Armed.About), say.DayLeads(result.Armed.Immediate, result.At, result.Armed.About, fires...))
			}
			// Immediate is the plan's own fact, never re-derived from the
			// two instants: subtracting them gives a true duration and a
			// false promise — 41 hours reads as "the day before" for a
			// reminder that arrives at once.
			// Every reminder is named: a timed event has two (ADR-0029).
			lead := say.Leads(result.Armed.Immediate, result.At, result.Armed.About, fires...)
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
		c := result.Correction
		if c == nil {
			return say.Corrected
		}
		if c.Unchanged {
			return say.Unchanged
		}
		// What changed, named (I40): a bare "Corrected." left the user unable
		// to tell what Nooma understood, and once claimed a change of reminders
		// that never happened. Wording alone keeps the short confirmation.
		var said []string
		switch {
		case c.MovedTo != nil && c.MovedToDayOnly:
			said = append(said, fmt.Sprintf(say.Moved, say.Date(*c.MovedTo)))
		case c.MovedTo != nil:
			said = append(said, fmt.Sprintf(say.Moved, say.Time(*c.MovedTo)))
		}
		switch {
		case !slices.Contains(c.Fields, correction.FieldReminderLeads):
		case len(c.Reminders) == 0:
			said = append(said, say.NoReminderLeft)
		default:
			said = append(said, fmt.Sprintf(say.RemindYou, say.ExactLeads(c.About, c.Reminders...)))
		}
		if len(said) == 0 {
			return say.Corrected
		}
		return strings.Join(said, " ")

	case brain.OutcomeAsked:
		// Leads that could not be set are never answered as set (I40).
		if c := result.Correction; c != nil && c.Why == brain.AskLeadsNotSet {
			return fmt.Sprintf(say.LeadsNotSet, prospection.MaxEventLeadDays)
		}
		// The model's own question about a follow-up, in the user's words.
		if result.Reply != "" {
			return result.Reply
		}
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
	case prospection.RefusalTooLate:
		return say.RefusalTooLate
	default:
		return refused.Message
	}
}
