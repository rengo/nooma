package brain

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/rengo/nooma/internal/core/selfmodel"
	"github.com/rengo/nooma/internal/ports"
)

// BeliefsService is the mirror's belief read model and its two user
// writes (m4e design §3.4): list the active beliefs by facet, edit one's
// content, retire one. One file per operation, one clock read each
// (belief_edit.go, belief_retire.go), so ByFacet, which has no clock,
// lives here.
type BeliefsService struct {
	clock   ports.Clock
	ids     ports.IDGen
	beliefs ports.SelfModelRepo
	signals ports.SignalRepo
	log     ports.DecisionLog
}

// NewBeliefsService wires a BeliefsService over the ports its three
// operations need.
func NewBeliefsService(clock ports.Clock, ids ports.IDGen, beliefs ports.SelfModelRepo, signals ports.SignalRepo, log ports.DecisionLog) *BeliefsService {
	return &BeliefsService{clock: clock, ids: ids, beliefs: beliefs, signals: signals, log: log}
}

// FacetBeliefs is one facet's active beliefs, in display order.
type FacetBeliefs struct {
	Facet   selfmodel.Facet
	Beliefs []ports.Belief
}

// ByFacet returns the active beliefs grouped under all five facets, in
// selfmodel.AllFacets order. A facet with no active belief is present and
// empty. Within a facet the order is total: confidence descending, then
// last_reinforced_at descending, then id ascending.
//
// The sort lives here, not in the store: ActiveBeliefs has no ORDER BY in
// SQLite and iterates a map in memrepo, so without it the page order would
// depend on the storage. A belief whose facet is outside the closed
// vocabulary (decode never produces one) is not listed.
func (s *BeliefsService) ByFacet(ctx context.Context) ([]FacetBeliefs, error) {
	active, err := s.beliefs.ActiveBeliefs(ctx)
	if err != nil {
		return nil, fmt.Errorf("beliefs by facet: read active beliefs: %w", err)
	}
	grouped := make(map[selfmodel.Facet][]ports.Belief, len(selfmodel.AllFacets()))
	for _, b := range active {
		grouped[b.Facet] = append(grouped[b.Facet], b)
	}

	groups := make([]FacetBeliefs, 0, len(selfmodel.AllFacets()))
	for _, facet := range selfmodel.AllFacets() {
		bs := grouped[facet]
		if bs == nil {
			bs = []ports.Belief{}
		}
		slices.SortStableFunc(bs, func(a, b ports.Belief) int {
			return cmp.Or(
				cmp.Compare(b.Confidence, a.Confidence),
				b.LastReinforcedAt.Compare(a.LastReinforcedAt),
				strings.Compare(a.ID, b.ID),
			)
		})
		groups = append(groups, FacetBeliefs{Facet: facet, Beliefs: bs})
	}
	return groups, nil
}

// recordBeliefSignal writes the learning signal of a user's write on b
// (doc 02 §9). The caller passes the valence: negative for a retirement or a
// content edit (the belief was wrong in the user's eyes, as for a
// correction), positive for a claim (the user kept the derived text, so the
// system derived it right). DecisionAction names the bucket that produced the
// belief, and only when that was derive: a seeded or user-stated belief
// came from no decision_log bucket, and nil beats a guess. decisionID links
// the log row and is omitted when the row could not be written.
func (s *BeliefsService) recordBeliefSignal(ctx context.Context, typ ports.SignalType, valence ports.Valence, b ports.Belief, decisionID string, now time.Time) error {
	contextJSON, err := json.Marshal(struct {
		BeliefID   string `json:"belief_id"`
		TopicKey   string `json:"topic_key"`
		DecisionID string `json:"decision_id,omitempty"`
	}{BeliefID: b.ID, TopicKey: b.TopicKey, DecisionID: decisionID})
	if err != nil {
		return fmt.Errorf("encode signal context for belief %q: %w", b.ID, err)
	}

	targetKind := ports.TargetKindBelief
	sig := ports.Signal{
		ID:         s.ids.New(),
		Type:       typ,
		Valence:    valence,
		TargetKind: &targetKind,
		TargetID:   &b.ID,
		Context:    contextJSON,
		OccurredAt: now,
	}
	if b.Origin == selfmodel.OriginDerived {
		bucket := ports.ActionDeriveBeliefCreated
		sig.DecisionAction = &bucket
	}
	if err := s.signals.Record(ctx, sig); err != nil {
		return fmt.Errorf("record %s signal for belief %q: %w", typ, b.ID, err)
	}
	return nil
}
