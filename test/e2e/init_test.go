//go:build e2e

// See test/e2e/doc.go for what this package is and when it runs.
package e2e

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/rengo/nooma/internal/config"
)

// nooma runs the compiled binary with $HOME pointed at a temporary directory, so
// a bare `nooma init` cannot touch the developer's real home — and so the
// home-mode default is exercised for real rather than mocked.
func nooma(t *testing.T, home, cwd string, args ...string) (stdout, stderr string, err error) {
	t.Helper()

	cmd := exec.Command(binaryPath(t), args...)
	cmd.Dir = cwd
	cmd.Env = append(os.Environ(),
		"HOME="+home,
		"USERPROFILE="+home, // Windows
		"NOOMA_VAULT=",      // never inherit a real one
	)

	var out, errOut strings.Builder
	cmd.Stdout = &out
	cmd.Stderr = &errOut
	err = cmd.Run()
	return out.String(), errOut.String(), err
}

// assertVault checks every entry docs/01-architecture.md §"Vault structure"
// promises. It reads the filesystem rather than the command's output, because the
// command claiming success is exactly what is under test.
func assertVault(t *testing.T, dir string) {
	t.Helper()

	for _, entry := range []string{"nooma.yml", ".env", "nooma.db", "attachments", "derived", "logs"} {
		if _, err := os.Stat(filepath.Join(dir, entry)); err != nil {
			t.Errorf("vault %s is missing %s: %v", dir, entry, err)
		}
	}
}

// TestInitWithAnExplicitPath is the plain case, and the control for the rest.
func TestInitWithAnExplicitPath(t *testing.T) {
	home := t.TempDir()
	work := t.TempDir()
	target := filepath.Join(work, "pablo.nooma")

	stdout, stderr, err := nooma(t, home, work, "init", target)
	if err != nil {
		t.Fatalf("init: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
	}
	assertVault(t, target)
	if !strings.Contains(stdout, target) {
		t.Errorf("init did not print the path it created, so the user cannot tell where it went:\n%s", stdout)
	}
}

// TestInitWithARelativePath is spec R6.4 applied to init: `nooma init
// pablo.nooma` is the form every user already knows.
func TestInitWithARelativePath(t *testing.T) {
	home := t.TempDir()
	work := t.TempDir()

	if _, stderr, err := nooma(t, home, work, "init", "pablo.nooma"); err != nil {
		t.Fatalf("init: %v\nstderr: %s", err, stderr)
	}
	assertVault(t, filepath.Join(work, "pablo.nooma"))
}

// TestInitLeavesNoStagingDirectoryBehind is spec R7.4 from the outside: whatever
// init does internally, a caller must never find debris beside the vault.
func TestInitLeavesNoStagingDirectoryBehind(t *testing.T) {
	home, work := t.TempDir(), t.TempDir()

	if _, stderr, err := nooma(t, home, work, "init", filepath.Join(work, "clean.nooma")); err != nil {
		t.Fatalf("init: %v\nstderr: %s", err, stderr)
	}

	entries, err := os.ReadDir(work)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.Contains(e.Name(), ".tmp-") {
			t.Errorf("init left a staging directory behind: %s", e.Name())
		}
	}
	if len(entries) != 1 {
		t.Errorf("expected only the vault beside it, got %d entries", len(entries))
	}
}

// binaryPath builds cmd/nooma once for the whole package and returns the path.
//
// Once, not per test: seven tests each running `go build` would spend more time
// compiling than testing, and every one of them would be compiling the same
// commit. version_test.go deliberately keeps its own inline build — spec R10.2
// requires that test to pass unmodified, and rewiring it to share this helper
// would be modifying it.
var (
	buildOnce sync.Once
	builtPath string
	buildErr  error
)

func binaryPath(t *testing.T) string {
	t.Helper()

	buildOnce.Do(func() {
		dir, err := os.MkdirTemp("", "nooma-e2e-")
		if err != nil {
			buildErr = err
			return
		}
		name := "nooma"
		if runtime.GOOS == "windows" {
			name += ".exe"
		}
		builtPath = filepath.Join(dir, name)

		build := exec.Command("go", "build", "-buildvcs=false", "-o", builtPath, "./cmd/nooma")
		build.Dir = repoRoot(t)
		if out, err := build.CombinedOutput(); err != nil {
			buildErr = fmt.Errorf("go build ./cmd/nooma: %w\n%s", err, out)
		}
	})

	if buildErr != nil {
		t.Fatal(buildErr)
	}
	return builtPath
}

// TestFreshVaultIsLoadable is the difference between init creating files and init
// creating a usable vault.
//
// Checking that nooma.yml exists proves nothing about whether it decodes under
// the loader's own strict rules, or whether it passes validation. This project
// has already been bitten by that distinction once: the DSN work was verified by
// tests asserting on the string a function produced, and the string was right
// while the thing still could not open a file.
func TestFreshVaultIsLoadable(t *testing.T) {
	home, work := t.TempDir(), t.TempDir()
	target := filepath.Join(work, "loadable.nooma")

	if _, stderr, err := nooma(t, home, work, "init", target); err != nil {
		t.Fatalf("init: %v\nstderr: %s", err, stderr)
	}

	raw, err := os.ReadFile(filepath.Join(target, "nooma.yml"))
	if err != nil {
		t.Fatal(err)
	}

	cfg, err := config.Decode(strings.NewReader(string(raw)))
	if err != nil {
		t.Fatalf("the nooma.yml init generated does not decode:\n%v", err)
	}
	cfg.ApplyDefaults()

	if err := cfg.Validate(target, func(string) (string, bool) { return "", false }); err != nil {
		t.Fatalf("the nooma.yml init generated does not validate:\n%v", err)
	}

	dbPath, err := cfg.DatabasePath(target)
	if err != nil {
		t.Fatalf("database.path does not resolve: %v", err)
	}
	if _, err := os.Stat(dbPath); err != nil {
		t.Errorf("the config points at %s, which init did not create: %v", dbPath, err)
	}
}

// TestInitNeverOverwrites is spec R7.3, and the file case is the one that matters
// most: os.ReadDir on a plain file returns an error rather than an empty listing,
// so an emptiness check that ignored that error would classify a stray file as
// empty — and os.Remove deletes plain files happily. `touch pablo.nooma` followed
// by init would then delete it.
func TestInitNeverOverwrites(t *testing.T) {
	t.Run("a non-empty directory", func(t *testing.T) {
		home, work := t.TempDir(), t.TempDir()
		target := filepath.Join(work, "taken.nooma")
		if err := os.MkdirAll(filepath.Join(target, "sub"), 0o755); err != nil {
			t.Fatal(err)
		}

		if _, stderr, err := nooma(t, home, work, "init", target); err == nil {
			t.Fatal("init overwrote a non-empty directory")
		} else if !strings.Contains(stderr, "not empty") {
			t.Errorf("the error does not say why:\n%s", stderr)
		}
		if _, err := os.Stat(filepath.Join(target, "sub")); err != nil {
			t.Errorf("the existing content was disturbed: %v", err)
		}
	})

	t.Run("a plain file", func(t *testing.T) {
		home, work := t.TempDir(), t.TempDir()
		target := filepath.Join(work, "decoy.nooma")
		if err := os.WriteFile(target, []byte("not a vault"), 0o644); err != nil {
			t.Fatal(err)
		}

		if _, _, err := nooma(t, home, work, "init", target); err == nil {
			t.Fatal("init accepted a plain file as its target")
		}
		content, err := os.ReadFile(target)
		if err != nil {
			t.Fatalf("the file was deleted: %v", err)
		}
		if string(content) != "not a vault" {
			t.Errorf("the file was modified: %q", content)
		}
	})

	t.Run("a symlink", func(t *testing.T) {
		home, work := t.TempDir(), t.TempDir()
		real := filepath.Join(work, "real")
		if err := os.MkdirAll(real, 0o755); err != nil {
			t.Fatal(err)
		}
		link := filepath.Join(work, "link.nooma")
		if err := os.Symlink(real, link); err != nil {
			t.Skipf("symlinks unavailable here: %v", err)
		}

		if _, _, err := nooma(t, home, work, "init", link); err == nil {
			t.Fatal("init created a vault through a symlink")
		}
		if _, err := os.Lstat(link); err != nil {
			t.Errorf("the symlink was removed: %v", err)
		}
	})
}

// TestInitAcceptsAnExistingEmptyDirectory is the case Go's os.Rename makes
// awkward: it refuses ANY existing directory, empty or not, because its Lstat
// guard fires before rename(2) — which POSIX would have allowed. t.TempDir()
// returns exactly such a directory, and so does `mkdir myvault.nooma` followed by
// init, which is a thing people do.
func TestInitAcceptsAnExistingEmptyDirectory(t *testing.T) {
	home, work := t.TempDir(), t.TempDir()
	target := filepath.Join(work, "empty.nooma")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}

	if _, stderr, err := nooma(t, home, work, "init", target); err != nil {
		t.Fatalf("init refused an existing empty directory: %v\nstderr: %s", err, stderr)
	}
	assertVault(t, target)
}

// TestInitWithNoArgumentUsesTheHomeLocation is spec R7.1b, and it is the case the
// proposal's own demo depends on: `nooma init && nooma serve` from anywhere.
//
// It also pins what a bare init must NOT do — scatter six entries into whatever
// directory the user happens to be standing in.
func TestInitWithNoArgumentUsesTheHomeLocation(t *testing.T) {
	home := t.TempDir()
	work := t.TempDir()

	stdout, stderr, err := nooma(t, home, work, "init")
	if err != nil {
		t.Fatalf("bare init: %v\nstderr: %s", err, stderr)
	}

	container := filepath.Join(home, ".nooma")
	entries, err := os.ReadDir(container)
	if err != nil {
		t.Fatalf("bare init did not create %s: %v", container, err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected exactly one vault in %s, got %d", container, len(entries))
	}

	vault := filepath.Join(container, entries[0].Name())
	assertVault(t, vault)
	if !strings.HasSuffix(vault, ".nooma") {
		t.Errorf("the created vault %s does not end in .nooma, so resolution will not find it", vault)
	}
	if !strings.Contains(stdout, vault) {
		t.Errorf("bare init did not print where it created the vault:\n%s", stdout)
	}

	if left, _ := os.ReadDir(work); len(left) != 0 {
		t.Errorf("a bare init wrote %d entries into the working directory; it must not touch the cwd", len(left))
	}
}

// TestSecondBareInitRefuses keeps the default location decidable. Two vaults in
// ~/.nooma would make resolution step 4 permanently ambiguous (R6.2), so init
// refuses to create the ambiguity rather than leaving the user to discover it.
func TestSecondBareInitRefuses(t *testing.T) {
	home := t.TempDir()
	work := t.TempDir()

	if _, stderr, err := nooma(t, home, work, "init"); err != nil {
		t.Fatalf("first init: %v\nstderr: %s", err, stderr)
	}

	_, stderr, err := nooma(t, home, work, "init")
	if err == nil {
		t.Fatal("a second bare init succeeded, leaving two vaults in the default location")
	}
	if !strings.Contains(stderr, "ambiguous") {
		t.Errorf("the error does not explain why a second default vault is refused:\n%s", stderr)
	}
}
