package classify

import (
	"strings"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/core/unit"
)

var reminderNow = time.Date(2026, 10, 14, 9, 0, 0, 0, buenosAires)

var stopCandidates = []ReminderEntry{
	{Label: "Revisar contrato Acme", When: "mié 14 oct, 22:30"},
	{Label: "Tomar vitamina C", When: "21:00"},
}

func promptWith(rc ReminderContext) string {
	return BuildPrompt("anything", nil, reminderNow, 0.5, rc)
}

func TestRenderReminderList(t *testing.T) {
	got := RenderReminderList(append(stopCandidates, ReminderEntry{Label: "Sin hora"}))
	want := "1) Revisar contrato Acme — mié 14 oct, 22:30\n2) Tomar vitamina C — 21:00\n3) Sin hora\n"
	if got != want {
		t.Errorf("RenderReminderList =\n%q\nwant\n%q", got, want)
	}
	many := make([]ReminderEntry, MaxReminderChoices+2)
	for i := range many {
		many[i] = ReminderEntry{Label: "r", When: "w"}
	}
	if n := strings.Count(RenderReminderList(many), "\n"); n != MaxReminderChoices {
		t.Errorf("a list of %d entries rendered %d lines, want %d", len(many), n, MaxReminderChoices)
	}
	if got := RenderReminderList(nil); got != "" {
		t.Errorf("RenderReminderList(nil) = %q, want empty", got)
	}
}

// TestBuildPromptDescribesReminderFields: the prompt explains every reminder
// field, with examples in both languages, a recall, a third party's reminder,
// reported speech, and "listo" reading none (ADR-0033 point 1).
func TestBuildPromptDescribesReminderFields(t *testing.T) {
	// Flattened: the prompt wraps its sentences, and the phrases below do not.
	p := strings.Join(strings.Fields(promptWith(ReminderContext{})), " ")
	for _, want := range []string{
		"remind_request", "reminder_intent", "question_answer", "choice_number", "reminder_target", "title",
		joinVocabulary(AllReminderIntents()), joinVocabulary(AllQuestionAnswers()), ReminderTargetUnclear,
		"recordame comprar café mañana a las 10", "remind me to call Ana at 5pm",
		"no me lo recuerdes más", "don't remind me anymore",
		"recordame qué hablamos con Juan", "my dentist sent a reminder to confirm",
		"Ana me dijo", "\"listo\"",
	} {
		if !strings.Contains(p, want) {
			t.Errorf("the prompt never says %q:\n%s", want, p)
		}
	}
}

func TestBuildPromptOpenQuestionOnlyWhenPassed(t *testing.T) {
	if p := promptWith(ReminderContext{}); strings.Contains(p, openQuestionHeading) {
		t.Errorf("with no question open the prompt still has %q", openQuestionHeading)
	}
	text := "¿Algún día y hora en particular?"
	p := promptWith(ReminderContext{Open: &OpenQuestion{Kind: QuestionRemindWhen, Text: text}})
	if !strings.Contains(p, openQuestionHeading) || !strings.Contains(p, text) {
		t.Errorf("an open question is not in the prompt:\n%s", p)
	}
	for _, kind := range []ReminderQuestionKind{QuestionRemindWhen, QuestionWhichReminder, QuestionUndo} {
		p := promptWith(ReminderContext{Open: &OpenQuestion{Kind: kind, Text: text}})
		if !strings.Contains(p, "("+string(kind)+")") {
			t.Errorf("the prompt does not say that a %s question is open", kind)
		}
	}
}

func TestBuildPromptCandidateListOnlyWhenGiven(t *testing.T) {
	if p := promptWith(ReminderContext{}); strings.Contains(p, candidatesHeading) {
		t.Errorf("with no candidates the prompt still has %q", candidatesHeading)
	}
	p := promptWith(ReminderContext{Candidates: stopCandidates})
	if !strings.Contains(p, candidatesHeading) || !strings.Contains(p, "1) Revisar contrato Acme — mié 14 oct, 22:30\n2) Tomar vitamina C — 21:00") {
		t.Errorf("the numbered candidates are not in the prompt:\n%s", p)
	}
}

// TestBuildPromptRendersOneListOnly: a which_reminder question open together
// with stop candidates shows the question's list alone, so a position means one
// thing (design D1).
func TestBuildPromptRendersOneListOnly(t *testing.T) {
	options := []ReminderEntry{{Label: "Llamar a Ana", When: "jue 15 oct"}, {Label: "Pagar luz", When: "vie 16 oct"}}
	open := &OpenQuestion{Kind: QuestionWhichReminder, Text: WhichReminderText("¿Cuál?", options)}
	p := promptWith(ReminderContext{Open: open, Candidates: stopCandidates})
	for _, e := range options {
		if !strings.Contains(p, e.Label) {
			t.Errorf("the question's option %q is not in the prompt", e.Label)
		}
	}
	for _, e := range stopCandidates {
		if strings.Contains(p, e.Label) {
			t.Errorf("candidate %q is in the prompt beside the question's list", e.Label)
		}
	}
	if strings.Contains(p, candidatesHeading) {
		t.Errorf("the candidates heading is in a prompt that has a which_reminder list")
	}
	if n := strings.Count(p, "\n1) "); n != 1 {
		t.Errorf("the prompt has %d lists starting at 1), want 1", n)
	}
	// Another kind of open question leaves the candidates alone.
	other := promptWith(ReminderContext{Open: &OpenQuestion{Kind: QuestionUndo, Text: "Listo."}, Candidates: stopCandidates})
	if !strings.Contains(other, "1) Revisar contrato Acme") {
		t.Errorf("an open undo hid the stop candidates")
	}
}

// TestPromptListEqualsTheQuestionText: the list in the prompt and the list in
// the question Nooma sends come from one renderer.
func TestPromptListEqualsTheQuestionText(t *testing.T) {
	list := RenderReminderList(stopCandidates)
	text := WhichReminderText("¿Cuál?", stopCandidates)
	if !strings.HasPrefix(text, "¿Cuál?\n") || !strings.HasSuffix(text, list) {
		t.Errorf("WhichReminderText = %q, want the lead then %q", text, list)
	}
	asQuestion := promptWith(ReminderContext{Open: &OpenQuestion{Kind: QuestionWhichReminder, Text: text}})
	asCandidates := promptWith(ReminderContext{Candidates: stopCandidates})
	for name, p := range map[string]string{"question": asQuestion, "candidates": asCandidates} {
		if n := strings.Count(p, list); n != 1 {
			t.Errorf("the %s prompt holds the rendered list %d times, want once:\n%s", name, n, p)
		}
	}
}

// TestBuildCorrectionPromptCarriesReminderFields: an explicit-referent stop is
// read on the correction path too.
func TestBuildCorrectionPromptCarriesReminderFields(t *testing.T) {
	target := unit.Unit{Content: "Dentista 2026-10-15 10:00"}
	open := &OpenQuestion{Kind: QuestionRemindWhen, Text: "¿Cuándo?"}
	p := BuildCorrectionPrompt("no me lo recuerdes más", target, nil, reminderNow, 0.5,
		ReminderContext{Open: open, Candidates: stopCandidates})
	for _, want := range []string{"reminder_intent", "question_answer", "reminder_target", "remind_request",
		openQuestionHeading, "¿Cuándo?", candidatesHeading, RenderReminderList(stopCandidates), "Correcting this unit"} {
		if !strings.Contains(p, want) {
			t.Errorf("the correction prompt never says %q", want)
		}
	}
}
