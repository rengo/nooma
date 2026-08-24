// Package conformance — see test/conformance/doc.go for the package contract.
package conformance

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/brain"
	"github.com/rengo/nooma/internal/ports"
	"github.com/rengo/nooma/test/support/memrepo"

	"github.com/rengo/nooma/internal/core/prospection"
)

// TestI16_QuietHoursSweepMatchesHalfOpenWindow proves invariant I16
// (docs/06-harness.md §4, docs/02-cognitive-core.md §7): nothing is
// delivered during quiet hours, [QuietHoursStartHour, QuietHoursEndHour)
// local, swept minute by minute across the whole day rather than sampled
// at a few boundaries — the InQuietHours boundary table
// (internal/core/prospection/quiethours_test.go, L1) already pins the
// four edge cases; this test re-asserts the same property at L2,
// exhaustively, in a fixed non-UTC Location so no environment read can
// make it pass by accident.
func TestI16_QuietHoursSweepMatchesHalfOpenWindow(t *testing.T) {
	// The window is non-empty and does not cross midnight. Asserted before
	// the sweep, and separately from it, because the sweep alone cannot
	// catch this: recalibrating the start hour to 22 makes
	// `hour >= 22 && hour < 7` false for all 24 hours, so InQuietHours
	// would defer nothing, ever — I16 violated in production — while a
	// sweep that recomputes its own expectation from the same two
	// constants agrees with it at every minute and stays green. A guard
	// that cannot fail on its own violation is not a guard.
	//
	// Should quiet hours ever need to span midnight, this assertion is the
	// thing that must fail first, so the wrap-around is implemented rather
	// than silently producing an empty window. DeliverableFrom carries the
	// same assumption (it shifts within one calendar day).
	if prospection.QuietHoursStartHour >= prospection.QuietHoursEndHour {
		t.Fatalf("quiet hours [%d, %d) is empty or spans midnight; InQuietHours's "+
			"half-open comparison cannot express a wrap-around window and would "+
			"silently defer nothing",
			prospection.QuietHoursStartHour, prospection.QuietHoursEndHour)
	}

	loc := time.FixedZone("UTC+3", 3*60*60)
	day := time.Date(2026, 8, 7, 0, 0, 0, 0, loc)

	quiet := 0
	for minute := 0; minute < 24*60; minute++ {
		instant := day.Add(time.Duration(minute) * time.Minute)
		want := instant.Hour() >= prospection.QuietHoursStartHour &&
			instant.Hour() < prospection.QuietHoursEndHour

		got := prospection.InQuietHours(instant)
		if got != want {
			t.Fatalf("InQuietHours(%v) = %v, want %v", instant, got, want)
		}
		if got {
			quiet++
		}
	}

	// Counted, not recomputed from the predicate. Its honest scope: it does
	// not catch an implementation off-by-one (the minute comparison above
	// fails first on those), and it cannot catch a recalibration of either
	// constant, which moves both sides together. What it does catch is a
	// sweep that stopped covering the day it claims to cover — an anchor
	// that drifted off midnight, or a span no longer 24 hours — which the
	// minute-by-minute comparison cannot notice, because every minute it
	// still visits agrees with itself.
	if wantQuiet := (prospection.QuietHoursEndHour - prospection.QuietHoursStartHour) * 60; quiet != wantQuiet {
		t.Fatalf("%d minutes of the day are quiet, want %d for the window [%d, %d)",
			quiet, wantQuiet, prospection.QuietHoursStartHour, prospection.QuietHoursEndHour)
	}
}

// TestI16_InQuietHoursTakesNoKindParameter proves this PR's own half of
// "the timer is the only push exception" (design §3.2, decision C):
// InQuietHours structurally cannot special-case a timer, because it
// takes exactly one parameter, a time.Time, and nothing that could carry
// a classify.Kind or an Armament through which a caller could smuggle a
// timer-shaped exception into the gate. The composed proof — that a
// timer alone bypasses this gate — is PR 2's TimerVerdict test
// (internal/core/prospection/staleness_test.go, task 2.3, cross-referenced
// here); this is only the structural half PR 1 can state on its own.
func TestI16_InQuietHoursTakesNoKindParameter(t *testing.T) {
	fn := reflect.TypeOf(prospection.InQuietHours)

	if fn.Kind() != reflect.Func {
		t.Fatalf("prospection.InQuietHours is not a func, it is %v", fn.Kind())
	}
	if got := fn.NumIn(); got != 1 {
		t.Fatalf("InQuietHours takes %d parameters, want exactly 1 (now time.Time) — a "+
			"second parameter would be exactly where a caller could smuggle a kind-based "+
			"exception into the gate", got)
	}
	if got := fn.In(0); got != reflect.TypeOf(time.Time{}) {
		t.Fatalf("InQuietHours's one parameter is %v, want time.Time", got)
	}
}

// TestI16_DeliveryIsDeferredInQuietHoursExceptATimer is I16's behavioural
// half at the delivery layer — the pure gate is asserted above, this is
// the pass acting on it.
//
// **Swept, not sampled.** m3b's G16 and G22 were the same defect found
// twice: a boundary with two regimes, checked at one point. The sweep
// walks every hour of a day and asserts the trigger is delivered outside
// the window and not inside, and that the timer is delivered at every hour
// — I16's one exception, and the only one.
func TestI16_DeliveryIsDeferredInQuietHoursExceptATimer(t *testing.T) {
	sweptInside, sweptOutside := 0, 0

	for hour := 0; hour < 24; hour++ {
		now := time.Date(2026, 8, 5, hour, 30, 0, 0, time.UTC)

		// Due one minute ago at every swept hour, so the trigger is
		// always deliverable and never stale: the ONLY thing changing
		// across the sweep is whether the window is open. A fixed fire_at
		// would make the late hours fail on staleness, which is I15's
		// subject and not this one's — and the guard at the end is what
		// caught that when this test was first written that way.
		fireAt := now.Add(-time.Minute)

		t.Run(now.Format("15h"), func(t *testing.T) {
			triggers := memrepo.NewTriggers()
			timers := memrepo.NewTimers()
			ch := &countingChannel{}

			ctx := context.Background()
			at := fireAt
			level := 0.9
			if err := triggers.Create(ctx, ports.Trigger{
				ID: "trg", Kind: ports.TriggerKindTimeBased, FireAt: &at,
				InterruptLevel: &level, CreatedAt: fireAt.Add(-time.Hour),
			}); err != nil {
				t.Fatalf("Create trigger: %v", err)
			}
			// The timer is due at this hour, so it is never stale.
			if err := timers.Create(ctx, ports.Timer{
				ID: "tmr", FireAt: now.Add(-time.Minute), CreatedAt: fireAt,
			}); err != nil {
				t.Fatalf("Create timer: %v", err)
			}

			report, err := brain.NewCheckService(fixedClock{now: now}, triggers, timers, &counterIDs{}, memrepo.NewDecisionLog(), ch).
				Check(ctx, brain.CheckRequest{})
			if err != nil {
				t.Fatalf("Check: %v", err)
			}

			// The timer is the exception, at every hour.
			if report.TimersFired != 1 {
				t.Errorf("TimersFired = %d at %s, want 1 — a timer is the one push exception to quiet hours (doc 02 §7)",
					report.TimersFired, now.Format("15:04"))
			}

			if prospection.InQuietHours(now) {
				sweptInside++
				if report.TriggersDelivered != 0 {
					t.Errorf("a trigger was delivered at %s, inside quiet hours", now.Format("15:04"))
				}
				return
			}
			sweptOutside++
			if report.TriggersDelivered != 1 {
				t.Errorf("TriggersDelivered = %d at %s, outside quiet hours, want 1", report.TriggersDelivered, now.Format("15:04"))
			}
		})
	}

	if sweptInside == 0 || sweptOutside == 0 {
		t.Fatalf("the sweep covered %d hour(s) inside the window and %d outside — a sweep that only reaches one regime is a sample",
			sweptInside, sweptOutside)
	}
}

// countingChannel counts sends and nothing else.
type countingChannel struct{ sent int }

func (c *countingChannel) Name() string                                            { return "test" }
func (c *countingChannel) Receive(context.Context) ([]ports.ChannelMessage, error) { return nil, nil }
func (c *countingChannel) Confirm(context.Context, string) error                   { return nil }
func (c *countingChannel) Close() error                                            { return nil }
func (c *countingChannel) Send(context.Context, ports.ConversationID, string) error {
	c.sent++
	return nil
}
