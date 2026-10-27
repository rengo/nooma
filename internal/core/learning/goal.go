package learning

import (
	"slices"
	"strings"
	"time"
)

// The goal cadence rule's numbers (doc 02 §9, §13; ADR-0055).
const (
	// GoalCadenceSample is how many of the newest closed goal check-ins are
	// read: doc 02 §9's "5 nudges with no response are 5 ignored ones".
	GoalCadenceSample = 5
	// GoalCadenceEngagedToShorten is how many of the sample engaged shorten
	// the interval. Lengthening needs the whole sample ignored.
	GoalCadenceEngagedToShorten = 3
	// GoalCadenceStepDays is how far one change moves goal_stagnation_days:
	// doc 02 §9's 21 -> 28.
	GoalCadenceStepDays = 7
	// MinGoalStagnationDays and MaxGoalStagnationDays bound what learning
	// sets: below a week a goal is nagged, past eight weeks it is forgotten.
	MinGoalStagnationDays = 7
	MaxGoalStagnationDays = 56
)

// GoalVerdictKind is how a goal check-in closed.
type GoalVerdictKind string

const (
	GoalEngaged  GoalVerdictKind = "engaged"
	GoalDeclined GoalVerdictKind = "declined"
	GoalIgnored  GoalVerdictKind = "ignored"
)

// GoalVerdict is one closed goal check-in: a nudge_engaged, nudge_declined
// or nudge_ignored signal naming a goal's belief (doc 02 §9).
type GoalVerdict struct {
	SignalID string
	Kind     GoalVerdictKind
	At       time.Time
}

// GoalCadenceState is goal_stagnation_days as the vault holds it.
type GoalCadenceState struct {
	Days int
	// Pinned is the user's own value, set from Admin: never written here.
	Pinned bool
	// CalibratedAt is the last change this rule made, nil when none.
	CalibratedAt *time.Time
}

// GoalCadenceChange is one step of goal_stagnation_days and its evidence.
type GoalCadenceChange struct {
	Direction                  Direction
	Previous, Next             int
	Engaged, Declined, Ignored int
	SignalIDs                  []string
}

// StepGoalCadence is the learn pass's goal cadence rule (doc 02 §9,
// ADR-0055). It reads the newest GoalCadenceSample closed goal check-ins made
// strictly after the last change and no later than now. All of them ignored
// lengthens goal_stagnation_days by GoalCadenceStepDays; at least
// GoalCadenceEngagedToShorten engaged shortens it as much; anything else, or
// fewer than a full sample, holds. A decline is an answer, so it neither
// lengthens nor shortens. After a change the rule waits the new interval
// itself, so the change has time to show its effect. It never writes a pinned
// value, stays within [MinGoalStagnationDays, MaxGoalStagnationDays], and is
// idempotent once its change is recorded at now.
func StepGoalCadence(s GoalCadenceState, verdicts []GoalVerdict, now time.Time) (GoalCadenceChange, bool) {
	if s.Pinned {
		return GoalCadenceChange{}, false
	}
	if s.CalibratedAt != nil && now.Sub(*s.CalibratedAt) < time.Duration(s.Days)*24*time.Hour {
		return GoalCadenceChange{}, false
	}
	sample := make([]GoalVerdict, 0, len(verdicts))
	for _, v := range verdicts {
		if v.At.After(now) || s.CalibratedAt != nil && !v.At.After(*s.CalibratedAt) {
			continue
		}
		sample = append(sample, v)
	}
	if len(sample) < GoalCadenceSample {
		return GoalCadenceChange{}, false
	}
	slices.SortFunc(sample, func(a, b GoalVerdict) int {
		if c := b.At.Compare(a.At); c != 0 {
			return c
		}
		return strings.Compare(b.SignalID, a.SignalID)
	})
	sample = sample[:GoalCadenceSample]

	c := GoalCadenceChange{Previous: s.Days, SignalIDs: make([]string, len(sample))}
	for i, v := range sample {
		c.SignalIDs[i] = v.SignalID
		switch v.Kind {
		case GoalEngaged:
			c.Engaged++
		case GoalDeclined:
			c.Declined++
		case GoalIgnored:
			c.Ignored++
		}
	}
	switch {
	case c.Ignored == GoalCadenceSample:
		c.Direction, c.Next = Raised, min(s.Days+GoalCadenceStepDays, MaxGoalStagnationDays)
		if c.Next <= s.Days {
			return GoalCadenceChange{}, false
		}
	case c.Engaged >= GoalCadenceEngagedToShorten:
		c.Direction, c.Next = Lowered, max(s.Days-GoalCadenceStepDays, MinGoalStagnationDays)
		if c.Next >= s.Days {
			return GoalCadenceChange{}, false
		}
	default:
		return GoalCadenceChange{}, false
	}
	return c, true
}
