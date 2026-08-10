package repocontract

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/ports"
)

// ConfigHarness is what a ports.ConfigRepo implementation must offer the
// contract so it can put a config row in front of it directly.
//
// SeedConfig exists for the same reason repocontract.RelationHarness's
// SeedThreshold does (design §4.2's own precedent): ports.ConfigRepo has
// exactly one write method, RecordConsolidationRun (spec R2.6), and it
// sets only ConsolidationLastRunAt. Without a seed hook, Load's "every
// field carries the column's stored value as stored, including a corrupt
// one" contract (spec R2.4) could never construct a fixture that stores
// anything in the other five columns at all, corrupt or not.
type ConfigHarness interface {
	ports.ConfigRepo

	// SeedConfig inserts cfg directly into the backing store, bypassing
	// ports.ConfigRepo entirely. A nil field in cfg is left unset — the
	// store's own zero value / NULL for that column. Calling SeedConfig
	// more than once against the same harness replaces the row.
	SeedConfig(t *testing.T, cfg ports.VaultConfig)

	// RowExists reports whether the config singleton row exists in the
	// backing store, independent of what Load returns. Load's "an absent
	// row returns an all-nil VaultConfig" contract (spec R2.4) is
	// satisfiable by two different implementations: one that writes
	// nothing on an absent read (correct — owner ruling Q1), and one that
	// lazily creates an all-NULL row on the way out of Load (incorrect,
	// but indistinguishable from Load's return value alone — an absent
	// row and an all-NULL row both decode to every field nil). RowExists
	// is what tells them apart.
	RowExists(t *testing.T) bool
}

// RunConfigRepoLoad runs the ports.ConfigRepo.Load contract against a
// fresh repository instance, built by newRepo for every subtest. newRepo
// must return a repository holding no config row.
//
// Spec R2.4; design §3.4. An absent row returns an all-nil VaultConfig and
// a nil error; a present row returns every field as stored, including a
// corrupt one — ConfigRepo never sanitizes or defaults.
func RunConfigRepoLoad(t *testing.T, newRepo func(t *testing.T) ConfigHarness) {
	t.Helper()

	t.Run("an absent row returns an all-nil VaultConfig and a nil error", func(t *testing.T) {
		repo := newRepo(t)

		got, err := repo.Load(context.Background())
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if got.WeightThreshold != nil || got.HysteresisMargin != nil || got.ConsolidationEnabled != nil ||
			got.GoalStagnationDays != nil || got.MentalLoadThreshold != nil || got.ConsolidationLastRunAt != nil {
			t.Errorf("Load(absent row) = %+v, want every field nil — an unwritten config row is the "+
				"normal state of a vault, not a failure", got)
		}
		if repo.RowExists(t) {
			t.Errorf("Load(absent row) created a config row as a side effect — Load must write " +
				"nothing to seed the row (spec R2.4, owner ruling Q1); only " +
				"RecordConsolidationRun may create it")
		}
	})

	// The genuine discriminator: a plausible wrong ConfigRepo "helpfully"
	// maps a non-finite WeightThreshold to the default or to nil on the
	// way out of Load, the same job consolidation.ResolveWeightThreshold
	// already owns (design §3.4). Only a corrupt fixture — not merely a
	// valid one — tells the correct "return it as stored" behaviour apart
	// from that wrong one; a fixture using only well-formed numbers would
	// pass a sanitizing Load exactly as easily as a faithful one.
	t.Run("a present row returns every field as stored, including a corrupt WeightThreshold", func(t *testing.T) {
		repo := newRepo(t)
		ctx := context.Background()

		corrupt := math.NaN()
		goalDays := 30
		mentalLoad := 9
		enabled := false
		lastRun := time.Date(2026, 8, 9, 3, 0, 0, 0, time.UTC)
		repo.SeedConfig(t, ports.VaultConfig{
			WeightThreshold:        &corrupt,
			GoalStagnationDays:     &goalDays,
			MentalLoadThreshold:    &mentalLoad,
			ConsolidationEnabled:   &enabled,
			ConsolidationLastRunAt: &lastRun,
			// HysteresisMargin deliberately left nil — proves a seeded row
			// can still carry a nil column, not just an absent row.
		})

		got, err := repo.Load(ctx)
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if got.WeightThreshold == nil || !math.IsNaN(*got.WeightThreshold) {
			t.Errorf("Load().WeightThreshold = %v, want the stored NaN returned as-is — ConfigRepo "+
				"must never sanitize or default a corrupt value itself (spec R2.4)", got.WeightThreshold)
		}
		if got.GoalStagnationDays == nil || *got.GoalStagnationDays != goalDays {
			t.Errorf("Load().GoalStagnationDays = %v, want %d", got.GoalStagnationDays, goalDays)
		}
		if got.MentalLoadThreshold == nil || *got.MentalLoadThreshold != mentalLoad {
			t.Errorf("Load().MentalLoadThreshold = %v, want %d", got.MentalLoadThreshold, mentalLoad)
		}
		if got.ConsolidationEnabled == nil || *got.ConsolidationEnabled != enabled {
			t.Errorf("Load().ConsolidationEnabled = %v, want %v", got.ConsolidationEnabled, enabled)
		}
		if got.ConsolidationLastRunAt == nil || !got.ConsolidationLastRunAt.Equal(lastRun) {
			t.Errorf("Load().ConsolidationLastRunAt = %v, want %v", got.ConsolidationLastRunAt, lastRun)
		}
		if got.HysteresisMargin != nil {
			t.Errorf("Load().HysteresisMargin = %v, want nil — the seed left this column unset",
				*got.HysteresisMargin)
		}
	})
}

// RunRecordConsolidationRun runs the ports.ConfigRepo.RecordConsolidationRun
// contract against a fresh repository instance, built by newRepo for every
// subtest. newRepo must return a repository holding no config row.
//
// Spec R2.6; design §3.4. Writing to an absent row creates it with
// consolidation_last_run_at set to the given instant and every other field
// still nil (mirroring the migration's own SQL DEFAULTs at the fake's own
// zero-value level — the fake proves the shape of the lazy-create, PR 6's
// sqlite implementation proves the SQL DEFAULTs themselves). Writing to an
// existing row changes only that one field.
func RunRecordConsolidationRun(t *testing.T, newRepo func(t *testing.T) ConfigHarness) {
	t.Helper()

	t.Run("writing to an absent row creates it with every other field still nil", func(t *testing.T) {
		repo := newRepo(t)
		ctx := context.Background()
		at := time.Date(2026, 8, 9, 3, 0, 0, 0, time.UTC)

		if err := repo.RecordConsolidationRun(ctx, at); err != nil {
			t.Fatalf("RecordConsolidationRun: %v", err)
		}

		got, err := repo.Load(ctx)
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if got.ConsolidationLastRunAt == nil || !got.ConsolidationLastRunAt.Equal(at) {
			t.Fatalf("Load().ConsolidationLastRunAt = %v, want %v", got.ConsolidationLastRunAt, at)
		}
		if got.WeightThreshold != nil || got.HysteresisMargin != nil || got.ConsolidationEnabled != nil ||
			got.GoalStagnationDays != nil || got.MentalLoadThreshold != nil {
			t.Errorf("Load() after a lazy-create = %+v, want every field but "+
				"ConsolidationLastRunAt still nil at the fake's own zero-value level", got)
		}
	})

	// The boundary this case exists for: a plausible wrong
	// RecordConsolidationRun could re-zero every other seeded field on
	// every write (a naive INSERT OR REPLACE with no ON CONFLICT DO
	// UPDATE, or a fake that always overwrites the whole struct) instead
	// of touching only consolidation_last_run_at. Seeding the other five
	// fields first, then writing twice, is what tells "changes only this
	// one column" apart from "replaces the row".
	t.Run("writing to an existing row changes only consolidation_last_run_at", func(t *testing.T) {
		repo := newRepo(t)
		ctx := context.Background()
		goalDays := 30
		repo.SeedConfig(t, ports.VaultConfig{GoalStagnationDays: &goalDays})

		first := time.Date(2026, 8, 8, 3, 0, 0, 0, time.UTC)
		second := time.Date(2026, 8, 9, 3, 0, 0, 0, time.UTC)
		if err := repo.RecordConsolidationRun(ctx, first); err != nil {
			t.Fatalf("first RecordConsolidationRun: %v", err)
		}
		if err := repo.RecordConsolidationRun(ctx, second); err != nil {
			t.Fatalf("second RecordConsolidationRun: %v", err)
		}

		got, err := repo.Load(ctx)
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if got.ConsolidationLastRunAt == nil || !got.ConsolidationLastRunAt.Equal(second) {
			t.Errorf("Load().ConsolidationLastRunAt = %v, want the second write's %v", got.ConsolidationLastRunAt, second)
		}
		if got.GoalStagnationDays == nil || *got.GoalStagnationDays != goalDays {
			t.Errorf("Load().GoalStagnationDays = %v, want the seeded %d untouched by two "+
				"RecordConsolidationRun calls", got.GoalStagnationDays, goalDays)
		}
	})
}
