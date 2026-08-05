package focus

import (
	"math"
	"reflect"
	"testing"
	"time"
)

// dueAfter returns a *time.Time exactly d days after now — the same small
// helper priority_test.go's dueIn provides, kept local to this file and
// named for the direction it always takes (forward) since no fixture below
// needs a due date in the past.
func dueAfter(now time.Time, days float64) *time.Time {
	t := now.Add(time.Duration(days * 24 * float64(time.Hour)))
	return &t
}

// TestRank_ThreeLevelTieBreak proves spec R3.6/design D6's total order:
// higher Score first; among exactly equal Scores, earlier DueAt first with
// a non-nil DueAt always ordered before a nil one regardless of the actual
// instant it names; among exactly equal Scores and both-nil DueAt, by ID.
//
// Every Weight-1.0 candidate below shares DecayRate 0, LastTouchedAt == now
// == CreatedAt (AgeRamp 0), and either no DueAt or a DueAt past
// UrgencyLeadDays (UrgencyRamp 0 either way) — so Priority reduces to
// exactly Weight (1.0 * 1 * 1) for all four: an exact float64 tie by
// construction, not a coincidence of the formula's arithmetic. "highest"
// alone carries a genuinely different, higher Weight (2.0), proving level 1
// actually governs when Scores differ rather than every case falling
// through to the tie-break.
func TestRank_ThreeLevelTieBreak(t *testing.T) {
	now := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)

	tied := func(id string, dueAt *time.Time) Candidate {
		return Candidate{
			ID:            id,
			Weight:        1.0,
			DecayRate:     0,
			LastTouchedAt: now,
			CreatedAt:     now,
			DueAt:         dueAt,
		}
	}

	highest := tied("highest", nil)
	highest.Weight = 2.0
	earlierDue := tied("earlier-due", dueAfter(now, 20)) // beyond the 7-day lead window: ramp 0
	laterDue := tied("later-due", dueAfter(now, 30))     // beyond the 7-day lead window: ramp 0
	noDueA := tied("a-no-due", nil)
	noDueB := tied("b-no-due", nil)

	// Deliberately out of every candidate order Rank should produce.
	cs := []Candidate{laterDue, noDueB, highest, noDueA, earlierDue}
	got := Rank(cs, nil, now)

	if len(got) != len(cs) {
		t.Fatalf("Rank() returned %d Ranked, want %d (one per Candidate)", len(got), len(cs))
	}

	wantOrder := []string{"highest", "earlier-due", "later-due", "a-no-due", "b-no-due"}
	gotOrder := make([]string, len(got))
	for i, r := range got {
		gotOrder[i] = r.Candidate.ID
	}
	if !reflect.DeepEqual(gotOrder, wantOrder) {
		t.Fatalf("Rank() order = %v, want %v", gotOrder, wantOrder)
	}

	// Guard against a stub or an accidental no-op that leaves every Score at
	// the same value: the four Weight-1.0 candidates must score identically
	// to each other (proving the tie is genuine) but strictly less than
	// "highest" (proving level 1 governs, not merely the tie-break alone).
	for _, r := range got[1:] {
		if r.Score != 1.0 {
			t.Errorf("Rank()[%q].Score = %v, want exactly 1.0 (Weight, no age or urgency lift)", r.Candidate.ID, r.Score)
		}
	}
	if got[0].Score != 2.0 {
		t.Errorf("Rank()[%q].Score = %v, want exactly 2.0", got[0].Candidate.ID, got[0].Score)
	}
}

// TestRank_AdjacencyMissingOrNil_BehavesAsZero proves spec R3.6's MUST: a
// nil adjacency map, and a non-nil map that simply does not mention a
// Candidate's id, both read as 0 for that Candidate. Indexing a Go map for
// an absent key always returns the zero value — nil map included — so this
// is what lets a caller with no relation graph loaded pass nil and still
// get a well-defined ranking (proposal §4.3). Each Candidate's Score is
// pinned against Priority(c, 0, now) computed independently — Priority is
// already implemented and verified (PR 3a) — not against Rank's own
// arithmetic restated.
func TestRank_AdjacencyMissingOrNil_BehavesAsZero(t *testing.T) {
	now := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	cs := []Candidate{
		{ID: "a", Weight: 1.0, DecayRate: 0.01, LastTouchedAt: now, CreatedAt: now.AddDate(0, 0, -5)},
		{ID: "b", Weight: 0.7, DecayRate: 0.0, LastTouchedAt: now, CreatedAt: now},
	}

	t.Run("nil map", func(t *testing.T) {
		got := Rank(cs, nil, now)
		if len(got) != len(cs) {
			t.Fatalf("Rank() returned %d Ranked, want %d", len(got), len(cs))
		}
		for _, r := range got {
			want := Priority(r.Candidate, 0, now)
			if r.Score != want {
				t.Errorf("Rank(cs, nil, now)[%q].Score = %v, want Priority(c, 0, now) = %v", r.Candidate.ID, r.Score, want)
			}
		}
	})

	t.Run("non-nil map, id absent", func(t *testing.T) {
		adjacency := map[string]float64{"unrelated-id": 0.9}
		got := Rank(cs, adjacency, now)
		if len(got) != len(cs) {
			t.Fatalf("Rank() returned %d Ranked, want %d", len(got), len(cs))
		}
		for _, r := range got {
			want := Priority(r.Candidate, 0, now)
			if r.Score != want {
				t.Errorf("Rank(cs, adjacency, now)[%q].Score = %v, want Priority(c, 0, now) = %v (id not present in adjacency)", r.Candidate.ID, r.Score, want)
			}
		}
	})

	t.Run("non-nil map, id present, other ids unaffected", func(t *testing.T) {
		adjacency := map[string]float64{"a": 0.6}
		got := Rank(cs, adjacency, now)
		if len(got) != len(cs) {
			t.Fatalf("Rank() returned %d Ranked, want %d", len(got), len(cs))
		}
		for _, r := range got {
			want := Priority(r.Candidate, 0, now)
			if r.Candidate.ID == "a" {
				want = Priority(r.Candidate, 0.6, now)
			}
			if r.Score != want {
				t.Errorf("Rank(cs, adjacency, now)[%q].Score = %v, want %v", r.Candidate.ID, r.Score, want)
			}
		}
	})
}

// TestRank_NonFiniteScore_SortsLastWithoutBreakingTotalOrder proves the
// decision this package takes about a non-finite Score. Priority returns
// NaN whenever weight.Effective's own Weight or DecayRate takes one of the
// four NaN-producing shapes decay.go's own doc comment enumerates — not
// only a literal NaN input (decay.go's own doc comment; Priority inherits
// that boundary unchanged, spec R3.1's finite-Weight/DecayRate qualifier)
// — sort.Slice's comparator must stay a
// strict weak ordering regardless, since comparing raw Scores with an
// ordinary > is inconsistent whenever exactly one side is NaN (every IEEE
// 754 comparison against NaN is false — the identical trap clamp's own doc
// comment records and closes for adjacency, C24 — and an inconsistent
// sort.Slice comparator is undefined behavior, not merely a wrong order).
// Rank's decision, exercised here: a NaN Score sorts to the very bottom,
// two NaN Scores fall through to the ID level between themselves rather
// than landing in an arbitrary relative order, and the returned Ranked
// still carries the literal NaN — Rank orders around a corrupted
// Candidate, it does not coerce or hide it. Run 20 times over the same
// input to guard against any incidental nondeterminism (e.g. a future
// refactor that ranges over a map), the same discipline
// internal/core/weight's C16 established for its own sort guarantee.
//
// +Inf is reachable through weight.Effective too (decay.go: a large,
// finite Weight with DecayRate 0 stays +Inf, no underflow involved) and
// needs no special handling: IEEE 754 already orders +Inf correctly
// against every finite value under an ordinary >, so it sorts to the very
// top with no coercion — included to make the asymmetry with NaN explicit
// rather than assumed.
func TestRank_NonFiniteScore_SortsLastWithoutBreakingTotalOrder(t *testing.T) {
	now := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)

	healthyHigh := Candidate{ID: "healthy-high", Weight: 2.0, DecayRate: 0, LastTouchedAt: now, CreatedAt: now}
	healthyLow := Candidate{ID: "healthy-low", Weight: 0.5, DecayRate: 0, LastTouchedAt: now, CreatedAt: now}
	corruptZ := Candidate{ID: "z-corrupt", Weight: math.NaN(), DecayRate: 0, LastTouchedAt: now, CreatedAt: now}
	corruptA := Candidate{ID: "a-corrupt", Weight: math.NaN(), DecayRate: 0, LastTouchedAt: now, CreatedAt: now}
	runaway := Candidate{ID: "runaway", Weight: math.Inf(1), DecayRate: 0, LastTouchedAt: now, CreatedAt: now}

	cs := []Candidate{corruptZ, healthyLow, corruptA, runaway, healthyHigh}
	wantOrder := []string{"runaway", "healthy-high", "healthy-low", "a-corrupt", "z-corrupt"}

	for i := 0; i < 20; i++ {
		got := Rank(cs, nil, now)
		if len(got) != len(cs) {
			t.Fatalf("iteration %d: Rank() returned %d Ranked, want %d", i, len(got), len(cs))
		}

		gotOrder := make([]string, len(got))
		for j, r := range got {
			gotOrder[j] = r.Candidate.ID
		}
		if !reflect.DeepEqual(gotOrder, wantOrder) {
			t.Fatalf("iteration %d: Rank() order = %v, want %v", i, gotOrder, wantOrder)
		}

		if !math.IsInf(got[0].Score, 1) {
			t.Fatalf("iteration %d: Rank()[%q].Score = %v, want +Inf", i, got[0].Candidate.ID, got[0].Score)
		}
		for _, r := range got[3:] {
			if !math.IsNaN(r.Score) {
				t.Fatalf("iteration %d: Rank()[%q].Score = %v, want NaN (Rank reports the literal corrupted score, it does not coerce it)", i, r.Candidate.ID, r.Score)
			}
		}
	}
}

// TestRank_DuplicateID_BothSurviveOrderUnpinned pins Rank's actual, current
// behaviour for the one input shape docs/02-cognitive-core.md §3 and Rank's
// own doc comment now both say is outside the tie-break's total-order
// guarantee: two distinct Candidate values sharing an ID, tied on Score and
// DueAt (both nil here). Rank has no dedup or validation step, so both
// entries survive — this guards against a future refactor silently dropping
// one the way a map-keyed rewrite (last write wins) would, the identical
// shape weight.Resurface's own Neighbourhood.States build was found to risk
// (C18). It asserts only that both survive and both carry the right ID and
// Score, never which one sorts first: sort.Slice makes no such promise once
// every comparator level ties, and pinning a specific order here would pin
// an implementation accident, not a guarantee (C26).
func TestRank_DuplicateID_BothSurviveOrderUnpinned(t *testing.T) {
	now := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)

	dupA := Candidate{ID: "dup", Weight: 1.0, DecayRate: 0, LastTouchedAt: now, CreatedAt: now}
	dupB := Candidate{ID: "dup", Weight: 1.0, DecayRate: 0, LastTouchedAt: now, CreatedAt: now}

	for _, cs := range [][]Candidate{{dupA, dupB}, {dupB, dupA}} {
		got := Rank(cs, nil, now)
		if len(got) != 2 {
			t.Fatalf("Rank() returned %d Ranked for 2 duplicate-ID Candidates, want 2 (nothing dropped)", len(got))
		}
		for _, r := range got {
			if r.Candidate.ID != "dup" {
				t.Errorf("Rank()[...].Candidate.ID = %q, want %q", r.Candidate.ID, "dup")
			}
			if r.Score != 1.0 {
				t.Errorf("Rank()[...].Score = %v, want exactly 1.0", r.Score)
			}
		}
	}
}
