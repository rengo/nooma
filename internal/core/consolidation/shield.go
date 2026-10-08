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
func RetiredKeyHits(proposedKeys []string, retired []KeyedBelief) map[int]string {
	return nil
}

// RouteProposals decides every proposal, in index order.
func RouteProposals(activeMerges, retiredMerges []MergeDecision, proposedKeys []string, active, retired []KeyedBelief) []Route {
	routes := make([]Route, len(proposedKeys))
	for i := range proposedKeys {
		routes[i] = Route{ProposedIndex: i, Kind: RouteCreate}
	}
	return routes
}
