package phrase

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/core/classify"
	"github.com/rengo/nooma/internal/core/prospection"
)

// TestSetsCoverEveryLanguage is the gate ADR-0022 asks for: widening
// classify.AllLanguages() without writing the sentences must fail here,
// not answer in the fallback and look fine.
//
// It reflects over Set's fields rather than listing them, because a
// listing is a second copy of the struct that rots the day a sentence is
// added — and a sentence added to English only is exactly the hole this
// test exists to find.
func TestSetsCoverEveryLanguage(t *testing.T) {
	for _, l := range classify.AllLanguages() {
		s, ok := sets[l]
		if !ok {
			t.Errorf("no Set for language %q — the vocabulary names a language nothing can be said in", l)
			continue
		}

		v := reflect.ValueOf(s)
		for i := 0; i < v.NumField(); i++ {
			name := v.Type().Field(i).Name
			switch f := v.Field(i); f.Kind() {
			case reflect.String:
				if strings.TrimSpace(f.String()) == "" {
					t.Errorf("%s.%s is empty — an empty sentence is the one failure a person cannot tell from being ignored", l, name)
				}
			case reflect.Array:
				for j := 0; j < f.Len(); j++ {
					if strings.TrimSpace(f.Index(j).String()) == "" {
						t.Errorf("%s.%s[%d] is empty", l, name, j)
					}
				}
			}
		}
	}
}

// TestSets_FormatVerbsMatchTheirArguments catches the failure mode a table
// of format strings invites: a translated sentence that drops or doubles a
// verb renders "%!s(MISSING)" to a person, and no compiler sees it.
func TestSets_FormatVerbsMatchTheirArguments(t *testing.T) {
	// field name -> how many verbs the renderer supplies.
	want := map[string]int{
		"Reminder": 1, "AtWhen": 2, "NotScheduled": 1, "NotedNotSet": 1, "TimerToday": 1, "TimerOn": 2, "EveryDay": 1, "EveryWeek": 2, "EveryMonth": 2, "EveryYear": 3, "NotedFor": 2, "NotedForDigest": 1, "NotedOn": 1,
		"LeadDays": 1, "LeadMinutes": 1, "LeadThatDayAt": 1, "LeadTodayAt": 1, "FoundOne": 1, "FoundMany": 1,
		"Moved": 1, "RemindYou": 1, "LeadHoursExact": 1, "LeadsNotSet": 1, "Closed": 1,
	}

	for _, l := range classify.AllLanguages() {
		v := reflect.ValueOf(sets[l])
		for name, n := range want {
			got := strings.Count(v.FieldByName(name).String(), "%") -
				2*strings.Count(v.FieldByName(name).String(), "%%")
			if got != n {
				t.Errorf("%s.%s has %d format verb(s), want %d — a mismatch renders %%!s(MISSING) to a person", l, name, got, n)
			}
		}
	}
}

// TestFor_UnknownLanguageFallsBackRatherThanEmpty covers the branch the
// completeness test above keeps unreachable for real vocabulary members.
// It exists for the caller who builds a classify.Language out of thin air:
// a zero Set renders empty strings everywhere, which is worse than the
// wrong language.
func TestFor_UnknownLanguageFallsBackRatherThanEmpty(t *testing.T) {
	got := For(classify.Language("kl"))
	if got.Noted != For(classify.Fallback()).Noted {
		t.Errorf("For(unknown).Noted = %q, want the fallback's %q", got.Noted, For(classify.Fallback()).Noted)
	}
	// The empty language is the realistic case, not an exotic one: a
	// CaptureResult built before this field existed carries it.
	if For("").Noted == "" {
		t.Error(`For("") returns a zero Set — a result with no language renders nothing at all`)
	}
}

// TestSet_Time_UsesTheLanguagesOwnNames is the half no configuration key
// could have fixed either. Go's stdlib writes English day and month names
// on every machine in the world, so this is hand-built from the table.
func TestSet_Time_UsesTheLanguagesOwnNames(t *testing.T) {
	// A Friday, deliberately: the day and the month both have to change.
	at := time.Date(2026, 8, 28, 9, 5, 0, 0, time.UTC)

	if got, want := For(classify.LanguageEN).Time(at), "Fri 28 Aug, 09:05"; got != want {
		t.Errorf("EN Time() = %q, want %q", got, want)
	}
	if got, want := For(classify.LanguageES).Time(at), "vie 28 ago, 09:05"; got != want {
		t.Errorf("ES Time() = %q, want %q", got, want)
	}
}

// TestSet_Time_KeepsTheInstantsOwnZone pins the rule that survived a real
// defect: re-rendering in the process's zone tells someone in Buenos Aires
// about a reminder at a time nobody set.
func TestSet_Time_KeepsTheInstantsOwnZone(t *testing.T) {
	buenosAires := time.FixedZone("-03", -3*60*60)
	at := time.Date(2026, 8, 28, 9, 0, 0, 0, buenosAires)

	if got := For(classify.LanguageES).Time(at); !strings.Contains(got, "09:00") {
		t.Errorf("Time() = %q, want it to carry 09:00 — the instant's own zone, not the process's", got)
	}
}

// TestSet_Found_AgreesWithItsNumber is why FoundOne and FoundMany are two
// fields rather than one plural rule. Plural agreement is not a shared
// mechanism across languages, and pretending it is produces "1 things" in
// whichever language was not the author's.
func TestSet_Found_AgreesWithItsNumber(t *testing.T) {
	tests := []struct {
		lang classify.Language
		n    int
		want string
	}{
		{classify.LanguageEN, 1, "Found 1 thing:"},
		{classify.LanguageEN, 3, "Found 3 things:"},
		{classify.LanguageEN, 0, "Found 0 things:"},
		{classify.LanguageES, 1, "Encontré 1 cosa:"},
		{classify.LanguageES, 3, "Encontré 3 cosas:"},
	}

	for _, tt := range tests {
		if got := For(tt.lang).Found(tt.n); got != tt.want {
			t.Errorf("%s Found(%d) = %q, want %q", tt.lang, tt.n, got, tt.want)
		}
	}
}

// TestSet_ThatDayAt names the hour a day alone is reminded at, in the
// language (ADR-0031 point 7).
func TestSet_ThatDayAt(t *testing.T) {
	fire := time.Date(2026, 10, 17, 8, 30, 0, 0, time.FixedZone("ART", -3*60*60))
	for l, want := range map[classify.Language]string{
		classify.LanguageES: "ese día a las 08:30",
		classify.LanguageEN: "that day at 08:30",
	} {
		if got := For(l).ThatDayAt(fire); got != want {
			t.Errorf("%s: ThatDayAt = %q, want %q", l, got, want)
		}
	}
}

// TestSet_TimerAt names the one instant a timer rings at, as the other
// reminder replies name a ring: its hour alone on the day it was set, and its
// day and hour on another day (a span from 23:50 rings tomorrow).
func TestSet_TimerAt(t *testing.T) {
	art := time.FixedZone("ART", -3*60*60)
	set := time.Date(2026, 10, 26, 10, 8, 0, 0, art)
	late := time.Date(2026, 10, 26, 23, 50, 0, 0, art)
	today := time.Date(2026, 10, 26, 10, 28, 0, 0, art)
	tomorrow := time.Date(2026, 10, 27, 0, 20, 0, 0, art)
	for _, tc := range []struct {
		l         classify.Language
		now, fire time.Time
		want      string
	}{
		{classify.LanguageES, set, today, "Te aviso a las 10:28."},
		{classify.LanguageES, late, tomorrow, "Te aviso el mar 27 oct a las 00:20."},
		{classify.LanguageEN, set, today, "I will remind you at 10:28."},
		{classify.LanguageEN, late, tomorrow, "I will remind you on Tue 27 Oct at 00:20."},
		// The day is the fire's own: 23:50 at -03:00 is already the 27th in
		// UTC, and read there a ring at 00:20 on the 27th would be "today".
		{classify.LanguageES, late.UTC(), tomorrow, "Te aviso el mar 27 oct a las 00:20."},
	} {
		if got := For(tc.l).TimerAt(tc.fire, tc.now); got != tc.want {
			t.Errorf("%s: TimerAt(%v, %v) = %q, want %q", tc.l, tc.fire, tc.now, got, tc.want)
		}
	}
}

// TestSet_Lead_ReadsImmediateRatherThanDerivingIt pins the distinction a
// real defect produced: subtracting the two instants gives a true duration
// and a false promise — 41 hours reads as "the day before" for a reminder
// that arrives at once.
func TestSet_Lead_ReadsImmediateRatherThanDerivingIt(t *testing.T) {
	es := For(classify.LanguageES)
	zone := time.FixedZone("ART", -3*60*60)
	about := time.Date(2026, 10, 16, 10, 0, 0, 0, zone)

	if got, want := es.Lead(true, about.Add(-41*time.Hour), about), es.LeadImmediate; got != want {
		t.Errorf("Lead(immediate=true, 41h) = %q, want %q — the plan's own fact wins over the arithmetic", got, want)
	}

	// The day is the calendar's, in the event's own zone, not a 24-48 hour
	// gap: a date-only event at midnight reminded at 09:00 the day before is
	// 15 hours ahead and is "the day before".
	tests := []struct {
		fire time.Time
		want string
	}{
		{about.Add(time.Hour), es.LeadImmediate},
		{about, es.LeadImmediate},
		{about.Add(-2 * time.Hour), es.LeadHours},
		{time.Date(2026, 10, 16, 0, 30, 0, 0, zone), es.LeadHours},
		{about.Add(-24 * time.Hour), es.LeadDayBefore},
		{time.Date(2026, 10, 15, 0, 0, 0, 0, zone), es.LeadDayBefore},
		{time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC), es.LeadDayBefore}, // 06:00 the 15th in ART
		{about.Add(-72 * time.Hour), "3 días antes"},
		// Under an hour is said in minutes: an asked "15 minutes before" is
		// not "a few hours before" (ADR-0031 point 7).
		{about.Add(-15 * time.Minute), "15 minutos antes"},
		{about.Add(-59 * time.Minute), "59 minutos antes"},
		{about.Add(-time.Minute), es.LeadMinute},
		{about.Add(-time.Hour), es.LeadHours},
	}
	for _, tt := range tests {
		if got := es.Lead(false, tt.fire, about); got != tt.want {
			t.Errorf("Lead(false, %v, %v) = %q, want %q", tt.fire, about, got, tt.want)
		}
	}
	date := time.Date(2026, 10, 16, 0, 0, 0, 0, zone)
	if got := es.Lead(false, time.Date(2026, 10, 15, 9, 0, 0, 0, zone), date); got != es.LeadDayBefore {
		t.Errorf("Lead(09:00 the day before a date) = %q, want %q", got, es.LeadDayBefore)
	}
}

// TestSet_Leads_NamesEveryReminder covers ADR-0029's reply: every reminder
// of an event is named, joined in the message's language, and two that
// read alike are said once.
func TestSet_Leads_NamesEveryReminder(t *testing.T) {
	en, es := For(classify.LanguageEN), For(classify.LanguageES)
	about := time.Date(2026, 10, 16, 10, 0, 0, 0, time.UTC)
	dayBefore, hoursBefore := about.Add(-24*time.Hour), about.Add(-2*time.Hour)

	if got, want := en.Leads(false, time.Time{}, about, dayBefore, hoursBefore), "the day before and a few hours before"; got != want {
		t.Errorf("en.Leads = %q, want %q", got, want)
	}
	if got, want := es.Leads(false, time.Time{}, about, dayBefore, hoursBefore), "el día anterior y unas horas antes"; got != want {
		t.Errorf("es.Leads = %q, want %q", got, want)
	}
	if got, want := en.Leads(false, time.Time{}, about, about.Add(-3*time.Hour), hoursBefore), en.LeadHours; got != want {
		t.Errorf("en.Leads of two alike = %q, want %q said once", got, want)
	}
	if got, want := en.Leads(true, time.Time{}, about, hoursBefore), en.LeadImmediate; got != want {
		t.Errorf("en.Leads(immediate) = %q, want %q", got, want)
	}
}

// TestSet_ExactLeads: leads the user just named are said back as named, a whole
// number of hours as that number (I40): "avisame 1 hora antes" answered "unas
// horas antes" would read as not having understood. Anything else reads as
// Lead does.
func TestSet_ExactLeads(t *testing.T) {
	en, es := For(classify.LanguageEN), For(classify.LanguageES)
	about := time.Date(2026, 10, 20, 11, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		s     Set
		fires []time.Time
		want  string
	}{
		{es, []time.Time{about.Add(-time.Hour)}, "1 hora antes"},
		{en, []time.Time{about.Add(-time.Hour)}, "1 hour before"},
		{es, []time.Time{about.Add(-24 * time.Hour), about.Add(-2 * time.Hour)}, "el día anterior y 2 horas antes"},
		{es, []time.Time{about.Add(-30 * time.Minute)}, "30 minutos antes"},
		{es, []time.Time{about.Add(-90 * time.Minute)}, "unas horas antes"},
	} {
		if got := tc.s.ExactLeads(about, tc.fires...); got != tc.want {
			t.Errorf("ExactLeads(%v) = %q, want %q", tc.fires, got, tc.want)
		}
	}
}

// TestSet_NamedLeads (I44, ADR-0045): the rings of an event whose message named
// its leads are said as named, and a ring that serves none of them, the one
// ADR-0039 keeps on the event's own day when every named lead is behind, is said
// as when it rings: "ese día a las 09:00", never "1 hora antes".
func TestSet_NamedLeads(t *testing.T) {
	es := For(classify.LanguageES)
	dentist := time.Date(2026, 10, 21, 10, 0, 0, 0, time.UTC)
	dinner := time.Date(2026, 10, 22, 21, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		about time.Time
		named []int
		fires []time.Time
		want  string
	}{
		{dentist, []int{60}, []time.Time{dentist.Add(-time.Hour)}, "1 hora antes"},
		{dentist, []int{1440}, []time.Time{time.Date(2026, 10, 21, 9, 0, 0, 0, time.UTC)}, "ese día a las 09:00"},
		{dinner, []int{1440, 30}, []time.Time{dinner.Add(-24 * time.Hour), dinner.Add(-30 * time.Minute)}, "el día anterior y 30 minutos antes"},
		{dinner, []int{1440, 30}, []time.Time{time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC), dinner.Add(-30 * time.Minute)}, "ese día a las 09:00 y 30 minutos antes"},
	} {
		if got := es.NamedLeads(tc.about, tc.named, tc.fires...); got != tc.want {
			t.Errorf("NamedLeads(%v, %v) = %q, want %q", tc.named, tc.fires, got, tc.want)
		}
	}
}

// TestSet_List_PutsEveryResultUnderItsHeader covers the assembled message,
// which is what a person actually receives — the header alone is not the
// answer.
func TestSet_List_PutsEveryResultUnderItsHeader(t *testing.T) {
	got := For(classify.LanguageES).List([]string{"turno con el dentista", "pagar el alquiler"})

	if !strings.HasPrefix(got, "Encontré 2 cosas:") {
		t.Errorf("List() does not open with its header: %q", got)
	}
	for _, want := range []string{"\n• turno con el dentista", "\n• pagar el alquiler"} {
		if !strings.Contains(got, want) {
			t.Errorf("List() is missing %q: %q", want, got)
		}
	}

	if got := For(classify.LanguageEN).List(nil); got != "Found 0 things:" {
		t.Errorf("List(nil) = %q — an empty list still renders its header; the caller is what decides not to send it", got)
	}
}

// TestPhraseSetsComplete pins the sentences a reminder is framed with
// (ADR-0032 point 4) in every language: the frame carries its one verb, the
// when-clause its two, and a date has no clock in it. The reflection test above
// already fails an empty field; this one fails a frame that renders nonsense.
func TestPhraseSetsComplete(t *testing.T) {
	when := time.Date(2026, time.October, 15, 11, 0, 0, 0, time.UTC)
	for _, l := range classify.AllLanguages() {
		s := For(l)
		got := s.Remind("dentist", s.Time(when))
		if line := s.Line("dentist", s.Date(when)); !strings.Contains(line, "dentist") || !strings.Contains(line, s.Date(when)) {
			t.Errorf("%s: Line = %q, want the words and the day", l, line)
		}
		if bare := s.Remind("dentist", ""); bare != fmt.Sprintf(s.Reminder, "dentist") {
			t.Errorf("%s: a reminder with no instant = %q, want the frame around the words alone", l, bare)
		}
		for _, part := range []string{"dentist", s.Time(when)} {
			if !strings.Contains(got, part) {
				t.Errorf("%s: Reminder(AtWhen) = %q, missing %q", l, got, part)
			}
		}
		if strings.Contains(got, "%!") {
			t.Errorf("%s: Reminder(AtWhen) = %q renders a broken verb", l, got)
		}
		day := s.Date(when)
		if strings.Contains(day, "11:00") || !strings.Contains(day, s.Weekdays[when.Weekday()]) {
			t.Errorf("%s: Date = %q, want the weekday and day with no clock", l, day)
		}
	}
	if got, want := For(classify.LanguageES).Remind("x", ""), "Recordatorio: x"; got != want {
		t.Errorf("Spanish frame = %q, want %q", got, want)
	}
	if got, want := For(classify.LanguageEN).Remind("x", ""), "Reminder: x"; got != want {
		t.Errorf("English frame = %q, want %q", got, want)
	}
}

// TestProactiveSentences pins what Nooma says without being asked (ADR-0032
// point 5): the digest's header in agreement with its count, its bullet, its
// question and the lateness note, in every language, with no broken verb.
func TestProactiveSentences(t *testing.T) {
	for _, l := range classify.AllLanguages() {
		s := For(l)
		for n, got := range map[int]string{1: s.DigestHeader(1), 3: s.DigestHeader(3), 0: s.DigestHeader(0)} {
			if got == "" || strings.Contains(got, "%!") || (n > 1 && !strings.Contains(got, fmt.Sprint(n))) {
				t.Errorf("%s: DigestHeader(%d) = %q", l, n, got)
			}
		}
		if s.DigestHeader(1) == s.DigestHeader(2) {
			t.Errorf("%s: DigestHeader ignores whether there is one line or several", l)
		}
		for _, c := range []struct{ name, got, word string }{
			{"DigestItem", s.DigestItem("dentist"), "dentist"},
			{"Question", s.Question("plan", "budget"), "plan"},
			{"Delayed", s.Delayed("dentist"), "dentist"},
		} {
			if strings.Contains(c.got, "%!") || !strings.Contains(c.got, c.word) {
				t.Errorf("%s: %s = %q renders a broken verb or drops its words", l, c.name, c.got)
			}
		}
		if q := s.Question("plan", "budget"); !strings.Contains(q, "budget") {
			t.Errorf("%s: Question = %q, want both endpoints", l, q)
		}
		if s.OneMore == "" || s.SomethingYouAsked == "" {
			t.Errorf("%s: a proactive sentence is empty", l)
		}
	}
	es := For(classify.LanguageES)
	for got, want := range map[string]string{
		es.DigestHeader(1):               "Hoy hay 1 cosa:",
		es.DigestHeader(3):               "Hoy hay 3 cosas:",
		es.Question("plan", "budget"):    `Relacioné "plan" con "budget". ¿Tienen relación?`,
		es.Delayed("Recordatorio: x"):    "Recordatorio: x (Esto llega más tarde de lo previsto.)",
		es.SomethingYouAsked:             "Me pediste que te recordara algo.",
		es.OneMore:                       "Una más — ",
		es.DigestItem("x"):               "• x",
		For(classify.LanguageEN).OneMore: "One more — ",
	} {
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	}
	en := For(classify.LanguageEN)
	if got, want := en.Question("plan", "budget"), `I linked "plan" with "budget". Are they related?`; got != want {
		t.Errorf("en.Question = %q, want %q", got, want)
	}
	if got, want := en.Delayed("x"), "x (This is later than intended.)"; got != want {
		t.Errorf("en.Delayed = %q, want %q", got, want)
	}
	if en.DigestHeader(1) != "Here is 1 thing for today:" || en.DigestHeader(2) != "Here are 2 things for today:" {
		t.Errorf("en headers = %q, %q", en.DigestHeader(1), en.DigestHeader(2))
	}
}

// TestAskAgainSentences: the digest asks a check-in again with a header and one
// question per check-in, the client's Spanish of 2026-10-16 and its English.
func TestAskAgainSentences(t *testing.T) {
	for l, want := range map[classify.Language][2]string{
		classify.LanguageES: {"Te pregunto de nuevo:", "¿Ya está «Comprar café»?"},
		classify.LanguageEN: {"Asking again:", "Is \"Buy coffee\" done?"},
	} {
		s := For(l)
		if s.AskAgain != want[0] {
			t.Errorf("%s: AskAgain = %q, want %q", l, s.AskAgain, want[0])
		}
		words := map[classify.Language]string{classify.LanguageES: "Comprar café", classify.LanguageEN: "Buy coffee"}[l]
		if got := s.Done(words); got != want[1] {
			t.Errorf("%s: Done = %q, want %q", l, got, want[1])
		}
	}
}

// TestSet_Every: a recurring reply names the anchor's weekday, day and month,
// never a clamped occurrence's, at the occurrence's clock time; a weekly anchor
// with no weekday reads it from the occurrence.
func TestSet_Every(t *testing.T) {
	zone := time.FixedZone("UTC-3", -3*60*60)
	nov30 := time.Date(2026, 11, 30, 18, 0, 0, 0, zone) // a day-31 monthly reminder's November ring
	friday := time.Weekday(5)
	es := For(classify.LanguageES)
	for _, tc := range []struct {
		rule   prospection.Rule
		anchor prospection.Anchor
		want   string
	}{
		{prospection.RuleDaily, prospection.Anchor{}, "Te aviso todos los días a las 18:00."},
		{prospection.RuleWeekly, prospection.Anchor{Weekday: &friday}, "Te aviso todos los viernes a las 18:00."},
		{prospection.RuleWeekly, prospection.Anchor{}, "Te aviso todos los lunes a las 18:00."},
		{prospection.RuleMonthly, prospection.Anchor{Month: time.October, Day: 31}, "Te aviso el día 31 de cada mes a las 18:00."},
		{prospection.RuleYearly, prospection.Anchor{Month: time.February, Day: 29}, "Te aviso todos los años el 29 de febrero a las 18:00."},
	} {
		if got := es.Every(tc.rule, tc.anchor, nov30); got != tc.want {
			t.Errorf("Every(%s, %+v) = %q, want %q", tc.rule, tc.anchor, got, tc.want)
		}
	}
}

// TestSet_DayLeads: a day alone names a ring on the day by its hour and one on
// an earlier day by how long before; at once is said as such; two alike once.
func TestSet_DayLeads(t *testing.T) {
	zone := time.FixedZone("UTC-3", -3*60*60)
	day := time.Date(2026, 10, 23, 0, 0, 0, 0, zone)
	at := func(d, h int) time.Time { return time.Date(2026, 10, d, h, 0, 0, 0, zone) }
	es := For(classify.LanguageES)
	for _, tc := range []struct {
		immediate bool
		fires     []time.Time
		want      string
	}{
		{false, []time.Time{at(23, 9)}, "ese día a las 09:00"},
		{false, []time.Time{at(22, 9), at(23, 9)}, "el día anterior y ese día a las 09:00"},
		{true, []time.Time{at(22, 21), at(23, 9)}, "ahora mismo y ese día a las 09:00"},
		{false, []time.Time{at(23, 9), at(23, 9)}, "ese día a las 09:00"},
	} {
		if got := es.DayLeads(tc.immediate, time.Time{}, day, tc.fires...); got != tc.want {
			t.Errorf("DayLeads(%v, %v) = %q, want %q", tc.immediate, tc.fires, got, tc.want)
		}
	}
}

// TestSet_Leads_ARingOnTheToldDayIsToday (ADR-0037): a ring that falls on the
// local day the user told Nooma about an event on a later day is named as
// today at its hour, never "the day before" — the client's complaint.
func TestSet_Leads_ARingOnTheToldDayIsToday(t *testing.T) {
	zone := time.FixedZone("UTC-3", -3*60*60)
	at := func(d, h, m int) time.Time { return time.Date(2026, 10, d, h, m, 0, 0, zone) }
	en, es := For(classify.LanguageEN), For(classify.LanguageES)
	told := at(19, 8, 0)

	for _, tc := range []struct {
		name string
		got  string
		want string
	}{
		{"es, a timed event tomorrow kept today", es.Leads(false, told, at(20, 1, 30), at(19, 19, 0)), "hoy a las 19:00"},
		{"en, a timed event tomorrow kept today", en.Leads(false, told, at(20, 1, 30), at(19, 19, 0)), "today at 19:00"},
		{"es, today and tomorrow", es.Leads(false, told, at(20, 10, 0), at(19, 11, 0), at(20, 8, 0)), "hoy a las 11:00 y unas horas antes"},
		{"es, an event later today is not today at", es.Leads(false, told, at(19, 20, 0), at(19, 18, 0)), "unas horas antes"},
		{"es, at once stays at once", es.Leads(true, told, at(20, 10, 0), told), "ahora mismo"},
		{"es, told on another day", es.Leads(false, at(18, 8, 0), at(20, 10, 0), at(19, 10, 0)), "el día anterior"},
		{"es, a date-only event tomorrow", es.DayLeads(false, told, at(20, 0, 0), at(19, 9, 0)), "hoy a las 09:00"},
		{"en, a date-only event tomorrow", en.DayLeads(false, told, at(20, 0, 0), at(19, 9, 0)), "today at 09:00"},
		{"es, a date-only event tomorrow told the day before", es.DayLeads(false, at(18, 8, 0), at(20, 0, 0), at(19, 9, 0)), "el día anterior"},
	} {
		if tc.got != tc.want {
			t.Errorf("%s: %q, want %q", tc.name, tc.got, tc.want)
		}
	}
}
