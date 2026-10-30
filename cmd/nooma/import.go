package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/rengo/nooma/internal/bundle"
	"github.com/rengo/nooma/internal/config"
	"github.com/rengo/nooma/internal/store/sqlite"
)

func init() {
	commands["import"] = command{
		summary: "make a new vault from a .noomabundle, with Telegram off",
		run:     runImport,
	}
}

// importSpaceMargin is free space import keeps beyond the bundle's declared
// size: migrating and rebuilding the word index write a little more than the
// files themselves.
const (
	importSpaceMarginFloor = 16 << 20 // bytes
	importSpaceMarginShare = 10       // percent of the declared size
)

// imported is what an import made.
type imported struct {
	target     string
	files      int
	fromSchema int
	toSchema   int
	telegram   config.TelegramEdit
}

// runImport is `nooma import <file.noomabundle> <vault-dir>` (ADR-0057).
func runImport(args []string, out, errOut io.Writer) error {
	fs := flag.NewFlagSet("import", flag.ContinueOnError)
	fs.SetOutput(errOut)
	fs.Usage = func() {
		_, _ = fmt.Fprint(errOut, "usage: nooma import <file.noomabundle> <vault-dir>\n")
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 2 {
		fs.Usage()
		return fmt.Errorf("import takes a bundle file and a vault directory, got %d arguments (see usage)", fs.NArg())
	}
	_, err := importVault(context.Background(), fs.Arg(0), fs.Arg(1), out, freeSpace)
	return err
}

// importVault makes a new vault at target from the bundle at bundlePath
// (ADR-0057): copy only, never into a vault that exists.
//
// Everything that can refuse refuses before anything is written: a target
// that exists and is not empty, a bundle Verify rejects (a newer bundle
// format among them), a schema newer than this binary's, and too little free
// space. Then the bundle unpacks into a working folder beside target, each
// file checked again as it is written; the database must pass
// integrity_check at the schema the manifest states, is migrated forward by
// sqlite.Open, and has its word index rebuilt (doc 03: the snapshot came
// from a vacuum). nooma.yml gets Telegram turned off, and is decoded
// strictly. Only then is the folder renamed into place; any failure removes
// it, and no vault is left.
func importVault(ctx context.Context, bundlePath, target string, out io.Writer, free func(string) (uint64, bool)) (imported, error) {
	target, err := filepath.Abs(target)
	if err != nil {
		return imported{}, err
	}
	targetExists, err := freshTarget(target)
	if err != nil {
		return imported{}, err
	}
	m, err := bundle.Verify(bundlePath)
	if err != nil {
		return imported{}, err
	}
	schema, err := sqlite.SchemaTarget()
	if err != nil {
		return imported{}, err
	}
	if m.SchemaVersion > schema {
		return imported{}, fmt.Errorf("the bundle's database is at schema %d and this nooma knows up to %d: it was written by nooma %s, so import it with that version or newer (nothing was written)",
			m.SchemaVersion, schema, m.NoomaVersion)
	}

	parent := filepath.Dir(target)
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return imported{}, fmt.Errorf("creating %s: %w", parent, err)
	}
	if err := checkFreeSpace(m, parent, out, free); err != nil {
		return imported{}, err
	}

	work, err := os.MkdirTemp(parent, ".nooma-import-*")
	if err != nil {
		return imported{}, fmt.Errorf("preparing the import: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = os.RemoveAll(work)
		}
	}()

	res, err := assembleVault(ctx, bundlePath, m, work)
	if err != nil {
		return imported{}, err
	}
	res.toSchema = schema

	if targetExists {
		// Empty, as freshTarget checked; a rename cannot land on a directory
		// on every platform.
		if err := os.Remove(target); err != nil {
			return imported{}, fmt.Errorf("replacing the empty directory %s: %w", target, err)
		}
	}
	if err := os.Rename(work, target); err != nil {
		return imported{}, fmt.Errorf("moving the vault into %s: %w", target, err)
	}
	committed = true
	res.target = target
	reportImport(out, res)
	return res, nil
}

// freshTarget refuses a target import must not write into, and reports
// whether it exists (an empty directory).
func freshTarget(target string) (bool, error) {
	info, err := os.Lstat(target)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !info.IsDir() {
		return false, fmt.Errorf("%s exists and is not a directory; import makes a new vault", target)
	}
	entries, err := os.ReadDir(target)
	if err != nil {
		return false, err
	}
	if len(entries) > 0 {
		return false, fmt.Errorf("%s exists and is not empty; import never writes over a vault, so choose a new directory (or move this one aside)", target)
	}
	return true, nil
}

// checkFreeSpace refuses when the bundle's files and a margin do not fit in
// dir's filesystem. A platform that cannot say is warned about, not refused.
func checkFreeSpace(m bundle.Manifest, dir string, out io.Writer, free func(string) (uint64, bool)) error {
	var declared uint64
	for _, f := range m.Files {
		declared += uint64(f.Size) //nolint:gosec // Verify refuses a negative size
	}
	need := declared + max(importSpaceMarginFloor, declared*importSpaceMarginShare/100)
	have, ok := free(dir)
	if !ok {
		_, _ = fmt.Fprintf(out, "warning: could not read the free space in %s; importing %s anyway\n", dir, humanSize(int64(need))) //nolint:gosec // a bundle's size fits in an int64
		return nil
	}
	if have < need {
		return fmt.Errorf("the vault needs about %s free in %s and %s is free; nothing was written",
			humanSize(int64(need)), dir, humanSize(int64(have))) //nolint:gosec // as above
	}
	return nil
}

// assembleVault builds the vault in work.
func assembleVault(ctx context.Context, bundlePath string, m bundle.Manifest, work string) (imported, error) {
	if err := bundle.Extract(bundlePath, m, work); err != nil {
		return imported{}, err
	}
	for _, sub := range vaultDirs {
		if err := os.MkdirAll(filepath.Join(work, sub), 0o700); err != nil {
			return imported{}, fmt.Errorf("creating %s: %w", sub, err)
		}
	}

	ymlPath := filepath.Join(work, config.ConfigFileName)
	doc, err := os.ReadFile(ymlPath)
	if err != nil {
		return imported{}, err
	}
	turnedOff, how, err := config.DisableTelegram(doc)
	if err != nil {
		return imported{}, fmt.Errorf("the bundle's %w", err)
	}
	if how != config.TelegramAlreadyOff {
		if err := os.WriteFile(ymlPath, turnedOff, 0o600); err != nil {
			return imported{}, err
		}
	}
	cfg, err := config.Decode(bytes.NewReader(turnedOff))
	if err != nil {
		return imported{}, fmt.Errorf("the bundle's %w", err)
	}
	cfg.ApplyDefaults()
	dbPath, err := cfg.DatabasePath(work)
	if err != nil {
		return imported{}, fmt.Errorf("the bundle's nooma.yml: %w", err)
	}

	unpacked := filepath.Join(work, "nooma.db")
	version, err := sqlite.VerifySnapshot(ctx, unpacked)
	if err != nil {
		return imported{}, err
	}
	if version != m.SchemaVersion {
		return imported{}, fmt.Errorf("the bundle's nooma.db is at schema %d and its manifest says %d; refusing a bundle that disagrees with itself", version, m.SchemaVersion)
	}
	if dbPath != unpacked {
		if err := os.MkdirAll(filepath.Dir(dbPath), 0o700); err != nil {
			return imported{}, err
		}
		if err := os.Rename(unpacked, dbPath); err != nil {
			return imported{}, fmt.Errorf("placing the database where nooma.yml says: %w", err)
		}
	}

	db, err := sqlite.Open(ctx, dbPath)
	if err != nil {
		return imported{}, err
	}
	rebuildErr := db.RebuildSearchIndex(ctx)
	if err := errors.Join(rebuildErr, db.Close()); err != nil {
		return imported{}, err
	}
	return imported{files: len(m.Files), fromSchema: m.SchemaVersion, telegram: how}, nil
}

// reportImport says what was made, and why Telegram is off when import
// turned it off.
func reportImport(out io.Writer, r imported) {
	schema := fmt.Sprintf("schema %d", r.toSchema)
	if r.fromSchema != r.toSchema {
		schema = fmt.Sprintf("schema %d → %d", r.fromSchema, r.toSchema)
	}
	_, _ = fmt.Fprintf(out, "import: %s (%d files, %s)\n", r.target, r.files, schema)
	switch r.telegram {
	case config.TelegramTurnedOff, config.TelegramRewritten:
		_, _ = fmt.Fprintln(out, "Telegram is off in the imported nooma.yml: two copies polling one bot token steal each other's messages. Turn it back on (channels.telegram.enabled: true) only in the copy that should answer, with the original stopped or its Telegram off.")
		if r.telegram == config.TelegramRewritten {
			_, _ = fmt.Fprintln(out, "nooma.yml was rewritten to do it, so its comments or layout may have moved.")
		}
	}
}
