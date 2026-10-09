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
	// Plan is what a fresh capture of the corrected date arms, by the same
	// functions Arm calls. What == ArmNothing when that is nothing.
	Plan Plan
	// Carry is the live trigger moved to Plan; "" with an arming Plan means
	// none was live and one is created.
	Carry string
	// Expire lists the live triggers that stop: every one but Carry.
	Expire []string
}

// Follow decides, for an event unit whose event_at is now eventAt, what
// its live triggers become so that exactly one stays armed, at what a
// fresh capture would arm — or none, when that arms nothing. live is in
// (fire_at, id) order; a recurring trigger keeps the unit recurring and is
// the one carried, otherwise the earliest is. interruptLevel is the
// correction's own reading, used only by a trigger created here: a carried
// one keeps the level it was armed with.
func Follow(eventAt time.Time, live []Live, interruptLevel *float64, now time.Time) Following {
	interrupt := ResolveInterrupt(interruptLevel)
	carry := -1
	var plan Plan
	for i, l := range live {
		if l.Rule != nil {
			carry, plan = i, recurringTrigger(eventAt, *l.Rule, now, interrupt)
			break
		}
	}
	if carry < 0 {
		var arms bool
		plan, arms = datedTrigger(eventAt, now, interrupt)
		if arms && len(live) > 0 {
			carry = 0
		}
	}

	f := Following{Plan: plan}
	for i, l := range live {
		if i == carry {
			f.Carry = l.ID
			continue
		}
		f.Expire = append(f.Expire, l.ID)
	}
	return f
}
