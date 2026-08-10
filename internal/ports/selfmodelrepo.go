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
	Origin           string
	SourceUnitID     *string
	Status           string
	LastReinforcedAt time.Time
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

// SelfModelRepo is the repository port over self_beliefs — design §4.3,
// spec R2.1-R2.3.
//
// Three methods, and the two write methods' names are the whole guard
// spec R2.2's MUST NOT exists for: UpsertByTopicKey is the CREATE half of
// a consolidation.MergeDecision (MergeInto == ""), ReinforceByID is the
// MERGE half (MergeInto names an existing belief's id, chosen by embedding
// similarity, m2b spec R4.4 — an id that need not equal the newly-derived
// belief's own computed topic_key). Routing a merge through the
// topic-key-keyed upsert would silently create a second belief instead of
// reinforcing the one the merge decision found; the two method names make
// that mistake read wrong at the call site, which is the guard, not a
// runtime check.
type SelfModelRepo interface {
	// ActiveBeliefs returns every belief whose Status is "active", every
	// facet included — no status parameter on the call (LiveByIDs's own
	// precedent, m2b design §8: "a read whose name carries 'active', never
	// a status parameter"). derive's two dedup defenses and pattern_eval's
	// EvaluateStagnation share this one read; the FacetGoal filter
	// EvaluateStagnation applies (m2b spec R5.1) is core's own job, not
	// this port's (spec R2.3).
	ActiveBeliefs(ctx context.Context) ([]Belief, error)

	// UpsertByTopicKey writes b, conflicting on self_beliefs.topic_key
	// (UNIQUE, migration 0001:75) — RelationRepo.Upsert's own pattern
	// applied to the one column doc 02 §10 defines as a belief's natural
	// key. A second write for the same TopicKey updates the existing row
	// in place, keeping its ID; it never creates a duplicate (spec R2.1).
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
	// that has since vanished (spec R2.2).
	ReinforceByID(ctx context.Context, id string, confidence float64, at time.Time) error
}

// ErrBeliefNotFound is returned by ReinforceByID when no belief with the
// given id exists — ports.ErrUnitNotFound's shape (spec R2.2).
var ErrBeliefNotFound = errors.New("belief not found")
