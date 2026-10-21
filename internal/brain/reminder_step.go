package brain

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
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
type reminderTally struct{ armed, moved, cancelled, refused int }

// replan is the reminder step (ADR-0032 point 3). The stale one-shot rows are
// the armed ones the due loop deferred and the undelivered ones that are late or
// superseded. Each unit with any gets the reminders its current event still has
// ahead, carrying its healthy armed rows, written FIRST; only then are the stale
// rows retired. A crash in between leaves them un-retired, and the next pass
// finds the plan armed and carries it, creating nothing twice. A unit owed only
// an at-once catch-up, every lead behind and nothing sent, is held: its rows are
// neither sent nor retired until the catch-up exists.
func (r checkRunner) replan(ctx context.Context, report *CheckReport, stale []staleRow, settled map[string]settledRow, q *pushQueue, now time.Time, commit bool) error {
	undelivered, err := r.triggers.Undelivered(ctx)
	if err != nil {
		return fmt.Errorf("check: undelivered triggers: %w", err)
	}
	g := r.gate()
	var retry []ports.DueTrigger
	for _, t := range undelivered {
		if !pushed(t) {
			continue
		}
		verdict, reason, err := g.judge(ctx, t, true, now)
		if err != nil {
			return err
		}
		switch {
		case verdict == prospection.VerdictStale && reason != prospection.LapsePassed:
			stale = append(stale, staleRow{t, false, reason})
		case verdict == prospection.VerdictDeliver:
			_, touched := settled[t.ID]
			if !touched && !r.unmarked.has(t.ID) {
				retry = append(retry, t)
			}
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
	// A reminder that could not be pushed is pushed again, while its event is
	// ahead (ADR-0032 point 2; a recurring one while its day lasts, ADR-0034). Not one this pass fired or already
	// tried, and not one sent that the vault could not record.
	for _, t := range retry {
		if commit {
			q.add(t)
		} else {
			report.TriggersDelivered++
		}
	}
	report.RemindersArmed += tally.armed
	report.RemindersMoved += tally.moved
	report.RemindersCancelled += tally.cancelled
	report.RemindersRefused += tally.refused
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
	// An asked unit is planned at the instant it asked about, as a capture arms
	// it (ADR-0031); any other unit has reminders only as a dated event.
	at, asked := askedAt(u)
	if !asked {
		if u.Type != unit.TypeEvent || u.EventAt == nil {
			return false, nil
		}
		at = *u.EventAt
	}
	event, dayOnly := u.Type == unit.TypeEvent, group[0].Payload.DayOnly
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
	sent := sentReminders(fired, at)
	interrupt := group[0].InterruptLevel
	// An event's own leads, when it has them, are its leads (I40).
	prefs = prefs.WithEventLeads(u.ReminderLeads)
	f := prospection.FollowSent(at, live, sent, prefs, interrupt, u.CreatedAt, now)
	if asked {
		f = prospection.FollowAsked(at, event, dayOnly, live, sent, prefs, interrupt, u.CreatedAt, now)
	}
	// Nothing live and nothing planned is the unit's whole trail unless it says
	// why. Quiet hours with no instant left is a refusal; every lead behind (or a
	// date-only event on its day, which FollowSent reads as past; or, on the
	// told day, leads_behind, ADR-0047: a lapse is not a telling) is a catch-up,
	// unless something about the event already reached the user or is on its way,
	// when the late rows simply lapse. Covered and past events lapse as they are.
	if p := f.Plans[0]; p.What == prospection.ArmNothing && len(live) == 0 {
		switch p.Why {
		case prospection.RefusalQuietHours:
			return false, r.refuse(ctx, u, at, tally, now, commit)
		case prospection.RefusalNone, prospection.RefusalAlreadyPast, prospection.RefusalLeadsBehind:
			plan, owed := prospection.CatchUp(at, live, sent, prefs, prospection.ResolveInterrupt(interrupt), now)
			if asked {
				plan, owed = prospection.CatchUpAsked(at, event, dayOnly, live, sent, prefs, prospection.ResolveInterrupt(interrupt), now)
			}
			if !owed {
				return false, nil
			}
			return false, r.armCatchUp(ctx, u, at, plan, tally, now, commit)
		}
	}
	w := reminderSet{triggers: r.triggers, log: r.log, ids: r.ids, cause: checkCause(), tally: tally, dry: !commit}
	return false, w.apply(ctx, u, f, byID, at, shownText(u), false, now)
}

// askedAt is the instant an asked unit is reminded about, as prospection.Arm
// reads it: an event's event_at, any other kind's due_at, else the other
// field. false for a unit nobody asked about, or one with no instant.
func askedAt(u unit.Unit) (time.Time, bool) {
	own, other := u.DueAt, u.EventAt
	if u.Type == unit.TypeEvent {
		own, other = other, own
	}
	switch {
	case !u.RemindMe:
	case own != nil:
		return *own, true
	case other != nil:
		return *other, true
	}
	return time.Time{}, false
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

// armCatchUp arms the catch-up an event is owed, or says why it cannot be sent.
// Its id names the unit, the event instant and the attempt, and the create comes
// first: the id is what lets two passes racing for the same attempt resolve to
// one reminder, and the loser must leave no row. The attempt follows the ids
// already used for that instant, not their mark (a correction clears the mark of
// a catch-up it moves, and the id stays), so an undelivered catch-up lapses and
// the event, still ahead, is owed the next number.
func (r checkRunner) armCatchUp(ctx context.Context, u unit.Unit, about time.Time, plan prospection.Plan, tally *reminderTally, now time.Time, commit bool) error {
	if plan.What == prospection.ArmNothing {
		return r.refuse(ctx, u, about, tally, now, commit)
	}
	if !commit {
		tally.armed++
		return nil
	}
	tried, err := r.triggers.OneShotForUnit(ctx, u.ID)
	if err != nil {
		return fmt.Errorf("check: read the reminders of unit %q: %w", u.ID, err)
	}
	prefix := fmt.Sprintf("catch-up:%s:%s:", u.ID, rfc3339(plan.About))
	attempt := 1
	for _, t := range tried {
		if n, err := strconv.Atoi(strings.TrimPrefix(t.ID, prefix)); err == nil && strings.HasPrefix(t.ID, prefix) && n >= attempt {
			attempt = n + 1
		}
	}
	trigger := armedTrigger(prefix+strconv.Itoa(attempt), &u.ID, shownText(u), plan, now)
	trigger.Payload.CatchUp = true
	err = r.triggers.Create(ctx, trigger)
	switch {
	case err == nil:
		tally.armed++
		w := reminderSet{log: r.log, ids: r.ids, cause: checkCause()}
		return w.recordReminder(ctx, ports.ActionCheckReminderArmed,
			fmt.Sprintf("armed a catch-up (attempt %d) for unit %q: %s", attempt, u.ID, armRationale(plan)),
			reminderChange{UnitID: u.ID, TriggerID: trigger.ID, Fields: []string{"fire_at"},
				Previous: map[string]any{"fire_at": nil}, Next: map[string]any{"fire_at": rfc3339(plan.FireAt)},
				About: rfc3339(plan.About), Immediate: plan.Immediate}, now)
	case errors.Is(err, ports.ErrTriggerExists):
		// Another pass took this attempt between the scan above and this create,
		// and records it. The scan already counts every id used before then, so
		// there is no number to try next.
		return nil
	default:
		return fmt.Errorf("check: arm a catch-up for unit %q: %w", u.ID, err)
	}
}

// refuse says that no reminder could be sent for a unit that is left with none:
// every moment left before its event is in quiet hours (ADR-0030).
func (r checkRunner) refuse(ctx context.Context, u unit.Unit, about time.Time, tally *reminderTally, now time.Time, commit bool) error {
	tally.refused++
	if !commit {
		return nil
	}
	return r.recordDetail(ctx, now, ports.ActionCheckReminderRefused,
		fmt.Sprintf("no reminder was sent for unit %q: every moment left before its event is in quiet hours, when nothing is sent", u.ID),
		refusedDetail{UnitID: u.ID, Why: string(prospection.RefusalQuietHours), About: rfc3339(about)})
}

// refusedDetail is the context of a check.reminder.refused row.
type refusedDetail struct {
	UnitID string `json:"unit_id"`
	Why    string `json:"why"`
	About  string `json:"about"`
}
