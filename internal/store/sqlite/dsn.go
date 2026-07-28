package sqlite

import (
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"runtime"
	"strings"
)

// ErrRelativeDBPath is the error buildDSN returns when dbPath is not
// absolute under the pathStyle it was checked against. See buildDSN's doc
// comment for why a relative path is rejected rather than resolved.
var ErrRelativeDBPath = errors.New("sqlite: vault path must be absolute")

// pathStyle names which operating system's path shape rules apply when
// buildDSN decides whether a string is absolute and how to turn it into a
// "file:" URI path.
//
// This exists because "what does an absolute path look like" is
// environment-dependent in exactly the same sense the current instant is
// (nooma-core: internal/core never calls time.Now(), it takes a time.Time
// through ports.Clock). A path's shape does not carry which OS wrote it —
// on POSIX, "C:/data/vault.db" is a perfectly ordinary *relative* path
// (backslash is a legal filename byte, and a directory can be named
// "C:"), while on Windows the same string is the standard *absolute* form.
// Deciding absoluteness by pattern-matching the string's shape, independent
// of which platform is asking, is therefore ambiguous by construction: any
// rule permissive enough to recognize a genuine Windows path also accepts
// POSIX-relative paths that merely happen to look like one. That ambiguity
// is not hypothetical — it is exactly the residual defect a table-driven
// regression test in dsn_test.go once caught silently passing through.
//
// The fix is injection, not a smarter pattern: pathStyle is a plain
// parameter buildDSN takes, resolved from the real environment in exactly
// one place (pathStyleForGOOS, below) for production, and set directly by
// each dsn_test.go case for tests. This keeps both branches testable from a
// single Linux runner — the same reason filepath.IsAbs is not used here:
// filepath.IsAbs only recognizes the path shape native to the GOOS it was
// compiled for, so a Linux build of this package could never exercise the
// Windows branch (and this package is cross-compiled for Windows, ADR-0001,
// without ever running a Windows binary in this repo's own CI) — but unlike
// pattern-guessing, injection never lets one style's shape leak into the
// other's verdict.
type pathStyle int

const (
	// posixStyle: absolute means a leading "/". A backslash is an ordinary
	// filename character — no translation, no drive-letter recognition.
	posixStyle pathStyle = iota
	// windowsStyle: absolute means a drive-letter prefix ("C:\" or "C:/").
	windowsStyle
)

// pathStyleForGOOS resolves the pathStyle production code uses, from the
// operating system this binary was actually built for. This is the single
// named place the environment enters buildDSN's absoluteness decision —
// everywhere else in this package takes pathStyle as a plain parameter, so
// a reader auditing "where does the OS come from" has exactly one place to
// look, the same discipline internal/brain applies to ports.Clock.
func pathStyleForGOOS() pathStyle {
	if runtime.GOOS == "windows" {
		return windowsStyle
	}
	return posixStyle
}

// windowsDriveAbs matches a Windows drive-letter absolute path such as
// `C:\Users\pablo` or `C:/Users/pablo`.
var windowsDriveAbs = regexp.MustCompile(`^[A-Za-z]:[\\/]`)

// isAbsoluteVaultPath reports whether p is absolute under style.
//
// Under posixStyle, only a leading "/" counts. "C:/data/vault.db" and
// `C:\data\vault.db` are NOT recognized here — on POSIX they are ordinary
// relative paths (see pathStyle's doc comment), so they fall through to
// ErrRelativeDBPath like any other relative string.
//
// Under windowsStyle, only the drive-letter form counts ("C:\..." or
// "C:/..."). A bare leading "/" or "\" (Go's filepath.IsAbs also rejects
// this on windows: it is "rooted" but not absolute, since it names no
// volume) is deliberately NOT accepted — this function only recognizes
// shapes it can also turn into a correct file: URI, and a driveless root
// has no drive letter to put in that URI's path.
//
// A UNC path ("\\server\share\vault.db") is also not recognized: it does
// not match the drive-letter pattern, so it falls through to
// ErrRelativeDBPath the same way. That is deliberate, not an oversight — a
// UNC share is a valid absolute Windows path, but this store does not
// support network-share vaults, and representing one correctly needs a
// file: URI with a host component ("file://server/share/..."), a different
// shape than every other DSN this function builds (always an empty
// authority). Accepting the string here without also deciding how the DSN
// carries that host would silently produce a wrong URI instead of
// rejecting a shape this package does not yet support.
func isAbsoluteVaultPath(p string, style pathStyle) bool {
	switch style {
	case windowsStyle:
		return windowsDriveAbs.MatchString(p)
	default:
		return strings.HasPrefix(p, "/")
	}
}

// toURIPath converts an already-confirmed-absolute dbPath to the path
// component a "file:" URI expects.
//
// Under posixStyle the path is already forward-slash-shaped (isAbsoluteVaultPath
// guaranteed a leading "/") and needs no translation.
//
// Under windowsStyle, backslashes become forward slashes and a leading "/"
// is prepended ahead of the drive letter, producing the standard
// "file:///C:/Users/..." shape (RFC 8089's file-absolute form) that SQLite
// accepts.
func toURIPath(dbPath string, style pathStyle) string {
	if style != windowsStyle {
		return dbPath
	}
	return "/" + strings.ReplaceAll(dbPath, `\`, "/")
}

// buildDSN builds the file: URI driver.Open expects from a plain,
// absolute filesystem path, judged absolute under style (see pathStyle's
// doc comment for why the style is a parameter instead of guessed from
// dbPath's own shape). It is built with net/url rather than string
// concatenation — a vault path with a space, a '?' or a '#' would
// otherwise be silently truncated or reinterpreted as query parameters
// (design D3) — and it carries every operational setting this store
// requires, in the order design D3 fixes.
//
// dbPath MUST be absolute under style. This is checked structurally, not
// documented as a warning (CLAUDE.md non-negotiable #7): url.URL{Path: p}
// treats a path that does not start with "/" as ambiguous with the URI's
// authority component, and net/url.URL.String() resolves that ambiguity by
// silently writing the path's first segment where a host would go. The DSN
// still parses, driver.Open still succeeds, and the vault opened is a
// different file than the one dbPath names — e.g. dbPath "relvault.db"
// produces a DSN whose path is "/relvault.db", not "./relvault.db", with no
// error anywhere. Resolving a relative path against a working directory or
// a configured vault root is a decision this package refuses to make for
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
func buildDSN(dbPath string, style pathStyle) (string, error) {
	if !isAbsoluteVaultPath(dbPath, style) {
		return "", fmt.Errorf("%w: %q", ErrRelativeDBPath, dbPath)
	}

	q := url.Values{}
	q.Add("_pragma", "busy_timeout(5000)")
	q.Add("_pragma", "journal_mode(wal)")
	q.Add("_pragma", "foreign_keys(on)")
	q.Set("_txlock", "immediate")

	u := url.URL{
		Scheme:   "file",
		Path:     toURIPath(dbPath, style),
		RawQuery: q.Encode(),
	}
	return u.String(), nil
}
