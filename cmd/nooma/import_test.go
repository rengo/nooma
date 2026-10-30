package main

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/rengo/nooma/internal/bundle"
	"github.com/rengo/nooma/internal/core/unit"
	"github.com/rengo/nooma/internal/ports"
	"github.com/rengo/nooma/internal/store/sqlite"
	"github.com/rengo/nooma/internal/store/sqlite/sqlitetest"
)

// telegramYML is a nooma.yml with the channel on and comments around it.
const telegramYML = "# the production brain\nchannels:\n  telegram:\n    enabled: true   # the real bot\n    bot_token_env: TELEGRAM_BOT_TOKEN\n    allowed_chat_ids: [123]\n"

// roundTripVault is a vault holding live and archived units, a relation,
// vectors under two models, an attachment and a nooma.yml with Telegram on.
func roundTripVault(t *testing.T) string {
	t.Helper()
	vault, _, _ := doctorVault(t, func(db *sqlite.Vault) {
		ctx := context.Background()
		seedUnits(t, db, map[string]string{"u-a": "model-a", "u-b": "model-b"}, "u-a", "u-b", "u-bare")
		old := unit.Unit{ID: "u-old", Type: unit.TypeKnowledge, Content: "an archived note", Status: unit.StatusArchived,
			Weight: 0.01, WeightDecayRate: 0.1, CreatedAt: doctorSeedAt, UpdatedAt: doctorSeedAt, LastTouchedAt: doctorSeedAt}
		if err := sqlite.NewUnitRepo(db).Create(ctx, old); err != nil {
			t.Fatal(err)
		}
		if err := sqlite.NewRelationRepo(db).Upsert(ctx, ports.Relation{ID: "r-1", FromUnitID: "u-a", ToUnitID: "u-b", Type: "same_topic",
			Strength: 0.4, Confidence: 0.6, CreatedBy: "consolidation", CreatedAt: doctorSeedAt}); err != nil {
			t.Fatal(err)
		}
	})
	writeYML(t, vault, telegramYML)
	if err := os.MkdirAll(filepath.Join(vault, "attachments", "2026"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(vault, "attachments", "2026", "photo.txt"), []byte("a picture"), 0o600); err != nil {
		t.Fatal(err)
	}
	return vault
}

func exportTo(t *testing.T, vault string) string {
	t.Helper()
	dest := filepath.Join(t.TempDir(), "brain.noomabundle")
	if err := runExport([]string{vault, dest}, io.Discard, io.Discard); err != nil {
		t.Fatalf("export: %v", err)
	}
	return dest
}

// plenty reports a lot of free space.
func plenty(string) (uint64, bool) { return 1 << 40, true }

// openVault opens vault's database for reading what import wrote.
func openVault(t *testing.T, vault string) *sqlite.Vault {
	t.Helper()
	cfg, err := loadVaultConfig(vault)
	if err != nil {
		t.Fatalf("loadVaultConfig(%s): %v", vault, err)
	}
	dbPath, err := cfg.DatabasePath(vault)
	if err != nil {
		t.Fatal(err)
	}
	db, err := sqlite.Open(context.Background(), dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// TestI54_AnExportImportsBackToTheSameBrain: export, then import into a new
// directory, gives the same units (archived ones included), relations and
// vectors under each model, the attachments, a coherent word index, the
// vault's folders, and nooma.yml byte for byte except that Telegram is off,
// with the reason printed. .env stays behind.
func TestI54_AnExportImportsBackToTheSameBrain(t *testing.T) {
	vault := roundTripVault(t)
	if err := os.WriteFile(filepath.Join(vault, ".env"), []byte("TELEGRAM_BOT_TOKEN=secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// The source's word index has lost one note, so only import's rebuild
	// can leave the copy's coherent.
	src := openVault(t, vault)
	sqlitetest.Exec(t, src, `INSERT INTO units_fts(units_fts, rowid, content) SELECT 'delete', rowid, content FROM units WHERE id = 'u-old'`)
	if gaps, _ := src.SearchIndexGaps(context.Background()); gaps.Missing != 1 {
		t.Fatalf("the source's word index gaps = %+v, want one note missing", gaps)
	}
	b := exportTo(t, vault)
	target := filepath.Join(t.TempDir(), "copy.nooma")

	var out bytes.Buffer
	if _, err := importVault(context.Background(), b, target, &out, plenty); err != nil {
		t.Fatalf("import: %v", err)
	}
	for _, want := range []string{target, "Telegram is off in the imported nooma.yml", "steal each other's messages"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("import output lacks %q:\n%s", want, out.String())
		}
	}

	gotYML, _ := os.ReadFile(filepath.Join(target, "nooma.yml"))
	if want := strings.Replace(telegramYML, "enabled: true", "enabled: false", 1); string(gotYML) != want {
		t.Errorf("imported nooma.yml =\n%s\nwant the original with only Telegram turned off:\n%s", gotYML, want)
	}
	if _, err := os.Stat(filepath.Join(target, ".env")); !os.IsNotExist(err) {
		t.Errorf(".env reached the copy: %v", err)
	}
	for _, dir := range vaultDirs {
		if info, err := os.Stat(filepath.Join(target, dir)); err != nil || !info.IsDir() {
			t.Errorf("the copy lacks %s/: %v", dir, err)
		}
	}
	if b, _ := os.ReadFile(filepath.Join(target, "attachments", "2026", "photo.txt")); string(b) != "a picture" {
		t.Errorf("attachment = %q", b)
	}

	dst := openVault(t, target)
	ctx := context.Background()
	for _, id := range []string{"u-a", "u-b", "u-bare", "u-old"} {
		want, err := sqlite.NewUnitRepo(src).ByID(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		got, err := sqlite.NewUnitRepo(dst).ByID(ctx, id)
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Errorf("unit %s in the copy = %+v, %v; want %+v", id, got, err, want)
		}
	}
	wantRel, _ := sqlite.NewRelationRepo(src).ByID(ctx, "r-1")
	if got, err := sqlite.NewRelationRepo(dst).ByID(ctx, "r-1"); err != nil || !reflect.DeepEqual(got, wantRel) {
		t.Errorf("relation in the copy = %+v, %v; want %+v", got, err, wantRel)
	}
	for _, model := range []string{"model-a", "model-b"} {
		want, _ := sqlite.NewEmbeddingRepo(src).LoadIndex(ctx, model)
		got, err := sqlite.NewEmbeddingRepo(dst).LoadIndex(ctx, model)
		if err != nil || !reflect.DeepEqual(got, want) || len(got.IDs) != 1 {
			t.Errorf("%s vectors in the copy = %+v, %v; want %+v", model, got, err, want)
		}
	}
	if gaps, err := dst.SearchIndexGaps(ctx); err != nil || gaps != (sqlite.SearchIndexGaps{}) {
		t.Errorf("the copy's word index = %+v, %v; want it rebuilt and coherent", gaps, err)
	}
	if left, _ := filepath.Glob(filepath.Join(filepath.Dir(target), ".nooma-import-*")); len(left) != 0 {
		t.Errorf("import left its working folder: %v", left)
	}
}

// TestImport_IntoAnEmptyDirectory: an empty directory, as `install -d` makes
// one on a server, is a fresh target.
func TestImport_IntoAnEmptyDirectory(t *testing.T) {
	b := exportTo(t, roundTripVault(t))
	target := t.TempDir()
	if _, err := importVault(context.Background(), b, target, io.Discard, plenty); err != nil {
		t.Fatalf("import into an empty directory: %v", err)
	}
	if _, err := os.Stat(filepath.Join(target, "nooma.db")); err != nil {
		t.Errorf("no database in the target: %v", err)
	}
}

// TestImport_MigratesAnOlderBundleForward: a bundle of a vault at the schema
// before this binary's opens migrated to this binary's.
func TestImport_MigratesAnOlderBundleForward(t *testing.T) {
	target, err := sqlite.SchemaTarget()
	if err != nil {
		t.Fatal(err)
	}
	// Stand the vault at schema target-1 for real: drop what the last
	// migration adds (0015's column) and say so in user_version.
	vault, _, _ := doctorVault(t, func(db *sqlite.Vault) {
		sqlitetest.Exec(t, db, `ALTER TABLE config DROP COLUMN goal_stagnation_pinned`)
		sqlitetest.SetUserVersion(t, db, target-1)
	})
	b := exportTo(t, vault)
	m, err := bundle.Verify(b)
	if err != nil || m.SchemaVersion != target-1 {
		t.Fatalf("bundle schema = %d, %v; want %d", m.SchemaVersion, err, target-1)
	}
	dest := filepath.Join(t.TempDir(), "copy.nooma")
	var out bytes.Buffer
	if _, err := importVault(context.Background(), b, dest, &out, plenty); err != nil {
		t.Fatalf("import of an older bundle: %v", err)
	}
	if v, err := sqlite.VerifySnapshot(context.Background(), filepath.Join(dest, "nooma.db")); err != nil || v != target {
		t.Errorf("the copy is at schema %d, %v; want %d", v, err, target)
	}
	if want := "schema " + strconv.Itoa(target-1) + " → " + strconv.Itoa(target); !strings.Contains(out.String(), want) {
		t.Errorf("import output %q lacks %q", out.String(), want)
	}
}

// TestImport_WarnsAndContinuesWhenFreeSpaceIsUnknown: a platform that cannot
// report free space imports anyway, and says the check was skipped.
func TestImport_WarnsAndContinuesWhenFreeSpaceIsUnknown(t *testing.T) {
	b := exportTo(t, roundTripVault(t))
	var out bytes.Buffer
	unknown := func(string) (uint64, bool) { return 0, false }
	if _, err := importVault(context.Background(), b, filepath.Join(t.TempDir(), "copy.nooma"), &out, unknown); err != nil {
		t.Fatalf("import: %v", err)
	}
	if !strings.Contains(out.String(), "could not read the free space") {
		t.Errorf("import output lacks the skipped check:\n%s", out.String())
	}
}

// TestFreeSpace_ReportsATempDirectory: on the platforms CI runs, the real
// function answers.
func TestFreeSpace_ReportsATempDirectory(t *testing.T) {
	if free, ok := freeSpace(t.TempDir()); !ok || free == 0 {
		t.Errorf("freeSpace = %d, %v; want a positive answer", free, ok)
	}
}

// rewriteManifest rewrites the bundle at p with edit applied to its manifest;
// the files keep their bytes and hashes.
func rewriteManifest(t *testing.T, p string, edit func(m map[string]any)) {
	t.Helper()
	z, err := zip.OpenReader(p)
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for _, e := range z.File {
		rc, _ := e.Open()
		body, _ := io.ReadAll(rc)
		_ = rc.Close()
		if e.Name == bundle.ManifestName {
			var m map[string]any
			if err := json.Unmarshal(body, &m); err != nil {
				t.Fatal(err)
			}
			edit(m)
			body, _ = json.Marshal(m)
		}
		fw, _ := w.Create(e.Name)
		_, _ = fw.Write(body)
	}
	_ = z.Close()
	_ = w.Close()
	if err := os.WriteFile(p, buf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
}

// TestImport_RefusesAndLeavesNoVault: every refusal comes before a vault
// exists, and every failure removes what it wrote: the target is not created
// (or, when it existed, is left exactly as it was) and no working folder
// remains.
func TestImport_RefusesAndLeavesNoVault(t *testing.T) {
	schemaTarget, _ := sqlite.SchemaTarget()
	cases := map[string]struct {
		prepare func(t *testing.T, b, target string) (string, func(string) (uint64, bool))
		want    string
	}{
		"a directory that is not empty": {func(t *testing.T, b, target string) (string, func(string) (uint64, bool)) {
			if err := os.MkdirAll(target, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(target, "notes.txt"), []byte("mine"), 0o600); err != nil {
				t.Fatal(err)
			}
			return b, plenty
		}, "never writes over a vault"},
		"a newer schema": {func(t *testing.T, b, target string) (string, func(string) (uint64, bool)) {
			rewriteManifest(t, b, func(m map[string]any) { m["schema_version"] = schemaTarget + 1; m["nooma_version"] = "v9.9.9" })
			return b, plenty
		}, "v9.9.9"},
		"a newer bundle format": {func(t *testing.T, b, target string) (string, func(string) (uint64, bool)) {
			rewriteManifest(t, b, func(m map[string]any) { m["bundle_format"] = 2 })
			return b, plenty
		}, "bundle format 2"},
		"too little free space": {func(t *testing.T, b, target string) (string, func(string) (uint64, bool)) {
			return b, func(string) (uint64, bool) { return 1024, true }
		}, "free"},
		"a database that is not sound": {func(t *testing.T, b, target string) (string, func(string) (uint64, bool)) {
			dir := t.TempDir()
			junk, yml := filepath.Join(dir, "nooma.db"), filepath.Join(dir, "nooma.yml")
			_ = os.WriteFile(junk, []byte("not a database at all"), 0o600)
			_ = os.WriteFile(yml, []byte(""), 0o600)
			p := filepath.Join(dir, "junk.noomabundle")
			f, _ := os.Create(p)
			if _, err := bundle.Write(f, bundle.Manifest{SchemaVersion: schemaTarget}, []bundle.Source{{Name: "nooma.db", DiskPath: junk}, {Name: "nooma.yml", DiskPath: yml}}); err != nil {
				t.Fatal(err)
			}
			_ = f.Close()
			return p, plenty
		}, "nooma.db"},
		"a schema the database does not have": {func(t *testing.T, b, target string) (string, func(string) (uint64, bool)) {
			rewriteManifest(t, b, func(m map[string]any) { m["schema_version"] = schemaTarget - 1 })
			return b, plenty
		}, "schema"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			b := exportTo(t, roundTripVault(t))
			parent := t.TempDir()
			target := filepath.Join(parent, "copy.nooma")
			b, free := c.prepare(t, b, target)
			existed := fileExists(target)

			_, err := importVault(context.Background(), b, target, io.Discard, free)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Errorf("import = %v, want a refusal naming %q", err, c.want)
			}
			if !existed && fileExists(target) {
				t.Errorf("a failed import left %s", target)
			}
			if existed {
				if entries, _ := os.ReadDir(target); len(entries) != 1 {
					t.Errorf("the existing directory changed: %v", entries)
				}
			}
			if left, _ := filepath.Glob(filepath.Join(parent, ".nooma-import-*")); len(left) != 0 {
				t.Errorf("a failed import left its working folder: %v", left)
			}
		})
	}
}

// TestRunImport_TakesTheBundleAndTheTarget: the command line.
func TestRunImport_TakesTheBundleAndTheTarget(t *testing.T) {
	b := exportTo(t, roundTripVault(t))
	target := filepath.Join(t.TempDir(), "copy.nooma")
	var out bytes.Buffer
	if err := runImport([]string{b, target}, &out, io.Discard); err != nil {
		t.Fatalf("nooma import: %v", err)
	}
	if !strings.Contains(out.String(), "import: "+target) {
		t.Errorf("output = %q", out.String())
	}
	if err := runImport([]string{b}, io.Discard, io.Discard); err == nil {
		t.Error("nooma import with one argument succeeded")
	}
}
