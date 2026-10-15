package classify

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
