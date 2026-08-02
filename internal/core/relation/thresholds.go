package relation

// The two default thresholds and the dedup candidate bound — design D7.
//
// DefaultMinConfidenceToPersist and DefaultMinConfidenceToSurface are pinned
// to migration 0002's relation_thresholds column DEFAULTs (0002:33, 0002:34)
// and kept from drifting apart by TestRelationThresholdDefaultsMatchMigration
// (L2, test/conformance), which reads the SQL off disk — depguard forbids os
// here.
//
// DedupCandidateK bounds how many recall candidates the relation judge is
// asked about per capture (design D5, D7) — a behavioral number, named per
// docs/02-cognitive-core.md §13's calibration table.
const (
	DefaultMinConfidenceToPersist = 0.30
	DefaultMinConfidenceToSurface = 0.50
	DedupCandidateK               = 5
)

// Thresholds is a relation type's resolved persist/surface pair — doc 02 §4.
type Thresholds struct {
	Persist float64
	Surface float64
}

// Resolve is Q1's closed answer: relation_thresholds seeds no row for any
// type — relation type is open text, so no seed could ever be exhaustive —
// so a nil row (the type has never been looked up before) falls back to the
// two package defaults above. A non-nil row passes through unchanged.
//
// core/relation never reads relation_thresholds itself (R5.1's purity
// MUST): brain performs the lookup and hands the result, or nil, here.
func Resolve(row *Thresholds) Thresholds {
	if row == nil {
		return Thresholds{Persist: DefaultMinConfidenceToPersist, Surface: DefaultMinConfidenceToSurface}
	}
	return *row
}
