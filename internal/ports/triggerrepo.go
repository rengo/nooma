package ports

import (
	"context"
	"errors"
	"time"

	"github.com/rengo/nooma/internal/core/prospection"
)

// TriggerStatus is triggers.status's vocabulary (migration 0001:46).
//
// It lives in internal/ports, not in internal/core/prospection, because
// core "states only this neutral vocabulary — never a schema status"
// (prospection/staleness.go:30-34) and no core function reads a trigger
// status: a core type here would be a type with no core consumer. It is
// therefore outside test/conformance/calibration_doc_test.go's reach too
// (§13 covers internal/core symbols only) — exactly
// StateSourceConsolidation's own situation, and pinned the same way, to
// migration 0001's column comment by an L2 test
// (test/conformance/trigger_timer_vocabulary_ddl_test.go).
type TriggerStatus string

// The triggers.status vocabulary, in migration 0001:46's own order.
//
// TriggerStatusDismissed is declared and never written here: dismissal is
// a user's answer to a delivered trigger, and delivery is m3d's. It sits
// beside the three this slice does write the way StateSourceUser sits
// beside a StateSourceConsolidation that is the only one anything writes.
const (
	TriggerStatusArmed     TriggerStatus = "armed"
	TriggerStatusFired     TriggerStatus = "fired"
	TriggerStatusDismissed TriggerStatus = "dismissed"
	TriggerStatusExpired   TriggerStatus = "expired"
)

// AllTriggerStatuses returns a fresh slice holding the four TriggerStatus
// members, in the order the constants above declare them.
//
// A function, not an exported var (AllDecisionActions's own reasoning,
// decisionlog.go:98-106): an exported slice is mutable by any importer,
// and a mutated result could defeat a completeness check run from outside
// this package.
func AllTriggerStatuses() []TriggerStatus {
	return []TriggerStatus{
		TriggerStatusArmed, TriggerStatusFired, TriggerStatusDismissed, TriggerStatusExpired,
	}
}

// TriggerKind is triggers.kind's vocabulary (migration 0001:45) — see
// TriggerStatus for why it lives here.
type TriggerKind string

// The triggers.kind vocabulary, in migration 0001:45's own order.
// TriggerKindEventBased and TriggerKindPatternBased are declared and never
// written: prospection.Arm produces only time-based armaments, and
// producing the other two is a stated non-goal of this slice.
const (
	TriggerKindTimeBased    TriggerKind = "time_based"
	TriggerKindEventBased   TriggerKind = "event_based"
	TriggerKindPatternBased TriggerKind = "pattern_based"
)

// AllTriggerKinds returns a fresh slice holding the three TriggerKind
// members — see AllTriggerStatuses for why it is a function.
func AllTriggerKinds() []TriggerKind {
	return []TriggerKind{TriggerKindTimeBased, TriggerKindEventBased, TriggerKindPatternBased}
}

// TriggerResolution is triggers.resolution's vocabulary (migration
// 0001:54) — how a delivered nudge ended, in the user's own answer.
//
// The fourth vocabulary this port declares, and the first whose members a
// user chooses rather than the system. See TriggerStatus for why they all
// live in internal/ports.
type TriggerResolution string

// The triggers.resolution vocabulary, in migration 0001:54's own order.
const (
	// ResolutionEngaged: the user acted on it.
	ResolutionEngaged TriggerResolution = "engaged"
	// ResolutionDeclined: the user said no.
	ResolutionDeclined TriggerResolution = "declined"
	// ResolutionSelfHealed: fresh activity resolved it before the user
	// answered — doc 02 §7's own third case, and the one nobody types.
	ResolutionSelfHealed TriggerResolution = "self_healed"
)

// AllTriggerResolutions returns a fresh slice holding the three members —
// see AllTriggerStatuses for why it is a function.
func AllTriggerResolutions() []TriggerResolution {
	return []TriggerResolution{ResolutionEngaged, ResolutionDeclined, ResolutionSelfHealed}
}

// TriggerPayload is triggers.payload's declared shape, and the reason it
// is declared rather than opaque is that something reads it back: doc 02
// §7 says a recurring trigger's re-arm propagates lead_days, so LeadDays
// has a reader and therefore a type. Decision.Context is a
// json.RawMessage for the opposite reason (decisionlog.go:128-135) —
// nothing reads it structurally, so nothing guarantees its keys.
type TriggerPayload struct {
	// ActionText is what the trigger will say when it is delivered.
	ActionText string
	// Rationale is why it was armed — doc 02 §11's glass box.
	Rationale string
	// LeadDays is how far ahead of the event the trigger fires. A
	// recurring trigger's re-arm propagates it (doc 02 §7).
	LeadDays int
}

// Trigger is TriggerRepo's write shape: every triggers column this slice
// writes, and none it does not. Write shape and read shape are different
// types, following LiveDecayStates's own rule (unitrepo.go:141-161).
//
// No Status field. Create only ever writes an armed row — arming is what
// it is — so a status parameter would be the one channel through which a
// prospection.Verdict string could reach a column that carries no CHECK
// constraint. The literal lives in one SQL string inside
// internal/store/sqlite and never crosses this boundary.
//
// No Condition field. condition is JSON for event_based and pattern_based
// triggers, and this slice produces neither.
type Trigger struct {
	// ID is the trigger's own id, generated by the caller.
	ID string
	// UnitID is the unit the trigger hangs off, or nil for a
	// pattern_based trigger, which hangs off nothing (migration 0001:44).
	UnitID *string
	// Kind is time_based for everything prospection.Arm produces.
	Kind TriggerKind
	// InterruptLevel is the already-converted float, never a
	// prospection.Interrupt: the conversion lives in internal/brain, so
	// internal/ports does not import that vocabulary. nil is the
	// degraded case — the column is nullable and the absence is
	// meaningful, not a zero.
	InterruptLevel *float64
	// Payload is marshalled to triggers.payload by the implementation.
	Payload TriggerPayload
	// FireAt is when the trigger comes due, or nil for a trigger that
	// does not come due on a clock (migration 0001:50).
	FireAt *time.Time
	// RecurrenceRule and RecurrenceAnchor are nil for a one-shot trigger.
	// prospection.Rule crosses this boundary on purpose: doc 02 §7 and
	// triggers.recurrence_rule's column comment name the same two strings
	// by design, so there is one vocabulary and no mapping to get wrong.
	RecurrenceRule   *prospection.Rule
	RecurrenceAnchor *prospection.Anchor
	// CreatedAt arrives as data — no method on this port reads a clock.
	CreatedAt time.Time
}

// DueTrigger is TriggerRepo's read shape: what the due scan needs to
// decide, and nothing else.
//
// No Status field: Due returns armed rows only, so it would have no
// reader. FireAt is not a pointer here for the same reason — Due's
// predicate includes fire_at IS NOT NULL, so a due trigger always has one.
type DueTrigger struct {
	ID     string
	UnitID *string
	FireAt time.Time
	// Payload is what the trigger will say. It joined this shape when
	// delivery arrived: m3b made DueTrigger narrow because the only
	// reader was a core decision over (fireAt, now), and a payload would
	// have been a field with no reader. Delivery is a reader, and a
	// second read to fetch the text of a row this one already returned
	// would be a query per trigger for data the first query had in hand.
	Payload          TriggerPayload
	InterruptLevel   *float64
	RecurrenceRule   *prospection.Rule
	RecurrenceAnchor *prospection.Anchor
}

// TriggerRepo is the repository port over triggers.
//
// Eight methods, and three absences that are deliberate:
//
//   - No method whose name begins Delete, Remove, Purge, Drop or Destroy
//     — I03's strengthened prefix set, asserted over this interface's own
//     method set by the shared contract suite
//     (test/support/repocontract/triggerrepo.go).
//   - No Due(status, …) parameterized read. UnitRepo's own rule: a status
//     parameter is precisely how a live read surface accidentally becomes
//     a non-live one, so every read here is named for what it returns.
//   - No Cancel-by-user and no List. doc 02 §8 promises both; chat is
//     m3c/m3d's and the UI is M4's, and a method with no caller is what
//     UpdateEventAt's doc comment refuses.
//
// Every transition is its own method taking no target status, which keeps
// the status literal inside one SQL string per method — StateRepo.
// OpenHypothesis sets its own source column literal for the same reason
// (staterepo.go:34-44). M4's dismissed adds a fourth method rather than a
// new argument.
//
// No method reads a clock: every timestamp arrives as an explicit
// parameter.
type TriggerRepo interface {
	// Create persists t. It returns ErrTriggerExists if a trigger with
	// t.ID already exists. The row is created armed.
	Create(ctx context.Context, t Trigger) error

	// Due returns every armed trigger whose fire_at is non-NULL and at or
	// before at, ordered by (fire_at, id). fire_at IS NOT NULL is part of
	// the predicate, not a defensive scan guard — a pattern_based trigger
	// legitimately has none (migration 0001:44, :50).
	//
	// This is an unbounded read: every trigger that has come due must be
	// seen, which is what the scan is. On a personal vault that is
	// O(due) memory per call and there is no paging.
	Due(ctx context.Context, at time.Time) ([]DueTrigger, error)

	// Fire moves id from armed to fired and writes fired_at = at in one
	// statement, so a fired row without a fired_at is unrepresentable.
	// surfaced_at is untouched and stays NULL — "pending delivery"
	// (migration 0001:52) is m3d's to close.
	//
	// It returns ErrTriggerStatusConflict if id is no longer armed, and
	// ErrTriggerNotFound if no trigger with id exists. The armed
	// precondition is optimistic concurrency, not validation: two scans
	// racing over one due trigger must produce exactly one fired row.
	Fire(ctx context.Context, id string, at time.Time) error

	// Surface records that id reached the user at at, writing
	// surfaced_at.
	//
	// Separate from Fire because firing and delivering are different
	// facts with different failure modes: a trigger can fire and then
	// fail to send. m3b left surfaced_at NULL precisely so this could be
	// the thing that fills it, and a caller writes it only AFTER a
	// successful send — a trigger the user never saw must not be recorded
	// as delivered.
	//
	// It returns ErrTriggerStatusConflict if id is not fired, and
	// ErrTriggerNotFound if it does not exist.
	Surface(ctx context.Context, id string, at time.Time) error

	// Undelivered returns every trigger that fired and has not reached
	// the user, ordered by (fired_at, id) — the digest's own source.
	//
	// Named for what it returns, like every other read here. A
	// Fired(status, surfaced bool) would be the parameterized read
	// UnitRepo's rule forbids, wearing two parameters instead of one.
	Undelivered(ctx context.Context) ([]DueTrigger, error)

	// Delivered returns every trigger that reached the user and has no
	// answer yet — doc 02 §5's "open check-ins", ordered most recent
	// first so a caller resolving one answer takes the head.
	Delivered(ctx context.Context) ([]DueTrigger, error)

	// Resolve records the user's answer: responded_at = at and
	// resolution = to, under a surfaced-and-unanswered precondition.
	//
	// It takes a `to` where Fire and Expire do not, and the asymmetry is
	// deliberate rather than an inconsistency. Those two reject a status
	// parameter because every call site has exactly one legal value, so
	// the parameter would only ever be a channel for a wrong one. Here
	// every call site has three, and they come from the user's own
	// answer through classify's vocabulary — a value, not a status the
	// transition implies.
	//
	// It returns ErrTriggerStatusConflict if id is not surfaced or is
	// already answered, and ErrTriggerNotFound if it does not exist.
	Resolve(ctx context.Context, id string, to TriggerResolution, at time.Time) error

	// Expire moves id from armed to expired (I15: a trigger past its
	// window expires, it does not fire late). triggers carries no
	// expired_at column, so no timestamp is written and none is invented.
	//
	// It returns the same two errors as Fire, for the same reasons.
	Expire(ctx context.Context, id string) error
}

// Sentinel errors ports.TriggerRepo implementations return.
var (
	// ErrTriggerNotFound is returned by Fire and Expire when no trigger
	// with the given id exists.
	ErrTriggerNotFound = errors.New("trigger not found")

	// ErrTriggerExists is returned by Create when a trigger with t.ID
	// already exists.
	ErrTriggerExists = errors.New("trigger already exists")

	// ErrTriggerStatusConflict is returned by Fire and Expire when the
	// trigger's current status is not armed.
	ErrTriggerStatusConflict = errors.New("trigger is not in the expected status")
)
