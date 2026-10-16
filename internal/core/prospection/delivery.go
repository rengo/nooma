package prospection

import "math"

// PushThreshold is doc 02 §7's own comparison: interrupt_level >= 0.70 is
// a push (spec R3.2). Declared alongside DefaultInterruptLevel because
// TestDefaultInterruptLevel_BelowPushThreshold needs both named constants
// to exist together, ahead of Route (task 3.3).
const PushThreshold = 0.70

// DefaultInterruptLevel fills a degraded or unreadable interrupt_level
// (spec R3.1; design §3.4). Every value below PushThreshold behaves
// identically in v1 — the delivery split is the only consumer, and the
// digest's own ordering is focus.Priority, not interrupt_level — so the
// number is chosen for auditability, not behaviour: 0.0 reads as "no
// claim was made" rather than as an invented sentinel doc 02 §5.1 warns
// against ("a degraded weight is not a zero weight"). That warning is
// honoured by Interrupt.Degraded() carrying the distinction in structure,
// not by this value.
const DefaultInterruptLevel = 0.0

// Interrupt is a resolved interrupt level (spec R3.1/R3.2; design §3.4).
// Its fields are unexported, so ResolveInterrupt is the only way to
// obtain one: no caller outside this package can construct a
// non-degraded Interrupt carrying an out-of-range level. confirmed
// defaults to false, so the zero value Interrupt{} — a forgotten
// initialisation, a bare struct literal — also reports itself degraded;
// see Route, which can never route a degraded Interrupt to push.
type Interrupt struct {
	level     float64
	confirmed bool
}

// ResolveInterrupt reads what classify (or the triggers.interrupt_level
// column) supplied. nil, a non-finite value (NaN, ±Inf), or a value
// outside [0,1] degrades to DefaultInterruptLevel — never clamped, since
// clamping 1.7 to 1.0 would manufacture a push out of a corrupt number
// (design §3.4; mirrors consolidation.ResolveWeightThreshold's posture
// toward an unusable configured input). Any other value passes through
// unchanged.
func ResolveInterrupt(level *float64) Interrupt {
	if level == nil {
		return Interrupt{level: DefaultInterruptLevel}
	}
	v := *level
	if math.IsNaN(v) || math.IsInf(v, 0) || v < 0 || v > 1 {
		return Interrupt{level: DefaultInterruptLevel}
	}
	return Interrupt{level: v, confirmed: true}
}

// Level returns the resolved interrupt level: the input ResolveInterrupt
// received when it was usable, or DefaultInterruptLevel otherwise.
func (i Interrupt) Level() float64 { return i.level }

// Degraded reports whether the resolution fell back to
// DefaultInterruptLevel rather than trusting a supplied value — the
// distinction doc 02 §5.1 requires, carried separately from the level
// itself.
func (i Interrupt) Degraded() bool { return !i.confirmed }

// Route is the delivery split's own two-member vocabulary (spec R3.2):
// mutually exclusive by construction, never a bare bool a caller could
// misread.
type Route string

const (
	// RoutePush is the immediate, cadence-skipping delivery path.
	RoutePush Route = "push"
	// RouteDigest is the accumulating, cadence-gated delivery path.
	RouteDigest Route = "digest"
)

// Route decides push vs digest for this resolved interrupt (spec R3.2;
// design §3.4). The degraded check runs first and short-circuits: ruling
// 1's "a degraded classification never produces a push" then holds even
// if a future recalibration moves PushThreshold below DefaultInterruptLevel,
// which the arithmetic comparison alone would not survive. The boundary
// is inclusive — level == PushThreshold routes to push — matching doc 02
// §7's own "interrupt_level >= 0.7" wording and DelayCaveat's own
// permissive-side convention (staleness.go).
func (i Interrupt) Route() Route {
	if i.Degraded() {
		return RouteDigest
	}
	if i.level >= PushThreshold {
		return RoutePush
	}
	return RouteDigest
}
