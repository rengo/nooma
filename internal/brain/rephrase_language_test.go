package brain

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/core/prospection"
	"github.com/rengo/nooma/internal/core/unit"
	"github.com/rengo/nooma/test/support/memrepo"
)

// newestSpeaks is a unit repo whose newest unit was captured in language.
func newestSpeaks(t *testing.T, language string) *memrepo.Units {
	t.Helper()
	units := memrepo.NewUnits()
	at := rephraseFireAt.Add(-time.Hour)
	if err := units.Create(context.Background(), unit.Unit{
		ID: "newest", Type: unit.TypeEvent, Status: unit.StatusPool, Content: "x", Weight: 1, Language: language,
		LastTouchedAt: at, CreatedAt: at, UpdatedAt: at,
	}); err != nil {
		t.Fatalf("seed unit: %v", err)
	}
	return units
}

// TestTimerFollowsTheNewestUnitsLanguage: a timer is not a unit and has no
// language of its own, so what it says follows the newest unit's, like the
// digest, and English when none is known (ADR-0032 point 5): the generic nudge,
// the lateness caveat on the user's own words, and the language the model is
// asked to reword in.
func TestTimerFollowsTheNewestUnitsLanguage(t *testing.T) {
	late := rephraseFireAt.Add(time.Duration(prospection.DelayCaveatMinutes) * time.Minute)
	for _, c := range []struct {
		name, language, nudge, caveat, asked string
	}{
		{"spanish", "es", "Me pediste que te recordara algo ahora.", "(Esto llega más tarde de lo que pediste.)", "Spanish"},
		{"english", "en", "You asked me to remind you about something now.", "(This is later than you asked for.)", "English"},
		{"none known", "", "You asked me to remind you about something now.", "(This is later than you asked for.)", "English"},
	} {
		t.Run(c.name, func(t *testing.T) {
			ctx := context.Background()
			r := checkRunner{ids: &countingIDs{}, log: &recordingLog{}, units: newestSpeaks(t, c.language)}

			if got, err := r.renderTimer(ctx, dueTimer("t", "", rephraseFireAt), rephraseFireAt); err != nil || got.text != c.nudge {
				t.Errorf("generic nudge = %q, %v; want %q", got.text, err, c.nudge)
			}
			if got, err := r.renderTimer(ctx, dueTimer("t", "pan", rephraseFireAt), late); err != nil || got.text != "pan "+c.caveat {
				t.Errorf("late verbatim = %q, %v; want the words and %q", got.text, err, c.caveat)
			}

			llm := &scriptedLLM{text: "ok"}
			r.llm = llm
			if _, err := r.renderTimer(ctx, dueTimer("t", "pan", rephraseFireAt), late); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(llm.seen[0].Prompt, "in "+c.asked) {
				t.Errorf("the prompt does not ask for %s:\n%s", c.asked, llm.seen[0].Prompt)
			}
		})
	}
}
