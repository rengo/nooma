//go:build integration

package integration

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

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
	defer v.Close() //nolint:errcheck // best-effort cleanup, the assertions below already ran

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
	defer db.Close() //nolint:errcheck // best-effort cleanup, the assertions below already ran

	_, err = db.ExecContext(context.Background(),
		`CREATE VIRTUAL TABLE temp.nooma_fts5_probe USING fts5(c)`)
	if err == nil {
		t.Fatal("CREATE VIRTUAL TABLE ... USING fts5 succeeded on an unregistered connection, want \"no such module: fts5\"")
	}
}

// TestFTS5AvailableAcrossPoolConnections is belt-and-braces evidence for D2:
// fts5 registration is not a first-connection-only guarantee, and the pool
// really does open more than one connection under concurrent use.
//
// This is made deterministic rather than a race against scheduling. Firing
// `goroutines` calls to v.Check in a loop with no synchronization (the
// original shape of this test) only forces database/sql to open a second
// connection if two requests happen to be in flight at the same instant —
// on a throttled CI runner, each single fast round trip can easily finish
// before the next goroutine even asks the pool for a connection, so the
// pool never needs more than one and the assertion below fails for a
// reason that has nothing to do with D2. Two changes close that gap:
//
//  1. A barrier (ready/start) holds every goroutine at the starting line
//     so all of them request a connection at (as close to) the same
//     instant as the scheduler allows, instead of trickling in one at a
//     time; each then hammers v.Check in a loop instead of calling it
//     once, widening the window in which they can all be observed
//     in flight together.
//  2. Stats().OpenConnections is polled for its peak *during* that burst,
//     not read once after wg.Wait(): connections handed back to the pool
//     stay open until Go's database/sql default idle-connection ceiling
//     (2) evicts the excess, so reading Stats after every goroutine has
//     already finished races against how much of that eviction already
//     happened by the time this goroutine gets scheduled again.
func TestFTS5AvailableAcrossPoolConnections(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "vault.db")

	v, err := sqlite.Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("sqlite.Open(%q) = _, %v, want nil error", dbPath, err)
	}
	defer v.Close() //nolint:errcheck // best-effort cleanup, the assertions below already ran

	const goroutines = 8
	const burst = 300 * time.Millisecond

	start := make(chan struct{})
	stop := make(chan struct{})
	var ready, wg sync.WaitGroup
	ready.Add(goroutines)
	wg.Add(goroutines)
	errs := make([]error, goroutines)
	for i := range goroutines {
		go func(i int) {
			defer wg.Done()
			ready.Done()
			<-start
			for {
				select {
				case <-stop:
					return
				default:
				}
				if err := v.Check(ctx); err != nil {
					errs[i] = err
					return
				}
			}
		}(i)
	}
	ready.Wait()
	close(start)

	var peak int
	deadline := time.Now().Add(burst)
	for time.Now().Before(deadline) {
		if got := v.Stats().OpenConnections; got > peak {
			peak = got
		}
	}
	close(stop)
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("goroutine %d: v.Check() = %v, want nil", i, err)
		}
	}

	if peak <= 1 {
		t.Errorf("peak v.Stats().OpenConnections during the burst = %d, want more than 1 (belt-and-braces pooling evidence for D2, not the guarantee itself)", peak)
	}
}
