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

// TestScheduler_Start_JoinsBootCatchUp closes the gap PR 3a's own Start
// comment disclosed: "the boot catch-up goroutine's own wg.Add(1)/Done()
// is PR 4's addition to this same group." Start must spawn both the cron
// and the catch-up goroutines into the SAME sync.WaitGroup, so Wait does
// not return early because only one of the two has unwound.
func TestScheduler_Start_JoinsBootCatchUp(t *testing.T) {
	ft := newFakeTimer() // never fed — both goroutines block on it until ctx is done
	s, err := New(Deps{Clock: fixedClock{now: fixedNow}, Config: memrepo.NewConfig(), Consolidate: newRecordingConsolidator(), Timer: ft})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	s.Start(ctx)

	// Both goroutines must have asked the timer for a wait — proof both
	// are actually running, not merely the cron loop alone.
	waitForAfterCall(t, ft)
	waitForAfterCall(t, ft)

	cancel()

	waitCtx, waitCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer waitCancel()
	s.Wait(waitCtx)

	if err := waitCtx.Err(); err != nil {
		t.Fatalf("Wait did not return before its own 2s ctx expired: %v", err)
	}
}
