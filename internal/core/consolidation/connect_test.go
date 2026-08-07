package consolidation

import (
	"testing"
	"time"

	"github.com/rengo/nooma/internal/core/unit"
)

func mustParseTime(t *testing.T, s string) time.Time {
	t.Helper()
	tm, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatalf("parse %q: %v", s, err)
	}
	return tm
}

// TestSelectConnectSources_SinceNilTakesWholeLivePool proves R4.1: since ==
// nil (the first pass over an existing vault) makes every live source
// eligible, regardless of LastTouchedAt.
func TestSelectConnectSources_SinceNilTakesWholeLivePool(t *testing.T) {
	now := mustParseTime(t, "2026-01-10T00:00:00Z")
	sources := []Source{
		{UnitID: "a", Status: unit.StatusPool, Weight: 1.0, LastTouchedAt: mustParseTime(t, "2025-01-01T00:00:00Z")},
		{UnitID: "b", Status: unit.StatusPool, Weight: 2.0, LastTouchedAt: mustParseTime(t, "2026-01-09T00:00:00Z")},
		{UnitID: "c", Status: unit.StatusPool, Weight: 0.5, LastTouchedAt: mustParseTime(t, "2020-01-01T00:00:00Z")},
	}

	got := SelectConnectSources(sources, nil, now)
	if len(got) != 3 {
		t.Fatalf("SelectConnectSources() = %v, want all 3 live sources", got)
	}
}

// TestSelectConnectSources_NonLiveExcluded proves R4.1: a Source whose
// Status is not unit.StatusPool is never eligible, even with since == nil.
func TestSelectConnectSources_NonLiveExcluded(t *testing.T) {
	now := mustParseTime(t, "2026-01-10T00:00:00Z")
	sources := []Source{
		{UnitID: "live", Status: unit.StatusPool, Weight: 1.0, LastTouchedAt: now},
		{UnitID: "archived", Status: unit.StatusArchived, Weight: 5.0, LastTouchedAt: now},
		{UnitID: "superseded", Status: unit.StatusSuperseded, Weight: 5.0, LastTouchedAt: now},
		{UnitID: "incomplete", Status: unit.StatusIncomplete, Weight: 5.0, LastTouchedAt: now},
	}

	got := SelectConnectSources(sources, nil, now)
	if len(got) != 1 || got[0] != "live" {
		t.Fatalf("SelectConnectSources() = %v, want exactly [\"live\"]", got)
	}
}

// TestSelectConnectSources_TouchedBeforeSinceExcluded proves R4.1: with a
// non-nil since, a source is eligible only when LastTouchedAt is at or
// after since — a source touched strictly before since is excluded.
func TestSelectConnectSources_TouchedBeforeSinceExcluded(t *testing.T) {
	since := mustParseTime(t, "2026-01-05T00:00:00Z")
	now := mustParseTime(t, "2026-01-10T00:00:00Z")
	sources := []Source{
		{UnitID: "before", Status: unit.StatusPool, Weight: 1.0, LastTouchedAt: mustParseTime(t, "2026-01-04T23:59:59Z")},
		{UnitID: "at", Status: unit.StatusPool, Weight: 1.0, LastTouchedAt: since},
		{UnitID: "after", Status: unit.StatusPool, Weight: 1.0, LastTouchedAt: mustParseTime(t, "2026-01-06T00:00:00Z")},
	}

	got := SelectConnectSources(sources, &since, now)
	want := map[string]bool{"at": true, "after": true}
	if len(got) != len(want) {
		t.Fatalf("SelectConnectSources() = %v, want exactly %v", got, want)
	}
	for _, id := range got {
		if !want[id] {
			t.Errorf("SelectConnectSources() includes %q, which is touched before since", id)
		}
	}
}

// TestSelectConnectSources_OrderedByEffectiveDescendingTieByID proves R4.1's
// ordering: eligible sources are ranked by weight.Effective descending, ties
// broken by UnitID ascending — and the result is the SAME regardless of the
// input slice's own order (no accidental dependence on input order or map
// iteration, since SelectConnectSources ranks a slice, never a map).
func TestSelectConnectSources_OrderedByEffectiveDescendingTieByID(t *testing.T) {
	now := mustParseTime(t, "2026-01-10T00:00:00Z")
	// DecayRate 0 makes weight.Effective == Weight regardless of
	// LastTouchedAt, so the fixture's ranking is fully controlled by Weight.
	fixture := []Source{
		{UnitID: "z-tie", Status: unit.StatusPool, Weight: 2.0, LastTouchedAt: now},
		{UnitID: "a-tie", Status: unit.StatusPool, Weight: 2.0, LastTouchedAt: now},
		{UnitID: "highest", Status: unit.StatusPool, Weight: 5.0, LastTouchedAt: now},
		{UnitID: "lowest", Status: unit.StatusPool, Weight: 0.1, LastTouchedAt: now},
		{UnitID: "mid", Status: unit.StatusPool, Weight: 3.0, LastTouchedAt: now},
	}
	want := []string{"highest", "mid", "a-tie", "z-tie", "lowest"}

	forward := SelectConnectSources(fixture, nil, now)
	if !equalStrings(forward, want) {
		t.Fatalf("SelectConnectSources() = %v, want %v", forward, want)
	}

	reversed := make([]Source, len(fixture))
	for i, s := range fixture {
		reversed[len(fixture)-1-i] = s
	}
	got := SelectConnectSources(reversed, nil, now)
	if !equalStrings(got, want) {
		t.Fatalf("SelectConnectSources() over a reversed-order input = %v, want the same %v — ordering must not depend on input order",
			got, want)
	}
}

// TestSelectConnectSources_CappedAtConnectSourceLimit proves R4.1: the
// result never holds more than ConnectSourceLimit entries, and the entries
// kept are the highest-weighted ones.
func TestSelectConnectSources_CappedAtConnectSourceLimit(t *testing.T) {
	now := mustParseTime(t, "2026-01-10T00:00:00Z")
	total := ConnectSourceLimit + 5
	sources := make([]Source, total)
	for i := 0; i < total; i++ {
		sources[i] = Source{
			UnitID:        idFor(i),
			Status:        unit.StatusPool,
			Weight:        float64(total - i), // descending: id0 has the highest weight
			LastTouchedAt: now,
		}
	}

	got := SelectConnectSources(sources, nil, now)
	if len(got) != ConnectSourceLimit {
		t.Fatalf("SelectConnectSources() returned %d entries, want exactly ConnectSourceLimit (%d)", len(got), ConnectSourceLimit)
	}
	for i, id := range got {
		if id != idFor(i) {
			t.Errorf("position %d: got %q, want %q — the cap must keep the highest-weighted sources", i, id, idFor(i))
		}
	}
}

func idFor(i int) string {
	return "u" + string(rune('a'+i%26)) + string(rune('0'+i/26))
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
