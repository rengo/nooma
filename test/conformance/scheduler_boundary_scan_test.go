// Package conformance — see test/conformance/doc.go for the package contract.
package conformance

import (
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
// Two legs, landing at two different points in the m2d-scheduler-demo
// chain:
//
//  1. No non-test .go file under internal/scheduler contains the literal
//     "time.Hour". The scheduler's only two legitimate durations are
//     BootConsolidationDelay's 120 * time.Second (PR link 4) and the
//     time.Duration NextDailyRun's returned instant derives into (PR link
//     3a) — neither needs an hour-scale literal anywhere. VACUOUSLY TRUE
//     right now: internal/scheduler holds only doc.go, which contains no
//     such literal — disclosed here rather than claimed as a real red/green
//     step, the same posture m2a's own C9 established and this change's PR
//     1 (task 2.1, .golangci.yml) already took for the sibling
//     scheduler-boundary depguard rule. It becomes a live guard from PR
//     link 3a onward, once real scheduler files exist that could hold a
//     violation.
//
//  2. FORWARD REFERENCE ONLY — not implemented in this file. Once
//     CatchUpDue has a real caller (PR link 4), every non-test,
//     non-doc.go file under internal/scheduler must reference all three of
//     consolidation.CatchUpDue, consolidation.ResolveConsolidationEnabled
//     and consolidation.NextDailyRun at least once. That leg cannot exist
//     as a real check yet — there is no such file today — and is deferred
//     to PR link 4's own task 4.11, the same forward-reference pattern
//     m2c's task 1.6 used for a leg that could not exist yet either. Named
//     here, in the file leg 1 already lives in, so the obligation stays
//     visible rather than silently dropped.
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
}
