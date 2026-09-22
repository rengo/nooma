package main

import "testing"

// TestResolveUIEnabled_NoUIFlagOverridesConfig pins design m4a §3.9's
// precedence rule: an explicit --no-ui flag beats server.ui: true when both
// are set; server.ui: false alone has the same effect. Pulled out of
// runServe as a small pure function (resolveUIEnabled) so the rule is
// testable without starting a server — a naming/shape choice for
// testability, the same kind PR 2's newUIMux made (design m4a §3.2).
func TestResolveUIEnabled_NoUIFlagOverridesConfig(t *testing.T) {
	cases := []struct {
		name     string
		serverUI bool
		noUIFlag bool
		want     bool
	}{
		{"default: ui on, flag absent", true, false, true},
		{"flag wins over ui: true", true, true, false},
		{"config alone turns it off", false, false, false},
		{"both off", false, true, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := resolveUIEnabled(tc.serverUI, tc.noUIFlag); got != tc.want {
				t.Errorf("resolveUIEnabled(%v, %v) = %v, want %v", tc.serverUI, tc.noUIFlag, got, tc.want)
			}
		})
	}
}
