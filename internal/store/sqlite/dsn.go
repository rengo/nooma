package sqlite

import "net/url"

// buildDSN builds the file: URI driver.Open expects from a plain
// filesystem path. It is built with net/url rather than string
// concatenation — a vault path with a space, a '?' or a '#' would
// otherwise be silently truncated or reinterpreted as query parameters
// (design D3) — and it carries every operational setting this store
// requires, in the order design D3 fixes.
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
//  4. _txlock=immediate — the top-level parameter, NOT a PRAGMA. BEGIN
//     DEFERRED plus a read-then-write upgrade can return
//     SQLITE_BUSY_SNAPSHOT, which the busy handler does not retry; BEGIN
//     IMMEDIATE takes the write lock upfront instead.
func buildDSN(dbPath string) string {
	q := url.Values{}
	q.Add("_pragma", "busy_timeout(5000)")
	q.Add("_pragma", "journal_mode(wal)")
	q.Add("_pragma", "foreign_keys(on)")
	q.Set("_txlock", "immediate")

	u := url.URL{
		Scheme:   "file",
		Path:     dbPath,
		RawQuery: q.Encode(),
	}
	return u.String()
}
