// Package scheduler runs in-process cron and ADR-0009's boot catch-up.
//
// What this package owns: goroutines (one for the daily cron loop, one for
// the boot catch-up delay), a timer seam over time.After (timer.go), and a
// process-level log — an io.Writer naming a fire, a skip, an abort, or a
// completed pass's refused units (design §5.4). Nothing else.
//
// What this package does not own: any decision. Every `if` over a
// duration, an hour-of-day, or a *bool lives in
// internal/core/consolidation, never here — "is a catch-up due?", "is the
// gate open?", and "when does the cron next fire?" are pure functions this
// package only asks and obeys (design §2, §3.1 D1). This is enforced
// structurally, not by convention: core-purity denies this package from
// reaching internal/core at all in the wrong direction, the
// scheduler-boundary depguard rule (.golangci.yml) denies this package
// internal/store, database/sql, internal/providers, internal/httpapi and
// net/http, and test/conformance/scheduler_boundary_scan_test.go scans for
// a re-derived duration literal. The scheduler reaches the vault only
// through brain.ConsolidateService — never by opening a connection itself.
//
// See docs/06-harness.md §1 for the dependency rule.
package scheduler
