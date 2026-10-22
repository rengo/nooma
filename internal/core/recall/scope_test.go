package recall

import (
	"slices"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/core/unit"
)

// TestPrecise: the ranked ids a term matched are the answer, in the ranking's
// order; when no term matched any, the ranking stands whole — the floor's
// own answer (ADR-0048). A term's hit the ranking does not hold is not
// admitted: the vector admits, and a term only narrows.
func TestPrecise(t *testing.T) {
	ranked := []string{"oculist", "dentist", "padel"}
	for _, tc := range []struct {
		name    string
		matched []string
		want    []string
	}{
		{"one matched", []string{"oculist"}, []string{"oculist"}},
		{"two matched, in the ranking's order", []string{"padel", "oculist"}, []string{"oculist", "padel"}},
		{"a hit the ranking does not hold", []string{"kinesiologist", "dentist"}, []string{"dentist"}},
		{"nothing matched", nil, ranked},
		{"only hits the ranking does not hold", []string{"kinesiologist"}, ranked},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := Precise(ranked, tc.matched); !slices.Equal(got, tc.want) {
				t.Errorf("Precise = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestWindowHolds: a told window reads created_at, a when window reads
// event_at and else due_at, from inclusive and to exclusive, and a unit with
// no such instant is outside every when window.
func TestWindowHolds(t *testing.T) {
	zone := time.FixedZone("UTC-3", -3*60*60)
	at := func(d, h int) *time.Time { t := time.Date(2026, 10, d, h, 0, 0, 0, zone); return &t }
	from, to := *at(21, 18), *at(26, 0)
	told := Window{On: WindowTold, From: from, To: to}
	when := Window{On: WindowWhen, From: from, To: to}
	for _, tc := range []struct {
		name       string
		u          unit.Unit
		told, when bool
	}{
		{"told at from, an event after to", unit.Unit{CreatedAt: from, EventAt: at(27, 9)}, true, false},
		{"told before from, an event inside", unit.Unit{CreatedAt: *at(19, 9), EventAt: at(23, 10)}, false, true},
		{"an event at to is outside", unit.Unit{CreatedAt: *at(19, 9), EventAt: &to}, false, false},
		{"told at to is outside", unit.Unit{CreatedAt: to}, false, false},
		{"a due day inside", unit.Unit{CreatedAt: *at(10, 9), DueAt: at(25, 0)}, false, true},
		{"the event wins over the due day", unit.Unit{CreatedAt: *at(10, 9), EventAt: at(20, 10), DueAt: at(25, 0)}, false, false},
		{"no instant at all", unit.Unit{CreatedAt: *at(10, 9)}, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := told.Holds(tc.u); got != tc.told {
				t.Errorf("told window holds = %v, want %v", got, tc.told)
			}
			if got := when.Holds(tc.u); got != tc.when {
				t.Errorf("when window holds = %v, want %v", got, tc.when)
			}
		})
	}
}

// TestRecallWindowLimitIsAShortList: a window answers in one chat message,
// listed by instant, so its bound is a list a person reads, not a page.
func TestRecallWindowLimitIsAShortList(t *testing.T) {
	if RecallWindowLimit < 5 || RecallWindowLimit > RecallTopK {
		t.Errorf("RecallWindowLimit = %d, want between 5 and RecallTopK (%d)", RecallWindowLimit, RecallTopK)
	}
}

// TestWindowInstant: which instant of a unit each WindowOn reads — the one a
// store orders a window's list by.
func TestWindowInstant(t *testing.T) {
	told := time.Date(2026, 10, 19, 10, 0, 0, 0, time.UTC)
	event, due := told.Add(48*time.Hour), told.Add(96*time.Hour)
	for _, tc := range []struct {
		on   WindowOn
		u    unit.Unit
		want *time.Time
	}{
		{WindowTold, unit.Unit{CreatedAt: told, EventAt: &event}, &told},
		{WindowWhen, unit.Unit{CreatedAt: told, EventAt: &event, DueAt: &due}, &event},
		{WindowWhen, unit.Unit{CreatedAt: told, DueAt: &due}, &due},
		{WindowWhen, unit.Unit{CreatedAt: told}, nil},
	} {
		got := Window{On: tc.on}.Instant(tc.u)
		if (got == nil) != (tc.want == nil) || got != nil && !got.Equal(*tc.want) {
			t.Errorf("%s window reads %v of %+v, want %v", tc.on, got, tc.u, tc.want)
		}
	}
}
