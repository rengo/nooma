// Package conformance — see test/conformance/doc.go for the package contract.
package conformance

import (
	"testing"

	"github.com/rengo/nooma/test/support/fakechannel"
	"github.com/rengo/nooma/test/support/repocontract"
)

// TestChannel_Fake runs repocontract.RunChannel against the in-memory fake,
// at L2. internal/channels/telegram answers the identical suite at L3 in a
// later PR of this chain — design D6's "answered twice" standing rule.
//
// The fake is the whole argument for a port here. A channel interface with
// exactly one implementation is a layer of indirection, not a boundary;
// what makes it a boundary is that the contract can be satisfied by
// something with no network, no token and no vendor at all.
func TestChannel_Fake(t *testing.T) {
	repocontract.RunChannel(t, func(t *testing.T) repocontract.ChannelHarness {
		t.Helper()
		return fakechannel.New()
	})
}
