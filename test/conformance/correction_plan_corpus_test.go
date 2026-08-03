// Package conformance — see test/conformance/doc.go for the package contract.
package conformance

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/rengo/nooma/internal/core/classify"
	"github.com/rengo/nooma/internal/core/correction"
	"github.com/rengo/nooma/test/support/goldenset"
)

// TestPlanEditOverCorrectionCorpus is R1.8's own L2 Verified-by, run
// against the shared testdata/classify/cases corpus rather than inline
// literals — the "written once, used in two places" discipline
// m1b-pipeline established, applied here to plan_test.go's own L1 table
// (task 12c.3).
//
// Each correction case's own Expected fields decide, structurally, which
// row of R1.8's table it proves: a case whose Expected carries event_at and
// no due_at is this test's proof of the event_at-only row, and so on. A
// case is never annotated with the field it expects — the corpus already
// states the classification, and PlanEdit's own decision reads directly off
// it, the same way the real pipeline would.
//
// Expected is re-marshaled and run back through classify.Decode rather than
// read directly, so this test drives PlanEdit against a real Classification
// — the same value the real pipeline produces — not a value hand-built to
// resemble one.
func TestPlanEditOverCorrectionCorpus(t *testing.T) {
	repoRoot := repoRootFromCaller(t)
	casesDir := filepath.Join(repoRoot, "testdata", "classify", "cases")

	entries, err := os.ReadDir(casesDir)
	if err != nil {
		t.Fatalf("reading %s: %v", casesDir, err)
	}

	seen := 0
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}

		var example goldenset.ClassifyExample
		path := filepath.Join(casesDir, entry.Name())
		if err := goldenset.Load(path, &example); err != nil {
			t.Fatalf("loading %s: %v", path, err)
		}
		if example.Expected.Type != string(classify.KindCorrection) {
			continue
		}
		seen++

		t.Run(entry.Name(), func(t *testing.T) {
			raw, err := json.Marshal(example.Expected)
			if err != nil {
				t.Fatalf("marshaling %s's expected block: %v", entry.Name(), err)
			}
			c, err := classify.Decode(string(raw), corpusNow)
			if err != nil {
				t.Fatalf("classify.Decode: %v", err)
			}

			edits, ok := correction.PlanEdit(c)

			hasEvent := c.EventAt != nil
			hasDue := c.DueAt != nil
			switch {
			case hasEvent && hasDue:
				if ok {
					t.Fatalf("PlanEdit ok = true for a both-dates case, want false (ask)")
				}
			case hasEvent:
				assertPlansSingleField(t, edits, ok, correction.FieldEventAt)
			case hasDue:
				assertPlansSingleField(t, edits, ok, correction.FieldDueAt)
			case c.NormalizedContent != nil:
				assertPlansSingleField(t, edits, ok, correction.FieldContent)
			default:
				if ok {
					t.Fatalf("PlanEdit ok = true for a no-date-no-content case, want false (ask)")
				}
			}
		})
	}

	if seen == 0 {
		t.Fatal("no correction case in the corpus — R1.8's L2 proof has nothing to run against")
	}
}

func assertPlansSingleField(t *testing.T, edits []correction.Edit, ok bool, want correction.Field) {
	t.Helper()
	if !ok {
		t.Fatalf("PlanEdit ok = false, want true")
	}
	if len(edits) != 1 {
		t.Fatalf("PlanEdit returned %d edits, want 1", len(edits))
	}
	if got := edits[0].Field(); got != want {
		t.Fatalf("PlanEdit wrote %q, want %q", got, want)
	}
}
