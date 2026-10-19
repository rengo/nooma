// Package conformance — see test/conformance/doc.go for the package contract.
package conformance

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/brain"
	"github.com/rengo/nooma/internal/core/classify"
	"github.com/rengo/nooma/internal/core/unit"
	"github.com/rengo/nooma/internal/ports"
	"github.com/rengo/nooma/test/support/fakeprovider"
	"github.com/rengo/nooma/test/support/memrepo"
)

// scriptedLLM answers every call with one hand-written response and keeps
// the prompts it was sent. The responses are hand-written on purpose: the
// recorded phrasing set covers how a real model reads these follow-ups; this
// test covers what the code does with each reading.
type scriptedLLM struct {
	response string
	prompts  []string
}

func (s *scriptedLLM) Complete(_ context.Context, req ports.LLMRequest) (ports.LLMResponse, error) {
	s.prompts = append(s.prompts, req.Prompt)
	return ports.LLMResponse{Text: s.response}, nil
}

type i40Fixture struct {
	svc       *brain.CaptureService
	llm       *scriptedLLM
	units     *memrepo.Units
	triggers  *memrepo.Triggers
	decisions *memrepo.DecisionLog
	conv      *brain.Conversation
	relations *memrepo.Relations
	questions *memrepo.PendingQuestions
}

var (
	i40Now     = time.Date(2026, 10, 14, 18, 0, 0, 0, time.FixedZone("ART", -3*3600))
	i40Meeting = time.Date(2026, 10, 14, 22, 30, 0, 0, time.FixedZone("ART", -3*3600))
)

const i40Chat ports.ConversationID = "42"

// newI40 seeds the meeting the user told Nooma about ten minutes ago, and the
// two turns of that exchange, then answers the next message with response.
func newI40(t *testing.T, response string) i40Fixture {
	t.Helper()
	return newI40With(t, response, func(*unit.Unit) {})
}

// newI40With is newI40 with the meeting changed by edit before it is seeded.
func newI40With(t *testing.T, response string, edit func(*unit.Unit)) i40Fixture {
	t.Helper()
	ctx := context.Background()
	f := i40Fixture{
		llm: &scriptedLLM{response: response}, units: memrepo.NewUnits(), triggers: memrepo.NewTriggers(),
		decisions: memrepo.NewDecisionLog(), conv: brain.NewConversation(),
		relations: memrepo.NewRelations(), questions: memrepo.NewPendingQuestions(),
	}
	meeting := unit.Unit{
		ID: "meeting", Type: unit.TypeEvent, Status: unit.StatusPool, Source: "telegram",
		Content: "Tengo reunión con Juan el 2026-10-14 a las 22:30.", Title: "Reunión con Juan",
		EventAt: &i40Meeting, CreatedAt: i40Now, UpdatedAt: i40Now,
	}
	edit(&meeting)
	if err := f.units.Create(ctx, meeting); err != nil {
		t.Fatal(err)
	}
	said := i40Now.Add(-10 * time.Minute)
	f.conv.Add(i40Chat, brain.Turn{At: said, Speaker: classify.SpeakerUser, Text: "reunión con Juan hoy 22:30", UnitIDs: []string{"meeting"}})
	f.conv.Add(i40Chat, brain.Turn{At: said, Speaker: classify.SpeakerNooma, Text: "Anotado.", UnitIDs: []string{"meeting"}})
	embeddings := memrepo.NewEmbeddings()
	idx, err := embeddings.LoadIndex(ctx, embedFakeModel)
	if err != nil {
		t.Fatal(err)
	}
	f.svc = brain.NewCaptureService(fixedClock{now: i40Now}, &counterIDs{}, f.units, embeddings, memrepo.NewLexical(),
		f.relations, f.decisions, f.llm, f.llm, f.llm, fakeprovider.NewEmbeddingFake(embedFakeModel),
		brain.NewIndex(idx), memrepo.NewSignals(), f.triggers, memrepo.NewTimers(), 0.5, f.questions).WithConversation(f.conv)
	return f
}

func (f i40Fixture) capture(t *testing.T, text string) brain.CaptureResult {
	t.Helper()
	res, err := f.svc.Capture(context.Background(), brain.CaptureInput{Text: text, Channel: "telegram", Conversation: i40Chat})
	if err != nil {
		t.Fatalf("Capture(%q): %v", text, err)
	}
	return res
}

func (f i40Fixture) meeting(t *testing.T) unit.Unit {
	t.Helper()
	u, err := f.units.ByID(context.Background(), "meeting")
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func i40Actions(t *testing.T, d *memrepo.DecisionLog) []ports.Decision {
	t.Helper()
	got, err := d.Since(context.Background(), time.Time{}, 100)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

// TestI40_AFollowUpCorrectsTheItemItFollows is the live case (ADR-0036):
// "Es a las 08:00." right after a meeting was captured moves that meeting,
// in the moment. The model was shown the exchange, tagged A, and answered a
// correction of A; nothing new is stored and the pre-image says the referent
// came from the conversation.
func TestI40_AFollowUpCorrectsTheItemItFollows(t *testing.T) {
	f := newI40(t, `{"type":"correction","normalized_content":"Tengo reunión con Juan el 2026-10-14 a las 08:00.",`+
		`"event_at":"2026-10-14T08:00:00-03:00","weight":0.7,"decay_rate":0.05,"language":"es","referent":"A","title":"Reunión con Juan"}`)
	res := f.capture(t, "Es a las 08:00.")

	if res.Outcome != brain.OutcomeCorrected || res.Correction == nil || res.Correction.UnitID != "meeting" {
		t.Fatalf("result = %+v, want the meeting corrected", res)
	}
	if n := f.units.Count(); n != 1 {
		t.Errorf("units = %d, want 1: a follow-up is not a new memory", n)
	}
	want := time.Date(2026, 10, 14, 8, 0, 0, 0, i40Now.Location())
	if got := f.meeting(t); got.EventAt == nil || !got.EventAt.Equal(want) || got.Content != "Tengo reunión con Juan el 2026-10-14 a las 08:00." {
		t.Errorf("meeting = %q at %v, want moved to %v", got.Content, got.EventAt, want)
	}
	// The reply names where it moved to, so the result carries it (I40).
	if m := res.Correction.MovedTo; m == nil || !m.Equal(want) || m.Location() != i40Now.Location() || res.Correction.MovedToDayOnly {
		t.Errorf("MovedTo = %v (day only %v), want %v in the user's zone", m, res.Correction.MovedToDayOnly, want)
	}
	p := f.llm.prompts[0]
	for _, shown := range []string{`user: "reunión con Juan hoy 22:30" [A]`, `Nooma: "Anotado." [A]`, `A) text (quoted): "Tengo reunión con Juan el 2026-10-14 a las 22:30."`} {
		if !strings.Contains(p, shown) {
			t.Errorf("the prompt lacks %q:\n%s", shown, p)
		}
	}
	rows := i40Actions(t, f.decisions)
	var pre struct {
		Referent struct {
			Source string `json:"source"`
		} `json:"referent"`
	}
	if len(rows) == 0 || rows[0].Action != ports.ActionCorrectionApplied || json.Unmarshal(rows[0].Context, &pre) != nil || pre.Referent.Source != "conversation" {
		t.Errorf("first row = %+v, want correction.applied with referent source conversation", rows)
	}
}

// TestI40_ALetterThatWasNotShownNeverBinds: the model named B and only A was
// shown. The letter is dropped and the correction takes the chat path it
// took before, which asks here; the meeting is untouched.
func TestI40_ALetterThatWasNotShownNeverBinds(t *testing.T) {
	f := newI40(t, `{"type":"correction","normalized_content":"Es a las 08:00.","event_at":"2026-10-14T08:00:00-03:00",`+
		`"weight":0.7,"decay_rate":0.05,"referent":"B"}`)
	res := f.capture(t, "Es a las 08:00.")
	if res.Outcome == brain.OutcomeCorrected {
		t.Errorf("result = %+v: an unlisted letter corrected a unit", res)
	}
	if got := f.meeting(t); !got.EventAt.Equal(i40Meeting) {
		t.Errorf("meeting moved to %v by an unlisted letter", got.EventAt)
	}
}

// TestI40_AnUnclearFollowUpAsksInTheMoment: the model could not tell which
// item, and wrote the question. Nooma sends it as it is and stores nothing;
// the ask is in the trail.
func TestI40_AnUnclearFollowUpAsksInTheMoment(t *testing.T) {
	const q = "¿Te referís a la reunión con Juan?"
	f := newI40(t, `{"type":"correction","normalized_content":"Es a las 08:00.","weight":0.7,"decay_rate":0.05,`+
		`"referent":"unclear","clarifying_question":"`+q+`"}`)
	res := f.capture(t, "Es a las 08:00.")
	if res.Outcome != brain.OutcomeAsked || res.Reply != q || res.Correction == nil || res.Correction.Why != brain.AskReferentUnclear {
		t.Fatalf("result = %+v, want the model's question asked", res)
	}
	if n := f.units.Count(); n != 1 {
		t.Errorf("units = %d, want 1: asking stores nothing", n)
	}
	if got := f.meeting(t); !got.EventAt.Equal(i40Meeting) {
		t.Errorf("meeting moved to %v while Nooma was asking", got.EventAt)
	}
	rows := i40Actions(t, f.decisions)
	if len(rows) != 1 || rows[0].Action != ports.ActionCorrectionAmbiguous || !strings.Contains(string(rows[0].Context), `"referent_unclear"`) {
		t.Errorf("rows = %+v, want one correction.ambiguous row, reason referent_unclear", rows)
	}
}

// TestI40_ANewMessageStaysNew: a referent beside any type but correction is
// ignored, so a new message with the conversation shown is a new unit.
func TestI40_ANewMessageStaysNew(t *testing.T) {
	f := newI40(t, `{"type":"knowledge","normalized_content":"A Juan le gusta el café.","weight":0.6,"decay_rate":0.02,"referent":"A"}`)
	if res := f.capture(t, "A Juan le gusta el café"); res.Outcome != brain.OutcomeStored {
		t.Errorf("result = %+v, want a new unit", res)
	}
	if n := f.units.Count(); n != 2 {
		t.Errorf("units = %d, want 2", n)
	}
	if got := f.meeting(t); !got.EventAt.Equal(i40Meeting) {
		t.Errorf("meeting moved to %v by a new message", got.EventAt)
	}
}

// TestI40_OnlyARecentChatIsShown: a message with no conversation, or one
// whose turns are older than the window, is read alone.
func TestI40_OnlyARecentChatIsShown(t *testing.T) {
	const knowledge = `{"type":"knowledge","normalized_content":"x","weight":0.6,"decay_rate":0.02}`
	// Read alone, the prompt carries none of the follow-up machinery.
	alone := func(p string) bool {
		return !strings.Contains(p, "Recent conversation") && !strings.Contains(p, "Follow-ups")
	}
	f := newI40(t, knowledge)
	if _, err := f.svc.Capture(context.Background(), brain.CaptureInput{Text: "x", Channel: "cli"}); err != nil {
		t.Fatal(err)
	}
	if !alone(f.llm.prompts[0]) {
		t.Errorf("a capture with no conversation was shown one")
	}

	stale := newI40(t, knowledge)
	stale.conv.Add("7", brain.Turn{At: i40Now.Add(-classify.ConversationWindow - time.Second), Speaker: classify.SpeakerUser, Text: "old", UnitIDs: []string{"meeting"}})
	if _, err := stale.svc.Capture(context.Background(), brain.CaptureInput{Text: "x", Channel: "telegram", Conversation: "7"}); err != nil {
		t.Fatal(err)
	}
	if !alone(stale.llm.prompts[0]) {
		t.Errorf("a turn past the window was shown:\n%s", stale.llm.prompts[0])
	}
}

// TestI40_AUnitNoLongerLiveIsNeverALetter: the chat names an archived unit
// and one that does not exist. Neither is lettered, so neither can be bound:
// their turns are shown untagged, and the meeting, the only live item, is
// the only letter. A follow-up naming the second letter binds nothing.
func TestI40_AUnitNoLongerLiveIsNeverALetter(t *testing.T) {
	f := newI40(t, `{"type":"correction","normalized_content":"x","event_at":"2026-10-14T08:00:00-03:00",`+
		`"weight":0.7,"decay_rate":0.05,"referent":"B"}`)
	ctx := context.Background()
	if err := f.units.Create(ctx, unit.Unit{
		ID: "old", Type: unit.TypeEvent, Status: unit.StatusArchived, Source: "telegram",
		Content: "Cena con Ana el 2026-10-14 a las 21:00.", EventAt: &i40Meeting, CreatedAt: i40Now, UpdatedAt: i40Now,
	}); err != nil {
		t.Fatal(err)
	}
	said := i40Now.Add(-5 * time.Minute)
	f.conv.Add(i40Chat, brain.Turn{At: said, Speaker: classify.SpeakerUser, Text: "cena con Ana a las 21", UnitIDs: []string{"old"}})
	f.conv.Add(i40Chat, brain.Turn{At: said, Speaker: classify.SpeakerUser, Text: "algo que ya no está", UnitIDs: []string{"gone"}})

	if res := f.capture(t, "Es a las 08:00."); res.Outcome == brain.OutcomeCorrected {
		t.Errorf("result = %+v: a letter past the live items corrected a unit", res)
	}
	p := f.llm.prompts[0]
	for _, untagged := range []string{`user: "cena con Ana a las 21"` + "\n", `user: "algo que ya no está"` + "\n"} {
		if !strings.Contains(p, untagged) {
			t.Errorf("a turn about a unit that is not live is tagged, or missing; want %q in:\n%s", untagged, p)
		}
	}
	if strings.Contains(p, "B) text") || strings.Contains(p, "Cena con Ana el") {
		t.Errorf("an archived or missing unit is listed as an item:\n%s", p)
	}
	old, err := f.units.ByID(ctx, "old")
	if err != nil || !old.EventAt.Equal(i40Meeting) {
		t.Errorf("the archived unit was touched: %+v, %v", old, err)
	}
}

// TestI40_AnAnswerNamingNoItemChangesNothing: after Nooma asked which item,
// the user says it is none of them. The model reads chitchat with no
// referent and no check-in outcome; nothing is corrected, by the chat or by
// recall, and nothing is stored or resolved.
func TestI40_AnAnswerNamingNoItemChangesNothing(t *testing.T) {
	f := newI40(t, `{"type":"chitchat","normalized_content":"Ninguna de esas.","weight":0.1,"decay_rate":0.3,"language":"es"}`)
	said := i40Now.Add(-time.Minute)
	f.conv.Add(i40Chat, brain.Turn{At: said, Speaker: classify.SpeakerUser, Text: "Es a las 08:00."})
	f.conv.Add(i40Chat, brain.Turn{At: said, Speaker: classify.SpeakerNooma, Text: "¿Te referís a la reunión con Juan?"})
	res := f.capture(t, "no, ninguna de esas")
	if res.Outcome != brain.OutcomeConversed {
		t.Errorf("result = %+v, want a conversed reply", res)
	}
	if n := f.units.Count(); n != 1 {
		t.Errorf("units = %d, want 1", n)
	}
	if got := f.meeting(t); !got.EventAt.Equal(i40Meeting) {
		t.Errorf("meeting moved to %v by an answer naming no item", got.EventAt)
	}
	for _, d := range i40Actions(t, f.decisions) {
		if d.Action == ports.ActionCorrectionApplied || d.Action == ports.ActionCorrectionAmbiguous {
			t.Errorf("row %s written for an answer naming no item", d.Action)
		}
	}
}

// TestI40_ABoundFollowUpAnswersNoRelationQuestion: a relation question is
// open, and the model's reading of a follow-up it binds also carries
// relation_outcome confirmed, as the recorded answer to the which-item
// question did (classify-followup-answer-si, known_misread). The follow-up
// fork returns before relation resolution, so that outcome reaches nothing:
// the question stays open and the relation keeps its confidence.
func TestI40_ABoundFollowUpAnswersNoRelationQuestion(t *testing.T) {
	f := newI40(t, `{"type":"correction","normalized_content":"Tengo reunión con Juan el 2026-10-14 a las 08:00.",`+
		`"event_at":"2026-10-14T08:00:00-03:00","weight":0.7,"decay_rate":0.05,"referent":"A","relation_outcome":"confirmed"}`)
	ctx := context.Background()
	f.relations.EnsureUnit(t, "meeting")
	f.relations.EnsureUnit(t, "other")
	if err := f.relations.Upsert(ctx, ports.Relation{
		ID: "rel-1", FromUnitID: "meeting", ToUnitID: "other", Type: "same_topic",
		Strength: 0.5, Confidence: 0.4, CreatedBy: "consolidation", CreatedAt: i40Now.Add(-24 * time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
	f.questions.EnsureRelation(t, "rel-1", "same_topic", "meeting", "other", "meeting", "other")
	if err := f.questions.Create(ctx, ports.PendingQuestion{ID: "q-1", Kind: ports.QuestionKindRelation, RelationID: "rel-1", CreatedAt: i40Now.Add(-24 * time.Hour)}); err != nil {
		t.Fatal(err)
	}
	if err := f.questions.MarkAsked(ctx, "q-1", i40Now.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}

	if res := f.capture(t, "sí"); res.Outcome != brain.OutcomeCorrected {
		t.Fatalf("result = %+v, want the meeting corrected", res)
	}
	if open, err := f.questions.Open(ctx); err != nil || len(open) != 1 || open[0].ID != "q-1" {
		t.Errorf("open relation questions = %+v, %v; want q-1 still open", open, err)
	}
	if rel, err := f.relations.ByID(ctx, "rel-1"); err != nil || rel.Confidence != 0.4 {
		t.Errorf("relation = %+v, %v; want its confidence untouched at 0.4", rel, err)
	}
}
