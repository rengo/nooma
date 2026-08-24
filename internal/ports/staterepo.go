package ports

import (
	"context"
	"time"

	"github.com/rengo/nooma/internal/core/prospection"
)

// StateSourceUser, StateSourceConsolidation and MoodLoaded are the
// literals StateRepo's implementations read and write. Constants in
// internal/ports, therefore outside
// test/conformance/calibration_doc_test.go's reach (§13 covers
// internal/core symbols only) — stated, not implied. They are pinned
// instead to migration 0003's own column comment by an L2 test in PR 4,
// the shape relation.AllCreatedBy already uses against 0001:37.
const (
	StateSourceUser          = "user"
	StateSourceConsolidation = "consolidation"
	MoodLoaded               = "loaded"
)

// StateHypothesis is one current_state row written by the load watcher.
type StateHypothesis struct {
	ID         string
	Mood       string // MoodLoaded for the load watcher
	RecordedAt time.Time
}

// StateRepo is the repository port over current_state's
// consolidation-sourced rows — design §4.4.
//
// current_state.source does not exist yet. Migration 0002 — the vault's
// current schema as of this PR — declares current_state with id, energy,
// mood, active and recorded_at only (migration 0001:87-93); there is no
// source column. This port is declared against the schema PR 4
// (feat/schema-current-state-source) will leave once its migration 0003
// lands the column, not against the schema as it stands today. No
// internal/store/sqlite implementation of StateRepo exists until PR 6
// (feat/store-selfmodel-config-state), by which point PR 4's migration
// will already be in place.
type StateRepo interface {
	// OpenHypothesis appends one current_state row with
	// source = StateSourceConsolidation, energy NULL and active = 1.
	// Append-only: this port has no update path and no removal-prefixed
	// method, so doc 02 §10's "append-only rows" is structural here rather
	// than a convention the caller remembers.
	OpenHypothesis(ctx context.Context, h StateHypothesis) error

	// LastHypothesisAt returns the recorded_at of the most recent row
	// written with source = StateSourceConsolidation, or nil when there is
	// none. It feeds consolidation.EvaluateLoad's lastHypothesisAt
	// parameter directly (m2b §9 Q6).
	LastHypothesisAt(ctx context.Context) (*time.Time, error)

	// LatestEnergy returns the most recent current_state energy reading
	// with the instant it was recorded, or nil when there is none.
	//
	// **nil, never a zero value.** prospection.LowEnergy treats a missing
	// or stale reading as NOT low, because that gate suppresses delivery
	// and anything core cannot vouch for must not be grounds to suppress.
	// A zero EnergyReading would read as level 0.0 recorded at the zero
	// instant — low and ancient — and a vault whose owner has never
	// answered a check-in would silently stop speaking. The pointer is
	// what keeps "no reading" from becoming "a bad reading".
	LatestEnergy(ctx context.Context) (*prospection.EnergyReading, error)
}
