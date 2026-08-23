package channels

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/rengo/nooma/internal/brain"
	"github.com/rengo/nooma/internal/ports"
)

// stubCapturer answers with a scripted result, and records what it was
// asked.
type stubCapturer struct {
	result brain.CaptureResult
	err    error
	seen   []brain.CaptureInput
}

func (c *stubCapturer) Capture(_ context.Context, in brain.CaptureInput) (brain.CaptureResult, error) {
	c.seen = append(c.seen, in)
	return c.result, c.err
}

// sendingChannel records replies and can be made to fail.
type sendingChannel struct {
	scriptedChannel
	sendErr error
	sent    []string
}

func (c *sendingChannel) Send(_ context.Context, _ ports.ConversationID, text string) error {
	if c.sendErr != nil {
		return c.sendErr
	}
	c.sent = append(c.sent, text)
	return nil
}

// TestCaptureHandler_PassesTheTextAndTheChannelThrough is R5.1's first
// half, and R5.2's: the handler adds nothing the capture did not ask for.
func TestCaptureHandler_PassesTheTextAndTheChannelThrough(t *testing.T) {
	capturer := &stubCapturer{result: brain.CaptureResult{Outcome: brain.OutcomeStored, UnitID: "u-1"}}
	ch := &sendingChannel{}

	handle := CaptureHandler(capturer, ch, nil)
	err := handle(context.Background(), ports.ChannelMessage{
		ID: "1", Conversation: "conv", Text: "pick up the dry cleaning", Channel: "telegram",
	})
	if err != nil {
		t.Fatalf("handle: %v", err)
	}

	if len(capturer.seen) != 1 {
		t.Fatalf("Capture was called %d time(s), want 1", len(capturer.seen))
	}
	if got := capturer.seen[0].Text; got != "pick up the dry cleaning" {
		t.Errorf("Text = %q, want it verbatim — nothing between the person and classify rewrites what they typed", got)
	}
	if got := capturer.seen[0].Channel; got != "telegram" {
		t.Errorf("Channel = %q, want the channel's own name %q — provenance is the caller's fact", got, "telegram")
	}
	if len(ch.sent) != 1 || ch.sent[0] == "" {
		t.Fatalf("replies sent = %v, want exactly one non-empty", ch.sent)
	}
}

// TestCaptureHandler_ACaptureErrorIsReturnedSoNothingIsConfirmed is R4.1
// at the handler's edge: only a capture failure means the message was not
// handled.
func TestCaptureHandler_ACaptureErrorIsReturnedSoNothingIsConfirmed(t *testing.T) {
	capturer := &stubCapturer{err: errors.New("the vault is closed")}
	ch := &sendingChannel{}

	err := CaptureHandler(capturer, ch, nil)(context.Background(), ports.ChannelMessage{ID: "1"})
	if err == nil {
		t.Fatal("handle returned nil for a failed capture — the runner would confirm it and the message would be gone")
	}
	if len(ch.sent) != 0 {
		t.Errorf("a failed capture still replied %v — the person would be told their note was kept", ch.sent)
	}
}

// TestCaptureHandler_AFailedReplyDoesNotUndoTheCapture is owner item R3,
// and it is the ordering that is easy to get backwards.
//
// The capture is durable and the reply is not. Returning an error here
// would leave the message unconfirmed, so the transport would redeliver it
// and the capture would run again — duplicating the unit to retry a
// sentence. That trades an unrecoverable loss for a recoverable one,
// backwards.
func TestCaptureHandler_AFailedReplyDoesNotUndoTheCapture(t *testing.T) {
	capturer := &stubCapturer{result: brain.CaptureResult{Outcome: brain.OutcomeStored, UnitID: "u-1"}}
	ch := &sendingChannel{sendErr: errors.New("telegram is down")}
	var log bytes.Buffer

	err := CaptureHandler(capturer, ch, &log)(context.Background(), ports.ChannelMessage{ID: "1"})
	if err != nil {
		t.Fatalf("handle returned %v for a failed reply — the message would be redelivered and captured twice", err)
	}
	if !strings.Contains(log.String(), "the capture stands") {
		t.Errorf("the log does not say the capture survived the failed reply:\n%s", log.String())
	}
}
