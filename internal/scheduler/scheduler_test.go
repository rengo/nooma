package scheduler

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math"
	"sync"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/brain"
	"github.com/rengo/nooma/internal/core/consolidation"
	"github.com/rengo/nooma/internal/core/recall"
	"github.com/rengo/nooma/internal/core/unit"
	"github.com/rengo/nooma/internal/ports"
	"github.com/rengo/nooma/test/support/fakeprovider"
	"github.com/rengo/nooma/test/support/memrepo"
)

// fixedClock is a deterministic ports.Clock for this package's own tests,
// mirroring internal/brain/consolidate_test.go's identical precedent — a
// small package-local test double.
type fixedClock struct{ now time.Time }

func (c fixedClock) Now() time.Time { return c.now }

// fixedNow is this package's own fixture instant: a time with no
// significance beyond being deterministic, comfortably before
// ConsolidationHour so a test needs no particular hour of its own.
var fixedNow = time.Date(2026, 8, 12, 2, 0, 0, 0, time.UTC)

// recordingConsolidator is a package-local Consolidator fake: every call
// is appended to calls, and a buffered notify channel lets a test block
// until at least one call has landed without a busy-poll loop.
type recordingConsolidator struct {
	mu     sync.Mutex
	calls  []brain.ConsolidateRequest
	notify chan struct{}
}

func newRecordingConsolidator() *recordingConsolidator {
	return &recordingConsolidator{notify: make(chan struct{}, 8)}
}

func (c *recordingConsolidator) Consolidate(_ context.Context, req brain.ConsolidateRequest) (brain.ConsolidateReport, error) {
	c.mu.Lock()
	c.calls = append(c.calls, req)
	c.mu.Unlock()
	select {
	case c.notify <- struct{}{}:
	default:
	}
	return brain.ConsolidateReport{}, nil
}

// validDeps returns a Deps every field of which is non-nil — the baseline
// TestNew_RejectsNilDeps mutates one field of at a time.
func validDeps() Deps {
	return Deps{
		Clock:       fixedClock{now: fixedNow},
		Config:      memrepo.NewConfig(),
		Consolidate: newRecordingConsolidator(),
	}
}

// TestNew_RejectsNilDeps is task 3a.2: New rejects a nil Clock, a nil
// Config, and a nil Consolidate — three cases, each an otherwise-valid
// Deps with exactly one field zeroed.
func TestNew_RejectsNilDeps(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(Deps) Deps
	}{
		{name: "nil Clock", mutate: func(d Deps) Deps { d.Clock = nil; return d }},
		{name: "nil Config", mutate: func(d Deps) Deps { d.Config = nil; return d }},
		{name: "nil Consolidate", mutate: func(d Deps) Deps { d.Consolidate = nil; return d }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := tt.mutate(validDeps())

			s, err := New(d)

			if err == nil {
				t.Fatalf("New() with %s = _, nil; want a non-nil error", tt.name)
			}
			if s != nil {
				t.Fatalf("New() with %s = %v, %v; want a nil *Scheduler alongside the error", tt.name, s, err)
			}
		})
	}
}

// TestNew_AcceptsValidDeps triangulates TestNew_RejectsNilDeps above with
// the opposite expected output: a fully populated Deps returns a
// *Scheduler and no error.
func TestNew_AcceptsValidDeps(t *testing.T) {
	s, err := New(validDeps())
	if err != nil {
		t.Fatalf("New(validDeps()) = %v, %v; want a *Scheduler and a nil error", s, err)
	}
	if s == nil {
		t.Fatal("New(validDeps()) returned a nil *Scheduler alongside a nil error")
	}
}

// blockingConsolidator is task 3b.1's fixture: every call blocks on
// release until the test lets it go, and tracks the peak number of
// concurrent entries via channel synchronization only — no sleep-based
// polling — so the "exactly one in flight" assertion is real, not
// timing-dependent (spec R1.3; design §3.4).
type blockingConsolidator struct {
	entered chan struct{} // signaled once per Consolidate call, right after entry
	release chan struct{} // the test closes this to let every blocked call return

	mu          sync.Mutex
	calls       int
	inFlight    int
	maxInFlight int
	lastReq     brain.ConsolidateRequest // task 4.7's own addition: the last call's own request
}

func newBlockingConsolidator() *blockingConsolidator {
	return &blockingConsolidator{entered: make(chan struct{}, 4), release: make(chan struct{})}
}

func (b *blockingConsolidator) Consolidate(_ context.Context, req brain.ConsolidateRequest) (brain.ConsolidateReport, error) {
	b.mu.Lock()
	b.calls++
	b.inFlight++
	if b.inFlight > b.maxInFlight {
		b.maxInFlight = b.inFlight
	}
	b.lastReq = req
	b.mu.Unlock()

	b.entered <- struct{}{}
	<-b.release

	b.mu.Lock()
	b.inFlight--
	b.mu.Unlock()
	return brain.ConsolidateReport{}, nil
}

func (b *blockingConsolidator) snapshot() (calls, maxInFlight int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.calls, b.maxInFlight
}

func (b *blockingConsolidator) lastRequest() brain.ConsolidateRequest {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.lastReq
}

// TestScheduler_NoOverlap_ExactlyOneInFlight is task 3b.1: a slow fake
// Consolidate blocked on a channel, two fires landing inside that window —
// a cron fire plus a direct runPass(ctx, "test") call, since the catch-up
// trigger does not exist until PR 4 — assert exactly one call is ever in
// flight. Spec R1.3; design §3.4 (D4).
func TestScheduler_NoOverlap_ExactlyOneInFlight(t *testing.T) {
	ft := newFakeTimer()
	bc := newBlockingConsolidator()
	s, err := New(Deps{Clock: fixedClock{now: fixedNow}, Config: memrepo.NewConfig(), Consolidate: bc, Timer: ft})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	cronDone := make(chan struct{})
	go func() {
		s.runCron(ctx)
		close(cronDone)
	}()

	waitForAfterCall(t, ft) // the cron loop is now blocked in its first wait
	ft.fire <- fixedNow     // the cron's own 03:00-equivalent fire

	// Wait for the cron fire's own runPass to actually enter Consolidate
	// (holding the slot), not merely for the tick to have been sent.
	select {
	case <-bc.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for the cron fire's own Consolidate call to start")
	}

	// A second fire lands inside that same window — a direct runPass call,
	// exactly design §3.4's own "the catch-up's 120s delay elapses at 03:00
	// on top of a pass that has just begun" scenario, using "test" as the
	// trigger since the real catch-up caller does not exist until PR 4.
	secondDone := make(chan struct{})
	go func() {
		s.runPass(ctx, "test")
		close(secondDone)
	}()

	// Exactly one of these two outcomes happens, deterministically: either
	// the second call enters Consolidate too (no guard — both in flight),
	// or it returns without ever entering (a guard skipped it). No sleep:
	// whichever the second runPass call actually does is what is observed.
	select {
	case <-bc.entered:
		// The second call entered Consolidate while the first was still in
		// flight. Release both so the test can finish either way, then
		// fail below on the maxInFlight assertion.
		close(bc.release)
		<-secondDone
	case <-secondDone:
		// The second call returned without ever calling Consolidate — a
		// guard skipped it. Release the first (still in flight) so the
		// cron goroutine can finish.
		close(bc.release)
	case <-time.After(2 * time.Second):
		close(bc.release)
		t.Fatal("timed out waiting for the second runPass call to either enter Consolidate or return")
	}

	cancel()
	select {
	case <-cronDone:
	case <-time.After(2 * time.Second):
		t.Fatal("runCron did not return after ctx cancellation")
	}

	calls, maxInFlight := bc.snapshot()
	if maxInFlight != 1 {
		t.Fatalf("max concurrent Consolidate calls = %d, want exactly 1 — two fires must never run a pass at once", maxInFlight)
	}
	if calls != 1 {
		t.Fatalf("Consolidate called %d times, want exactly 1 — the second fire must be skipped while the first is in flight", calls)
	}
}

// TestScheduler_SkippedFire_LogsOneLine is tasks 3b.3/3b.4. Disclosed, not
// a genuine red: task 3b.2's own text quotes design §3.4's exact shape
// ("default: skip+log") as part of its own scope, and the runPass commit
// already carries the final s.logf("scheduler: %s fire skipped, a pass is
// already running", trigger) call verbatim — so this test passes on first
// run, with no prior implementation state where it could have failed.
//
// It also asserts a narrower claim than task 3b.3's own prose ("naming the
// trigger it skipped and the trigger holding the slot"): design §3.4's own
// code block and task 3b.4's own verbatim text both give a single %s — the
// skipped trigger only. No task adds a field that records which trigger
// currently holds the slot, so that half of 3b.3's prose describes a log
// line this implementation does not produce; asserting it here would be
// testing a design that was never built. Followed here: verbatim design.
func TestScheduler_SkippedFire_LogsOneLine(t *testing.T) {
	ft := newFakeTimer()
	bc := newBlockingConsolidator()
	var logBuf bytes.Buffer
	s, err := New(Deps{Clock: fixedClock{now: fixedNow}, Config: memrepo.NewConfig(), Consolidate: bc, Timer: ft, Log: &logBuf})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	cronDone := make(chan struct{})
	go func() {
		s.runCron(ctx)
		close(cronDone)
	}()

	waitForAfterCall(t, ft)
	ft.fire <- fixedNow

	select {
	case <-bc.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for the cron fire's own Consolidate call to start")
	}

	// The direct call is the one that gets skipped — its trigger name is
	// what the log line must name.
	s.runPass(ctx, "catchup")

	close(bc.release)
	cancel()
	select {
	case <-cronDone:
	case <-time.After(2 * time.Second):
		t.Fatal("runCron did not return after ctx cancellation")
	}

	const want = "scheduler: catchup fire skipped, a pass is already running\n"
	if got := logBuf.String(); got != want {
		t.Fatalf("log = %q, want %q", got, want)
	}
}

// TestScheduler_Wait is task 3a.9: Wait(ctx) returns once the cron
// goroutine unwinds after ctx cancellation, or when ctx itself is done
// first — design §5.2, §3.5 (D5, mechanical join only; the shutdown-budget
// wiring itself is PR 7's own scope).
func TestScheduler_Wait(t *testing.T) {
	t.Run("returns once the cron goroutine unwinds after ctx cancellation", func(t *testing.T) {
		ft := newFakeTimer() // never fed — the cron goroutine blocks on it until ctx is done
		s, err := New(Deps{Clock: fixedClock{now: fixedNow}, Config: memrepo.NewConfig(), Consolidate: newRecordingConsolidator(), Timer: ft})
		if err != nil {
			t.Fatalf("New: %v", err)
		}

		ctx, cancel := context.WithCancel(context.Background())
		s.Start(ctx)
		cancel()

		waitCtx, waitCancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer waitCancel()
		s.Wait(waitCtx)

		if err := waitCtx.Err(); err != nil {
			t.Fatalf("Wait did not return before its own 2s ctx expired: %v", err)
		}
	})

	t.Run("returns when ctx itself is done first", func(t *testing.T) {
		ft := newFakeTimer() // never fed — the cron goroutine never unwinds on its own
		s, err := New(Deps{Clock: fixedClock{now: fixedNow}, Config: memrepo.NewConfig(), Consolidate: newRecordingConsolidator(), Timer: ft})
		if err != nil {
			t.Fatalf("New: %v", err)
		}

		// Start with a ctx that this subtest never cancels: only Wait's own
		// ctx governs when this call unblocks, isolating the second claim
		// from the first.
		schedCtx, schedCancel := context.WithCancel(context.Background())
		t.Cleanup(schedCancel)
		s.Start(schedCtx)

		waitCtx, waitCancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		defer waitCancel()

		start := time.Now()
		s.Wait(waitCtx)
		if elapsed := time.Since(start); elapsed > time.Second {
			t.Fatalf("Wait blocked for %v; want it to return promptly once its own ctx is done", elapsed)
		}
	})
}

// TestScheduler_NilScheduler_StartAndWaitAreNoOps is PR 6's own addition
// (task 6.7; non-negotiable: "Start/Wait must be no-ops on a nil
// *Scheduler so runServe needs no branch"): wireScheduler
// (cmd/nooma/wiring.go) returns a nil *Scheduler, nil error when a vault's
// resolveConsolidateProviders refuses (design §6; spec R3.2), and
// cmd/nooma/serve.go calls Start/Wait on that result unconditionally, with
// no nil guard of its own. Both calls, on a nil receiver, must return
// rather than panic on a nil-pointer field dereference.
func TestScheduler_NilScheduler_StartAndWaitAreNoOps(t *testing.T) {
	var s *Scheduler

	s.Start(context.Background())

	waitCtx, waitCancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer waitCancel()
	s.Wait(waitCtx)
}

// erroringConsolidator is task 5.1's own fixture: every call returns the
// scripted error and the zero-value ConsolidateReport, simulating a
// mid-pass abort — persistBoosts's own ports.ErrUnitNotFound (spec R1.4).
// runPass must return cleanly rather than propagate or panic, and the
// process log must record the failure (design §5.4).
type erroringConsolidator struct {
	mu    sync.Mutex
	calls []brain.ConsolidateRequest
	err   error
}

func (c *erroringConsolidator) Consolidate(_ context.Context, req brain.ConsolidateRequest) (brain.ConsolidateReport, error) {
	c.mu.Lock()
	c.calls = append(c.calls, req)
	c.mu.Unlock()
	return brain.ConsolidateReport{}, c.err
}

func (c *erroringConsolidator) callCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.calls)
}

func (c *erroringConsolidator) recordedCalls() []brain.ConsolidateRequest {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]brain.ConsolidateRequest(nil), c.calls...)
}

// TestRunPass_AbortSurfacesOnProcessLog is task 5.1: a fake Consolidator
// returns ports.ErrUnitNotFound mid-pass (simulating persistBoosts's own
// abort), triggered by a scheduler fire (not a direct Consolidate call) —
// runPass returns without panicking and the operational log records the
// failure. Spec R1.4; design §5.4.
//
// Scope note: the "consolidation_last_run_at unwritten" property is m2c
// R5.4's own already-proven guarantee (RecordConsolidationRun unreachable
// on any runPhase error) — relied upon here as a fact the scheduler package
// has no write path to re-assert, not re-tested at this layer.
//
// Red: runPass's error path is unasserted before this test — fails on the
// missing log line (no s.logf call exists on this path yet).
func TestRunPass_AbortSurfacesOnProcessLog(t *testing.T) {
	ft := newFakeTimer()
	ec := &erroringConsolidator{err: ports.ErrUnitNotFound}
	var logBuf bytes.Buffer
	s, err := New(Deps{Clock: fixedClock{now: fixedNow}, Config: memrepo.NewConfig(), Consolidate: ec, Timer: ft, Log: &logBuf})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	done := make(chan struct{})
	go func() {
		s.runCron(ctx)
		close(done)
	}()

	waitForAfterCall(t, ft) // the cron loop is now blocked in its first wait
	ft.fire <- fixedNow

	// Wait for the loop to ask the timer for its NEXT wait — proof the
	// erroring call already returned and runPass already unwound, not
	// merely that the tick was sent.
	waitForAfterCall(t, ft)

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("runCron did not return after ctx cancellation")
	}

	if got := ec.callCount(); got != 1 {
		t.Fatalf("Consolidate called %d times, want exactly 1", got)
	}

	const want = "scheduler: pass aborted (cron): unit not found\n"
	if got := logBuf.String(); got != want {
		t.Fatalf("log = %q, want %q", got, want)
	}
}

// blockingErrorConsolidator is JD-5-01's own fixture: every call blocks on
// entry, signaling entered, until the test closes release, then returns the
// scripted error — the shape needed to hold the pass slot open (so a second,
// genuinely concurrent fire is forced onto the default/skip branch) while
// this call later aborts and calls logf itself.
type blockingErrorConsolidator struct {
	entered chan struct{}
	release chan struct{}
	err     error
}

func newBlockingErrorConsolidator(err error) *blockingErrorConsolidator {
	return &blockingErrorConsolidator{entered: make(chan struct{}, 1), release: make(chan struct{}), err: err}
}

func (b *blockingErrorConsolidator) Consolidate(_ context.Context, _ brain.ConsolidateRequest) (brain.ConsolidateReport, error) {
	b.entered <- struct{}{}
	<-b.release
	return brain.ConsolidateReport{}, b.err
}

// TestRunPass_LogfIsRaceFree is JD-5-01: logf (scheduler.go) wrote to
// s.log with no synchronization of its own. The non-blocking try-lock
// (design §3.4, D4) only excludes concurrent entry into Consolidate — a
// fire that loses the try-lock (the default branch) calls logf
// immediately, with no happens-before relationship at all to the fire
// that is still holding the slot and will itself call logf on abort
// moments later. This test exercises exactly that interleaving for real:
//
//  1. Goroutine A calls runPass, acquires the slot, and blocks inside
//     Consolidate (signaled via bc.entered, so the test knows A genuinely
//     holds the slot before proceeding — not a timing guess).
//  2. Goroutine B then calls runPass concurrently. Because A still holds
//     the slot, B deterministically takes the default (skip) branch and
//     calls logf right away — this determinism comes from Go's channel
//     memory model (A's slot acquisition happens-before the test's own
//     receive from bc.entered, which happens-before B's goroutine starts),
//     not from waiting on B in any way.
//  3. The test then releases A, which returns its scripted error and
//     calls logf itself on the abort path.
//
// B's logf call and A's logf call are never ordered relative to each
// other by any channel, mutex, or WaitGroup — the try-lock only ever
// orders the SLOT, not the LOG — so this is a genuine, reproducible data
// race on the shared bytes.Buffer under -race, not a theoretical one.
func TestRunPass_LogfIsRaceFree(t *testing.T) {
	bc := newBlockingErrorConsolidator(errors.New("boom"))
	var logBuf bytes.Buffer
	s, err := New(Deps{Clock: fixedClock{now: fixedNow}, Config: memrepo.NewConfig(), Consolidate: bc, Log: &logBuf})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	ctx := context.Background()
	aDone := make(chan struct{})
	go func() {
		s.runPass(ctx, "cron") // acquires the slot, blocks inside Consolidate, then aborts
		close(aDone)
	}()

	select {
	case <-bc.entered:
		// A now holds the slot, synchronously blocked inside Consolidate.
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for the first fire to enter Consolidate")
	}

	bDone := make(chan struct{})
	go func() {
		s.runPass(ctx, "catchup") // the slot is held: takes the default branch and skips
		close(bDone)
	}()

	close(bc.release) // let A's Consolidate call return its scripted error, unordered with B's own logf call above

	select {
	case <-aDone:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for the first fire to return")
	}
	select {
	case <-bDone:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for the second fire to return")
	}

	// The real assertion is `go test -race`: an unsynchronized shared
	// io.Writer written from both goroutines above is a data race whether
	// or not the two lines below look sane.
	if logBuf.Len() == 0 {
		t.Fatal("expected both fires to have written to the log")
	}
}

// TestRunPass_NextFireAttemptsFreshWholePass is task 5.3: after an aborted
// fire, a second runPass call attempts a full pass again — the fake
// Consolidator's recorded ConsolidateRequest{} is the same zero value both
// times, no carried state. Spec R1.4 (second Verified-by clause).
//
// Disclosed, not a genuine red: this is only red if 5.2's abort path
// accidentally threads state into the next call — runPass already
// constructs brain.ConsolidateRequest{} fresh at every call and never
// mutates trigger-scoped state across calls, so this is a regression guard
// more than a fresh failure.
func TestRunPass_NextFireAttemptsFreshWholePass(t *testing.T) {
	ec := &erroringConsolidator{err: ports.ErrUnitNotFound}
	s, err := New(Deps{Clock: fixedClock{now: fixedNow}, Config: memrepo.NewConfig(), Consolidate: ec})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	ctx := context.Background()
	s.runPass(ctx, "cron")
	s.runPass(ctx, "cron")

	calls := ec.recordedCalls()
	if len(calls) != 2 {
		t.Fatalf("Consolidate called %d times, want exactly 2", len(calls))
	}
	for i, req := range calls {
		if req != (brain.ConsolidateRequest{}) {
			t.Fatalf("call %d: request = %+v, want the zero value (no carried state from the prior aborted call)", i, req)
		}
	}
}

// newSingleCorruptedUnitConsolidator wires a real *brain.ConsolidateService
// over in-memory repos seeded with exactly one unit whose decay state is
// non-finite (Weight: NaN) — partitionLiveDecayStates (internal/brain)
// refuses it in every phase that reads LiveDecayStates (archive, connect,
// reweight), and report.reportCorrupted dedups the id to one entry.
//
// Disclosed deviation from task 5.5's own "a fake Consolidate" wording:
// brain.ConsolidateReport's fields are unexported, so no value outside
// package brain can construct one with a non-empty Corrupted() — and this
// PR's own hard constraints forbid both touching internal/brain/
// consolidate.go (task 5.7) and adding any new file (this PR's own diff
// scope note). A real ConsolidateService, wired the same way internal/
// brain's own TestConsolidate_WholePassReportsEachCorruptedIDOnce and
// TestConsolidate_NoEffects already do, is the only way to produce a
// genuine non-empty Corrupted() value without either. The single seeded
// unit is refused everywhere and never a valid source: connect's and
// derive's own sourceIDs are computed from the usable (non-refused) set,
// which is empty here, so both short-circuit before ever calling the judge
// or the recall service — the whole pass is otherwise a true no-op, the
// same "nothing fed, nothing written" shape TestConsolidate_NoEffects
// already proves for an entirely empty fixture.
func newSingleCorruptedUnitConsolidator(t *testing.T) *brain.ConsolidateService {
	t.Helper()

	units := memrepo.NewUnits()
	if err := units.Create(context.Background(), unit.Unit{
		ID:              "u-corrupted",
		Type:            unit.TypeKnowledge,
		Status:          unit.StatusPool,
		Content:         "a unit whose decay state is non-finite",
		Source:          "chat",
		Weight:          math.NaN(),
		WeightDecayRate: 0,
		LastTouchedAt:   fixedNow,
		CreatedAt:       fixedNow,
		UpdatedAt:       fixedNow,
	}); err != nil {
		t.Fatalf("seed corrupted unit: %v", err)
	}

	recallSvc := brain.NewRecallService(
		brain.NewIndex(recall.VectorIndex{Model: "test-model"}),
		memrepo.NewLexical(),
		units,
		fakeprovider.NewEmbeddingFake("test-model"),
	)
	// Zero scripted cases: the only seeded unit is refused everywhere, so
	// derive's own sourceIDs end up empty and the judge is never called —
	// an unexpected call fails this test loudly (fakeprovider's own
	// unscripted-call guard) rather than reaching a real provider (CLAUDE.md
	// non-negotiable #5).
	judge := fakeprovider.New(t, "")

	return brain.NewConsolidateService(
		fixedClock{now: fixedNow},
		memrepo.NewConfig(),
		units,
		memrepo.NewRelations(),
		&fakeIDGen{},
		memrepo.NewDecisionLog(),
		recallSvc,
		judge,
		memrepo.NewSelfModel(),
		memrepo.NewState(),
	)
}

// fakeIDGen is a small package-local ports.IDGen double, the same
// precedent internal/brain/correction_test.go's own fakeIDs sets — no
// export exists for this from package brain, and this PR adds no new file,
// so it is reimplemented locally rather than shared.
type fakeIDGen struct{ n int }

func (g *fakeIDGen) New() string {
	g.n++
	return fmt.Sprintf("scheduler-test-id-%d", g.n)
}

// TestRunPass_CompletedPassLogsCorrupted is task 5.5: a fake Consolidate
// returning a brain.ConsolidateReport with a non-empty Corrupted() (no
// error) logs one line naming the refused unit ids on success. Design §5.2
// ("Corrupted() is operationally meaningful for an unattended pass").
//
// Red: no log call exists on the success path yet — fails on the missing
// line.
func TestRunPass_CompletedPassLogsCorrupted(t *testing.T) {
	svc := newSingleCorruptedUnitConsolidator(t)
	var logBuf bytes.Buffer
	s, err := New(Deps{Clock: fixedClock{now: fixedNow}, Config: memrepo.NewConfig(), Consolidate: svc, Log: &logBuf})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	s.runPass(context.Background(), "cron")

	const want = "scheduler: cron pass completed, refused 1 unit(s): [u-corrupted]\n"
	if got := logBuf.String(); got != want {
		t.Fatalf("log = %q, want %q", got, want)
	}
}

// abortingUnitsAfterNCalls is JD-5-02's own fixture: wraps a real
// ports.UnitRepo and returns err from its nth LiveDecayStates call onward,
// every other call and every other method passing straight through to the
// wrapped repo. LiveDecayStates is called four times per whole pass — slot
// 2 (archive), slot 4 (connect), slot 5 (derive), slot 6 (reweight)
// (internal/ports/unitrepo.go:148-149) — so n=2 aborts at Connect's own
// read, strictly after Archive's own report.reportCorrupted call
// (internal/brain/consolidate.go:1090-1091) has already run. This is the
// only way to make a real *brain.ConsolidateService return a (report, err)
// pair where both are non-trivial in the same call, without touching
// internal/brain/consolidate.go (task 5.7's own hard constraint) or
// constructing a brain.ConsolidateReport by hand from outside package brain
// (its fields are unexported — task 5.5's own disclosed precedent for the
// identical problem).
type abortingUnitsAfterNCalls struct {
	ports.UnitRepo
	n   int
	err error

	mu    sync.Mutex
	calls int
}

func (u *abortingUnitsAfterNCalls) LiveDecayStates(ctx context.Context) ([]consolidation.Cold, error) {
	u.mu.Lock()
	u.calls++
	call := u.calls
	u.mu.Unlock()
	if call == u.n {
		return nil, u.err
	}
	return u.UnitRepo.LiveDecayStates(ctx)
}

// TestRunPass_AbortSurfacesRefusedUnits is JD-5-02: runPass's abort branch
// logged only the abort line and discarded report entirely, but
// internal/brain/consolidate.go:1044-1045 returns (report, err) together,
// and report.reportCorrupted is called from five separate phase sites
// (lines 1091, 1103, 1113, 1132, 1134) — so an earlier phase can refuse
// units and a later phase can still abort the same pass, and those
// already-refused ids never reached the process log. Unattended, the
// process log is the only place a refused unit is surfaced at all (design
// §5.4).
//
// This wires a real *brain.ConsolidateService (task 5.5's own precedent)
// over one seeded unit whose decay state is non-finite — refused at
// Archive (slot 2) — and abortingUnitsAfterNCalls{n: 2}, which fails
// Connect's own LiveDecayStates read (slot 4), strictly after Archive's own
// refusal was already recorded. The returned report therefore genuinely
// carries both Corrupted() = ["u-corrupted"] and a non-nil err in the same
// call — not two independently-scripted fake return values.
//
// Red: runPass's abort branch discards report and logs only the plain
// abort line — fails on the missing refused-unit clause.
func TestRunPass_AbortSurfacesRefusedUnits(t *testing.T) {
	units := memrepo.NewUnits()
	if err := units.Create(context.Background(), unit.Unit{
		ID:              "u-corrupted",
		Type:            unit.TypeKnowledge,
		Status:          unit.StatusPool,
		Content:         "a unit whose decay state is non-finite",
		Source:          "chat",
		Weight:          math.NaN(),
		WeightDecayRate: 0,
		LastTouchedAt:   fixedNow,
		CreatedAt:       fixedNow,
		UpdatedAt:       fixedNow,
	}); err != nil {
		t.Fatalf("seed corrupted unit: %v", err)
	}

	wrapped := &abortingUnitsAfterNCalls{UnitRepo: units, n: 2, err: errors.New("boom")}

	recallSvc := brain.NewRecallService(
		brain.NewIndex(recall.VectorIndex{Model: "test-model"}),
		memrepo.NewLexical(),
		units,
		fakeprovider.NewEmbeddingFake("test-model"),
	)
	judge := fakeprovider.New(t, "") // zero scripted cases: never reached before the abort

	svc := brain.NewConsolidateService(
		fixedClock{now: fixedNow},
		memrepo.NewConfig(),
		wrapped,
		memrepo.NewRelations(),
		&fakeIDGen{},
		memrepo.NewDecisionLog(),
		recallSvc,
		judge,
		memrepo.NewSelfModel(),
		memrepo.NewState(),
	)

	var logBuf bytes.Buffer
	s, err := New(Deps{Clock: fixedClock{now: fixedNow}, Config: memrepo.NewConfig(), Consolidate: svc, Log: &logBuf})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	s.runPass(context.Background(), "cron")

	const want = "scheduler: pass aborted (cron): consolidate: connect: read live decay states: boom, refused 1 unit(s) before the abort: [u-corrupted]\n"
	if got := logBuf.String(); got != want {
		t.Fatalf("log = %q, want %q", got, want)
	}
}

// permanentlyBlockedWriter is JD-6-01's own fixture: Write blocks forever
// once entered, signaling entered exactly once (buffered, non-blocking
// send) so a test can deterministically wait until logf is synchronously
// inside Write before proceeding — no sleep-based timing. This is the test
// double for a genuinely blocking os.Stderr: an unread `docker logs`
// consumer, a full pipe buffer, a stalled journald, or a full disk are all
// real deployment shapes that block a write indefinitely, not fabricated
// ones.
type permanentlyBlockedWriter struct {
	entered chan struct{}
}

func newPermanentlyBlockedWriter() *permanentlyBlockedWriter {
	return &permanentlyBlockedWriter{entered: make(chan struct{}, 1)}
}

func (w *permanentlyBlockedWriter) Write(_ []byte) (int, error) {
	select {
	case w.entered <- struct{}{}:
	default:
	}
	select {} // blocks forever, deliberately — see the type's own doc comment
}

// twoPhaseConsolidator is JD-6-01's own fixture: the first call returns a
// scripted error immediately (routing runPass into the abort-log path,
// which then calls logf); every later call returns success immediately and
// is recorded. This is the shape needed to prove a SECOND, independent
// fire can still reach Consolidate after the first fire's own logf call is
// permanently blocked inside Write.
type twoPhaseConsolidator struct {
	mu    sync.Mutex
	calls int
	err   error
}

func (c *twoPhaseConsolidator) Consolidate(_ context.Context, _ brain.ConsolidateRequest) (brain.ConsolidateReport, error) {
	c.mu.Lock()
	c.calls++
	first := c.calls == 1
	c.mu.Unlock()
	if first {
		return brain.ConsolidateReport{}, c.err
	}
	return brain.ConsolidateReport{}, nil
}

func (c *twoPhaseConsolidator) callCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls
}

// TestRunPass_SlotReleasedBeforeBlockedLog is JD-6-01: logf holds logMu for
// the duration of fmt.Fprintf(s.log, ...), and runPass's original shape
// released the slot only via a defer that fires when runPass itself
// returns. A fire whose logf call blocks forever therefore never returns,
// so the slot it still holds is never released either — every later fire
// then takes the default (skip) branch and calls logf itself, which also
// blocks forever on the SAME logMu the first goroutine is still holding
// inside its own blocked Fprintf call. The result: consolidation halts
// permanently, with no crash and no log line escaping to say so, because
// the log is the thing that is stuck.
//
// This test proves the cascade is broken: the first fire's Consolidate
// call errors (routing it into the abort-log path) and its Log writer
// blocks forever on the very first Write. A SECOND, independent fire —
// sharing the same *Scheduler, the same slot and the same logMu — must
// still be able to acquire the slot and actually run its own pass, which
// is only possible if the slot was released strictly before the first
// fire's blocked logf call, not after (i.e. never, in this scenario).
//
// Red, observed against the pre-fix code (slot released by a defer that
// only fires when runPass returns):
//
//	go test -run TestRunPass_SlotReleasedBeforeBlockedLog -v -timeout 10s ./internal/scheduler/...
//	--- FAIL: TestRunPass_SlotReleasedBeforeBlockedLog (2.00s)
//	    scheduler_test.go:...: timed out waiting for the second fire to return — the slot (and logMu) are permanently wedged by the first fire's blocked logf call
//
// Green after the fix: the slot is released before Consolidate's caller
// ever calls logf, so the second fire's try-lock succeeds and it reaches
// Consolidate on its own, independently of the first fire's still-blocked
// Write call.
func TestRunPass_SlotReleasedBeforeBlockedLog(t *testing.T) {
	tc := &twoPhaseConsolidator{err: errors.New("boom")}
	w := newPermanentlyBlockedWriter()
	s, err := New(Deps{Clock: fixedClock{now: fixedNow}, Config: memrepo.NewConfig(), Consolidate: tc, Log: w})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	ctx := context.Background()

	// First fire: acquires the slot, aborts (tc.err), and calls logf,
	// which blocks forever inside Write. This goroutine never returns — it
	// is deliberately leaked for the lifetime of the test, mirroring
	// exactly what a genuinely blocked os.Stderr would do to the real
	// goroutine in production.
	go func() {
		s.runPass(ctx, "cron")
	}()

	select {
	case <-w.entered:
		// The first fire's logf call is now synchronously blocked inside
		// Write — the checkpoint this test needs: under the fix, the slot
		// was already released strictly before this call; under the bug,
		// it is still held and will never be released.
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for the first fire's logf call to reach the blocked writer")
	}

	// A second, independent fire must still be able to acquire the slot
	// and reach Consolidate — proof consolidation did not permanently
	// halt. Bounded: under the bug this call blocks forever inside its own
	// logf call (default branch, same wedged logMu), so it must never be
	// allowed to hang the test itself.
	secondDone := make(chan struct{})
	go func() {
		s.runPass(ctx, "catchup")
		close(secondDone)
	}()

	select {
	case <-secondDone:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for the second fire to return — the slot (and logMu) are permanently wedged by the first fire's blocked logf call")
	}

	if got := tc.callCount(); got != 2 {
		t.Fatalf("Consolidate called %d times, want exactly 2 — the second fire must have actually run a pass, not merely returned", got)
	}
}

// discriminatingTimer is JD-4-04's own fixture. fakeTimer (cron_test.go)
// hands every caller the SAME shared channel, so a test with both the cron
// and the catch-up goroutine running cannot fire one without also risking
// the other racing to receive it. discriminatingTimer instead keys a
// distinct channel per distinct requested duration — the cron's own
// next-tick duration and the catch-up's own BootConsolidationDelay are
// never equal in this package's fixtures — so a test can fire exactly one
// of the two waits and leave the other genuinely unfired.
type discriminatingTimer struct {
	mu    sync.Mutex
	chans map[time.Duration]chan time.Time
	calls chan struct{}
}

func newDiscriminatingTimer() *discriminatingTimer {
	return &discriminatingTimer{chans: make(map[time.Duration]chan time.Time), calls: make(chan struct{}, 8)}
}

func (d *discriminatingTimer) After(dur time.Duration) <-chan time.Time {
	d.mu.Lock()
	ch, ok := d.chans[dur]
	if !ok {
		ch = make(chan time.Time, 1)
		d.chans[dur] = ch
	}
	d.mu.Unlock()
	select {
	case d.calls <- struct{}{}:
	default:
	}
	return ch
}

// chanFor reports dur's channel if a caller has asked for it, or nil.
// Test-only: it exists so a test can wait until a specific loop has
// reached its own wait, rather than sleeping and hoping.
func (d *discriminatingTimer) chanFor(dur time.Duration) chan time.Time {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.chans[dur]
}

// fire sends t on dur's own channel, creating it first if no caller has
// asked for dur yet.
func (d *discriminatingTimer) fire(dur time.Duration, t time.Time) {
	d.mu.Lock()
	ch, ok := d.chans[dur]
	if !ok {
		ch = make(chan time.Time, 1)
		d.chans[dur] = ch
	}
	d.mu.Unlock()
	ch <- t
}

// waitForTimerCalls blocks until dt has recorded n more After calls (any
// duration), or fails after 2s.
func waitForTimerCalls(t *testing.T, dt *discriminatingTimer, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		select {
		case <-dt.calls:
		case <-time.After(2 * time.Second):
			t.Fatalf("timed out waiting for timer call %d/%d", i+1, n)
		}
	}
}

// TestScheduler_Start_JoinsBootCatchUp closes the gap PR 3a's own Start
// comment disclosed: "the boot catch-up goroutine's own wg.Add(1)/Done()
// is PR 4's addition to this same group." Start must spawn both the cron
// and the catch-up goroutines into the SAME sync.WaitGroup, so Wait does
// not return early because only one of the two has unwound.
//
// JD-4-04 correction: the original version of this test used a fakeTimer
// that both goroutines were left blocked on forever (never fed), so after
// cancel() both goroutines returned via their own ctx.Done() branch almost
// immediately — the only assertion was that Wait returned before a 2s
// timeout, which holds whether or not the catch-up goroutine was ever
// added to s.wg. This version instead lets the cron goroutine unwind via
// ctx.Done() (its own wait duration is left unfired) while the catch-up
// goroutine's own wait is fired and routed into a real, deliberately
// blocked Consolidate call — so Wait cannot return until this test
// releases it, and a s.Wait call with a short bounded ctx, issued while
// the catch-up goroutine is still deterministically blocked inside
// Consolidate, must NOT return: it can only return early if the catch-up
// goroutine is missing from s.wg, exactly the bug this test exists to
// catch. Verified genuinely discriminating by a disclosed temporary probe:
// see this task's own evidence.
func TestScheduler_Start_JoinsBootCatchUp(t *testing.T) {
	dt := newDiscriminatingTimer()
	bc := newBlockingConsolidator()
	s, err := New(Deps{Clock: fixedClock{now: fixedNow}, Config: memrepo.NewConfig(), Consolidate: bc, Timer: dt})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	s.Start(ctx)

	// Both goroutines must have asked the timer for a wait — proof both
	// are actually running, not merely the cron loop alone.
	waitForTimerCalls(t, dt, 2)

	// Fire only the catch-up's own wait, BEFORE cancelling ctx. Its due
	// fire now routes through runPass into bc.Consolidate, which blocks
	// on bc.release until this test releases it — the catch-up goroutine
	// cannot reach its own deferred wg.Done() until then. Firing this
	// before cancel matters: cancelling ctx first would make the
	// catch-up goroutine's own select also pick its ctx.Done() branch
	// (the only ready case, since its timer channel would not have been
	// fired yet), defeating the whole point.
	dt.fire(BootConsolidationDelay, fixedNow)

	select {
	case <-bc.entered:
		// Deterministic checkpoint: the catch-up goroutine is now
		// synchronously blocked inside Consolidate (bc.release is not
		// yet closed), strictly before its own
		// runPass/runCatchUp/wg.Done() unwind in that same goroutine's
		// own program order — it cannot have called wg.Done() yet.
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for the catch-up's own Consolidate call to start")
	}

	// Cancel ctx now: the cron goroutine's own wait (its next-tick
	// duration) is never fired, so its own select picks the ctx.Done()
	// branch and it unwinds within microseconds — plain control flow and
	// one deferred wg.Done() call, no I/O. The catch-up goroutine is
	// unaffected: it is already past its own select, synchronously
	// blocked inside Consolidate, not waiting on ctx at all.
	cancel()

	// At this checkpoint Wait must NOT be able to return: the cron
	// goroutine has (or is about to have) unwound above, and if the
	// catch-up goroutine were not in s.wg, the group would already be
	// empty and Wait would return immediately regardless of the
	// catch-up goroutine's own still-blocked state. 200ms is far more
	// than the cron goroutine needs to unwind after cancel — this bound
	// exists only to prove the negative promptly, not as the
	// synchronization mechanism for the positive proof below.
	blockedCtx, blockedCancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer blockedCancel()
	s.Wait(blockedCtx)
	if blockedCtx.Err() == nil {
		t.Fatal("Wait returned while the catch-up goroutine was still blocked inside Consolidate — proof Wait is not actually joining the catch-up goroutine's own wg.Add(1)")
	}

	// Release the blocked call: the catch-up goroutine can now return
	// from Consolidate, unwind through runPass/runCatchUp, and reach its
	// own deferred wg.Done().
	close(bc.release)

	waitCtx, waitCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer waitCancel()
	s.Wait(waitCtx)

	if err := waitCtx.Err(); err != nil {
		t.Fatalf("Wait did not return before its own 2s ctx expired: %v", err)
	}
}
