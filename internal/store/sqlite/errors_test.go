package sqlite

import (
	"strings"
	"testing"
)

// TestVersionErrorError asserts the message names both versions and states
// that nothing was modified, so a user hitting this error is not left to
// guess whether it is safe to retry (design §5.3, spec R3.6).
func TestVersionErrorError(t *testing.T) {
	err := &VersionError{VaultVersion: 3, BinaryVersion: 2}
	msg := err.Error()

	for _, want := range []string{"3", "2", "nothing was modified"} {
		if !strings.Contains(msg, want) {
			t.Errorf("VersionError.Error() = %q, want it to contain %q", msg, want)
		}
	}
}
