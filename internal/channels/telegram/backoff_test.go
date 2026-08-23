package telegram

import (
	"testing"
	"time"
)

// TestBackoffFor_IsBoundedAtEveryFailureCount is R4.2.
//
// backoffFor is a pure function of the failure count, so it is tested as
// one: no clock, no sleeping, no goroutine. What a loop does with the
// duration is the loop's own test.
//
// The overflow leg is the one worth writing. The obvious implementation is
// base << n, and doubling a time.Duration — an int64 of nanoseconds — wraps
// negative somewhere around the fortieth failure. A negative sleep returns
// immediately, which turns a backoff into a **busy loop against a failing
// API**: the exact opposite of what it exists for, at the exact moment it
// is needed most. A three-value table never reaches it.
func TestBackoffFor_IsBoundedAtEveryFailureCount(t *testing.T) {
	if got := backoffFor(0); got != pollBackoffBase {
		t.Errorf("backoffFor(0) = %s, want the base %s", got, pollBackoffBase)
	}
	if got := backoffFor(1); got != 2*pollBackoffBase {
		t.Errorf("backoffFor(1) = %s, want %s", got, 2*pollBackoffBase)
	}

	for n := 0; n <= 64; n++ {
		got := backoffFor(n)
		switch {
		case got <= 0:
			t.Fatalf("backoffFor(%d) = %s — a non-positive backoff is a busy loop against an API that is already failing", n, got)
		case got > pollBackoffMax:
			t.Fatalf("backoffFor(%d) = %s, above the ceiling %s", n, got, pollBackoffMax)
		case got < pollBackoffBase:
			t.Fatalf("backoffFor(%d) = %s, below the base %s", n, got, pollBackoffBase)
		}
	}
}

// TestBackoffFor_ReachesTheCeilingAndStaysThere: a backoff that grew
// forever would be indistinguishable from a stopped channel.
func TestBackoffFor_ReachesTheCeilingAndStaysThere(t *testing.T) {
	reached := false
	for n := 0; n <= 64; n++ {
		if backoffFor(n) == pollBackoffMax {
			reached = true
			break
		}
	}
	if !reached {
		t.Fatalf("backoffFor never reaches the ceiling %s within 64 failures — a channel that backs off past it is indistinguishable from a stopped one", pollBackoffMax)
	}
}

// TestBackoffFor_IsMonotonic: each step is at least as long as the one
// before. A sequence that dipped would hammer the API mid-outage.
func TestBackoffFor_IsMonotonic(t *testing.T) {
	prev := time.Duration(0)
	for n := 0; n <= 64; n++ {
		got := backoffFor(n)
		if got < prev {
			t.Fatalf("backoffFor(%d) = %s, shorter than backoffFor(%d) = %s", n, got, n-1, prev)
		}
		prev = got
	}
}
