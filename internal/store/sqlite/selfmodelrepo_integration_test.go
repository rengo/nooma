//go:build integration

package sqlite

import (
	"testing"

	"github.com/rengo/nooma/internal/ports"
	"github.com/rengo/nooma/test/support/repocontract"
)

// TestSelfModelRepo_ActiveBeliefs, TestSelfModelRepo_UpsertByTopicKey and
// TestSelfModelRepo_ReinforceByID run the same repocontract suites
// test/support/memrepo/selfmodel.go already answers at L2 (PR 3), now
// against a real temporary SQLite vault at L3 — design D6's "answered
// twice" standing rule.
func TestSelfModelRepo_ActiveBeliefs(t *testing.T) {
	repocontract.RunActiveBeliefs(t, func(t *testing.T) ports.SelfModelRepo {
		return NewSelfModelRepo(openTestVault(t))
	})
}

func TestSelfModelRepo_UpsertByTopicKey(t *testing.T) {
	repocontract.RunUpsertByTopicKey(t, func(t *testing.T) ports.SelfModelRepo {
		return NewSelfModelRepo(openTestVault(t))
	})
}

func TestSelfModelRepo_ReinforceByID(t *testing.T) {
	repocontract.RunReinforceByID(t, func(t *testing.T) ports.SelfModelRepo {
		return NewSelfModelRepo(openTestVault(t))
	})
}
