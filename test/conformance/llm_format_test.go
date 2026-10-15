package conformance

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/rengo/nooma/test/support/goldenset"
	"github.com/rengo/nooma/testdata/llm"
)

// TestLLMFormatDocumentsEveryField keeps testdata/llm/format.md and the type
// that decodes its cases from drifting: every JSON field of
// goldenset.LLMExample has a row in the Fields table. The single fenced
// example cannot carry the reminder fields (the harness allows one fence), so
// this is the check that a field added to the type is also written down.
func TestLLMFormatDocumentsEveryField(t *testing.T) {
	path := filepath.Join(repoRootFromCaller(t), "testdata", "llm", "format.md")
	doc, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	typ := reflect.TypeOf(goldenset.LLMExample{})
	for i := 0; i < typ.NumField(); i++ {
		name, _, _ := strings.Cut(typ.Field(i).Tag.Get("json"), ",")
		if !strings.Contains(string(doc), "| `"+name+"` |") {
			t.Errorf("format.md has no Fields row for `%s`", name)
		}
	}
}

// TestRecordedFileKeysAreTheTypesFields keeps the key order `nooma doctor
// --record` writes (llm.FileFields) equal to the fields of the type that
// decodes a case: a field added to one and not the other would make the
// command refuse a valid file, or write one in a different order.
func TestRecordedFileKeysAreTheTypesFields(t *testing.T) {
	typ := reflect.TypeOf(goldenset.LLMExample{})
	var want []string
	for i := 0; i < typ.NumField(); i++ {
		name, _, _ := strings.Cut(typ.Field(i).Tag.Get("json"), ",")
		want = append(want, name)
	}
	if !reflect.DeepEqual(llm.FileFields, want) {
		t.Fatalf("llm.FileFields = %v, want %v", llm.FileFields, want)
	}
}
