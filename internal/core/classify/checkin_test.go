package classify

import "testing"

// TestResolveCheckin is the one mapping from a reading's check-in outcomes to
// the resolution it closes the open check-in with (doc 02 §5 step 1). brain
// acts on it and internal/reading judges a recording by it, so the two cannot
// disagree about what a reading does.
func TestResolveCheckin(t *testing.T) {
	n := func(o NudgeOutcome) *NudgeOutcome { return &o }
	k := func(o TaskCheckinOutcome) *TaskCheckinOutcome { return &o }
	for name, tc := range map[string]struct {
		c    Classification
		want CheckinResolution
		ok   bool
	}{
		"nothing":                            {Classification{}, "", false},
		"engaged":                            {Classification{NudgeOutcome: n(NudgeOutcomeEngaged)}, CheckinEngaged, true},
		"declined":                           {Classification{NudgeOutcome: n(NudgeOutcomeDeclined)}, CheckinDeclined, true},
		"done":                               {Classification{TaskCheckinOutcome: k(TaskCheckinOutcomeDone)}, CheckinEngaged, true},
		"drop":                               {Classification{TaskCheckinOutcome: k(TaskCheckinOutcomeDrop)}, CheckinDeclined, true},
		"snooze leaves it open":              {Classification{TaskCheckinOutcome: k(TaskCheckinOutcomeSnooze)}, "", false},
		"the nudge wins over a done":         {Classification{NudgeOutcome: n(NudgeOutcomeDeclined), TaskCheckinOutcome: k(TaskCheckinOutcomeDone)}, CheckinDeclined, true},
		"the nudge wins over a snooze":       {Classification{NudgeOutcome: n(NudgeOutcomeEngaged), TaskCheckinOutcome: k(TaskCheckinOutcomeSnooze)}, CheckinEngaged, true},
		"an unknown nudge falls to the task": {Classification{NudgeOutcome: n("maybe"), TaskCheckinOutcome: k(TaskCheckinOutcomeDrop)}, CheckinDeclined, true},
		"an unknown task outcome resolves nothing": {Classification{TaskCheckinOutcome: k("later")}, "", false},
	} {
		t.Run(name, func(t *testing.T) {
			got, ok := ResolveCheckin(tc.c)
			if got != tc.want || ok != tc.ok {
				t.Errorf("ResolveCheckin = %q, %v; want %q, %v", got, ok, tc.want, tc.ok)
			}
		})
	}
}
