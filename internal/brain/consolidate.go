package brain

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/rengo/nooma/internal/core/consolidation"
	"github.com/rengo/nooma/internal/ports"
)

// ConsolidateService is the one entry point into a consolidation pass, and
// the only place in this file holding a ports.Clock — the same
// shell/worker split CaptureService uses (capture.go), for the same
// reason: test/conformance/brain_single_clock_read_test.go fails a
// non-test file under internal/brain/** on a second Now() call
// expression, so a whole-pass entry and a per-phase entry cannot each read
// the clock independently — the scope a call runs is a request field
// instead (design §3.3(a)).
type ConsolidateService struct {
	clock ports.Clock
	run   consolidateRunner
}

// NewConsolidateService wires a ConsolidateService over the ports one pass
// needs. clock is read exactly once per Consolidate call; run never sees
// clock at all — the same structural guarantee captureRunner's split gives
// (see capture.go's own doc comment). ids and log back record (design
// §3.3(e)) — the one call site every effect a phase persists goes through.
func NewConsolidateService(clock ports.Clock, cfg ports.ConfigRepo, ids ports.IDGen, log ports.DecisionLog) *ConsolidateService {
	return &ConsolidateService{
		clock: clock,
		run:   consolidateRunner{cfg: cfg, ids: ids, log: log},
	}
}

// ConsolidateRequest selects what one Consolidate call runs. Its zero
// value is a whole pass — the shape `nooma consolidate` with no flag has.
type ConsolidateRequest struct {
	// Phase, when non-nil, runs exactly that one phase and nothing else. A
	// *Phase and not a Phase: PhaseExpireIncomplete is Phase(0), so a bare
	// field could not distinguish "not set" from "run expire_incomplete"
	// — the same nil-sentinel idiom relation.Resolve and
	// consolidation.ResolveWeightThreshold already use, for the same
	// reason (design §3.3(a)).
	Phase *consolidation.Phase
}

// ConsolidateReport is Consolidate's return value.
type ConsolidateReport struct {
	// phasesRun records every phase runPhase reached, in the exact order
	// consolidation.Order() presents them — I11's own behavioural proof
	// reads this directly (spec R4.1), from inside package brain
	// (consolidate_test.go is white-box). Unexported deliberately: no
	// caller outside this package needs it yet, and PR 12's eventual
	// report rendering is a future design decision, not a reason to widen
	// this struct's public surface ahead of it.
	phasesRun []consolidation.Phase
	// corrupted collects every refused entry any of the four
	// corrupted-capable phases (archive, strengthen, reweight, derive)
	// reports — design §3.3(e), spec R4.2's MUST NOT: a refusal had no
	// vault effect, so it is surfaced here and never in decision_log.
	// Unexported for the same reason phasesRun is: PR 12 owns the eventual
	// public report shape.
	corrupted []string
}

// reportCorrupted folds ids into r.corrupted and touches nothing else —
// deliberately: this method has no access to a consolidateRunner's log or
// ids, so it cannot itself become a call site that routes a corrupted
// entry into record (design §3.3(e): "a corrupted entry from any phase is
// surfaced in ConsolidateReport and never in decision_log"). The refusal
// rule is decided once, here, and applied uniformly to all four
// corrupted-capable phases' own future call sites (PR 8-11).
func (r *ConsolidateReport) reportCorrupted(ids []string) {
	r.corrupted = append(r.corrupted, ids...)
}

// Consolidate is this file's only ports.Clock.Now() read — one per
// invocation, whole pass or single phase (spec R0.2; design §3.3(a)).
func (s *ConsolidateService) Consolidate(ctx context.Context, req ConsolidateRequest) (ConsolidateReport, error) {
	return s.run.at(ctx, req, s.clock.Now())
}

// consolidateRunner is the clockless worker owning one pass (design
// §3.3(a)) — no ConsolidateService field, no ports.Clock of its own,
// mirroring captureRunner/correctionRunner's own split. ids and log exist
// for exactly one purpose: record, below — no other method on this type
// reads either field.
type consolidateRunner struct {
	cfg ports.ConfigRepo
	ids ports.IDGen
	log ports.DecisionLog
}

// record persists one decision_log row — the one call site every effect a
// phase's core decision function returns goes through (design §3.3(e),
// spec R4.2's first MUST). detail is marshalled into Decision.Context;
// rationale is the legible sentence spec R6.4's exit criterion needs.
//
// Honest limit (design §3.3(e)): nothing here structurally forbids a
// future persist call from skipping this helper. The guard is the L2
// fixture pair spec R4.2 requires — every phase fed produces one row per
// effect, no phase fed produces zero rows — plus review; this is a
// convention, not a gate, and is named as one rather than presented as
// something it is not.
func (r consolidateRunner) record(ctx context.Context, now time.Time, action ports.DecisionAction, rationale string, detail any) error {
	detailJSON, err := json.Marshal(detail)
	if err != nil {
		return fmt.Errorf("consolidate: encode decision context: %w", err)
	}

	d := ports.Decision{
		ID:         r.ids.New(),
		Action:     action,
		Rationale:  rationale,
		Context:    detailJSON,
		OccurredAt: now,
	}
	if err := r.log.Record(ctx, d); err != nil {
		return fmt.Errorf("consolidate: record decision: %w", err)
	}
	return nil
}

// persistArchiveTransitions applies ts through units.SetStatus, in the
// order archive planned them, and records one decision_log row per outcome
// (design §3.3(e), spec R4.2). A concurrent capture or correction that
// revived a unit between archive's own read and this write surfaces as
// ports.ErrStatusConflict: the write is skipped, the skip itself is
// recorded — spec R4.3's own "this IS an effect worth logging: the pass
// decided to archive and a race prevented it" — and persistence continues
// with the remaining transitions. A race never fails the pass; any other
// SetStatus error still does, returned to the caller unchanged.
func (r consolidateRunner) persistArchiveTransitions(ctx context.Context, ts []consolidation.Transition, units ports.UnitRepo, now time.Time) error {
	for _, t := range ts {
		err := units.SetStatus(ctx, t.UnitID, t.From, t.To, now)
		switch {
		case err == nil:
			rationale := fmt.Sprintf("archived unit %q: effective weight fell below threshold", t.UnitID)
			if rerr := r.record(ctx, now, ports.ActionArchiveArchived, rationale, t); rerr != nil {
				return rerr
			}
		case errors.Is(err, ports.ErrStatusConflict):
			rationale := fmt.Sprintf("skipped archiving unit %q: a concurrent capture or correction revived it first", t.UnitID)
			if rerr := r.record(ctx, now, ports.ActionArchiveConflictSkipped, rationale, t); rerr != nil {
				return rerr
			}
		default:
			return fmt.Errorf("consolidate: archive: set status for unit %q: %w", t.UnitID, err)
		}
	}
	return nil
}

// passContext is everything one invocation reads before any phase runs —
// design §3.3(c). Assembled once by buildPassContext and passed by value
// to every phase runPhase reaches, so `since` and `cfg` cannot drift
// between phases within the same pass.
type passContext struct {
	now   time.Time
	cfg   ports.VaultConfig
	since *time.Time
}

// buildPassContext reads r.cfg exactly once and assembles pass (design
// §3.3(c)). A separate method rather than inlined into at, so a test can
// call it directly and inspect since — this PR ships no phase consumer of
// since yet (PR 8/9 add the first two), so there is nothing else to
// observe it through.
func (r consolidateRunner) buildPassContext(ctx context.Context, now time.Time) (passContext, error) {
	cfg, err := r.cfg.Load(ctx)
	if err != nil {
		return passContext{}, fmt.Errorf("consolidate: load config: %w", err)
	}
	return passContext{now: now, cfg: cfg, since: cfg.ConsolidationLastRunAt}, nil
}

// at runs one invocation given the instant Consolidate already read —
// design §3.3(b): one execution path, filtered, never a second dispatch.
// The per-phase run iterates consolidation.Order() and skips; it never
// calls a phase function directly, so a per-phase run can never reach
// anything a whole pass would not, or vice versa. A req.Phase outside
// Order() matches nothing in that loop, so phasesRun stays empty — the
// check below turns that into consolidation.ErrUnknownPhase instead of
// the false "success, nothing ran" a caller would otherwise see (Judgment
// Day, PR 7a round 1: confirmed by both judges, design.md's own test
// matrix row 1078 already required this and the shipped test only proved
// it against runPhase in isolation, never through this entry point).
//
// What this does not cover (design §3.3(d)): consolidation_last_run_at is
// written only after every phase in the loop below has returned. A phase
// that returns an error aborts the pass before that write runs — correct,
// since an aborted pass did not happen — but it leaves since pointing at
// the previous pass, so the next pass re-reads whatever slots the failed
// one had already read. Every phase M2 ships is idempotent under a
// re-read (archive re-reads status, strengthen re-computes from the same
// since, connect re-excludes existing pairs), so this is a cost, not a
// correctness problem — stated here so it is not rediscovered as a
// surprise.
func (r consolidateRunner) at(ctx context.Context, req ConsolidateRequest, now time.Time) (ConsolidateReport, error) {
	pass, err := r.buildPassContext(ctx, now)
	if err != nil {
		return ConsolidateReport{}, err
	}

	var report ConsolidateReport
	for _, p := range consolidation.Order() {
		if req.Phase != nil && p != *req.Phase {
			continue
		}
		report.phasesRun = append(report.phasesRun, p)
		if err := r.runPhase(ctx, p, pass); err != nil {
			return report, err
		}
	}

	if req.Phase != nil && len(report.phasesRun) == 0 {
		return report, fmt.Errorf("consolidate: %w: %s", consolidation.ErrUnknownPhase, *req.Phase)
	}

	if req.Phase == nil {
		if err := r.cfg.RecordConsolidationRun(ctx, pass.now); err != nil {
			return report, fmt.Errorf("consolidate: record consolidation run: %w", err)
		}
	}

	return report, nil
}

// runPhase dispatches p to its own arm — design §3.3(b)'s switch over
// consolidation's own Phase constants, the only enumeration of the eight
// phases this file contains (m2b §3.2 leg 4's tree scan bans a second
// one). No arm does real work yet: each is a placeholder the phase-IO PRs
// (8-11) fill in. default names the unhandled phase rather than silently
// doing nothing, so a ninth phase added to Order() without a matching
// case fails loudly here instead of being skipped.
func (r consolidateRunner) runPhase(_ context.Context, p consolidation.Phase, _ passContext) error {
	switch p {
	case consolidation.PhaseExpireIncomplete:
		// placeholder — PR 8 wires the real read/write.
	case consolidation.PhaseArchive:
		// placeholder — PR 8 wires the real read/write.
	case consolidation.PhaseStrengthen:
		// placeholder — PR 8 wires the real read/write.
	case consolidation.PhaseConnect:
		// placeholder — PR 9 wires the real read/write.
	case consolidation.PhaseDerive:
		// placeholder — PR 10 wires the real read/write.
	case consolidation.PhaseReweight:
		// placeholder — PR 11 wires the real read/write.
	case consolidation.PhasePatternEval:
		// placeholder — PR 11 wires the real read/write.
	case consolidation.PhaseLearn:
		// No work, ever (owner ruling 3) — this arm exists so Order()'s
		// last slot is reached and reported, and performs nothing.
	default:
		return fmt.Errorf("consolidate: %w: %s", consolidation.ErrUnknownPhase, p)
	}
	return nil
}
