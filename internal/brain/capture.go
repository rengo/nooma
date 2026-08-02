package brain

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/rengo/nooma/internal/core/classify"
	"github.com/rengo/nooma/internal/core/unit"
	"github.com/rengo/nooma/internal/ports"
)

// taskCaptureProcessing is the LLMRequest.Task value capture sends —
// internal/config.DocumentedTaskNames' own "capture_processing" entry
// (internal/config/validate.go). It is a literal here, not an imported
// constant: internal/brain may import internal/core and internal/ports and
// nothing else (docs/06-harness.md §1), and internal/config is neither.
const taskCaptureProcessing = "capture_processing"

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
// design D4's own captureRunner field order (design.md:346-357), restricted
// to the fields this slice populates.
func NewCaptureService(clock ports.Clock, ids ports.IDGen, units ports.UnitRepo, embeds ports.EmbeddingRepo, log ports.DecisionLog, llm ports.LLMProvider, embed ports.EmbeddingProvider) *CaptureService {
	return &CaptureService{
		clock: clock,
		run: captureRunner{
			ids:    ids,
			units:  units,
			embeds: embeds,
			log:    log,
			llm:    llm,
			embed:  embed,
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
// This is PR 10b's second slice: the ordinary path spec R4.2 requires — an
// LLM call, a decode, a persisted unit, one decision_log row — plus spec
// R4.3's embedding step (design D8). lex, rels, judge and index (design
// D4's full struct) are the third slice's fields; they are not declared
// here at all, rather than declared and left unused, because a field this
// slice cannot populate is a promise the code does not keep.
type captureRunner struct {
	ids    ports.IDGen
	units  ports.UnitRepo
	embeds ports.EmbeddingRepo
	log    ports.DecisionLog
	llm    ports.LLMProvider // capture_processing
	embed  ports.EmbeddingProvider
}

// at runs one capture given the instant CaptureService.Capture already
// read. It is design.md's pipeline diagram, restricted to this slice's
// scope (spec R4.2, R4.3): classify.BuildPrompt -> llm.Complete ->
// classify.Decode -> classify.ToUnit -> units.Create, one decision_log row
// naming the classification, then embed.Embed -> embeds.Put (design D8's
// persist-before-embed ordering).
//
// What it does not yet do, on purpose: run hybrid recall for dedup/relation
// candidates (10b.5), or turn a classify.ToUnit error into a caller-visible
// refusal instead of a bare error (10b.6's timer/ambiguous-person halves,
// out of this slice's scope — design's own package table assigns
// CaptureResult.Deferred to PR 10c). A classification that maps to no
// unit.Type, or that lost its content, is propagated as a plain Go error
// today; nothing about that behavior is asserted by this slice's tests, and
// a later slice is expected to replace it with the distinguishable result
// Q3a's refusal requires.
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
		return CaptureResult{}, fmt.Errorf("capture: decode classification: %w", err)
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

	if err := r.recordClassifyDecision(ctx, c, u, now); err != nil {
		return CaptureResult{}, err
	}

	embedded, err := r.embedAndStore(ctx, u, now)
	if err != nil {
		return CaptureResult{}, err
	}

	return CaptureResult{UnitID: u.ID, Embedded: embedded}, nil
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
// It returns (true, nil) once the embedding is written, or (false, nil)
// once a failure has been recorded to decision_log as
// ports.ActionCaptureEmbeddingFailed — the caller-visible degradation
// design D8 names rather than hides. The only way this returns a non-nil
// error is recordEmbeddingFailedDecision's own log write failing, which is
// not a case design D8 discusses; it is handled the same way
// recordClassifyDecision already handles its own log-write failure, by
// propagating it.
func (r captureRunner) embedAndStore(ctx context.Context, u unit.Unit, now time.Time) (bool, error) {
	ev, err := r.embed.Embed(ctx, ports.EmbedRequest{Text: u.Content})
	if err != nil {
		return false, r.recordEmbeddingFailedDecision(ctx, u, now, err)
	}

	if err := r.embeds.Put(ctx, ports.Embedding{UnitID: u.ID, Model: ev.Model, Vector: ev.Vector, At: now}); err != nil {
		return false, r.recordEmbeddingFailedDecision(ctx, u, now, err)
	}

	return true, nil
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
