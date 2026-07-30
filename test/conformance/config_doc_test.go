package conformance

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	yaml "github.com/goccy/go-yaml"

	"github.com/rengo/nooma/internal/config"
	"github.com/rengo/nooma/test/support/mdfence"
)

// docSection and docLanguage locate the one block in docs/01-architecture.md that
// this gate treats as authoritative.
const (
	docPath     = "docs/01-architecture.md"
	docSection  = "Configuration"
	docLanguage = "yaml"
)

// TestHarness_ConfigMatchesDoc01 makes docs/01-architecture.md's configuration
// example executable (spec R9.1).
//
// The README declares doc 02 the source of truth for behavior; doc 01 is the
// source of truth for the shape of a nooma.yml, and prose nobody executes drifts
// from the code. Always. This gate is what stops that: the documented example
// must decode into config.Config, and the two key sets must match in both
// directions — a documented key with no field, and a field with no documented
// key, are both failures.
//
// The two directions are not caught in the same place, and it is worth being
// precise about that rather than implying symmetry. Both were observed failing
// before this test was trusted:
//
//	a field added to config.Config  -> the key-set comparison below reports
//	                                   "server.read_timeout"
//	a key added to doc 01           -> config.Decode rejects it first, with a
//	                                   line, a column and a caret
//
// So the documented-but-absent direction is normally caught by the strict decode,
// and the key-set check for it is a backstop: it is what still guards this
// property the day somebody relaxes the decode to "just check the shape". It is
// deliberately kept, and deliberately not described as the primary mechanism.
//
// Three properties are worth stating, because each one is a way this gate could
// have looked healthy while checking nothing.
//
// **It compares the schema, not a re-encoding.** An earlier design compared key
// sets by marshalling the decoded value back to YAML. That is vacuous the moment
// any field carries `omitempty`: a zero-valued undocumented field vanishes from
// the re-encode and the gate passes. The schema side here comes from reflect over
// the struct tags, independent of any particular value's zero-ness.
//
// **Map-keyed sections union across entries.** `providers` and `tasks` have
// user-chosen keys, so their key paths collapse to `providers.*`, and the fields
// observed under every entry are unioned before comparing. Doc 01's four provider
// entries use disjoint field subsets — `anthropic` needs `api_key_env`, `ollama`
// needs `endpoint`, `whisper_cpp` needs `binary_path` — so a per-entry
// completeness check would be unsatisfiable on the real document, or on any
// realistic one.
//
// **It decodes and compares; it MUST NOT validate.** Doc 01's tasks block
// contains the literal placeholder `embedding: { provider: ... }`, and `...`
// decodes cleanly to the Go string "...". The documented example decodes, which is
// all this gate checks. It would fail any validator that checked that provider
// name against the declared `providers:` map — and that failure would be the gate
// wrongly blaming doc 01 for not being a validator. The first person to "improve"
// this gate by adding validation breaks it; this comment exists so that
// improvement gets rejected in review instead of merged.
func TestHarness_ConfigMatchesDoc01(t *testing.T) {
	repoRoot := repoRootFromCaller(t)

	md, err := os.ReadFile(filepath.Join(repoRoot, docPath))
	if err != nil {
		t.Fatalf("read %s: %v", docPath, err)
	}

	block, err := mdfence.Extract(md, docSection, docLanguage)
	if err != nil {
		t.Fatalf("extracting the configuration example from %s: %v", docPath, err)
	}

	// Decoding through the loader's own entry point is the point: the gate proves
	// the documented example survives the same strict rules a user's file does,
	// not merely that it is well-formed YAML.
	if _, err := config.Decode(strings.NewReader(string(block))); err != nil {
		t.Fatalf("%s's configuration example does not decode:\n%v", docPath, err)
	}

	var document map[string]any
	if err := yaml.Unmarshal(block, &document); err != nil {
		t.Fatalf("re-reading the example as a generic document: %v", err)
	}

	schema := schemaKeyPaths(reflect.TypeOf(config.Config{}), "")
	documented := documentKeyPaths(document, reflect.TypeOf(config.Config{}), "")

	if len(schema) == 0 || len(documented) == 0 {
		t.Fatalf("collected %d schema paths and %d documented paths — the walk is broken, not the documents", len(schema), len(documented))
	}

	if missing := difference(schema, documented); len(missing) > 0 {
		t.Errorf(
			"config.Config has %d field(s) that %s does not document:\n  %s\n\n"+
				"Every field must appear in the documented example, so a reader of the doc\n"+
				"sees the whole schema. Add them to the yaml block in %s §%q.",
			len(missing), docPath, strings.Join(missing, "\n  "), docPath, docSection)
	}

	if extra := difference(documented, schema); len(extra) > 0 {
		t.Errorf(
			"%s documents %d key(s) that config.Config does not have:\n  %s\n\n"+
				"A documented key with no field is a promise the loader will reject at runtime\n"+
				"with an unknown-key error. Either add the field or remove the key.",
			docPath, len(extra), strings.Join(extra, "\n  "))
	}
}

// schemaKeyPaths walks a Go type through its yaml struct tags and returns every
// key path it defines. A map field contributes `name.*` plus whatever its value
// type defines beneath that, because the map's own keys are user data and carry no
// schema.
func schemaKeyPaths(t reflect.Type, prefix string) map[string]bool {
	out := map[string]bool{}
	collectSchema(t, prefix, out)
	return out
}

func collectSchema(t reflect.Type, prefix string, out map[string]bool) {
	switch t.Kind() {
	case reflect.Pointer:
		collectSchema(t.Elem(), prefix, out)
	case reflect.Map:
		out[prefix+".*"] = true
		collectSchema(t.Elem(), prefix+".*", out)
	case reflect.Struct:
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			name := yamlName(f)
			if name == "" {
				continue
			}
			path := join(prefix, name)
			out[path] = true
			collectSchema(f.Type, path, out)
		}
	}
}

// documentKeyPaths walks the decoded document alongside the schema type, so that
// a section the schema declares as a map has its user-chosen keys collapsed to
// `*`. That collapse is what unions the fields observed across every entry: four
// provider entries with disjoint fields contribute one merged set under
// `providers.*`.
func documentKeyPaths(node any, t reflect.Type, prefix string) map[string]bool {
	out := map[string]bool{}
	collectDocument(node, t, prefix, out)
	return out
}

func collectDocument(node any, t reflect.Type, prefix string, out map[string]bool) {
	for t != nil && t.Kind() == reflect.Pointer {
		t = t.Elem()
	}

	mapping, ok := asMapping(node)
	if !ok || t == nil {
		return
	}

	switch t.Kind() {
	case reflect.Map:
		out[prefix+".*"] = true
		for _, v := range mapping {
			collectDocument(v, t.Elem(), prefix+".*", out)
		}
	case reflect.Struct:
		fields := map[string]reflect.StructField{}
		for i := 0; i < t.NumField(); i++ {
			if name := yamlName(t.Field(i)); name != "" {
				fields[name] = t.Field(i)
			}
		}
		for key, value := range mapping {
			path := join(prefix, key)
			out[path] = true
			if f, known := fields[key]; known {
				collectDocument(value, f.Type, path, out)
			}
			// An unknown key still contributes its path, which is what makes the
			// documented-but-not-in-schema direction fail rather than pass quietly.
		}
	}
}

// asMapping normalises the two shapes a YAML mapping can decode into.
func asMapping(node any) (map[string]any, bool) {
	switch m := node.(type) {
	case map[string]any:
		return m, true
	case map[any]any:
		out := make(map[string]any, len(m))
		for k, v := range m {
			key, ok := k.(string)
			if !ok {
				return nil, false
			}
			out[key] = v
		}
		return out, true
	default:
		return nil, false
	}
}

func yamlName(f reflect.StructField) string {
	if !f.IsExported() {
		return ""
	}
	tag := f.Tag.Get("yaml")
	if tag == "" || tag == "-" {
		return ""
	}
	return strings.Split(tag, ",")[0]
}

func join(prefix, name string) string {
	if prefix == "" {
		return name
	}
	return prefix + "." + name
}

func difference(a, b map[string]bool) []string {
	var only []string
	for k := range a {
		if !b[k] {
			only = append(only, k)
		}
	}
	sort.Strings(only)
	return only
}
