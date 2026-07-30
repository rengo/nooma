//go:build e2e

package e2e

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/rengo/nooma/internal/store/vaultlock"
)

// initVault creates a vault and returns its path, so the status tests start from
// the real thing rather than a hand-built imitation.
func initVault(t *testing.T, home, work, name string) string {
	t.Helper()

	target := filepath.Join(work, name)
	if _, stderr, err := nooma(t, home, work, "init", target); err != nil {
		t.Fatalf("init: %v\nstderr: %s", err, stderr)
	}
	return target
}

// TestStatusReportsWhatM0Owns is spec R12.1.
//
// The list is deliberately short, and the omission is the design. Doc 01
// describes status as reporting "last consolidation, channels, size" — but last
// consolidation is a domain row, and testdata/schema/store_api.golden exists to
// keep the store surface from growing a way to read one before M1. So M0's status
// reports what M0 owns: where the vault is, what schema it carries, whether
// anything holds it, how big it is, and what the configuration will actually do.
func TestStatusReportsWhatM0Owns(t *testing.T) {
	home, work := t.TempDir(), t.TempDir()
	vault := initVault(t, home, work, "pablo.nooma")

	stdout, stderr, err := nooma(t, home, work, "status", vault)
	if err != nil {
		t.Fatalf("status: %v\nstderr: %s", err, stderr)
	}

	for _, want := range []string{vault, "127.0.0.1", "7777", "nooma.db"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("status does not report %q:\n%s", want, stdout)
		}
	}
	if !strings.Contains(stdout, "schema") {
		t.Errorf("status does not report the schema version:\n%s", stdout)
	}
}

// TestStatusResolvesTheVaultLikeEveryOtherCommand is R10.4: with no argument it
// uses the same four-step resolution, so `cd pablo.nooma && nooma status` works.
func TestStatusResolvesTheVaultLikeEveryOtherCommand(t *testing.T) {
	home, work := t.TempDir(), t.TempDir()
	vault := initVault(t, home, work, "pablo.nooma")

	t.Run("from inside the vault", func(t *testing.T) {
		stdout, stderr, err := nooma(t, home, vault, "status")
		if err != nil {
			t.Fatalf("status: %v\nstderr: %s", err, stderr)
		}
		if !strings.Contains(stdout, vault) {
			t.Errorf("status resolved something else:\n%s", stdout)
		}
	})

	t.Run("from the directory holding it", func(t *testing.T) {
		stdout, stderr, err := nooma(t, home, work, "status")
		if err != nil {
			t.Fatalf("status: %v\nstderr: %s", err, stderr)
		}
		if !strings.Contains(stdout, vault) {
			t.Errorf("status resolved something else:\n%s", stdout)
		}
	})
}

// TestStatusOnAVaultThatIsNotThere fails with the path the user gave, rather than
// falling through to something else (R6.4).
func TestStatusOnAVaultThatIsNotThere(t *testing.T) {
	home, work := t.TempDir(), t.TempDir()

	missing := filepath.Join(work, "nope.nooma")
	_, stderr, err := nooma(t, home, work, "status", missing)
	if err == nil {
		t.Fatal("status succeeded on a path that is not a vault")
	}
	if !strings.Contains(stderr, "nope.nooma") {
		t.Errorf("the error does not name the path the user gave:\n%s", stderr)
	}
}

// TestStatusNeverPrintsASecret is spec R4.2 at the command level. The config
// holds only variable names, so this passes by construction today — which is
// exactly why it is pinned now, before somebody makes the output friendlier by
// resolving one.
func TestStatusNeverPrintsASecret(t *testing.T) {
	const sentinel = "sk-live-DO-NOT-LEAK-2f9c"

	home, work := t.TempDir(), t.TempDir()
	vault := initVault(t, home, work, "pablo.nooma")

	if err := os.WriteFile(filepath.Join(vault, ".env"), []byte("ANTHROPIC_API_KEY="+sentinel+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	stdout, stderr, err := nooma(t, home, work, "status", vault)
	if err != nil {
		t.Fatalf("status: %v\nstderr: %s", err, stderr)
	}
	if strings.Contains(stdout, sentinel) || strings.Contains(stderr, sentinel) {
		t.Fatalf("status leaked a secret value:\n%s\n%s", stdout, stderr)
	}
}

// TestStatusReportsTheLockHolder is spec R8.4 and R12.3 from the outside: status
// is read-only, so it must work on a vault a writer holds and say who holds it.
// It must never take the write lock, which would make "status on a running
// instance" impossible.
func TestStatusReportsTheLockHolder(t *testing.T) {
	home, work := t.TempDir(), t.TempDir()
	vault := initVault(t, home, work, "pablo.nooma")

	holder := holdVaultLock(t, vault)

	stdout, stderr, err := nooma(t, home, work, "status", vault)
	if err != nil {
		t.Fatalf("status on a held vault: %v\nstderr: %s", err, stderr)
	}
	if !strings.Contains(stdout, strconv.Itoa(holder)) {
		t.Errorf("status does not name the holding PID %d:\n%s", holder, stdout)
	}

	// And the same command on a free vault must not invent a holder.
	free := initVault(t, home, work, "free.nooma")
	stdout, _, err = nooma(t, home, work, "status", free)
	if err != nil {
		t.Fatalf("status on a free vault: %v", err)
	}
	if strings.Contains(stdout, strconv.Itoa(holder)) {
		t.Errorf("status reported a holder for a vault nobody holds:\n%s", stdout)
	}
}

// holdVaultLock takes the vault's write lock in THIS process and returns the PID
// status should report.
//
// The test process is the holder, which is simpler than spawning a third party
// and proves the same thing: `nooma status` runs as a child, and if it tried to
// acquire rather than merely read, it would fail against a lock this process
// holds. The assertion that it succeeds *is* the assertion that it takes no lock.
func holdVaultLock(t *testing.T, vault string) int {
	t.Helper()

	lock, err := vaultlock.Acquire(vault)
	if err != nil {
		t.Fatalf("acquiring the vault lock: %v", err)
	}
	t.Cleanup(func() { _ = lock.Release() })
	return os.Getpid()
}
