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
