package main

import (
	"context"
	"testing"

	"github.com/rengo/nooma/internal/store/sqlite"
)

// TestWireUnits_BuildsAWorkingService is wireUnits's only caller until PR 3
// widens serve.go's uiDeps call with it — wireToday's own precedent
// (wiring_today_test.go): browsing needs no provider, so an empty,
// migrated vault still answers Browse with a real, if empty, page.
func TestWireUnits_BuildsAWorkingService(t *testing.T) {
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

	page, err := wireUnits(db).Browse(ctx, nil, nil)
	if err != nil {
		t.Fatalf("Browse: %v", err)
	}
	if len(page.Units) != 0 {
		t.Fatalf("len(Units) = %d, want 0 — an empty vault has no live units", len(page.Units))
	}
	if page.Next != nil {
		t.Fatalf("Next = %v, want nil — an empty page has no next cursor", page.Next)
	}
}
