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

// Conversation is the recent chat a phrasing is read in (ADR-0036,
// classify.Conversation): the turns oldest first, and the items they tag.
type Conversation struct {
	Turns []ConversationTurn  `json:"turns"`
	Items []ConversationEntry `json:"items,omitempty"`
}

// ConversationTurn is one turn: when it was said (RFC 3339, at or before the
// case's now), speaker is user, nooma or push (a reminder Nooma sent), and
// about the letters of the items it concerns, if any.
type ConversationTurn struct {
	At      string   `json:"at"`
	Speaker string   `json:"speaker"`
	Text    string   `json:"text"`
	About   []string `json:"about,omitempty"`
}

// ConversationEntry is one item as it stands: its text and title, and its
// instants as RFC 3339 or a bare YYYY-MM-DD (a day with no time, in the
// case's zone).
type ConversationEntry struct {
	Content string `json:"content"`
	Title   string `json:"title,omitempty"`
	EventAt string `json:"event_at,omitempty"`
	DueAt   string `json:"due_at,omitempty"`
}

// The varieties Expect.Voice names.
const (
	VoiceVoseo = "voseo"
	VoiceTuteo = "tuteo"
)

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

	// Referent is the letter of the conversation item a follow-up corrects, or
	// "unclear". It is strict: unset means the model must name none, since a
	// referent corrects a unit in production (ADR-0036). A clarifying question
	// must come back exactly when it is unclear.
	Referent string `json:"referent,omitempty"`
	// Voice is the variety the clarifying question must be in, VoiceVoseo or
	// VoiceTuteo, checked only when set: the question names no form of the
	// other one.
	Voice string `json:"voice,omitempty"`
	// EventAt is the instant a follow-up moves its item to, RFC 3339, checked
	// only when set.
	EventAt string `json:"event_at,omitempty"`
	// DueAt is the instant a task is due, RFC 3339 (a bare date is its local
	// midnight), checked only when set.
	DueAt string `json:"due_at,omitempty"`
	// ReminderLeadsMinutes are the leads a follow-up sets on its item, in
	// order. Strict: unset means none, since leads change reminders (I40).
	ReminderLeadsMinutes []int `json:"reminder_leads_minutes,omitempty"`
	// ReminderLeadsAdd says the follow-up adds its leads to the item's
	// reminders rather than replacing them (ADR-0045). Strict: unset means it
	// must not add.
	ReminderLeadsAdd bool `json:"reminder_leads_add,omitempty"`

	// RecallTerms are words a recall's scope must hold among its terms, case
	// folded (ADR-0048); more are free. RecallWindow is the window it must
	// name. Both are strict beside Type recall, and only there: unset means
	// the reading names none, since what it names narrows the answer.
	RecallTerms  []string `json:"recall_terms,omitempty"`
	RecallWindow *Window  `json:"recall_window,omitempty"`

	// TimerRequest is what the message asks of the live timers, and
	// TimerTarget, "1" to "5" or "unclear", which listed one a cancel names
	// (ADR-0051). Both strict: unset means none, since a list answers and a
	// cancel cancels.
	TimerRequest string `json:"timer_request,omitempty"`
	TimerTarget  string `json:"timer_target,omitempty"`

	// JudgeOutcome is what a relation_evaluation case should store: new,
	// related or duplicate, read by its effect at the default thresholds
	// (internal/reading.ReplayJudge). It is the judge set's only field.
	JudgeOutcome string `json:"judge_outcome,omitempty"`
}

// TimerEntry is one live timer a case's prompt lists, soonest first: what it
// says, empty for a generic nudge, and when it rings, RFC 3339.
type TimerEntry struct {
	Text string `json:"text,omitempty"`
	At   string `json:"at"`
}

// Window is a recall window as a case expects it: on is told or when, and from
// and to are RFC 3339, from inclusive, to exclusive.
type Window struct {
	On   string `json:"on"`
	From string `json:"from"`
	To   string `json:"to"`
}

// FileFields are the keys of a recorded example in the order they are written
// (goldenset.LLMExample's field order). `nooma doctor --record` edits a file by
// key, so a value it does not set, `expect` first of all, is written back byte
// for byte; a key outside this list makes it refuse the file.
var FileFields = []string{
	"id", "provider", "model", "task", "message", "candidates", "response", "error",
	"record", "now", "open_question", "candidates_list", "conversation", "timers_list", "expect", "known_misread", "prompt_sha256",
}
