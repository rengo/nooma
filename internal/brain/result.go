package brain

import (
	"github.com/rengo/nooma/internal/core/correction"
	"github.com/rengo/nooma/internal/core/unit"
)

// CaptureInput is what a caller hands to CaptureService.Capture — the "in"
// design D4's pipeline diagram threads through every step.
//
// Text and Channel are both the caller's facts, not core's: BuildPrompt
// renders Text verbatim (design D4), and Channel becomes classify.ToUnit's
// source parameter (conflict C10.1) — units.source is NOT NULL and its
// column DEFAULT never fires, because the repository always passes the
// field explicitly. Hardcoding a channel inside core would be silently
// wrong the day a capture arrives from somewhere other than chat; naming it
// here, at the one place that actually knows it, is what keeps that from
// happening.
type CaptureInput struct {
	// Text is the raw message text to classify.
	Text string
	// Channel is where this capture came from (e.g. "chat", "telegram"),
	// and becomes the persisted unit's Source.
	Channel string
	// ReferentID is an optional explicit target-unit id, meaningful only
	// when the classification resolves to classify.KindCorrection (spec
	// R1.5, design D7). When non-empty it wins over chat-path referent
	// resolution outright: recall does not run at all, and an id naming no
	// existing unit fails the correction rather than falling back to
	// recall.
	ReferentID string
}

// CaptureOutcome is the closed vocabulary of every way a capture can end —
// design D8, replacing the prior Stored bool (C7): keeping both would give
// one fact two sources that could disagree. AllCaptureOutcomes lets a
// caller (13b's HTTP status mapping) build a total switch that fails loudly
// the day a member is added without a mapping.
type CaptureOutcome string

const (
	OutcomeStored    CaptureOutcome = "stored"    // a unit was persisted
	OutcomeDiscarded CaptureOutcome = "discarded" // chitchat / out_of_scope
	OutcomeRecalled  CaptureOutcome = "recalled"  // a recall, answered
	OutcomeCorrected CaptureOutcome = "corrected" // a correction, applied
	OutcomeAsked     CaptureOutcome = "asked"     // a correction whose referent or plan was ambiguous
)

// AllCaptureOutcomes returns a fresh slice holding every CaptureOutcome, in
// the order the constants above declare them — a function, not an exported
// var, for the same mutability reason ports.AllDecisionActions is one.
func AllCaptureOutcomes() []CaptureOutcome {
	return []CaptureOutcome{
		OutcomeStored, OutcomeDiscarded,
		OutcomeRecalled, OutcomeCorrected, OutcomeAsked,
	}
}

// CaptureResult is what CaptureService.Capture returns — a tagged union
// over Outcome (design D8): only the fields naming that outcome are ever
// populated; every other field stays its zero value.
type CaptureResult struct {
	// Outcome names which of the five ways this capture ended — the one
	// field every caller switches on.
	Outcome CaptureOutcome

	// UnitID is the ID of the unit this capture persisted. Set only for
	// Outcome == OutcomeStored.
	UnitID string
	// Embedded reports whether this capture's embedding was written.
	// False means the unit is persisted and lexically findable but not yet
	// semantically searchable — design D8's accepted, named gap: a local
	// embedding-provider or store outage degrades the index, it does not
	// refuse the capture (doc 02 §5's product rule). Set only for
	// Outcome == OutcomeStored.
	Embedded bool
	// Candidates holds the ids RecallService found for this capture's own
	// unit, in the RRF-fused, I02-filtered order design D5 produces — the
	// just-persisted unit's own id is never among them (spec R4.4's own
	// MUST). Set only for Outcome == OutcomeStored; empty, never nil, when
	// embedding did not happen or recall found nothing.
	Candidates []string

	// Recalled holds the units RecallService.ForText found for a
	// `recall`-classified capture (spec R2.3, design D9), in fused order.
	// Set only for Outcome == OutcomeRecalled; never persists a unit.
	Recalled []unit.Unit

	// Correction names how a correction resolved, or why it could not. Set
	// only for Outcome == OutcomeCorrected or Outcome == OutcomeAsked.
	Correction *Correction
}

// Correction is what CaptureResult carries for the Corrected and Asked
// outcomes (design D8) — capture's own account of how a correction
// resolved, or why it could not.
type Correction struct {
	// UnitID is the referent unit's id. Empty when Outcome is Asked because
	// the referent itself could not be resolved (R1.6); set when Outcome is
	// Asked because a referent resolved but its edit plan was ambiguous
	// (R1.8); always set when Outcome is Corrected.
	UnitID string
	// Fields names the columns PlanEdit wrote, in order. Empty for Asked.
	Fields []correction.Field
	// Ambiguous is true for the ask-shaped outcome — captureRunner's own
	// Kind == correction fork reads this to choose OutcomeAsked over
	// OutcomeCorrected, rather than re-deriving it.
	Ambiguous bool
}
