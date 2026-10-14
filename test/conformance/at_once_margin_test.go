// Package conformance — see test/conformance/doc.go for the package contract.
package conformance

import (
	"testing"
	"time"

	"github.com/rengo/nooma/internal/core/prospection"
	"github.com/rengo/nooma/internal/scheduler"
)

// TestAtOnceMarginMatchesProactiveInterval pins ADR-0031 point 2: an asked
// reminder is armed at once only when more than one check interval remains,
// because a nearer instant is gone before the next pass could send it. Core
// may not import the scheduler, so the relation lives here (L2).
func TestAtOnceMarginMatchesProactiveInterval(t *testing.T) {
	if got := time.Duration(prospection.AtOnceMarginMinutes) * time.Minute; got != scheduler.ProactiveCheckInterval {
		t.Fatalf("AtOnceMarginMinutes = %v, ProactiveCheckInterval = %v: the margin is one check interval", got, scheduler.ProactiveCheckInterval)
	}
}
