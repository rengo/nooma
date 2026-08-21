package ports

import (
	"context"
	"errors"
	"time"

	"github.com/rengo/nooma/internal/core/consolidation"
	"github.com/rengo/nooma/internal/core/focus"
	"github.com/rengo/nooma/internal/core/unit"
	"github.com/rengo/nooma/internal/core/weight"
)

// UnitRepo is the repository port over units — docs/02-cognitive-core.md
// §1 ("Nothing is deleted. Archiving is a state transition, not a
// removal") and CLAUDE.md non-negotiable #6, made structural (design D5).
//
// Twelve methods, and two absences that are deliberate:
//
//   - No method whose name begins Delete, Remove, Purge, Drop or Destroy
//     (I03's promoted reflection check, strengthened by this PR — design
//     D5's own stated gap: a Delete-only check would let Purge, Remove or
//     Drop slip past it).
//   - No List(status) parameterized read. A status parameter is precisely
//     how a live read surface accidentally becomes a non-live one — every
//     read method here is named for what it returns: LiveByIDs cannot be
//     asked for anything but status = 'pool', and ByID is the deliberate,
//     single any-status escape hatch corrections and audit need.
//     CountLiveByType takes a unit.Type, not a status — unit.Type has no
//     live/non-live axis of its own, so this does not reopen the rule.
//     IncompleteOlderThan is the one deliberate non-live read in M2 (I02's
//     exception) — its name carries the exception explicitly rather than
//     hiding it behind a status argument (design §4.1).
//
// No method reads a clock: every timestamp arrives as data, inside the
// unit.Unit value for Create, or as an explicit at parameter for the
// partial updates (design D5).
type UnitRepo interface {
	// Create persists u. It returns ErrUnitExists if a unit with u.ID
	// already exists.
	Create(ctx context.Context, u unit.Unit) error

	// ByID returns the unit stored under id, at any status — the
	// deliberate escape hatch corrections and audit need. It returns
	// ErrUnitNotFound if no unit with id exists.
	ByID(ctx context.Context, id string) (unit.Unit, error)

	// LiveByIDs returns the units among ids whose status is pool
	// (unit.Status.IsLive), in the order ids names them. An id that is
	// absent or not live is omitted, not an error.
	LiveByIDs(ctx context.Context, ids []string) ([]unit.Unit, error)

	// UpdateContent rewrites id's Content and UpdatedAt to at, leaving
	// every other column unchanged. It returns ErrUnitNotFound if no unit
	// with id exists.
	UpdateContent(ctx context.Context, id, content string, at time.Time) error

	// UpdateEventAt rewrites id's EventAt to eventAt and UpdatedAt to at,
	// leaving every other column — DueAt included — unchanged. It returns
	// ErrUnitNotFound if no unit with id exists.
	//
	// eventAt is not nullable: a classification cannot express "the user
	// removed the date" (design D4), so there is no producer for a
	// clear-the-date call, and a *time.Time parameter would ship a branch
	// with no caller.
	UpdateEventAt(ctx context.Context, id string, eventAt, at time.Time) error

	// UpdateDueAt rewrites id's DueAt to dueAt and UpdatedAt to at, leaving
	// every other column — EventAt included — unchanged. It returns
	// ErrUnitNotFound if no unit with id exists. See UpdateEventAt's doc
	// comment for why dueAt is not nullable.
	UpdateDueAt(ctx context.Context, id string, dueAt, at time.Time) error

	// SetStatus moves id from status from to status to, recording at.
	// from is an optimistic-concurrency precondition, not a validation —
	// the legality of the transition is unit.ValidateTransition's
	// decision, made before this call. It returns ErrStatusConflict if
	// id's current status is not from, and ErrUnitNotFound if no unit
	// with id exists.
	SetStatus(ctx context.Context, id string, from, to unit.Status, at time.Time) error

	// ApplyBoosts writes every boost's (Weight, LastTouchedAt) pair to its
	// own unit, and at to every touched unit's updated_at — I24's
	// structural guarantee (docs/06-harness.md §4, spec R1.1). Three
	// packed decisions, each with its own reason (design §3.1(a)-(c)):
	//
	//   - The parameter is weight.Boost, and nothing else on this
	//     interface can carry a weight change. weight.Boost is the shape
	//     m2a declared for exactly this purpose — {UnitID, Weight,
	//     LastTouchedAt} — so a SetWeight(id string, w float64) that
	//     leaves LastTouchedAt alone is not expressible at this port.
	//   - boosts is a batch, not a single Boost. Reweight already returns
	//     []weight.Boost for a whole pass; a single-value method would
	//     force the caller into a loop that re-zips a unit id to a weight
	//     and a timestamp N times — precisely the cross-unit-zip shape
	//     I24 forbids. A single write is still expressible as
	//     ApplyBoosts(ctx, []weight.Boost{b}, at); no expressiveness is
	//     lost.
	//   - at is a parameter distinct from each Boost's own LastTouchedAt,
	//     even though every M2 call site passes the pass's own instant
	//     for both. UpdateEventAt/UpdateDueAt already split "the value"
	//     from "the audit timestamp" this way, and repocontract fixtures
	//     the two with different instants on purpose — a swapped-argument
	//     call site, or an implementation that writes one into the
	//     other's column, fails.
	//
	// ApplyBoosts is all-or-nothing over the batch: a Boost naming a unit
	// id that does not exist returns ErrUnitNotFound, leaving every row in
	// the call — including boosts for units that do exist — untouched. A
	// non-finite Weight (NaN or ±Inf) anywhere in the batch returns
	// ErrNonFiniteWeight and writes nothing at all, refused before any row
	// is touched.
	ApplyBoosts(ctx context.Context, boosts []weight.Boost, at time.Time) error

	// CountLiveByType returns the count of units whose status is pool
	// (unit.Status.IsLive) and whose Type is t — never a slice the caller
	// would count itself (owner ruling 6, spec R1.2). pattern_eval's
	// EvaluateLoad needs exactly one integer; a method returning
	// []unit.Unit here would put an unbounded read where the phase needs
	// a bound. The name carries what it counts (Live) rather than reading
	// as a general parameterized list — unit.Type is a closed
	// nine-member vocabulary with no live/non-live axis, so this
	// parameter is never a status in the sense the package doc comment's
	// "no List(status)" rule forbids.
	CountLiveByType(ctx context.Context, t unit.Type) (int, error)

	// IncompleteOlderThan returns every unit.StatusIncomplete unit whose
	// CreatedAt is strictly before cutoff — expire_incomplete's own read
	// (design §4.1), and the one deliberate non-live read in M2 (I02's
	// exception, m2b design §8's own fixed name). No pool, archived or
	// superseded unit is ever returned, regardless of age.
	//
	// cutoff is a bound, not the decision: the caller computes it as
	// now.Add(-consolidation.IncompleteExpiryHours * time.Hour), and
	// consolidation.ExpireIncomplete applies the identical 24-hour
	// predicate itself. Two implementations of one rule is a drift risk
	// this port does not resolve by picking one side to trust —
	// over-delivering rows changes nothing, under-delivering is the only
	// failure mode, and that is what the constant pin (design §4.1) checks.
	IncompleteOlderThan(ctx context.Context, cutoff time.Time) ([]consolidation.Incomplete, error)

	// LiveDecayStates returns every unit.StatusPool unit's decay-relevant
	// fields (consolidation.Cold's shape) — never a unit.Unit-shaped value
	// (m2a D9's rule, kept one layer up). archive, connect and derive all
	// consume this same read (design §4.1): consolidation.Cold and
	// consolidation.Source declare the identical five fields, so one read
	// shape serves every one of them.
	//
	// LiveDecayStates is called four times per whole pass — slot 2
	// (archive), slot 4 (connect), slot 5 (derive), slot 6 (reweight) — and
	// this port makes no caching guarantee across those calls; each call
	// reflects the store's state at the instant it runs. archive changes
	// unit status at slot 2, so a cached snapshot taken before slot 2 would
	// hand the later phases units archive had just archived as live
	// sources — a caller that caches this read across phases reintroduces
	// exactly that bug.
	//
	// This is an unbounded read: archive must see every live unit, which is
	// what the phase is. On a personal vault (doc 02's model) that is
	// O(vault) memory per call and there is no paging (design §4.1, named
	// as a risk rather than mitigated).
	LiveDecayStates(ctx context.Context) ([]consolidation.Cold, error)

	// LiveFocusCandidates returns, for every id among ids whose status is
	// pool, that unit's focus.Candidate — ordered by id. An id that is
	// absent or not live is omitted, not an error, and an empty ids
	// returns an empty slice, LiveByIDs' own posture.
	//
	// ORDER BY id is a deterministic tie-break, not a ranking, and the
	// distinction is the whole reason this method returns a set rather
	// than a top-k. focus.Priority is a multiplicative envelope over
	// weight.Effective at an instant (focus/priority.go:168-198): no SQL
	// expression can order by it, and weight DESC LIMIT k cannot
	// approximate it either, because Priority is not monotone in raw
	// weight once urgency and age enter — a LIMIT would drop a
	// high-urgency low-weight item. So the ranking stays in focus.Rank,
	// where it is already reviewed and covered, and ordering here exists
	// only so two runs over one vault agree. Ordering in SQL would be the
	// drift IncompleteOlderThan's own doc comment warns about: one rule in
	// two languages.
	//
	// Why a second live read beside LiveByIDs rather than a shape
	// parameter on it: LiveByIDs returns unit.Unit and this returns
	// focus.Candidate, which is m2a D9's rule — a narrow read shape for
	// the core decision that consumes it — held one layer up. The name
	// carries both halves, UnitRepo's own convention: Live is the status,
	// FocusCandidates is the shape.
	//
	// It ships with no production caller. m3d's digest assembly is the
	// consumer; declaring a port ahead of it inside one chain is house
	// practice and is said out loud rather than hidden — ports.StateRepo
	// was declared before both its migration and its implementation
	// (staterepo.go:31-39). It is not untested: repocontract runs it
	// against the fake and the SQLite implementation alike.
	//
	// A NULL triggers.unit_id — a pattern_based trigger — must never reach
	// this method as an id to resolve. focus.Candidate has no
	// representation for "no source unit", and prospection's own Carry
	// already expects a nil *focus.Candidate for that case. That is the
	// caller's obligation, not something this signature can enforce.
	LiveFocusCandidates(ctx context.Context, ids []string) ([]focus.Candidate, error)
}

// Sentinel errors ports.UnitRepo implementations return — design D5.
var (
	// ErrUnitNotFound is returned by ByID, UpdateContent, SetStatus and
	// ApplyBoosts when no unit with the given id exists.
	ErrUnitNotFound = errors.New("unit not found")

	// ErrUnitExists is returned by Create when a unit with u.ID already
	// exists.
	ErrUnitExists = errors.New("unit already exists")

	// ErrStatusConflict is returned by SetStatus when id's current status
	// is not the from precondition.
	ErrStatusConflict = errors.New("unit is not in the expected status")

	// ErrNonFiniteWeight is returned by ApplyBoosts when any Boost in the
	// batch carries a NaN or ±Inf Weight. The whole batch is refused —
	// nothing is written, including finite Boosts in the same call
	// (design §3.1(e)).
	ErrNonFiniteWeight = errors.New("weight is not a finite number")
)
