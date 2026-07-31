package ports

import (
	"context"
	"errors"
	"time"

	"github.com/rengo/nooma/internal/core/unit"
)

// UnitRepo is the repository port over units — docs/02-cognitive-core.md
// §1 ("Nothing is deleted. Archiving is a state transition, not a
// removal") and CLAUDE.md non-negotiable #6, made structural (design D5).
//
// Five methods, and two absences that are deliberate:
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

	// SetStatus moves id from status from to status to, recording at.
	// from is an optimistic-concurrency precondition, not a validation —
	// the legality of the transition is unit.ValidateTransition's
	// decision, made before this call. It returns ErrStatusConflict if
	// id's current status is not from, and ErrUnitNotFound if no unit
	// with id exists.
	SetStatus(ctx context.Context, id string, from, to unit.Status, at time.Time) error
}

// Sentinel errors ports.UnitRepo implementations return — design D5.
var (
	// ErrUnitNotFound is returned by ByID, UpdateContent and SetStatus
	// when no unit with the given id exists.
	ErrUnitNotFound = errors.New("unit not found")

	// ErrUnitExists is returned by Create when a unit with u.ID already
	// exists.
	ErrUnitExists = errors.New("unit already exists")

	// ErrStatusConflict is returned by SetStatus when id's current status
	// is not the from precondition.
	ErrStatusConflict = errors.New("unit is not in the expected status")
)
