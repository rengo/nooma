package phrase

import "testing"

// TestItemOf: a unit with a title is as it was; with no title and an instant it
// is unnamed, so no reply says its content beside the instant it already
// carries (ADR-0065); with no title and no instant its content is all it has.
func TestItemOf(t *testing.T) {
	when := moment(at(11, 4, 22, 30), false, false)

	titled := ItemOf("Reunión con Juan", "tengo reunión con Juan el 2026-11-04 a las 22:30", when, true)
	if titled.Unnamed || titled.Text != "Reunión con Juan" || titled.When != when {
		t.Errorf("titled item = %+v, want its title, named, with its instant", titled)
	}
	if want := NamesOf("Reunión con Juan", "tengo reunión con Juan el 2026-11-04 a las 22:30"); titled.Names != want {
		t.Errorf("titled Names = %q, want %q", titled.Names, want)
	}

	unnamed := ItemOf("", "tengo reunión con Juan el 2026-11-04 a las 22:30", when, true)
	if !unnamed.Unnamed || unnamed.Text != "" {
		t.Errorf("untitled dated item = %+v, want Unnamed and none of the content", unnamed)
	}

	undated := ItemOf("", "La clave del wifi es 1234.", nil, false)
	if undated.Unnamed || undated.Text != "La clave del wifi es 1234." {
		t.Errorf("untitled undated item = %+v, want its content, named", undated)
	}

	// A recurring reminder with no title and no instant of its own is pushed
	// with a day the reply computed: that day is the reply's, not the unit's,
	// and the unit's words are all it has.
	recurring := ItemOf("", "tomar las pastillas", moment(at(11, 4, 0, 0), true, false), false)
	if recurring.Unnamed || recurring.Text != "tomar las pastillas" || recurring.When == nil {
		t.Errorf("untitled undated unit with the reply's day = %+v, want its content, named, with the day", recurring)
	}
}

// TestUnnamed pins every frame of ADR-0065 point 5, exact text, both languages:
// the sentence holds the instant and none of the unit's words.
func TestUnnamed(t *testing.T) {
	today := &Moment{At: at(11, 4, 22, 30)}
	thursday := &Moment{At: at(11, 26, 8, 30)}
	yesterday := &Moment{At: at(11, 3, 9, 0)}
	dueTomorrow := &Moment{At: at(11, 5, 0, 0), DayOnly: true, Due: true}
	night := at(11, 4, 22, 0)
	item := func(m *Moment) Item { return Item{Unnamed: true, When: m} }

	for _, tc := range []struct {
		name         string
		esGot, enGot string
		esOut, enOut string
	}{
		{"push, today",
			es.Push(item(today), night), en.Push(item(today), night),
			"Acordate: tenés algo anotado hoy a las 22:30.", "Don't forget: you have something noted today at 22:30."},
		{"push, due tomorrow",
			es.Push(item(dueTomorrow), night), en.Push(item(dueTomorrow), night),
			"Acordate: tenés algo anotado para mañana.", "Don't forget: you have something noted due tomorrow."},
		{"recall one, ahead",
			es.Recall([]Item{item(thursday)}, now), en.Recall([]Item{item(thursday)}, now),
			"Tenés algo anotado el jueves 26 de noviembre a las 8:30.", "You have something noted on Thursday 26 November at 8:30."},
		{"recall one, past",
			es.Recall([]Item{item(yesterday)}, now), en.Recall([]Item{item(yesterday)}, now),
			"Tenías algo anotado ayer a las 9:00.", "You had something noted yesterday at 9:00."},
		{"list line",
			es.DigestLineOf(item(today), night), en.DigestLineOf(item(today), night),
			"Algo anotado, hoy a las 22:30", "Something noted, today at 22:30"},
		{"recall many",
			es.Recall([]Item{item(today), item(thursday)}, night), en.Recall([]Item{item(today), item(thursday)}, night),
			"Tenés 2 cosas anotadas:\n• Algo anotado, hoy a las 22:30\n• Algo anotado, el jueves 26 de noviembre a las 8:30",
			"You have 2 things noted:\n• Something noted, today at 22:30\n• Something noted, on Thursday 26 November at 8:30"},
	} {
		if tc.esGot != tc.esOut {
			t.Errorf("%s: ES =\n%q\nwant\n%q", tc.name, tc.esGot, tc.esOut)
		}
		if tc.enGot != tc.enOut {
			t.Errorf("%s: EN =\n%q\nwant\n%q", tc.name, tc.enGot, tc.enOut)
		}
	}
}
