package classify

import "testing"

// TestDecode_InterruptLevel covers spec R3.1's classify-side half (design
// §3.8, owner-review R3): interrupt_level is optional, so its absence is the
// ordinary case and reports no Reason at all — the same posture the nine
// other optional fields already take (decode.go:71-80). A present, in-range
// value decodes to the float; a present, out-of-range value degrades with
// ReasonBadFormat, reused rather than adding a sixth Reason (R3's own
// argument, design §3.8).
func TestDecode_InterruptLevel(t *testing.T) {
	tests := map[string]struct {
		payload string
		want    *float64
		reason  Reason // "" means no degradation reported for this field
	}{
		"absent": {
			payload: `{"type":"task","normalized_content":"buy milk","weight":1.5,"decay_rate":0.02}`,
			want:    nil,
			reason:  "",
		},
		"present, in range": {
			payload: `{"type":"task","normalized_content":"buy milk","weight":1.5,"decay_rate":0.02,"interrupt_level":0.42}`,
			want:    ptr(0.42),
			reason:  "",
		},
		"present, boundary zero": {
			payload: `{"type":"task","normalized_content":"buy milk","weight":1.5,"decay_rate":0.02,"interrupt_level":0}`,
			want:    ptr(0.0),
			reason:  "",
		},
		"present, boundary one": {
			payload: `{"type":"task","normalized_content":"buy milk","weight":1.5,"decay_rate":0.02,"interrupt_level":1}`,
			want:    ptr(1.0),
			reason:  "",
		},
		"present, above range": {
			payload: `{"type":"task","normalized_content":"buy milk","weight":1.5,"decay_rate":0.02,"interrupt_level":1.1}`,
			want:    nil,
			reason:  ReasonBadFormat,
		},
		// An explicit null is present-but-unusable, and must never become a
		// claimed value. json.Unmarshal into a non-pointer float64 accepts
		// `null` without error and leaves the zero — so the obvious assigner
		// shape silently turns "the model declined to answer" into "the model
		// said 0.0", which §5.1's "a degraded weight is not a zero weight"
		// exists to forbid and which doc 02 §7's NULL round trip depends on.
		// ReasonWrongType, because null is not the JSON type this field reads.
		"present, explicit null": {
			payload: `{"type":"task","normalized_content":"buy milk","weight":1.5,"decay_rate":0.02,"interrupt_level":null}`,
			want:    nil,
			reason:  ReasonWrongType,
		},
		"present, below range": {
			payload: `{"type":"task","normalized_content":"buy milk","weight":1.5,"decay_rate":0.02,"interrupt_level":-0.1}`,
			want:    nil,
			reason:  ReasonBadFormat,
		},
		"present, wrong type": {
			payload: `{"type":"task","normalized_content":"buy milk","weight":1.5,"decay_rate":0.02,"interrupt_level":"high"}`,
			want:    nil,
			reason:  ReasonWrongType,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			c, err := Decode(tc.payload, testNow)
			if err != nil {
				t.Fatalf("Decode error = %v, want nil", err)
			}

			if tc.reason == "" {
				for _, d := range c.Degradations {
					if d.Field == "interrupt_level" {
						t.Fatalf("interrupt_level degraded with %q, want no degradation at all", d.Reason)
					}
				}
			} else {
				assertOnlyDegraded(t, c, "interrupt_level", tc.reason)
			}

			switch {
			case tc.want == nil && c.InterruptLevel != nil:
				t.Errorf("InterruptLevel = %v, want nil", *c.InterruptLevel)
			case tc.want != nil && (c.InterruptLevel == nil || *c.InterruptLevel != *tc.want):
				t.Errorf("InterruptLevel = %v, want %v", c.InterruptLevel, *tc.want)
			}
		})
	}
}

func ptr(f float64) *float64 { return &f }

// TestDecode_RecurrenceRule covers design §3.8's second field: a decoded,
// closed-vocabulary "yearly | monthly", not opaque structured_data (doc 02
// §5.1: "structured_data ... is opaque to the brain and stays opaque"). It
// is optional exactly like the six orthogonal fields, and degrades exactly
// as they do: a value outside the vocabulary is ReasonUnknownEnum, not a
// new Reason.
func TestDecode_RecurrenceRule(t *testing.T) {
	tests := map[string]struct {
		payload string
		want    *RecurrenceRule
		reason  Reason // "" means no degradation reported for this field
	}{
		"absent": {
			payload: `{"type":"recurring_reminder","normalized_content":"water the plants","weight":0.5,"decay_rate":0.02}`,
			want:    nil,
			reason:  "",
		},
		"present, yearly": {
			payload: `{"type":"recurring_reminder","normalized_content":"mom's birthday","weight":0.5,"decay_rate":0.02,"recurrence_rule":"yearly"}`,
			want:    recurrenceRulePtr(RecurrenceRuleYearly),
			reason:  "",
		},
		"present, monthly": {
			payload: `{"type":"recurring_reminder","normalized_content":"pay rent","weight":0.5,"decay_rate":0.02,"recurrence_rule":"monthly"}`,
			want:    recurrenceRulePtr(RecurrenceRuleMonthly),
			reason:  "",
		},
		"present, daily": {
			payload: `{"type":"recurring_reminder","normalized_content":"take the pills","weight":0.5,"decay_rate":0.02,"recurrence_rule":"daily"}`,
			want:    recurrenceRulePtr(RecurrenceRuleDaily),
			reason:  "",
		},
		"present, unknown enum": {
			// "weekly" is still outside the vocabulary at this point in the
			// chain, and is the value doctor's live quality gate rejected on
			// "remind me to water the plants every Sunday". It stops being a
			// valid example here the moment weekly is added, and this case
			// then needs a value no vocabulary will claim — the same
			// re-picking TestNextOccurrence_UnknownRuleFallsBackToYearly just
			// went through.
			payload: `{"type":"recurring_reminder","normalized_content":"water the plants","weight":0.5,"decay_rate":0.02,"recurrence_rule":"weekly"}`,
			want:    nil,
			reason:  ReasonUnknownEnum,
		},
		"present, wrong type": {
			payload: `{"type":"recurring_reminder","normalized_content":"water the plants","weight":0.5,"decay_rate":0.02,"recurrence_rule":12}`,
			want:    nil,
			reason:  ReasonWrongType,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			c, err := Decode(tc.payload, testNow)
			if err != nil {
				t.Fatalf("Decode error = %v, want nil", err)
			}

			if tc.reason == "" {
				for _, d := range c.Degradations {
					if d.Field == "recurrence_rule" {
						t.Fatalf("recurrence_rule degraded with %q, want no degradation at all", d.Reason)
					}
				}
			} else {
				assertOnlyDegraded(t, c, "recurrence_rule", tc.reason)
			}

			switch {
			case tc.want == nil && c.RecurrenceRule != nil:
				t.Errorf("RecurrenceRule = %v, want nil", *c.RecurrenceRule)
			case tc.want != nil && (c.RecurrenceRule == nil || *c.RecurrenceRule != *tc.want):
				t.Errorf("RecurrenceRule = %v, want %v", c.RecurrenceRule, *tc.want)
			}
		})
	}
}

func recurrenceRulePtr(r RecurrenceRule) *RecurrenceRule { return &r }
