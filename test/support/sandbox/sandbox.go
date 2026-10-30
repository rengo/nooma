// Package sandbox keeps a test binary away from the machine's real vaults.
//
// On 2026-10-30 a test ran `nooma reindex` with no vault argument; vault
// resolution found the maintainer's ~/.nooma, and the command migrated and
// rewrote a real vault. Main closes every road resolution takes to a real
// vault before any test runs: NOOMA_VAULT names a folder that is not a vault,
// so a command given none stops at resolution's first step, and HOME (with
// its Windows and XDG relatives) is a throwaway folder in case anything looks
// further.
package sandbox

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

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

	// Go's own caches default to HOME; pin them where they are first, so a
	// test that runs `go build` neither starts cold nor downloads.
	pinGoEnv("GOPATH", "GOMODCACHE", "GOCACHE")

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
	return m.Run()
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
