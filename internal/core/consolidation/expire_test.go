package consolidation

import (
	"testing"
	"time"

	"github.com/rengo/nooma/internal/core/unit"
)

// TestExpireIncomplete_ElapsedUnderExpiryHours_ProducesNoTransition proves
// R2.1's "nothing until 24h" half.
func TestExpireIncomplete_ElapsedUnderExpiryHours_ProducesNoTransition(t *testing.T) {
	now := time.Date(2026, 8, 7, 3, 0, 0, 0, time.UTC)
	us := []Incomplete{
		{UnitID: "u1", CreatedAt: now.Add(-(IncompleteExpiryHours - 1) * time.Hour), Unresolved: true},
	}

	got := ExpireIncomplete(us, now)
	if len(got) != 0 {
		t.Fatalf("ExpireIncomplete() = %v, want no transitions for elapsed < IncompleteExpiryHours", got)
	}
}

// TestExpireIncomplete_ElapsedExactlyExpiryHours_BothUnresolvedBranches is
// the C14 length guard: it must fail on the ≥24h fixture's length before
// any content assertion runs, against a nil stub.
func TestExpireIncomplete_ElapsedExactlyExpiryHours_BothUnresolvedBranches(t *testing.T) {
	now := time.Date(2026, 8, 7, 3, 0, 0, 0, time.UTC)

	tests := []struct {
		name       string
		unresolved bool
		wantTo     unit.Status
		wantReason Reason
	}{
		{"unresolved archives", true, unit.StatusArchived, ReasonIncompleteExpired},
		{"resolved promotes", false, unit.StatusPool, ReasonIncompletePromoted},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			us := []Incomplete{
				{UnitID: "u1", CreatedAt: now.Add(-IncompleteExpiryHours * time.Hour), Unresolved: tt.unresolved},
			}
			got := ExpireIncomplete(us, now)
			if len(got) != 1 {
				t.Fatalf("ExpireIncomplete() returned %d transitions, want 1", len(got))
			}
			want := Transition{UnitID: "u1", From: unit.StatusIncomplete, To: tt.wantTo, Reason: tt.wantReason}
			if got[0] != want {
				t.Errorf("ExpireIncomplete()[0] = %+v, want %+v", got[0], want)
			}
		})
	}
}

// TestExpireIncomplete_ElapsedOverExpiryHours_BothUnresolvedBranches proves
// the same predicate holds strictly past the boundary, not only at it.
func TestExpireIncomplete_ElapsedOverExpiryHours_BothUnresolvedBranches(t *testing.T) {
	now := time.Date(2026, 8, 7, 3, 0, 0, 0, time.UTC)

	tests := []struct {
		name       string
		unresolved bool
		wantTo     unit.Status
		wantReason Reason
	}{
		{"unresolved archives", true, unit.StatusArchived, ReasonIncompleteExpired},
		{"resolved promotes", false, unit.StatusPool, ReasonIncompletePromoted},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			us := []Incomplete{
				{UnitID: "u1", CreatedAt: now.Add(-48 * time.Hour), Unresolved: tt.unresolved},
			}
			got := ExpireIncomplete(us, now)
			if len(got) != 1 {
				t.Fatalf("ExpireIncomplete() returned %d transitions, want 1", len(got))
			}
			if got[0].To != tt.wantTo || got[0].Reason != tt.wantReason {
				t.Errorf("ExpireIncomplete()[0] = %+v, want To=%v Reason=%v", got[0], tt.wantTo, tt.wantReason)
			}
		})
	}
}

// TestExpireIncomplete_CreatedAtAfterNow_ClockSkewClampsToZero proves the
// same clamp-at-zero rule design.md §4.2 states: a unit that does not yet
// exist has waited no time.
func TestExpireIncomplete_CreatedAtAfterNow_ClockSkewClampsToZero(t *testing.T) {
	now := time.Date(2026, 8, 7, 3, 0, 0, 0, time.UTC)
	us := []Incomplete{
		{UnitID: "u1", CreatedAt: now.Add(time.Hour), Unresolved: true},
	}

	got := ExpireIncomplete(us, now)
	if len(got) != 0 {
		t.Fatalf("ExpireIncomplete() = %v, want no transitions when CreatedAt is after now", got)
	}
}

// TestExpireIncomplete_OutputSortedByUnitID is the mutation guard against a
// missing sort: inputs are handed in reverse order.
func TestExpireIncomplete_OutputSortedByUnitID(t *testing.T) {
	now := time.Date(2026, 8, 7, 3, 0, 0, 0, time.UTC)
	us := []Incomplete{
		{UnitID: "charlie", CreatedAt: now.Add(-48 * time.Hour)},
		{UnitID: "alice", CreatedAt: now.Add(-48 * time.Hour)},
		{UnitID: "bob", CreatedAt: now.Add(-48 * time.Hour)},
	}

	got := ExpireIncomplete(us, now)
	if len(got) != 3 {
		t.Fatalf("ExpireIncomplete() returned %d transitions, want 3", len(got))
	}
	want := []string{"alice", "bob", "charlie"}
	for i, id := range want {
		if got[i].UnitID != id {
			t.Fatalf("ExpireIncomplete()[%d].UnitID = %q, want %q — output must be sorted", i, got[i].UnitID, id)
		}
	}
}
