package telegram

import "time"

// pollBackoffBase is how long the channel waits after its first
// consecutive polling failure.
//
// One second, derived from what the failure usually is: a dropped
// connection or a brief 5xx, both of which clear in well under a second.
// Waiting longer would make an ordinary blip look like an outage; waiting
// less would retry before the network has finished failing.
const pollBackoffBase = time.Second

// pollBackoffMax is the ceiling.
//
// Five minutes, derived from the other end: a channel that backs off past
// a few minutes is indistinguishable from a stopped one to a user who just
// sent a message, and the whole point of the transport is that a person is
// waiting on the other side. Long enough to stop hammering a real outage,
// short enough that recovery is not something anyone has to notice.
const pollBackoffMax = 5 * time.Minute

// backoffFor returns how long to wait after n consecutive failures.
//
// A pure function of the count, so the loop holds no backoff state beyond
// a counter and this is testable with no clock at all.
//
// The doubling is computed with an explicit guard rather than as
// base << n, and the guard is not defensive padding: time.Duration is an
// int64 of nanoseconds, so shifting a one-second base wraps NEGATIVE
// around the fortieth failure. A negative sleep returns immediately, which
// converts the backoff into a busy loop against an API that is already
// failing — the exact opposite of its purpose, at the moment it matters
// most.
func backoffFor(n int) time.Duration {
	if n < 0 {
		return pollBackoffBase
	}

	wait := pollBackoffBase
	for i := 0; i < n; i++ {
		// Check before doubling, so the overflow never happens rather
		// than being detected after it does.
		if wait >= pollBackoffMax/2 {
			return pollBackoffMax
		}
		wait *= 2
	}
	if wait > pollBackoffMax {
		return pollBackoffMax
	}
	return wait
}
