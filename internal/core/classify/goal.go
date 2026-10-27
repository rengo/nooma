package classify

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"
)

// GoalOutcome is how a message answers an open goal check-in (doc 02 §7,
// ADR-0054): still on it, let it rest, or not now.
type GoalOutcome string

const (
	GoalOutcomeEngaged  GoalOutcome = "engaged"
	GoalOutcomeDeclined GoalOutcome = "declined"
	GoalOutcomeSnooze   GoalOutcome = "snooze"
)

// AllGoalOutcomes returns a fresh slice of the GoalOutcome vocabulary.
func AllGoalOutcomes() []GoalOutcome {
	return []GoalOutcome{GoalOutcomeEngaged, GoalOutcomeDeclined, GoalOutcomeSnooze}
}

// GoalEntry is one open goal check-in as the prompt lists it: what Nooma
// asked. Nothing else about the goal is shown.
type GoalEntry struct {
	Text string
}

// goalsHeading heads the numbered list of open goal check-ins.
const goalsHeading = "Goal check-ins waiting for an answer"

// RenderGoalList is the prompt's numbered list of open goal check-ins, "1) <what
// Nooma asked>", at most MaxReminderChoices, one line each, clipped as a label.
func RenderGoalList(entries []GoalEntry) string {
	var b strings.Builder
	for i, e := range entries {
		if i == MaxReminderChoices {
			break
		}
		b.WriteString(strconv.Itoa(i+1) + ") " + clipLabel(oneLine(e.Text)) + "\n")
	}
	return b.String()
}

// writeGoals renders the goal check-ins a message may answer and the two
// fields that answer one (ADR-0054). It is written only beside a chat with an
// open goal check-in, so every other prompt is the one recorded before it.
func writeGoals(b *strings.Builder, goals []GoalEntry) {
	b.WriteString("Goal check-ins\n")
	b.WriteString("  Nooma asked the user about a goal of theirs and is waiting for the answer.\n")
	b.WriteString("  Only an answer to its check-in names a goal: a correction, a done, a reminder,\n")
	b.WriteString("  a new note or a recall about the same topic names none, and never sets these\n")
	b.WriteString("  fields.\n")
	b.WriteString("  goal_target        the position, 1 to " + strconv.Itoa(len(goals)) + ", of the check-in listed below that the\n")
	b.WriteString("                     message answers, by the goal it is about, even several messages\n")
	b.WriteString("                     after it was asked, or \"" + ReminderTargetUnclear + "\" when you cannot tell which.\n")
	b.WriteString("                     Never invent a position\n")
	b.WriteString("  goal_outcome       one of: " + joinVocabulary(AllGoalOutcomes()) + "\n")
	b.WriteString("                     engaged: still on it, or tells of doing it (\"sí\", \"ayer salí a\n")
	b.WriteString("                     correr\"). declined: let it rest for now (\"dejala descansar\").\n")
	b.WriteString("                     snooze: not now, ask again later (\"no por ahora\").\n")
	b.WriteString("  A goal answer travels in these two fields only, never in nudge_outcome.\n")
	b.WriteString("\n" + goalsHeading + "\n")
	b.WriteString(RenderGoalList(goals))
	b.WriteString("\n")
}

// assignGoalTarget reads goal_target as timer_target is read.
func assignGoalTarget(raw json.RawMessage, c *Classification, _ time.Time) Reason {
	t, r := readTarget(raw)
	c.GoalTarget = t
	return r
}

// ReadGoalAnswer is the goal check-in a classification answers, among the
// listed ones, and how: a position the prompt listed, 1 to listed, with an
// outcome. Anything less answers none (ADR-0054): the code binds only a
// check-in it showed.
func ReadGoalAnswer(c Classification, listed int) (int, GoalOutcome, bool) {
	if c.GoalTarget == nil || c.GoalTarget.Unclear || c.GoalOutcome == nil {
		return 0, "", false
	}
	if p := c.GoalTarget.Position; p >= 1 && p <= min(listed, MaxReminderChoices) {
		return p, *c.GoalOutcome, true
	}
	return 0, "", false
}
