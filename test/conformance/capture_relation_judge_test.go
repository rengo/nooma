// Package conformance — see test/conformance/doc.go for the package contract.
package conformance

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/brain"
	"github.com/rengo/nooma/internal/ports"
	"github.com/rengo/nooma/test/support/fakeprovider"
	"github.com/rengo/nooma/test/support/memrepo"
)

// judgeTestFixture wires one capture whose recall step finds exactly one
// candidate — candidateID, seeded so only the vector leg can find it, the
// same single-leg-match shape capture_recall_test.go already establishes —
// then runs the capture with judgeCaseID scripted as the relation judge's
// second Complete call. This is the wiring task 11c.2 adds to captureRunner
// (design D4's diagram tail): one candidate is enough to prove it end to
// end, and every 11c.2/11c.3/11c.4 scenario differs only in what the
// scripted judge response says, not in how the candidate got there.
func judgeTestFixture(t *testing.T, candidateID, judgeCaseID string) (result brain.CaptureResult, units *memrepo.Units, relations *memrepo.Relations, decisions *memrepo.DecisionLog) {
	t.Helper()

	ctx := context.Background()
	now := time.Date(2026, 8, 1, 9, 30, 0, 0, time.UTC)

	units = memrepo.NewUnits()
	decisions = memrepo.NewDecisionLog()
	embeddings := memrepo.NewEmbeddings()
	lexical := memrepo.NewLexical()
	relations = memrepo.NewRelations()

	const newContent = "Pick up the dry cleaning"

	setupEmbed := fakeprovider.NewEmbeddingFake(embedFakeModel)
	matchVector, err := setupEmbed.Embed(ctx, ports.EmbedRequest{Text: newContent})
	if err != nil {
		t.Fatalf("deriving the match vector: %v", err)
	}

	if err := units.Create(ctx, poolUnit(candidateID, "unrelated stored text")); err != nil {
		t.Fatalf("seeding %s: %v", candidateID, err)
	}
	if err := embeddings.Put(ctx, ports.Embedding{UnitID: candidateID, Model: embedFakeModel, Vector: matchVector.Vector, At: now}); err != nil {
		t.Fatalf("seeding %s's embedding: %v", candidateID, err)
	}

	idx, err := embeddings.LoadIndex(ctx, embedFakeModel)
	if err != nil {
		t.Fatalf("embeddings.LoadIndex(%q): %v", embedFakeModel, err)
	}

	llm := fakeprovider.New(t, testdataLLMCasesDir(t), "classify-pick-up-dry-cleaning", judgeCaseID)
	embed := fakeprovider.NewEmbeddingFake(embedFakeModel)
	svc := brain.NewCaptureService(fixedClock{now: now}, &counterIDs{}, units, embeddings, lexical, relations, decisions, llm, llm, embed, brain.NewIndex(idx), memrepo.NewSignals(), memrepo.NewTriggers(), memrepo.NewTimers())

	result, err = svc.Capture(ctx, brain.CaptureInput{
		Text:    "Pick up the dry cleaning on Friday",
		Channel: "chat",
	})
	if err != nil {
		t.Fatalf("Capture error = %v, want nil", err)
	}
	if len(result.Candidates) != 1 || result.Candidates[0] != candidateID {
		t.Fatalf("Candidates = %v, want exactly [%q] — this test's own setup is broken, not the judge wiring under test", result.Candidates, candidateID)
	}
	return result, units, relations, decisions
}

// decisionRowCount is a small helper shared by this file's three tests:
// every scenario here reads the whole log back and counts rows, the same
// pattern capture_pipeline_test.go and capture_ambiguous_person_ref_test.go
// already use.
func decisionRows(t *testing.T, decisions *memrepo.DecisionLog, since time.Time) []ports.Decision {
	t.Helper()
	rows, err := decisions.Since(context.Background(), since.Add(-time.Hour), -1)
	if err != nil {
		t.Fatalf("decisions.Since: %v", err)
	}
	return rows
}

// TestCapture_RelationJudgePersistsOutcomeMatchingConfidenceBand is spec
// R5.5's own "Verified by" scenario: the judge's full path —
// ports.LLMProvider.Complete with Task "relation_evaluation" ->
// relation.DecodeJudgment -> relation.Decide against the resolved
// Thresholds — driven through fakeprovider/memrepo end to end (task 11c.2).
//
// "relation-related-uncertain-band" scripts confidence 0.40, inside
// [min_confidence_to_persist, min_confidence_to_surface) = [0.30, 0.50) —
// the Uncertain band specifically, not the trivial highest band, so this
// test also proves relation.Resolve's fallback (no relation_thresholds row
// exists in a fresh memrepo.Relations) is exercised for real, not merely
// reachable.
func TestCapture_RelationJudgePersistsOutcomeMatchingConfidenceBand(t *testing.T) {
	now := time.Date(2026, 8, 1, 9, 30, 0, 0, time.UTC)
	result, _, relations, decisions := judgeTestFixture(t, "3527ca73-93c4-4688-a680-145243ce1e04", "relation-related-uncertain-band")

	rels, err := relations.ByUnit(context.Background(), result.UnitID)
	if err != nil {
		t.Fatalf("relations.ByUnit(%q): %v", result.UnitID, err)
	}
	if len(rels) != 1 {
		t.Fatalf("relations.ByUnit(%q) = %v, want exactly 1 relation", result.UnitID, rels)
	}
	rel := rels[0]
	if rel.FromUnitID != result.UnitID {
		t.Errorf("FromUnitID = %q, want the new unit %q", rel.FromUnitID, result.UnitID)
	}
	if rel.ToUnitID != "3527ca73-93c4-4688-a680-145243ce1e04" {
		t.Errorf("ToUnitID = %q, want %q", rel.ToUnitID, "3527ca73-93c4-4688-a680-145243ce1e04")
	}
	if rel.Type != "same_topic" {
		t.Errorf("Type = %q, want the judge's own %q", rel.Type, "same_topic")
	}
	if rel.Confidence != 0.40 {
		t.Errorf("Confidence = %v, want the judge's recorded 0.40 — the persisted outcome must match the recorded confidence's band", rel.Confidence)
	}
	if rel.CreatedBy != "system" {
		t.Errorf("CreatedBy = %q, want %q — an automatic judge decision, not a user correction (doc 02 §4)", rel.CreatedBy, "system")
	}

	rows := decisionRows(t, decisions, now)
	if len(rows) != 2 {
		t.Fatalf("decision_log has %d rows, want exactly 2 (capture.classify + relation.persisted): %+v", len(rows), rows)
	}
	var persistedRow *ports.Decision
	for i := range rows {
		if rows[i].Action == ports.ActionRelationPersisted {
			persistedRow = &rows[i]
		}
	}
	if persistedRow == nil {
		t.Fatalf("no %q row found among %+v", ports.ActionRelationPersisted, rows)
	}
	if persistedRow.Rationale == "" {
		t.Error("relation.persisted Rationale is empty — doc 02 §11 requires a human-readable sentence")
	}
}

// TestCapture_RelationJudgeDiscardsBelowMinConfidenceToPersist is spec
// R5.4's own scenario (I08, task 11c.3): a candidate below
// min_confidence_to_persist leaves no relations row and exactly one
// decision_log row recording the discard and its rationale.
//
// "relation-discard-low-confidence" scripts confidence 0.10 — R5.4's own
// stated scenario value, well below the 0.30 default.
func TestCapture_RelationJudgeDiscardsBelowMinConfidenceToPersist(t *testing.T) {
	now := time.Date(2026, 8, 1, 9, 30, 0, 0, time.UTC)
	result, _, relations, decisions := judgeTestFixture(t, "ce8d8460-dfb3-42bf-9cd0-0fc74f3dab42", "relation-discard-low-confidence")

	rels, err := relations.ByUnit(context.Background(), result.UnitID)
	if err != nil {
		t.Fatalf("relations.ByUnit(%q): %v", result.UnitID, err)
	}
	if len(rels) != 0 {
		t.Fatalf("relations.ByUnit(%q) = %v, want none — I08: below min_confidence_to_persist, it is not even stored", result.UnitID, rels)
	}

	rows := decisionRows(t, decisions, now)
	if len(rows) != 2 {
		t.Fatalf("decision_log has %d rows, want exactly 2 (capture.classify + relation.discarded): %+v", len(rows), rows)
	}
	var discardRow *ports.Decision
	for i := range rows {
		if rows[i].Action == ports.ActionRelationDiscarded {
			discardRow = &rows[i]
		}
	}
	if discardRow == nil {
		t.Fatalf("no %q row found among %+v", ports.ActionRelationDiscarded, rows)
	}
	if discardRow.Rationale == "" {
		t.Error("relation.discarded Rationale is empty — doc 02 §11 requires a human-readable sentence")
	}
	if !discardRow.OccurredAt.Equal(now) {
		t.Errorf("relation.discarded OccurredAt = %v, want the single clock read %v", discardRow.OccurredAt, now)
	}
}

// TestCapture_RelationJudgeRecordsDuplicateWithoutMerging is design D7's own
// resolution (task 11c.4): a duplicate-outcome judgment writes a
// duplicate-typed relation from the new unit to the existing one, and a
// decision_log row stating plainly the duplicate was recorded, not merged —
// neither superseding the new unit nor reviving the existing one.
//
// "relation-duplicate-high-confidence" scripts confidence 0.85, comfortably
// inside the Asserted band, so this scenario is never mistaken for the
// discard path above.
func TestCapture_RelationJudgeRecordsDuplicateWithoutMerging(t *testing.T) {
	now := time.Date(2026, 8, 1, 9, 30, 0, 0, time.UTC)
	result, _, relations, decisions := judgeTestFixture(t, "d7f4da5a-d2e4-4595-afdc-ecdad45b8579", "relation-duplicate-high-confidence")

	rels, err := relations.ByUnit(context.Background(), result.UnitID)
	if err != nil {
		t.Fatalf("relations.ByUnit(%q): %v", result.UnitID, err)
	}
	if len(rels) != 1 {
		t.Fatalf("relations.ByUnit(%q) = %v, want exactly 1 relation", result.UnitID, rels)
	}
	rel := rels[0]
	if rel.FromUnitID != result.UnitID {
		t.Errorf("FromUnitID = %q, want the new unit %q — design D7: the direction is the new unit -> the existing one", rel.FromUnitID, result.UnitID)
	}
	if rel.ToUnitID != "d7f4da5a-d2e4-4595-afdc-ecdad45b8579" {
		t.Errorf("ToUnitID = %q, want %q", rel.ToUnitID, "d7f4da5a-d2e4-4595-afdc-ecdad45b8579")
	}
	if rel.Type != "duplicate" {
		t.Errorf("Type = %q, want %q", rel.Type, "duplicate")
	}

	rows := decisionRows(t, decisions, now)
	if len(rows) != 2 {
		t.Fatalf("decision_log has %d rows, want exactly 2 (capture.classify + relation.duplicate.recorded): %+v", len(rows), rows)
	}
	var dupRow *ports.Decision
	for i := range rows {
		if rows[i].Action == ports.ActionRelationDuplicateRecorded {
			dupRow = &rows[i]
		}
	}
	if dupRow == nil {
		t.Fatalf("no %q row found among %+v", ports.ActionRelationDuplicateRecorded, rows)
	}
	if !strings.Contains(dupRow.Rationale, "not merged") {
		t.Errorf("relation.duplicate.recorded Rationale = %q, want it to state plainly that the duplicate was recorded, not merged (design D7)", dupRow.Rationale)
	}
	if !dupRow.OccurredAt.Equal(now) {
		t.Errorf("relation.duplicate.recorded OccurredAt = %v, want the single clock read %v", dupRow.OccurredAt, now)
	}
}

// TestCapture_RelationJudgeProviderFailureLeavesUnitPersisted is C14a's own
// resolution, mirroring TestCapture_EmbeddingProviderFailureLeavesUnitPersisted
// (capture_embed_test.go) for the judge step instead of the embed step: a
// scripted r.judge.Complete failure leaves the unit persisted, stores no
// relations, writes exactly one capture.dedup.failed decision_log row
// carrying the provider error in its context, and Capture itself does not
// return an error — the same posture design D8 already takes for an
// embedding-provider outage, now taken for a judge-provider outage too
// rather than propagating it (design D4's diagram, corrected).
func TestCapture_RelationJudgeProviderFailureLeavesUnitPersisted(t *testing.T) {
	now := time.Date(2026, 8, 1, 9, 30, 0, 0, time.UTC)
	result, units, relations, decisions := judgeTestFixture(t, "279d538a-8cc4-4a21-85f7-d006fb9b3980", "relation-judge-provider-outage")

	if _, err := units.ByID(context.Background(), result.UnitID); err != nil {
		t.Fatalf("units.ByID(%q): %v — the unit must be persisted even though the relation judge failed", result.UnitID, err)
	}

	rels, err := relations.ByUnit(context.Background(), result.UnitID)
	if err != nil {
		t.Fatalf("relations.ByUnit(%q): %v", result.UnitID, err)
	}
	if len(rels) != 0 {
		t.Fatalf("relations.ByUnit(%q) = %v, want none — a judge-provider outage must store no relations", result.UnitID, rels)
	}

	rows := decisionRows(t, decisions, now)
	if len(rows) != 2 {
		t.Fatalf("decision_log has %d rows, want exactly 2 (capture.classify + capture.dedup.failed): %+v", len(rows), rows)
	}
	var failedRow *ports.Decision
	for i := range rows {
		if rows[i].Action == ports.ActionCaptureDedupFailed {
			failedRow = &rows[i]
		}
	}
	if failedRow == nil {
		t.Fatalf("no %q row found among %+v", ports.ActionCaptureDedupFailed, rows)
	}
	if failedRow.Rationale == "" {
		t.Error("capture.dedup.failed Rationale is empty — doc 02 §11 requires a human-readable sentence")
	}
	if !failedRow.OccurredAt.Equal(now) {
		t.Errorf("capture.dedup.failed OccurredAt = %v, want the single clock read %v", failedRow.OccurredAt, now)
	}

	var ctx struct {
		UnitID string `json:"unit_id"`
		Error  string `json:"error"`
	}
	if err := json.Unmarshal(failedRow.Context, &ctx); err != nil {
		t.Fatalf("capture.dedup.failed Context is not valid JSON: %v (%s)", err, failedRow.Context)
	}
	if ctx.UnitID != result.UnitID {
		t.Errorf("capture.dedup.failed Context.unit_id = %q, want %q", ctx.UnitID, result.UnitID)
	}
	const wantErr = "ollama: connection refused"
	if ctx.Error != wantErr {
		t.Errorf("capture.dedup.failed Context.error = %q, want the provider error %q", ctx.Error, wantErr)
	}
}
