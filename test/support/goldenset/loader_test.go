package goldenset

import (
	"encoding/json"
	"os"
	"path/filepath"
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
	}{
		{
			name:        "recall",
			examplePath: "../../../testdata/recall/format_example.json",
			newValue:    func() any { return &RecallExample{} },
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
					if u.ID == "" || u.Type == "" || u.Content == "" {
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
		})
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
