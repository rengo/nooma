package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestUnreadableDirectoryFailsResolution is spec R6.8, and it is this project's
// dominant defect family in its natural habitat.
//
// Treating a permission error as "nothing here" makes the ascent walk PAST the
// level that may hold the right vault and open a different one higher up, or fall
// through to ~/.nooma — silently, with the user's own filesystem as the only
// evidence that anything happened.
func TestUnreadableDirectoryFailsResolution(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root: permission bits do not deny anything, so this cannot be observed")
	}

	root := vaultTree(t, "locked/inside/", "decoy.nooma/nooma.yml")
	locked := filepath.Join(root, "locked")

	if err := os.Chmod(locked, 0o111); err != nil { // traversable, not listable
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })

	_, err := resolveVault("", env(filepath.Join(locked, "inside"), root, nil))
	if err == nil {
		t.Fatal("resolution walked past a directory it could not list; a vault there would have been invisible")
	}
	if !strings.Contains(err.Error(), "locked") {
		t.Errorf("error does not name the directory that could not be listed:\n%v", err)
	}
	// Falling through to decoy.nooma is exactly the silent wrong-vault outcome.
	if strings.Contains(err.Error(), "decoy.nooma") {
		t.Errorf("resolution continued upward and found another vault:\n%v", err)
	}
}

// TestMissingDirectoryIsNotAnError is R6.8's other half, and the reason the rule
// distinguishes rather than treating every listing failure alike. ~/.nooma is
// absent on a machine that has never run `nooma init`; that is the "no vault
// found" case, not a permission problem the user does not have.
func TestMissingDirectoryIsNotAnError(t *testing.T) {
	t.Parallel()

	home := t.TempDir() // no .nooma inside
	elsewhere := t.TempDir()

	_, err := resolveVault("", env(elsewhere, home, nil))
	if err == nil {
		t.Fatal("resolveVault found a vault where there is none")
	}
	if !strings.Contains(err.Error(), "nooma init") {
		t.Errorf("a missing ~/.nooma should read as 'no vault yet', not as a failure to list it:\n%v", err)
	}
	if strings.Contains(err.Error(), "permission") {
		t.Errorf("a missing directory was reported as unreadable:\n%v", err)
	}
}

// TestNoomaYmlMustBeAFile is spec R6.9. `mkdir nooma.yml` happens — a typo, a bad
// archive extraction, a botched cp. An existence-only predicate accepts it,
// resolution succeeds, and the failure surfaces later as a confusing read error
// about a path the user believes is a file.
func TestNoomaYmlMustBeAFile(t *testing.T) {
	t.Parallel()

	root := vaultTree(t, "broken.nooma/nooma.yml/", "here/")

	if IsVault(filepath.Join(root, "broken.nooma")) {
		t.Error("IsVault accepted a directory named nooma.yml")
	}

	_, err := resolveVault("", env(filepath.Join(root, "here"), root, nil))
	if err == nil {
		t.Fatal("resolveVault treated a directory named nooma.yml as a vault")
	}
}

// TestDescribeVaultProblem is spec R6.5's specific diagnostic. A single-criterion
// predicate cannot distinguish "db present, config absent" from "empty
// directory", so the diagnostic probes the DEFAULT database path only — the real
// one is configurable and lives in the very file that is missing. That limit is
// stated rather than papered over.
func TestDescribeVaultProblem(t *testing.T) {
	t.Parallel()

	t.Run("a directory holding only a database names what is missing", func(t *testing.T) {
		t.Parallel()

		root := vaultTree(t, "half.nooma/nooma.db")

		got := DescribeVaultProblem(filepath.Join(root, "half.nooma"))
		if !strings.Contains(got, ConfigFileName) {
			t.Errorf("diagnostic does not name the missing file:\n%s", got)
		}
		if !strings.Contains(got, "nooma.db") {
			t.Errorf("diagnostic does not mention the database it did find, which is what makes it specific:\n%s", got)
		}
	})

	t.Run("an ordinary directory gets the plain answer", func(t *testing.T) {
		t.Parallel()

		root := vaultTree(t, "empty/")

		got := DescribeVaultProblem(filepath.Join(root, "empty"))
		if !strings.Contains(got, ConfigFileName) {
			t.Errorf("diagnostic does not name what a vault needs:\n%s", got)
		}
		if strings.Contains(got, "nooma.db") {
			t.Errorf("diagnostic invented a database that is not there:\n%s", got)
		}
	})

	t.Run("a directory named nooma.yml is named as the problem it is", func(t *testing.T) {
		t.Parallel()

		root := vaultTree(t, "broken.nooma/nooma.yml/")

		got := DescribeVaultProblem(filepath.Join(root, "broken.nooma"))
		if !strings.Contains(got, "directory") {
			t.Errorf("diagnostic does not say nooma.yml is a directory, which is the actual problem:\n%s", got)
		}
	})
}
