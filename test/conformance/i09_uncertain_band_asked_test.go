// Package conformance — see test/conformance/doc.go for the package contract.
package conformance

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/brain"
	"github.com/rengo/nooma/internal/core/consolidation"
	"github.com/rengo/nooma/internal/core/prospection"
	"github.com/rengo/nooma/internal/core/recall"
	"github.com/rengo/nooma/internal/core/unit"
	"github.com/rengo/nooma/internal/ports"
	"github.com/rengo/nooma/test/support/fakechannel"
	"github.com/rengo/nooma/test/support/fakeprovider"
	"github.com/rengo/nooma/test/support/memrepo"
)

// i09SourceContent and i09CandidateContent are the two endpoints part (b)
// must find named in the digest's rendered text, once PR5 ships that
// (task 5.7). Named as constants so both subtests below read the identical
// strings rather than two copies that could quietly drift apart.
const (
	i09SourceContent    = "plan the quarterly offsite in Lisbon"
	i09CandidateContent = "quarterly offsite travel budget"
	i09CandidateID      = "3527ca73-93c4-4688-a680-145243ce1e04"
)

// i09QueueUncertainRelation drives connect's real dispatch — the same path
// TestConsolidateRunner_Connect_PersistsAcceptedJudgmentThroughRealDispatch
// (internal/brain/consolidate_test.go) and
// TestCapture_RelationJudgePersistsOutcomeMatchingConfidenceBand
// (capture_relation_judge_test.go) already prove the band for — to reach a
// real, persisted Uncertain-band relation with its own queued
// pending_questions row. Both I09 subtests below build on this one fixture:
// part (a) asserts directly on its output, part (b) additionally runs a
// digest pass over the same units.
//
// "relation-related-uncertain-band" scripts confidence 0.40, inside the
// defaults [Persist, Surface) = [0.30, 0.50).
func i09QueueUncertainRelation(t *testing.T) (units *memrepo.Units, rels *memrepo.Relations, questions *memrepo.PendingQuestions, relID string) {
	t.Helper()
	ctx := context.Background()
	now := time.Date(2026, 8, 1, 9, 30, 0, 0, time.UTC)
	since := now.Add(-time.Hour)

	units = memrepo.NewUnits()
	for _, seed := range []struct {
		id, content   string
		lastTouchedAt time.Time
	}{
		{"u-source", i09SourceContent, now},
		{i09CandidateID, i09CandidateContent, since.Add(-time.Hour)},
	} {
		if err := units.Create(ctx, unit.Unit{
			ID: seed.id, Type: unit.TypeKnowledge, Status: unit.StatusPool,
			Content: seed.content, Source: "chat",
			Weight: 1.0, WeightDecayRate: 0, LastTouchedAt: seed.lastTouchedAt, CreatedAt: now, UpdatedAt: now,
		}); err != nil {
			t.Fatalf("seed %s: %v", seed.id, err)
		}
	}

	lex := memrepo.NewLexical()
	lex.SeedLexical(t, "u-source", "quarterly offsite Lisbon")
	lex.SeedLexical(t, i09CandidateID, "quarterly offsite budget")

	rels = memrepo.NewRelations()
	rels.EnsureUnit(t, "u-source")
	rels.EnsureUnit(t, i09CandidateID)

	cfg := memrepo.NewConfig()
	if err := cfg.RecordConsolidationRun(ctx, since); err != nil {
		t.Fatalf("seed since: %v", err)
	}

	rec := brain.NewRecallService(brain.NewIndex(recall.VectorIndex{Model: "test-model"}), lex, units, fakeprovider.NewEmbeddingFake("test-model"))
	judge := fakeprovider.New(t, testdataLLMCasesDir(t), "relation-related-uncertain-band")
	ids := &counterIDs{}

	// counterIDs is deterministic ("id-1", "id-2", ...) and rel.ID is the
	// first id judgeAndPersistPair hands out in this codepath (rel.ID, then
	// the relation_persisted decision row, then the question's own id) —
	// the exact call-order fact the internal/brain real-dispatch test
	// relies on. memrepo.PendingQuestions' relation-existence set is its
	// own, independent of memrepo.Relations (it mirrors the real SQLite N1
	// guard, not a live foreign key), so it must be seeded under that exact
	// id ahead of the run.
	questions = memrepo.NewPendingQuestions()
	questions.EnsureRelation(t, "id-1", "same_topic", "u-source", i09CandidateID, i09SourceContent, i09CandidateContent)

	svc := brain.NewConsolidateService(fixedClock{now: now}, cfg, units, rels, ids, memrepo.NewDecisionLog(), rec, judge, memrepo.NewSelfModel(), memrepo.NewState(), questions)
	phase := consolidation.PhaseConnect
	if _, err := svc.Consolidate(ctx, brain.ConsolidateRequest{Phase: &phase}); err != nil {
		t.Fatalf("Consolidate(PhaseConnect): %v", err)
	}

	got, err := rels.ByUnit(ctx, "u-source")
	if err != nil {
		t.Fatalf("relations.ByUnit(u-source): %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("relations.ByUnit(u-source) = %v, want exactly 1 — this fixture's own setup is broken, not I09", got)
	}
	if got[0].Confidence != 0.40 {
		t.Fatalf("relation Confidence = %v, want the judge's recorded 0.40 (the Uncertain band [0.30, 0.50)) — this fixture's own setup is broken, not I09", got[0].Confidence)
	}
	return units, rels, questions, got[0].ID
}

// TestI09_UncertainBandStoresRelationAndQueuesQuestion is I09's storing
// half (design §8, task 4.2 part (a)): a judgment landing in
// [min_confidence_to_persist, min_confidence_to_surface) — the Uncertain
// band — is stored through connect's real dispatch AND queues exactly one
// pending_questions row naming the relation just persisted.
//
// Satisfiable by this PR alone (PR4) — unlike its sibling below.
func TestI09_UncertainBandStoresRelationAndQueuesQuestion(t *testing.T) {
	_, _, questions, relID := i09QueueUncertainRelation(t)

	unasked, err := questions.Unasked(context.Background())
	if err != nil {
		t.Fatalf("questions.Unasked: %v", err)
	}
	if len(unasked) != 1 {
		t.Fatalf("questions.Unasked() = %+v, want exactly 1 (I09's storing half: an Uncertain-band relation queues a question)", unasked)
	}
	if unasked[0].RelationID != relID {
		t.Errorf("Unasked()[0].RelationID = %q, want the persisted relation's own id %q", unasked[0].RelationID, relID)
	}
	if unasked[0].FromContent != i09SourceContent || unasked[0].ToContent != i09CandidateContent {
		t.Errorf("Unasked()[0] contents = (%q, %q), want (%q, %q)",
			unasked[0].FromContent, unasked[0].ToContent, i09SourceContent, i09CandidateContent)
	}
}

// TestI09_QuestionIsNotYetNamedInTheDigest is I09's asking half (design
// §8, task 4.2 part (b)): the next due digest's rendered text is supposed
// to name both endpoints of an open relation question. It genuinely does
// not yet — checkRunner has no pending_questions read at all until PR5
// wires the digest's second item source (task 5.1-5.4) and
// checkRunner.questions (task 5.9, 5.12) — so this subtest fails on
// purpose. Task 4.2's own instruction: created RED here, turned GREEN in
// PR5 (task 5.7), disclosed rather than weakened or skipped.
//
// The trigger fixture below exists only so a digest is sent AT ALL today
// ("an empty digest is not sent" — digest.go's own rule): with zero due
// trigger items and nothing else that can put an item in the digest yet,
// assembleDigest would return before ever reaching renderDigest, and this
// subtest would prove nothing about I09's asking half specifically.
func TestI09_QuestionIsNotYetNamedInTheDigest(t *testing.T) {
	units, _, _, _ := i09QueueUncertainRelation(t)
	ctx := context.Background()

	triggers := memrepo.NewTriggers()
	sourceID := "u-source"
	fireAt := time.Date(2026, 8, 5, prospection.DigestHour, 0, 0, 0, time.UTC).Add(-time.Hour)
	if err := triggers.Create(ctx, ports.Trigger{
		ID: "trg-1", Kind: ports.TriggerKindTimeBased, FireAt: &fireAt, UnitID: &sourceID,
		Payload: ports.TriggerPayload{ActionText: "renew the passport"}, CreatedAt: fireAt.Add(-time.Hour),
	}); err != nil {
		t.Fatalf("seed trigger: %v", err)
	}

	ch := fakechannel.New()
	now := time.Date(2026, 8, 5, prospection.DigestHour, 5, 0, 0, time.UTC)
	report, err := brain.NewCheckService(fixedClock{now: now}, triggers, memrepo.NewTimers(), &counterIDs{}, memrepo.NewDecisionLog(), ch, units, memrepo.NewState(), nil, "12449194").
		Check(ctx, brain.CheckRequest{})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if report.DigestCarried == 0 {
		t.Fatalf("DigestCarried = 0, want at least 1 — this fixture's own trigger setup is broken, not I09's asking half")
	}

	sent := ch.Sent(t)
	if len(sent) != 1 {
		t.Fatalf("channel received %d message(s), want exactly 1 digest: %+v", len(sent), sent)
	}

	namesBoth := strings.Contains(sent[0].Text, i09SourceContent) && strings.Contains(sent[0].Text, i09CandidateContent)
	if namesBoth {
		t.Fatalf("digest text = %q already names both endpoints — I09's asking half is GREEN; "+
			"update this subtest's own doc comment (task 5.7) instead of leaving it claiming RED", sent[0].Text)
	}
	t.Logf("expected RED (I09's asking half ships in PR5): digest text = %q does not yet name %q or %q",
		sent[0].Text, i09SourceContent, i09CandidateContent)
	t.Fatal("I09's asking half is not built yet (task 5.1-5.4, 5.9, 5.12) — the digest does not read pending_questions, so its rendered text cannot name the relation's two endpoints. Expected to fail until PR5 (task 5.7 turns this GREEN); disclosed in tasks.md, not a defect.")
}
