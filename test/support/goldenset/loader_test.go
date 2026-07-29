package goldenset

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// TestGoldenSetFormatExamples proves, for each of the three golden-set
// formats (spec R10.3), that Load succeeds on the checked-in
// format_example.json and populates a value, and that Load rejects a copy
// of that same file with one added, undocumented field.
//
// The "one added, undocumented field" copy is derived programmatically
// from the real committed fixture (addUnknownField below), not from a
// second, separately maintained fixture file — so this test can never
// drift from the example it is supposed to be probing.
func TestGoldenSetFormatExamples(t *testing.T) {
	tests := []struct {
		name        string
		examplePath string
		newValue    func() any
		populated   func(t *testing.T, v any)
		// nestedPath, when non-empty, is a dot path (object keys, array
		// indices as plain integers) into a nested object that also gets
		// an added, undocumented field — proving DisallowUnknownFields
		// rejects a NESTED unknown field, not only a top-level one
		// (four-lens pre-PR review, WARNING finding 8). Left empty for
		// "llm", which has no nested object to inject into.
		nestedPath string
	}{
		{
			name:        "recall",
			examplePath: "../../../testdata/recall/format_example.json",
			newValue:    func() any { return &RecallExample{} },
			nestedPath:  "units.0",
			populated: func(t *testing.T, v any) {
				t.Helper()
				ex, ok := v.(*RecallExample)
				if !ok {
					t.Fatalf("value is %T, want *RecallExample", v)
				}
				if ex.ID == "" {
					t.Error("ID is empty")
				}
				if len(ex.Units) == 0 {
					t.Error("Units is empty")
				}
				for _, u := range ex.Units {
					if u.ID == "" || u.Type == "" || u.Content == "" || u.Status == "" {
						t.Errorf("unit %+v is not fully populated", u)
					}
				}
				if len(ex.Queries) == 0 {
					t.Error("Queries is empty")
				}
				for _, q := range ex.Queries {
					if q.Query == "" || len(q.ExpectedUnitIDs) == 0 {
						t.Errorf("query %+v is not fully populated", q)
					}
				}
			},
		},
		{
			name:        "classify",
			examplePath: "../../../testdata/classify/format_example.json",
			newValue:    func() any { return &ClassifyExample{} },
			nestedPath:  "expected",
			populated: func(t *testing.T, v any) {
				t.Helper()
				ex, ok := v.(*ClassifyExample)
				if !ok {
					t.Fatalf("value is %T, want *ClassifyExample", v)
				}
				if ex.ID == "" || ex.Input == "" {
					t.Errorf("ID or Input is empty: %+v", ex)
				}
				if ex.Expected.Type == "" || ex.Expected.NormalizedContent == "" {
					t.Errorf("Expected.Type or Expected.NormalizedContent is empty: %+v", ex.Expected)
				}
				if len(ex.Expected.StructuredData) == 0 {
					t.Error("Expected.StructuredData is empty")
				}
			},
		},
		{
			name:        "llm",
			examplePath: "../../../testdata/llm/format_example.json",
			newValue:    func() any { return &LLMExample{} },
			populated: func(t *testing.T, v any) {
				t.Helper()
				ex, ok := v.(*LLMExample)
				if !ok {
					t.Fatalf("value is %T, want *LLMExample", v)
				}
				if ex.ID == "" || ex.Provider == "" || ex.Model == "" ||
					ex.Task == "" || ex.Prompt == "" || ex.Response == "" {
					t.Errorf("example is not fully populated: %+v", ex)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := tt.newValue()
			if err := Load(tt.examplePath, v); err != nil {
				t.Fatalf("Load(%s) = %v, want success", tt.examplePath, err)
			}
			tt.populated(t, v)

			withExtra := filepath.Join(t.TempDir(), "format_example_with_extra_field.json")
			addUnknownField(t, tt.examplePath, withExtra)

			if err := Load(withExtra, tt.newValue()); err == nil {
				t.Fatalf(
					"Load(%s) with an added, undocumented field succeeded, want an error (R10.3 unknown-field rejection)",
					withExtra,
				)
			}

			if tt.nestedPath == "" {
				return
			}
			withNestedExtra := filepath.Join(t.TempDir(), "format_example_with_nested_extra_field.json")
			addNestedUnknownField(t, tt.examplePath, withNestedExtra, tt.nestedPath)

			if err := Load(withNestedExtra, tt.newValue()); err == nil {
				t.Fatalf(
					"Load(%s) with a NESTED added, undocumented field at %q succeeded, want an error "+
						"(R10.3 unknown-field rejection applies to nested fields too, four-lens pre-PR review finding 8)",
					withNestedExtra, tt.nestedPath,
				)
			}
		})
	}
}

// TestClassifyExampleLinksToLLMExample proves the checked-in
// testdata/classify/format_example.json's LLMCaseID actually resolves to
// the checked-in testdata/llm/format_example.json's ID — a structural
// gate, not the informal naming echo the two examples used to share with
// no field connecting them (four-lens pre-PR review, WARNING finding 6).
func TestClassifyExampleLinksToLLMExample(t *testing.T) {
	var classifyEx ClassifyExample
	if err := Load("../../../testdata/classify/format_example.json", &classifyEx); err != nil {
		t.Fatalf("Load(classify format_example.json) = %v, want success", err)
	}
	if classifyEx.LLMCaseID == "" {
		t.Fatal("classify format_example.json's llm_case_id is empty, want it to name an llm case id")
	}

	var llmEx LLMExample
	if err := Load("../../../testdata/llm/format_example.json", &llmEx); err != nil {
		t.Fatalf("Load(llm format_example.json) = %v, want success", err)
	}

	if classifyEx.LLMCaseID != llmEx.ID {
		t.Errorf(
			"classify example's llm_case_id = %q, llm example's id = %q, want them equal — "+
				"the link must resolve to a real case, not just echo a naming convention",
			classifyEx.LLMCaseID, llmEx.ID,
		)
	}
}

// addUnknownField copies src to dst, adding one top-level field the
// destination type does not declare — proving Load's
// DisallowUnknownFields actually rejects an added field, not merely that
// malformed JSON fails to parse.
func addUnknownField(t *testing.T, src, dst string) {
	t.Helper()

	raw, err := os.ReadFile(src)
	if err != nil {
		t.Fatalf("read %s: %v", src, err)
	}

	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("unmarshal %s: %v", src, err)
	}
	doc["__undocumented_probe_field"] = true

	out, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal modified %s: %v", src, err)
	}
	if err := os.WriteFile(dst, out, 0o644); err != nil {
		t.Fatalf("write %s: %v", dst, err)
	}
}

// addNestedUnknownField copies src to dst, adding one field the
// destination type does not declare inside the object reached by dotPath
// (object keys, array indices as plain integers, e.g. "units.0" or
// "expected") — proving DisallowUnknownFields rejects a NESTED unknown
// field, not only a top-level one (four-lens pre-PR review, WARNING
// finding 8). addUnknownField above only ever injects at the top level;
// this is its nested counterpart.
func addNestedUnknownField(t *testing.T, src, dst, dotPath string) {
	t.Helper()

	raw, err := os.ReadFile(src)
	if err != nil {
		t.Fatalf("read %s: %v", src, err)
	}

	var doc any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("unmarshal %s: %v", src, err)
	}

	cur := doc
	for _, seg := range strings.Split(dotPath, ".") {
		if idx, convErr := strconv.Atoi(seg); convErr == nil {
			arr, ok := cur.([]any)
			if !ok || idx >= len(arr) {
				t.Fatalf("addNestedUnknownField(%s, %q): segment %q is not a valid array index into %T", src, dotPath, seg, cur)
			}
			cur = arr[idx]
			continue
		}
		m, ok := cur.(map[string]any)
		if !ok {
			t.Fatalf("addNestedUnknownField(%s, %q): segment %q is not an object, got %T", src, dotPath, seg, cur)
		}
		next, ok := m[seg]
		if !ok {
			t.Fatalf("addNestedUnknownField(%s, %q): segment %q: key not found", src, dotPath, seg)
		}
		cur = next
	}

	obj, ok := cur.(map[string]any)
	if !ok {
		t.Fatalf("addNestedUnknownField(%s, %q) does not resolve to an object, got %T", src, dotPath, cur)
	}
	obj["__undocumented_nested_probe_field"] = true

	out, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal modified %s: %v", src, err)
	}
	if err := os.WriteFile(dst, out, 0o644); err != nil {
		t.Fatalf("write %s: %v", dst, err)
	}
}
