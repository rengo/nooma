package brain

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/rengo/nooma/internal/core/prospection"
	"github.com/rengo/nooma/internal/ports"
)

// rephraseTask names this pipeline call for the provider fixtures, the
// same way "classify" does.
const rephraseTask = "timer_rephrase"

// genericNudge is what a timer with no action_text says. doc 02 §8: NULL
// action_text is "remind me in 15 min" with no object — the user asked to
// be interrupted and said nothing about why.
const genericNudge = "You asked me to remind you about something now."

// rephrasePrompt asks the model to word one timer at delivery.
//
// The prompt is here rather than in internal/core because it decides
// nothing: it is a string with a value interpolated, and core is where
// decisions live. classify's prompt lives in core because it carries the
// output schema every degradation rule is written against; this carries
// none — whatever comes back is one line of text, and if it comes back
// wrong the fallback is the user's own words.
func rephrasePrompt(actionText string, overdue time.Duration) string {
	var b strings.Builder
	b.WriteString("Reword this reminder as one short, warm sentence addressed to the person who set it. ")
	b.WriteString("Do not add information, do not ask a question, do not use their name. ")
	if prospection.DelayCaveat(overdue) {
		b.WriteString("It is being delivered later than they asked, so acknowledge the delay in the same sentence. ")
	}
	b.WriteString("Answer with the sentence and nothing else.\n\nReminder: ")
	b.WriteString(actionText)
	return b.String()
}

// timerDelivery is what one fired timer says, and what gets stored as its
// rendered_text.
type timerDelivery struct {
	// text is delivered to the user.
	text string
	// rendered is stored in timers.rendered_text, and is nil when nothing
	// was worded — a generic nudge, or a rephrasing that failed. The
	// column's "not yet worded" and "worded as itself" are one absence.
	rendered *string
}

// renderTimer words one timer at delivery.
//
// **action_text is never modified**, which is doc 02 §8's "the request is
// stored verbatim and only worded at delivery time". The rephrasing is a
// second column, and the user's own words survive whatever the model says
// about them — including the day the model is replaced.
//
// A rephrasing failure delivers the verbatim text rather than failing the
// delivery. The user asked for a reminder; a model outage is not a reason
// to withhold it, and it is the same posture capture takes toward an
// embedding failure (doc 02 §5's product rule).
func (r checkRunner) renderTimer(ctx context.Context, t ports.DueTimer, now time.Time) (timerDelivery, error) {
	overdue := now.Sub(t.FireAt)

	if t.ActionText == nil || *t.ActionText == "" {
		// Nothing to reword. No provider call at all: spending one on a
		// nudge with no content would be paying for a sentence the model
		// would have to invent.
		return timerDelivery{text: withCaveat(genericNudge, overdue)}, nil
	}
	verbatim := *t.ActionText

	if r.llm == nil {
		return timerDelivery{text: withCaveat(verbatim, overdue)}, nil
	}

	// **JSONOnly is deliberately not set.** This is the one LLM call in the
	// repository whose answer is read as a sentence rather than parsed, and
	// a free-text task forced into JSON mode answers in a shape nothing
	// downstream reads — the fallback below would then fire on every timer,
	// silently, since a JSON object is not an empty string.
	resp, err := r.llm.Complete(ctx, ports.LLMRequest{
		Prompt: rephrasePrompt(verbatim, overdue),
		Task:   rephraseTask,
	})
	if err != nil || strings.TrimSpace(resp.Text) == "" {
		// Degraded, and recorded: the glass box should show that the
		// user got their own words back rather than the wording that was
		// meant for them.
		if logErr := r.record(ctx, now, ports.ActionCheckTimerRephraseFailed,
			fmt.Sprintf("timer %q was delivered in the user's own words; the rephrasing did not come back: %v", t.ID, err),
			checkDetail{ID: t.ID, FireAt: t.FireAt.UTC().Format(time.RFC3339)}); logErr != nil {
			return timerDelivery{}, logErr
		}
		return timerDelivery{text: withCaveat(verbatim, overdue)}, nil
	}

	worded := strings.TrimSpace(resp.Text)
	return timerDelivery{text: worded, rendered: &worded}, nil
}

// withCaveat appends the delay note when prospection says the lateness is
// worth mentioning.
//
// Only the fallback paths use it: a successful rephrasing was already
// asked to acknowledge the delay in its own sentence, and appending a
// second mention would say it twice.
//
// DelayCaveat decides and this picks the words — doc 02 §7's own division
// of labour, and nothing here re-derives the threshold.
func withCaveat(text string, overdue time.Duration) string {
	if !prospection.DelayCaveat(overdue) {
		return text
	}
	return text + " (This is later than you asked for.)"
}
