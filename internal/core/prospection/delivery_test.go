package prospection

import (
	"math"
	"reflect"
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

// TestResolveInterrupt_EdgeOfRangeShapesAreInRange pins two float64 shapes
// that no other test in this file exercises, and records that both are
// ordinary in-range values rather than anything this gate special-cases:
//
//   - Negative zero is not less than zero (IEEE-754: -0.0 == 0.0), so it
//     passes through as a claimed 0.0.
//   - The smallest positive denormal is in range and is trusted, because
//     "unusably small" is not a thing this gate judges: every value below
//     PushThreshold behaves identically, so there is nothing to protect
//     against and no reason to invent a floor.
//
// Its honest weight: this test is NOT what catches an operator slip in the
// guard. Rewriting `v < 0` as `v <= 0` is already caught by
// TestResolveInterrupt_InclusiveBoundsPassThrough's exactly-0 case and by
// the round-trip table's lower bound — verified by running that mutation
// against the suite. What this adds is the two shapes stated explicitly,
// so a reader who wonders what -0.0 does here finds the answer asserted
// rather than has to derive it from the comparison.
func TestResolveInterrupt_EdgeOfRangeShapesAreInRange(t *testing.T) {
	tests := []struct {
		name string
		v    float64
	}{
		{"negative zero", math.Copysign(0, -1)},
		{"smallest positive denormal", math.SmallestNonzeroFloat64},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := tt.v
			got := ResolveInterrupt(&v)
			if got.Degraded() {
				t.Errorf("ResolveInterrupt(&%v).Degraded() = true, want false — %v is inside "+
					"[0,1] and is a claim the model made, not a reading core failed to parse",
					tt.v, tt.v)
			}
			if got.Level() != tt.v {
				t.Errorf("ResolveInterrupt(&%v).Level() = %v, want %v unchanged",
					tt.v, got.Level(), tt.v)
			}
			if got.Route() != RouteDigest {
				t.Errorf("ResolveInterrupt(&%v).Route() = %v, want RouteDigest", tt.v, got.Route())
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

// TestInterrupt_LevelAndDegraded_RoundTripEveryConstructedInterrupt
// proves Level()/Degraded() report exactly what ResolveInterrupt resolved
// for every shape exercised above, in one place. Not a missing-symbol red
// step: both accessors already compile and pass (task 3.2) — disclosed
// per this project's own convention (m2a C9).
func TestInterrupt_LevelAndDegraded_RoundTripEveryConstructedInterrupt(t *testing.T) {
	inRange := 0.42
	lowerBound := 0.0
	upperBound := 1.0
	nan := math.NaN()

	tests := []struct {
		name         string
		level        *float64
		wantLevel    float64
		wantDegraded bool
	}{
		{"nil", nil, DefaultInterruptLevel, true},
		{"non-finite", &nan, DefaultInterruptLevel, true},
		{"in range", &inRange, inRange, false},
		{"lower bound", &lowerBound, lowerBound, false},
		{"upper bound", &upperBound, upperBound, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ResolveInterrupt(tt.level)
			if got.Level() != tt.wantLevel {
				t.Errorf("ResolveInterrupt(%v).Level() = %v, want %v", tt.name, got.Level(), tt.wantLevel)
			}
			if got.Degraded() != tt.wantDegraded {
				t.Errorf("ResolveInterrupt(%v).Degraded() = %v, want %v", tt.name, got.Degraded(), tt.wantDegraded)
			}
		})
	}
}

// TestInterrupt_FieldsAreUnexported is design §3.4's own type-safety
// argument, checked rather than only asserted in prose: if either field
// of Interrupt is ever exported, ResolveInterrupt stops being the only
// way to construct one, and a caller outside this package could build a
// non-degraded Interrupt carrying an out-of-range level directly —
// exactly the bypass spec R3.1's own MUST forbids. This is the
// in-package structural proof that no such caller exists today.
func TestInterrupt_FieldsAreUnexported(t *testing.T) {
	typ := reflect.TypeOf(Interrupt{})
	for i := range typ.NumField() {
		f := typ.Field(i)
		if f.IsExported() {
			t.Errorf("Interrupt.%s is exported; ResolveInterrupt must be the only way to "+
				"construct a non-degraded Interrupt from outside this package", f.Name)
		}
	}
}
