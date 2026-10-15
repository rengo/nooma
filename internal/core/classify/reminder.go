package classify

import (
	"strconv"
	"strings"
)

// MaxReminderChoices is how many entries a numbered reminder list holds, and so
// the highest position a choice_number or a reminder_target may name — doc 02
// §13, ADR-0033 point 2.
const MaxReminderChoices = 5

// ReminderIntent is what the message does to a reminder that already exists —
// ADR-0033 point 1: none | stop | keep. keep is read only as the answer to an
// open undo; the code, not the model, decides whether one is open.
type ReminderIntent string

const (
	ReminderIntentNone ReminderIntent = "none"
	ReminderIntentStop ReminderIntent = "stop"
	ReminderIntentKeep ReminderIntent = "keep"
)

// AllReminderIntents returns a fresh slice of the ReminderIntent vocabulary.
func AllReminderIntents() []ReminderIntent {
	return []ReminderIntent{ReminderIntentNone, ReminderIntentStop, ReminderIntentKeep}
}

// QuestionAnswer is how the message answers the reminder question Nooma has
// open — ADR-0033 point 1: none | when | no | choice.
type QuestionAnswer string

const (
	QuestionAnswerNone   QuestionAnswer = "none"
	QuestionAnswerWhen   QuestionAnswer = "when"
	QuestionAnswerNo     QuestionAnswer = "no"
	QuestionAnswerChoice QuestionAnswer = "choice"
)

// AllQuestionAnswers returns a fresh slice of the QuestionAnswer vocabulary.
func AllQuestionAnswers() []QuestionAnswer {
	return []QuestionAnswer{QuestionAnswerNone, QuestionAnswerWhen, QuestionAnswerNo, QuestionAnswerChoice}
}

// ReminderTargetUnclear is the wire word for "the described reminder is not in
// the list".
const ReminderTargetUnclear = "unclear"

// ReminderTarget is which reminder a stop is about: a position in the numbered
// candidate list the prompt carried, or Unclear. Position is 0 when Unclear.
type ReminderTarget struct {
	Position int
	Unclear  bool
}

// ReminderEntry is one line of a numbered reminder list: what the reminder is
// called (its title, else its content) and when, both already rendered by the
// caller in the language the user reads.
type ReminderEntry struct {
	Label string
	When  string
}

// ReminderQuestionKind names a reminder question Nooma can have open.
type ReminderQuestionKind string

const (
	// QuestionRemindWhen asks when to remind, for a request with no instant.
	QuestionRemindWhen ReminderQuestionKind = "remind_when"
	// QuestionWhichReminder asks which of several reminders a stop is about.
	QuestionWhichReminder ReminderQuestionKind = "which_reminder"
	// QuestionUndo is the stop's reply, open for the undo window.
	QuestionUndo ReminderQuestionKind = "undo"
)

// OpenQuestion is the reminder question Nooma has open, exactly as it sent it.
// Text includes the numbered list of a which_reminder question, built with
// WhichReminderText, so the prompt and the message the user read are one text.
type OpenQuestion struct {
	Kind ReminderQuestionKind
	Text string
}

// ReminderContext is what the code puts in front of the model for the reminder
// fields: the open question, and the stop candidates. The zero value is "none
// open, none to stop", which adds nothing to the prompt.
type ReminderContext struct {
	Open       *OpenQuestion
	Candidates []ReminderEntry
}

// RenderReminderList is the one numbered list a prompt or a question carries:
// "1) <label> — <when>", one entry per line (the date left out when an entry
// has none), at most MaxReminderChoices. The prompt and the question Nooma
// sends both call it, so a position the model reads is a position the user saw.
func RenderReminderList(entries []ReminderEntry) string {
	var b strings.Builder
	for i, e := range entries {
		if i == MaxReminderChoices {
			break
		}
		b.WriteString(strconv.Itoa(i+1) + ") " + e.Label)
		if e.When != "" {
			b.WriteString(" — " + e.When)
		}
		b.WriteString("\n")
	}
	return b.String()
}

// WhichReminderText is the which_reminder question Nooma sends, and the text
// the prompt shows the model for it: lead, then the list.
func WhichReminderText(lead string, entries []ReminderEntry) string {
	return lead + "\n" + RenderReminderList(entries)
}
