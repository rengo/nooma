package brain

import (
	"context"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/core/prospection"
	"github.com/rengo/nooma/internal/core/unit"
	"github.com/rengo/nooma/internal/ports"
	"github.com/rengo/nooma/test/support/memrepo"
)

// TestMoveReminderClearsCatchUpOnANewInstant: a catch-up is the reminder owed
// for one event instant. Moved onto another instant (a corrected date) it is an
// ordinary reminder of the new one and no longer a catch-up, so the new instant
// can be owed its own; moved without the instant changing it stays one.
func TestMoveReminderClearsCatchUpOnANewInstant(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 10, 14, 10, 0, 0, 0, time.UTC)
	was := now.Add(time.Hour)
	for _, tc := range []struct {
		name    string
		to      time.Time
		wantMrk bool
	}{{"the same instant", was, true}, {"a new instant", now.Add(48 * time.Hour), false}} {
		t.Run(tc.name, func(t *testing.T) {
			triggers := memrepo.NewTriggers()
			unitID, fireAt := "u", now
			if err := triggers.Create(ctx, ports.Trigger{
				ID: "c", UnitID: &unitID, Kind: ports.TriggerKindTimeBased, FireAt: &fireAt, CreatedAt: now,
				Payload: ports.TriggerPayload{ActionText: "dentist", About: was, CatchUp: true},
			}); err != nil {
				t.Fatal(err)
			}
			armed, _ := triggers.ArmedForUnit(ctx, "u")
			plan := prospection.Plan{What: prospection.ArmTrigger, FireAt: now.Add(time.Minute), About: tc.to, LeadMinutes: int(tc.to.Sub(now) / time.Minute)}
			w := reminderSet{triggers: triggers, log: memrepo.NewDecisionLog(), ids: &countingIDs{}, cause: checkCause()}

			if err := w.moveReminder(ctx, unit.Unit{ID: "u", EventAt: &tc.to}, armed[0], plan, tc.to, "dentist", false, now); err != nil {
				t.Fatalf("moveReminder: %v", err)
			}

			if got := triggers.All()[0].Payload.CatchUp; got != tc.wantMrk {
				t.Errorf("CatchUp after a move to %s = %v, want %v", tc.name, got, tc.wantMrk)
			}
		})
	}
}
