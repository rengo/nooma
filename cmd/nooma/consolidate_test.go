package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/rengo/nooma/internal/brain"
	"github.com/rengo/nooma/internal/core/consolidation"
)

// TestRenderConsolidateReport_NamesRefusedUnits closes the gap Judgment Day
// found on this PR: every corrupted-capable phase (archive, strengthen,
// reweight, derive) accumulates refused unit ids into ConsolidateReport's
// corrupted set, and that set is deliberately kept OUT of decision_log —
// internal/brain/consolidate.go says so, and adds "PR 12 owns the eventual
// public report shape".
//
// This is PR 12. Before this test the CLI discarded the report entirely
// (`if _, err := svc.Consolidate(...)`) and rendered a static string built
// from the REQUEST, so a refused unit appeared nowhere: not in
// decision_log by design, and not on stdout by omission. A vault holding a
// non-finite weight would be skipped silently, every pass, forever.
func TestRenderConsolidateReport_NamesRefusedUnits(t *testing.T) {
	t.Run("refused units are named", func(t *testing.T) {
		var out bytes.Buffer
		if err := renderConsolidateReport(&out, brain.ConsolidateRequest{}, []string{"u-nan", "u-inf"}); err != nil {
			t.Fatalf("renderConsolidateReport: %v", err)
		}

		got := out.String()
		for _, id := range []string{"u-nan", "u-inf"} {
			if !strings.Contains(got, id) {
				t.Errorf("report does not name refused unit %q — a refusal is never written to decision_log, so this is the only place it can surface:\n%s", id, got)
			}
		}
	})

	t.Run("a clean pass says nothing about refusals", func(t *testing.T) {
		var out bytes.Buffer
		if err := renderConsolidateReport(&out, brain.ConsolidateRequest{}, nil); err != nil {
			t.Fatalf("renderConsolidateReport: %v", err)
		}
		if strings.Contains(strings.ToLower(out.String()), "refused") {
			t.Errorf("a pass that refused nothing must not mention refusals:\n%s", out.String())
		}
	})

	t.Run("a per-phase run still names its phase", func(t *testing.T) {
		var out bytes.Buffer
		phase := consolidation.PhaseArchive
		if err := renderConsolidateReport(&out, brain.ConsolidateRequest{Phase: &phase}, nil); err != nil {
			t.Fatalf("renderConsolidateReport: %v", err)
		}
		if !strings.Contains(out.String(), consolidation.PhaseArchive.String()) {
			t.Errorf("per-phase report does not name the phase:\n%s", out.String())
		}
	})
}
