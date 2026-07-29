// Package e2e holds L4 tests: each one compiles the real `nooma` binary and
// runs it as a subprocess, the only level permitted to touch the compiled
// artifact instead of package code. See docs/06-harness.md §3.
//
// This file carries no build tag on purpose — every _test.go file here is
// tagged `e2e`, and a directory whose only Go files are all build-tag-excluded
// fails `go build ./...` with "build constraints exclude all Go files". This
// file is what keeps that from happening (mirrors test/conformance/doc.go and
// test/integration/doc.go).
//
// L4 runs in CI only on push to main, never on every PR (docs/06-harness.md
// §6, §3) — see .github/workflows/main.yml's own comment for why that is a
// deliberate latency/cost tradeoff, not an oversight.
package e2e
