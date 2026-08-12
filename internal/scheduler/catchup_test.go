package scheduler

import (
	"context"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/ports"
	"github.com/rengo/nooma/test/support/memrepo"
)

// staleLastRun is this file's own fixture instant: fixedNow minus more
// than CatchUpStalenessHours (24h), so consolidation.CatchUpDue reads it
// as due against fixedNow.
var staleLastRun = fixedNow.Add(-25 * time.Hour)

// TestCatchUp_FiresAfterDelay is task 4.1: CatchUpDue evaluates true at
// boot (a lastRunAt older than CatchUpStalenessHours) — Consolidate is not
// called before BootConsolidationDelay elapses, and is called once after,
// via a fake clock/timer. Spec R2.3; design §5.1, §5.3.
//
// Red: undefined: scheduler.BootConsolidationDelay / catchup.go's
// goroutine — package does not compile.
func TestCatchUp_FiresAfterDelay(t *testing.T) {
	ft := newFakeTimer()
	rc := newRecordingConsolidator()
	cfg := memrepo.NewConfig()
	cfg.SeedConfig(t, ports.VaultConfig{ConsolidationLastRunAt: &staleLastRun})

	s, err := New(Deps{Clock: fixedClock{now: fixedNow}, Config: cfg, Consolidate: rc, Timer: ft})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	done := make(chan struct{})
	go func() {
		s.runCatchUp(ctx)
		close(done)
	}()

	waitForAfterCall(t, ft) // the catch-up goroutine is now blocked in its delay wait

	if got := rc.callCount(); got != 0 {
		t.Fatalf("Consolidate called %d times before the delay elapsed, want 0", got)
	}

	ft.fire <- fixedNow
	waitForCall(t, rc)

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("runCatchUp did not return after firing")
	}

	if got := rc.callCount(); got != 1 {
		t.Fatalf("Consolidate called %d times, want exactly 1", got)
	}
	if rc.calls[0].Phase != nil {
		t.Fatalf("Phase = %v, want nil (a whole pass)", rc.calls[0].Phase)
	}
}

// TestCatchUp_GatedOff_ZeroCallsEvenOnStaleVault is task 4.3:
// ConsolidationEnabled = &false plus a 30-day-stale lastRunAt asserts zero
// Consolidate calls — owner ruling round 1 #1 (D3): consolidation_enabled
// gates the boot catch-up too, not only the cron. Spec R2.4.
//
// Red: genuinely red against this file's own committed runCatchUp — it
// checks only CatchUpDue, with no consolidation_enabled gate at all, so a
// disabled vault with a 30-day-stale lastRunAt still fires.
func TestCatchUp_GatedOff_ZeroCallsEvenOnStaleVault(t *testing.T) {
	ft := newFakeTimer()
	rc := newRecordingConsolidator()
	cfg := memrepo.NewConfig()
	disabled := false
	thirtyDaysStale := fixedNow.Add(-30 * 24 * time.Hour)
	cfg.SeedConfig(t, ports.VaultConfig{
		ConsolidationEnabled:   &disabled,
		ConsolidationLastRunAt: &thirtyDaysStale,
	})

	s, err := New(Deps{Clock: fixedClock{now: fixedNow}, Config: cfg, Consolidate: rc, Timer: ft})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	done := make(chan struct{})
	go func() {
		s.runCatchUp(ctx)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("runCatchUp did not return — a gated-off catch-up must be a true no-op, never reaching the delay")
	}

	if got := rc.callCount(); got != 0 {
		t.Fatalf("Consolidate called %d times, want 0 — ConsolidationEnabled is false", got)
	}
}

// TestCatchUp_CancelledDelayNeverFires is task 4.5: ctx cancelled before
// BootConsolidationDelay elapses asserts Consolidate is never called.
// Spec R2.3 (second Verified-by clause).
//
// Disclosed, not a genuine red: runCatchUp's select already carries the
// ctx.Done() arm since its first committed shape (task 4.2's own commit),
// so this test passes on first run — confirmatory rather than a fresh
// failure, the same posture task 4.5's own text discloses.
func TestCatchUp_CancelledDelayNeverFires(t *testing.T) {
	ft := newFakeTimer()
	rc := newRecordingConsolidator()
	cfg := memrepo.NewConfig()
	cfg.SeedConfig(t, ports.VaultConfig{ConsolidationLastRunAt: &staleLastRun})

	s, err := New(Deps{Clock: fixedClock{now: fixedNow}, Config: cfg, Consolidate: rc, Timer: ft})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		s.runCatchUp(ctx)
		close(done)
	}()

	waitForAfterCall(t, ft) // the catch-up goroutine is now blocked in its delay wait
	cancel()                // cancelled before the delay ever elapses — ft.fire is never fed

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("runCatchUp did not return after ctx cancellation")
	}

	if got := rc.callCount(); got != 0 {
		t.Fatalf("Consolidate called %d times, want 0 — ctx was cancelled before the delay elapsed", got)
	}
}
