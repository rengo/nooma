//go:build integration

package integration

import (
	"context"
	"path/filepath"
	"sync"
	"testing"

	"github.com/ncruces/go-sqlite3/driver"

	"github.com/rengo/nooma/internal/store/sqlite"
)

// TestFTS5RegisteredOnEveryConnection proves the opener's guarantee (design
// D2/§4.3): a connection produced by sqlite.Open can run the fts5 module
// probe without "no such module: fts5".
func TestFTS5RegisteredOnEveryConnection(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "vault.db")

	v, err := sqlite.Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("sqlite.Open(%q) = _, %v, want nil error", dbPath, err)
	}
	defer v.Close()

	if err := v.Check(ctx); err != nil {
		t.Errorf("v.Check() = %v, want nil (fts5 must be registered on this connection)", err)
	}
}

// TestFTS5MissingWithoutRegistration is the control for the test above: a
// bare driver.Open with no init callback on the same file must fail the same
// probe with "no such module: fts5". If it did not, the test it controls
// would prove nothing (design §4.3).
func TestFTS5MissingWithoutRegistration(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "control.db")

	db, err := driver.Open("file:" + dbPath)
	if err != nil {
		t.Fatalf("driver.Open(%q) = _, %v, want nil error", dbPath, err)
	}
	defer db.Close()

	_, err = db.ExecContext(context.Background(),
		`CREATE VIRTUAL TABLE temp.nooma_fts5_probe USING fts5(c)`)
	if err == nil {
		t.Fatal("CREATE VIRTUAL TABLE ... USING fts5 succeeded on an unregistered connection, want \"no such module: fts5\"")
	}
}

// TestFTS5AvailableAcrossPoolConnections is belt-and-braces evidence for D2:
// fts5 registration is not a first-connection-only guarantee, and the pool
// really does open more than one connection under concurrent use.
func TestFTS5AvailableAcrossPoolConnections(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "vault.db")

	v, err := sqlite.Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("sqlite.Open(%q) = _, %v, want nil error", dbPath, err)
	}
	defer v.Close()

	const goroutines = 8
	var wg sync.WaitGroup
	errs := make([]error, goroutines)
	for i := range goroutines {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errs[i] = v.Check(ctx)
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("goroutine %d: v.Check() = %v, want nil", i, err)
		}
	}

	if got := v.Stats().OpenConnections; got <= 1 {
		t.Errorf("v.Stats().OpenConnections = %d, want more than 1 (belt-and-braces pooling evidence for D2, not the guarantee itself)", got)
	}
}
