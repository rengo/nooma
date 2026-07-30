package main

import (
	"bytes"
	"strings"
	"testing"
)

// L1: pure, no I/O beyond an in-memory buffer. See docs/06-harness.md §3.
//
// These three predate the dispatch table and are kept as they were, with only the
// second writer threaded through: they assert the contract a user sees, and that
// contract did not change when the implementation behind it did.

func TestRunWithoutArgsPrintsUsage(t *testing.T) {
	var out, errOut bytes.Buffer

	if err := run(nil, &out, &errOut); err != nil {
		t.Fatalf("run() returned %v, want nil", err)
	}
	if !strings.Contains(out.String(), "usage: nooma") {
		t.Errorf("run() wrote %q, want it to contain the usage line", out.String())
	}
}

func TestRunVersionPrintsBuildString(t *testing.T) {
	var out, errOut bytes.Buffer

	if err := run([]string{"version"}, &out, &errOut); err != nil {
		t.Fatalf("run() returned %v, want nil", err)
	}
	if got := out.String(); !strings.HasPrefix(got, "nooma ") {
		t.Errorf("run() wrote %q, want it to start with the binary name", got)
	}
}

func TestRunUnknownCommandFails(t *testing.T) {
	var out, errOut bytes.Buffer

	err := run([]string{"definitely-not-a-command"}, &out, &errOut)
	if err == nil {
		t.Fatal("run() returned nil, want an error for an unknown command")
	}
	if !strings.Contains(err.Error(), "definitely-not-a-command") {
		t.Errorf("run() error is %q, want it to name the offending command", err)
	}
}
