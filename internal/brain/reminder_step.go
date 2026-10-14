package brain

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/rengo/nooma/internal/core/prospection"
	"github.com/rengo/nooma/internal/core/unit"
	"github.com/rengo/nooma/internal/ports"
)

// canDeliver is whether this pass has somewhere to speak: a channel and a
// conversation. Only such a pass re-plans (ADR-0032 point 3); one that cannot
// only retires reminders whose event passed.
func (r checkRunner) canDeliver() bool { return r.channel != nil && r.conversation != "" }

// settledRow is a trigger this pass already moved out of armed: fired (it then
// counts as sent) or expired. A dry run, which writes neither, must still see
// the repository as the real pass leaves it.
type settledRow struct {
	row   ports.DueTrigger
	fired bool
}

// reminderTally counts what the re-plan step did to a unit's armed set.
type reminderTally struct{ armed, moved, cancelled int }

// replan is the reminder step (ADR-0032 point 3). The stale one-shot rows are
// the armed ones the due loop deferred and the undelivered ones that are late or
// superseded. Each unit with any gets the reminders its current event still has
// ahead, carrying its healthy armed rows, written FIRST; only then are the stale
// rows retired. A crash in between leaves them un-retired, and the next pass
// finds the plan armed and carries it, creating nothing twice. A unit owed only
// an at-once catch-up, every lead behind and nothing sent, is held: its rows are
// neither sent nor retired until the catch-up exists.
func (r checkRunner) replan(ctx context.Context, report *CheckReport, stale []staleRow, settled map[string]settledRow, now time.Time, commit bool) error {
	undelivered, err := r.triggers.Undelivered(ctx)
	if err != nil {
		return fmt.Errorf("check: undelivered triggers: %w", err)
	}
	g := r.gate()
	for _, t := range undelivered {
		if t.RecurrenceRule != nil {
			continue
		}
		verdict, reason, err := g.judge(ctx, t, true, now)
		if err != nil {
			return err
		}
		if verdict == prospection.VerdictStale && reason != prospection.LapsePassed {
			stale = append(stale, staleRow{t, false, reason})
		}
	}

	prefs, tally := reminderPrefs(ctx, r.config), &reminderTally{}
	for len(stale) > 0 {
		unitID, group := "", []staleRow{}
		if id := stale[0].UnitID; id != nil {
			unitID = *id
		}
		rest := stale[:0:0]
		for _, s := range stale {
			if (s.UnitID == nil && unitID == "") || (s.UnitID != nil && *s.UnitID == unitID) {
				group = append(group, s)
			} else {
				rest = append(rest, s)
			}
		}
		stale = rest

		held, err := r.replanUnit(ctx, unitID, group, settled, prefs, tally, now, commit)
		if err != nil {
			return err
		}
		if held {
			continue
		}
		for _, s := range group {
			done, err := r.retire(ctx, s, now, commit)
			if err != nil {
				return err
			}
			switch {
			case done && s.armed:
				report.TriggersExpired++
			case done:
				report.TriggersLapsed++
			}
		}
	}
	report.RemindersArmed += tally.armed
	report.RemindersMoved += tally.moved
	report.RemindersCancelled += tally.cancelled
	return nil
}

// replanUnit writes the plan for one unit whose rows are stale and reports
// whether the unit is held. The healthy armed rows and the reminders already
// sent exclude every stale row, so a stale row is never carried onto the plan
// and then lost when it is retired, nor counted as sent.
func (r checkRunner) replanUnit(ctx context.Context, unitID string, group []staleRow, settled map[string]settledRow, prefs prospection.ReminderPrefs, tally *reminderTally, now time.Time, commit bool) (bool, error) {
	if unitID == "" || r.units == nil {
		return false, nil
	}
	u, err := r.units.ByID(ctx, unitID)
	if errors.Is(err, ports.ErrUnitNotFound) {
		return false, nil
	}
	if err != nil {
		// A unit that cannot be read cannot be planned for: its rows are held
		// until a pass can read it, rather than stalling the digest behind them.
		// The pass reports it (reportUnreadable).
		r.unreadable[unitID] = err
		return true, nil
	}
	if u.Type != unit.TypeEvent || u.EventAt == nil {
		return false, nil
	}
	stale := map[string]bool{}
	for _, s := range group {
		stale[s.ID] = true
	}

	armed, err := r.triggers.ArmedForUnit(ctx, unitID)
	if err != nil {
		return false, fmt.Errorf("check: read reminders of unit %q: %w", unitID, err)
	}
	armed = slices.DeleteFunc(armed, func(t ports.DueTrigger) bool {
		_, gone := settled[t.ID]
		return t.RecurrenceRule != nil || stale[t.ID] || gone
	})
	fired, err := r.triggers.FiredForUnit(ctx, unitID)
	if err != nil {
		return false, fmt.Errorf("check: read the reminders sent for unit %q: %w", unitID, err)
	}
	for id, s := range settled {
		if s.fired && s.row.UnitID != nil && *s.row.UnitID == unitID && !slices.ContainsFunc(fired, func(f ports.FiredTrigger) bool { return f.ID == id }) {
			fired = append(fired, ports.FiredTrigger{ID: id, FireAt: s.row.FireAt, FiredAt: now, Payload: s.row.Payload})
		}
	}
	fired = slices.DeleteFunc(fired, func(f ports.FiredTrigger) bool { return stale[f.ID] })

	live, byID := liveSet(armed)
	f := prospection.FollowSent(*u.EventAt, live, sentReminders(fired, *u.EventAt), prefs, group[0].InterruptLevel, now)
	// Held: FollowSent answers "every lead behind, nothing live" with ArmNothing and
	// no refusal (the others carry one: passed, quiet hours, covered). Link 4's
	// catch-up replaces this branch, so the coupling is deliberate and tested
	// (TestI37_UnitOwedOnlyACatchUpIsHeld).
	if p := f.Plans[0]; p.What == prospection.ArmNothing && p.Why == prospection.RefusalNone {
		return true, nil
	}
	w := reminderSet{triggers: r.triggers, log: r.log, ids: r.ids, cause: checkCause(), tally: tally, dry: !commit}
	return false, w.apply(ctx, u, f, byID, *u.EventAt, u.Content, false, now)
}

// checkCause is a due scan's re-plan of an event's reminders (ADR-0032).
func checkCause() reminderCause {
	return reminderCause{
		op:    "check",
		moved: ports.ActionCheckReminderMoved, armed: ports.ActionCheckReminderArmed, cancelled: ports.ActionCheckReminderCancelled,
		movedWith: "to the unit's current date", armedFor: "for unit %q's current date",
		carried: "another reminder of the unit carries the current date",
		quiet:   "every moment left before the event is in quiet hours, when nothing is sent",
		passed:  "the event has already passed",
		covered: "a reminder of the event already sent covers its leads",
	}
}
