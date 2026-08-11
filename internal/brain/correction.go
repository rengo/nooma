package brain

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/rengo/nooma/internal/core/classify"
	"github.com/rengo/nooma/internal/core/correction"
	"github.com/rengo/nooma/internal/core/unit"
	"github.com/rengo/nooma/internal/ports"
)

// correctionRunner is the clockless worker owning the correction path
// (design D7) — no CorrectionService, no ports.Clock of its own; it
// receives now from captureRunner.at, which received it from the single
// CaptureService.Capture clock read (design D4 Layer 1, unaffected by this
// slice). 12f-i implemented applyWithPreImage/dispatchEdits (D5 Layers 1
// and 3) with only units/log/ids; 12f-ii added signals where its own
// signals.Record call is written (recordCorrectionSignal). This PR (12g)
// adds recall — design D7's full five-field struct, converged — where at's
// own referent-resolution routing first reads it (Conflicts §C7's own
// incremental-field precedent, matching captureRunner's own "rels and judge
// are this PR's own two additions").
type correctionRunner struct {
	units   ports.UnitRepo
	log     ports.DecisionLog
	signals ports.SignalRepo
	ids     ports.IDGen
	recall  *RecallService
}

// referentSource records how applyWithPreImage's caller resolved target's
// id, for the pre-image's own "referent" object (design D5's JSON shape).
// Source is "recall" or "explicit"; the three score fields are populated
// only on the recall path and left nil on the explicit path — a zero score
// would be a claim about a ratio nobody computed, and an absent JSON key is
// the truth (design D5).
type referentSource struct {
	Source        string
	Score         *float64
	RunnerUpScore *float64
	Margin        *float64
}

// at is design D7's own orchestration entry, and the only caller
// applyWithPreImage has left to gain: captureRunner.at's own Kind ==
// classify.KindCorrection fork (spec R1.1). It resolves a referent — an
// explicit in.ReferentID first (R1.5), the chat-path hybrid-recall gate
// otherwise (R1.6) — plans the edit (correction.PlanEdit, R1.8), and
// applies it (applyWithPreImage, R1.9/R1.10). Either resolution step can
// ask instead of deciding: no unit is touched, one correction.ambiguous row
// is written, and the returned *Correction carries Ambiguous == true for
// the caller to map onto OutcomeAsked.
func (r correctionRunner) at(ctx context.Context, in CaptureInput, c classify.Classification, now time.Time) (*Correction, error) {
	target, ref, err := r.resolveReferent(ctx, in, now)
	if err != nil {
		return nil, err
	}
	if target == nil {
		return &Correction{Ambiguous: true}, nil
	}

	plan, ok := correction.PlanEdit(c)
	if !ok {
		if err := r.recordAmbiguousDecision(ctx, now, struct {
			Reason string `json:"reason"`
			UnitID string `json:"unit_id"`
		}{Reason: "plan_ambiguous", UnitID: target.ID}); err != nil {
			return nil, err
		}
		return &Correction{UnitID: target.ID, Ambiguous: true}, nil
	}

	if err := r.applyWithPreImage(ctx, *target, plan, ref, now); err != nil {
		return nil, err
	}

	fields := make([]correction.Field, len(plan))
	for i, e := range plan {
		fields[i] = e.Field()
	}
	return &Correction{UnitID: target.ID, Fields: fields}, nil
}

// resolveReferent is R1.5/R1.6's own fork: in.ReferentID wins wherever it
// is non-empty, and recall does not run at all when it does (design D7's
// "an instrumented index that fails the test if queried proves it" —
// r.recall.ScoredFor is simply never called on this branch). An unknown
// explicit id is an error, never a silent fallback to recall (R1.5's own
// MUST — a fallback would defeat the caller's explicit intent).
//
// Otherwise it runs r.recall.ScoredFor(ctx, in.Text) — the raw text, D9's
// forced argument — and gates the result through correction.Referent at
// ReferentMargin. ScoredFor's own join already resolves scores against
// only the LIVE candidates (LiveByIDs, before the ratio is ever computed —
// design D2/D9, closing 12b's own ordering debt): the candidate slice this
// method hands to correction.Referent already excludes every non-live
// scorer, so the ratio Referent computes is the ratio over the survivors,
// never recomputed by this method itself. A nil target with a nil error
// means the gate asked; the caller records that outcome.
func (r correctionRunner) resolveReferent(ctx context.Context, in CaptureInput, now time.Time) (*unit.Unit, referentSource, error) {
	if in.ReferentID != "" {
		u, err := r.units.ByID(ctx, in.ReferentID)
		if err != nil {
			return nil, referentSource{}, fmt.Errorf("correction: resolve explicit referent %q: %w", in.ReferentID, err)
		}
		return &u, referentSource{Source: "explicit"}, nil
	}

	scored, _, err := r.recall.ScoredFor(ctx, in.Text)
	if err != nil {
		return nil, referentSource{}, fmt.Errorf("correction: resolve referent: %w", err)
	}
	// scoredToFused (consolidate.go) is this mapping's one implementation,
	// shared rather than copied — connect's own candidate search (design
	// §7.1, m2c PR 9) is its second caller.
	cands, byID := scoredToFused(scored)

	id, ok := correction.Referent(cands, correction.ReferentMargin)
	if !ok {
		type scoredCand struct {
			ID    string  `json:"id"`
			Score float64 `json:"score"`
		}
		ctxCands := make([]scoredCand, len(cands))
		for i, c := range cands {
			ctxCands[i] = scoredCand{ID: c.ID, Score: c.Score}
		}
		if err := r.recordAmbiguousDecision(ctx, now, struct {
			Reason     string       `json:"reason"`
			Candidates []scoredCand `json:"candidates"`
		}{Reason: "referent_ambiguous", Candidates: ctxCands}); err != nil {
			return nil, referentSource{}, err
		}
		return nil, referentSource{}, nil
	}

	target := byID[id]
	margin := correction.ReferentMargin
	score := cands[0].Score
	ref := referentSource{Source: "recall", Score: &score, Margin: &margin}
	if len(cands) > 1 {
		runnerUp := cands[1].Score
		ref.RunnerUpScore = &runnerUp
	}
	return &target, ref, nil
}

// recordAmbiguousDecision writes design D2/D3's ask row
// (ports.ActionCorrectionAmbiguous): a decision with no vault effect — the
// referent gate or the edit plan asked instead of picking. Shares
// captureRunner.recordOrphanDecision's shape (a rationale plus a context
// value) for the same reason: neither call site has a unit or a full
// classification to embed, only a reason and, on the referent path, the
// candidates that made the gate ask.
func (r correctionRunner) recordAmbiguousDecision(ctx context.Context, now time.Time, ctxValue any) error {
	contextJSON, err := json.Marshal(ctxValue)
	if err != nil {
		return fmt.Errorf("correction: encode ambiguous decision context: %w", err)
	}

	d := ports.Decision{
		ID:         r.ids.New(),
		Action:     ports.ActionCorrectionAmbiguous,
		Rationale:  "correction could not resolve unambiguously — no unit was edited",
		Context:    contextJSON,
		OccurredAt: now,
	}
	if err := r.log.Record(ctx, d); err != nil {
		return fmt.Errorf("correction: record ambiguous decision: %w", err)
	}
	return nil
}

// applyWithPreImage is the ONLY path in this package to a ports.UnitRepo
// Update* method — design D5 Layer 1, ADR-0016's ordering made structural.
// The pre-image row is written first; if that write fails, no Update* call
// runs at all (ADR-0016: "The row is written first. If it fails, the UPDATE
// does not happen"). Layer 2's AST guard (test/conformance) mechanizes both
// halves of this door: no Update* call exists outside dispatchEdits, and
// recordPreImage's own call appears strictly before dispatchEdits's.
//
// One pre-image row covers the whole plan: under C6's ruling plan always
// holds at most one Edit, so the row names one field, but the shape does
// not change if a later milestone widens plan (design D5).
//
// Once every edit has landed, applyWithPreImage writes the R1.10/design D6
// learning signal — never before, and never when either the pre-image
// write or an edit failed (D6: "a signal for an edit that failed would
// teach a future learning pass from an event that did not occur").
func (r correctionRunner) applyWithPreImage(ctx context.Context, target unit.Unit, plan []correction.Edit, ref referentSource, now time.Time) error {
	decisionID, err := r.recordPreImage(ctx, target, plan, ref, now)
	if err != nil {
		return err // ADR-0016: the edit does not happen
	}
	if err := r.dispatchEdits(ctx, target.ID, plan, now); err != nil {
		return err // D6: no signal for a correction that did not land
	}
	return r.recordCorrectionSignal(ctx, target.ID, plan, decisionID, now)
}

// recordPreImage writes ADR-0016's pre-image: one decision_log row
// (ports.ActionCorrectionApplied) whose context carries the target unit's
// id, the field(s) about to change, their previous and next values (keyed
// by column name, per design D5's JSON shape), and how the referent was
// resolved. It returns the written row's id, which recordCorrectionSignal
// links back to via its own context.decision_id (design D6).
func (r correctionRunner) recordPreImage(ctx context.Context, target unit.Unit, plan []correction.Edit, ref referentSource, now time.Time) (string, error) {
	fields := make([]string, 0, len(plan))
	previous := make(map[string]any, len(plan))
	next := make(map[string]any, len(plan))

	for _, e := range plan {
		f := string(e.Field())
		fields = append(fields, f)
		switch e.Field() {
		case correction.FieldContent:
			previous[f] = target.Content
			v, _ := e.Content()
			next[f] = v
		case correction.FieldEventAt:
			previous[f] = target.EventAt
			v, _ := e.EventAt()
			next[f] = v
		case correction.FieldDueAt:
			previous[f] = target.DueAt
			v, _ := e.DueAt()
			next[f] = v
		}
	}

	contextJSON, err := json.Marshal(struct {
		UnitID   string         `json:"unit_id"`
		Fields   []string       `json:"fields"`
		Previous map[string]any `json:"previous"`
		Next     map[string]any `json:"next"`
		Referent struct {
			Source        string   `json:"source"`
			Score         *float64 `json:"score,omitempty"`
			RunnerUpScore *float64 `json:"runner_up_score,omitempty"`
			Margin        *float64 `json:"margin,omitempty"`
		} `json:"referent"`
	}{
		UnitID:   target.ID,
		Fields:   fields,
		Previous: previous,
		Next:     next,
		Referent: struct {
			Source        string   `json:"source"`
			Score         *float64 `json:"score,omitempty"`
			RunnerUpScore *float64 `json:"runner_up_score,omitempty"`
			Margin        *float64 `json:"margin,omitempty"`
		}{Source: ref.Source, Score: ref.Score, RunnerUpScore: ref.RunnerUpScore, Margin: ref.Margin},
	})
	if err != nil {
		return "", fmt.Errorf("correction: encode pre-image context: %w", err)
	}

	d := ports.Decision{
		ID:         r.ids.New(),
		Action:     ports.ActionCorrectionApplied,
		Rationale:  fmt.Sprintf("correction about to write %v to unit %q; previous values recorded before the edit", fields, target.ID),
		Context:    contextJSON,
		OccurredAt: now,
	}
	if err := r.log.Record(ctx, d); err != nil {
		return "", fmt.Errorf("correction: record pre-image for unit %q: %w", target.ID, err)
	}
	return d.ID, nil
}

// dispatchEdits applies plan's edits to id, one ports.UnitRepo Update* call
// per plan entry — a total switch over correction.Field, each arm reading
// the accessor named after the column its method writes (design D3's
// crossed-wiring argument). This is the ONLY function in this package
// permitted to call UpdateContent/UpdateEventAt/UpdateDueAt — design D5
// Layer 2's AST guard enforces that no other call site exists anywhere
// under internal/ outside internal/store/**/internal/ports/**.
func (r correctionRunner) dispatchEdits(ctx context.Context, id string, plan []correction.Edit, now time.Time) error {
	for _, e := range plan {
		switch e.Field() {
		case correction.FieldContent:
			v, _ := e.Content()
			if err := r.units.UpdateContent(ctx, id, v, now); err != nil {
				return fmt.Errorf("correction: update content for unit %q: %w", id, err)
			}
		case correction.FieldEventAt:
			v, _ := e.EventAt()
			if err := r.units.UpdateEventAt(ctx, id, v, now); err != nil {
				return fmt.Errorf("correction: update event_at for unit %q: %w", id, err)
			}
		case correction.FieldDueAt:
			v, _ := e.DueAt()
			if err := r.units.UpdateDueAt(ctx, id, v, now); err != nil {
				return fmt.Errorf("correction: update due_at for unit %q: %w", id, err)
			}
		default:
			return fmt.Errorf("correction: unknown edit field %q for unit %q", e.Field(), id)
		}
	}
	return nil
}

// recordCorrectionSignal writes spec R1.10's learning_signals row — design
// D6's fields, decided rather than defaulted: Type = correction,
// TargetKind = unit, TargetID = id, Valence = negative (doc 02 §4 calls an
// explicit user correction "the strongest learning signal", the opposite of
// the neutral "no information" a signal's zero value would otherwise read
// as), DecisionAction left nil rather than guessed — decision_log carries
// no unit index, so which prior decision this corrects is undiscoverable
// here, and Magnitude left nil since no document defines a scale for it
// (M5's own column). Context carries {unit_id, fields, decision_id}, where
// decision_id names the accompanying correction.applied row recordPreImage
// already wrote — the real link M5 can follow.
//
// Only applyWithPreImage calls this, and only after dispatchEdits has
// already succeeded: a signal for a correction that did not land would
// teach a future learning pass from an event that did not occur (D6).
func (r correctionRunner) recordCorrectionSignal(ctx context.Context, id string, plan []correction.Edit, decisionID string, now time.Time) error {
	fields := make([]string, 0, len(plan))
	for _, e := range plan {
		fields = append(fields, string(e.Field()))
	}

	contextJSON, err := json.Marshal(struct {
		UnitID     string   `json:"unit_id"`
		Fields     []string `json:"fields"`
		DecisionID string   `json:"decision_id"`
	}{UnitID: id, Fields: fields, DecisionID: decisionID})
	if err != nil {
		return fmt.Errorf("correction: encode signal context for unit %q: %w", id, err)
	}

	targetKind := ports.TargetKindUnit
	s := ports.Signal{
		ID:         r.ids.New(),
		Type:       ports.SignalCorrection,
		Valence:    ports.ValenceNegative,
		TargetKind: &targetKind,
		TargetID:   &id,
		Context:    contextJSON,
		OccurredAt: now,
	}
	if err := r.signals.Record(ctx, s); err != nil {
		return fmt.Errorf("correction: record signal for unit %q: %w", id, err)
	}
	return nil
}
