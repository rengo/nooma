package scheduler

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/rengo/nooma/internal/brain"
	"github.com/rengo/nooma/internal/core/consolidation"
	"github.com/rengo/nooma/internal/ports"
)

// ConsolidationHour is owner ruling round 1 #2: the cron fires at 03:00
// local time. Design §5.1 places this constant here, beside
// BootConsolidationDelay — "the constants" — not in cron.go.
const ConsolidationHour = 3

// BootConsolidationDelay is ADR-0009's own reasoning for the boot catch-up:
// startup is already busy opening the vault, running migrations, and
// connecting channels, and consolidation "bothers nobody" by waiting two
// minutes rather than firing immediately. Design §5.1 places this constant
// here, beside ConsolidationHour, not in catchup.go.
const BootConsolidationDelay = 120 * time.Second

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
	slot        chan struct{} // capacity 1: the non-blocking try-lock, design §3.4 (D4)
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
		slot:        make(chan struct{}, 1),
	}, nil
}

// logf writes one line to the process log (design §5.4): an abort, a
// skipped fire, or a completed pass that refused units. Never decision_log
// — an aborted or skipped pass had no vault effect (m2c's own I12 scoping).
func (s *Scheduler) logf(format string, args ...any) {
	_, _ = fmt.Fprintf(s.log, format+"\n", args...)
}

// Start spawns the cron goroutine and the boot catch-up goroutine and
// returns immediately (design §5.2). Both join the same sync.WaitGroup, so
// Wait does not return until both have unwound — closing the gap PR 3a's
// own Start disclosed ("the boot catch-up goroutine's own wg.Add(1)/Done()
// is PR 4's addition to this same group").
func (s *Scheduler) Start(ctx context.Context) {
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.runCron(ctx)
	}()

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.runCatchUp(ctx)
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
// The non-blocking try-lock (design §3.4, D4) serializes both triggers
// into this one method: a fire that cannot take the slot skips rather than
// queuing behind the one in flight — queuing would run a second whole pass
// over a corpus the first one just consolidated (design §3.4's own
// rejected alternative).
//
// A Consolidate error aborts the pass: logged to the process log only
// (design §5.4), never decision_log — nothing was written to the vault, so
// there is no decision to log (m2c's own I12 effect-scoping). No retry
// loop, no special-cased "retry" state: the next fire attempts a fresh
// whole pass, safe because m2c R5.4 already gates
// consolidation_last_run_at on full pass completion (spec R1.4). A
// completed pass that refused units (report.Corrupted() non-empty) is
// logged too, for the identical reason renderConsolidateReport already
// surfaces it to `nooma consolidate`'s own terminal audience
// (cmd/nooma/consolidate.go:120-125) — an unattended pass has none, so
// silence would make the refusal invisible until someone notices a stale
// decision_log.
func (s *Scheduler) runPass(ctx context.Context, trigger string) {
	cfg, err := s.config.Load(ctx)
	if err != nil {
		// Fail closed: an unread config is not an open gate. Treating a
		// Load error as a zero-value VaultConfig would resolve
		// ConsolidationEnabled as nil, which defaults to true — silently
		// converting a read failure into a full consolidation pass.
		return
	}
	if !consolidation.ResolveConsolidationEnabled(cfg.ConsolidationEnabled) {
		return
	}

	select {
	case s.slot <- struct{}{}: // acquired
		defer func() { <-s.slot }()
	default:
		s.logf("scheduler: %s fire skipped, a pass is already running", trigger)
		return
	}

	report, err := s.consolidate.Consolidate(ctx, brain.ConsolidateRequest{})
	if err != nil {
		// Aborted, not retried: m2c R5.4 already gates
		// consolidation_last_run_at on full pass completion, so an aborted
		// pass writes nothing and looks, to the very next fire, exactly
		// like one that never started (spec R1.4). No retry loop, no
		// special-cased "retry" state — the next fire attempts a fresh
		// whole pass, same as any other.
		s.logf("scheduler: pass aborted (%s): %v", trigger, err)
		return
	}
	if corrupted := report.Corrupted(); len(corrupted) > 0 {
		// A refusal had no vault effect, so it is process-log only, never
		// decision_log (m2c's own I12 effect-scoping) — the same rule
		// runPass's own abort line above already follows. Unattended, this
		// is the only place a refused unit is surfaced at all;
		// renderConsolidateReport (cmd/nooma/consolidate.go:120-125) is a
		// hand-run pass's own terminal audience, which a scheduler-
		// triggered pass has none of.
		s.logf("scheduler: %s pass completed, refused %d unit(s): %v", trigger, len(corrupted), corrupted)
	}
}
