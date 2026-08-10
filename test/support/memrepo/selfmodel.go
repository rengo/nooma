package memrepo

import (
	"context"
	"sync"
	"time"

	"github.com/rengo/nooma/internal/ports"
)

// SelfModel is an in-memory ports.SelfModelRepo. The zero value is not
// usable — call NewSelfModel. Two instances share no state, matching
// memrepo.Units's own isolation rule.
type SelfModel struct {
	mu sync.Mutex
	// byID holds every belief, keyed by ID — ReinforceByID's own lookup
	// key.
	byID map[string]ports.Belief
	// byTopicKey indexes byID by TopicKey — self_beliefs.topic_key's
	// UNIQUE constraint (migration 0001:75), UpsertByTopicKey's own
	// conflict target.
	byTopicKey map[string]string // topic_key -> id
}

// Assert at compile time, following internal/store/sqlite/unitrepo.go:33's
// precedent.
var _ ports.SelfModelRepo = (*SelfModel)(nil)

// NewSelfModel returns an empty, ready-to-use in-memory
// ports.SelfModelRepo. Every call returns an independent instance.
func NewSelfModel() *SelfModel {
	return &SelfModel{
		byID:       make(map[string]ports.Belief),
		byTopicKey: make(map[string]string),
	}
}

// ActiveBeliefs implements ports.SelfModelRepo. Returns every belief whose
// Status is "active", every facet included — no status parameter on the
// call itself (spec R2.3).
func (s *SelfModel) ActiveBeliefs(_ context.Context) ([]ports.Belief, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var out []ports.Belief
	for _, b := range s.byID {
		if b.Status == "active" {
			out = append(out, b)
		}
	}
	return out, nil
}

// UpsertByTopicKey implements ports.SelfModelRepo. Conflicts on
// b.TopicKey — a second write for the same TopicKey updates the existing
// row in place, keeping its ID, rather than creating a duplicate (spec
// R2.1).
func (s *SelfModel) UpsertByTopicKey(_ context.Context, b ports.Belief) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if id, ok := s.byTopicKey[b.TopicKey]; ok {
		b.ID = id
		s.byID[id] = b
		return nil
	}
	s.byTopicKey[b.TopicKey] = b.ID
	s.byID[b.ID] = b
	return nil
}

// ReinforceByID implements ports.SelfModelRepo. Updates only Confidence
// and LastReinforcedAt for the belief named by id, leaving every other
// field unchanged. Returns ports.ErrBeliefNotFound rather than creating a
// row when id does not exist (spec R2.2).
func (s *SelfModel) ReinforceByID(_ context.Context, id string, confidence float64, at time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	existing, ok := s.byID[id]
	if !ok {
		return ports.ErrBeliefNotFound
	}
	existing.Confidence = confidence
	existing.LastReinforcedAt = at
	s.byID[id] = existing
	return nil
}
