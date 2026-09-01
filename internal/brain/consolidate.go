package brain

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"slices"
	"time"

	"github.com/rengo/nooma/internal/core/classify"
	"github.com/rengo/nooma/internal/core/consolidation"
	"github.com/rengo/nooma/internal/core/recall"
	"github.com/rengo/nooma/internal/core/relation"
	"github.com/rengo/nooma/internal/core/selfmodel"
	"github.com/rengo/nooma/internal/core/unit"
	"github.com/rengo/nooma/internal/core/weight"
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
// units and rels are PR 8's own widening: expire_incomplete and archive
// read/write through units, strengthen reads/writes through rels (design
// §6.3 slots 1-3). recall is PR 9a's own addition (slot 4, design §7.1):
// the same *RecallService instance wiring shares with NewCaptureService
// (design D9's "one shared RecallService instance"), never a second one
// built here — connect's candidate search calls its ScoredFor method,
// never a new fusion implementation. judge is PR 9b's own addition
// (design §7.1, spec R5.5): the identical relation-judge ports.LLMProvider
// shape NewCaptureService's own judge parameter already establishes —
// connect's persist decision routes through the same
// relation.Resolve/Decide, never a second judge wiring.
// selfModel is PR 10b's own addition (design §7.3, spec R5.6/R5.8): derive's
// two SelfModelRepo reads/writes — the same shape recall/judge already
// establish for their own ports, never a second wiring convention. state is
// PR 11's own addition (design §3.2 Q6, §6.3 slot 7): pattern_eval's load
// half reads StateRepo.LastHypothesisAt and writes OpenHypothesis, the same
// widen-at-the-end-of-the-parameter-list convention selfModel already set.
func NewConsolidateService(clock ports.Clock, cfg ports.ConfigRepo, units ports.UnitRepo, rels ports.RelationRepo, ids ports.IDGen, log ports.DecisionLog, recallSvc *RecallService, judge ports.LLMProvider, selfModel ports.SelfModelRepo, state ports.StateRepo) *ConsolidateService {
	return &ConsolidateService{
		clock: clock,
		run:   consolidateRunner{cfg: cfg, units: units, rels: rels, ids: ids, log: log, recall: recallSvc, judge: judge, selfModel: selfModel, state: state},
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
	// The field stays unexported; Corrupted() below is the public shape
	// PR 12 was left to decide, and it is the ONLY way out of this struct.
	corrupted []string
	// newRelationEdges accumulates every relation connect (slot 4) persists
	// during THIS pass, as the plain weight.Edge shape reweight's own
	// Reweight function consumes — doc 02 §6 item 6's own words: "over this
	// pass's new edges only". This is a disclosed design gap: neither
	// spec.md nor design.md's §6.3 pipeline diagram states how newEdges
	// reaches slot 6 from slot 4's own persist step (design §6.3 only names
	// the two arguments, "Reweight(states, newEdges, now)", never their
	// source). ConsolidateReport already plays the identical mid-pass
	// accumulator role for corrupted (above), so extending it — rather than
	// widening passContext, which is assembled once BEFORE any phase runs
	// (design §3.3(c)) and cannot hold a value slot 4 only produces DURING
	// the pass — is the minimal change that closes the gap without a new
	// port or a second wiring convention. One direct consequence, named
	// rather than silently accepted: a per-phase `--phase=reweight` run
	// never populates this field (connect never runs), so it always sees
	// zero new edges — correct per doc 02 §6 item 6's own text, not a bug;
	// unlike derive (design §7.3), reweight's "this pass's new edges" rule
	// is inherently pass-relative, not something a fresh independent read
	// could ever reconstruct for a single-phase invocation.
	newRelationEdges []weight.Edge
}

// reportCorrupted folds ids into r.corrupted and touches nothing else —
// deliberately: this method has no access to a consolidateRunner's log or
// ids, so it cannot itself become a call site that routes a corrupted
// entry into record (design §3.3(e): "a corrupted entry from any phase is
// surfaced in ConsolidateReport and never in decision_log"). The refusal
// rule is decided once, here, and applied uniformly to all four
// corrupted-capable phases' own future call sites (PR 8-11).
//
// An id already reported is not appended again. Two phases refusing the
// same row is the normal case, not an edge one: archive (slot 2) and
// connect (slot 4) each read LiveDecayStates independently within one
// pass, and a refusal writes nothing to the vault, so the second read sees
// the same corrupt row the first one refused. corrupted answers "which
// units could not be used", so one unit is one entry however many phases
// tripped over it. Membership is a linear scan rather than a set field:
// the slice holds refused rows only, which are rare by construction and
// bounded by the live pool, and a set would widen this struct's own shape
// for no measurable gain.
func (r *ConsolidateReport) reportCorrupted(ids []string) {
	for _, id := range ids {
		if !slices.Contains(r.corrupted, id) {
			r.corrupted = append(r.corrupted, id)
		}
	}
}

// Corrupted returns the ids of every unit a phase refused this pass, in
// first-refused order. It returns a copy: a caller that sorts or truncates
// the result must not be able to edit a report the runner already handed
// back.
//
// This is the public shape the field's own doc comment deferred to PR 12,
// and it exists because a refusal has exactly one honest exit. It is kept
// out of decision_log deliberately (spec R4.2's MUST NOT — nothing was
// written to the vault, so no decision was made), which means that without
// a reader here a refused unit would appear nowhere at all: `nooma
// consolidate` would report a clean pass while silently skipping the same
// corrupt row every night. Judgment Day on PR 12 found exactly that gap —
// the CLI discarded the whole report — and this method plus its caller in
// cmd/nooma/consolidate.go close it.
func (r ConsolidateReport) Corrupted() []string {
	return slices.Clone(r.corrupted)
}

// Consolidate is this file's only ports.Clock.Now() read — one per
// invocation, whole pass or single phase (spec R0.2; design §3.3(a)).
func (s *ConsolidateService) Consolidate(ctx context.Context, req ConsolidateRequest) (ConsolidateReport, error) {
	return s.run.at(ctx, req, s.clock.Now())
}

// consolidateRunner is the clockless worker owning one pass (design
// §3.3(a)) — no ConsolidateService field, no ports.Clock of its own,
// mirroring captureRunner/correctionRunner's own split. ids and log exist
// for exactly one purpose: record, below. units and rels are runPhase's own
// per-phase reads/writes (design §6.3): units backs expire_incomplete and
// archive, rels backs strengthen and connect. recall is connect's own
// slot-4 read (design §7.1) — no ports.Clock of its own either, the same
// property RecallService already has. judge is connect's own persist step
// (design §7.1, spec R5.5) — the identical relation-judge ports.LLMProvider
// shape captureRunner's own judge field already holds. selfModel is
// derive's own slot-5 read/write pair (design §7.3, spec R5.6/R5.8) — the
// same judge field above sends derive's own belief_derivation call too, one
// port for both connect's and derive's judge traffic. state is
// pattern_eval's own slot-7 load-half pair (design §3.2 Q6, §6.3 slot 7,
// spec R5.10's second MUST): StateRepo.LastHypothesisAt feeds
// consolidation.EvaluateLoad, and a firing result appends through
// OpenHypothesis.
type consolidateRunner struct {
	cfg       ports.ConfigRepo
	units     ports.UnitRepo
	rels      ports.RelationRepo
	ids       ports.IDGen
	log       ports.DecisionLog
	recall    *RecallService
	judge     ports.LLMProvider
	selfModel ports.SelfModelRepo
	state     ports.StateRepo
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

// persistExpireIncompleteTransitions applies ts through units.SetStatus, in
// the order ExpireIncomplete planned them, and records one decision_log row
// per transition (design §3.3(e), spec R4.2, R5.1). Unlike archive's own
// persist (below), expire_incomplete has no documented concurrent-revive
// exception (spec R4.3 names only archive's race) — any SetStatus error
// here propagates and aborts the pass, matching every other phase's default
// posture.
func (r consolidateRunner) persistExpireIncompleteTransitions(ctx context.Context, ts []consolidation.Transition, now time.Time) error {
	for _, t := range ts {
		if err := r.units.SetStatus(ctx, t.UnitID, t.From, t.To, now); err != nil {
			return fmt.Errorf("consolidate: expire_incomplete: set status for unit %q: %w", t.UnitID, err)
		}
		rationale := fmt.Sprintf("expire_incomplete: unit %q transitioned %s -> %s (%s)", t.UnitID, t.From, t.To, t.Reason)
		if err := r.record(ctx, now, ports.ActionExpireIncompleteTransitioned, rationale, t); err != nil {
			return err
		}
	}
	return nil
}

// partitionLiveDecayStates splits cs into usable and refused entries, using
// the identical non-finite predicate consolidation.Archive applies
// internally — design §8.1: "consolidateRunner partitions
// []consolidation.Cold into usable and refused before mapping to []Source,
// using the same non-finite predicate Archive applies, and reports the
// refused ids through ConsolidateReport's corrupted set." Introduced here
// for archive's own LiveDecayStates read (slot 2); connect, derive and
// reweight (PR 9/10b/11, slots 4/5/6) reuse this exact function over their
// own LiveDecayStates reads rather than restating the predicate once per
// phase — the guard that closes the SelectConnectSources NaN-comparator
// hazard those phases would otherwise feed unfiltered (design §8.1's "two
// uncovered paths"). Four readers now share it; the count is deliberately
// not restated here, because a comment carrying a call count goes stale
// the next time a phase learns to read decay states.
func partitionLiveDecayStates(cs []consolidation.Cold) (usable []consolidation.Cold, refused []string) {
	for _, c := range cs {
		if math.IsNaN(c.Weight) || math.IsInf(c.Weight, 0) || math.IsNaN(c.DecayRate) || math.IsInf(c.DecayRate, 0) {
			refused = append(refused, c.UnitID)
			continue
		}
		usable = append(usable, c)
	}
	return usable, refused
}

// coldToSources adapts LiveDecayStates' already-refused-filtered
// []consolidation.Cold into []consolidation.Source — the two declare the
// identical five decay fields in the identical order (design §4.1's own
// "one read shape serves all three phases"), so each entry is a direct
// type conversion, not a computation. connect's own read (design §6.3 slot
// 4) is this function's first caller; derive (PR 10b, slot 5) is its
// second.
func coldToSources(cs []consolidation.Cold) []consolidation.Source {
	out := make([]consolidation.Source, len(cs))
	for i, c := range cs {
		out[i] = consolidation.Source(c)
	}
	return out
}

// coldToStates adapts LiveDecayStates' already-refused-filtered
// []consolidation.Cold into reweight's own map[string]weight.Current shape
// (design §6.3 slot 6, spec R3.3) — reweight's own second caller-supplied
// input, alongside coldToSources' identical Cold source above. Cold's own
// extra Status field is dropped: weight.Current never carried one, and
// Reweight has no use for it (its own scenario reasons about weight,
// decay rate and last-touched only). Keyed by UnitID, matching Reweight's
// own map[string]weight.Current parameter shape exactly — the same "make a
// duplicate id unrepresentable" property reweight.go's own doc comment
// names for m2a C18.
func coldToStates(cs []consolidation.Cold) map[string]weight.Current {
	out := make(map[string]weight.Current, len(cs))
	for _, c := range cs {
		out[c.UnitID] = weight.Current{UnitID: c.UnitID, Weight: c.Weight, DecayRate: c.DecayRate, LastTouchedAt: c.LastTouchedAt}
	}
	return out
}

// beliefsToConsolidation adapts SelfModelRepo.ActiveBeliefs' own
// []ports.Belief into consolidation.Belief — the shape both derive's
// BuildDerivePrompt (slot 5, spec R5.6) and pattern_eval's own
// EvaluateStagnation (slot 7, spec R5.10) consume. Extracted here (PR 11)
// from derive's own inline conversion, which pattern_eval would otherwise
// duplicate a second time — Origin and Status carry nothing either core
// function reads, so both are dropped rather than added to
// consolidation.Belief for no consumer.
func beliefsToConsolidation(bs []ports.Belief) []consolidation.Belief {
	out := make([]consolidation.Belief, len(bs))
	for i, b := range bs {
		out[i] = consolidation.Belief{
			ID:               b.ID,
			Facet:            b.Facet,
			TopicKey:         b.TopicKey,
			Content:          b.Content,
			Confidence:       b.Confidence,
			LastReinforcedAt: b.LastReinforcedAt,
		}
	}
	return out
}

// scoredToFused adapts []ScoredUnit into the []recall.FusedCandidate shape
// consolidation.ConnectPairs and correction.Referent both consume, plus an
// id -> unit.Unit lookup for a caller that needs the full candidate back —
// design §7.1: "internal/brain/correction.go:117-120 already maps
// []ScoredUnit -> []recall.FusedCandidate ... The adapter is written once
// and shared, not copied." correction.go's resolveReferent was the first
// caller (its own inline block moved here); connect's own candidate search
// (below) is the second, over the exact same shape.
func scoredToFused(scored []ScoredUnit) ([]recall.FusedCandidate, map[string]unit.Unit) {
	cands := make([]recall.FusedCandidate, len(scored))
	byID := make(map[string]unit.Unit, len(scored))
	for i, su := range scored {
		cands[i] = recall.FusedCandidate{ID: su.Unit.ID, Score: su.Score}
		byID[su.Unit.ID] = su.Unit
	}
	return cands, byID
}

// connectPairsForSource is connectSources' own per-source step: recall's
// fused ranking over source's content (RecallService.ScoredFor, spec R5.5),
// adapted through scoredToFused, filtered against rels.ExistingPairs
// (design §4.2's own exclusion lookup), and bounded by
// consolidation.ConnectPairs.
func (r consolidateRunner) connectPairsForSource(ctx context.Context, source unit.Unit) ([]consolidation.Pair, error) {
	scored, _, err := r.recall.ScoredFor(ctx, source.Content)
	if err != nil {
		return nil, fmt.Errorf("consolidate: connect: recall for unit %q: %w", source.ID, err)
	}
	fused, _ := scoredToFused(scored)

	candidatePairs := make([]consolidation.Pair, len(fused))
	for i, c := range fused {
		candidatePairs[i] = consolidation.CanonicalPair(source.ID, c.ID)
	}
	existing, err := r.rels.ExistingPairs(ctx, candidatePairs)
	if err != nil {
		return nil, fmt.Errorf("consolidate: connect: existing pairs for unit %q: %w", source.ID, err)
	}

	return consolidation.ConnectPairs(source.ID, fused, existing), nil
}

// connectSources runs slot 4's own candidate search (design §7.1, §6.3) for
// every id in sourceIDs and returns the bounded, existing-pair-excluded
// pairs consolidation.ConnectPairs plans for each — one source may
// contribute up to consolidation.ConnectCandidateK pairs. It does not judge
// or persist anything itself: judgeAndPersistPairs (below) is PR 9b's own
// step, over each pair this method returns.
func (r consolidateRunner) connectSources(ctx context.Context, sourceIDs []string) ([]consolidation.Pair, error) {
	if len(sourceIDs) == 0 {
		return nil, nil
	}

	sourceUnits, err := r.units.LiveByIDs(ctx, sourceIDs)
	if err != nil {
		return nil, fmt.Errorf("consolidate: connect: read source units: %w", err)
	}

	var all []consolidation.Pair
	for _, source := range sourceUnits {
		pairs, err := r.connectPairsForSource(ctx, source)
		if err != nil {
			return nil, err
		}
		all = append(all, pairs...)
	}
	return all, nil
}

// judgeAndPersistPairs runs PR 9b's own step (design §7.1, spec R5.5) over
// every pair connectSources returned: one relation-judge call per pair
// (bounded by ConnectSourceLimit * ConnectCandidateK, design.md's own cost
// comment on connect.go), decided through consolidation.ProposeRelation and
// persisted on acceptance. ids is deduplicated once, up front, so a unit
// that is both a source in one pair and a candidate in another (or that
// repeats across several pairs) costs one units.LiveByIDs lookup, not one
// per pair. report is PR 11's own addition (design §6.3 slot 6's disclosed
// gap, see ConsolidateReport.newRelationEdges' own doc comment): every
// relation actually persisted here is also folded into
// report.newRelationEdges, the exact "this pass's new edges" reweight
// reads three slots later.
func (r consolidateRunner) judgeAndPersistPairs(ctx context.Context, pairs []consolidation.Pair, report *ConsolidateReport, now time.Time) error {
	if len(pairs) == 0 {
		return nil
	}

	ids := make([]string, 0, len(pairs)*2)
	seen := make(map[string]bool, len(pairs)*2)
	for _, p := range pairs {
		for _, id := range [2]string{p.From, p.To} {
			if !seen[id] {
				seen[id] = true
				ids = append(ids, id)
			}
		}
	}
	units, err := r.units.LiveByIDs(ctx, ids)
	if err != nil {
		return fmt.Errorf("consolidate: connect: read pair units: %w", err)
	}
	byID := make(map[string]unit.Unit, len(units))
	for _, u := range units {
		byID[u.ID] = u
	}

	for _, p := range pairs {
		source, ok := byID[p.From]
		if !ok {
			continue
		}
		target, ok := byID[p.To]
		if !ok {
			continue
		}
		if err := r.judgeAndPersistPair(ctx, source, target, report, now); err != nil {
			return err
		}
	}
	return nil
}

// judgeAndPersistPair judges one connect-planned pair through the same
// relation-judge shape capture.go's own judgeRelation establishes (design
// §7.1, spec R5.5: "the same relation-judge ports.LLMProvider shape
// capture.go's own judge call already establishes"), one candidate per
// call — JudgePrompt(source, []unit.Unit{target}) — matching connect.go's
// own ConnectSourceLimit * ConnectCandidateK cost bound (one call per
// pair, never one call per source's whole candidate list).
//
// The persist decision is consolidation.ProposeRelation, unchanged
// (design §7.1) — this function only supplies the resolved
// relation.Thresholds it needs. A judgment missing Type cannot even be
// looked up (ThresholdsFor needs a type name), so that check happens here,
// before the repo round trip; every other missing-field, discard and
// outcome-"new" case is ProposeRelation's own contract, already proven at
// the core level (connect_test.go).
//
// A judgment ProposeRelation refuses (ok == false) writes no
// decision_log row — deliberately unlike capture's own
// ActionRelationDiscarded (design §7.1's own stated divergence, spec
// R4.2's second MUST, flagged for owner review at design §12 Q2).
//
// report is PR 11's own addition: an accepted proposal also becomes one
// weight.Edge in report.newRelationEdges, folded from proposed's own
// From/To/Strength (never rel's DB-shaped fields) — the exact triple
// weight.Edge declares (see ConsolidateReport.newRelationEdges' own doc
// comment for why this accumulates here rather than through a repo read).
func (r consolidateRunner) judgeAndPersistPair(ctx context.Context, source, target unit.Unit, report *ConsolidateReport, now time.Time) error {
	resp, err := r.judge.Complete(ctx, ports.LLMRequest{Prompt: JudgePrompt(source, []unit.Unit{target}), Task: taskRelationEvaluation, JSONOnly: true})
	if err != nil {
		return fmt.Errorf("consolidate: connect: judge relation for unit %q: %w", source.ID, err)
	}

	j, _ := relation.DecodeJudgment(resp.Text)
	if j.Type == nil {
		return nil
	}

	// ADR-0026, before the threshold read rather than after it: exactly one
	// candidate went into the prompt, so any other ID is wrong by
	// construction, and a wrong ID does not deserve a repo round trip. The
	// row is recorded here because ProposeRelation cannot — it is pure, and
	// refusing is all it can do.
	offered := []string{target.ID}
	if j.TargetUnitID != nil && !relation.TargetOffered(*j.TargetUnitID, offered) {
		return r.recordConnectTargetUnknownDecision(ctx, source, *j.TargetUnitID, offered, now)
	}

	row, err := r.rels.ThresholdsFor(ctx, *j.Type)
	if err != nil {
		return fmt.Errorf("consolidate: connect: resolve relation thresholds for %q: %w", *j.Type, err)
	}

	proposed, ok := consolidation.ProposeRelation(source.ID, j, relation.Resolve(row), offered)
	if !ok {
		return nil
	}

	rel := ports.Relation{
		ID:         r.ids.New(),
		FromUnitID: proposed.From,
		ToUnitID:   proposed.To,
		Type:       proposed.Type,
		Strength:   proposed.Strength,
		Confidence: proposed.Confidence,
		CreatedBy:  string(proposed.CreatedBy),
		CreatedAt:  now,
	}
	if err := r.rels.Upsert(ctx, rel); err != nil {
		return fmt.Errorf("consolidate: connect: persist relation %s->%s: %w", rel.FromUnitID, rel.ToUnitID, err)
	}
	report.newRelationEdges = append(report.newRelationEdges, weight.Edge{From: proposed.From, To: proposed.To, Strength: proposed.Strength})

	rationale := fmt.Sprintf("connect: judged unit %q related to %q as %q", rel.FromUnitID, rel.ToUnitID, rel.Type)
	return r.record(ctx, now, ports.ActionConnectRelationPersisted, rationale, rel)
}

// recordConnectTargetUnknownDecision writes ADR-0026's row for connect.
//
// The Context carries both halves — what was offered and what came back —
// because neither alone is evidence. "The judge named unit X" is unremarkable
// until you can see that X was not on the list, and a list with no answer
// beside it says nothing about the model at all.
func (r consolidateRunner) recordConnectTargetUnknownDecision(ctx context.Context, source unit.Unit, answered string, offered []string, now time.Time) error {
	rationale := fmt.Sprintf(
		"connect: the relation judge for unit %q answered about unit %q, which was not among the %d candidate(s) it was shown — nothing stored",
		source.ID, answered, len(offered))
	return r.record(ctx, now, ports.ActionConnectTargetUnknown, rationale, map[string]any{
		"from_unit_id":     source.ID,
		"answered_unit_id": answered,
		"offered_unit_ids": offered,
	})
}

// taskBeliefDerivation is the LLMRequest.Task value derive's own judge
// call sends (design §7.2's tasksConsolidateConsumes, spec R5.6) — the
// exact string internal/config.DocumentedTaskNames already documents
// (design §7.2), so this PR adds no config-vocabulary entry.
const taskBeliefDerivation = "belief_derivation"

// deriveSourceIDs is derive's own slot-5 source read (design §7.3): the
// identical recently-touched, weight-ranked, ConnectSourceLimit-capped
// selection connect's own slot 4 computes — over derive's OWN fresh
// LiveDecayStates read, never connect's cached slot-4 slice (design §6.3's
// pipeline note; spec-verified by the three-calls-per-pass L2 fixture
// design.md names for archive/connect/derive together).
func (r consolidateRunner) deriveSourceIDs(ctx context.Context, pass passContext, report *ConsolidateReport) ([]string, error) {
	cs, err := r.units.LiveDecayStates(ctx)
	if err != nil {
		return nil, fmt.Errorf("consolidate: derive: read live decay states: %w", err)
	}
	usable, refused := partitionLiveDecayStates(cs)
	report.reportCorrupted(refused)
	return consolidation.SelectConnectSources(coldToSources(usable), pass.since, pass.now), nil
}

// derive runs slot 5's own read/write pipeline (design §6.3, §7.3; spec
// R5.6-R5.8): the same source selection connect's own slot 4 computes,
// fresh, feeds consolidation.BuildDerivePrompt (PR 10a) alongside every
// active belief (dedup defense 1, spec R5.6) into one belief_derivation
// judge call.
// A pass with nothing to derive from (no live source unit, the same
// "nothing changed since the last sleep" condition SelectConnectSources
// already applies) never calls the judge at all — connectSources' own
// "if len(sourceIDs) == 0 { return nil, nil }" precedent (above), applied
// here for the identical reason: TestConsolidate_NoEffects's own MUST
// ("a phase fed nothing must write nothing") and every noJudge(t) fixture
// this file already carries would otherwise send a judge call into a
// provider scripted with zero cases. Zero source units is the only skip
// condition — an empty *existing beliefs* list still sends (spec R5.6's
// second MUST, task 10b.1's own proof).
func (r consolidateRunner) derive(ctx context.Context, pass passContext, report *ConsolidateReport) error {
	sourceIDs, err := r.deriveSourceIDs(ctx, pass, report)
	if err != nil {
		return err
	}
	if len(sourceIDs) == 0 {
		return nil
	}
	sourceUnits, err := r.units.LiveByIDs(ctx, sourceIDs)
	if err != nil {
		return fmt.Errorf("consolidate: derive: read source units: %w", err)
	}
	sources := make([]consolidation.DeriveSource, len(sourceUnits))
	for i, u := range sourceUnits {
		sources[i] = consolidation.DeriveSource{UnitID: u.ID, Type: u.Type, Content: u.Content}
	}

	active, err := r.selfModel.ActiveBeliefs(ctx)
	if err != nil {
		return fmt.Errorf("consolidate: derive: read active beliefs: %w", err)
	}
	existingBeliefs := beliefsToConsolidation(active)

	resp, err := r.judge.Complete(ctx, ports.LLMRequest{
		Prompt:   consolidation.BuildDerivePrompt(sources, existingBeliefs),
		Task:     taskBeliefDerivation,
		JSONOnly: true,
	})
	if err != nil {
		return fmt.Errorf("consolidate: derive: judge belief derivation: %w", err)
	}
	proposals := decodeDerivedBeliefs(resp.Text)

	existingVectors, proposedVectors, model, err := r.embedForMerge(ctx, active, proposals)
	if err != nil {
		return fmt.Errorf("consolidate: derive: %w", err)
	}

	decisions, err := consolidation.MergeProposals(model, existingVectors, proposedVectors)
	if err != nil {
		return fmt.Errorf("consolidate: derive: merge proposals: %w", err)
	}

	return r.persistMergeDecisions(ctx, decisions, proposals, active, pass.now)
}

// persistMergeDecisions routes every MergeProposals decision to its own
// write (spec R5.8): MergeInto == "" (its own zero value) is the CREATE
// half, routed to r.createDerivedBelief; MergeInto != "" is the MERGE
// half, routed to r.reinforceDerivedBelief — never the topic-key upsert
// for a merge, per ports.SelfModelRepo's own MUST NOT (selfmodelrepo.go).
// active is indexed by id once, up front, so the merge half can look up
// each target's current confidence without a second SelfModelRepo round
// trip per decision.
func (r consolidateRunner) persistMergeDecisions(ctx context.Context, decisions []consolidation.MergeDecision, proposals []derivedBeliefProposal, active []ports.Belief, now time.Time) error {
	if len(decisions) == 0 {
		return nil
	}

	byID := make(map[string]ports.Belief, len(active))
	for _, b := range active {
		byID[b.ID] = b
	}

	for _, d := range decisions {
		proposal := proposals[d.ProposedIndex]
		if d.MergeInto == "" {
			if err := r.createDerivedBelief(ctx, proposal, now); err != nil {
				return err
			}
			continue
		}
		if err := r.reinforceDerivedBelief(ctx, byID, d.MergeInto, now); err != nil {
			return err
		}
	}
	return nil
}

// createDerivedBelief is R5.8's CREATE half: p's own facet/topic_key/
// content/confidence become a new self_beliefs row, keyed the way
// consolidation.DeriveTopicKey renders it (doc 02 §10:
// "derived/{facet}/{key}"), written through UpsertByTopicKey and recorded
// as ActionDeriveBeliefCreated (design §7.5's own count, row 5).
func (r consolidateRunner) createDerivedBelief(ctx context.Context, p derivedBeliefProposal, now time.Time) error {
	facet, err := selfmodel.ParseFacet(p.Facet)
	if err != nil {
		// decodeDerivedBeliefs already filters an unparsable facet out of
		// every proposal it returns — unreachable in practice, but treated
		// as "nothing to create" rather than a panic, the same safe-default
		// posture every other decode degradation in this file takes.
		return nil
	}
	b := ports.Belief{
		ID:               r.ids.New(),
		Facet:            facet,
		TopicKey:         consolidation.DeriveTopicKey(facet, p.Key),
		Content:          p.Content,
		Confidence:       p.Confidence,
		Origin:           "derived",
		Status:           "active",
		LastReinforcedAt: now,
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	if err := r.selfModel.UpsertByTopicKey(ctx, b); err != nil {
		return fmt.Errorf("consolidate: derive: upsert belief %q: %w", b.TopicKey, err)
	}
	rationale := fmt.Sprintf("derive: created belief %q (facet %q)", b.TopicKey, b.Facet)
	return r.record(ctx, now, ports.ActionDeriveBeliefCreated, rationale, b)
}

// reinforceDerivedBelief is R5.8's MERGE half: id names the existing
// belief MergeProposals chose (by embedding similarity, m2b spec R4.4),
// looked up in active for the current Confidence consolidation.Reinforce
// needs (m2b spec R4.5's own asymptotic law). active not containing id is
// defensive-only — MergeInto is always chosen from the exact `existing`
// slice built from active itself (embedForMerge, above), so this is
// unreachable in practice, the same "never assume, still guard" posture
// judgeAndPersistPair's own j.Type == nil check takes for connect's judge
// response (PR 9b), restated here because ReinforceByID's own
// ErrBeliefNotFound would otherwise abort the whole pass on a case that
// should just skip one decision. A decision with no effect writes nothing
// (doc 02 §11): a belief already at exactly confidence 1 also returns
// early here, via Reinforce's own (confidence, false).
func (r consolidateRunner) reinforceDerivedBelief(ctx context.Context, active map[string]ports.Belief, id string, now time.Time) error {
	belief, ok := active[id]
	if !ok {
		return nil
	}
	newConfidence, ok := consolidation.Reinforce(belief.Confidence)
	if !ok {
		return nil
	}
	if err := r.selfModel.ReinforceByID(ctx, id, newConfidence, now); err != nil {
		return fmt.Errorf("consolidate: derive: reinforce belief %q: %w", id, err)
	}
	rationale := fmt.Sprintf("derive: reinforced belief %q to confidence %.4f", id, newConfidence)
	return r.record(ctx, now, ports.ActionDeriveBeliefReinforced, rationale, map[string]any{"belief_id": id, "confidence": newConfidence})
}

// derivedBeliefProposal is one belief_derivation response's own proposed
// belief. Decoded here, in internal/brain, rather than in
// internal/core/consolidation: design §10.2 names PR 10a's prompt.go as
// the one internal/core file m2c adds in the whole change, and this
// decode step is response-wiring, not a core decision function.
//
// Named plainly because Judgment Day on this PR flagged it (both judges,
// WARNING): this placement is the INVERSE of connect's, not an instance of
// it. Connect puts JudgePrompt in internal/brain and DecodeJudgment in
// internal/core/relation; derive puts BuildDerivePrompt in
// internal/core/consolidation and this decode in internal/brain. So the
// codebase now holds two judge tasks whose prompt/decode halves sit on
// opposite sides of the core boundary. The owner's ruling was to keep the
// decode here — moving it would amend design §10.2, an approved decision —
// and to stop the comment claiming a precedent it reverses. The real
// justification is the §10.2 constraint alone; consistency with connect is
// a debt this leaves open, not a property it has. One concrete cost is
// already visible: the confidence [0,1]/NaN check below restates a bound
// consolidation.Reinforce also encodes, so the same rule now lives in two
// packages and can drift.
//
// No analogous decode function existed anywhere for belief_derivation
// before this PR:
// BuildDerivePrompt's own prompt text ("For each new belief worth
// proposing, decide facet, topic_key and content") is this decode's only
// documented contract — there is no testdata/llm/format.md precedent, no
// core decode function, and no §9 wire-format section in design.md to
// reuse for this task's response shape (a genuine design gap, disclosed
// in this PR's own apply report rather than silently invented). Wire
// shape: {"beliefs":[{"facet":...,"topic_key":...,"content":...,
// "confidence":...}, ...]} — confidence is not literally requested by
// BuildDerivePrompt's own text, but every other judge task in this
// codebase reports one (relation.Judgment's own "confidence" field), and
// requiring it here needs no invented core constant for a newly created
// belief's starting confidence.
type derivedBeliefProposal struct {
	Facet      string  `json:"facet"`
	Key        string  `json:"topic_key"`
	Content    string  `json:"content"`
	Confidence float64 `json:"confidence"`
}

// decodeDerivedBeliefs tolerantly extracts the "beliefs" array a
// belief_derivation response carries, degrading to zero proposals — never
// an error — on anything malformed: no "{" found, no "beliefs" field, an
// unparsable array, an unknown facet, an empty key/content, or a
// confidence outside [0,1] (including NaN). This is the same posture
// relation.DecodeJudgment already established for a judge response this
// codebase cannot fully trust (classify.Salvage's own tolerant byte-scan),
// restated here because no core package exists to hold it (see
// derivedBeliefProposal's own doc comment).
//
// A (facet, topic_key) pair already seen in this same response is dropped,
// first occurrence winning. Nothing in the derivation prompt forbids the
// judge proposing one topic key twice, and downstream nothing else would
// catch it: both proposals reach createDerivedBelief, each records its own
// ActionDeriveBeliefCreated row, and UpsertByTopicKey's ON CONFLICT
// (topic_key) DO UPDATE silently overwrites the first — leaving
// decision_log claiming two beliefs were created where the vault kept one,
// with the losing content gone and no refusal naming it. Dedup belongs
// here rather than at the persist site because a collision is a malformed
// response, not a persist outcome: dropping it before any decision exists
// keeps decision_log's count equal to the vault's, which is the property
// doc 05's own M2 demo ("the decision_log tells the story") rests on.
func decodeDerivedBeliefs(raw string) []derivedBeliefProposal {
	fields, _ := classify.Salvage([]byte(raw))
	beliefsRaw, ok := fields["beliefs"]
	if !ok {
		return nil
	}
	var candidates []derivedBeliefProposal
	if err := json.Unmarshal(beliefsRaw, &candidates); err != nil {
		return nil
	}
	out := make([]derivedBeliefProposal, 0, len(candidates))
	seen := make(map[string]bool, len(candidates))
	for _, c := range candidates {
		facet, err := selfmodel.ParseFacet(c.Facet)
		if err != nil {
			continue
		}
		if c.Key == "" || c.Content == "" {
			continue
		}
		if math.IsNaN(c.Confidence) || c.Confidence < 0 || c.Confidence > 1 {
			continue
		}
		topicKey := consolidation.DeriveTopicKey(facet, c.Key)
		if seen[topicKey] {
			continue
		}
		seen[topicKey] = true
		out = append(out, c)
	}
	return out
}

// embedForMerge is spec R5.7's own embedding step: every entry in active
// is embedded exactly once, unconditionally — "in memory, per pass"
// regardless of whether proposals is empty — followed by every entry in
// proposals. Both sides go through the same *RecallService instance's own
// ports.EmbeddingProvider (r.recall's own unexported embed field,
// package-private but same-package here — design D9's "one shared
// RecallService instance", never a second wiring convention for this
// port), never a new consolidateRunner field: PR 10b widens no
// NewConsolidateService parameter beyond ce23b23's own selfModel add.
// model is whichever side's own EmbedResponse.Model answers first — both
// sides share one EmbeddingProvider within a single phase run, so
// idx.Model and every query's Model are never different values within one
// MergeProposals call (design.md §7.1's own note on this exact hazard,
// restated for derive).
func (r consolidateRunner) embedForMerge(ctx context.Context, active []ports.Belief, proposals []derivedBeliefProposal) (existing, proposed []consolidation.BeliefVector, model string, err error) {
	existing = make([]consolidation.BeliefVector, len(active))
	for i, b := range active {
		ev, eerr := r.recall.embed.Embed(ctx, ports.EmbedRequest{Text: b.Content})
		if eerr != nil {
			return nil, nil, "", fmt.Errorf("embed active belief %q: %w", b.ID, eerr)
		}
		existing[i] = consolidation.BeliefVector{BeliefID: b.ID, Vector: ev.Vector}
		model = ev.Model
	}

	proposed = make([]consolidation.BeliefVector, len(proposals))
	for i, p := range proposals {
		ev, eerr := r.recall.embed.Embed(ctx, ports.EmbedRequest{Text: p.Content})
		if eerr != nil {
			return nil, nil, "", fmt.Errorf("embed proposed belief %d: %w", i, eerr)
		}
		proposed[i] = consolidation.BeliefVector{Vector: ev.Vector}
		if model == "" {
			model = ev.Model
		}
	}
	return existing, proposed, model, nil
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

// persistStrengthChanges applies cs through rels.Upsert and records one
// decision_log row per change (design §3.3(e), spec R4.2, R5.3). es is the
// same Evidence() read Strengthen consumed: RelationRepo.Upsert conflicts
// on the (FromUnitID, ToUnitID, Type) triple, never on id (design §4.2's
// own Upsert doc comment), so each StrengthChange — which carries only
// RelationID and the new Strength — is looked up back into es for the
// identity fields a correct Upsert call needs. Confidence rides along
// unchanged: Strengthen never revises it, and Upsert always overwrites
// Confidence with whatever it is given, so passing the value es already
// read (rather than a zero value) is what keeps this call a strength-only
// change in practice, not merely in intent.
func (r consolidateRunner) persistStrengthChanges(ctx context.Context, cs []consolidation.StrengthChange, es []consolidation.RelationEvidence, now time.Time) error {
	byID := make(map[string]consolidation.RelationEvidence, len(es))
	for _, e := range es {
		byID[e.RelationID] = e
	}

	for _, c := range cs {
		e, ok := byID[c.RelationID]
		if !ok {
			return fmt.Errorf("consolidate: strengthen: no evidence found for relation %q", c.RelationID)
		}
		rel := ports.Relation{
			ID:         c.RelationID,
			FromUnitID: e.FromUnitID,
			ToUnitID:   e.ToUnitID,
			Type:       e.Type,
			Strength:   c.Strength,
			Confidence: e.Confidence,
		}
		if err := r.rels.Upsert(ctx, rel); err != nil {
			return fmt.Errorf("consolidate: strengthen: upsert relation %q: %w", c.RelationID, err)
		}
		rationale := fmt.Sprintf("strengthen: relation %q raised to strength %.4f", c.RelationID, c.Strength)
		if err := r.record(ctx, now, ports.ActionStrengthenApplied, rationale, c); err != nil {
			return err
		}
	}
	return nil
}

// persistBoosts applies boosts through units.ApplyBoosts — one batch call,
// one transaction, matching design §5.2's own "one statement per boost
// inside one transaction, never two statements a partial failure could
// leave half-applied" — and records one decision_log row per boost
// afterward (design §3.3(e), spec R4.2, R5.9). corrupted entries are never
// passed here: runPhase's own PhaseReweight arm (below) routes
// consolidation.Reweight's corrupted return only into
// report.reportCorrupted, the one place a refusal is surfaced (spec R4.2's
// MUST NOT).
//
// An ApplyBoosts error aborts the whole pass, including ports.ErrUnitNotFound
// — and that is deliberately NOT what persistArchiveTransitions does with its
// own analogous race (it catches ports.ErrStatusConflict, records the skip and
// continues). Judgment Day on PR 11 named the asymmetry; it is left standing,
// and the reason is that only one of the two races has a mandate. Spec R4.3
// documents archive's concurrent-revive case and states the required
// behaviour, so tolerating it there implements a decision already taken. No
// spec line covers a unit disappearing between reweight's own LiveDecayStates
// read and this call, so inventing tolerance here would be deciding design
// from an implementation seat — and the failure is loud, not silent.
//
// The cost is real and worth stating plainly: because ApplyBoosts is
// all-or-nothing over its batch (ports.UnitRepo's own contract), one vanished
// unit discards every legitimate boost in the same call, and because `at`
// aborts on any phase error, pattern_eval and learn do not run that pass. The
// next pass re-reads and re-derives, so nothing is permanently lost; a pass is
// skipped, not corrupted. If M2's scheduler later runs passes unattended
// (m2d), this is the first place to revisit — an unattended pass that dies on
// a race nobody sees is a different proposition from a hand-run one.
func (r consolidateRunner) persistBoosts(ctx context.Context, boosts []weight.Boost, now time.Time) error {
	if len(boosts) == 0 {
		return nil
	}
	if err := r.units.ApplyBoosts(ctx, boosts, now); err != nil {
		return fmt.Errorf("consolidate: reweight: apply boosts: %w", err)
	}
	for _, b := range boosts {
		rationale := fmt.Sprintf("reweight: unit %q boosted to weight %.4f", b.UnitID, b.Weight)
		if err := r.record(ctx, now, ports.ActionReweightBoostApplied, rationale, b); err != nil {
			return err
		}
	}
	return nil
}

// persistStagnationFindings records one decision_log row per finding
// (design §3.3(e), spec R5.10's first MUST) — doc 02 §7's stagnation
// check-in has no M2 delivery mechanism of its own, so decision_log is its
// only recorded trace this milestone (spec R5.10's own framing).
func (r consolidateRunner) persistStagnationFindings(ctx context.Context, fs []consolidation.StagnationFinding, now time.Time) error {
	for _, f := range fs {
		rationale := fmt.Sprintf("pattern_eval: goal belief %q stagnant for %.1f days", f.TopicKey, f.StagnantDays)
		if err := r.record(ctx, now, ports.ActionPatternEvalStagnationFound, rationale, f); err != nil {
			return err
		}
	}
	return nil
}

// loadHypothesisMapping is the fixed sentence spec R5.10's second MUST
// requires the decision_log row's own Context to carry, verbatim: m2b
// design §9 Q6 left lastHypothesisAt's anchor an open question, and Q6's
// own instruction is that whichever mapping m2c picks, "m2c must map it
// and say so in the decision_log context" — not merely pick it in code.
const loadHypothesisMapping = "lastHypothesisAt is StateRepo.LastHypothesisAt's own return: the recorded_at of this phase's most recent PRIOR current_state row written with source='consolidation'. M2 has no state_confirmed/state_denied resolution signal yet (that arrives in M5), so the cooldown anchors on the hypothesis's own write, never on a resolution (m2b design §9 Q6)."

// loadHypothesisDetail is ActionPatternEvalLoadHypothesisOpened's own
// Context shape (spec R5.10's second MUST).
type loadHypothesisDetail struct {
	OpenCount        int        `json:"open_count"`
	Threshold        int        `json:"threshold"`
	LastHypothesisAt *time.Time `json:"last_hypothesis_at"`
	Mapping          string     `json:"last_hypothesis_at_mapping"`
}

// persistLoadHypothesis appends the current_state row EvaluateLoad's firing
// result plans, through StateRepo.OpenHypothesis, and records the matching
// decision_log row (design §3.3(e), §4.4; spec R5.10's second MUST).
// lastAt is the SAME value EvaluateLoad already consumed for its own
// cooldown check — passed through again here only to restate it in
// Context, never recomputed.
func (r consolidateRunner) persistLoadHypothesis(ctx context.Context, f consolidation.LoadFinding, lastAt *time.Time, now time.Time) error {
	h := ports.StateHypothesis{ID: r.ids.New(), Mood: ports.MoodLoaded, RecordedAt: now}
	if err := r.state.OpenHypothesis(ctx, h); err != nil {
		return fmt.Errorf("consolidate: pattern_eval: open load hypothesis: %w", err)
	}
	rationale := fmt.Sprintf("pattern_eval: mental load hypothesis opened (%d open loops at or above threshold %d)", f.OpenCount, f.Threshold)
	detail := loadHypothesisDetail{OpenCount: f.OpenCount, Threshold: f.Threshold, LastHypothesisAt: lastAt, Mapping: loadHypothesisMapping}
	return r.record(ctx, now, ports.ActionPatternEvalLoadHypothesisOpened, rationale, detail)
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
		if err := r.runPhase(ctx, p, pass, &report); err != nil {
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
// one). report accumulates every corrupted id a phase's own read refuses
// (design §3.3(e)) — the one place a refusal is surfaced, never in
// decision_log (spec R4.2's MUST NOT). PR 11 is the last phase-IO PR:
// every arm but learn's now wires real I/O (design §6.3 slots 1-8);
// learn's own arm performs no work, ever (owner ruling 3), by design
// rather than by omission. default names the unhandled phase rather than
// silently doing nothing, so a ninth phase added to Order() without a
// matching case fails loudly here instead of being skipped.
func (r consolidateRunner) runPhase(ctx context.Context, p consolidation.Phase, pass passContext, report *ConsolidateReport) error {
	switch p {
	case consolidation.PhaseExpireIncomplete:
		cutoff := pass.now.Add(-consolidation.IncompleteExpiryHours * time.Hour)
		us, err := r.units.IncompleteOlderThan(ctx, cutoff)
		if err != nil {
			return fmt.Errorf("consolidate: expire_incomplete: read incomplete units: %w", err)
		}
		ts := consolidation.ExpireIncomplete(us, pass.now)
		if err := r.persistExpireIncompleteTransitions(ctx, ts, pass.now); err != nil {
			return err
		}
	case consolidation.PhaseArchive:
		cs, err := r.units.LiveDecayStates(ctx)
		if err != nil {
			return fmt.Errorf("consolidate: archive: read live decay states: %w", err)
		}
		usable, refused := partitionLiveDecayStates(cs)
		report.reportCorrupted(refused)
		threshold := consolidation.ResolveWeightThreshold(pass.cfg.WeightThreshold)
		ts, _ := consolidation.Archive(usable, threshold, pass.now)
		if err := r.persistArchiveTransitions(ctx, ts, r.units, pass.now); err != nil {
			return err
		}
	case consolidation.PhaseStrengthen:
		es, err := r.rels.Evidence(ctx)
		if err != nil {
			return fmt.Errorf("consolidate: strengthen: read relation evidence: %w", err)
		}
		changes, corrupted := consolidation.Strengthen(es, pass.since)
		report.reportCorrupted(corrupted)
		if err := r.persistStrengthChanges(ctx, changes, es, pass.now); err != nil {
			return err
		}
	case consolidation.PhaseConnect:
		cs, err := r.units.LiveDecayStates(ctx)
		if err != nil {
			return fmt.Errorf("consolidate: connect: read live decay states: %w", err)
		}
		usable, refused := partitionLiveDecayStates(cs)
		report.reportCorrupted(refused)
		sourceIDs := consolidation.SelectConnectSources(coldToSources(usable), pass.since, pass.now)
		pairs, err := r.connectSources(ctx, sourceIDs)
		if err != nil {
			return err
		}
		if err := r.judgeAndPersistPairs(ctx, pairs, report, pass.now); err != nil {
			return err
		}
	case consolidation.PhaseDerive:
		if err := r.derive(ctx, pass, report); err != nil {
			return err
		}
	case consolidation.PhaseReweight:
		cs, err := r.units.LiveDecayStates(ctx)
		if err != nil {
			return fmt.Errorf("consolidate: reweight: read live decay states: %w", err)
		}
		usable, refused := partitionLiveDecayStates(cs)
		report.reportCorrupted(refused)
		boosts, corrupted := consolidation.Reweight(coldToStates(usable), report.newRelationEdges, pass.now)
		report.reportCorrupted(corrupted)
		if err := r.persistBoosts(ctx, boosts, pass.now); err != nil {
			return err
		}
	case consolidation.PhasePatternEval:
		active, err := r.selfModel.ActiveBeliefs(ctx)
		if err != nil {
			return fmt.Errorf("consolidate: pattern_eval: read active beliefs: %w", err)
		}
		findings := consolidation.EvaluateStagnation(beliefsToConsolidation(active), consolidation.ResolveGoalStagnationDays(pass.cfg.GoalStagnationDays), pass.now)
		if err := r.persistStagnationFindings(ctx, findings, pass.now); err != nil {
			return err
		}

		n, err := r.units.CountLiveByType(ctx, unit.TypeMentalLoad)
		if err != nil {
			return fmt.Errorf("consolidate: pattern_eval: count live mental load units: %w", err)
		}
		lastAt, err := r.state.LastHypothesisAt(ctx)
		if err != nil {
			return fmt.Errorf("consolidate: pattern_eval: read last hypothesis: %w", err)
		}
		if finding, fired := consolidation.EvaluateLoad(n, consolidation.ResolveMentalLoadThreshold(pass.cfg.MentalLoadThreshold), lastAt, pass.now); fired {
			if err := r.persistLoadHypothesis(ctx, finding, lastAt, pass.now); err != nil {
				return err
			}
		}
	case consolidation.PhaseLearn:
		// No work, ever (owner ruling 3) — this arm exists so Order()'s
		// last slot is reached and reported, and performs nothing.
	default:
		return fmt.Errorf("consolidate: %w: %s", consolidation.ErrUnknownPhase, p)
	}
	return nil
}
