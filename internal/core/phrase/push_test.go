package phrase

import (
	"testing"
	"time"
)

// TestPush pins every push of warm-replies design §6, exact text, both
// languages: the item's words and its day and time in one sentence, with no
// article guessed before the words (ADR-0064).
func TestPush(t *testing.T) {
	oct := func(day, hour, minute int) time.Time { return at(time.October, day, hour, minute) }
	for _, tc := range []struct {
		name         string
		now          time.Time
		esIt, enIt   Item
		esOut, enOut string
	}{
		{"recurring, a day alone", oct(29, 9, 0),
			Item{Text: "Tomar las pastillas", When: &Moment{At: oct(29, 0, 0), DayOnly: true}},
			Item{Text: "Take the pills", When: &Moment{At: oct(29, 0, 0), DayOnly: true}},
			"Acordate: tomar las pastillas, hoy.", "Don't forget: Take the pills, today."},
		{"event, the same day", oct(20, 9, 0),
			Item{Text: "Turno con el oculista", When: &Moment{At: oct(20, 11, 0)}},
			Item{Text: "Appointment with the optician", When: &Moment{At: oct(20, 11, 0)}},
			"Acordate: turno con el oculista, hoy a las 11:00.", "Don't forget: Appointment with the optician, today at 11:00."},
		{"event, the day before", now,
			Item{Text: "Cena con Ana", When: &Moment{At: at(11, 5, 21, 0)}},
			Item{Text: "Dinner with Ana", When: &Moment{At: at(11, 5, 21, 0)}},
			"Acordate: cena con Ana, mañana a las 21:00.", "Don't forget: Dinner with Ana, tomorrow at 21:00."},
		{"event, a named date", now,
			Item{Text: "Cena con Ana", When: &Moment{At: at(11, 13, 21, 0)}},
			Item{Text: "Dinner with Ana", When: &Moment{At: at(11, 13, 21, 0)}},
			"Acordate: cena con Ana, el viernes 13 de noviembre a las 21:00.", "Don't forget: Dinner with Ana, on Friday 13 November at 21:00."},
		{"no instant, trailing period dropped", now,
			Item{Text: "Llamar a mamá."},
			Item{Text: "Call mom."},
			"Acordate: llamar a mamá.", "Don't forget: Call mom."},
	} {
		if got := es.Push(tc.esIt, tc.now); got != tc.esOut {
			t.Errorf("%s: ES Push = %q, want %q", tc.name, got, tc.esOut)
		}
		if got := en.Push(tc.enIt, tc.now); got != tc.enOut {
			t.Errorf("%s: EN Push = %q, want %q", tc.name, got, tc.enOut)
		}
	}
}

// TestPush_LateNote: the apology follows the sentence, in its own words.
func TestPush_LateNote(t *testing.T) {
	n := at(time.October, 20, 9, 0)
	it := Item{Text: "Turno con el oculista", When: &Moment{At: at(time.October, 20, 11, 0)}}
	if got, want := es.Delayed(es.Push(it, n)), "Acordate: turno con el oculista, hoy a las 11:00. Perdón, te llega más tarde de lo previsto."; got != want {
		t.Errorf("ES late push = %q, want %q", got, want)
	}
	it.Text = "Appointment with the optician"
	if got, want := en.Delayed(en.Push(it, n)), "Don't forget: Appointment with the optician, today at 11:00. Sorry, this is later than intended."; got != want {
		t.Errorf("EN late push = %q, want %q", got, want)
	}
}

// TestRing: a ringing timer says its words in a fixed sentence; the late note
// is a word of the set (ADR-0064 point 7).
func TestRing(t *testing.T) {
	if got, want := es.Ring("Sacar la ropa."), "Es la hora: sacar la ropa."; got != want {
		t.Errorf("ES Ring = %q, want %q", got, want)
	}
	if got, want := en.Ring("Take the clothes out"), "Time's up: Take the clothes out."; got != want {
		t.Errorf("EN Ring = %q, want %q", got, want)
	}
	if got, want := es.TimerLate, "Perdón, te llega más tarde de lo que pediste."; got != want {
		t.Errorf("ES TimerLate = %q, want %q", got, want)
	}
	if got, want := en.TimerLate, "Sorry, this is later than you asked for."; got != want {
		t.Errorf("EN TimerLate = %q, want %q", got, want)
	}
}

// TestNamesOf: each text loses its own first word, so an ordinary first word is
// never evidence of a name (ADR-0064 point 3).
func TestNamesOf(t *testing.T) {
	if got, want := NamesOf("Pedro cumple", "Regalo para Pedro"), "cumple para Pedro"; got != want {
		t.Errorf("NamesOf = %q, want %q", got, want)
	}
	if got := NamesOf("", "Turno"); got != " " {
		t.Errorf("NamesOf with single words = %q, want a bare separator", got)
	}
}

// TestPush_RingAndRecall_NoSecondTerminator: words that end in ?, ! or a clipped
// ellipsis are not followed by the frame's period.
func TestPush_RingAndRecall_NoSecondTerminator(t *testing.T) {
	for _, tc := range []struct{ words, esPush, esRing, esRecall string }{
		{"¿Llamaste a mamá?", "Acordate: ¿llamaste a mamá?", "Es la hora: ¿llamaste a mamá?", "Tenés anotado: ¿llamaste a mamá?"},
		{"¡Llamá a mamá!", "Acordate: ¡llamá a mamá!", "Es la hora: ¡llamá a mamá!", "Tenés anotado: ¡llamá a mamá!"},
		{"Llamar a mamá!", "Acordate: llamar a mamá!", "Es la hora: llamar a mamá!", "Tenés anotado: llamar a mamá!"},
		{"Llamar a mamá y a papá…", "Acordate: llamar a mamá y a papá…", "Es la hora: llamar a mamá y a papá…", "Tenés anotado: llamar a mamá y a papá…"},
	} {
		it := Item{Text: tc.words}
		if got := es.Push(it, now); got != tc.esPush {
			t.Errorf("Push(%q) = %q, want %q", tc.words, got, tc.esPush)
		}
		if got := es.Ring(tc.words); got != tc.esRing {
			t.Errorf("Ring(%q) = %q, want %q", tc.words, got, tc.esRing)
		}
		if got := es.Recall([]Item{it}, now); got != tc.esRecall {
			t.Errorf("Recall(%q) = %q, want %q", tc.words, got, tc.esRecall)
		}
	}
	if got, want := en.Push(Item{Text: "Did you call mom?"}, now), "Don't forget: Did you call mom?"; got != want {
		t.Errorf("EN Push = %q, want %q", got, want)
	}
}
