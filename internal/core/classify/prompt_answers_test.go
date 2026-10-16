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
}

// TestBuildPromptDoneIsAMeaningAndAStopAnswersNoCheckin: link 1b's first
// prompt listed "listo, nos vemos mañana" under saying a thing is done and sent
// it to task_checkin_outcome done, and the model obeyed: a farewell, the very
// reading ADR-0033's context names a misread, closed the newest check-in, and
// "listo, no me lo recuerdes más" read a done beside its stop. Done is stated
// by what it means, with no example, and a stop answers no check-in.
func TestBuildPromptDoneIsAMeaningAndAStopAnswersNoCheckin(t *testing.T) {
	p := promptWith(ReminderContext{Candidates: stopCandidates})
	if strings.Contains(p, "nos vemos") {
		t.Errorf("the prompt still carries the farewell ADR-0033 names as misread as done")
	}
	flat := strings.Join(strings.Fields(p), " ")
	for _, want := range []string{
		"Saying a task is finished is none, not stop: it is task_checkin_outcome done.",
		"A stop is not also a check-in answer: give it no task_checkin_outcome and no nudge_outcome.",
	} {
		if !strings.Contains(flat, want) {
			t.Errorf("the prompt never says %q:\n%s", want, flat)
		}
	}
	i := strings.Index(flat, "Saying a task is finished")
	if i < 0 {
		return
	}
	done := flat[i:]
	if done = done[:strings.Index(done, ".")]; strings.Contains(done, "\"") {
		t.Errorf("done is explained by quoted words, not by what it means: %q", done)
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

// TestBuildPromptListedRemindersAreNotAStop: the stop candidates are shown so
// a stop can name one, and a bare "listo" with that list and no check-in open
// must still not be a stop (the done descope, ADR-0033). The list says it is
// not a request to stop anything; it is said only where the list is.
func TestBuildPromptListedRemindersAreNotAStop(t *testing.T) {
	const rule = "Being listed here is not a request to stop"
	s := section(promptWith(ReminderContext{Candidates: stopCandidates}), candidatesHeading)
	if flat := strings.Join(strings.Fields(s), " "); !strings.Contains(flat, rule+
		": only a message that asks to stop being reminded is a stop") {
		t.Errorf("the candidate list never says %q:\n%s", rule, s)
	}
	which := &OpenQuestion{Kind: QuestionWhichReminder, Text: WhichReminderText("¿Cuál?", stopCandidates)}
	for _, rc := range []ReminderContext{{}, {Open: which, Candidates: stopCandidates}} {
		if p := promptWith(rc); strings.Contains(p, rule) {
			t.Errorf("the list rule is in a prompt that shows no candidate list: %+v", rc)
		}
	}
}

// TestBuildPromptAReplyIsNotANewUnit: a bare "listo" or "ya" came back as a
// task, and capture stored "La tarea está hecha" as a new unit (review of
// #365). The prompt says what chitchat is: a message that only answers Nooma or
// only talks stores nothing, and its answer travels in the outcome fields.
func TestBuildPromptAReplyIsNotANewUnit(t *testing.T) {
	flat := strings.Join(strings.Fields(promptWith(ReminderContext{})), " ")
	for _, want := range []string{
		"chitchat a message that only answers Nooma (a check-in, a nudge, a question it asked) or only talks, with nothing new to keep",
		"its answer travels in the outcome fields, never as a new task",
	} {
		if !strings.Contains(flat, want) {
			t.Errorf("the prompt never says %q", want)
		}
	}
}

// TestBuildPromptAShortReplyAnswersACheckin: the set's one rule for a bare
// word (format.md): with no reminder question open, a reply that is only a yes,
// a no or a done answers the check-in Nooma asked, so it carries the outcome
// it gives. A bare yes and "dale, contame" read no outcome, and an open nudge
// stayed open (review of #365). Said by meaning, never by the words.
func TestBuildPromptAShortReplyAnswersACheckin(t *testing.T) {
	const rule = "With no reminder question open, a message that is only a short reply (agreeing, declining, or saying it is done) answers the check-in: give the outcome it means"
	s := section(promptWith(ReminderContext{}), checkinHeading)
	if flat := strings.Join(strings.Fields(s), " "); !strings.Contains(flat, rule) {
		t.Errorf("the check-in section never says %q:\n%s", rule, s)
	}
	if strings.Contains(s, "\"") {
		t.Errorf("the check-in section quotes words:\n%s", s)
	}
}

// TestBuildPromptAShortDeclineIsNotYet: the client ruled on a bare "no" to a
// check-in (#365): ask again later, unless the user explicitly refuses. A short
// decline alone is snooze, and only an explicit refusal to do the task, or to
// be asked about it, closes the check-in as drop or declined.
func TestBuildPromptAShortDeclineIsNotYet(t *testing.T) {
	s := section(promptWith(ReminderContext{}), checkinHeading)
	flat := strings.Join(strings.Fields(s), " ")
	for _, want := range []string{
		"A short decline alone means not yet: task_checkin_outcome snooze, and no nudge_outcome",
		"Only an explicit refusal, to do the task or to be asked about it, is drop or declined",
	} {
		if !strings.Contains(flat, want) {
			t.Errorf("the check-in section never says %q:\n%s", want, s)
		}
	}
}
