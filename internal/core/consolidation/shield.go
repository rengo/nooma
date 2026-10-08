package consolidation

import "github.com/rengo/nooma/internal/core/selfmodel"

// RouteKind is what derive does with one proposed belief (m4e R12, R13).
type RouteKind string

const (
	// RouteCreate writes the proposal through the topic-key upsert.
	RouteCreate RouteKind = "create"
	// RouteReinforce raises an existing active belief's confidence; the
	// proposal's text is never written.
	RouteReinforce RouteKind = "reinforce"
	// RouteSkipRetired writes nothing: the proposal hits a retired belief.
	RouteSkipRetired RouteKind = "skip_retired"
)

// The Route.Reason values a skip carries. Reinforce and create routes
// carry no reason.
const (
	RouteReasonRetiredTopicKey = "retired_topic_key"
	RouteReasonRetiredSimilar  = "retired_similar"
)

// KeyedBelief is the part of a belief the shield decides from: no text, no
// vector.
type KeyedBelief struct {
	ID       string
	TopicKey string
	Origin   selfmodel.Origin
}

// Route is the decision for one proposal.
type Route struct {
	ProposedIndex int
	Kind          RouteKind
	// BeliefID is the reinforce target, or the retired belief that decided
	// a skip; "" for a create.
	BeliefID string
	// Reason is RouteReasonRetiredTopicKey or RouteReasonRetiredSimilar for
	// a skip, "" otherwise.
	Reason string
	// Similarity is set only for a decision made by a semantic match.
	Similarity float64
}

// RetiredKeyHits returns, per proposal index, the id of the retired belief
// whose topic key the proposal derives. It is rule 1 of RouteProposals and
// needs no vectors, so the caller can tell which proposals still need a
// semantic comparison before it spends a single embedding call.
//
// The store keeps topic_key unique, so two retired beliefs never share a
// key in production. Should the input hold duplicates anyway, the first one
// by input order wins, so the result does not depend on map iteration.
func RetiredKeyHits(proposedKeys []string, retired []KeyedBelief) map[int]string {
	byKey := make(map[string]string, len(retired))
	for _, b := range retired {
		if _, seen := byKey[b.TopicKey]; !seen {
			byKey[b.TopicKey] = b.ID
		}
	}
	hits := make(map[int]string)
	for i, key := range proposedKeys {
		if id, ok := byKey[key]; ok {
			hits[i] = id
		}
	}
	return hits
}

// RouteProposals decides every proposal, in index order. activeMerges and
// retiredMerges are two separate MergeProposals results (one over the
// active vectors, one over the retired ones), each indexed by the ORIGINAL
// proposal index; a proposal decided by key, or whose vector was unusable,
// has no entry in either. They are two calls rather than one over a union
// because MergeProposals keeps only the single nearest neighbour, so over a
// union a tie between an active and a retired belief would be settled by
// search order instead of by the rule below.
//
// First match wins:
//
//  1. the proposal's key is a retired belief's: skip.
//  2. a retired belief is the nearest retired neighbour at or above
//     BeliefMergeCosine, and no active neighbour qualifies or the retired
//     one is at least as near: skip. A tie goes to retired, the user's word
//     wins.
//  3. the key is an active belief whose origin is not derived (the user's
//     own words): reinforce it. This wins even when another active belief is
//     nearer, so a proposal can raise an edited belief's confidence but
//     never rewrite its text.
//  4. an active neighbour qualifies: reinforce it.
//  5. otherwise create. An active DERIVED key collision still overwrites in
//     place (m2c R2.1), which is the one overwrite the store permits.
func RouteProposals(activeMerges, retiredMerges []MergeDecision, proposedKeys []string, active, retired []KeyedBelief) []Route {
	hits := RetiredKeyHits(proposedKeys, retired)
	activeByIndex := qualifyingByIndex(activeMerges)
	retiredByIndex := qualifyingByIndex(retiredMerges)
	nonDerivedByKey := make(map[string]string, len(active))
	for _, b := range active {
		if b.Origin != selfmodel.OriginDerived {
			nonDerivedByKey[b.TopicKey] = b.ID
		}
	}

	routes := make([]Route, len(proposedKeys))
	for i, key := range proposedKeys {
		routes[i] = routeOne(i, key, hits, activeByIndex, retiredByIndex, nonDerivedByKey)
	}
	return routes
}

func routeOne(i int, key string, hits map[int]string, activeByIndex, retiredByIndex map[int]MergeDecision, nonDerivedByKey map[string]string) Route {
	if id, ok := hits[i]; ok {
		return Route{ProposedIndex: i, Kind: RouteSkipRetired, BeliefID: id, Reason: RouteReasonRetiredTopicKey}
	}
	act, activeQualifies := activeByIndex[i]
	ret, retiredQualifies := retiredByIndex[i]
	if retiredQualifies && (!activeQualifies || ret.Similarity >= act.Similarity) {
		return Route{ProposedIndex: i, Kind: RouteSkipRetired, BeliefID: ret.MergeInto, Reason: RouteReasonRetiredSimilar, Similarity: ret.Similarity}
	}
	if id, ok := nonDerivedByKey[key]; ok {
		return Route{ProposedIndex: i, Kind: RouteReinforce, BeliefID: id}
	}
	if activeQualifies {
		return Route{ProposedIndex: i, Kind: RouteReinforce, BeliefID: act.MergeInto, Similarity: act.Similarity}
	}
	return Route{ProposedIndex: i, Kind: RouteCreate}
}

// qualifyingByIndex keeps the decisions that name a neighbour, by proposal
// index. MergeInto == "" is MergeProposals's "no neighbour at the cosine".
func qualifyingByIndex(ds []MergeDecision) map[int]MergeDecision {
	out := make(map[int]MergeDecision, len(ds))
	for _, d := range ds {
		if d.MergeInto != "" {
			out[d.ProposedIndex] = d
		}
	}
	return out
}
