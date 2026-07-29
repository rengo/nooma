package goldenset

import (
	"strings"
	"testing"
)

// TestDecodeStrict_EnforcesRequiredFields proves DecodeStrict — the single
// decoder configuration Load and the format.md fence gate both share —
// rejects a document missing a "Required: yes" field instead of silently
// decoding it to its Go zero value (four-lens pre-PR review, WARNING
// finding 4). Before this fix, `DecodeStrict([]byte("{}"), &ClassifyExample{})`
// returned nil: a gutted example, or a real case missing a required field,
// passed every gate in this package.
func TestDecodeStrict_EnforcesRequiredFields(t *testing.T) {
	tests := []struct {
		name     string
		data     string
		newValue func() any
		wantErr  string
	}{
		{
			name:     "recall: a gutted {} is rejected, missing every required field",
			data:     `{}`,
			newValue: func() any { return &RecallExample{} },
			wantErr:  "id is required",
		},
		{
			name:     "recall: a unit missing status is rejected",
			data:     `{"id":"x","units":[{"id":"u1","type":"knowledge","content":"c"}],"queries":[{"query":"q","expected_unit_ids":["u1"]}]}`,
			newValue: func() any { return &RecallExample{} },
			wantErr:  "status is required",
		},
		{
			name:     "classify: expected.weight explicit null is rejected, not silently decoded to zero",
			data:     `{"id":"x","input":"i","expected":{"type":"task","normalized_content":"n","weight":null,"decay_rate":0.01}}`,
			newValue: func() any { return &ClassifyExample{} },
			wantErr:  "expected.weight is required",
		},
		{
			name:     "classify: expected.decay_rate missing entirely is rejected",
			data:     `{"id":"x","input":"i","expected":{"type":"task","normalized_content":"n","weight":1.0}}`,
			newValue: func() any { return &ClassifyExample{} },
			wantErr:  "expected.decay_rate is required",
		},
		{
			name:     "llm: every required field empty is rejected",
			data:     `{}`,
			newValue: func() any { return &LLMExample{} },
			wantErr:  "id is required",
		},
		{
			name:     "llm: neither response nor error set is rejected",
			data:     `{"id":"x","provider":"p","model":"m","task":"t","prompt":"pr"}`,
			newValue: func() any { return &LLMExample{} },
			wantErr:  "exactly one of response or error",
		},
		{
			name:     "llm: both response and error set is rejected",
			data:     `{"id":"x","provider":"p","model":"m","task":"t","prompt":"pr","response":"r","error":"timeout"}`,
			newValue: func() any { return &LLMExample{} },
			wantErr:  "exactly one of response or error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := tt.newValue()
			err := DecodeStrict([]byte(tt.data), v)
			if err == nil {
				t.Fatalf("DecodeStrict(%s) = nil, want an error containing %q", tt.data, tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("DecodeStrict(%s) error = %q, want it to contain %q", tt.data, err.Error(), tt.wantErr)
			}
		})
	}
}
