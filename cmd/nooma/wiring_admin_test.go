package main

import (
	"context"
	"testing"

	"github.com/rengo/nooma/internal/store/sqlite"
)

// TestWireAdmin_ViewsAFreshVaultAndSavesIntoIt: the service serve.go hands
// /ui/admin reads a fresh vault as the defaults in force, never consolidated
// and with nothing learned, and an update lands in config and the log through
// the real repositories.
func TestWireAdmin_ViewsAFreshVaultAndSavesIntoIt(t *testing.T) {
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

	svc := wireAdmin(db)
	v, err := svc.View(ctx)
	if err != nil {
		t.Fatalf("View: %v", err)
	}
	if v.LastRun != nil || !v.Settings.ConsolidationEnabled || len(v.Learned) != 0 || len(v.RecentEffects) != 0 || v.Settings.GoalStagnationDays != 21 {
		t.Fatalf("View = %+v, want never run, enabled, nothing learned, no effects, the defaults", v)
	}

	res, err := svc.Update(ctx, "goal_stagnation_days", "28")
	if err != nil || !res.Changed {
		t.Fatalf("Update = %+v, %v; want a change", res, err)
	}
	if v, err = svc.View(ctx); err != nil || v.Settings.GoalStagnationDays != 28 {
		t.Fatalf("View after Update = %+v, %v; want 28 days", v.Settings, err)
	}
	logged, err := sqlite.NewDecisionLog(db).Before(ctx, nil, "config.", 10)
	if err != nil || len(logged) != 1 {
		t.Fatalf("config.updated rows = %d, %v; want exactly 1", len(logged), err)
	}
}
