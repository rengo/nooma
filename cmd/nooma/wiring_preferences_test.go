package main

import (
	"context"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/core/unit"
	"github.com/rengo/nooma/internal/ports"
	"github.com/rengo/nooma/internal/store/sqlite"
)

// TestWirePreferences_SavesAndReArmsOverARealVault: the service serve.go
// hands /ui/preferences reads the defaults on a fresh vault, and a save
// lands in config and moves an armed reminder of an event ahead through the
// real repositories (ADR-0029 point 6).
func TestWirePreferences_SavesAndReArmsOverARealVault(t *testing.T) {
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

	svc := wirePreferences(db)
	got, err := svc.Preferences(ctx)
	if err != nil || len(got.LeadMinutes) != 2 || got.QuietEnd != "07:00" {
		t.Fatalf("Preferences() = %+v, %v; want the defaults", got, err)
	}

	// An event three days ahead at 15:00 local, clear of quiet hours at
	// every lead used here, with its 2-hour reminder armed.
	now := time.Now()
	y, m, d := now.AddDate(0, 0, 3).Date()
	eventAt := time.Date(y, m, d, 15, 0, 0, 0, now.Location())
	if err := sqlite.NewUnitRepo(db).Create(ctx, unit.Unit{ID: "flight", Type: unit.TypeEvent, Status: unit.StatusPool, Content: "flight",
		Weight: 0.5, WeightDecayRate: 0.03, EventAt: &eventAt, Source: "cli", CreatedAt: now, UpdatedAt: now, LastTouchedAt: now}); err != nil {
		t.Fatalf("creating unit: %v", err)
	}
	unitID, fireAt := "flight", eventAt.Add(-2*time.Hour)
	triggers := sqlite.NewTriggerRepo(db)
	if err := triggers.Create(ctx, ports.Trigger{ID: "flight-2h", UnitID: &unitID, Kind: ports.TriggerKindTimeBased,
		Payload: ports.TriggerPayload{ActionText: "flight", LeadMinutes: 120}, FireAt: &fireAt, CreatedAt: now}); err != nil {
		t.Fatalf("creating trigger: %v", err)
	}

	got.LeadMinutes = []int{180}
	if _, err := svc.Save(ctx, got); err != nil {
		t.Fatalf("Save: %v", err)
	}
	stored, err := sqlite.NewConfigRepo(db).Load(ctx)
	if err != nil || stored.EventReminderLeads == nil || *stored.EventReminderLeads != "[180]" {
		t.Fatalf("stored leads = %v, %v; want [180]", stored.EventReminderLeads, err)
	}
	armed, err := triggers.ArmedForUnit(ctx, "flight")
	if err != nil || len(armed) != 1 || !armed[0].FireAt.Equal(eventAt.Add(-3*time.Hour)) {
		t.Fatalf("armed = %+v, %v; want the reminder moved to 3 hours before", armed, err)
	}
}
