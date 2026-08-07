package conformance

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/rengo/nooma/internal/core/consolidation"
)

// phasesInOrderPreamble marks doc 02 §6's preamble sentence; the arrow
// line naming the eight phases follows inside the next fenced code block
// (docs/02-cognitive-core.md:657-661 at the time this test was written).
const phasesInOrderPreamble = "phases IN ORDER"

// TestI11_PhaseOrderMatchesDoc02ArrowLine proves I11's third leg (design.md
// §3.2): consolidation.Order()'s names, joined by " → ", must equal doc
// 02 §6's own arrow line — parsed off disk at test time, not restated as a
// second copy of the eight names inside this file. This is the first
// application of the migration-text-parsing trick (i13_learning_signal_test.go,
// migrationSQLText) to doc 02 prose rather than to SQL DDL.
//
// This is not a missing-symbol RED: Order, String and phaseNames already
// compile and pass their own tests in phase_test.go, added earlier in this
// same PR (m2a C9's disclosure convention — this is a doc-vs-code pin, not
// a TDD red step).
func TestI11_PhaseOrderMatchesDoc02ArrowLine(t *testing.T) {
	repoRoot := repoRootFromCaller(t)
	docPath := filepath.Join(repoRoot, "docs", "02-cognitive-core.md")

	content, err := os.ReadFile(docPath)
	if err != nil {
		t.Fatalf("read %s: %v", docPath, err)
	}
	lines := strings.Split(string(content), "\n")

	preambleIdx := -1
	for i, line := range lines {
		if strings.Contains(line, phasesInOrderPreamble) {
			preambleIdx = i
			break
		}
	}
	if preambleIdx == -1 {
		t.Fatalf("docs/02-cognitive-core.md has no %q preamble — nothing to check", phasesInOrderPreamble)
	}

	arrowLine := ""
	for _, line := range lines[preambleIdx+1:] {
		if strings.Contains(line, "→") {
			arrowLine = strings.TrimSpace(line)
			break
		}
	}
	if arrowLine == "" {
		t.Fatalf("found no arrow line after the %q preamble in docs/02-cognitive-core.md", phasesInOrderPreamble)
	}

	order := consolidation.Order()
	if len(order) == 0 {
		t.Fatal("consolidation.Order() returned zero phases — nothing to check yet")
	}

	names := make([]string, 0, len(order))
	for _, p := range order {
		// Totality (design §3.2's leg-3 note): phaseNames() is an array
		// sized by phaseCount, so an unnamed slot renders "" here rather
		// than a subtle mis-order — checked through the exported surface,
		// since phaseNames() itself is unexported.
		s := p.String()
		if s == "" {
			t.Fatalf("Phase %d's String() is empty — phaseNames() is not total over Order()'s members (I11)", int(p))
		}
		names = append(names, s)
	}

	got := strings.Join(names, " → ")
	if got != arrowLine {
		t.Fatalf("consolidation.Order()'s names joined = %q, want docs/02-cognitive-core.md §6's arrow line %q", got, arrowLine)
	}
}

// TestI11_NoCallerOutsideConsolidationListsThePhaseNames proves I11's
// fourth leg (design.md §3.2): a caller may switch over the Phase
// constants (that is the type, not a list), but no non-test .go file
// outside internal/core/consolidation may keep its own copy of the eight
// phase-name string literals — a future runner or CLI help renderer must
// build its names from Order() + String(), never restate them.
func TestI11_NoCallerOutsideConsolidationListsThePhaseNames(t *testing.T) {
	repoRoot := repoRootFromCaller(t)

	consolidationDir, err := filepath.Abs(filepath.Join(repoRoot, "internal", "core", "consolidation"))
	if err != nil {
		t.Fatalf("resolve consolidation dir: %v", err)
	}

	phaseLiterals := make(map[string]bool, len(consolidation.Order()))
	for _, p := range consolidation.Order() {
		phaseLiterals[p.String()] = true
	}
	if len(phaseLiterals) == 0 {
		t.Fatal("collected zero phase-name literals from consolidation.Order() — nothing to scan for")
	}

	fset := token.NewFileSet()
	scanned := 0

	err = filepath.WalkDir(repoRoot, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			if d.Name() == ".git" {
				return filepath.SkipDir
			}
			// .claude/worktrees holds live git worktrees of this same
			// repository, created by review agents. Each contains its own
			// copy of internal/core/consolidation/phase.go, which this scan
			// would otherwise report as a second package listing the phase
			// names — a spurious failure that depends on whether another
			// agent happens to be running. Found during Judgment Day on
			// PR 5, where a sibling worktree made this test fail against an
			// unmodified tree.
			if d.Name() == "worktrees" && filepath.Base(filepath.Dir(path)) == ".claude" {
				return filepath.SkipDir
			}
			abs, absErr := filepath.Abs(path)
			if absErr != nil {
				return absErr
			}
			if abs == consolidationDir {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}

		file, parseErr := parser.ParseFile(fset, path, nil, 0)
		if parseErr != nil {
			return parseErr
		}
		scanned++

		found := map[string]bool{}
		ast.Inspect(file, func(n ast.Node) bool {
			lit, ok := n.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			value, unquoteErr := strconv.Unquote(lit.Value)
			if unquoteErr != nil {
				return true
			}
			if phaseLiterals[value] {
				found[value] = true
			}
			return true
		})

		if len(found) >= 2 {
			names := make([]string, 0, len(found))
			for name := range found {
				names = append(names, name)
			}
			sort.Strings(names)
			t.Errorf(
				"%s contains %d of the eight phase-name string literals %v — a caller must switch over the Phase constants, not keep its own list (R1.2)",
				path, len(names), names,
			)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", repoRoot, err)
	}

	if scanned == 0 {
		t.Fatal("scanned zero non-test .go files outside internal/core/consolidation — D10's guard: nothing to check yet")
	}
}
