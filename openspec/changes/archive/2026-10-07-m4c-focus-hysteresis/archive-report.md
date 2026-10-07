# Archive report — `m4c-focus-hysteresis`

**Closed**: 2026-10-07. **Verify verdict**: PASS — 0 CRITICAL; the report's two warnings are
resolved below. **`main` at archive**: `0d9d3e7`, the merge of PR #285.
**Milestone**: the third of M4's six slices. `openspec/changes/m4-mirror-ui/proposal.md` stays
active for `m4d`, `m4e` and `m4f`.

## What shipped

`focus.Select` has its first production callers. A `brain.FocusKeeper`, built once per `serve`
process, holds the incumbent for both Kinds as one immutable snapshot behind an `atomic.Pointer`,
in memory only (I01). Two computations write it (owner ruling, 2026-10-07): Today, when a request
succeeds, and the morning digest, after its `Send` succeeds. A challenger displaces an incumbent
only by more than `hysteresis_margin` (I19). Relations feed adjacency through `RelationRepo.ByUnit`
and `weight.Edge.Strength`: each focus ranking reads its own Kind's incumbent; the digest's `Carry`
reads the union of both Kinds' members of the incumbent it loaded (P), re-keyed to trigger ids;
Today's pending-digest mirror reads the snapshot it is about to publish (P'), so `/ui` shows what
the next digest carries.

| PR | Branch | What | Impl+docs |
|---|---|---|---|
| #283 | `plan/m4c-focus-hysteresis` | spec, design, tasks; umbrella m4c rows | — |
| #284 | `feat/brain-focus-incumbent` | keeper, `Select` for both writers, per-Kind adjacency in Today, `check.focus.unavailable` | 310 |
| #285 | `feat/brain-focus-adjacency` | digest `Carry` union adjacency, mirror reads P', `sqlitetest.SeedEnergy`, `sqlitetest-tests-only` lint rule | 120 |

The pre-defined 1b cut (digest as second writer in its own PR) was not needed: PR 1 measured
under the 400-line ceiling.

## What it leaves behind, beyond the feature

| Gate | What it makes unexpressible |
|---|---|
| `TestBrain_DeclaresNoPackageLevelVar` (default-deny AST) | a process-wide incumbent in `internal/brain`; only `var _ T = …` and error sentinels pass |
| `TestServe_OneFocusKeeperSharedByTodayAndDigest` | a second keeper, a `nil` keeper, or a missing `wireToday`/`wireScheduler` call on the path to either writer |
| `TestI01_IncumbentDoesNotSurviveAFreshService` | an incumbent that outlives its keeper |
| depguard `sqlitetest-tests-only` | a production import of the test-only energy seed, which would be the energy writer `ports.StateRepo` deliberately lacks |
| decision-action vocabulary (forty-eight) | an unlisted `check.*` action |

## Cost, stated plainly

- Design: three Judgment Day rounds. Round 2 caught a CRITICAL — every hysteresis fixture had two
  candidates against a seven-slot cut, so nothing could be displaced and the tests could not
  fail. Fixture rules FX-H and FX-L came from it.
- PR 1: two rounds. Round 1 found a surviving mutant (publish moved after the `Surface` loop) and
  gate holes; both closed.
- PR 2: three rounds. The apply's raw-SQL seed was the repo's first `//nolint:depguard`; it was
  replaced by a store-side helper, then the helper's "tests only" comment was made a lint rule.
- The apply's mutation enumeration from the production diff found a mixed mutant the design had
  not listed (P' members over P's edges); a test now kills it.
- Test lines far exceeded the forecast (~1,997 for PR 1 against ~480), mostly the serve gate and
  digest focus tests.

## Verification

`sdd-verify` (engram `sdd/m4c-focus-hysteresis/verify-report`) at `0d9d3e7`: R1–R10 each mapped
to code and a passing test; every gate above re-proven in both directions in a throwaway worktree;
15 behavioural mutants killed; `make check-all` exit 0; `go vet -tags integration,e2e ./...` and
`go test -race` clean. The real binary served `GET /ui` twice, byte-identical, over an empty vault
— wiring and stability only; hysteresis on data is covered by `TestWireProactive_*` over a real
migrated vault.

Resolved report items:

- **WARNING 2** described a raw-SQL `//nolint:depguard` seed. It was read from stale apply notes:
  on `main` there is none — `cmd/nooma/serve_gate_test.go` calls `sqlitetest.SeedEnergy`.
- **WARNING 1** is a format note: apply-progress records TDD evidence as narrative per task, not
  as the strict-TDD table.

## Open owner-review items (not blocking)

- **OR1** — relations in m3e's uncertain band raise adjacency at full strength, the same as
  confirmed ones.
- **OR2** — `GET /ui` fails on a `ByUnit` or config read error, while the digest degrades to empty
  adjacency and records `check.focus.unavailable`.
- Spec R9 could state that the incumbent is published after `Send` and before the `Surface` loop
  (pinned by `TestDigest_PublishesBeforeSurface`).

## Next

`m4d-graph` waits on ADR-0019 being `Accepted`; per proposal Q4, `m4e` may run first if it is
still `Proposed`.
