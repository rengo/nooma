// Package fakechannel is the in-memory ports.Channel test double, for L2
// use. It ships no HTTP, no token, no vendor and no production import path
// — the established test/support precedent.
//
// It is not a second implementation of Telegram, and the difference
// matters: it holds a slice of pending messages and a slice of sent
// replies, and nothing else. If a contract case cannot be written against
// it, that case belongs to an adapter's own L3 suite rather than to the
// port — which is the line that keeps the port from quietly becoming
// Telegram's shape with the name filed off.
package fakechannel

import (
	"context"
	"sync"
	"testing"

	"github.com/rengo/nooma/internal/ports"
	"github.com/rengo/nooma/test/support/repocontract"
)

// Name is the fake's channel name. A real word rather than "fake", because
// it becomes units.source in any test that captures through this channel,
// and a source column reading "fake" in a fixture is a fixture that will
// eventually be read as production data.
const Name = "memory"

// Fake is an in-memory ports.Channel. The zero value is not usable — call
// New. Two instances share no state.
type Fake struct {
	mu sync.Mutex
	// pending holds every delivered-but-unconfirmed message, in delivery
	// order. Confirm removes through the named id; Receive returns what is
	// left, which is how "an unconfirmed message comes back" holds without
	// the fake tracking a cursor of its own.
	pending []ports.ChannelMessage
	sent    []repocontract.SentMessage
	closed  bool
}

var _ ports.Channel = (*Fake)(nil)

// New returns an empty, ready-to-use in-memory ports.Channel.
func New() *Fake { return &Fake{} }

// Name implements ports.Channel.
func (f *Fake) Name() string { return Name }

// Receive implements ports.Channel. It returns immediately rather than
// blocking: a long-polling transport blocks for its own timeout and both
// satisfy the contract, which is why RunChannel asserts no timing.
func (f *Fake) Receive(_ context.Context) ([]ports.ChannelMessage, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	out := make([]ports.ChannelMessage, len(f.pending))
	copy(out, f.pending)
	return out, nil
}

// Confirm implements ports.Channel. Every message up to and including id
// is dropped, matching the port's "up to and including" wording rather
// than only the named one.
func (f *Fake) Confirm(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	for i, msg := range f.pending {
		if msg.ID == id {
			f.pending = f.pending[i+1:]
			return nil
		}
	}
	// An id that is not pending is already confirmed. Silently fine: a
	// caller confirming twice is a caller that restarted, not a caller
	// with a bug.
	return nil
}

// Send implements ports.Channel.
func (f *Fake) Send(_ context.Context, conversation ports.ConversationID, text string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.sent = append(f.sent, repocontract.SentMessage{Conversation: conversation, Text: text})
	return nil
}

// Close implements ports.Channel.
func (f *Fake) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.closed = true
	return nil
}

// Deliver implements repocontract.ChannelHarness: it makes one message
// available to the next Receive.
func (f *Fake) Deliver(_ *testing.T, id, conversation, text string) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.pending = append(f.pending, ports.ChannelMessage{
		ID:           id,
		Conversation: ports.ConversationID(conversation),
		Text:         text,
		Channel:      Name,
	})
}

// Sent implements repocontract.ChannelHarness.
func (f *Fake) Sent(_ *testing.T) []repocontract.SentMessage {
	f.mu.Lock()
	defer f.mu.Unlock()

	out := make([]repocontract.SentMessage, len(f.sent))
	copy(out, f.sent)
	return out
}

// Closed reports whether Close has been called — test-only, for a caller
// asserting shutdown reached the channel.
func (f *Fake) Closed() bool {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.closed
}
