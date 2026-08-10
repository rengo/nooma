package memrepo

import (
	"context"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/core/consolidation"
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
	// lastTouchedAt holds each unit id's last_touched_at, set via
	// SetLastTouchedAt — Evidence's join reads this per endpoint. This fake
	// tracks no other unit column: it exists only to answer this one join,
	// not to duplicate memrepo.Units.
	lastTouchedAt map[string]time.Time
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
		byKey:         make(map[relationKey]ports.Relation),
		thresholds:    make(map[string]relation.Thresholds),
		lastTouchedAt: make(map[string]time.Time),
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

// SetLastTouchedAt implements repocontract.RelationHarness. Records id's
// last_touched_at for Evidence's join to read — the fake's only unit-side
// bookkeeping, since memrepo.Relations otherwise tracks no unit data at
// all.
func (r *Relations) SetLastTouchedAt(_ *testing.T, id string, at time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lastTouchedAt[id] = at
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

// Evidence implements ports.RelationRepo. Joins every stored relation to
// both endpoints' last_touched_at, read from r.lastTouchedAt (set via
// SetLastTouchedAt) — an endpoint never seeded reports the zero time.Time,
// matching what an ordinary SQL LEFT JOIN would return for a genuinely
// missing row (this fake enforces no foreign key, so the case cannot arise
// from a stored relation, but the zero value is still the honest answer for
// an endpoint the harness never told this fake about).
func (r *Relations) Evidence(_ context.Context) ([]consolidation.RelationEvidence, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	var out []consolidation.RelationEvidence
	for _, rel := range r.byKey {
		out = append(out, consolidation.RelationEvidence{
			RelationID:        rel.ID,
			FromUnitID:        rel.FromUnitID,
			ToUnitID:          rel.ToUnitID,
			Type:              rel.Type,
			Strength:          rel.Strength,
			Confidence:        rel.Confidence,
			FromLastTouchedAt: r.lastTouchedAt[rel.FromUnitID],
			ToLastTouchedAt:   r.lastTouchedAt[rel.ToUnitID],
		})
	}
	return out, nil
}

// ExistingPairs implements ports.RelationRepo. Keyed by
// consolidation.CanonicalPair, over the candidate set pairs names only — a
// pair with no stored relation is left absent from the returned map, never
// set to false, so a caller cannot mistake "never checked" for "checked and
// absent" from the zero value.
func (r *Relations) ExistingPairs(_ context.Context, pairs []consolidation.Pair) (map[consolidation.Pair]bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	stored := make(map[consolidation.Pair]bool, len(r.byKey))
	for _, rel := range r.byKey {
		stored[consolidation.CanonicalPair(rel.FromUnitID, rel.ToUnitID)] = true
	}

	out := make(map[consolidation.Pair]bool, len(pairs))
	for _, p := range pairs {
		if stored[p] {
			out[p] = true
		}
	}
	return out, nil
}
