package focus

// DefaultHysteresisMargin is doc 02 §13's `hysteresis_margin (focus,
// relative)` default (spec R4.4, design D4's surviving half, ruling 5).
// Pinned to migration 0002's config.hysteresis_margin column DEFAULT by
// test/conformance/focus_margin_ddl_test.go — PR 4b's own file, since this
// PR declares the constant and its meaning, not the DDL pin itself.
const DefaultHysteresisMargin = 0.05

// ResolveMargin is relation.Resolve's shape verbatim
// (internal/core/relation/thresholds.go:26-38): config.hysteresis_margin's
// singleton row has never existed in any vault (owner ruling round 2 #2,
// proposal R1), so every read of it today returns nothing, and a nil
// configured means exactly that absence — "no row has ever been written" —
// never a deliberately-configured zero margin, the same reading
// relation.Resolve already gives its own two thresholds. m2c supplies the
// *float64 (or nil, until a row exists); m2a supplies the meaning of nil.
func ResolveMargin(configured *float64) float64 {
	if configured == nil {
		return DefaultHysteresisMargin
	}
	return *configured
}

// Displaces implements doc 02 §3's anti-jitter hysteresis (spec R4.3,
// design D8): a challenger displaces an incumbent from the focus only when
// it exceeds the incumbent by MORE than margin, relative — as a ratio of
// the incumbent's own score, never an absolute band. Under Priority's
// multiplicative envelope (priority.go's own doc comment) a score has no
// fixed scale, so an absolute margin would mean a 5% band at priority 1.0
// and a 1.25% band at priority 4.0, damping weakest exactly where the
// contested values are largest — relative hysteresis is entailed by that
// envelope, not an independent preference (design D8, owner ruling 6).
//
// Equality does not displace: challenger == incumbent*(1+margin) returns
// false. The incumbent wins ties, which is the entire point of hysteresis
// — a challenger that is merely as good, not better, does not get to evict
// what is already there.
//
// Displaces takes no now. It compares two Scores that Rank already
// produced with now one layer up (design D8), so hysteresis is
// time-independent — testable without a fake clock at all, unlike every
// function in priority.go and rank.go.
//
// challenger and incumbent are ordinarily two Ranked.Score values, and
// Rank's own doc comment states a Score can be NaN (a corrupted Candidate)
// or +Inf (a legitimately extreme one) — this is the highest-risk boundary
// in this function, decided here rather than left implicit. scoreKey
// (rank.go) remaps a NaN score to negative infinity before either side of
// the comparison runs — the identical "a corrupted input contributes no
// promotion, sorts as if it had nothing going for it" posture Rank already
// takes for the same shape. Worked out rather than assumed:
//
//   - A NaN incumbent is remapped to -Inf, and -Inf*(1+margin) is still
//     -Inf for any margin > -1, so ANY non-NaN challenger displaces it: a
//     corrupted incumbent is not a permanent occupant of the focus nothing
//     could ever unseat. An ordinary `>` over the raw scores would set
//     exactly that trap: incumbent*(1+margin) is itself NaN, and
//     "x > NaN" is false for every x under IEEE 754, so a naive comparison
//     would make a corrupted incumbent unbeatable — the same class of
//     silent trap clamp's own doc comment records for Priority's adjacency
//     (C24), one layer up.
//   - A NaN challenger remains -Inf and can never displace anything,
//     including another NaN incumbent (-Inf is not strictly greater than
//     -Inf*(1+margin), itself -Inf): when neither side can be compared,
//     the incumbent keeps its seat — the same rule this function already
//     applies to two ordinary equal scores.
//   - +Inf needs no equivalent remap: it already orders correctly against
//     every finite value and against itself under an ordinary >, and two
//     equal +Inf scores correctly do not displace each other — the
//     equality rule above, extended with no special case.
//
// margin is assumed finite. It is m2c's resolved config.hysteresis_margin
// (via ResolveMargin), and neither that function nor this one sanitizes
// it — the same posture this milestone already takes toward
// config.weight_threshold's own unchecked column (R2.4/R2.7's own
// comment): validating a stored config value is a config-layer concern
// this change does not own.
func Displaces(challenger, incumbent, margin float64) bool {
	return scoreKey(challenger) > scoreKey(incumbent)*(1+margin)
}
