package conformance

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/rengo/nooma/internal/core/consolidation"
)

// TestConsolidationExpiryHoursMatchesDoc02 pins consolidation.IncompleteExpiryHours
// to the number docs/02-cognitive-core.md states in prose, read off disk.
//
// The constant has no config column and no schema DEFAULT, so the DDL-pin
// pattern consolidation_defaults_ddl_test.go uses for DefaultWeightThreshold
// has nothing to pin against. What it does have is doc 02 stating "24 h" twice
// as a normative literal — §1's own definition of the incomplete status and
// §6.1's phase description — and spec R2.1 writing it as a MUST.
//
// Judgment Day round 1 on this PR found the gap, both judges independently:
// every fixture in expire_test.go derives its offsets from the constant itself
// (now.Add(-IncompleteExpiryHours * time.Hour)), so the suite pins the boundary
// LOGIC and never the calibrated NUMBER. Mutating the constant to 23 or to 25
// left the whole package green at 100% statement coverage — the same
// coverage-is-not-discrimination shape m2a's linear-decay incident recorded,
// and the same gap conflicts C7 and C28 already closed twice elsewhere for
// exactly this class of value.
//
// This test is the doc-text equivalent of that DDL pin: the number lives in
// one place a human calibrates (doc 02), and drifting either side fails here.
func TestConsolidationExpiryHoursMatchesDoc02(t *testing.T) {
	repoRoot := repoRootFromCaller(t)
	docPath := filepath.Join(repoRoot, "docs", "02-cognitive-core.md")

	content, err := os.ReadFile(docPath)
	if err != nil {
		t.Fatalf("read %s: %v", docPath, err)
	}

	// Both sentences that state the number normatively. Each must be present,
	// and each must carry the same figure the Go constant does.
	anchors := []struct {
		name    string
		pattern *regexp.Regexp
	}{
		{
			name:    "§1's incomplete-status definition",
			pattern: regexp.MustCompile(`unresolved, after (\d+) h during consolidation`),
		},
		{
			name:    "§6.1's phase description",
			pattern: regexp.MustCompile(`\x60incomplete\x60 units older than (\d+) h`),
		},
	}

	want := fmt.Sprintf("%d", consolidation.IncompleteExpiryHours)

	for _, a := range anchors {
		t.Run(a.name, func(t *testing.T) {
			m := a.pattern.FindSubmatch(content)
			if m == nil {
				t.Fatalf("docs/02-cognitive-core.md no longer matches %v — %s was reworded, so this pin cannot see the number it exists to check", a.pattern, a.name)
			}
			got := string(m[1])
			if got != want {
				t.Fatalf("%s states %s h, consolidation.IncompleteExpiryHours is %s — one number, not two that drift", a.name, got, want)
			}
		})
	}

	// §13's calibration row carries it a third time, in a table cell.
	row := regexp.MustCompile(`\|\s*\x60incomplete_expiry_hours\x60[^|]*\|\s*(\d+)\s*\|`)
	if m := row.FindSubmatch(content); m != nil {
		if got := string(m[1]); got != want {
			t.Fatalf("§13's incomplete_expiry_hours row states %s, consolidation.IncompleteExpiryHours is %s", got, want)
		}
	} else if strings.Contains(string(content), "incomplete_expiry_hours") {
		t.Fatalf("docs/02-cognitive-core.md mentions incomplete_expiry_hours but §13's row no longer parses — this pin cannot see it")
	}
}
