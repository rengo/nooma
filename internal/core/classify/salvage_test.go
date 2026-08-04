package classify

import (
	"encoding/json"
	"testing"
)

// TestSalvage covers the three shapes design D1 names for the streaming,
// truncation-tolerant reader: a truncated payload (every member completed
// before the cut survives, truncatedAfter reports the cut), a non-object
// payload (zero completed members — there is nothing to salvage a member
// from), and a payload truncated before its first value (zero completed
// members, still flagged truncated — the boundary where a naive reader
// either panics or reports success with nothing, design D1's floor). It
// also covers preamble tolerance (docs/02-cognitive-core.md §5.1): a
// response wrapped in a markdown code fence, prose containing a brace that
// opens nothing decodable, and the documented, accepted limitation of
// scanning for only the first '{'.
func TestSalvage(t *testing.T) {
	tests := []struct {
		name               string
		raw                string
		wantFields         map[string]string // field -> raw JSON text, compared decoded
		wantTruncatedAfter bool
	}{
		{
			name:               "complete object: every member present, not truncated",
			raw:                `{"type":"task","weight":0.5}`,
			wantFields:         map[string]string{"type": `"task"`, "weight": `0.5`},
			wantTruncatedAfter: false,
		},
		{
			name:               "truncated mid-object: earlier members survive, flagged truncated",
			raw:                `{"type":"task","normalized_content":"buy milk","weight":`,
			wantFields:         map[string]string{"type": `"task"`, "normalized_content": `"buy milk"`},
			wantTruncatedAfter: true,
		},
		{
			name:               "non-object payload: a JSON array salvages zero members",
			raw:                `[1,2,3]`,
			wantFields:         map[string]string{},
			wantTruncatedAfter: true,
		},
		{
			name:               "malformed key after a later member: earlier members survive",
			raw:                `{"a":1,x}`,
			wantFields:         map[string]string{"a": `1`},
			wantTruncatedAfter: true,
		},
		{
			name:               "non-object payload: a bare JSON string salvages zero members",
			raw:                `"not an object"`,
			wantFields:         map[string]string{},
			wantTruncatedAfter: true,
		},
		{
			name:               "truncated before its first value: an empty stream",
			raw:                ``,
			wantFields:         map[string]string{},
			wantTruncatedAfter: true,
		},
		{
			name:               "truncated before its first value: only the opening brace",
			raw:                `{`,
			wantFields:         map[string]string{},
			wantTruncatedAfter: true,
		},
		{
			name:               "truncated before its first value: a key with no value",
			raw:                `{"type":`,
			wantFields:         map[string]string{},
			wantTruncatedAfter: true,
		},
		// The three cases below are the production defect this file was
		// bitten by (see salvage.go's own doc comment): gpt-4o-mini wraps
		// its answer in a markdown code fence even after being told not
		// to, and every fixture this repository had authored until now was
		// bare, well-formed JSON — so nothing caught it before a human ran
		// `nooma doctor` against a live key.
		{
			name: "markdown-fenced object: the fence is discarded, the object decodes as if bare",
			raw: "```json\n" +
				`{"type":"task","normalized_content":"Pick up the dry cleaning","weight":0.6,"decay_rate":0.1}` +
				"\n```",
			wantFields: map[string]string{
				"type":               `"task"`,
				"normalized_content": `"Pick up the dry cleaning"`,
				"weight":             `0.6`,
				"decay_rate":         `0.1`,
			},
			wantTruncatedAfter: false,
		},
		{
			name:               "prose containing a brace that opens nothing decodable: fails whole, not the wrong brace",
			raw:                `here is the JSON: {see below}`,
			wantFields:         map[string]string{},
			wantTruncatedAfter: true,
		},
		// Documents a known, accepted limitation rather than leaving it
		// undiscovered (Salvage's own doc comment): scanning for the FIRST
		// '{' means an earlier fragment that is itself a complete,
		// well-formed object is picked over a later, real one. No
		// recording in this corpus has this shape; fixing it would need
		// machinery to judge which brace is "real" that nothing observed
		// has justified yet.
		{
			name:               "known limitation: an earlier, independently valid object is picked over the real one",
			raw:                `Example: {"a":1} is not the answer. Real answer: {"type":"task"}`,
			wantFields:         map[string]string{"a": `1`},
			wantTruncatedAfter: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fields, truncatedAfter := Salvage([]byte(tt.raw))

			if truncatedAfter != tt.wantTruncatedAfter {
				t.Errorf("Salvage(%q) truncatedAfter = %v, want %v", tt.raw, truncatedAfter, tt.wantTruncatedAfter)
			}
			if len(fields) != len(tt.wantFields) {
				t.Fatalf("Salvage(%q) returned %d fields, want %d: %v", tt.raw, len(fields), len(tt.wantFields), fields)
			}
			for key, wantRaw := range tt.wantFields {
				gotRaw, ok := fields[key]
				if !ok {
					t.Errorf("Salvage(%q): missing field %q", tt.raw, key)
					continue
				}
				if !json.Valid(gotRaw) || string(gotRaw) != wantRaw {
					t.Errorf("Salvage(%q) field %q = %s, want %s", tt.raw, key, gotRaw, wantRaw)
				}
			}
		})
	}
}
