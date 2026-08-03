package correction

import (
	"testing"
	"time"

	"github.com/rengo/nooma/internal/core/classify"
)

func strPtr(s string) *string        { return &s }
func timePtr(t time.Time) *time.Time { return &t }

// TestPlanEdit drives every row of R1.8's table — design D3.
func TestPlanEdit(t *testing.T) {
	event := time.Date(2026, 8, 15, 9, 0, 0, 0, time.UTC)
	due := time.Date(2026, 8, 20, 9, 0, 0, 0, time.UTC)
	content := "It's Ana, not Anna"

	tests := []struct {
		name string
		c    classify.Classification
		want []Edit
		ok   bool
	}{
		{
			name: "event_at present, due_at absent -> event_at only",
			c: classify.Classification{
				EventAt:           timePtr(event),
				NormalizedContent: strPtr(content),
			},
			want: []Edit{NewEventAtEdit(event)},
			ok:   true,
		},
		{
			name: "due_at present, event_at absent -> due_at only",
			c: classify.Classification{
				DueAt:             timePtr(due),
				NormalizedContent: strPtr(content),
			},
			want: []Edit{NewDueAtEdit(due)},
			ok:   true,
		},
		{
			name: "neither date, content survived -> content only",
			c: classify.Classification{
				NormalizedContent: strPtr(content),
			},
			want: []Edit{NewContentEdit(content)},
			ok:   true,
		},
		{
			name: "both dates present -> ask",
			c: classify.Classification{
				EventAt:           timePtr(event),
				DueAt:             timePtr(due),
				NormalizedContent: strPtr(content),
			},
			want: nil,
			ok:   false,
		},
		{
			name: "no date, no content -> ask",
			c:    classify.Classification{},
			want: nil,
			ok:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := PlanEdit(tt.c)
			if ok != tt.ok {
				t.Fatalf("PlanEdit() ok = %v, want %v", ok, tt.ok)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("PlanEdit() = %d edits, want %d", len(got), len(tt.want))
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("edit[%d] = %+v, want %+v", i, got[i], tt.want[i])
				}
			}
		})
	}
}

// TestPlanEdit_DateCorrectionLeavesContentByteForByteUntouched is R1.8's
// explicit scenario and its own MUST NOT: content is never written on a
// correction that also resolved a date, even though normalized_content
// survived decoding alongside it.
func TestPlanEdit_DateCorrectionLeavesContentByteForByteUntouched(t *testing.T) {
	event := time.Date(2026, 8, 15, 9, 0, 0, 0, time.UTC)
	c := classify.Classification{
		EventAt:           timePtr(event),
		NormalizedContent: strPtr("It's Ana, not Anna"),
	}

	got, ok := PlanEdit(c)
	if !ok {
		t.Fatalf("PlanEdit() ok = false, want true")
	}
	if len(got) != 1 {
		t.Fatalf("PlanEdit() = %d edits, want 1", len(got))
	}
	if _, isContent := got[0].Content(); isContent {
		t.Fatalf("plan wrote content on a date-resolved correction — R1.8's own MUST NOT")
	}
	gotEvent, isEvent := got[0].EventAt()
	if !isEvent || !gotEvent.Equal(event) {
		t.Fatalf("plan did not write event_at = %v (isEvent=%v, got=%v)", event, isEvent, gotEvent)
	}
}

// TestPlanEdit_ReturnedSliceHoldsAtMostOneElement pins D3's own stated
// invariant — "the plan holds exactly one element" — in this package's own
// L1 table, per D3's stated reason: so a future ruling can be read and
// changed in one place.
func TestPlanEdit_ReturnedSliceHoldsAtMostOneElement(t *testing.T) {
	event := time.Date(2026, 8, 15, 9, 0, 0, 0, time.UTC)
	due := time.Date(2026, 8, 20, 9, 0, 0, 0, time.UTC)
	content := "It's Ana, not Anna"

	cases := []classify.Classification{
		{EventAt: timePtr(event), NormalizedContent: strPtr(content)},
		{DueAt: timePtr(due), NormalizedContent: strPtr(content)},
		{NormalizedContent: strPtr(content)},
		{EventAt: timePtr(event), DueAt: timePtr(due), NormalizedContent: strPtr(content)},
		{},
	}
	for _, c := range cases {
		if edits, _ := PlanEdit(c); len(edits) > 1 {
			t.Errorf("PlanEdit(%+v) returned %d edits, D3's own invariant allows at most 1", c, len(edits))
		}
	}
}
