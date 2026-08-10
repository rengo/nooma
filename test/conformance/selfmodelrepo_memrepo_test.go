// Package conformance — see test/conformance/doc.go for the package contract.
package conformance

import (
	"testing"

	"github.com/rengo/nooma/internal/ports"
	"github.com/rengo/nooma/test/support/memrepo"
	"github.com/rengo/nooma/test/support/repocontract"
)

// TestSelfModelRepo_MemRepo runs repocontract.RunActiveBeliefs against the
// in-memory fake, at L2. internal/store/sqlite has no implementation of
// ports.SelfModelRepo until PR 6 (feat/store-selfmodel-config-state) —
// design D6's "answered twice" rule applies there, not here.
func TestSelfModelRepo_MemRepo(t *testing.T) {
	repocontract.RunActiveBeliefs(t, func(t *testing.T) ports.SelfModelRepo {
		t.Helper()
		return memrepo.NewSelfModel()
	})
}

// TestSelfModelRepo_MemRepo_UpsertByTopicKey runs
// repocontract.RunUpsertByTopicKey — spec R2.1's contract suite — against
// the same fake.
func TestSelfModelRepo_MemRepo_UpsertByTopicKey(t *testing.T) {
	repocontract.RunUpsertByTopicKey(t, func(t *testing.T) ports.SelfModelRepo {
		t.Helper()
		return memrepo.NewSelfModel()
	})
}

// TestSelfModelRepo_MemRepo_ReinforceByID runs
// repocontract.RunReinforceByID — spec R2.2's contract suite — against the
// same fake.
func TestSelfModelRepo_MemRepo_ReinforceByID(t *testing.T) {
	repocontract.RunReinforceByID(t, func(t *testing.T) ports.SelfModelRepo {
		t.Helper()
		return memrepo.NewSelfModel()
	})
}
