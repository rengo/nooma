package harness

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSandboxGate_TheTreeHasNoUnsandboxedCommandTests: every package whose
// tests import os/exec or internal/config, use sandbox.Child, or are a
// package main under cmd/ starts in sandbox.Main, and no test empties
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
			t.Errorf("the gate did not find %s among the packages that need a sandbox; it would pass a tree it cannot see", dir)
		}
	}
}

// fixture packages for the gate: file contents are keyed by "dir/file".
const (
	sbPath  = "github.com/rengo/nooma/test/support/sandbox"
	cfgPath = "github.com/rengo/nooma/internal/config"
	// A TestMain that starts in sandbox.Main, under the import name sb.
	sandboxedMain = "package p\nimport (\"os\"; \"testing\"; sb \"" + sbPath + "\")\nfunc TestMain(m *testing.M) { os.Exit(sb.Main(m)) }\n"
)

// TestSandboxGate_FindsEachKindOfViolation: fixture packages, one per rule,
// each reported; the sandboxed and the harmless ones are not.
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
	test := func(imports, body string) string {
		return "package p\nimport (\"testing\"; " + imports + ")\nfunc TestA(t *testing.T) { " + body + " }\n"
	}

	// A package must start in sandbox.Main when it imports os/exec, imports
	// internal/config, imports the sandbox package, or is a package main
	// under cmd/, however it spells the command it runs.
	write("exec", "a_test.go", test(`"os/exec"`, `_ = exec.Command("nooma", "status")`))
	write("execpath", "a_test.go", test(`"os/exec"; "path/filepath"`, `_ = exec.Command(filepath.Join("a", "nooma"))`))
	write("execalias", "a_test.go", test(`x "os/exec"`, `_ = x.Command("go", "run", "github.com/rengo/nooma/cmd/nooma@latest")`))
	write("config", "a_test.go", test(`cfg "`+cfgPath+`"`, `_, _ = cfg.ResolveVault("")`))
	write("childonly", "a_test.go", test(`"`+sbPath+`"`, `sandbox.Child(nil, "h")`))
	write("cmd/tool", "a_test.go", "package main\nimport \"testing\"\nfunc TestA(t *testing.T) { _ = run(nil) }\n")
	write("wrongmain", "a_test.go", test(`"os/exec"`, `_ = exec.Command("x")`)+"func TestMain(m *testing.M) { m.Run() }\n")
	write("othermain", "a_test.go", test(`"os/exec"`, `_ = exec.Command("x")`)+"func TestMain(m *testing.M) { other.Main(m) }\n")
	write("dotimport", "a_test.go", "package p\nimport (\"os\"; \"os/exec\"; \"testing\"; . \""+sbPath+"\")\nfunc TestMain(m *testing.M) { os.Exit(Main(m)) }\nfunc TestA(t *testing.T) { _ = exec.Command(\"x\") }\n")
	// A TestMain that does not call sandbox.Main does not count because Main is
	// called from some other function.
	write("mainelsewhere", "a_test.go", "package p\nimport (\"testing\"; sb \""+sbPath+"\")\nfunc TestMain(m *testing.M) { m.Run() }\nfunc other(m *testing.M) int { return sb.Main(m) }\n")
	// A helper under test/support that runs commands or resolves a vault
	// (imports os/exec or internal/config), directly or through another, needs
	// the sandbox of whoever imports it, and of its own tests; the sandbox
	// package counts as one. A helper that does neither does not.
	write("test/support/execer", "e.go", "package execer\nimport \"os/exec\"\nvar Run = exec.Command\n")
	write("test/support/viaexecer", "v.go", "package viaexecer\nimport _ \""+"github.com/rengo/nooma/test/support/execer\"\n")
	write("test/support/plain", "p.go", "package plain\nimport \"strings\"\nvar F = strings.ToUpper\n")
	write("test/support/sandbox", "s.go", "package sandbox\nimport \"os/exec\"\nvar R = exec.Command\n")
	write("test/support/cfghelper", "c.go", "package cfghelper\nimport _ \""+cfgPath+"\"\n")
	write("test/support/ownrunner", "r.go", "package ownrunner\nimport \"os/exec\"\nvar R = exec.Command\n")
	write("test/support/ownrunner", "r_test.go", "package ownrunner\nimport \"testing\"\nfunc TestR(t *testing.T) { _ = R }\n")
	write("usecfg", "a_test.go", test(`"github.com/rengo/nooma/test/support/cfghelper"`, `_ = 1`))
	write("lib/runner", "r.go", "package runner\nimport \"os/exec\"\nvar R = exec.Command\n")
	write("userunner", "a_test.go", test(`"github.com/rengo/nooma/lib/runner"`, `_ = runner.R`))
	write("usehelper", "a_test.go", test(`"github.com/rengo/nooma/test/support/execer"`, `_ = execer.Run`))
	write("usevia", "a_test.go", test(`"github.com/rengo/nooma/test/support/viaexecer"`, `_ = 1`))
	write("useplain", "a_test.go", test(`"github.com/rengo/nooma/test/support/plain"`, `_ = plain.F`))
	// Only .claude/worktrees is skipped, not any folder of that name.
	write("tools/worktrees/x", "a_test.go", test(`"os/exec"`, `_ = exec.Command("x")`))
	// The rule is per package: a sandboxed file does not cover another's
	// directory, and one that imports exec is enough.
	write("split", "a_test.go", test(`"os/exec"`, `_ = exec.Command("x")`))
	write("split", "b_test.go", "package p\nimport \"testing\"\nfunc TestB(t *testing.T) {}\n")

	// A vault emptied in the test's own process, or by hand for a child.
	write("empties", "a_test.go", test("", `t.Setenv("NOOMA_VAULT", "")`))
	write("unsets", "a_test.go", test(`"os"`, `_ = os.Unsetenv("NOOMA_VAULT")`))
	write("clears", "a_test.go", test(`"os"`, `os.Clearenv()`))
	write("constkey", "a_test.go", "package p\nimport \"testing\"\nconst k = \"NOOMA_VAULT\"\nfunc TestA(t *testing.T) { t.Setenv(k, \"\") }\n")
	write("localconstkey", "a_test.go", test("", `const k = "NOOMA_VAULT"; t.Setenv(k, "")`))
	write("envliteral", "a_test.go", test("", `_ = []string{"PATH=/bin", "NOOMA_VAULT="}`))
	write("envappend", "a_test.go", test(`"os"`, `_ = append(os.Environ(), "HOME=", "NOOMA_VAULT=")`))
	write("envsplit", "a_test.go", test(`"os"`, `e := append(os.Environ(), "HOME="+"h"); e = append(e, "NOOMA_VAULT=")`))

	// Sandboxed, harmless, or not a violation at all.
	write("ok", "a_test.go", test(`"os/exec"`, `_ = exec.Command("x")`))
	write("ok", "main_test.go", sandboxedMain)
	write("cmd/okmain", "a_test.go", "package main\nimport \"testing\"\nfunc TestA(t *testing.T) { _ = run(nil) }\n")
	write("cmd/okmain", "main_test.go", strings.Replace(sandboxedMain, "package p", "package main", 1))
	write("child", "a_test.go", test(`"os/exec"; "`+sbPath+`"`, `sandbox.Child(exec.Command("x"), "h")`))
	write("child", "main_test.go", sandboxedMain)
	write("sandbox", "main_test.go", "package sandbox\nimport (\"os\"; \"os/exec\"; \"testing\")\nfunc TestMain(m *testing.M) { os.Exit(Main(m)) }\nfunc TestA(t *testing.T) { _ = exec.Command(\"x\") }\n")
	write("noexec", "a_test.go", test("", `_ = 1`))
	write("cmdlib", "a_test.go", test("", `_ = 1`))
	// Reads or filters the entry without building one: a concatenation with
	// a value, any call that names it, an entry for another variable.
	write("unrelated", "a_test.go", "package p\nimport (\"os\"; \"strings\"; \"testing\")\nfunc f(a, b string) {}\nfunc TestA(t *testing.T) {\n"+
		"\tf(\"want\", \"NOOMA_VAULT=\")\n\t_ = strings.HasPrefix(\"x\", \"NOOMA_VAULT=\")\n\tv := \"/v\"\n"+
		"\t_ = append(os.Environ(), \"NOOMA_VAULT=\"+v)\n\t_ = []string{\"NOOMA_VAULT=\" + v, \"NOOMA_VAULTS=\"}\n\tt.Setenv(\"NOOMA_VAULT\", v)\n\tt.Setenv(\"OTHER\", \"\")\n}\n")
	write("setsvalue", "a_test.go", test("", `t.Setenv("NOOMA_VAULT", "/v")`))
	write(".claude/worktrees/stale/cmd/nooma", "a_test.go", "package main\nimport \"os/exec\"\nvar _ = exec.Command\n")

	violations, needs, err := sandboxViolations(root)
	if err != nil {
		t.Fatal(err)
	}
	reported := map[string]bool{}
	for _, v := range violations {
		dir, _, _ := strings.Cut(v, ": ")
		reported[dir] = true
		if strings.HasPrefix(dir, ".claude/") {
			reported[".claude"] = true
		}
	}
	for _, dir := range []string{"exec", "execpath", "execalias", "config", "childonly", "cmd/tool", "wrongmain",
		"othermain", "dotimport", "split", "empties", "unsets", "clears", "constkey", "localconstkey",
		"envliteral", "envappend", "envsplit", "mainelsewhere", "usehelper", "usevia", "usecfg", "test/support/ownrunner", "tools/worktrees/x"} {
		if !reported[dir] {
			t.Errorf("the gate did not report %s:\n%s", dir, strings.Join(violations, "\n"))
		}
	}
	for _, dir := range []string{"ok", "cmd/okmain", "child", "sandbox", "noexec", "cmdlib", "unrelated", "setsvalue", "useplain", "userunner", ".claude"} {
		if reported[dir] {
			t.Errorf("the gate reported %s, which is sandboxed, harmless, or builds no entry of its own:\n%s", dir, strings.Join(violations, "\n"))
		}
	}
	for _, dir := range []string{"ok", "cmd/okmain", "child", "sandbox"} {
		if !needs[dir] {
			t.Errorf("the gate did not count %s among the packages that need a sandbox, so its TestMain was never checked", dir)
		}
	}
	for _, dir := range []string{"noexec", "unrelated", "setsvalue", "useplain", "userunner"} {
		if needs[dir] {
			t.Errorf("the gate asked %s for a sandbox", dir)
		}
	}
}
