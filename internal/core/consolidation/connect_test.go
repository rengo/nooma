package consolidation

import (
	"testing"
	"time"

	"github.com/rengo/nooma/internal/core/recall"
	"github.com/rengo/nooma/internal/core/relation"
	"github.com/rengo/nooma/internal/core/unit"
)

func mustParseTime(t *testing.T, s string) time.Time {
	t.Helper()
	tm, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatalf("parse %q: %v", s, err)
	}
	return tm
}

// TestSelectConnectSources_SinceNilTakesWholeLivePool proves R4.1: since ==
// nil (the first pass over an existing vault) makes every live source
// eligible, regardless of LastTouchedAt.
func TestSelectConnectSources_SinceNilTakesWholeLivePool(t *testing.T) {
	now := mustParseTime(t, "2026-01-10T00:00:00Z")
	sources := []Source{
		{UnitID: "a", Status: unit.StatusPool, Weight: 1.0, LastTouchedAt: mustParseTime(t, "2025-01-01T00:00:00Z")},
		{UnitID: "b", Status: unit.StatusPool, Weight: 2.0, LastTouchedAt: mustParseTime(t, "2026-01-09T00:00:00Z")},
		{UnitID: "c", Status: unit.StatusPool, Weight: 0.5, LastTouchedAt: mustParseTime(t, "2020-01-01T00:00:00Z")},
	}

	got := SelectConnectSources(sources, nil, now)
	if len(got) != 3 {
		t.Fatalf("SelectConnectSources() = %v, want all 3 live sources", got)
	}
}

// TestSelectConnectSources_NonLiveExcluded proves R4.1: a Source whose
// Status is not unit.StatusPool is never eligible, even with since == nil.
func TestSelectConnectSources_NonLiveExcluded(t *testing.T) {
	now := mustParseTime(t, "2026-01-10T00:00:00Z")
	sources := []Source{
		{UnitID: "live", Status: unit.StatusPool, Weight: 1.0, LastTouchedAt: now},
		{UnitID: "archived", Status: unit.StatusArchived, Weight: 5.0, LastTouchedAt: now},
		{UnitID: "superseded", Status: unit.StatusSuperseded, Weight: 5.0, LastTouchedAt: now},
		{UnitID: "incomplete", Status: unit.StatusIncomplete, Weight: 5.0, LastTouchedAt: now},
	}

	got := SelectConnectSources(sources, nil, now)
	if len(got) != 1 || got[0] != "live" {
		t.Fatalf("SelectConnectSources() = %v, want exactly [\"live\"]", got)
	}
}

// TestSelectConnectSources_TouchedBeforeSinceExcluded proves R4.1: with a
// non-nil since, a source is eligible only when LastTouchedAt is at or
// after since — a source touched strictly before since is excluded.
func TestSelectConnectSources_TouchedBeforeSinceExcluded(t *testing.T) {
	since := mustParseTime(t, "2026-01-05T00:00:00Z")
	now := mustParseTime(t, "2026-01-10T00:00:00Z")
	sources := []Source{
		{UnitID: "before", Status: unit.StatusPool, Weight: 1.0, LastTouchedAt: mustParseTime(t, "2026-01-04T23:59:59Z")},
		{UnitID: "at", Status: unit.StatusPool, Weight: 1.0, LastTouchedAt: since},
		{UnitID: "after", Status: unit.StatusPool, Weight: 1.0, LastTouchedAt: mustParseTime(t, "2026-01-06T00:00:00Z")},
	}

	got := SelectConnectSources(sources, &since, now)
	want := map[string]bool{"at": true, "after": true}
	if len(got) != len(want) {
		t.Fatalf("SelectConnectSources() = %v, want exactly %v", got, want)
	}
	for _, id := range got {
		if !want[id] {
			t.Errorf("SelectConnectSources() includes %q, which is touched before since", id)
		}
	}
}

// TestSelectConnectSources_OrderedByEffectiveDescendingTieByID proves R4.1's
// ordering: eligible sources are ranked by weight.Effective descending, ties
// broken by UnitID ascending — and the result is the SAME regardless of the
// input slice's own order (no accidental dependence on input order or map
// iteration, since SelectConnectSources ranks a slice, never a map).
func TestSelectConnectSources_OrderedByEffectiveDescendingTieByID(t *testing.T) {
	now := mustParseTime(t, "2026-01-10T00:00:00Z")
	// DecayRate 0 makes weight.Effective == Weight regardless of
	// LastTouchedAt, so the fixture's ranking is fully controlled by Weight.
	fixture := []Source{
		{UnitID: "z-tie", Status: unit.StatusPool, Weight: 2.0, LastTouchedAt: now},
		{UnitID: "a-tie", Status: unit.StatusPool, Weight: 2.0, LastTouchedAt: now},
		{UnitID: "highest", Status: unit.StatusPool, Weight: 5.0, LastTouchedAt: now},
		{UnitID: "lowest", Status: unit.StatusPool, Weight: 0.1, LastTouchedAt: now},
		{UnitID: "mid", Status: unit.StatusPool, Weight: 3.0, LastTouchedAt: now},
	}
	want := []string{"highest", "mid", "a-tie", "z-tie", "lowest"}

	forward := SelectConnectSources(fixture, nil, now)
	if !equalStrings(forward, want) {
		t.Fatalf("SelectConnectSources() = %v, want %v", forward, want)
	}

	reversed := make([]Source, len(fixture))
	for i, s := range fixture {
		reversed[len(fixture)-1-i] = s
	}
	got := SelectConnectSources(reversed, nil, now)
	if !equalStrings(got, want) {
		t.Fatalf("SelectConnectSources() over a reversed-order input = %v, want the same %v — ordering must not depend on input order",
			got, want)
	}
}

// TestSelectConnectSources_CappedAtConnectSourceLimit proves R4.1: the
// result never holds more than ConnectSourceLimit entries, and the entries
// kept are the highest-weighted ones.
func TestSelectConnectSources_CappedAtConnectSourceLimit(t *testing.T) {
	now := mustParseTime(t, "2026-01-10T00:00:00Z")
	total := ConnectSourceLimit + 5
	sources := make([]Source, total)
	for i := 0; i < total; i++ {
		sources[i] = Source{
			UnitID:        idFor(i),
			Status:        unit.StatusPool,
			Weight:        float64(total - i), // descending: id0 has the highest weight
			LastTouchedAt: now,
		}
	}

	got := SelectConnectSources(sources, nil, now)
	if len(got) != ConnectSourceLimit {
		t.Fatalf("SelectConnectSources() returned %d entries, want exactly ConnectSourceLimit (%d)", len(got), ConnectSourceLimit)
	}
	for i, id := range got {
		if id != idFor(i) {
			t.Errorf("position %d: got %q, want %q — the cap must keep the highest-weighted sources", i, id, idFor(i))
		}
	}
}

func idFor(i int) string {
	return "u" + string(rune('a'+i%26)) + string(rune('0'+i/26))
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestCanonicalPair_IsSymmetricAndLexicographicallyOrdered proves R4.2:
// CanonicalPair(a, b) == CanonicalPair(b, a), and the result is ordered
// lexicographically regardless of argument order.
func TestCanonicalPair_IsSymmetricAndLexicographicallyOrdered(t *testing.T) {
	got1 := CanonicalPair("b-unit", "a-unit")
	got2 := CanonicalPair("a-unit", "b-unit")
	want := Pair{From: "a-unit", To: "b-unit"}

	if got1 != want {
		t.Errorf("CanonicalPair(%q, %q) = %v, want %v", "b-unit", "a-unit", got1, want)
	}
	if got2 != want {
		t.Errorf("CanonicalPair(%q, %q) = %v, want %v", "a-unit", "b-unit", got2, want)
	}
	if got1 != got2 {
		t.Fatalf("CanonicalPair is not symmetric: CanonicalPair(b,a) = %v, CanonicalPair(a,b) = %v", got1, got2)
	}
}

// TestConnectPairs_SourceNeverItsOwnCandidate proves R4.2: source is
// excluded from its own candidate list even when fused's own recall result
// includes it.
func TestConnectPairs_SourceNeverItsOwnCandidate(t *testing.T) {
	fused := []recall.FusedCandidate{
		{ID: "source-unit", Score: 1.0},
		{ID: "other", Score: 0.5},
	}

	got := ConnectPairs("source-unit", fused, map[Pair]bool{})
	for _, p := range got {
		if p.To == "source-unit" {
			t.Fatalf("ConnectPairs(%q, ...) includes the source as its own candidate: %v", "source-unit", got)
		}
	}
	if len(got) != 1 || got[0] != (Pair{From: "source-unit", To: "other"}) {
		t.Fatalf("ConnectPairs() = %v, want exactly [{source-unit other}]", got)
	}
}

// TestConnectPairs_ExistingExcludesByCanonicalPairRegardlessOfStoredDirection
// proves R4.2: a candidate already related to source is excluded whether
// existing stored it as source->candidate or candidate->source — the
// lookup key is CanonicalPair, not the stored direction.
func TestConnectPairs_ExistingExcludesByCanonicalPairRegardlessOfStoredDirection(t *testing.T) {
	fused := []recall.FusedCandidate{
		{ID: "already-a-to-b"},
		{ID: "already-b-to-a"},
		{ID: "unjudged"},
	}

	existing := map[Pair]bool{
		CanonicalPair("source", "already-a-to-b"): true,
		CanonicalPair("already-b-to-a", "source"): true,
	}

	got := ConnectPairs("source", fused, existing)
	want := []Pair{{From: "source", To: "unjudged"}}
	if len(got) != len(want) || got[0] != want[0] {
		t.Fatalf("ConnectPairs() = %v, want exactly %v", got, want)
	}
}

// TestConnectPairs_CappedAtConnectCandidateK proves R4.2: the result never
// holds more than ConnectCandidateK pairs.
func TestConnectPairs_CappedAtConnectCandidateK(t *testing.T) {
	total := ConnectCandidateK + 3
	fused := make([]recall.FusedCandidate, total)
	for i := 0; i < total; i++ {
		fused[i] = recall.FusedCandidate{ID: idFor(i), Score: float64(total - i)}
	}

	got := ConnectPairs("source", fused, map[Pair]bool{})
	if len(got) != ConnectCandidateK {
		t.Fatalf("ConnectPairs() returned %d pairs, want exactly ConnectCandidateK (%d)", len(got), ConnectCandidateK)
	}
}

// TestConnectPairs_PreservesFusedOrder proves R4.2: ConnectPairs does not
// re-rank fused — the returned pairs follow fused's own order.
func TestConnectPairs_PreservesFusedOrder(t *testing.T) {
	fused := []recall.FusedCandidate{
		{ID: "third", Score: 0.1},
		{ID: "first", Score: 0.9},
		{ID: "second", Score: 0.5},
	}

	got := ConnectPairs("source", fused, map[Pair]bool{})
	want := []Pair{
		{From: "source", To: "third"},
		{From: "source", To: "first"},
		{From: "source", To: "second"},
	}
	if len(got) != len(want) {
		t.Fatalf("ConnectPairs() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("position %d: got %v, want %v", i, got[i], want[i])
		}
	}
}

func ptrOutcome(o relation.Outcome) *relation.Outcome { return &o }
func ptrString(s string) *string                      { return &s }
func ptrFloat(f float64) *float64                     { return &f }

// completeJudgment returns a relation.Judgment with every pointer field
// present, so a test can nil out exactly one at a time.
func completeJudgment(outcome relation.Outcome, confidence float64) relation.Judgment {
	return relation.Judgment{
		Outcome:      ptrOutcome(outcome),
		TargetUnitID: ptrString("target-unit"),
		Type:         ptrString("same_topic"),
		Strength:     ptrFloat(0.5),
		Confidence:   ptrFloat(confidence),
	}
}

func connectPairsThresholds() relation.Thresholds {
	return relation.Thresholds{Persist: 0.30, Surface: 0.50}
}

// TestProposeRelation_OutcomeNewWritesNothing proves R4.3: outcome "new"
// returns (_, false) regardless of every other field being present.
func TestProposeRelation_OutcomeNewWritesNothing(t *testing.T) {
	j := completeJudgment(relation.OutcomeNew, 0.90)

	_, ok := ProposeRelation("source-unit", j, connectPairsThresholds())
	if ok {
		t.Fatal("ProposeRelation() returned true for outcome \"new\" — a judgment with this outcome decided nothing")
	}
}

// TestProposeRelation_DiscardWritesNothing proves R4.3 (I08): a
// relation.Discard verdict returns (_, false), even with every field
// present.
func TestProposeRelation_DiscardWritesNothing(t *testing.T) {
	j := completeJudgment(relation.OutcomeDuplicate, 0.10) // below Persist (0.30) -> Discard

	_, ok := ProposeRelation("source-unit", j, connectPairsThresholds())
	if ok {
		t.Fatal("ProposeRelation() returned true for a relation.Discard verdict — I08 requires it store nothing")
	}
}

// TestProposeRelation_EachMissingFieldWritesNothing proves R4.3: any of
// the four pointer fields being nil after tolerant decode returns
// (_, false), tested individually.
func TestProposeRelation_EachMissingFieldWritesNothing(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*relation.Judgment)
	}{
		{"TargetUnitID", func(j *relation.Judgment) { j.TargetUnitID = nil }},
		{"Type", func(j *relation.Judgment) { j.Type = nil }},
		{"Strength", func(j *relation.Judgment) { j.Strength = nil }},
		{"Confidence", func(j *relation.Judgment) { j.Confidence = nil }},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			j := completeJudgment(relation.OutcomeDuplicate, 0.90)
			tc.mutate(&j)

			_, ok := ProposeRelation("source-unit", j, connectPairsThresholds())
			if ok {
				t.Fatalf("ProposeRelation() returned true with %s nil — a judgment missing a field decided nothing", tc.name)
			}
		})
	}
}

// TestProposeRelation_UncertainAndAssertedWriteAPlan proves R4.3: both
// relation.Uncertain and relation.Asserted, with every field present,
// return (_, true) — the Uncertain band is stored AND asked about (I09).
func TestProposeRelation_UncertainAndAssertedWriteAPlan(t *testing.T) {
	tests := []struct {
		name       string
		confidence float64
	}{
		{"Uncertain", 0.40}, // Persist (0.30) <= 0.40 < Surface (0.50)
		{"Asserted", 0.90},  // >= Surface
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			j := completeJudgment(relation.OutcomeDuplicate, tc.confidence)

			got, ok := ProposeRelation("source-unit", j, connectPairsThresholds())
			if !ok {
				t.Fatalf("ProposeRelation() returned false for confidence %v, want true", tc.confidence)
			}
			want := ProposedRelation{
				From:       "source-unit",
				To:         "target-unit",
				Type:       "same_topic",
				Strength:   0.5,
				Confidence: tc.confidence,
				CreatedBy:  relation.CreatedByConsolidation,
			}
			if got != want {
				t.Errorf("ProposeRelation() = %+v, want %+v", got, want)
			}
		})
	}
}

// TestProposeRelation_CreatedByIsAlwaysConsolidation proves R4.3: the
// returned ProposedRelation.CreatedBy is always
// relation.CreatedByConsolidation, never CreatedBySystem or CreatedByUser.
func TestProposeRelation_CreatedByIsAlwaysConsolidation(t *testing.T) {
	j := completeJudgment(relation.OutcomeRelated, 0.90)

	got, ok := ProposeRelation("source-unit", j, connectPairsThresholds())
	if !ok {
		t.Fatal("ProposeRelation() returned false, want true")
	}
	if got.CreatedBy != relation.CreatedByConsolidation {
		t.Errorf("ProposedRelation.CreatedBy = %q, want %q", got.CreatedBy, relation.CreatedByConsolidation)
	}
}

// TestConnectBudgetConstants_ArePinnedToTheirCalibratedValues guards both
// knobs against a wrong VALUE, independently of every shape test in this
// file — C7 and C28's convention (openspec/changes/m2a-weight-focus/tasks.md).
//
// The shape tests here size their fixtures from the constants themselves
// (ConnectSourceLimit+3 sources, ConnectCandidateK+2 candidates), which is
// correct for pinning the bounding LOGIC and structurally incapable of
// noticing a constant moved: a mutated value flows into the fixture exactly
// as it flows into the function under test.
//
// This convention has now failed to transfer four times — focus's seven
// constants (C28), IncompleteExpiryHours in PR 2, StrengthenGain in PR 3,
// and these two — so the reason is restated rather than cross-referenced:
// only a comparison against an independent literal catches a recalibration
// nobody meant to make.
//
// Both are CHOSEN, per doc 02 §13. What the owner actually calibrates is
// their PRODUCT — at most 100 judge calls per night — so the product is
// pinned too: moving either factor while compensating with the other still
// changes the two §13 rows, and both literals here must move with them.
func TestConnectBudgetConstants_ArePinnedToTheirCalibratedValues(t *testing.T) {
	const (
		wantSourceLimit = 20
		wantCandidateK  = 5
		wantBudget      = 100
	)

	if ConnectSourceLimit != wantSourceLimit {
		t.Errorf("ConnectSourceLimit = %d, want %d — doc 02 §13's chosen value; recalibrating means editing the §13 row and this literal together", ConnectSourceLimit, wantSourceLimit)
	}
	if ConnectCandidateK != wantCandidateK {
		t.Errorf("ConnectCandidateK = %d, want %d — doc 02 §13's chosen value; recalibrating means editing the §13 row and this literal together", ConnectCandidateK, wantCandidateK)
	}
	if got := ConnectSourceLimit * ConnectCandidateK; got != wantBudget {
		t.Errorf("ConnectSourceLimit*ConnectCandidateK = %d, want %d — doc 02 §6.4 states the per-night provider cost as this one product", got, wantBudget)
	}
}
