// Package sandbox keeps a test binary away from the machine's real vaults.
//
// On 2026-10-30 a test ran `nooma reindex` with no vault argument; vault
// resolution found the maintainer's ~/.nooma, and the command migrated and
// rewrote a real vault. Main closes every road resolution takes to a real
// vault before any test runs: NOOMA_VAULT names a folder that is not a vault,
// so a command given none stops at resolution's first step, and HOME (with
// its Windows and XDG relatives) is a throwaway folder in case anything looks
// further. Under Main, NOOMA_VAULT is never empty in a test's own process: empty, resolution
// would walk up the working directory's parents and could find a *.nooma
// folder there before it reached HOME. The temporary directory moves into the
// sandbox too, and Child starts a child there, so a child given an empty
// NOOMA_VAULT (with its own HOME) climbs through the sandbox first.
// test/harness's sandbox gate checks every package follows this.
package sandbox

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// active is true while Main is running a package's tests. Child refuses to
// run outside it, where HOME and the temporary directory are the machine's.
var active bool

// Main runs m inside a throwaway home, for a package's TestMain:
//
//	func TestMain(m *testing.M) { os.Exit(sandbox.Main(m)) }
//
// A test that needs a vault names one, or sets NOOMA_VAULT itself.
func Main(m *testing.M) int {
	root, err := os.MkdirTemp("", "nooma-sandbox-*")
	if err != nil {
		fmt.Fprintln(os.Stderr, "sandbox:", err)
		return 1
	}
	defer func() { _ = os.RemoveAll(root) }()

	// Go's own caches and its config file default to HOME; pin them where
	// they are first, so a test that runs `go build` neither starts cold, nor
	// downloads, nor reads a different go env.
	pinGoEnv("GOPATH", "GOMODCACHE", "GOCACHE", "GOENV")

	home, notAVault := filepath.Join(root, "home"), filepath.Join(root, "not-a-vault")
	dirs := map[string]string{
		"HOME":            home,
		"USERPROFILE":     home,
		"NOOMA_VAULT":     notAVault,
		"XDG_CONFIG_HOME": filepath.Join(root, "xdg-config"),
		"XDG_DATA_HOME":   filepath.Join(root, "xdg-data"),
		"XDG_STATE_HOME":  filepath.Join(root, "xdg-state"),
		"XDG_CACHE_HOME":  filepath.Join(root, "xdg-cache"),
		"APPDATA":         filepath.Join(root, "appdata"),
		"LOCALAPPDATA":    filepath.Join(root, "localappdata"),
		"TMPDIR":          filepath.Join(root, "tmp"),
		"TMP":             filepath.Join(root, "tmp"),
		"TEMP":            filepath.Join(root, "tmp"),
	}
	for name, dir := range dirs {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			fmt.Fprintln(os.Stderr, "sandbox:", err)
			return 1
		}
		if err := os.Setenv(name, dir); err != nil {
			fmt.Fprintln(os.Stderr, "sandbox:", err)
			return 1
		}
	}
	active = true
	defer func() { active = false }()
	return m.Run()
}

// Child readies cmd to run nooma with no vault named: its own HOME, and an
// empty NOOMA_VAULT so the child exercises resolution. Empty is
// acceptable only here, beside a HOME of the child's own and a working
// directory under the sandbox's temporary folder (the caller's Dir, when it set
// one). Resolution still walks past the sandbox to the filesystem root (on
// Windows the default temporary folder is under the user's profile), so this
// narrows the roads to a real vault and does not prove it closed. A test builds
// no such environment by hand, which the gate in test/harness checks.
// Child panics outside Main: there the temporary folder is the machine's.
func Child(cmd *exec.Cmd, home string) {
	if !active {
		panic("sandbox.Child: called outside sandbox.Main; the package's TestMain must call it (docs/06-harness.md §3)")
	}
	if home == "" {
		panic("sandbox.Child: an empty home is not a home of the child's own")
	}
	cmd.Env = append(os.Environ(), "HOME="+home, "USERPROFILE="+home, "NOOMA_VAULT=")
	if cmd.Dir == "" {
		cmd.Dir = os.TempDir()
	}
}

// pinGoEnv sets each of names to the value `go env` reports for it now, when
// the environment does not already set it.
func pinGoEnv(names ...string) {
	out, err := exec.Command("go", append([]string{"env"}, names...)...).Output()
	if err != nil {
		return // no go on PATH: nothing in this run builds with it
	}
	values := strings.Split(strings.TrimRight(string(out), "\n"), "\n")
	for i, name := range names {
		if i < len(values) && os.Getenv(name) == "" && strings.TrimSpace(values[i]) != "" {
			_ = os.Setenv(name, strings.TrimSpace(values[i]))
		}
	}
}
