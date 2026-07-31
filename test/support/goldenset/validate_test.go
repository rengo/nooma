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

// TestRecallExample_ValidateVectorCrossField proves RecallExample.Validate
// enforces design §4.2's mechanizable cross-field rule for the vector
// widening (spec R2.6's precondition): once any unit in a case carries a
// vector, every unit and every query in that case must carry one too, and
// every vector — units and queries alike — must share one length. Without
// this check, a case author who forgets one unit's vector, or types one at
// a different dimension than the rest, would only find out at Search time
// (internal/core/recall, PR 8a's package), not at load time.
func TestRecallExample_ValidateVectorCrossField(t *testing.T) {
	unit := func(id string, vector []float32) RecallUnit {
		return RecallUnit{ID: id, Type: "knowledge", Content: "c", Status: "pool", Vector: vector}
	}
	query := func(vector []float32) RecallQuery {
		return RecallQuery{Query: "q", Vector: vector, ExpectedUnitIDs: []string{"u1"}}
	}

	tests := []struct {
		name    string
		example RecallExample
		wantErr string // empty means Validate() must return nil
	}{
		{
			name: "no unit carries a vector: the cross-field check does not apply",
			example: RecallExample{
				ID:      "x",
				Units:   []RecallUnit{unit("u1", nil)},
				Queries: []RecallQuery{query(nil)},
			},
		},
		{
			name: "every unit and every query carry a vector of the same length: valid",
			example: RecallExample{
				ID:      "x",
				Units:   []RecallUnit{unit("u1", []float32{1, 0, 0}), unit("u2", []float32{0, 1, 0})},
				Queries: []RecallQuery{query([]float32{1, 1, 0})},
			},
		},
		{
			name: "one unit carries a vector, a second one does not: rejected",
			example: RecallExample{
				ID:      "x",
				Units:   []RecallUnit{unit("u1", []float32{1, 0, 0}), unit("u2", nil)},
				Queries: []RecallQuery{query([]float32{1, 1, 0})},
			},
			wantErr: "units[1]: vector is required",
		},
		{
			name: "units carry vectors, the query does not: rejected",
			example: RecallExample{
				ID:      "x",
				Units:   []RecallUnit{unit("u1", []float32{1, 0, 0})},
				Queries: []RecallQuery{query(nil)},
			},
			wantErr: "queries[0]: vector is required",
		},
		{
			name: "a unit's vector has a different length than the rest: rejected",
			example: RecallExample{
				ID:      "x",
				Units:   []RecallUnit{unit("u1", []float32{1, 0, 0}), unit("u2", []float32{1, 0})},
				Queries: []RecallQuery{query([]float32{1, 1, 0})},
			},
			wantErr: "units[1]: vector has length 2, want 3",
		},
		{
			name: "the query's vector has a different length than the units': rejected",
			example: RecallExample{
				ID:      "x",
				Units:   []RecallUnit{unit("u1", []float32{1, 0, 0})},
				Queries: []RecallQuery{query([]float32{1, 1})},
			},
			wantErr: "queries[0]: vector has length 2, want 3",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.example.Validate()
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("Validate() = %v, want nil", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("Validate() = nil, want an error containing %q", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("Validate() error = %q, want it to contain %q", err.Error(), tt.wantErr)
			}
		})
	}
}
