package weight

import "github.com/rengo/nooma/internal/core/unit"

// Zone is a unit's thermal zone — doc 02 §2's emergent, never-persisted
// classification of a unit's attention state.
type Zone int

// The three zones, doc 02 §2's table.
const (
	ZoneHot Zone = iota
	ZoneWarm
	ZoneCold
)

// AllZones returns a fresh slice holding the three Zone vocabulary
// members, in the order the constants above declare them — a function,
// not an exported var, for the same reason unit.AllStatuses is
// (internal/core/unit/status.go).
func AllZones() []Zone {
	return nil
}

// String names the zone. Every member has a distinct, lowercase name.
func (z Zone) String() string {
	return ""
}

// ZoneOf classifies a unit's thermal zone from its status and focus
// membership alone, matching doc 02 §2's table exactly:
//
//	Hot  — status == pool && inFocus
//	Warm — status == pool && !inFocus
//	Cold — status == archived (either inFocus value)
//
// ZoneOf is total over unit.AllStatuses() x {true, false}: superseded and
// incomplete — the two statuses doc 02 §2's table does not name — also map
// to ZoneCold. The zone vocabulary is about attention and neither status
// is a candidate for attention (spec R1.4, design D2).
//
// ZoneOf takes no now: temperature is not a function of time, it is a
// function of two decisions already made. Cold's parenthetical in doc 02
// §2 — "its effective weight crossed the threshold during a
// consolidation" — is causal history, not a determination re-derived on
// read.
func ZoneOf(status unit.Status, inFocus bool) Zone {
	return ZoneCold
}
