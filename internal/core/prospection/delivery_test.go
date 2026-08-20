package prospection

import (
	"math"
	"testing"
)

// TestResolveInterrupt_Nil_ReturnsDegradedDefault is the C14 content guard:
// it must fail against a stub returning the bare zero Interrupt, because
// Level() alone would not distinguish the intended degraded default from
// an accidental zero-value stub — Degraded() must independently report
// true. Declared first, ahead of TestInterrupt_ZeroValue_ReportsDegraded
// below, so this is the case that fails first rather than a
// coincidentally-matching zero-value case (spec R3.1).
func TestResolveInterrupt_Nil_ReturnsDegradedDefault(t *testing.T) {
	got := ResolveInterrupt(nil)
	if got.Level() != DefaultInterruptLevel {
		t.Errorf("ResolveInterrupt(nil).Level() = %v, want DefaultInterruptLevel (%v)",
			got.Level(), DefaultInterruptLevel)
	}
	if !got.Degraded() {
		t.Errorf("ResolveInterrupt(nil).Degraded() = false, want true — an absent reading "+
			"is unusable, never a claimed %v", DefaultInterruptLevel)
	}
}

// TestResolveInterrupt_NonFiniteOrOutOfRange_DegradesToDefault proves spec
// R3.1's own enumeration, each shape asserted individually rather than
// swept: a value core cannot interpret is never trusted as-is, and is
// never clamped into range — clamping 1.7 to 1.0 would manufacture a push
// out of a corrupt number (design §3.4).
func TestResolveInterrupt_NonFiniteOrOutOfRange_DegradesToDefault(t *testing.T) {
	tests := []struct {
		name string
		v    float64
	}{
		{"NaN", math.NaN()},
		{"+Inf", math.Inf(1)},
		{"-Inf", math.Inf(-1)},
		{"below range", -0.1},
		{"above range", 1.1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := tt.v
			got := ResolveInterrupt(&v)
			if got.Level() != DefaultInterruptLevel {
				t.Errorf("ResolveInterrupt(&%v).Level() = %v, want DefaultInterruptLevel (%v)",
					tt.v, got.Level(), DefaultInterruptLevel)
			}
			if !got.Degraded() {
				t.Errorf("ResolveInterrupt(&%v).Degraded() = false, want true", tt.v)
			}
		})
	}
}

// TestResolveInterrupt_InRangeValue_PassesThroughNonDegraded proves a
// well-formed reading is never second-guessed.
func TestResolveInterrupt_InRangeValue_PassesThroughNonDegraded(t *testing.T) {
	v := 0.42
	got := ResolveInterrupt(&v)
	if got.Level() != v {
		t.Errorf("ResolveInterrupt(&%v).Level() = %v, want %v unchanged", v, got.Level(), v)
	}
	if got.Degraded() {
		t.Errorf("ResolveInterrupt(&%v).Degraded() = true, want false", v)
	}
}

// TestResolveInterrupt_InclusiveBoundsPassThrough pins both endpoints of
// the accepted [0,1] range, which the in-range test above misses by
// sampling only 0.42 — both are legitimate readings, not corruption.
func TestResolveInterrupt_InclusiveBoundsPassThrough(t *testing.T) {
	tests := []struct {
		name string
		v    float64
	}{
		{"exactly 0", 0},
		{"exactly 1", 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := tt.v
			got := ResolveInterrupt(&v)
			if got.Level() != tt.v {
				t.Errorf("ResolveInterrupt(&%v).Level() = %v, want %v unchanged — an endpoint "+
					"of the accepted range is a configuration, not corruption", tt.v, got.Level(), tt.v)
			}
			if got.Degraded() {
				t.Errorf("ResolveInterrupt(&%v).Degraded() = true, want false", tt.v)
			}
		})
	}
}

// TestInterrupt_ZeroValue_ReportsDegraded proves design §3.4's own
// "forgotten initialisation is safe" property: a caller that never calls
// ResolveInterrupt at all — a forgotten field, a bare struct literal —
// still cannot reach RoutePush by accident, because Degraded() reads an
// unexported field whose zero value means "not confirmed".
func TestInterrupt_ZeroValue_ReportsDegraded(t *testing.T) {
	var zero Interrupt
	if !zero.Degraded() {
		t.Errorf("Interrupt{}.Degraded() = false, want true")
	}
}

// TestDefaultInterruptLevel_BelowPushThreshold proves spec R3.1's own MUST:
// the numeric guard alone, computed from both named constants — never
// repeated literals — so a future recalibration of either one breaks this
// test loudly instead of drifting silently. This is the arithmetic half
// of ruling 1's two independent guards (design §3.4); the degraded flag
// is the other, and Route (task 3.3) proves it holds even if this
// inequality is later violated by recalibration.
func TestDefaultInterruptLevel_BelowPushThreshold(t *testing.T) {
	if !(DefaultInterruptLevel < PushThreshold) {
		t.Fatalf("DefaultInterruptLevel (%v) must be strictly below PushThreshold (%v)",
			DefaultInterruptLevel, PushThreshold)
	}
}

// TestInterrupt_Route_DegradedAlwaysDigest proves spec R3.2's own MUST: a
// degraded Interrupt routes to RouteDigest regardless of the level it
// carries — including a corrupt level far above PushThreshold,
// constructed directly (in-package) rather than through ResolveInterrupt,
// since that is exactly the shape a bug elsewhere in this package could
// produce. Declared first among the Route tests so this is the case that
// fails first against the always-push stub, not a boundary case the stub
// happens to satisfy already.
func TestInterrupt_Route_DegradedAlwaysDigest(t *testing.T) {
	corrupt := Interrupt{level: 0.95} // confirmed left false: degraded
	if got := corrupt.Route(); got != RouteDigest {
		t.Errorf("Interrupt{level: 0.95, degraded}.Route() = %v, want RouteDigest — the "+
			"degraded short-circuit must run before the level comparison", got)
	}
}

// TestInterrupt_Route_BoundaryIsInclusive proves spec R3.2's own
// boundary: level == PushThreshold routes to push, matching doc 02 §7's
// "interrupt_level >= 0.7" wording and DelayCaveat's own permissive-side
// convention (staleness.go).
func TestInterrupt_Route_BoundaryIsInclusive(t *testing.T) {
	v := PushThreshold
	if got := ResolveInterrupt(&v).Route(); got != RoutePush {
		t.Errorf("ResolveInterrupt(&PushThreshold).Route() = %v, want RoutePush (inclusive boundary)", got)
	}
}

// TestInterrupt_Route_OneUlpBelowThresholdIsDigest proves the boundary is
// not accidentally inclusive on the wrong side.
func TestInterrupt_Route_OneUlpBelowThresholdIsDigest(t *testing.T) {
	v := math.Nextafter(PushThreshold, 0)
	if v >= PushThreshold {
		t.Fatalf("test fixture is broken: %v must be strictly below PushThreshold (%v)", v, PushThreshold)
	}
	if got := ResolveInterrupt(&v).Route(); got != RouteDigest {
		t.Errorf("ResolveInterrupt(&%v).Route() = %v, want RouteDigest", v, got)
	}
}

// TestInterrupt_Route_MaxLevelIsPush proves the top of the accepted range
// routes to push, matching TestResolveInterrupt_InclusiveBoundsPassThrough's
// own "exactly 1" case.
func TestInterrupt_Route_MaxLevelIsPush(t *testing.T) {
	v := 1.0
	if got := ResolveInterrupt(&v).Route(); got != RoutePush {
		t.Errorf("ResolveInterrupt(&1.0).Route() = %v, want RoutePush", got)
	}
}

// TestInterrupt_Route_ResolvedFromNilRoutesDigest proves spec R3.2's own
// composed scenario end to end: a degraded classification never reaches
// Push, twice over — by the arithmetic inequality (R3.1) and by the
// degraded short-circuit, either of which alone would suffice.
func TestInterrupt_Route_ResolvedFromNilRoutesDigest(t *testing.T) {
	if got := ResolveInterrupt(nil).Route(); got != RouteDigest {
		t.Errorf("ResolveInterrupt(nil).Route() = %v, want RouteDigest", got)
	}
}
