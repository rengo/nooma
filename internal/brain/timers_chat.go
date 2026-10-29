package brain

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/rengo/nooma/internal/core/classify"
	"github.com/rengo/nooma/internal/ports"
)

// liveTimers reads the pending timers a chat lists and cancels from (doc 02
// §8, ADR-0051), soonest first, each read in now's zone. A vault with no timer
// store has none.
func (r captureRunner) liveTimers(ctx context.Context, now time.Time) ([]ListedTimer, error) {
	if r.timers == nil {
		return nil, nil
	}
	live, err := readLiveTimers(ctx, r.timers, now)
	if err != nil {
		return nil, fmt.Errorf("capture: %w", err)
	}
	return live, nil
}

// readLiveTimers is the one read of the live timers, for the chat and the UI:
// every pending timer by fire_at then id, each in now's zone.
func readLiveTimers(ctx context.Context, timers ports.TimerRepo, now time.Time) ([]ListedTimer, error) {
	pending, err := timers.Pending(ctx)
	if err != nil {
		return nil, fmt.Errorf("reading the live timers: %w", err)
	}
	live := make([]ListedTimer, len(pending))
	for i, d := range pending {
		live[i] = ListedTimer{ID: d.ID, FireAt: d.FireAt.In(now.Location()), Text: d.ActionText}
	}
	return live, nil
}

// shownTimers is the part of live the prompt numbers, and so the part a
// cancel's position counts.
func shownTimers(live []ListedTimer) []ListedTimer {
	return live[:min(len(live), classify.MaxReminderChoices)]
}

// timerEntries is what the prompt lists for the shown timers.
func timerEntries(live []ListedTimer) []classify.TimerEntry {
	shown := shownTimers(live)
	entries := make([]classify.TimerEntry, len(shown))
	for i, t := range shown {
		entries[i] = classify.TimerEntry{At: t.FireAt, Text: t.Text}
	}
	return entries
}

// answerTimers answers a reading that asks to list or cancel the live timers,
// whatever its type: the model read the request, and the code lists or
// cancels only a timer it showed (ADR-0033, ADR-0051). It reports false for
// any other reading.
func (r captureRunner) answerTimers(ctx context.Context, c classify.Classification, live []ListedTimer, now time.Time) (CaptureResult, bool, error) {
	if c.TimerRequest == nil || *c.TimerRequest == classify.TimerRequestNone {
		return CaptureResult{}, false, nil
	}
	shown := shownTimers(live)
	answer := &TimersAnswer{Listed: shown, More: len(live) - len(shown)}
	result := CaptureResult{Outcome: OutcomeTimers, Timers: answer}
	if *c.TimerRequest == classify.TimerRequestList || len(live) == 0 {
		return result, true, nil
	}

	// A cancel binds only a position the prompt showed; anything else asks.
	target := c.TimerTarget
	if target == nil || target.Unclear || target.Position > len(shown) {
		answer.Which = true
		return result, true, nil
	}
	named := shown[target.Position-1]
	answer.Listed, answer.More = nil, 0
	err := cancelListedTimer(ctx, r.timers, r.log, r.ids, named, target.Position, len(shown), timerSurfaceChat, now)
	switch {
	case errors.Is(err, ErrTimerNoLongerSet):
		answer.Gone = true
		return result, true, nil
	case err != nil:
		return CaptureResult{}, false, fmt.Errorf("capture: %w", err)
	}
	answer.Cancelled = &named
	return result, true, nil
}

// ErrTimerNoLongerSet is a cancel of a timer that rang, or was cancelled,
// after the list it was named in was read — or that was never there.
var ErrTimerNoLongerSet = errors.New("the timer is no longer set")

// timerSurface is where a cancel was asked from, written on its row.
type timerSurface string

const (
	timerSurfaceChat timerSurface = "chat"
	timerSurfaceUI   timerSurface = "ui"
)

// words is how a row's rationale names the surface.
func (s timerSurface) words() string {
	if s == timerSurfaceUI {
		return "from the UI"
	}
	return "from chat"
}

// cancelListedTimer is the one cancel of a timer, from chat and from the UI
// alike (doc 02 §8, ADR-0051): named, at position of the listed timers it was
// shown among, moves pending -> cancelled, never a delete, and writes one
// capture.timer.cancelled row naming it, its instant, its position and the
// surface. It emits no signal (ADR-0052). A timer no longer pending is
// ErrTimerNoLongerSet, and nothing is logged; a cancel whose row could not be
// written is a *WriteLandedError.
func cancelListedTimer(ctx context.Context, timers ports.TimerRepo, log ports.DecisionLog, ids ports.IDGen, named ListedTimer, position, listed int, surface timerSurface, now time.Time) error {
	if err := timers.Cancel(ctx, named.ID); err != nil {
		if errors.Is(err, ports.ErrTimerStatusConflict) || errors.Is(err, ports.ErrTimerNotFound) {
			return ErrTimerNoLongerSet
		}
		return fmt.Errorf("cancel timer %q: %w", named.ID, err)
	}
	contextJSON, err := marshalContext(struct {
		TimerID  string `json:"timer_id"`
		FireAt   string `json:"fire_at"`
		Position int    `json:"position"`
		Listed   int    `json:"listed"`
		Surface  string `json:"surface"`
	}{named.ID, named.FireAt.UTC().Format(time.RFC3339), position, listed, string(surface)})
	if err != nil {
		return fmt.Errorf("encode timer cancel context: %w", err)
	}
	if err := log.Record(ctx, ports.Decision{
		ID: ids.New(), Action: ports.ActionCaptureTimerCancelled, Context: contextJSON, OccurredAt: now,
		Rationale: fmt.Sprintf("timer %q, listed at position %d of %d, was cancelled %s", named.ID, position, listed, surface.words()),
	}); err != nil {
		// The timer is cancelled; only its record is missing.
		return &WriteLandedError{Record: true, Err: fmt.Errorf("record timer cancel: %w", err)}
	}
	return nil
}
