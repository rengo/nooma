package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/ncruces/go-sqlite3"

	"github.com/rengo/nooma/internal/ports"
)

// DecisionLog is the SQLite-backed ports.DecisionLog over decision_log
// (migration 0001:95-102).
type DecisionLog struct {
	db *sql.DB
}

// NewDecisionLog returns a ports.DecisionLog backed by v's already-migrated
// vault.
func NewDecisionLog(v *Vault) *DecisionLog {
	return &DecisionLog{db: v.db}
}

var _ ports.DecisionLog = (*DecisionLog)(nil)

// Record implements ports.DecisionLog.
//
// A nil/empty Context omits the context column from the INSERT rather than
// binding NULL — the column is `NOT NULL DEFAULT '{}'` (0001:98), so
// binding NULL would violate the constraint instead of falling back to the
// default. This is the one place this PR lets the SQL DEFAULT actually
// supply the value, rather than mirroring it as a Go literal: decisionlog.go
// is a new file, not one of Phase A's closed files (design D3's own
// "omit the column" option was rejected only where UnitRepo.Create binds
// every column and R6.3 keeps that file closed — neither applies here).
func (r *DecisionLog) Record(ctx context.Context, d ports.Decision) error {
	var err error
	if len(d.Context) == 0 {
		_, err = r.db.ExecContext(ctx, `
INSERT INTO decision_log (id, action, rationale, occurred_at)
VALUES (?, ?, ?, ?)`,
			d.ID, string(d.Action), d.Rationale, d.OccurredAt.UTC().Format(unitTimeLayout),
		)
	} else {
		_, err = r.db.ExecContext(ctx, `
INSERT INTO decision_log (id, action, rationale, context, occurred_at)
VALUES (?, ?, ?, ?, ?)`,
			d.ID, string(d.Action), d.Rationale, string(d.Context), d.OccurredAt.UTC().Format(unitTimeLayout),
		)
	}
	if err != nil {
		if errors.Is(err, sqlite3.CONSTRAINT_PRIMARYKEY) {
			return ports.ErrDecisionExists
		}
		return fmt.Errorf("recording decision %q: %w", d.ID, err)
	}
	return nil
}

// Since implements ports.DecisionLog. occurred_at > ? is a strict bound —
// see ports.DecisionLog.Since's doc comment for why an inclusive bound
// would re-deliver the caller's own cursor row forever. ORDER BY
// occurred_at, id gives a deterministic order even when two rows share one
// occurred_at value, which the column carries no uniqueness constraint
// against. LIMIT is bound directly rather than truncated in Go, so the
// database — not this process — decides which rows are "the earliest
// limit".
func (r *DecisionLog) Since(ctx context.Context, t time.Time, limit int) ([]ports.Decision, error) {
	const q = `
SELECT id, action, rationale, context, occurred_at
FROM decision_log
WHERE occurred_at > ?
ORDER BY occurred_at, id
LIMIT ?`

	rows, err := r.db.QueryContext(ctx, q, t.UTC().Format(unitTimeLayout), limit)
	if err != nil {
		return nil, fmt.Errorf("reading decisions since %s: %w", t, err)
	}
	defer func() { _ = rows.Close() }()

	var out []ports.Decision
	for rows.Next() {
		var (
			id, action, rationale, context, occurredAt string
		)
		if err := rows.Scan(&id, &action, &rationale, &context, &occurredAt); err != nil {
			return nil, fmt.Errorf("scanning a decision row: %w", err)
		}
		at, err := time.Parse(unitTimeLayout, occurredAt)
		if err != nil {
			return nil, fmt.Errorf("decision %q: occurred_at: %w", id, err)
		}
		out = append(out, ports.Decision{
			ID:         id,
			Action:     ports.DecisionAction(action),
			Rationale:  rationale,
			Context:    []byte(context),
			OccurredAt: at,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("reading decisions since %s: %w", t, err)
	}
	return out, nil
}
