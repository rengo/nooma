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
    loc := time.FixedZone("UTC+3", 3*60*60)
    day := time.Date(2026, 8, 7, 0, 0, 0, 0, loc)

    for minute := 0; minute < 24*60; minute++ {
        instant := day.Add(time.Duration(minute) * time.Minute)
        want := instant.Hour() >= prospection.QuietHoursStartHour &&
            instant.Hour() < prospection.QuietHoursEndHour

        if got := prospection.InQuietHours(instant); got != want {
            t.Fatalf("InQuietHours(%v) = %v, want %v", instant, got, want)
        }
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
