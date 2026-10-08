package consolidation

import (
	"reflect"
	"testing"

	"github.com/rengo/nooma/internal/core/selfmodel"
)

// The FX-D fixture (design §6), expressed as MergeProposals results: the
// shield decides from keys, origins and two nearest-neighbour results, so
// no vector is needed at this level. Proposal indices are the ORIGINAL ones;
// an index decided by key has no entry in either merge list.
var (
	shieldRetired = []KeyedBelief{
		{ID: "r1", TopicKey: "derived/goal/marathon", Origin: selfmodel.OriginDerived},
		{ID: "r2", TopicKey: "derived/goal/swim", Origin: selfmodel.OriginDerived},
		{ID: "r3", TopicKey: "derived/goal/cycle", Origin: selfmodel.OriginDerived},
	}
	shieldActive = []KeyedBelief{
		{ID: "a", TopicKey: "derived/value/health", Origin: selfmodel.OriginDerived},
		{ID: "a2", TopicKey: "derived/value/sleep", Origin: selfmodel.OriginDerived},
		{ID: "u", TopicKey: "derived/goal/read", Origin: selfmodel.OriginUserStated},
		{ID: "d", TopicKey: "derived/goal/code", Origin: selfmodel.OriginDerived},
		{ID: "s", TopicKey: "derived/identity/seed", Origin: selfmodel.OriginSeed},
	}
)

func merge(i int, into string, sim float64) MergeDecision {
	return MergeDecision{ProposedIndex: i, MergeInto: into, Similarity: sim}
}

func route(t *testing.T, activeMerges, retiredMerges []MergeDecision, keys []string) []Route {
	t.Helper()
	return RouteProposals(activeMerges, retiredMerges, keys, shieldActive, shieldRetired)
}

func wantRoute(t *testing.T, got []Route, i int, want Route) {
	t.Helper()
	if i >= len(got) {
		t.Fatalf("routes has %d entries, want one at index %d: %+v", len(got), i, got)
	}
	want.ProposedIndex = i
	if got[i] != want {
		t.Errorf("route[%d] = %+v, want %+v", i, got[i], want)
	}
}

// p0: a fresh key far from everything is created (the control).
func TestRouteProposals_FarFreshKeyIsCreated(t *testing.T) {
	got := route(t, []MergeDecision{merge(0, "", 0)}, nil, []string{"derived/goal/fresh"})
	if len(got) != 1 {
		t.Fatalf("len(routes) = %d, want 1: %+v", len(got), got)
	}
	wantRoute(t, got, 0, Route{Kind: RouteCreate})
}

// p1 (D1): a proposal deriving a retired belief's key is skipped even when
// no neighbour is near it. Only rule 1 can catch it.
func TestRouteProposals_RetiredKeySkipsEvenWhenFar(t *testing.T) {
	got := route(t, nil, nil, []string{"derived/goal/fresh", "derived/goal/marathon"})
	wantRoute(t, got, 0, Route{Kind: RouteCreate})
	wantRoute(t, got, 1, Route{Kind: RouteSkipRetired, BeliefID: "r1", Reason: RouteReasonRetiredTopicKey})
}

// D6: rule 1 outranks the active-nearest rule. The key names retired r1
// while an active belief is the nearest neighbour.
func TestRouteProposals_RetiredKeyOutranksActiveNearest(t *testing.T) {
	got := route(t, []MergeDecision{merge(0, "a", 0.99)}, nil, []string{"derived/goal/marathon"})
	wantRoute(t, got, 0, Route{Kind: RouteSkipRetired, BeliefID: "r1", Reason: RouteReasonRetiredTopicKey})
}

// Rule 1 also outranks rule 2: the key names r1 while the semantic match is
// a different retired belief. The skip names the belief the KEY hit.
func TestRouteProposals_RetiredKeyOutranksRetiredNearest(t *testing.T) {
	got := route(t, nil, []MergeDecision{merge(0, "r2", 0.97)}, []string{"derived/goal/marathon"})
	wantRoute(t, got, 0, Route{Kind: RouteSkipRetired, BeliefID: "r1", Reason: RouteReasonRetiredTopicKey})
}

// p2 (D2): new key, retired r2 at 0.95 and active a at 0.90 -> skip.
func TestRouteProposals_RetiredNearestSkips(t *testing.T) {
	got := route(t, []MergeDecision{merge(0, "a", 0.90)}, []MergeDecision{merge(0, "r2", 0.95)}, []string{"derived/goal/new"})
	wantRoute(t, got, 0, Route{Kind: RouteSkipRetired, BeliefID: "r2", Reason: RouteReasonRetiredSimilar, Similarity: 0.95})
}

// Retired qualifies and no active neighbour does: still a skip.
func TestRouteProposals_RetiredNearestSkipsWithNoActiveNeighbour(t *testing.T) {
	got := route(t, []MergeDecision{merge(0, "", 0)}, []MergeDecision{merge(0, "r3", 0.86)}, []string{"derived/goal/new"})
	wantRoute(t, got, 0, Route{Kind: RouteSkipRetired, BeliefID: "r3", Reason: RouteReasonRetiredSimilar, Similarity: 0.86})
}

// p3 (D3): active a at 0.95, retired r2 at 0.90 -> reinforce a. An active
// belief that is strictly nearer is the proposal's belief; reinforcing it
// revives nothing.
func TestRouteProposals_NearerActiveBeatsQualifyingRetired(t *testing.T) {
	got := route(t, []MergeDecision{merge(0, "a", 0.95)}, []MergeDecision{merge(0, "r2", 0.90)}, []string{"derived/goal/new"})
	wantRoute(t, got, 0, Route{Kind: RouteReinforce, BeliefID: "a", Similarity: 0.95})
}

// p4 (D4): the key of a user-stated belief reinforces it, whatever the
// content is like.
func TestRouteProposals_UserStatedKeyReinforces(t *testing.T) {
	got := route(t, []MergeDecision{merge(0, "", 0)}, nil, []string{"derived/goal/read"})
	wantRoute(t, got, 0, Route{Kind: RouteReinforce, BeliefID: "u"})
}

// A seed belief is the user's word too: any origin but derived reinforces.
func TestRouteProposals_SeedKeyReinforces(t *testing.T) {
	got := route(t, nil, nil, []string{"derived/identity/seed"})
	wantRoute(t, got, 0, Route{Kind: RouteReinforce, BeliefID: "s"})
}

// p5 (D5): the key of an active DERIVED belief is created, i.e. overwritten
// in place by the store (m2c control).
func TestRouteProposals_ActiveDerivedKeyIsCreated(t *testing.T) {
	got := route(t, []MergeDecision{merge(0, "", 0)}, nil, []string{"derived/goal/code"})
	wantRoute(t, got, 0, Route{Kind: RouteCreate})
}

// p6 (D13): exactly equal similarity to a retired and an active belief goes
// to retired. Both ties and one-ulp-off values are pinned, so >= cannot
// become > or the reverse.
func TestRouteProposals_TieGoesToRetired(t *testing.T) {
	got := route(t, []MergeDecision{merge(0, "a2", 0.9)}, []MergeDecision{merge(0, "r3", 0.9)}, []string{"derived/goal/new"})
	wantRoute(t, got, 0, Route{Kind: RouteSkipRetired, BeliefID: "r3", Reason: RouteReasonRetiredSimilar, Similarity: 0.9})
}

func TestRouteProposals_RetiredJustBelowActiveReinforcesActive(t *testing.T) {
	got := route(t, []MergeDecision{merge(0, "a2", 0.9000001)}, []MergeDecision{merge(0, "r3", 0.9)}, []string{"derived/goal/new"})
	wantRoute(t, got, 0, Route{Kind: RouteReinforce, BeliefID: "a2", Similarity: 0.9000001})
}

// p7 (D23): the key of user-stated u wins over a nearer active neighbour a.
func TestRouteProposals_UserStatedKeyOutranksNearerActive(t *testing.T) {
	got := route(t, []MergeDecision{merge(0, "a", 0.95)}, nil, []string{"derived/goal/read"})
	wantRoute(t, got, 0, Route{Kind: RouteReinforce, BeliefID: "u"})
}

// Rule 2 outranks rule 3: a retired belief that is at least as near as the
// active one skips, even when the proposal's key is a user-stated belief's.
// (The user retired a near-duplicate of u's text; u's own key is not a
// licence to bring it back.)
func TestRouteProposals_RetiredNearestOutranksUserStatedKey(t *testing.T) {
	got := route(t, nil, []MergeDecision{merge(0, "r1", 0.92)}, []string{"derived/goal/read"})
	wantRoute(t, got, 0, Route{Kind: RouteSkipRetired, BeliefID: "r1", Reason: RouteReasonRetiredSimilar, Similarity: 0.92})
}

// FX-D in one call, retired-key proposal NOT first, merges indexed by the
// original proposal index: every route lands on its own proposal.
func TestRouteProposals_FXD_MixedBatchKeepsEachIndex(t *testing.T) {
	keys := []string{
		"derived/goal/fresh",     // p0 create
		"derived/goal/marathon",  // p1 retired key
		"derived/goal/new2",      // p2 retired similar
		"derived/goal/new3",      // p3 reinforce a
		"derived/goal/read",      // p4 reinforce u
		"derived/goal/code",      // p5 create (derived key)
		"derived/goal/new6",      // p6 tie -> retired
		"derived/goal/read-copy", // p7 placeholder: far, create
	}
	activeMerges := []MergeDecision{
		merge(0, "", 0), merge(2, "a", 0.90), merge(3, "a", 0.95), merge(4, "", 0),
		merge(5, "", 0), merge(6, "a2", 0.9), merge(7, "", 0),
	}
	retiredMerges := []MergeDecision{
		merge(0, "", 0), merge(2, "r2", 0.95), merge(3, "r2", 0.90), merge(4, "", 0),
		merge(5, "", 0), merge(6, "r3", 0.9), merge(7, "", 0),
	}

	got := route(t, activeMerges, retiredMerges, keys)

	if len(got) != len(keys) {
		t.Fatalf("len(routes) = %d, want %d", len(got), len(keys))
	}
	wantRoute(t, got, 0, Route{Kind: RouteCreate})
	wantRoute(t, got, 1, Route{Kind: RouteSkipRetired, BeliefID: "r1", Reason: RouteReasonRetiredTopicKey})
	wantRoute(t, got, 2, Route{Kind: RouteSkipRetired, BeliefID: "r2", Reason: RouteReasonRetiredSimilar, Similarity: 0.95})
	wantRoute(t, got, 3, Route{Kind: RouteReinforce, BeliefID: "a", Similarity: 0.95})
	wantRoute(t, got, 4, Route{Kind: RouteReinforce, BeliefID: "u"})
	wantRoute(t, got, 5, Route{Kind: RouteCreate})
	wantRoute(t, got, 6, Route{Kind: RouteSkipRetired, BeliefID: "r3", Reason: RouteReasonRetiredSimilar, Similarity: 0.9})
	wantRoute(t, got, 7, Route{Kind: RouteCreate})
}

func TestRouteProposals_NoProposalsNoRoutes(t *testing.T) {
	got := route(t, nil, nil, nil)
	if len(got) != 0 {
		t.Fatalf("routes = %+v, want none for no proposals", got)
	}
}

func TestRetiredKeyHits(t *testing.T) {
	keys := []string{"derived/goal/fresh", "derived/goal/cycle", "derived/goal/read", "derived/goal/marathon"}

	got := RetiredKeyHits(keys, shieldRetired)

	want := map[int]string{1: "r3", 3: "r1"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("RetiredKeyHits = %v, want %v (a user-stated ACTIVE key is not a retired hit)", got, want)
	}
	if got := RetiredKeyHits(keys, nil); len(got) != 0 {
		t.Errorf("RetiredKeyHits with no retired beliefs = %v, want none", got)
	}
}

// The store keeps topic_key UNIQUE (migration 0001), so two retired beliefs
// sharing a key cannot occur in production. The pick is still pinned so the
// function stays deterministic on any input: the first by input order wins.
func TestRetiredKeyHits_DuplicateRetiredKeyPicksFirstByInputOrder(t *testing.T) {
	retired := []KeyedBelief{
		{ID: "first", TopicKey: "derived/goal/dup", Origin: selfmodel.OriginDerived},
		{ID: "second", TopicKey: "derived/goal/dup", Origin: selfmodel.OriginDerived},
	}

	got := RetiredKeyHits([]string{"derived/goal/dup"}, retired)

	want := map[int]string{0: "first"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("RetiredKeyHits = %v, want %v (first retired belief by input order)", got, want)
	}
}

// RouteKind is a closed set and its strings are what a reader of the code
// sees: one batch must produce each member, so a member nothing can reach,
// or a renamed one, shows.
func TestRouteKind_EveryKindIsProducedByARealBatch(t *testing.T) {
	got := route(t,
		[]MergeDecision{merge(0, "", 0), merge(1, "a", 0.95)},
		nil,
		[]string{"derived/goal/fresh", "derived/goal/new", "derived/goal/marathon"},
	)

	var kinds []RouteKind
	for _, r := range got {
		kinds = append(kinds, r.Kind)
	}
	want := []RouteKind{RouteCreate, RouteReinforce, RouteSkipRetired}
	if !reflect.DeepEqual(kinds, want) {
		t.Fatalf("kinds = %v, want %v", kinds, want)
	}
	if string(RouteCreate) != "create" || string(RouteReinforce) != "reinforce" || string(RouteSkipRetired) != "skip_retired" {
		t.Errorf("route kind strings = %q, %q, %q, want create, reinforce, skip_retired", RouteCreate, RouteReinforce, RouteSkipRetired)
	}
}
