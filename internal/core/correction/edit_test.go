package correction

import (
	"slices"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/core/unit"
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
		wantTitle   bool
		wantLeads   bool
	}{
		{FieldContent, NewContentEdit("It's Ana, not Anna"), true, false, false, false, false},
		{FieldEventAt, NewEventAtEdit(when), false, true, false, false, false},
		{FieldDueAt, NewDueAtEdit(when), false, false, true, false, false},
		{FieldTitle, NewTitleEdit("Reunión con Juan"), false, false, false, true, false},
		{FieldReminderLeads, NewReminderLeadsEdit([]int{60}), false, false, false, false, true},
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

		_, gotTitle := tt.edit.Title()
		if gotTitle != tt.wantTitle {
			t.Errorf("%s: Title() ok = %v, want %v", tt.field, gotTitle, tt.wantTitle)
		}

		_, gotLeads := tt.edit.ReminderLeads()
		if gotLeads != tt.wantLeads {
			t.Errorf("%s: ReminderLeads() ok = %v, want %v", tt.field, gotLeads, tt.wantLeads)
		}

		trueCount := 0
		for _, ok := range []bool{gotContent, gotEventAt, gotDueAt, gotTitle, gotLeads} {
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

func TestChanges(t *testing.T) {
	at := time.Date(2026, 12, 20, 9, 0, 0, 0, time.UTC)
	due := time.Date(2026, 12, 21, 9, 0, 0, 0, time.UTC)
	u := unit.Unit{Content: "body", Title: "Name", EventAt: &at, DueAt: &due}
	otherZone := at.In(time.FixedZone("ART", -3*60*60)) // the same instant
	later := at.Add(time.Hour)

	tests := []struct {
		name string
		plan []Edit
		u    unit.Unit
		want int
	}{
		{"same content", []Edit{NewContentEdit("body")}, u, 0},
		{"new content", []Edit{NewContentEdit("other")}, u, 1},
		{"same event_at in another frame", []Edit{NewEventAtEdit(otherZone)}, u, 0},
		{"new event_at", []Edit{NewEventAtEdit(later)}, u, 1},
		{"same due_at", []Edit{NewDueAtEdit(due)}, u, 0},
		{"new due_at", []Edit{NewDueAtEdit(later)}, u, 1},
		{"a date the unit lacks", []Edit{NewEventAtEdit(at)}, unit.Unit{Content: "body"}, 1},
		{"a due_at the unit lacks", []Edit{NewDueAtEdit(due)}, unit.Unit{Content: "body"}, 1},
		{"same title", []Edit{NewTitleEdit("Name")}, u, 0},
		{"new title", []Edit{NewTitleEdit("Other")}, u, 1},
		{"clearing a title the unit has", []Edit{NewTitleEdit("")}, u, 1},
		{"clearing a title the unit lacks", []Edit{NewTitleEdit("")}, unit.Unit{Content: "body"}, 0},
		{"only the edits that change", []Edit{NewEventAtEdit(at), NewContentEdit("other")}, u, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Changes(tt.plan, tt.u); len(got) != tt.want {
				t.Errorf("Changes() = %+v, want %d edit(s)", got, tt.want)
			}
		})
	}
}

func TestNewReminderLeadsEdit_ValueRoundTrips(t *testing.T) {
	got, ok := NewReminderLeadsEdit([]int{1440, 60}).ReminderLeads()
	if !ok || !slices.Equal(got, []int{1440, 60}) {
		t.Errorf("ReminderLeads() = (%v, %v), want ([1440 60], true)", got, ok)
	}
}

// TestChanges_ReminderLeads: the same leads again are no change; other leads,
// or leads where the unit followed the preferences, are.
func TestChanges_ReminderLeads(t *testing.T) {
	own := unit.Unit{ReminderLeads: []int{60}}
	for _, tc := range []struct {
		name string
		u    unit.Unit
		edit []int
		want int
	}{
		{"same leads", own, []int{60}, 0},
		{"other leads", own, []int{120}, 1},
		{"leads where the unit had none", unit.Unit{}, []int{60}, 1},
	} {
		if got := len(Changes([]Edit{NewReminderLeadsEdit(tc.edit)}, tc.u)); got != tc.want {
			t.Errorf("%s: %d changes, want %d", tc.name, got, tc.want)
		}
	}
}
