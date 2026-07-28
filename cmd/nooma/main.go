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
)

// version is overridden at build time with -ldflags "-X main.version=...".
var version = "dev"

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "nooma:", err)
		os.Exit(1)
	}
}

// run holds the CLI behavior. It takes its writer so that it stays testable
// without touching the process streams.
func run(args []string, out io.Writer) error {
	if len(args) == 0 {
		_, err := fmt.Fprint(out, "nooma — a personal digital brain\nusage: nooma <command>\n")
		return err
	}

	switch args[0] {
	case "version":
		_, err := fmt.Fprintln(out, buildString())
		return err
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
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
