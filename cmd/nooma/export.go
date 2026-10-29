package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/rengo/nooma/internal/bundle"
	"github.com/rengo/nooma/internal/config"
	"github.com/rengo/nooma/internal/store/sqlite"
)

func init() {
	commands["export"] = command{
		summary: "write a vault into one verified .noomabundle file, even while it is served",
		run:     runExport,
	}
}

// bundleSuffix is the only name export writes.
const bundleSuffix = ".noomabundle"

// runExport is `nooma export <vault> <file.noomabundle>` (ADR-0057).
func runExport(args []string, out, errOut io.Writer) error {
	fs := flag.NewFlagSet("export", flag.ContinueOnError)
	fs.SetOutput(errOut)
	fs.Usage = func() {
		_, _ = fmt.Fprint(errOut, "usage: nooma export <vault> <file.noomabundle>\n")
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 2 {
		fs.Usage()
		return fmt.Errorf("export takes a vault and a bundle file, got %d arguments (see usage)", fs.NArg())
	}
	m, err := exportVault(context.Background(), fs.Arg(0), fs.Arg(1), time.Now(), bundle.Verify)
	if err != nil {
		return err
	}
	dest, _ := filepath.Abs(fs.Arg(1))
	_, err = fmt.Fprintf(out, "export: %s (%d files, schema %d)\n", dest, len(m.Files), m.SchemaVersion)
	return err
}

// exportVault writes the vault into dest as a .noomabundle and returns its
// manifest (ADR-0057).
//
// It takes no vault lock: the database is copied by backup's own snapshot, a
// VACUUM INTO over a read-only connection, so it never migrates or writes the
// vault and runs beside serve. The bundle holds that snapshot once it passed
// integrity_check, nooma.yml as it is, and the attachments; never .env,
// derived/, logs/ or the lock. It is written as dest.partial, re-read and
// checked against its own manifest by verify, and only then renamed: on any
// failure nothing is left at dest or beside it.
func exportVault(ctx context.Context, vaultArg, dest string, now time.Time, verify func(string) (bundle.Manifest, error)) (bundle.Manifest, error) {
	vault, err := config.ResolveVault(vaultArg)
	if err != nil {
		return bundle.Manifest{}, err
	}
	cfg, err := loadVaultConfig(vault)
	if err != nil {
		return bundle.Manifest{}, err
	}
	if err := refuseEndpointCredentials(cfg); err != nil {
		return bundle.Manifest{}, err
	}
	dbPath, err := cfg.DatabasePath(vault)
	if err != nil {
		return bundle.Manifest{}, err
	}
	if _, err := os.Stat(dbPath); err != nil {
		return bundle.Manifest{}, fmt.Errorf("the database %s is missing: %w", filepath.Base(dbPath), err)
	}
	if dest, err = exportDestination(vault, dest); err != nil {
		return bundle.Manifest{}, err
	}
	attachments, err := attachmentSources(vault)
	if err != nil {
		return bundle.Manifest{}, err
	}

	// The snapshot is written beside dest, on its filesystem, and removed
	// with its folder whatever happens.
	work, err := os.MkdirTemp(filepath.Dir(dest), ".nooma-export-*")
	if err != nil {
		return bundle.Manifest{}, fmt.Errorf("preparing the export: %w", err)
	}
	defer func() { _ = os.RemoveAll(work) }()
	partial := dest + ".partial"
	committed := false
	defer func() {
		if !committed {
			_ = os.Remove(partial)
		}
	}()

	snap := filepath.Join(work, "nooma.db")
	if err := snapshot(ctx, dbPath, snap); err != nil {
		return bundle.Manifest{}, err
	}
	schema, err := sqlite.VerifySnapshot(ctx, snap)
	if err != nil {
		return bundle.Manifest{}, err
	}
	models, err := snapshotModels(ctx, snap)
	if err != nil {
		return bundle.Manifest{}, err
	}

	sources := append([]bundle.Source{
		{Name: "nooma.db", DiskPath: snap},
		{Name: "nooma.yml", DiskPath: filepath.Join(vault, config.ConfigFileName)},
	}, attachments...)
	written, err := writeBundleFile(partial, bundle.Manifest{
		NoomaVersion: version, SchemaVersion: schema, CreatedAt: now, EmbeddingModels: models,
	}, sources)
	if err != nil {
		return bundle.Manifest{}, err
	}

	read, err := verify(partial)
	if err != nil {
		return bundle.Manifest{}, fmt.Errorf("the bundle did not verify, so none was written: %w", err)
	}
	if !reflect.DeepEqual(read.Files, written.Files) {
		return bundle.Manifest{}, errors.New("the bundle re-read differently from what was written, so none was written")
	}
	if err := os.Rename(partial, dest); err != nil {
		return bundle.Manifest{}, fmt.Errorf("finishing %s: %w", dest, err)
	}
	committed = true
	return written, nil
}

// exportDestination makes dest absolute and refuses one export must not
// write: not named .noomabundle, inside the vault, or already there.
func exportDestination(vault, dest string) (string, error) {
	dest, err := filepath.Abs(dest)
	if err != nil {
		return "", err
	}
	if !strings.HasSuffix(dest, bundleSuffix) {
		return "", fmt.Errorf("%s does not end in %s; name the bundle file so", dest, bundleSuffix)
	}
	if insideVault(vault, dest) {
		return "", fmt.Errorf("%s is inside the vault %s; write the bundle somewhere else", dest, vault)
	}
	for _, p := range []string{dest, dest + ".partial"} {
		if _, err := os.Lstat(p); err == nil {
			return "", fmt.Errorf("%s already exists; refusing to replace it", p)
		}
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o700); err != nil {
		return "", fmt.Errorf("creating %s: %w", filepath.Dir(dest), err)
	}
	return dest, nil
}

// refuseEndpointCredentials refuses a provider endpoint carrying a user or
// password in its URL: nooma.yml goes into the bundle as it is, and a secret
// belongs in .env, which never does.
func refuseEndpointCredentials(cfg *config.Config) error {
	names := make([]string, 0, len(cfg.Providers))
	for name := range cfg.Providers {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		endpoint := cfg.Providers[name].Endpoint
		if endpoint == "" {
			continue
		}
		if u, err := url.Parse(endpoint); err == nil && u.User != nil {
			return fmt.Errorf("provider %s: its endpoint carries credentials in the URL, which would travel in the bundle; move the secret to .env (api_key_env) and export again", name)
		}
	}
	return nil
}

// attachmentSources lists every regular file under attachments/, by path. A
// link or any other kind of entry is refused, naming it: a link could carry a
// file from outside the vault, .env included, into the bundle.
func attachmentSources(vault string) ([]bundle.Source, error) {
	root := filepath.Join(vault, "attachments")
	var out []bundle.Source
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) && p == root {
				return fs.SkipDir
			}
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(vault, p)
		if err != nil {
			return err
		}
		if !d.Type().IsRegular() {
			return fmt.Errorf("%s is not a regular file; export carries only regular files from attachments/", rel)
		}
		out = append(out, bundle.Source{Name: filepath.ToSlash(rel), DiskPath: p})
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("reading the vault's attachments: %w", err)
	}
	return out, nil
}

// snapshotModels counts the snapshot's embedding rows by model, read-only.
func snapshotModels(ctx context.Context, snap string) ([]bundle.ModelRows, error) {
	v, err := sqlite.OpenReadOnly(ctx, snap)
	if err != nil {
		return nil, err
	}
	defer func() { _ = v.Close() }()
	counts, err := sqlite.NewEmbeddingRepo(v).ModelRows(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]bundle.ModelRows, len(counts))
	for i, c := range counts {
		out[i] = bundle.ModelRows{Model: c.Model, Rows: c.Units}
	}
	return out, nil
}

// writeBundleFile writes the bundle to path, which must not exist, owner-only.
func writeBundleFile(path string, head bundle.Manifest, sources []bundle.Source) (bundle.Manifest, error) {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return bundle.Manifest{}, fmt.Errorf("writing %s: %w", path, err)
	}
	m, err := bundle.Write(f, head, sources)
	if err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return bundle.Manifest{}, fmt.Errorf("writing %s: %w", path, err)
	}
	return m, nil
}
