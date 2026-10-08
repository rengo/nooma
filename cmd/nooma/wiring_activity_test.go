package main

import (
	"context"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/ports"
	"github.com/rengo/nooma/internal/store/sqlite"
)

// TestWireActivity_BuildsAWorkingService is wireActivity's proof over a real,
// empty, migrated vault (wireUnits' precedent, wiring_units_test.go): the
// service is non-nil and reads the real decision_log, so a row recorded
// through the store comes back through Page. No provider is needed.
func TestWireActivity_BuildsAWorkingService(t *testing.T) {
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

	svc := wireActivity(db)
	if svc == nil {
		t.Fatal("wireActivity returned nil — a nil *ActivityService behind ui.Deps would answer every request with a panic")
	}

	page, err := svc.Page(ctx, "", nil)
	if err != nil {
		t.Fatalf("Page on an empty vault: %v", err)
	}
	if len(page.Rows) != 0 || page.Next != nil {
		t.Fatalf("empty vault page = %+v, want no rows and no next cursor", page)
	}

	d := ports.Decision{ID: "d-1", Action: ports.ActionCaptureUnitCreated, Rationale: "stored", OccurredAt: time.Date(2026, 9, 1, 9, 0, 0, 0, time.UTC)}
	if err := sqlite.NewDecisionLog(db).Record(ctx, d); err != nil {
		t.Fatalf("Record: %v", err)
	}
	page, err = svc.Page(ctx, "capture", nil)
	if err != nil {
		t.Fatalf("Page: %v", err)
	}
	if len(page.Rows) != 1 || page.Rows[0].ID != "d-1" {
		t.Fatalf("Page = %+v, want the recorded row d-1: wireActivity is not over the real decision_log", page.Rows)
	}
}
