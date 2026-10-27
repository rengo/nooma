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
	pending, err := r.timers.Pending(ctx)
	if err != nil {
		return nil, fmt.Errorf("capture: reading the live timers: %w", err)
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
	if err := r.timers.Cancel(ctx, named.ID); err != nil {
		// It rang, or was cancelled, after the list was read.
		if errors.Is(err, ports.ErrTimerStatusConflict) || errors.Is(err, ports.ErrTimerNotFound) {
			answer.Gone = true
			return result, true, nil
		}
		return CaptureResult{}, false, fmt.Errorf("capture: cancel timer %q: %w", named.ID, err)
	}
	contextJSON, err := marshalContext(struct {
		TimerID  string `json:"timer_id"`
		FireAt   string `json:"fire_at"`
		Position int    `json:"position"`
		Listed   int    `json:"listed"`
	}{named.ID, named.FireAt.UTC().Format(time.RFC3339), target.Position, len(shown)})
	if err != nil {
		return CaptureResult{}, false, fmt.Errorf("capture: encode timer cancel context: %w", err)
	}
	if err := r.log.Record(ctx, ports.Decision{
		ID: r.ids.New(), Action: ports.ActionCaptureTimerCancelled, Context: contextJSON, OccurredAt: now,
		Rationale: fmt.Sprintf("timer %q, listed at position %d of %d, was cancelled from chat", named.ID, target.Position, len(shown)),
	}); err != nil {
		return CaptureResult{}, false, fmt.Errorf("capture: record timer cancel: %w", err)
	}
	answer.Cancelled = &named
	return result, true, nil
}
