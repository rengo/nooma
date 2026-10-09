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
	today := time.Date(2026, 10, 9, 10, 0, 0, 0, time.UTC)
	past := time.Date(2026, 10, 8, 10, 0, 0, 0, time.UTC)
	yearly := RuleYearly
	one := Live{ID: "a", FireAt: time.Date(2026, 10, 9, 10, 0, 0, 0, time.UTC)}
	two := Live{ID: "b", FireAt: time.Date(2026, 10, 12, 10, 0, 0, 0, time.UTC)}
	rec := Live{ID: "r", FireAt: time.Date(2026, 10, 20, 10, 0, 0, 0, time.UTC), Rule: &yearly}

	event := classify.KindEvent
	fresh := func(at time.Time) Plan {
		p, _ := Arm(classify.Classification{Kind: &event, EventAt: &at}, now)
		return p
	}

	t.Run("a date ahead moves the live trigger to what a fresh capture arms", func(t *testing.T) {
		want := Following{Plan: fresh(later), Carry: "a"}
		if got := Follow(later, []Live{one}, nil, now); !reflect.DeepEqual(got, want) {
			t.Errorf("Follow = %+v, want %+v", got, want)
		}
	})
	t.Run("a horizon already behind fires at once", func(t *testing.T) {
		got := Follow(today, []Live{one}, nil, now)
		if !got.Plan.Immediate || !got.Plan.FireAt.Equal(now) || got.Carry != "a" {
			t.Errorf("Follow = %+v, want carry a firing now", got)
		}
	})
	t.Run("with nothing live, the plan arms a new one", func(t *testing.T) {
		got := Follow(later, nil, nil, now)
		if got.Carry != "" || got.Plan.What != ArmTrigger {
			t.Errorf("Follow = %+v, want a new trigger", got)
		}
	})
	t.Run("a past date expires everything live and arms nothing", func(t *testing.T) {
		got := Follow(past, []Live{one, two}, nil, now)
		if got.Plan.What != ArmNothing || got.Carry != "" || !slices.Equal(got.Expire, []string{"a", "b"}) {
			t.Errorf("Follow = %+v, want a and b expired", got)
		}
	})
	t.Run("one live trigger carries the plan and the rest expire", func(t *testing.T) {
		got := Follow(later, []Live{one, two}, nil, now)
		if got.Carry != "a" || !slices.Equal(got.Expire, []string{"b"}) {
			t.Errorf("Follow = %+v, want a carried, b expired", got)
		}
	})
	t.Run("a recurring trigger stays recurring, re-anchored", func(t *testing.T) {
		got := Follow(past, []Live{one, rec}, nil, now)
		if got.Carry != "r" || got.Plan.What != ArmRecurring || got.Plan.Rule != RuleYearly ||
			got.Plan.Anchor.Month != time.October || got.Plan.Anchor.Day != 8 || !slices.Equal(got.Expire, []string{"a"}) {
			t.Errorf("Follow = %+v, want r carried as yearly on Oct 8, a expired", got)
		}
	})
	t.Run("a new trigger takes the given interrupt level", func(t *testing.T) {
		level := 0.9
		if got := Follow(later, nil, &level, now); got.Plan.Interrupt != ResolveInterrupt(&level) {
			t.Errorf("Interrupt = %+v, want the resolved %v", got.Plan.Interrupt, level)
		}
	})
}
