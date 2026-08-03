// Package conformance — see test/conformance/doc.go for the package contract.
package conformance

import (
	"testing"

	"github.com/rengo/nooma/internal/ports"
	"github.com/rengo/nooma/test/support/memrepo"
	"github.com/rengo/nooma/test/support/repocontract"
)

// TestSignalRepo_MemRepo runs repocontract.RunSignalRepo against the
// in-memory fake, at L2. internal/store/sqlite's implementation answers
// the identical suite at L3 in the same PR (12e.2) — design D6's "answered
// twice" rule, which is what stops the fake and the real store from
// drifting while one lags behind the other.
func TestSignalRepo_MemRepo(t *testing.T) {
	repocontract.RunSignalRepo(t, func(t *testing.T) ports.SignalRepo {
		t.Helper()
		return memrepo.NewSignals()
	})
}
