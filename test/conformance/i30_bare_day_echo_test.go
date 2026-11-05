// Package conformance — see test/conformance/doc.go for the package contract.
package conformance

import (
	"slices"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/brain"
	"github.com/rengo/nooma/internal/channels"
	"github.com/rengo/nooma/internal/core/correction"
	"github.com/rengo/nooma/internal/core/unit"
)

// TestI30_ABareDayRestatingATimedTaskMovesNothing: the model answers a
// title-only correction of a task due Friday 09:00 with the bare day it was
// shown. That is an echo (I30), not a move to midnight: the due time stays,
// and the reply says "corrected", not "moved".
func TestI30_ABareDayRestatingATimedTaskMovesNothing(t *testing.T) {
	zone := i40Now.Location()
	due := time.Date(2026, 11, 13, 9, 0, 0, 0, zone)
	f := newI40With(t, `{"type":"correction","normalized_content":"Llevar el auto al taller el 2026-11-13 a las 09:00.","due_at":"2026-11-13",`+
		`"weight":0.6,"decay_rate":0.05,"language":"es","referent":"A","title":"Auto al taller"}`, func(u *unit.Unit) {
		u.Type, u.EventAt, u.DueAt = unit.TypeTask, nil, &due
		u.Content, u.Title = "Llevar el auto al service el 2026-11-13 a las 09:00.", "Auto al service"
	})
	res := f.capture(t, "no, es al taller")
	if res.Outcome != brain.OutcomeCorrected || res.Correction == nil {
		t.Fatalf("result = %+v, want a correction", res)
	}
	if res.Correction.MovedTo != nil || slices.Contains(res.Correction.Fields, correction.FieldDueAt) {
		t.Errorf("correction moved the due date: fields %v, MovedTo %v", res.Correction.Fields, res.Correction.MovedTo)
	}
	if got := f.meeting(t); got.DueAt == nil || !got.DueAt.Equal(due) || got.Title != "Auto al taller" {
		t.Errorf("task = %q due %v, want the title changed and due kept at %v", got.Title, got.DueAt, due)
	}
	if reply := channels.RenderReply(res); reply != "Listo, lo corregí." {
		t.Errorf("reply = %q, want %q", reply, "Listo, lo corregí.")
	}
}
