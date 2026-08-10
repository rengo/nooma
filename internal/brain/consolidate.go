package brain

import (
	"context"
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
// (see capture.go's own doc comment).
func NewConsolidateService(clock ports.Clock, cfg ports.ConfigRepo) *ConsolidateService {
	return &ConsolidateService{
		clock: clock,
		run:   consolidateRunner{cfg: cfg},
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
	// PhasesRun records every phase runPhase reached, in the exact order
	// consolidation.Order() presents them — I11's own behavioural proof
	// reads this directly (spec R4.1). This is real output, not a test
	// seam: `nooma consolidate`'s eventual report rendering (PR 12) has
	// the same use for it.
	PhasesRun []consolidation.Phase
}

// Consolidate is this file's only ports.Clock.Now() read — one per
// invocation, whole pass or single phase (spec R0.2; design §3.3(a)).
func (s *ConsolidateService) Consolidate(ctx context.Context, req ConsolidateRequest) (ConsolidateReport, error) {
	return s.run.at(ctx, req, s.clock.Now())
}

// consolidateRunner is the clockless worker owning one pass (design
// §3.3(a)) — no ConsolidateService field, no ports.Clock of its own,
// mirroring captureRunner/correctionRunner's own split.
type consolidateRunner struct {
	cfg ports.ConfigRepo
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
// anything a whole pass would not, or vice versa.
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
		report.PhasesRun = append(report.PhasesRun, p)
		if err := r.runPhase(ctx, p, pass); err != nil {
			return report, err
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
		return fmt.Errorf("consolidate: unhandled phase %s", p)
	}
	return nil
}
