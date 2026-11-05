package consolidation

import (
	"sort"
	"time"
)

// TitleBackfillPerPass is how many untitled dated units one night's title phase
// names at most (doc 02 §13, ADR-0065 point 3). It bounds the night's provider
// cost: a vault with hundreds of legacy units drains over a few nights, the units
// that will ring first named first.
const TitleBackfillPerPass = 50

// Untitled is one dated unit with no title, as SelectToName reads it: its id, the
// instants it carries and the soonest fire of a reminder still pending for it.
type Untitled struct {
	ID       string
	EventAt  *time.Time
	DueAt    *time.Time
	NextFire *time.Time // nil when no reminder is pending for the unit
}

// instant is the moment a reply would say for the unit: its event_at, else its
// due_at. The zero time when it has neither, which cannot reach here (a
// candidate is dated).
func (u Untitled) instant() time.Time {
	switch {
	case u.EventAt != nil:
		return *u.EventAt
	case u.DueAt != nil:
		return *u.DueAt
	}
	return time.Time{}
}

// titleRank is the tier a candidate sorts into: 0 a pending reminder, 1 an instant
// not yet past, 2 a past instant.
func (u Untitled) titleRank(now time.Time) int {
	switch {
	case u.NextFire != nil:
		return 0
	case !u.instant().Before(now):
		return 1
	}
	return 2
}

// SelectToName returns the ids the night names, at most limit (ADR-0065 point 3):
// units with a pending reminder first, soonest fire first; then future instants,
// nearest first; then past instants, most recent first. Ties break by id. An
// instant equal to now is still ahead. cands is not reordered.
func SelectToName(cands []Untitled, now time.Time, limit int) []string {
	if limit <= 0 || len(cands) == 0 {
		return nil
	}
	sorted := append([]Untitled(nil), cands...)
	sort.SliceStable(sorted, func(i, j int) bool {
		a, b := sorted[i], sorted[j]
		if ra, rb := a.titleRank(now), b.titleRank(now); ra != rb {
			return ra < rb
		}
		var ka, kb time.Time
		switch a.titleRank(now) {
		case 0:
			ka, kb = *a.NextFire, *b.NextFire
		case 1:
			ka, kb = a.instant(), b.instant()
		default:
			ka, kb = b.instant(), a.instant() // most recent first
		}
		if !ka.Equal(kb) {
			return ka.Before(kb)
		}
		return a.ID < b.ID
	})
	if len(sorted) > limit {
		sorted = sorted[:limit]
	}
	ids := make([]string, len(sorted))
	for i, u := range sorted {
		ids[i] = u.ID
	}
	return ids
}
