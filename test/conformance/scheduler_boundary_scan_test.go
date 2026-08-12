// Package conformance — see test/conformance/doc.go for the package contract.
package conformance

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSchedulerBoundaryScan is design §3.1 item 4's source scan: a coarse,
// line-based heuristic over internal/scheduler's own non-test files, named
// as a scan and not a proof (design §3.1 item 4's own words) — it catches
// the realistic regression of someone re-deriving "24h" or "03:00" inline
// instead of asking internal/core/consolidation's three pure decisions, and
// nothing stronger.
//
// Three legs:
//
//  1. No non-test .go file under internal/scheduler contains the literal
//     "time.Hour". The scheduler's only two legitimate durations are
//     BootConsolidationDelay's 120 * time.Second and the time.Duration
//     NextDailyRun's returned instant derives into — neither needs an
//     hour-scale literal anywhere.
//
//  2. Every one of the three consolidation.CatchUpDue,
//     consolidation.ResolveConsolidationEnabled, consolidation.NextDailyRun
//     symbols has a real call site at least once, SOMEWHERE across the
//     non-test, non-doc.go files under internal/scheduler — collectively,
//     not per file. A per-file reading (task 2.5/design §3.1 item 4's own
//     "every... file... references all three" wording, read most
//     literally) is unsatisfiable by construction: timer.go is a bare
//     seam over time.After (design §5.2's own "no meaningful red is
//     possible" file) and has no legitimate reason to import
//     internal/core/consolidation at all. The collective reading is what
//     design §3.1 item 4's own prose actually states ("non-test files
//     under internal/scheduler reference all three core symbols") and
//     what this leg checks: the package as a whole never re-derives a
//     duration/hour/bool decision it could instead ask the three pure
//     functions for. Deviation from task 2.5's own per-file phrasing,
//     disclosed rather than silently resolved — the same posture PR 3b
//     took for design §3.4's skip-log wording. The scope kept exactly
//     this collective reading is JD-4-01's own confirmed-correct part;
//     only the detection methodology below was defective.
//
//     Discharges PR link 2's own task 2.5 forward reference. Genuinely
//     red before this PR's own catchup.go landed: CatchUpDue had zero
//     callers anywhere in the chain until task 4.2, so a scan run against
//     the tree as PR 3b left it (scheduler.go, cron.go, timer.go only)
//     finds CatchUpDue referenced nowhere and fails — verified directly
//     by a disclosed temporary probe (catchup.go moved out of the
//     package, the leg re-run, observed failing exactly on the missing
//     CatchUpDue reference, then restored), the same technique task
//     3a.11 used to prove leg 1 was genuinely live rather than merely
//     passing because there was nothing to check.
//
//     JD-4-01 correction: the original implementation checked
//     strings.Contains(text, sym) over each file's raw bytes, so a symbol
//     name appearing only in a doc comment (catchup.go's own line-24
//     comment mentions CatchUpDue; cron.go's own lines 9/12 mention
//     NextDailyRun) satisfied the check even with the real call deleted —
//     verified by a disclosed temporary probe below. This leg now parses
//     each scanned file with go/parser and looks for an actual
//     *ast.CallExpr selecting the symbol (a real call site), which
//     comments and string literals cannot satisfy. The collective,
//     package-union scope is unchanged.
//
//  3. No non-test .go file under internal/scheduler references
//     "time_based", "expired", or "cancelled" as a Go string literal
//     (spec R2.4's own MUST NOT — no part of ADR-0009 beyond
//     "Consolidation — always recovered" is implemented in this chain;
//     the time-based trigger and ephemeral timer kinds, their staleness
//     gates, and their expiry/cancellation status transitions are M3's
//     scope). Not genuinely red: nothing in this chain has ever
//     referenced those literals, so this leg is a guard against a future
//     regression, not a proven-broken state — the same posture m2a's own
//     C9 established for a check with nothing yet to catch.
func TestSchedulerBoundaryScan(t *testing.T) {
	t.Run("leg 1: no time.Hour literal outside tests", func(t *testing.T) {
		repoRoot := repoRootFromCaller(t)
		dir := filepath.Join(repoRoot, "internal", "scheduler")

		scanned := 0
		err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			scanned++

			content, readErr := os.ReadFile(path)
			if readErr != nil {
				return readErr
			}
			for i, line := range strings.Split(string(content), "\n") {
				if strings.Contains(line, "time.Hour") {
					t.Errorf(
						"%s:%d: %q — the scheduler's only durations are "+
							"BootConsolidationDelay (120*time.Second, PR link 4) and "+
							"NextDailyRun's own returned instant (PR link 3a); "+
							"re-deriving an hour-scale duration inline is exactly the "+
							"regression design §3.1 item 4 watches for",
						path, i+1, strings.TrimSpace(line),
					)
				}
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", dir, err)
		}
		if scanned == 0 {
			t.Fatal("scanned zero non-test .go files under internal/scheduler — nothing to check yet")
		}
	})

	t.Run("leg 2: package references all three core decisions", func(t *testing.T) {
		repoRoot := repoRootFromCaller(t)
		dir := filepath.Join(repoRoot, "internal", "scheduler")

		want := []string{"CatchUpDue", "ResolveConsolidationEnabled", "NextDailyRun"}
		found := make(map[string]bool, len(want))

		fset := token.NewFileSet()
		scanned := 0
		err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") || filepath.Base(path) == "doc.go" {
				return nil
			}
			scanned++

			// go/parser, not strings.Contains over raw bytes (JD-4-01): a
			// symbol name inside a comment or a string literal must not
			// satisfy this check — only a real *ast.CallExpr selecting the
			// symbol does, since that is the only thing that actually
			// delegates the decision to internal/core/consolidation.
			file, parseErr := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
			if parseErr != nil {
				return parseErr
			}
			ast.Inspect(file, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				for _, sym := range want {
					if sel.Sel.Name == sym {
						found[sym] = true
					}
				}
				return true
			})
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", dir, err)
		}
		if scanned == 0 {
			t.Fatal("scanned zero non-test, non-doc.go files under internal/scheduler — nothing to check yet")
		}
		for _, sym := range want {
			if !found[sym] {
				t.Errorf(
					"no non-test, non-doc.go file under internal/scheduler has a real call site "+
						"for consolidation.%s — the package's only durable proof it decides "+
						"nothing itself is that every one of the three core pure functions "+
						"has a real caller here, and a mention inside a comment or string "+
						"literal does not count",
					sym,
				)
			}
		}
	})

	t.Run("leg 3: no ADR-0009 status literal beyond this chain's own scope", func(t *testing.T) {
		repoRoot := repoRootFromCaller(t)
		dir := filepath.Join(repoRoot, "internal", "scheduler")

		denied := []string{"time_based", "expired", "cancelled"}
		scanned := 0
		err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			scanned++

			content, readErr := os.ReadFile(path)
			if readErr != nil {
				return readErr
			}
			for i, line := range strings.Split(string(content), "\n") {
				for _, lit := range denied {
					if strings.Contains(line, `"`+lit+`"`) {
						t.Errorf(
							"%s:%d: %q — spec R2.4's own MUST NOT: this chain implements no "+
								"part of ADR-0009 beyond \"Consolidation — always recovered\"; "+
								"the time-based trigger's own staleness gate, the ephemeral "+
								"timer's own staleness gate, and any expiry/cancel status "+
								"transition are M3's scope",
							path, i+1, strings.TrimSpace(line),
						)
					}
				}
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", dir, err)
		}
		if scanned == 0 {
			t.Fatal("scanned zero non-test .go files under internal/scheduler — nothing to check yet")
		}
	})
}
