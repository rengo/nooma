package main

import (
	"bytes"
	"errors"
	"flag"
	"strings"
	"testing"
)

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

// TestServeUsageShowsNoUIPrecedence pins that `nooma serve -h` actually shows
// the --no-ui flag and design m4a §3.9's precedence rule, not just runServe's
// own one-line "usage: nooma serve [--no-ui] [vault]". The flag's own
// description ("Overrides server.ui when both are set") is the single source
// for the wording; fs.Usage calls fs.PrintDefaults() rather than restating
// it, so the two cannot drift apart.
//
// Mutation this catches: dropping fs.PrintDefaults() (or the flag's
// precedence wording) from runServe's fs.Usage — the help text would lose
// the --no-ui/server.ui precedence rule entirely, silently.
func TestServeUsageShowsNoUIPrecedence(t *testing.T) {
	var out, errOut bytes.Buffer
	err := runServe([]string{"-h"}, &out, &errOut)
	if !errors.Is(err, flag.ErrHelp) {
		t.Fatalf("runServe([-h]) error = %v, want flag.ErrHelp", err)
	}

	got := errOut.String()
	if !strings.Contains(got, "no-ui") {
		t.Errorf("usage output does not mention --no-ui:\n%s", got)
	}
	if !strings.Contains(got, "Overrides server.ui when both are set") {
		t.Errorf("usage output does not state the --no-ui/server.ui precedence rule:\n%s", got)
	}
}
