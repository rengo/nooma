package memrepo

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/rengo/nooma/internal/ports"
)

// DecisionLog is an in-memory ports.DecisionLog. The zero value is not
// usable — call NewDecisionLog. Two instances share no state, matching
// memrepo.Units's own isolation rule.
type DecisionLog struct {
	mu sync.Mutex
	// order preserves insertion order; Since re-sorts by (OccurredAt, ID)
	// itself rather than relying on it, since decision_log carries no
	// uniqueness constraint on occurred_at and two decisions can share one
	// instant.
	order []string
	byID  map[string]ports.Decision
}

// Assert at compile time, following internal/store/sqlite/unitrepo.go:33's
// precedent.
var _ ports.DecisionLog = (*DecisionLog)(nil)

// NewDecisionLog returns an empty, ready-to-use in-memory ports.DecisionLog.
// Every call returns an independent instance.
func NewDecisionLog() *DecisionLog {
	return &DecisionLog{byID: make(map[string]ports.Decision)}
}

// Record implements ports.DecisionLog. It returns ports.ErrDecisionExists
// on a duplicate id, mirroring decision_log.id's PRIMARY KEY (0001:96) —
// the fake enforces this deliberately rather than silently upserting,
// because a contract answered only by a fake that upserts on id would be
// unimplementable over the real schema (design D6's "answered twice" rule,
// the same shape C11 caught for EmbeddingRepo).
func (r *DecisionLog) Record(_ context.Context, d ports.Decision) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.byID[d.ID]; exists {
		return ports.ErrDecisionExists
	}
	r.order = append(r.order, d.ID)
	r.byID[d.ID] = d
	return nil
}

// Since implements ports.DecisionLog: every decision recorded strictly
// after t (occurred_at > t, never >=), ordered by OccurredAt ascending and
// tie-broken by ID, truncated to the earliest limit entries.
func (r *DecisionLog) Since(_ context.Context, t time.Time, limit int) ([]ports.Decision, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	matched := make([]ports.Decision, 0, len(r.order))
	for _, id := range r.order {
		d := r.byID[id]
		if d.OccurredAt.After(t) {
			matched = append(matched, d)
		}
	}
	sort.Slice(matched, func(i, j int) bool {
		if !matched[i].OccurredAt.Equal(matched[j].OccurredAt) {
			return matched[i].OccurredAt.Before(matched[j].OccurredAt)
		}
		return matched[i].ID < matched[j].ID
	})
	if limit >= 0 && len(matched) > limit {
		matched = matched[:limit]
	}
	return matched, nil
}
