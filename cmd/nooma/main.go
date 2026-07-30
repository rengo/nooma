// Command nooma is the single self-contained binary that runs a personal digital brain.
//
// It is the only place in the tree that wires adapters to the cognitive core.
// See docs/01-architecture.md for the CLI surface and docs/06-harness.md §1 for
// the dependency rule.
package main

import (
	"fmt"
	"io"
	"os"
	"runtime/debug"
	"sort"
)

// version is overridden at build time with -ldflags "-X main.version=...".
var version = "dev"

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "nooma:", err)
		os.Exit(1)
	}
}

// command is one dispatchable subcommand.
//
// args is everything after the command name, so each command parses its own
// flags. out and errOut are passed rather than taken from the process, which is
// what keeps the whole CLI testable without touching os.Stdout — the property a
// framework would have taken away, and the reason there is no framework here.
type command struct {
	summary string
	run     func(args []string, out, errOut io.Writer) error
}

// commands is the dispatch table, and it is also the source of truth for the
// usage message.
//
// A command appears here only when it works. Spec R10.1 forbids a stub printing
// "not implemented", because a stub teaches the user the command exists — and
// listing an absent command in usage teaches the same lesson by a shorter route:
// they read it, type it, and get an error. So `serve`, `status` and `doctor` join
// this table in the slices that make them work, not before, and usage cannot
// drift from reality because it is generated from here rather than written
// alongside it.
var commands = map[string]command{
	"version": {
		summary: "print the version and build information",
		run:     runVersion,
	},
}

// run holds the CLI behavior.
func run(args []string, out, errOut io.Writer) error {
	if len(args) == 0 {
		return usage(out)
	}

	name := args[0]
	cmd, known := commands[name]
	if !known {
		return fmt.Errorf("unknown command %q — run `nooma` with no arguments to see what exists", name)
	}
	return cmd.run(args[1:], out, errOut)
}

func usage(out io.Writer) error {
	names := make([]string, 0, len(commands))
	width := 0
	for name := range commands {
		names = append(names, name)
		if len(name) > width {
			width = len(name)
		}
	}
	sort.Strings(names)

	if _, err := fmt.Fprint(out, "nooma — a personal digital brain\n\nusage: nooma <command> [arguments]\n\n"); err != nil {
		return err
	}
	for _, name := range names {
		if _, err := fmt.Fprintf(out, "  %-*s  %s\n", width, name, commands[name].summary); err != nil {
			return err
		}
	}
	return nil
}

func runVersion(_ []string, out, _ io.Writer) error {
	_, err := fmt.Fprintln(out, buildString())
	return err
}

// buildString reports the version and the revision stamped into the binary.
func buildString() string {
	revision := "unknown"
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, s := range info.Settings {
			if s.Key == "vcs.revision" {
				revision = s.Value
			}
		}
	}
	return fmt.Sprintf("nooma %s (%s)", version, revision)
}
