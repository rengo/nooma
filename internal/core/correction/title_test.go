package correction

import (
	"testing"
	"time"

	"github.com/rengo/nooma/internal/core/classify"
	"github.com/rengo/nooma/internal/core/unit"
)

func TestWithTitle(t *testing.T) {
	at := time.Date(2026, 10, 16, 10, 0, 0, 0, time.UTC)
	title := func(s string) *string { return &s }
	dateOnly := []Edit{NewEventAtEdit(at)}
	withContent := []Edit{NewContentEdit("Reunión con Juan y Ana")}
	withDateAndText := []Edit{NewEventAtEdit(at), NewContentEdit("Reunión con Juan el 2026-10-16 a las 10:00")}
	titled := unit.Unit{Content: "x", Title: "Reunión del jueves"}

	tests := []struct {
		name  string
		plan  []Edit
		model *string // classified title
		u     unit.Unit
		saw   bool
		want  []string // "title=<value>" for a title edit, then nothing else appended
	}{
		{"a valid new title is written", dateOnly, title("Reunión del viernes"), titled, true, []string{"Reunión del viernes"}},
		{"a title that repeats the unit's is no edit", dateOnly, title("Reunión del jueves"), titled, true, nil},
		{"a date-only edit with no title keeps it", dateOnly, nil, titled, true, nil},
		{"a content edit with no title clears it", withContent, nil, titled, true, []string{""}},
		{"a date edit with the model's own text and no title clears it", withDateAndText, nil, titled, true, []string{""}},
		{"a content edit with a valid title writes it", withContent, title("Reunión con Juan y Ana"), titled, true, []string{"Reunión con Juan y Ana"}},
		{"a content edit that repeats the title keeps it", withContent, title("Reunión del jueves"), titled, true, nil},
		{"clearing a unit with no title is no edit", withContent, nil, unit.Unit{Content: "x"}, true, nil},
		{"a title the model wrote without seeing the unit is not trusted", dateOnly, title("Cambio al viernes"), titled, false, nil},
		{"and a content edit then clears the stale one", withContent, title("Cambio al viernes"), titled, false, []string{""}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := WithTitle(tt.plan, classify.Classification{Title: tt.model}, tt.u, tt.saw)
			var titles []string
			for _, e := range got[len(tt.plan):] {
				v, ok := e.Title()
				if !ok {
					t.Fatalf("appended a %s edit, want only title edits", e.Field())
				}
				titles = append(titles, v)
			}
			if len(got) < len(tt.plan) || len(titles) != len(tt.want) || (len(tt.want) == 1 && titles[0] != tt.want[0]) {
				t.Errorf("appended titles %q, want %q", titles, tt.want)
			}
		})
	}
}
