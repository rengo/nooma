// Package conformance — see test/conformance/doc.go for the package contract.
package conformance

import (
	"testing"

	"github.com/rengo/nooma/test/support/memrepo"
	"github.com/rengo/nooma/test/support/repocontract"
)

// TestTriggerRepo_MemRepo runs repocontract.RunTriggerRepo against the
// in-memory fake, at L2. internal/store/sqlite answers the identical suite
// at L3 in the next PR (feat/store-trigger-timer) — design D6's "answered
// twice" rule.
func TestTriggerRepo_MemRepo(t *testing.T) {
	repocontract.RunTriggerRepo(t, func(t *testing.T) repocontract.TriggerHarness {
		t.Helper()
		return memrepo.NewTriggers()
	})
}

// TestTriggerDelivery_MemRepo runs repocontract.RunTriggerDelivery against
// the in-memory fake, at L2.
func TestTriggerDelivery_MemRepo(t *testing.T) {
	repocontract.RunTriggerDelivery(t, func(t *testing.T) repocontract.TriggerHarness {
		t.Helper()
		return memrepo.NewTriggers()
	})
}
