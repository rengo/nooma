//go:build integration

package sqlite

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/core/unit"
)

// TestSnapshot_WritesAVerifiableCopyAtTheSameSchema: VACUUM INTO produces a
// file VerifySnapshot accepts, at the schema version the source carries.
func TestSnapshot_WritesAVerifiableCopyAtTheSameSchema(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	v, err := Open(ctx, filepath.Join(dir, "nooma.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = v.Close() }()
	want, err := v.SchemaVersion(ctx)
	if err != nil {
		t.Fatalf("SchemaVersion: %v", err)
	}

	dest := filepath.Join(dir, "snap.db")
	if err := Snapshot(ctx, v.Path(), dest); err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	got, err := VerifySnapshot(ctx, dest)
	if err != nil {
		t.Fatalf("VerifySnapshot: %v", err)
	}
	if got != want {
		t.Errorf("snapshot schema = %d, want the source's %d", got, want)
	}
	// Verification reads the file and leaves nothing beside it: a -wal or
	// -shm next to a backup would be uploaded as if it were one.
	for _, suffix := range []string{"-wal", "-shm"} {
		if _, err := os.Stat(dest + suffix); err == nil {
			t.Errorf("VerifySnapshot left %s beside the snapshot", filepath.Base(dest+suffix))
		}
	}
}

// TestSnapshot_RefusesAnExistingDestination: VACUUM INTO never overwrites,
// so a backup cannot silently replace an earlier one.
func TestSnapshot_RefusesAnExistingDestination(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	v, err := Open(ctx, filepath.Join(dir, "nooma.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = v.Close() }()

	dest := filepath.Join(dir, "taken.db")
	if err := os.WriteFile(dest, []byte("an earlier backup"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Snapshot(ctx, v.Path(), dest); err == nil {
		t.Fatal("Snapshot over an existing file returned nil, want an error")
	}
	if b, _ := os.ReadFile(dest); string(b) != "an earlier backup" {
		t.Errorf("the existing file was modified: %q", b)
	}
}

// TestVerifySnapshot_RejectsAFileThatIsNotAVault: a corrupt or foreign file
// fails verification, naming the path.
func TestVerifySnapshot_RejectsAFileThatIsNotAVault(t *testing.T) {
	path := filepath.Join(t.TempDir(), "garbage.db")
	if err := os.WriteFile(path, []byte(strings.Repeat("not sqlite ", 1000)), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := VerifySnapshot(context.Background(), path)
	if err == nil {
		t.Fatal("VerifySnapshot(garbage) = nil error, want one")
	}
	if !strings.Contains(err.Error(), path) {
		t.Errorf("error %q does not name %s", err, path)
	}
}

// TestVerifySnapshot_RejectsAMissingFile: a read-only open must not create
// the file it was asked to verify.
func TestVerifySnapshot_RejectsAMissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "absent.db")
	if _, err := VerifySnapshot(context.Background(), path); err == nil {
		t.Fatal("VerifySnapshot(absent) = nil error, want one")
	}
	if _, err := os.Stat(path); err == nil {
		t.Error("VerifySnapshot created the file it was asked to verify")
	}
}

// TestSnapshot_NeverMigratesTheVaultItReads: a backup binary newer than the
// vault must not apply its migrations to the live file. Snapshot reads
// through a read-only connection, so a vault one version behind stays there.
func TestSnapshot_NeverMigratesTheVaultItReads(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	path := filepath.Join(dir, "nooma.db")
	v, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	current, err := v.SchemaVersion(ctx)
	if err != nil {
		t.Fatal(err)
	}
	older := current - 1
	if _, err := v.db.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version = %d", older)); err != nil {
		t.Fatal(err)
	}
	if err := v.Close(); err != nil {
		t.Fatal(err)
	}

	dest := filepath.Join(dir, "snap.db")
	if err := Snapshot(ctx, path, dest); err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	for _, p := range []string{path, dest} {
		got, err := VerifySnapshot(ctx, p)
		if err != nil {
			t.Fatalf("VerifySnapshot(%s): %v", filepath.Base(p), err)
		}
		if got != older {
			t.Errorf("%s is at user_version %d after the snapshot, want the vault's own %d", filepath.Base(p), got, older)
		}
	}
}

// TestSnapshot_UnderACommittingWriterIsConsistent: VACUUM INTO is one read
// transaction, so a snapshot taken while another connection keeps committing
// verifies, and holds every row committed before it began.
func TestSnapshot_UnderACommittingWriterIsConsistent(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	v, err := Open(ctx, filepath.Join(dir, "nooma.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = v.Close() }()
	units := NewUnitRepo(v)
	at := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	create := func(id string) error {
		return units.Create(ctx, unit.Unit{ID: id, Type: unit.TypeKnowledge, Status: unit.StatusPool, Content: id,
			Source: "cli", Weight: 0.5, WeightDecayRate: 0.01, CreatedAt: at, UpdatedAt: at, LastTouchedAt: at})
	}
	if err := create("before"); err != nil {
		t.Fatal(err)
	}

	stop, done := make(chan struct{}), make(chan error, 1)
	committed := make(chan struct{})
	go func() {
		for i := 0; ; i++ {
			select {
			case <-stop:
				done <- nil
				return
			default:
			}
			if err := create(fmt.Sprintf("during-%04d", i)); err != nil {
				done <- err
				return
			}
			if i == 0 {
				close(committed)
			}
		}
	}()
	<-committed

	dest := filepath.Join(dir, "snap.db")
	snapErr := Snapshot(ctx, v.Path(), dest)
	close(stop)
	if err := <-done; err != nil {
		t.Fatalf("the writer failed during the snapshot: %v", err)
	}
	if snapErr != nil {
		t.Fatalf("Snapshot: %v", snapErr)
	}
	if _, err := VerifySnapshot(ctx, dest); err != nil {
		t.Fatalf("VerifySnapshot: %v", err)
	}
	restored, err := Open(ctx, dest)
	if err != nil {
		t.Fatalf("Open(snapshot): %v", err)
	}
	defer func() { _ = restored.Close() }()
	for _, id := range []string{"before", "during-0000"} {
		if _, err := NewUnitRepo(restored).ByID(ctx, id); err != nil {
			t.Errorf("unit %s, committed before the snapshot, is missing: %v", id, err)
		}
	}
}
