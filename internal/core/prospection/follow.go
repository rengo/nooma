package prospection

import (
	"slices"
	"time"

	"github.com/rengo/nooma/internal/core/classify"
)

// Live is an armed trigger hanging off a unit whose event date was
// corrected: what Follow needs to decide its fate.
type Live struct {
	ID     string
	FireAt time.Time
	Rule   *Rule // nil for a one-shot trigger
}

// Following is what a corrected event date does to its unit's armed
// triggers (doc 02 §5 step 4, I29).
type Following struct {
	// Plans is what a fresh capture of the corrected date arms, by the
	// functions Arm calls — or one ArmNothing plan when nothing is armed.
	Plans []Plan
	// Carry is parallel to Plans: the live trigger moved onto Plans[i], or
	// "" when Plans[i] is created.
	Carry []string
	// Expire lists the live triggers that stop: every one not carried.
	Expire []string
}

// Follow decides, for an event unit whose event_at is now eventAt, what its
// live triggers become so that exactly the set a fresh capture would arm
// stays armed (ADR-0029 point 4). live is in (fire_at, id) order. A
// recurring trigger keeps the unit recurring and is the one carried;
// otherwise live triggers are carried onto the plans in firing order,
// surplus ones expire and missing ones are created. interruptLevel is the
// correction's own reading, used only by a trigger created here: a carried
// one keeps the level it was armed with.
//
// **A correction never creates an at-once reminder.** When every lead of
// the new instant is behind and nothing is live, it arms nothing: the
// at-once reminder is a capture's, sent once (ADR-0029 point 4). A live
// one is still carried to now, and one already due keeps its instant, so
// the same correction again moves nothing. A lead that ADR-0030 pulled to
// now is not that reminder — its lead was ahead — and is armed with the
// rest of the set.
func Follow(eventAt time.Time, live []Live, prefs ReminderPrefs, interruptLevel *float64, now time.Time) Following {
	return FollowSent(eventAt, live, nil, prefs, interruptLevel, now)
}

// FollowSent is Follow for a re-arm under new preferences (ADR-0029 point
// 6): a lead a reminder already sent about this same event instant covers
// (coveredBy) is not planned again. A nil sent is Follow.
func FollowSent(eventAt time.Time, live []Live, sent []Sent, prefs ReminderPrefs, interruptLevel *float64, now time.Time) Following {
	interrupt := ResolveInterrupt(interruptLevel)
	for i, l := range live {
		if l.Rule != nil {
			return settle([]Plan{recurringTrigger(eventAt, *l.Rule, prefs, now, interrupt)}, live, []int{i})
		}
	}
	plans, arms := eventReminders(eventAt, prefs, now, interrupt, sent)
	if !arms || (len(live) == 0 && everyLeadBehind(eventAt, prefs, now)) {
		return settle([]Plan{{What: ArmNothing, Why: plans[0].Why, Interrupt: interrupt}}, live, []int{-1})
	}
	return carryOnto(plans, live, now)
}

// FollowAsked is FollowSent for a unit the user asked to be reminded of
// (ADR-0031): the plan is what a capture of the same request still arms at its
// instant at — the asked lead, and ADR-0029's leads too when event — so a
// re-plan carries an asked reminder instead of expiring it. dayOnly is a day
// alone. Like a correction it plans no at-once reminder: a re-plan owes that
// only as CatchUpAsked's catch-up.
func FollowAsked(at time.Time, event, dayOnly bool, live []Live, sent []Sent, prefs ReminderPrefs, interruptLevel *float64, now time.Time) Following {
	interrupt := ResolveInterrupt(interruptLevel)
	plans, arms := askedReminders(at, event, dayOnly, prefs, now, interrupt, sent)
	if arms {
		plans = slices.DeleteFunc(plans, atOnce)
	}
	if len(plans) == 0 {
		plans = []Plan{{What: ArmNothing, Interrupt: interrupt}}
	}
	if plans[0].What == ArmNothing {
		return settle(plans[:1], live, []int{-1})
	}
	return carryOnto(plans, live, now)
}

// CatchUpAsked is CatchUp for an asked unit whose every lead is behind: the
// at-once reminder its capture would arm (ADR-0031 point 2), refused too late
// as there, owed only while nothing of it is live and nothing sent is about its
// instant. A day alone is ahead until its day ends. Its instant is Nooma's
// choice, not one the user named, so it keeps the night rules (ADR-0031 point
// 6), as CatchUp does: inside quiet hours it waits for quiet end when that is
// before the instant, and is refused (RefusalQuietHours, still owed) when not.
func CatchUpAsked(at time.Time, event, dayOnly bool, live []Live, sent []Sent, prefs ReminderPrefs, interrupt Interrupt, now time.Time) (Plan, bool) {
	if len(live) > 0 || slices.ContainsFunc(sent, func(s Sent) bool { return s.About.Equal(at) }) {
		return Plan{}, false
	}
	plans, arms := askedReminders(at, event, dayOnly, prefs, now, interrupt, nil)
	if !arms || len(plans) != 1 || !atOnce(plans[0]) {
		return Plan{}, false
	}
	plan, by := plans[0], at
	if dayOnly {
		y, m, d := at.In(now.Location()).Date()
		by = time.Date(y, m, d+1, 0, 0, 0, 0, now.Location())
	}
	fire, ok := deliverAt(now, by, now, prefs)
	if !ok {
		return Plan{What: ArmNothing, Why: RefusalQuietHours, Interrupt: interrupt}, true
	}
	plan.FireAt, plan.Immediate, plan.NightExempt = fire, fire.Equal(now), false
	if !plan.Immediate && !dayOnly {
		plan.LeadMinutes = int(at.Sub(fire) / time.Minute)
	}
	return plan, true
}

// askedReminders is the reminders Arm plans for an asked unit at at.
func askedReminders(at time.Time, event, dayOnly bool, prefs ReminderPrefs, now time.Time, interrupt Interrupt, sent []Sent) ([]Plan, bool) {
	kind := classify.KindTask
	if event {
		kind = classify.KindEvent
	}
	return reminders(at, dayOnly, leadSet(kind, true, at, dayOnly, prefs, now.Location()), prefs, now, interrupt, sent)
}

// atOnce is the asked reminder sent at once because its lead is behind: no
// lead served, armed for the arming instant.
func atOnce(p Plan) bool { return p.Asked && p.Immediate && len(p.Leads) == 0 }

// carryOnto carries live triggers onto plans in firing order, Follow's rule:
// one already due keeps its instant when its plan is now.
func carryOnto(plans []Plan, live []Live, now time.Time) Following {
	carried := make([]int, len(plans))
	for i := range plans {
		carried[i] = -1
		if i < len(live) {
			carried[i] = i
			if plans[i].Immediate && !live[i].FireAt.After(now) {
				plans[i].FireAt = live[i].FireAt
			}
		}
	}
	return settle(plans, live, carried)
}

// settle pairs plans with the live triggers carried onto them (an index
// into live, or -1) and expires every live trigger left over.
func settle(plans []Plan, live []Live, carried []int) Following {
	f := Following{Plans: plans, Carry: make([]string, len(plans))}
	kept := map[int]bool{}
	for i, c := range carried {
		if c >= 0 {
			f.Carry[i], kept[c] = live[c].ID, true
		}
	}
	for i, l := range live {
		if !kept[i] {
			f.Expire = append(f.Expire, l.ID)
		}
	}
	return f
}
