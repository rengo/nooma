// Package conformance — see test/conformance/doc.go for the package contract.
package conformance

import (
	"context"
	"reflect"
	"testing"

	"github.com/rengo/nooma/internal/core/unit"
	"github.com/rengo/nooma/internal/core/weight"
	"github.com/rengo/nooma/internal/ports"
)

// TestI24_UnitRepoWeightWriteStructural proves invariant I24
// (docs/06-harness.md §4, line 196: "A weight write moves weight and
// last_touched_at together; neither is written alone") structurally, at
// the ports.UnitRepo interface — spec R1.1, design §3.1(d).
//
// Two legs live in this file, each a distinct claim:
//
//  1. No method of ports.UnitRepo declares a float64 parameter, after
//     unwrapping slice/map/pointer/array element types. This is the leg
//     that actually carries I24's weight (design §3.1(d)): a float64 is
//     the only primitive shape a bare weight can take, so an interface
//     with none cannot acquire a SetWeight(id string, w float64) — the
//     exact method I24 forbids. **Not a missing-symbol red**: true today,
//     before PR 1 adds anything (the seven existing methods take no float
//     at all), and it stays true once ApplyBoosts exists — disclosed as a
//     non-red step per m2a C9 rather than claimed as one.
//
//  2. Exactly one method's parameter list mentions weight.Boost (bare or
//     sliced) — a second weight-writing method, however named, fails
//     this. **Genuinely red on the tree as it stands right now**: zero
//     methods take a weight.Boost today, the assertion wants exactly one,
//     0 != 1 fails for the right reason. This is watched red before
//     ports.UnitRepo.ApplyBoosts is added to the interface (the next
//     commit in this PR), specifically so leg 2's own claim of being a
//     genuine red is observed rather than assumed.
//
// A third leg — the two-column SQL assignment appears in exactly one
// method's SQL text under internal/store/sqlite (design §3.1(d) row 3,
// spec R3.4) — is deliberately NOT here. It needs internal/store/sqlite
// source text, which does not exist until PR 5
// (feat/store-unit-relation-repos) implements ApplyBoosts over SQLite.
// That leg is test/conformance/i05_effective_weight_computed_on_read_test.go
// there, not a third t.Run in this file — recorded here as a forward
// reference so leg 3 does not appear silently missing from PR 1.
//
// What legs 1-2 do NOT cover, named per design §3.1(d) rather than left to
// be discovered later: leg 1 unwraps containers, not struct fields, so a
// future method taking a *different* struct with a `Weight float64` field
// passes leg 1 undetected. A method named Touch(id string, at time.Time)
// that writes last_touched_at alone passes both legs, because I24's text
// is about a *weight write* moving both columns — a bare timestamp write
// is a different question doc 02 does not forbid.
func TestI24_UnitRepoWeightWriteStructural(t *testing.T) {
	repoType := reflect.TypeOf((*ports.UnitRepo)(nil)).Elem()
	if repoType.Kind() != reflect.Interface {
		t.Fatalf("ports.UnitRepo has kind %s, want interface", repoType.Kind())
	}
	if repoType.NumMethod() == 0 {
		t.Fatal("ports.UnitRepo declares zero methods — nothing to check yet")
	}

	boostType := reflect.TypeOf(weight.Boost{})

	t.Run("leg 1: no method takes a bare float64, containers unwrapped", func(t *testing.T) {
		for i := 0; i < repoType.NumMethod(); i++ {
			m := repoType.Method(i)
			for p := 0; p < m.Type.NumIn(); p++ {
				pt := m.Type.In(p)
				if isContextType(pt) {
					continue
				}
				if unwrapContainerType(pt).Kind() == reflect.Float64 {
					t.Errorf(
						"ports.UnitRepo.%s takes a bare float64 parameter (%s) — I24 requires "+
							"weight and last_touched_at to move together, which a bare float64 "+
							"cannot express (docs/06-harness.md §4 I24, design §3.1(d) leg 1)",
						m.Name, pt)
				}
			}
		}
	})

	t.Run("leg 2: exactly one method's parameters mention weight.Boost", func(t *testing.T) {
		var matches []string
		for i := 0; i < repoType.NumMethod(); i++ {
			m := repoType.Method(i)
			for p := 0; p < m.Type.NumIn(); p++ {
				pt := m.Type.In(p)
				if isContextType(pt) {
					continue
				}
				if unwrapSliceType(pt) == boostType {
					matches = append(matches, m.Name)
					break
				}
			}
		}
		if len(matches) != 1 {
			t.Errorf(
				"ports.UnitRepo has %d method(s) whose parameters mention weight.Boost (%v), "+
					"want exactly 1 — a second weight-writing method, however named, is exactly "+
					"what I24 forbids (docs/06-harness.md §4 I24, design §3.1(d) leg 2)",
				len(matches), matches)
		}
	})

	// Spec R1.2's second MUST: the live-count-by-type method's name
	// carries what it counts, checked structurally alongside I24's own
	// reflection sweep since both are shape checks over the same
	// interface. Not a red step at any point in this PR's own commit
	// order: today (before CountLiveByType exists) no method takes a
	// unit.Type at all, and once it exists it returns an int, never
	// []unit.Unit — disclosed as a non-red structural check per m2a C9,
	// the same posture leg 1 above discloses.
	t.Run("R1.2: no method both accepts a unit.Type and returns []unit.Unit", func(t *testing.T) {
		unitSliceType := reflect.TypeOf([]unit.Unit{})
		typeType := reflect.TypeOf(unit.TypeTask)

		for i := 0; i < repoType.NumMethod(); i++ {
			m := repoType.Method(i)

			takesType := false
			for p := 0; p < m.Type.NumIn(); p++ {
				if m.Type.In(p) == typeType {
					takesType = true
					break
				}
			}
			if !takesType {
				continue
			}

			for o := 0; o < m.Type.NumOut(); o++ {
				if m.Type.Out(o) == unitSliceType {
					t.Errorf(
						"ports.UnitRepo.%s both accepts a unit.Type and returns []unit.Unit — "+
							"spec R1.2 forbids this shape: a live-count method returns an int, "+
							"never a slice the caller would count itself (owner ruling 6)",
						m.Name)
				}
			}
		}
	})
}

// isContextType reports whether t is the context.Context interface type
// exactly — every ports.UnitRepo method's first parameter, and not a
// shape either leg above is checking.
func isContextType(t reflect.Type) bool {
	return t == reflect.TypeOf((*context.Context)(nil)).Elem()
}

// unwrapContainerType peels Ptr, Slice, Array and Map layers off t,
// repeatedly, until it reaches a non-container kind — design §3.1(d)
// leg 1's own scoping ("after unwrapping slice/map/pointer/array element
// types"). It does not unwrap struct fields: a struct carrying a
// `Weight float64` field is not a bare float64 parameter, and leg 1's own
// doc comment names that as a caveat, not an oversight.
func unwrapContainerType(t reflect.Type) reflect.Type {
	for {
		switch t.Kind() {
		case reflect.Pointer, reflect.Slice, reflect.Array, reflect.Map:
			t = t.Elem()
		default:
			return t
		}
	}
}

// unwrapSliceType peels Slice and Array layers off t, repeatedly, until it
// reaches a non-slice, non-array kind — design §3.1(d) leg 2's own
// scoping ("bare or sliced"). Unlike unwrapContainerType it does not
// unwrap Ptr or Map: leg 2 is asking "does this parameter carry a
// weight.Boost value, directly or as a batch", not walking every possible
// container shape.
func unwrapSliceType(t reflect.Type) reflect.Type {
	for t.Kind() == reflect.Slice || t.Kind() == reflect.Array {
		t = t.Elem()
	}
	return t
}
