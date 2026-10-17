package brain

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"github.com/rengo/nooma/internal/core/prospection"
	"github.com/rengo/nooma/internal/core/unit"
	"github.com/rengo/nooma/internal/ports"
)

// reminderGate is where a stored trigger becomes prospection.ReminderVerdict's
// input, for the due scan, the re-plan step, the digest and Today alike
// (ADR-0032 point 1).
type reminderGate struct {
	units    ports.UnitRepo
	triggers ports.TriggerRepo
	night    prospection.QuietHours
	// unreadable collects the units a pass could not read, judged without
	// them; the pass reports each once. Nil (Today) collects nothing.
	unreadable map[string]error
	// lenient skips, and logs, a reminder it cannot judge instead of failing: a
	// read-only page must not vanish for one damaged unit row.
	lenient bool
}

func (r checkRunner) gate() reminderGate {
	return reminderGate{units: r.units, triggers: r.triggers, night: r.night, unreadable: r.unreadable}
}

// reportUnreadable records one row per unit this pass judged without its unit
// (a write, so not in a dry run), so a damaged unit row is visible instead of
// silently skipped.
func (r checkRunner) reportUnreadable(ctx context.Context, now time.Time, commit bool) error {
	ids := make([]string, 0, len(r.unreadable))
	for id := range r.unreadable {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	if !commit {
		return nil
	}
	for _, id := range ids {
		if err := r.record(ctx, now, ports.ActionCheckDeliveryFailed,
			fmt.Sprintf("unit %q could not be read (%v), so its reminders were judged without it", id, r.unreadable[id]), checkDetail{UnitID: id}); err != nil {
			return err
		}
	}
	return nil
}

// currentEvent is the instant a unit holds for its event now. It is the unit's
// event_at only; #337's task units will add their due_at here.
func currentEvent(u unit.Unit) *time.Time { return u.EventAt }

// newReminder is the gate's view of a stored trigger, before its unit is read.
func newReminder(t ports.DueTrigger, fired bool) prospection.Reminder {
	return prospection.Reminder{
		FireAt: t.FireAt, EventAt: t.Payload.About, LeadMinutes: t.Payload.LeadMinutes,
		Rule: t.RecurrenceRule, Anchor: t.RecurrenceAnchor, Waiting: fired && (t.RecurrenceRule != nil || t.FireAt.IsZero()),
	}
}

// judge returns the verdict for t. fired says it already fired and never
// reached the user. A recurring one then waits for the digest, which lateness
// does not judge (ADR-0009's window is a push's), nor one with no clock; a
// one-shot row is judged late.
// The unit is read for a reminder that names its event in the payload (to see
// whether it moved) and for a legacy one that names none. A damaged unit row is
// tolerated for the former, which is judged without it and reported, so it
// cannot stall a pass; for a legacy row it aborts the pass. A legacy fired row
// is superseded when its unit has armed a one-shot of its own since (D8). A
// nil port or a missing unit means unknown.
func (g reminderGate) judge(ctx context.Context, t ports.DueTrigger, fired bool, now time.Time) (prospection.Verdict, prospection.LapseReason, error) {
	reminder := newReminder(t, fired)
	if t.UnitID != nil && g.units != nil && (!reminder.EventAt.IsZero() || !reminder.NamesEvent()) {
		u, err := g.units.ByID(ctx, *t.UnitID)
		switch {
		case err == nil:
			reminder.Current = currentEvent(u)
		case errors.Is(err, ports.ErrUnitNotFound):
		case reminder.NamesEvent():
			if g.unreadable != nil {
				g.unreadable[*t.UnitID] = err
			}
		default:
			return "", "", fmt.Errorf("reminder: read unit %q of trigger %q: %w", *t.UnitID, t.ID, err)
		}
	}
	if fired && !reminder.NamesEvent() && t.UnitID != nil && g.triggers != nil {
		armed, err := g.triggers.ArmedForUnit(ctx, *t.UnitID)
		if err != nil {
			return "", "", fmt.Errorf("reminder: read reminders of unit %q: %w", *t.UnitID, err)
		}
		reminder.Newer = slices.ContainsFunc(armed, func(a ports.DueTrigger) bool { return a.RecurrenceRule == nil })
	}
	verdict, reason := g.night.ReminderVerdict(reminder, now)
	return verdict, reason, nil
}

// splitPast sets apart the fired, undelivered reminders whose event is past
// from those still worth carrying. A one-shot row that is late or superseded is
// the re-plan step's, held or re-planned, and is in neither list: the digest
// neither sends nor lapses it. It writes nothing, so Today and a dry run share
// it with the real pass.
func splitPast(ctx context.Context, g reminderGate, pending []ports.DueTrigger, now time.Time) (kept, past []ports.DueTrigger, err error) {
	kept = make([]ports.DueTrigger, 0, len(pending))
	for _, t := range pending {
		verdict, reason, err := g.judge(ctx, t, true, now)
		switch {
		case err != nil && g.lenient:
			slog.Warn("a reminder could not be judged and is left out", "trigger", t.ID, "error", err)
		case err != nil:
			return nil, nil, err
		case verdict != prospection.VerdictStale:
			kept = append(kept, t)
		case reason == prospection.LapsePassed:
			past = append(past, t)
		}
	}
	return kept, past, nil
}

// staleRow is a reminder that will not be sent: armed (to expire) or fired and
// never delivered (to lapse), and why.
type staleRow struct {
	ports.DueTrigger
	armed  bool
	reason prospection.LapseReason
}

// retire expires an armed row or lapses a fired one, and says why in the
// trail; an expired recurring row arms its next occurrence (leave). A row
// another pass already moved is skipped (false). commit false reports the
// retirement without writing it.
func (r checkRunner) retire(ctx context.Context, s staleRow, now time.Time, commit bool) (bool, error) {
	if !commit {
		return true, nil
	}
	action, verb := ports.ActionCheckTriggerLapsed, "fired and never reached the user, and was lapsed"
	var next string
	var err error
	if s.armed {
		action, verb = ports.ActionCheckTriggerExpired, "came due and was expired"
		next, err = r.leave(ctx, s.DueTrigger, nil, now)
	} else {
		err = r.triggers.Lapse(ctx, s.ID)
	}
	if err != nil {
		skipped, skipErr := r.skipOnConflict(ctx, now, err, ports.ErrTriggerStatusConflict, "triggers", s.ID, s.FireAt)
		if skipErr != nil || skipped {
			return false, skipErr
		}
		return false, fmt.Errorf("check: retire trigger %q: %w", s.ID, err)
	}
	return true, r.record(ctx, now, action,
		fmt.Sprintf("trigger %q %s rather than sent: %s%s", s.ID, verb, lapseWhy(s.reason), next),
		checkDetail{ID: s.ID, FireAt: s.FireAt.UTC().Format(time.RFC3339), Verdict: string(prospection.VerdictStale), Reason: string(s.reason)})
}

func lapseWhy(reason prospection.LapseReason) string {
	switch reason {
	case prospection.LapseOverdue:
		return "it is overdue past its staleness window"
	case prospection.LapseSuperseded:
		return "its unit no longer has the event it is about"
	}
	return "its event is no longer ahead"
}
