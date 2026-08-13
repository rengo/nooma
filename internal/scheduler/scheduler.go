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
	// Log is the process log; serve passes its errOut. nil defaults to
	// io.Discard. It is written from multiple goroutines: the cron and
	// catch-up goroutines both call runPass, and their fires can
	// genuinely overlap — design §3.4/D4's try-lock excludes concurrent
	// entry into Consolidate, not concurrent entry into logf. logf is the
	// only sanctioned path to this writer; nothing else in this package
	// writes to it directly (JD-5-01).
	Log   io.Writer
	Timer timer
}

// Scheduler owns the cron and boot catch-up goroutines, the timer seam,
// and the pass log (design §5.1). It decides nothing itself: every gate
// it checks is a call into internal/core/consolidation.
type Scheduler struct {
	clock       ports.Clock
	config      ports.ConfigRepo
	consolidate Consolidator
	log         io.Writer
	logMu       sync.Mutex // guards every write to log; see logf (JD-5-01)
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
//
// logf is the only sanctioned path to s.log, and this is deliberate, not
// incidental: s.log is written from multiple goroutines (the cron and
// catch-up goroutines both call runPass), and the non-blocking try-lock
// (design §3.4/D4) only ever excludes concurrent entry into Consolidate —
// a fire that loses the try-lock takes the default branch and calls logf
// immediately, with no ordering at all relative to the fire that is still
// holding the slot and will itself call logf on abort or on a completed
// pass with refusals moments later. logMu guards every write here so those
// calls never race (JD-5-01) — no caller may bypass logf and write to
// s.log directly.
func (s *Scheduler) logf(format string, args ...any) {
	s.logMu.Lock()
	defer s.logMu.Unlock()
	_, _ = fmt.Fprintf(s.log, format+"\n", args...)
}

// Start spawns the cron goroutine and the boot catch-up goroutine and
// returns immediately (design §5.2). Both join the same sync.WaitGroup, so
// Wait does not return until both have unwound — closing the gap PR 3a's
// own Start disclosed ("the boot catch-up goroutine's own wg.Add(1)/Done()
// is PR 4's addition to this same group").
//
// Start is a no-op on a nil *Scheduler — design §6's own note, PR 6: a
// vault whose resolveConsolidateProviders refuses (wireScheduler,
// cmd/nooma/wiring.go) has no working *scheduler.Scheduler to start, and
// runServe calls Start unconditionally rather than branching on nil.
func (s *Scheduler) Start(ctx context.Context) {
	if s == nil {
		return
	}

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
//
// Wait is a no-op on a nil *Scheduler, matching Start above: a nil
// Scheduler spawned no goroutines, so there is nothing to join.
func (s *Scheduler) Wait(ctx context.Context) {
	if s == nil {
		return
	}

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
//
// An aborted pass can ALSO carry already-refused units (JD-5-02):
// internal/brain/consolidate.go:1044-1045 returns (report, err) together,
// and report.reportCorrupted runs from six call sites across all five
// phases — Archive, Strengthen and Connect one each, Derive one (inside
// deriveSourceIDs rather than the phase switch itself), and Reweight two —
// every one of which can run and refuse units before a LATER phase's own
// error aborts the same pass. The abort branch
// below surfaces report.Corrupted() alongside the abort itself, as one
// combined log line rather than two separate logf calls: two calls would
// let an unrelated, concurrent write from another goroutine land between
// them (logMu, JD-5-01, only ever makes ONE logf call atomic, not two
// consecutive ones), splitting one pass's own story across two lines that
// might not even stay adjacent in the log. When Corrupted() is empty the
// line is unchanged from before — no refusal, nothing to add.
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
	default:
		s.logf("scheduler: %s fire skipped, a pass is already running", trigger)
		return
	}

	// The slot is released explicitly below, right after Consolidate
	// returns and BEFORE any logging — never by a defer that only fires
	// once this whole method returns (JD-6-01). logf holds logMu for the
	// duration of its write, and Deps.Log can be a genuinely blocking
	// io.Writer in production (wireScheduler passes errOut, os.Stderr as
	// of this PR — an unread `docker logs` consumer, a full pipe buffer, a
	// stalled journald, or a full disk all block a write indefinitely). A
	// slot held until runPass itself returns would stay held for as long
	// as a blocked logf call blocks, and every later fire would then also
	// block trying to acquire the SAME logMu inside its own logf call on
	// the default (skip) branch below — a permanent, silent halt of
	// consolidation, because the log is the very thing stuck.
	//
	// released + the release closure make this a single release no matter
	// which path runs: the explicit call right after Consolidate covers
	// the normal case, and the deferred call is panic-safety only (a panic
	// inside Consolidate must not leak the slot). released guards against
	// ever running both: a second `<-s.slot` on a capacity-1 channel that
	// already gave up its one token would simply block the releasing
	// goroutine forever — not corrupt state, but silently swallow whatever
	// runs after it, and permanently consume a slot token no fire ever
	// legitimately holds, deadlocking the very next fire's own
	// non-blocking acquire. release() is idempotent specifically to make
	// that impossible.
	released := false
	release := func() {
		if !released {
			released = true
			<-s.slot
		}
	}
	defer release()

	report, err := s.consolidate.Consolidate(ctx, brain.ConsolidateRequest{})
	release() // released before any logging — see the comment above
	if err != nil {
		// Aborted, not retried: m2c R5.4 already gates
		// consolidation_last_run_at on full pass completion, so an aborted
		// pass writes nothing and looks, to the very next fire, exactly
		// like one that never started (spec R1.4). No retry loop, no
		// special-cased "retry" state — the next fire attempts a fresh
		// whole pass, same as any other.
		//
		// report is not discarded (JD-5-02): an earlier phase can have
		// already refused units before this later abort, and the process
		// log is the only place that refusal is ever surfaced for an
		// unattended pass. One combined line, not two separate logf calls
		// — see this method's own doc comment above for why.
		if corrupted := report.Corrupted(); len(corrupted) > 0 {
			s.logf("scheduler: pass aborted (%s): %v, refused %d unit(s) before the abort: %v", trigger, err, len(corrupted), corrupted)
		} else {
			s.logf("scheduler: pass aborted (%s): %v", trigger, err)
		}
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
