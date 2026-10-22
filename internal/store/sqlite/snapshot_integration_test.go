//go:build integration

package sqlite

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSnapshotTo_WritesAVerifiableCopyAtTheSameSchema: VACUUM INTO produces a
// file VerifySnapshot accepts, at the schema version the source carries.
func TestSnapshotTo_WritesAVerifiableCopyAtTheSameSchema(t *testing.T) {
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
	if err := v.SnapshotTo(ctx, dest); err != nil {
		t.Fatalf("SnapshotTo: %v", err)
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

// TestSnapshotTo_RefusesAnExistingDestination: VACUUM INTO never overwrites,
// so a backup cannot silently replace an earlier one.
func TestSnapshotTo_RefusesAnExistingDestination(t *testing.T) {
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
	if err := v.SnapshotTo(ctx, dest); err == nil {
		t.Fatal("SnapshotTo over an existing file returned nil, want an error")
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
