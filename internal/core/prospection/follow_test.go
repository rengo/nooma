package prospection

import (
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/core/classify"
)

func TestFollow(t *testing.T) {
	now := time.Date(2026, 10, 9, 8, 0, 0, 0, time.UTC)
	later := time.Date(2026, 10, 30, 10, 0, 0, 0, time.UTC)
	soon := time.Date(2026, 10, 9, 9, 0, 0, 0, time.UTC) // both leads behind, the event ahead
	past := time.Date(2026, 10, 8, 10, 0, 0, 0, time.UTC)
	yearly := RuleYearly
	one := Live{ID: "a", FireAt: time.Date(2026, 10, 9, 10, 0, 0, 0, time.UTC)}
	two := Live{ID: "b", FireAt: time.Date(2026, 10, 12, 10, 0, 0, 0, time.UTC)}
	three := Live{ID: "c", FireAt: time.Date(2026, 10, 13, 10, 0, 0, 0, time.UTC)}
	rec := Live{ID: "r", FireAt: time.Date(2026, 10, 20, 10, 0, 0, 0, time.UTC), Rule: &yearly}
	prefs := DefaultReminderPrefs()

	event := classify.KindEvent
	fresh := func(at time.Time) []Plan {
		p, _ := Arm(classify.Classification{Kind: &event, EventAt: &at}, prefs, now)
		return p
	}

	t.Run("a date ahead moves the live triggers onto what a fresh capture arms, in order", func(t *testing.T) {
		want := Following{Plans: fresh(later), Carry: []string{"a", "b"}}
		if got := Follow(later, []Live{one, two}, prefs, nil, now); !reflect.DeepEqual(got, want) {
			t.Errorf("Follow = %+v, want %+v", got, want)
		}
	})
	t.Run("a missing reminder is created beside the carried one", func(t *testing.T) {
		got := Follow(later, []Live{one}, prefs, nil, now)
		if len(got.Plans) != 2 || !slices.Equal(got.Carry, []string{"a", ""}) || len(got.Expire) != 0 {
			t.Errorf("Follow = %+v, want a carried onto the first lead and the second created", got)
		}
	})
	t.Run("with nothing live, the whole set is created", func(t *testing.T) {
		got := Follow(later, nil, prefs, nil, now)
		if !reflect.DeepEqual(got.Plans, fresh(later)) || !slices.Equal(got.Carry, []string{"", ""}) {
			t.Errorf("Follow = %+v, want two new triggers", got)
		}
	})
	t.Run("surplus live triggers expire", func(t *testing.T) {
		got := Follow(later, []Live{one, two, three}, prefs, nil, now)
		if !slices.Equal(got.Carry, []string{"a", "b"}) || !slices.Equal(got.Expire, []string{"c"}) {
			t.Errorf("Follow = %+v, want a and b carried, c expired", got)
		}
	})
	t.Run("every lead behind: one live trigger is pulled to now, the rest expire", func(t *testing.T) {
		got := Follow(soon, []Live{one, two}, prefs, nil, now)
		if len(got.Plans) != 1 || !got.Plans[0].Immediate || !got.Plans[0].FireAt.Equal(now) ||
			!slices.Equal(got.Carry, []string{"a"}) || !slices.Equal(got.Expire, []string{"b"}) {
			t.Errorf("Follow = %+v, want a firing now, b expired", got)
		}
	})
	t.Run("a correction never creates an at-once reminder", func(t *testing.T) {
		got := Follow(soon, nil, prefs, nil, now)
		if len(got.Plans) != 1 || got.Plans[0].What != ArmNothing || len(got.Carry) != 1 || got.Carry[0] != "" {
			t.Errorf("Follow = %+v, want nothing armed", got)
		}
	})
	t.Run("an at-once reminder already due keeps its instant", func(t *testing.T) {
		due := Live{ID: "a", FireAt: now.Add(-time.Minute)}
		got := Follow(soon, []Live{due}, prefs, nil, now)
		if !got.Plans[0].FireAt.Equal(due.FireAt) || got.Carry[0] != "a" {
			t.Errorf("Follow = %+v, want a kept at %v", got, due.FireAt)
		}
	})
	t.Run("a past date expires everything live and arms nothing", func(t *testing.T) {
		got := Follow(past, []Live{one, two}, prefs, nil, now)
		if got.Plans[0].What != ArmNothing || got.Carry[0] != "" || !slices.Equal(got.Expire, []string{"a", "b"}) {
			t.Errorf("Follow = %+v, want a and b expired", got)
		}
	})
	t.Run("a recurring trigger stays recurring, re-anchored", func(t *testing.T) {
		got := Follow(past, []Live{one, rec}, prefs, nil, now)
		p := got.Plans[0]
		if len(got.Plans) != 1 || got.Carry[0] != "r" || p.What != ArmRecurring || p.Rule != RuleYearly ||
			p.Anchor.Month != time.October || p.Anchor.Day != 8 || !slices.Equal(got.Expire, []string{"a"}) {
			t.Errorf("Follow = %+v, want r carried as yearly on Oct 8, a expired", got)
		}
	})
	t.Run("a new trigger takes the given interrupt level", func(t *testing.T) {
		level := 0.9
		if got := Follow(later, nil, prefs, &level, now); got.Plans[1].Interrupt != ResolveInterrupt(&level) {
			t.Errorf("Interrupt = %+v, want the resolved %v", got.Plans[1].Interrupt, level)
		}
	})
}
