package main

import (
	"bytes"
	"errors"
	"flag"
	"strings"
	"testing"

	"github.com/rengo/nooma/internal/ui"
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

// TestUIDeps_NilServicesStayNilInterfaces is design §3.4's typed-nil gotcha,
// made behavioural: assigning a nil *brain.TodayService/*brain.UnitsService/
// *brain.RecallService/*brain.CaptureService straight into ui.Deps'
// interface fields would produce a NON-nil interface wrapping a nil pointer
// — deps.Today != nil would be true, so ui.Handler's own nil check
// (h.deps.Today == nil, "not wired in this build") would never fire, and
// the first request would panic on a nil-receiver method call instead of
// answering 503. uiDeps exists to keep every one of its four service
// parameters out of that trap, at cmd/nooma's one call site — wireToday and
// wireUnits never actually return nil in production, but this proves the
// guard holds regardless.
func TestUIDeps_NilServicesStayNilInterfaces(t *testing.T) {
	deps := uiDeps(nil, nil, nil, nil, ui.Serving{})

	if deps.Today != nil {
		t.Error("Today: want a nil interface for a nil *brain.TodayService, got non-nil — the typed-nil trap uiDeps exists to avoid")
	}
	if deps.Units != nil {
		t.Error("Units: want a nil interface for a nil *brain.UnitsService, got non-nil")
	}
	if deps.Search != nil {
		t.Error("Search: want a nil interface for a nil *brain.RecallService, got non-nil")
	}
	if deps.Capture != nil {
		t.Error("Capture: want a nil interface for a nil *brain.CaptureService, got non-nil")
	}
}
