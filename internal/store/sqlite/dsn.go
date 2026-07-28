package sqlite

import (
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

// ErrRelativeDBPath is the error buildDSN returns when dbPath is not
// absolute. See buildDSN's doc comment for why a relative path is rejected
// rather than resolved.
var ErrRelativeDBPath = errors.New("sqlite: vault path must be absolute")

// windowsDriveAbs matches a Windows drive-letter absolute path such as
// `C:\Users\pablo` or `C:/Users/pablo`. It is a plain string pattern, not a
// filepath.IsAbs call, on purpose: filepath.IsAbs only recognizes the path
// shape native to the GOOS it was compiled for, so a Linux build of this
// package would never recognize a Windows path as absolute — and this
// package is cross-compiled for Windows (ADR-0001) without ever running a
// Windows binary in this repo's own CI. Detecting both shapes by pattern,
// independent of the build's own GOOS, is what lets dsn_test.go prove the
// Windows case on a Linux runner.
var windowsDriveAbs = regexp.MustCompile(`^[A-Za-z]:[\\/]`)

// isAbsoluteVaultPath reports whether p is absolute in either POSIX form
// (a leading "/") or Windows drive-letter form.
func isAbsoluteVaultPath(p string) bool {
	return strings.HasPrefix(p, "/") || windowsDriveAbs.MatchString(p)
}

// toURIPath converts an already-confirmed-absolute dbPath to the path
// component a "file:" URI expects: forward slashes throughout, and a
// leading "/" even for a Windows drive letter, producing the standard
// "file:///C:/Users/..." shape (RFC 8089's file-absolute form) that
// SQLite accepts on every target platform.
func toURIPath(dbPath string) string {
	p := strings.ReplaceAll(dbPath, `\`, "/")
	if windowsDriveAbs.MatchString(dbPath) {
		p = "/" + p
	}
	return p
}

// buildDSN builds the file: URI driver.Open expects from a plain,
// absolute filesystem path. It is built with net/url rather than string
// concatenation — a vault path with a space, a '?' or a '#' would
// otherwise be silently truncated or reinterpreted as query parameters
// (design D3) — and it carries every operational setting this store
// requires, in the order design D3 fixes.
//
// dbPath MUST be absolute. This is checked structurally, not documented as
// a warning (CLAUDE.md non-negotiable #7): url.URL{Path: p} treats a path
// that does not start with "/" as ambiguous with the URI's authority
// component, and net/url.URL.String() resolves that ambiguity by silently
// writing the path's first segment where a host would go. The DSN still
// parses, driver.Open still succeeds, and the vault opened is a different
// file than the one dbPath names — e.g. dbPath "relvault.db" produces a
// DSN whose path is "/relvault.db", not "./relvault.db", with no error
// anywhere. Resolving a relative path against a working directory or a
// configured vault root is a decision this package refuses to make for
// the caller (this function "resolves nothing else" per Open's own doc
// comment) — buildDSN rejects it outright with ErrRelativeDBPath instead
// of guessing.
//
// The driver recognizes two distinct query-parameter containers, and they
// are NOT interchangeable:
//
//   - "_pragma" is repeatable; each occurrence is a PRAGMA statement, run
//     verbatim (e.g. "_pragma=journal_mode(wal)"). "_txlock" is not a
//     PRAGMA — SQLite silently ignores an unknown pragma, so putting it
//     here compiles, runs, and does nothing.
//   - "_txlock" is its own, single, top-level query parameter (never
//     repeated inside "_pragma") that sets the driver's own default
//     transaction mode.
//
// Design D3's order:
//
//  1. busy_timeout — a PRAGMA, set first. If any _pragma is present the
//     driver stops applying its own default 1-minute busy handler, so this
//     must be set explicitly or the vault ends up with no busy handler at
//     all.
//  2. journal_mode=wal — a PRAGMA. Readers never block the writer.
//  3. foreign_keys=on — a PRAGMA. Off by default per SQLite connection.
//  4. _txlock=immediate — the top-level parameter, NOT a PRAGMA. Takes the
//     write lock upfront instead of leaving BEGIN DEFERRED's read-then-
//     write upgrade exposed to an unretriable SQLITE_BUSY_SNAPSHOT; full
//     rationale in design.md D3 (openspec/changes/complete-harness).
func buildDSN(dbPath string) (string, error) {
	if !isAbsoluteVaultPath(dbPath) {
		return "", fmt.Errorf("%w: %q", ErrRelativeDBPath, dbPath)
	}

	q := url.Values{}
	q.Add("_pragma", "busy_timeout(5000)")
	q.Add("_pragma", "journal_mode(wal)")
	q.Add("_pragma", "foreign_keys(on)")
	q.Set("_txlock", "immediate")

	u := url.URL{
		Scheme:   "file",
		Path:     toURIPath(dbPath),
		RawQuery: q.Encode(),
	}
	return u.String(), nil
}
