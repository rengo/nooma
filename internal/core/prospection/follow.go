package prospection

import "time"

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
// at-once reminder is a capture's, sent once. A live one is still carried
// to now, and one already due keeps its instant, so the same correction
// again moves nothing.
func Follow(eventAt time.Time, live []Live, prefs ReminderPrefs, interruptLevel *float64, now time.Time) Following {
	interrupt := ResolveInterrupt(interruptLevel)
	for i, l := range live {
		if l.Rule != nil {
			return settle([]Plan{recurringTrigger(eventAt, *l.Rule, now, interrupt)}, live, []int{i})
		}
	}
	plans, arms := eventReminders(eventAt, prefs, now, interrupt)
	if !arms || (plans[0].Immediate && len(live) == 0) {
		return settle([]Plan{{What: ArmNothing, Why: plans[0].Why, Interrupt: interrupt}}, live, []int{-1})
	}
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
