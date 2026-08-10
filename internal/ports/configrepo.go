package ports

import (
	"context"
	"time"
)

// VaultConfig mirrors the config singleton row (migration 0002:61-70) as
// nil-sentinel typed pointers. nil means "the row does not exist, or that
// column is NULL". ConfigRepo never decides what nil means — that is
// consolidation.Resolve*/focus.ResolveMargin's job, and each of them
// already takes exactly the pointer type below (owner ruling Q1, option
// C).
type VaultConfig struct {
	WeightThreshold        *float64   // → consolidation.ResolveWeightThreshold
	HysteresisMargin       *float64   // → focus.ResolveMargin — NO reader in m2c
	ConsolidationEnabled   *bool      // → no reader in m2c; m2d's cron gate
	GoalStagnationDays     *int       // → consolidation.ResolveGoalStagnationDays
	MentalLoadThreshold    *int       // → consolidation.ResolveMentalLoadThreshold
	ConsolidationLastRunAt *time.Time // → passContext.since, and m2d's boot catch-up
}

// ConfigRepo is the repository port over the config singleton row —
// design §3.4, spec R2.4-R2.6.
type ConfigRepo interface {
	// Load returns the singleton row (id = 1). When it does not exist
	// every field is nil and the error is nil: an unwritten config row is
	// the normal state of a vault, not a failure (spec R2.4). No migration
	// seeds it, and this port never creates it except through
	// RecordConsolidationRun. When the row exists, every field carries the
	// column's stored value as stored — including a corrupt one —
	// ConfigRepo does not itself decide what a nil or an out-of-range
	// value defaults to; that is consolidation.Resolve*/focus.ResolveMargin's
	// job, both already shipped (m2a/m2b), fed the pointer this method
	// returns.
	Load(ctx context.Context) (VaultConfig, error)

	// RecordConsolidationRun writes at to consolidation_last_run_at,
	// creating row id = 1 with the migration's own SQL DEFAULTs for every
	// other column when it is absent; changing only that one column when
	// it exists. ConfigRepo's only write (spec R2.6) — the sole exception
	// to Load's "writes nothing to seed it" rule, and not a contradiction
	// of it: the row's lazy creation is a side effect of the first
	// completed consolidation pass, never a Go literal duplicating the SQL
	// DEFAULTs. A vault that never runs a pass still has no config row at
	// all.
	RecordConsolidationRun(ctx context.Context, at time.Time) error
}
