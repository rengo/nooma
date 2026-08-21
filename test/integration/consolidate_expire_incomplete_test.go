//go:build integration

package integration

import (
	"context"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/brain"
	"github.com/rengo/nooma/internal/core/consolidation"
	"github.com/rengo/nooma/internal/ports"
	"github.com/rengo/nooma/internal/store/sqlite"
	"github.com/rengo/nooma/test/support/fakeprovider"
)

// repoRootForConsolidateIT resolves the repository root from this file's
// own path — test/conformance/store_api_test.go's repoRootFromCaller
// precedent, reimplemented locally since that helper lives in a different
// package (test/conformance) this file cannot import.
func repoRootForConsolidateIT(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller(0) failed to report this test file's path")
	}
	// thisFile is .../test/integration/consolidate_expire_incomplete_test.go
	return filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
}

// counterIDs is a deterministic ports.IDGen — consolidate_test.go's fakeIDs
// precedent, reimplemented locally for the same reason as
// repoRootForConsolidateIT above.
type counterIDs struct{ n int }

func (g *counterIDs) New() string {
	g.n++
	return "id-" + string(rune('0'+g.n))
}

// fixedClock is a deterministic ports.Clock for this file only.
type fixedClock struct{ now time.Time }

func (c fixedClock) Now() time.Time { return c.now }

const consolidateITEmbedModel = "integration-embed-fake-v1"

// TestExpireIncomplete_RealCapturePathProducesNoIncompleteUnits is spec
// R5.1's own second MUST, proven rather than merely asserted (owner ruling
// Q3's closed question): a vault seeded through the REAL capture path —
// brain.CaptureService, wired over the real sqlite store, not a
// repo-constructed unit.StatusIncomplete row — produces zero incomplete
// units, so expire_incomplete's whole-pass run over it is a genuine no-op:
// IncompleteOlderThan's read returns empty and the phase persists nothing.
//
// "classify-pick-up-dry-cleaning" (testdata/llm/cases) is the identical
// fixture test/conformance/capture_pipeline_test.go's
// TestCapture_OrdinaryClassificationPersistsAUnit already proves produces a
// live (unit.StatusPool) unit on a fresh, candidate-free vault — reused
// here rather than duplicated, over the real store instead of memrepo.
func TestExpireIncomplete_RealCapturePathProducesNoIncompleteUnits(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 8, 1, 9, 30, 0, 0, time.UTC)

	dbPath := filepath.Join(t.TempDir(), "vault.db")
	v, err := sqlite.Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("sqlite.Open(%q): %v", dbPath, err)
	}
	t.Cleanup(func() { _ = v.Close() })

	units := sqlite.NewUnitRepo(v)
	embeddings := sqlite.NewEmbeddingRepo(v)
	lexical := sqlite.NewSearch(v)
	relations := sqlite.NewRelationRepo(v)
	decisions := sqlite.NewDecisionLog(v)
	signals := sqlite.NewSignalRepo(v)
	cfg := sqlite.NewConfigRepo(v)
	triggers := sqlite.NewTriggerRepo(v)
	timers := sqlite.NewTimerRepo(v)

	idx, err := embeddings.LoadIndex(ctx, consolidateITEmbedModel)
	if err != nil {
		t.Fatalf("embeddings.LoadIndex(%q): %v", consolidateITEmbedModel, err)
	}

	llm := fakeprovider.New(t, filepath.Join(repoRootForConsolidateIT(t), "testdata", "llm", "cases"), "classify-pick-up-dry-cleaning")
	embed := fakeprovider.NewEmbeddingFake(consolidateITEmbedModel)

	captureSvc := brain.NewCaptureService(fixedClock{now: now}, &counterIDs{}, units, embeddings, lexical, relations, decisions, llm, llm, embed, brain.NewIndex(idx), signals, triggers, timers)

	result, err := captureSvc.Capture(ctx, brain.CaptureInput{
		Text:    "Pick up the dry cleaning on Friday",
		Channel: "chat",
	})
	if err != nil {
		t.Fatalf("Capture: %v", err)
	}
	if result.UnitID == "" {
		t.Fatal("CaptureResult.UnitID is empty — the real capture path did not persist a unit, invalidating this fixture's premise")
	}

	// The read itself, proven directly first: no unit.StatusIncomplete row
	// exists on this vault, at any cutoff.
	cutoff := now.Add(-consolidation.IncompleteExpiryHours * time.Hour)
	incomplete, err := units.IncompleteOlderThan(ctx, cutoff)
	if err != nil {
		t.Fatalf("IncompleteOlderThan: %v", err)
	}
	if len(incomplete) != 0 {
		t.Fatalf("IncompleteOlderThan = %v, want empty — the real capture path never produces an incomplete unit (owner ruling Q3)", incomplete)
	}

	// The wired phase, proven end-to-end through the real runner: a
	// per-phase expire_incomplete run over this same vault, later than
	// IncompleteExpiryHours past the capture, completes with no error and
	// writes no decision_log row — the phase's persist step is a genuine
	// no-op, not merely "the read happened to be empty".
	later := now.Add((consolidation.IncompleteExpiryHours + 1) * time.Hour)
	recallSvc := brain.NewRecallService(brain.NewIndex(idx), lexical, units, embed)
	// This fixture's own PhaseExpireIncomplete run never reaches PR 9b's
	// judge call, so a zero-case fake is what NewConsolidateService's own
	// widened judge parameter needs here — consolidate_test.go's noJudge
	// precedent, reimplemented locally for the same reason
	// repoRootForConsolidateIT above is: that helper lives in package brain,
	// which this package cannot import.
	consolidateSvc := brain.NewConsolidateService(fixedClock{now: later}, cfg, units, relations, &counterIDs{}, decisions, recallSvc, fakeprovider.New(t, ""), sqlite.NewSelfModelRepo(v), sqlite.NewStateRepo(v))
	phase := consolidation.PhaseExpireIncomplete
	if _, err := consolidateSvc.Consolidate(ctx, brain.ConsolidateRequest{Phase: &phase}); err != nil {
		t.Fatalf("Consolidate(PhaseExpireIncomplete): %v", err)
	}

	rows, err := decisions.Since(ctx, now.Add(-time.Hour), 100)
	if err != nil {
		t.Fatalf("decisions.Since: %v", err)
	}
	for _, row := range rows {
		if row.Action == ports.ActionExpireIncompleteTransitioned {
			t.Errorf("decision_log gained an expire_incomplete row %+v, want none — the phase's persist step must be a no-op on this vault", row)
		}
	}
}
