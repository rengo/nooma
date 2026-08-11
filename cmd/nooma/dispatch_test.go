package main

import (
	"bytes"
	"strings"
	"testing"
)

// TestUsageNamesOnlyCommandsThatWork is spec R10.3 read together with R10.1's
// "no stubs" rule, and the two constrain each other more tightly than either
// reads alone.
//
// R10.3 says the usage message names the commands. R10.1 says a command that
// does not work yet must not exist — not even as a stub printing "not
// implemented", because a stub teaches the user the command is there. Listing a
// command in usage is the same lesson by a shorter route: the user reads it,
// types it, and gets an error.
//
// So usage names exactly what is dispatchable, and each command joins the list in
// the slice that makes it work. This test is what keeps the two in step: it reads
// the dispatch table rather than a hardcoded list, so a command can never appear
// in one and not the other.
func TestUsageNamesOnlyCommandsThatWork(t *testing.T) {
	t.Parallel()

	var out, errOut bytes.Buffer
	if err := run(nil, &out, &errOut); err != nil {
		t.Fatalf("bare invocation returned an error: %v", err)
	}

	usage := out.String()
	for name := range commands {
		if !strings.Contains(usage, name) {
			t.Errorf("usage does not name %q, which is dispatchable:\n%s", name, usage)
		}
	}
	if errOut.Len() != 0 {
		t.Errorf("usage went to stderr:\n%s", errOut.String())
	}
}

// TestUnimplementedCommandsDoNotExist is spec R10.1's no-stubs rule, aimed at the
// commands docs/01-architecture.md documents but M0 does not build. A stub
// printing "not implemented" would teach the user the command is there; an
// unknown-command error is the honest answer until it works.
//
// "consolidate" left this list in m2c-consolidation-runtime PR 12
// (cmd/nooma/consolidate.go): it is dispatchable now, so it belongs in
// TestUsageNamesOnlyCommandsThatWork's own coverage above instead —
// leaving it here would fail that exact test this file already runs.
func TestUnimplementedCommandsDoNotExist(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"reindex", "export", "import", "nonsense"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var out, errOut bytes.Buffer
			err := run([]string{name}, &out, &errOut)
			if err == nil {
				t.Fatalf("%q was accepted; a command that does not work yet must not exist, not even as a stub", name)
			}
			if !strings.Contains(err.Error(), name) {
				t.Errorf("the error does not name the command the user typed:\n%v", err)
			}
			if out.Len() != 0 {
				t.Errorf("a failing command wrote to stdout:\n%s", out.String())
			}
		})
	}
}

// TestFailuresWriteNothingToStdout is spec R10.5's other half: success on stdout,
// failure on stderr. A script that pipes stdout must not receive an error message
// in the data stream.
func TestFailuresWriteNothingToStdout(t *testing.T) {
	t.Parallel()

	var out, errOut bytes.Buffer
	if err := run([]string{"no-such-command"}, &out, &errOut); err == nil {
		t.Fatal("an unknown command was accepted")
	}
	if out.Len() != 0 {
		t.Errorf("flag-parsing noise reached stdout:\n%s", out.String())
	}
}
