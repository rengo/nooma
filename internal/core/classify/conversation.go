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
// many items the prompt showed: a correction, or a task_checkin_outcome done
// of any type (ADR-0040), naming a letter inside the list, or unclear with a
// question. A correction that also carries done stays a correction. Anything
// else takes the path it took before.
func ReadFollowUp(c Classification, shown int) (FollowUp, bool) {
	correction := c.Kind != nil && *c.Kind == KindCorrection
	done := c.TaskCheckinOutcome != nil && *c.TaskCheckinOutcome == TaskCheckinOutcomeDone
	if (!correction && !done) || c.Referent == nil {
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
	return FollowUp{Position: c.Referent.Position, Done: !correction}, true
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

// writeFollowUps renders the two follow-up fields, the patch rule, the
// transcript and the items it tags (ADR-0036). It is written only beside a
// chat: without one there is nothing to follow up on. The model reads what
// the message means; the code binds only a letter it showed.
func writeFollowUps(b *strings.Builder, conv Conversation, now time.Time) {
	b.WriteString(followUpsHeading + "\n")
	b.WriteString("  A chat message can follow up on the recent conversation, when one is shown below.\n")
	b.WriteString("  referent             for type correction: the letter of the listed item the\n")
	b.WriteString("                       message changes, or \"" + ReferentUnclear + "\" when it changes one of them and\n")
	b.WriteString("                       you cannot tell which. Never a letter that is not listed.\n")
	// **A done names what it closes** (ADR-0040). "ya lo hice" after the
	// digest named a task left it open: an answer carried no item.
	b.WriteString("                       Beside task_checkin_outcome done, whatever the type: the\n")
	b.WriteString("                       letter of the listed item the message says is done, or\n")
	b.WriteString("                       \"" + ReferentUnclear + "\" when it is one of them and you cannot tell which.\n")
	b.WriteString("                       Leave it out for everything else, and for a correction of\n")
	b.WriteString("                       something not listed. An answer to Nooma's which-item\n")
	b.WriteString("                       question names its referent as said below\n")
	b.WriteString("  clarifying_question  only beside referent \"" + ReferentUnclear + "\": one short question, in the\n")
	b.WriteString("                       message's language, asking whether the user means the item\n")
	b.WriteString("                       you think most likely, named as the user would (\"¿Te referís\n")
	b.WriteString("                       a la reunión con Juan?\"). Nooma sends it as it is and stores\n")
	b.WriteString("                       nothing; the answer comes back with this exchange shown\n")
	// **How long before, as a value** (I40, ADR-0038). "avisame solo 1 hora
	// antes" was read as a rewrite of the item's text and answered "Corregido."
	// while nothing about its reminders changed.
	b.WriteString("  reminder_leads_minutes  only beside a referent: when the message changes how long\n")
	b.WriteString("                       before the item the user wants to be reminded, every lead it\n")
	b.WriteString("                       asks for, in whole minutes before the item: [60] for one hour\n")
	b.WriteString("                       before, [1440, 120] for the day before and two hours before.\n")
	b.WriteString("                       It replaces the item's reminders. Leave it out when the\n")
	b.WriteString("                       message says nothing about when to be reminded\n")
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
	b.WriteString("  A message that says a listed item is done answers task_checkin_outcome done and\n")
	b.WriteString("  that item's referent: Nooma closes it.\n")
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
	b.WriteString("  The chat just before the message, oldest first. A turn about something Nooma\n")
	b.WriteString("  holds is tagged with its letter; the items follow, as they stand now.\n")
	for _, t := range conv.Turns {
		var tags []string
		for _, pos := range t.About {
			if pos >= 1 && pos <= len(conv.Items) && pos <= MaxConversationItems {
				tags = append(tags, ReferentLetter(pos))
			}
		}
		b.WriteString("  " + speakerLabel(t.Speaker) + ": " + strconv.Quote(clipTurn(t.Text)))
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

const (
	followUpsHeading    = "Follow-ups"
	conversationHeading = "Recent conversation"
)
