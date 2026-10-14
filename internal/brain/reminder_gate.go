package brain

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/rengo/nooma/internal/core/prospection"
	"github.com/rengo/nooma/internal/ports"
)

// judgeReminder is where a stored trigger becomes prospection.ReminderVerdict's
// input, for the due scan, the digest and Today alike (ADR-0032 point 1). A
// reminder that names its own event reads no unit, so a damaged unit row cannot
// stall a pass; a legacy row is judged on its unit's event. A nil port or a
// missing unit means unknown; any other read error aborts the pass.
func judgeReminder(ctx context.Context, units ports.UnitRepo, night prospection.QuietHours, t ports.DueTrigger, waiting bool, now time.Time) (prospection.Verdict, prospection.LapseReason, error) {
	reminder := prospection.Reminder{
		FireAt: t.FireAt, EventAt: t.Payload.About, LeadMinutes: t.Payload.LeadMinutes,
		Rule: t.RecurrenceRule, Anchor: t.RecurrenceAnchor, Waiting: waiting,
	}
	if !reminder.NamesEvent() && t.UnitID != nil && units != nil {
		u, err := units.ByID(ctx, *t.UnitID)
		switch {
		case err == nil:
			reminder.Current = u.EventAt
		case !errors.Is(err, ports.ErrUnitNotFound):
			return "", "", fmt.Errorf("reminder: read unit %q of trigger %q: %w", *t.UnitID, t.ID, err)
		}
	}
	verdict, reason := night.ReminderVerdict(reminder, now)
	return verdict, reason, nil
}

// splitPast sets apart the fired, undelivered reminders whose event is past
// from those still worth carrying. They are judged as waiting for the digest:
// lateness is a push's concern (ADR-0032 point 1). It writes nothing, so Today
// and a dry run share it with the real pass.
func splitPast(ctx context.Context, units ports.UnitRepo, night prospection.QuietHours, pending []ports.DueTrigger, now time.Time) (kept, past []ports.DueTrigger, err error) {
	kept = make([]ports.DueTrigger, 0, len(pending))
	for _, t := range pending {
		verdict, _, err := judgeReminder(ctx, units, night, t, true, now)
		if err != nil {
			return nil, nil, err
		}
		if verdict == prospection.VerdictStale {
			past = append(past, t)
			continue
		}
		kept = append(kept, t)
	}
	return kept, past, nil
}

// lapsePast retires each of past and says why in the trail. A row another pass
// already surfaced or lapsed is skipped. It returns how many it lapsed, or
// would have when commit is false.
func (r checkRunner) lapsePast(ctx context.Context, past []ports.DueTrigger, now time.Time, commit bool) (int, error) {
	lapsed := 0
	for _, t := range past {
		if commit {
			if err := r.triggers.Lapse(ctx, t.ID); err != nil {
				skipped, skipErr := r.skipOnConflict(ctx, now, err, ports.ErrTriggerStatusConflict, "triggers", t.ID, t.FireAt)
				if skipErr != nil {
					return 0, skipErr
				}
				if skipped {
					continue
				}
				return 0, fmt.Errorf("check: lapse trigger %q: %w", t.ID, err)
			}
			if err := r.record(ctx, now, ports.ActionCheckTriggerLapsed,
				fmt.Sprintf("trigger %q fired and never reached the user, and the event it is about is no longer ahead, so it was retired rather than sent", t.ID),
				checkDetail{ID: t.ID, FireAt: t.FireAt.UTC().Format(time.RFC3339), Verdict: string(prospection.VerdictStale), Reason: string(prospection.LapsePassed)},
			); err != nil {
				return 0, err
			}
		}
		lapsed++
	}
	return lapsed, nil
}
