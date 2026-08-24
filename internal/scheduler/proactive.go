package scheduler

import (
	"context"
	"time"
)

// ProactiveCheckInterval is how often the proactive pass runs.
//
// Five minutes, which is `docs/01-architecture.md:227`'s own `*/5 * * * *`
// expressed as a duration. **It is a constant here rather than a parsed
// cron expression, and that is the existing shape rather than a new
// decision**: nothing in this repository parses a cron expression at all.
// `ConsolidationHour` is a constant beside it for the same reason, and
// `schedules.consolidate` has been a decoded-and-unread config key since
// M0 (finding J5).
//
// The number is bounded from both sides. Longer, and an item deferred out
// of quiet hours waits past 07:00 by up to that long, every morning.
// Shorter, and a personal vault does arithmetic it has no reason to do —
// the pass reads two tables and usually decides nothing.
const ProactiveCheckInterval = 5 * time.Minute

// ProactiveChecker is the one call the proactive job makes. An interface
// so the scheduler needs no import of internal/brain, exactly as
// Consolidator already is.
type ProactiveChecker interface {
	ProactiveCheck(ctx context.Context) error
}

// runProactive is the proactive loop: wait one interval, run one pass,
// forever, until ctx is done.
//
// Simpler than runCron because it has no daily instant to compute — the
// cadence is an interval, not an hour, so there is no NextDailyRun and no
// clock read here at all. The pass reads its own instant, once, which is
// what brain_single_clock_read_test.go requires of it.
func (s *Scheduler) runProactive(ctx context.Context) {
	for {
		select {
		case <-s.timer.After(ProactiveCheckInterval):
			s.runProactivePass(ctx)
		case <-ctx.Done():
			return
		}
	}
}

// runProactivePass runs one proactive pass under its own guard.
//
// **Its own slot, not the consolidation job's.** The two run at wildly
// different cadences: consolidation is nightly and can take minutes, this
// is every five. Sharing one slot would let a single long nightly pass
// suppress every check it overlaps — which is the early morning, exactly
// when the items deferred through quiet hours are waiting to resurface.
//
// The slot is released right after the call and before any logging, for
// the reason runPass records at length (JD-6-01): logf holds logMu for the
// duration of a write to a writer that can block indefinitely, and a slot
// held across that would halt the job permanently the day the log stalls.
func (s *Scheduler) runProactivePass(ctx context.Context) {
	if s.proactive == nil {
		return
	}

	select {
	case s.proactiveSlot <- struct{}{}: // acquired
	default:
		s.logf("scheduler: proactive fire skipped, a pass is already running")
		return
	}

	err := s.proactive.ProactiveCheck(ctx)
	<-s.proactiveSlot

	if err != nil {
		// Logged, never fatal: the next tick still fires. A pass that
		// failed is not a scheduler that should stop, and five minutes
		// from now the vault may well be readable again.
		s.logf("scheduler: proactive pass failed: %v", err)
	}
}
