// Package conformance — see test/conformance/doc.go for the package contract.
package conformance

import (
	"testing"

	"github.com/rengo/nooma/test/support/memrepo"
	"github.com/rengo/nooma/test/support/repocontract"
)

// TestLexicalSearch_MemRepo runs repocontract.RunLexicalSearch against the
// in-memory fake, at L2. internal/store/sqlite's FTS5 implementation answers
// the identical suite at L3 in PR 9c — design D6's "answered twice" rule.
func TestLexicalSearch_MemRepo(t *testing.T) {
	repocontract.RunLexicalSearch(t, func(t *testing.T) repocontract.LexicalSeeder {
		t.Helper()
		return memrepo.NewLexical()
	})
}
