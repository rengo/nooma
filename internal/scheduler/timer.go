package scheduler

import "time"

// timer is this package's own seam over time.After (design §5.2's "Why a
// package-local timer seam and not a widened ports.Clock"): ports.Clock is
// Now() and nothing else, implemented by every fake in the tree, and
// adding After would force each of them to change for this package alone.
// internal/scheduler is outside forbidigo's scope (.golangci.yml
// path-except: internal/core/), so realTimer below may call time.After
// directly. Fakes for this interface live in this package's own tests.
//
// task 3a.4: no meaningful red is possible for this file on its own — a
// bare interface trivially satisfied by both implementations has no
// failing behavior to assert. It is proven indirectly by the cron loop's
// own test (cron_test.go), the first real caller of realTimer's
// counterpart fake.
type timer interface {
	After(d time.Duration) <-chan time.Time
}

// realTimer is the seam's real implementation — the only place in this
// package that calls time.After.
type realTimer struct{}

func (realTimer) After(d time.Duration) <-chan time.Time {
	return time.After(d)
}
