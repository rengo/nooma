package relation

// Verdict is the outcome of comparing a candidate relation's confidence
// against its type's resolved thresholds — design D7, doc 02 §4.
type Verdict int

const (
	// Discard — confidence < Persist. The candidate is not stored at all
	// (I08).
	Discard Verdict = iota
	// Uncertain — Persist <= confidence < Surface. Stored, and asked about
	// in the digest (I09's storage half).
	Uncertain
	// Asserted — confidence >= Surface. Stored and asserted without asking.
	Asserted
)

// Decide applies doc 02 §4's two thresholds to a judge-reported confidence
// and returns which of the three bands it falls in.
//
// Boundary semantics (design D7): doc 02 §4's prose reads ambiguously at
// confidence == Surface ("above this, it is asserted without asking" versus
// the same paragraph's own band notation [persist, surface), which
// partitions the line with no gap). The band notation wins: confidence ==
// Surface is Asserted, not Uncertain. By the same partition, confidence ==
// Persist is Uncertain, not Discard. Both boundaries are therefore
// inclusive toward the higher band.
func Decide(confidence float64, t Thresholds) Verdict {
	switch {
	case confidence < t.Persist:
		return Discard
	case confidence < t.Surface:
		return Uncertain
	default:
		return Asserted
	}
}
