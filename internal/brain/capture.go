package brain

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/rengo/nooma/internal/core/classify"
	"github.com/rengo/nooma/internal/core/prospection"
	"github.com/rengo/nooma/internal/core/recall"
	"github.com/rengo/nooma/internal/core/relation"
	"github.com/rengo/nooma/internal/core/unit"
	"github.com/rengo/nooma/internal/ports"
)

// taskCaptureProcessing is the LLMRequest.Task value capture sends —
// internal/config.DocumentedTaskNames' own "capture_processing" entry
// (internal/config/validate.go). It is a literal here, not an imported
// constant: internal/brain may import internal/core and internal/ports and
// nothing else (docs/06-harness.md §1), and internal/config is neither.
const taskCaptureProcessing = "capture_processing"

// taskRelationEvaluation is the LLMRequest.Task value the relation judge
// sends — internal/config.DocumentedTaskNames' own "relation_evaluation"
// entry, restated as a literal for the same reason taskCaptureProcessing is
// (spec R5.5, design D7).
const taskRelationEvaluation = "relation_evaluation"

// CaptureService is the one entry point into the capture pipeline, and the
// only place in this package holding a ports.Clock — design D4 Layer 1.
//
// The split exists for one reason: a second, independent clock read mid
// operation would let one capture see two different instants (proposal R9's
// named risk). Splitting the type into a thin clock-owning shell (this one)
// and a clockless worker (captureRunner) does not just discourage that
// mistake, it makes it structurally unreachable — captureRunner has no
// field of type ports.Clock and no method that could produce one, so a
// second read can only be introduced by adding a field, which is a
// reviewable act, not a one-line slip. See also
// test/conformance/brain_no_direct_clock_read_test.go and
// test/conformance/brain_single_clock_read_test.go, the two structural
// guards that keep this property from silently rotting.
type CaptureService struct {
	clock ports.Clock
	run   captureRunner
}

// NewCaptureService wires a CaptureService over the ports its pipeline
// needs. clock is read exactly once per Capture call; every other port
// belongs to run, which never sees clock at all. Parameter order follows
// design D4's own captureRunner field order (design.md:346-357), now
// carrying every field that struct declares — rels and judge are this PR's
// own two additions, landing where D4's diagram places them: rels beside
// the other repository ports, judge beside llm as the pipeline's second
// ports.LLMProvider use.
//
// index is the in-memory Index ADR-0012 requires be loaded once at vault
// open — NewCaptureService never builds one itself, it only holds the one
// its caller already loaded.
func NewCaptureService(clock ports.Clock, ids ports.IDGen, units ports.UnitRepo, embeds ports.EmbeddingRepo, lex ports.LexicalSearch, rels ports.RelationRepo, log ports.DecisionLog, llm ports.LLMProvider, judge ports.LLMProvider, embed ports.EmbeddingProvider, index *Index, signals ports.SignalRepo, triggers ports.TriggerRepo, timers ports.TimerRepo) *CaptureService {
	sharedRecall := NewRecallService(index, lex, units, embed)
	return &CaptureService{
		clock: clock,
		run: captureRunner{
			ids:      ids,
			units:    units,
			embeds:   embeds,
			lex:      lex,
			rels:     rels,
			log:      log,
			llm:      llm,
			judge:    judge,
			embed:    embed,
			index:    index,
			recall:   sharedRecall,
			triggers: triggers,
			timers:   timers,
			correction: correctionRunner{
				units: units, log: log, signals: signals, ids: ids, recall: sharedRecall,
			},
		},
	}
}

// Capture is the package's only ports.Clock.Now() read (spec R4.1, design
// D4). Every timestamp this pipeline produces — classify's local-date
// injection, I18's three unit timestamps, decision_log.occurred_at — comes
// from this one instant, passed down as a plain argument from here on. A
// second Capture call reads the clock again, as it must (two captures are
// two operations), but within a single call there is exactly one read.
func (s *CaptureService) Capture(ctx context.Context, in CaptureInput) (CaptureResult, error) {
	return s.run.at(ctx, in, s.clock.Now())
}

// captureRunner does the actual work of one capture, over every port
// CaptureService.Capture needs except the clock (design D4 Layer 1).
//
// This is PR 11c's slice, closing design D4's diagram: rels (PR 11b's
// ports.RelationRepo) and judge (the pipeline's second ports.LLMProvider
// use, spec R5.5) persist and log the relation judge's verdict against the
// candidates PR 10b's recall step already finds.
type captureRunner struct {
	ids    ports.IDGen
	units  ports.UnitRepo
	embeds ports.EmbeddingRepo
	lex    ports.LexicalSearch
	rels   ports.RelationRepo
	log    ports.DecisionLog
	llm    ports.LLMProvider // capture_processing
	judge  ports.LLMProvider // relation_evaluation
	embed  ports.EmbeddingProvider
	index  *Index
	// recall is design D9's one shared RecallService instance — built once
	// by NewCaptureService, not per capture (fixing a per-call construction
	// this field replaces below). correction holds the same instance
	// (design D7: "the correction path needs the same instance").
	recall *RecallService
	// correction is 12g's own clockless worker for the correction path
	// (design D7), owned by captureRunner and built once, alongside recall,
	// rather than per capture.
	correction correctionRunner
	// triggers and timers are what arming writes through. There is no
	// clock field beside them on purpose: Arm(c, now) reuses the instant
	// Capture already read, so the single-clock-read rule holds by
	// construction rather than by a reviewer noticing.
	triggers ports.TriggerRepo
	timers   ports.TimerRepo
}

// at runs one capture given the instant CaptureService.Capture already
// read. It is design.md's pipeline diagram, restricted to this PR's scope
// (spec R4.2-R4.7): classify.BuildPrompt -> llm.Complete -> classify.Decode
// -> route on c.Kind (design D9) -> classify.ToUnit -> units.Create, one or
// two decision_log rows naming what happened, then embed.Embed ->
// embeds.Put (design D8's persist-before-embed ordering), then hybrid
// recall for dedup/relation candidates (design D5) once that embedding
// exists.
//
// The routing step is Q3a's refusal path (design D9): a timer or
// recurring_reminder classification never reaches classify.ToUnit at all —
// doc 02 §8's bold sentence ("a timer is NEVER a unit") means there is
// nothing for ToUnit to build, so this returns before r.ids.New() is even
// called for a unit, not after a persist attempt that is then discarded.
// Every other classification proceeds to classify.ToUnit as before; an
// ambiguous person reference still persists (spec R4.7 — Q3a's closed
// reasoning: an incomplete unit would be invisible to every live read
// surface and immortal until M2), but writes capture.unit.created instead
// of capture.classify, plus a second row naming the unresolved reference.
//
// Once recall finds candidates, this PR's own step asks the relation judge
// what to do with them (design D4's diagram tail, spec R5.4/R5.5): one
// judge.Complete call over the whole bounded candidate list, decoded by
// relation.DecodeJudgment, decided by relation.Decide against the
// candidates' type's resolved thresholds, and persisted or discarded with a
// decision_log row naming the outcome. Design D8's full routing table:
// chitchat/out_of_scope are discarded (13a), correction and recall each
// fork to their own mechanism before ToUnit is ever reached (12g, below) —
// only a classification with no Kind at all still propagates as a plain Go
// error here.
func (r captureRunner) at(ctx context.Context, in CaptureInput, now time.Time) (CaptureResult, error) {
	// beliefs is always nil in M1 — design D4: nothing reads self_beliefs
	// yet (derive is M2, seeding is M4), so there is nothing to project.
	prompt := classify.BuildPrompt(in.Text, nil, now)

	resp, err := r.llm.Complete(ctx, ports.LLMRequest{Prompt: prompt, Task: taskCaptureProcessing})
	if err != nil {
		return CaptureResult{}, fmt.Errorf("capture: classify completion: %w", err)
	}

	c, err := classify.Decode(resp.Text, now)
	if err != nil {
		if errors.Is(err, classify.ErrNoFieldsSalvaged) {
			rationale := fmt.Sprintf("classification response could not be parsed: %s — no field was salvaged", err)
			ctxValue := struct {
				Error string `json:"error"`
			}{Error: err.Error()}
			if logErr := r.recordOrphanDecision(ctx, ports.ActionCaptureUnparseable, rationale, now, ctxValue); logErr != nil {
				return CaptureResult{}, logErr
			}
		}
		return CaptureResult{}, fmt.Errorf("capture: decode classification: %w", err)
	}

	// The arming fork (design §3.5), in the position the retired timer
	// refusal held: before classify.ToUnit, so a timer never reaches it
	// and I04 stays structural rather than remembered. Arm takes the
	// instant Capture already read — there is no second clock read here,
	// and captureRunner holds no clock to make one with.
	if plan, armed := prospection.Arm(c, now); armed {
		result, err := r.arm(ctx, c, plan, now)
		if err != nil {
			return CaptureResult{}, err
		}
		return result, nil
	}

	// The correction fork (design D7/D8, spec R1.1): a correction never
	// reaches classify.ToUnit — it forks to correctionRunner.at, the whole
	// referent-resolution/edit-plan/pre-image orchestration (12b, 12c,
	// 12f-i, 12f-ii, 13a all converge here). An ambiguous referent or an
	// ambiguous edit plan asks rather than edits; either way this returns
	// before classify.ToUnit is ever reached, and r.units.Create is never
	// called for this Kind.
	if c.Kind != nil && *c.Kind == classify.KindCorrection {
		corr, err := r.correction.at(ctx, in, c, now)
		if err != nil {
			return CaptureResult{}, fmt.Errorf("capture: correction: %w", err)
		}
		if corr.Ambiguous {
			return CaptureResult{Outcome: OutcomeAsked, Correction: corr}, nil
		}
		return CaptureResult{Outcome: OutcomeCorrected, Correction: corr}, nil
	}

	// The recall fork (spec R2.3, design D9; Conflicts §C11): a
	// `recall`-classified capture never reaches classify.ToUnit either — it
	// answers through the exact RecallService.ForText both entrances share
	// (I22), searching in.Text — never c.NormalizedContent, D9's forced
	// argument — and never persists a unit (R2.3's own MUST NOT).
	if c.Kind != nil && *c.Kind == classify.KindRecall {
		units, _, err := r.recall.ForText(ctx, in.Text)
		if err != nil {
			return CaptureResult{}, fmt.Errorf("capture: recall: %w", err)
		}
		return CaptureResult{Outcome: OutcomeRecalled, Recalled: units}, nil
	}

	// The discard fork (design D8, 13a's own half of it): chitchat and
	// out_of_scope are not memory, so they never reach classify.ToUnit at
	// all — the same "route before ToUnit" shape the retired timer refusal
	// established.
	if c.Kind != nil && (*c.Kind == classify.KindChitchat || *c.Kind == classify.KindOutOfScope) {
		rationale := fmt.Sprintf("classified as %q — not memory content, nothing was stored", string(*c.Kind))
		ctxValue := struct {
			Kind string `json:"kind"`
		}{Kind: string(*c.Kind)}
		if err := r.recordOrphanDecision(ctx, ports.ActionCaptureDiscarded, rationale, now, ctxValue); err != nil {
			return CaptureResult{}, err
		}
		return CaptureResult{Outcome: OutcomeDiscarded}, nil
	}

	// A classification with no type at all has nothing left to decide from
	// (docs/02-cognitive-core.md §5.1's own floor) — classify.ToUnit would
	// return ErrNoUnitType for this too, but capture.classify.unclassifiable
	// needs its own decision_log row first (design D8).
	if c.Kind == nil {
		ctxValue := struct {
			Reason string `json:"reason"`
		}{Reason: "no_type"}
		if err := r.recordOrphanDecision(ctx, ports.ActionCaptureUnclassifiable, "classification carried no type — nothing was stored", now, ctxValue); err != nil {
			return CaptureResult{}, err
		}
		return CaptureResult{}, fmt.Errorf("capture: build unit: %w", classify.ErrNoUnitType)
	}

	// The base priors, design D3: there are exactly two numbers, not
	// eighteen, and both are migration 0001's own column defaults.
	priors := classify.Priors{Weight: classify.PriorWeight, DecayRate: classify.PriorDecayRate}
	u, err := classify.ToUnit(c, r.ids.New(), in.Channel, now, priors)
	if err != nil {
		return CaptureResult{}, fmt.Errorf("capture: build unit: %w", err)
	}

	if err := r.units.Create(ctx, u); err != nil {
		return CaptureResult{}, fmt.Errorf("capture: persist unit %q: %w", u.ID, err)
	}

	ambiguousPersonRef := c.PersonRefStatus != nil && *c.PersonRefStatus == classify.PersonRefStatusAmbiguous
	if ambiguousPersonRef {
		if err := r.recordUnitCreatedDecision(ctx, c, u, now); err != nil {
			return CaptureResult{}, err
		}
		if err := r.recordAmbiguousPersonRefDecision(ctx, u, now); err != nil {
			return CaptureResult{}, err
		}
	} else if err := r.recordClassifyDecision(ctx, c, u, now); err != nil {
		return CaptureResult{}, err
	}

	ev, embedded, err := r.embedAndStore(ctx, u, now)
	if err != nil {
		return CaptureResult{}, err
	}

	var candidateUnits []unit.Unit
	if embedded {
		candidateUnits, err = r.recallCandidates(ctx, u, ev)
		if err != nil {
			return CaptureResult{}, err
		}
	}

	if len(candidateUnits) > 0 {
		if err := r.judgeRelation(ctx, u, candidateUnits, now); err != nil {
			return CaptureResult{}, err
		}
	}

	candidates := make([]string, len(candidateUnits))
	for i, cu := range candidateUnits {
		candidates[i] = cu.ID
	}

	return CaptureResult{Outcome: OutcomeStored, UnitID: u.ID, Embedded: embedded, Candidates: candidates}, nil
}

// embedAndStore is design D8's step past persistence (spec R4.3): embed u's
// content and write the result, without ever turning a failure here into a
// failure of Capture itself. u is already durable by the time this runs
// (design D8's persist-before-embed ordering), so a local provider or store
// outage must degrade the index, not refuse the capture — doc 02 §5's
// product rule ("Nooma captures with what it has ... and only asks when
// ambiguity blocks it") forbids the atomic alternative that was considered
// and rejected.
//
// It returns (ev, true, nil) once the embedding is written — ev is what
// recallCandidates needs next, so the pipeline never asks the embedding
// provider for the same unit's vector twice — or (ports.EmbedResponse{},
// false, nil) once a failure has been recorded to decision_log as
// ports.ActionCaptureEmbeddingFailed — the caller-visible degradation
// design D8 names rather than hides. The only way this returns a non-nil
// error is recordEmbeddingFailedDecision's own log write failing, which is
// not a case design D8 discusses; it is handled the same way
// recordClassifyDecision already handles its own log-write failure, by
// propagating it.
func (r captureRunner) embedAndStore(ctx context.Context, u unit.Unit, now time.Time) (ports.EmbedResponse, bool, error) {
	ev, err := r.embed.Embed(ctx, ports.EmbedRequest{Text: u.Content})
	if err != nil {
		return ports.EmbedResponse{}, false, r.recordEmbeddingFailedDecision(ctx, u, now, err)
	}

	if err := r.embeds.Put(ctx, ports.Embedding{UnitID: u.ID, Model: ev.Model, Vector: ev.Vector, At: now}); err != nil {
		return ports.EmbedResponse{}, false, r.recordEmbeddingFailedDecision(ctx, u, now, err)
	}

	return ev, true, nil
}

// recallCandidates runs design D4's hybrid-recall step (spec R4.4) for u,
// now that its embedding ev is in hand. It normalizes ev.Vector into design
// D4's own pipeline-diagram "normalized" (recall.Normalize — the same
// storage-boundary obligation the SQL EmbeddingRepo already applies at
// rest, restated here for the resident Index, which no adapter sits in
// front of), grows the resident Index so this capture's own recall step —
// and every capture or recall after it, before the next vault open — can
// find u, then asks r.recall — captureRunner's own one shared RecallService
// instance (design D9), not a fresh one per capture — for u's candidates,
// excluding u itself.
//
// It only runs once embedAndStore has already reported success (r.at's own
// caller): an unembedded unit has no vector to search with, and running
// recall over an incomplete index would stack a second, silent degradation
// on top of the one design D8 already names and accepts for the embedding
// step alone. Nothing in this slice's tests exercises recall over a vector
// that failed to normalize either — a zero vector (recall.ErrZeroVector) is
// treated the same way, as a reason to skip this capture's own recall
// rather than to fail it, but that path is not independently proven here.
//
// It returns the full []unit.Unit RecallService.Candidates already
// materializes, not merely their ids — PR 10b's own note (design D5:
// "materializes the units the fused ranking needs anyway — for the response
// and for the judge's prompt") named this PR's own need for full candidate
// content without a second ports.UnitRepo.LiveByIDs round trip. r.at narrows
// this to ids for CaptureResult.Candidates itself.
func (r captureRunner) recallCandidates(ctx context.Context, u unit.Unit, ev ports.EmbedResponse) ([]unit.Unit, error) {
	normalized, err := recall.Normalize(ev.Vector)
	if err != nil {
		return nil, nil
	}

	r.index.Add(u.ID, normalized)

	cand, err := r.recall.Candidates(ctx, u.Content, normalized, ev.Model, u.ID)
	if err != nil {
		return nil, fmt.Errorf("capture: recall candidates for unit %q: %w", u.ID, err)
	}
	return cand, nil
}

// judgeRelation is design D4's diagram tail (spec R5.4, R5.5; design D7):
// one relation-judge call over candidates (bounded to
// relation.DedupCandidateK — design D7's own named bound on prompt size and
// cost), decoded by relation.DecodeJudgment, decided by relation.Decide
// against the resolved thresholds for the type the judgment names, and
// either discarded (I08: no relations row, one decision_log row naming why)
// or persisted via r.rels.Upsert (I07) with a decision_log row of its own —
// relation.persisted for an ordinary related outcome, relation.duplicate.
// recorded for a duplicate outcome, whose relation is still written
// (design D7: "recorded and not merged", not acted on further).
//
// It runs only once r.at's own caller has already confirmed candidates is
// non-empty (spec R4.4's own stated property, restated by design D4:
// "the judge is not called when the candidate list is empty" —
// fakeprovider's unscripted-call failure enforces this for free once a test
// scripts no relation_evaluation case).
//
// C14a resolved what design D4's own diagram left as an omission rather
// than a decision: `jr := judge.Complete(...)`'s error does NOT propagate
// as a Capture error. u is already durably persisted by this point (design
// D8's ordering), so refusing the whole capture over a local judge-provider
// outage would tell the user their note failed when it is safely stored —
// the same half-synced shape design D8 already rejected for the embedding
// step, mirrored here rather than re-argued: degrade
// (ports.ActionCaptureDedupFailed, recordDedupFailedDecision below), store
// no relations, return nil. The only way this branch itself returns a
// non-nil error is recordDedupFailedDecision's own log write failing — the
// same posture recordEmbeddingFailedDecision and recordClassifyDecision
// already take toward their own log-write failures, by propagating them.
// `j, _ := relation.DecodeJudgment(...)` DOES discard its error: a response
// with nothing salvaged decodes to a zero Judgment, which the nil-Outcome
// check below already treats as nothing to persist and nothing to log — the
// same outcome an explicit error branch would produce, with no second
// branch to maintain.
//
// This PR's own open decision, because design D7 resolves discard,
// uncertain/asserted and duplicate but not this one: an Outcome of "new", a
// nil Outcome, a nil Confidence, a "related" outcome with no Type, or any
// outcome with no TargetUnitID, are all treated as nothing decided — no
// relations row, no decision_log row, mirroring design D9's own "reads are
// not logged" reasoning for a step with no effect. A missing Strength on an
// otherwise-actionable judgment defaults to 0.0 (doc 02 §4's own low end,
// "a passing mention") rather than blocking the persist — Strength has no
// threshold gate the way Confidence does.
func (r captureRunner) judgeRelation(ctx context.Context, u unit.Unit, candidates []unit.Unit, now time.Time) error {
	if len(candidates) > relation.DedupCandidateK {
		candidates = candidates[:relation.DedupCandidateK]
	}

	resp, err := r.judge.Complete(ctx, ports.LLMRequest{Prompt: JudgePrompt(u, candidates), Task: taskRelationEvaluation})
	if err != nil {
		return r.recordDedupFailedDecision(ctx, u, now, err)
	}

	j, _ := relation.DecodeJudgment(resp.Text)
	if j.Outcome == nil || *j.Outcome == relation.OutcomeNew || j.Confidence == nil || j.TargetUnitID == nil {
		return nil
	}

	relType := "duplicate"
	if *j.Outcome == relation.OutcomeRelated {
		if j.Type == nil {
			return nil
		}
		relType = *j.Type
	}

	row, err := r.rels.ThresholdsFor(ctx, relType)
	if err != nil {
		return fmt.Errorf("capture: resolve relation thresholds for %q: %w", relType, err)
	}
	verdict := relation.Decide(*j.Confidence, relation.Resolve(row))

	if verdict == relation.Discard {
		return r.recordRelationDiscardedDecision(ctx, u, *j.TargetUnitID, relType, *j.Confidence, now)
	}

	strength := 0.0
	if j.Strength != nil {
		strength = *j.Strength
	}
	rel := ports.Relation{
		ID:         r.ids.New(),
		FromUnitID: u.ID,
		ToUnitID:   *j.TargetUnitID,
		Type:       relType,
		Strength:   strength,
		Confidence: *j.Confidence,
		CreatedBy:  string(relation.CreatedBySystem),
		CreatedAt:  now,
	}
	if err := r.rels.Upsert(ctx, rel); err != nil {
		return fmt.Errorf("capture: persist relation %s->%s: %w", rel.FromUnitID, rel.ToUnitID, err)
	}

	if *j.Outcome == relation.OutcomeDuplicate {
		return r.recordRelationDuplicateDecision(ctx, u, rel, now)
	}
	return r.recordRelationPersistedDecision(ctx, u, rel, now)
}

// JudgePrompt renders the relation-judge request body. Design D4's own
// diagram writes this request directly at the brain call site ("{...}"),
// never as a core/relation step the way classify.BuildPrompt is for
// capture_processing — this PR's own task list (11c.1-11c.5) assigns
// core/relation exactly Judgment and DecodeJudgment, no prompt builder. Kept
// in internal/brain for that literal reading, not because it could not be
// pure: it is a pure function of u and candidates, and moving it to
// core/relation later would be a compatible change, not a breaking one.
//
// Exported (project/quality-gate-sends-stub-prompts) so `nooma doctor`'s
// structured-JSON quality gate (cmd/nooma/doctor.go) can call the exact
// function production calls to build a relation_evaluation prompt, rather
// than sending a corpus case's own separately recorded stub — the defect
// that made the gate fail 21 of 21 prompts against a real provider before
// it had ever once run against one.
func JudgePrompt(u unit.Unit, candidates []unit.Unit) string {
	var b strings.Builder
	b.WriteString("You judge whether a new memory unit duplicates or relates to any of the candidates below.\n")
	b.WriteString("Answer with one JSON object and nothing else — no prose, no code fence.\n\n")

	b.WriteString("New unit\n")
	b.WriteString("  " + u.Content + "\n\n")

	b.WriteString("Candidates\n")
	for _, c := range candidates {
		b.WriteString("  " + c.ID + ": " + c.Content + "\n")
	}
	b.WriteString("\n")

	b.WriteString("Required fields\n")
	b.WriteString("  outcome              one of: new | duplicate | related\n\n")

	b.WriteString("Optional fields — omit unless outcome is duplicate or related\n")
	b.WriteString("  target_unit_id       the candidate's id above\n")
	b.WriteString("  type                 open text naming the relation, e.g. same_topic (omit for duplicate)\n")
	b.WriteString("  strength             0-1, how strongly the new unit is about that relation\n")
	b.WriteString("  confidence           0-1, how sure you are\n")

	return b.String()
}

// recordClassifyDecision writes decision_log's account of an ordinary
// capture (spec R4.5, I12): classify produced a persisted unit, and that is
// the one decision this slice's path makes with an effect. action is
// ports.ActionCaptureClassify, matching migration 0001's own DDL comment
// example and spec R4.5's naming — a caller reading decision_log later
// needs no separate lookup table to know what "capture.classify" means.
//
// occurred_at is now, the same single clock read every other timestamp in
// this capture used (spec R4.1) — a decision row timestamped separately
// from the unit it describes would let the two disagree about when the
// capture actually happened.
func (r captureRunner) recordClassifyDecision(ctx context.Context, c classify.Classification, u unit.Unit, now time.Time) error {
	rationale := fmt.Sprintf("classified as %q and persisted a %s unit", string(*c.Kind), u.Status)
	contextJSON, err := json.Marshal(struct {
		Kind   string `json:"kind"`
		UnitID string `json:"unit_id"`
		Source string `json:"source"`
	}{Kind: string(*c.Kind), UnitID: u.ID, Source: u.Source})
	if err != nil {
		return fmt.Errorf("capture: encode decision context: %w", err)
	}

	d := ports.Decision{
		ID:         r.ids.New(),
		Action:     ports.ActionCaptureClassify,
		Rationale:  rationale,
		Context:    contextJSON,
		OccurredAt: now,
	}
	if err := r.log.Record(ctx, d); err != nil {
		return fmt.Errorf("capture: record decision for unit %q: %w", u.ID, err)
	}
	return nil
}

// recordUnitCreatedDecision writes decision_log's account of the first of
// the ambiguous-person-reference scenario's two decisions (spec R4.5, R4.7;
// design D9): a unit was created despite the reference being unresolved.
// action is ports.ActionCaptureUnitCreated, not ports.ActionCaptureClassify
// — design.md:934 reserves capture.unit.created for exactly this two-row
// scenario, distinguishing it from the ordinary single-row path
// recordClassifyDecision already covers.
//
// occurred_at is now, the same single clock read every other timestamp in
// this capture used (spec R4.1) — the same reason recordClassifyDecision
// gives.
func (r captureRunner) recordUnitCreatedDecision(ctx context.Context, c classify.Classification, u unit.Unit, now time.Time) error {
	rationale := fmt.Sprintf("classified as %q with an ambiguous person reference, and persisted a %s unit rather than holding it", string(*c.Kind), u.Status)
	contextJSON, err := json.Marshal(struct {
		Kind   string `json:"kind"`
		UnitID string `json:"unit_id"`
		Source string `json:"source"`
	}{Kind: string(*c.Kind), UnitID: u.ID, Source: u.Source})
	if err != nil {
		return fmt.Errorf("capture: encode unit-created decision context: %w", err)
	}

	d := ports.Decision{
		ID:         r.ids.New(),
		Action:     ports.ActionCaptureUnitCreated,
		Rationale:  rationale,
		Context:    contextJSON,
		OccurredAt: now,
	}
	if err := r.log.Record(ctx, d); err != nil {
		return fmt.Errorf("capture: record unit-created decision for unit %q: %w", u.ID, err)
	}
	return nil
}

// recordAmbiguousPersonRefDecision writes decision_log's account of the
// second of the ambiguous-person-reference scenario's two decisions (spec
// R4.5, R4.7; design D9): the reference itself was never resolved.
//
// Its action is ports.ActionCapturePersonRefAmbiguous, which it no longer
// shares with anything. It used to be ports.ActionCaptureHookDeferred, one
// bucket for two unrelated facts told apart only by context.kind; the other
// producer was the timer/recurring_reminder refusal this change retires, and
// once that went, the name described nothing left standing.
//
// This decision does not accompany a distinct outcome — the unit u names is
// already persisted (spec R4.7's own MUST: unit.StatusPool, never
// unit.StatusIncomplete), so the rationale says what was deferred
// (disambiguation), not what was refused.
func (r captureRunner) recordAmbiguousPersonRefDecision(ctx context.Context, u unit.Unit, now time.Time) error {
	rationale := fmt.Sprintf("person reference in unit %q was ambiguous and left unresolved; the unit was stored complete rather than held, because nothing can promote an incomplete unit before M2", u.ID)
	contextJSON, err := json.Marshal(struct {
		Kind   string `json:"kind"`
		UnitID string `json:"unit_id"`
	}{Kind: "ambiguous_person_ref", UnitID: u.ID})
	if err != nil {
		return fmt.Errorf("capture: encode ambiguous-person-ref decision context: %w", err)
	}

	d := ports.Decision{
		ID:         r.ids.New(),
		Action:     ports.ActionCapturePersonRefAmbiguous,
		Rationale:  rationale,
		Context:    contextJSON,
		OccurredAt: now,
	}
	if err := r.log.Record(ctx, d); err != nil {
		return fmt.Errorf("capture: record ambiguous-person-ref decision for unit %q: %w", u.ID, err)
	}
	return nil
}

// arm persists what one Plan decided and reports it back to the caller.
//
// One row, in one table, per Plan — never both, which is why the switch
// has no shared tail: a timer and a trigger disagree about almost every
// column, and a merged writer would have to carry the union of two shapes
// and decide which half to leave zero.
//
// The id is generated here rather than inside prospection: Plan is a
// decision about what to arm, and an id is a fact about a row that does
// not exist yet.
func (r captureRunner) arm(ctx context.Context, c classify.Classification, plan prospection.Plan, now time.Time) (CaptureResult, error) {
	id := r.ids.New()

	switch plan.What {
	case prospection.ArmTimer:
		// action_text is the request VERBATIM — doc 02 §8: "the request is
		// stored verbatim and only worded at delivery time". Wording it
		// here would spend an LLM call at capture on text that may never
		// be shown, and would lose the user's own words for good.
		//
		// c.NormalizedContent is a *string because classify degrades a
		// missing field to nil, and that nil is passed through rather than
		// flattened to "": timers.action_text's own NULL already means
		// "generic nudge" (migration 0001:65), which is exactly what a
		// timer with no salvaged text is.
		if err := r.timers.Create(ctx, ports.Timer{
			ID:         id,
			FireAt:     plan.FireAt,
			ActionText: c.NormalizedContent,
			CreatedAt:  now,
		}); err != nil {
			return CaptureResult{}, fmt.Errorf("capture: arm timer: %w", err)
		}
		if err := r.recordArmedDecision(ctx, ports.ActionCaptureArmedTimer, id, plan, now); err != nil {
			return CaptureResult{}, err
		}

	case prospection.ArmTrigger, prospection.ArmRecurring:
		fireAt := plan.FireAt
		trigger := ports.Trigger{
			ID:             id,
			Kind:           ports.TriggerKindTimeBased,
			InterruptLevel: interruptColumn(plan.Interrupt),
			Payload: ports.TriggerPayload{
				// The trigger payload is JSON, not a nullable column, so
				// a degraded normalized_content lands as the empty string
				// here rather than as a key meaning "generic nudge" —
				// triggers have no such concept, and inventing one for
				// them would give payload.action two readings.
				ActionText: derefString(c.NormalizedContent),
				Rationale:  armRationale(plan),
				LeadDays:   plan.LeadDays,
			},
			FireAt:    &fireAt,
			CreatedAt: now,
		}

		action := ports.ActionCaptureArmedTrigger
		if plan.What == prospection.ArmRecurring {
			// The recurrence columns are written only for ArmRecurring.
			// Plan.Rule and Plan.Anchor carry zero values for the one-shot
			// case, and writing those would claim a recurrence the user
			// never asked for.
			rule, anchor := plan.Rule, plan.Anchor
			trigger.RecurrenceRule = &rule
			trigger.RecurrenceAnchor = &anchor
			action = ports.ActionCaptureArmedRecurring
		}

		if err := r.triggers.Create(ctx, trigger); err != nil {
			return CaptureResult{}, fmt.Errorf("capture: arm trigger: %w", err)
		}
		if err := r.recordArmedDecision(ctx, action, id, plan, now); err != nil {
			return CaptureResult{}, err
		}

	default:
		// Unreachable while Arm's second return value is the guard: it is
		// false for ArmNothing. A default that returned a zero
		// CaptureResult would answer a caller with an outcome of "", so
		// this fails loudly instead.
		return CaptureResult{}, fmt.Errorf("capture: arm: unhandled armament %q", plan.What)
	}

	return CaptureResult{
		Outcome: OutcomeArmed,
		Armed:   &Armed{What: plan.What, ID: id, FireAt: plan.FireAt},
	}, nil
}

// recordArmedDecision writes decision_log's account of one arming (doc 02
// §11's glass box).
//
// Three actions rather than one carrying the armament in context, following
// the rule that effects split when their Context shapes differ: a timer row
// has no lead days, no recurrence and no interrupt level, so a merged
// action would produce rows whose keys mean "absent" for one armament and
// "missing" for another with nothing to tell them apart. m3d's re-arm needs
// its own action too, which would read oddly beside a merged capture.armed.
func (r captureRunner) recordArmedDecision(ctx context.Context, action ports.DecisionAction, id string, plan prospection.Plan, now time.Time) error {
	rationale := armRationale(plan)

	type armedContext struct {
		ArmedID        string   `json:"armed_id"`
		What           string   `json:"what"`
		FireAt         string   `json:"fire_at"`
		LeadDays       *int     `json:"lead_days,omitempty"`
		RecurrenceRule *string  `json:"recurrence_rule,omitempty"`
		InterruptLevel *float64 `json:"interrupt_level,omitempty"`
		// InterruptDegraded travels beside the level rather than being
		// inferred from its absence: a NULL level and a level the store
		// could not use look identical downstream, and the glass box
		// should show both what was stored and what was made of it.
		InterruptDegraded bool `json:"interrupt_degraded"`
	}

	ctxValue := armedContext{
		ArmedID:           id,
		What:              string(plan.What),
		FireAt:            plan.FireAt.UTC().Format(time.RFC3339),
		InterruptLevel:    interruptColumn(plan.Interrupt),
		InterruptDegraded: plan.Interrupt.Degraded(),
	}
	if plan.What != prospection.ArmTimer {
		leadDays := plan.LeadDays
		ctxValue.LeadDays = &leadDays
	}
	if plan.What == prospection.ArmRecurring {
		rule := string(plan.Rule)
		ctxValue.RecurrenceRule = &rule
	}

	contextJSON, err := json.Marshal(ctxValue)
	if err != nil {
		return fmt.Errorf("capture: encode armed decision context: %w", err)
	}

	d := ports.Decision{
		ID:         r.ids.New(),
		Action:     action,
		Rationale:  rationale,
		Context:    contextJSON,
		OccurredAt: now,
	}
	if err := r.log.Record(ctx, d); err != nil {
		return fmt.Errorf("capture: record armed decision for %q: %w", id, err)
	}
	return nil
}

// derefString reads a possibly-degraded classify field into the plain
// string a JSON payload needs.
func derefString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// armRationale is doc 02 §11's human-readable sentence for one arming — a
// sentence, not a code a reader has to look up.
func armRationale(plan prospection.Plan) string {
	switch plan.What {
	case prospection.ArmTimer:
		return fmt.Sprintf("armed a timer to fire at %s", plan.FireAt.UTC().Format(time.RFC3339))
	case prospection.ArmRecurring:
		return fmt.Sprintf("armed a %s recurring trigger, first firing at %s, %d days ahead of the occurrence",
			plan.Rule, plan.FireAt.UTC().Format(time.RFC3339), plan.LeadDays)
	default:
		return fmt.Sprintf("armed a trigger to fire at %s, %d days ahead of the event",
			plan.FireAt.UTC().Format(time.RFC3339), plan.LeadDays)
	}
}

// recordOrphanDecision writes one of design D8's three orphan-action rows
// this PR gives a caller — capture.discarded, capture.classify.unparseable,
// capture.classify.unclassifiable — sharing one shape (a rationale plus a
// context value, nothing else) unlike this file's other record* methods,
// none of which have a unit or a full classification to embed.
func (r captureRunner) recordOrphanDecision(ctx context.Context, action ports.DecisionAction, rationale string, now time.Time, ctxValue any) error {
	contextJSON, err := json.Marshal(ctxValue)
	if err != nil {
		return fmt.Errorf("capture: encode %s decision context: %w", action, err)
	}

	d := ports.Decision{ID: r.ids.New(), Action: action, Rationale: rationale, Context: contextJSON, OccurredAt: now}
	if err := r.log.Record(ctx, d); err != nil {
		return fmt.Errorf("capture: record %s decision: %w", action, err)
	}
	return nil
}

// recordEmbeddingFailedDecision writes decision_log's account of design
// D8's accepted gap: u is persisted, but r.embed.Embed or r.embeds.Put
// failed. action is ports.ActionCaptureEmbeddingFailed, and cause's message
// travels in context.error — task 10b.8's own MUST ("a capture.embedding.failed
// decision_log row with the provider error in context").
//
// occurred_at is now, the same single clock read every other timestamp in
// this capture used (spec R4.1), for the same reason recordClassifyDecision
// uses it: a decision row timestamped separately from the capture it
// describes would let the two disagree about when the failure actually
// happened.
func (r captureRunner) recordEmbeddingFailedDecision(ctx context.Context, u unit.Unit, now time.Time, cause error) error {
	rationale := fmt.Sprintf("embedding failed for unit %q: %s — the unit is persisted and lexically findable, but not yet semantically searchable", u.ID, cause)
	contextJSON, err := json.Marshal(struct {
		UnitID string `json:"unit_id"`
		Error  string `json:"error"`
	}{UnitID: u.ID, Error: cause.Error()})
	if err != nil {
		return fmt.Errorf("capture: encode embedding-failed decision context: %w", err)
	}

	d := ports.Decision{
		ID:         r.ids.New(),
		Action:     ports.ActionCaptureEmbeddingFailed,
		Rationale:  rationale,
		Context:    contextJSON,
		OccurredAt: now,
	}
	if err := r.log.Record(ctx, d); err != nil {
		return fmt.Errorf("capture: record embedding-failed decision for unit %q: %w", u.ID, err)
	}
	return nil
}

// recordDedupFailedDecision writes decision_log's account of C14a's
// resolution: judgeRelation's own r.judge.Complete call failed. action is
// ports.ActionCaptureDedupFailed, and cause's message travels in
// context.error — the same shape recordEmbeddingFailedDecision already
// gives context.unit_id and context.error for its own sibling gap.
//
// This degrades only the provider call. A failure of this method's own
// r.log.Record below still propagates, exactly as
// recordEmbeddingFailedDecision and recordClassifyDecision already do for
// theirs — an audit trail that swallows its own write failure is not one.
//
// occurred_at is now, the same single clock read every other timestamp in
// this capture used (spec R4.1), for the same reason
// recordEmbeddingFailedDecision uses it.
func (r captureRunner) recordDedupFailedDecision(ctx context.Context, u unit.Unit, now time.Time, cause error) error {
	rationale := fmt.Sprintf("relation judge failed for unit %q: %s — the unit is persisted, but no relations were evaluated for it", u.ID, cause)
	contextJSON, err := json.Marshal(struct {
		UnitID string `json:"unit_id"`
		Error  string `json:"error"`
	}{UnitID: u.ID, Error: cause.Error()})
	if err != nil {
		return fmt.Errorf("capture: encode dedup-failed decision context: %w", err)
	}

	d := ports.Decision{
		ID:         r.ids.New(),
		Action:     ports.ActionCaptureDedupFailed,
		Rationale:  rationale,
		Context:    contextJSON,
		OccurredAt: now,
	}
	if err := r.log.Record(ctx, d); err != nil {
		return fmt.Errorf("capture: record dedup-failed decision for unit %q: %w", u.ID, err)
	}
	return nil
}

// recordRelationDiscardedDecision writes decision_log's account of a
// relation candidate the judge evaluated but that did not clear
// min_confidence_to_persist (spec R5.4, I08): no relations row is written
// for it (I08's own "below this, it is not even stored"), only this row,
// naming what was considered and why it was dropped.
//
// occurred_at is now, the same single clock read every other timestamp in
// this capture used (spec R4.1), for the same reason every other record*
// method in this file uses it.
func (r captureRunner) recordRelationDiscardedDecision(ctx context.Context, u unit.Unit, targetUnitID, relType string, confidence float64, now time.Time) error {
	rationale := fmt.Sprintf("relation candidate %q (type %q) scored %.2f confidence, below min_confidence_to_persist — discarded, not stored", targetUnitID, relType, confidence)
	contextJSON, err := json.Marshal(struct {
		FromUnitID string  `json:"from_unit_id"`
		ToUnitID   string  `json:"to_unit_id"`
		Type       string  `json:"type"`
		Confidence float64 `json:"confidence"`
	}{FromUnitID: u.ID, ToUnitID: targetUnitID, Type: relType, Confidence: confidence})
	if err != nil {
		return fmt.Errorf("capture: encode relation-discarded decision context: %w", err)
	}

	d := ports.Decision{
		ID:         r.ids.New(),
		Action:     ports.ActionRelationDiscarded,
		Rationale:  rationale,
		Context:    contextJSON,
		OccurredAt: now,
	}
	if err := r.log.Record(ctx, d); err != nil {
		return fmt.Errorf("capture: record relation-discarded decision for unit %q: %w", u.ID, err)
	}
	return nil
}

// recordRelationPersistedDecision writes decision_log's account of an
// ordinary (non-duplicate) relation the judge persisted, uncertain or
// asserted (spec R5.4): rel is already written to r.rels by the caller, and
// this row names the type and confidence it was persisted with.
func (r captureRunner) recordRelationPersistedDecision(ctx context.Context, u unit.Unit, rel ports.Relation, now time.Time) error {
	rationale := fmt.Sprintf("relation %q to unit %q persisted with %.2f confidence", rel.Type, rel.ToUnitID, rel.Confidence)
	contextJSON, err := json.Marshal(struct {
		FromUnitID string  `json:"from_unit_id"`
		ToUnitID   string  `json:"to_unit_id"`
		Type       string  `json:"type"`
		Confidence float64 `json:"confidence"`
	}{FromUnitID: rel.FromUnitID, ToUnitID: rel.ToUnitID, Type: rel.Type, Confidence: rel.Confidence})
	if err != nil {
		return fmt.Errorf("capture: encode relation-persisted decision context: %w", err)
	}

	d := ports.Decision{
		ID:         r.ids.New(),
		Action:     ports.ActionRelationPersisted,
		Rationale:  rationale,
		Context:    contextJSON,
		OccurredAt: now,
	}
	if err := r.log.Record(ctx, d); err != nil {
		return fmt.Errorf("capture: record relation-persisted decision for unit %q: %w", u.ID, err)
	}
	return nil
}

// recordRelationDuplicateDecision writes decision_log's account of a
// duplicate outcome — design D7's own resolution: rel (already a
// "duplicate"-typed relation, written by the caller) says plainly that the
// duplicate was recorded, not merged. Neither superseding the new unit
// (unit.StatusSuperseded's one documented meaning is "replaced insight",
// doc 02 §1) nor reviving the existing one (a weight write on the decay
// path, an explicit M2 non-goal) is in this PR's scope — design D7 names
// both as out of bounds and records the gap as an accepted risk with an M2
// owner, not something this decision_log row pretends to resolve.
func (r captureRunner) recordRelationDuplicateDecision(ctx context.Context, u unit.Unit, rel ports.Relation, now time.Time) error {
	rationale := fmt.Sprintf("unit %q judged a duplicate of existing unit %q — recorded as a duplicate relation, not merged", u.ID, rel.ToUnitID)
	contextJSON, err := json.Marshal(struct {
		FromUnitID string  `json:"from_unit_id"`
		ToUnitID   string  `json:"to_unit_id"`
		Confidence float64 `json:"confidence"`
	}{FromUnitID: rel.FromUnitID, ToUnitID: rel.ToUnitID, Confidence: rel.Confidence})
	if err != nil {
		return fmt.Errorf("capture: encode relation-duplicate decision context: %w", err)
	}

	d := ports.Decision{
		ID:         r.ids.New(),
		Action:     ports.ActionRelationDuplicateRecorded,
		Rationale:  rationale,
		Context:    contextJSON,
		OccurredAt: now,
	}
	if err := r.log.Record(ctx, d); err != nil {
		return fmt.Errorf("capture: record relation-duplicate decision for unit %q: %w", u.ID, err)
	}
	return nil
}
