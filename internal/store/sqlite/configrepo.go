package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/rengo/nooma/internal/ports"
)

// ConfigRepo is the SQLite-backed ports.ConfigRepo over the config
// singleton row (migration 0002:61-70) — design §3.4, spec R2.4/R2.6.
type ConfigRepo struct {
	db *sql.DB
}

// NewConfigRepo returns a ports.ConfigRepo backed by v's already-migrated
// vault.
func NewConfigRepo(v *Vault) *ConfigRepo {
	return &ConfigRepo{db: v.db}
}

var _ ports.ConfigRepo = (*ConfigRepo)(nil)

// Load implements ports.ConfigRepo.
//
// An absent row (id = 1) returns an all-nil VaultConfig and a nil error —
// the normal state of a vault that has never completed a consolidation
// pass (spec R2.4, owner ruling Q1). Load itself writes nothing to seed
// the row; only RecordConsolidationRun may create it.
//
// A present row returns every field as stored, including a corrupt one —
// Load never sanitizes or defaults; that is
// consolidation.Resolve*/focus.ResolveMargin's job (design §3.4).
//
// consolidation_last_run_at is the one exception to "as stored": a value
// that does not parse as unitTimeLayout is an ERROR, never nil for
// "unparseable" (design §3.4, §5.4) — a corrupt timestamp silently read as
// nil would turn SelectConnectSources into a whole-live-pool read instead
// of surfacing the corruption.
func (r *ConfigRepo) Load(ctx context.Context) (ports.VaultConfig, error) {
	const q = `
SELECT weight_threshold, hysteresis_margin, consolidation_enabled,
       goal_stagnation_days, mental_load_threshold, consolidation_last_run_at
FROM config WHERE id = 1`

	var (
		weightThreshold, hysteresisMargin       sql.NullFloat64
		consolidationEnabled                    sql.NullBool
		goalStagnationDays, mentalLoadThreshold sql.NullInt64
		consolidationLastRunAt                  sql.NullString
	)
	err := r.db.QueryRowContext(ctx, q).Scan(
		&weightThreshold, &hysteresisMargin, &consolidationEnabled,
		&goalStagnationDays, &mentalLoadThreshold, &consolidationLastRunAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return ports.VaultConfig{}, nil
	}
	if err != nil {
		return ports.VaultConfig{}, fmt.Errorf("reading config: %w", err)
	}

	var cfg ports.VaultConfig
	if weightThreshold.Valid {
		v := weightThreshold.Float64
		cfg.WeightThreshold = &v
	}
	if hysteresisMargin.Valid {
		v := hysteresisMargin.Float64
		cfg.HysteresisMargin = &v
	}
	if consolidationEnabled.Valid {
		v := consolidationEnabled.Bool
		cfg.ConsolidationEnabled = &v
	}
	if goalStagnationDays.Valid {
		v := int(goalStagnationDays.Int64)
		cfg.GoalStagnationDays = &v
	}
	if mentalLoadThreshold.Valid {
		v := int(mentalLoadThreshold.Int64)
		cfg.MentalLoadThreshold = &v
	}
	if consolidationLastRunAt.Valid {
		t, err := time.Parse(unitTimeLayout, consolidationLastRunAt.String)
		if err != nil {
			return ports.VaultConfig{}, fmt.Errorf("config.consolidation_last_run_at: %w", err)
		}
		cfg.ConsolidationLastRunAt = &t
	}
	return cfg, nil
}

// RecordConsolidationRun implements ports.ConfigRepo — design §3.4's exact
// UPSERT. Every column but id, consolidation_last_run_at and updated_at is
// omitted from the statement, so a fresh row (id = 1 absent) takes the
// migration's own SQL DEFAULTs (weight_threshold 0.5, hysteresis_margin
// 0.05, consolidation_enabled 1, goal_stagnation_days 21,
// mental_load_threshold 7) — this statement never names a default in Go
// (spec R2.6). config.updated_at is TEXT NOT NULL with no DEFAULT
// (migration 0002:69), so it is always supplied, with at — the pass's own
// instant.
func (r *ConfigRepo) RecordConsolidationRun(ctx context.Context, at time.Time) error {
	const q = `
INSERT INTO config (id, consolidation_last_run_at, updated_at)
VALUES (1, ?, ?)
ON CONFLICT(id) DO UPDATE SET
  consolidation_last_run_at = excluded.consolidation_last_run_at,
  updated_at = excluded.updated_at`

	atText := formatUnitTime(at)
	if _, err := r.db.ExecContext(ctx, q, atText, atText); err != nil {
		return fmt.Errorf("recording consolidation run at %s: %w", at, err)
	}
	return nil
}
