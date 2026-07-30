package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/rengo/nooma/internal/config"
	"github.com/rengo/nooma/internal/store/sqlite"
	"github.com/rengo/nooma/internal/store/vaultlock"
)

// runStatus reports on a vault without starting anything (spec R12.1).
//
// The list of what it reports is short, and the omission is the design.
// docs/01-architecture.md describes status as showing "last consolidation,
// channels, size" — but last consolidation is a domain row, and
// testdata/schema/store_api.golden exists precisely to keep the store surface
// from growing a way to read one before M1. So M0's status reports what M0 owns:
// where the vault is, what schema it carries, whether anything holds it, how big
// it is, and what the configuration will actually do.
//
// It is read-only in the strong sense: it never takes the write lock (R8.4). An
// implementation that acquired in order to inspect would make `nooma status`
// useless against a running instance, which is the one moment somebody most wants
// to run it.
func runStatus(args []string, out, errOut io.Writer) error {
	fs := flag.NewFlagSet("status", flag.ContinueOnError)
	fs.SetOutput(errOut)
	fs.Usage = func() { _, _ = fmt.Fprint(errOut, "usage: nooma status [vault]\n") }
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 1 {
		return fmt.Errorf("status takes at most one vault path, got %d", fs.NArg())
	}

	vault, err := config.ResolveVault(fs.Arg(0))
	if err != nil {
		return err
	}

	cfg, err := loadVaultConfig(vault)
	if err != nil {
		return err
	}

	var b strings.Builder
	fmt.Fprintf(&b, "vault:     %s\n", vault)

	if pid, held, err := vaultlock.ReadHolder(vault); err != nil {
		fmt.Fprintf(&b, "lock:      unreadable: %v\n", err)
	} else if held {
		fmt.Fprintf(&b, "lock:      held by process %d\n", pid)
	} else {
		fmt.Fprintf(&b, "lock:      free\n")
	}

	dbPath, err := cfg.DatabasePath(vault)
	if err != nil {
		return err
	}
	if info, err := os.Stat(dbPath); err == nil {
		fmt.Fprintf(&b, "database:  %s (%s)\n", filepath.Base(dbPath), humanSize(info.Size()))
	} else {
		fmt.Fprintf(&b, "database:  %s (missing)\n", filepath.Base(dbPath))
	}

	// Opening the vault read-only is the only way to learn its schema version, and
	// it is safe while a writer holds it: SQLite in WAL mode allows concurrent
	// readers, and this takes no vaultlock.
	if version, err := schemaVersion(dbPath); err != nil {
		fmt.Fprintf(&b, "schema:    unreadable: %v\n", err)
	} else {
		fmt.Fprintf(&b, "schema:    version %d\n", version)
	}

	b.WriteString("\n")
	b.WriteString(cfg.Summary())

	_, err = io.WriteString(out, b.String())
	return err
}

// loadVaultConfig reads and prepares a vault's configuration the way every
// command that touches a vault does: decode strictly, apply the documented
// defaults, and load .env without letting it override the environment.
func loadVaultConfig(vault string) (*config.Config, error) {
	if assignments, err := readEnvFile(filepath.Join(vault, ".env")); err != nil {
		return nil, err
	} else if err := config.ApplyEnv(assignments, os.LookupEnv, os.Setenv); err != nil {
		return nil, err
	}

	f, err := os.Open(filepath.Join(vault, config.ConfigFileName))
	if err != nil {
		return nil, fmt.Errorf("opening the vault's configuration: %w", err)
	}
	defer func() { _ = f.Close() }()

	cfg, err := config.Decode(f)
	if err != nil {
		return nil, err
	}
	cfg.ApplyDefaults()
	return cfg, nil
}

func readEnvFile(path string) (map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			// A vault whose secrets all come from the environment is valid.
			return nil, nil
		}
		return nil, fmt.Errorf("opening %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()
	return config.ParseEnvFile(f)
}

func schemaVersion(dbPath string) (int, error) {
	vault, err := sqlite.Open(context.Background(), dbPath)
	if err != nil {
		return 0, err
	}
	defer func() { _ = vault.Close() }()
	return vault.SchemaVersion(context.Background())
}

// humanSize keeps the output readable without pulling in a dependency for four
// lines of arithmetic.
func humanSize(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for size := n / unit; size >= unit; size /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGT"[exp])
}
