package relation

import (
	"testing"

	"github.com/rengo/nooma/internal/core/classify"
)

// float64Ptr and stringPtr build pointer fixtures — Judgment's fields are
// pointers for the same reason classify.Classification's are (design D1):
// a genuine zero value and an absent field must stay distinguishable.
func float64Ptr(f float64) *float64 { return &f }
func stringPtr(s string) *string    { return &s }

// wantOutcome builds a want-shaped *Outcome for table comparisons.
func wantOutcome(o Outcome) *Outcome { return &o }

// TestDecodeJudgment_NoFieldsSalvaged covers design D1's own floor, restated
// for the judge's wire contract: a payload with nothing salvaged has nothing
// to build a Judgment from, so this is the one error DecodeJudgment returns.
func TestDecodeJudgment_NoFieldsSalvaged(t *testing.T) {
	tests := []struct {
		name string
		raw  string
	}{
		{"empty string", ""},
		{"non-object payload", `["new"]`},
		{"only the opening brace", `{`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			j, err := DecodeJudgment(tt.raw)
			if err != ErrNoFieldsSalvaged {
				t.Fatalf("DecodeJudgment(%q) error = %v, want %v", tt.raw, err, ErrNoFieldsSalvaged)
			}
			if j.Outcome != nil {
				t.Errorf("DecodeJudgment(%q) Judgment = %+v, want the zero value", tt.raw, j)
			}
		})
	}
}

// TestDecodeJudgment_TolerantOfMarkdownFencedResponse proves the shared fix
// (classify.Salvage, docs/02-cognitive-core.md §5.1) covers this consumer
// too — 5 of the confirmed production failures were relation_evaluation,
// not capture_processing, and both call sites reuse the same tolerant
// decoder rather than each carrying its own fence-handling.
func TestDecodeJudgment_TolerantOfMarkdownFencedResponse(t *testing.T) {
	raw := "```json\n" + `{"outcome":"duplicate","target_unit_id":"u1","strength":0.8,"confidence":0.9}` + "\n```"

	j, err := DecodeJudgment(raw)
	if err != nil {
		t.Fatalf("DecodeJudgment(%q) error = %v, want nil", raw, err)
	}
	if len(j.Degradations) != 0 {
		t.Fatalf("DecodeJudgment(%q) degraded %v, want none — the fence is discarded preamble, "+
			"not a malformed field", raw, j.Degradations)
	}
	if j.Outcome == nil || *j.Outcome != OutcomeDuplicate {
		t.Errorf("Outcome = %v, want duplicate", j.Outcome)
	}
}

// TestDecodeJudgment_WellFormed covers doc 02 §4 step 3's three outcomes,
// each with the fields that outcome actually carries — "new" carries none
// of the other four (they are meaningful only for duplicate/related, per
// doc 02 §4: "if related, with what strength/confidence"), so their absence
// here must not be reported as a degradation.
func TestDecodeJudgment_WellFormed(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want Judgment
	}{
		{
			name: "new: no candidate matched, no other field carried",
			raw:  `{"outcome":"new"}`,
			want: Judgment{Outcome: wantOutcome(OutcomeNew)},
		},
		{
			name: "duplicate: names the existing unit and a confidence",
			raw:  `{"outcome":"duplicate","target_unit_id":"unit-42","confidence":0.82}`,
			want: Judgment{
				Outcome:      wantOutcome(OutcomeDuplicate),
				TargetUnitID: stringPtr("unit-42"),
				Confidence:   float64Ptr(0.82),
			},
		},
		{
			name: "related: names a type, a strength and a confidence",
			raw:  `{"outcome":"related","target_unit_id":"unit-7","type":"same_topic","strength":0.6,"confidence":0.4}`,
			want: Judgment{
				Outcome:      wantOutcome(OutcomeRelated),
				TargetUnitID: stringPtr("unit-7"),
				Type:         stringPtr("same_topic"),
				Strength:     float64Ptr(0.6),
				Confidence:   float64Ptr(0.4),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := DecodeJudgment(tt.raw)
			if err != nil {
				t.Fatalf("DecodeJudgment(%q) error = %v, want nil", tt.raw, err)
			}
			if len(got.Degradations) != 0 {
				t.Fatalf("DecodeJudgment(%q) Degradations = %v, want none — a well-formed response for its own outcome must not degrade", tt.raw, got.Degradations)
			}
			assertJudgmentEqual(t, tt.raw, got, tt.want)
		})
	}
}

// TestDecodeJudgment_Degrades covers I14's tolerant-decode contract applied
// to Judgment's own five fields: a malformed or missing-and-required field
// degrades to absent, named in Degradations, and the rest of the judgment
// survives.
func TestDecodeJudgment_Degrades(t *testing.T) {
	tests := []struct {
		name       string
		raw        string
		wantField  string
		wantReason classify.Reason
		wantRest   Judgment // the fields that must have survived regardless
	}{
		{
			name:       "outcome absent, payload closed cleanly",
			raw:        `{"confidence":0.5}`,
			wantField:  "outcome",
			wantReason: classify.ReasonAbsent,
			wantRest:   Judgment{Confidence: float64Ptr(0.5)},
		},
		{
			name:       "outcome absent, payload truncated before its value arrived",
			raw:        `{"confidence":0.5,"outcome":`,
			wantField:  "outcome",
			wantReason: classify.ReasonTruncated,
			wantRest:   Judgment{Confidence: float64Ptr(0.5)},
		},
		{
			name:       "outcome wrong JSON type",
			raw:        `{"outcome":1,"confidence":0.5}`,
			wantField:  "outcome",
			wantReason: classify.ReasonWrongType,
			wantRest:   Judgment{Confidence: float64Ptr(0.5)},
		},
		{
			name:       "outcome outside its closed vocabulary",
			raw:        `{"outcome":"maybe"}`,
			wantField:  "outcome",
			wantReason: classify.ReasonUnknownEnum,
			wantRest:   Judgment{},
		},
		{
			name:       "target_unit_id wrong JSON type",
			raw:        `{"outcome":"duplicate","target_unit_id":42}`,
			wantField:  "target_unit_id",
			wantReason: classify.ReasonWrongType,
			wantRest:   Judgment{Outcome: wantOutcome(OutcomeDuplicate)},
		},
		{
			name:       "type wrong JSON type",
			raw:        `{"outcome":"related","type":7}`,
			wantField:  "type",
			wantReason: classify.ReasonWrongType,
			wantRest:   Judgment{Outcome: wantOutcome(OutcomeRelated)},
		},
		{
			name:       "strength wrong JSON type",
			raw:        `{"outcome":"related","strength":"high"}`,
			wantField:  "strength",
			wantReason: classify.ReasonWrongType,
			wantRest:   Judgment{Outcome: wantOutcome(OutcomeRelated)},
		},
		{
			name:       "confidence wrong JSON type",
			raw:        `{"outcome":"related","confidence":"high"}`,
			wantField:  "confidence",
			wantReason: classify.ReasonWrongType,
			wantRest:   Judgment{Outcome: wantOutcome(OutcomeRelated)},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := DecodeJudgment(tt.raw)
			if err != nil {
				t.Fatalf("DecodeJudgment(%q) error = %v, want nil", tt.raw, err)
			}
			if len(got.Degradations) != 1 {
				t.Fatalf("DecodeJudgment(%q) Degradations = %v, want exactly one", tt.raw, got.Degradations)
			}
			d := got.Degradations[0]
			if d.Field != tt.wantField || d.Reason != tt.wantReason {
				t.Errorf("DecodeJudgment(%q) degraded %s:%s, want %s:%s", tt.raw, d.Field, d.Reason, tt.wantField, tt.wantReason)
			}
			got.Degradations = nil
			assertJudgmentEqual(t, tt.raw, got, tt.wantRest)
		})
	}
}

// TestDecodeJudgment_OptionalFieldsAbsentWithoutDegradation covers this
// file's own open decision (judgmentFieldSpecs' required column): only
// "outcome" is required. target_unit_id, type, strength and confidence are
// each independently optional, and a "related" outcome that only carries
// some of them must not degrade for the ones it left out.
func TestDecodeJudgment_OptionalFieldsAbsentWithoutDegradation(t *testing.T) {
	got, err := DecodeJudgment(`{"outcome":"related","type":"same_topic"}`)
	if err != nil {
		t.Fatalf("DecodeJudgment error = %v, want nil", err)
	}
	if len(got.Degradations) != 0 {
		t.Fatalf("Degradations = %v, want none — target_unit_id, strength and confidence are optional", got.Degradations)
	}
	if got.Type == nil || *got.Type != "same_topic" {
		t.Errorf("Type = %v, want %q", got.Type, "same_topic")
	}
	if got.TargetUnitID != nil {
		t.Errorf("TargetUnitID = %v, want nil", got.TargetUnitID)
	}
}

// assertJudgmentEqual compares every field of got against want, field by
// field rather than reflect.DeepEqual — Outcome/TargetUnitID/Type/Strength/
// Confidence are all pointers, and a field-by-field compare reports which
// one differs instead of "not equal".
func assertJudgmentEqual(t *testing.T, raw string, got, want Judgment) {
	t.Helper()

	if !outcomeEqual(got.Outcome, want.Outcome) {
		t.Errorf("DecodeJudgment(%q) Outcome = %v, want %v", raw, derefOutcome(got.Outcome), derefOutcome(want.Outcome))
	}
	if !stringEqual(got.TargetUnitID, want.TargetUnitID) {
		t.Errorf("DecodeJudgment(%q) TargetUnitID = %v, want %v", raw, derefString(got.TargetUnitID), derefString(want.TargetUnitID))
	}
	if !stringEqual(got.Type, want.Type) {
		t.Errorf("DecodeJudgment(%q) Type = %v, want %v", raw, derefString(got.Type), derefString(want.Type))
	}
	if !floatEqual(got.Strength, want.Strength) {
		t.Errorf("DecodeJudgment(%q) Strength = %v, want %v", raw, derefFloat(got.Strength), derefFloat(want.Strength))
	}
	if !floatEqual(got.Confidence, want.Confidence) {
		t.Errorf("DecodeJudgment(%q) Confidence = %v, want %v", raw, derefFloat(got.Confidence), derefFloat(want.Confidence))
	}
}

func outcomeEqual(a, b *Outcome) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func stringEqual(a, b *string) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func floatEqual(a, b *float64) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func derefOutcome(o *Outcome) any {
	if o == nil {
		return nil
	}
	return *o
}

func derefString(s *string) any {
	if s == nil {
		return nil
	}
	return *s
}

func derefFloat(f *float64) any {
	if f == nil {
		return nil
	}
	return *f
}
