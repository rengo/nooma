package phrase

import "testing"

// TestDigestLineOf pins the digest's bullet lines of warm-replies design §6:
// the item's words as stored (the bullet keeps the capital), then its day and
// time, a due day as due (ADR-0064).
func TestDigestLineOf(t *testing.T) {
	now := at(11, 4, 8, 0)
	day := func(d int) *Moment { return &Moment{At: at(11, d, 0, 0), DayOnly: true} }
	for _, tc := range []struct {
		name         string
		es, en       Item
		esOut, enOut string
	}{
		{"an event with a time",
			Item{Text: "Turno con el oculista", When: &Moment{At: at(11, 4, 11, 0)}},
			Item{Text: "Appointment with the optician", When: &Moment{At: at(11, 4, 11, 0)}},
			"Turno con el oculista, hoy a las 11:00", "Appointment with the optician, today at 11:00"},
		{"a day alone",
			Item{Text: "Tomar las pastillas", When: day(4)},
			Item{Text: "Take the pills", When: day(4)},
			"Tomar las pastillas, hoy", "Take the pills, today"},
		{"a task due today",
			Item{Text: "Renovar el pasaporte", When: &Moment{At: at(11, 4, 0, 0), DayOnly: true, Due: true}},
			Item{Text: "Renew the passport", When: &Moment{At: at(11, 4, 0, 0), DayOnly: true, Due: true}},
			"Renovar el pasaporte, para hoy", "Renew the passport, due today"},
		{"a task due and past",
			Item{Text: "Renovar el pasaporte", When: &Moment{At: at(11, 3, 0, 0), DayOnly: true, Due: true}},
			Item{Text: "Renew the passport", When: &Moment{At: at(11, 3, 0, 0), DayOnly: true, Due: true}},
			"Renovar el pasaporte, era para ayer", "Renew the passport, was due yesterday"},
		{"no instant, the words as stored",
			Item{Text: "Llamar a mamá."}, Item{Text: "Call mom."},
			"Llamar a mamá.", "Call mom."},
	} {
		if got := es.DigestLineOf(tc.es, now); got != tc.esOut {
			t.Errorf("%s: ES = %q, want %q", tc.name, got, tc.esOut)
		}
		if got := en.DigestLineOf(tc.en, now); got != tc.enOut {
			t.Errorf("%s: EN = %q, want %q", tc.name, got, tc.enOut)
		}
	}
}

// TestDigestFrames pins the digest's greeting, header, "one more question" and
// "and N more" of design §6, exact text, both languages.
func TestDigestFrames(t *testing.T) {
	for got, want := range map[string]string{
		es.Morning:         "Buen día.",
		es.DigestHeader(1): "Buen día. Tenés una cosa:",
		es.DigestHeader(3): "Buen día. Tenés 3 cosas:",
		es.OneMore:         "Una pregunta más:\n",
		es.More(2):         "Y 2 más que te cuento mañana.",
		es.Question("Cena con Ana", "Reservar restaurante"): "Relacioné «Cena con Ana» con «Reservar restaurante». ¿Tienen que ver?",
		en.Morning:         "Good morning.",
		en.DigestHeader(1): "Good morning. You have one thing:",
		en.DigestHeader(3): "Good morning. You have 3 things:",
		en.OneMore:         "One more question:\n",
		en.More(2):         "And 2 more I'll tell you about tomorrow.",
		en.Question("Dinner with Ana", "Book a restaurant"): "I linked “Dinner with Ana” with “Book a restaurant”. Are they related?",
	} {
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	}
}
