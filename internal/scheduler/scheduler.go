package scheduler

import (
	"context"
	"errors"
	"io"

	"github.com/rengo/nooma/internal/brain"
	"github.com/rengo/nooma/internal/ports"
)

// ConsolidationHour is owner ruling round 1 #2: the cron fires at 03:00
// local time. Design §5.1 places this constant here, beside
// BootConsolidationDelay (PR 4) — "the constants" — not in cron.go.
const ConsolidationHour = 3

// Consolidator is the only thing the scheduler asks the brain to do.
// *brain.ConsolidateService satisfies it; this package's own L2 fixtures
// implement it directly (design §5.2).
type Consolidator interface {
	Consolidate(ctx context.Context, req brain.ConsolidateRequest) (brain.ConsolidateReport, error)
}

// Deps is the struct of dependencies New takes, following httpapi.Deps'
// own shape (cmd/nooma/serve.go:108) rather than a long positional
// constructor (design §5.2). Timer is the only optional field: nil means
// the real time.After-backed implementation (realTimer, timer.go).
type Deps struct {
	Clock       ports.Clock
	Config      ports.ConfigRepo
	Consolidate Consolidator
	Log         io.Writer // the process log; serve passes its errOut. nil defaults to io.Discard.
	Timer       timer
}

// Scheduler owns the cron and boot catch-up goroutines, the timer seam,
// and the pass log (design §5.1). It decides nothing itself: every gate
// it checks is a call into internal/core/consolidation.
type Scheduler struct {
	clock       ports.Clock
	config      ports.ConfigRepo
	consolidate Consolidator
	log         io.Writer
	timer       timer
}

// New validates d's three required dependencies and returns a *Scheduler
// ready for Start. A nil Clock, Config, or Consolidate is rejected here
// rather than deferred to a nil-pointer panic at the first fire.
func New(d Deps) (*Scheduler, error) {
	if d.Clock == nil {
		return nil, errors.New("scheduler: Clock is required")
	}
	if d.Config == nil {
		return nil, errors.New("scheduler: Config is required")
	}
	if d.Consolidate == nil {
		return nil, errors.New("scheduler: Consolidate is required")
	}

	log := d.Log
	if log == nil {
		log = io.Discard
	}
	t := d.Timer
	if t == nil {
		t = realTimer{}
	}

	return &Scheduler{
		clock:       d.Clock,
		config:      d.Config,
		consolidate: d.Consolidate,
		log:         log,
		timer:       t,
	}, nil
}

// runPass is the one entry point into Consolidate from this package
// (design §5.3): both the cron and the boot catch-up (PR 4) call only
// this method, so a per-phase scheduled run is unrepresentable from this
// package (spec R1.1) — brain.ConsolidateRequest{} is its zero value,
// constructed nowhere else.
//
// This commit's version (task 3a.6) checks nothing before calling
// Consolidate: no gate (task 3a.8's own addition), no overlap guard (PR
// 3b), no abort/Corrupted() logging (PR 5). trigger is accepted now,
// ahead of its first reader, so this method's signature does not change
// again once those callers land.
func (s *Scheduler) runPass(ctx context.Context, trigger string) {
	_ = trigger
	_, _ = s.consolidate.Consolidate(ctx, brain.ConsolidateRequest{})
}
