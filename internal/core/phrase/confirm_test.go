package phrase

import (
	"fmt"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/core/prospection"
)

// TestConfirmations pins every confirmation and correction of warm-replies
// design §6, exact text, both languages: a day and a time said as a person says
// them, through Dated and the sentence frames (ADR-0064).
func TestConfirmations(t *testing.T) {
	day := func(m time.Month, d int) Moment { return Moment{At: at(m, d, 0, 0), DayOnly: true} }
	timed := func(m time.Month, d, h, min int) Moment { return Moment{At: at(m, d, h, min)} }
	for _, tc := range []struct {
		name           string
		es, en         string
		wantES, wantEN string
	}{
		{"plain note", es.Noted, en.Noted, "Listo, anotado.", "Got it, noted."},
		{"event armed",
			fmt.Sprintf(es.NotedFor, es.Dated(timed(11, 12, 10, 0), now), es.Leads(false, now, at(11, 12, 10, 0), at(11, 11, 10, 0), at(11, 12, 7, 0))),
			fmt.Sprintf(en.NotedFor, en.Dated(timed(11, 12, 10, 0), now), en.Leads(false, now, at(11, 12, 10, 0), at(11, 11, 10, 0), at(11, 12, 7, 0))),
			"Listo, anotado para el jueves 12 de noviembre a las 10:00. Te aviso el día anterior y unas horas antes.",
			"Got it, noted for Thursday 12 November at 10:00. I'll remind you the day before and a few hours before."},
		{"day alone, asked",
			fmt.Sprintf(es.NotedFor, es.Dated(day(11, 13), now), es.DayLeads(false, now, at(11, 13, 0, 0), at(11, 13, 9, 0))),
			fmt.Sprintf(en.NotedFor, en.Dated(day(11, 13), now), en.DayLeads(false, now, at(11, 13, 0, 0), at(11, 13, 9, 0))),
			"Listo, anotado para el viernes 13 de noviembre. Te aviso ese día a las 9:00.",
			"Got it, noted for Friday 13 November. I'll remind you that day at 9:00."},
		{"task to the digest",
			fmt.Sprintf(es.NotedForDigest, es.Dated(day(11, 9), now)),
			fmt.Sprintf(en.NotedForDigest, en.Dated(day(11, 9), now)),
			"Listo, anotado para el lunes 9 de noviembre. Te lo recuerdo en el resumen de esa mañana.",
			"Got it, noted for Monday 9 November. I'll mention it in that morning's digest."},
		{"task to the digest, tomorrow at an hour",
			fmt.Sprintf(es.NotedForDigest, es.Dated(timed(11, 5, 16, 0), now)),
			fmt.Sprintf(en.NotedForDigest, en.Dated(timed(11, 5, 16, 0), now)),
			"Listo, anotado para mañana a las 16:00. Te lo recuerdo en el resumen de esa mañana.",
			"Got it, noted for tomorrow at 16:00. I'll mention it in that morning's digest."},
		{"event nothing was armed for",
			fmt.Sprintf(es.NotedOn, es.Dated(timed(11, 6, 1, 0), now)),
			fmt.Sprintf(en.NotedOn, en.Dated(timed(11, 6, 1, 0), now)),
			"Listo, anotado para pasado mañana a la 1:00.",
			"Got it, noted for the day after tomorrow at 1:00."},
		{"daily", es.Every(prospection.RuleDaily, prospection.Anchor{}, at(11, 5, 9, 0)), en.Every(prospection.RuleDaily, prospection.Anchor{}, at(11, 5, 9, 0)),
			"Listo, te aviso todos los días a las 9:00.", "Sure, I'll remind you every day at 9:00."},
		{"yearly", es.Every(prospection.RuleYearly, prospection.Anchor{Month: time.October, Day: 23}, at(11, 5, 9, 0)),
			en.Every(prospection.RuleYearly, prospection.Anchor{Month: time.October, Day: 23}, at(11, 5, 9, 0)),
			"Listo, te aviso todos los años el 23 de octubre a las 9:00.", "Sure, I'll remind you every year on 23 October at 9:00."},
		{"timer, tomorrow", es.TimerAt(at(11, 5, 0, 20), now), en.TimerAt(at(11, 5, 0, 20), now),
			"Listo, te aviso mañana a las 0:20.", "Sure, I'll remind you tomorrow at 0:20."},
		{"timer, today", es.TimerAt(at(11, 4, 10, 20), now), en.TimerAt(at(11, 4, 10, 20), now),
			"Listo, te aviso a las 10:20.", "Sure, I'll remind you at 10:20."},
		{"timer, a named date", es.TimerAt(at(11, 27, 8, 30), now), en.TimerAt(at(11, 27, 8, 30), now),
			"Listo, te aviso el viernes 27 de noviembre a las 8:30.", "Sure, I'll remind you on Friday 27 November at 8:30."},
		{"asked, past", fmt.Sprintf(es.NotedNotSet, es.RefusalPast), fmt.Sprintf(en.NotedNotSet, en.RefusalPast),
			"Lo anoté, pero no te puse aviso: esa hora ya pasó.", "I noted it, but I didn't set a reminder: that time has already passed."},
		{"moved, with leads",
			fmt.Sprintf(es.Moved, es.Dated(timed(11, 5, 11, 0), now)) + " " + fmt.Sprintf(es.RemindYou, es.ExactLeads(at(11, 5, 11, 0), at(11, 5, 7, 30), at(11, 5, 10, 45))),
			fmt.Sprintf(en.Moved, en.Dated(timed(11, 5, 11, 0), now)) + " " + fmt.Sprintf(en.RemindYou, en.ExactLeads(at(11, 5, 11, 0), at(11, 5, 7, 30), at(11, 5, 10, 45))),
			"Listo, lo pasé para mañana a las 11:00. Te aviso unas horas antes y 15 minutos antes.",
			"Done, I moved it to tomorrow at 11:00. I'll remind you a few hours before and 15 minutes before."},
		{"corrected", es.Corrected, en.Corrected, "Listo, lo corregí.", "Done, I fixed it."},
		{"unchanged", es.Unchanged, en.Unchanged, "Ya decía eso, así que no cambié nada.", "It already said that, so I changed nothing."},
		{"not scheduled", fmt.Sprintf(es.NotScheduled, es.RefusalPast), fmt.Sprintf(en.NotScheduled, en.RefusalPast),
			"No lo programé: esa hora ya pasó", "I did not set that: that time has already passed"},
	} {
		if tc.es != tc.wantES {
			t.Errorf("%s: ES\n%q\nwant\n%q", tc.name, tc.es, tc.wantES)
		}
		if tc.en != tc.wantEN {
			t.Errorf("%s: EN\n%q\nwant\n%q", tc.name, tc.en, tc.wantEN)
		}
	}
}

// TestClockEverywhere: every lead phrase that names an hour says it as Clock
// does, so one message never mixes 09:00 and 9:00 (ADR-0064 point 2).
func TestClockEverywhere(t *testing.T) {
	fire := at(11, 5, 9, 5)
	for _, tc := range []struct{ name, got, want string }{
		{"that day, es", es.ThatDayAt(fire), "ese día a las 9:05"},
		{"that day, en", en.ThatDayAt(fire), "that day at 9:05"},
		{"today, es", es.TodayAt(fire), "hoy a las 9:05"},
		{"today, en", en.TodayAt(fire), "today at 9:05"},
		{"one o'clock", es.ThatDayAt(at(11, 5, 1, 0)), "ese día a la 1:00"},
		{"weekly", es.Every(prospection.RuleWeekly, prospection.Anchor{}, at(11, 9, 8, 30)), "Listo, te aviso todos los lunes a las 8:30."},
		{"monthly", en.Every(prospection.RuleMonthly, prospection.Anchor{Day: 15}, at(11, 15, 8, 30)), "Sure, I'll remind you on day 15 of every month at 8:30."},
		{"timer list line, today", es.TimerLine(at(11, 4, 10, 20), now, ptr("sacar la ropa")), "10:20 — sacar la ropa"},
		{"timer list line, no padding", es.TimerLine(at(11, 4, 9, 5), now, ptr("sacar la ropa")), "9:05 — sacar la ropa"},
		{"timer list line, no words", en.TimerLine(at(11, 4, 10, 20), now, nil), "10:20"},
	} {
		if tc.got != tc.want {
			t.Errorf("%s: %q, want %q", tc.name, tc.got, tc.want)
		}
	}
}

func ptr(s string) *string { return &s }
