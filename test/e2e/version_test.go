//go:build e2e

// See test/e2e/doc.go for what this package is and when it runs.
package e2e

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"testing"
)

// versionShape is the binary contract from cmd/nooma/main.go's buildString:
// "nooma <version> (<revision>)\n", where <version> and <revision> are each a
// non-empty run of non-whitespace bytes. It is the shape that is fixed, not
// any particular value — pinning a literal version string would fail on
// every release and teach contributors to weaken the test instead.
var versionShape = regexp.MustCompile(`^nooma \S+ \(\S+\)\n$`)

// TestBinaryReportsVersion is the first test in this repository that compiles
// and executes the real nooma binary rather than package code. It is the
// first thing that would catch a build that succeeds but produces a binary
// unrunnable or unresponsive on its most basic command.
//
// What it genuinely proves: the test/e2e package builds and runs under the
// `e2e` tag (R11.1's tag/build half), that `go build ./cmd/nooma` produces a
// binary that starts and exits 0 for `nooma version`, and that the output
// matches the documented "nooma <version> (<revision>)" shape.
//
// What it does NOT prove, stated plainly rather than left implicit: it does
// not prove the version or revision values are correct (there is no oracle
// for "correct" here — the binary is built from this same checkout), and it
// exercises no command besides `version` — no `init`, `serve`, `capture`,
// `recall`, `doctor`, or `export` (those do not exist yet; R11.1 MUST NOT).
// A change that would make this test fail: dropping the revision or the
// parentheses from buildString's format, breaking the "version" case in
// main's command switch, or a build failure in cmd/nooma or anything it
// imports. A change that would NOT make it fail: bumping the version string,
// or any change to a package this binary does not import.
func TestBinaryReportsVersion(t *testing.T) {
	// The ".exe" suffix is not decoration on Windows: without it the file
	// `go build -o` writes is not an executable as far as exec.Command is
	// concerned, and the test fails with "executable file not found in
	// %PATH%" while naming a file that plainly exists. This is the only
	// edit this file has taken, and R10.2 records why it is not the kind
	// of edit R10.2 forbids: it changes where the binary is written, never
	// what `nooma version` must print.
	name := "nooma"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	binPath := filepath.Join(t.TempDir(), name)

	build := exec.Command("go", "build", "-buildvcs=false", "-o", binPath, "./cmd/nooma")
	build.Dir = repoRoot(t)
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build ./cmd/nooma: %v\n%s", err, out)
	}

	var stdout bytes.Buffer
	run := exec.Command(binPath, "version")
	run.Stdout = &stdout
	if err := run.Run(); err != nil {
		t.Fatalf("nooma version: %v", err)
	}

	got := stdout.String()
	if !versionShape.MatchString(got) {
		t.Fatalf("nooma version output = %q, want shape %s", got, versionShape.String())
	}
}

// repoRoot returns the module root, so `go build ./cmd/nooma` resolves
// regardless of the working directory `go test` happens to use.
func repoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd: %v", err)
	}
	// test/e2e -> repo root is two levels up.
	return filepath.Join(wd, "..", "..")
}
