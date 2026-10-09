package ui

import "testing"

// time.Local's name is the literal "Local" on every machine started without TZ
// (doc 02 section 5): the note must use the offset, never that word.
func TestDescribeZone(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		secs int
		want string
	}{
		{"America/Argentina/Buenos_Aires", -3 * 3600, "Times in America/Argentina/Buenos_Aires (UTC-03:00)"},
		{"Local", -3 * 3600, "Times in UTC-03:00"},
		{"", 5*3600 + 30*60, "Times in UTC+05:30"},
		{"Local", 0, "Times in UTC"},
		{"UTC", 0, "Times in UTC"},
		{"Europe/London", 0, "Times in Europe/London (UTC+00:00)"},
	} {
		if got := describeZone(tc.name, tc.secs); got != tc.want {
			t.Errorf("describeZone(%q, %d) = %q, want %q", tc.name, tc.secs, got, tc.want)
		}
	}
}
