package correction

import (
	"testing"
	"time"
)

// TestAllFields_EveryEditReportsExactlyOneAccessorTrue is design D3's own
// completeness table: for each Field AllFields lists, the Edit built by
// that field's constructor reports Field() equal to it, and exactly one of
// Content/EventAt/DueAt reports true — the shape that makes a crossed
// accessor a name mismatch a reader sees, not a silently wrong column.
func TestAllFields_EveryEditReportsExactlyOneAccessorTrue(t *testing.T) {
	when := time.Date(2026, 8, 15, 9, 0, 0, 0, time.UTC)

	tests := []struct {
		field       Field
		edit        Edit
		wantContent bool
		wantEventAt bool
		wantDueAt   bool
	}{
		{FieldContent, NewContentEdit("It's Ana, not Anna"), true, false, false},
		{FieldEventAt, NewEventAtEdit(when), false, true, false},
		{FieldDueAt, NewDueAtEdit(when), false, false, true},
	}

	seen := map[Field]bool{}
	for _, tt := range tests {
		seen[tt.field] = true

		if got := tt.edit.Field(); got != tt.field {
			t.Errorf("%s: Field() = %q, want %q", tt.field, got, tt.field)
		}

		_, gotContent := tt.edit.Content()
		if gotContent != tt.wantContent {
			t.Errorf("%s: Content() ok = %v, want %v", tt.field, gotContent, tt.wantContent)
		}
		_, gotEventAt := tt.edit.EventAt()
		if gotEventAt != tt.wantEventAt {
			t.Errorf("%s: EventAt() ok = %v, want %v", tt.field, gotEventAt, tt.wantEventAt)
		}
		_, gotDueAt := tt.edit.DueAt()
		if gotDueAt != tt.wantDueAt {
			t.Errorf("%s: DueAt() ok = %v, want %v", tt.field, gotDueAt, tt.wantDueAt)
		}

		trueCount := 0
		for _, ok := range []bool{gotContent, gotEventAt, gotDueAt} {
			if ok {
				trueCount++
			}
		}
		if trueCount != 1 {
			t.Errorf("%s: %d accessors report true, want exactly 1", tt.field, trueCount)
		}
	}

	all := AllFields()
	if len(all) != len(tests) {
		t.Fatalf("AllFields() has %d members, want %d — this test's own table is out of sync with the type", len(all), len(tests))
	}
	for _, f := range all {
		if !seen[f] {
			t.Errorf("AllFields() returned %q with no constructor exercised by this test", f)
		}
	}
}

// TestNewContentEdit_ValueRoundTrips confirms the constructed value survives
// the accessor unchanged — the boundary this test's sibling above does not
// check.
func TestNewContentEdit_ValueRoundTrips(t *testing.T) {
	want := "It's Ana, not Anna"
	got, ok := NewContentEdit(want).Content()
	if !ok || got != want {
		t.Errorf("Content() = (%q, %v), want (%q, true)", got, ok, want)
	}
}

func TestNewEventAtEdit_ValueRoundTrips(t *testing.T) {
	want := time.Date(2026, 8, 15, 9, 0, 0, 0, time.UTC)
	got, ok := NewEventAtEdit(want).EventAt()
	if !ok || !got.Equal(want) {
		t.Errorf("EventAt() = (%v, %v), want (%v, true)", got, ok, want)
	}
}

func TestNewDueAtEdit_ValueRoundTrips(t *testing.T) {
	want := time.Date(2026, 8, 20, 9, 0, 0, 0, time.UTC)
	got, ok := NewDueAtEdit(want).DueAt()
	if !ok || !got.Equal(want) {
		t.Errorf("DueAt() = (%v, %v), want (%v, true)", got, ok, want)
	}
}
