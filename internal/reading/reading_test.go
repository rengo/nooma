package reading

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/core/classify"
	"github.com/rengo/nooma/internal/core/consolidation"
	"github.com/rengo/nooma/testdata/llm"
)

const testNow = "2026-10-15T09:30:00-03:00"

func boolp(b bool) *bool { return &b }
func intp(n int) *int    { return &n }

func baseCase() llm.Case {
	return llm.Case{
		ID: "x", Task: "classify", Message: "recordame comprar café mañana a las 10", Now: testNow,
		Expect: &llm.Expect{RemindRequest: boolp(true)},
	}
}

func recorded(t *testing.T, c llm.Case) llm.Case {
	t.Helper()
	p, err := Prompt(c)
	if err != nil {
		t.Fatal(err)
	}
	c.PromptSHA256 = Hash(p)
	return c
}

const readsAsRequest = `{"type":"task","normalized_content":"Comprar café","weight":0.6,"decay_rate":0.1,"remind_request":true}`

func TestPromptSendsNilBeliefs(t *testing.T) {
	c := baseCase()
	c.OpenQuestion = &llm.Question{Kind: "undo", Text: "Listo, lo dejé de recordar."}
	c.CandidatesList = []llm.Entry{{Label: "Café", When: "jue 15 oct"}}
	got, err := Prompt(c)
	if err != nil {
		t.Fatal(err)
	}
	now, _ := time.Parse(time.RFC3339, testNow)
	want := classify.BuildPrompt(c.Message, nil, now, consolidation.DefaultWeightThreshold, classify.ReminderContext{
		Open:       &classify.OpenQuestion{Kind: classify.QuestionUndo, Text: "Listo, lo dejé de recordar."},
		Candidates: []classify.ReminderEntry{{Label: "Café", When: "jue 15 oct"}},
	})
	if got != want {
		t.Fatalf("Prompt is not the production prompt with beliefs nil, the case's now, question and list:\n%s", got)
	}
	if !strings.Contains(got, "1) Café — jue 15 oct") {
		t.Fatalf("the candidate list is not in the prompt:\n%s", got)
	}
}

func TestPromptNeedsTheInstantItWasBuiltAt(t *testing.T) {
	c := baseCase()
	c.Now = ""
	if _, err := Prompt(c); err == nil || !strings.Contains(err.Error(), "now") {
		t.Fatalf("Prompt without now = %v, want an error naming now", err)
	}
}

func TestReplayAcceptsAMatchingRecording(t *testing.T) {
	if err := Replay(recorded(t, baseCase()), readsAsRequest); err != nil {
		t.Fatalf("Replay = %v, want nil", err)
	}
}

func TestReplayFailsOnPromptHashMismatch(t *testing.T) {
	c := recorded(t, baseCase())
	c.Message = "recordame llamar a Ana a las 5" // the prompt moved on after the recording
	err := Replay(c, readsAsRequest)
	if err == nil || !strings.Contains(err.Error(), "prompt hash") || !strings.Contains(err.Error(), "--force") {
		t.Fatalf("Replay = %v, want a prompt hash mismatch that says to record again with --force", err)
	}
}

func TestReplayFailsWithoutAHash(t *testing.T) {
	err := Replay(baseCase(), readsAsRequest)
	if err == nil || !strings.Contains(err.Error(), "prompt_sha256") {
		t.Fatalf("Replay of a recording with no hash = %v, want an error naming prompt_sha256", err)
	}
}

func TestReplayFailsWhenTheReadingIsNotTheExpectedOne(t *testing.T) {
	err := Replay(recorded(t, baseCase()), `{"type":"task","normalized_content":"Comprar café","weight":0.6,"decay_rate":0.1}`)
	if err == nil || !strings.Contains(err.Error(), "remind_request") {
		t.Fatalf("Replay = %v, want a miss naming remind_request", err)
	}
}

func TestKnownMisreadStaysMarked(t *testing.T) {
	c := recorded(t, baseCase())
	c.KnownMisread = "the model reads a recall as a request"

	err := Replay(c, readsAsRequest) // it reads as expected: the mark is stale
	if err == nil || !strings.Contains(err.Error(), "known_misread") {
		t.Fatalf("a known_misread that reads as expected = %v, want an error saying to remove the mark", err)
	}
	if err := Replay(c, `{"type":"task","normalized_content":"x","weight":0.6,"decay_rate":0.1}`); err != nil {
		t.Fatalf("a known_misread that still misreads = %v, want nil", err)
	}
}

func decode(t *testing.T, raw string) classify.Classification {
	t.Helper()
	now, _ := time.Parse(time.RFC3339, testNow)
	cls, err := classify.Decode(raw, now)
	if err != nil {
		t.Fatal(err)
	}
	return cls
}

func TestCheckReminderFieldsAreStrict(t *testing.T) {
	// An empty expectation means the model must read none, false or absent.
	const head = `{"type":"task","normalized_content":"x","weight":0.6,"decay_rate":0.1`
	for name, tc := range map[string]struct{ extra, miss string }{
		"a request":         {`,"remind_request":true`, FieldRemindRequest},
		"a stop":            {`,"reminder_intent":"stop"`, FieldReminderIntent},
		"a keep":            {`,"reminder_intent":"keep"`, FieldReminderIntent},
		"an answer":         {`,"question_answer":"no"`, FieldQuestionAnswer},
		"a choice number":   {`,"question_answer":"none","choice_number":2`, FieldChoiceNumber},
		"a target":          {`,"reminder_target":2`, FieldReminderTarget},
		"an unclear target": {`,"reminder_target":"unclear"`, FieldReminderTarget},
	} {
		t.Run(name, func(t *testing.T) {
			got := Check(llm.Case{}, decode(t, head+tc.extra+`}`))
			if len(got) != 1 || got[0] != tc.miss {
				t.Fatalf("Check = %v, want [%s]", got, tc.miss)
			}
		})
	}
	for name, raw := range map[string]string{
		"nothing":       head + `}`,
		"explicit none": head + `,"remind_request":false,"reminder_intent":"none","question_answer":"none"}`,
	} {
		t.Run("reads "+name, func(t *testing.T) {
			if got := Check(llm.Case{}, decode(t, raw)); len(got) != 0 {
				t.Fatalf("Check = %v, want no miss", got)
			}
		})
	}
}

// TestCheckOutcomesWithASideEffectAreStrictWhenUnexpected: an outcome the
// expectation does not name is not harmless. In production a nudge or task
// check-in outcome closes the most recent open check-in, a relation outcome
// confirms or deletes a relation, and a state outcome records a signal, so
// one the phrasing should not get is a misread (link 1b, review of #365).
// The two check-in fields are one answer: a case that names either may get
// the other beside it, as the recorded drop did, while the check-in is closed
// as the expected outcome would close it.
func TestCheckOutcomesWithASideEffectAreStrictWhenUnexpected(t *testing.T) {
	const head = `{"type":"chitchat","normalized_content":"x","weight":0.2,"decay_rate":0.3`
	for name, tc := range map[string]struct{ extra, miss string }{
		"a done":       {`,"task_checkin_outcome":"done"`, FieldTaskCheckinOutcome},
		"a snooze":     {`,"task_checkin_outcome":"snooze"`, FieldTaskCheckinOutcome},
		"an engaged":   {`,"nudge_outcome":"engaged"`, FieldNudgeOutcome},
		"a rejection":  {`,"relation_outcome":"rejected"`, FieldRelationOutcome},
		"a state read": {`,"state_outcome":"denied"`, FieldStateOutcome},
	} {
		t.Run(name, func(t *testing.T) {
			got := Check(llm.Case{Expect: &llm.Expect{}}, decode(t, head+tc.extra+`}`))
			if len(got) != 1 || got[0] != tc.miss {
				t.Fatalf("Check = %v, want [%s]", got, tc.miss)
			}
		})
	}
	drop := decode(t, head+`,"nudge_outcome":"declined","task_checkin_outcome":"drop"}`)
	if got := Check(llm.Case{Expect: &llm.Expect{TaskCheckinOutcome: "drop"}}, drop); len(got) != 0 {
		t.Errorf("a drop with the nudge field beside it = %v, want no miss", got)
	}
	// The field left free is free only while the effect is the expected one:
	// production reads the nudge first, so a declined beside a done closes the
	// check-in as declined, and a nudge beside a snooze closes what a snooze
	// leaves open (classify.ResolveCheckin).
	for name, tc := range map[string]struct {
		expect llm.Expect
		extra  string
	}{
		"a declined beside a done":   {llm.Expect{TaskCheckinOutcome: "done"}, `,"task_checkin_outcome":"done","nudge_outcome":"declined"`},
		"an engaged beside a snooze": {llm.Expect{TaskCheckinOutcome: "snooze"}, `,"task_checkin_outcome":"snooze","nudge_outcome":"engaged"`},
		"a drop beside an engaged":   {llm.Expect{NudgeOutcome: "engaged"}, `,"nudge_outcome":"engaged","task_checkin_outcome":"drop"`},
	} {
		got := Check(llm.Case{Expect: &tc.expect}, decode(t, head+tc.extra+`}`))
		free := FieldNudgeOutcome
		if tc.expect.NudgeOutcome != "" {
			free = FieldTaskCheckinOutcome
		}
		if name == "a drop beside an engaged" {
			// The nudge wins, so the drop changes nothing: the same effect.
			if len(got) != 0 {
				t.Errorf("%s = %v, want no miss: the nudge decides", name, got)
			}
			continue
		}
		if len(got) != 1 || got[0] != free {
			t.Errorf("%s = %v, want [%s]: it changes what the check-in is closed as", name, got, free)
		}
	}
	// A check-in answer does not free the relation field.
	related := decode(t, head+`,"task_checkin_outcome":"done","relation_outcome":"rejected"}`)
	if got := Check(llm.Case{Expect: &llm.Expect{TaskCheckinOutcome: "done"}}, related); len(got) != 1 || got[0] != FieldRelationOutcome {
		t.Errorf("a rejection beside an expected done = %v, want [%s]", got, FieldRelationOutcome)
	}
	want := FieldTaskCheckinOutcome + "," + FieldNudgeOutcome + "," + FieldRelationOutcome
	if got := Check(llm.Case{Expect: &llm.Expect{RelationOutcome: "confirmed"}}, drop); strings.Join(got, ",") != want {
		t.Errorf("check-in outcomes read for a relation answer = %v, want %s", got, want)
	}
}

func TestCheckMatchesTheExpectedReminderReading(t *testing.T) {
	c := llm.Case{Expect: &llm.Expect{ReminderIntent: "stop", ReminderTarget: "2"}}
	const head = `{"type":"task","normalized_content":"x","weight":0.6,"decay_rate":0.1`
	if got := Check(c, decode(t, head+`,"reminder_intent":"stop","reminder_target":2}`)); len(got) != 0 {
		t.Fatalf("Check = %v, want no miss", got)
	}
	if got := Check(c, decode(t, head+`,"reminder_intent":"stop","reminder_target":3}`)); len(got) != 1 || got[0] != FieldReminderTarget {
		t.Fatalf("Check of the wrong position = %v, want [%s]", got, FieldReminderTarget)
	}
	c = llm.Case{Expect: &llm.Expect{ReminderTarget: "unclear", QuestionAnswer: "choice", ChoiceNumber: intp(1)}}
	if got := Check(c, decode(t, head+`,"question_answer":"choice","choice_number":1,"reminder_target":"unclear"}`)); len(got) != 0 {
		t.Fatalf("Check = %v, want no miss", got)
	}
}

func TestCheckOptionalFieldsOnlyWhenSet(t *testing.T) {
	const head = `{"type":"task","normalized_content":"x","weight":0.6,"decay_rate":0.1`
	bare := decode(t, head+`,"due_at":"2026-10-16","title":"Dentista","task_checkin_outcome":"done"}`)
	timed := decode(t, head+`,"due_at":"2026-10-16T10:00:00-03:00"}`)

	// Day only and title have no effect of their own, so unset they are not
	// checked. (An unexpected outcome is: see the test above.)
	untitled := decode(t, head+`,"due_at":"2026-10-16","title":"Dentista"}`)
	if got := Check(llm.Case{}, untitled); len(got) != 0 {
		t.Fatalf("unset optional expectations were checked: %v", got)
	}
	yes := llm.Case{Expect: &llm.Expect{DayOnly: boolp(true), Title: boolp(true), TaskCheckinOutcome: "done"}}
	if got := Check(yes, bare); len(got) != 0 {
		t.Fatalf("Check = %v, want no miss", got)
	}
	got := Check(yes, timed)
	want := []string{FieldDayOnly, FieldTitle, FieldTaskCheckinOutcome}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("Check = %v, want %v", got, want)
	}
	if got := Check(llm.Case{Expect: &llm.Expect{DayOnly: boolp(false)}}, untitled); len(got) != 1 || got[0] != FieldDayOnly {
		t.Fatalf("a bare date where a timed one was expected = %v, want [%s]", got, FieldDayOnly)
	}
}

// TestCheckTypeOnlyWhenSet: a bare "listo" came back as a task and capture
// stored "La tarea está hecha" as a new unit, which no field of the set saw
// (review of #365). A case may name the type it should get; unset, any type
// passes, since most phrasings are about their reminder fields.
// TestCheckJudgesAClosingAnswerByEffect: the model is never shown whether the
// open check-in is a task or a nudge, and the store decides which one an
// answer closes (ADR-0033 point 2), so for an answer that closes it, done and
// engaged are the same reading, and so are drop and declined (review of #365).
// A snooze closes nothing, as no outcome does, so it keeps its exact value.
func TestCheckJudgesAClosingAnswerByEffect(t *testing.T) {
	const head = `{"type":"chitchat","normalized_content":"x","weight":0.2,"decay_rate":0.3`
	for name, tc := range map[string]struct {
		expect llm.Expect
		read   string
	}{
		"declined, read drop only": {llm.Expect{NudgeOutcome: "declined"}, `,"task_checkin_outcome":"drop"`},
		"engaged, read done only":  {llm.Expect{NudgeOutcome: "engaged"}, `,"task_checkin_outcome":"done"`},
		"done, read engaged only":  {llm.Expect{TaskCheckinOutcome: "done"}, `,"nudge_outcome":"engaged"`},
	} {
		t.Run(name, func(t *testing.T) {
			if got := Check(llm.Case{Expect: &tc.expect}, decode(t, head+tc.read+`}`)); len(got) != 0 {
				t.Errorf("Check = %v, want no miss: the check-in closes the same way", got)
			}
		})
	}
	for name, tc := range map[string]struct {
		expect llm.Expect
		read   string
		miss   string
	}{
		"done, read drop":       {llm.Expect{TaskCheckinOutcome: "done"}, `,"task_checkin_outcome":"drop"`, FieldTaskCheckinOutcome},
		"engaged, read nothing": {llm.Expect{NudgeOutcome: "engaged"}, ``, FieldNudgeOutcome},
		"declined, read snooze": {llm.Expect{NudgeOutcome: "declined"}, `,"task_checkin_outcome":"snooze"`, FieldTaskCheckinOutcome + "," + FieldNudgeOutcome},
		"snooze, read nothing":  {llm.Expect{TaskCheckinOutcome: "snooze"}, ``, FieldTaskCheckinOutcome},
	} {
		t.Run(name, func(t *testing.T) {
			if got := Check(llm.Case{Expect: &tc.expect}, decode(t, head+tc.read+`}`)); strings.Join(got, ",") != tc.miss {
				t.Errorf("Check = %v, want [%s]", got, tc.miss)
			}
		})
	}
}

func TestCheckTypeOnlyWhenSet(t *testing.T) {
	task := decode(t, `{"type":"task","normalized_content":"La tarea está hecha.","weight":0.5,"decay_rate":0.1}`)
	if got := Check(llm.Case{Expect: &llm.Expect{}}, task); len(got) != 0 {
		t.Errorf("an unset type was checked: %v", got)
	}
	if got := Check(llm.Case{Expect: &llm.Expect{Type: "chitchat"}}, task); len(got) != 1 || got[0] != FieldType {
		t.Errorf("a task where chitchat was expected = %v, want [%s]", got, FieldType)
	}
	untyped := decode(t, `{"normalized_content":"x"}`)
	if got := Check(llm.Case{Expect: &llm.Expect{Type: "chitchat"}}, untyped); len(got) != 1 || got[0] != FieldType {
		t.Errorf("no type where chitchat was expected = %v, want [%s]", got, FieldType)
	}
	if err := ValidateCase(llm.Case{ID: "x", Expect: &llm.Expect{Type: "memo"}}); err == nil {
		t.Error("a type outside the vocabulary was accepted")
	}
}

func TestValidateCaseRejectsWhatReplayCouldNotMean(t *testing.T) {
	bad := map[string]func(*llm.Case){
		"intent":      func(c *llm.Case) { c.Expect = &llm.Expect{ReminderIntent: "delete"} },
		"answer":      func(c *llm.Case) { c.Expect = &llm.Expect{QuestionAnswer: "maybe"} },
		"choice":      func(c *llm.Case) { c.Expect = &llm.Expect{ChoiceNumber: intp(6)} },
		"target":      func(c *llm.Case) { c.Expect = &llm.Expect{ReminderTarget: "0"} },
		"outcome":     func(c *llm.Case) { c.Expect = &llm.Expect{TaskCheckinOutcome: "later"} },
		"question":    func(c *llm.Case) { c.OpenQuestion = &llm.Question{Kind: "chat", Text: "x"} },
		"empty text":  func(c *llm.Case) { c.OpenQuestion = &llm.Question{Kind: "undo"} },
		"six entries": func(c *llm.Case) { c.CandidatesList = make([]llm.Entry, 6) },
		"no expect":   func(c *llm.Case) { c.Expect = nil },
	}
	for name, mutate := range bad {
		t.Run(name, func(t *testing.T) {
			c := baseCase()
			mutate(&c)
			if err := ValidateCase(c); err == nil {
				t.Fatal("ValidateCase = nil, want an error")
			}
		})
	}
	if err := ValidateCase(baseCase()); err != nil {
		t.Fatalf("ValidateCase of a good case = %v", err)
	}
}

func TestReminderMissedCountsOnlyTheReminderFields(t *testing.T) {
	for _, field := range ReminderFields {
		if !ReminderMissed([]string{field}) {
			t.Errorf("a miss on %s is a miss of the reminder reading", field)
		}
	}
	for _, field := range []string{FieldTitle, FieldDayOnly, FieldTaskCheckinOutcome, FieldNudgeOutcome, FieldRelationOutcome} {
		if ReminderMissed([]string{field}) {
			t.Errorf("a miss on %s is not a miss of the reminder reading (ADR-0033's rate is the intent fields)", field)
		}
	}
	if ReminderMissed(nil) || !ReminderMissed([]string{FieldTitle, "(response)"}) {
		t.Error("no miss is no miss; an unreadable response is a miss")
	}
}

var followUpConversation = &llm.Conversation{
	Turns: []llm.ConversationTurn{
		{At: "2026-10-14T21:00:00-03:00", Speaker: "user", Text: "reunión con Juan el 15 a las 22:30", About: []string{"A"}},
		{At: "2026-10-14T21:00:00-03:00", Speaker: "nooma", Text: "Anotado.", About: []string{"A"}},
		{At: "2026-10-15T09:00:00-03:00", Speaker: "push", Text: "Mañana: dentista", About: []string{"B"}},
	},
	Items: []llm.ConversationEntry{
		{Content: "Tengo reunión con Juan el 2026-10-15 a las 22:30.", Title: "Reunión con Juan", EventAt: "2026-10-15T22:30:00-03:00"},
		{Content: "Dentista.", EventAt: "2026-10-16"},
	},
}

// TestPromptCarriesTheConversation: a case's transcript reaches the prompt as
// production builds it, a bare date as the first moment of its day in the
// case's zone (ADR-0036).
func TestPromptCarriesTheConversation(t *testing.T) {
	c := baseCase()
	c.Conversation = followUpConversation
	got, err := Prompt(c)
	if err != nil {
		t.Fatal(err)
	}
	now, _ := time.Parse(time.RFC3339, testNow)
	meeting := time.Date(2026, 10, 15, 22, 30, 0, 0, now.Location())
	friday := time.Date(2026, 10, 16, 0, 0, 0, 0, now.Location())
	told, pushed := time.Date(2026, 10, 14, 21, 0, 0, 0, now.Location()), time.Date(2026, 10, 15, 9, 0, 0, 0, now.Location())
	want := classify.BuildPrompt(c.Message, nil, now, consolidation.DefaultWeightThreshold, classify.ReminderContext{
		Conversation: classify.Conversation{
			Turns: []classify.Turn{
				{At: told, Speaker: classify.SpeakerUser, Text: "reunión con Juan el 15 a las 22:30", About: []int{1}},
				{At: told, Speaker: classify.SpeakerNooma, Text: "Anotado.", About: []int{1}},
				{At: pushed, Speaker: classify.SpeakerPush, Text: "Mañana: dentista", About: []int{2}},
			},
			Items: []classify.ConversationItem{
				{Content: "Tengo reunión con Juan el 2026-10-15 a las 22:30.", Title: "Reunión con Juan", EventAt: &meeting},
				{Content: "Dentista.", EventAt: &friday},
			},
		},
	})
	if got != want {
		t.Fatalf("Prompt is not the production prompt with the case's conversation:\n%s", got)
	}
}

// TestCheckReferentIsStrict: a referent the case does not expect is a miss
// (it would correct a unit in production), an expected one must match, and
// a question comes back exactly when the referent is unclear.
func TestCheckReferentIsStrict(t *testing.T) {
	q := "¿Te referís a la reunión?"
	for _, tc := range []struct {
		name, want string
		got        classify.Classification
		miss       string
	}{
		{"none expected, none read", "", classify.Classification{}, ""},
		{"none expected, one read", "", classify.Classification{Referent: &classify.Referent{Position: 1}}, FieldReferent},
		{"A expected and read", "A", classify.Classification{Referent: &classify.Referent{Position: 1}}, ""},
		{"A expected, B read", "A", classify.Classification{Referent: &classify.Referent{Position: 2}}, FieldReferent},
		{"unclear expected, asked", "unclear", classify.Classification{Referent: &classify.Referent{Unclear: true}, ClarifyingQuestion: &q}, ""},
		{"unclear expected, not asked", "unclear", classify.Classification{Referent: &classify.Referent{Unclear: true}}, FieldClarifyingQuestion},
		{"a question beside a letter", "A", classify.Classification{Referent: &classify.Referent{Position: 1}, ClarifyingQuestion: &q}, FieldClarifyingQuestion},
	} {
		t.Run(tc.name, func(t *testing.T) {
			misses := Check(llm.Case{Expect: &llm.Expect{Referent: tc.want}}, tc.got)
			for _, f := range []string{FieldReferent, FieldClarifyingQuestion} {
				if got := slices.Contains(misses, f); got != (f == tc.miss) {
					t.Errorf("misses = %v, want only %q", misses, tc.miss)
				}
			}
		})
	}
}

// TestCheckEventAtOnlyWhenSet: the instant a follow-up moves to is checked
// when the case names one, as an instant whatever offset writes it.
func TestCheckEventAtOnlyWhenSet(t *testing.T) {
	at := time.Date(2026, 10, 15, 11, 0, 0, 0, time.UTC)
	cls := classify.Classification{EventAt: &at}
	missed := func(want string, cls classify.Classification) bool {
		return slices.Contains(Check(llm.Case{Expect: &llm.Expect{EventAt: want}}, cls), FieldEventAt)
	}
	if missed("", cls) || missed("2026-10-15T08:00:00-03:00", cls) {
		t.Error("the same instant, or none expected, was missed")
	}
	if !missed("2026-10-15T09:00:00-03:00", cls) || !missed("2026-10-15T08:00:00-03:00", classify.Classification{}) {
		t.Error("another instant, or none at all, was accepted")
	}
}

// TestCheckDueAtOnlyWhenSet: the instant a task is due is checked when the
// case names one, as an instant whatever offset writes it, and an unreadable
// one is refused before any replay.
func TestCheckDueAtOnlyWhenSet(t *testing.T) {
	at := time.Date(2026, 10, 20, 3, 0, 0, 0, time.UTC)
	cls := classify.Classification{DueAt: &at}
	missed := func(want string, cls classify.Classification) bool {
		return slices.Contains(Check(llm.Case{Expect: &llm.Expect{DueAt: want}}, cls), FieldDueAt)
	}
	if missed("", cls) || missed("2026-10-20T00:00:00-03:00", cls) {
		t.Error("the same instant, or none expected, was missed")
	}
	if !missed("2026-10-21T00:00:00-03:00", cls) || !missed("2026-10-20T00:00:00-03:00", classify.Classification{}) {
		t.Error("another instant, or none at all, was accepted")
	}
	c := baseCase()
	c.Expect = &llm.Expect{DueAt: "tomorrow"}
	if err := ValidateCase(c); err == nil {
		t.Error("an unreadable expect.due_at was accepted")
	}
}

// TestValidateCaseChecksTheConversation: a referent, a transcript or an
// instant the replay could not mean fails at once.
func TestValidateCaseChecksTheConversation(t *testing.T) {
	valid := func() llm.Case {
		c := baseCase()
		conv := *followUpConversation
		conv.Turns = slices.Clone(conv.Turns)
		conv.Items = slices.Clone(conv.Items)
		c.Conversation = &conv
		c.Expect = &llm.Expect{Referent: "B", EventAt: "2026-10-16T08:00:00-03:00"}
		return c
	}
	if err := ValidateCase(valid()); err != nil {
		t.Fatalf("a valid case was rejected: %v", err)
	}
	for name, mutate := range map[string]func(*llm.Case){
		"a referent not listed":     func(c *llm.Case) { c.Expect.Referent = "C" },
		"a number for a referent":   func(c *llm.Case) { c.Expect.Referent = "1" },
		"an unreadable event_at":    func(c *llm.Case) { c.Expect.EventAt = "tomorrow" },
		"an unreadable item date":   func(c *llm.Case) { c.Conversation.Items[1].EventAt = "16/10" },
		"an item with no content":   func(c *llm.Case) { c.Conversation.Items[0].Content = "" },
		"a turn tagging no item":    func(c *llm.Case) { c.Conversation.Turns[0].About = []string{"C"} },
		"an unknown speaker":        func(c *llm.Case) { c.Conversation.Turns[0].Speaker = "bot" },
		"a turn with no text":       func(c *llm.Case) { c.Conversation.Turns[1].Text = "" },
		"a turn with no time":       func(c *llm.Case) { c.Conversation.Turns[0].At = "" },
		"a turn after now":          func(c *llm.Case) { c.Conversation.Turns[2].At = "2026-10-15T09:31:00-03:00" },
		"too many turns":            func(c *llm.Case) { c.Conversation.Turns = make([]llm.ConversationTurn, classify.ConversationTurns+1) },
		"a conversation and no now": func(c *llm.Case) { c.Now = "" },
	} {
		t.Run(name, func(t *testing.T) {
			c := valid()
			mutate(&c)
			if err := ValidateCase(c); err == nil {
				t.Errorf("ValidateCase accepted %s", name)
			}
		})
	}
}

// TestCheckReminderLeadsAreStrict: leads set an event's reminders in
// production (I40), so leads nobody expected are a miss, and expected ones
// must come back exactly, in order.
func TestCheckReminderLeadsAreStrict(t *testing.T) {
	const head = `{"type":"correction","normalized_content":"x","weight":0.6,"decay_rate":0.1,"referent":"A"`
	for name, tc := range map[string]struct {
		expect []int
		extra  string
		miss   bool
	}{
		"unexpected":      {nil, `,"reminder_leads_minutes":[60]`, true},
		"expected":        {[]int{60}, `,"reminder_leads_minutes":[60]`, false},
		"another lead":    {[]int{60}, `,"reminder_leads_minutes":[120]`, true},
		"expected absent": {[]int{60}, ``, true},
	} {
		got := Check(llm.Case{Expect: &llm.Expect{Referent: "A", ReminderLeadsMinutes: tc.expect}}, decode(t, head+tc.extra+`}`))
		if missed := slices.Contains(got, FieldReminderLeads); missed != tc.miss || len(got) > 1 {
			t.Errorf("%s: Check = %v, want a %s miss only: %v", name, got, FieldReminderLeads, tc.miss)
		}
	}
}

// TestCheckReminderLeadsAddIsStrict: an added lead keeps the item's reminders
// in production and a replacing one drops them (ADR-0045), so either one
// nobody expected is a miss.
func TestCheckReminderLeadsAddIsStrict(t *testing.T) {
	const head = `{"type":"correction","normalized_content":"x","weight":0.6,"decay_rate":0.1,"referent":"A","reminder_leads_minutes":[60]`
	for name, tc := range map[string]struct {
		expect bool
		extra  string
		miss   bool
	}{
		"unexpected add":  {false, `,"reminder_leads_add":true`, true},
		"expected add":    {true, `,"reminder_leads_add":true`, false},
		"expected absent": {true, ``, true},
		"replace":         {false, `,"reminder_leads_add":false`, false},
	} {
		got := Check(llm.Case{Expect: &llm.Expect{Referent: "A", ReminderLeadsMinutes: []int{60}, ReminderLeadsAdd: tc.expect}}, decode(t, head+tc.extra+`}`))
		if missed := slices.Contains(got, FieldReminderLeadsAdd); missed != tc.miss || len(got) > 1 {
			t.Errorf("%s: Check = %v, want a %s miss only: %v", name, got, FieldReminderLeadsAdd, tc.miss)
		}
	}
}
