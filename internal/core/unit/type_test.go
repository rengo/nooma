package unit

import "testing"

// TestAllTypes_HasExactlyTheDoc02Members proves R2.4: AllTypes returns
// exactly the nine persisted values doc 02 §1 lists, as a set, and the two
// classify outcomes ("timer", "recurring_reminder" — doc 02 §5.1's
// vocabulary, not this one, design D4) are not among them.
func TestAllTypes_HasExactlyTheDoc02Members(t *testing.T) {
	want := map[Type]bool{
		TypeTask:          true,
		TypeMentalLoad:    true,
		TypeEvent:         true,
		TypeKnowledge:     true,
		TypeProcedural:    true,
		TypeEmotional:     true,
		TypeList:          true,
		TypeStructuredRef: true,
		TypeInsight:       true,
	}

	got := AllTypes()
	if len(got) != len(want) {
		t.Fatalf("AllTypes() has %d members, want %d: %v", len(got), len(want), got)
	}

	seen := make(map[Type]bool, len(got))
	for _, ty := range got {
		if !want[ty] {
			t.Errorf("AllTypes() includes %q, which is not a doc 02 §1 member", ty)
		}
		if seen[ty] {
			t.Errorf("AllTypes() lists %q more than once", ty)
		}
		seen[ty] = true
	}

	// classify outcomes (doc 02 §5.1), a different vocabulary — design D4.
	excludedClassifyOutcomes := []string{"timer", "recurring_reminder"}
	for _, excluded := range excludedClassifyOutcomes {
		if seen[Type(excluded)] {
			t.Errorf("AllTypes() includes %q — that is a classify outcome (doc 02 §5.1), not a persisted unit type", excluded)
		}
	}
}

// TestAllTypes_ReturnsAFreshSliceEachCall proves AllTypes follows the same
// D1 pattern AllStatuses does: a function, not a mutable exported var.
func TestAllTypes_ReturnsAFreshSliceEachCall(t *testing.T) {
	first := AllTypes()
	first[0] = Type("mutated")

	second := AllTypes()
	for _, ty := range second {
		if ty == Type("mutated") {
			t.Fatal("AllTypes() shares backing storage across calls — mutating one call's result changed another's")
		}
	}
}
