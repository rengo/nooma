// Package conformance — see test/conformance/doc.go for the package contract.
package conformance

import (
	"reflect"
	"testing"
	"time"

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
