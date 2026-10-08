package memrepo

import (
	"context"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/rengo/nooma/internal/core/selfmodel"
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
		if b.Status == selfmodel.StatusActive {
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
		// The store guard, mirrored: only an active, derived row is
		// overwritten.
		if existing := s.byID[id]; existing.Status != selfmodel.StatusActive || existing.Origin != selfmodel.OriginDerived {
			return ports.ErrBeliefProtected
		}
		b.ID = id
		s.byID[id] = b
		return nil
	}
	// The primary key, mirrored: SQLite refuses an insert whose id is
	// already held under another topic_key, and so does this fake.
	if _, taken := s.byID[b.ID]; taken {
		return fmt.Errorf("upserting belief for topic_key %q: id %q is already held under another topic_key", b.TopicKey, b.ID)
	}
	s.byTopicKey[b.TopicKey] = b.ID
	s.byID[b.ID] = b
	return nil
}

// ReinforceByID implements ports.SelfModelRepo. Updates only Confidence
// and LastReinforcedAt for the belief named by id, leaving every other
// field unchanged. Returns ports.ErrBeliefNotFound rather than creating a
// row when id does not exist (spec R2.2), and ports.ErrBeliefStatusConflict
// for a belief that is not active.
func (s *SelfModel) ReinforceByID(_ context.Context, id string, confidence float64, at time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	existing, ok := s.byID[id]
	if !ok {
		return ports.ErrBeliefNotFound
	}
	if existing.Status != selfmodel.StatusActive {
		return ports.ErrBeliefStatusConflict
	}
	existing.Confidence = confidence
	existing.LastReinforcedAt = at
	s.byID[id] = existing
	return nil
}

// RetiredBeliefs implements ports.SelfModelRepo. Returns every belief whose
// Status is "retired", every facet included.
func (s *SelfModel) RetiredBeliefs(_ context.Context) ([]ports.Belief, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var out []ports.Belief
	for _, b := range s.byID {
		if b.Status == selfmodel.StatusRetired {
			out = append(out, b)
		}
	}
	return out, nil
}

// BeliefByID implements ports.SelfModelRepo. Any status.
func (s *SelfModel) BeliefByID(_ context.Context, id string) (ports.Belief, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	b, ok := s.byID[id]
	if !ok {
		return ports.Belief{}, ports.ErrBeliefNotFound
	}
	return b, nil
}

// SetStatus implements ports.SelfModelRepo. A from or to outside
// selfmodel.AllStatuses() is ports.ErrBeliefStatusInvalid; from is a
// precondition: a belief not currently in from is
// ports.ErrBeliefStatusConflict and nothing is written.
func (s *SelfModel) SetStatus(_ context.Context, id string, from, to selfmodel.Status, at time.Time) error {
	known := selfmodel.AllStatuses()
	if !slices.Contains(known, from) || !slices.Contains(known, to) {
		return fmt.Errorf("belief %q status %q -> %q: %w", id, from, to, ports.ErrBeliefStatusInvalid)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	existing, ok := s.byID[id]
	if !ok {
		return ports.ErrBeliefNotFound
	}
	if existing.Status != from {
		return ports.ErrBeliefStatusConflict
	}
	existing.Status = to
	existing.UpdatedAt = at
	s.byID[id] = existing
	return nil
}

// EditContent implements ports.SelfModelRepo. Only an active belief whose
// content is still from is written; the write marks it user_stated and
// moves nothing but content, origin and UpdatedAt.
func (s *SelfModel) EditContent(_ context.Context, id, from, to string, at time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	existing, ok := s.byID[id]
	if !ok {
		return ports.ErrBeliefNotFound
	}
	if existing.Status != selfmodel.StatusActive || existing.Content != from {
		return ports.ErrBeliefStatusConflict
	}
	existing.Content = to
	existing.Origin = selfmodel.OriginUserStated
	existing.UpdatedAt = at
	s.byID[id] = existing
	return nil
}
