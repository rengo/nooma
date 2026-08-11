package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/rengo/nooma/internal/brain"
	"github.com/rengo/nooma/internal/config"
	"github.com/rengo/nooma/internal/store/sqlite"
	"github.com/rengo/nooma/internal/store/vaultlock"
)

// init registers "consolidate" into main.go's dispatch table (main.go:47).
// A package-level var initializes before any init() runs, so commands is
// already the literal map main.go declares by the time this assignment
// happens — the same guarantee that lets capture_test.go and every other
// command test read commands["<name>"] without caring which file added it.
func init() {
	commands["consolidate"] = command{
		summary: "run a consolidation pass over a vault",
		run:     runConsolidate,
	}
}

// runConsolidate is spec R6's own CLI: `nooma consolidate` writes directly
// to the vault (unlike `capture`, which proxies over HTTP to a `serve`
// that already holds the lock), so it takes the write lock itself —
// `serve.go`'s own pattern (runServe, cmd/nooma/serve.go:71-79) — before
// opening the store, and fails with a clean error naming the holder
// instead of a silent hang or a corrupted concurrent write (spec R6.1).
func runConsolidate(args []string, out, errOut io.Writer) error {
	fs := flag.NewFlagSet("consolidate", flag.ContinueOnError)
	fs.SetOutput(errOut)
	fs.Usage = func() { _, _ = fmt.Fprint(errOut, "usage: nooma consolidate [vault]\n") }
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 1 {
		return fmt.Errorf("consolidate takes at most one vault path, got %d", fs.NArg())
	}

	vault, err := config.ResolveVault(fs.Arg(0))
	if err != nil {
		return err
	}
	cfg, err := loadVaultConfig(vault)
	if err != nil {
		return err
	}

	lock, err := vaultlock.Acquire(vault)
	if err != nil {
		var inUse *vaultlock.InUseError
		if errors.As(err, &inUse) {
			return err
		}
		return fmt.Errorf("taking the vault lock: %w", err)
	}
	defer func() { _ = lock.Release() }()

	dbPath, err := cfg.DatabasePath(vault)
	if err != nil {
		return err
	}
	db, err := sqlite.Open(context.Background(), dbPath)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	svc, err := wireConsolidate(context.Background(), db, cfg, os.LookupEnv)
	if err != nil {
		return err
	}

	if _, err := svc.Consolidate(context.Background(), brain.ConsolidateRequest{}); err != nil {
		return err
	}

	_, err = fmt.Fprintln(out, "consolidate: ran the whole pass")
	return err
}
