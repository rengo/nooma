package selfmodel

import (
	"errors"
	"strings"
	"testing"
)

// TestNormalizeText covers the total, non-validating half of the content
// rules (FX-N): CRLF becomes LF, surrounding whitespace is trimmed, and
// nothing else is touched.
func TestNormalizeText(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want string
	}{
		{"crlf becomes lf", "a\r\nb", "a\nb"},
		{"lf is untouched", "a\nb", "a\nb"},
		{"surrounding spaces are trimmed", "  a  ", "a"},
		{"surrounding crlf is trimmed after conversion", "\r\na\r\n", "a"},
		{"crlf and trim together", "  a\r\nb  ", "a\nb"},
		{"interior whitespace is preserved", "a  b", "a  b"},
		{"a lone carriage return is not a crlf", "a\rb", "a\rb"},
		{"every crlf converts, not only the first", "a\r\nb\r\nc", "a\nb\nc"},
		{"whitespace only collapses to empty", " \t\r\n ", ""},
		{"empty stays empty", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := NormalizeText(tc.raw); got != tc.want {
				t.Errorf("NormalizeText(%q) = %q, want %q", tc.raw, got, tc.want)
			}
		})
	}
}

// TestNormalizeContent covers the validating half (FX-N): the normalised
// value is what is bounded and returned.
func TestNormalizeContent(t *testing.T) {
	atBound := strings.Repeat("a", MaxBeliefContentRunes)
	// A multibyte rune counts once: MaxBeliefContentRunes runes of a
	// three-byte character are far more than MaxBeliefContentRunes bytes.
	multibyteAtBound := strings.Repeat("あ", MaxBeliefContentRunes)

	accepted := []struct {
		name string
		raw  string
		want string
	}{
		{"plain text", "likes tea", "likes tea"},
		{"crlf and trim are applied to the returned value", "  a\r\nb  ", "a\nb"},
		{"exactly the bound is accepted", atBound, atBound},
		{"multibyte at the bound is accepted", multibyteAtBound, multibyteAtBound},
		{"padding around a bound-sized text is trimmed before counting", "  " + atBound + "  ", atBound},
		// 400 x "a\r\n" is 1200 raw runes, but 799 once CRLF collapses and the
		// trailing newline is trimmed: the bound applies to the normalised
		// value, not to what was typed.
		{"the bound applies after normalising", strings.Repeat("a\r\n", 400), strings.TrimSpace(strings.Repeat("a\n", 400))},
	}
	for _, tc := range accepted {
		t.Run("accepts "+tc.name, func(t *testing.T) {
			got, err := NormalizeContent(tc.raw)
			if err != nil {
				t.Fatalf("NormalizeContent error = %v, want nil", err)
			}
			if got != tc.want {
				t.Errorf("NormalizeContent returned %d runes %q..., want %d runes", len([]rune(got)), truncate(got), len([]rune(tc.want)))
			}
		})
	}

	rejected := []struct {
		name string
		raw  string
		want error
	}{
		{"empty", "", ErrEmptyContent},
		{"spaces only", "   ", ErrEmptyContent},
		{"crlf and tabs only", "\r\n\t\r\n", ErrEmptyContent},
		{"one rune over the bound", atBound + "a", ErrContentTooLong},
		{"multibyte one rune over the bound", multibyteAtBound + "あ", ErrContentTooLong},
	}
	for _, tc := range rejected {
		t.Run("rejects "+tc.name, func(t *testing.T) {
			got, err := NormalizeContent(tc.raw)
			if !errors.Is(err, tc.want) {
				t.Fatalf("NormalizeContent error = %v, want %v", err, tc.want)
			}
			if got != "" {
				t.Errorf("NormalizeContent returned %q alongside an error, want the empty string", truncate(got))
			}
		})
	}
}

// TestMaxBeliefContentRunes_IsTheDesignedBound pins the chosen value so a
// change to it is a visible, reviewed edit (design §3.4: chosen, not
// calibrated, and not a doc 02 §13 row).
func TestMaxBeliefContentRunes_IsTheDesignedBound(t *testing.T) {
	if MaxBeliefContentRunes != 1000 {
		t.Errorf("MaxBeliefContentRunes = %d, want 1000", MaxBeliefContentRunes)
	}
}

func truncate(s string) string {
	r := []rune(s)
	if len(r) > 20 {
		return string(r[:20])
	}
	return s
}
