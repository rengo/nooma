//go:build e2e

package e2e

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/brain"
	"github.com/rengo/nooma/internal/core/prospection"
	"github.com/rengo/nooma/internal/core/relation"
	"github.com/rengo/nooma/internal/core/unit"
	"github.com/rengo/nooma/internal/ports"
	"github.com/rengo/nooma/internal/store/sqlite"
	"github.com/rengo/nooma/test/support/fakechannel"
	"github.com/rengo/nooma/test/support/fakeprovider"
)

// The two units the demo's uncertain relation links, and the text the
// digest is expected to quote back.
const (
	relationDemoFromContent = "plan the quarterly offsite in Lisbon"
	relationDemoToContent   = "quarterly offsite travel budget"
	relationDemoConfidence  = 0.40
)

// relationDemo is one vault carrying exactly one uncertain-band relation
// with a queued question about it, plus everything the two passes below
// need to run against it.
type relationDemo struct {
	db        *sqlite.Vault
	relations *sqlite.RelationRepo
	questions *sqlite.PendingQuestionRepo
	decisions *sqlite.DecisionLog
	signals   *sqlite.SignalRepo
	channel   *fakechannel.Fake
	ids       *demoIDs
	relID     string
}

// newRelationDemo builds that vault.
//
// The relation and its question are seeded through the real repositories
// rather than produced by a real connect pass: that half — an Uncertain
// band storing a relation AND queueing a question — is I09's own
// conformance test, proven there against connect's real dispatch. What no
// other test says is that ONE vault then carries that question out through
// a real digest and back in through a real capture, over real SQLite.
func newRelationDemo(t *testing.T, at time.Time) relationDemo {
	t.Helper()
	ctx := context.Background()

	home, work := t.TempDir(), t.TempDir()
	dbPath := vaultDBPath(t, initVault(t, home, work, "relation-demo.nooma"))
	db, err := sqlite.Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("sqlite.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	units := sqlite.NewUnitRepo(db)
	for _, seed := range []struct{ id, content string }{
		{"unit-offsite", relationDemoFromContent},
		{"unit-budget", relationDemoToContent},
	} {
		if err := units.Create(ctx, unit.Unit{
			ID: seed.id, Type: unit.TypeKnowledge, Status: unit.StatusPool,
			Content: seed.content, Source: "chat",
			Weight: 1.0, WeightDecayRate: 0.01,
			LastTouchedAt: at.Add(-48 * time.Hour), CreatedAt: at.Add(-48 * time.Hour), UpdatedAt: at.Add(-48 * time.Hour),
		}); err != nil {
			t.Fatalf("seeding unit %s: %v", seed.id, err)
		}
	}

	relations := sqlite.NewRelationRepo(db)
	const relID = "rel-offsite-budget"
	if err := relations.Upsert(ctx, ports.Relation{
		ID: relID, FromUnitID: "unit-offsite", ToUnitID: "unit-budget", Type: "same_topic",
		Strength: 0.6, Confidence: relationDemoConfidence, CreatedBy: "consolidation",
		CreatedAt: at.Add(-24 * time.Hour),
	}); err != nil {
		t.Fatalf("seeding the uncertain relation: %v", err)
	}
	// The band this whole demo is about — asserted here so a later change
	// to the defaults fails the fixture rather than the conclusion.
	if band := relation.Decide(relationDemoConfidence, relation.Resolve(nil)); band != relation.Uncertain {
		t.Fatalf("the seeded confidence %v lands in band %v, want Uncertain — this fixture's own setup is broken, not the demo", relationDemoConfidence, band)
	}

	questions := sqlite.NewPendingQuestionRepo(db)
	if err := questions.Create(ctx, ports.PendingQuestion{
		ID: "pq-offsite-budget", Kind: ports.QuestionKindRelation, RelationID: relID,
		CreatedAt: at.Add(-24 * time.Hour),
	}); err != nil {
		t.Fatalf("queueing the question: %v", err)
	}

	return relationDemo{
		db: db, relations: relations, questions: questions,
		decisions: sqlite.NewDecisionLog(db), signals: sqlite.NewSignalRepo(db),
		channel: fakechannel.New(), ids: &demoIDs{}, relID: relID,
	}
}

// askTheQuestion runs a real check pass and returns the digest's text,
// failing unless the digest went out naming both endpoints.
func (d relationDemo) askTheQuestion(t *testing.T, at time.Time) string {
	t.Helper()
	ctx := context.Background()

	if _, err := brain.NewCheckService(demoClock{now: at},
		sqlite.NewTriggerRepo(d.db), sqlite.NewTimerRepo(d.db), d.ids, d.decisions,
		d.channel, sqlite.NewUnitRepo(d.db), sqlite.NewStateRepo(d.db), nil, "12449194",
		d.questions).Check(ctx, brain.CheckRequest{}); err != nil {
		t.Fatalf("check: %v", err)
	}

	sent := d.channel.Sent(t)
	if len(sent) != 1 {
		t.Fatalf("the vault sent %d message(s), want exactly one digest — the vault has no due trigger at all, so the QUESTION is what makes this digest worth sending", len(sent))
	}
	text := sent[0].Text
	if !strings.Contains(text, relationDemoFromContent) || !strings.Contains(text, relationDemoToContent) {
		t.Fatalf("the digest does not name both endpoints:\n%s", text)
	}

	open, err := d.questions.Open(ctx)
	if err != nil {
		t.Fatalf("questions.Open: %v", err)
	}
	if len(open) != 1 {
		t.Fatalf("Open() = %+v after the digest, want the one question it asked", open)
	}
	return text
}

// answer runs a real capture pass over classifyCase — a chitchat carrying
// a relation_outcome, which is the shape an inbound reply actually has.
func (d relationDemo) answer(t *testing.T, at time.Time, classifyCase string) {
	t.Helper()
	ctx := context.Background()

	embeddings := sqlite.NewEmbeddingRepo(d.db)
	loaded, err := embeddings.LoadIndex(ctx, demoEmbedModel)
	if err != nil {
		t.Fatalf("embeddings.LoadIndex(%q): %v", demoEmbedModel, err)
	}

	// Two scripted calls, and no judge: a chitchat persists no unit, so
	// nothing reaches relation_evaluation.
	llm := fakeprovider.New(t, llmCasesDir(t), classifyCase, "chat-relation-answer-acknowledged")
	capture := brain.NewCaptureService(demoClock{now: at}, d.ids,
		sqlite.NewUnitRepo(d.db), embeddings, sqlite.NewSearch(d.db), d.relations, d.decisions,
		llm, llm, llm, fakeprovider.NewEmbeddingFake(demoEmbedModel), brain.NewIndex(loaded),
		d.signals, sqlite.NewTriggerRepo(d.db), sqlite.NewTimerRepo(d.db), 0.5, d.questions)

	if _, err := capture.Capture(ctx, brain.CaptureInput{Text: "a reply to the digest", Channel: "chat"}); err != nil {
		t.Fatalf("capture: %v", err)
	}
}

// relationSignals is every learning signal the vault holds, newest window.
func (d relationDemo) relationSignals(t *testing.T, since time.Time) []ports.Signal {
	t.Helper()
	sigs, err := d.signals.Since(context.Background(), since, -1)
	if err != nil {
		t.Fatalf("signals.Since: %v", err)
	}
	out := make([]ports.Signal, 0, len(sigs))
	for _, s := range sigs {
		if s.Type == ports.SignalRelationConfirm || s.Type == ports.SignalRelationReject {
			out = append(out, s)
		}
	}
	return out
}

// TestM3eDemo_TheDigestAsksAndTheAnswerLands is m3e's exit criterion and
// the proposal's own demo, both branches, against a real migrated vault.
//
// The claim is the chain, not any link in it. A vault holding one relation
// the nightly job was not sure about sends a digest that asks about it by
// name — a digest it would not have sent at all, because nothing else in
// this vault is due — and the user's one-line reply either raises that
// relation out of the uncertain band or deletes it, with a learning signal
// either way and decision_log telling the whole story.
//
// Two subtests rather than two questions in one vault: "yes" and "no" are
// mutually exclusive answers to the same question, and running them in one
// pass would prove neither.
func TestM3eDemo_TheDigestAsksAndTheAnswerLands(t *testing.T) {
	// A Wednesday at the digest hour, and the reply an hour later.
	morning := time.Date(2026, 8, 5, prospection.DigestHour, 5, 0, 0, time.UTC)
	replyAt := morning.Add(time.Hour)

	t.Run("yes raises it out of the band", func(t *testing.T) {
		demo := newRelationDemo(t, morning)
		ctx := context.Background()

		demo.askTheQuestion(t, morning)
		demo.answer(t, replyAt, "classify-relation-answer-confirmed")

		rel, err := demo.relations.ByID(ctx, demo.relID)
		if err != nil {
			t.Fatalf("relations.ByID: %v", err)
		}
		want := relation.DefaultMinConfidenceToSurface
		if rel.Confidence != want {
			t.Errorf("confidence = %v, want %v — confirming raises it to GREATEST(current, min_confidence_to_surface)", rel.Confidence, want)
		}
		if band := relation.Decide(rel.Confidence, relation.Resolve(nil)); band != relation.Asserted {
			t.Errorf("the confirmed relation is still in band %v — confirming is what takes it OUT of the uncertain band", band)
		}
		if rel.Strength != 0.6 || rel.CreatedBy != "consolidation" {
			t.Errorf("the confirmed relation reads %+v — only confidence is revised, and I07 revises in place", rel)
		}

		sigs := demo.relationSignals(t, morning.Add(-24*time.Hour))
		if len(sigs) != 1 || sigs[0].Type != ports.SignalRelationConfirm {
			t.Fatalf("signals = %+v, want exactly one relation_confirm — its first emission anywhere in this tree", sigs)
		}
		if sigs[0].TargetID == nil || *sigs[0].TargetID != demo.relID {
			t.Errorf("the confirm signal names %v, want the relation it confirmed", sigs[0].TargetID)
		}

		assertQuestionClosed(t, demo, ports.ActionCaptureRelationCheckInResolved)
	})

	t.Run("no deletes it, signal first", func(t *testing.T) {
		demo := newRelationDemo(t, morning)
		ctx := context.Background()

		demo.askTheQuestion(t, morning)
		demo.answer(t, replyAt, "classify-relation-answer-rejected")

		if _, err := demo.relations.ByID(ctx, demo.relID); err == nil {
			t.Fatal("the rejected relation survives — rejecting deletes it (I10)")
		}

		sigs := demo.relationSignals(t, morning.Add(-24*time.Hour))
		if len(sigs) != 1 || sigs[0].Type != ports.SignalRelationReject {
			t.Fatalf("signals = %+v, want exactly one relation_reject", sigs)
		}
		// I10's ordering, observed where it actually matters: the signal
		// outlives the relation it is about, because there is no foreign
		// key to take it with the row (I13, and ADR-0027's own reason for
		// pending_questions carrying none either).
		if sigs[0].TargetID == nil || *sigs[0].TargetID != demo.relID {
			t.Errorf("the reject signal names %v, want the relation that was deleted — a signal that lost its target is evidence about nothing", sigs[0].TargetID)
		}

		assertQuestionClosed(t, demo, ports.ActionCaptureRelationCheckInResolved)
	})
}

// assertQuestionClosed is the half both branches share: the question the
// digest asked is no longer open, and decision_log names what happened,
// in a sentence a person can read.
func assertQuestionClosed(t *testing.T, demo relationDemo, want ports.DecisionAction) {
	t.Helper()
	ctx := context.Background()

	open, err := demo.questions.Open(ctx)
	if err != nil {
		t.Fatalf("questions.Open: %v", err)
	}
	if len(open) != 0 {
		t.Errorf("Open() = %+v after the answer, want empty — an answered question is closed", open)
	}

	rows, err := demo.decisions.Since(ctx, time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC), -1)
	if err != nil {
		t.Fatalf("decisions.Since: %v", err)
	}
	seen := map[ports.DecisionAction]int{}
	for _, row := range rows {
		seen[row.Action]++
		if strings.TrimSpace(row.Rationale) == "" {
			t.Errorf("a %q row has no rationale — doc 02 §11 requires a sentence a person can read", row.Action)
		}
	}
	for _, action := range []ports.DecisionAction{
		ports.ActionCheckDigestSent,
		ports.ActionCheckDigestQuestionAsked,
		want,
	} {
		if seen[action] == 0 {
			t.Errorf("decision_log has no %q row; it holds %v", action, seen)
		}
	}
}
