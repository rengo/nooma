package correction

import "github.com/rengo/nooma/internal/core/recall"

// ReferentMargin is docs/02-cognitive-core.md §13's
// correction_referent_margin: the ratio the top two candidates must clear
// for Referent to pick instead of asking. Doc 02 §5 step 4's own wording —
// the system asks when the top two are "closer together than" the margin —
// is a strict inequality on the ask side, so Referent's boundary is
// inclusive on the pick side: a ratio exactly equal to the margin picks
// (m1b D7's own lesson about conf == Surface, applied here).
const ReferentMargin = 1.5

// Referent decides which unit a correction targets when the caller gave no
// explicit id — doc 02 §5 step 4, design D2.
//
//	len(cands) == 0                          -> "", false  (nothing to correct)
//	len(cands) == 1                          -> id,  true   (no ambiguity to gate)
//	cands[0].Score/cands[1].Score >= margin  -> id,  true
//	otherwise                                -> "", false  (ask)
//
// Only cands[0] and cands[1] ever participate (R1.3's own MUST): a third or
// later candidate never changes the answer, even if its own score would
// flip the ratio had it been compared instead.
//
// cands must be the LIVE candidates, already filtered through
// ports.UnitRepo.LiveByIDs — never before. A superseded or archived
// candidate has no unit left to correct; a ratio computed before that
// filter gates the surviving top candidate against a score that belonged
// to a unit nobody can edit (design D2). Referent itself has no notion of
// unit status and cannot enforce this — it trusts the caller, the same way
// it trusts cands to be pre-sorted descending by score.
//
// margin is a parameter, not the constant — ReferentMargin above is only
// the caller's default, the same shape relation.Decide's own thresholds
// use. A margin <= 1 makes the gate vacuous, since the ratio of any two
// strictly positive scores compared here is never negative: documented
// here, not guarded in code, because the only producer of margin in this
// codebase is the constant above, and a guard would be a branch no test
// can ever reach.
//
// Referent is pure: no LLM, no I/O, no clock — a function of cands and
// margin, nothing else.
func Referent(cands []recall.FusedCandidate, margin float64) (string, bool) {
	switch len(cands) {
	case 0:
		return "", false
	case 1:
		return cands[0].ID, true
	}
	if cands[0].Score/cands[1].Score >= margin {
		return cands[0].ID, true
	}
	return "", false
}
