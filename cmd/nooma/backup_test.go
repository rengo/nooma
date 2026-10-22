package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/config"
	"github.com/rengo/nooma/internal/core/unit"
	"github.com/rengo/nooma/internal/store/sqlite"
	"github.com/rengo/nooma/internal/store/sqlite/sqlitetest"
	"github.com/rengo/nooma/internal/store/vaultlock"
)

// heldVault is a migrated vault held the way `nooma serve` holds one: the
// write lock taken and a connection open.
func heldVault(t *testing.T) (vault string, db *sqlite.Vault) {
	t.Helper()
	vault = writeVault(t, "")
	lock, err := vaultlock.Acquire(vault)
	if err != nil {
		t.Fatalf("vaultlock.Acquire: %v", err)
	}
	t.Cleanup(func() { _ = lock.Release() })

	cfg, err := loadVaultConfig(vault)
	if err != nil {
		t.Fatalf("loadVaultConfig: %v", err)
	}
	dbPath, err := cfg.DatabasePath(vault)
	if err != nil {
		t.Fatalf("DatabasePath: %v", err)
	}
	db, err = sqlite.Open(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("sqlite.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return vault, db
}

func createUnit(ctx context.Context, repo *sqlite.UnitRepo, id string) error {
	at := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	return repo.Create(ctx, unit.Unit{
		ID: id, Type: unit.TypeKnowledge, Status: unit.StatusPool, Content: "unit " + id, Source: "cli",
		Weight: 0.5, WeightDecayRate: 0.01, CreatedAt: at, UpdatedAt: at, LastTouchedAt: at,
	})
}

// TestBackup_UnderARunningWriterRestoresToAnOpenableVaultWithTheSameUnits is
// the command's reason to exist: while another process holds the vault and
// keeps writing, `nooma backup` produces a snapshot that, put in place as a
// vault's database beside the backed-up nooma.yml, opens and holds every unit
// committed before the backup began.
func TestBackup_UnderARunningWriterRestoresToAnOpenableVaultWithTheSameUnits(t *testing.T) {
	ctx := context.Background()
	vault, db := heldVault(t)
	units := sqlite.NewUnitRepo(db)

	var before []string
	for i := range 20 {
		id := fmt.Sprintf("before-%02d", i)
		if err := createUnit(ctx, units, id); err != nil {
			t.Fatalf("seeding %s: %v", id, err)
		}
		before = append(before, id)
	}

	// The writer keeps committing for the whole backup, and has committed
	// at least once before it starts.
	stop, started := make(chan struct{}), make(chan struct{})
	var wg sync.WaitGroup
	var writerErr error
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; ; i++ {
			if i == 1 {
				close(started)
			}
			select {
			case <-stop:
				return
			default:
			}
			if err := createUnit(ctx, units, fmt.Sprintf("during-%04d", i)); err != nil {
				writerErr = err
				if i == 0 {
					close(started)
				}
				return
			}
		}
	}()
	<-started

	dest := filepath.Join(t.TempDir(), "backups")
	var out, errOut bytes.Buffer
	err := run([]string{"backup", vault, dest, "--keep", "3"}, &out, &errOut)
	close(stop)
	wg.Wait()
	if err != nil {
		t.Fatalf("nooma backup: %v\nstderr: %s", err, errOut.String())
	}
	if writerErr != nil {
		t.Fatalf("the writer failed during the backup: %v", writerErr)
	}

	dbs := backupNames(t, dest, ".db")
	if len(dbs) != 1 {
		t.Fatalf("dest holds %v, want exactly one nooma-*.db", dbs)
	}
	base := strings.TrimSuffix(dbs[0], ".db")
	if !strings.Contains(out.String(), dbs[0]) {
		t.Errorf("output %q does not name the snapshot %s", out.String(), dbs[0])
	}

	// Restore as docs/08-deploy.md says: the snapshot becomes nooma.db, the
	// backed-up config becomes nooma.yml.
	restored := t.TempDir()
	copyFile(t, filepath.Join(dest, base+".db"), filepath.Join(restored, "nooma.db"))
	copyFile(t, filepath.Join(dest, base+".yml"), filepath.Join(restored, config.ConfigFileName))

	cfg, err := loadVaultConfig(restored)
	if err != nil {
		t.Fatalf("the restored vault's config does not load: %v", err)
	}
	dbPath, err := cfg.DatabasePath(restored)
	if err != nil {
		t.Fatal(err)
	}
	rdb, err := sqlite.Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("the restored vault does not open: %v", err)
	}
	defer func() { _ = rdb.Close() }()
	if err := rdb.IntegrityCheck(ctx); err != nil {
		t.Fatalf("restored vault: %v", err)
	}
	rUnits := sqlite.NewUnitRepo(rdb)
	for _, id := range before {
		if _, err := rUnits.ByID(ctx, id); err != nil {
			t.Errorf("unit %s committed before the backup is missing from the restore: %v", id, err)
		}
	}
}

// TestBackup_KeepPrunesOnlyOlderBackupsThisCommandMade: --keep counts and
// deletes only files named the way this command names its output. A file
// that merely resembles one, and anything else in the directory, is left.
func TestBackup_KeepPrunesOnlyOlderBackupsThisCommandMade(t *testing.T) {
	vault, _ := heldVault(t)
	dest := t.TempDir()
	old := []string{"nooma-20260101-000000", "nooma-20260102-000000", "nooma-20260103-000000"}
	for _, b := range old {
		writeFile(t, filepath.Join(dest, b+".db"), "old")
		writeFile(t, filepath.Join(dest, b+".yml"), "old")
	}
	strangers := []string{"notes.txt", "nooma-latest.db", "nooma-2026.db", "nooma-20260101-000000.db.bak", "other-20260101-000000.db"}
	for _, s := range strangers {
		writeFile(t, filepath.Join(dest, s), "not ours")
	}

	now := time.Date(2026, 10, 22, 3, 30, 0, 0, time.UTC)
	if _, err := backupVault(context.Background(), vault, dest, 2, now, sqlite.VerifySnapshot); err != nil {
		t.Fatalf("backupVault: %v", err)
	}

	want := []string{"nooma-20260103-000000", "nooma-20261022-033000"}
	for _, ext := range []string{".db", ".yml"} {
		got := backupNames(t, dest, ext)
		var bases []string
		for _, g := range got {
			bases = append(bases, strings.TrimSuffix(g, ext))
		}
		if strings.Join(bases, ",") != strings.Join(want, ",") {
			t.Errorf("%s backups kept = %v, want %v", ext, bases, want)
		}
	}
	for _, s := range strangers {
		if _, err := os.Stat(filepath.Join(dest, s)); err != nil {
			t.Errorf("--keep touched %s, a file it did not make: %v", s, err)
		}
	}
}

// TestBackup_AFailedVerificationExitsWithAnErrorAndLeavesNoBackup: a snapshot
// that fails integrity_check is an error, and no file that looks like a good
// backup — nor a partial one — is left behind for the upload to carry.
func TestBackup_AFailedVerificationExitsWithAnErrorAndLeavesNoBackup(t *testing.T) {
	vault, _ := heldVault(t)
	dest := t.TempDir()
	writeFile(t, filepath.Join(dest, "nooma-20260101-000000.db"), "old")

	broken := func(context.Context, string) (int, error) {
		return 0, errors.New("integrity_check reported: page 3 is never used")
	}
	_, err := backupVault(context.Background(), vault, dest, 1, time.Now(), broken)
	if err == nil || !strings.Contains(err.Error(), "page 3") {
		t.Fatalf("backupVault with a failing verification = %v, want the verification error", err)
	}

	entries, _ := os.ReadDir(dest)
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	if strings.Join(names, ",") != "nooma-20260101-000000.db" {
		t.Errorf("dest holds %v after a failed backup, want only the earlier backup, unpruned", names)
	}
}

// TestBackup_Refusals: each refusal is an error naming its cause, and none
// writes a backup.
func TestBackup_Refusals(t *testing.T) {
	vault, _ := heldVault(t)
	fileDest := filepath.Join(t.TempDir(), "a-file")
	writeFile(t, fileDest, "x")

	withAttachment := writeVault(t, "")
	if err := os.MkdirAll(filepath.Join(withAttachment, "attachments"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(withAttachment, "attachments", "photo.jpg"), "jpeg")

	cases := []struct {
		name string
		args []string
		want string
	}{
		{"no arguments", []string{"backup"}, "usage"},
		{"one argument", []string{"backup", vault}, "usage"},
		{"three arguments", []string{"backup", vault, t.TempDir(), "extra"}, "usage"},
		{"negative keep", []string{"backup", vault, t.TempDir(), "--keep", "-1"}, "--keep"},
		{"not a vault", []string{"backup", t.TempDir(), t.TempDir()}, "not a vault"},
		{"dest is a file", []string{"backup", vault, fileDest}, fileDest},
		{"attachments the backup cannot carry", []string{"backup", withAttachment, t.TempDir()}, "attachments"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var out, errOut bytes.Buffer
			err := run(tc.args, &out, &errOut)
			if err == nil {
				t.Fatalf("run(%v) = nil, want an error", tc.args)
			}
			if msg := err.Error() + errOut.String(); !strings.Contains(msg, tc.want) {
				t.Errorf("run(%v) = %q, want it to mention %q", tc.args, msg, tc.want)
			}
		})
	}
}

// TestBackup_RefusesToReplaceABackupWithTheSameName: two backups in one second
// do not overwrite each other.
func TestBackup_RefusesToReplaceABackupWithTheSameName(t *testing.T) {
	vault, _ := heldVault(t)
	dest := t.TempDir()
	now := time.Date(2026, 10, 22, 3, 30, 0, 0, time.UTC)
	writeFile(t, filepath.Join(dest, "nooma-20261022-033000.db"), "earlier")

	if _, err := backupVault(context.Background(), vault, dest, 0, now, sqlite.VerifySnapshot); err == nil {
		t.Fatal("backupVault over an existing backup name = nil, want an error")
	}
	if b, _ := os.ReadFile(filepath.Join(dest, "nooma-20261022-033000.db")); string(b) != "earlier" {
		t.Errorf("the earlier backup was replaced: %q", b)
	}
}

// TestBackup_LeavesAnOlderVaultAtItsSchemaVersion: a backup binary newer
// than the vault reads it and never migrates it; the live vault, not holding
// the lock for this command, keeps the version its serve expects.
func TestBackup_LeavesAnOlderVaultAtItsSchemaVersion(t *testing.T) {
	ctx := context.Background()
	vault := writeVault(t, "")
	dbPath := filepath.Join(vault, "nooma.db")
	db, err := sqlite.Open(ctx, dbPath)
	if err != nil {
		t.Fatal(err)
	}
	current, err := db.SchemaVersion(ctx)
	if err != nil {
		t.Fatal(err)
	}
	older := current - 1
	sqlitetest.SetUserVersion(t, db, older)
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	var out, errOut bytes.Buffer
	if err := run([]string{"backup", vault, t.TempDir()}, &out, &errOut); err != nil {
		t.Fatalf("nooma backup: %v", err)
	}
	got, err := sqlite.VerifySnapshot(ctx, dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if got != older {
		t.Errorf("the vault is at user_version %d after a backup, want its own %d: the backup migrated it", got, older)
	}
}

// TestBackup_RefusesADestinationInsideTheVault: backups, and the pruning that
// deletes them, never live in the vault folder.
func TestBackup_RefusesADestinationInsideTheVault(t *testing.T) {
	vault, _ := heldVault(t)
	for _, dest := range []string{vault, filepath.Join(vault, "backups"), filepath.Join(vault, ".")} {
		var out, errOut bytes.Buffer
		err := run([]string{"backup", vault, dest}, &out, &errOut)
		if err == nil || !strings.Contains(err.Error(), "inside the vault") {
			t.Errorf("backup into %s = %v, want a refusal naming the vault", dest, err)
		}
	}
	if _, err := os.Stat(filepath.Join(vault, "backups")); err == nil {
		t.Error("the refused destination was created inside the vault")
	}
	if names := backupNames(t, vault, ""); len(names) != 0 {
		t.Errorf("the vault holds backups after a refusal: %v", names)
	}
}

// TestBackup_KeepRemovesOnlyRegularFiles: a directory or symlink that happens
// to carry a backup's name is neither counted nor removed.
func TestBackup_KeepRemovesOnlyRegularFiles(t *testing.T) {
	vault, _ := heldVault(t)
	dest := t.TempDir()
	dirNamed := filepath.Join(dest, "nooma-20200101-000000.db")
	if err := os.Mkdir(dirNamed, 0o700); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "elsewhere")
	writeFile(t, target, "not a backup")
	linkNamed := filepath.Join(dest, "nooma-20200102-000000.db")
	if err := os.Symlink(target, linkNamed); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	now := time.Date(2026, 10, 22, 3, 30, 0, 0, time.UTC)
	if _, err := backupVault(context.Background(), vault, dest, 1, now, sqlite.VerifySnapshot); err != nil {
		t.Fatalf("backupVault: %v", err)
	}
	for _, p := range []string{dirNamed, linkNamed} {
		if _, err := os.Lstat(p); err != nil {
			t.Errorf("--keep removed %s, which is not a regular file: %v", filepath.Base(p), err)
		}
	}
}

func backupNames(t *testing.T, dir, ext string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		if backupName.MatchString(e.Name()) && strings.HasSuffix(e.Name(), ext) {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	return names
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func copyFile(t *testing.T, from, to string) {
	t.Helper()
	b, err := os.ReadFile(from)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(to, b, 0o600); err != nil {
		t.Fatal(err)
	}
}
