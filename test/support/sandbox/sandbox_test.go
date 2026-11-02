package sandbox

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The package's own tests run inside the sandbox they check.
func TestMain(m *testing.M) { os.Exit(Main(m)) }

// root is the sandbox's folder: HOME is root/home.
func root() string { return filepath.Dir(os.Getenv("HOME")) }

func inside(path, dir string) bool {
	rel, err := filepath.Rel(dir, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// TestMain_TheTemporaryDirectoryIsInsideTheSandbox: a child given an empty
// NOOMA_VAULT and a working directory under os.TempDir climbs through the
// sandbox first, never through the machine's own folders (docs/06-harness.md
// §3).
func TestMain_TheTemporaryDirectoryIsInsideTheSandbox(t *testing.T) {
	if !strings.Contains(root(), "nooma-sandbox-") {
		t.Fatalf("HOME = %q, want a folder in the sandbox", os.Getenv("HOME"))
	}
	if tmp := os.TempDir(); !inside(tmp, root()) {
		t.Errorf("os.TempDir() = %q, want it inside the sandbox %q", tmp, root())
	}
	if d := t.TempDir(); !inside(d, root()) {
		t.Errorf("t.TempDir() = %q, want it inside the sandbox %q", d, root())
	}
}

// TestMain_GoKeepsItsConfigFileOnceHomeMoves: `go env GOENV` defaults to a
// path under the user's config folder; with HOME moved it would read a
// different go env, so Main pins it where it was.
func TestMain_GoKeepsItsConfigFileOnceHomeMoves(t *testing.T) {
	out, err := exec.Command("go", "env", "GOENV").Output()
	if err != nil {
		t.Skip("no go on PATH")
	}
	if got := strings.TrimSpace(string(out)); got == "" || inside(got, root()) {
		t.Errorf("go env GOENV = %q, want the one outside the sandbox %q", got, root())
	}
}

// TestChild_BuildsAChildEnvironmentThatCannotEscape: an empty NOOMA_VAULT
// only beside the child's own HOME, and a working directory in the sandbox
// unless the caller chose one.
func TestChild_BuildsAChildEnvironmentThatCannotEscape(t *testing.T) {
	cmd := exec.Command("x")
	Child(cmd, "/h")
	env := map[string]string{}
	for _, kv := range cmd.Env {
		k, v, _ := strings.Cut(kv, "=")
		env[k] = v
	}
	if env["HOME"] != "/h" || env["USERPROFILE"] != "/h" {
		t.Errorf("HOME/USERPROFILE = %q/%q, want /h", env["HOME"], env["USERPROFILE"])
	}
	if v, ok := env["NOOMA_VAULT"]; !ok || v != "" {
		t.Errorf("NOOMA_VAULT = %q (set %v), want set and empty", v, ok)
	}
	if !inside(cmd.Dir, root()) {
		t.Errorf("Dir = %q, want inside the sandbox %q", cmd.Dir, root())
	}

	chosen := exec.Command("x")
	chosen.Dir = "/elsewhere"
	Child(chosen, "/h")
	if chosen.Dir != "/elsewhere" {
		t.Errorf("Dir = %q, want the caller's choice kept", chosen.Dir)
	}
}

func TestChild_RefusesAnEmptyHome(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("Child(cmd, \"\") did not panic: an empty HOME is not a home of the child's own")
		}
	}()
	Child(exec.Command("x"), "")
}

func TestChild_PanicsOutsideMain(t *testing.T) {
	active = false
	defer func() {
		active = true
		if recover() == nil {
			t.Error("Child did not panic outside Main: its working directory would be the machine's temp folder")
		}
	}()
	Child(exec.Command("x"), "/h")
}

// TestMain_GoKeepsItsCachesOnceHomeMoves: GOPATH, GOMODCACHE and GOCACHE
// default under HOME; Main pins them where they were, so a test that builds
// neither starts cold nor downloads into the sandbox.
func TestMain_GoKeepsItsCachesOnceHomeMoves(t *testing.T) {
	for _, name := range []string{"GOPATH", "GOMODCACHE", "GOCACHE"} {
		out, err := exec.Command("go", "env", name).Output()
		if err != nil {
			t.Skip("no go on PATH")
		}
		if got := strings.TrimSpace(string(out)); got == "" || inside(got, root()) {
			t.Errorf("go env %s = %q, want one outside the sandbox %q", name, got, root())
		}
	}
}
