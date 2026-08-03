package ports

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

// DecisionAction names the bucket a decision_log row belongs to — doc
// 02 §11's "glass box": "ONE table for all modules (action, rationale,
// context JSON, occurred_at). Every automatic decision with an effect is
// recorded with its reasoning."
//
// A defined string type with a closed vocabulary, following unit.Status's
// pattern (internal/core/unit/status.go): Action cannot be a bare literal
// at a call site, the whole vocabulary is greppable across Go and SQL, and
// each value below is exactly what migration 0001's own DDL comment names
// as an example ('capture.classify', 0001:97).
type DecisionAction string

// The fourteen members of the DecisionAction vocabulary — design D9, plus
// ActionCaptureDedupFailed (C14a: a judge-provider outage degrades the
// capture the same way ActionCaptureEmbeddingFailed already does for an
// embedding-provider outage, a decision D8 made and this one mirrors rather
// than re-argues) and, design D5, ActionCorrectionApplied/
// ActionCorrectionAmbiguous — the sanctioned R7.4 edit to this file for
// PR 12f-i's correction-audit slice. Order matches design D9's own
// declaration for its original eleven — the eight capture-time actions spec
// R4.5/R4.6/R4.7 and Q3a's refusal name, then the three relation-judging
// actions design D7 names — with the twelfth appended after its nearest
// sibling, ActionCaptureEmbeddingFailed, and the two correction actions
// appended last, rather than reordering what design D9 already fixed.
const (
	ActionCaptureClassify           DecisionAction = "capture.classify"
	ActionCaptureUnparseable        DecisionAction = "capture.classify.unparseable"
	ActionCaptureUnclassifiable     DecisionAction = "capture.classify.unclassifiable"
	ActionCaptureDiscarded          DecisionAction = "capture.discarded"
	ActionCaptureUnitCreated        DecisionAction = "capture.unit.created"
	ActionCaptureEmbeddingFailed    DecisionAction = "capture.embedding.failed"
	ActionCaptureDedupFailed        DecisionAction = "capture.dedup.failed"
	ActionCaptureHookDeferred       DecisionAction = "capture.hook.deferred"
	ActionCaptureDedupJudged        DecisionAction = "capture.dedup.judged"
	ActionRelationPersisted         DecisionAction = "relation.persisted"
	ActionRelationDiscarded         DecisionAction = "relation.discarded"
	ActionRelationDuplicateRecorded DecisionAction = "relation.duplicate.recorded"
	// ActionCorrectionApplied carries ADR-0016's pre-image: written before
	// the edit it describes, never after (design D5).
	ActionCorrectionApplied DecisionAction = "correction.applied"
	// ActionCorrectionAmbiguous records a decision with no vault effect —
	// the referent gate or the edit plan asked instead of picking (design
	// D2/D3, m1b D7's precedent for a discard that still explains itself).
	ActionCorrectionAmbiguous DecisionAction = "correction.ambiguous"
)

// AllDecisionActions returns a fresh slice holding the fourteen
// DecisionAction vocabulary members, in the order the constants above
// declare them.
//
// A function, not an exported var (design D1's reasoning, applied to this
// vocabulary too): an exported slice is mutable by any importer, and a
// mutated result could defeat a completeness check run from outside this
// package.
func AllDecisionActions() []DecisionAction {
	return []DecisionAction{
		ActionCaptureClassify, ActionCaptureUnparseable, ActionCaptureUnclassifiable,
		ActionCaptureDiscarded, ActionCaptureUnitCreated, ActionCaptureEmbeddingFailed,
		ActionCaptureDedupFailed, ActionCaptureHookDeferred, ActionCaptureDedupJudged,
		ActionRelationPersisted, ActionRelationDiscarded, ActionRelationDuplicateRecorded,
		ActionCorrectionApplied, ActionCorrectionAmbiguous,
	}
}

// Decision is one decision_log row (docs/03-data-model.md,
// migration 0001:95-102) — design D9.
type Decision struct {
	ID     string
	Action DecisionAction
	// Rationale is a human-readable sentence — doc 02 §11's own reasoning
	// requirement, not a code a caller has to look up.
	Rationale string
	// Context carries whatever structured detail the decision needs for
	// audit (spec R4.5) — doc 02 §11's glass box. Nil/empty is legal: the
	// column defaults to '{}' (migration 0001:98). How that default is
	// honored is internal/store/sqlite's concern, not this port's — the
	// fake enforces no column default at all, so that promise is proved at
	// L3, not here (internal/store/sqlite/decisionlog_integration_test.go).
	Context    json.RawMessage
	OccurredAt time.Time
}

// DecisionLog is the repository port over decision_log — design D9, spec
// R4.5. It is used exclusively from internal/brain: internal/core must not
// write to decision_log, directly or through a port (spec R4.5's own MUST
// NOT), and internal/ports is not on depguard's core-purity allow-list
// (.golangci.yml's core-purity rule lists only $gostd and
// internal/core itself), so internal/core cannot even import this file.
// That half of I12 is therefore free — lint-enforced, not tested (design
// D9's own note).
//
// No Delete*-prefixed method: decision_log is an audit trail, and CLAUDE.md
// non-negotiable #6 ("nothing is deleted in the vault") applies to it the
// same as it does to units. This is a convention this file follows, not one
// this PR mechanizes — I03's reflect check (design D5) is scoped to
// ports.UnitRepo alone.
type DecisionLog interface {
	// Record persists d. It returns ErrDecisionExists if a decision with
	// d.ID already exists — decision_log.id is a PRIMARY KEY (migration
	// 0001:96), and a caller-generated id colliding is a bug worth
	// surfacing rather than silently overwriting an existing row: an audit
	// trail that can be overwritten in place is not one.
	Record(ctx context.Context, d Decision) error

	// Since returns every decision recorded strictly after t —
	// occurred_at > t, never >=. That is what lets a caller pass the
	// occurred_at of the last decision it already read as the next call's
	// t without seeing that row a second time; an inclusive bound would
	// re-deliver it forever.
	//
	// Results are ordered by occurred_at ascending, tie-broken by id, so
	// two rows sharing one occurred_at value (the column carries no
	// uniqueness constraint) still come back in a deterministic order
	// across calls. limit bounds how many rows come back; when more than
	// limit rows qualify, the earliest limit are returned — the
	// read-forward-from-a-cursor shape this method exists for
	// (docs/02-cognitive-core.md §11: "Pull: everything is recorded and
	// explorable in the activity UI").
	Since(ctx context.Context, t time.Time, limit int) ([]Decision, error)
}

// ErrDecisionExists is returned by Record when a decision with the given
// ID already exists.
var ErrDecisionExists = errors.New("decision already exists")
