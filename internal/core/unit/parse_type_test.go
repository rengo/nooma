package unit

import (
	"errors"
	"testing"
)

// TestParseType_RoundTripsAndRejectsUnknown mirrors
// TestParseStatus_RoundTripsAndRejectsUnknown (design D1's pattern, repeated
// per D4): every AllTypes() member parses back to itself, and an
// unrecognized string returns ErrUnknownType naming the value.
func TestParseType_RoundTripsAndRejectsUnknown(t *testing.T) {
	for _, want := range AllTypes() {
		t.Run(string(want), func(t *testing.T) {
			got, err := ParseType(string(want))
			if err != nil {
				t.Fatalf("ParseType(%q) returned error %v, want nil", want, err)
			}
			if got != want {
				t.Errorf("ParseType(%q) = %q, want %q", want, got, want)
			}
		})
	}

	t.Run("unknown value", func(t *testing.T) {
		const rejectedValue = "chitchat"
		_, err := ParseType(rejectedValue)
		if err == nil {
			t.Fatalf("ParseType(%q) returned nil error, want ErrUnknownType", rejectedValue)
		}
		if !errors.Is(err, ErrUnknownType) {
			t.Errorf("ParseType(%q) error = %v, want it to wrap ErrUnknownType", rejectedValue, err)
		}
		if got := err.Error(); got == "" {
			t.Error("ParseType error message is empty, want it to name the rejected value")
		}
	})
}
