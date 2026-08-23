package channels

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/ports"
)

// scriptedChannel returns a scripted result per Receive call, then blocks
// on ctx so the loop does not spin past the script.
type scriptedChannel struct {
	mu        sync.Mutex
	script    []scriptStep
	call      int
	confirmed []string

	// drained closes once the script is exhausted, so a test that expects
	// the loop to keep running can stop it deterministically instead of
	// waiting out a timeout.
	drainOnce sync.Once
	drained   chan struct{}
}

type scriptStep struct {
	msgs []ports.ChannelMessage
	err  error
}

func (c *scriptedChannel) Name() string { return "scripted" }

func (c *scriptedChannel) Receive(ctx context.Context) ([]ports.ChannelMessage, error) {
	c.mu.Lock()
	step := scriptStep{}
	exhausted := c.call >= len(c.script)
	if !exhausted {
		step = c.script[c.call]
	}
	c.call++
	c.mu.Unlock()

	if exhausted {
		c.drainOnce.Do(func() { close(c.drained) })
		<-ctx.Done()
		return nil, ctx.Err()
	}
	return step.msgs, step.err
}

func (c *scriptedChannel) Confirm(_ context.Context, id string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.confirmed = append(c.confirmed, id)
	return nil
}

func (c *scriptedChannel) Send(context.Context, ports.ConversationID, string) error { return nil }
func (c *scriptedChannel) Close() error                                             { return nil }

func (c *scriptedChannel) confirmations() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]string, len(c.confirmed))
	copy(out, c.confirmed)
	return out
}

// permanentErr is a transport's own permanent failure, shaped the way the
// runner asks about it — by behaviour, not by importing a vendor's type.
type permanentErr struct{}

func (permanentErr) Error() string      { return "unauthorized" }
func (permanentErr) Unauthorized() bool { return true }

// newScripted builds a scriptedChannel with its drained channel ready.
func newScripted(steps ...scriptStep) *scriptedChannel {
	return &scriptedChannel{script: steps, drained: make(chan struct{})}
}

func msg(id string) ports.ChannelMessage {
	return ports.ChannelMessage{ID: id, Conversation: "conv", Text: "text " + id, Channel: "scripted"}
}

// newTestRunner builds a Runner whose sleeps are recorded instead of slept,
// so no test waits on a real backoff.
func newTestRunner(t *testing.T, ch ports.Channel, handle Handler) (*Runner, *[]time.Duration) {
	t.Helper()

	var mu sync.Mutex
	slept := []time.Duration{}
	r := NewRunner(ch, handle, nil)
	r.sleepFn = func(_ context.Context, d time.Duration) {
		mu.Lock()
		defer mu.Unlock()
		slept = append(slept, d)
	}
	return r, &slept
}

// runUntilStopped runs r until it returns on its own. Used only where the
// loop is expected to stop by itself; the bound is an assertion, not a
// safety net — a loop that should stop and does not fails this test rather
// than hanging it.
func runUntilStopped(t *testing.T, r *Runner, bound time.Duration) error {
	t.Helper()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- r.Run(ctx) }()

	select {
	case err := <-done:
		return err
	case <-time.After(bound):
		cancel()
		t.Fatalf("Run did not return within %s", bound)
		return nil
	}
}

// runUntilDrained runs r until ch's script is exhausted, then stops it.
// Used where the loop is expected to keep polling — which is the ordinary
// case, and the one where waiting for a return would mean waiting out a
// timeout.
func runUntilDrained(t *testing.T, r *Runner, ch *scriptedChannel) {
	t.Helper()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- r.Run(ctx) }()

	select {
	case <-ch.drained:
	case <-time.After(2 * time.Second):
		cancel()
		t.Fatal("the script was never exhausted — the loop stopped early")
	}
	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return within 2s of cancellation")
	}
}

// TestRunner_PermanentFailureStopsTheLoop is R4.2's asymmetry, and owner
// item R5.
func TestRunner_PermanentFailureStopsTheLoop(t *testing.T) {
	ch := newScripted(scriptStep{err: permanentErr{}})
	r, slept := newTestRunner(t, ch, func(context.Context, ports.ChannelMessage) error { return nil })

	err := runUntilStopped(t, r, 2*time.Second)
	if err == nil {
		t.Fatal("Run returned nil for a permanent failure — a channel that retries a wrong credential forever looks alive while being permanently deaf")
	}
	if len(*slept) != 0 {
		t.Errorf("the runner backed off %v before stopping — a permanent failure is not a transient one", *slept)
	}
}

// TestRunner_TransientFailureBacksOffAndRecovers: the counter resets on
// the first success, so one bad minute does not leave the channel slow for
// the rest of the day.
func TestRunner_TransientFailureBacksOffAndRecovers(t *testing.T) {
	ch := newScripted(
		scriptStep{err: errors.New("connection reset")},
		scriptStep{err: errors.New("connection reset")},
		scriptStep{msgs: []ports.ChannelMessage{msg("1")}},
		scriptStep{err: errors.New("connection reset")},
	)
	r, slept := newTestRunner(t, ch, func(context.Context, ports.ChannelMessage) error { return nil })

	runUntilDrained(t, r, ch)

	if len(*slept) != 3 {
		t.Fatalf("backed off %d time(s) %v, want 3", len(*slept), *slept)
	}
	if (*slept)[1] <= (*slept)[0] {
		t.Errorf("the second backoff %s is not longer than the first %s", (*slept)[1], (*slept)[0])
	}
	if (*slept)[2] != (*slept)[0] {
		t.Errorf("after a success the backoff is %s, want it reset to %s — one bad minute must not leave the channel slow all day", (*slept)[2], (*slept)[0])
	}
}

// TestRunner_ConfirmsOnlyWhatTheHandlerKept is R4.1 at the loop's level,
// and the assertion is the confirmations the channel actually received.
func TestRunner_ConfirmsOnlyWhatTheHandlerKept(t *testing.T) {
	ch := newScripted(scriptStep{msgs: []ports.ChannelMessage{msg("1"), msg("2"), msg("3")}})

	r, _ := newTestRunner(t, ch, func(_ context.Context, m ports.ChannelMessage) error {
		if m.ID == "2" {
			return errors.New("the vault is closed")
		}
		return nil
	})
	runUntilDrained(t, r, ch)

	got := ch.confirmations()
	if len(got) != 1 || got[0] != "1" {
		t.Fatalf("confirmed %v, want only [1] — a handler error must break the batch, because confirming past a failure loses a capture one message at a time", got)
	}
}

// TestRunner_SkipsARedeliveredMessage is the owner's Q1 ruling in the
// loop: the transport resends what was not confirmed, and a confirm can
// fail after the work is durable, so a redelivery is ordinary.
func TestRunner_SkipsARedeliveredMessage(t *testing.T) {
	ch := newScripted(
		scriptStep{msgs: []ports.ChannelMessage{msg("1")}},
		scriptStep{msgs: []ports.ChannelMessage{msg("1")}},
	)

	var handled int
	var mu sync.Mutex
	r, _ := newTestRunner(t, ch, func(context.Context, ports.ChannelMessage) error {
		mu.Lock()
		defer mu.Unlock()
		handled++
		return nil
	})
	runUntilDrained(t, r, ch)

	mu.Lock()
	defer mu.Unlock()
	if handled != 1 {
		t.Fatalf("the handler ran %d times for one message id, want 1 — a redelivery must not become a second unit", handled)
	}
}

// TestRunner_ShutdownIsPromptAndCleanlyReported: a cancelled context is
// shutdown, not a transport failure. Reporting it as one would make every
// clean stop log a spurious error and back off on the way out.
func TestRunner_ShutdownIsPromptAndCleanlyReported(t *testing.T) {
	ch := newScripted()
	r, slept := newTestRunner(t, ch, func(context.Context, ports.ChannelMessage) error { return nil })

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- r.Run(ctx) }()

	time.Sleep(20 * time.Millisecond) // let the loop reach its blocking Receive
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run returned %v on a cancelled context, want nil — shutdown is not a failure", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return within 2s of cancellation")
	}

	if len(*slept) != 0 {
		t.Errorf("the runner backed off %v on the way out", *slept)
	}
}
