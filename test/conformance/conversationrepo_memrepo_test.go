// Package conformance — see test/conformance/doc.go for the package contract.
package conformance

import (
	"testing"

	"github.com/rengo/nooma/internal/ports"
	"github.com/rengo/nooma/test/support/memrepo"
	"github.com/rengo/nooma/test/support/repocontract"
)

// TestConversationRepo_MemRepo runs repocontract.RunConversationRepo against
// the in-memory fake, at L2; internal/store/sqlite runs it against the vault.
func TestConversationRepo_MemRepo(t *testing.T) {
	repocontract.RunConversationRepo(t, func(t *testing.T) ports.ConversationRepo {
		t.Helper()
		return memrepo.NewConversations()
	})
}
