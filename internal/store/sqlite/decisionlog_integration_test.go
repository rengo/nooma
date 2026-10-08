//go:build integration

package sqlite

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/ports"
	"github.com/rengo/nooma/test/support/repocontract"
)

// decisionFixtureTime is a fixed, whole-second UTC instant: this suite does
// not exercise clock behavior, so a literal keeps every fixture identical.
var decisionFixtureTime = time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)

// TestDecisionLog_Contract runs the same repocontract.RunDecisionLog suite
// the in-memory fake answers at L2, now against a real temporary SQLite
// vault at L3 — design D6's "answered twice" standing rule. decision_log
// carries no foreign key (0001:95-102, confirmed off the DDL, not assumed),
// so unlike TestEmbeddingRepo_Contract this repo needs no harness beyond
// the port itself.
func TestDecisionLog_Contract(t *testing.T) {
	repocontract.RunDecisionLog(t, func(t *testing.T) ports.DecisionLog {
		return NewDecisionLog(openTestVault(t))
	})
}

// TestDecisionLog_ContextDefaultsToEmptyObject is task 10a.3's own claim,
// below the contract: migration 0001's decision_log.context column is
// `NOT NULL DEFAULT '{}'` (0001:98). Whether an absent Context ends up
// '{}' is a promise the store alone can keep — the in-memory fake enforces
// no column default at all, so this belongs at L3, not in the shared
// contract (C11/C12's lesson: a promise only one implementation can honor
// is not a contract case).
func TestDecisionLog_ContextDefaultsToEmptyObject(t *testing.T) {
	v := openTestVault(t)
	repo := NewDecisionLog(v)
	ctx := context.Background()

	d := ports.Decision{
		ID:         "decision-no-context",
		Action:     ports.ActionCaptureClassify,
		Rationale:  "context omitted on purpose",
		Context:    nil,
		OccurredAt: decisionFixtureTime,
	}
	if err := repo.Record(ctx, d); err != nil {
		t.Fatalf("Record: %v", err)
	}

	var context string
	row := v.db.QueryRowContext(ctx, `SELECT context FROM decision_log WHERE id = ?`, d.ID)
	if err := row.Scan(&context); err != nil {
		t.Fatalf("reading the stored row: %v", err)
	}
	if context != "{}" {
		t.Errorf("context = %q, want %q — migration 0001's own DDL default (0001:98)", context, "{}")
	}

	// Round-tripped through Since, too: a caller reading the row back must
	// see the same default the column actually holds, not a Go-side zero
	// value that happens to look similar.
	got, err := repo.Since(ctx, decisionFixtureTime.Add(-time.Minute), 10)
	if err != nil {
		t.Fatalf("Since: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("Since returned %d rows, want 1: %v", len(got), got)
	}
	if string(got[0].Context) != "{}" {
		t.Errorf("Since round-trip Context = %q, want %q", got[0].Context, "{}")
	}
}

// TestDecisionLog_ContextExplicitValueRoundTrips proves the non-default
// path: a Context the caller does supply is stored and read back verbatim,
// not overwritten by the column default.
func TestDecisionLog_ContextExplicitValueRoundTrips(t *testing.T) {
	v := openTestVault(t)
	repo := NewDecisionLog(v)
	ctx := context.Background()

	d := ports.Decision{
		ID:         "decision-with-context",
		Action:     ports.ActionCapturePersonRefAmbiguous,
		Rationale:  "context supplied explicitly",
		Context:    json.RawMessage(`{"kind":"timer","reason":"prospection_not_implemented"}`),
		OccurredAt: decisionFixtureTime,
	}
	if err := repo.Record(ctx, d); err != nil {
		t.Fatalf("Record: %v", err)
	}

	var context string
	row := v.db.QueryRowContext(ctx, `SELECT context FROM decision_log WHERE id = ?`, d.ID)
	if err := row.Scan(&context); err != nil {
		t.Fatalf("reading the stored row: %v", err)
	}
	if context != string(d.Context) {
		t.Errorf("context = %q, want %q — Record must not fall back to the column default "+
			"when the caller supplies a value", context, d.Context)
	}
}

// TestDecisionLog_BeforeUsesOccurredIndexWithoutSorting pins design §3.5's
// claim that Before's order is satisfiable from idx_decision_log_occurred
// (occurred_at, rowid): no migration, no temp b-tree. The unfiltered and the
// cursor forms are pinned; the prefix form is a filter on top of the same
// scan (risk RK-9).
func TestDecisionLog_BeforeUsesOccurredIndexWithoutSorting(t *testing.T) {
	cursor := &ports.DecisionCursor{OccurredAt: decisionFixtureTime, Seq: 7}
	tests := []struct {
		name   string
		cursor *ports.DecisionCursor
		prefix string
	}{
		{name: "no cursor", cursor: nil},
		{name: "cursor", cursor: cursor},
		{name: "cursor and prefix", cursor: cursor, prefix: "check."},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := openTestVault(t)
			query, args := buildDecisionsBeforeQuery(tt.cursor, tt.prefix, 51)

			rows, err := v.db.QueryContext(context.Background(), `EXPLAIN QUERY PLAN `+query, args...)
			if err != nil {
				t.Fatalf("EXPLAIN QUERY PLAN: %v", err)
			}
			defer rows.Close() //nolint:errcheck // read-only query, nothing left to clean up on error

			var plan string
			for rows.Next() {
				var id, parent, notused int
				var detail string
				if err := rows.Scan(&id, &parent, &notused, &detail); err != nil {
					t.Fatalf("scanning query plan row: %v", err)
				}
				plan += detail + "\n"
			}
			if err := rows.Err(); err != nil {
				t.Fatalf("query plan rows: %v", err)
			}
			if !strings.Contains(plan, "idx_decision_log_occurred") {
				t.Errorf("query plan for %q does not mention idx_decision_log_occurred:\n%s", query, plan)
			}
			if strings.Contains(plan, "USE TEMP B-TREE FOR ORDER BY") {
				t.Errorf("query plan for %q uses a temp b-tree for ORDER BY — the index should already "+
					"provide occurred_at DESC, rowid DESC order:\n%s", query, plan)
			}
		})
	}
}
