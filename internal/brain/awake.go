package brain

import (
	"context"
	"errors"
	"fmt"

	"github.com/rengo/nooma/internal/core/relation"
	"github.com/rengo/nooma/internal/ports"
)

// bars resolves relation types' thresholds through the one pair every
// confidence decision reads, ThresholdsFor -> relation.Resolve, once per type
// per read: the same bar decides what is stored, what is confirmable and,
// here, what is dormant (doc 02 §4, I49).
type bars struct {
	rels   ports.RelationRepo
	byType map[string]relation.Thresholds
}

func newBars(rels ports.RelationRepo) *bars {
	return &bars{rels: rels, byType: map[string]relation.Thresholds{}}
}

// of is relType's resolved thresholds.
func (b *bars) of(ctx context.Context, relType string) (relation.Thresholds, error) {
	if t, ok := b.byType[relType]; ok {
		return t, nil
	}
	row, err := b.rels.ThresholdsFor(ctx, relType)
	if err != nil {
		return relation.Thresholds{}, fmt.Errorf("thresholds for relation type %q: %w", relType, err)
	}
	t := relation.Resolve(row)
	b.byType[relType] = t
	return t, nil
}

// dormant reports whether r is below its type's bar (relation.Dormant).
func (b *bars) dormant(ctx context.Context, r ports.Relation) (bool, error) {
	t, err := b.of(ctx, r.Type)
	if err != nil {
		return false, err
	}
	return relation.Dormant(r.Confidence, t), nil
}

// awake is rs without its dormant relations, in rs's order. Nothing is
// written: a dormant relation stays stored and wakes when its bar falls.
func (b *bars) awake(ctx context.Context, rs []ports.Relation) ([]ports.Relation, error) {
	out := make([]ports.Relation, 0, len(rs))
	for _, r := range rs {
		d, err := b.dormant(ctx, r)
		if err != nil {
			return nil, err
		}
		if !d {
			out = append(out, r)
		}
	}
	return out, nil
}

// firstAwakeQuestion is the first of queued whose relation is awake: the one
// question the digest, and Today's preview of it, may ask. A dormant one stays
// queued, unasked, for a bar that falls. With no relation store wired (a
// test) every question is awake.
func firstAwakeQuestion(ctx context.Context, rels ports.RelationRepo, queued []ports.RelationQuestion) (*ports.RelationQuestion, error) {
	if rels == nil {
		if len(queued) == 0 {
			return nil, nil
		}
		return &queued[0], nil
	}
	b := newBars(rels)
	for i := range queued {
		rel, err := rels.ByID(ctx, queued[i].RelationID)
		if errors.Is(err, ports.ErrRelationNotFound) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("relation %q of question %q: %w", queued[i].RelationID, queued[i].ID, err)
		}
		d, err := b.dormant(ctx, rel)
		if err != nil {
			return nil, err
		}
		if !d {
			return &queued[i], nil
		}
	}
	return nil, nil
}
