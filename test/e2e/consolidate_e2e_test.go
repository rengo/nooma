//go:build e2e

package e2e

import (
	"fmt"
	"strings"
	"testing"

	"github.com/rengo/nooma/internal/store/vaultlock"
)

// TestConsolidate_Lock is spec R6.1 end to end: the compiled `nooma
// consolidate` subcommand, run against a vault a `serve` process already
// holds the write lock on, returns a clean, non-zero-exit error naming the
// holder — never a silent hang or a corrupted concurrent write. Against an
// unlocked vault, it succeeds.
func TestConsolidate_Lock(t *testing.T) {
	t.Run("a held vault refuses and names the holder", func(t *testing.T) {
		home, work := t.TempDir(), t.TempDir()
		vault := initVault(t, home, work, "pablo.nooma")

		holder := holdVaultLock(t, vault)

		_, stderr, err := nooma(t, home, work, "consolidate", vault)
		if err == nil {
			t.Fatal("consolidate succeeded against a vault a lock holder already holds")
		}
		if !strings.Contains(stderr, fmt.Sprint(holder)) {
			t.Errorf("the refusal does not name the holding PID %d:\n%s", holder, stderr)
		}
	})

	t.Run("an unlocked vault succeeds", func(t *testing.T) {
		home, work := t.TempDir(), t.TempDir()
		vault := initVault(t, home, work, "pablo.nooma")

		_, stderr, err := nooma(t, home, work, "consolidate", vault)
		if err != nil {
			t.Fatalf("consolidate: %v\nstderr: %s", err, stderr)
		}

		if _, held, err := vaultlock.ReadHolder(vault); err != nil {
			t.Fatalf("ReadHolder after consolidate: %v", err)
		} else if held {
			t.Error("the vault is still reported as held after consolidate exited")
		}
	})
}
