//go:build integration

package sqlite

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// TestBuildDSNWindowsStyleOpensTheFileItNames is the behavioral test that
// the Windows breakage of tasks.md §7.7 needed and never had.
//
// The bug it guards: every L3 test on windows-latest failed with
// "CreateFile /C:: The filename, directory name, or volume label syntax is
// incorrect", while TestBuildDSNAbsolutePathHandling — which asserts on the
// DSN's parsed URI — passed. The DSN was right by RFC 8089 and the vault
// still could not be opened. Asserting what a component produces is not
// asserting that the component works, and this file is the correction:
// it takes the DSN through driver.Open and asserts a real file appeared at
// the real path the caller named.
//
// Why it can run on Linux, which is the whole point. pathStyle is an
// injected parameter, not a guess made from the runner's own GOOS (see
// pathStyle's doc comment in dsn.go), so the Windows-shaped branch is
// reachable from any runner. And the failure reproduces there exactly:
// under `file:///C:/...` this driver hands the VFS the literal path
// "/C:/..." — on Linux that is "lstat /C:: no such file or directory", on
// Windows it is "CreateFile /C:". Same defect, same cause, two error
// strings. The driver's SQLite is a wasm build, so its URI parser is not a
// SQLITE_OS_WIN build and never applies the drive-letter fixup that would
// strip that leading slash; vfs/file.go then passes the name to
// os.OpenFile verbatim.
//
// Skipped on Windows, and the reason is not squeamishness: there "C:/data"
// is a genuinely absolute path, so the test would leave the temporary
// directory and write to the root of the runner's system drive. On Windows
// the coverage this test provides is provided better by the rest of the L3
// suite, which opens real vaults through real Windows file APIs — that is
// what the integration-windows job exists to run.
func TestBuildDSNWindowsStyleOpensTheFileItNames(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("on windows a drive-letter path is absolute and would escape t.TempDir(); the whole L3 suite covers this there")
	}

	t.Chdir(t.TempDir())

	// "C:" is an ordinary directory name on a POSIX filesystem, which is
	// what makes the Windows-shaped path reachable here at all.
	dir := filepath.Join("C:", "Users", "pablo")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("creating %s: %v", dir, err)
	}

	dsn, err := buildDSN(`C:\Users\pablo\vault.db`, windowsStyle)
	if err != nil {
		t.Fatalf(`buildDSN(C:\Users\pablo\vault.db, windowsStyle) = _, %v, want nil error`, err)
	}

	db, err := openFirstConnection(context.Background(), dsn)
	if err != nil {
		t.Fatalf("openFirstConnection(ctx, %q) = _, %v, want a usable connection", dsn, err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("closing the vault: %v", err)
		}
	})

	want := filepath.Join(dir, "vault.db")
	if _, err := os.Stat(want); err != nil {
		t.Fatalf("after opening %q, stat %s = %v, want the vault file to exist at the path the caller named", dsn, want, err)
	}
}

// TestBuildDSNWindowsStylePreservesAwkwardCharacters proves the escaping
// the opaque form made this package's own responsibility.
//
// Under the authority form, net/url escaped the path; under "file:<path>"
// it does not (url.URL.Opaque is written verbatim by String()), so a vault
// path containing a space, '#' or '?' would be truncated or reinterpreted
// as a query parameter — exactly the defect design D3 chose net/url to
// avoid. buildDSN escapes it explicitly instead, and SQLite percent-decodes
// it back. This test is what says so with a file on disk rather than with a
// string comparison.
func TestBuildDSNWindowsStylePreservesAwkwardCharacters(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("on windows a drive-letter path is absolute and would escape t.TempDir(); also '?' and '#' are not legal in a Windows filename")
	}

	t.Chdir(t.TempDir())

	dir := filepath.Join("C:", "Users", "pa blo")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("creating %s: %v", dir, err)
	}

	dsn, err := buildDSN(`C:\Users\pa blo\my vault #1?share.db`, windowsStyle)
	if err != nil {
		t.Fatalf("buildDSN(awkward path, windowsStyle) = _, %v, want nil error", err)
	}

	db, err := openFirstConnection(context.Background(), dsn)
	if err != nil {
		t.Fatalf("openFirstConnection(ctx, %q) = _, %v, want a usable connection", dsn, err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("closing the vault: %v", err)
		}
	})

	want := filepath.Join(dir, "my vault #1?share.db")
	if _, err := os.Stat(want); err != nil {
		t.Fatalf("after opening %q, stat %s = %v, want the vault file at the exact path named, with no truncation at the space, '#' or '?'", dsn, want, err)
	}
}
