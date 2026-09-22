//go:build integration

package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

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
