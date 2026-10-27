package consolidation

import (
	"testing"
	"time"
)

var goalCheckinNow = time.Date(2026, 11, 20, 3, 0, 0, 0, time.UTC)

func daysAgo(n int) time.Time { return goalCheckinNow.Add(-time.Duration(n) * 24 * time.Hour) }

// TestGoalCheckInsDue: a stagnant goal is asked at most once per
// goal_stagnation_days, counted from when its last check-in was armed, and
// after a declined answer only after GoalCheckInDeclinedFactor times that
// (doc 02 §7, the client's answer 5 of 2026-10-27). One still open is never
// asked again on top of itself.
func TestGoalCheckInsDue(t *testing.T) {
	stagnant := []StagnationFinding{{BeliefID: "b-run", TopicKey: "goal/run", StagnantDays: 30}}
	for _, tc := range []struct {
		name string
		last *GoalCheckIn
		want bool
	}{
		{"never asked", nil, true},
		{"one still open", &GoalCheckIn{BeliefID: "b-run", ArmedAt: daysAgo(60), Open: true}, false},
		{"answered inside the interval", &GoalCheckIn{BeliefID: "b-run", ArmedAt: daysAgo(20)}, false},
		{"answered exactly an interval ago", &GoalCheckIn{BeliefID: "b-run", ArmedAt: daysAgo(21)}, true},
		{"declined an interval ago", &GoalCheckIn{BeliefID: "b-run", ArmedAt: daysAgo(21), Declined: true}, false},
		{"declined just under twice the interval ago", &GoalCheckIn{BeliefID: "b-run", ArmedAt: daysAgo(42).Add(time.Second), Declined: true}, false},
		{"declined twice the interval ago", &GoalCheckIn{BeliefID: "b-run", ArmedAt: daysAgo(42), Declined: true}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			last := map[string]GoalCheckIn{}
			if tc.last != nil {
				last[tc.last.BeliefID] = *tc.last
			}
			got := GoalCheckInsDue(stagnant, last, 21, goalCheckinNow)
			if (len(got) == 1) != tc.want {
				t.Errorf("GoalCheckInsDue = %+v, want due %v", got, tc.want)
			}
		})
	}
}

// TestGoalCheckInsDue_KeepsEachGoalApart: one goal's check-in says nothing
// about another's, and the findings keep their order.
func TestGoalCheckInsDue_KeepsEachGoalApart(t *testing.T) {
	stagnant := []StagnationFinding{{BeliefID: "a"}, {BeliefID: "b"}, {BeliefID: "c"}}
	last := map[string]GoalCheckIn{"b": {BeliefID: "b", ArmedAt: daysAgo(1)}}
	got := GoalCheckInsDue(stagnant, last, 21, goalCheckinNow)
	if len(got) != 2 || got[0].BeliefID != "a" || got[1].BeliefID != "c" {
		t.Errorf("GoalCheckInsDue = %+v, want a and c", got)
	}
}

func TestGoalCheckInDeclinedFactor_IsPinned(t *testing.T) {
	if GoalCheckInDeclinedFactor != 2 {
		t.Errorf("GoalCheckInDeclinedFactor = %d, want doc 02 §13's 2", GoalCheckInDeclinedFactor)
	}
}
