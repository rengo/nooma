// Package conformance — see test/conformance/doc.go for the package contract.
package conformance

import (
	"testing"
	"time"

	"github.com/rengo/nooma/internal/core/classify"
	"github.com/rengo/nooma/internal/core/voice"
)

// TestI56_APhrasedTextStatesOnlyItsFacts is I56 (docs/06-harness.md §4), over
// docs/02-cognitive-core.md §5 step 2 and docs/adr/0061-the-model-words-what-the-code-found.md.
//
// The model never writes a fact: it writes slots ({1}, {1.when}, {count}) and
// the code fills them with the item's own words and day. So the client's own
// exchange of 2026-10-30 comes back worded, and the reviewer's probes on #435 —
// swapped items, a count that is a day, an invented item borrowing a day — cannot
// be written at all: each one sends the draft, today's fixed text.
func TestI56_APhrasedTextStatesOnlyItsFacts(t *testing.T) {
	art := time.FixedZone("ART", -3*3600)
	turno := time.Date(2026, 11, 26, 8, 30, 0, 0, art)
	facts := voice.Facts{
		Intent: voice.RecallFound, Language: classify.LanguageES, Now: time.Date(2026, 10, 30, 10, 0, 0, 0, art),
		Message: "cuando tengo turno al gastroenterologo?",
		Items:   []voice.Item{{Text: "Turno para estudios con el gastroenterólogo", Type: "event", At: &turno}},
	}
	const draft = "Encontré 1 cosa:\n• Turno para estudios con el gastroenterólogo — jue 26 nov, 08:30"
	got, why := voice.Check(facts, draft, "Tenés el {1} {1.when}.")
	if want := "Tenés el turno para estudios con el gastroenterólogo el jueves 26 de noviembre a las 8:30."; got != want || why != voice.Accepted {
		t.Errorf("the client's question was answered %q (%q), want %q", got, why, want)
	}
}

func TestI56_NoWordingCanGiveAnItemAnotherItemsDay(t *testing.T) {
	art := time.FixedZone("ART", -3*3600)
	dentista := time.Date(2026, 11, 3, 10, 0, 0, 0, art)
	reunion := time.Date(2026, 11, 5, 15, 30, 0, 0, art)
	facts := voice.Facts{
		Intent: voice.RecallFound, Language: classify.LanguageES, Now: time.Date(2026, 11, 2, 9, 0, 0, 0, art),
		Message: "¿qué tengo esta semana?",
		Items: []voice.Item{
			{Text: "Dentista", Type: "event", At: &dentista},
			{Text: "Reunión con Ana", Type: "event", At: &reunion},
		},
	}
	const draft = "Encontré 2 cosas:\n• Dentista — mar 3 nov, 10:00\n• Reunión con Ana — jue 5 nov, 15:30"

	got, why := voice.Check(facts, draft, "{1.when} tenés el {1}, y {2.when} la {2}.")
	if want := "Mañana a las 10:00 tenés el dentista, y el jueves 5 de noviembre a las 15:30 la reunión con Ana."; got != want || why != voice.Accepted {
		t.Errorf("an honest wording was answered %q (%q), want %q", got, why, want)
	}

	for _, wrong := range []string{
		"Tenés el {1} {2.when} y la {2} {1.when}.",                                     // swapped
		"Tenés 3 cosas: el {1} {1.when} y la {2} {2.when}.",                            // a count that is a day
		"Tenés tres cosas: el {1} {1.when} y la {2} {2.when}.",                         // a count in words
		"El {1} {1.when} y la {2} {2.when}, y el gimnasio {2.when}.",                   // a borrowed day
		"El {1} {1.when} y la {2} el jueves a las 15:30; también el gimnasio el 5/11.", // a day written
		"{1.when} el {1}. {2.when} vas al gimnasio y después a la {2}.",                // an invented item, no number
	} {
		if got, why := voice.Check(facts, draft, wrong); got != draft || why == voice.Accepted {
			t.Errorf("%q was sent (%q, %q); the draft must be", wrong, got, why)
		}
	}
}
