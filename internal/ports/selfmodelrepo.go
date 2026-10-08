package ports

import (
	"context"
	"errors"
	"time"

	"github.com/rengo/nooma/internal/core/selfmodel"
)

// Belief is one self_beliefs row (migration 0001:72-85). It carries
// Content, which consolidation.Belief does not — the derive prompt needs
// the text (spec R5.6) and no core decision function does.
type Belief struct {
	ID               string
	Facet            selfmodel.Facet
	TopicKey         string
	Content          string
	Confidence       float64
	Origin           selfmodel.Origin
	SourceUnitID     *string
	Status           selfmodel.Status
	LastReinforcedAt time.Time
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

// SelfModelRepo is the repository port over self_beliefs — design §4.3,
// spec R2.1-R2.3, and m4e's belief-status additions.
//
// Seven methods in three groups. The reads: ActiveBeliefs, RetiredBeliefs
// and BeliefByID. The derive writes: UpsertByTopicKey (the CREATE half of
// a consolidation.MergeDecision, MergeInto == "") and ReinforceByID (the
// MERGE half, MergeInto names an existing belief's id, chosen by embedding
// similarity, m2b spec R4.4 — an id that need not equal the
// newly-derived belief's own computed topic_key). Routing a merge through
// the topic-key-keyed upsert would silently create a second belief instead
// of reinforcing the one the merge decision found; the two method names
// make that mistake read wrong at the call site, which is the guard, not a
// runtime check. And the user writes: SetStatus (retire) and EditContent
// (edit), each a compare-and-swap on what the caller read.
//
// The derive writes are also guarded in the store, not only in the brain:
// neither may touch a retired belief, and the upsert may overwrite a row
// only while it is active AND derived, so no brain bug can revive a
// retired belief or rewrite the user's text.
type SelfModelRepo interface {
	// ActiveBeliefs returns every belief whose Status is "active", every
	// facet included — no status parameter on the call (LiveByIDs's own
	// precedent, m2b design §8: "a read whose name carries 'active', never
	// a status parameter"). derive's two dedup defenses and pattern_eval's
	// EvaluateStagnation share this one read; the FacetGoal filter
	// EvaluateStagnation applies (m2b spec R5.1) is core's own job, not
	// this port's (spec R2.3).
	ActiveBeliefs(ctx context.Context) ([]Belief, error)

	// RetiredBeliefs returns every belief whose Status is "retired", every
	// facet included — ActiveBeliefs's naming rule, for the other member
	// of the closed vocabulary.
	RetiredBeliefs(ctx context.Context) ([]Belief, error)

	// BeliefByID returns the belief with the given id in any status, or
	// ErrBeliefNotFound.
	BeliefByID(ctx context.Context, id string) (Belief, error)

	// UpsertByTopicKey writes b, conflicting on self_beliefs.topic_key
	// (UNIQUE, migration 0001:75) — RelationRepo.Upsert's own pattern
	// applied to the one column doc 02 §10 defines as a belief's natural
	// key. A second write for the same TopicKey updates the existing row
	// in place, keeping its ID; it never creates a duplicate (spec R2.1).
	//
	// The overwrite happens only while the existing row is active AND
	// derived. Over a retired row, or a seed or user_stated one, nothing
	// is written and the error is ErrBeliefProtected (m4e R12, R13).
	//
	// MUST NOT be used for the merge case (spec R2.2's MUST NOT) — see
	// ReinforceByID.
	UpsertByTopicKey(ctx context.Context, b Belief) error

	// ReinforceByID updates only Confidence and LastReinforcedAt for the
	// belief named by id, leaving TopicKey, Content, Facet, Origin and
	// SourceUnitID unchanged. It returns ErrBeliefNotFound rather than
	// creating a row when id does not exist — a reinforcement is a
	// decision about a specific, already-identified belief, and a repository
	// that upserts here would hide the case where MergeInto names a belief
	// that has since vanished (spec R2.2). A belief that exists but is not
	// active is ErrBeliefStatusConflict: reinforcing a retired belief would
	// revive its standing.
	ReinforceByID(ctx context.Context, id string, confidence float64, at time.Time) error

	// SetStatus moves the belief from one status to another and bumps
	// updated_at to at. from is an optimistic-concurrency precondition,
	// not a validation — UnitRepo.SetStatus's own shape. An unknown id is
	// ErrBeliefNotFound; a belief not currently in from is
	// ErrBeliefStatusConflict, with nothing written.
	SetStatus(ctx context.Context, id string, from, to selfmodel.Status, at time.Time) error

	// EditContent replaces the content of an ACTIVE belief whose content
	// is currently from with to, bumps updated_at to at, and marks the
	// belief origin = user_stated. The origin is part of the write itself:
	// there is no parameter for it, so an edit that does not mark the row
	// is not expressible. Nothing else moves. An unknown id is
	// ErrBeliefNotFound; a retired belief, or one whose content is no
	// longer from, is ErrBeliefStatusConflict with nothing written.
	EditContent(ctx context.Context, id, from, to string, at time.Time) error
}

// ErrBeliefNotFound is returned when no belief with the given id exists —
// ports.ErrUnitNotFound's shape (spec R2.2).
var ErrBeliefNotFound = errors.New("belief not found")

// ErrBeliefStatusConflict is returned when a belief exists but is not in
// the state the caller expected: SetStatus's from, EditContent's active
// status or content precondition, or ReinforceByID's active status.
var ErrBeliefStatusConflict = errors.New("belief is not in the expected state")

// ErrBeliefProtected is returned by UpsertByTopicKey when the row holding
// the topic_key is not active-and-derived: derive may not overwrite it.
var ErrBeliefProtected = errors.New("belief is not derived-and-active; derive may not overwrite it")
