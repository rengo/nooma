package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/rengo/nooma/internal/config"
	"github.com/rengo/nooma/internal/store/sqlite"
)

func init() {
	commands["backup"] = command{
		summary: "write a verified snapshot of a vault, even while it is served",
		run:     runBackup,
	}
}

// backupName is the only shape `nooma backup` writes, so the only one --keep
// may delete (ADR-0049); anything else in dest is not this command's.
var backupName = regexp.MustCompile(`^nooma-\d{8}-\d{6}\.(db|yml)$`)

// backupStamp names a backup by the UTC instant it was taken. UTC, so the
// names sort in time order across a daylight-saving change.
const backupStamp = "20060102-150405"

// runBackup is `nooma backup <vault> <dest-dir> [--keep N]` (ADR-0049).
//
// It takes no vault lock: a backup reads on a read-only connection
// (sqlite.Snapshot), so it never migrates or writes the vault.
func runBackup(args []string, out, errOut io.Writer) error {
	fs := flag.NewFlagSet("backup", flag.ContinueOnError)
	fs.SetOutput(errOut)
	keep := fs.Int("keep", 0, "after a good backup, delete all but the newest N backups in dest-dir (0 keeps all)")
	fs.Usage = func() {
		_, _ = fmt.Fprint(errOut, "usage: nooma backup <vault> <dest-dir> [--keep N]\n")
		fs.PrintDefaults()
	}

	// flag stops at the first positional and the documented form puts --keep
	// last, so parse the remainder again after taking each positional.
	var positional []string
	for {
		if err := fs.Parse(args); err != nil {
			return err
		}
		if fs.NArg() == 0 {
			break
		}
		positional = append(positional, fs.Arg(0))
		args = fs.Args()[1:]
	}
	if len(positional) != 2 {
		fs.Usage()
		return fmt.Errorf("backup takes a vault and a destination directory, got %d arguments (see usage)", len(positional))
	}
	if *keep < 0 {
		return fmt.Errorf("--keep must be 0 or more, got %d", *keep)
	}

	path, err := backupVault(context.Background(), positional[0], positional[1], *keep, time.Now(), sqlite.VerifySnapshot)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(out, "backup: %s\n", path)
	return err
}

// backupVault writes nooma-<stamp>.db (a VACUUM INTO snapshot, verified) and
// nooma-<stamp>.yml (the vault's configuration) into dest, then prunes.
//
// A restore needs the database and nooma.yml (docs/01-architecture.md
// §"Vault structure"). .env is secrets, derived/ is recomputable, logs/ and
// the lock are not state. attachments/ is needed and nothing writes it yet, so
// a non-empty one is refused rather than silently left out.
//
// Both files are written as .partial and renamed only after verification,
// the database last: a nooma-*.db in dest is always a complete, checked
// backup. Pruning runs only after success.
func backupVault(ctx context.Context, vaultArg, dest string, keep int, now time.Time,
	verify func(context.Context, string) (int, error)) (string, error) {
	vault, err := config.ResolveVault(vaultArg)
	if err != nil {
		return "", err
	}
	cfg, err := loadVaultConfig(vault)
	if err != nil {
		return "", err
	}
	if err := refuseAttachments(vault); err != nil {
		return "", err
	}
	dbPath, err := cfg.DatabasePath(vault)
	if err != nil {
		return "", err
	}

	if dest, err = filepath.Abs(dest); err != nil {
		return "", err
	}
	// Backups, and the --keep that deletes them, stay out of the vault
	// folder: nothing this command removes may ever sit inside a vault.
	if insideVault(vault, dest) {
		return "", fmt.Errorf("%s is inside the vault %s; write backups somewhere else", dest, vault)
	}
	if err := os.MkdirAll(dest, 0o700); err != nil {
		return "", fmt.Errorf("creating the backup directory %s: %w", dest, err)
	}

	base := filepath.Join(dest, "nooma-"+now.UTC().Format(backupStamp))
	finalDB, finalYML := base+".db", base+".yml"
	for _, p := range []string{finalDB, finalYML} {
		if _, err := os.Lstat(p); err == nil {
			return "", fmt.Errorf("%s already exists; refusing to replace a backup", p)
		}
	}
	partialDB, partialYML := finalDB+".partial", finalYML+".partial"
	committed := false
	defer func() {
		if !committed {
			_ = os.Remove(partialDB)
			_ = os.Remove(partialYML)
		}
	}()

	if err := snapshot(ctx, dbPath, partialDB); err != nil {
		return "", err
	}
	if _, err := verify(ctx, partialDB); err != nil {
		return "", err
	}
	if err := copyConfig(filepath.Join(vault, config.ConfigFileName), partialYML); err != nil {
		return "", err
	}
	if err := os.Rename(partialYML, finalYML); err != nil {
		return "", fmt.Errorf("finishing %s: %w", finalYML, err)
	}
	if err := os.Rename(partialDB, finalDB); err != nil {
		_ = os.Remove(finalYML)
		return "", fmt.Errorf("finishing %s: %w", finalDB, err)
	}
	committed = true

	if keep > 0 {
		if err := pruneBackups(dest, keep); err != nil {
			return "", err
		}
	}
	return finalDB, nil
}

// insideVault reports whether p is the vault folder or lies within it.
func insideVault(vault, p string) bool {
	rel, err := filepath.Rel(vault, p)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func refuseAttachments(vault string) error {
	entries, err := os.ReadDir(filepath.Join(vault, "attachments"))
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("reading the vault's attachments: %w", err)
	}
	if len(entries) > 0 {
		return fmt.Errorf("%s holds attachments, which nooma backup does not carry yet; refusing to write a backup that would leave them out",
			filepath.Join(vault, "attachments"))
	}
	return nil
}

func snapshot(ctx context.Context, dbPath, dest string) error {
	if err := sqlite.Snapshot(ctx, dbPath, dest); err != nil {
		return err
	}
	// The vault holds personal data; its copies are owner-only.
	return os.Chmod(dest, 0o600)
}

func copyConfig(from, to string) error {
	b, err := os.ReadFile(from)
	if err != nil {
		return fmt.Errorf("reading %s: %w", from, err)
	}
	if err := os.WriteFile(to, b, 0o600); err != nil {
		return fmt.Errorf("writing %s: %w", to, err)
	}
	return nil
}

// pruneBackups deletes all but the newest keep backups in dest. A backup is
// a nooma-<stamp> pair; only names backupName matches are counted or removed.
// These are copies this command made, never vault data, so non-negotiable 6
// ("nothing is deleted in the vault") is not in play.
func pruneBackups(dest string, keep int) error {
	entries, err := os.ReadDir(dest)
	if err != nil {
		return fmt.Errorf("listing %s: %w", dest, err)
	}
	stamps := map[string]bool{}
	for _, e := range entries {
		if e.Type().IsRegular() && backupName.MatchString(e.Name()) {
			stamps[strings.TrimSuffix(strings.TrimSuffix(e.Name(), ".db"), ".yml")] = true
		}
	}
	sorted := make([]string, 0, len(stamps))
	for s := range stamps {
		sorted = append(sorted, s)
	}
	sort.Sort(sort.Reverse(sort.StringSlice(sorted)))

	var errs []error
	for _, s := range sorted[min(keep, len(sorted)):] {
		for _, ext := range []string{".db", ".yml"} {
			if err := os.Remove(filepath.Join(dest, s+ext)); err != nil && !os.IsNotExist(err) {
				errs = append(errs, err)
			}
		}
	}
	return errors.Join(errs...)
}
