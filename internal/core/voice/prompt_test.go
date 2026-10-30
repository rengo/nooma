package voice

import (
	"strings"
	"testing"

	"github.com/rengo/nooma/internal/core/classify"
)

func TestBuildPrompt_HandsTheModelTheFactsAndTheDraft(t *testing.T) {
	t.Parallel()
	f := turno()
	f.Keep = []string{"(Si fue un error, decime «no, seguí».)"}
	p := BuildPrompt(f, turnoDraft)
	for _, want := range []string{
		"Spanish",
		"voseo",
		"Turno para estudios con el gastroenterólogo (event) — jueves 26 de noviembre, 08:30",
		`"cuando tengo turno al gastroenterologo?"`,
		turnoDraft,
		"(Si fue un error, decime «no, seguí».)",
		"No emojis",
		"Do not ask a question",
		"found",
	} {
		if !strings.Contains(p, want) {
			t.Errorf("the prompt lacks %q:\n%s", want, p)
		}
	}
	if p != BuildPrompt(f, turnoDraft) {
		t.Error("BuildPrompt is not a function of its arguments")
	}
}

func TestBuildPrompt_NamesTheDayAsTheCodeCountedIt(t *testing.T) {
	t.Parallel()
	f := turno()
	f.Language = classify.LanguageEN
	f.Items = []Item{
		{Text: "Flight", At: at(2026, 10, 31, 8, 30)},
		{Text: "Renewal", At: at(2027, 1, 4, 0, 0), DayOnly: true},
	}
	f.Intent = RecallNone
	p := BuildPrompt(f, "x")
	for _, want := range []string{"Flight — Saturday 31 October, 08:30 — tomorrow", "Renewal — Monday 4 January 2027\n", "nothing"} {
		if !strings.Contains(p, want) {
			t.Errorf("the prompt lacks %q:\n%s", want, p)
		}
	}
}
