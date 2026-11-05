package phrase

import (
	"testing"
	"time"

	"github.com/rengo/nooma/internal/core/classify"
)

// Zone -03:00, now Wed 2026-11-04 10:00: the fixture of warm-replies design §6.
var (
	zoneART = time.FixedZone("ART", -3*60*60)
	now     = time.Date(2026, 11, 4, 10, 0, 0, 0, zoneART)
)

func at(month time.Month, day, hour, minute int) time.Time {
	return time.Date(2026, month, day, hour, minute, 0, 0, zoneART)
}

var (
	es = For(classify.LanguageES)
	en = For(classify.LanguageEN)
)

// TestDay_RelativeAndFull: yesterday to the day after tomorrow by name, any
// other day by weekday, date and full month, the year only when it is not
// now's, and the day counted on the calendar in now's zone (ADR-0064 point 2).
func TestDay_RelativeAndFull(t *testing.T) {
	for _, tc := range []struct {
		name   string
		at     time.Time
		es, en string
	}{
		{"yesterday", at(11, 3, 21, 0), "ayer", "yesterday"},
		{"today", at(11, 4, 11, 0), "hoy", "today"},
		{"tomorrow", at(11, 5, 11, 0), "mañana", "tomorrow"},
		{"day after tomorrow", at(11, 6, 11, 0), "pasado mañana", "the day after tomorrow"},
		{"three days", at(11, 7, 9, 0), "el sábado 7 de noviembre", "Saturday 7 November"},
		{"two days back", at(11, 2, 9, 0), "el lunes 2 de noviembre", "Monday 2 November"},
		{"another year", time.Date(2027, 1, 5, 9, 0, 0, 0, zoneART), "el martes 5 de enero de 2027", "Tuesday 5 January 2027"},
		// 02:30 UTC on the 5th is 23:30 on the 4th in -03:00: today, not tomorrow.
		{"late evening in the zone", time.Date(2026, 11, 5, 2, 30, 0, 0, time.UTC), "hoy", "today"},
		// The instant arrives in another zone; the day is now's.
		{"read in now's zone", time.Date(2026, 11, 5, 1, 0, 0, 0, time.UTC), "hoy", "today"},
	} {
		if got := es.Day(Moment{At: tc.at}, now); got != tc.es {
			t.Errorf("%s: ES Day = %q, want %q", tc.name, got, tc.es)
		}
		if got := en.Day(Moment{At: tc.at}, now); got != tc.en {
			t.Errorf("%s: EN Day = %q, want %q", tc.name, got, tc.en)
		}
	}
}

// TestClock: a 24-hour time with no leading zero; Spanish says "a la" for
// one o'clock alone.
func TestClock(t *testing.T) {
	for _, tc := range []struct {
		h, m   int
		es, en string
	}{
		{8, 30, "a las 8:30", "at 8:30"},
		{1, 0, "a la 1:00", "at 1:00"},
		{13, 0, "a las 13:00", "at 13:00"},
		{0, 20, "a las 0:20", "at 0:20"},
	} {
		tm := at(11, 4, tc.h, tc.m)
		if got := es.Clock(tm); got != tc.es {
			t.Errorf("ES Clock(%d:%02d) = %q, want %q", tc.h, tc.m, got, tc.es)
		}
		if got := en.Clock(tm); got != tc.en {
			t.Errorf("EN Clock(%d:%02d) = %q, want %q", tc.h, tc.m, got, tc.en)
		}
	}
}

// TestWhen: the adverbial day and time, a day alone with no clock, and a due
// day said as due, in the past tense once it is behind now.
func TestWhen(t *testing.T) {
	for _, tc := range []struct {
		name   string
		m      Moment
		es, en string
	}{
		{"date and time", Moment{At: at(11, 26, 8, 30)}, "el jueves 26 de noviembre a las 8:30", "on Thursday 26 November at 8:30"},
		{"relative and time", Moment{At: at(11, 5, 10, 0)}, "mañana a las 10:00", "tomorrow at 10:00"},
		{"one o'clock", Moment{At: at(11, 4, 1, 0)}, "hoy a la 1:00", "today at 1:00"},
		{"day alone, relative", Moment{At: at(11, 4, 0, 0), DayOnly: true}, "hoy", "today"},
		{"day alone, dated", Moment{At: at(11, 13, 0, 0), DayOnly: true}, "el viernes 13 de noviembre", "on Friday 13 November"},
		{"due tomorrow", Moment{At: at(11, 5, 0, 0), DayOnly: true, Due: true}, "para mañana", "due tomorrow"},
		{"due today", Moment{At: at(11, 4, 0, 0), DayOnly: true, Due: true}, "para hoy", "due today"},
		{"due dated", Moment{At: at(11, 9, 0, 0), DayOnly: true, Due: true}, "para el lunes 9 de noviembre", "due Monday 9 November"},
		{"past due", Moment{At: at(11, 3, 0, 0), DayOnly: true, Due: true}, "era para ayer", "was due yesterday"},
	} {
		if got := es.When(tc.m, now); got != tc.es {
			t.Errorf("%s: ES When = %q, want %q", tc.name, got, tc.es)
		}
		if got := en.When(tc.m, now); got != tc.en {
			t.Errorf("%s: EN When = %q, want %q", tc.name, got, tc.en)
		}
	}
}

// TestInline: Spanish lowers an item's first letter unless the first word is
// an acronym or is written capitalised past the first word of the unit's
// title or content; English keeps the words; a trailing period goes.
func TestInline(t *testing.T) {
	for _, tc := range []struct {
		name   string
		it     Item
		es, en string
	}{
		{"ordinary first word", Item{Text: "Turno con el oculista"}, "turno con el oculista", "Turno con el oculista"},
		{"acronym kept", Item{Text: "DNI vence"}, "DNI vence", "DNI vence"},
		{"title and content open with the same word",
			Item{Text: "Turno con el oculista", Names: "con el oculista el jueves"}, "turno con el oculista", "Turno con el oculista"},
		{"name seen later in the content",
			Item{Text: "Pedro cumple", Names: "cumple para Pedro el jueves"}, "Pedro cumple", "Pedro cumple"},
		{"name the code cannot see",
			Item{Text: "Ana cumple", Names: "cumple cumple años"}, "ana cumple", "Ana cumple"},
		{"trailing period", Item{Text: "La clave del wifi es 1234."}, "la clave del wifi es 1234", "La clave del wifi es 1234"},
		{"already lower", Item{Text: "tomar las pastillas"}, "tomar las pastillas", "tomar las pastillas"},
		{"opening exclamation", Item{Text: "¡Llamar a mamá!"}, "¡llamar a mamá!", "¡Llamar a mamá!"},
		{"opening question", Item{Text: "¿Llamaste a mamá?"}, "¿llamaste a mamá?", "¿Llamaste a mamá?"},
		{"opening mark before an acronym", Item{Text: "¿DNI vencido?"}, "¿DNI vencido?", "¿DNI vencido?"},
		{"a digit first stays", Item{Text: "3 pastillas"}, "3 pastillas", "3 pastillas"},
	} {
		if got := es.Inline(tc.it); got != tc.es {
			t.Errorf("%s: ES Inline = %q, want %q", tc.name, got, tc.es)
		}
		if got := en.Inline(tc.it); got != tc.en {
			t.Errorf("%s: EN Inline = %q, want %q", tc.name, got, tc.en)
		}
	}
}

func moment(tm time.Time, dayOnly, due bool) *Moment {
	return &Moment{At: tm, DayOnly: dayOnly, Due: due}
}

// TestRecall pins every recall answer of design §6, exact text, both languages.
func TestRecall(t *testing.T) {
	gastro := Item{Text: "Turno para estudios con el gastroenterólogo", When: moment(at(11, 26, 8, 30), false, false), Names: "para estudios con el gastroenterólogo"}
	dinnerPast := Item{Text: "Cena con Ana", When: moment(at(11, 3, 21, 0), false, false), Names: "con Ana"}
	passport := Item{Text: "Renovar el pasaporte", When: moment(at(11, 5, 0, 0), true, true), Names: "el pasaporte"}
	passportLate := Item{Text: "Renovar el pasaporte", When: moment(at(11, 3, 0, 0), true, true)}
	wifi := Item{Text: "La clave del wifi es 1234.", Names: "clave del wifi es 1234."}
	oculist := Item{Text: "Turno con el oculista", When: moment(at(11, 5, 11, 0), false, false)}
	dinner := Item{Text: "Cena con Ana", When: moment(at(11, 13, 21, 0), false, false)}
	passportMon := Item{Text: "Renovar el pasaporte", When: moment(at(11, 9, 0, 0), true, true)}
	coffee := Item{Text: "Me gusta el café sin azúcar."}

	for _, tc := range []struct {
		name   string
		items  []Item
		es, en string
	}{
		{"one ahead", []Item{gastro},
			"Tenés anotado turno para estudios con el gastroenterólogo el jueves 26 de noviembre a las 8:30.",
			"You have “Turno para estudios con el gastroenterólogo” on Thursday 26 November at 8:30."},
		{"one past", []Item{dinnerPast},
			"Tenías anotado cena con Ana ayer a las 21:00.",
			"You had “Cena con Ana” yesterday at 21:00."},
		{"one timed earlier today", []Item{{Text: "Turno con el oculista", When: moment(at(11, 4, 8, 0), false, false)}},
			"Tenías anotado turno con el oculista hoy a las 8:00.",
			"You had “Turno con el oculista” today at 8:00."},
		{"one due", []Item{passport},
			"Tenés anotado renovar el pasaporte para mañana.",
			"You have “Renovar el pasaporte” due tomorrow."},
		{"one due, past", []Item{passportLate},
			"Tenías anotado renovar el pasaporte para ayer.",
			"You had “Renovar el pasaporte” due yesterday."},
		{"one day alone, today is not past", []Item{{Text: "Cumpleaños de Ana", When: moment(at(11, 4, 0, 0), true, false)}},
			"Tenés anotado cumpleaños de Ana hoy.",
			"You have “Cumpleaños de Ana” today."},
		{"one undated", []Item{wifi},
			"Tenés anotado: la clave del wifi es 1234.",
			"You have this noted: La clave del wifi es 1234."},
		{"several", []Item{oculist, dinner, passportMon},
			"Tenés 3 cosas anotadas:\n• Turno con el oculista, mañana a las 11:00\n• Cena con Ana, el viernes 13 de noviembre a las 21:00\n• Renovar el pasaporte, para el lunes 9 de noviembre",
			"You have 3 things noted:\n• Turno con el oculista, tomorrow at 11:00\n• Cena con Ana, on Friday 13 November at 21:00\n• Renovar el pasaporte, due Monday 9 November"},
		{"several, undated and past due", []Item{coffee, passportLate},
			"Tenés 2 cosas anotadas:\n• Me gusta el café sin azúcar.\n• Renovar el pasaporte, era para ayer",
			"You have 2 things noted:\n• Me gusta el café sin azúcar.\n• Renovar el pasaporte, was due yesterday"},
		{"none", nil, "No tengo nada anotado sobre eso.", "I don't have anything noted about that."},
	} {
		if got := es.Recall(tc.items, now); got != tc.es {
			t.Errorf("%s: ES Recall =\n%q\nwant\n%q", tc.name, got, tc.es)
		}
		if got := en.Recall(tc.items, now); got != tc.en {
			t.Errorf("%s: EN Recall =\n%q\nwant\n%q", tc.name, got, tc.en)
		}
	}
}

// TestDated_ReadsTheInstantInNowsZone: an instant stored in UTC is said in the
// user's zone, the clock and the day both. 2026-11-05 01:30 UTC is Wednesday
// 22:30 at -03:00, which is today, not tomorrow.
func TestDated_ReadsTheInstantInNowsZone(t *testing.T) {
	utc := time.Date(2026, 11, 5, 1, 30, 0, 0, time.UTC)
	m := Moment{At: utc}
	if got, want := es.Dated(m, now), "hoy a las 22:30"; got != want {
		t.Errorf("ES Dated = %q, want %q", got, want)
	}
	if got, want := en.Dated(m, now), "today at 22:30"; got != want {
		t.Errorf("EN Dated = %q, want %q", got, want)
	}
}
