package scheduler

import (
	"context"

	"github.com/rengo/nooma/internal/core/consolidation"
)

// runCatchUp is ADR-0009's boot catch-up (design §5.1, §5.3): resolves
// whether the vault's last completed pass is stale enough to recover, and
// if so delays the actual pass by BootConsolidationDelay so it does not
// compete with startup. The delay is itself cancellable: ctx.Done() before
// it elapses means the catch-up never fires (spec R2.3).
//
// The due fire goes through runPass(ctx, "catchup") — the same single
// entry point the cron uses — never a ConsolidateRequest this function
// builds itself, so the catch-up shares runPass's own non-blocking
// try-lock (design §3.4, D4) and is indistinguishable, on the
// Consolidator, from a cron-triggered call (spec R2.4).
func (s *Scheduler) runCatchUp(ctx context.Context) {
	cfg, err := s.config.Load(ctx)
	if err != nil {
		// Fail closed, matching runPass's own fix: an unread config leaves
		// ConsolidationLastRunAt nil, which CatchUpDue would read as
		// "never consolidated" and treat as always due.
		return
	}

	// ADR-0009's "Consolidation — always recovered" section contrasts
	// consolidation against the time-based trigger and ephemeral timer
	// kinds, which expire by staleness — it names no user-facing
	// off-switch at all. Gating this catch-up on consolidation_enabled
	// interprets that silence rather than overriding the ADR: the cron and
	// this catch-up are the same work behind two triggers (design §3.3,
	// D3; owner ruling round 1 #1).
	if !consolidation.ResolveConsolidationEnabled(cfg.ConsolidationEnabled) {
		return
	}
	if !consolidation.CatchUpDue(cfg.ConsolidationLastRunAt, s.clock.Now(), consolidation.CatchUpStalenessHours) {
		return
	}

	select {
	case <-s.timer.After(BootConsolidationDelay):
		s.runPass(ctx, "catchup")
	case <-ctx.Done():
		return
	}
}
