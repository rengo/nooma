package scheduler

import (
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
