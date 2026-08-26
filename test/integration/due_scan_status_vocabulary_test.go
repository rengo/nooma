//go:build integration

package integration

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/brain"
	"github.com/rengo/nooma/internal/core/prospection"
	"github.com/rengo/nooma/internal/ports"
	"github.com/rengo/nooma/internal/store/sqlite"

	_ "github.com/ncruces/go-sqlite3/driver"
)

// TestDueScan_WritesOnlyVocabularyStatuses is the constraint the schema
// does not carry.
//
// triggers.status and timers.status are plain TEXT with a DEFAULT and no
// CHECK (migration 0001), and SQLite's dynamic typing will store anything
// at all in them. So a mistyped literal anywhere in the write path — a
// verdict string leaking into a status column, a mapping returning "stale"
// instead of "expired" — persists happily and is found much later, by
// which point rows exist that no vocabulary can parse.
//
// Every other test in this change would stay green through exactly that
// mutation: the L2 sweeps assert what the mapping returns, and the fakes
// have no column to hold a wrong value in. This test reads what SQLite
// actually holds after a scan, which is the only place the claim is
// checkable.
//
// The fixtures cross every prospection.Verdict with both tables, so what
// is asserted is the vocabulary of the whole write path and not of one
// branch.
func TestDueScan_WritesOnlyVocabularyStatuses(t *testing.T) {
	ctx := context.Background()

	// A Wednesday noon; the scan runs well past every staleness window so
	// each seeded row has reached its verdict.
	fireAt := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	now := fireAt.Add(time.Duration(prospection.TriggerStalenessHours+1) * time.Hour)

	dbPath := filepath.Join(t.TempDir(), "vault.db")
	v, err := sqlite.Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("sqlite.Open(%q): %v", dbPath, err)
	}
	triggers := sqlite.NewTriggerRepo(v)
	timers := sqlite.NewTimerRepo(v)
	decisions := sqlite.NewDecisionLog(v)

	// One row per verdict-reaching shape, in both tables: already stale,
	// due right now, and not due yet.
	for i, offset := range []time.Duration{
		-time.Duration(prospection.TriggerStalenessHours+2) * time.Hour, // stale by the time now arrives
		0,              // due at fireAt
		48 * time.Hour, // still pending at now
	} {
		at := fireAt.Add(offset)
		if err := triggers.Create(ctx, ports.Trigger{
			ID:        "trg-" + string(rune('a'+i)),
			Kind:      ports.TriggerKindTimeBased,
			FireAt:    &at,
			CreatedAt: fireAt.Add(-72 * time.Hour),
		}); err != nil {
			t.Fatalf("Create trigger: %v", err)
		}
		if err := timers.Create(ctx, ports.Timer{
			ID:        "tmr-" + string(rune('a'+i)),
			FireAt:    at,
			CreatedAt: fireAt.Add(-72 * time.Hour),
		}); err != nil {
			t.Fatalf("Create timer: %v", err)
		}
	}

	if _, err := brain.NewCheckService(fixedClock{now: now}, triggers, timers, &counterIDs{}, decisions, nil, nil, nil, nil, "").
		Check(ctx, brain.CheckRequest{}); err != nil {
		t.Fatalf("Check: %v", err)
	}

	triggerVocabulary := map[string]bool{}
	for _, s := range ports.AllTriggerStatuses() {
		triggerVocabulary[string(s)] = true
	}
	// The vault's own handle is unexported, and reopening the file
	// read-only through the same DSN is how every L3 test in this package
	// reads raw — foreign_key_test.go's own pragmaDSN. Closing the vault
	// first keeps the two handles from arguing over the WAL.
	if err := v.Close(); err != nil {
		t.Fatalf("closing the vault before the raw read: %v", err)
	}
	raw, err := sql.Open("sqlite3", pragmaDSN(dbPath))
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	t.Cleanup(func() { _ = raw.Close() })

	assertDistinctStatuses(t, raw, "triggers", triggerVocabulary)

	timerVocabulary := map[string]bool{}
	for _, s := range ports.AllTimerStatuses() {
		timerVocabulary[string(s)] = true
	}
	assertDistinctStatuses(t, raw, "timers", timerVocabulary)
}

// assertDistinctStatuses fails unless every distinct status the table holds
// is a member of vocabulary, and fails loudly on an empty table rather than
// passing vacuously.
func assertDistinctStatuses(t *testing.T, raw *sql.DB, table string, vocabulary map[string]bool) {
	t.Helper()

	rows, err := raw.QueryContext(context.Background(),
		`SELECT DISTINCT status FROM `+table)
	if err != nil {
		t.Fatalf("select distinct %s.status: %v", table, err)
	}
	defer func() { _ = rows.Close() }()

	seen := 0
	for rows.Next() {
		var status sql.NullString
		if err := rows.Scan(&status); err != nil {
			t.Fatalf("scan %s.status: %v", table, err)
		}
		seen++
		if !status.Valid {
			t.Errorf("%s holds a NULL status — the column is NOT NULL and nothing may write one", table)
			continue
		}
		if !vocabulary[status.String] {
			t.Errorf("%s holds status %q, which is not a member of the port's vocabulary — the column has no CHECK constraint, so this test is the constraint", table, status.String)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("select distinct %s.status: %v", table, err)
	}
	if seen == 0 {
		t.Fatalf("%s holds no rows at all — this test checked nothing", table)
	}
}
