// Package conformance — see test/conformance/doc.go for the package contract.
package conformance

import (
	"testing"

	"github.com/rengo/nooma/internal/ports"
	"github.com/rengo/nooma/test/support/memrepo"
	"github.com/rengo/nooma/test/support/repocontract"
)

// TestUnitRepo_MemRepo runs repocontract.RunUnitRepo — the shared
// ports.UnitRepo contract suite (design D6) — against test/support/memrepo,
// its first caller. This is where the contract's every case actually runs
// for the first time, and where I03's behavioral half (not just
// i03_units_never_deleted_test.go's structural reflection check) gets
// proven against a real implementation, per spec R3.1, R3.2 and R3.3
// answered together.
func TestUnitRepo_MemRepo(t *testing.T) {
	repocontract.RunUnitRepo(t, func(t *testing.T) ports.UnitRepo {
		t.Helper()
		return memrepo.NewUnits()
	})
}

// TestUnitRepo_MemRepo_ApplyBoosts runs repocontract.RunApplyBoosts —
// I24's own contract suite (spec R1.1, R1.4) — against the same fake.
func TestUnitRepo_MemRepo_ApplyBoosts(t *testing.T) {
	repocontract.RunApplyBoosts(t, func(t *testing.T) ports.UnitRepo {
		t.Helper()
		return memrepo.NewUnits()
	})
}

// TestUnitRepo_MemRepo_CountLiveByType runs repocontract.RunCountLiveByType
// — owner ruling 6's contract suite (spec R1.2) — against the same fake.
func TestUnitRepo_MemRepo_CountLiveByType(t *testing.T) {
	repocontract.RunCountLiveByType(t, func(t *testing.T) ports.UnitRepo {
		t.Helper()
		return memrepo.NewUnits()
	})
}

// TestUnitRepo_MemRepo_IncompleteOlderThan runs
// repocontract.RunIncompleteOlderThan — spec R5.1's contract suite —
// against the same fake.
func TestUnitRepo_MemRepo_IncompleteOlderThan(t *testing.T) {
	repocontract.RunIncompleteOlderThan(t, func(t *testing.T) ports.UnitRepo {
		t.Helper()
		return memrepo.NewUnits()
	})
}

// TestUnitRepo_MemRepo_LiveDecayStates runs
// repocontract.RunLiveDecayStates — design §4.1's contract suite — against
// the same fake.
func TestUnitRepo_MemRepo_LiveDecayStates(t *testing.T) {
	repocontract.RunLiveDecayStates(t, func(t *testing.T) ports.UnitRepo {
		t.Helper()
		return memrepo.NewUnits()
	})
}

// TestUnitRepo_MemRepo_LiveFocusCandidates runs
// repocontract.RunLiveFocusCandidates — spec R3.1's contract suite —
// against the same fake.
func TestUnitRepo_MemRepo_LiveFocusCandidates(t *testing.T) {
	repocontract.RunLiveFocusCandidates(t, func(t *testing.T) ports.UnitRepo {
		t.Helper()
		return memrepo.NewUnits()
	})
}
