package classify

import (
	"strings"
	"testing"
)

// section is the part of p from heading up to the next blank line, or "" when
// p has no such heading.
func section(p, heading string) string {
	i := strings.Index(p, "\n"+heading+"\n")
	if i < 0 {
		return ""
	}
	s := p[i+1:]
	if end := strings.Index(s, "\n\n"); end >= 0 {
		s = s[:end]
	}
	return s
}

// TestBuildPromptSaysWhatEachCheckinOutcomeMeans: the prompt that only named
// done | snooze | drop and engaged | declined got no outcome back for a task
// done, put off or a nudge taken up, and M3's check-in resolution reads the
// model alone (link 1b). Each value is stated with what it means, never with
// the words that trigger it, and a value added to either vocabulary fails here
// until it is.
func TestBuildPromptSaysWhatEachCheckinOutcomeMeans(t *testing.T) {
	s := section(promptWith(ReminderContext{}), checkinHeading)
	if s == "" {
		t.Fatalf("the prompt has no %q section", checkinHeading)
	}
	values := append(asStrings(AllTaskCheckinOutcomes()), asStrings(AllNudgeOutcomes())...)
	for _, v := range values {
		var line string
		for _, l := range strings.Split(s, "\n") {
			if f := strings.Fields(l); len(f) > 1 && f[0] == v {
				line = l
			}
		}
		if line == "" {
			t.Errorf("the check-in section does not say what %q means:\n%s", v, s)
			continue
		}
		if strings.Contains(line, "\"") {
			t.Errorf("%q is explained by quoted words, not by what it means: %q", v, line)
		}
	}
	for _, field := range []string{"task_checkin_outcome", "nudge_outcome"} {
		if !strings.Contains(s, field) {
			t.Errorf("the check-in section never names %s", field)
		}
	}
	// Saying a thing is done is not a stop, and it is the check-in's answer.
	flat := strings.Join(strings.Fields(promptWith(ReminderContext{})), " ")
	if !strings.Contains(flat, "is none, not stop: it is task_checkin_outcome done") {
		t.Errorf("the stop rule does not send a thing done to task_checkin_outcome done:\n%s", flat)
	}
}

// TestBuildPromptAWhenAnswerCarriesItsInstant: "el viernes" answering an open
// remind_when came back as question_answer when with the day only in
// normalized_content, so the instant was lost (link 1b). A when answer puts the
// day or time it gives in due_at or event_at, a day alone as a bare date.
func TestBuildPromptAWhenAnswerCarriesItsInstant(t *testing.T) {
	flat := strings.Join(strings.Fields(promptWith(ReminderContext{})), " ")
	for _, want := range []string{
		"when (it gives a day or time: put that instant in due_at, or in event_at for an event",
		"a day with no time as a bare YYYY-MM-DD",
		"normalized_content alone loses it",
	} {
		if !strings.Contains(flat, want) {
			t.Errorf("the prompt never says %q:\n%s", want, flat)
		}
	}
}

// TestBuildPromptWhichAnswerIsNotAStop: "el 2" answering the which-question
// came back as a stop of its own (reminder_intent stop, reminder_target 2): the
// prompt never said the question is asked for a stop Nooma already holds
// (link 1b). With that question open, the prompt says the answer picks the
// reminder, sets only the choice, and is not a stop; ADR-0033 ties a stop to the
// user's words, and the pending stop is what binds.
func TestBuildPromptWhichAnswerIsNotAStop(t *testing.T) {
	const rule = "The answer is not itself a stop"
	which := &OpenQuestion{Kind: QuestionWhichReminder, Text: WhichReminderText("¿De cuál hablás?", stopCandidates)}
	flat := strings.Join(strings.Fields(promptWith(ReminderContext{Open: which})), " ")
	for _, want := range []string{
		"Nooma asked this to learn which reminder a stop the user already asked for is about",
		"set question_answer choice and choice_number, and nothing else",
		rule + ": reminder_intent is none and reminder_target is left out",
	} {
		if !strings.Contains(flat, want) {
			t.Errorf("with a which_reminder question open the prompt never says %q:\n%s", want, flat)
		}
	}
	for _, rc := range []ReminderContext{{}, {Open: &OpenQuestion{Kind: QuestionRemindWhen, Text: "¿Cuándo?"}}, {Candidates: stopCandidates}} {
		if p := promptWith(rc); strings.Contains(p, rule) {
			t.Errorf("the which-answer rule is in a prompt with no which_reminder question open: %+v", rc)
		}
	}
}

// TestBuildPromptNoQuestionOpenMeansNoAnswer: a bare "no" next to a list of
// reminders came back as question_answer no although no question was open
// (link 1b). "Only while a question below is open" asked the model to notice an
// absence; with none open, the prompt now says so, and that question_answer is
// none. With one open, it does not.
func TestBuildPromptNoQuestionOpenMeansNoAnswer(t *testing.T) {
	const rule = "No reminder question is open, so question_answer is none"
	for _, rc := range []ReminderContext{{}, {Candidates: stopCandidates}} {
		if flat := strings.Join(strings.Fields(promptWith(rc)), " "); !strings.Contains(flat, rule) {
			t.Errorf("with no question open the prompt never says %q: %+v", rule, rc)
		}
	}
	for _, kind := range []ReminderQuestionKind{QuestionRemindWhen, QuestionWhichReminder, QuestionUndo} {
		if p := promptWith(ReminderContext{Open: &OpenQuestion{Kind: kind, Text: "¿?"}}); strings.Contains(p, rule) {
			t.Errorf("with a %s question open the prompt says %q", kind, rule)
		}
	}
}
