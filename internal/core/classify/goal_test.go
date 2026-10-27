package classify

import (
	"strings"
	"testing"
	"time"
)

var goalNow = time.Date(2026, 10, 28, 9, 0, 0, 0, time.FixedZone("ART", -3*60*60))

func goalChat() Conversation {
	return Conversation{Turns: []Turn{{At: goalNow.Add(-time.Hour), Speaker: SpeakerPush, Text: "¿Seguís con correr una media maratón?"}}}
}

// TestBuildPrompt_ListsTheOpenGoalCheckIns (ADR-0054): beside a chat, an open
// goal check-in is listed by position under its own heading, with the two
// fields that answer it, and the rule that nothing else names one.
func TestBuildPrompt_ListsTheOpenGoalCheckIns(t *testing.T) {
	goals := []GoalEntry{{Text: "¿Seguís con correr una media maratón?"}, {Text: "¿Seguís con aprender alemán?"}}
	p := BuildPrompt("sí, ayer salí a correr", nil, goalNow, 0.5, ReminderContext{Conversation: goalChat(), Goals: goals})
	if !strings.Contains(p, goalsHeading+"\n"+RenderGoalList(goals)) {
		t.Errorf("the prompt does not list the open goal check-ins under %q:\n%s", goalsHeading, p)
	}
	flat := strings.Join(strings.Fields(p), " ")
	for _, want := range []string{
		"goal_target",
		"goal_outcome one of: " + joinVocabulary(AllGoalOutcomes()),
		"even several messages after it was asked",
		"Only an answer to its check-in names a goal",
		"Never invent a position",
	} {
		if !strings.Contains(flat, want) {
			t.Errorf("the prompt does not state %q", want)
		}
	}
}

// TestBuildPrompt_NoOpenGoalCheckInNoGoalMachinery: with no open goal
// check-in, or read alone, the prompt is the one every recording was made
// against, so no recording has to be made again for it.
func TestBuildPrompt_NoOpenGoalCheckInNoGoalMachinery(t *testing.T) {
	goals := []GoalEntry{{Text: "¿Seguís con correr?"}}
	for name, rc := range map[string]ReminderContext{
		"a chat with no goal check-in": {Conversation: goalChat()},
		"a goal check-in read alone":   {Goals: goals},
	} {
		without := ReminderContext{Conversation: rc.Conversation}
		if got, want := BuildPrompt("sí", nil, goalNow, 0.5, rc), BuildPrompt("sí", nil, goalNow, 0.5, without); got != want {
			t.Errorf("%s: the prompt changed", name)
		}
		if p := BuildPrompt("sí", nil, goalNow, 0.5, rc); strings.Contains(p, "goal_target") {
			t.Errorf("%s: the prompt carries the goal fields", name)
		}
	}
}

func TestDecode_GoalFields(t *testing.T) {
	base := `{"type":"chitchat","normalized_content":"Sí.","weight":0.1,"decay_rate":0.3,`
	for _, tc := range []struct {
		name, fields string
		position     int
		unclear      bool
		outcome      GoalOutcome
		degraded     bool
	}{
		{"engaged", `"goal_target":1,"goal_outcome":"engaged"}`, 1, false, GoalOutcomeEngaged, false},
		{"declined", `"goal_target":2,"goal_outcome":"declined"}`, 2, false, GoalOutcomeDeclined, false},
		{"snooze", `"goal_target":1,"goal_outcome":"snooze"}`, 1, false, GoalOutcomeSnooze, false},
		{"unclear", `"goal_target":"unclear","goal_outcome":"engaged"}`, 0, true, GoalOutcomeEngaged, false},
		{"an outcome outside the vocabulary", `"goal_target":1,"goal_outcome":"retired"}`, 1, false, "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, err := Decode(base+tc.fields, goalNow)
			if err != nil {
				t.Fatal(err)
			}
			if c.GoalTarget == nil || c.GoalTarget.Position != tc.position || c.GoalTarget.Unclear != tc.unclear {
				t.Errorf("goal_target = %+v", c.GoalTarget)
			}
			if tc.outcome == "" && c.GoalOutcome != nil || tc.outcome != "" && (c.GoalOutcome == nil || *c.GoalOutcome != tc.outcome) {
				t.Errorf("goal_outcome = %v, want %q", c.GoalOutcome, tc.outcome)
			}
			degraded := false
			for _, d := range c.Degradations {
				degraded = degraded || d.Field == "goal_outcome"
			}
			if degraded != tc.degraded {
				t.Errorf("goal_outcome degraded = %v, want %v (%+v)", degraded, tc.degraded, c.Degradations)
			}
		})
	}
}

// TestReadGoalAnswer: a goal answer binds only a position the prompt listed,
// with an outcome; anything less answers no goal check-in.
func TestReadGoalAnswer(t *testing.T) {
	pos := func(n int) *ReminderTarget { return &ReminderTarget{Position: n} }
	engaged := GoalOutcomeEngaged
	for _, tc := range []struct {
		name    string
		c       Classification
		listed  int
		wantPos int
		wantOK  bool
	}{
		{"a listed position with an outcome", Classification{GoalTarget: pos(2), GoalOutcome: &engaged}, 2, 2, true},
		{"no outcome", Classification{GoalTarget: pos(1)}, 1, 0, false},
		{"no target", Classification{GoalOutcome: &engaged}, 1, 0, false},
		{"unclear", Classification{GoalTarget: &ReminderTarget{Unclear: true}, GoalOutcome: &engaged}, 1, 0, false},
		{"a position past the list", Classification{GoalTarget: pos(2), GoalOutcome: &engaged}, 1, 0, false},
		{"a position before it", Classification{GoalTarget: pos(0), GoalOutcome: &engaged}, 1, 0, false},
		{"nothing listed", Classification{GoalTarget: pos(1), GoalOutcome: &engaged}, 0, 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, o, ok := ReadGoalAnswer(tc.c, tc.listed)
			if ok != tc.wantOK || p != tc.wantPos || ok && o != engaged {
				t.Errorf("ReadGoalAnswer = %d, %q, %v; want %d, ok %v", p, o, ok, tc.wantPos, tc.wantOK)
			}
		})
	}
}

func TestRenderGoalList(t *testing.T) {
	got := RenderGoalList([]GoalEntry{{Text: "¿Seguís con\ncorrer?"}, {Text: "¿Seguís con alemán?"}})
	if got != "1) ¿Seguís con correr?\n2) ¿Seguís con alemán?\n" {
		t.Errorf("RenderGoalList = %q", got)
	}
}
