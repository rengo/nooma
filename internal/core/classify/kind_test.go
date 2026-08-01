package classify

import (
	"testing"

	"github.com/rengo/nooma/internal/core/unit"
)

// TestAllKinds asserts the taxonomy's own completeness (design D11 point
// 4): exactly the thirteen values doc 02 §5 step 1 names, no more, no
// fewer, so a value added later without an entry here fails loudly.
func TestAllKinds(t *testing.T) {
	want := map[Kind]bool{
		KindTask:              true,
		KindMentalLoad:        true,
		KindEvent:             true,
		KindKnowledge:         true,
		KindProcedural:        true,
		KindEmotional:         true,
		KindChitchat:          true,
		KindOutOfScope:        true,
		KindRecall:            true,
		KindCorrection:        true,
		KindTimer:             true,
		KindRecurringReminder: true,
		KindList:              true,
	}

	got := AllKinds()
	if len(got) != len(want) {
		t.Fatalf("AllKinds() returned %d members, want %d: %v", len(got), len(want), got)
	}
	seen := make(map[Kind]bool, len(got))
	for _, k := range got {
		if !want[k] {
			t.Errorf("AllKinds() contains unexpected member %q", k)
		}
		if seen[k] {
			t.Errorf("AllKinds() contains %q twice", k)
		}
		seen[k] = true
	}
}

// TestKind_UnitType covers R1.1's own "MUST NOT be confused with unit.Type"
// half: the seven Kind values that persist a unit map to unit.Type's
// same-named member, and the six that never persist a unit — chitchat,
// out_of_scope, recall, correction, timer, recurring_reminder
// (docs/02-cognitive-core.md §8: "a timer is NEVER a unit") — return false,
// leaving the caller (classify.ToUnit, PR 7b) unable to forget the check.
func TestKind_UnitType(t *testing.T) {
	persisting := map[Kind]unit.Type{
		KindTask:       unit.TypeTask,
		KindMentalLoad: unit.TypeMentalLoad,
		KindEvent:      unit.TypeEvent,
		KindKnowledge:  unit.TypeKnowledge,
		KindProcedural: unit.TypeProcedural,
		KindEmotional:  unit.TypeEmotional,
		KindList:       unit.TypeList,
	}
	for k, want := range persisting {
		got, ok := k.UnitType()
		if !ok || got != want {
			t.Errorf("%s.UnitType() = (%v, %v), want (%v, true)", k, got, ok, want)
		}
	}

	nonPersisting := []Kind{
		KindChitchat, KindOutOfScope, KindRecall, KindCorrection,
		KindTimer, KindRecurringReminder,
	}
	for _, k := range nonPersisting {
		if _, ok := k.UnitType(); ok {
			t.Errorf("%s.UnitType() ok = true, want false — this Kind must never persist a unit", k)
		}
	}

	if len(persisting)+len(nonPersisting) != len(AllKinds()) {
		t.Fatalf("test covers %d Kind values, AllKinds() has %d — this table is not exhaustive",
			len(persisting)+len(nonPersisting), len(AllKinds()))
	}
}
