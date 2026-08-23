// Package fakechannel is the in-memory ports.Channel test double — the red
// step's version: every method is a no-op returning a zero value.
package fakechannel

import (
	"context"
	"testing"

	"github.com/rengo/nooma/internal/ports"
	"github.com/rengo/nooma/test/support/repocontract"
)

// Fake is an in-memory ports.Channel.
type Fake struct{}

var _ ports.Channel = (*Fake)(nil)

// New returns the red step's no-op fake.
func New() *Fake { return &Fake{} }

// Name implements ports.Channel.
func (f *Fake) Name() string { return "" }

// Receive implements ports.Channel.
func (f *Fake) Receive(_ context.Context) ([]ports.ChannelMessage, error) { return nil, nil }

// Confirm implements ports.Channel.
func (f *Fake) Confirm(_ context.Context, _ string) error { return nil }

// Send implements ports.Channel.
func (f *Fake) Send(_ context.Context, _ ports.ConversationID, _ string) error { return nil }

// Close implements ports.Channel.
func (f *Fake) Close() error { return nil }

// Deliver implements repocontract.ChannelHarness.
func (f *Fake) Deliver(_ *testing.T, _, _, _ string) {}

// Sent implements repocontract.ChannelHarness.
func (f *Fake) Sent(_ *testing.T) []repocontract.SentMessage { return nil }
