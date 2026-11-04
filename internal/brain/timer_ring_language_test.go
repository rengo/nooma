package brain

import (
	"context"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/core/unit"
	"github.com/rengo/nooma/test/support/memrepo"
)

// newestSpeaks is a unit repo whose newest unit was captured in language.
func newestSpeaks(t *testing.T, language string) *memrepo.Units {
	t.Helper()
	units := memrepo.NewUnits()
	at := ringFireAt.Add(-time.Hour)
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
// digest, and English when none is known (ADR-0032 point 5).
func TestTimerFollowsTheNewestUnitsLanguage(t *testing.T) {
	for _, c := range []struct{ name, language, want string }{
		{"spanish", "es", "Es la hora: pan."},
		{"english", "en", "Time's up: pan."},
		{"none known", "", "Time's up: pan."},
	} {
		t.Run(c.name, func(t *testing.T) {
			r := checkRunner{ids: &countingIDs{}, log: &recordingLog{}, units: newestSpeaks(t, c.language)}
			if got := r.renderTimer(context.Background(), dueTimer("t", "pan", ringFireAt), ringFireAt); got != c.want {
				t.Errorf("ring = %q, want %q", got, c.want)
			}
		})
	}
}
