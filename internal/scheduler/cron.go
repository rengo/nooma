package scheduler

import (
	"context"

	"github.com/rengo/nooma/internal/core/consolidation"
)

// runCron is the daily loop (design §5.3): asks consolidation.NextDailyRun
// for the next ConsolidationHour instant, waits for it through the timer
// seam, and fires one pass attempt through runPass — forever, until ctx is
// done. The loop decides nothing itself: NextDailyRun is the only place
// that reads Clock.Now() into a decision.
func (s *Scheduler) runCron(ctx context.Context) {
	for {
		now := s.clock.Now()
		next := consolidation.NextDailyRun(now, ConsolidationHour)

		select {
		case <-s.timer.After(next.Sub(now)):
			s.runPass(ctx, "cron")
		case <-ctx.Done():
			return
		}
	}
}
