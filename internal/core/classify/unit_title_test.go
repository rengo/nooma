package classify

import (
	"strings"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/core/unit"
)

// TestTitleRule_IsTheTextBuildPromptCarries: the capture prompt and the naming
// prompt hold one rule, so the two cannot drift (ADR-0065 point 1).
func TestTitleRule_IsTheTextBuildPromptCarries(t *testing.T) {
	rule := TitleRule()
	if rule == "" {
		t.Fatal("TitleRule() is empty")
	}
	prompt := BuildPrompt("recordame la reunión", nil, time.Date(2026, 11, 4, 10, 0, 0, 0, time.UTC), 0.5, ReminderContext{})
	if !strings.Contains(prompt, "title                "+rule+"\n") {
		t.Errorf("the capture prompt does not carry TitleRule() as its title field:\n%s", prompt)
	}
}

func TestBuildTitlePrompt_HoldsTheContentTheLanguageAndTheRule(t *testing.T) {
	u := unit.Unit{ID: "u", Content: "tengo reunión con Juan el 2026-11-04 a las 22:30\nlínea", Language: "es"}

	prompt := BuildTitlePrompt(u)

	for _, want := range []string{TitleRule(), `"tengo reunión con Juan el 2026-11-04 a las 22:30\nlínea"`, "es", `{"title"`} {
		if !strings.Contains(prompt, want) {
			t.Errorf("the naming prompt lacks %q:\n%s", want, prompt)
		}
	}
	if BuildTitlePrompt(u) != prompt {
		t.Error("BuildTitlePrompt is not deterministic")
	}
}

func TestBuildTitlePrompt_AnUnknownLanguageIsSaidSo(t *testing.T) {
	prompt := BuildTitlePrompt(unit.Unit{ID: "u", Content: "x"})
	if !strings.Contains(prompt, "the language of the text") {
		t.Errorf("a unit with no language should ask for the text's own language:\n%s", prompt)
	}
}

func TestDecodeTitle(t *testing.T) {
	for name, tc := range map[string]struct {
		raw    string
		want   string // "" means nil
		reason Reason
	}{
		"a usable title":             {`{"title":"Reunión con Juan"}`, "Reunión con Juan", ""},
		"padded and fenced":          {"```json\n{\"title\":\"  Dentista \"}\n```", "Dentista", ""},
		"an ISO date is dropped":     {`{"title":"Reunión 2026-11-04"}`, "", ReasonBadFormat},
		"an HH:MM is dropped":        {`{"title":"Cena 22:30"}`, "", ReasonBadFormat},
		"too long is dropped":        {`{"title":"` + strings.Repeat("a", MaxTitleRunes+1) + `"}`, "", ReasonBadFormat},
		"null names nothing":         {`{"title":null}`, "", ReasonAbsent},
		"no title key":               {`{"name":"x"}`, "", ReasonAbsent},
		"a number is the wrong type": {`{"title":5}`, "", ReasonWrongType},
		"not JSON":                   {`Reunión con Juan`, "", ReasonAbsent},
		"empty":                      {``, "", ReasonAbsent},
	} {
		t.Run(name, func(t *testing.T) {
			got, reason := DecodeTitle([]byte(tc.raw))
			if reason != tc.reason {
				t.Errorf("reason = %q, want %q", reason, tc.reason)
			}
			switch {
			case tc.want == "" && got != nil:
				t.Errorf("title = %q, want none", *got)
			case tc.want != "" && (got == nil || *got != tc.want):
				t.Errorf("title = %v, want %q", got, tc.want)
			}
		})
	}
}
