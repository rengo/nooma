package brain

import (
	"time"

	"github.com/rengo/nooma/internal/core/correction"
	"github.com/rengo/nooma/internal/core/prospection"
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
	OutcomeStored CaptureOutcome = "stored" // a unit was persisted
	OutcomeArmed  CaptureOutcome = "armed"  // a timer or trigger was armed — never a unit (I04)
	// OutcomeArmRefused: the capture asked for a nudge and could not get
	// one — an undated timer, or a date already behind us.
	OutcomeArmRefused CaptureOutcome = "arm_refused"
	OutcomeDiscarded  CaptureOutcome = "discarded" // chitchat / out_of_scope
	OutcomeRecalled   CaptureOutcome = "recalled"  // a recall, answered
	OutcomeCorrected  CaptureOutcome = "corrected" // a correction, applied
	OutcomeAsked      CaptureOutcome = "asked"     // a correction whose referent or plan was ambiguous
)

// AllCaptureOutcomes returns a fresh slice holding every CaptureOutcome, in
// the order the constants above declare them — a function, not an exported
// var, for the same mutability reason ports.AllDecisionActions is one.
func AllCaptureOutcomes() []CaptureOutcome {
	return []CaptureOutcome{
		OutcomeStored, OutcomeArmed, OutcomeArmRefused, OutcomeDiscarded,
		OutcomeRecalled, OutcomeCorrected, OutcomeAsked,
	}
}

// CaptureResult is what CaptureService.Capture returns — a tagged union
// over Outcome (design D8): only the fields naming that outcome are ever
// populated; every other field stays its zero value.
type CaptureResult struct {
	// Outcome names which of the seven ways this capture ended — the one
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

	// Armed names what this capture armed. Set only for
	// Outcome == OutcomeArmed.
	Armed *Armed

	// ArmRefused names why a capture that asked for a nudge did not get
	// one. Set only for Outcome == OutcomeArmRefused.
	ArmRefused *ArmRefused

	// Recalled holds the units RecallService.ForText found for a
	// `recall`-classified capture (spec R2.3, design D9), in fused order.
	// Set only for Outcome == OutcomeRecalled; never persists a unit.
	Recalled []unit.Unit

	// Correction names how a correction resolved, or why it could not. Set
	// only for Outcome == OutcomeCorrected or Outcome == OutcomeAsked.
	Correction *Correction
}

// Armed is what CaptureResult carries when a capture armed something
// instead of persisting a unit — a timer or a trigger, never both, and
// never a unit (I04, doc 02 §8's own bold sentence).
//
// It replaces the Deferred value this capture path used to return, and the
// difference is the whole of m3b: the refusal said "not yet", this says
// what was scheduled and when.
type Armed struct {
	// What names which of prospection.Arm's armaments was created — a
	// typed value, not a bare string, for the reason every closed
	// vocabulary in this codebase is typed.
	What prospection.Armament
	// ID is the created timers or triggers row's own id, so a caller can
	// name the thing it just scheduled.
	ID string
	// FireAt is when it will fire — the Plan's own instant, not a
	// re-derivation.
	FireAt time.Time
	// About is what it is about: the appointment, the deadline, the next
	// birthday. Carried separately because a reply that names FireAt
	// answers a question nobody asked — the two differ whenever a lead
	// time or a clamp moved the firing, which for a dated event is most
	// of the time.
	About time.Time
	// Immediate says the nudge goes out now rather than at some remove
	// from About — the plan's own fact, not a re-derivation from the two
	// instants above, which would read a gap of hours and describe a
	// reminder arriving "the day before" when it arrives at once.
	Immediate bool
}

// ArmRefused is what CaptureResult carries when a capture classified as
// something armable but could not be armed — an undated timer, or one
// whose instant is already behind us.
//
// It is a distinct outcome rather than a discard because the two are
// different facts: discarding says "nothing worth keeping", and this says
// "you asked for a reminder and I could not set it". Reusing OutcomeStored
// is not available either — I04 forbids the unit — so without this the
// capture has nothing truthful left to answer, which is how it came to
// fail outright instead.
type ArmRefused struct {
	// Why is prospection's own refusal vocabulary, typed rather than a
	// bare string.
	Why prospection.Refusal
	// Message is the caller-visible sentence, in plain words. A caller
	// renders this directly; it is never a technical error message.
	Message string
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
