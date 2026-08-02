package memrepo

import (
	"context"
	"sort"
	"sync"
	"testing"

	"github.com/rengo/nooma/internal/core/relation"
	"github.com/rengo/nooma/internal/ports"
)

// Relations is an in-memory ports.RelationRepo. The zero value is not usable
// — call NewRelations. Two instances share no state, matching memrepo.Units's
// own isolation rule.
type Relations struct {
	mu sync.Mutex
	// byKey is keyed on the same triple relations.UNIQUE(from_unit_id,
	// to_unit_id, type) constrains — the fake's own map key mirrors the real
	// schema's uniqueness rather than inventing a different one (C11's
	// lesson: a fake keyed on the wrong thing passes every case while being
	// unimplementable over the real schema).
	byKey map[relationKey]ports.Relation
	// thresholds holds relation_thresholds rows, keyed by relation_type —
	// the column's own UNIQUE constraint (migration 0002:32).
	thresholds map[string]relation.Thresholds
}

type relationKey struct {
	from, to, relType string
}

// Assert at compile time, following internal/store/sqlite/unitrepo.go:33's
// precedent.
var _ ports.RelationRepo = (*Relations)(nil)

// NewRelations returns an empty, ready-to-use in-memory ports.RelationRepo.
// Every call returns an independent instance.
func NewRelations() *Relations {
	return &Relations{
		byKey:      make(map[relationKey]ports.Relation),
		thresholds: make(map[string]relation.Thresholds),
	}
}

// EnsureUnit implements repocontract.RelationHarness. It does nothing: the
// fake enforces no foreign key, so every unit id is already a valid
// endpoint. The method exists so one suite can drive both implementations —
// see repocontract.RelationHarness for why the store needs it.
func (r *Relations) EnsureUnit(_ *testing.T, _ string) {}

// SeedThreshold implements repocontract.RelationHarness, bypassing Upsert
// entirely — there is no write method for relation_thresholds on
// ports.RelationRepo (design D8 names only three methods, and a fourth
// write method with no caller but this suite is the shape m1a-substrate D7
// rejected for TranscriptionProvider).
func (r *Relations) SeedThreshold(_ *testing.T, relType string, persist, surface float64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.thresholds[relType] = relation.Thresholds{Persist: persist, Surface: surface}
}

// Upsert implements ports.RelationRepo.
//
// Keyed on (FromUnitID, ToUnitID, Type) — I07. When a row already exists for
// that key, only Strength and Confidence are replaced; ID, CreatedBy and
// CreatedAt keep the first Upsert's values, exactly what the SQLite
// implementation's ON CONFLICT ... DO UPDATE SET strength = excluded.strength,
// confidence = excluded.confidence leaves untouched.
func (r *Relations) Upsert(_ context.Context, rel ports.Relation) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	key := relationKey{rel.FromUnitID, rel.ToUnitID, rel.Type}
	if existing, ok := r.byKey[key]; ok {
		existing.Strength = rel.Strength
		existing.Confidence = rel.Confidence
		r.byKey[key] = existing
		return nil
	}
	r.byKey[key] = rel
	return nil
}

// ByUnit implements ports.RelationRepo.
//
// Ordered by CreatedAt then ID — ports.RelationRepo.ByUnit's own documented
// tie-break, mirrored here rather than left to map-iteration order, which Go
// deliberately randomizes.
func (r *Relations) ByUnit(_ context.Context, unitID string) ([]ports.Relation, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	out := make([]ports.Relation, 0, len(r.byKey))
	for _, rel := range r.byKey {
		if rel.FromUnitID == unitID || rel.ToUnitID == unitID {
			out = append(out, rel)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].CreatedAt.Before(out[j].CreatedAt)
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}

// ThresholdsFor implements ports.RelationRepo. It returns (nil, nil) when no
// row has been seeded for relType — design D8, R5.6's fallback origin.
func (r *Relations) ThresholdsFor(_ context.Context, relType string) (*relation.Thresholds, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	t, ok := r.thresholds[relType]
	if !ok {
		return nil, nil
	}
	return &t, nil
}
