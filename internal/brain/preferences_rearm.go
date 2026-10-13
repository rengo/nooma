package brain

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/rengo/nooma/internal/core/prospection"
	"github.com/rengo/nooma/internal/core/unit"
	"github.com/rengo/nooma/internal/ports"
)

// ErrRemindersNotFollowed: the values are saved, the re-arm stopped part
// way, and the next save finishes it — unless the change's config.updated
// row also failed (the error then matches ErrWriteLanded too), when nothing
// marks it pending and the next change of a preference re-arms instead.
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
		covered: "a reminder of the event already sent covers its leads",
	}
}

// rearm makes every event still ahead whose armed triggers do not recur
// follow prefs through prospection.FollowSent (ADR-0029 points 6-7), the
// event's last sent reminder covering the leads due before it. A created
// trigger takes the level of the unit's earliest armed one (doc 02 §7).
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
		fired, err := s.triggers.FiredForUnit(ctx, id)
		if err != nil {
			return fmt.Errorf("read the reminders sent for unit %q: %w", id, err)
		}
		live, byID := liveSet(armed)
		f := prospection.FollowSent(*u.EventAt, live, sentReminders(fired, *u.EventAt), prefs, armed[0].InterruptLevel, now)
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

// rearmPending reports whether the last change's re-arm did not complete:
// the last config.updated row written comes after the last
// config.reminders.followed row written. Both are picked and compared by
// insertion sequence alone, so a clock that stepped back cannot hide one.
func (s *PreferencesService) rearmPending(ctx context.Context) (bool, error) {
	updated, err := s.lastWritten(ctx, ports.ActionConfigUpdated)
	if err != nil || updated == 0 {
		return false, err
	}
	followed, err := s.lastWritten(ctx, ports.ActionConfigRemindersFollowed)
	return followed < updated, err
}

// lastWritten is the highest insertion sequence among action's rows (one
// per save), 0 for none. Before orders by occurred_at, so all are read.
func (s *PreferencesService) lastWritten(ctx context.Context, action ports.DecisionAction) (int64, error) {
	rows, err := s.log.Before(ctx, nil, string(action), math.MaxInt32)
	var last int64
	for _, r := range rows {
		last = max(last, r.Seq)
	}
	return last, err
}

// recordFollowed marks the re-arm of the preferences in force complete.
func (s *PreferencesService) recordFollowed(ctx context.Context, now time.Time) error {
	if err := s.log.Record(ctx, ports.Decision{ID: s.ids.New(), Action: ports.ActionConfigRemindersFollowed,
		Rationale: "every reminder already set for an event ahead now follows the user's reminder preferences",
		Context:   []byte(`{}`), OccurredAt: now}); err != nil {
		return fmt.Errorf("preferences: record %s: %w", ports.ActionConfigRemindersFollowed, err)
	}
	return nil
}

// sentReminders is what went out for an event now at eventAt, as
// FollowSent reads it. A row armed before the event instant was recorded is
// placed by fire_at plus lead_minutes; one pulled to now or armed at once
// (lead_minutes 0) that fired before eventAt is taken as about eventAt and
// covers by time only (doc 02 §7: a correction's old date may be read as
// the new one for such rows, and nothing re-sends).
func sentReminders(fired []ports.FiredTrigger, eventAt time.Time) []prospection.Sent {
	out := make([]prospection.Sent, len(fired))
	for i, f := range fired {
		about := f.Payload.About
		switch {
		case !about.IsZero():
		case f.Payload.LeadMinutes > 0:
			about = f.FireAt.Add(time.Duration(f.Payload.LeadMinutes) * time.Minute)
		case f.FiredAt.Before(eventAt):
			about = eventAt
		}
		s := prospection.Sent{About: about, At: f.FiredAt}
		for _, m := range f.Payload.UserLeadsMinutes {
			s.Leads = append(s.Leads, time.Duration(m)*time.Minute)
		}
		out[i] = s
	}
	return out
}
