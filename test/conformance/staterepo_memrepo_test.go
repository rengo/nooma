// Package conformance — see test/conformance/doc.go for the package contract.
package conformance

import (
	"testing"

	"github.com/rengo/nooma/internal/ports"
	"github.com/rengo/nooma/test/support/memrepo"
	"github.com/rengo/nooma/test/support/repocontract"
)

// TestStateRepo_MemRepo runs repocontract.RunOpenHypothesis against the
// in-memory fake, at L2. internal/store/sqlite has no implementation of
// ports.StateRepo until PR 6 (feat/store-selfmodel-config-state) — design
// D6's "answered twice" rule applies there, not here; StateRepo is
// declared against PR 4's schema, which has not landed at this PR
// (ports.StateRepo's own doc comment).
func TestStateRepo_MemRepo(t *testing.T) {
	repocontract.RunOpenHypothesis(t, func(t *testing.T) ports.StateRepo {
		t.Helper()
		return memrepo.NewState()
	})
}

// TestStateRepo_MemRepo_LastHypothesisAt runs
// repocontract.RunLastHypothesisAt against the same fake.
func TestStateRepo_MemRepo_LastHypothesisAt(t *testing.T) {
	repocontract.RunLastHypothesisAt(t, func(t *testing.T) ports.StateRepo {
		t.Helper()
		return memrepo.NewState()
	})
}
