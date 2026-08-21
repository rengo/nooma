package brain

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/rengo/nooma/internal/core/prospection"
	"github.com/rengo/nooma/internal/ports"
)

// CheckService is the one entry point into a due scan, and the only place
// in this file holding a ports.Clock — ConsolidateService's shell/worker
// split verbatim, for the same reason: brain_single_clock_read_test.go
// fails a non-test file under internal/brain on a second Now() call
// expression, so the pass reads the clock once and passes the instant
// down. Every trigger and timer in one scan is therefore judged against
// exactly one instant, which is what makes a scan's verdicts consistent
// with each other.
type CheckService struct {
	clock ports.Clock
	run   checkRunner
}

// NewCheckService wires a CheckService over the ports one scan needs.
// clock is read exactly once per Check call; run never sees it.
func NewCheckService(clock ports.Clock, triggers ports.TriggerRepo, timers ports.TimerRepo, ids ports.IDGen, log ports.DecisionLog) *CheckService {
	return &CheckService{
		clock: clock,
		run:   checkRunner{triggers: triggers, timers: timers, ids: ids, log: log},
	}
}

// CheckReport is what one scan did — counts, not rows, because a caller
// renders a summary and the rows themselves are in decision_log where the
// glass box can explore them.
type CheckReport struct {
	// TriggersDue and TimersDue are how many came due, before any verdict.
	TriggersDue int
	TimersDue   int
	// TriggersExpired, TimersFired and TimersCancelled are the effects.
	// Their sum is the number of decision_log rows this scan wrote, which
	// is I12 stated as arithmetic rather than as a promise.
	TriggersExpired int
	TimersFired     int
	TimersCancelled int
}

// Check runs one due scan. It is this file's one ports.Clock.Now() call.
func (s *CheckService) Check(ctx context.Context) (CheckReport, error) {
	return s.run.at(ctx, s.clock.Now())
}

// at runs one scan at the instant Check already read.
//
// Every decision is delegated: prospection.TriggerVerdict and
// prospection.TimerVerdict own staleness and quiet hours, and this pass
// owns only what a verdict means for a column. Nothing here re-derives a
// window, a threshold or a boundary — the scan that decides for itself
// what "overdue" means is the scan that drifts from the gate it was
// supposed to run.
func (r checkRunner) at(ctx context.Context, now time.Time) (CheckReport, error) {
	var report CheckReport

	due, err := r.triggers.Due(ctx, now)
	if err != nil {
		return CheckReport{}, fmt.Errorf("check: due triggers: %w", err)
	}
	report.TriggersDue = len(due)

	for _, t := range due {
		status, writes := triggerTransition(prospection.TriggerVerdict(t.FireAt, now))
		if !writes {
			continue
		}
		// Expired is the only writing verdict a trigger has in this
		// change, so the switch a later one will need is not written yet:
		// a branch with no producer is a branch nothing can be said about.
		if status != ports.TriggerStatusExpired {
			return CheckReport{}, fmt.Errorf("check: trigger %q: no write path for status %q", t.ID, status)
		}
		if err := r.triggers.Expire(ctx, t.ID); err != nil {
			skipped, skipErr := r.skipOnConflict(ctx, now, err, ports.ErrTriggerStatusConflict, "triggers", t.ID, t.FireAt)
			if skipErr != nil {
				return CheckReport{}, skipErr
			}
			if skipped {
				continue
			}
			return CheckReport{}, fmt.Errorf("check: expire trigger %q: %w", t.ID, err)
		}
		if err := r.record(ctx, now, ports.ActionCheckTriggerExpired,
			fmt.Sprintf("trigger %q was overdue past its staleness window and expired rather than firing late", t.ID),
			checkDetail{ID: t.ID, FireAt: t.FireAt.UTC().Format(time.RFC3339), Verdict: string(prospection.VerdictStale)},
		); err != nil {
			return CheckReport{}, err
		}
		report.TriggersExpired++
	}

	dueTimers, err := r.timers.Due(ctx, now)
	if err != nil {
		return CheckReport{}, fmt.Errorf("check: due timers: %w", err)
	}
	report.TimersDue = len(dueTimers)

	for _, t := range dueTimers {
		verdict := prospection.TimerVerdict(t.FireAt, now)
		status, writes := timerTransition(verdict)
		if !writes {
			continue
		}

		detail := checkDetail{ID: t.ID, FireAt: t.FireAt.UTC().Format(time.RFC3339), Verdict: string(verdict)}
		switch status {
		case ports.TimerStatusFired:
			if err := r.timers.Fire(ctx, t.ID, now); err != nil {
				skipped, skipErr := r.skipOnConflict(ctx, now, err, ports.ErrTimerStatusConflict, "timers", t.ID, t.FireAt)
				if skipErr != nil {
					return CheckReport{}, skipErr
				}
				if skipped {
					continue
				}
				return CheckReport{}, fmt.Errorf("check: fire timer %q: %w", t.ID, err)
			}
			if err := r.record(ctx, now, ports.ActionCheckTimerFired,
				fmt.Sprintf("timer %q came due and fired", t.ID), detail); err != nil {
				return CheckReport{}, err
			}
			report.TimersFired++

		case ports.TimerStatusCancelled:
			if err := r.timers.Cancel(ctx, t.ID); err != nil {
				skipped, skipErr := r.skipOnConflict(ctx, now, err, ports.ErrTimerStatusConflict, "timers", t.ID, t.FireAt)
				if skipErr != nil {
					return CheckReport{}, skipErr
				}
				if skipped {
					continue
				}
				return CheckReport{}, fmt.Errorf("check: cancel timer %q: %w", t.ID, err)
			}
			if err := r.record(ctx, now, ports.ActionCheckTimerCancelled,
				fmt.Sprintf("timer %q was overdue past its staleness window and was cancelled rather than fired late", t.ID),
				detail); err != nil {
				return CheckReport{}, err
			}
			report.TimersCancelled++

		default:
			return CheckReport{}, fmt.Errorf("check: timer %q: no write path for status %q", t.ID, status)
		}
	}

	return report, nil
}

// checkRunner does the work of one scan over every port Check needs except
// the clock.
type checkRunner struct {
	triggers ports.TriggerRepo
	timers   ports.TimerRepo
	ids      ports.IDGen
	log      ports.DecisionLog
}

// checkDetail is every check.* row's context shape. One shape for all
// three actions, unlike arming's three: these rows carry the same three
// facts, and splitting a context that does not differ would be splitting
// for symmetry rather than for meaning.
type checkDetail struct {
	ID     string `json:"id"`
	FireAt string `json:"fire_at"`
	// Verdict is what the gate decided, on the three rows that record an
	// effect. It is empty on a conflict row, where no verdict was acted
	// on — the write was refused before the verdict could become one.
	Verdict string `json:"verdict,omitempty"`
	// Table names which of the two a conflict row is about, and is empty
	// on the effect rows, where the action already says. Putting the
	// table into Verdict instead would have made one field mean two
	// things and read as a lie in the audit trail.
	Table string `json:"table,omitempty"`
}

// record is this pass's one decision_log call site — consolidateRunner's
// own shape.
func (r checkRunner) record(ctx context.Context, now time.Time, action ports.DecisionAction, rationale string, detail checkDetail) error {
	detailJSON, err := json.Marshal(detail)
	if err != nil {
		return fmt.Errorf("check: encode decision context: %w", err)
	}

	d := ports.Decision{
		ID:         r.ids.New(),
		Action:     action,
		Rationale:  rationale,
		Context:    detailJSON,
		OccurredAt: now,
	}
	if err := r.log.Record(ctx, d); err != nil {
		return fmt.Errorf("check: record decision: %w", err)
	}
	return nil
}

// skipOnConflict is this pass's whole tolerance for concurrency, and its
// shape is persistArchiveTransitions' verbatim.
//
// A transition another pass already made is not this pass's failure: two
// scans overlapping is the normal state of a thing that runs every few
// minutes, and one contended row must not cost every row behind it in the
// same scan. So a conflict is recorded and skipped.
//
// Anything else aborts. That asymmetry is load-bearing — a repository that
// cannot be reached at all is not a race, and swallowing it would turn a
// broken vault into a scan that cheerfully reports having done nothing.
//
// It reports whether the error was a conflict, so the caller decides what
// to do with everything else rather than having this function guess.
func (r checkRunner) skipOnConflict(ctx context.Context, now time.Time, err, sentinel error, table, id string, fireAt time.Time) (bool, error) {
	if !errors.Is(err, sentinel) {
		return false, nil
	}

	if recordErr := r.record(ctx, now, ports.ActionCheckConflictSkipped,
		fmt.Sprintf("%s %q changed status between this scan's read and its write — another pass got there first, and this one skipped it", table, id),
		checkDetail{ID: id, FireAt: fireAt.UTC().Format(time.RFC3339), Table: table},
	); recordErr != nil {
		return false, recordErr
	}
	return true, nil
}

// triggerTransition maps one verdict to the triggers.status a scan writes
// for it, and reports whether it writes at all.
//
// Pending is not yet due. Defer writes nothing and that is load-bearing:
// quiet-hours deferral is recomputed every pass from fire_at and now and
// needs no persisted state, so a deferred trigger stays armed and
// resurfaces by arithmetic. Writing a row per pass per deferred trigger
// would put dozens of rows a night into the audit trail for a decision
// with no effect.
//
// Deliver writes nothing either, and that one is worth stating plainly:
// nothing in this change can surface a fired trigger, so moving it to
// fired here would record a delivery that never happened and lose the row
// from the next scan's Due. It stays armed until something can deliver it.
func triggerTransition(v prospection.Verdict) (ports.TriggerStatus, bool) {
	if v == prospection.VerdictStale {
		return ports.TriggerStatusExpired, true
	}
	return "", false
}

// timerTransition is the timer's half. Stale cancels rather than expires —
// doc 02 §8's own pending|fired|cancelled vocabulary — and Deliver fires,
// because a timer's firing IS its delivery and there is no rendering step
// for it to wait on.
//
// Defer is unreachable: TimerVerdict passes deferInQuietHours = false,
// which is how a timer stays the one push exception to quiet hours. It
// still gets a defined answer here rather than falling into a panic that
// waits for the day it becomes reachable.
func timerTransition(v prospection.Verdict) (ports.TimerStatus, bool) {
	switch v {
	case prospection.VerdictStale:
		return ports.TimerStatusCancelled, true
	case prospection.VerdictDeliver:
		return ports.TimerStatusFired, true
	default:
		return "", false
	}
}
