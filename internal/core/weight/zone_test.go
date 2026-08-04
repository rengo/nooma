package weight

import (
	"testing"

	"github.com/rengo/nooma/internal/core/unit"
)

// TestZoneOf_TotalOverStatusAndFocus proves R1.4: ZoneOf is a total pure
// function over unit.AllStatuses() x {true, false} matrix, matching doc 02
// §2's table exactly for pool and archived, and mapping superseded and
// incomplete — the two statuses doc 02 §2's table does not name — to
// ZoneCold (divergence C-a, resolved in design.md's favour: a deliberately
// untested arm is an uncovered statement, and the coverage floor is >= 90%).
//
// The table is driven by unit.AllStatuses() and asserts its own
// completeness against it, so a fifth status added later fails this test
// rather than silently falling through (design D2).
func TestZoneOf_TotalOverStatusAndFocus(t *testing.T) {
	want := map[unit.Status]map[bool]Zone{
		unit.StatusPool: {
			true:  ZoneHot,
			false: ZoneWarm,
		},
		unit.StatusArchived: {
			true:  ZoneCold,
			false: ZoneCold,
		},
		unit.StatusSuperseded: {
			true:  ZoneCold,
			false: ZoneCold,
		},
		unit.StatusIncomplete: {
			true:  ZoneCold,
			false: ZoneCold,
		},
	}

	statuses := unit.AllStatuses()
	if len(statuses) == 0 {
		t.Fatal("unit.AllStatuses() returned zero statuses — nothing to check")
	}
	if len(want) != len(statuses) {
		t.Fatalf("this test's own table covers %d statuses, unit.AllStatuses() has %d — table is out of sync", len(want), len(statuses))
	}

	for _, status := range statuses {
		byFocus, ok := want[status]
		if !ok {
			t.Fatalf("unit.AllStatuses() includes %q, which this test's table does not cover — table is out of sync", status)
		}
		for _, inFocus := range []bool{true, false} {
			got := ZoneOf(status, inFocus)
			if got != byFocus[inFocus] {
				t.Errorf("ZoneOf(%q, inFocus=%v) = %v, want %v", status, inFocus, got, byFocus[inFocus])
			}
		}
	}
}

// TestAllZones_ReturnsTheThreeZones proves AllZones() enumerates exactly
// ZoneCold, ZoneWarm and ZoneHot, IN THAT ORDER — the order its own doc
// comment promises ("in the order the constants above declare them") and
// design D2's declared const order. C3.2 (tasks.md) found this test's
// membership-only check let that promise go unenforced: want was already
// an ordered slice, which reads as though sequence were compared, but only
// a `seen` set was checked. A judge scrambled AllZones()'s returned
// literal and the suite passed. This version compares element by element.
func TestAllZones_ReturnsTheThreeZones(t *testing.T) {
	got := AllZones()
	want := []Zone{ZoneCold, ZoneWarm, ZoneHot}

	if len(got) != len(want) {
		t.Fatalf("AllZones() has %d members, want %d: %v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("AllZones()[%d] = %v, want %v — AllZones() promises the constants' declared order (design D2)", i, got[i], want[i])
		}
	}
}

// TestZone_ZeroValueIsCold pins the meaning of Zone's zero value, per C3.3
// (tasks.md): design D2 declares the const order Cold, Warm, Hot — the
// shipped code had it reversed, making the zero value (an unclassified or
// zero-initialized Zone) claim to be Hot, the state a unit has to earn,
// rather than Cold, the resting state decay carries things toward and the
// safer default for something nothing has classified yet. Zone is never
// persisted (doc 02 §2, I01) — this is guarding a Go-level default, not a
// migration or golden-file concern.
func TestZone_ZeroValueIsCold(t *testing.T) {
	var z Zone
	if z != ZoneCold {
		t.Errorf("the zero value of Zone is %v, want ZoneCold — an unclassified zone must default to the resting state, not the state a unit has to earn (design D2)", z)
	}
}

// TestZoneString_NamesEachZone proves Zone.String() gives every zone a
// distinct, readable name.
func TestZoneString_NamesEachZone(t *testing.T) {
	cases := []struct {
		zone Zone
		want string
	}{
		{ZoneHot, "hot"},
		{ZoneWarm, "warm"},
		{ZoneCold, "cold"},
	}
	seen := make(map[string]bool, len(cases))
	for _, c := range cases {
		got := c.zone.String()
		if got != c.want {
			t.Errorf("Zone(%d).String() = %q, want %q", c.zone, got, c.want)
		}
		if seen[got] {
			t.Errorf("Zone.String() = %q is not unique across zones", got)
		}
		seen[got] = true
	}
}

// TestZoneString_NamesAnUnknownZoneWithoutPanicking covers String's default
// arm, which TestZoneString_NamesEachZone cannot reach because it iterates
// only the three real members.
//
// The arm is not dead code waiting for a fourth zone: Zone's underlying type
// admits any int, so a value read back from a future store, a corrupted
// fixture, or an arithmetic slip reaches it. What matters is that such a
// value renders as something a reader recognises as wrong rather than as
// empty text — an empty string in a decision_log row reads as "no zone
// recorded", which is a different and more comforting claim than "a zone
// nothing can name".
func TestZoneString_NamesAnUnknownZoneWithoutPanicking(t *testing.T) {
	outside := Zone(len(AllZones()) + 1)

	got := outside.String()
	if got != "unknown" {
		t.Errorf("Zone(%d).String() = %q, want %q", outside, got, "unknown")
	}
	if got == "" {
		t.Error("Zone.String() returned empty text for an out-of-vocabulary zone; an empty name reads as 'no zone recorded' rather than as a fault")
	}
}
