package bundle_test

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/bundle"
)

var bundleAt = time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)

// sources writes each name's content into dir and returns them as bundle
// sources, in order.
func sources(t *testing.T, dir string, files [][2]string) []bundle.Source {
	t.Helper()
	var out []bundle.Source
	for _, f := range files {
		p := filepath.Join(dir, strings.ReplaceAll(f[0], "/", "_"))
		if err := os.WriteFile(p, []byte(f[1]), 0o600); err != nil {
			t.Fatal(err)
		}
		out = append(out, bundle.Source{Name: f[0], DiskPath: p})
	}
	return out
}

var sample = [][2]string{
	{"nooma.db", "SQLite format 3\x00 pretend"},
	{"nooma.yml", "server:\n  bind: 127.0.0.1\n"},
	{"attachments/notes/a.txt", "an attachment"},
}

func header() bundle.Manifest {
	return bundle.Manifest{
		NoomaVersion: "v0.5.0", SchemaVersion: 15, CreatedAt: bundleAt,
		EmbeddingModels: []bundle.ModelRows{{Model: "model-a", Rows: 2}},
	}
}

// writeBundle writes a bundle of files to dir/b.noomabundle.
func writeBundle(t *testing.T, dir string, files [][2]string) (string, bundle.Manifest) {
	t.Helper()
	path := filepath.Join(dir, "b.noomabundle")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	m, err := bundle.Write(f, header(), sources(t, dir, files))
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	return path, m
}

func sum(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

// TestWrite_ThenVerify_RoundTripsEveryFile (I54): the manifest names every
// file with its size and sha256, Verify re-reads the zip and agrees, and each
// entry holds exactly the bytes written.
func TestWrite_ThenVerify_RoundTripsEveryFile(t *testing.T) {
	path, written := writeBundle(t, t.TempDir(), sample)

	if written.BundleFormat != bundle.Format || bundle.Format != 1 {
		t.Errorf("bundle_format = %d (Format %d), want 1", written.BundleFormat, bundle.Format)
	}
	if len(written.Files) != 3 {
		t.Fatalf("manifest files = %+v, want 3", written.Files)
	}
	for i, f := range sample {
		got := written.Files[i]
		if got.Path != f[0] || got.Size != int64(len(f[1])) || got.SHA256 != sum(f[1]) {
			t.Errorf("manifest file %d = %+v, want %s, %d bytes, %s", i, got, f[0], len(f[1]), sum(f[1]))
		}
	}

	read, err := bundle.Verify(path)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if read.SchemaVersion != 15 || read.NoomaVersion != "v0.5.0" || !read.CreatedAt.Equal(bundleAt) ||
		len(read.EmbeddingModels) != 1 || read.EmbeddingModels[0] != (bundle.ModelRows{Model: "model-a", Rows: 2}) {
		t.Errorf("Verify's manifest = %+v, want the header written", read)
	}

	z, err := zip.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = z.Close() }()
	contents := map[string]string{}
	for _, e := range z.File {
		rc, err := e.Open()
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(rc)
		_ = rc.Close()
		contents[e.Name] = string(b)
	}
	for _, f := range sample {
		if contents[f[0]] != f[1] {
			t.Errorf("entry %s = %q, want %q", f[0], contents[f[0]], f[1])
		}
	}
	var raw map[string]any
	if err := json.Unmarshal([]byte(contents[bundle.ManifestName]), &raw); err != nil {
		t.Fatalf("manifest.json: %v", err)
	}
	for _, key := range []string{"bundle_format", "nooma_version", "schema_version", "created_at", "embedding_models", "files"} {
		if _, ok := raw[key]; !ok {
			t.Errorf("manifest.json lacks %q: %s", key, contents[bundle.ManifestName])
		}
	}
	if raw["created_at"] != "2026-10-29T12:00:00Z" {
		t.Errorf("created_at = %v, want UTC RFC3339", raw["created_at"])
	}
	if len(contents) != 4 {
		t.Errorf("zip entries = %d, want the three files and the manifest", len(contents))
	}
}

// rezip rewrites the bundle at path with edit applied to its entries.
func rezip(t *testing.T, path string, edit func(entries map[string][]byte)) {
	t.Helper()
	z, err := zip.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	entries := map[string][]byte{}
	var order []string
	for _, e := range z.File {
		rc, _ := e.Open()
		b, _ := io.ReadAll(rc)
		_ = rc.Close()
		entries[e.Name] = b
		order = append(order, e.Name)
	}
	_ = z.Close()
	edit(entries)
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	seen := map[string]bool{}
	for _, name := range append(order, keys(entries)...) {
		b, ok := entries[name]
		if !ok || seen[name] {
			continue
		}
		seen[name] = true
		fw, _ := w.Create(name)
		_, _ = fw.Write(b)
	}
	_ = w.Close()
	if err := os.WriteFile(path, buf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
}

func keys(m map[string][]byte) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}

func editManifest(t *testing.T, entries map[string][]byte, edit func(m map[string]any)) {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(entries[bundle.ManifestName], &m); err != nil {
		t.Fatal(err)
	}
	edit(m)
	b, _ := json.Marshal(m)
	entries[bundle.ManifestName] = b
}

// TestVerify_RefusesABundleThatDoesNotMatchItsManifest: every way a bundle
// can disagree with its manifest, or be unsafe to unpack, is refused.
func TestVerify_RefusesABundleThatDoesNotMatchItsManifest(t *testing.T) {
	cases := map[string]struct {
		edit func(t *testing.T, e map[string][]byte)
		want string
	}{
		"a file's bytes changed": {func(t *testing.T, e map[string][]byte) { e["nooma.yml"] = []byte("server:\n  bind: 0.0.0.0\n") }, "nooma.yml"},
		// The same length, so only the sha256 can tell.
		"a file's bytes changed, its size kept": {func(t *testing.T, e map[string][]byte) { e["nooma.yml"] = []byte("server:\n  bind: 127.0.0.2\n") }, "sha256"},
		"a listed file missing":                 {func(t *testing.T, e map[string][]byte) { delete(e, "attachments/notes/a.txt") }, "attachments/notes/a.txt"},
		"an unlisted entry":                     {func(t *testing.T, e map[string][]byte) { e[".env"] = []byte("KEY=secret") }, ".env"},
		"no manifest":                           {func(t *testing.T, e map[string][]byte) { delete(e, bundle.ManifestName) }, bundle.ManifestName},
		"a newer bundle format": {func(t *testing.T, e map[string][]byte) {
			editManifest(t, e, func(m map[string]any) { m["bundle_format"] = 2 })
		}, "bundle format 2"},
		"a size that disagrees": {func(t *testing.T, e map[string][]byte) {
			editManifest(t, e, func(m map[string]any) { m["files"].([]any)[1].(map[string]any)["size"] = 1 })
		}, "nooma.yml"},
		"a path leaving the bundle": {func(t *testing.T, e map[string][]byte) {
			e["../outside.txt"] = []byte("x")
			editManifest(t, e, func(m map[string]any) {
				m["files"] = append(m["files"].([]any), map[string]any{"path": "../outside.txt", "size": 1, "sha256": sum("x")})
			})
		}, "../outside.txt"},
		"no database": {func(t *testing.T, e map[string][]byte) {
			delete(e, "nooma.db")
			editManifest(t, e, func(m map[string]any) { m["files"] = m["files"].([]any)[1:] })
		}, "nooma.db"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			path, _ := writeBundle(t, t.TempDir(), sample)
			rezip(t, path, func(e map[string][]byte) { c.edit(t, e) })
			_, err := bundle.Verify(path)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Errorf("Verify = %v, want a refusal naming %q", err, c.want)
			}
		})
	}
}

// TestWrite_RefusesAnUnsafeOrRepeatedName: a source named outside the bundle,
// absolute, or twice, is refused before anything is written for it.
func TestWrite_RefusesAnUnsafeOrRepeatedName(t *testing.T) {
	for name, files := range map[string][][2]string{
		"parent":   {{"../x", "x"}},
		"absolute": {{"/etc/passwd", "x"}},
		"repeated": {{"nooma.yml", "a"}, {"nooma.yml", "b"}},
		"manifest": {{bundle.ManifestName, "{}"}},
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			var buf bytes.Buffer
			if _, err := bundle.Write(&buf, header(), sources(t, dir, files)); err == nil {
				t.Errorf("Write accepted %v", files)
			}
		})
	}
}
