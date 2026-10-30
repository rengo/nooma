package main

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/rengo/nooma/internal/bundle"
	"github.com/rengo/nooma/internal/store/sqlite"
	"github.com/rengo/nooma/internal/store/sqlite/sqlitetest"
	"github.com/rengo/nooma/internal/store/vaultlock"
)

// exportFixture is a vault holding two embedded units and one not, an
// attachment, and everything a bundle must leave out: .env, derived/, logs/
// and the lock.
func exportFixture(t *testing.T) (vault, dbPath string) {
	t.Helper()
	vault, _, dbPath = doctorVault(t, func(db *sqlite.Vault) {
		seedUnits(t, db, map[string]string{"u-a": "model-a", "u-b": "model-b"}, "u-a", "u-b", "u-bare")
	})
	for path, content := range map[string]string{
		"attachments/2026/photo.txt": "a picture, as text",
		".env":                       "OPENAI_API_KEY=sk-secret\n",
		"derived/cache.bin":          "recomputable",
		"logs/nooma.log":             "a log line",
	} {
		full := filepath.Join(vault, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(full), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return vault, dbPath
}

// zipEntries reads every entry of the zip at path.
func zipEntries(t *testing.T, path string) map[string][]byte {
	t.Helper()
	z, err := zip.OpenReader(path)
	if err != nil {
		t.Fatalf("opening %s as a zip: %v", path, err)
	}
	defer func() { _ = z.Close() }()
	out := map[string][]byte{}
	for _, e := range z.File {
		rc, err := e.Open()
		if err != nil {
			t.Fatal(err)
		}
		b, err := io.ReadAll(rc)
		_ = rc.Close()
		if err != nil {
			t.Fatal(err)
		}
		out[e.Name] = b
	}
	return out
}

// TestI54_AnExportRoundTripsAtTheFileLevel: the bundle holds exactly the
// database snapshot, nooma.yml as it is and the attachments, each listed in
// the manifest with its size and sha256, which re-reading the zip confirms;
// the snapshot opens at the schema the manifest states, with the embedding
// models it counts. Nothing else of the vault goes in.
func TestI54_AnExportRoundTripsAtTheFileLevel(t *testing.T) {
	vault, _ := exportFixture(t)
	dest := filepath.Join(t.TempDir(), "brain.noomabundle")

	var out, errOut bytes.Buffer
	if err := runExport([]string{vault, dest}, &out, &errOut); err != nil {
		t.Fatalf("export: %v\n%s", err, errOut.String())
	}
	if !strings.Contains(out.String(), dest) {
		t.Errorf("export output %q does not name the bundle", out.String())
	}
	if _, err := os.Stat(dest + ".partial"); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the .partial file is still there: %v", err)
	}
	if left := workLeftovers(t, dest); len(left) != 0 {
		t.Errorf("export left its working folder: %v", left)
	}

	entries := zipEntries(t, dest)
	names := make([]string, 0, len(entries))
	for n := range entries {
		names = append(names, n)
	}
	sort.Strings(names)
	want := []string{"attachments/2026/photo.txt", bundle.ManifestName, "nooma.db", "nooma.yml"}
	if strings.Join(names, ",") != strings.Join(want, ",") {
		t.Fatalf("bundle entries = %v, want %v", names, want)
	}

	m, err := bundle.Verify(dest)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	for _, f := range m.Files {
		h := sha256.Sum256(entries[f.Path])
		if f.SHA256 != hex.EncodeToString(h[:]) || f.Size != int64(len(entries[f.Path])) {
			t.Errorf("manifest %+v disagrees with the entry's own bytes", f)
		}
	}
	yml, _ := os.ReadFile(filepath.Join(vault, "nooma.yml"))
	if !bytes.Equal(entries["nooma.yml"], yml) {
		t.Error("nooma.yml in the bundle is not the vault's, byte for byte")
	}
	if string(entries["attachments/2026/photo.txt"]) != "a picture, as text" {
		t.Errorf("attachment = %q", entries["attachments/2026/photo.txt"])
	}

	target, _ := sqlite.SchemaTarget()
	if m.BundleFormat != 1 || m.SchemaVersion != target || m.NoomaVersion == "" || m.CreatedAt.Location().String() != "UTC" {
		t.Errorf("manifest header = %+v, want format 1, schema %d, a version and a UTC instant", m, target)
	}
	if len(m.EmbeddingModels) != 2 || m.EmbeddingModels[0] != (bundle.ModelRows{Model: "model-a", Rows: 1}) ||
		m.EmbeddingModels[1] != (bundle.ModelRows{Model: "model-b", Rows: 1}) {
		t.Errorf("embedding models = %+v, want model-a and model-b with one row each", m.EmbeddingModels)
	}

	db := filepath.Join(t.TempDir(), "nooma.db")
	if err := os.WriteFile(db, entries["nooma.db"], 0o600); err != nil {
		t.Fatal(err)
	}
	if version, err := sqlite.VerifySnapshot(context.Background(), db); err != nil || version != target {
		t.Errorf("the bundled database = schema %d, %v; want a sound snapshot at %d", version, err, target)
	}
}

// TestExport_NeverMigratesTheVault: a vault behind the binary is exported at
// its own schema and left there, as backup does.
func TestExport_NeverMigratesTheVault(t *testing.T) {
	target, _ := sqlite.SchemaTarget()
	vault, _, dbPath := doctorVault(t, func(db *sqlite.Vault) { sqlitetest.SetUserVersion(t, db, target-1) })
	before := fileSum(t, dbPath)
	dest := filepath.Join(t.TempDir(), "old.noomabundle")

	if err := runExport([]string{vault, dest}, io.Discard, io.Discard); err != nil {
		t.Fatal(err)
	}
	m, err := bundle.Verify(dest)
	if err != nil || m.SchemaVersion != target-1 {
		t.Errorf("manifest schema = %d, %v; want %d, the vault's own", m.SchemaVersion, err, target-1)
	}
	if fileSum(t, dbPath) != before {
		t.Error("export changed the vault's database file")
	}
}

// TestExport_RunsBesideAHeldLock: export takes no lock, so it runs while
// serve holds the vault.
func TestExport_RunsBesideAHeldLock(t *testing.T) {
	vault, _ := exportFixture(t)
	lock, err := vaultlock.Acquire(vault)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = lock.Release() }()
	if err := runExport([]string{vault, filepath.Join(t.TempDir(), "held.noomabundle")}, io.Discard, io.Discard); err != nil {
		t.Errorf("export beside a held lock: %v", err)
	}
}

// TestExport_RefusesAndLeavesNoFile: every refusal and every failure exits
// with an error and leaves neither the bundle nor its .partial.
func TestExport_RefusesAndLeavesNoFile(t *testing.T) {
	cases := map[string]struct {
		setup func(t *testing.T, vault string) string // returns the destination
		want  string
	}{
		"an endpoint with credentials in it": {func(t *testing.T, vault string) string {
			writeYML(t, vault, "providers:\n  local:\n    type: ollama\n    model: m\n    endpoint: http://me:hunter2@localhost:11434\n")
			return filepath.Join(t.TempDir(), "x.noomabundle")
		}, "credentials"},
		"an endpoint that does not parse": {func(t *testing.T, vault string) string {
			writeYML(t, vault, "providers:\n  local:\n    type: ollama\n    model: m\n    endpoint: http://host/%zz\n")
			return filepath.Join(t.TempDir(), "x.noomabundle")
		}, "does not parse as a URL"},
		"an endpoint with a query string": {func(t *testing.T, vault string) string {
			writeYML(t, vault, "providers:\n  cloud:\n    type: openai\n    model: m\n    endpoint: https://api.example.com/v1?key=sk-secret\n")
			return filepath.Join(t.TempDir(), "x.noomabundle")
		}, "query string"},
		// The decoder refuses a key the schema has no field for, so a literal
		// secret under one never reaches the bundle.
		"an unknown key holding a secret": {func(t *testing.T, vault string) string {
			writeYML(t, vault, "api_key: sk-secret\n")
			return filepath.Join(t.TempDir(), "x.noomabundle")
		}, "unknown field"},
		"a second YAML document": {func(t *testing.T, vault string) string {
			writeYML(t, vault, "server:\n  bind: 127.0.0.1\n---\napi_key: sk-secret\n")
			return filepath.Join(t.TempDir(), "x.noomabundle")
		}, "more than one YAML document"},
		// QA's two: the YAML library stops at an empty or comment-only
		// document as if the file ended there.
		"a document behind an empty one": {func(t *testing.T, vault string) string {
			writeYML(t, vault, "server:\n  bind: 127.0.0.1\n---\n---\napi_key: x\n")
			return filepath.Join(t.TempDir(), "x.noomabundle")
		}, "more than one YAML document"},
		"a document behind a comment-only one": {func(t *testing.T, vault string) string {
			writeYML(t, vault, "server:\n  bind: 127.0.0.1\n---\n# c\n---\napi_key: x\n")
			return filepath.Join(t.TempDir(), "x.noomabundle")
		}, "more than one YAML document"},
		"a name that is not a .noomabundle": {func(t *testing.T, vault string) string {
			return filepath.Join(t.TempDir(), "x.zip")
		}, ".noomabundle"},
		"a destination inside the vault": {func(t *testing.T, vault string) string {
			return filepath.Join(vault, "x.noomabundle")
		}, "inside the vault"},
		"a destination that exists": {func(t *testing.T, vault string) string {
			p := filepath.Join(t.TempDir(), "x.noomabundle")
			if err := os.WriteFile(p, []byte("older"), 0o600); err != nil {
				t.Fatal(err)
			}
			return p
		}, "already exists"},
		"a missing database": {func(t *testing.T, vault string) string {
			if err := os.Remove(filepath.Join(vault, "nooma.db")); err != nil {
				t.Fatal(err)
			}
			return filepath.Join(t.TempDir(), "x.noomabundle")
		}, "nooma.db"},
		"an attachment that is a link": {func(t *testing.T, vault string) string {
			if err := os.Symlink(filepath.Join(vault, ".env"), filepath.Join(vault, "attachments", "env-link")); err != nil {
				t.Skip("symlinks unavailable:", err)
			}
			return filepath.Join(t.TempDir(), "x.noomabundle")
		}, "env-link"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			vault, _ := exportFixture(t)
			dest := c.setup(t, vault)
			existed := fileExists(dest)
			err := runExport([]string{vault, dest}, io.Discard, io.Discard)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Errorf("export = %v, want a refusal naming %q", err, c.want)
			}
			if !existed && fileExists(dest) {
				t.Errorf("a failed export left %s", dest)
			}
			if fileExists(dest + ".partial") {
				t.Errorf("a failed export left %s.partial", dest)
			}
			if left := workLeftovers(t, dest); len(left) != 0 {
				t.Errorf("a failed export left its working folder: %v", left)
			}
		})
	}
}

// TestExport_AVerifyFailureLeavesNoFile: a bundle that does not re-read as
// written is never renamed into place.
func TestExport_AVerifyFailureLeavesNoFile(t *testing.T) {
	vault, _ := exportFixture(t)
	dest := filepath.Join(t.TempDir(), "x.noomabundle")
	failing := func(string) (bundle.Manifest, error) {
		return bundle.Manifest{}, errors.New("sha256 mismatch on nooma.db")
	}

	_, err := exportVault(context.Background(), vault, dest, doctorSeedAt, failing)
	if err == nil || !strings.Contains(err.Error(), "sha256 mismatch") {
		t.Errorf("exportVault = %v, want the verify failure", err)
	}
	if fileExists(dest) || fileExists(dest+".partial") || len(workLeftovers(t, dest)) != 0 {
		t.Error("a bundle that failed its verification, or its working folder, was left behind")
	}
}

// workLeftovers is any export working folder left beside dest.
func workLeftovers(t *testing.T, dest string) []string {
	t.Helper()
	left, err := filepath.Glob(filepath.Join(filepath.Dir(dest), ".nooma-export-*"))
	if err != nil {
		t.Fatal(err)
	}
	return left
}

func fileExists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

func writeYML(t *testing.T, vault, document string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(vault, "nooma.yml"), []byte(document), 0o644); err != nil {
		t.Fatal(err)
	}
}
