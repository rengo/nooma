package scheduler

import (
	"bytes"
	"context"
	"sync"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/brain"
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
