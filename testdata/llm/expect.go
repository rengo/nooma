package llm

// Question is the reminder question Nooma has open when a phrasing is read,
// exactly as it sent it (classify.OpenQuestion, kept as plain strings so this
// package stays free of the core).
type Question struct {
	Kind string `json:"kind"`
	Text string `json:"text"`
}

// Entry is one line of the numbered candidate list a phrasing is read with
// (classify.ReminderEntry).
type Entry struct {
	Label string `json:"label"`
	When  string `json:"when,omitempty"`
}

// Expect is the reading a phrasing should get, written by hand and never by
// the recording command. The reminder fields are strict: an unset one means
// the model must read none, false or absent. The rest are checked only when
// set. internal/reading owns the comparison.
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

	TaskCheckinOutcome string `json:"task_checkin_outcome,omitempty"`
	NudgeOutcome       string `json:"nudge_outcome,omitempty"`
	RelationOutcome    string `json:"relation_outcome,omitempty"`
}
