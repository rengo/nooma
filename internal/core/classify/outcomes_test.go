package classify

import "testing"

// assertVocabulary checks that all holds exactly the members of want — no
// more, no fewer, no duplicates — and that the expectation itself is not
// silently short (design D11 point 4: a value added later without an entry
// here fails loudly instead of defaulting to a passing arm).
//
// It is generic over ~string for the same reason decodeEnum is (design D11
// point 2): six vocabularies with one set of assertions, not six copies.
func assertVocabulary[T ~string](t *testing.T, all []T, want map[T]bool) {
	t.Helper()

	if len(all) != len(want) {
		t.Fatalf("vocabulary has %d members, want %d: %v", len(all), len(want), all)
	}
	seen := make(map[T]bool, len(all))
	for _, v := range all {
		if !want[v] {
			t.Errorf("vocabulary contains unexpected member %q", string(v))
		}
		if seen[v] {
			t.Errorf("vocabulary contains %q twice", string(v))
		}
		seen[v] = true
	}
}

// TestOrthogonalVocabularies pins the six orthogonal resolution fields to the
// values doc 02 §5 names at 02:120-123, mirrored by testdata/classify/format.md
// :56-61. They are orthogonal fields, not types (spec R1.1's adjacent half):
// each closes over its own vocabulary and degrades independently of the others
// and of Kind.
func TestOrthogonalVocabularies(t *testing.T) {
	t.Run("nudge_outcome", func(t *testing.T) {
		assertVocabulary(t, AllNudgeOutcomes(), map[NudgeOutcome]bool{
			NudgeOutcomeEngaged:  true,
			NudgeOutcomeDeclined: true,
		})
	})

	t.Run("relation_outcome", func(t *testing.T) {
		assertVocabulary(t, AllRelationOutcomes(), map[RelationOutcome]bool{
			RelationOutcomeConfirmed: true,
			RelationOutcomeRejected:  true,
		})
	})

	t.Run("state_outcome", func(t *testing.T) {
		assertVocabulary(t, AllStateOutcomes(), map[StateOutcome]bool{
			StateOutcomeConfirmed: true,
			StateOutcomeDenied:    true,
		})
	})

	t.Run("task_checkin_outcome", func(t *testing.T) {
		assertVocabulary(t, AllTaskCheckinOutcomes(), map[TaskCheckinOutcome]bool{
			TaskCheckinOutcomeDone:   true,
			TaskCheckinOutcomeSnooze: true,
			TaskCheckinOutcomeDrop:   true,
		})
	})

	t.Run("list_op", func(t *testing.T) {
		assertVocabulary(t, AllListOps(), map[ListOp]bool{
			ListOpAppend:   true,
			ListOpDelete:   true,
			ListOpMarkDone: true,
			ListOpRemove:   true,
		})
	})

	t.Run("person_ref_status", func(t *testing.T) {
		assertVocabulary(t, AllPersonRefStatuses(), map[PersonRefStatus]bool{
			PersonRefStatusResolved:  true,
			PersonRefStatusNew:       true,
			PersonRefStatusAmbiguous: true,
		})
	})
}

// TestOrthogonalVocabulariesAreDistinct guards the one confusion these six
// invite: state_outcome and relation_outcome both carry "confirmed", and
// nothing but their types keeps them apart. A refactor that collapsed them
// into one vocabulary would pass every completeness check above — this is
// what catches it. Two vocabularies sharing a wire value is not a defect;
// two vocabularies becoming one is.
func TestOrthogonalVocabulariesAreDistinct(t *testing.T) {
	if string(StateOutcomeConfirmed) != string(RelationOutcomeConfirmed) {
		t.Fatalf("doc 02:121-122 gives both state_outcome and relation_outcome the wire value "+
			"%q; got state=%q relation=%q", "confirmed",
			StateOutcomeConfirmed, RelationOutcomeConfirmed)
	}

	// The negative halves diverge, which is what makes them two vocabularies:
	// relation rejects, state denies.
	if string(RelationOutcomeRejected) == string(StateOutcomeDenied) {
		t.Errorf("relation_outcome and state_outcome must not share their negative value; "+
			"both are %q, doc 02:121-122 says rejected and denied", RelationOutcomeRejected)
	}
}
