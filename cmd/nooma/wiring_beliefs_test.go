package main

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/core/selfmodel"
	"github.com/rengo/nooma/internal/ports"
	"github.com/rengo/nooma/internal/store/sqlite"
)

// TestWireBeliefs_BuildsAWorkingService is wireBeliefs' proof over a real,
// empty, migrated vault (wireUnits' precedent, wiring_units_test.go): the
// service is non-nil, reads all five facets, and each of its three ports is
// the real one, because an edit through it moves the belief row and leaves
// one decision_log row and one learning signal behind. A provider-free
// vault is enough: no belief operation calls a model.
func TestWireBeliefs_BuildsAWorkingService(t *testing.T) {
	vault := writeVault(t, "")
	cfg, err := loadVaultConfig(vault)
	if err != nil {
		t.Fatalf("loadVaultConfig: %v", err)
	}
	dbPath, err := cfg.DatabasePath(vault)
	if err != nil {
		t.Fatalf("DatabasePath: %v", err)
	}

	ctx := context.Background()
	db, err := sqlite.Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("sqlite.Open: %v", err)
	}
	defer func() { _ = db.Close() }()

	svc := wireBeliefs(db)
	if svc == nil {
		t.Fatal("wireBeliefs returned nil — a nil *BeliefsService behind ui.Deps would answer every request with a panic")
	}

	groups, err := svc.ByFacet(ctx)
	if err != nil {
		t.Fatalf("ByFacet on an empty vault: %v", err)
	}
	if len(groups) != len(selfmodel.AllFacets()) {
		t.Fatalf("ByFacet returned %d groups, want one per facet (%d) — an empty facet is present, not absent", len(groups), len(selfmodel.AllFacets()))
	}
	for _, g := range groups {
		if len(g.Beliefs) != 0 {
			t.Errorf("facet %s lists %d belief(s) on an empty vault, want 0", g.Facet, len(g.Beliefs))
		}
	}

	if err := svc.Edit(ctx, "no-such-belief", "x"); !errors.Is(err, ports.ErrBeliefNotFound) {
		t.Fatalf("Edit of an unknown id error = %v, want ErrBeliefNotFound", err)
	}

	seeded := time.Date(2026, 9, 1, 9, 0, 0, 0, time.UTC)
	beliefs := sqlite.NewSelfModelRepo(db)
	if err := beliefs.UpsertByTopicKey(ctx, ports.Belief{
		ID: "b-1", Facet: selfmodel.FacetGoal, TopicKey: "goal/run", Content: "run a marathon",
		Confidence: 0.7, Origin: selfmodel.OriginDerived, Status: selfmodel.StatusActive,
		LastReinforcedAt: seeded, CreatedAt: seeded, UpdatedAt: seeded,
	}); err != nil {
		t.Fatalf("seed belief: %v", err)
	}

	if err := svc.Edit(ctx, "b-1", "run a half marathon"); err != nil {
		t.Fatalf("Edit: %v", err)
	}
	got, err := beliefs.BeliefByID(ctx, "b-1")
	if err != nil {
		t.Fatalf("BeliefByID: %v", err)
	}
	if got.Content != "run a half marathon" || got.Origin != selfmodel.OriginUserStated {
		t.Errorf("belief after Edit = %q / %s, want the new content and user_stated", got.Content, got.Origin)
	}
	rows, err := sqlite.NewDecisionLog(db).Since(ctx, time.Time{}, 10)
	if err != nil {
		t.Fatalf("decision_log Since: %v", err)
	}
	if len(rows) != 1 || rows[0].Action != ports.ActionBeliefEdited {
		t.Errorf("decision_log rows = %+v, want exactly one %s", rows, ports.ActionBeliefEdited)
	}
	signals, err := sqlite.NewSignalRepo(db).Since(ctx, time.Time{}, 10)
	if err != nil {
		t.Fatalf("signals Since: %v", err)
	}
	if len(signals) != 1 || signals[0].Type != "belief_edit" {
		t.Errorf("signals = %+v, want exactly one belief_edit", signals)
	}
}
