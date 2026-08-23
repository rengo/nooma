package channels

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/rengo/nooma/internal/brain"
	"github.com/rengo/nooma/internal/ports"
)

// Handler is what one admitted message is handed to. Returning an error
// means the message was NOT handled durably, and the runner will neither
// confirm nor remember it — so the transport delivers it again.
//
// A function rather than an interface, and taking no dependency on
// internal/brain, so this loop stays what it claims to be: transport- AND
// consumer-independent. Design §3.6's own sketch calls CaptureService
// inline; passing a Handler instead keeps internal/channels free of the
// brain, which is the same boundary internal/ports keeps one layer down.
type Handler func(ctx context.Context, msg ports.ChannelMessage) error

// Runner drives one channel: receive, skip what was already handled, hand
// the rest to the Handler, confirm what stuck.
type Runner struct {
	ch      ports.Channel
	handle  Handler
	log     io.Writer
	logMu   sync.Mutex
	seen    *dedupRing
	sleepFn func(ctx context.Context, d time.Duration)
}

// NewRunner returns a Runner over ch.
func NewRunner(ch ports.Channel, handle Handler, log io.Writer) *Runner {
	if log == nil {
		log = io.Discard
	}
	return &Runner{ch: ch, handle: handle, log: log, seen: newDedupRing(dedupWindow), sleepFn: sleepCtx}
}

// permanentFailure is what a channel returns when retrying cannot help.
// The runner asks the error, rather than knowing which transports have
// which codes: a second channel's permanent failure is its own to name.
type permanentFailure interface{ Unauthorized() bool }

// Run polls until ctx is done, or until the channel reports a failure that
// retrying cannot fix.
//
// It returns nil on a clean shutdown and an error on a permanent failure,
// because those are different things to a caller: one is the process
// stopping and the other is a channel that will never work again with the
// credentials it has.
func (r *Runner) Run(ctx context.Context) error {
	failures := 0

	for {
		if ctx.Err() != nil {
			return nil
		}

		msgs, err := r.ch.Receive(ctx)
		switch {
		case err == nil:
			failures = 0
		case ctx.Err() != nil:
			// Cancellation reaching an in-flight poll surfaces as a
			// transport error. It is shutdown, not a failure to back off
			// from — treating it as one would make every clean stop log a
			// spurious error.
			return nil
		case isPermanent(err):
			// A wrong or revoked credential never becomes right by
			// waiting. A channel that retried it forever would look alive
			// while being permanently deaf, which is the failure the
			// "does not start" posture exists to avoid, one layer later.
			return fmt.Errorf("channels: %s stopped: %w", r.ch.Name(), err)
		default:
			failures++
			r.logf("channels: %s: poll failed (%d consecutive): %v", r.ch.Name(), failures, err)
			r.sleepFn(ctx, backoffForFailures(failures))
			continue
		}

		r.handleBatch(ctx, msgs)
	}
}

// handleBatch runs one batch, stopping at the first message that did not
// stick.
//
// **A handler error breaks the batch rather than skipping the message**,
// and that is deliberate: whatever failed is likely to fail for the next
// message too — a closed vault, a dead provider — and confirming past a
// failure is how the "never lose a capture" rule gets violated one message
// at a time.
func (r *Runner) handleBatch(ctx context.Context, msgs []ports.ChannelMessage) {
	for _, msg := range msgs {
		if r.seen.seen(msg.ID) {
			// A redelivery of something already handled. The transport
			// resends whatever was not confirmed, and a confirm can fail
			// after the work succeeded — so this is the ordinary case, not
			// an anomaly.
			continue
		}

		if err := r.handle(ctx, msg); err != nil {
			r.logf("channels: %s: handling %s failed, not confirmed: %v", r.ch.Name(), msg.ID, err)
			return
		}

		// Marked BEFORE the confirm, because the confirm is what can fail
		// after the work is already durable. Marking after would leave a
		// window where a redelivery re-does work that succeeded.
		r.seen.mark(msg.ID)

		if err := r.ch.Confirm(ctx, msg.ID); err != nil {
			r.logf("channels: %s: confirming %s failed: %v", r.ch.Name(), msg.ID, err)
			return
		}
	}
}

func isPermanent(err error) bool {
	var p permanentFailure
	return errors.As(err, &p) && p.Unauthorized()
}

// backoffForFailures is the runner's own copy of the curve's shape: the
// first failure waits the base, and each one after doubles to a ceiling.
// The transport owns its own numbers; this owns the sequence.
func backoffForFailures(n int) time.Duration {
	wait := runnerBackoffBase
	for i := 1; i < n; i++ {
		if wait >= runnerBackoffMax/2 {
			return runnerBackoffMax
		}
		wait *= 2
	}
	if wait > runnerBackoffMax {
		return runnerBackoffMax
	}
	return wait
}

const (
	runnerBackoffBase = time.Second
	runnerBackoffMax  = 5 * time.Minute
)

// sleepCtx waits d, or returns early if ctx is done — which is what makes
// shutdown prompt even mid-backoff. A bare time.Sleep here would hold a
// stopping process for up to the ceiling.
func sleepCtx(ctx context.Context, d time.Duration) {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
	case <-t.C:
	}
}

func (r *Runner) logf(format string, args ...any) {
	r.logMu.Lock()
	defer r.logMu.Unlock()
	_, _ = fmt.Fprintf(r.log, format+"\n", args...)
}

// Capturer is the half of brain.CaptureService this runner uses.
//
// An interface rather than the concrete type, and it is deliberately one
// method wide: internal/channels depends on "text goes in, a result comes
// out" and on nothing else a CaptureService can do. It also lets the
// handler's own tests run with no vault, no provider and no index.
type Capturer interface {
	Capture(ctx context.Context, in brain.CaptureInput) (brain.CaptureResult, error)
}

// CaptureHandler builds the Handler that turns an admitted message into a
// capture and answers it.
//
// **The order is capture, reply, and then the caller confirms** — and a
// failed reply does not stop the confirm, which is why this returns nil
// after logging one. The capture is the durable thing; the reply is not.
// Re-running the capture to retry a reply would duplicate the unit,
// trading an unrecoverable loss for a recoverable one, backwards.
//
// Only a capture error is returned, because only a capture error means the
// message was not handled.
func CaptureHandler(capture Capturer, ch ports.Channel, log io.Writer) Handler {
	if log == nil {
		log = io.Discard
	}
	var mu sync.Mutex

	return func(ctx context.Context, msg ports.ChannelMessage) error {
		result, err := capture.Capture(ctx, brain.CaptureInput{
			Text: msg.Text,
			// The channel's own name, carried in from the adapter rather
			// than hardcoded here: provenance is the caller's fact.
			Channel: msg.Channel,
		})
		if err != nil {
			return fmt.Errorf("capturing %s: %w", msg.ID, err)
		}

		reply := RenderReply(result)
		if reply == "" {
			// An outcome with no rendering. The totality test exists to
			// stop this reaching production; if it ever does, saying
			// nothing is worse than saying something plain.
			reply = "Done."
		}

		if err := ch.Send(ctx, msg.Conversation, reply); err != nil {
			mu.Lock()
			_, _ = fmt.Fprintf(log, "channels: %s: replying to %s failed, the capture stands: %v\n", ch.Name(), msg.ID, err)
			mu.Unlock()
		}
		return nil
	}
}
