package voice

import (
	"strings"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/core/classify"
	"github.com/rengo/nooma/internal/core/phrase"
)

var art = time.FixedZone("ART", -3*3600)

// now is the client's own morning of 2026-10-30 (ADR-0061's context).
var now = time.Date(2026, 10, 30, 10, 0, 0, 0, art)

func at(y int, m time.Month, d, h, min int) *time.Time {
	t := time.Date(y, m, d, h, min, 0, 0, art)
	return &t
}

const turnoDraft = "Encontré 1 cosa:\n• Turno para estudios con el gastroenterólogo — jue 26 nov, 08:30"

// turno is the client's question and what the recall found for it.
func turno() Facts {
	return Facts{
		Intent:   RecallFound,
		Language: classify.LanguageES,
		Now:      now,
		Message:  "cuando tengo turno al gastroenterologo?",
		Items:    []Item{{Text: "Turno para estudios con el gastroenterólogo", Type: "event", At: at(2026, 11, 26, 8, 30)}},
	}
}

func TestCheck_TheClientsReplyIsSent(t *testing.T) {
	t.Parallel()
	got, why := Check(turno(), turnoDraft, "  «Tenés el {1} {1.when}.»\n")
	if want := "Tenés el turno para estudios con el gastroenterólogo el jueves 26 de noviembre a las 8:30."; why != Accepted || got != want {
		t.Fatalf("Check = %q, %q; want %q", got, why, want)
	}
}

func TestCheck_AWordingOfTheWrongShapeSendsTheDraft(t *testing.T) {
	t.Parallel()
	long := "Tenés el {1} {1.when}" + strings.Repeat(", para los estudios", 20) + "."
	tests := []struct {
		name, out string
		facts     func(*Facts)
		want      Rejection
	}{
		{"empty", "   ", nil, RejectEmpty},
		{"an emoji", "Tenés el {1} {1.when} 🙂", nil, RejectEmoji},
		{"an emoji from the technical block", "Tenés el {1} {1.when} ⏰", nil, RejectEmoji},
		{"markdown", "Tenés el **{1}** {1.when}.", nil, RejectMarkup},
		{"single-asterisk emphasis", "Tenés el *{1}* {1.when}.", nil, RejectMarkup},
		{"a heading", "# Turno\nEl {1} {1.when}.", nil, RejectMarkup},
		{"json", `{"reply": "{1} {1.when}"}`, nil, RejectMarkup},
		{"a dash list", "Tenés esto:\n- {1}, {1.when}", nil, RejectMarkup},
		{"more list lines than items", "Tenés esto:\n• {1}, {1.when}\n• nada", nil, RejectMarkup},
		{"an offer", "Tenés el {1} {1.when}. ¿Querés que te avise?", nil, RejectQuestion},
		{"three sentences", "Lo tengo. Es el {1}. Es {1.when}.", nil, RejectSentences},
		{"too long", long, nil, RejectLength},
		{"a kept literal dropped", "Tenés el {1} {1.when}.",
			func(f *Facts) { f.Keep = []string{"(Si fue un error, decime «no, seguí».)"} }, RejectKeep},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := turno()
			if tc.facts != nil {
				tc.facts(&f)
			}
			got, why := Check(f, turnoDraft, tc.out)
			if why != tc.want || got != turnoDraft {
				t.Fatalf("Check(%q) = %q, %q; want the draft and %q", tc.out, got, why, tc.want)
			}
		})
	}
}

// meetings is the reviewer's two-item morning of Monday 2 November.
func meetings() Facts {
	return Facts{Intent: RecallFound, Language: classify.LanguageES, Now: time.Date(2026, 11, 2, 9, 0, 0, 0, art),
		Message: "¿qué tengo esta semana?", Items: []Item{
			{Text: "Dentista", Type: "event", At: at(2026, 11, 3, 10, 0)},
			{Text: "Reunión con Ana", Type: "event", At: at(2026, 11, 5, 15, 30)},
		}}
}

const meetingsDraft = "Encontré 2 cosas:\n• Dentista — mar 3 nov, 10:00\n• Reunión con Ana — jue 5 nov, 15:30"

// TestCheck_HonestWordingsAreSentInAnyOrder is the reviewer's thirteen honest
// two-item wordings, in slots: the day may come before its item or after it.
func TestCheck_HonestWordingsAreSentInAnyOrder(t *testing.T) {
	t.Parallel()
	const d, r = "el Dentista", "la Reunión con Ana"
	const tue, thu = "mañana a las 10:00", "el jueves 5 de noviembre a las 15:30"
	for _, tc := range []struct{ out, want string }{
		{"Tenés el {1} {1.when} y la {2} {2.when}.", "Tenés " + d + " " + tue + " y " + r + " " + thu + "."},
		{"Tenés el {1} {1.when}. La {2} es {2.when}.", "Tenés " + d + " " + tue + ". La Reunión con Ana es " + thu + "."},
		{"{1.when}, {1}.\n{2.when}, {2}.", "Mañana a las 10:00, Dentista.\nEl jueves 5 de noviembre a las 15:30, Reunión con Ana."},
		{"Tenés {count} cosas: el {1} {1.when} y la {2} {2.when}.", "Tenés 2 cosas: " + d + " " + tue + " y " + r + " " + thu + "."},
		{"{1.when} el {1}, y {2.when} la {2}.", "Mañana a las 10:00 " + d + ", y " + thu + " " + r + "."},
		{"{1.when} tenés el {1} y {2.when}, la {2}.", "Mañana a las 10:00 tenés " + d + " y " + thu + ", " + r + "."},
		{"{1.when} te toca el {1}, y {2.when} la {2}.", "Mañana a las 10:00 te toca " + d + ", y " + thu + " " + r + "."},
		{"Lo primero es {1.when}, el {1}; después, {2.when}, la {2}.", "Lo primero es " + tue + ", " + d + "; después, " + thu + ", " + r + "."},
		{"Tenés el {1} {1.when}, y {2.when} te espera la {2}.", "Tenés " + d + " " + tue + ", y " + thu + " te espera " + r + "."},
		{"Tenés {count} cosas:\n• {1}, {1.when}\n• {2}, {2.when}", "Tenés 2 cosas:\n• Dentista, " + tue + "\n• Reunión con Ana, " + thu},
		{"{2.when} tenés la {2}, y antes, {1.when}, el {1}.", "El jueves 5 de noviembre a las 15:30 tenés " + r + ", y antes, " + tue + ", " + d + "."},
		{"Esto es lo que encontré: el {1} {1.when} y la {2} {2.when}.", "Esto es lo que encontré: " + d + " " + tue + " y " + r + " " + thu + "."},
		{"Para {1.when} tenés el {1}, y para {2.when}, la {2}.", "Para " + tue + " tenés " + d + ", y para " + thu + ", " + r + "."},
	} {
		if got, why := Check(meetings(), meetingsDraft, tc.out); why != Accepted || got != tc.want {
			t.Errorf("Check(%q) = %q, %q; want %q", tc.out, got, why, tc.want)
		}
	}
}

// TestCheck_AFactCanOnlyComeFromItsSlot: a date, a count or an item the model
// writes itself is not sent, nor is a slot used out of its place (I56).
func TestCheck_AFactCanOnlyComeFromItsSlot(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		out  string
		want Rejection
	}{
		{"Tenés el {1} el jueves 5 a las 15:30 y la {2} {2.when}.", RejectFact},   // a date written
		{"Tenés 3 cosas: el {1} {1.when} y la {2} {2.when}.", RejectFact},         // a count written
		{"Tenés tres cosas: el {1} {1.when} y la {2} {2.when}.", RejectFact},      // a count in words
		{"Tenés el {1} {1.when} y la {2} {2.when}, hoy.", RejectFact},             // a relative day
		{"Tenés el {1} {1.when} y la {2} {2.when} a la tarde.", RejectFact},       // a time of day
		{"Tenés el {1} {1.when} y la {2} {2.when}, en noviembre.", RejectFact},    // a month
		{"Tenés el {1} el {1.when} y la {2} {2.when}.", RejectSlot},               // an article before a day
		{"Tenés el {1} {2.when} y la {2} {1.when}.", RejectSlot},                  // swapped
		{"Tenés el {1} {1.when} y la {2} {2.when}, y todo {2.when}.", RejectSlot}, // a borrowed day
		{"Tenés el {1} {1.when}.", RejectSlot},                                    // an item left out
		{"Tenés el {1} {1.when}, la {2} y la {3} {2.when}.", RejectSlot},          // no such item
		{"Tenés el {1} {1.time} y la {2} {2.when}.", RejectSlot},                  // no such slot
		{"Tenés el {1} {1.when} y la {2} {2.when} {count} {count}.", RejectSlot},
		{"Tenés el {1} {1.when} la {2} {2.when}.", RejectSlot},          // two items run together
		{"Tenés el {1}, {2.when}, y la {2}, {1.when}.", RejectSlot},     // swapped, with commas
		{"{1.when} y la {2} {2.when}.", RejectSlot},                     // an item's words left out
		{"Tenés el {1} {1.when} y la {2} {2.when}}.", RejectSlot},       // a stray brace
		{"Tenés el {1} {1.when} y la {2} {2.when}, y {3}.", RejectSlot}, // an item past the last
		{"Tenés el {1} {1.when} y la {2} {2.when}, pero mañana.", RejectFact},
		{"Tenés el {1}. El {1.when} y la {2} {2.when}.", RejectSlot},
		{"Tenés el {1} {1.when} y la {2} {2.when}, y también el gimnasio.", RejectWord},
		{"No tenés {1} {1.when}, ni {2} {2.when}.", RejectWord},        // a negation the facts do not hold
		{"No hay nada: ni {1} {1.when}, ni {2} {2.when}.", RejectWord}, // the same
		{"Tenés una cosa: {1} {1.when}, y {2} {2.when}.", RejectWord},  // one, for two items
		{"Tenés un turno: {1} {1.when} y {2} {2.when}.", RejectWord},   // the same
		{"Tenés el {1} {1.when} y la {2} {2.when} con Marta.", RejectWord},
	} {
		if got, why := Check(meetings(), meetingsDraft, tc.out); why != tc.want || got != meetingsDraft {
			t.Errorf("Check(%q) = %q, %q; want the draft and %q", tc.out, got, why, tc.want)
		}
	}
	en := meetings()
	en.Language = classify.LanguageEN
	for _, out := range []string{"You have no {1} {1.when}, and no {2} {2.when}.", "Just a thing: {1} {1.when} and {2} {2.when}."} {
		if got, why := Check(en, "Found 2 things", out); why != RejectWord || got != "Found 2 things" {
			t.Errorf("Check(%q) = %q; want the draft and %q", out, why, RejectWord)
		}
	}
	none := Facts{Intent: RecallNone, Language: classify.LanguageES, Now: now, Message: "¿tengo algo el 26?"}
	for _, out := range []string{"No tenés nada el 26.", "Hoy no tenés nada.", "No, pero tenés una reunión con Marta.", "No encontré {1}.",
		// QA's affirmative wordings of an empty recall: with no {none}, or beside it.
		"Encontré una cosa sobre eso.", "Encontré algo sobre eso.", "Sí, {none}, pero encontré algo.", "{none}, pero tenés algo.",
		"{none}, pero hay algo.", "Sí, hay una cosa pendiente. {none}.", "Anotado, ya está agendado: {none}.", "{none} {none}.",
		"{none}, pero el {count}."} {
		if got, _ := Check(none, "No encontré nada.", out); got != "No encontré nada." {
			t.Errorf("an empty recall sent %q", got)
		}
	}
}

func TestCheck_AWordingOfTheRightShapeIsSent(t *testing.T) {
	t.Parallel()
	tomorrow := turno()
	tomorrow.Items[0].At = at(2026, 10, 31, 8, 30)
	dayAlone := turno()
	dayAlone.Items[0].At, dayAlone.Items[0].DayOnly = at(2027, 1, 4, 0, 0), true
	one := turno()
	one.Items[0].At = at(2026, 11, 26, 1, 5)
	en := turno()
	en.Language, en.Items[0].Text = classify.LanguageEN, "Flight to Lisbon"
	pedro := turno()
	pedro.Items[0].Text, pedro.Items[0].At = "Pedro cumple años", at(2026, 10, 31, 13, 0)
	kept := Facts{Intent: RecallNone, Language: classify.LanguageES, Now: now, Keep: []string{"(Si fue un error, decime «no, seguí».)"}}
	none := Facts{Intent: RecallNone, Language: classify.LanguageES, Now: now, Message: "¿qué sé del gastroenterólogo?"}
	noneEN := Facts{Intent: RecallNone, Language: classify.LanguageEN, Now: now, Message: "what about the dentist?"}
	client := turno()
	client.Message = "cuando tengo turno al gastroenterologo?"
	for _, tc := range []struct {
		f                Facts
		draft, out, want string
	}{
		{tomorrow, turnoDraft, "{1.when} tenés el {1}.", "Mañana a las 8:30 tenés el turno para estudios con el gastroenterólogo."},
		{dayAlone, turnoDraft, "Tenés el {1} {1.when}.", "Tenés el turno para estudios con el gastroenterólogo el lunes 4 de enero de 2027."},
		{one, turnoDraft, "{1}: {1.when}.", "Turno para estudios con el gastroenterólogo: el jueves 26 de noviembre a la 1:05."},
		{en, "Found 1 thing", "Your {1} is {1.when}.", "Your Flight to Lisbon is on Thursday 26 November at 8:30."},
		{turno(), "Encontré 1 cosa.", "Tenés el {1} {1.when}, para los estudios.", "Tenés el turno para estudios con el gastroenterólogo el jueves 26 de noviembre a las 8:30, para los estudios."},
		{turno(), turnoDraft, "«{1}» {1.when}, para «estudios»", "«Turno para estudios con el gastroenterólogo» el jueves 26 de noviembre a las 8:30, para «estudios»"},
		{kept, "Listo.", "Mirá, {none}. (Si fue un error, decime «no, seguí».)", "Mirá, no encontré nada. (Si fue un error, decime «no, seguí».)"},
		{pedro, turnoDraft, "Recordá: {1.when}, {1}.", "Recordá: mañana a las 13:00, Pedro cumple años."},
		{turno(), turnoDraft, "Tenés una cosa: el {1} {1.when}.", "Tenés una cosa: el turno para estudios con el gastroenterólogo el jueves 26 de noviembre a las 8:30."},
		{none, "No encontré nada.", "{none} sobre el gastroenterólogo.", "No encontré nada sobre el gastroenterólogo."},
		{none, "No encontré nada.", "Che, por ahora {none} acerca de ese tema.", "Che, por ahora no encontré nada acerca de ese tema."},
		{none, "No encontré nada.", "Todavía {none}.", "Todavía no encontré nada."},
		{none, "No encontré nada.", "No, {none}.", "No, no encontré nada."},
		{noneEN, "I found nothing.", "Sorry, {none} about the dentist.", "Sorry, I found nothing about the dentist."},
		{client, turnoDraft, "Tenés el {1} {1.when}.", "Tenés el turno para estudios con el gastroenterólogo el jueves 26 de noviembre a las 8:30."},
	} {
		if got, why := Check(tc.f, tc.draft, tc.out); why != Accepted || got != tc.want {
			t.Errorf("Check(%q) = %q, %q; want %q", tc.out, got, why, tc.want)
		}
	}
}

func TestVocabularyCoversEveryLanguage(t *testing.T) {
	t.Parallel()
	for _, l := range classify.AllLanguages() {
		v, ok := vocabularies[l]
		if !ok {
			t.Fatalf("no voice vocabulary for %q", l)
		}
		if v.onDate == "" || v.at == "" || len(v.labels) != 4 || len(v.frame) == 0 || len(v.forbidden) == 0 || phrase.For(l).LanguageName == "" {
			t.Errorf("the %q vocabulary is incomplete: %+v", l, v)
		}
		for i, w := range v.weekdays {
			if w == "" {
				t.Errorf("%q weekday %d has no name", l, i)
			}
		}
	}
	// A language outside the table words its facts in the fallback's.
	if got := vocabularyFor("xx"); got.onDate != vocabularies[classify.Fallback()].onDate {
		t.Error("an unknown language does not fall back")
	}
}

// TestTheClientsBounds pins the client's 2026-10-30 answers: a wait of up to 5 s,
// one or two sentences, and a length bound that is exactly where it says.
func TestTheClientsBounds(t *testing.T) {
	t.Parallel()
	if TimeoutSeconds != 5 || MaxSentences != 2 {
		t.Errorf("TimeoutSeconds, MaxSentences = %d, %d; the client chose 5 and 2", TimeoutSeconds, MaxSentences)
	}
	f := Facts{Intent: RecallNone, Language: classify.LanguageES, Now: now}
	draft := "No encontré nada."
	limit := max(MinRunes, RunesFactor*len([]rune(draft)))
	word := func(n int) string { // filled, n runes: "No encontré nada" (16), then " a" or " che" and " a"s, then "."
		if n%2 == 1 {
			return "{none}" + strings.Repeat(" a", (n-17)/2) + "."
		}
		return "{none} che" + strings.Repeat(" a", (n-21)/2) + "."
	}
	if _, why := Check(f, draft, word(limit)); why != Accepted {
		t.Errorf("a wording of exactly %d runes was not sent: %q", limit, why)
	}
	if _, why := Check(f, draft, word(limit+1)); why != RejectLength {
		t.Errorf("a wording of %d runes was sent: %q", limit+1, why)
	}
	long := strings.Repeat("x", MinRunes)
	if _, why := Check(f, long, word(RunesFactor*MinRunes)); why != Accepted {
		t.Errorf("a wording twice a long draft was not sent: %q", why)
	}
}
