//go:build integration

package integration

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	_ "github.com/ncruces/go-sqlite3/driver"

	"github.com/rengo/nooma/internal/brain"
	"github.com/rengo/nooma/internal/core/consolidation"
	"github.com/rengo/nooma/internal/core/unit"
	"github.com/rengo/nooma/internal/ports"
	"github.com/rengo/nooma/internal/store/sqlite"
	"github.com/rengo/nooma/test/support/fakeprovider"
)

// TestPatternEval_LoadFiringWritesCurrentStateRowAgainstRealSQLite is spec
// R5.10's second MUST, proven at L3 (design §3.2 Q6, §6.3 slot 7): a real
// vault with consolidation.DefaultMentalLoadThreshold live mental_load
// units, run through the wired PhasePatternEval arm against a real sqlite
// store, appends exactly one current_state row shaped
// (source='consolidation', mood='loaded', energy IS NULL, active=1) —
// migration 0003's own column (PR 4) and the reason
// ports.StateSourceConsolidation/MoodLoaded exist — plus one decision_log
// row naming the lastHypothesisAt mapping. counterIDs, fixedClock and
// repoRootForConsolidateIT are consolidate_expire_incomplete_test.go's own
// package-local fixtures, reused rather than duplicated.
//
// Red: PhasePatternEval's load half does not exist yet — no current_state
// row is ever appended and no ActionPatternEvalLoadHypothesisOpened row is
// ever logged, at L2 (consolidate_test.go) or here at L3.
func TestPatternEval_LoadFiringWritesCurrentStateRowAgainstRealSQLite(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 8, 10, 3, 0, 0, 0, time.UTC)

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
	cfg := sqlite.NewConfigRepo(v)
	selfModel := sqlite.NewSelfModelRepo(v)
	state := sqlite.NewStateRepo(v)

	idx, err := embeddings.LoadIndex(ctx, consolidateITEmbedModel)
	if err != nil {
		t.Fatalf("embeddings.LoadIndex(%q): %v", consolidateITEmbedModel, err)
	}
	embed := fakeprovider.NewEmbeddingFake(consolidateITEmbedModel)
	recallSvc := brain.NewRecallService(brain.NewIndex(idx), lexical, units, embed)

	ids := &counterIDs{}
	for i := 0; i < consolidation.DefaultMentalLoadThreshold; i++ {
		id := ids.New()
		if err := units.Create(ctx, unit.Unit{
			ID: id, Type: unit.TypeMentalLoad, Status: unit.StatusPool,
			Content: "open loop", Source: "chat",
			LastTouchedAt: now, CreatedAt: now, UpdatedAt: now,
		}); err != nil {
			t.Fatalf("seed mental_load unit %d: %v", i, err)
		}
	}

	consolidateSvc := brain.NewConsolidateService(fixedClock{now: now}, cfg, units, relations, ids, decisions, recallSvc, fakeprovider.New(t, ""), selfModel, state)
	phase := consolidation.PhasePatternEval
	if _, err := consolidateSvc.Consolidate(ctx, brain.ConsolidateRequest{Phase: &phase}); err != nil {
		t.Fatalf("Consolidate(PhasePatternEval): %v", err)
	}

	lastAt, err := state.LastHypothesisAt(ctx)
	if err != nil {
		t.Fatalf("LastHypothesisAt: %v", err)
	}
	if lastAt == nil || !lastAt.Equal(now) {
		t.Fatalf("LastHypothesisAt = %v, want %v", lastAt, now)
	}

	raw, err := sql.Open("sqlite3", "file:"+dbPath)
	if err != nil {
		t.Fatalf("sql.Open(%q): %v", dbPath, err)
	}
	t.Cleanup(func() { _ = raw.Close() })

	var count int
	if err := raw.QueryRowContext(ctx, "SELECT count(*) FROM current_state WHERE source = 'consolidation'").Scan(&count); err != nil {
		t.Fatalf("count consolidation-sourced current_state rows: %v", err)
	}
	if count != 1 {
		t.Fatalf("consolidation-sourced current_state rows = %d, want exactly 1", count)
	}

	var mood string
	var energy sql.NullFloat64
	var active int
	if err := raw.QueryRowContext(ctx, "SELECT mood, energy, active FROM current_state WHERE source = 'consolidation'").Scan(&mood, &energy, &active); err != nil {
		t.Fatalf("read the consolidation-sourced row: %v", err)
	}
	if mood != ports.MoodLoaded {
		t.Errorf("mood = %q, want %q", mood, ports.MoodLoaded)
	}
	if energy.Valid {
		t.Errorf("energy = %v, want NULL — the watcher never observed a value (design §4.4)", energy.Float64)
	}
	if active != 1 {
		t.Errorf("active = %d, want 1", active)
	}

	rows, err := decisions.Since(ctx, now.Add(-time.Hour), 10)
	if err != nil {
		t.Fatalf("decisions.Since: %v", err)
	}
	found := false
	for _, row := range rows {
		if row.Action == ports.ActionPatternEvalLoadHypothesisOpened {
			found = true
		}
	}
	if !found {
		t.Fatalf("decision_log rows = %+v, want one %s row", rows, ports.ActionPatternEvalLoadHypothesisOpened)
	}
}
