package brain

import (
	"testing"

	"github.com/rengo/nooma/internal/core/prospection"
	"github.com/rengo/nooma/internal/ports"
)

// TestTriggerTransition_MapsEveryVerdict and its timer sibling sweep
// prospection.AllVerdicts() rather than listing the four cases.
//
// The distinction is the whole point. A hand-written switch over the four
// verdicts we have today compiles unchanged the day a fifth is added, and
// says nothing about it — while these tests fail on the loop pass that
// finds no expectation for it. The mapping itself is brain's decision, not
// core's: core states a neutral verdict and never a schema status
// (staleness.go's own note), so this is where the two vocabularies meet
// and where a mistranslation would be invisible from either side.
func TestTriggerTransition_MapsEveryVerdict(t *testing.T) {
	verdicts := prospection.AllVerdicts()
	if len(verdicts) == 0 {
		t.Fatal("prospection.AllVerdicts() is empty — this sweep proves nothing")
	}

	want := map[prospection.Verdict]struct {
		status ports.TriggerStatus
		writes bool
	}{
		prospection.VerdictPending: {writes: false},
		prospection.VerdictDefer:   {writes: false},
		prospection.VerdictStale:   {status: ports.TriggerStatusExpired, writes: true},
		// Deliver writes nothing for a trigger, and that is not an
		// oversight: nothing in this change can surface a fired trigger,
		// so firing one here would record a delivery that never happened.
		// It stays armed and Due returns it again next pass.
		prospection.VerdictDeliver: {writes: false},
	}

	for _, v := range verdicts {
		expected, known := want[v]
		if !known {
			t.Errorf("verdict %q has no expected trigger transition — a member was added to the vocabulary and this mapping was not revisited", v)
			continue
		}

		status, writes := triggerTransition(v)
		if writes != expected.writes {
			t.Errorf("triggerTransition(%q) writes = %v, want %v", v, writes, expected.writes)
			continue
		}
		if writes && status != expected.status {
			t.Errorf("triggerTransition(%q) = %q, want %q", v, status, expected.status)
		}
	}
}

// TestTimerTransition_MapsEveryVerdict sweeps the same vocabulary for the
// timer half.
//
// VerdictDefer is unreachable for a timer — TimerVerdict passes
// deferInQuietHours = false, which is how a timer stays the one push
// exception to quiet hours — and it is still given a defined answer here
// rather than left to fall through. An unreachable case that panics is a
// crash waiting for the day it becomes reachable.
func TestTimerTransition_MapsEveryVerdict(t *testing.T) {
	verdicts := prospection.AllVerdicts()
	if len(verdicts) == 0 {
		t.Fatal("prospection.AllVerdicts() is empty — this sweep proves nothing")
	}

	want := map[prospection.Verdict]struct {
		status ports.TimerStatus
		writes bool
	}{
		prospection.VerdictPending: {writes: false},
		prospection.VerdictDefer:   {writes: false},
		prospection.VerdictStale:   {status: ports.TimerStatusCancelled, writes: true},
		prospection.VerdictDeliver: {status: ports.TimerStatusFired, writes: true},
	}

	for _, v := range verdicts {
		expected, known := want[v]
		if !known {
			t.Errorf("verdict %q has no expected timer transition — a member was added to the vocabulary and this mapping was not revisited", v)
			continue
		}

		status, writes := timerTransition(v)
		if writes != expected.writes {
			t.Errorf("timerTransition(%q) writes = %v, want %v", v, writes, expected.writes)
			continue
		}
		if writes && status != expected.status {
			t.Errorf("timerTransition(%q) = %q, want %q", v, status, expected.status)
		}
	}
}

// TestTransitions_ProduceOnlyVocabularyMembers is the guard the schema does
// not carry: triggers.status and timers.status have no CHECK constraint
// (migration 0001), so a mistyped literal in either mapping would be
// written happily and found much later. Whatever these functions return
// must be a member of the port's own vocabulary.
func TestTransitions_ProduceOnlyVocabularyMembers(t *testing.T) {
	triggerStatuses := map[ports.TriggerStatus]bool{}
	for _, s := range ports.AllTriggerStatuses() {
		triggerStatuses[s] = true
	}
	timerStatuses := map[ports.TimerStatus]bool{}
	for _, s := range ports.AllTimerStatuses() {
		timerStatuses[s] = true
	}

	for _, v := range prospection.AllVerdicts() {
		if status, writes := triggerTransition(v); writes && !triggerStatuses[status] {
			t.Errorf("triggerTransition(%q) = %q, which is not a member of ports.AllTriggerStatuses()", v, status)
		}
		if status, writes := timerTransition(v); writes && !timerStatuses[status] {
			t.Errorf("timerTransition(%q) = %q, which is not a member of ports.AllTimerStatuses()", v, status)
		}
	}
}
