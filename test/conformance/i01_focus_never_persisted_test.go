//go:build pendingimpl

// Package conformance — see test/conformance/doc.go for the package contract.
//
// This file is tagged pendingimpl (design.md §8, openspec/changes/complete-harness/
// design.md) and is never compiled by the untagged build (`go build ./...`,
// `make test`). It is compiled in isolation by `make pending-red`
// (scripts/pending-red.sh), whose whole job is to confirm this package
// FAILS to compile, and fails for the right reason.
package conformance

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/rengo/nooma/internal/core/unit"
)

// TestI01_FocusIsNeverAPersistedStatus proves invariant I01
// (docs/02-cognitive-core.md §3): "being in focus" is a computed view over
// the pool — sort by effective priority, take the top N — never a stored
// status. status='focus' must never exist as a member of the unit status
// vocabulary.
//
// Anchor: unit.Status / unit.AllStatuses (internal/core/unit), spec R6.1/
// R6.4, design.md §8.4. Neither symbol exists yet — see
// internal/core/unit/doc.go and test/conformance/pending_symbols.txt. In
// this chain the RED is a compile error naming both symbols
// (`undefined: unit.Status` and/or `undefined: unit.AllStatuses`) — that
// IS the passing state of scripts/pending-red.sh (design §8.1/§8.2, D9),
// not a defect to fix. This test never turns green inside this change.
//
// Promotion: the PR that adds unit.Status and unit.AllStatuses must, in the
// SAME PR, drop the pendingimpl tag from this file, move it into the
// untagged L2 suite, and remove both lines from pending_symbols.txt
// (design §8.3/§8.5, spec R7.3) — that PR is the one that trips
// scripts/pending-red.sh's failure mode 1 ("the symbols now exist").
//
// Two independent checks, mirroring docs/06-harness.md §4's own framing
// ("a test that fails if the literal 'focus' appears as a status value in
// the tree"):
//
//  1. Vocabulary: no member of unit.AllStatuses() is "focus".
//  2. Tree scan: no Go source file under internal/ or cmd/ assigns the
//     literal "focus" to something named Status. This is a coarse,
//     line-based heuristic (like i13_learning_signal_test.go's migration
//     scan), not a type-checked one — deliberately, so it needs no import
//     of go/ast to reason about a status literal it does not yet know the
//     shape of. Go source only: migrations are .sql, embedded via
//     go:embed, and are naturally outside this scan (design D1).
//
// Both checks apply design D10's non-empty-corpus guard: assert a non-empty
// corpus was found before asserting anything about its content, so a moved
// or renamed directory cannot turn this test vacuously green.
func TestI01_FocusIsNeverAPersistedStatus(t *testing.T) {
	t.Run("Status is a string-kind vocabulary type", func(t *testing.T) {
		// Referenced directly (not only through AllStatuses' return type) so
		// this file's RED names both anchored symbols, unit.Status and
		// unit.AllStatuses, independently of one another — see
		// test/conformance/pending_symbols.txt and scripts/pending-red.sh.
		var zero unit.Status
		if reflect.TypeOf(zero).Kind() != reflect.String {
			t.Errorf("unit.Status has kind %s, want a string-kind vocabulary type", reflect.TypeOf(zero).Kind())
		}
	})

	t.Run("vocabulary", func(t *testing.T) {
		statuses := unit.AllStatuses()
		if len(statuses) == 0 {
			t.Fatal("unit.AllStatuses() returned zero statuses — D10's guard: nothing to check yet")
		}
		for _, s := range statuses {
			if string(s) == "focus" {
				t.Errorf(
					"unit.AllStatuses() includes %q — focus is a computed view "+
						"(docs/02-cognitive-core.md §3: sort the pool by priority, take "+
						"the top N), never a persisted unit.Status",
					s,
				)
			}
		}
	})

	t.Run("tree scan", func(t *testing.T) {
		repoRoot := repoRootFromCaller(t)
		report := func(path string, lineNum int, line string) {
			t.Errorf(
				"%s:%d: %q — status='focus' must never be a persisted value "+
					"(docs/02-cognitive-core.md §3: focus is a computed view, not a stored status)",
				path, lineNum, strings.TrimSpace(line),
			)
		}

		scanned := scanGoTree(t, filepath.Join(repoRoot, "internal"), isFocusStatusLiteral, report)
		scanned += scanGoTree(t, filepath.Join(repoRoot, "cmd"), isFocusStatusLiteral, report)
		if scanned == 0 {
			t.Fatal("scanned zero .go files under internal/ and cmd/ — D10's guard: nothing to check yet")
		}
	})
}

// isFocusStatusLiteral reports whether line carries both the literal
// "focus" and the substring "Status" — a coarse proxy for "a Status field
// or constant is being set to focus", per docs/06-harness.md §4's own
// framing.
func isFocusStatusLiteral(line string) bool {
	return strings.Contains(line, `"focus"`) && strings.Contains(line, "Status")
}
