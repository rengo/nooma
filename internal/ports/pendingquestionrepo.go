package ports

import (
	"context"
	"errors"
	"time"
)

// QuestionKind is what a pending question is about. One member today
// (relation) — the column exists from the first row so a second kind costs
// a row rather than a migration (ADR-0027).
type QuestionKind string

// QuestionKindRelation is the only QuestionKind member m3e writes.
const QuestionKindRelation QuestionKind = "relation"

// AllQuestionKinds returns a fresh slice holding the QuestionKind vocabulary
// members, in the order the constants above declare them.
//
// A function, not an exported var (design D1's reasoning, the same rule
// AllDecisionActions and AllSignalTypes already follow): an exported slice
// is mutable by any importer, and a mutated result could defeat a
// completeness check run from outside this package.
func AllQuestionKinds() []QuestionKind {
	return []QuestionKind{QuestionKindRelation}
}

// QuestionResolution is how a pending question stopped being open.
//
// Deliberately not ports.TriggerResolution: engaged|declined|self_healed
// would record a relation confirmation as "engaged", an audit row that
// misdescribes what happened (doc 02 §11, and proposal §4's own argument
// against reusing triggers for this store).
type QuestionResolution string

// The QuestionResolution vocabulary, migration 0004's own column comment
// order.
const (
	QuestionConfirmed QuestionResolution = "confirmed"
	QuestionRejected  QuestionResolution = "rejected"
	QuestionExpired   QuestionResolution = "expired"
)

// AllQuestionResolutions returns a fresh slice holding the three
// QuestionResolution members — see AllQuestionKinds for why it is a
// function.
func AllQuestionResolutions() []QuestionResolution {
	return []QuestionResolution{QuestionConfirmed, QuestionRejected, QuestionExpired}
}

// PendingQuestion is PendingQuestionRepo's WRITE shape. A question is
// always created queued and open: asked_at, resolved_at and resolution have
// no field here, so a row born already-answered is unrepresentable rather
// than merely refused.
type PendingQuestion struct {
	ID         string
	Kind       QuestionKind
	RelationID string
	CreatedAt  time.Time
}

// RelationQuestion is PendingQuestionRepo's READ shape: one pending_questions
// row joined to the relation it is about and to both of that relation's
// endpoints, in one read — ports.RelationRepo.Evidence's own stated reason
// applies unchanged (relationrepo.go:84-97): the alternative is relations,
// then units, then a zip in brain, which is two round trips and a
// correctness hazard if a row moves between them.
//
// Both Unasked and Open return it, though only the digest renders the two
// text contents today: the expiry sweep's own rationale names both
// endpoints too ("the question about X and Y expired unanswered"), so the
// join is load-bearing on both reads.
type RelationQuestion struct {
	ID           string
	RelationID   string
	RelationType string
	FromUnitID   string
	ToUnitID     string
	FromContent  string
	ToContent    string
	CreatedAt    time.Time
	// AskedAt is nil on every row Unasked returns, by construction — Unasked
	// only ever returns a queued (not-yet-asked) row.
	AskedAt *time.Time
}

// PendingQuestionRepo is the repository port over pending_questions — design
// §3.2, ADR-0027.
//
// Seven methods, seven callers: Create (consolidate, when ProposeRelation's
// Band is Uncertain), Unasked (the digest's queue read), Open
// (disambiguation AND the digest's own expiry sweep), MarkAsked (the
// digest, after a successful send), Confirm/Reject (the check-in path),
// Expire (the digest's expiry sweep). No method with no caller, and no
// Delete-prefixed method — this port joins every other repository interface
// in test/conformance/i03_units_never_deleted_test.go's reflection sweep,
// with no carve-out: a pending question is a state machine, never a
// removal.
//
// No method takes a resolution parameter. That omission is deliberate: it
// is the one channel through which an unvetted string could reach the
// resolution column, which carries no CHECK constraint (no table in this
// schema does) — m3b's own posture for its two vocabularies, applied here.
type PendingQuestionRepo interface {
	// Create persists q. It returns ErrRelationNotFound when q.RelationID
	// names no relation — the referential check moves to INSERT time
	// instead of a foreign key (ADR-0027's own reasoning: the audit row
	// must outlive its subject, since RejectRelation deletes the relation a
	// question is about).
	Create(ctx context.Context, q PendingQuestion) error

	// Unasked returns every queued question (asked_at IS NULL, resolved_at
	// IS NULL) whose relation still exists, oldest created_at first, then
	// id — the digest's own FIFO queue (design §3.5's tie-break: ranking by
	// confidence would ask first about the relation closest to asserting
	// itself anyway).
	Unasked(ctx context.Context) ([]RelationQuestion, error)

	// Open returns every asked, unanswered question (asked_at IS NOT NULL,
	// resolved_at IS NULL) whose relation still exists, most recently asked
	// FIRST, then id — mirroring ports.TriggerRepo.Delivered's own order,
	// because disambiguation makes the identical "most recent" choice
	// resolveCheckIn does.
	Open(ctx context.Context) ([]RelationQuestion, error)

	// MarkAsked stamps asked_at. Precondition asked_at IS NULL, in the
	// UPDATE's WHERE clause and nowhere else — a question is asked exactly
	// once.
	MarkAsked(ctx context.Context, id string, at time.Time) error

	// Confirm sets resolved_at and resolution = confirmed in one statement,
	// under a resolved_at IS NULL precondition. It returns
	// ErrQuestionStatusConflict if the question is already closed, and
	// ErrQuestionNotFound if id does not exist.
	Confirm(ctx context.Context, id string, at time.Time) error

	// Reject sets resolved_at and resolution = rejected — Confirm's own
	// contract, for the rejected case.
	Reject(ctx context.Context, id string, at time.Time) error

	// Expire sets resolved_at and resolution = expired — Confirm's own
	// contract, for the digest's own expiry sweep (design §3.5, spec R7).
	Expire(ctx context.Context, id string, at time.Time) error
}

// Sentinel errors ports.PendingQuestionRepo implementations return.
var (
	// ErrQuestionNotFound is returned by Confirm, Reject and Expire when no
	// pending question with the given id exists.
	ErrQuestionNotFound = errors.New("pending question not found")

	// ErrQuestionStatusConflict is returned by Confirm, Reject and Expire
	// when the question's resolved_at is already set.
	ErrQuestionStatusConflict = errors.New("pending question is no longer in the expected status")
)
