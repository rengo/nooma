package relation

import (
	"encoding/json"
	"errors"

	"github.com/rengo/nooma/internal/core/classify"
)

// ErrNoFieldsSalvaged reports that the judge's response yielded no completed
// members at all — classify.ErrNoFieldsSalvaged's own reasoning (design D1),
// restated for this wire contract rather than reused directly: a caller
// catching one must not silently also catch the other's, since they name
// failures of two different provider calls.
var ErrNoFieldsSalvaged = errors.New("relation: no fields salvaged from the judge's response")

// Outcome is what the relation judge decided about a new unit relative to
// the recall candidates it was shown — doc 02 §4 step 3 ("new | duplicate |
// related"), design D7.
type Outcome string

const (
	OutcomeNew       Outcome = "new"
	OutcomeDuplicate Outcome = "duplicate"
	OutcomeRelated   Outcome = "related"
)

// allOutcomes is Outcome's closed vocabulary. A function, not an exported
// var — classify.AllKinds' own reasoning (design D1): an exported slice is
// mutable by any importer, and a mutated result could defeat the enum check
// below.
func allOutcomes() []Outcome {
	return []Outcome{OutcomeNew, OutcomeDuplicate, OutcomeRelated}
}

// Judgment is the relation judge's tolerant-decoded response — design D7.
//
// Every field but Degradations is a pointer, for classify.Classification's
// own stated reason (design D1): with a plain float64, a genuine 0 and an
// absent field decode identically, and I14's tolerant-decode contract needs
// to tell them apart. TargetUnitID, Type, Strength and Confidence are
// meaningful only when Outcome is duplicate or related — doc 02 §4: "if
// related, with what strength/confidence" — so their absence for a "new"
// outcome is the ordinary case, not a degradation; DecodeJudgment does not
// report it (see judgmentFieldSpecs' required column below).
type Judgment struct {
	Outcome      *Outcome
	TargetUnitID *string
	Type         *string
	Strength     *float64
	Confidence   *float64

	// Degradations reuses classify.Degradation and its Reason vocabulary
	// verbatim, rather than a second copy of the same five values — design
	// D7's own reasoning for reusing classify.Salvage applies here too: the
	// judge's response is JSON from the same class of provider, with the
	// same failure modes.
	Degradations []classify.Degradation
}

// judgmentFieldSpec is one wire field of the judge's response — the same
// table shape classify.fieldSpec uses (design D11 point 1: one row per
// field, one loop in the decoder, so statement count does not grow with the
// number of malformation shapes), sized for Judgment's five fields rather
// than Classification's thirteen.
//
// required marks the one field whose absence is itself worth a degradation:
// "outcome" is the judge's one always-expected answer, per doc 02 §4 step 3
// — every capture with candidates asks it. The other four ride alongside
// only when Outcome is duplicate or related, so their absence is ordinary,
// not a loss. This split is this PR's own open decision — design D7 does
// not state it either way.
type judgmentFieldSpec struct {
	name     string
	required bool
	assign   func(raw json.RawMessage, j *Judgment) classify.Reason
}

func judgmentFieldSpecs() []judgmentFieldSpec {
	return []judgmentFieldSpec{
		{"outcome", true, assignOutcome},
		{"target_unit_id", false, assignJudgmentString(func(j *Judgment, v *string) { j.TargetUnitID = v })},
		{"type", false, assignJudgmentString(func(j *Judgment, v *string) { j.Type = v })},
		{"strength", false, assignJudgmentFloat(func(j *Judgment, v *float64) { j.Strength = v })},
		{"confidence", false, assignJudgmentFloat(func(j *Judgment, v *float64) { j.Confidence = v })},
	}
}

// DecodeJudgment turns a relation judge's raw response into a Judgment —
// doc 02 §4 step 3, design D7. It reuses classify.Salvage — I14's own
// truncation-tolerant mechanism, design D7's "one project, two tolerant
// decoders that can disagree" reasoning against a third, jsonsalvage-shaped
// core package for one function it would otherwise duplicate. A malformed
// or truncated field degrades to absent, and the rest of the judgment
// survives. The only error is ErrNoFieldsSalvaged.
func DecodeJudgment(raw string) (Judgment, error) {
	fields, truncated := classify.Salvage([]byte(raw))
	if len(fields) == 0 {
		return Judgment{}, ErrNoFieldsSalvaged
	}

	var j Judgment
	for _, spec := range judgmentFieldSpecs() {
		value, present := fields[spec.name]
		if !present {
			if spec.required {
				j.Degradations = append(j.Degradations, classify.Degradation{Field: spec.name, Reason: missingReason(truncated)})
			}
			continue
		}
		if r := spec.assign(value, &j); r != "" {
			j.Degradations = append(j.Degradations, classify.Degradation{Field: spec.name, Reason: r})
		}
	}
	return j, nil
}

// missingReason distinguishes the two ways the required field can fail to
// arrive — classify.Decode's own missingReason, restated because that one
// is unexported to its own package.
func missingReason(truncated bool) classify.Reason {
	if truncated {
		return classify.ReasonTruncated
	}
	return classify.ReasonAbsent
}

// assignOutcome matches the raw "outcome" value against Outcome's closed
// vocabulary — classify.decodeEnum's own shape, restated because that one is
// unexported and generic over Classification's own fields, not Judgment's.
func assignOutcome(raw json.RawMessage, j *Judgment) classify.Reason {
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return classify.ReasonWrongType
	}
	for _, o := range allOutcomes() {
		if string(o) == s {
			v := o
			j.Outcome = &v
			return ""
		}
	}
	return classify.ReasonUnknownEnum
}

func assignJudgmentString(set func(*Judgment, *string)) func(json.RawMessage, *Judgment) classify.Reason {
	return func(raw json.RawMessage, j *Judgment) classify.Reason {
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return classify.ReasonWrongType
		}
		set(j, &s)
		return ""
	}
}

func assignJudgmentFloat(set func(*Judgment, *float64)) func(json.RawMessage, *Judgment) classify.Reason {
	return func(raw json.RawMessage, j *Judgment) classify.Reason {
		var f float64
		if err := json.Unmarshal(raw, &f); err != nil {
			return classify.ReasonWrongType
		}
		set(j, &f)
		return ""
	}
}
