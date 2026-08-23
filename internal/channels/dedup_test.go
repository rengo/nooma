package channels

import (
	"fmt"
	"testing"
)

// TestDedupRing_RemembersItsWindowAndNoMore is the owner's 2026-08-23
// ruling on Q1, made checkable.
//
// A redelivered update must not become a second unit. The ruling was
// bounded in-memory deduplication, and "bounded" is the half a test has to
// carry: an unbounded map satisfies every behavioural assertion below and
// leaks for the process's whole life, which for a polling loop means
// forever.
func TestDedupRing_RemembersItsWindowAndNoMore(t *testing.T) {
	ring := newDedupRing(4)

	t.Run("a fresh id is not seen, and is after it is marked", func(t *testing.T) {
		if ring.seen("a") {
			t.Fatal("a fresh id reports itself already seen")
		}
		ring.mark("a")
		if !ring.seen("a") {
			t.Fatal("a marked id does not report itself seen — a redelivery would capture twice")
		}
	})

	t.Run("marking is idempotent", func(t *testing.T) {
		r := newDedupRing(4)
		r.mark("x")
		r.mark("x")
		r.mark("x")
		if got := r.len(); got != 1 {
			t.Fatalf("len = %d after marking one id three times, want 1 — a redelivered id must not consume the window", got)
		}
	})

	t.Run("the oldest id is evicted first", func(t *testing.T) {
		r := newDedupRing(2)
		r.mark("first")
		r.mark("second")
		r.mark("third")

		if r.seen("first") {
			t.Error("the oldest id survived past the window")
		}
		for _, id := range []string{"second", "third"} {
			if !r.seen(id) {
				t.Errorf("%q was evicted before the oldest", id)
			}
		}
	})

	t.Run("it does not grow past its window", func(t *testing.T) {
		const window = 8
		r := newDedupRing(window)

		// Ten windows' worth. An unbounded map passes every other case in
		// this file and fails only here, which is why this case exists.
		for i := 0; i < window*10; i++ {
			r.mark(fmt.Sprintf("update-%d", i))
		}
		if got := r.len(); got > window {
			t.Fatalf("len = %d after %d ids, want at most the window %d — an unbounded set is a leak for the life of the process", got, window*10, window)
		}
	})
}

// TestDedupRing_ZeroWindowRemembersNothing: a misconfigured window must
// degrade to "no deduplication" rather than to a panic or to a ring that
// remembers one thing by accident.
func TestDedupRing_ZeroWindowRemembersNothing(t *testing.T) {
	r := newDedupRing(0)
	r.mark("a")
	if r.seen("a") {
		t.Fatal("a zero-window ring remembered an id")
	}
}
