package conformance

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/brain"
	"github.com/rengo/nooma/internal/core/prospection"
	"github.com/rengo/nooma/internal/core/relation"
	"github.com/rengo/nooma/internal/core/unit"
	"github.com/rengo/nooma/internal/ports"
	"github.com/rengo/nooma/test/support/fakechannel"
	"github.com/rengo/nooma/test/support/fakeprovider"
	"github.com/rengo/nooma/test/support/memrepo"
)

// I49 (doc 02 §4, client answer 2 of 2026-10-27): a relation below its
// type's current persist bar is dormant, computed at read time. It is not
// drawn, not listed on its unit's page, not curated, and not asked about,
// and it stays stored: lowering the bar brings it back, unchanged.

var i49Now = time.Date(2026, 10, 29, prospection.DigestHour, 5, 0, 0, time.UTC)

const i49LowContent = "a dentist appointment"

type i49Vault struct {
	units     *memrepo.Units
	rels      *memrepo.Relations
	questions *memrepo.PendingQuestions
	cfg       *memrepo.Config
}

// newI49Vault holds centre linked by same_topic to low at 0.33, above today's
// 0.30 bar, and to high and mid at 0.80; high and mid are joined by r-chord,
// also at 0.33. A question about r-low waits in the digest's queue.
func newI49Vault(t *testing.T) *i49Vault {
	t.Helper()
	ctx := context.Background()
	v := &i49Vault{units: memrepo.NewUnits(), rels: memrepo.NewRelations(), questions: memrepo.NewPendingQuestions(), cfg: memrepo.NewConfig()}
	made := i49Now.Add(-48 * time.Hour)
	for id, content := range map[string]string{"centre": "buy coffee", "low": i49LowContent, "high": "the coffee shop on the corner", "mid": "grind the beans"} {
		if err := v.units.Create(ctx, unit.Unit{ID: id, Type: unit.TypeKnowledge, Status: unit.StatusPool, Content: content,
			Source: "chat", Weight: 1, LastTouchedAt: made, CreatedAt: made, UpdatedAt: made}); err != nil {
			t.Fatal(err)
		}
		v.rels.EnsureUnit(t, id)
	}
	for _, r := range []ports.Relation{
		{ID: "r-low", FromUnitID: "centre", ToUnitID: "low", Type: "same_topic", Strength: 0.5, Confidence: 0.33, CreatedBy: "consolidation", CreatedAt: made},
		{ID: "r-high", FromUnitID: "centre", ToUnitID: "high", Type: "same_topic", Strength: 0.5, Confidence: 0.80, CreatedBy: "consolidation", CreatedAt: made},
		{ID: "r-mid", FromUnitID: "centre", ToUnitID: "mid", Type: "same_topic", Strength: 0.5, Confidence: 0.80, CreatedBy: "consolidation", CreatedAt: made},
		{ID: "r-chord", FromUnitID: "high", ToUnitID: "mid", Type: "same_topic", Strength: 0.5, Confidence: 0.33, CreatedBy: "consolidation", CreatedAt: made},
	} {
		if err := v.rels.Upsert(ctx, r); err != nil {
			t.Fatal(err)
		}
	}
	v.questions.EnsureRelation(t, "r-low", "same_topic", "centre", "low", "buy coffee", i49LowContent)
	v.questions.SetRelationConfidence(t, "r-low", 0.33)
	if err := v.questions.Create(ctx, ports.PendingQuestion{ID: "q-low", Kind: ports.QuestionKindRelation, RelationID: "r-low", CreatedAt: made}); err != nil {
		t.Fatal(err)
	}
	return v
}

// setBar is what the learn pass writes for same_topic.
func (v *i49Vault) setBar(t *testing.T, persist float64) {
	t.Helper()
	if err := v.rels.LearnThresholds(context.Background(), "same_topic", relation.Thresholds{Persist: persist, Surface: 0.5}, i49Now); err != nil {
		t.Fatal(err)
	}
}

// seen reports where the 0.33 relations show: r-low or r-chord on the graph
// (a dormant chord between two awake neighbours included), r-low on its
// unit's page, and r-low as the question Today says the next digest would ask.
func (v *i49Vault) seen(t *testing.T) (graph, page, question bool) {
	t.Helper()
	ctx := context.Background()
	n, ok, err := brain.NewGraphService(v.units, v.rels).Neighbourhood(ctx, "centre")
	if err != nil || !ok {
		t.Fatalf("graph: %v %v", ok, err)
	}
	for _, nb := range n.Neighbours {
		graph = graph || nb.Unit.ID == "low"
	}
	for _, e := range n.Edges {
		graph = graph || e.ID == "r-low" || e.ID == "r-chord"
	}
	d, ok, err := brain.NewUnitsService(v.units, v.rels).Detail(ctx, "centre")
	if err != nil || !ok {
		t.Fatalf("unit page: %v %v", ok, err)
	}
	for _, r := range d.Relations {
		page = page || r.RelationID == "r-low"
	}
	today, err := brain.NewTodayService(fixedClock{now: i49Now}, v.units, v.cfg, memrepo.NewState(), memrepo.NewTriggers(),
		v.questions, memrepo.NewDecisionLog(), brain.NewFocusKeeper(v.units, v.cfg, v.rels), v.rels).Today(ctx)
	if err != nil {
		t.Fatalf("today: %v", err)
	}
	question = today.Digest.Question != nil && today.Digest.Question.RelationID == "r-low"
	return graph, page, question
}

func TestI49_ARelationBelowItsTypesBarIsDormantAndComesBack(t *testing.T) {
	v := newI49Vault(t)
	if g, p, q := v.seen(t); !g || !p || !q {
		t.Fatalf("under the default bar r-low shows on graph %v, page %v, question %v; want all three", g, p, q)
	}

	v.setBar(t, 0.35)
	if g, p, q := v.seen(t); g || p || q {
		t.Errorf("under a 0.35 bar r-low still shows on graph %v, page %v, question %v; want none", g, p, q)
	}
	if rel, err := v.rels.ByID(context.Background(), "r-low"); err != nil || rel.Confidence != 0.33 {
		t.Errorf("r-low = %+v, %v; want it stored, unchanged", rel, err)
	}

	v.setBar(t, 0.30)
	if g, p, q := v.seen(t); !g || !p || !q {
		t.Errorf("with the bar back at 0.30 r-low shows on graph %v, page %v, question %v; want all three", g, p, q)
	}
}

// TestI49_TheDigestDoesNotAskAboutADormantRelation: the digest pass itself
// skips it, and leaves it queued, unasked, for a bar that falls.
func TestI49_TheDigestDoesNotAskAboutADormantRelation(t *testing.T) {
	v := newI49Vault(t)
	v.setBar(t, 0.35)
	ch := fakechannel.New()
	if _, err := brain.NewCheckService(fixedClock{now: i49Now}, memrepo.NewTriggers(), memrepo.NewTimers(), &counterIDs{}, memrepo.NewDecisionLog(),
		ch, v.units, memrepo.NewState(), nil, "12449194", v.questions, nil, nil, v.rels).Check(context.Background(), brain.CheckRequest{}); err != nil {
		t.Fatalf("Check: %v", err)
	}
	for _, m := range ch.Sent(t) {
		if strings.Contains(m.Text, i49LowContent) {
			t.Errorf("the digest asked about a dormant relation: %q", m.Text)
		}
	}
	if unasked, err := v.questions.Unasked(context.Background()); err != nil || len(unasked) != 1 {
		t.Errorf("unasked = %+v, %v; want q-low still queued", unasked, err)
	}
}

// TestI49_ADormantRelationIsNotCurated: only a relation the graph draws can be
// curated (doc 02 §4), and a dormant one is not drawn.
func TestI49_ADormantRelationIsNotCurated(t *testing.T) {
	v := newI49Vault(t)
	v.setBar(t, 0.35)
	svc := brain.NewCurationService(fixedClock{now: i49Now}, &counterIDs{}, v.units, v.rels, memrepo.NewSignals(), memrepo.NewDecisionLog(), v.questions)
	for name, act := range map[string]func(context.Context, string) error{"confirm": svc.Confirm, "reject": svc.Reject} {
		if err := act(context.Background(), "r-low"); !errors.Is(err, ports.ErrRelationNotFound) {
			t.Errorf("%s of a dormant relation = %v, want ErrRelationNotFound", name, err)
		}
	}
	if _, err := v.rels.ByID(context.Background(), "r-low"); err != nil {
		t.Errorf("r-low was touched: %v", err)
	}
}

// TestI49_AQuestionAskedBeforeItsRelationWentDormantIsStillAnswered (#409
// review): the digest asked about r-low, then the bar rose. The question was
// put to the user, so their "no" in the chat still answers it, and deletes
// the relation as any answer does. Dormancy hides what is not asked; it does
// not take back what was.
func TestI49_AQuestionAskedBeforeItsRelationWentDormantIsStillAnswered(t *testing.T) {
	v := newI49Vault(t)
	ctx := context.Background()
	if err := v.questions.MarkAsked(ctx, "q-low", i49Now.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	v.setBar(t, 0.35)

	embeddings := memrepo.NewEmbeddings()
	idx, err := embeddings.LoadIndex(ctx, embedFakeModel)
	if err != nil {
		t.Fatal(err)
	}
	llm := fakeprovider.New(t, testdataLLMCasesDir(t), "classify-relation-answer-rejected", "chat-relation-answer-acknowledged")
	svc := brain.NewCaptureService(fixedClock{now: i49Now}, &counterIDs{}, v.units, embeddings, memrepo.NewLexical(), v.rels,
		memrepo.NewDecisionLog(), llm, llm, llm, fakeprovider.NewEmbeddingFake(embedFakeModel), brain.NewIndex(idx),
		memrepo.NewSignals(), memrepo.NewTriggers(), memrepo.NewTimers(), 0.5, v.questions)
	if _, err := svc.Capture(ctx, brain.CaptureInput{Text: "no", Channel: "chat"}); err != nil {
		t.Fatalf("Capture: %v", err)
	}
	if _, err := v.rels.ByID(ctx, "r-low"); !errors.Is(err, ports.ErrRelationNotFound) {
		t.Errorf("r-low after the answer: %v, want it deleted by the user's no", err)
	}
}
