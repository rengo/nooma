// Package integration holds L3 tests: each one starts from a real, empty
// temporary SQLite vault and is the only level permitted to open a live
// SQLite connection. See docs/06-harness.md §3.
//
// This file carries no build tag on purpose — every _test.go file here is
// tagged `integration`, and a directory whose only Go files are all
// build-tag-excluded fails `go build ./...` with "build constraints exclude
// all Go files". This file is what keeps that from happening.
package integration
