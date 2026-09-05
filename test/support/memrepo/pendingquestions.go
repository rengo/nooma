package memrepo

import (
	"context"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/ports"
)

// relationStub is what PendingQuestions.EnsureRelation records about a
// relation id — enough to answer RelationQuestion's own join (design §3.2)
// without this fake duplicating memrepo.Relations's own storage.
type relationStub struct {
	relType     string
	fromUnitID  string
	toUnitID    string
	fromContent string
	toContent   string
}

// storedQuestion is one pending_questions row plus the bookkeeping
// PendingQuestion's write shape deliberately excludes — askedAt, resolvedAt
// and resolution are all written by a later transition, never by Create.
type storedQuestion struct {
	q          ports.PendingQuestion
	askedAt    *time.Time
	resolvedAt *time.Time
	resolution ports.QuestionResolution
}

// PendingQuestions is an in-memory ports.PendingQuestionRepo. The zero value
// is not usable — call NewPendingQuestions. Two instances share no state,
// matching memrepo.Relations's own isolation rule.
type PendingQuestions struct {
	mu        sync.Mutex
	questions map[string]storedQuestion
	relations map[string]relationStub
}

var _ ports.PendingQuestionRepo = (*PendingQuestions)(nil)

// NewPendingQuestions returns an empty, ready-to-use in-memory
// ports.PendingQuestionRepo.
func NewPendingQuestions() *PendingQuestions {
	return &PendingQuestions{
		questions: make(map[string]storedQuestion),
		relations: make(map[string]relationStub),
	}
}

// EnsureRelation implements repocontract.PendingQuestionHarness. It records
// enough about relationID for Create's existence check and for the joined
// RelationQuestion reads to answer — the fake's own equivalent of a real
// relations+units join.
func (r *PendingQuestions) EnsureRelation(_ *testing.T, relationID, relType, fromUnitID, toUnitID, fromContent, toContent string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.relations[relationID] = relationStub{
		relType: relType, fromUnitID: fromUnitID, toUnitID: toUnitID,
		fromContent: fromContent, toContent: toContent,
	}
}

// ForgetRelation implements repocontract.PendingQuestionHarness. It removes
// relationID from the fake's own relation-existence set, so Unasked/Open's
// inner-join behaviour (R8's mitigation: a question whose relation is gone
// is skipped, not failed on) can be exercised without a real DELETE.
func (r *PendingQuestions) ForgetRelation(_ *testing.T, relationID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.relations, relationID)
}

// Create implements ports.PendingQuestionRepo.
func (r *PendingQuestions) Create(_ context.Context, q ports.PendingQuestion) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, ok := r.relations[q.RelationID]; !ok {
		return ports.ErrRelationNotFound
	}
	r.questions[q.ID] = storedQuestion{q: q}
	return nil
}

// Unasked implements ports.PendingQuestionRepo. Queued rows (asked_at and
// resolved_at both nil) whose relation still exists, oldest created_at
// first, then id.
func (r *PendingQuestions) Unasked(_ context.Context) ([]ports.RelationQuestion, error) {
	return r.filtered(func(s storedQuestion) bool {
		return s.askedAt == nil && s.resolvedAt == nil
	}, func(a, b ports.RelationQuestion) bool {
		if !a.CreatedAt.Equal(b.CreatedAt) {
			return a.CreatedAt.Before(b.CreatedAt)
		}
		return a.ID < b.ID
	}), nil
}

// Open implements ports.PendingQuestionRepo. Asked, unanswered rows whose
// relation still exists, most recently asked FIRST, then id.
func (r *PendingQuestions) Open(_ context.Context) ([]ports.RelationQuestion, error) {
	return r.filtered(func(s storedQuestion) bool {
		return s.askedAt != nil && s.resolvedAt == nil
	}, func(a, b ports.RelationQuestion) bool {
		if !a.AskedAt.Equal(*b.AskedAt) {
			return a.AskedAt.After(*b.AskedAt)
		}
		return a.ID > b.ID
	}), nil
}

// filtered is the shared read behind Unasked and Open: every stored
// question matching keep, whose relation still exists (the inner-join
// design §3.2 requires), rendered into RelationQuestion and sorted by less.
func (r *PendingQuestions) filtered(keep func(storedQuestion) bool, less func(a, b ports.RelationQuestion) bool) []ports.RelationQuestion {
	r.mu.Lock()
	defer r.mu.Unlock()

	out := make([]ports.RelationQuestion, 0, len(r.questions))
	for _, s := range r.questions {
		if !keep(s) {
			continue
		}
		rel, ok := r.relations[s.q.RelationID]
		if !ok {
			// R8's mitigation: a question whose relation is gone is
			// skipped, not failed on.
			continue
		}
		rq := ports.RelationQuestion{
			ID: s.q.ID, RelationID: s.q.RelationID, RelationType: rel.relType,
			FromUnitID: rel.fromUnitID, ToUnitID: rel.toUnitID,
			FromContent: rel.fromContent, ToContent: rel.toContent,
			CreatedAt: s.q.CreatedAt,
		}
		if s.askedAt != nil {
			when := *s.askedAt
			rq.AskedAt = &when
		}
		out = append(out, rq)
	}
	sort.Slice(out, func(i, j int) bool { return less(out[i], out[j]) })
	return out
}

// MarkAsked implements ports.PendingQuestionRepo. Precondition asked_at IS
// NULL — a question is asked exactly once.
func (r *PendingQuestions) MarkAsked(_ context.Context, id string, at time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	s, ok := r.questions[id]
	if !ok {
		return ports.ErrQuestionNotFound
	}
	if s.askedAt != nil {
		return ports.ErrQuestionStatusConflict
	}
	when := at
	s.askedAt = &when
	r.questions[id] = s
	return nil
}

// Confirm implements ports.PendingQuestionRepo.
func (r *PendingQuestions) Confirm(ctx context.Context, id string, at time.Time) error {
	return r.resolve(id, at, ports.QuestionConfirmed)
}

// Reject implements ports.PendingQuestionRepo.
func (r *PendingQuestions) Reject(ctx context.Context, id string, at time.Time) error {
	return r.resolve(id, at, ports.QuestionRejected)
}

// Expire implements ports.PendingQuestionRepo.
func (r *PendingQuestions) Expire(ctx context.Context, id string, at time.Time) error {
	return r.resolve(id, at, ports.QuestionExpired)
}

// resolve is Confirm/Reject/Expire's shared precondition: resolved_at IS
// NULL, set together with its own resolution literal.
func (r *PendingQuestions) resolve(id string, at time.Time, resolution ports.QuestionResolution) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	s, ok := r.questions[id]
	if !ok {
		return ports.ErrQuestionNotFound
	}
	if s.resolvedAt != nil {
		return ports.ErrQuestionStatusConflict
	}
	when := at
	s.resolvedAt = &when
	s.resolution = resolution
	r.questions[id] = s
	return nil
}
