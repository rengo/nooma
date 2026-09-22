//go:build integration

package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/core/prospection"
	"github.com/rengo/nooma/internal/ports"
	"github.com/rengo/nooma/test/support/repocontract"
)

// TestStateRepo_OpenHypothesis and TestStateRepo_LastHypothesisAt run the
// same repocontract suites test/support/memrepo/state.go already answers at
// L2 (PR 3), now against a real temporary SQLite vault at L3 — design D6's
// "answered twice" standing rule. Migration 0003 (PR 4, already merged to
// main) is what makes current_state.source exist for this adapter to write
// and filter on.
func TestStateRepo_OpenHypothesis(t *testing.T) {
	repocontract.RunOpenHypothesis(t, func(t *testing.T) ports.StateRepo {
		return NewStateRepo(openTestVault(t))
	})
}

func TestStateRepo_LastHypothesisAt(t *testing.T) {
	repocontract.RunLastHypothesisAt(t, func(t *testing.T) ports.StateRepo {
		return NewStateRepo(openTestVault(t))
	})
}

// TestStateRepo_LatestEnergy runs repocontract.RunLatestEnergy — design
// §3.6's Source-widening contract — against a real temporary SQLite vault,
// seeding each row with a raw INSERT since ports.StateRepo declares no
// energy-writing method (repocontract.RunLatestEnergy's own doc comment).
func TestStateRepo_LatestEnergy(t *testing.T) {
	repocontract.RunLatestEnergy(t,
		func(t *testing.T) ports.StateRepo {
			return NewStateRepo(openTestVault(t))
		},
		func(t *testing.T, repo ports.StateRepo, level float64, recordedAt time.Time, source string) {
			t.Helper()
			seedEnergyReading(t, repo.(*StateRepo).db, level, recordedAt, source)
		},
	)
}

// TestStateRepo_LatestEnergySkipsNewerNullEnergyRow covers the branch
// repocontract.RunLatestEnergy's own doc comment names as SQLite-only: an
// older row carrying a real energy value, and a newer row OpenHypothesis
// wrote with energy left NULL by design (LatestEnergy's own doc comment).
// memrepo.State cannot host this fixture — it keeps consolidationRows and
// energy in two separate slices with no shared ordering
// (test/support/memrepo/state.go), so a mixed timeline cannot be expressed
// without restructuring the fake, out of this PR's scope. The NULL row is
// written through the real OpenHypothesis, not a raw INSERT, so this test
// exercises the exact row shape production writes.
func TestStateRepo_LatestEnergySkipsNewerNullEnergyRow(t *testing.T) {
	repo := NewStateRepo(openTestVault(t))
	ctx := context.Background()

	older := time.Date(2026, 8, 19, 7, 0, 0, 0, time.UTC)
	newer := older.Add(24 * time.Hour)

	seedEnergyReading(t, repo.db, 0.42, older, ports.StateSourceUser)

	if err := repo.OpenHypothesis(ctx, ports.StateHypothesis{
		ID:         "hyp-after-energy",
		Mood:       ports.MoodLoaded,
		RecordedAt: newer,
	}); err != nil {
		t.Fatalf("OpenHypothesis: %v", err)
	}

	got, err := repo.LatestEnergy(ctx)
	if err != nil {
		t.Fatalf("LatestEnergy: %v", err)
	}
	if got == nil {
		t.Fatalf("LatestEnergy() = nil, want the older energy reading — a newer NULL-energy " +
			"hypothesis row must not shadow it")
	}
	want := prospection.EnergyReading{Level: 0.42, RecordedAt: older, Source: ports.StateSourceUser}
	if got.Level != want.Level || got.Source != want.Source || !got.RecordedAt.Equal(want.RecordedAt) {
		t.Fatalf("LatestEnergy() = %+v, want %+v (the older reading, unaffected by the newer NULL row)",
			got, want)
	}
}

// seedEnergyReading inserts one current_state row directly via SQL — not
// through StateRepo, which declares no energy-writing method — carrying
// level, recordedAt and source.
func seedEnergyReading(t *testing.T, db *sql.DB, level float64, recordedAt time.Time, source string) {
	t.Helper()
	id := fmt.Sprintf("energy-%s-%d", source, recordedAt.UnixNano())
	_, err := db.ExecContext(context.Background(),
		`INSERT INTO current_state (id, energy, mood, active, recorded_at, source) VALUES (?, ?, '', 0, ?, ?)`,
		id, level, formatUnitTime(recordedAt), source,
	)
	if err != nil {
		t.Fatalf("seedEnergyReading(%v, %v, %q): %v", level, recordedAt, source, err)
	}
}
