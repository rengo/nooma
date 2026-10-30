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
	out := "Tenés el turno el jueves 26 de noviembre a las 8:30, para los estudios."
	got, why := Check(turno(), turnoDraft, "  «"+out+"»\n")
	if why != Accepted || got != out {
		t.Fatalf("Check = %q, %q; want the reply unquoted and accepted", got, why)
	}
}

func TestCheck_AWordingOfTheWrongShapeSendsTheDraft(t *testing.T) {
	t.Parallel()
	long := "Tenés el turno el jueves 26 de noviembre a las 8:30" + strings.Repeat(", para los estudios", 20) + "."
	tests := []struct {
		name, out string
		facts     func(*Facts)
		want      Rejection
	}{
		{"empty", "   ", nil, RejectEmpty},
		{"an emoji", "Tenés el turno el jueves 26 de noviembre a las 8:30 🙂", nil, RejectEmoji},
		{"markdown", "Tenés el turno el **jueves 26 de noviembre** a las 8:30.", nil, RejectMarkup},
		{"a heading", "# Turno\nEl jueves 26 de noviembre a las 8:30.", nil, RejectMarkup},
		{"json", `{"reply": "el jueves 26 de noviembre a las 8:30"}`, nil, RejectMarkup},
		{"an offer", "Tenés el turno el jueves 26 de noviembre a las 8:30. ¿Querés que te avise?", nil, RejectQuestion},
		{"three sentences", "Lo tengo. Es el jueves 26 de noviembre a las 8:30. Para los estudios.", nil, RejectSentences},
		{"too long", long, nil, RejectLength},
		{"a kept literal dropped", "Tenés el turno el jueves 26 de noviembre a las 8:30.",
			func(f *Facts) { f.Keep = []string{"(Si fue un error, decime «no, seguí».)"} }, RejectKeep},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := turno()
			if tc.facts != nil {
				tc.facts(&f)
			}
			draft := turnoDraft
			if f.Intent == RecallNone {
				draft = "No encontré nada sobre eso."
			}
			got, why := Check(f, draft, tc.out)
			if why != tc.want || got != draft {
				t.Fatalf("Check(%q) = %q, %q; want the draft and %q", tc.out, got, why, tc.want)
			}
		})
	}
}

func TestCheck_AWordingOfTheRightShapeIsSent(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, out string
		facts     func(*Facts)
		draft     string
	}{
		{"quotes inside are not a wrapping pair", "«Turno» el jueves 26 de noviembre a las 8:30, para «estudios»", nil, ""},
		{"a kept literal", "Listo, no te lo recuerdo más. (Si fue un error, decime «no, seguí».)",
			func(f *Facts) {
				f.Intent, f.Items, f.Keep = RecallNone, nil, []string{"(Si fue un error, decime «no, seguí».)"}
			},
			"Listo. (Si fue un error, decime «no, seguí».)"},
		{"nothing found", "No encontré nada sobre eso.",
			func(f *Facts) { f.Intent, f.Items = RecallNone, nil }, "No encontré nada."},
		{"english", "Your gastroenterologist appointment is on Thursday 26 November at 08:30.",
			func(f *Facts) { f.Language = classify.LanguageEN }, ""},
		{"three items, one line each",
			"Tenés tres cosas:\n• El turno, el jueves 26 de noviembre a las 8:30.\n• El dentista, el 3 de diciembre.\n• La reunión, el 4 de diciembre a las 9:15.",
			func(f *Facts) {
				f.Items = append(f.Items,
					Item{Text: "Dentista", Type: "event", At: at(2026, 12, 3, 0, 0), DayOnly: true},
					Item{Text: "Reunión", Type: "event", At: at(2026, 12, 4, 9, 15)})
			}, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := turno()
			if tc.facts != nil {
				tc.facts(&f)
			}
			draft := tc.draft
			if draft == "" {
				draft = turnoDraft
			}
			if got, why := Check(f, draft, tc.out); why != Accepted || got != tc.out {
				t.Fatalf("Check(%q) = %q, %q; want it sent as written", tc.out, got, why)
			}
		})
	}
}

func TestVocabularyCoversEveryLanguage(t *testing.T) {
	t.Parallel()
	for _, l := range classify.AllLanguages() {
		v, ok := vocabularies[l]
		if !ok {
			t.Fatalf("no voice vocabulary for %q", l)
		}
		if v.longDate == "" || len(v.labels) != 4 || phrase.For(l).LanguageName == "" {
			t.Errorf("the %q vocabulary is incomplete: %+v", l, v)
		}
		for i, w := range v.weekdays {
			if w == "" {
				t.Errorf("%q weekday %d has no name", l, i)
			}
		}
	}
	// A language outside the table words its facts in the fallback's.
	if got := vocabularyFor("xx"); got.longDate != vocabularies[classify.Fallback()].longDate {
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
	word := func(n int) string { return "N" + strings.Repeat("o", n-2) + "." }
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
