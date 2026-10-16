package llm

// Question is the reminder question Nooma has open when a phrasing is read,
// exactly as it sent it (classify.OpenQuestion, kept as plain strings so this
// package stays free of the core).
type Question struct {
	Kind string `json:"kind"`
	Text string `json:"text"`
	// List is the which_reminder question's numbered entries as data, so a test
	// can check the numbered list inside Text against the renderer. The prompt
	// reads Text alone.
	List []Entry `json:"list,omitempty"`
}

// Entry is one line of the numbered candidate list a phrasing is read with
// (classify.ReminderEntry).
//
// When is the text the prompt shows. At and DayOnly are the instant it renders,
// kept so a test can check When against internal/core/phrase and that the
// instant is after the case's now; the prompt never reads them.
type Entry struct {
	Label   string `json:"label"`
	When    string `json:"when,omitempty"`
	At      string `json:"at,omitempty"`
	DayOnly bool   `json:"day_only,omitempty"`
}

// Expect is the reading a phrasing should get, written by hand and never by
// the recording command. The reminder fields and the outcomes are strict: an
// unset one means the model must read none, false or absent. DayOnly, Title
// and Type are checked only when set. internal/reading owns the comparison.
type Expect struct {
	RemindRequest  *bool  `json:"remind_request,omitempty"`
	ReminderIntent string `json:"reminder_intent,omitempty"`
	QuestionAnswer string `json:"question_answer,omitempty"`
	ChoiceNumber   *int   `json:"choice_number,omitempty"`
	// ReminderTarget is "1" to "5" or "unclear".
	ReminderTarget string `json:"reminder_target,omitempty"`

	// DayOnly says whether the instant should come back as a bare date.
	DayOnly *bool `json:"day_only,omitempty"`
	// Title says whether a valid, date-free title should come back.
	Title *bool `json:"title,omitempty"`

	// Type is the classify type the phrasing should get, checked only when set:
	// a reply that answers Nooma is not a new unit.
	Type string `json:"type,omitempty"`

	TaskCheckinOutcome string `json:"task_checkin_outcome,omitempty"`
	NudgeOutcome       string `json:"nudge_outcome,omitempty"`
	RelationOutcome    string `json:"relation_outcome,omitempty"`
}

// FileFields are the keys of a recorded example in the order they are written
// (goldenset.LLMExample's field order). `nooma doctor --record` edits a file by
// key, so a value it does not set, `expect` first of all, is written back byte
// for byte; a key outside this list makes it refuse the file.
var FileFields = []string{
	"id", "provider", "model", "task", "message", "candidates", "response", "error",
	"record", "now", "open_question", "candidates_list", "expect", "known_misread", "prompt_sha256",
}
