package scheduler

import (
	"bytes"
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rengo/nooma/test/support/memrepo"
)

// countingChecker records how many proactive passes ran, and can be made
// to block so a second fire arrives while the first is still inside it.
type countingChecker struct {
	mu      sync.Mutex
	calls   int
	release chan struct{}
}

func (c *countingChecker) ProactiveCheck(context.Context) error {
	c.mu.Lock()
	c.calls++
	release := c.release
	c.mu.Unlock()

	if release != nil {
		<-release
	}
	return nil
}

func (c *countingChecker) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls
}

// lockedBuffer is a log a test can read while a goroutine writes it. The
// package's own logf holds logMu for its write, but a test reading the
// buffer takes no such lock — scheduler_test.go:495 records that as a real
// race under -race, not a theoretical one.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// newProactiveScheduler builds a Scheduler with both jobs wired and a
// discriminating timer, so a test can fire one loop's wait without
// touching the other's.
func newProactiveScheduler(t *testing.T, dt *discriminatingTimer, checker ProactiveChecker, log *lockedBuffer) *Scheduler {
	t.Helper()

	s, err := New(Deps{
		Clock:       fixedClock{now: fixedNow},
		Config:      memrepo.NewConfig(),
		Consolidate: newBlockingConsolidator(),
		Proactive:   checker,
		Timer:       dt,
		Log:         log,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return s
}

// waitUntil polls cond until it holds, or fails after 2s.
func waitUntil(t *testing.T, cond func() bool, msg string) {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal(msg)
}

// TestProactive_FiresOnItsOwnInterval is R1.1's first half.
func TestProactive_FiresOnItsOwnInterval(t *testing.T) {
	dt := newDiscriminatingTimer()
	checker := &countingChecker{}
	s := newProactiveScheduler(t, dt, checker, &lockedBuffer{})

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	done := make(chan struct{})
	go func() { s.runProactive(ctx); close(done) }()

	waitUntil(t, func() bool { return dt.chanFor(ProactiveCheckInterval) != nil },
		"the proactive loop never asked the timer for its own interval")
	dt.fire(ProactiveCheckInterval, fixedNow)

	waitUntil(t, func() bool { return checker.count() == 1 }, "the proactive checker never ran")
	cancel()
	<-done
}

// TestProactive_ASecondFireDuringAPassIsSkippedAndLogged mirrors the
// consolidation job's own guard behaviour.
func TestProactive_ASecondFireDuringAPassIsSkippedAndLogged(t *testing.T) {
	dt := newDiscriminatingTimer()
	checker := &countingChecker{release: make(chan struct{})}
	log := &lockedBuffer{}
	s := newProactiveScheduler(t, dt, checker, log)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	done := make(chan struct{})
	go func() { s.runProactive(ctx); close(done) }()

	waitUntil(t, func() bool { return dt.chanFor(ProactiveCheckInterval) != nil }, "no timer wait")
	dt.fire(ProactiveCheckInterval, fixedNow)
	waitUntil(t, func() bool { return checker.count() == 1 }, "the first pass never started")

	// The loop is blocked inside ProactiveCheck; fire again from outside,
	// which is what a second goroutine's tick would do.
	go s.runProactivePass(ctx)
	waitUntil(t, func() bool { return strings.Contains(log.String(), "skipped") },
		"the skipped fire was not logged")

	if got := checker.count(); got != 1 {
		t.Fatalf("the checker ran %d time(s), want 1 — a second pass entered while the first was running", got)
	}

	close(checker.release)
	cancel()
	<-done
}

// TestProactive_ARunningConsolidationDoesNotSkipACheck is the assertion
// that decides the design, and the one a shared guard fails.
//
// The two jobs run at wildly different cadences: consolidation is nightly
// and can take minutes; the proactive check is every five. Sharing one
// slot would let a single nightly pass suppress every check it overlaps —
// which for a long pass is the early morning, exactly when the items
// deferred through quiet hours are waiting to resurface.
func TestProactive_ARunningConsolidationDoesNotSkipACheck(t *testing.T) {
	dt := newDiscriminatingTimer()
	checker := &countingChecker{}
	s := newProactiveScheduler(t, dt, checker, &lockedBuffer{})

	// Hold the consolidation slot, as a running nightly pass would.
	s.slot <- struct{}{}
	t.Cleanup(func() { <-s.slot })

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	done := make(chan struct{})
	go func() { s.runProactive(ctx); close(done) }()

	waitUntil(t, func() bool { return dt.chanFor(ProactiveCheckInterval) != nil }, "no timer wait")
	dt.fire(ProactiveCheckInterval, fixedNow)

	waitUntil(t, func() bool { return checker.count() == 1 },
		"the proactive check was skipped while a consolidation held its slot — the two jobs share a guard, and a long nightly pass would suppress every check it overlaps")
	cancel()
	<-done
}

// TestProactive_IsOptional: a Scheduler with no ProactiveChecker starts and
// runs, because a vault that wants only consolidation is an ordinary vault.
func TestProactive_IsOptional(t *testing.T) {
	dt := newDiscriminatingTimer()
	s, err := New(Deps{
		Clock:       fixedClock{now: fixedNow},
		Config:      memrepo.NewConfig(),
		Consolidate: newBlockingConsolidator(),
		Timer:       dt,
	})
	if err != nil {
		t.Fatalf("New without a ProactiveChecker: %v — the proactive job is optional, unlike Consolidate", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	s.Start(ctx) // must not panic
	s.Wait(context.Background())
}
