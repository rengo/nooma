//go:build integration

package sqlite

import (
	"context"
	"net/url"
	"path/filepath"
	"testing"
	"time"

	"github.com/ncruces/go-sqlite3/driver"
)

// TestOpenTxlockIsImmediate is an L3 test (nooma-testing: "transactions" ->
// test/integration, tag integration) that proves, through observed SQLite
// lock behavior rather than through the DSN string, that the DSN buildDSN
// produces really does start transactions with BEGIN IMMEDIATE (design D3
// item 4, spec R2.1). A string-shaped test like TestBuildDSN (dsn_test.go,
// L1) can only prove buildDSN emits the right characters; it cannot prove
// those characters mean what they are supposed to mean to the driver. This
// test opens a real connection with the real DSN and watches SQLite's lock
// manager instead.
//
// Location, recorded rather than silent: this file lives inside package
// sqlite (not test/integration/) specifically so it can call the
// unexported buildDSN directly, and it is wired into `make
// test-integration` through the Makefile. Vault's exported surface is
// intentionally frozen with no way to start a transaction (design D7,
// "Vault exposes no query surface"; TestOpenAppliesPragmas in
// test/integration/pragma_test.go records the equivalent residual risk
// for the sibling per-connection settings busy_timeout and foreign_keys,
// and accepts it instead of closing it). A test living in
// test/integration/ cannot reach buildDSN, so the best it could do is
// reconstruct the DSN by hand — which would pass unconditionally
// regardless of what buildDSN actually returns, and could never have
// failed against the historical bug this test guards (_txlock folded
// into a _pragma value instead of sent as its own parameter). That is not
// a regression test. Calling buildDSN from inside its own package is the
// smallest change that keeps this one.
//
// How this distinguishes immediate from deferred: BEGIN IMMEDIATE
// acquires SQLite's RESERVED lock synchronously, at BEGIN time, before
// any statement runs inside the transaction; BEGIN DEFERRED (what an
// empty or absent "_txlock" falls back to) acquires no lock at all until
// the first read or write statement runs. Connection A opens a
// transaction built from the real production DSN and runs nothing past
// BEGIN. Connection B, independently opened on the same file with a real
// top-level "_txlock=immediate" and a short busy_timeout, then calls
// BeginTx too. If A's BEGIN really took the write lock upfront, B's own
// BEGIN IMMEDIATE contends for it and fails with SQLITE_BUSY once its
// busy_timeout elapses. If A's BEGIN took no lock at all, there is
// nothing for B to contend with and it returns immediately. B's own
// busy_timeout is set far below the production 5s (design D3 item 1) so
// a regression that makes this genuinely deferred fails fast rather than
// making the suite sit near the production timeout.
func TestOpenTxlockIsImmediate(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "vault.db")
	ctx := context.Background()

	dsn, err := buildDSN(dbPath, pathStyleForGOOS())
	if err != nil {
		t.Fatalf("buildDSN(%q) = _, %v, want nil error", dbPath, err)
	}
	dbA, err := driver.Open(dsn)
	if err != nil {
		t.Fatalf("driver.Open(%q) = _, %v, want nil error", dsn, err)
	}
	defer dbA.Close() //nolint:errcheck // best-effort cleanup, the assertions below already ran

	txA, err := dbA.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("connA.BeginTx() = _, %v, want nil error (uncontended, must always succeed)", err)
	}
	defer txA.Rollback() //nolint:errcheck // best-effort cleanup, the assertions below already ran

	// Connection B is deliberately independent of buildDSN: it plays a
	// second nooma writer racing connection A, and always requests a real
	// top-level _txlock=immediate plus a short busy_timeout. Whether it
	// actually contends is determined entirely by what connection A's
	// BEGIN — built from the real production DSN — did.
	qB := url.Values{}
	qB.Add("_pragma", "busy_timeout(200)")
	qB.Set("_txlock", "immediate")
	dsnB := (&url.URL{Scheme: "file", Path: dbPath, RawQuery: qB.Encode()}).String()

	dbB, err := driver.Open(dsnB)
	if err != nil {
		t.Fatalf("driver.Open(%q) = _, %v, want nil error", dsnB, err)
	}
	defer dbB.Close() //nolint:errcheck // best-effort cleanup, the assertions below already ran

	start := time.Now()
	txB, err := dbB.BeginTx(ctx, nil)
	elapsed := time.Since(start)

	if err == nil {
		txB.Rollback() //nolint:errcheck // best-effort cleanup, the test already fails via t.Fatalf below
		t.Fatalf("connB.BeginTx() succeeded in %v while connA's transaction (built from buildDSN's own DSN) was still open, want SQLITE_BUSY — connA's BEGIN did not take the write lock, so buildDSN's _txlock is not in effect as \"immediate\"", elapsed)
	}
	if elapsed > 2*time.Second {
		t.Errorf("connB.BeginTx() took %v to fail, want it bounded by its 200ms busy_timeout", elapsed)
	}
}
