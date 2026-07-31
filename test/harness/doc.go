// Package harness holds regression tests for the CI harness's own gate
// scripts (scripts/core-coverage.sh, scripts/docs-sync.sh). Both scripts
// gate real merges, but — unlike the now-retired pending-red gate, which
// re-proved itself on every CI run by construction — their real Actions
// history has only ever exercised their vacuous/trivial branches: the coverage job
// while internal/core has no statements, and the docs-sync job on PRs that
// never touch internal/core/**. Their FAIL branches have never run for
// real. These tests exercise them directly, against synthetic fixtures, so
// that guarantee does not rest on one-off manual probes that leave no
// artifact. Untagged, so they run under `make check` and cannot rot
// silently. See docs/06-harness.md §6 and §8 point 6.
package harness
