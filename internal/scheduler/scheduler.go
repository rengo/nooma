package scheduler

import (
	"context"
	"errors"
	"io"
	"sync"

	"github.com/rengo/nooma/internal/brain"
	"github.com/rengo/nooma/internal/core/consolidation"
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
	wg          sync.WaitGroup
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

// Start spawns the cron goroutine and returns immediately (design §5.2).
//
// Known gap, disclosed rather than silently assumed complete: the boot
// catch-up goroutine's own wg.Add(1)/Done() is PR 4's addition to this
// same group. Until PR 4 lands, Wait unwinds as soon as the cron goroutine
// alone returns — there is no second goroutine to join yet.
func (s *Scheduler) Start(ctx context.Context) {
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.runCron(ctx)
	}()
}

// Wait blocks until every goroutine Start spawned has unwound, or until
// ctx is done, whichever happens first — the mechanical join design §3.5
// (D5) names; the shutdown-budget wiring that calls this with a
// timeout-bounded ctx is PR 7's own scope.
func (s *Scheduler) Wait(ctx context.Context) {
	done := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-ctx.Done():
	}
}

// runPass is the one entry point into Consolidate from this package
// (design §5.3): both the cron and the boot catch-up (PR 4) call only
// this method, so a per-phase scheduled run is unrepresentable from this
// package (spec R1.1) — brain.ConsolidateRequest{} is its zero value,
// constructed nowhere else.
//
// Config.Load is read at every fire (design §3.3's "the cron re-reads
// config at every fire, so flipping the switch on a running serve takes
// effect at the next tick without a restart"). A false resolution returns
// before Consolidate is ever called — no pass, no decision_log rows, no
// consolidation_last_run_at write, no side effect beyond this read (spec
// R1.2).
//
// No overlap guard yet (PR 3b) and no abort/Corrupted() logging yet (PR
// 5), each named here rather than silently assumed. trigger is accepted
// now, ahead of its first reader, so this method's signature does not
// change again once those callers land.
func (s *Scheduler) runPass(ctx context.Context, trigger string) {
	_ = trigger

	cfg, _ := s.config.Load(ctx)
	if !consolidation.ResolveConsolidationEnabled(cfg.ConsolidationEnabled) {
		return
	}

	_, _ = s.consolidate.Consolidate(ctx, brain.ConsolidateRequest{})
}
