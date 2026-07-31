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
// either panics or reports success with nothing, design D1's floor).
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
