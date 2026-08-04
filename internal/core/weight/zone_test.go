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
// ZoneHot, ZoneWarm and ZoneCold.
func TestAllZones_ReturnsTheThreeZones(t *testing.T) {
	got := AllZones()
	want := []Zone{ZoneHot, ZoneWarm, ZoneCold}

	if len(got) != len(want) {
		t.Fatalf("AllZones() has %d members, want %d: %v", len(got), len(want), got)
	}
	seen := make(map[Zone]bool, len(got))
	for _, z := range got {
		seen[z] = true
	}
	for _, z := range want {
		if !seen[z] {
			t.Errorf("AllZones() is missing %v", z)
		}
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
