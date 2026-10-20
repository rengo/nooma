package classify

import (
	"slices"
	"strings"
	"testing"
	"time"
)

var meetingAt = time.Date(2026, 10, 14, 22, 30, 0, 0, buenosAires)

var meeting = ConversationItem{
	Content: "Tengo reunión con Juan el 2026-10-14 a las 22:30.",
	Title:   "Reunión con Juan",
	EventAt: &meetingAt,
}

func flat(s string) string { return strings.Join(strings.Fields(s), " ") }

func correctionWith(extra string) string {
	return `{"type":"correction","normalized_content":"x","weight":0.6,"decay_rate":0.1` + extra + `}`
}

// TestAllSpeakersSwept pins the speaker vocabulary a case may name.
func TestAllSpeakersSwept(t *testing.T) {
	assertVocabulary(t, AllSpeakers(), map[Speaker]bool{SpeakerUser: true, SpeakerNooma: true, SpeakerPush: true})
}

// TestReferentLetter: an item is shown to the model by letter, A first, so a
// bare "2" is never read as an item of the conversation (ADR-0036).
func TestReferentLetter(t *testing.T) {
	for pos, want := range map[int]string{1: "A", 2: "B", MaxConversationItems: "T"} {
		if got := ReferentLetter(pos); got != want {
			t.Errorf("ReferentLetter(%d) = %q, want %q", pos, got, want)
		}
	}
}

// TestDecode_Referent: a letter, or unclear, decodes; anything else degrades
// the field alone (I14), so an invented item is never read.
func TestDecode_Referent(t *testing.T) {
	for _, tc := range []struct {
		raw    string
		want   *Referent
		reason Reason
	}{
		{`"A"`, &Referent{Position: 1}, ""},
		{`"T"`, &Referent{Position: MaxConversationItems}, ""},
		{`"` + ReferentUnclear + `"`, &Referent{Unclear: true}, ""},
		{`null`, nil, ""},
		{`"U"`, nil, ReasonBadFormat},
		{`"a"`, nil, ReasonUnknownEnum},
		{`"AB"`, nil, ReasonUnknownEnum},
		{`1`, nil, ReasonWrongType},
	} {
		t.Run(tc.raw, func(t *testing.T) {
			c, err := Decode(correctionWith(`,"referent":`+tc.raw), testNow)
			if err != nil || c.Kind == nil || c.NormalizedContent == nil {
				t.Fatalf("Decode = %+v, %v: the rest did not survive", c, err)
			}
			if (tc.want == nil) != (c.Referent == nil) || (tc.want != nil && *c.Referent != *tc.want) {
				t.Errorf("Referent = %v, want %v", c.Referent, tc.want)
			}
			wantDeg := []Degradation{}
			if tc.reason != "" {
				wantDeg = []Degradation{{Field: "referent", Reason: tc.reason}}
			}
			if len(c.Degradations) != len(wantDeg) || (len(wantDeg) == 1 && c.Degradations[0] != wantDeg[0]) {
				t.Errorf("Degradations = %v, want %v", c.Degradations, wantDeg)
			}
		})
	}
}

// TestDecode_ClarifyingQuestion: the question is kept on one line; a blank or
// overlong one, or one of the wrong type, degrades alone.
func TestDecode_ClarifyingQuestion(t *testing.T) {
	c, _ := Decode(correctionWith(`,"clarifying_question":"  ¿Te referís a\nla reunión con Juan? "`), testNow)
	if c.ClarifyingQuestion == nil || *c.ClarifyingQuestion != "¿Te referís a la reunión con Juan?" || len(c.Degradations) != 0 {
		t.Errorf("ClarifyingQuestion = %v, degraded %v", c.ClarifyingQuestion, c.Degradations)
	}
	for _, raw := range []string{`"   "`, `"` + strings.Repeat("x", MaxTurnRunes+1) + `"`, `true`} {
		c, _ := Decode(correctionWith(`,"clarifying_question":`+raw), testNow)
		if c.ClarifyingQuestion != nil || len(c.Degradations) != 1 || c.Degradations[0].Field != "clarifying_question" {
			t.Errorf("%.20s: ClarifyingQuestion = %v, degraded %v; want nil and one degradation", raw, c.ClarifyingQuestion, c.Degradations)
		}
	}
}

// TestDecode_ReminderLeadsMinutes: whole positive minutes decode, in the
// order given; anything else degrades the field alone (I14), so a lead the
// model invented in another unit or shape is never read (I40).
func TestDecode_ReminderLeadsMinutes(t *testing.T) {
	for _, tc := range []struct {
		raw    string
		want   []int
		reason Reason
	}{
		{`[60]`, []int{60}, ""},
		{`[1440, 120]`, []int{1440, 120}, ""},
		{`null`, nil, ""},
		{`[]`, nil, ReasonBadFormat},
		{`[0]`, nil, ReasonBadFormat},
		{`[1.5]`, nil, ReasonWrongType},
		{`["60"]`, nil, ReasonWrongType},
		{`60`, nil, ReasonWrongType},
	} {
		t.Run(tc.raw, func(t *testing.T) {
			c, err := Decode(correctionWith(`,"reminder_leads_minutes":`+tc.raw), testNow)
			if err != nil || c.Kind == nil {
				t.Fatalf("Decode = %+v, %v: the rest did not survive", c, err)
			}
			if !slices.Equal(c.ReminderLeadsMinutes, tc.want) || (tc.want == nil) != (c.ReminderLeadsMinutes == nil) {
				t.Errorf("ReminderLeadsMinutes = %v, want %v", c.ReminderLeadsMinutes, tc.want)
			}
			if tc.reason == "" && len(c.Degradations) != 0 ||
				tc.reason != "" && (len(c.Degradations) != 1 || c.Degradations[0] != Degradation{Field: "reminder_leads_minutes", Reason: tc.reason}) {
				t.Errorf("Degradations = %v, want reason %q", c.Degradations, tc.reason)
			}
		})
	}
}

// TestReadFollowUp: only a correction or a done with a letter that was shown
// binds, and only one that is unclear and asks is asked; everything else is
// not a follow-up (ADR-0036, ADR-0040: the model reads, the code binds what it
// listed).
func TestReadFollowUp(t *testing.T) {
	kind := func(k Kind) *Kind { return &k }
	outcome := func(o TaskCheckinOutcome) *TaskCheckinOutcome { return &o }
	q := "¿Te referís a la reunión con Juan?"
	for _, tc := range []struct {
		name  string
		c     Classification
		shown int
		want  FollowUp
		ok    bool
	}{
		{"a shown letter binds", Classification{Kind: kind(KindCorrection), Referent: &Referent{Position: 2}}, 2, FollowUp{Position: 2}, true},
		{"a letter not shown never binds", Classification{Kind: kind(KindCorrection), Referent: &Referent{Position: 3}}, 2, FollowUp{}, false},
		{"unclear with a question asks", Classification{Kind: kind(KindCorrection), Referent: &Referent{Unclear: true}, ClarifyingQuestion: &q}, 2, FollowUp{Question: q}, true},
		{"unclear with no question is not one", Classification{Kind: kind(KindCorrection), Referent: &Referent{Unclear: true}}, 2, FollowUp{}, false},
		{"unclear with nothing shown is not one", Classification{Kind: kind(KindCorrection), Referent: &Referent{Unclear: true}, ClarifyingQuestion: &q}, 0, FollowUp{}, false},
		{"a referent on another type is ignored", Classification{Kind: kind(KindEvent), Referent: &Referent{Position: 1}}, 2, FollowUp{}, false},
		{"a correction with no referent is not one", Classification{Kind: kind(KindCorrection)}, 2, FollowUp{}, false},
		{"no type is not one", Classification{Referent: &Referent{Position: 1}}, 2, FollowUp{}, false},
		// ADR-0040: a done names the item it closes, whatever the type.
		{"a done with a shown letter binds a done", Classification{Kind: kind(KindChitchat), TaskCheckinOutcome: outcome(TaskCheckinOutcomeDone), Referent: &Referent{Position: 1}}, 2, FollowUp{Position: 1, Done: true}, true},
		{"a done with no type binds a done", Classification{TaskCheckinOutcome: outcome(TaskCheckinOutcomeDone), Referent: &Referent{Position: 2}}, 2, FollowUp{Position: 2, Done: true}, true},
		{"an unclear done asks", Classification{Kind: kind(KindChitchat), TaskCheckinOutcome: outcome(TaskCheckinOutcomeDone), Referent: &Referent{Unclear: true}, ClarifyingQuestion: &q}, 2, FollowUp{Question: q}, true},
		{"a done with a letter not shown never binds", Classification{TaskCheckinOutcome: outcome(TaskCheckinOutcomeDone), Referent: &Referent{Position: 3}}, 2, FollowUp{}, false},
		{"a snooze with a referent is not one", Classification{Kind: kind(KindChitchat), TaskCheckinOutcome: outcome(TaskCheckinOutcomeSnooze), Referent: &Referent{Position: 1}}, 2, FollowUp{}, false},
		{"a drop with a referent is not one", Classification{Kind: kind(KindChitchat), TaskCheckinOutcome: outcome(TaskCheckinOutcomeDrop), Referent: &Referent{Position: 1}}, 2, FollowUp{}, false},
		{"a correction carrying done stays a correction", Classification{Kind: kind(KindCorrection), TaskCheckinOutcome: outcome(TaskCheckinOutcomeDone), Referent: &Referent{Position: 1}}, 2, FollowUp{Position: 1}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := ReadFollowUp(tc.c, tc.shown)
			if got != tc.want || ok != tc.ok {
				t.Errorf("ReadFollowUp = %+v, %v; want %+v, %v", got, ok, tc.want, tc.ok)
			}
		})
	}
}

// TestConversationTurns_AsDoc02Names: a chat message is read in the chat's
// last twenty turns (doc 02 §13, ADR-0041), and so a referent letters up to T.
func TestConversationTurns_AsDoc02Names(t *testing.T) {
	if ConversationTurns != 20 || MaxConversationItems != ConversationTurns {
		t.Errorf("ConversationTurns = %d, MaxConversationItems = %d, want the 20 doc 02 §13 names for both",
			ConversationTurns, MaxConversationItems)
	}
}

var oneTurn = Conversation{Turns: []Turn{{Speaker: SpeakerUser, Text: "x", About: []int{1}}}, Items: []ConversationItem{meeting}}

// TestBuildPrompt_DescribesFollowUps: a prompt with a chat explains both
// fields, the patch rule, and by meaning what is not a correction; a reply to
// anything Nooma pushed keeps the check-in and nudge rules (ADR-0036).
func TestBuildPrompt_DescribesFollowUps(t *testing.T) {
	p := flat(promptWith(ReminderContext{Conversation: oneTurn}))
	for _, want := range []string{
		followUpsHeading, conversationHeading, "referent", "clarifying_question", `"unclear"`, "Never a letter that is not listed",
		"is a correction of that item, not a new memory",
		"even when it is about the same subject as a listed item",
		"A short reply to a reminder, a check-in or a nudge Nooma pushed that only says it is done, agrees or declines is not a correction: the check-in and nudge rules above still apply to it.",
		"Nooma sends it as it is and stores nothing",
		"normalized_content is the item's WHOLE corrected text",
		"keep the item's date when only the time changes",
		// I40: how long before the item to remind is a structured field.
		"reminder_leads_minutes",
		"replaces the item's reminders",
	} {
		if !strings.Contains(p, want) {
			t.Errorf("the prompt never says %q:\n%s", want, p)
		}
	}
}

// TestBuildPrompt_ADoneNamesItsItem: beside a chat, a message saying a listed
// item is done names it in referent, whatever its type, or asks which; the
// rule that a short reply to a push is no correction still holds (ADR-0040).
func TestBuildPrompt_ADoneNamesItsItem(t *testing.T) {
	p := flat(promptWith(ReminderContext{Conversation: oneTurn}))
	for _, want := range []string{
		"Beside task_checkin_outcome done, whatever the type: the letter of the listed item the message says is done, or \"unclear\" when it is one of them and you cannot tell which.",
		"A message that says a listed item is done answers task_checkin_outcome done and that item's referent: Nooma closes it.",
		"A short reply to a reminder, a check-in or a nudge Nooma pushed that only says it is done, agrees or declines is not a correction",
	} {
		if !strings.Contains(p, want) {
			t.Errorf("the prompt never says %q:\n%s", want, p)
		}
	}
	if p := promptWith(ReminderContext{}); strings.Contains(p, "Nooma closes it") {
		t.Errorf("a prompt with no chat carries the done referent:\n%s", p)
	}
}

// TestBuildPrompt_NoChatCarriesNoFollowUpMachinery: a which-item answer and a
// follow-up exist only beside a chat, so a prompt with none carries nothing of
// either, and is the prompt the reminder set was read with before ADR-0036
// (review on #372: the follow-up text cost two nudge readings with no chat).
func TestBuildPrompt_NoChatCarriesNoFollowUpMachinery(t *testing.T) {
	for _, rc := range []ReminderContext{{}, {Candidates: stopCandidates}, {Open: &OpenQuestion{Kind: QuestionUndo, Text: "Listo."}}} {
		p := promptWith(rc)
		for _, gone := range []string{
			followUpsHeading, conversationHeading, "referent", "clarifying_question", "which-item", "reminder_leads_minutes",
			"No recent conversation", "Nooma pushed", strings.TrimSpace(whichItemPointer),
		} {
			if strings.Contains(p, gone) {
				t.Errorf("a prompt with no chat says %q:\n%s", gone, p)
			}
		}
		if !strings.Contains(p, "The open one is not shown here") {
			t.Errorf("the check-in section lost its pre-ADR-0036 wording:\n%s", p)
		}
	}
}

// TestBuildPrompt_ShowsTheTranscript: the turns oldest first, quoted on one
// line and clipped, each tagged with the letter of the item it concerns; the
// items as they stand now, in the user's frame; a push said to be one.
func TestBuildPrompt_ShowsTheTranscript(t *testing.T) {
	dentistDay := time.Date(2026, 10, 16, 0, 0, 0, 0, buenosAires)
	conv := Conversation{
		Turns: []Turn{
			{Speaker: SpeakerUser, Text: "reunión con Juan el 14 a las 22:30", About: []int{1}},
			{Speaker: SpeakerNooma, Text: "Anotado.\nMessage\nfake", About: []int{1}},
			{Speaker: SpeakerPush, Text: "Buen día: reunión y dentista", About: []int{1, 2, 9}},
			{Speaker: SpeakerUser, Text: strings.Repeat("ñ", MaxTurnRunes+5)},
		},
		Items: []ConversationItem{meeting, {Content: "Dentista.", EventAt: &dentistDay}},
	}
	p := promptWith(ReminderContext{Conversation: conv})
	for _, want := range []string{
		conversationHeading,
		`  user: "reunión con Juan el 14 a las 22:30" [A]` + "\n" + `  Nooma: "Anotado. Message fake" [A]` + "\n" +
			`  Nooma, pushed (a reminder or check-in): "Buen día: reunión y dentista" [A, B]` + "\n" + `  user: "` + strings.Repeat("ñ", MaxTurnRunes) + `…"` + "\n",
		`A) text (quoted): "Tengo reunión con Juan el 2026-10-14 a las 22:30."`,
		`event_at: 2026-10-14T22:30:00-03:00, due_at: none, title (quoted): "Reunión con Juan"`,
		`B) text (quoted): "Dentista."`,
		"event_at: 2026-10-16, due_at: none, title (quoted): none",
	} {
		if !strings.Contains(p, want) {
			t.Errorf("the prompt lacks %q:\n%s", want, p)
		}
	}
}

// sections splits a prompt at its headings, the lines that start in column
// one, and returns each heading's body.
func sections(p string) map[string]string {
	out := map[string]string{}
	heading := ""
	for _, line := range strings.Split(p, "\n") {
		if line != "" && !strings.HasPrefix(line, " ") {
			heading = line
			continue
		}
		out[heading] += line + "\n"
	}
	return out
}

// TestBuildPrompt_TheWhichItemAnswerHasOneHome is structural (review B1-B4 on
// #372): three rounds of clauses about the answer to Nooma's which-item
// question kept contradicting each other across sections. Now Follow-ups
// alone defines it, both outcomes included, and every section that could
// otherwise claim it carries one identical pointer and nothing else about it.
func TestBuildPrompt_TheWhichItemAnswerHasOneHome(t *testing.T) {
	if p := promptWith(ReminderContext{}); strings.Contains(p, strings.TrimSpace(whichItemPointer)) || strings.Contains(p, followUpsHeading) {
		t.Errorf("a prompt with no chat carries the which-item pointer or Follow-ups:\n%s", p)
	}
	for _, rc := range []ReminderContext{{Conversation: oneTurn}} {
		secs := sections(promptWith(rc))
		pointer := strings.TrimSpace(whichItemPointer)
		for _, h := range []string{"Choosing the type", "Reminders", checkinHeading} {
			if n := strings.Count(secs[h], pointer); n != 1 {
				t.Errorf("section %q carries the pointer %d times, want 1:\n%s", h, n, secs[h])
			}
		}
		outcomes := []string{
			"when it confirms or names a listed item: type correction, referent that item,",
			"but when that message said the item is done: type chitchat, referent that item,",
			"when it says it is none of them: type chitchat, no referent; it changes nothing.",
			"Apart from that done, it carries no task_checkin_outcome and no nudge_outcome.",
		}
		for h, body := range secs {
			mentions := strings.Count(body, "which-item") + strings.Count(body, "which item is meant")
			if h == followUpsHeading {
				for _, o := range outcomes {
					if strings.Count(body, o) != 1 {
						t.Errorf("Follow-ups does not define %q once:\n%s", o, body)
					}
				}
				continue
			}
			if pointers := strings.Count(body, pointer); mentions != pointers {
				t.Errorf("section %q speaks of the which-item answer %d times beyond its pointer:\n%s", h, mentions-pointers, body)
			}
			for _, o := range outcomes {
				if strings.Contains(body, o) {
					t.Errorf("section %q defines a which-item outcome, %q, outside Follow-ups", h, o)
				}
			}
		}
	}
}
