package recall

import "testing"

// TestAdmit_KeepsOnlyWhatClearsTheFloor is ADR-0020's arithmetic half.
//
// Search returns the NEAREST, which in a vault of one is that one whatever
// was asked. A maintainer asked their own brain over Telegram:
//
//	Pablo:  y cuando tengo gym?
//	Nooma:  Found 1 thing: • Tengo cita con el dentista el 2026-08-28.
//
// Vectors are unit-normalised at the storage boundary, so dot IS a cosine
// in [-1, 1] and a floor means something. Admit is where "nearest" becomes
// "near enough to say out loud".
//
// Mutation: return the input unchanged and the below-floor cases fail.
func TestAdmit_KeepsOnlyWhatClearsTheFloor(t *testing.T) {
	in := []Scored{
		{ID: "far", Score: RecallMinSimilarity - 0.01},
		{ID: "exactly-at", Score: RecallMinSimilarity},
		{ID: "near", Score: RecallMinSimilarity + 0.2},
		{ID: "opposite", Score: -0.4},
	}

	got := Admit(in)

	want := []string{"exactly-at", "near"}
	if len(got) != len(want) {
		t.Fatalf("Admit kept %d of %d: %+v", len(got), len(in), got)
	}
	for i, id := range want {
		if got[i].ID != id {
			t.Errorf("kept[%d] = %q, want %q", i, got[i].ID, id)
		}
	}
}

// TestAdmit_AtTheFloorIsAdmitted pins the boundary as inclusive, and says
// why in the only place a reader will look for it: a floor stated as a
// number people will tune is easier to reason about as "at least this
// similar" than as "strictly more similar than".
func TestAdmit_AtTheFloorIsAdmitted(t *testing.T) {
	if got := Admit([]Scored{{ID: "u", Score: RecallMinSimilarity}}); len(got) != 1 {
		t.Errorf("a result exactly at the floor was rejected; the floor is a minimum, not an " +
			"exclusive bound")
	}
}

// TestAdmit_PreservesOrder: admission decides membership and nothing else.
// Reordering here would silently overrule the fusion that ADR-0010 owns.
func TestAdmit_PreservesOrder(t *testing.T) {
	in := []Scored{
		{ID: "a", Score: 0.9},
		{ID: "b", Score: 0.95},
		{ID: "c", Score: 0.91},
	}
	got := Admit(in)
	if len(got) != 3 {
		t.Fatalf("Admit dropped a result that clears the floor: %+v", got)
	}
	for i := range in {
		if got[i].ID != in[i].ID {
			t.Fatalf("Admit reordered %v into %v — ordering belongs to the fusion (ADR-0010), "+
				"not to admission", in, got)
		}
	}
}

// TestRecallMinSimilarity_IsCalibratable guards the number against being
// inlined, which is how a behavioural constant stops being findable.
func TestRecallMinSimilarity_IsCalibratable(t *testing.T) {
	if RecallMinSimilarity <= 0 || RecallMinSimilarity >= 1 {
		t.Errorf("RecallMinSimilarity = %v; a cosine floor outside (0,1) either admits "+
			"everything or nothing", RecallMinSimilarity)
	}
}
