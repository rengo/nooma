package brain

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/ports"
	"github.com/rengo/nooma/test/support/fakeprovider"
	"github.com/rengo/nooma/test/support/memrepo"
)

// An armed trigger's decision row names the unit it hangs off, so the
// activity page can say what a "Reminder set" row is about and link to it.
func TestCapture_ArmedTriggerRowNamesItsUnit(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	embeddings := memrepo.NewEmbeddings()
	idx, err := embeddings.LoadIndex(ctx, "fake-embed")
	if err != nil {
		t.Fatal(err)
	}
	llm := fakeprovider.New(t, testdataLLMCasesDir(t), "classify-event-dentist-next-friday")
	log := memrepo.NewDecisionLog()
	svc := NewCaptureService(fixedClock{now: now}, &seqIDs{}, memrepo.NewUnits(), embeddings,
		memrepo.NewLexical(), memrepo.NewRelations(), log, llm, llm, llm, fakeprovider.NewEmbeddingFake("fake-embed"),
		NewIndex(idx), memrepo.NewSignals(), memrepo.NewTriggers(), memrepo.NewTimers(), 0.5, nil)

	res, err := svc.Capture(ctx, CaptureInput{Text: "replayed by case id", Channel: "chat"})
	if err != nil {
		t.Fatalf("Capture: %v", err)
	}
	rows, err := log.Before(ctx, nil, string(ports.ActionCaptureArmedTrigger), 10)
	if err != nil || len(rows) != 2 {
		t.Fatalf("armed rows = %d (%v), want 2, one per reminder", len(rows), err)
	}
	var got struct {
		UnitID string `json:"unit_id"`
	}
	if err := json.Unmarshal(rows[0].Context, &got); err != nil {
		t.Fatal(err)
	}
	if res.UnitID == "" || got.UnitID != res.UnitID {
		t.Errorf("armed row unit_id = %q, want the stored unit %q", got.UnitID, res.UnitID)
	}
}
