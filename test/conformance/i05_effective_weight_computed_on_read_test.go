// Package conformance — see test/conformance/doc.go for the package contract.
package conformance

import (
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// TestI05_WeightPackageReturnsNoUnitShapedValue proves I05's *pure* half
// (docs/06-harness.md §4, doc 02 §2: decay is computed on read, never
// written per read) as a structural property of internal/core/weight's
// exported surface, at the point this PR leaves it: no exported function
// returns unit.Unit, *unit.Unit, or []unit.Unit. A read path therefore
// holds no unit-shaped value it could hand to a repository, and
// "accidentally persist a decayed weight" has no syntax (spec R1.3,
// design D9).
//
// This is I05's structural half, extended incrementally as design D9's
// full statement becomes assertable. design D9 requires that Boost — the
// package's only persistable value — has exactly two producers, Revive
// and Resurface. PR1 shipped neither, so the producer-count clause was
// not yet assertable. PR 2a shipped Revive and asserted a one-producer
// state (then named TestI05_BoostHasExactlyOneProducer). PR 2b ships
// Resurface, completing the set: the same check now asserts the FINAL
// two-producer state and is renamed TestI05_BoostHasExactlyTwoProducers
// to say so, rather than keep a stronger, now-final guarantee under a name
// that describes an intermediate state. A reader who diffs this file
// against PR 2a should expect the "want" set to have grown from one
// producer to two, and the name to have been corrected to match.
//
// The full I05 guarantee — "no *read path* writes decay" — needs a store
// to prove a read path exists at all; that structural half is m2c's, once
// ports.UnitRepo exists (spec R1.3's own scope note).
func TestI05_WeightPackageReturnsNoUnitShapedValue(t *testing.T) {
	repoRoot := repoRootFromCaller(t)
	weightDir := filepath.Join(repoRoot, "internal", "core", "weight")

	names, resultTypes := exportedFuncResultTypes(t, weightDir)
	if len(names) == 0 {
		t.Fatal("internal/core/weight has zero exported functions — nothing to check (D10's non-empty-corpus guard)")
	}

	const banned = "unit.Unit"
	for i, name := range names {
		for _, resultType := range resultTypes[i] {
			if strings.Contains(resultType, banned) {
				t.Errorf(
					"%s returns %q, which contains %q — no exported function in "+
						"internal/core/weight may return a unit-shaped value; a read "+
						"path must have no value it could hand to a repository (I05, spec R1.3)",
					name, resultType, banned,
				)
			}
		}
	}
}

// TestI05_BoostHasExactlyTwoProducers proves design D9's producer-count
// clause at its FINAL state, the one spec R1.3 describes: weight.Boost is
// produced by exactly two exported functions, Resurface and Revive, and no
// third. PR 2a shipped only Revive and asserted a one-producer state under
// the name TestI05_BoostHasExactlyOneProducer; this PR (2b) ships
// Resurface, completing the set, so the test is renamed to say what it now
// proves rather than keep a name ("ExactlyOneProducer") the code no longer
// supports — a claim narrower or wider than what is actually checked is
// exactly the defect this project's own review history keeps finding
// (openspec/changes/m2a-weight-focus/tasks.md's introduction). Its body,
// and the "want" set it checks, are what actually grows across PRs, per
// this file's own package-level doc comment above.
//
// Disclosure (tasks.md's own convention, C1's lesson): this assertion is
// trivially true the instant spread.go's Resurface stub lands (spec R2.5),
// since a stub returning nil still has result type []Boost — go/ast sees
// the same producer set whether Resurface is implemented or not. It is not
// a missing-symbol TDD red step for that reason; what it proves is the
// FINAL, closed producer set design D9 requires, and it stays a permanent
// guard against a future third producer being added without anyone
// noticing the API grew a new way to make a persistable value.
//
// A producer is any exported function whose result includes the literal
// rendering "Boost" or "[]Boost" — the two shapes a bare-Boost return and
// a slice-of-Boost return respectively print as via go/printer.
func TestI05_BoostHasExactlyTwoProducers(t *testing.T) {
	repoRoot := repoRootFromCaller(t)
	weightDir := filepath.Join(repoRoot, "internal", "core", "weight")

	names, resultTypes := exportedFuncResultTypes(t, weightDir)

	var producers []string
	for i, name := range names {
		for _, resultType := range resultTypes[i] {
			if resultType == "Boost" || resultType == "[]Boost" {
				producers = append(producers, name)
				break
			}
		}
	}
	sort.Strings(producers)

	want := []string{"Resurface", "Revive"}
	if len(producers) != len(want) {
		t.Fatalf("Boost producers = %v, want exactly %v (I05, design D9, spec R1.3)", producers, want)
	}
	for i := range want {
		if producers[i] != want[i] {
			t.Errorf("Boost producers = %v, want exactly %v (I05, design D9, spec R1.3)", producers, want)
		}
	}
}

// TestI05_NoBareFloat64WeightBypassesBoost extends I05's structural half
// past TestI05_BoostHasExactlyOneProducer, which only proves Boost's
// producer set is exactly {Revive}. It says nothing about a *different*
// future exported function that computes a weight meant for persistence
// and returns it as a bare float64 instead — sidestepping Boost, and with
// it I24's "weight and last_touched_at move together" guarantee (spec
// R2.1), entirely. That is C5
// (openspec/changes/m2a-weight-focus/tasks.md): the guarantee held only
// because no such function existed, not because anything stopped one.
//
// This test cannot close that gap completely. "A float64 intended for
// persistence" is not a property go/ast can read off a signature: Effective
// legitimately returns a bare float64 too, and R1.2 defines it that way — it
// is a read, recomputed on every call, never a value a caller writes back.
// Nothing about the *type* float64 distinguishes Effective's case from a
// hypothetical bypass; only what the caller does with the result does, and
// that is intent, not structure.
//
// What this test does instead: it turns a silent addition into a forced,
// reviewable one. want is the closed list of exported functions this
// package currently allows to return a bare float64 — today, only
// Effective. Any new exported function returning float64 fails this test
// until a human deliberately adds it to want, the same mechanism
// TestI05_BoostHasExactlyOneProducer already uses for Boost's producer set.
// Passing this test is not proof the addition is a legitimate read rather
// than a persistence bypass — that judgment stays with whoever reviews the
// change that edits want — but it stops the bypass C5 described from
// landing without anyone having to notice.
//
// Its ceiling, named because two blind reviewers found it and a future
// reader deserves it stated rather than discovered: this check matches
// go/printer's RENDERING of the result type, so it sees the identifier a
// function declares, not the type that identifier resolves to. Three shapes
// pass it untouched, all verified by building them:
//
//	type PersistWeight = float64                      // alias
//	type WeightValue float64                          // defined type
//	type PersistResult struct{ Value float64 }        // struct wrapper
//
// The third is the one to worry about. Boost is itself a struct wrapping a
// float64, so "a struct carrying a weight" is the shape this package has
// already taught everyone to write — a well-meaning helper returning one is
// a far likelier accident than a bare float64. Catching these needs type
// resolution (go/types), not the ast-only walk this file does, and banning
// float-carrying structs outright would ban Boost.
//
// So this guard raises the cost of the laziest bypass and nothing more.
// I24's real enforcement arrives in m2c, when ports.UnitRepo gains the
// weight-write method that gives the invariant something to protect
// (tasks.md C6).
func TestI05_NoBareFloat64WeightBypassesBoost(t *testing.T) {
	repoRoot := repoRootFromCaller(t)
	weightDir := filepath.Join(repoRoot, "internal", "core", "weight")

	names, resultTypes := exportedFuncResultTypes(t, weightDir)

	var producers []string
	for i, name := range names {
		for _, resultType := range resultTypes[i] {
			if resultType == "float64" {
				producers = append(producers, name)
				break
			}
		}
	}
	sort.Strings(producers)

	want := []string{"Effective"}
	if len(producers) != len(want) {
		t.Fatalf("exported float64-returning functions in internal/core/weight = %v, want exactly %v — a new one must be added here deliberately, with a reviewed reason it is a read and not a persistence bypass (C5, spec R2.1)", producers, want)
	}
	for i := range want {
		if producers[i] != want[i] {
			t.Errorf("exported float64-returning functions in internal/core/weight = %v, want exactly %v (C5, spec R2.1)", producers, want)
		}
	}
}

// exportedFuncResultTypes parses every non-test .go file in dir and
// returns, in parallel slices, each exported top-level function or
// method's qualified name and the textual rendering of each of its result
// types.
func exportedFuncResultTypes(t *testing.T, dir string) (names []string, resultTypes [][]string) {
	t.Helper()

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir %s: %v", dir, err)
	}

	fset := token.NewFileSet()
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}

		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || !fn.Name.IsExported() || fn.Type.Results == nil {
				continue
			}

			var results []string
			for _, field := range fn.Type.Results.List {
				var sb strings.Builder
				if err := printer.Fprint(&sb, fset, field.Type); err != nil {
					t.Fatalf("render result type of %s: %v", fn.Name.Name, err)
				}
				results = append(results, sb.String())
			}

			names = append(names, fn.Name.Name)
			resultTypes = append(resultTypes, results)
		}
	}

	return names, resultTypes
}
