package main

import (
	"context"
	"errors"
	"testing"

	"github.com/rengo/nooma/internal/brain"
	"github.com/rengo/nooma/internal/ports"
	"github.com/rengo/nooma/internal/store/sqlite"
)

// TestWireLearning_ViewsAFreshVault: the service serve.go hands /ui/learning
// and GET /learning reads a fresh vault as nothing learned, the goal cadence
// at its default, and refuses a release of a value nobody pinned.
func TestWireLearning_ViewsAFreshVault(t *testing.T) {
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

	svc := wireLearning(db)
	v, err := svc.View(ctx)
	if err != nil {
		t.Fatalf("View: %v", err)
	}
	if len(v.Values) != 1 || v.Values[0].Value != "21 days" || v.Values[0].At != nil || v.Values[0].Pinned {
		t.Fatalf("View = %+v, want the default goal cadence alone", v)
	}
	if err := svc.Release(ctx, brain.LearnedGoalCadence, brain.GoalCadenceKey); !errors.Is(err, ports.ErrNotPinned) {
		t.Errorf("Release = %v, want ErrNotPinned", err)
	}
}
