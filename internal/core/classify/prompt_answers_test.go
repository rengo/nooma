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
