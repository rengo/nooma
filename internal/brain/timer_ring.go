package brain

import (
	"context"
	"strings"
	"time"

	"github.com/rengo/nooma/internal/core/phrase"
	"github.com/rengo/nooma/internal/core/prospection"
	"github.com/rengo/nooma/internal/ports"
)

// A timer with no action_text says the set's TimerNudge. doc 02 §8: NULL
// action_text is "remind me in 15 min" with no object: the user asked to
// be interrupted and said nothing about why.

// renderTimer is what one ringing timer says: a fixed sentence around its
// words, or the nudge when it has none, with the lateness note when
// prospection says the ring is late (ADR-0064 point 7). No model words it, so
// timers.rendered_text is never written.
//
// action_text is never modified: it is stored verbatim and only said here.
func (r checkRunner) renderTimer(ctx context.Context, t ports.DueTimer, now time.Time) string {
	// A timer is not a unit and has no language of its own: it speaks the newest
	// unit's, like the digest, and English when none is known (ADR-0032 point 5).
	set := r.setFor(ctx, "")
	text := set.TimerNudge
	if t.ActionText != nil && strings.TrimSpace(*t.ActionText) != "" {
		text = set.Ring(clip(*t.ActionText))
	}
	return withCaveat(text, now.Sub(t.FireAt), set)
}

// withCaveat appends the delay note when prospection says the lateness is
// worth mentioning. DelayCaveat decides and this picks the words: doc 02 §7's
// own division of labour, and nothing here re-derives the threshold.
func withCaveat(text string, overdue time.Duration, set phrase.Set) string {
	if !prospection.DelayCaveat(overdue) {
		return text
	}
	return text + " " + set.TimerLate
}
