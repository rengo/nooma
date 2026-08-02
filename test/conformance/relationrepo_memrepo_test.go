// Package conformance — see test/conformance/doc.go for the package contract.
package conformance

import (
	"testing"

	"github.com/rengo/nooma/test/support/memrepo"
	"github.com/rengo/nooma/test/support/repocontract"
)

// TestRelationRepo_MemRepo runs repocontract.RunRelationRepo against the
// in-memory fake, at L2. internal/store/sqlite's implementation answers the
// identical suite at L3 in this same PR — design D6's "answered twice" rule,
// which is what stops the fake and the real store from drifting while one
// lags behind the other.
func TestRelationRepo_MemRepo(t *testing.T) {
	repocontract.RunRelationRepo(t, func(t *testing.T) repocontract.RelationHarness {
		t.Helper()
		return memrepo.NewRelations()
	})
}
