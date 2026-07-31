package unit

import "fmt"

// Status is the unit's persisted lifecycle state — doc 02 §1 names exactly
// four values. It is a defined string type, not an int enum: it prints as
// itself in an error, a log line and a decision_log row, and binds to the
// units.status TEXT column with no conversion table (design D1).
//
// Status("focus") compiles — the type does not make an invalid value
// unrepresentable. Validity is a boundary property, enforced by
// ParseStatus, the closed AllStatuses() vocabulary, and I01's promoted
// vocabulary check (design D1, design §7 risk #2).
type Status string

// The four members of the Status vocabulary, doc 02 §1. Order matches
// migration 0001's units.status column comment
// ("pool|archived|superseded|incomplete") — TestUnitStatusDDLMatchesAllStatuses
// pins the two together.
const (
	StatusPool       Status = "pool"
	StatusArchived   Status = "archived"
	StatusSuperseded Status = "superseded"
	StatusIncomplete Status = "incomplete"
)

// AllStatuses returns a fresh slice holding the four Status vocabulary
// members, in the order the constants above declare them.
//
// A function, not an exported var (design D1): an exported slice is
// mutable by any importer, which would let unit.AllStatuses()[0] = "focus"
// defeat I01's own vocabulary check from outside this package.
func AllStatuses() []Status {
	return []Status{StatusPool, StatusArchived, StatusSuperseded, StatusIncomplete}
}

// ErrUnknownStatus is returned by ParseStatus when s does not name a
// member of AllStatuses().
var ErrUnknownStatus = fmt.Errorf("unknown unit status")

// ParseStatus is the sole entry point from untrusted text — a database
// row, a JSON body, a CLI flag — into the Status vocabulary (design D1).
// It returns ErrUnknownStatus, naming the rejected value, for anything
// that is not one of AllStatuses()'s members.
func ParseStatus(s string) (Status, error) {
	for _, want := range AllStatuses() {
		if s == string(want) {
			return want, nil
		}
	}
	return "", fmt.Errorf("%w: %q", ErrUnknownStatus, s)
}

// IsLive reports whether s is the one status a live read surface includes.
// It takes no clock and no arguments — liveness is a property of the
// status value, not a function of time (design D2). Every LIVE read
// surface must filter positively (status == pool), never as a negation
// list, so a status added later is excluded by default rather than
// silently admitted (docs/02-cognitive-core.md §1).
func (s Status) IsLive() bool {
	return s == StatusPool
}
