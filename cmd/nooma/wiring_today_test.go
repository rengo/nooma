package main

import (
	"context"
	"testing"

	"github.com/rengo/nooma/internal/store/sqlite"
)

// TestWireToday_BuildsAWorkingService is wireToday's only caller until
// PR 7 widens cmd/nooma/serve.go's own ui.New(...) call with it (task
// 7.7, design §3.6). With no production call site yet in this PR — the
// GET /ui route stays PR 2's shell through PR 6 (design §7.2) — an
// unexported wireToday would otherwise fail golangci-lint's unused check;
// this exercises the real wiring over a real, empty, migrated vault
// instead of leaving the result of a call discarded.
func TestWireToday_BuildsAWorkingService(t *testing.T) {
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

	today, err := wireToday(db).Today(ctx)
	if err != nil {
		t.Fatalf("Today: %v", err)
	}
	if len(today.Focuses) != 2 {
		t.Fatalf("len(Focuses) = %d, want 2 (task, then load) — an empty vault still renders Today", len(today.Focuses))
	}
}
