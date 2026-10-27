package conformance

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/brain"
	"github.com/rengo/nooma/internal/core/consolidation"
	"github.com/rengo/nooma/internal/core/recall"
	"github.com/rengo/nooma/internal/core/relation"
	"github.com/rengo/nooma/internal/core/unit"
	"github.com/rengo/nooma/internal/ports"
	"github.com/rengo/nooma/test/support/fakeprovider"
	"github.com/rengo/nooma/test/support/memrepo"
)

// I48 (doc 02 §9, ADR-0053): the nightly learn slot runs the relation rule.
// This file is M5's demo, made a conformance test: rejecting six same_topic
// links raises that type's bar, and the decision_log says why.

var (
	i48Made    = time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC)
	i48Judged  = time.Date(2026, 10, 27, 20, 0, 0, 0, time.UTC)
	i48Night   = time.Date(2026, 10, 28, 3, 0, 0, 0, time.UTC)
	i48Later   = i48Night.Add(8 * 24 * time.Hour)
	i48Pairs   = 8
	i48Rejects = 6
)

// i48Vault is one vault whose eight same_topic links Nooma made at night:
// centre -> n1 ... n8, each in the uncertain band.
type i48Vault struct {
	units     *memrepo.Units
	rels      *memrepo.Relations
	signals   *memrepo.Signals
	log       *memrepo.DecisionLog
	questions *memrepo.PendingQuestions
	cfg       *memrepo.Config
	ids       *counterIDs
}

func newI48Vault(t *testing.T) *i48Vault {
	t.Helper()
	ctx := context.Background()
	v := &i48Vault{units: memrepo.NewUnits(), rels: memrepo.NewRelations(), signals: memrepo.NewSignals(),
		log: memrepo.NewDecisionLog(), questions: memrepo.NewPendingQuestions(), cfg: memrepo.NewConfig(), ids: &counterIDs{}}
	for i := 0; i <= i48Pairs; i++ {
		id := fmt.Sprintf("n%d", i)
		if i == 0 {
			id = "centre"
		}
		if err := v.units.Create(ctx, unit.Unit{ID: id, Type: unit.TypeKnowledge, Status: unit.StatusPool, Content: "note " + id,
			Source: "chat", Weight: 1, LastTouchedAt: i48Made, CreatedAt: i48Made, UpdatedAt: i48Made}); err != nil {
			t.Fatal(err)
		}
		v.rels.EnsureUnit(t, id)
		if i == 0 {
			continue
		}
		rel := ports.Relation{ID: fmt.Sprintf("r%d", i), FromUnitID: "centre", ToUnitID: id, Type: "same_topic",
			Strength: 0.4, Confidence: 0.4, CreatedBy: "consolidation", CreatedAt: i48Made}
		if err := v.rels.Upsert(ctx, rel); err != nil {
			t.Fatal(err)
		}
		v.questions.EnsureRelation(t, rel.ID, rel.Type, "centre", id, "note centre", "note "+id)
	}
	return v
}

// graph deletes or confirms relation id from /ui/graph's service.
func (v *i48Vault) graph(t *testing.T, id string, reject bool) {
	t.Helper()
	svc := brain.NewCurationService(fixedClock{now: i48Judged}, v.ids, v.units, v.rels, v.signals, v.log, v.questions)
	act := svc.Confirm
	if reject {
		act = svc.Reject
	}
	if err := act(context.Background(), id); err != nil {
		t.Fatalf("graph %s: %v", id, err)
	}
}

// chat asks relation id's question in a digest and answers it no, or yes.
func (v *i48Vault) chat(t *testing.T, id string, reject bool) {
	t.Helper()
	ctx := context.Background()
	q := "q-" + id
	if err := v.questions.Create(ctx, ports.PendingQuestion{ID: q, Kind: ports.QuestionKindRelation, RelationID: id, CreatedAt: i48Made}); err != nil {
		t.Fatal(err)
	}
	if err := v.questions.MarkAsked(ctx, q, i48Judged.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	reading := "classify-relation-answer-confirmed"
	if reject {
		reading = "classify-relation-answer-rejected"
	}
	embeddings := memrepo.NewEmbeddings()
	idx, err := embeddings.LoadIndex(ctx, embedFakeModel)
	if err != nil {
		t.Fatal(err)
	}
	llm := fakeprovider.New(t, testdataLLMCasesDir(t), reading, "chat-relation-answer-acknowledged")
	svc := brain.NewCaptureService(fixedClock{now: i48Judged}, v.ids, v.units, embeddings, memrepo.NewLexical(), v.rels, v.log,
		llm, llm, llm, fakeprovider.NewEmbeddingFake(embedFakeModel), brain.NewIndex(idx),
		v.signals, memrepo.NewTriggers(), memrepo.NewTimers(), 0.5, v.questions)
	if _, err := svc.Capture(ctx, brain.CaptureInput{Text: "an answer to the question", Channel: "chat"}); err != nil {
		t.Fatalf("chat answer about %s: %v", id, err)
	}
}

// judgeSix rejects six links, three from the graph and three from the chat,
// and confirms the other two, one from each.
func (v *i48Vault) judgeSix(t *testing.T) {
	t.Helper()
	for i := 1; i <= i48Pairs; i++ {
		id, reject := fmt.Sprintf("r%d", i), i <= i48Rejects
		if i%2 == 1 {
			v.graph(t, id, reject)
		} else {
			v.chat(t, id, reject)
		}
	}
}

// learn runs the learn slot alone at now.
func (v *i48Vault) learn(t *testing.T, now time.Time) {
	t.Helper()
	phase := consolidation.PhaseLearn
	svc := brain.NewConsolidateService(fixedClock{now: now}, v.cfg, v.units, v.rels, v.ids, v.log, nil, nil,
		memrepo.NewSelfModel(), memrepo.NewState(), v.questions, v.signals)
	if _, err := svc.Consolidate(context.Background(), brain.ConsolidateRequest{Phase: &phase}); err != nil {
		t.Fatalf("learn at %s: %v", now, err)
	}
}

func (v *i48Vault) learnRows(t *testing.T) []ports.Decision {
	t.Helper()
	all, err := v.log.Since(context.Background(), time.Time{}, -1)
	if err != nil {
		t.Fatal(err)
	}
	var out []ports.Decision
	for _, d := range all {
		if d.Action == ports.ActionLearnRelationThreshold {
			out = append(out, d)
		}
	}
	return out
}

func (v *i48Vault) bar(t *testing.T, relType string) relation.Thresholds {
	t.Helper()
	row, err := v.rels.ThresholdsFor(context.Background(), relType)
	if err != nil {
		t.Fatal(err)
	}
	return relation.Resolve(row)
}

// learnDetail is the learn row's context.
type learnDetail struct {
	RelationType string              `json:"relation_type"`
	Direction    string              `json:"direction"`
	Previous     relation.Thresholds `json:"previous"`
	Next         relation.Thresholds `json:"next"`
	Rejected     int                 `json:"rejected"`
	Confirmed    int                 `json:"confirmed"`
	SignalIDs    []string            `json:"signal_ids"`
}

func TestI48_SixSameTopicRejectionsRaiseTheThreshold(t *testing.T) {
	v := newI48Vault(t)
	v.judgeSix(t)

	v.learn(t, i48Night)

	if got, want := v.bar(t, "same_topic"), (relation.Thresholds{Persist: 0.35, Surface: 0.50}); got != want {
		t.Fatalf("same_topic bar = %+v, want %+v", got, want)
	}
	rows := v.learnRows(t)
	if len(rows) != 1 {
		t.Fatalf("%d learn rows, want exactly 1", len(rows))
	}
	if !strings.Contains(rows[0].Rationale, "6 of the last 8") || !strings.Contains(rows[0].Rationale, "same_topic") ||
		!strings.Contains(rows[0].Rationale, "0.30") || !strings.Contains(rows[0].Rationale, "0.35") {
		t.Errorf("rationale %q does not say why: same_topic, 0.30 -> 0.35, 6 of the last 8", rows[0].Rationale)
	}
	if !rows[0].OccurredAt.Equal(i48Night) {
		t.Errorf("row at %s, want the pass's instant %s", rows[0].OccurredAt, i48Night)
	}
	var d learnDetail
	if err := json.Unmarshal(rows[0].Context, &d); err != nil {
		t.Fatalf("context %s: %v", rows[0].Context, err)
	}
	if d.RelationType != "same_topic" || d.Direction != "raised" || d.Previous.Persist != 0.30 || d.Next.Persist != 0.35 ||
		d.Rejected != 6 || d.Confirmed != 2 || len(d.SignalIDs) != 8 {
		t.Errorf("context = %+v, want same_topic raised 0.30 -> 0.35 on 6 rejected, 2 confirmed, 8 signals", d)
	}

	// The same night again, and a night past the cooldown with no new
	// verdict: the evidence was counted once, so nothing moves.
	for _, again := range []time.Time{i48Night, i48Later} {
		v.learn(t, again)
		if got := v.bar(t, "same_topic"); got.Persist != 0.35 {
			t.Errorf("at %s the bar moved again to %+v", again, got)
		}
	}
	if n := len(v.learnRows(t)); n != 1 {
		t.Errorf("%d learn rows after re-running, want still 1", n)
	}

	// The next night, a same_topic link judged at 0.33 is not stored: it is
	// below the new bar, where under the default one it was uncertain.
	if relation.Decide(0.33, relation.Resolve(nil)) == relation.Discard {
		t.Fatal("test bug: 0.33 is below the default bar too, so this proves nothing")
	}
	if v.connectAt033(t) {
		t.Error("a 0.33 same_topic link was stored under the raised bar")
	}
}

// connectAt033 runs the next night's connect on a new unit whose judge
// answers same_topic at 0.33 about n1's neighbour, and reports whether a
// relation was stored.
func (v *i48Vault) connectAt033(t *testing.T) bool {
	t.Helper()
	ctx := context.Background()
	night := i48Night.Add(24 * time.Hour)
	if err := v.cfg.RecordConsolidationRun(ctx, i48Night); err != nil {
		t.Fatal(err)
	}
	if err := v.units.Create(ctx, unit.Unit{ID: "u-new", Type: unit.TypeKnowledge, Status: unit.StatusPool, Content: "quarterly offsite budget",
		Source: "chat", Weight: 1, LastTouchedAt: night.Add(-time.Hour), CreatedAt: night.Add(-time.Hour), UpdatedAt: night.Add(-time.Hour)}); err != nil {
		t.Fatal(err)
	}
	v.rels.EnsureUnit(t, "u-new")
	lex := memrepo.NewLexical()
	lex.SeedLexical(t, "u-new", "quarterly offsite budget")
	lex.SeedLexical(t, "n7", "quarterly offsite budget")

	dir := t.TempDir()
	body, err := json.Marshal(map[string]any{"id": "judge-033", "provider": "openai", "model": "gpt-6-luna", "task": "relation_evaluation",
		"message": "quarterly offsite budget", "candidates": []map[string]string{{"id": "n7", "content": "note n7"}},
		"response": `{"outcome":"related","target_unit_id":"n7","type":"same_topic","strength":0.5,"confidence":0.33}`})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "judge-033.json"), body, 0o600); err != nil {
		t.Fatal(err)
	}
	judge := fakeprovider.New(t, dir, "judge-033")
	rec := brain.NewRecallService(brain.NewIndex(recall.VectorIndex{Model: "test-model"}), lex, v.units, fakeprovider.NewEmbeddingFake("test-model"))
	phase := consolidation.PhaseConnect
	svc := brain.NewConsolidateService(fixedClock{now: night}, v.cfg, v.units, v.rels, v.ids, v.log, rec, judge,
		memrepo.NewSelfModel(), memrepo.NewState(), v.questions, v.signals)
	if _, err := svc.Consolidate(ctx, brain.ConsolidateRequest{Phase: &phase}); err != nil {
		t.Fatalf("connect: %v", err)
	}
	got, err := v.rels.ByUnit(ctx, "u-new")
	if err != nil {
		t.Fatal(err)
	}
	return len(got) > 0
}

// A night whose evidence moves nothing writes nothing (I12): five rejections
// are not six.
func TestI48_ANightThatLearnsNothingWritesNothing(t *testing.T) {
	v := newI48Vault(t)
	for i := 1; i <= 5; i++ {
		v.graph(t, fmt.Sprintf("r%d", i), true)
	}
	v.learn(t, i48Night)
	if got := v.bar(t, "same_topic"); got.Persist != relation.DefaultMinConfidenceToPersist {
		t.Errorf("bar moved to %+v on five rejections", got)
	}
	if n := len(v.learnRows(t)); n != 0 {
		t.Errorf("%d learn rows for a night that changed nothing, want 0", n)
	}
}

// A type the user pinned is never written, whatever the evidence.
func TestI48_APinnedTypeIsNeverWritten(t *testing.T) {
	v := newI48Vault(t)
	v.rels.SeedPinnedThreshold(t, "same_topic", 0.30, 0.50)
	v.judgeSix(t)
	v.learn(t, i48Night)
	if got := v.bar(t, "same_topic"); got.Persist != 0.30 {
		t.Errorf("a pinned bar moved to %+v", got)
	}
	if n := len(v.learnRows(t)); n != 0 {
		t.Errorf("%d learn rows for a pinned type, want 0", n)
	}
}
