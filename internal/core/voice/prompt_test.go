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
		`{1} = "Turno para estudios con el gastroenterólogo" (event); {1.when} = "el jueves 26 de noviembre a las 8:30"`,
		"{count} = 1",
		"Never write a date",
		`put its article before the slot ("el {1}", "la {1}")`,
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

func TestBuildPrompt_ShowsWhatEachSlotHolds(t *testing.T) {
	t.Parallel()
	f := turno()
	f.Language = classify.LanguageEN
	f.Items = []Item{
		{Text: "Flight", At: at(2026, 10, 31, 8, 30)},
		{Text: "Renewal", At: at(2027, 1, 4, 0, 0), DayOnly: true},
	}
	f.Intent = RecallNone
	p := BuildPrompt(f, "x")
	for _, want := range []string{`{1} = "Flight"; {1.when} = "tomorrow at 8:30"`, `{2} = "Renewal"; {2.when} = "on Monday 4 January 2027"`, "{count} = 2", "{none}", `"I found nothing"`} {
		if !strings.Contains(p, want) {
			t.Errorf("the prompt lacks %q:\n%s", want, p)
		}
	}
}
