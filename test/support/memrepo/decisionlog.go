package memrepo

import (
	"context"
	"sort"
	"strings"
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
	// seq is each row's 1-based insertion sequence, the in-memory twin of
	// decision_log's rowid: monotone in write order, never reused.
	seq map[string]int64
	// failRecord, when non-nil, makes every Record call return it without
	// touching state — design D5 Layer 3's RED-first audit-failure test
	// (I23): a correction whose pre-image write fails must leave the target
	// unit untouched. Set via NewFailingDecisionLog, following
	// fakeprovider.NewEmbeddingFakeWithError's own "WithError" shape.
	failRecord error
}

// Assert at compile time, following internal/store/sqlite/unitrepo.go:33's
// precedent.
var _ ports.DecisionLog = (*DecisionLog)(nil)

// NewDecisionLog returns an empty, ready-to-use in-memory ports.DecisionLog.
// Every call returns an independent instance.
func NewDecisionLog() *DecisionLog {
	return &DecisionLog{byID: make(map[string]ports.Decision), seq: make(map[string]int64)}
}

// NewFailingDecisionLog returns an in-memory ports.DecisionLog whose Record
// always fails with err, never persisting anything — design D5 Layer 3's
// audit-write-failure double.
func NewFailingDecisionLog(err error) *DecisionLog {
	return &DecisionLog{byID: make(map[string]ports.Decision), seq: make(map[string]int64), failRecord: err}
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

	if r.failRecord != nil {
		return r.failRecord
	}
	if _, exists := r.byID[d.ID]; exists {
		return ports.ErrDecisionExists
	}
	r.order = append(r.order, d.ID)
	r.byID[d.ID] = d
	r.seq[d.ID] = int64(len(r.order))
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

// Before implements ports.DecisionLog over the full-precision in-memory rows:
// (OccurredAt, Seq) descending, strictly older than the cursor, filtered by
// action prefix, bounded by limit. Callers use whole-second instants so this
// agrees with SQLite's one-second storage.
func (r *DecisionLog) Before(_ context.Context, before *ports.DecisionCursor, actionPrefix string, limit int) ([]ports.DecisionRow, error) {
	if limit < 1 {
		return nil, nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	var matched []ports.DecisionRow
	for _, id := range r.order {
		d := r.byID[id]
		if !strings.HasPrefix(string(d.Action), actionPrefix) {
			continue
		}
		row := ports.DecisionRow{Decision: d, Seq: r.seq[id]}
		if before != nil && !olderThan(row, *before) {
			continue
		}
		matched = append(matched, row)
	}
	sort.Slice(matched, func(i, j int) bool {
		if !matched[i].OccurredAt.Equal(matched[j].OccurredAt) {
			return matched[i].OccurredAt.After(matched[j].OccurredAt)
		}
		return matched[i].Seq > matched[j].Seq
	})
	if len(matched) > limit {
		matched = matched[:limit]
	}
	return matched, nil
}

// olderThan reports whether row sorts strictly after c in the newest-first
// order: (OccurredAt, Seq) < (c.OccurredAt, c.Seq).
func olderThan(row ports.DecisionRow, c ports.DecisionCursor) bool {
	if !row.OccurredAt.Equal(c.OccurredAt) {
		return row.OccurredAt.Before(c.OccurredAt)
	}
	return row.Seq < c.Seq
}
