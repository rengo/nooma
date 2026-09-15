// Package conformance — see test/conformance/doc.go for the package contract.
package conformance

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/brain"
	"github.com/rengo/nooma/internal/ports"
	"github.com/rengo/nooma/test/support/fakeprovider"
	"github.com/rengo/nooma/test/support/memrepo"
)

// relationAnswerNow is the instant every fixture below captures at.
var relationAnswerNow = time.Date(2026, 8, 5, 10, 0, 0, 0, time.UTC)

// relationAnswerFixture wires a capture whose inbound text is a reply to a
// relation question, over openIDs open pending questions — each with its
// own real relation — and returns what the run left behind.
//
// It drives the REAL pipeline (brain.Capture, classify.Decode, the real
// resolveRelationCheckIn) rather than the runner directly, because the
// property under test is about untrusted inbound TEXT reaching an
// irreversible delete, and the decode step is part of that path.
//
// memrepo.PendingQuestions keeps its own relation-existence set,
// independent of memrepo.Relations — it mirrors the real SQLite insert
// guard, not a live foreign key — so both are seeded.
func relationAnswerFixture(t *testing.T, classifyCase string, openIDs ...string) (*memrepo.Relations, *memrepo.PendingQuestions, *memrepo.DecisionLog, *memrepo.Signals) {
	t.Helper()
	ctx := context.Background()

	relations := memrepo.NewRelations()
	questions := memrepo.NewPendingQuestions()
	decisions := memrepo.NewDecisionLog()
	signals := memrepo.NewSignals()

	for i, id := range openIDs {
		relID := "rel-" + id
		from, to := "u-"+id+"-from", "u-"+id+"-to"
		relations.EnsureUnit(t, from)
		relations.EnsureUnit(t, to)
		if err := relations.Upsert(ctx, ports.Relation{
			ID: relID, FromUnitID: from, ToUnitID: to, Type: "same_topic",
			Strength: 0.5, Confidence: 0.4, CreatedBy: "consolidation",
			CreatedAt: relationAnswerNow.Add(-24 * time.Hour),
		}); err != nil {
			t.Fatalf("seeding relation %s: %v", relID, err)
		}
		questions.EnsureRelation(t, relID, "same_topic", from, to, "content of "+from, "content of "+to)
		if err := questions.Create(ctx, ports.PendingQuestion{
			ID: id, Kind: ports.QuestionKindRelation, RelationID: relID,
			CreatedAt: relationAnswerNow.Add(-24 * time.Hour),
		}); err != nil {
			t.Fatalf("seeding question %s: %v", id, err)
		}
		// Asked in slice order, each an hour after the last, so Open's
		// most-recently-asked-first ordering is a fact of the fixture and
		// not of the map iteration that produced it.
		if err := questions.MarkAsked(ctx, id, relationAnswerNow.Add(-time.Duration(len(openIDs)-i)*time.Hour)); err != nil {
			t.Fatalf("marking question %s asked: %v", id, err)
		}
	}

	embeddings := memrepo.NewEmbeddings()
	idx, err := embeddings.LoadIndex(ctx, embedFakeModel)
	if err != nil {
		t.Fatalf("embeddings.LoadIndex(%q): %v", embedFakeModel, err)
	}

	// Two scripted calls: the classification, then the conversational
	// reply a chitchat gets (ADR-0021). No judge call — a chitchat
	// persists no unit, so nothing reaches the relation judge.
	llm := fakeprovider.New(t, testdataLLMCasesDir(t), classifyCase, "chat-relation-answer-acknowledged")
	svc := brain.NewCaptureService(fixedClock{now: relationAnswerNow}, &counterIDs{},
		memrepo.NewUnits(), embeddings, memrepo.NewLexical(), relations, decisions,
		llm, llm, llm, fakeprovider.NewEmbeddingFake(embedFakeModel), brain.NewIndex(idx),
		signals, memrepo.NewTriggers(), memrepo.NewTimers(), 0.5, questions)

	if _, err := svc.Capture(ctx, brain.CaptureInput{Text: "a reply to a relation question", Channel: "chat"}); err != nil {
		t.Fatalf("Capture: %v", err)
	}
	return relations, questions, decisions, signals
}

// countRelations is how many relations survive, counted through the port
// rather than through the fake's internals.
func countRelations(t *testing.T, relations *memrepo.Relations, unitIDs ...string) int {
	t.Helper()
	n := 0
	for _, id := range unitIDs {
		got, err := relations.ByUnit(context.Background(), id)
		if err != nil {
			t.Fatalf("relations.ByUnit(%q): %v", id, err)
		}
		n += len(got)
	}
	return n
}

// TestRelationAnswer_AnUndecodableOutcomeDeletesNothing is design §9's one
// applicable threat-matrix row: untrusted inbound text reaching the only
// irreversible act in this vault.
//
// A model that answers outside the relation_outcome vocabulary degrades
// that field to null (I14) and the capture carries on being whatever else
// it is. What must NOT happen is a near-miss being coerced into
// "rejected": a delete is not recoverable, and the whole disambiguation
// path is downstream of a string this system did not write.
func TestRelationAnswer_AnUndecodableOutcomeDeletesNothing(t *testing.T) {
	relations, questions, decisions, signals := relationAnswerFixture(t,
		"classify-relation-answer-unknown-outcome", "q-1")
	ctx := context.Background()

	if n := countRelations(t, relations, "u-q-1-from"); n != 1 {
		t.Fatalf("%d relation(s) survive, want 1 — an outcome outside the vocabulary must delete nothing", n)
	}
	open, err := questions.Open(ctx)
	if err != nil {
		t.Fatalf("questions.Open: %v", err)
	}
	if len(open) != 1 {
		t.Errorf("Open() = %+v, want the question still open — nothing answered it", open)
	}

	rows, err := decisions.Since(ctx, relationAnswerNow.Add(-time.Hour), -1)
	if err != nil {
		t.Fatalf("decisions.Since: %v", err)
	}
	for _, row := range rows {
		if row.Action == ports.ActionCaptureRelationCheckInResolved || row.Action == ports.ActionCaptureRelationCheckInUnmatched {
			t.Errorf("an undecodable outcome wrote a %q row — it answers no relation question at all, so there is nothing to record", row.Action)
		}
	}
	assertNoRelationSignals(t, signals)
}

// TestRelationAnswer_ARejectionWithNothingOpenDeletesNothing is R8 and
// owner ruling Q5, driven end to end.
//
// This is the case the threat matrix is actually about. A "no" that
// matches no open question gives disambiguation nothing to work with, and
// guessing which relation to delete would be a guess with no undo. One
// audit row, and the graph is untouched.
func TestRelationAnswer_ARejectionWithNothingOpenDeletesNothing(t *testing.T) {
	relations, _, decisions, signals := relationAnswerFixture(t, "classify-relation-answer-rejected")
	ctx := context.Background()

	if n := countRelations(t, relations); n != 0 {
		t.Fatalf("the fixture seeded %d relation(s) — this test seeds none on purpose", n)
	}

	rows, err := decisions.Since(ctx, relationAnswerNow.Add(-time.Hour), -1)
	if err != nil {
		t.Fatalf("decisions.Since: %v", err)
	}
	unmatched := 0
	for _, row := range rows {
		if row.Action == ports.ActionCaptureRelationCheckInUnmatched {
			unmatched++
		}
		if row.Action == ports.ActionCaptureRelationCheckInResolved {
			t.Error("a rejection with nothing open recorded a resolution")
		}
	}
	if unmatched != 1 {
		t.Errorf("%d unmatched row(s), want 1 — an answer that arrived after its question vanished is worth seeing", unmatched)
	}
	assertNoRelationSignals(t, signals)
}

// TestRelationAnswer_ARejectionWithThreeOpenDeletesExactlyOne is R4's
// disambiguation with its audit trail, driven end to end.
//
// Three open questions and one "no". Exactly one relation goes, and the
// row records that there were three — because an audit trail that showed
// a deletion without showing the ambiguity behind it would read as though
// there had only ever been one candidate.
func TestRelationAnswer_ARejectionWithThreeOpenDeletesExactlyOne(t *testing.T) {
	relations, questions, decisions, signals := relationAnswerFixture(t,
		"classify-relation-answer-rejected", "q-oldest", "q-older", "q-most-recent")
	ctx := context.Background()

	surviving := countRelations(t, relations, "u-q-oldest-from", "u-q-older-from", "u-q-most-recent-from")
	if surviving != 2 {
		t.Fatalf("%d relation(s) survive, want 2 — one answer deletes exactly one relation", surviving)
	}
	if n := countRelations(t, relations, "u-q-most-recent-from"); n != 0 {
		t.Errorf("the most recently asked question's relation survived — Open orders most recent first, and that is the one an answer answers")
	}

	open, err := questions.Open(ctx)
	if err != nil {
		t.Fatalf("questions.Open: %v", err)
	}
	if len(open) != 2 {
		t.Errorf("Open() has %d question(s), want 2 — only the one answered is closed", len(open))
	}

	rows, err := decisions.Since(ctx, relationAnswerNow.Add(-time.Hour), -1)
	if err != nil {
		t.Fatalf("decisions.Since: %v", err)
	}
	var resolved []ports.Decision
	for _, row := range rows {
		if row.Action == ports.ActionCaptureRelationCheckInResolved {
			resolved = append(resolved, row)
		}
	}
	if len(resolved) != 1 {
		t.Fatalf("%d resolved row(s), want 1", len(resolved))
	}
	var got struct {
		QuestionID            string `json:"question_id"`
		RelationID            string `json:"relation_id"`
		Resolution            string `json:"resolution"`
		OpenRelationQuestions int    `json:"open_relation_questions"`
	}
	if err := json.Unmarshal(resolved[0].Context, &got); err != nil {
		t.Fatalf("decoding the resolved row's context %s: %v", resolved[0].Context, err)
	}
	if got.OpenRelationQuestions != 3 {
		t.Errorf("open_relation_questions = %d, want 3 — the ambiguity is the fact worth recording", got.OpenRelationQuestions)
	}
	if got.QuestionID != "q-most-recent" || got.RelationID != "rel-q-most-recent" {
		t.Errorf("context = %+v, want the chosen question and the relation it named", got)
	}
	if got.Resolution != "rejected" {
		t.Errorf("resolution = %q, want \"rejected\"", got.Resolution)
	}

	// I10, on the one path that reaches a real relation: the signal for
	// the deletion exists, and it names what was deleted.
	sigs, err := signals.Since(ctx, relationAnswerNow.Add(-time.Hour), -1)
	if err != nil {
		t.Fatalf("signals.Since: %v", err)
	}
	if len(sigs) != 1 || sigs[0].Type != ports.SignalRelationReject {
		t.Fatalf("signals = %+v, want exactly one relation_reject", sigs)
	}
	if sigs[0].TargetID == nil || *sigs[0].TargetID != "rel-q-most-recent" {
		t.Errorf("the reject signal names %v, want the relation that was deleted", sigs[0].TargetID)
	}
}

// assertNoRelationSignals fails unless the run emitted no relation signal
// at all — the shared half of the two no-op cases above.
func assertNoRelationSignals(t *testing.T, signals *memrepo.Signals) {
	t.Helper()
	sigs, err := signals.Since(context.Background(), relationAnswerNow.Add(-time.Hour), -1)
	if err != nil {
		t.Fatalf("signals.Since: %v", err)
	}
	for _, s := range sigs {
		if s.Type == ports.SignalRelationReject || s.Type == ports.SignalRelationConfirm {
			t.Errorf("a %q signal was emitted for an answer that resolved nothing — the learning module would tune on it forever", s.Type)
		}
	}
}
