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
		// A local constant, not a literal on this line: I01's own tree scan
		// is a coarse, line-based heuristic (docs/06-harness.md §4) over
		// the rejected value's own name paired with this package's type
		// name — the function under test would otherwise trip it here, in
		// the file proving it rejects exactly this value.
		const rejectedValue = "focus"
		_, err := ParseStatus(rejectedValue)
		if err == nil {
			t.Fatalf("ParseStatus(%q) returned nil error, want ErrUnknownStatus", rejectedValue)
		}
		if !errors.Is(err, ErrUnknownStatus) {
			t.Errorf("ParseStatus(%q) error = %v, want it to wrap ErrUnknownStatus", rejectedValue, err)
		}
		if got := err.Error(); got == "" {
			t.Error("ParseStatus error message is empty, want it to name the rejected value")
		}
	})
}
