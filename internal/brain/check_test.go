package brain

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

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
		// Deliver fires, now that m3d can deliver. m3b mapped this to no
		// write and said in the same breath that it would change when
		// something could surface a fired trigger; this is that.
		prospection.VerdictDeliver: {status: ports.TriggerStatusFired, writes: true},
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

// conflictingTriggers is a ports.TriggerRepo whose Due returns two rows and
// whose Expire refuses the first with ErrTriggerStatusConflict — the exact
// shape a concurrent scan produces: Due read a row that was still armed,
// and by the time the UPDATE ran another pass had already moved it.
//
// A fake, and stated as one: it proves the runner's arm is reached when the
// port returns the sentinel, and it cannot and does not claim to prove the
// race itself. That claim belongs to L3, against a real vault whose
// _txlock=immediate DSN is the thing under test
// (test/integration/due_scan_concurrent_test.go).
type conflictingTriggers struct {
	due          []ports.DueTrigger
	conflictOnID string
	expired      []string
}

func (r *conflictingTriggers) Create(context.Context, ports.Trigger) error { return nil }

func (r *conflictingTriggers) Due(context.Context, time.Time) ([]ports.DueTrigger, error) {
	return r.due, nil
}

func (r *conflictingTriggers) Fire(context.Context, string, time.Time) error { return nil }

func (r *conflictingTriggers) Expire(_ context.Context, id string) error {
	if id == r.conflictOnID {
		return ports.ErrTriggerStatusConflict
	}
	r.expired = append(r.expired, id)
	return nil
}

// TestCheck_ConflictIsRecordedAndTheScanContinues is R5.4.
//
// A transition another pass already made is not this pass's failure. The
// arm records check.conflict_skipped and keeps going, so one contended row
// cannot cost every row behind it in the same scan — the shape
// persistArchiveTransitions already established for archive's own race.
//
// The assertion that matters is the third one: the SECOND row still gets
// processed. A conflict arm that recorded the row and then returned would
// pass an "exactly one conflict row" check and still abort the pass.
func TestCheck_ConflictIsRecordedAndTheScanContinues(t *testing.T) {
	fireAt := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	now := fireAt.Add(time.Duration(prospection.TriggerStalenessHours+1) * time.Hour)

	triggers := &conflictingTriggers{
		conflictOnID: "trg-lost-the-race",
		due: []ports.DueTrigger{
			{ID: "trg-lost-the-race", FireAt: fireAt},
			{ID: "trg-behind-it", FireAt: fireAt},
		},
	}
	log := &recordingLog{}

	report, err := checkRunner{
		triggers: triggers,
		timers:   &emptyTimers{},
		ids:      &countingIDs{},
		log:      log,
	}.at(context.Background(), now, true)
	if err != nil {
		t.Fatalf("at: %v — a transition another pass already made is not this pass's failure", err)
	}

	if got := log.count(ports.ActionCheckConflictSkipped); got != 1 {
		t.Errorf("%d check.conflict_skipped rows, want 1", got)
	}
	if got := log.count(ports.ActionCheckTriggerExpired); got != 1 {
		t.Errorf("%d check.trigger.expired rows, want 1 — the row behind the contended one must still be processed", got)
	}
	if len(triggers.expired) != 1 || triggers.expired[0] != "trg-behind-it" {
		t.Errorf("expired %v, want exactly [trg-behind-it]", triggers.expired)
	}
	if report.TriggersExpired != 1 {
		t.Errorf("TriggersExpired = %d, want 1 — a skipped conflict is not an effect", report.TriggersExpired)
	}
}

// conflictingTimers mirrors conflictingTriggers for the timer half: Fire
// refuses, so the timer's own delivering transition exercises the arm too.
type conflictingTimers struct {
	due          []ports.DueTimer
	conflictOnID string
	fired        []string
}

func (r *conflictingTimers) Create(context.Context, ports.Timer) error { return nil }

func (r *conflictingTimers) Due(context.Context, time.Time) ([]ports.DueTimer, error) {
	return r.due, nil
}

func (r *conflictingTimers) Fire(_ context.Context, id string, _ time.Time) error {
	if id == r.conflictOnID {
		return ports.ErrTimerStatusConflict
	}
	r.fired = append(r.fired, id)
	return nil
}

func (r *conflictingTimers) Cancel(context.Context, string) error { return nil }

// TestCheck_TimerConflictIsRecordedAndTheScanContinues is the timer half.
// Both ports get the arm, and both are asserted, because "it works for
// triggers" is not evidence about a switch arm in another loop.
func TestCheck_TimerConflictIsRecordedAndTheScanContinues(t *testing.T) {
	fireAt := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	now := fireAt.Add(time.Minute)

	timers := &conflictingTimers{
		conflictOnID: "tmr-lost-the-race",
		due: []ports.DueTimer{
			{ID: "tmr-lost-the-race", FireAt: fireAt},
			{ID: "tmr-behind-it", FireAt: fireAt},
		},
	}
	log := &recordingLog{}

	report, err := checkRunner{
		triggers: &emptyTriggers{},
		timers:   timers,
		ids:      &countingIDs{},
		log:      log,
	}.at(context.Background(), now, true)
	if err != nil {
		t.Fatalf("at: %v", err)
	}

	if got := log.count(ports.ActionCheckConflictSkipped); got != 1 {
		t.Errorf("%d check.conflict_skipped rows, want 1", got)
	}
	if got := log.count(ports.ActionCheckTimerFired); got != 1 {
		t.Errorf("%d check.timer.fired rows, want 1", got)
	}
	if len(timers.fired) != 1 || timers.fired[0] != "tmr-behind-it" {
		t.Errorf("fired %v, want exactly [tmr-behind-it]", timers.fired)
	}
	if report.TimersFired != 1 {
		t.Errorf("TimersFired = %d, want 1", report.TimersFired)
	}
}

// TestCheck_AnyOtherErrorStillAbortsTheScan is the other half of R5.4, and
// the one a permissive conflict arm quietly destroys: only a conflict is
// survivable. A repository that cannot be reached at all is not a race, and
// swallowing it would turn a broken vault into a scan that reports doing
// nothing.
func TestCheck_AnyOtherErrorStillAbortsTheScan(t *testing.T) {
	fireAt := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	now := fireAt.Add(time.Duration(prospection.TriggerStalenessHours+1) * time.Hour)

	broken := &failingTriggers{
		due: []ports.DueTrigger{{ID: "trg", FireAt: fireAt}},
		err: errors.New("disk went away"),
	}

	if _, err := (checkRunner{triggers: broken, timers: &emptyTimers{}, ids: &countingIDs{}, log: &recordingLog{}}).
		at(context.Background(), now, true); err == nil {
		t.Fatal("at returned nil for a non-conflict repository error — only a conflict is survivable")
	}
}

type failingTriggers struct {
	due []ports.DueTrigger
	err error
}

func (r *failingTriggers) Create(context.Context, ports.Trigger) error { return nil }
func (r *failingTriggers) Due(context.Context, time.Time) ([]ports.DueTrigger, error) {
	return r.due, nil
}
func (r *failingTriggers) Fire(context.Context, string, time.Time) error { return nil }
func (r *failingTriggers) Expire(context.Context, string) error          { return r.err }

type emptyTriggers struct{}

func (emptyTriggers) Create(context.Context, ports.Trigger) error { return nil }
func (emptyTriggers) Due(context.Context, time.Time) ([]ports.DueTrigger, error) {
	return nil, nil
}
func (emptyTriggers) Fire(context.Context, string, time.Time) error { return nil }
func (emptyTriggers) Expire(context.Context, string) error          { return nil }

type emptyTimers struct{}

func (emptyTimers) Create(context.Context, ports.Timer) error                { return nil }
func (emptyTimers) Due(context.Context, time.Time) ([]ports.DueTimer, error) { return nil, nil }
func (emptyTimers) Fire(context.Context, string, time.Time) error            { return nil }
func (emptyTimers) Cancel(context.Context, string) error                     { return nil }

// recordingLog is a ports.DecisionLog that only counts, because that is all
// these cases assert.
type recordingLog struct{ actions []ports.DecisionAction }

func (l *recordingLog) Record(_ context.Context, d ports.Decision) error {
	l.actions = append(l.actions, d.Action)
	return nil
}

func (l *recordingLog) Since(context.Context, time.Time, int) ([]ports.Decision, error) {
	return nil, nil
}

func (l *recordingLog) count(action ports.DecisionAction) int {
	n := 0
	for _, a := range l.actions {
		if a == action {
			n++
		}
	}
	return n
}

type countingIDs struct{ n int }

func (g *countingIDs) New() string {
	g.n++
	return fmt.Sprintf("id-%d", g.n)
}

// TestCheckDryRun_ReachesTheSameVerdictsAndWritesNothing is owner decision
// Q1 at the layer that can prove it cheapest: same runner, same rows, same
// counts, no writes and no rows.
//
// The two runs use separate repository doubles seeded identically rather
// than one vault twice, so the wet run's own effects cannot be what makes
// the dry run look correct.
func TestCheckDryRun_ReachesTheSameVerdictsAndWritesNothing(t *testing.T) {
	fireAt := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	now := fireAt.Add(time.Duration(prospection.TriggerStalenessHours+1) * time.Hour)

	seed := func() (*conflictingTriggers, *conflictingTimers, *recordingLog) {
		return &conflictingTriggers{due: []ports.DueTrigger{{ID: "trg", FireAt: fireAt}}},
			&conflictingTimers{due: []ports.DueTimer{{ID: "tmr", FireAt: fireAt}}},
			&recordingLog{}
	}

	dryTriggers, dryTimers, dryLog := seed()
	dry, err := checkRunner{triggers: dryTriggers, timers: dryTimers, ids: &countingIDs{}, log: dryLog}.
		at(context.Background(), now, false)
	if err != nil {
		t.Fatalf("dry run: %v", err)
	}

	if len(dryLog.actions) != 0 {
		t.Errorf("the dry run wrote %v, want nothing", dryLog.actions)
	}
	if len(dryTriggers.expired) != 0 {
		t.Errorf("the dry run expired %v, want nothing", dryTriggers.expired)
	}
	if len(dryTimers.fired) != 0 {
		t.Errorf("the dry run fired %v, want nothing", dryTimers.fired)
	}

	wetTriggers, wetTimers, wetLog := seed()
	wet, err := checkRunner{triggers: wetTriggers, timers: wetTimers, ids: &countingIDs{}, log: wetLog}.
		at(context.Background(), now, true)
	if err != nil {
		t.Fatalf("wet run: %v", err)
	}

	if dry != wet {
		t.Fatalf("dry run reported %+v, wet run reported %+v — a dry run must reach the identical verdicts", dry, wet)
	}
	if len(wetLog.actions) == 0 {
		t.Fatal("the wet run wrote nothing — this test's own premise is gone")
	}
}

// The delivery half of ports.TriggerRepo, which these fixtures do not
// exercise: m3d PR 2 widened the port and these tests are about the scan.
func (r *conflictingTriggers) Surface(context.Context, string, time.Time) error { return nil }
func (r *conflictingTriggers) Undelivered(context.Context) ([]ports.DueTrigger, error) {
	return nil, nil
}
func (r *conflictingTriggers) Delivered(context.Context) ([]ports.DueTrigger, error) {
	return nil, nil
}
func (r *conflictingTriggers) Resolve(context.Context, string, ports.TriggerResolution, time.Time) error {
	return nil
}

func (r *failingTriggers) Surface(context.Context, string, time.Time) error { return nil }
func (r *failingTriggers) Undelivered(context.Context) ([]ports.DueTrigger, error) {
	return nil, nil
}
func (r *failingTriggers) Delivered(context.Context) ([]ports.DueTrigger, error) { return nil, nil }
func (r *failingTriggers) Resolve(context.Context, string, ports.TriggerResolution, time.Time) error {
	return nil
}

func (emptyTriggers) Surface(context.Context, string, time.Time) error        { return nil }
func (emptyTriggers) Undelivered(context.Context) ([]ports.DueTrigger, error) { return nil, nil }
func (emptyTriggers) Delivered(context.Context) ([]ports.DueTrigger, error)   { return nil, nil }
func (emptyTriggers) Resolve(context.Context, string, ports.TriggerResolution, time.Time) error {
	return nil
}
