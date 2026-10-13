package brain

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/rengo/nooma/internal/core/prospection"
	"github.com/rengo/nooma/internal/core/unit"
	"github.com/rengo/nooma/internal/ports"
)

// ErrRemindersNotFollowed reports a save whose values are in force but
// whose re-arm of the reminders already set stopped part way. Saving again
// finishes it: Follow over a set already consistent writes nothing.
var ErrRemindersNotFollowed = errors.New("preferences: saved, but the reminders already set were not all moved")

// preferencesCause is a preference change's (ADR-0029 point 6).
func preferencesCause() reminderCause {
	return reminderCause{
		op:    "preferences",
		moved: ports.ActionConfigReminderMoved, armed: ports.ActionConfigReminderArmed, cancelled: ports.ActionConfigReminderCancelled,
		movedWith: "to the user's new reminder preferences", armedFor: "for unit %q at the user's new reminder preferences",
		carried: "another reminder of the unit carries the new leads",
		quiet:   "every moment left before the event is in quiet hours, when nothing is sent",
		passed:  "the event has already passed",
	}
}

// rearm makes every event still ahead follow prefs (ADR-0029 point 6):
// each unit an armed trigger hangs off is read, and an event whose instant
// is after now and none of whose armed triggers recurs is re-armed through
// prospection.Follow, the decision a corrected date uses. A recurring
// reminder keeps its own timing (ADR-0029 point 7), and anything else arms
// nothing from these preferences. A trigger created here takes the level
// of the unit's earliest armed one, which carries the capture's reading.
func (s *PreferencesService) rearm(ctx context.Context, prefs prospection.ReminderPrefs, now time.Time) error {
	ids, err := s.triggers.ArmedUnitIDs(ctx)
	if err != nil {
		return fmt.Errorf("read units with armed reminders: %w", err)
	}
	w := reminderSet{triggers: s.triggers, log: s.log, ids: s.ids, cause: preferencesCause()}
	for _, id := range ids {
		u, err := s.units.ByID(ctx, id)
		if err != nil {
			return fmt.Errorf("read unit %q: %w", id, err)
		}
		if u.Type != unit.TypeEvent || u.EventAt == nil || !u.EventAt.After(now) {
			continue
		}
		armed, err := s.triggers.ArmedForUnit(ctx, id)
		if err != nil {
			return fmt.Errorf("read reminders of unit %q: %w", id, err)
		}
		if len(armed) == 0 || recurs(armed) {
			continue
		}
		live, byID := liveSet(armed)
		f := prospection.Follow(*u.EventAt, live, prefs, armed[0].InterruptLevel, now)
		if err := w.apply(ctx, u, f, byID, *u.EventAt, u.Content, false, now); err != nil {
			return err
		}
	}
	return nil
}

func recurs(armed []ports.DueTrigger) bool {
	for _, t := range armed {
		if t.RecurrenceRule != nil {
			return true
		}
	}
	return false
}
