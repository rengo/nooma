package phrase

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/core/classify"
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
		"NotScheduled": 1, "TimerSet": 1, "RecurringSet": 1, "NotedFor": 2,
		"LeadDays": 1, "FoundOne": 1, "FoundMany": 1,
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

	if got, want := en.Leads(false, about, dayBefore, hoursBefore), "the day before and a few hours before"; got != want {
		t.Errorf("en.Leads = %q, want %q", got, want)
	}
	if got, want := es.Leads(false, about, dayBefore, hoursBefore), "el día anterior y unas horas antes"; got != want {
		t.Errorf("es.Leads = %q, want %q", got, want)
	}
	if got, want := en.Leads(false, about, about.Add(-3*time.Hour), hoursBefore), en.LeadHours; got != want {
		t.Errorf("en.Leads of two alike = %q, want %q said once", got, want)
	}
	if got, want := en.Leads(true, about, hoursBefore), en.LeadImmediate; got != want {
		t.Errorf("en.Leads(immediate) = %q, want %q", got, want)
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
