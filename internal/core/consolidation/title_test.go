package consolidation

import (
	"reflect"
	"testing"
	"time"
)

func tp(t time.Time) *time.Time { return &t }

// TestSelectToName_OrdersByWhatRingsFirst pins ADR-0065 point 3: units with a
// pending reminder first (soonest fire first), then future instants nearest
// first, then past instants most recent first, ties by id, cut at the limit.
func TestSelectToName_OrdersByWhatRingsFirst(t *testing.T) {
	now := time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)
	h := func(n int) time.Time { return now.Add(time.Duration(n) * time.Hour) }
	cands := []Untitled{
		{ID: "past-old", EventAt: tp(h(-48))},
		{ID: "future-far", DueAt: tp(h(72))},
		{ID: "past-recent", EventAt: tp(h(-2))},
		{ID: "armed-late", EventAt: tp(h(200)), NextFire: tp(h(150))},
		{ID: "future-near", EventAt: tp(h(3))},
		{ID: "armed-soon", EventAt: tp(h(-5)), NextFire: tp(h(1))},
	}
	want := []string{"armed-soon", "armed-late", "future-near", "future-far", "past-recent", "past-old"}
	if got := SelectToName(cands, now, 50); !reflect.DeepEqual(got, want) {
		t.Fatalf("SelectToName = %v, want %v", got, want)
	}
	if got := SelectToName(cands, now, 3); !reflect.DeepEqual(got, want[:3]) {
		t.Fatalf("SelectToName cut at 3 = %v, want %v", got, want[:3])
	}
}

func TestSelectToName_TiesBreakByIDAndTheInstantNowIsAhead(t *testing.T) {
	now := time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)
	cands := []Untitled{
		{ID: "b", EventAt: tp(now)},
		{ID: "a", EventAt: tp(now)},
		{ID: "c", EventAt: tp(now.Add(-time.Minute))},
	}
	want := []string{"a", "b", "c"}
	if got := SelectToName(cands, now, 50); !reflect.DeepEqual(got, want) {
		t.Fatalf("SelectToName = %v, want %v", got, want)
	}
}

func TestSelectToName_EmptyAndNonPositiveLimitSelectNothing(t *testing.T) {
	now := time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)
	if got := SelectToName(nil, now, 50); len(got) != 0 {
		t.Fatalf("SelectToName(nil) = %v, want none", got)
	}
	if got := SelectToName([]Untitled{{ID: "a", EventAt: tp(now)}}, now, 0); len(got) != 0 {
		t.Fatalf("SelectToName with limit 0 = %v, want none", got)
	}
}

func TestSelectToName_DoesNotReorderItsInput(t *testing.T) {
	now := time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)
	cands := []Untitled{{ID: "z", EventAt: tp(now.Add(-time.Hour))}, {ID: "a", EventAt: tp(now.Add(time.Hour))}}
	SelectToName(cands, now, 50)
	if cands[0].ID != "z" {
		t.Fatalf("SelectToName reordered its input: %v", cands)
	}
}

// TestTitleBackfillPerPass_IsFifty pins doc 02 §13's cap on one night's naming.
func TestTitleBackfillPerPass_IsFifty(t *testing.T) {
	if TitleBackfillPerPass != 50 {
		t.Fatalf("TitleBackfillPerPass = %d, want 50 (doc 02 §13)", TitleBackfillPerPass)
	}
}
