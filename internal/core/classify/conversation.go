package classify

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"
)

// ConversationTurns bounds the recent chat a message is read in: the chat's
// last twenty turns, whatever their age (doc 02 §13, ADR-0041, which says why).
const ConversationTurns = 20

// MaxConversationItems is the most items a transcript tags, and so the last
// letter a referent may name.
const MaxConversationItems = ConversationTurns

// MaxTurnRunes is the longest a turn is shown, and the longest clarifying
// question Nooma sends (doc 02 §13).
const MaxTurnRunes = 280

// ReferentUnclear is the wire word for "the message corrects a listed item
// and I cannot tell which".
const ReferentUnclear = "unclear"

// Speaker is who said a turn; push is Nooma speaking first.
type Speaker string

const (
	SpeakerUser  Speaker = "user"
	SpeakerNooma Speaker = "nooma"
	SpeakerPush  Speaker = "push"
)

// AllSpeakers returns a fresh slice of the Speaker vocabulary.
func AllSpeakers() []Speaker { return []Speaker{SpeakerUser, SpeakerNooma, SpeakerPush} }

// Turn is one turn of the recent conversation. About holds the 1-based
// positions, in Conversation.Items, of the items the turn concerns: none for
// a remark, one for a capture, several for a digest.
type Turn struct {
	At      time.Time
	Speaker Speaker
	Text    string
	About   []int
}

// ConversationItem is an item a turn concerns, as it stands now: an earlier
// turn may state a value that is no longer true.
type ConversationItem struct {
	Content string
	Title   string
	EventAt *time.Time
	DueAt   *time.Time
}

// Conversation is the recent chat a message is read in, oldest turn first
// (ADR-0036). The zero value is no conversation.
type Conversation struct {
	Turns []Turn
	Items []ConversationItem
}

// Referent is which listed item a correction patches: a position in
// Conversation.Items (1 is the letter A), or Unclear, when Position is 0.
type Referent struct {
	Position int
	Unclear  bool
}

// ReferentLetter is the letter the prompt shows for a position, 1 being A.
// Letters, not numbers: the reminder lists are numbered, and a bare "2" must
// never read as an item of the conversation.
func ReferentLetter(position int) string { return string(rune('A' + position - 1)) }

// FollowUp is what a follow-up reading asks the code to do: act on the item
// at Position (correct it, or, when Done, close it), or send Question and
// store nothing.
type FollowUp struct {
	Position int
	Done     bool
	Question string
}

// ReadFollowUp decides whether c is a follow-up the code acts on, given how
// many items the prompt showed: a correction, a task_checkin_outcome done of
// any type (ADR-0040), or a remind_request of any type (ADR-0044), naming a
// letter inside the list, or unclear with a question. A correction that also
// carries done stays a correction. Anything else takes the path it took
// before.
func ReadFollowUp(c Classification, shown int) (FollowUp, bool) {
	correction := c.Kind != nil && *c.Kind == KindCorrection
	done := c.TaskCheckinOutcome != nil && *c.TaskCheckinOutcome == TaskCheckinOutcomeDone
	remind := c.RemindRequest != nil && *c.RemindRequest
	if (!correction && !done && !remind) || c.Referent == nil {
		return FollowUp{}, false
	}
	if c.Referent.Unclear {
		if c.ClarifyingQuestion == nil || shown == 0 {
			return FollowUp{}, false
		}
		return FollowUp{Question: *c.ClarifyingQuestion}, true
	}
	if c.Referent.Position < 1 || c.Referent.Position > shown {
		return FollowUp{}, false
	}
	return FollowUp{Position: c.Referent.Position, Done: done && !correction}, true
}

// assignReferent reads referent: a capital letter up to the last item a
// transcript can tag, or the word unclear.
func assignReferent(raw json.RawMessage, c *Classification, _ time.Time) Reason {
	var s *string
	if err := json.Unmarshal(raw, &s); err != nil || s == nil {
		return ReasonWrongType
	}
	if *s == ReferentUnclear {
		c.Referent = &Referent{Unclear: true}
		return ""
	}
	if len(*s) != 1 || (*s)[0] < 'A' || (*s)[0] > 'Z' {
		return ReasonUnknownEnum
	}
	pos := int((*s)[0]-'A') + 1
	if pos > MaxConversationItems {
		return ReasonBadFormat
	}
	c.Referent = &Referent{Position: pos}
	return ""
}

// assignClarifyingQuestion reads clarifying_question, flattened to one line.
// A blank one, or one longer than MaxTurnRunes, is not a question to send.
func assignClarifyingQuestion(raw json.RawMessage, c *Classification, _ time.Time) Reason {
	var s *string
	if err := json.Unmarshal(raw, &s); err != nil || s == nil {
		return ReasonWrongType
	}
	q := oneLine(*s)
	if q == "" || len([]rune(q)) > MaxTurnRunes {
		return ReasonBadFormat
	}
	c.ClarifyingQuestion = &q
	return ""
}

// assignReminderLeads reads reminder_leads_minutes: a non-empty array of whole
// minutes, each at least one. A value of another shape is wrong_type; an empty
// array or a lead under a minute is bad_format.
func assignReminderLeads(raw json.RawMessage, c *Classification, _ time.Time) Reason {
	var items []json.RawMessage
	if err := json.Unmarshal(raw, &items); err != nil {
		return ReasonWrongType
	}
	if len(items) == 0 {
		return ReasonBadFormat
	}
	leads := make([]int, len(items))
	for i, item := range items {
		m, err := strconv.Atoi(strings.TrimSpace(string(item)))
		if err != nil {
			return ReasonWrongType
		}
		if m < 1 {
			return ReasonBadFormat
		}
		leads[i] = m
	}
	c.ReminderLeadsMinutes = leads
	return ""
}

// assignReminderLeadsAdd reads reminder_leads_add: a boolean, wrong_type
// otherwise.
func assignReminderLeadsAdd(raw json.RawMessage, c *Classification, _ time.Time) Reason {
	var b bool
	if err := json.Unmarshal(raw, &b); err != nil {
		return ReasonWrongType
	}
	c.ReminderLeadsAdd = b
	return ""
}

// writeFollowUps renders the two follow-up fields, the patch rule, the
// transcript and the items it tags (ADR-0036). It is written only beside a
// chat: without one there is nothing to follow up on. The model reads what
// the message means; the code binds only a letter it showed.
func writeFollowUps(b *strings.Builder, conv Conversation, now time.Time) {
	b.WriteString(followUpsHeading + "\n")
	b.WriteString("  A chat message can follow up on the recent conversation, when one is shown below.\n")
	// **One meaning, every use** (ADR-0040, ADR-0044): the item the message
	// is about. A done names what it closes ("ya lo hice" after the digest
	// left the task open), a remind-me what it reminds of, a check-in answer
	// the push it answers: "recordámelo" after a push, with referent given no
	// meaning beside a remind-me, was read as that push's answer.
	b.WriteString("  referent             the letter of the listed item the message is about, or\n")
	b.WriteString("                       \"" + ReferentUnclear + "\" when it is about one of them and you cannot tell which.\n")
	b.WriteString("                       Never a letter that is not listed. It is given for type\n")
	b.WriteString("                       correction (the item the message changes), beside\n")
	b.WriteString("                       remind_request true (the item the user asks to be reminded\n")
	b.WriteString("                       of), and beside a check-in answer other than snooze, whatever\n")
	b.WriteString("                       the type (the item the message says is done, or the pushed\n")
	b.WriteString("                       item whose check-in it answers).\n")
	b.WriteString("                       Leave it out for everything else, and for a correction of\n")
	b.WriteString("                       something not listed. An answer to Nooma's which-item\n")
	b.WriteString("                       question names its referent as said below\n")
	b.WriteString("  clarifying_question  only beside referent \"" + ReferentUnclear + "\": one short question, in the\n")
	b.WriteString("                       message's language, asking whether the user means the item\n")
	b.WriteString("                       you think most likely, named as the user would (\"¿Te referís\n")
	b.WriteString("                       a la reunión con Juan?\"). Nooma sends it as it is and stores\n")
	b.WriteString("                       nothing; the answer comes back with this exchange shown\n")
	// **A listed item's leads** (I40, ADR-0038): "avisame solo 1 hora antes"
	// was read as a rewrite of the item's text and answered "Corregido." while
	// nothing about its reminders changed. The field itself is in Reminders.
	b.WriteString("  Beside a referent, reminder_leads_minutes says when to be reminded of that listed\n")
	b.WriteString("  item.\n")
	// **Add or replace, read by the model** (ADR-0045, ADR-0033): "también 1
	// hora antes" replaced the item's reminders with that one.
	b.WriteString("  reminder_leads_add   only beside a referent and reminder_leads_minutes: true when\n")
	b.WriteString("                       the message adds those leads to the reminders the item\n")
	b.WriteString("                       already has; false or left out when they are all the\n")
	b.WriteString("                       reminders the user wants for it, which replace the ones it has.\n")
	// Review on #383: "avisame 2 horas antes" came back adding.
	b.WriteString("                       A message that only names leads, with nothing saying they\n")
	b.WriteString("                       come in addition, names them all: false (\"avisame 2 horas\n")
	b.WriteString("                       antes\")\n")
	b.WriteString("  A follow-up that changes a listed item (a new time, a new day, a different\n")
	b.WriteString("  detail, even with no subject named in it) is a correction of that item, not a\n")
	b.WriteString("  new memory.\n")
	// **The which-item answer, defined here and only here.** Every other
	// section that could claim it carries whichItemPointer instead.
	b.WriteString("  Nooma's which-item question is a question Nooma asked in the recent\n")
	b.WriteString("  conversation about which listed item the user means. An answer to it is read\n")
	b.WriteString("  here, and only here:\n")
	b.WriteString("    when it confirms or names a listed item: type correction, referent that item,\n")
	b.WriteString("    and the change the user asked for in the message the question was about;\n")
	b.WriteString("    but when that message said the item is done: type chitchat, referent that item,\n")
	b.WriteString("    and task_checkin_outcome done;\n")
	b.WriteString("    when it says it is none of them: type chitchat, no referent; it changes nothing.\n")
	b.WriteString("  Apart from that done, it carries no task_checkin_outcome and no nudge_outcome.\n")
	b.WriteString("  Apart from that answer, a message that tells something new, asks a question, or\n")
	b.WriteString("  only answers something else Nooma said is not a correction, even when it is\n")
	b.WriteString("  about the same subject as a listed item.\n")
	// **A check-in answer stays one.** A pushed reminder is listed, and a
	// short reply to it is the check-in's answer, as it is with no list.
	b.WriteString("  A short reply to a reminder, a check-in or a nudge Nooma pushed that only says\n")
	b.WriteString("  it is done, agrees or declines is not a correction: the check-in and nudge\n")
	b.WriteString("  rules above still apply to it.\n")
	// **The latest exchange, not the open check-in** (ADR-0044): with a push
	// earlier that morning, "recordámelo" right after a new task answered the
	// push.
	b.WriteString("  A message answers the latest exchange it follows. A short reply that names no\n")
	b.WriteString("  item is about the item the newest turns are about: it answers a pushed\n")
	b.WriteString("  check-in only while that push is the latest exchange, and once the user has\n")
	b.WriteString("  told Nooma something newer, it is about that newer item and answers no check-in\n")
	b.WriteString("  of the earlier push.\n")
	b.WriteString("  A message that only asks to be reminded, naming nothing, asks it of the item it\n")
	b.WriteString("  follows: remind_request true and that item's referent, never a chitchat.\n")
	// **An event is done too** (ADR-0046): "listo, ya fui" after an event
	// told today closed nothing, and its rings still went off.
	b.WriteString("  A message that says a listed item is done, a task finished or an event that\n")
	b.WriteString("  already took place, answers task_checkin_outcome done and that item's\n")
	b.WriteString("  referent: Nooma closes it.\n")
	// **Asked, not guessed** (review on #383): "ya está" after a digest of two
	// tasks named none, and Nooma answered there was nothing to close.
	b.WriteString("  When the message could be about more than one listed item and does not say\n")
	b.WriteString("  which, referent \"" + ReferentUnclear + "\" and the clarifying_question, still with\n")
	b.WriteString("  task_checkin_outcome done: Nooma asks.\n")
	b.WriteString("  When referent names an item, the correction is a PATCH of that item: change\n")
	b.WriteString("  only what the user speaks about and keep everything else.\n")
	b.WriteString("  normalized_content is the item's WHOLE corrected text — its current text with\n")
	b.WriteString("  only that change applied, never the message on its own.\n")
	b.WriteString("  If the day or the time changes, answer event_at or due_at, the one the item\n")
	b.WriteString("  has, with the complete new instant: keep the item's date when only the time\n")
	b.WriteString("  changes, and its time when only the date changes. An instant shown as a bare\n")
	b.WriteString("  date has no time: when only its day changes, answer the new bare date. Also\n")
	b.WriteString("  answer a fresh title, with no date, day or time in it.\n")
	b.WriteString("  A message that only changes when to be reminded keeps normalized_content the\n")
	b.WriteString("  item's current text and answers no event_at, due_at or title.\n")

	b.WriteString("\n" + conversationHeading + "\n")
	// **The last turns, not the moment before** (ADR-0042): the chat has no
	// time bound, so a turn may be days old, and its time says so.
	b.WriteString("  The latest turns of this chat, oldest first, each at the local time it was\n")
	b.WriteString("  said; the oldest may be days before the message. A turn about something Nooma\n")
	b.WriteString("  holds is tagged with its letter; the items follow, as they stand now.\n")
	for _, t := range conv.Turns {
		var tags []string
		for _, pos := range t.About {
			if pos >= 1 && pos <= len(conv.Items) && pos <= MaxConversationItems {
				tags = append(tags, ReferentLetter(pos))
			}
		}
		// **When, so the model can judge how stale a turn is** (ADR-0042):
		// the chat has no time bound (ADR-0041).
		b.WriteString("  " + t.At.In(now.Location()).Format(turnTimeLayout) + " " + speakerLabel(t.Speaker) + ": " + strconv.Quote(clipTurn(t.Text)))
		if len(tags) > 0 {
			b.WriteString(" [" + strings.Join(tags, ", ") + "]")
		}
		b.WriteString("\n")
	}
	for i, item := range conv.Items {
		if i == MaxConversationItems {
			break
		}
		title := "none"
		if item.Title != "" {
			title = strconv.Quote(item.Title)
		}
		// Quoted on one line, so a body's own line breaks cannot pass for a
		// section of this prompt.
		b.WriteString("  " + ReferentLetter(i+1) + ") text (quoted): " + strconv.Quote(item.Content) + "\n")
		b.WriteString("     event_at: " + shownInstant(item.EventAt, now) + ", due_at: " + shownInstant(item.DueAt, now) +
			", title (quoted): " + title + "\n")
	}
	b.WriteString("\n")
}

func speakerLabel(s Speaker) string {
	switch s {
	case SpeakerNooma:
		return "Nooma"
	case SpeakerPush:
		return "Nooma, pushed (a reminder or check-in)"
	}
	return "user"
}

// clipTurn is a turn's text on one line, cut to MaxTurnRunes with an ellipsis.
func clipTurn(s string) string {
	s = oneLine(s)
	if runes := []rune(s); len(runes) > MaxTurnRunes {
		return string(runes[:MaxTurnRunes]) + "…"
	}
	return s
}

// turnTimeLayout is a turn's local time: the calendar's date form, with its
// weekday and the time of day.
const turnTimeLayout = "Mon 2006-01-02 15:04"

const (
	followUpsHeading    = "Follow-ups"
	conversationHeading = "Recent conversation"
)
