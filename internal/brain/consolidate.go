package brain

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"slices"
	"time"

	"github.com/rengo/nooma/internal/core/consolidation"
	"github.com/rengo/nooma/internal/core/recall"
	"github.com/rengo/nooma/internal/core/relation"
	"github.com/rengo/nooma/internal/core/unit"
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
// establish for their own ports, never a second wiring convention.
func NewConsolidateService(clock ports.Clock, cfg ports.ConfigRepo, units ports.UnitRepo, rels ports.RelationRepo, ids ports.IDGen, log ports.DecisionLog, recallSvc *RecallService, judge ports.LLMProvider, selfModel ports.SelfModelRepo) *ConsolidateService {
	return &ConsolidateService{
		clock: clock,
		run:   consolidateRunner{cfg: cfg, units: units, rels: rels, ids: ids, log: log, recall: recallSvc, judge: judge, selfModel: selfModel},
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
// port for both connect's and derive's judge traffic.
type consolidateRunner struct {
	cfg       ports.ConfigRepo
	units     ports.UnitRepo
	rels      ports.RelationRepo
	ids       ports.IDGen
	log       ports.DecisionLog
	recall    *RecallService
	judge     ports.LLMProvider
	selfModel ports.SelfModelRepo
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
// for archive's own LiveDecayStates read (slot 2); connect and derive
// (PR 9/10b, slots 4/5) reuse this exact function over their own
// LiveDecayStates reads rather than duplicating the predicate a third and
// fourth time — the guard that closes the SelectConnectSources NaN-
// comparator hazard those two phases would otherwise feed unfiltered
// (design §8.1's "two uncovered paths").
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
// per pair.
func (r consolidateRunner) judgeAndPersistPairs(ctx context.Context, pairs []consolidation.Pair, now time.Time) error {
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
		if err := r.judgeAndPersistPair(ctx, source, target, now); err != nil {
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
func (r consolidateRunner) judgeAndPersistPair(ctx context.Context, source, target unit.Unit, now time.Time) error {
	resp, err := r.judge.Complete(ctx, ports.LLMRequest{Prompt: JudgePrompt(source, []unit.Unit{target}), Task: taskRelationEvaluation})
	if err != nil {
		return fmt.Errorf("consolidate: connect: judge relation for unit %q: %w", source.ID, err)
	}

	j, _ := relation.DecodeJudgment(resp.Text)
	if j.Type == nil {
		return nil
	}

	row, err := r.rels.ThresholdsFor(ctx, *j.Type)
	if err != nil {
		return fmt.Errorf("consolidate: connect: resolve relation thresholds for %q: %w", *j.Type, err)
	}

	proposed, ok := consolidation.ProposeRelation(source.ID, j, relation.Resolve(row))
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

	rationale := fmt.Sprintf("connect: judged unit %q related to %q as %q", rel.FromUnitID, rel.ToUnitID, rel.Type)
	return r.record(ctx, now, ports.ActionConnectRelationPersisted, rationale, rel)
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
	existingBeliefs := make([]consolidation.Belief, len(active))
	for i, b := range active {
		existingBeliefs[i] = consolidation.Belief{
			ID:               b.ID,
			Facet:            b.Facet,
			TopicKey:         b.TopicKey,
			Content:          b.Content,
			Confidence:       b.Confidence,
			LastReinforcedAt: b.LastReinforcedAt,
		}
	}

	if _, err := r.judge.Complete(ctx, ports.LLMRequest{
		Prompt: consolidation.BuildDerivePrompt(sources, existingBeliefs),
		Task:   taskBeliefDerivation,
	}); err != nil {
		return fmt.Errorf("consolidate: derive: judge belief derivation: %w", err)
	}

	// Embedding (task 10b.4), merge (task 10b.4) and the create/reinforce
	// routing (task 10b.6) land in the following commits — this call
	// already exercises the real dedup-defense-1 prompt (spec R5.6), which
	// is all task 10b.2 owns.
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
// decision_log (spec R4.2's MUST NOT). Four arms wire real I/O by this PR
// (expire_incomplete, archive, strengthen — design §6.3 slots 1-3 — and
// connect's own bounded candidate search, slot 4, whose judge/persist step
// PR 9b still owes); the rest stay placeholders for PR 10-11. default names
// the unhandled phase
// rather than silently doing nothing, so a ninth phase added to Order()
// without a matching case fails loudly here instead of being skipped.
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
		if err := r.judgeAndPersistPairs(ctx, pairs, pass.now); err != nil {
			return err
		}
	case consolidation.PhaseDerive:
		if err := r.derive(ctx, pass, report); err != nil {
			return err
		}
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
