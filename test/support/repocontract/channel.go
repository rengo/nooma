// Package repocontract — see unitrepo.go for the package contract.
package repocontract

import (
	"context"
	"reflect"
	"testing"

	"github.com/rengo/nooma/internal/ports"
)

// SentMessage is one message a Channel posted — what a contract case can
// observe about Send without knowing anything about the transport that
// carried it.
type SentMessage struct {
	Conversation ports.ConversationID
	Text         string
}

// ChannelHarness is what a Channel implementation must offer the contract
// so the suite can put messages in front of it and see what came back.
//
// Deliver takes three plain strings rather than a ports.ChannelMessage on
// purpose: every transport can encode an id, a conversation and a body,
// and a transport-shaped parameter would have made the harness a place
// where the port's own type is constructed by the test rather than by the
// implementation under test. The Telegram harness builds an update
// envelope out of these three; the in-memory fake stores them.
//
// repocontract.EmbeddingHarness and repocontract.TriggerHarness are the
// same shape, for the same reason: a contract that cannot be set up
// against both implementations is not a contract, it is one
// implementation's opinion.
type ChannelHarness interface {
	ports.Channel

	// Deliver makes one message available to the next Receive.
	Deliver(t *testing.T, id, conversation, text string)

	// Sent returns every message Send posted, in order.
	Sent(t *testing.T) []SentMessage
}

// RunChannel runs the ports.Channel contract against a fresh implementation,
// built by newChannel for every subtest.
//
// One thing this suite deliberately does not assert: what Receive does
// while no message is waiting. The in-memory fake returns immediately and
// a long-polling transport blocks for its own timeout — both satisfy "an
// empty slice and a nil error", and pinning the timing here would make the
// contract describe the fake rather than the port.
func RunChannel(t *testing.T, newChannel func(t *testing.T) ChannelHarness) {
	t.Helper()

	t.Run("the port declares no removal method", func(t *testing.T) {
		assertNoRemovalMethod(t, reflect.TypeOf((*ports.Channel)(nil)).Elem())
	})

	t.Run("Name is not empty", func(t *testing.T) {
		ch := newChannel(t)
		if ch.Name() == "" {
			t.Fatal("Name() is empty — it becomes brain.CaptureInput.Channel and therefore units.source, which is NOT NULL")
		}
	})

	t.Run("Receive on a quiet channel returns no messages and no error", func(t *testing.T) {
		ch := newChannel(t)

		// The nil error is the assertion. Quiet is the ordinary state of a
		// conversation, and an implementation that reported it as a
		// failure would drive the runner's backoff on every idle poll.
		msgs, err := ch.Receive(context.Background())
		if err != nil {
			t.Fatalf("Receive on a quiet channel: %v, want nil — quiet is not a failure", err)
		}
		if len(msgs) != 0 {
			t.Fatalf("Receive on a quiet channel returned %d message(s), want 0", len(msgs))
		}
	})

	t.Run("a delivered message comes back with its id, conversation, text and channel", func(t *testing.T) {
		ch := newChannel(t)
		ch.Deliver(t, "msg-1", "conv-1", "pick up the dry cleaning")

		msgs := receive(t, ch)
		if len(msgs) != 1 {
			t.Fatalf("Receive returned %d message(s), want 1", len(msgs))
		}
		got := msgs[0]
		if got.ID != "msg-1" {
			t.Errorf("ID = %q, want %q", got.ID, "msg-1")
		}
		if string(got.Conversation) != "conv-1" {
			t.Errorf("Conversation = %q, want %q", got.Conversation, "conv-1")
		}
		if got.Text != "pick up the dry cleaning" {
			t.Errorf("Text = %q, want it verbatim", got.Text)
		}
		if got.Channel != ch.Name() {
			t.Errorf("Channel = %q, want the channel's own name %q", got.Channel, ch.Name())
		}
	})

	t.Run("a confirmed message is not delivered again", func(t *testing.T) {
		ch := newChannel(t)
		ch.Deliver(t, "msg-confirmed", "conv-1", "first")

		msgs := receive(t, ch)
		if len(msgs) != 1 {
			t.Fatalf("first Receive returned %d message(s), want 1", len(msgs))
		}
		if err := ch.Confirm(context.Background(), msgs[0].ID); err != nil {
			t.Fatalf("Confirm: %v", err)
		}

		if again := receive(t, ch); len(again) != 0 {
			t.Fatalf("Receive after Confirm returned %+v, want nothing — Confirm is the durability boundary", again)
		}
	})

	t.Run("an unconfirmed message is delivered again", func(t *testing.T) {
		ch := newChannel(t)
		ch.Deliver(t, "msg-unconfirmed", "conv-1", "first")

		if msgs := receive(t, ch); len(msgs) != 1 {
			t.Fatalf("first Receive returned %d message(s), want 1", len(msgs))
		}

		// No Confirm. This is the half that keeps a capture from being
		// lost: whatever was not confirmed comes back.
		again := receive(t, ch)
		if len(again) != 1 || again[0].ID != "msg-unconfirmed" {
			t.Fatalf("Receive without Confirm returned %+v, want the same message again", again)
		}
	})

	t.Run("Send records the text against the conversation it was given", func(t *testing.T) {
		ch := newChannel(t)

		if err := ch.Send(context.Background(), ports.ConversationID("conv-7"), "noted"); err != nil {
			t.Fatalf("Send: %v", err)
		}

		sent := ch.Sent(t)
		if len(sent) != 1 {
			t.Fatalf("Sent() returned %d message(s), want 1", len(sent))
		}
		if string(sent[0].Conversation) != "conv-7" {
			t.Errorf("Conversation = %q, want %q", sent[0].Conversation, "conv-7")
		}
		if sent[0].Text != "noted" {
			t.Errorf("Text = %q, want %q", sent[0].Text, "noted")
		}
	})

	t.Run("Send posts in the order it was called", func(t *testing.T) {
		ch := newChannel(t)
		ctx := context.Background()

		for _, text := range []string{"first", "second", "third"} {
			if err := ch.Send(ctx, ports.ConversationID("conv-1"), text); err != nil {
				t.Fatalf("Send %q: %v", text, err)
			}
		}

		sent := ch.Sent(t)
		if len(sent) != 3 {
			t.Fatalf("Sent() returned %d message(s), want 3", len(sent))
		}
		for i, want := range []string{"first", "second", "third"} {
			if sent[i].Text != want {
				t.Fatalf("Sent()[%d] = %q, want %q — a reply out of order reads as a reply to the wrong message", i, sent[i].Text, want)
			}
		}
	})

	t.Run("Close is safe to call and does not error", func(t *testing.T) {
		ch := newChannel(t)
		if err := ch.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}
	})
}

// receive calls Receive and fails the test on an error, so every case above
// reads as the assertion it is making.
func receive(t *testing.T, ch ports.Channel) []ports.ChannelMessage {
	t.Helper()

	msgs, err := ch.Receive(context.Background())
	if err != nil {
		t.Fatalf("Receive: %v", err)
	}
	return msgs
}
