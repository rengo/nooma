package learning

import (
	"fmt"
	"testing"
	"time"
)

var cadenceNow = time.Date(2026, 12, 1, 3, 0, 0, 0, time.UTC)

// goalVerdicts builds n closed goal check-ins of kind, a day apart, newest
// last, the newest a day before now.
func goalVerdicts(kinds ...GoalVerdictKind) []GoalVerdict {
	var out []GoalVerdict
	for i, k := range kinds {
		out = append(out, GoalVerdict{SignalID: fmt.Sprintf("g-%02d", i), Kind: k,
			At: cadenceNow.Add(-time.Duration(len(kinds)-i) * 24 * time.Hour)})
	}
	return out
}

func repeat(k GoalVerdictKind, n int) []GoalVerdictKind {
	out := make([]GoalVerdictKind, n)
	for i := range out {
		out[i] = k
	}
	return out
}

// TestStepGoalCadence_FiveIgnoredLengthenTheInterval is doc 02 §9's own
// example: five nudges with no response are five ignored ones, 21 -> 28.
func TestStepGoalCadence_FiveIgnoredLengthenTheInterval(t *testing.T) {
	c, ok := StepGoalCadence(GoalCadenceState{Days: 21}, goalVerdicts(repeat(GoalIgnored, 5)...), cadenceNow)
	if !ok || c.Previous != 21 || c.Next != 28 || c.Direction != Raised || c.Ignored != 5 || len(c.SignalIDs) != 5 {
		t.Errorf("change = %+v, %v; want 21 -> 28 on 5 ignored", c, ok)
	}
}

func TestStepGoalCadence_ThreeEngagedShortenIt(t *testing.T) {
	c, ok := StepGoalCadence(GoalCadenceState{Days: 21}, goalVerdicts(GoalIgnored, GoalEngaged, GoalDeclined, GoalEngaged, GoalEngaged), cadenceNow)
	if !ok || c.Next != 14 || c.Direction != Lowered || c.Engaged != 3 || c.Declined != 1 || c.Ignored != 1 {
		t.Errorf("change = %+v, %v; want 21 -> 14 on 3 engaged", c, ok)
	}
}

// TestStepGoalCadence_AnythingElseHolds: four ignored is not five, a decline
// is a response, and fewer than five closed check-ins are not yet evidence.
func TestStepGoalCadence_AnythingElseHolds(t *testing.T) {
	for name, kinds := range map[string][]GoalVerdictKind{
		"four ignored and a decline": {GoalIgnored, GoalIgnored, GoalDeclined, GoalIgnored, GoalIgnored},
		"two engaged":                {GoalEngaged, GoalIgnored, GoalEngaged, GoalIgnored, GoalIgnored},
		"four ignored only":          repeat(GoalIgnored, 4),
		"five declined":              repeat(GoalDeclined, 5),
		// #415 review B1: fewer than a full sample is not yet evidence,
		// even when it already holds enough engaged to shorten.
		"three engaged and nothing else": repeat(GoalEngaged, 3),
		"four engaged and nothing else":  repeat(GoalEngaged, 4),
	} {
		if c, ok := StepGoalCadence(GoalCadenceState{Days: 21}, goalVerdicts(kinds...), cadenceNow); ok {
			t.Errorf("%s moved the interval: %+v", name, c)
		}
	}
}

// TestStepGoalCadence_OnlyTheNewestFiveSinceTheLastChange: older check-ins
// fall out, and those before the last change were already counted.
func TestStepGoalCadence_OnlyTheNewestFiveSinceTheLastChange(t *testing.T) {
	kinds := append(repeat(GoalEngaged, 3), repeat(GoalIgnored, 5)...)
	if c, ok := StepGoalCadence(GoalCadenceState{Days: 21}, goalVerdicts(kinds...), cadenceNow); !ok || c.Next != 28 {
		t.Errorf("change = %+v, %v; want the newest five, all ignored, to lengthen it", c, ok)
	}
	vs := goalVerdicts(repeat(GoalIgnored, 5)...)
	changed := vs[0].At // the oldest is at the change: not after it
	if c, ok := StepGoalCadence(GoalCadenceState{Days: 1, CalibratedAt: &changed}, vs, cadenceNow); ok {
		t.Errorf("counted a check-in from the instant of the last change: %+v", c)
	}
}

// TestStepGoalCadence_CooldownIsTheIntervalItself: after a change, doc 02 §9
// says to wait until the new interval has had time to show its effect.
func TestStepGoalCadence_CooldownIsTheIntervalItself(t *testing.T) {
	vs := goalVerdicts(repeat(GoalIgnored, 5)...)
	inside := cadenceNow.Add(-28*24*time.Hour + time.Second)
	exactly := cadenceNow.Add(-28 * 24 * time.Hour)
	before := exactly.Add(-6 * 24 * time.Hour) // before every verdict, so all count
	if c, ok := StepGoalCadence(GoalCadenceState{Days: 28, CalibratedAt: &inside}, vs, cadenceNow); ok {
		t.Errorf("moved inside the cooldown: %+v", c)
	}
	for _, at := range []time.Time{exactly, before} {
		if _, ok := StepGoalCadence(GoalCadenceState{Days: 28, CalibratedAt: &at}, vs, cadenceNow); !ok {
			t.Errorf("held with the cooldown over (changed at %s)", at)
		}
	}
}

func TestStepGoalCadence_APinnedIntervalIsNeverWritten(t *testing.T) {
	if c, ok := StepGoalCadence(GoalCadenceState{Days: 21, Pinned: true}, goalVerdicts(repeat(GoalIgnored, 5)...), cadenceNow); ok {
		t.Errorf("a pinned interval moved: %+v", c)
	}
}

// TestStepGoalCadence_StaysWithinItsBounds: [7, 56], a step past a bound
// lands on it, and one at a bound, or that would move the wrong way, is none.
func TestStepGoalCadence_StaysWithinItsBounds(t *testing.T) {
	for _, tc := range []struct {
		days  int
		kinds []GoalVerdictKind
		next  int
		ok    bool
	}{
		{56, repeat(GoalIgnored, 5), 0, false},
		{52, repeat(GoalIgnored, 5), 56, true},
		{7, repeat(GoalEngaged, 5), 0, false},
		{10, repeat(GoalEngaged, 5), 7, true},
		{60, repeat(GoalIgnored, 5), 0, false},
		{3, repeat(GoalEngaged, 5), 0, false},
	} {
		c, ok := StepGoalCadence(GoalCadenceState{Days: tc.days}, goalVerdicts(tc.kinds...), cadenceNow)
		if ok != tc.ok || ok && c.Next != tc.next {
			t.Errorf("from %d: %+v, %v; want ok %v at %d", tc.days, c, ok, tc.ok, tc.next)
		}
	}
}

// TestStepGoalCadence_IsIdempotent: once its change is applied at now, the
// same evidence moves nothing, now or after the cooldown.
func TestStepGoalCadence_IsIdempotent(t *testing.T) {
	vs := goalVerdicts(repeat(GoalIgnored, 5)...)
	var c GoalCadenceChange
	c, ok := StepGoalCadence(GoalCadenceState{Days: 21}, vs, cadenceNow)
	if !ok {
		t.Fatal("no first change")
	}
	at := cadenceNow
	for _, later := range []time.Time{cadenceNow, cadenceNow.Add(60 * 24 * time.Hour)} {
		if c2, ok := StepGoalCadence(GoalCadenceState{Days: c.Next, CalibratedAt: &at}, vs, later); ok {
			t.Errorf("the same evidence moved it again at %s: %+v", later, c2)
		}
	}
}

func TestStepGoalCadence_IgnoresWhatIsNotYetTrue(t *testing.T) {
	vs := goalVerdicts(repeat(GoalIgnored, 5)...)
	for i := range vs {
		vs[i].At = cadenceNow.Add(time.Duration(i+1) * time.Minute)
	}
	if c, ok := StepGoalCadence(GoalCadenceState{Days: 21}, vs, cadenceNow); ok {
		t.Errorf("moved on check-ins from the future: %+v", c)
	}
}

func TestGoalCadenceConstants_ArePinned(t *testing.T) {
	if GoalCadenceSample != 5 || GoalCadenceEngagedToShorten != 3 || GoalCadenceStepDays != 7 ||
		MinGoalStagnationDays != 7 || MaxGoalStagnationDays != 56 {
		t.Error("a goal cadence constant moved from doc 02 §13's value")
	}
	if GoalCadenceEngagedToShorten*2 <= GoalCadenceSample {
		t.Error("a sample could both lengthen and shorten: engaged-to-shorten must be a majority")
	}
}
