package brain

import (
	"context"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/core/classify"
	"github.com/rengo/nooma/internal/core/phrase"
	"github.com/rengo/nooma/internal/core/prospection"
	"github.com/rengo/nooma/internal/ports"
)

// forbiddenLLM fails the test on any call: a ringing timer is built by code
// (ADR-0064 point 7), so nothing may ask a provider to word it.
type forbiddenLLM struct{ t *testing.T }

func (l forbiddenLLM) Complete(_ context.Context, req ports.LLMRequest) (ports.LLMResponse, error) {
	l.t.Errorf("a timer ring called the provider (task %q); no model words a reply that carries a fact", req.Task)
	return ports.LLMResponse{}, nil
}

func dueTimer(id, text string, fireAt time.Time) ports.DueTimer {
	t := ports.DueTimer{ID: id, FireAt: fireAt}
	if text != "" {
		t.ActionText = &text
	}
	return t
}

var ringFireAt = time.Date(2026, 8, 5, 10, 0, 0, 0, time.UTC)

// TestRenderTimer pins the ring of warm-replies design §6, both languages, on
// time and late, with a provider that fails the test on any call. Nothing is
// worded, so rendered_text is never written.
func TestRenderTimer(t *testing.T) {
	late := ringFireAt.Add(time.Duration(prospection.DelayCaveatMinutes) * time.Minute)
	for _, c := range []struct {
		language                                string
		words, ring, ringLate, nudge, nudgeLate string
	}{
		{"es", "sacar la ropa", "Es la hora: sacar la ropa.",
			"Es la hora: sacar la ropa. Perdón, te llega más tarde de lo que pediste.",
			"Me pediste que te recordara algo ahora.",
			"Me pediste que te recordara algo ahora. Perdón, te llega más tarde de lo que pediste."},
		{"en", "Take the clothes out", "Time's up: Take the clothes out.",
			"Time's up: Take the clothes out. Sorry, this is later than you asked for.",
			"You asked me to remind you about something now.",
			"You asked me to remind you about something now. Sorry, this is later than you asked for."},
	} {
		t.Run(c.language, func(t *testing.T) {
			ctx := context.Background()
			r := checkRunner{ids: &countingIDs{}, log: &recordingLog{}, units: newestSpeaks(t, c.language), llm: forbiddenLLM{t}}
			for _, tc := range []struct {
				name, words string
				now         time.Time
				want        string
			}{
				{"words", c.words, ringFireAt.Add(time.Minute), c.ring},
				{"words, late", c.words, late, c.ringLate},
				{"no words", "", ringFireAt, c.nudge},
				{"no words, late", "", late, c.nudgeLate},
			} {
				got := r.renderTimer(ctx, dueTimer("t", tc.words, ringFireAt), tc.now)
				if got != tc.want {
					t.Errorf("%s: ring = %q, want %q", tc.name, got, tc.want)
				}
			}
		})
	}
}

// TestRenderTimer_TheDelayNoteIsSweptAcrossItsBoundary is R4.2: swept rather
// than sampled, and the boundary itself asserted. m3a's F6 made DelayCaveat
// inclusive on purpose: a note that was not strictly needed costs a clause of
// politeness, a missing one on a genuinely late delivery is what ADR-0009
// exists to prevent.
func TestRenderTimer_TheDelayNoteIsSweptAcrossItsBoundary(t *testing.T) {
	boundary := time.Duration(prospection.DelayCaveatMinutes) * time.Minute
	note := phrase.For(classify.Fallback()).TimerLate
	for offset := -5 * time.Minute; offset <= 5*time.Minute; offset += time.Minute {
		overdue := boundary + offset
		if overdue < 0 {
			continue
		}
		r := checkRunner{ids: &countingIDs{}, log: &recordingLog{}}
		got := r.renderTimer(context.Background(), dueTimer("t", "take the bread out", ringFireAt), ringFireAt.Add(overdue))
		want := "Time's up: take the bread out."
		if overdue >= boundary {
			want += " " + note
		}
		if got != want {
			t.Fatalf("overdue %s: ring = %q, want %q", overdue, got, want)
		}
	}
}
