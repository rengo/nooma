//go:build integration

package sqlite

import (
	"testing"

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
