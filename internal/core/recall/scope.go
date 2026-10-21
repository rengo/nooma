package recall

import (
	"slices"
	"time"

	"github.com/rengo/nooma/internal/core/unit"
)

// RecallWindowLimit bounds how many units a recall's window lists, by their
// instant (ADR-0048): the answer is one chat message a person reads, as
// RecallTopK bounds each leg of a search. docs/02-cognitive-core.md §13.
const RecallWindowLimit = 20

// WindowOn names which instant of a unit a window reads.
type WindowOn string

const (
	// WindowTold reads when the unit was told: its created_at.
	WindowTold WindowOn = "told"
	// WindowWhen reads when the unit is: its event_at, else its due_at.
	WindowWhen WindowOn = "when"
)

// Window is a span of time a recall asks about, From inclusive and To
// exclusive. The model reads it; the code only compares instants with it
// (ADR-0033, ADR-0048).
type Window struct {
	On       WindowOn
	From, To time.Time
}

// Instant is the instant of u the window reads, nil when u has none: a when
// window holds no undated unit.
func (w Window) Instant(u unit.Unit) *time.Time {
	switch {
	case w.On == WindowTold:
		return &u.CreatedAt
	case u.EventAt != nil:
		return u.EventAt
	}
	return u.DueAt
}

// Holds reports whether u's instant is inside w.
func (w Window) Holds(u unit.Unit) bool {
	at := w.Instant(u)
	return at != nil && !at.Before(w.From) && at.Before(w.To)
}

// Precise is ADR-0048's topic precision: of ranked, the answering ranking
// the vector admitted, the ids a term matched lexically, in ranked's order —
// or ranked whole when a term matched none of them. A term the model wrote
// carries no function words, which is what kept the lexical leg from
// admitting on a raw message (ADR-0020); it still only narrows what the
// vector admitted, and a term nothing holds is not a reason to answer with
// nothing.
func Precise(ranked, matched []string) []string {
	var precise []string
	for _, id := range ranked {
		if slices.Contains(matched, id) {
			precise = append(precise, id)
		}
	}
	if len(precise) == 0 {
		return ranked
	}
	return precise
}
