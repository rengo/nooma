package consolidation

import (
	"sort"
	"time"

	"github.com/rengo/nooma/internal/core/unit"
)

// IncompleteExpiryHours is doc 02 §1's own fixed number — quoted, not
// chosen (design.md §4.2): "an `incomplete` unit ... promoted with what it
// has, or archived if still unresolved, after 24 h during consolidation".
const IncompleteExpiryHours = 24

// Incomplete is a unit.StatusIncomplete unit's ambiguity-resolution state
// at the instant expire_incomplete runs (spec R2.1).
//
// Unresolved is a field this package declares and no code in M2 produces
// (design.md §4.2, design §9 Q1): m2c passes false for every unit, because
// no column and no surface can set it yet. This is proven against a
// repo-constructed Incomplete{Unresolved: true} input; it does not claim a
// producer exists.
type Incomplete struct {
	UnitID     string
	CreatedAt  time.Time
	Unresolved bool
}

// ExpireIncomplete resolves doc 02's §1/§6.1 contradiction: promotion is
// the default outcome at 24h, and archival is the exception a caller must
// evidence via Unresolved (spec R2.1, design.md §4.2).
//
// elapsed = now - u.CreatedAt, clamped at zero when negative (clock skew, a
// backdated import — a unit that does not yet exist has waited no time).
// elapsed < IncompleteExpiryHours produces no transition. At or past that
// bound, the emitted transition is incomplete -> archived (reason
// ReasonIncompleteExpired) when u.Unresolved, and incomplete -> pool
// (reason ReasonIncompletePromoted) otherwise — promotion is the default,
// archival is the exception the caller must evidence.
//
// Output is sorted by UnitID.
func ExpireIncomplete(us []Incomplete, now time.Time) []Transition {
	var out []Transition
	for _, u := range us {
		elapsed := now.Sub(u.CreatedAt).Hours()
		if elapsed < 0 {
			elapsed = 0
		}
		if elapsed < IncompleteExpiryHours {
			continue
		}

		if u.Unresolved {
			out = append(out, Transition{
				UnitID: u.UnitID,
				From:   unit.StatusIncomplete,
				To:     unit.StatusArchived,
				Reason: ReasonIncompleteExpired,
			})
			continue
		}
		out = append(out, Transition{
			UnitID: u.UnitID,
			From:   unit.StatusIncomplete,
			To:     unit.StatusPool,
			Reason: ReasonIncompletePromoted,
		})
	}

	sort.Slice(out, func(i, j int) bool { return out[i].UnitID < out[j].UnitID })
	return out
}
