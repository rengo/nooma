package correction

import (
	"testing"
	"time"

	"github.com/rengo/nooma/internal/core/classify"
	"github.com/rengo/nooma/internal/core/unit"
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
			got, ok := PlanEdit(tt.c, unit.Unit{}, time.UTC)
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

// TestPlanEdit_AnEchoedDateIsNoEdit is I30's echo rule: the correction
// prompt shows the model the unit's current instants, so a date field
// equal to the unit's own value can be the model repeating it. It yields
// to a content change that keeps the unit's instant; otherwise it stands
// as a same-date correction, the edit I29's repair path repeats.
func TestPlanEdit_AnEchoedDateIsNoEdit(t *testing.T) {
	art := time.FixedZone("ART", -3*60*60)
	event := time.Date(2026, 10, 11, 9, 0, 0, 0, art)
	due := time.Date(2026, 10, 20, 9, 0, 0, 0, art)
	moved := time.Date(2026, 10, 11, 8, 0, 0, 0, art)
	sameInUTC := event.UTC() // the same instant written in another frame
	content := "Tengo un vuelo a Roma el 2026-10-11 a las 09:00."
	u := unit.Unit{Content: "Tengo un vuelo el 2026-10-11 a las 09:00.", EventAt: &event, DueAt: &due}
	eventOnly := unit.Unit{Content: u.Content, EventAt: &event}

	tests := []struct {
		name   string
		c      classify.Classification
		want   []Edit
		ok     bool
		target *unit.Unit // u when nil
	}{
		{"echoed event_at beside a text change -> content", classify.Classification{EventAt: &sameInUTC, NormalizedContent: strPtr(content)}, []Edit{NewContentEdit(content)}, true, &eventOnly},
		{"both echoed beside a text change -> content", classify.Classification{EventAt: timePtr(event), DueAt: timePtr(due), NormalizedContent: strPtr(content)}, []Edit{NewContentEdit(content)}, true, nil},
		{"echo beside content losing the instant stands", classify.Classification{EventAt: timePtr(event), NormalizedContent: strPtr("Es a Roma.")}, []Edit{NewEventAtEdit(event)}, true, &eventOnly},
		{"echo beside the unchanged body stands", classify.Classification{EventAt: timePtr(event), NormalizedContent: strPtr(u.Content)}, []Edit{NewEventAtEdit(event)}, true, &eventOnly},
		{"echo alone stands", classify.Classification{EventAt: timePtr(event)}, []Edit{NewEventAtEdit(event)}, true, &eventOnly},
		{"echoed due_at beside a moved event_at -> event_at", classify.Classification{EventAt: timePtr(moved), DueAt: timePtr(due), NormalizedContent: strPtr(content)}, []Edit{NewEventAtEdit(moved)}, true, nil},
		{"a moved event_at still wins over content", classify.Classification{EventAt: timePtr(moved), NormalizedContent: strPtr(content)}, []Edit{NewEventAtEdit(moved)}, true, &eventOnly},
		{"a date the unit lacks is never an echo", classify.Classification{DueAt: timePtr(due), NormalizedContent: strPtr(content)}, []Edit{NewDueAtEdit(due)}, true, &eventOnly},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			target := u
			if tt.target != nil {
				target = *tt.target
			}
			got, ok := PlanEdit(tt.c, target, art)
			if ok != tt.ok || len(got) != len(tt.want) {
				t.Fatalf("PlanEdit() = %+v, %v; want %+v, %v", got, ok, tt.want, tt.ok)
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

	got, ok := PlanEdit(c, unit.Unit{}, time.UTC)
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
		if edits, _ := PlanEdit(c, unit.Unit{}, time.UTC); len(edits) > 1 {
			t.Errorf("PlanEdit(%+v) returned %d edits, D3's own invariant allows at most 1", c, len(edits))
		}
	}
}
