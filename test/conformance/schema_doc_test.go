package conformance

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rengo/nooma/test/support/schema"
)

// doc03RelPath and structureGoldenRelPath are read directly from disk, the
// same "reads text, no live SQLite connection" shape R5.2/R4.3's own
// "Verified by" note requires of this L2 gate — see i13_learning_signal_test.go
// for the identical rationale (design D1/§6.4).
const (
	doc03RelPath           = "docs/03-data-model.md"
	structureGoldenRelPath = "testdata/schema/structure.golden"
)

// TestHarness_SchemaMatchesDoc03 asserts R4.3: every CREATE TABLE, INDEX,
// VIRTUAL TABLE and TRIGGER statement docs/03-data-model.md's fenced ```sql```
// blocks declare has a matching object of the same name and kind in the
// committed schema golden (structural comparison only — R4.2 — never a
// byte-exact one), and vice versa (R9.2's own verification note: this gate
// is what continuously proves the FTS5 trigger DDL 4a added to doc 03 stays
// true).
func TestHarness_SchemaMatchesDoc03(t *testing.T) {
	repoRoot := repoRootFromCaller(t)

	docBytes, err := os.ReadFile(filepath.Join(repoRoot, doc03RelPath))
	if err != nil {
		t.Fatalf("read %s: %v", doc03RelPath, err)
	}
	docObjs, err := schema.ParseMarkdown(docBytes)
	if err != nil {
		t.Fatalf("schema.ParseMarkdown(%s) = _, %v, want nil error", doc03RelPath, err)
	}
	if len(docObjs) == 0 {
		t.Fatalf("schema.ParseMarkdown(%s) returned zero objects — the parser is broken, not doc 03 (design D10's non-empty-corpus guard)", doc03RelPath)
	}

	goldenBytes, err := os.ReadFile(filepath.Join(repoRoot, structureGoldenRelPath))
	if err != nil {
		t.Fatalf("read %s: %v (run `make schema-golden` first)", structureGoldenRelPath, err)
	}
	goldenObjs, err := schema.ParseGolden(goldenBytes)
	if err != nil {
		t.Fatalf("schema.ParseGolden(%s) = _, %v, want nil error", structureGoldenRelPath, err)
	}
	if len(goldenObjs) == 0 {
		t.Fatalf("schema.ParseGolden(%s) returned zero objects — the parser is broken, not the golden (design D10's non-empty-corpus guard)", structureGoldenRelPath)
	}

	if diffs := schema.Diff(docObjs, goldenObjs); len(diffs) > 0 {
		t.Errorf("%s", formatSchemaDiff(diffs))
	}
}

// formatSchemaDiff renders design §6.5's three-part failure report: what
// doc 03 declares but the schema lacks, what the schema has but doc 03
// never declares, and, for any table/virtual_table present on both sides,
// which columns disagree in which direction.
func formatSchemaDiff(diffs []schema.Difference) string {
	var b strings.Builder
	b.WriteString("schema does not match docs/03-data-model.md:\n")
	for _, d := range diffs {
		switch d.DiffKind {
		case schema.DiffDuplicateInDoc:
			fmt.Fprintf(&b, "  doc 03 declares the same object twice: %s %s\n", d.Kind, d.Name)
		case schema.DiffMissingFromSchema:
			fmt.Fprintf(&b, "  declared in doc 03 but absent from the schema: %s %s\n", d.Kind, d.Name)
		case schema.DiffUndeclaredInDoc:
			fmt.Fprintf(&b, "  present in the schema but not declared in doc 03: %s %s\n", d.Kind, d.Name)
		case schema.DiffColumnMismatch:
			fmt.Fprintf(&b, "  %s %s — column sets differ:\n", d.Kind, d.Name)
			fmt.Fprintf(&b, "    only in doc 03: %s\n", formatColumnList(d.OnlyInDoc))
			fmt.Fprintf(&b, "    only in schema: %s\n", formatColumnList(d.OnlyInSchema))
		default:
			fmt.Fprintf(&b, "  unrecognized difference kind %q for %s %s\n", d.DiffKind, d.Kind, d.Name)
		}
	}
	return b.String()
}

func formatColumnList(cols []string) string {
	if len(cols) == 0 {
		return "(none)"
	}
	return strings.Join(cols, ", ")
}

// TestSchemaDocAnchorsExpectedObjectCount is the "anchor the parser outside
// itself" safety net this task's brief demanded explicitly (the precedent
// is test/integration/schema_golden_anchor_test.go's hand-written line
// list for the golden's own generator, slice 4a): a hand-written,
// NOT-generated list of every object docs/03-data-model.md declares today,
// checked against schema.ParseMarkdown's return value by exact set
// equality. If ParseMarkdown starts silently dropping an object it fails
// to understand (rather than erroring, which is markdown_test.go's job to
// prove separately), the count or the membership check here fails even
// though TestHarness_SchemaMatchesDoc03 above might not notice — a doc 03
// object dropped from BOTH the parsed doc set and (coincidentally) never
// declared in the schema either would produce no Diff at all, exactly the
// false-assurance failure mode this task's brief warns about.
func TestSchemaDocAnchorsExpectedObjectCount(t *testing.T) {
	repoRoot := repoRootFromCaller(t)

	docBytes, err := os.ReadFile(filepath.Join(repoRoot, doc03RelPath))
	if err != nil {
		t.Fatalf("read %s: %v", doc03RelPath, err)
	}
	docObjs, err := schema.ParseMarkdown(docBytes)
	if err != nil {
		t.Fatalf("schema.ParseMarkdown(%s) = _, %v, want nil error", doc03RelPath, err)
	}

	got := make(map[string]bool, len(docObjs))
	for _, o := range docObjs {
		got[string(o.Kind)+" "+o.Name] = true
	}

	// When you add a CREATE statement to docs/03-data-model.md, add its
	// object line here too — that is the whole point of this anchor.
	//
	// Hand-written, not generated: every object docs/03-data-model.md's
	// fenced ```sql``` blocks declare today, kept as one flat list (not
	// derived from anything schema.ParseMarkdown itself computes) so
	// nothing here shares code with the thing being guarded against.
	required := []string{
		// Core tables
		"table units",
		"index idx_units_status_touched",
		"unique_index idx_units_unique_active_insight",
		"table relations",
		"table triggers",
		"index idx_triggers_status_fire",
		"table timers",
		"table self_beliefs",
		"table current_state",
		"table decision_log",
		"index idx_decision_log_occurred",
		// Learning
		"table learning_signals",
		"index idx_learning_signals_occurred",
		"table learning_state",
		"table relation_thresholds",
		"table calibration",
		// Measurements
		"table measurements",
		"index idx_measurements_metric",
		"index idx_measurements_ref_unit",
		// System config
		"table config",
		// Search
		"table unit_embeddings",
		"index idx_unit_embeddings_model",
		"virtual_table units_fts",
		"trigger units_fts_ai",
		"trigger units_fts_ad",
		"trigger units_fts_au",
	}

	for _, want := range required {
		if !got[want] {
			t.Errorf("schema.ParseMarkdown(%s) is missing required object %q", doc03RelPath, want)
		}
	}
	if len(docObjs) != len(required) {
		t.Errorf("schema.ParseMarkdown(%s) returned %d objects, want exactly %d (the hand-written anchor list) — an unexpected count means either a genuine doc 03 change this anchor was not updated for, or the parser silently gained or dropped an object",
			doc03RelPath, len(docObjs), len(required))
	}
}
