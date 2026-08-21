// Package conformance — see test/conformance/doc.go for the package contract.
package conformance

import (
	"testing"

	"github.com/rengo/nooma/internal/ports"
	"github.com/rengo/nooma/test/support/memrepo"
	"github.com/rengo/nooma/test/support/repocontract"
)

// TestTimerRepo_MemRepo runs repocontract.RunTimerRepo against the
// in-memory fake, at L2 — see TestTriggerRepo_MemRepo's own note.
func TestTimerRepo_MemRepo(t *testing.T) {
	repocontract.RunTimerRepo(t, func(t *testing.T) ports.TimerRepo {
		t.Helper()
		return memrepo.NewTimers()
	})
}
