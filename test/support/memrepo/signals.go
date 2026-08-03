package memrepo

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/rengo/nooma/internal/ports"
)

// Signals is an in-memory ports.SignalRepo. The zero value is not usable —
// call NewSignals. Two instances share no state, matching memrepo.Units's
// own isolation rule.
type Signals struct {
	mu  sync.Mutex
	all []ports.Signal
}

// Assert at compile time, following internal/store/sqlite/unitrepo.go:33's
// precedent.
var _ ports.SignalRepo = (*Signals)(nil)

// NewSignals returns an empty, ready-to-use in-memory ports.SignalRepo.
// Every call returns an independent instance.
func NewSignals() *Signals {
	return &Signals{}
}

// Record implements ports.SignalRepo. Unlike memrepo.DecisionLog, it
// enforces no id uniqueness: learning_signals.id is a PRIMARY KEY at the
// real store (migration 0002:9), but no case in repocontract.RunSignalRepo
// asks this fake to reject a duplicate, and inventing that rejection here
// would be answering a question the shared contract never states.
func (r *Signals) Record(_ context.Context, s ports.Signal) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.all = append(r.all, s)
	return nil
}

// Since implements ports.SignalRepo: every signal recorded strictly after
// t (occurred_at > t, never >=), ordered by OccurredAt ascending and
// tie-broken by ID, truncated to the earliest limit entries — the same
// shape memrepo.DecisionLog.Since implements.
func (r *Signals) Since(_ context.Context, t time.Time, limit int) ([]ports.Signal, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	matched := make([]ports.Signal, 0, len(r.all))
	for _, s := range r.all {
		if s.OccurredAt.After(t) {
			matched = append(matched, s)
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
