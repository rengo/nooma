// Package conformance holds L2 tests: invariants from
// docs/02-cognitive-core.md, checked against real (untagged) or future
// (pendingimpl-tagged) domain symbols, never against a live SQLite
// connection. See docs/06-harness.md §3 and design.md §3/§8
// (openspec/changes/complete-harness/design.md).
//
// This file exists so the package always carries at least one untagged
// file — go build ./... and the untagged `make test` fail with "build
// constraints exclude all Go files" otherwise (design §8.6, spec R7.2).
package conformance
