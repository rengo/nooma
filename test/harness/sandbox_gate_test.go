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

	// Ways to run nooma the first fixtures did not name.
	write("gobuild", "a_test.go", "package p\nimport (\"os/exec\"; \"testing\")\nfunc TestA(t *testing.T) { _ = exec.Command(\"go\", \"build\", \"./cmd/nooma\") }\n")
	write("gobuildmodule", "a_test.go", "package p\nimport (\"os/exec\"; \"testing\")\nfunc TestA(t *testing.T) { _ = exec.Command(\"go\", \"build\", \"-o\", \"x\", \"github.com/rengo/nooma/cmd/nooma\") }\n")
	write("gobuildctx", "a_test.go", "package p\nimport (\"context\"; \"os/exec\"; \"testing\")\nfunc TestA(t *testing.T) { _ = exec.CommandContext(context.Background(), \"go\", \"build\", \"./cmd/nooma\") }\n")
	write("gobuildspread", "a_test.go", "package p\nimport (\"os/exec\"; \"testing\")\nfunc TestA(t *testing.T) { a := []string{\"build\", \"./cmd/nooma\"}; _ = exec.Command(\"go\", a...) }\n")
	write("aliased", "a_test.go", "package p\nimport (\"testing\"; cfg \"github.com/rengo/nooma/internal/config\")\nfunc TestA(t *testing.T) { _, _ = cfg.ResolveVault(\"\") }\n")
	// A child's environment is never built with an empty vault by hand,
	// whatever the shape; sandbox.Child builds it.
	write("envliteral", "a_test.go", "package p\nimport (\"os/exec\"; \"testing\")\nfunc TestA(t *testing.T) { c := exec.Command(\"x\"); c.Env = []string{\"PATH=/bin\", \"NOOMA_VAULT=\"} }\n")
	write("envemptyhome", "a_test.go", "package p\nimport (\"os\"; \"testing\")\nfunc TestA(t *testing.T) { _ = append(os.Environ(), \"HOME=\", \"NOOMA_VAULT=\") }\n")
	write("envsplit", "a_test.go", "package p\nimport (\"os\"; \"testing\")\nfunc TestA(t *testing.T) { e := append(os.Environ(), \"HOME=\"+\"h\"); e = append(e, \"NOOMA_VAULT=\") }\n")
	write("constkey", "a_test.go", "package p\nimport \"testing\"\nconst k = \"NOOMA_VAULT\"\nfunc TestA(t *testing.T) { t.Setenv(k, \"\") }\n")
	write("child", "a_test.go", "package p\nimport (\"os/exec\"; \"testing\"; \"x/sandbox\")\nfunc TestA(t *testing.T) { c := exec.Command(\"x\"); sandbox.Child(c, \"h\") }\n")
	// Not nooma: an unrelated call that merely names the string, a go command
	// that builds something else, and a stale worktree of this repository.
	write("unrelated", "a_test.go", "package p\nimport \"testing\"\nfunc f(a, b string) {}\nfunc TestA(t *testing.T) { f(\"want\", \"NOOMA_VAULT\") }\n")
	write("gootherbuild", "a_test.go", "package p\nimport (\"os/exec\"; \"testing\")\nfunc TestA(t *testing.T) { _ = exec.Command(\"go\", \"build\", \"./cmd/other\") }\n")
	write(".claude/worktrees/stale/cmd/nooma", "a_test.go", "package main\nimport \"testing\"\nfunc TestA(t *testing.T) { _ = run(nil, nil, nil) }\n")

	violations, _, err := sandboxViolations(root)
	if err != nil {
		t.Fatal(err)
	}
	reported := map[string]bool{}
	for _, v := range violations {
		dir, _, _ := strings.Cut(v, ":")
		reported[dir] = true
		if strings.HasPrefix(dir, ".claude/") {
			reported[".claude"] = true
		}
	}
	for _, dir := range []string{"runs", "binary", "resolves", "wrongmain", "empties", "unsets",
		"gobuild", "gobuildmodule", "gobuildctx", "gobuildspread", "aliased",
		"envliteral", "envemptyhome", "envsplit", "constkey"} {
		if !reported[dir] {
			t.Errorf("the gate did not report %s:\n%s", dir, strings.Join(violations, "\n"))
		}
	}
	for _, dir := range []string{"ok", "child", "unrelated", "gootherbuild", ".claude"} {
		if reported[dir] {
			t.Errorf("the gate reported %s, which is sandboxed, builds a child through sandbox.Child, or is not nooma:\n%s", dir, strings.Join(violations, "\n"))
		}
	}
}
