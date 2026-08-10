// Package conformance — see test/conformance/doc.go for the package contract.
package conformance

import (
	"testing"

	"github.com/rengo/nooma/test/support/memrepo"
	"github.com/rengo/nooma/test/support/repocontract"
)

// TestConfigRepo_MemRepo runs repocontract.RunConfigRepoLoad against the
// in-memory fake, at L2. internal/store/sqlite has no implementation of
// ports.ConfigRepo until PR 6 (feat/store-selfmodel-config-state) —
// design D6's "answered twice" rule applies there, not here.
func TestConfigRepo_MemRepo(t *testing.T) {
	repocontract.RunConfigRepoLoad(t, func(t *testing.T) repocontract.ConfigHarness {
		t.Helper()
		return memrepo.NewConfig()
	})
}

// TestConfigRepo_MemRepo_RecordConsolidationRun runs
// repocontract.RunRecordConsolidationRun — spec R2.6's contract suite —
// against the same fake.
func TestConfigRepo_MemRepo_RecordConsolidationRun(t *testing.T) {
	repocontract.RunRecordConsolidationRun(t, func(t *testing.T) repocontract.ConfigHarness {
		t.Helper()
		return memrepo.NewConfig()
	})
}
