// Package sqlite opens a vault, migrates it, reads no domain row.
//
// It is the only package in the tree that imports database/sql or the
// ncruces/go-sqlite3 driver — .golangci.yml's sqlite-containment rule makes
// that boundary a lint error everywhere else. See docs/06-harness.md §1 and
// design.md D1/D7 (openspec/changes/complete-harness/design.md) for the full
// rationale.
package sqlite
