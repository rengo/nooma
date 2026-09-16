package relation

// ConfirmedConfidence is doc 02 §4's GREATEST(current, confirmed_floor) with
// owner ruling Q1's answer substituted: the floor is the type's own
// Surface threshold (ADR-0027's "Related decision" section). A confirmed
// relation lands at or above the edge of the band it was in, and Decide's
// boundaries are inclusive toward the higher band (verdict.go:20-27), so it
// leaves the band by construction.
//
// Written as !(current > t.Surface) rather than math.Max, and that is a
// decision: relations.confidence is a REAL column with no CHECK, so a NaN
// can be read back from it. math.Max would propagate the NaN into the row,
// and Decide would then read NaN as Asserted by ACCIDENT — both comparisons
// fail, so it falls through to the default arm. This form lands NaN on the
// floor, where the row is honest and the property below holds for a reason
// rather than by luck.
func ConfirmedConfidence(current float64, t Thresholds) float64 {
	if !(current > t.Surface) {
		return t.Surface
	}
	return current
}
