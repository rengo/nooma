package unit

import (
	"errors"
	"testing"
)

// TestParseStatus_RoundTripsAndRejectsUnknown proves R2.1's boundary-validity
// half: ParseStatus is the sole entry point from untrusted text (design D1)
// — every AllStatuses() member parses back to itself, and an unrecognized
// string returns ErrUnknownStatus naming the value.
func TestParseStatus_RoundTripsAndRejectsUnknown(t *testing.T) {
	for _, want := range AllStatuses() {
		t.Run(string(want), func(t *testing.T) {
			got, err := ParseStatus(string(want))
			if err != nil {
				t.Fatalf("ParseStatus(%q) returned error %v, want nil", want, err)
			}
			if got != want {
				t.Errorf("ParseStatus(%q) = %q, want %q", want, got, want)
			}
		})
	}

	t.Run("unknown value", func(t *testing.T) {
		_, err := ParseStatus("focus")
		if err == nil {
			t.Fatal("ParseStatus(\"focus\") returned nil error, want ErrUnknownStatus")
		}
		if !errors.Is(err, ErrUnknownStatus) {
			t.Errorf("ParseStatus(\"focus\") error = %v, want it to wrap ErrUnknownStatus", err)
		}
		if got := err.Error(); got == "" {
			t.Error("ParseStatus error message is empty, want it to name the rejected value")
		}
	})
}
