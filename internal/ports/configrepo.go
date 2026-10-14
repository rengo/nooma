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
	HysteresisMargin       *float64   // → focus.ResolveMargin, read by brain.FocusKeeper
	ConsolidationEnabled   *bool      // → internal/scheduler (catchup.go:36, scheduler.go:247)
	GoalStagnationDays     *int       // → consolidation.ResolveGoalStagnationDays
	MentalLoadThreshold    *int       // → consolidation.ResolveMentalLoadThreshold
	ConsolidationLastRunAt *time.Time // → passContext.since, and m2d's boot catch-up
	EventReminderLeads     *string    // → prospection.ResolveReminderPrefs (ADR-0029)
	DateOnlyReminderAt     *string    // → prospection.ResolveReminderPrefs
	QuietHoursEnd          *string    // → prospection.ResolveReminderPrefs (ADR-0030)
	EveningReminderAt      *string    // → prospection.ResolveReminderPrefs
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

	// SetReminderPrefs writes the user's reminder and quiet-hours
	// preferences (ADR-0029, ADR-0030) in one statement: each non-nil field
	// of u replaces its column, each nil one leaves it as it is, and
	// updated_at becomes at. An absent row is created the way
	// RecordConsolidationRun creates it. One write for the four because the
	// evening time is valid only against the quiet end it lands with.
	// Values are stored as given: validating them is the caller's
	// (brain.PreferencesService), with the same parsers Resolve uses.
	SetReminderPrefs(ctx context.Context, u ReminderPrefsUpdate, at time.Time) error

	// The five setters below are the admin settings' only write path
	// (m4e2-admin design Q5): one per column, no generic writer, no setter
	// for relation thresholds or consolidation_last_run_at. Each one writes
	// its own column and updated_at = at and nothing else, creating an
	// absent row the way RecordConsolidationRun does (the migration's own
	// defaults for every other column). Values are stored as given:
	// validating them is the caller's (brain.AdminService), with the same
	// Resolve functions the readers use.
	SetWeightThreshold(ctx context.Context, v float64, at time.Time) error
	SetHysteresisMargin(ctx context.Context, v float64, at time.Time) error
	SetGoalStagnationDays(ctx context.Context, v int, at time.Time) error
	SetMentalLoadThreshold(ctx context.Context, v int, at time.Time) error
	SetConsolidationEnabled(ctx context.Context, v bool, at time.Time) error
}

// ReminderPrefsUpdate is the preference columns SetReminderPrefs writes,
// in their stored form; nil leaves a column unchanged.
type ReminderPrefsUpdate struct {
	EventReminderLeads *string // a JSON array of minutes
	DateOnlyReminderAt *string // HH:MM
	QuietHoursEnd      *string // HH:MM
	EveningReminderAt  *string // HH:MM
}
