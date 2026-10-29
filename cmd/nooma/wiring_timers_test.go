package main

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/brain"
	"github.com/rengo/nooma/internal/ports"
	"github.com/rengo/nooma/internal/store/sqlite"
)

// TestWireTimers_ListsAndCancelsOverARealVault: the service serve.go hands
// /ui/timers reads the vault's pending timers and cancels one through the
// SQLite store, the cancel recorded, a second cancel no longer set.
func TestWireTimers_ListsAndCancelsOverARealVault(t *testing.T) {
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
	t.Cleanup(func() { _ = db.Close() })

	text := "Sacar la pizza"
	at := time.Now().Add(time.Hour).UTC().Truncate(time.Second)
	if err := sqlite.NewTimerRepo(db).Create(ctx, ports.Timer{ID: "tmr-1", FireAt: at, ActionText: &text, CreatedAt: at.Add(-2 * time.Hour)}); err != nil {
		t.Fatal(err)
	}

	svc := wireTimers(db)
	live, err := svc.Live(ctx)
	if err != nil || len(live) != 1 || live[0].ID != "tmr-1" {
		t.Fatalf("Live = %+v, %v; want tmr-1", live, err)
	}
	if _, err := svc.Cancel(ctx, "tmr-1"); err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	if _, err := svc.Cancel(ctx, "tmr-1"); !errors.Is(err, brain.ErrTimerNoLongerSet) {
		t.Errorf("second Cancel = %v, want ErrTimerNoLongerSet", err)
	}
	rows, err := sqlite.NewDecisionLog(db).Since(ctx, time.Time{}, 10)
	if err != nil || len(rows) != 1 || rows[0].Action != ports.ActionCaptureTimerCancelled {
		t.Errorf("decision rows = %+v, %v; want one capture.timer.cancelled", rows, err)
	}
}
