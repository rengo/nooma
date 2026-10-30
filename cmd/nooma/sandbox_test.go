package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rengo/nooma/internal/config"
)

// TestSandbox_AVaultlessCommandNeverReachesAHomeVault: every test in this
// package runs with HOME in a throwaway folder and NOOMA_VAULT naming a
// folder that is not a vault (TestMain, sandbox.Main). So a command given no
// vault stops at the first step of resolution, before the working directory
// or ~/.nooma are searched: even a vault planted in the sandbox's own
// ~/.nooma is never opened. On 2026-10-30 a test that ran `reindex` with no
// vault resolved the maintainer's real one and migrated it.
func TestSandbox_AVaultlessCommandNeverReachesAHomeVault(t *testing.T) {
	home := os.Getenv("HOME")
	if !strings.Contains(home, "nooma-sandbox-") {
		t.Fatalf("HOME = %q during tests, want a folder in the sandbox: TestMain did not sandbox this package", home)
	}
	if vault := os.Getenv("NOOMA_VAULT"); vault == "" || !strings.Contains(vault, "nooma-sandbox-") {
		t.Fatalf("NOOMA_VAULT = %q during tests, want the sandbox's folder that is not a vault", vault)
	}
	if real, _ := os.UserHomeDir(); real != home {
		t.Fatalf("os.UserHomeDir = %q, want the sandboxed %q", real, home)
	}

	decoy := filepath.Join(home, config.HomeVaultDir, "decoy.nooma")
	if err := os.MkdirAll(decoy, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(decoy, config.ConfigFileName), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(filepath.Join(home, config.HomeVaultDir)) })

	if _, err := config.ResolveVault(""); err == nil || !strings.Contains(err.Error(), "is not a vault") {
		t.Errorf("ResolveVault(\"\") = %v, want it to stop at NOOMA_VAULT, which is not a vault", err)
	}
	for _, command := range []string{"consolidate", "status", "doctor"} {
		var out, errOut bytes.Buffer
		if err := run([]string{command}, &out, &errOut); err == nil || !strings.Contains(err.Error(), "is not a vault") {
			t.Errorf("nooma %s with no vault = %v, want a refusal before any vault is found", command, err)
		}
	}
	entries, err := os.ReadDir(decoy)
	if err != nil || len(entries) != 1 {
		t.Errorf("the planted vault holds %v, %v; want only its nooma.yml: nothing reached it", entries, err)
	}
}
