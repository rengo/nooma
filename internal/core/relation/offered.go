package relation

// TargetOffered reports whether target is one of the candidate IDs the judge
// was actually shown — doc 02 §4, ADR-0026.
//
// Both judge call sites render a specific candidate list into the prompt and
// then read a target ID back out of the answer. Nothing made those two agree.
// The consolidation pass renders exactly one candidate per call, so any other
// ID is wrong by construction; capture renders at most DedupCandidateK.
//
// The failure this closes is not a hallucinated ID — `relations.to_unit_id`
// REFERENCES `units(id)` and the vault opens with `foreign_keys=on`, so an
// invented ID is refused by SQLite. It is an ID that is **real and was never
// offered**: the foreign key is satisfied, and an edge appears between two
// units the judge never compared. A judge naming the source itself is the same
// class, and passes the same foreign key.
//
// The empty target is not offered by an empty candidate. A judge that answered
// nothing and a candidate list that carried nothing are two separate faults,
// and matching them against each other would turn both into a persisted edge.
//
// Comparison is exact: unit IDs are opaque identifiers, and a predicate that
// folded case or matched prefixes would accept "unit-1" for "unit-10".
func TargetOffered(target string, offered []string) bool {
	if target == "" {
		return false
	}
	for _, id := range offered {
		if id == target {
			return true
		}
	}
	return false
}
