package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// VaultSuffix marks a directory as a vault by name. The structural test is
// IsVault; this is only what the search globs for.
const VaultSuffix = ".nooma"

// HomeVaultDir is where installed mode keeps vaults. It is a container, never a
// vault itself — see the note in candidatesIn about why that distinction needs
// enforcing rather than assuming.
const HomeVaultDir = ".nooma"

// environment is everything resolution reads from the outside world.
//
// It is injected for the reason D7 gives: without it, the precedence tests would
// have to mutate the real $HOME and chdir the process, which is global state no
// parallel test can share.
//
// **There is deliberately no executable member.** R6.6 forbids resolution from
// consulting the binary's own directory, and the surest way to guarantee that is
// to leave it no way to ask. os.Executable returns the *resolved* path, so a
// symlinked install would search a directory the user never typed — a search
// location nobody can predict from the command they ran.
type environment struct {
	getenv  func(string) string
	getwd   func() (string, error)
	homeDir func() (string, error)
	readDir func(string) ([]os.DirEntry, error)
}

func osEnvironment() environment {
	return environment{
		getenv:  os.Getenv,
		getwd:   os.Getwd,
		homeDir: os.UserHomeDir,
		readDir: os.ReadDir,
	}
}

// ResolveVault finds the vault to operate on, in the four ordered steps of
// docs/01-architecture.md §"Vault resolution at startup".
//
// arg is the optional explicit path; pass "" when the user gave none.
func ResolveVault(arg string) (string, error) {
	return resolveVault(arg, osEnvironment())
}

func resolveVault(arg string, env environment) (string, error) {
	// Steps 1 and 2: an explicitly named path is used as given and never falls
	// through. Falling through would mean a typo in $NOOMA_VAULT silently opens a
	// different brain, which is the failure the candidate rules exist to prevent,
	// arriving by another route.
	if arg == "" {
		arg = env.getenv("NOOMA_VAULT")
	}
	if arg != "" {
		path, err := absolute(arg, env)
		if err != nil {
			return "", err
		}
		if !IsVault(path) {
			return "", fmt.Errorf("%s is not a vault: no %s inside it", path, ConfigFileName)
		}
		return path, nil
	}

	// Step 3: ascend from the working directory to the filesystem root. At each
	// level the directory itself is tested before its children, so standing in a
	// vault means that vault even when it contains another.
	cwd, err := env.getwd()
	if err != nil {
		return "", fmt.Errorf("resolving the working directory: %w", err)
	}
	dir, err := absolute(cwd, env)
	if err != nil {
		return "", err
	}
	for {
		if IsVault(dir) {
			return dir, nil
		}
		found, err := candidatesIn(dir, env)
		if err != nil {
			return "", err
		}
		if len(found) > 0 {
			return pickExactlyOne(dir, found)
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}

	// Step 4: installed mode.
	home, err := env.homeDir()
	if err != nil {
		return "", fmt.Errorf("resolving the home directory: %w", err)
	}
	homeVaults := filepath.Join(home, HomeVaultDir)
	found, err := candidatesIn(homeVaults, env)
	if err != nil {
		return "", err
	}
	if len(found) == 0 {
		return "", fmt.Errorf(
			"no vault found: searched from %s up to the filesystem root, then %s.\nRun `nooma init` to create one, or pass a path explicitly",
			cwd, homeVaults)
	}
	return pickExactlyOne(homeVaults, found)
}

// candidatesIn lists the vaults directly inside dir.
//
// Two exclusions, both narrower than they look:
//
// Only directories count (R6.3). A file named `decoy.nooma` is not a vault, and
// counting it would turn a stray download into an ambiguity the user has to
// resolve.
//
// The literal name `.nooma` is never a candidate (R6.7). The glob matches it,
// because `*` also matches the empty string, so ~/.nooma — the container that
// holds vaults — looks like one by name. It has no config inside, so the
// structural predicate would reject it anyway; the requirement is that it must
// never be REPORTED as a broken vault, because it is doing exactly its job and
// sending the user to repair it would be a bug in the message.
func candidatesIn(dir string, env environment) ([]string, error) {
	entries, err := env.readDir(dir)
	if err != nil {
		// R6.8. A directory that does not exist holds no candidates, and that is
		// an ordinary state: ~/.nooma is absent on a machine that has never run
		// `nooma init`.
		//
		// Any *other* listing failure is fatal, and the distinction is the whole
		// requirement. Treating a permission error as "nothing here" makes the
		// ascent walk past the level that may hold the right vault and open a
		// different one higher up — silently, with the user's own filesystem as
		// the only evidence anything happened.
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("cannot list %s while looking for a vault: %w", dir, err)
	}

	var found []string
	for _, e := range entries {
		name := e.Name()
		if !e.IsDir() || name == HomeVaultDir || !strings.HasSuffix(name, VaultSuffix) {
			continue
		}
		if IsVault(filepath.Join(dir, name)) {
			found = append(found, name)
		}
	}
	return found, nil
}

func pickExactlyOne(dir string, found []string) (string, error) {
	if len(found) == 1 {
		return filepath.Join(dir, found[0]), nil
	}
	return "", fmt.Errorf(
		"%d vaults in %s:\n  %s\nPass one explicitly, for example `nooma serve %s`",
		len(found), dir, strings.Join(found, "\n  "), filepath.Join(dir, found[0]))
}

// ConfigFileName is the marker that makes a directory a vault.
//
// The database cannot be the marker: database.path is configurable, so a vault
// whose database lives in a subdirectory is still a vault. The configuration file
// is at a fixed path by the layout decision, which leaves it the only stable one.
const ConfigFileName = "nooma.yml"

// IsVault reports whether dir is a vault.
//
// R6.9: the config must be a file, not merely an entry that exists.
// `mkdir nooma.yml` happens — a typo, a bad archive extraction, a botched cp — and
// an existence-only predicate would accept it, let resolution succeed, and leave
// the failure to surface later as a confusing read error about a path the user
// believes is a file. The check costs one IsDir() on information Stat already
// returned.
func IsVault(dir string) bool {
	info, err := os.Stat(filepath.Join(dir, ConfigFileName))
	return err == nil && !info.IsDir()
}

// DescribeVaultProblem explains, for a directory that is not a vault, what is
// missing — the specific diagnostic spec R6.5 asks for.
//
// It probes the DEFAULT database path only, and that limit is real: the actual
// path is configurable and lives in `nooma.yml`, which is the very file absent in
// the case worth diagnosing. A vault whose database was moved elsewhere trades
// this diagnostic away, which is an honest limitation rather than a check that
// pretends to know something it cannot.
func DescribeVaultProblem(dir string) string {
	config := filepath.Join(dir, ConfigFileName)

	if info, err := os.Stat(config); err == nil {
		if info.IsDir() {
			return fmt.Sprintf("%s is a directory, not a file; a vault's %s must be a file", config, ConfigFileName)
		}
		return fmt.Sprintf("%s looks like a vault", dir)
	}

	if _, err := os.Stat(filepath.Join(dir, filepath.Base(DefaultDatabasePath))); err == nil {
		return fmt.Sprintf(
			"%s has a %s but no %s, so it is not a usable vault — the configuration is what makes one",
			dir, filepath.Base(DefaultDatabasePath), ConfigFileName)
	}

	return fmt.Sprintf("%s is not a vault: it has no %s", dir, ConfigFileName)
}

func absolute(path string, env environment) (string, error) {
	if filepath.IsAbs(path) {
		return filepath.Clean(path), nil
	}
	cwd, err := env.getwd()
	if err != nil {
		return "", fmt.Errorf("resolving %q against the working directory: %w", path, err)
	}
	return filepath.Clean(filepath.Join(cwd, path)), nil
}
