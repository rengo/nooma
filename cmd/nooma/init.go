package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/user"
	"path/filepath"
	"strings"

	"github.com/rengo/nooma/internal/config"
	"github.com/rengo/nooma/internal/store/sqlite"
)

// vaultDirs are the directories `nooma init` creates, per
// docs/01-architecture.md §"Vault structure".
var vaultDirs = []string{"attachments", "derived", "logs"}

// runInit creates a vault.
//
// With a path, that path is the target — relative to the working directory if
// relative, matching every other command (spec R6.4). With no argument the target
// is ~/.nooma/<username>.nooma (spec R7.1b).
func runInit(args []string, out, errOut io.Writer) error {
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	fs.SetOutput(errOut)
	fs.Usage = func() {
		_, _ = fmt.Fprint(errOut, "usage: nooma init [vault]\n\n"+
			"With no argument, creates ~/.nooma/<username>.nooma.\n")
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 1 {
		return fmt.Errorf("init takes at most one vault path, got %d", fs.NArg())
	}

	target, err := initTarget(fs.Arg(0))
	if err != nil {
		return err
	}

	if err := createVault(target); err != nil {
		return err
	}

	_, err = fmt.Fprintf(out, "created vault %s\n", target)
	return err
}

// initTarget resolves where the vault will be created (spec R7.1b).
//
// With no argument the target is ~/.nooma/<username>.nooma, not the working
// directory, and that choice is the whole of this function's judgement. A bare
// command that writes nooma.db, nooma.yml, .env and three directories into
// wherever the user happens to be standing is a command that will one day be run
// in $HOME, or in a source checkout. Creating a named directory in a known place
// is recoverable and predictable; scattering six entries into an arbitrary cwd is
// neither.
func initTarget(arg string) (string, error) {
	if arg != "" {
		return filepath.Abs(arg)
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolving the home directory: %w", err)
	}
	container := filepath.Join(home, config.HomeVaultDir)

	// A second bare init would leave two vaults in ~/.nooma, and resolution step 4
	// requires exactly one (spec R6.2). Refusing here keeps the default location
	// usable by refusing to make it undecidable — the alternative is a user who
	// runs init twice and then cannot start the binary without an explicit path.
	if entries, err := os.ReadDir(container); err == nil {
		for _, e := range entries {
			name := e.Name()
			if !e.IsDir() || name == config.HomeVaultDir || !strings.HasSuffix(name, config.VaultSuffix) {
				continue
			}
			if config.IsVault(filepath.Join(container, name)) {
				return "", fmt.Errorf(
					"%s already holds the vault %s, and a second one there would make the default location ambiguous.\n"+
						"Pass a path explicitly, for example `nooma init ~/work.nooma`",
					container, name)
			}
		}
	}

	return filepath.Join(container, username()+config.VaultSuffix), nil
}

// username is the vault's name in home mode, matching docs/01-architecture.md's
// own `pablo.nooma` example.
func username() string {
	if u, err := user.Current(); err == nil && u.Username != "" {
		name := u.Username
		// On Windows this is "DOMAIN\\user", and only the last element is a legal
		// directory name.
		if i := strings.LastIndexAny(name, `\/`); i >= 0 {
			name = name[i+1:]
		}
		if name != "" {
			return name
		}
	}
	return "nooma"
}

// createVault builds the vault in a sibling staging directory and moves it into
// place, so a failure anywhere leaves the filesystem as it was (spec R7.4).
//
// The order of the target checks IS the safety:
//
//  1. Lstat first, and refuse anything that is not a plain directory without
//     touching it. os.ReadDir on a plain FILE returns an error, not an empty
//     listing — so an emptiness check written as len(entries) == 0 without also
//     requiring err == nil classifies a stray file as "empty", and os.Remove
//     deletes files happily. `touch pablo.nooma && nooma init pablo.nooma` would
//     delete it: non-negotiable #6 violated at the vault's own root. Lstat rather
//     than Stat, so a symlink is seen as a symlink instead of as whatever it
//     points at.
//  2. An existing empty directory is accepted, and has to be: os.Rename refuses
//     ANY existing directory, empty or not (Go's own Lstat guard fires before
//     rename(2), which POSIX would have allowed), and `mkdir x.nooma && nooma
//     init x.nooma` is a thing people do.
//  3. A non-empty directory is refused (R7.3). An init that overwrites is a
//     delete with better manners.
func createVault(target string) error {
	switch info, err := os.Lstat(target); {
	case err == nil && info.Mode()&os.ModeSymlink != 0:
		return fmt.Errorf("%s is a symlink; refusing to create a vault through it", target)
	case err == nil && !info.IsDir():
		return fmt.Errorf("%s already exists and is not a directory", target)
	case err == nil:
		empty, err := isEmptyDir(target)
		if err != nil {
			return err
		}
		if !empty {
			return fmt.Errorf("%s already exists and is not empty; refusing to overwrite it", target)
		}
	case !os.IsNotExist(err):
		return fmt.Errorf("inspecting %s: %w", target, err)
	}

	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return fmt.Errorf("creating %s: %w", filepath.Dir(target), err)
	}

	// MkdirTemp creates the directory exclusively and appends a random suffix, so
	// two racing `init`s cannot build into the same staging directory. That is a
	// guarantee from the standard library rather than a name this code picks and
	// hopes about.
	staging, err := os.MkdirTemp(filepath.Dir(target), filepath.Base(target)+".tmp-")
	if err != nil {
		return fmt.Errorf("creating a staging directory beside %s: %w", target, err)
	}
	defer func() { _ = os.RemoveAll(staging) }()

	if err := populateVault(staging); err != nil {
		return err
	}

	return moveIntoPlace(staging, target)
}

func moveIntoPlace(staging, target string) error {
	if _, err := os.Lstat(target); err == nil {
		if err := os.Remove(target); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("clearing the empty directory at %s: %w", target, err)
		}
	}

	if err := os.Rename(staging, target); err != nil {
		if errors.Is(err, os.ErrExist) || strings.Contains(err.Error(), "not empty") {
			return fmt.Errorf("%s was created by something else while this vault was being built", target)
		}
		return fmt.Errorf("moving the new vault into %s: %w", target, err)
	}
	return nil
}

func populateVault(dir string) error {
	for _, sub := range vaultDirs {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0o755); err != nil {
			return fmt.Errorf("creating %s: %w", sub, err)
		}
	}

	if err := os.WriteFile(filepath.Join(dir, config.ConfigFileName), []byte(defaultConfig()), 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", config.ConfigFileName, err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte(envSkeleton()), 0o600); err != nil {
		return fmt.Errorf("writing .env: %w", err)
	}

	// The database is created last, because it is the only step that can fail for
	// reasons outside this process's control, and failing before it means less to
	// unwind.
	dbPath := filepath.Join(dir, filepath.Base(config.DefaultDatabasePath))
	vault, err := sqlite.Open(context.Background(), dbPath)
	if err != nil {
		return fmt.Errorf("creating the database: %w", err)
	}
	return vault.Close()
}

func isEmptyDir(dir string) (bool, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false, fmt.Errorf("listing %s: %w", dir, err)
	}
	return len(entries) == 0, nil
}

// defaultConfig is the nooma.yml a new vault starts with.
//
// TestFreshVaultIsLoadable runs `nooma init` and then loads the result through
// the real loader, so this is not a template somebody eyeballed once: if it stops
// decoding or stops validating, that test fails. Everything here is either a documented default restated for discoverability or
// a commented example — a fresh vault configures no provider and enables no
// channel, because M0 interprets neither.
func defaultConfig() string {
	return `# nooma.yml — see docs/01-architecture.md for the full schema.
#
# Secrets are never written here. A credential is always referenced by the NAME
# of an environment variable, and the value lives in the .env beside this file
# (which is never committable) or in the process environment.

server:
  bind: 127.0.0.1      # a non-loopback bind requires auth_token_env (ADR-0007)
  http_port: 7777
  ui: true

database:
  path: ./nooma.db     # relative to this vault; it may not point outside it

# providers:           # added in M1, when nooma starts calling models
# tasks:

channels:
  telegram:
    enabled: false     # enabling this without allowed_chat_ids is a config error
`
}

// envSkeleton documents the accepted format where the user edits it.
//
// The parser accepts a deliberately narrow subset and rejects everything else by
// name, so the rules belong here rather than only in a doc: a user who writes
// `export FOO=bar` should find out from the file they are editing, not from an
// error after a restart.
func envSkeleton() string {
	return `# Secrets for this vault. Never commit this file.
#
# Accepted, and nothing else:
#
#   KEY=value          KEY is letters, digits and _, not starting with a digit
#   KEY="value"        quotes are stripped
#   KEY='value'        either kind
#   KEY=               a deliberate empty value
#   # a comment        first non-space character is #
#
# Rejected, with the line number: an export prefix, a missing '=', a quote that
# is never closed, the same KEY twice, and a bare # after an unquoted value
# (quote the value if the # is part of it).
#
# A variable already set in the environment always wins over this file.

# ANTHROPIC_API_KEY=
# TELEGRAM_BOT_TOKEN=
`
}
