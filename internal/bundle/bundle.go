// Package bundle is the .noomabundle format (ADR-0057): a zip holding a
// vault's database snapshot, its nooma.yml and its attachments, beside a
// manifest.json that names every file with its size and sha256.
//
// It knows files and bytes, never SQLite or configuration: cmd/nooma decides
// what goes in and supplies it as files on disk. It reads the standard
// library only.
package bundle

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"strings"
	"time"
)

// Format is the bundle layout this binary writes and the newest it reads. It
// moves only when the layout changes, never with the schema or the binary.
const Format = 1

// ManifestName is the manifest's entry name.
const ManifestName = "manifest.json"

// maxManifestBytes bounds how much of a manifest Verify reads: a few hundred
// files' entries fit in a few kilobytes, and an unbounded read is the cheapest
// zip bomb there is.
const maxManifestBytes = 1 << 20

// Required are the entries every bundle holds.
var required = []string{"nooma.db", "nooma.yml"}

// File is one bundled file as the manifest lists it.
type File struct {
	Path   string `json:"path"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

// ModelRows is how many embedding rows the bundled database holds under one
// model.
type ModelRows struct {
	Model string `json:"model"`
	Rows  int    `json:"rows"`
}

// Manifest is manifest.json.
type Manifest struct {
	BundleFormat    int         `json:"bundle_format"`
	NoomaVersion    string      `json:"nooma_version"`
	SchemaVersion   int         `json:"schema_version"`
	CreatedAt       time.Time   `json:"created_at"`
	EmbeddingModels []ModelRows `json:"embedding_models"`
	Files           []File      `json:"files"`
}

// Source is one file to bundle: its name in the bundle and where it is on
// disk.
type Source struct {
	Name     string
	DiskPath string
}

// Write writes a bundle of sources to w, in order, then the manifest: head
// with BundleFormat set, CreatedAt in UTC, and Files computed from the bytes
// written. It returns that manifest. A name that is not a clean relative path,
// that repeats, or that is the manifest's is refused before anything of it is
// written.
func Write(w io.Writer, head Manifest, sources []Source) (Manifest, error) {
	m := head
	m.BundleFormat = Format
	m.CreatedAt = head.CreatedAt.UTC()
	m.Files = nil
	if m.EmbeddingModels == nil {
		m.EmbeddingModels = []ModelRows{}
	}

	seen := map[string]bool{ManifestName: true}
	for _, s := range sources {
		if err := checkName(s.Name); err != nil {
			return Manifest{}, err
		}
		if seen[s.Name] {
			return Manifest{}, fmt.Errorf("bundle: %q is named twice, or is the manifest's name", s.Name)
		}
		seen[s.Name] = true
	}

	zw := zip.NewWriter(w)
	for _, s := range sources {
		f, err := addFile(zw, s)
		if err != nil {
			return Manifest{}, err
		}
		m.Files = append(m.Files, f)
	}
	mw, err := zw.Create(ManifestName)
	if err != nil {
		return Manifest{}, fmt.Errorf("bundle: writing the manifest: %w", err)
	}
	enc := json.NewEncoder(mw)
	enc.SetIndent("", "  ")
	if err := enc.Encode(m); err != nil {
		return Manifest{}, fmt.Errorf("bundle: writing the manifest: %w", err)
	}
	if err := zw.Close(); err != nil {
		return Manifest{}, fmt.Errorf("bundle: finishing the zip: %w", err)
	}
	return m, nil
}

func addFile(zw *zip.Writer, s Source) (File, error) {
	in, err := os.Open(s.DiskPath)
	if err != nil {
		return File{}, fmt.Errorf("bundle: reading %s: %w", s.Name, err)
	}
	defer func() { _ = in.Close() }()
	fw, err := zw.CreateHeader(&zip.FileHeader{Name: s.Name, Method: zip.Deflate})
	if err != nil {
		return File{}, fmt.Errorf("bundle: adding %s: %w", s.Name, err)
	}
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(fw, h), in)
	if err != nil {
		return File{}, fmt.Errorf("bundle: adding %s: %w", s.Name, err)
	}
	return File{Path: s.Name, Size: n, SHA256: hex.EncodeToString(h.Sum(nil))}, nil
}

// checkName refuses a name a reader could not unpack safely: empty, absolute,
// with a backslash, leaving the bundle through "..", or one Windows cannot
// hold, so a bundle made anywhere imports everywhere.
func checkName(name string) error {
	if name == "" || strings.HasPrefix(name, "/") || strings.Contains(name, `\`) ||
		path.Clean(name) != name || name == ".." || strings.HasPrefix(name, "../") {
		return fmt.Errorf("bundle: %q is not a path inside the bundle", name)
	}
	for _, seg := range strings.Split(name, "/") {
		if windowsReserved(seg) {
			return fmt.Errorf("bundle: %q is a name Windows cannot hold", name)
		}
	}
	return nil
}

// windowsReserved reports a path segment Windows refuses or rewrites: a
// device name (CON, PRN, AUX, NUL, COM1-9, LPT1-9, with any extension), a
// trailing dot or space, a character it reserves, or a control character.
func windowsReserved(seg string) bool {
	if strings.HasSuffix(seg, ".") || strings.HasSuffix(seg, " ") || strings.ContainsAny(seg, `<>:"|?*`) {
		return true
	}
	for _, r := range seg {
		if r < 0x20 {
			return true
		}
	}
	stem, _, _ := strings.Cut(strings.ToUpper(seg), ".")
	switch stem {
	case "CON", "PRN", "AUX", "NUL":
		return true
	}
	return len(stem) == 4 && (strings.HasPrefix(stem, "COM") || strings.HasPrefix(stem, "LPT")) && stem[3] >= '1' && stem[3] <= '9'
}

// Verify re-reads the bundle at p and returns its manifest only when the zip
// and the manifest agree exactly: a format this binary knows, the required
// files present, every listed file present once with its size and sha256,
// every name safe to unpack on any system, every entry a regular file, no two
// names equal once case is folded, and no entry the manifest does not list.
// It is what import trusts before writing anything, so it reads a bounded
// manifest and never decompresses an entry past its declared size.
func Verify(p string) (Manifest, error) {
	z, err := zip.OpenReader(p)
	if err != nil {
		return Manifest{}, fmt.Errorf("bundle: %s is not a readable zip: %w", p, err)
	}
	defer func() { _ = z.Close() }()

	entries := map[string]*zip.File{}
	folded := map[string]string{}
	var errs []error
	for _, e := range z.File {
		if _, dup := entries[e.Name]; dup {
			return Manifest{}, fmt.Errorf("bundle: %q appears twice", e.Name)
		}
		entries[e.Name] = e
		if !e.Mode().IsRegular() {
			errs = append(errs, fmt.Errorf("bundle: %q is not a regular file (%s)", e.Name, e.Mode().Type()))
		}
		// Two names a case-insensitive filesystem would make one file.
		key := strings.ToLower(e.Name)
		if other, ok := folded[key]; ok {
			errs = append(errs, fmt.Errorf("bundle: %q and %q differ only by case", other, e.Name))
		}
		folded[key] = e.Name
	}
	me, ok := entries[ManifestName]
	if !ok {
		return Manifest{}, fmt.Errorf("bundle: %s holds no %s", p, ManifestName)
	}
	m, err := readManifest(me)
	if err != nil {
		return Manifest{}, err
	}
	if m.BundleFormat < 1 || m.BundleFormat > Format {
		return Manifest{}, fmt.Errorf("bundle: bundle format %d is not one this binary reads (it reads up to %d); upgrade nooma", m.BundleFormat, Format)
	}

	listed := map[string]bool{ManifestName: true}
	for _, f := range m.Files {
		if err := checkName(f.Path); err != nil {
			errs = append(errs, err)
			continue
		}
		listed[f.Path] = true
		e, ok := entries[f.Path]
		if !ok {
			errs = append(errs, fmt.Errorf("bundle: %s is listed and missing", f.Path))
			continue
		}
		if err := checkEntry(e, f); err != nil {
			errs = append(errs, err)
		}
	}
	for _, name := range required {
		if !listed[name] {
			errs = append(errs, fmt.Errorf("bundle: no %s", name))
		}
	}
	for _, e := range z.File {
		if !listed[e.Name] {
			errs = append(errs, fmt.Errorf("bundle: %q is not in the manifest", e.Name))
		}
	}
	if err := errors.Join(errs...); err != nil {
		return Manifest{}, err
	}
	return m, nil
}

func readManifest(e *zip.File) (Manifest, error) {
	rc, err := e.Open()
	if err != nil {
		return Manifest{}, fmt.Errorf("bundle: reading %s: %w", ManifestName, err)
	}
	defer func() { _ = rc.Close() }()
	raw, err := io.ReadAll(io.LimitReader(rc, maxManifestBytes+1))
	if err != nil {
		return Manifest{}, fmt.Errorf("bundle: reading %s: %w", ManifestName, err)
	}
	if len(raw) > maxManifestBytes {
		return Manifest{}, fmt.Errorf("bundle: %s is larger than %d bytes; refusing to read it", ManifestName, maxManifestBytes)
	}
	var m Manifest
	if err := json.Unmarshal(raw, &m); err != nil {
		return Manifest{}, fmt.Errorf("bundle: %s does not decode: %w", ManifestName, err)
	}
	return m, nil
}

// checkEntry streams e and compares its size and sha256 with f. The zip's own
// header must declare the manifest's size, and no more than that size and one
// byte is ever decompressed: an entry that inflates past it is refused at
// that byte, not after.
func checkEntry(e *zip.File, f File) error {
	if f.Size < 0 || e.UncompressedSize64 != uint64(f.Size) {
		return fmt.Errorf("bundle: %s declares %d bytes in the zip, the manifest says %d", f.Path, e.UncompressedSize64, f.Size)
	}
	rc, err := e.Open()
	if err != nil {
		return fmt.Errorf("bundle: reading %s: %w", f.Path, err)
	}
	defer func() { _ = rc.Close() }()
	h := sha256.New()
	// The zip reader itself stops at the header's size, which must be the
	// manifest's; the limit holds that bound even if it ever did not.
	n, err := io.Copy(h, io.LimitReader(rc, f.Size+1))
	if err != nil {
		return fmt.Errorf("bundle: reading %s: %w", f.Path, err)
	}
	if n != f.Size {
		return fmt.Errorf("bundle: %s is %d bytes, the manifest says %d", f.Path, n, f.Size)
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != f.SHA256 {
		return fmt.Errorf("bundle: %s does not match its sha256 in the manifest", f.Path)
	}
	return nil
}
