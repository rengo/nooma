package harness

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSandboxGate_TheTreeHasNoUnsandboxedCommandTests: every package whose
// tests run nooma (cmd/nooma's run(), the built binary, or vault resolution
// from the real environment) starts in sandbox.Main, and no test empties
// NOOMA_VAULT in its own process. On 2026-10-30 a test reached the
// maintainer's real vault; this keeps the next package from doing it.
func TestSandboxGate_TheTreeHasNoUnsandboxedCommandTests(t *testing.T) {
	violations, scanned, err := sandboxViolations(repoRootFromCaller(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range violations {
		t.Error(v)
	}
	// The gate is armed: it saw the packages that run commands.
	for _, dir := range []string{"cmd/nooma", "test/e2e"} {
		if !scanned[dir] {
			t.Errorf("the gate did not find %s among the packages that run nooma; it would pass a tree it cannot see", dir)
		}
	}
}

// TestSandboxGate_FindsEachKindOfViolation: fixture packages, one per way a
// test can reach a real vault, each reported; the sandboxed ones are not.
func TestSandboxGate_FindsEachKindOfViolation(t *testing.T) {
	root := t.TempDir()
	write := func(dir, name, src string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Join(root, dir), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, dir, name), []byte(src), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	const sandboxed = "package p\nimport (\"os\"; \"testing\"; \"x/sandbox\")\nfunc TestMain(m *testing.M) { os.Exit(sandbox.Main(m)) }\n"
	write("runs", "a_test.go", "package main\nimport \"testing\"\nfunc TestA(t *testing.T) { _ = run([]string{\"status\"}, nil, nil) }\n")
	write("binary", "a_test.go", "package e2e\nimport (\"os/exec\"; \"testing\")\nfunc TestA(t *testing.T) { _ = exec.Command(binaryPath(t)) }\n")
	write("resolves", "a_test.go", "package p\nimport (\"testing\"; \"x/config\")\nfunc TestA(t *testing.T) { _, _ = config.ResolveVault(\"\") }\n")
	write("ok", "a_test.go", "package main\nimport \"testing\"\nfunc TestA(t *testing.T) { _ = run([]string{\"status\"}, nil, nil) }\n")
	write("ok", "main_test.go", strings.Replace(sandboxed, "package p", "package main", 1))
	write("wrongmain", "a_test.go", "package main\nimport \"testing\"\nfunc TestA(t *testing.T) { _ = run(nil, nil, nil) }\nfunc TestMain(m *testing.M) { m.Run() }\n")
	write("empties", "a_test.go", "package p\nimport \"testing\"\nfunc TestA(t *testing.T) { t.Setenv(\"NOOMA_VAULT\", \"\") }\n")
	write("unsets", "a_test.go", "package p\nimport (\"os\"; \"testing\")\nfunc TestA(t *testing.T) { _ = os.Unsetenv(\"NOOMA_VAULT\") }\n")
	write("childnohome", "a_test.go", "package p\nimport (\"os\"; \"testing\")\nfunc TestA(t *testing.T) { _ = append(os.Environ(), \"NOOMA_VAULT=\") }\n")
	write("childhome", "a_test.go", "package p\nimport (\"os\"; \"testing\")\nfunc TestA(t *testing.T) { home := \"h\"; _ = append(os.Environ(), \"HOME=\"+home, \"NOOMA_VAULT=\") }\n")

	violations, _, err := sandboxViolations(root)
	if err != nil {
		t.Fatal(err)
	}
	reported := map[string]bool{}
	for _, v := range violations {
		dir, _, _ := strings.Cut(v, ":")
		reported[dir] = true
	}
	for _, dir := range []string{"runs", "binary", "resolves", "wrongmain", "empties", "unsets", "childnohome"} {
		if !reported[dir] {
			t.Errorf("the gate did not report %s:\n%s", dir, strings.Join(violations, "\n"))
		}
	}
	for _, dir := range []string{"ok", "childhome"} {
		if reported[dir] {
			t.Errorf("the gate reported %s, which is sandboxed or gives its child a HOME:\n%s", dir, strings.Join(violations, "\n"))
		}
	}
}
