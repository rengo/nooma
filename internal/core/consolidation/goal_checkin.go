package consolidation

import "time"

// GoalCheckInDeclinedFactor is how many goal_stagnation_days a goal waits for
// its next check-in after the user declined the last one ("dejala
// descansar"): it is asked less often, never retired (doc 02 §7; the client's
// answer 5 of 2026-10-27, and its ruling of 2026-10-28 on the number).
const GoalCheckInDeclinedFactor = 2

// GoalCheckIn is a goal's latest check-in as the vault holds it.
type GoalCheckIn struct {
	BeliefID string
	// ArmedAt is when the check-in was armed; the next waits from it.
	ArmedAt time.Time
	// Open is a check-in still waiting: armed, or delivered and unanswered.
	Open bool
	// Declined is a check-in the user answered declined.
	Declined bool
}

// GoalCheckInsDue is the stagnant goals that are due a check-in (doc 02 §7,
// ADR-0054), in findings' order: one never asked, or whose last check-in was
// armed at least stagnationDays ago, GoalCheckInDeclinedFactor times that
// when it was declined. A goal whose check-in is still open is not asked again
// on top of it, so a goal is asked at most once per interval however many
// nights it stays stagnant. Both boundaries are inclusive.
func GoalCheckInsDue(findings []StagnationFinding, last map[string]GoalCheckIn, stagnationDays int, now time.Time) []StagnationFinding {
	var due []StagnationFinding
	for _, f := range findings {
		l, asked := last[f.BeliefID]
		if asked {
			if l.Open {
				continue
			}
			days := stagnationDays
			if l.Declined {
				days *= GoalCheckInDeclinedFactor
			}
			if now.Sub(l.ArmedAt) < time.Duration(days)*24*time.Hour {
				continue
			}
		}
		due = append(due, f)
	}
	return due
}
