//go:build integration

package integration

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rengo/nooma/internal/store/sqlite"
)

// TestOpenCorruptVaultNamesThePath asserts Open wraps the driver's raw
// error with the vault path and what was being attempted, instead of
// returning it bare. Opening a file of garbage bytes trips SQLite's own
// PRAGMA execution — the DSN's PRAGMAs are the first statements run
// against a fresh handle (design D3) — and fails with a message like
// "sqlite3: invalid _pragma: sqlite3: file is not a database", which on
// its own names neither the vault nor what was being attempted. This is
// the project's own stated concern ("fails late and far from the cause")
// recurring through a different door.
func TestOpenCorruptVaultNamesThePath(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "corrupt.db")
	if err := os.WriteFile(dbPath, []byte("not a sqlite database, just garbage bytes"), 0o600); err != nil {
		t.Fatalf("os.WriteFile(%q) = %v, want nil error", dbPath, err)
	}

	_, err := sqlite.Open(context.Background(), dbPath)
	if err == nil {
		t.Fatal("sqlite.Open(...) on a corrupt vault file = nil error, want non-nil")
	}
	if !strings.Contains(err.Error(), dbPath) {
		t.Errorf("sqlite.Open(...) error = %q, want it to mention the vault path %q", err.Error(), dbPath)
	}
}
