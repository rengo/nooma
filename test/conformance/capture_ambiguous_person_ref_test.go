// Package conformance — see test/conformance/doc.go for the package contract.
package conformance

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/brain"
	"github.com/rengo/nooma/internal/core/unit"
	"github.com/rengo/nooma/internal/ports"
	"github.com/rengo/nooma/test/support/fakeprovider"
	"github.com/rengo/nooma/test/support/memrepo"
)

// TestCapture_AmbiguousPersonRefPersistsPoolUnitAndLogsTwice is spec R4.7's
// own scenario (design D9, Q3a): a classification whose person_ref_status
// is "ambiguous" persists a unit.StatusPool unit — the same status every
// other Phase B capture produces — never unit.StatusIncomplete, and writes
// TWO decision_log rows, because two decisions happened: the unit was
// created (capture.unit.created, not capture.classify — design.md:934
// reserves that action for this exact scenario), and the reference was left
// unresolved (capture.person_ref.ambiguous, context.kind = "ambiguous_person_ref").
//
// I06 is explicitly out of scope for this test, and this paragraph exists
// so a future reader does not mistake that for an oversight. I06
// ("an incomplete unit has no embedding until promoted") is about
// unit.StatusIncomplete, and this PR's own MUST is that no incomplete unit
// is EVER created (an incomplete unit would be invisible to every live read
// surface per I02, and immortal until M2's expire_incomplete ships — Q3a's
// closed reasoning, spec R4.7). Because this pipeline creates no incomplete
// unit at all, I06 has nothing to exercise here: "no test fails" must not be
// read as "I06 holds" — the same honesty core-coverage.sh gives an "armed
// but vacuous" line instead of a bare OK (design D9's own framing, matching
// the umbrella proposal's "I06 is honestly out of scope rather than
// vacuously green").
func TestCapture_AmbiguousPersonRefPersistsPoolUnitAndLogsTwice(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 8, 1, 9, 30, 0, 0, time.UTC)
	units := memrepo.NewUnits()
	decisions := memrepo.NewDecisionLog()
	embeddings := memrepo.NewEmbeddings()
	lexical := memrepo.NewLexical()
	relations := memrepo.NewRelations()
	llm := fakeprovider.New(t, testdataLLMCasesDir(t), "classify-person-ref-ambiguous-ana")
	embed := fakeprovider.NewEmbeddingFake(embedFakeModel)

	idx, err := embeddings.LoadIndex(ctx, embedFakeModel)
	if err != nil {
		t.Fatalf("embeddings.LoadIndex(%q): %v", embedFakeModel, err)
	}
	svc := brain.NewCaptureService(fixedClock{now: now}, &counterIDs{}, units, embeddings, lexical, relations, decisions, llm, llm, llm, embed, brain.NewIndex(idx), memrepo.NewSignals(), memrepo.NewTriggers(), memrepo.NewTimers(), 0.5)

	result, err := svc.Capture(ctx, brain.CaptureInput{
		Text:    "Ana asked me to send her the contract",
		Channel: "chat",
	})
	if err != nil {
		t.Fatalf("Capture error = %v, want nil — an ambiguous person reference still captures with what it has (doc 02 §5's product rule)", err)
	}
	if result.Outcome != brain.OutcomeStored {
		t.Errorf("CaptureResult.Outcome = %q, want %q — an ambiguous person reference is an ordinary successful capture, not a refusal", result.Outcome, brain.OutcomeStored)
	}
	if result.UnitID == "" {
		t.Fatal("CaptureResult.UnitID is empty — the caller has no way to find the unit this capture persisted")
	}

	u, err := units.ByID(ctx, result.UnitID)
	if err != nil {
		t.Fatalf("units.ByID(%q): %v — the pipeline reported success but persisted nothing", result.UnitID, err)
	}
	if u.Status != unit.StatusPool {
		t.Errorf("Status = %q, want %q — an ambiguous person reference must never produce unit.StatusIncomplete (Q3a, spec R4.7)", u.Status, unit.StatusPool)
	}

	rows, err := decisions.Since(ctx, now.Add(-time.Hour), -1)
	if err != nil {
		t.Fatalf("decisions.Since: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("decision_log has %d rows, want exactly 2 (capture.unit.created + capture.person_ref.ambiguous): %+v", len(rows), rows)
	}

	var createdRow, deferredRow *ports.Decision
	for i := range rows {
		switch rows[i].Action {
		case ports.ActionCaptureUnitCreated:
			createdRow = &rows[i]
		case ports.ActionCapturePersonRefAmbiguous:
			deferredRow = &rows[i]
		}
	}
	if createdRow == nil {
		t.Fatalf("no %q row found among %+v", ports.ActionCaptureUnitCreated, rows)
	}
	if deferredRow == nil {
		t.Fatalf("no %q row found among %+v", ports.ActionCapturePersonRefAmbiguous, rows)
	}

	if createdRow.Rationale == "" {
		t.Error("capture.unit.created Rationale is empty — doc 02 §11 requires a human-readable sentence")
	}
	var createdContext struct {
		UnitID string `json:"unit_id"`
	}
	if err := json.Unmarshal(createdRow.Context, &createdContext); err != nil {
		t.Fatalf("capture.unit.created Context is not valid JSON: %v (%s)", err, createdRow.Context)
	}
	if createdContext.UnitID != result.UnitID {
		t.Errorf("capture.unit.created Context.unit_id = %q, want %q", createdContext.UnitID, result.UnitID)
	}

	if deferredRow.Rationale == "" {
		t.Error("capture.person_ref.ambiguous Rationale is empty — doc 02 §11 requires a human-readable sentence")
	}
	var deferredContext struct {
		Kind   string `json:"kind"`
		UnitID string `json:"unit_id"`
	}
	if err := json.Unmarshal(deferredRow.Context, &deferredContext); err != nil {
		t.Fatalf("capture.person_ref.ambiguous Context is not valid JSON: %v (%s)", err, deferredRow.Context)
	}
	if deferredContext.Kind != "ambiguous_person_ref" {
		t.Errorf("capture.person_ref.ambiguous Context.kind = %q, want %q", deferredContext.Kind, "ambiguous_person_ref")
	}
	if deferredContext.UnitID != result.UnitID {
		t.Errorf("capture.person_ref.ambiguous Context.unit_id = %q, want %q", deferredContext.UnitID, result.UnitID)
	}
}
