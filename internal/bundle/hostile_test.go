package bundle_test

import (
	"archive/zip"
	"bytes"
	"compress/flate"
	"encoding/json"
	"hash/crc32"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rengo/nooma/internal/bundle"
)

// rawEntry is one entry of a hand-built zip: what a foreign bundle can hold,
// whatever bundle.Write would have refused to write.
type rawEntry struct {
	name string
	body []byte
	mode fs.FileMode // 0 for a regular file
}

// rawBundle writes entries and a manifest listing listed (path -> body) to a
// zip on disk, so Verify can be given what an attacker or a bug could make.
func rawBundle(t *testing.T, entries []rawEntry, listed []bundle.File, manifest []byte) string {
	t.Helper()
	if manifest == nil {
		m := bundle.Manifest{BundleFormat: 1, SchemaVersion: 15, CreatedAt: bundleAt, Files: listed}
		var err error
		if manifest, err = json.Marshal(m); err != nil {
			t.Fatal(err)
		}
	}
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for _, e := range append(entries, rawEntry{name: bundle.ManifestName, body: manifest}) {
		h := &zip.FileHeader{Name: e.name, Method: zip.Deflate}
		if e.mode != 0 {
			h.SetMode(e.mode)
		}
		fw, err := w.CreateHeader(h)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := fw.Write(e.body); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(t.TempDir(), "hostile.noomabundle")
	if err := os.WriteFile(p, buf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func listedFile(name, body string) bundle.File {
	return bundle.File{Path: name, Size: int64(len(body)), SHA256: sum(body)}
}

// base is a well-formed bundle's two required entries.
func base() ([]rawEntry, []bundle.File) {
	return []rawEntry{{name: "nooma.db", body: []byte("db")}, {name: "nooma.yml", body: []byte("yml")}},
		[]bundle.File{listedFile("nooma.db", "db"), listedFile("nooma.yml", "yml")}
}

// TestVerify_AcceptsAWellFormedHandBuiltBundle: the hand-built helper makes a
// bundle Verify accepts, so each refusal below is the one change it names.
func TestVerify_AcceptsAWellFormedHandBuiltBundle(t *testing.T) {
	entries, listed := base()
	if _, err := bundle.Verify(rawBundle(t, entries, listed, nil)); err != nil {
		t.Fatalf("Verify of a well-formed bundle = %v", err)
	}
}

// TestVerify_RefusesAHostileBundle: what a foreign bundle can carry that a
// reader must not unpack: a name twice, an entry that is not a regular file,
// an entry that decompresses past its declared size, an oversized manifest,
// names that collide once case is folded, and names Windows cannot hold.
func TestVerify_RefusesAHostileBundle(t *testing.T) {
	big := strings.Repeat("0", 1<<20)
	cases := map[string]struct {
		build func() string
		want  string
	}{
		"a name twice": {func() string {
			entries, listed := base()
			entries = append(entries, rawEntry{name: "nooma.yml", body: []byte("yml")})
			return rawBundle(t, entries, listed, nil)
		}, "appears twice"},
		"a directory entry": {func() string {
			entries, listed := base()
			entries = append(entries, rawEntry{name: "attachments/", mode: fs.ModeDir | 0o755})
			return rawBundle(t, entries, append(listed, listedFile("attachments/", "")), nil)
		}, "not a regular file"},
		"a link": {func() string {
			entries, listed := base()
			entries = append(entries, rawEntry{name: "attachments/key", body: []byte("../.env"), mode: fs.ModeSymlink | 0o777})
			return rawBundle(t, entries, append(listed, listedFile("attachments/key", "../.env")), nil)
		}, "not a regular file"},
		"an entry larger than declared": {func() string {
			entries, listed := base()
			entries = append(entries, rawEntry{name: "attachments/bomb", body: []byte(big)})
			// The manifest says 10 bytes; a reader must stop there, not
			// decompress the megabyte to find out.
			return rawBundle(t, entries, append(listed, bundle.File{Path: "attachments/bomb", Size: 10, SHA256: sum(big[:10])}), nil)
		}, "attachments/bomb declares 1048576 bytes in the zip, the manifest says 10"},
		"an oversized manifest": {func() string {
			entries, listed := base()
			m, _ := json.Marshal(bundle.Manifest{BundleFormat: 1, Files: listed, NoomaVersion: strings.Repeat("v", 2<<20)})
			return rawBundle(t, entries, listed, m)
		}, "manifest.json is larger than"},
		"names that differ only by case": {func() string {
			entries, listed := base()
			entries = append(entries, rawEntry{name: "attachments/Photo.txt", body: []byte("a")}, rawEntry{name: "attachments/photo.txt", body: []byte("b")})
			return rawBundle(t, entries, append(listed, listedFile("attachments/Photo.txt", "a"), listedFile("attachments/photo.txt", "b")), nil)
		}, "differ only by case"},
	}
	for _, reserved := range []string{"attachments/CON", "attachments/aux.txt", "attachments/com1.log", "attachments/lpt9", "attachments/nul",
		"attachments/trailing.", "attachments/trailing ", "attachments/a:b", "attachments/q?.txt", "attachments/x\x01y"} {
		cases["a Windows name "+strings.ReplaceAll(reserved, "\x01", `\x01`)] = struct {
			build func() string
			want  string
		}{func() string {
			entries, listed := base()
			entries = append(entries, rawEntry{name: reserved, body: []byte("x")})
			return rawBundle(t, entries, append(listed, listedFile(reserved, "x")), nil)
		}, "Windows"}
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := bundle.Verify(c.build())
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Errorf("Verify = %v, want a refusal naming %q", err, c.want)
			}
		})
	}
}

// TestWrite_RefusesAWindowsReservedName: export refuses what import on
// Windows could not create, before writing it.
func TestWrite_RefusesAWindowsReservedName(t *testing.T) {
	var buf bytes.Buffer
	if _, err := bundle.Write(&buf, header(), sources(t, t.TempDir(), [][2]string{{"attachments/con.txt", "x"}})); err == nil ||
		!strings.Contains(err.Error(), "Windows") {
		t.Errorf("Write = %v, want a refusal naming Windows", err)
	}
}

// TestVerify_StopsABombWhoseZipHeaderLies: an entry whose zip header and
// manifest both claim 10 bytes, and whose data inflates to a megabyte, is
// refused at the declared size, never inflated whole.
func TestVerify_StopsABombWhoseZipHeaderLies(t *testing.T) {
	var comp bytes.Buffer
	fw, err := flate.NewWriter(&comp, flate.BestCompression)
	if err != nil {
		t.Fatal(err)
	}
	inflated := strings.Repeat("0", 1<<20)
	if _, err := fw.Write([]byte(inflated)); err != nil {
		t.Fatal(err)
	}
	_ = fw.Close()

	entries, listed := base()
	listed = append(listed, bundle.File{Path: "attachments/bomb", Size: 10, SHA256: sum(inflated[:10])})
	m, _ := json.Marshal(bundle.Manifest{BundleFormat: 1, SchemaVersion: 15, CreatedAt: bundleAt, Files: listed})
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for _, e := range append(entries, rawEntry{name: bundle.ManifestName, body: m}) {
		ew, _ := w.Create(e.name)
		_, _ = ew.Write(e.body)
	}
	raw, err := w.CreateRaw(&zip.FileHeader{
		Name: "attachments/bomb", Method: zip.Deflate,
		CompressedSize64: uint64(comp.Len()), UncompressedSize64: 10,
		CRC32: crc32.ChecksumIEEE([]byte(inflated[:10])),
	})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = raw.Write(comp.Bytes())
	_ = w.Close()
	p := filepath.Join(t.TempDir(), "bomb.noomabundle")
	if err := os.WriteFile(p, buf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := bundle.Verify(p); err == nil || !strings.Contains(err.Error(), "attachments/bomb") {
		t.Errorf("Verify of a bomb with a lying header = %v, want a refusal naming it", err)
	}
}
