package sqlite

import "net/url"

// buildDSN builds the file: URI driver.Open expects from a plain
// filesystem path. It is built with net/url rather than string
// concatenation — a vault path with a space, a '?' or a '#' would
// otherwise be silently truncated or reinterpreted as query parameters
// (design D3) — and it carries every operational PRAGMA this store
// requires, in the order design D3 fixes:
//
//  1. busy_timeout — set first. If any _pragma is present the driver stops
//     applying its own default 1-minute busy handler, so this must be set
//     explicitly or the vault ends up with no busy handler at all.
//  2. journal_mode=wal — readers never block the writer.
//  3. foreign_keys=on — off by default per SQLite connection.
//  4. _txlock=immediate — BEGIN DEFERRED plus a read-then-write upgrade
//     can return SQLITE_BUSY_SNAPSHOT, which the busy handler does not
//     retry; BEGIN IMMEDIATE takes the write lock upfront instead.
func buildDSN(dbPath string) string {
	q := url.Values{}
	q.Add("_pragma", "busy_timeout(5000)")
	q.Add("_pragma", "journal_mode(wal)")
	q.Add("_pragma", "foreign_keys(on)")
	q.Add("_pragma", "_txlock(immediate)")

	u := url.URL{
		Scheme:   "file",
		Path:     dbPath,
		RawQuery: q.Encode(),
	}
	return u.String()
}
