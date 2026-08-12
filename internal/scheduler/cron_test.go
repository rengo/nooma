package scheduler

import (
	"context"
	"testing"
	"time"
)

// fakeTimer is this package's own timer seam test double (design §5.2):
// the real timer.go implementation calls time.After directly, and this
// fake lets a test control exactly when the cron loop's own wait fires,
// with no wall-clock sleep. fire is buffered so a test can send before the
// loop's own goroutine has reached its receive.
type fakeTimer struct {
	fire chan time.Time
}

func newFakeTimer() *fakeTimer {
	return &fakeTimer{fire: make(chan time.Time, 1)}
}

func (f *fakeTimer) After(time.Duration) <-chan time.Time {
	return f.fire
}

// callCount is recordingConsolidator's own read, used from this file
// onward (scheduler_test.go defines the type; this is its first caller).
func (c *recordingConsolidator) callCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.calls)
}

// waitForCall blocks until rc has recorded at least one call, or fails the
// test after 2s — long enough for a scheduled goroutine on a loaded CI
// runner, short enough that a genuine hang is still reported promptly.
func waitForCall(t *testing.T, rc *recordingConsolidator) {
	t.Helper()
	select {
	case <-rc.notify:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for the cron loop to call Consolidate")
	}
}

// TestCron_FiresAfterHourElapses is task 3a.5's first case: a fake
// clock/timer advanced past ConsolidationHour asserts exactly one
// whole-pass call (Phase == nil) to the fake Consolidator.
func TestCron_FiresAfterHourElapses(t *testing.T) {
	ft := newFakeTimer()
	rc := newRecordingConsolidator()
	s, err := New(Deps{Clock: fixedClock{now: fixedNow}, Config: validDeps().Config, Consolidate: rc, Timer: ft})
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

	// Advance the timer past ConsolidationHour.
	ft.fire <- fixedNow
	waitForCall(t, rc)

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("runCron did not return after ctx cancellation")
	}

	if got := rc.callCount(); got != 1 {
		t.Fatalf("Consolidate called %d times, want exactly 1", got)
	}
	if rc.calls[0].Phase != nil {
		t.Fatalf("Phase = %v, want nil (a whole pass)", rc.calls[0].Phase)
	}
}

// TestCron_NeverFiresBeforeCtxCancelled is task 3a.5's second case: a
// clock/timer that never reaches the hour asserts zero calls.
func TestCron_NeverFiresBeforeCtxCancelled(t *testing.T) {
	ft := newFakeTimer() // never fed — the loop's own select blocks forever on it
	rc := newRecordingConsolidator()
	s, err := New(Deps{Clock: fixedClock{now: fixedNow}, Config: validDeps().Config, Consolidate: rc, Timer: ft})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		s.runCron(ctx)
		close(done)
	}()

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("runCron did not return after ctx cancellation")
	}

	if got := rc.callCount(); got != 0 {
		t.Fatalf("Consolidate called %d times, want 0 — the timer never fired", got)
	}
}
