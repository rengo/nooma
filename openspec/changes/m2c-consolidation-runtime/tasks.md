# Tasks — M2 Phase C: the consolidation runtime

Implementation task list for `m2c-consolidation-runtime`, derived from `spec.md` (R0–R6.4, eight
sections) and `design.md` (§1–§14, thirteen forecast rows), both read in full before this
document. Design ran the re-derivation `spec.md`'s own Scope boundary grants it authority to run
(`spec.md` line 35-36), superseding the proposal's six-PR row: **fourteen links**, not six.

Chain strategy **`stacked-to-main`**, delivery strategy chained PRs — owner rulings taken
2026-08-09, restated here so they bind: each link merges to `main` in order, as M1's own chain and
`m2a`/`m2b`'s did; no `size:exception` is needed anywhere in the chain (`design.md` §13's tallest
link, PR 5, sits at ~330 impl+docs, 0.83× the ceiling).

**Migration 0003 is approved** — one column, `current_state.source TEXT NOT NULL DEFAULT 'user'`
(`design.md` §3.2 option A). Its full verified cost is carried as explicit line items in PR 4
below, not summarized: it is the first migration since the harness's own measurement rule started
counting, and every touched site is named rather than described.

**Strict TDD is active** (`CLAUDE.md` non-negotiable #4) and **has no Makefile gate** —
`scripts/pending-red.sh` was retired in `714934e` and does not exist. Every behavioral task below
states the two-commit shape `m2a`/`m2b` already established: **commit 1** is the test plus a stub
with the final signature returning zero values (the suite compiles, the assertion fails — red for
the right reason); **commit 2** is the implementation (green). Where a test's own assertions
cannot be red against a missing symbol — an L2 doc/DDL pin, a tree scan over code that already
compiles within the same PR, a repocontract case exercised against a fake that already
implements the method by the time the case is written — this document says so explicitly at the
task, per this project's own convention (`m2a` C9) against claiming a red step that cannot occur.

Every PR runs `make check-all` before opening, not `make check` — the L3 suites and the
`integration` build tag that carry most of this change's own proof live behind the gate `make
check` does not run. `docs-sync.yml` fires on `internal/core/`; only PR 10a touches that
directory (`design.md` §10.2, one file), so it is the only PR in this chain that needs a genuine
`docs/02-cognitive-core.md` delta from `docs-sync`'s own perspective — PR 4 and PR 10b also amend
doc 02, per `design.md` §10.3, but PR 4's amendment is not gated by `docs-sync` (no
`internal/core/` file in that PR) and is done anyway, per non-negotiable #1.

For every invariant this change owns — I03 (widened surface), I05 (structural half), I11
(behavioural half), I12 (both directions), I24 (structural, both legs) — the conformance test is
its own task, ordered strictly before the task that implements what satisfies it, per
`nooma-testing` hard rule 3. **I24's harness row already exists**: `docs/06-harness.md` §4 line
196 carries it, added when `m2a` defined the invariant (`design.md` §1's own verification, line
71). This document does not re-add it; PR 1's task 1.3 is a verification that the row is present
and correctly worded, not an edit — `nooma-testing`'s step 2 ("add it to the table... and only
then write the test") is satisfied by the row already being there, so this document states that
rather than manufacturing a doc edit with nothing to change. The same holds for I03, I05, I11 and
I12 (`design.md` §1 line 71, §10.3's closing sentence): `docs/06-harness.md` is untouched by this
entire change.

**Chain-merge verification is a checklist item at every merge point, not a footnote.** After
merging each PR below and before opening the next, per `nooma-pr`'s "Merging a Chain" section:

1. `git ls-remote --heads origin <merged-branch>` returns nothing (the branch is gone).
2. `gh pr view <next-pr> --json baseRefName` names `main`, not the branch just merged.

Both are stated as their own numbered task inside every PR's Verify block below, not assumed.

---

## Handoffs inherited from `m2a`/`m2b`, and what this document does with each

`spec.md` §7 already carries the full disposition table (fifteen rows) — this document does not
restate it. What follows is only the subset that turns into a task in this change, cited by the PR
that discharges it: `ConfigRepo`'s nil-sentinel shape → PR 3 (tasks 3.9–3.14); `UnitRepo`'s weight
write taking `weight.Boost` → PR 1 (tasks 1.4–1.9); the live-count-by-type method returning `int`
→ PR 1 (tasks 1.10–1.11); `SelfModelRepo`'s upsert-by-`topic_key` + `ActiveBeliefs` → PR 3 (tasks
3.1–3.8); belief embeddings in memory, no table → PR 10b (task 10b.7); `brain/capture.go:485`
adopting `relation.CreatedBySystem` → PR 9 (task 9.9); `current_state`'s one append-only row per
`LoadFinding` → PR 4 (migration) + PR 11 (write, tasks 11b.1–11b.4); `goal_stagnation_days`'s two
schema homes (`m2b` §9 Q3) → PR 3 (tasks 3.13–3.14, discharged); `since` read once, one field on
the pass context (`m2b` §9 Q8) → PR 7a (tasks 7a.5–7a.6); I24 unenforced until the port has a
write path (`m2a` C6) → PR 1 (task 1.9) + PR 5 (task 5.4, the single-statement SQL). `m2a` C17,
C19, C29 are **not** discharged by this change — `spec.md` §7's table already says so and it is
restated at the end of this document (§ "Handoffs `m2c` leaves open") so the archive does not lose
it a second time.

---

## PR 1 — `feat/ports-unit-weight-count` (~170 impl+docs / ~460 test, est.)

Depends on nothing outside this change. Goes first — every later PR's fakes and contract suite
build on this one's `repocontract`/`memrepo` shape. Ships `ports.UnitRepo`'s two new methods
(design §3.1, §4.1), I24's two reflection legs, and the two depguard rules design §10.1 found
missing.

- [x] **1.1** Commit 1 (RED): `test/support/repocontract/unitrepo.go` (extend) — add
      `RunApplyBoosts(t, newRepo)` cases: (a) for ≥3 units with distinguishable
      `(Weight, LastTouchedAt)` per unit, `ApplyBoosts` writes each unit's pair from its own
      `weight.Boost`, never a cross-unit zip; (b) a `weight.Boost` naming a non-existent unit id
      returns `ports.ErrUnitNotFound` and leaves every other row in the same call untouched; (c) a
      `NaN`/`+Inf`/`-Inf` `Weight` in any one `Boost` of the batch returns `ErrNonFiniteWeight` and
      writes nothing at all, including finite `Boost`s in the same call; (d) `at` and
      `Boost.LastTouchedAt` land in `updated_at` and `last_touched_at` respectively, fixtured with
      **different** instants so a swapped-argument implementation fails (`UpdateEventAt`'s own
      fixturing pattern).
      **Red**: `undefined: ports.ErrNonFiniteWeight`, `ApplyBoosts` missing from `ports.UnitRepo`,
      `memrepo`'s `UnitRepo` fake does not implement it — package does not compile.
      Stub (same commit): `ApplyBoosts(ctx context.Context, boosts []weight.Boost, at time.Time)
      error` added to `ports.UnitRepo`; `var ErrNonFiniteWeight = errors.New(...)`; `memrepo`'s
      fake gets a zero-value implementation (`return nil`) — compiles, case (a)'s pairing
      assertion fails first (no unit was actually written).
      Requirement: spec R1.1, R1.4; design §3.1(a)–(c), §5.2.
      **Apply note (honest gap, not smoothed over)**: `test/support/repocontract/repocontract.go`
      was renamed to `unitrepo.go` first (it held only `RunUnitRepo` already — a legacy name that
      predates the per-repository file convention `relationrepo.go` and its siblings established),
      then extended. The wiring test that actually runs `RunApplyBoosts` against the `memrepo` fake
      (`test/conformance/unitrepo_memrepo_test.go`) was watched red via a throwaway, uncommitted
      file, then committed together with task 1.2's GREEN implementation rather than in this RED
      commit — this commit alone, checked out in isolation, does not carry a permanent failing
      test in the tree. The red state itself was genuinely observed (see PR description).
      Also, widening `ports.UnitRepo` broke `internal/store/sqlite.UnitRepo`'s existing
      `var _ ports.UnitRepo = (*UnitRepo)(nil)` compile-time assertion — undocumented by
      `design.md`'s own package layout (§6.1), which assigns `ApplyBoosts`'s real SQL body to
      PR 5 and says nothing about PR 1 touching `internal/store/sqlite`. A minimal placeholder
      (`internal/store/sqlite/unitrepo.go`, returns an explicit "not implemented until PR 5"
      error) was added in this same commit to keep `main` buildable, and
      `testdata/schema/store_api.golden` regenerated accordingly (`make store-api-golden`) since
      the placeholder is a new exported method. See the PR description for the full reasoning —
      this affects PR 2 and PR 3's own interface widenings the same way and should be resolved
      before they land.
- [x] **1.2** Commit 2 (GREEN): implement `ApplyBoosts` in `test/support/memrepo`'s `UnitRepo`
      fake — per-unit write inside the fake's own map, all-or-nothing on a missing id (no partial
      writes for the surviving entries when one is missing — the fake mirrors the sqlite
      transaction's semantics, not merely the port's error contract), refuse non-finite `Weight`
      before touching any row.
      Verify: `go test ./test/support/... ./test/conformance/...`; `golangci-lint run`.
      Requirement: spec R1.1, R1.4; design §3.1(e).
- [x] **1.3** Verification, not an edit: confirm `docs/06-harness.md` §4's I24 row (line 196,
      *"A weight write moves `weight` and `last_touched_at` together; neither is written alone"*)
      is present and its wording matches what task 1.4's test proves. This is `nooma-testing`
      step 2 satisfied by inheritance, not a new row — recorded as its own task so the ordering
      (`row → test → implementation`) is visible in this document rather than assumed.
      Requirement: `nooma-testing` hard rule 3 (ordering); design §1 (I24 already has a §4 row).
      Verified by direct read: `docs/06-harness.md:196` carries the row verbatim as quoted above.
      No edit made.
- [x] **1.4** Commit 1 (RED): `test/conformance/i24_unitrepo_weight_write_test.go` (new) — two
      legs in one file, per design §3.1(d):
      **Leg 1**: no `ports.UnitRepo` method declares a `float64` parameter, after unwrapping
      slice/map/pointer/array element types. **Not a missing-symbol red**: true today with zero
      methods taking a float at all, and stays true once `ApplyBoosts` exists — disclosed per
      `m2a` C9 rather than claimed as a red step.
      **Leg 2**: exactly one `ports.UnitRepo` method's parameter list mentions `weight.Boost`
      (bare or sliced). **Genuinely red before task 1.1/1.2 land**: zero methods do today, the
      test asserts exactly one — 0 ≠ 1 fails for the right reason.
      Requirement: spec R1.1; design §3.1(d), leg 1 and leg 2.
      **Apply note**: executed *before* task 1.1's interface stub, not after, despite the numbering
      above — leg 2's own text claims it is "genuinely red before task 1.1/1.2 land", which is only
      observably true if it is actually run before 1.1/1.2 land. Run in that order: leg 1 passed
      trivially (disclosed non-red, as its own text says), leg 2 failed with `0 != 1` for the exact
      reason stated. `git log` on this branch therefore has this commit *before* task 1.1's.
- [x] **1.5** Commit 2 (GREEN, structural — no new implementation code, only the interface change
      from task 1.1): confirm both legs pass once `ApplyBoosts` exists. If leg 1 or leg 2 fails,
      the interface shape is wrong — this task is the checkpoint, not a place to add code.
      Verify: `go test ./test/conformance/... -run TestI24`.
      Requirement: spec R1.1; design §3.1(d).
      Verified: `go test ./test/conformance/... -run TestI24` — both subtests PASS.
- [x] **1.6** `test/conformance/i24_unitrepo_weight_write_test.go` (continued) — leg 3
      (R3.4/design §3.1(d) row 3, the SQL-level proof) cannot run until PR 5 ships the sqlite
      implementation. State that explicitly in this file's own doc comment now, as a forward
      reference, rather than let leg 3 appear silently missing from PR 1: *"Leg 3 (the two-column
      SQL assignment appears in exactly one method) is `test/conformance/i05_...` in PR 5, not
      here — it needs `internal/store/sqlite` source text to scan."*
      Requirement: design §3.1(d), row 3 (forward reference only).
- [x] **1.7** doc comment: `internal/ports/unitrepo.go`'s package/interface doc comment gains one
      paragraph stating `ApplyBoosts`'s three packed decisions (§3.1(a)–(c)) — the parameter type,
      the batch shape, and `at` as a separate parameter from `Boost.LastTouchedAt` — each with the
      one-line reason `design.md` §3.1 gives, so a reader of the port does not have to find the
      design doc to understand why the signature looks the way it does.
      Requirement: design §3.1.
- [x] **1.8** Commit 1 (RED): `test/support/repocontract/unitrepo.go` (extend) —
      `RunCountLiveByType(t, newRepo)`: a fixture with live (`pool`) and non-live (`archived`,
      `superseded`, `incomplete`) units across ≥2 `unit.Type` values; `CountLiveByType` returns the
      count of live units of the requested type only, `0` for a type with none live.
      **Red**: `undefined: ports.UnitRepo.CountLiveByType` — package does not compile.
      Stub: `CountLiveByType(ctx context.Context, t unit.Type) (int, error)` added to the
      interface; `memrepo` fake returns `(0, nil)` — compiles; the live-fixture case expects a
      positive count, stub returns `0`, fails first.
      Requirement: spec R1.2; design §4.1.
      Same build-green consequence as task 1.1: `internal/store/sqlite/unitrepo.go` gained a
      matching placeholder for `CountLiveByType`, and the store-api golden was regenerated again.
      The red state was watched via a throwaway wiring file; the committed wiring test landed with
      task 1.9's GREEN commit, same disclosed gap as task 1.1's note above.
- [x] **1.9** Commit 2 (GREEN): implement `CountLiveByType` in the `memrepo` fake — iterate and
      count, never build and return a slice the caller would count itself (owner ruling 6's own
      point, kept true at the fake too).
      Verify: `go test ./test/support/...`; `golangci-lint run`.
      Requirement: spec R1.2; design §4.1.
- [x] **1.10** `test/conformance/i24_unitrepo_weight_write_test.go` (extend, or a shared reflection
      helper) — no exported `ports.UnitRepo` method both accepts a `unit.Type` and returns
      `[]unit.Unit` (spec R1.2's second MUST — the name-carries-what-it-counts discipline, checked
      structurally alongside I24's own reflection sweep since both are shape checks over the same
      interface).
      Requirement: spec R1.2.
- [x] **1.11** Commit 1 (RED): `.golangci.yml` — add the `ports-purity` and `brain-boundary`
      depguard rules exactly as design §10.1 specifies (allow-list for `internal/ports`; deny-list
      for `internal/brain` against `internal/store`, `internal/providers`, `internal/httpapi`,
      `internal/channels`). **Red, recorded as a manual verification rather than a Go test**: add
      a temporary `internal/store/sqlite` import to any file under `internal/brain/`, run
      `golangci-lint run`, confirm it fails with `brain-boundary`'s own `desc` message, then revert
      the temporary import — the same "record the break" discipline `schema_golden_anchor_test.go`
      already uses for its own gate.
      Requirement: spec R0.1 (the claim design §10.1 found false); design §10.1.
      Verified as described: `internal/brain/zz_tmp_boundary_check.go` was added importing
      `internal/store/sqlite`, `golangci-lint run ./internal/brain/...` failed with exactly
      `brain-boundary`'s own `desc` ("brain reaches persistence only through a port —
      docs/06-harness.md §1"), then the file was deleted. The mirror attempt for `ports-purity`
      (a temporary `internal/store/sqlite` import inside `internal/ports`) hit a Go import cycle
      before `depguard` could even evaluate it — `internal/store/sqlite` already imports
      `internal/ports` — a stronger, compiler-level protection for that specific pair, noted here
      rather than claimed as a `depguard` red.
- [x] **1.12** Commit 2 (GREEN, no code — this is the confirmation the rule doesn't fire on
      today's tree): `golangci-lint run` passes clean with the two new rules in place, over
      `internal/ports` and `internal/brain` as they stand at the end of PR 1.
      Verify: `golangci-lint run`.
      Requirement: design §10.1.
- [x] **1.13** Purity/coverage: `golangci-lint run` (`depguard`'s `ports-purity` — this PR's new
      `internal/ports` files import only stdlib + `internal/core/weight`/`internal/core/unit`);
      `go vet ./...`.
      Both clean.
- [x] Verify (PR-level): `make check-all` — green (lint, vet, race unit+integration tests,
      schema-golden-clean, 100% core coverage, seven-target cross-compile, e2e).
      **Diff does NOT match the file list above** — disclosed rather than silently widened. Actual
      diff: `internal/ports/unitrepo.go`, `test/support/memrepo/units.go`,
      `test/support/repocontract/unitrepo.go` (renamed from `repocontract.go`),
      `test/conformance/i24_unitrepo_weight_write_test.go`,
      `test/conformance/unitrepo_memrepo_test.go`, `.golangci.yml`, plus two files this list did
      not anticipate: `internal/store/sqlite/unitrepo.go` (PR-5 placeholder methods, see task 1.1's
      note) and `testdata/schema/store_api.golden` (regenerated because of that placeholder).
      111 impl+docs lines (well under the ~170 estimate and the 400 ceiling, 0.28×), 474 test
      lines.
      **Chain-merge check 1**: PR #158 merged 2026-08-09T23:19:15Z (`gh pr view 158 --json
      state,mergedAt` confirms `state: MERGED`). `git ls-remote --heads origin
      feat/ports-unit-weight-count` returns nothing — the branch is gone. **Performed, by PR 2's
      own executor, updating this stale line rather than leaving it claiming "not yet performed"
      after the merge had already happened.**
      **Chain-merge check 2**: PR 2 (`feat/ports-unit-relation-reads`) is opened against `main` —
      confirmed at PR-open time below.

---

## PR 2 — `feat/ports-unit-relation-reads` (~150 impl+docs / ~400 test, est.)

Depends on PR 1 (widens the same `test/support/repocontract`/`memrepo` shape; no direct call into
PR 1's new methods). Ships `ports.UnitRepo.IncompleteOlderThan`/`LiveDecayStates`,
`ports.RelationRepo.Evidence`/`ExistingPairs` (design §4.1, §4.2).

- [x] **2.1** Commit 1 (RED): `test/support/repocontract/unitrepo.go` (extend) —
      `RunIncompleteOlderThan(t, newRepo)`: an `incomplete` unit older than `cutoff` is returned; one
      younger is not; a `pool`/`archived`/`superseded` unit is never returned regardless of age
      (I02's exception is named, not general).
      **Red**: `undefined: ports.UnitRepo.IncompleteOlderThan`, `undefined:
      consolidation.Incomplete` reference in the port signature — package does not compile.
      Stub: `IncompleteOlderThan(ctx context.Context, cutoff time.Time)
      ([]consolidation.Incomplete, error)`; `memrepo` returns `(nil, nil)` — compiles; the
      older-than-cutoff fixture expects one result, stub returns none, fails first.
      Requirement: spec R5.1; design §4.1 (`IncompleteOlderThan` row).
      **Verified as described, genuinely, both flavors**: `go build ./test/support/...` failed
      with exactly `repo.IncompleteOlderThan undefined` (and the sibling `LiveDecayStates` error
      from task 2.3, added in the same commit) before the port stub existed — a real
      package-does-not-compile red, not a throwaway file. After the stub and the wiring test
      (`test/conformance/unitrepo_memrepo_test.go`, committed in this same RED commit rather than
      deferred to GREEN as PR 1's task 1.1 disclosed doing) landed, `go test -run
      TestUnitRepo_MemRepo_IncompleteOlderThan` failed on the assertion exactly as predicted:
      `IncompleteOlderThan(...) = [], want exactly [incomplete-older]`.
      Widening `ports.UnitRepo` again broke `internal/store/sqlite.UnitRepo`'s compile-time
      assertion, same as PR 1's task 1.1 — a matching placeholder was added in the same commit
      (see task 2.3's note for the shared file), and `testdata/schema/store_api.golden` was
      regenerated.
- [x] **2.2** Commit 2 (GREEN): implement `IncompleteOlderThan` in the `memrepo` fake — filter by
      `status = incomplete` and `CreatedAt < cutoff` only; the port's own doc comment states this
      is the one deliberate non-live read in M2 and names the exception explicitly (I02).
      Verify: `go test ./test/support/...`.
      Requirement: spec R5.1; design §4.1.
- [x] **2.3** Commit 1 (RED): `test/support/repocontract/unitrepo.go` (extend) —
      `RunLiveDecayStates(t, newRepo)`: returns `pool`-status units only, carrying the five decay
      fields (`consolidation.Cold`'s shape); no `unit.Unit`-shaped value anywhere in the return —
      asserted by checking the returned type is `[]consolidation.Cold`, not by inspecting field
      count.
      **Red**: `undefined: ports.UnitRepo.LiveDecayStates` — package does not compile.
      Stub: `LiveDecayStates(ctx context.Context) ([]consolidation.Cold, error)`; `memrepo` returns
      `(nil, nil)` — compiles; a live-pool fixture expects ≥1 result, fails first.
      Requirement: design §4.1 (`LiveDecayStates` row), consumed by `archive`/`connect`/`derive`.
      **Verified together with task 2.1** (one commit, one `go build` failure covering both
      undefined methods, one `go test` run covering both wiring-test assertion failures):
      `LiveDecayStates() = [], want exactly one entry`.
      `internal/store/sqlite/unitrepo.go` gained matching placeholders for both
      `IncompleteOlderThan` and `LiveDecayStates` (plain errors, "not implemented until PR 5", no
      sentinel — no caller exists today) in this same commit, and `make store-api-golden` was run
      once for both new methods together.
- [x] **2.4** Commit 2 (GREEN): implement `LiveDecayStates` in the `memrepo` fake.
      Verify: `go test ./test/support/...`.
      Requirement: design §4.1.
- [x] **2.5** Doc comment, `internal/ports/unitrepo.go`: state the duplicated-predicate risk design
      §4.1 names explicitly — `IncompleteOlderThan`'s `cutoff` is a **bound**, computed by the
      caller from `consolidation.IncompleteExpiryHours`, never the decision itself; `brain` must
      not derive it from a literal. This doc comment is what task 8's conformance test (PR 8) will
      pin against.
      Requirement: design §4.1 ("the cutoff duplicates a predicate" note).
      Landed inside task 2.1's own RED commit (the method's doc comment is written alongside its
      signature, not as a separate edit) rather than as its own commit — same paragraph also
      states `LiveDecayStates`'s three-call/never-cached guarantee (design §4.1) so both of
      §4.1's two "decisions, not details" paragraphs are on the port, not only in the design doc.
- [x] **2.6** Commit 1 (RED): `test/support/repocontract/relationrepo.go` (extend) —
      `RunEvidence(t, newRepo)`: a fixture with ≥2 relations, each endpoint carrying a distinct
      `last_touched_at`; `Evidence` returns every relation joined to **both** endpoints'
      `last_touched_at` in one read (no zip in the caller).
      **Red**: `undefined: ports.RelationRepo.Evidence`, `undefined:
      consolidation.RelationEvidence` in the signature — package does not compile.
      Stub: `Evidence(ctx context.Context) ([]consolidation.RelationEvidence, error)`; `memrepo`
      returns `(nil, nil)` — compiles; fails on the non-empty fixture.
      Requirement: spec R3.5; design §4.2.
      **Apply note (not anticipated by this task's own text)**: `RunEvidence`'s fixture needs each
      relation's two endpoints to carry *distinct* `last_touched_at` values, and
      `RelationHarness.EnsureUnit` gives no implementation a way to set one — it only makes an id a
      valid endpoint. `RelationHarness` gained a fourth method, `SetLastTouchedAt(t, id, at)`,
      implemented for real (not a PR-5 placeholder — it is an ordinary `units.last_touched_at`
      `UPDATE`/map-write, unrelated to `Evidence`'s own join logic) in both `memrepo.Relations` and
      the existing `internal/store/sqlite/relationrepo_integration_test.go` harness, which this PR
      therefore also touches (not on the task's own anticipated file list, disclosed here and in
      the PR-level Verify block below).
      **Verified as described, genuinely**: `go build ./...` failed with `repo.Evidence undefined`
      (and the sibling `ExistingPairs` error from task 2.8, added in the same commit) before the
      port stub existed; after the stub and wiring test landed, `go test -run
      TestRelationRepo_MemRepo_Evidence` failed on the assertion: `Evidence() returned 0 entries,
      want 2`.
      Widening `ports.RelationRepo` broke `internal/store/sqlite.RelationRepo`'s compile-time
      assertion — a matching placeholder was added in this same commit (see task 2.8's note), and
      `testdata/schema/store_api.golden` was regenerated again.
- [x] **2.7** Commit 2 (GREEN): implement `Evidence` in the `memrepo` fake.
      Verify: `go test ./test/support/...`.
      Requirement: spec R3.5; design §4.2.
- [x] **2.8** Commit 1 (RED): `test/support/repocontract/relationrepo.go` (extend) —
      `RunExistingPairs(t, newRepo)`: a relation stored `a→b` returns `true` for a lookup built
      from `CanonicalPair(b, a)` (the opposite direction's canonical form) — spec R3.6's own
      symmetry requirement; a pair with no stored relation returns `false` (absent from the map,
      not a zero-value present entry).
      **Red**: `undefined: ports.RelationRepo.ExistingPairs`, `undefined: consolidation.Pair` in
      the signature — package does not compile.
      Stub: `ExistingPairs(ctx context.Context, pairs []consolidation.Pair)
      (map[consolidation.Pair]bool, error)`; `memrepo` returns `(nil, nil)` — compiles; fails the
      opposite-direction fixture (`false` where `true` is expected).
      Requirement: spec R3.6; design §4.2.
      **Verified together with task 2.6** (one commit, one `go build` failure covering both
      undefined methods, one `go test` run covering both wiring-test assertion failures):
      `ExistingPairs()[{From:pair-a To:pair-b}] = (false, present=false), want (true,
      present=true)`.
      `internal/store/sqlite/relationrepo.go` gained matching placeholders for both `Evidence` and
      `ExistingPairs` (plain errors, "not implemented until PR 5", no sentinel) in this same
      commit, and `make store-api-golden` was run once for all four new methods (two on
      `UnitRepo`, two on `RelationRepo`) together.
- [x] **2.9** Commit 2 (GREEN): implement `ExistingPairs` in the `memrepo` fake, keyed by
      `consolidation.CanonicalPair`.
      Verify: `go test ./test/support/...`.
      Requirement: spec R3.6; design §4.2.
- [x] **2.10** Purity/coverage: `golangci-lint run` (`ports-purity` from PR 1 stays green — this
      PR's new files import only `internal/core/consolidation`, `internal/core/unit`, stdlib).
      Clean — `0 issues`.
      **Also decided in this PR (not a task the list named, raised in this PR's own brief)**:
      `internal/ports/relationrepo.go`'s doc comment claimed I03's strengthened prefix set is
      "satisfied for every ports interface, not only `ports.UnitRepo`", but
      `test/conformance/i03_units_never_deleted_test.go`'s reflection sweep only ever ran over
      `ports.UnitRepo` — a description wider than the code. `tasks.md` itself is not silent here:
      PR 3 (tasks 3.19–3.20) already owns widening that sweep to all five `ports` interfaces, so
      doing it in this PR would both duplicate PR 3's task and pull PR 3 scope forward, which this
      PR's own brief forbids. **Chosen resolution: narrow the doc comment to what is true today**
      (the sweep covers `ports.UnitRepo` only; PR 3 task 3.19 widens it) rather than extend the
      sweep now.
- [x] Verify (PR-level): `make check-all` — green (lint 0 issues, vet clean, race unit+integration
      tests, schema-golden-clean, 100% core coverage, seven-target cross-compile, e2e all green).
      **Diff does NOT match the file list this task's own text anticipated** — disclosed rather
      than silently widened, same discipline PR 1's Verify block used. Actual diff:
      `internal/ports/{unitrepo,relationrepo}.go`, `test/support/memrepo/{units,relations}.go`,
      `test/support/repocontract/{unitrepo,relationrepo}.go`,
      `test/conformance/{unitrepo,relationrepo}_memrepo_test.go` (the anticipated "+ tests"), plus
      four files this list did not name: `internal/store/sqlite/{unitrepo,relationrepo}.go` (PR-5
      placeholders, same owner ruling as PR 1's), `internal/store/sqlite/relationrepo_integration_test.go`
      (task 2.6's `SetLastTouchedAt` harness addition, a real implementation not a placeholder),
      and `testdata/schema/store_api.golden` (regenerated twice, once per placeholder pair).
      144 impl+docs lines (0.36× the 400-line ceiling, just under the ~150 estimate; ports.go +
      sqlite placeholder files + the golden diff), 394 test lines (repocontract + memrepo + wiring
      tests + the integration-tag harness addition).
      **Chain-merge check 1**: after merge, `git ls-remote --heads origin feat/ports-unit-relation-reads`
      returns nothing. **Not yet performed — this PR has not been merged.**
      **Chain-merge check 2**: `gh pr view <PR3> --json baseRefName` names `main`.
      **Not yet performed — PR 3 does not exist yet.**

---

## PR 3 — `feat/ports-selfmodel-config-state` (~240 impl+docs / ~470 test, est.)

Depends on PR 2 (no direct call; same fake/contract shape widened a third time). Ships
`ports.SelfModelRepo` (three methods), `ports.ConfigRepo` (two methods), `ports.StateRepo` (two
methods, plus the three literals) — design §4.3, §4.4, §3.4 — and **I03's widened reflection
sweep** (spec R2.7), the PR where all three new interfaces exist together.

### `SelfModelRepo` (design §4.3)

- [ ] **3.1** Commit 1 (RED): `test/support/repocontract/selfmodelrepo.go` (new) —
      `RunActiveBeliefs(t, newRepo)`: a fixture with beliefs across every `selfmodel.Facet` and
      both `active`/non-`active` status; returns exactly the active ones, all facets included, no
      status parameter on the call.
      **Red**: `undefined: ports.SelfModelRepo` — package does not compile.
      Stub: `type Belief struct{...}` (design §4.3's eleven fields); `type SelfModelRepo
      interface { ActiveBeliefs(ctx context.Context) ([]Belief, error); ... }` with
      `UpsertByTopicKey`/`ReinforceByID` declared but not yet exercised; a new
      `test/support/memrepo/selfmodel.go` fake returning `(nil, nil)` for `ActiveBeliefs` —
      compiles; fails the non-empty active-belief fixture.
      Requirement: spec R2.3; design §4.3.
- [ ] **3.2** Commit 2 (GREEN): implement `ActiveBeliefs` in the fake.
      Verify: `go test ./test/support/...`.
      Requirement: spec R2.3; design §4.3.
- [ ] **3.3** Commit 1 (RED): `test/support/repocontract/selfmodelrepo.go` (extend) —
      `RunUpsertByTopicKey(t, newRepo)`: writing the same `topic_key` twice with different
      `Content` produces **one** row, updated in place, not two.
      **Red**: covered by the same interface stub from 3.1 — `UpsertByTopicKey` compiles but the
      fake's zero-value implementation is a no-op, so the fixture's second write does not update
      the first; fails on the round-trip read.
      Requirement: spec R2.1.
- [ ] **3.4** Commit 2 (GREEN): implement `UpsertByTopicKey` in the fake — conflict target is
      `topic_key`, `RelationRepo.Upsert`'s own pattern.
      Verify: `go test ./test/support/...`.
      Requirement: spec R2.1; design §4.3.
- [ ] **3.5** Commit 1 (RED): `test/support/repocontract/selfmodelrepo.go` (extend) —
      `RunReinforceByID(t, newRepo)`: reinforcing an existing id changes only `confidence` and
      `last_reinforced_at`, leaving `topic_key`/`content`/`facet`/`origin`/`source_unit_id`
      unchanged; reinforcing an absent id returns `ErrBeliefNotFound` and creates **no** row.
      **Red**: `undefined: ports.ErrBeliefNotFound` — package does not compile.
      Stub: `var ErrBeliefNotFound = errors.New(...)`; fake's `ReinforceByID` returns
      `ErrBeliefNotFound` unconditionally — compiles; fails the existing-id fixture (expects the
      two fields changed, gets an error instead).
      Requirement: spec R2.2; design §4.3.
- [ ] **3.6** Commit 2 (GREEN): implement `ReinforceByID` in the fake — updates the two named
      columns only on a found id, `ErrBeliefNotFound` on a miss, never a silent create (spec R2.2's
      own `MUST`).
      Verify: `go test ./test/support/...`.
      Requirement: spec R2.2; design §4.3.
- [ ] **3.7** Doc comment, `internal/ports/selfmodelrepo.go`: state spec R2.2's `MUST NOT` inline —
      a merge (`MergeInto != ""`) must never route through `UpsertByTopicKey`, because
      `MergeInto` names an id that need not equal the newly-derived belief's own `topic_key`, and
      routing it through the wrong method silently creates a second belief instead of reinforcing
      the one the merge decision found. The two method names are the guard (design §4.3's own
      point) — this comment is what makes the wrong call read wrong at the call site.
      Requirement: spec R2.1, R2.2 (the `MUST NOT`); design §4.3.

### `ConfigRepo` (design §3.4)

- [ ] **3.8** Commit 1 (RED): `test/support/repocontract/configrepo.go` (new) —
      `RunConfigRepoLoad(t, newRepo)`: on an empty backing store, `Load` returns an all-`nil`
      `VaultConfig` and a `nil` error — every one of the six fields is `nil`, not a partial
      struct.
      **Red**: `undefined: ports.ConfigRepo`, `undefined: ports.VaultConfig` — package does not
      compile.
      Stub: `type VaultConfig struct{...}` (the six nil-sentinel pointer fields, design §3.4);
      `type ConfigRepo interface { Load(ctx context.Context) (VaultConfig, error);
      RecordConsolidationRun(ctx context.Context, at time.Time) error }`; a new
      `test/support/memrepo/config.go` fake returning `(VaultConfig{}, nil)` — compiles (this
      case passes immediately since the zero value of six pointers is already all-nil; **not a
      missing-symbol red beyond the compile step**, disclosed per `m2a` C9).
      Requirement: spec R2.4; design §3.4.
- [ ] **3.9** `configrepo.go` (repocontract, continued) — `RunConfigRepoLoad`: on a store holding a
      config row, every field carries the column's stored value **as stored** — including a
      corrupt one (e.g. `WeightThreshold` set to a `NaN`-equivalent sentinel the fake can produce)
      — never sanitized or defaulted by `ConfigRepo` itself. This is a genuine red once the fake
      grows a way to hold a config row (task 3.10 introduces it): the zero-value stub cannot
      round-trip a stored value.
      Requirement: spec R2.4 ("`ConfigRepo` does not itself decide what a `nil` or an
      out-of-range value defaults to"); design §3.4.
- [ ] **3.10** Commit 2 (GREEN): implement the fake's config storage (a single optional
      `*VaultConfig` field the fake holds, `nil` until `RecordConsolidationRun` first writes it)
      and `Load` reading it back verbatim.
      Verify: `go test ./test/support/...`.
      Requirement: spec R2.4; design §3.4.
- [ ] **3.11** Commit 1 (RED): `configrepo.go` (repocontract, continued) —
      `RunRecordConsolidationRun(t, newRepo)`: writing to an absent row creates it with
      `consolidation_last_run_at` set to the given instant and asserts the fake's own `Load`
      round-trips that one field with every other field still `nil` (mirroring "every other
      column takes the migration's own `DEFAULT`" at the fake's own zero-value level, since the
      fake has no SQL `DEFAULT` to read — design §3.4's own honest scoping: the fake proves the
      **shape** of the lazy-create, PR 6's sqlite implementation proves the SQL `DEFAULT`s
      themselves, task 6.9 below); writing to an existing row changes only that one field.
      **Red**: the fake's zero-value `RecordConsolidationRun` (task 3.8's stub) is a no-op —
      fails the round-trip.
      Requirement: spec R2.6; design §3.4.
- [ ] **3.12** Commit 2 (GREEN): implement `RecordConsolidationRun` in the fake.
      Verify: `go test ./test/support/...`.
      Requirement: spec R2.6; design §3.4.
- [ ] **3.13** `test/conformance/` (new) — a source-tree scan, the same shape I03's own DELETE-scan
      uses: no `.go` file under `internal/` references the string `"calibration"` as a table name.
      This is the L2 half of spec R2.5's "the `calibration` table stays fully unused through the
      whole of `m2c`" — genuinely red for the right reason if any earlier task had accidentally
      referenced it; passes today because nothing does.
      Requirement: spec R2.5.
- [ ] **3.14** doc 02 §13 amendment: rewrite the `goal_stagnation_days` row (currently: *"two
      schema homes exist today... `m2c` must pick the table `ConfigRepo` reads"*, doc 02 line 897)
      to state the decision this PR makes rather than the open question it used to name:
      `ConfigRepo` reads `config.goal_stagnation_days`; `calibration` stays unused, verified by
      task 3.13's scan. This is an **amendment to an existing row**, not a new row — `m2c`
      introduces no new `internal/core` constant (spec R0.3), so `calibration_doc_test.go`'s
      symbol/value pair for this row is unaffected; only the prose after the em dash changes.
      Requirement: spec R2.5 (discharges `m2b` §9 Q3).

### `StateRepo` (design §4.4)

- [ ] **3.15** Commit 1 (RED): `test/support/repocontract/staterepo.go` (new) —
      `RunOpenHypothesis(t, newRepo)`: two calls append two rows, neither call updates the other —
      asserted by reading both back and confirming both exist with their own `RecordedAt`.
      **Red**: `undefined: ports.StateRepo`, `undefined: ports.StateHypothesis` — package does not
      compile.
      Stub: the three literals (`StateSourceUser`, `StateSourceConsolidation`, `MoodLoaded`);
      `type StateHypothesis struct{ ID string; Mood string; RecordedAt time.Time }`; `type
      StateRepo interface { OpenHypothesis(ctx, StateHypothesis) error; LastHypothesisAt(ctx)
      (*time.Time, error) }`; a new `test/support/memrepo/state.go` fake with a no-op
      `OpenHypothesis` — compiles; fails the two-rows-appended fixture (only zero or one row
      visible).
      Requirement: design §4.4; spec R5.10 (the shape, not yet the SQL pin — that is PR 4).
- [ ] **3.16** Commit 2 (GREEN): implement `OpenHypothesis` in the fake — append-only, no update
      path exists on the port at all (structural, per design §4.4's own point).
      Verify: `go test ./test/support/...`.
      Requirement: design §4.4.
- [ ] **3.17** Commit 1 (RED): `staterepo.go` (repocontract, continued) —
      `RunLastHypothesisAt(t, newRepo)`: ignores rows written with `Mood` not carrying the
      `source = consolidation` marker the fake tracks internally (the fake must track `source`
      even though `StateHypothesis` itself carries no `Source` field — the port's real
      `OpenHypothesis` always writes `source = 'consolidation'`, so the fake's internal bookkeeping
      marks every row it stores that way; a row the fake never wrote — the "user" case — is
      represented by an empty backing store for that subtest) and returns `nil` when no
      `consolidation`-sourced row exists.
      **Red**: the fake's zero-value `LastHypothesisAt` (task 3.15's stub) always returns
      `(nil, nil)` — passes the empty case trivially but fails the non-empty case once task 3.16
      lands rows to find.
      Requirement: design §4.4 ("feeds `EvaluateLoad`'s `lastHypothesisAt` parameter directly").
- [ ] **3.18** Commit 2 (GREEN): implement `LastHypothesisAt` in the fake.
      Verify: `go test ./test/support/...`.
      Requirement: design §4.4.

### I03 widened, cross-cutting for this PR

- [ ] **3.19** Commit 1 (RED): `test/conformance/i03_units_never_deleted_test.go` (extend) —
      replace the single `reflect.TypeOf((*ports.UnitRepo)(nil)).Elem()` with a loop over five
      `reflect.Type` values: `UnitRepo`, `RelationRepo`, `SelfModelRepo`, `ConfigRepo`,
      `StateRepo`. **Genuinely red for the right reason before this task's own change**: the test
      as it stands only sweeps `UnitRepo`, so a temporarily-added `PurgeBelief` method on
      `SelfModelRepo` (added and removed as this task's own verification step, mirroring `m2a`
      C3.3's discipline of checking rather than trusting) would pass the *old* test and must fail
      the *new* one — this is the mutation check the task performs before considering itself done.
      Requirement: spec R2.7.
- [ ] **3.20** Commit 2 (GREEN, structural): confirm the widened loop passes against the real five
      interfaces as they stand at the end of this PR, and that the temporary `PurgeBelief`
      mutation from task 3.19 is reverted, tree clean.
      Verify: `go test ./test/conformance/... -run TestI03`.
      Requirement: spec R2.7; design §4.6.
- [ ] **3.21** Purity/coverage: `golangci-lint run` (`ports-purity` — this PR's three new
      `internal/ports` files import only stdlib + `internal/core/{selfmodel,unit}`, no store, no
      brain).
- [ ] Verify (PR-level): `make check-all`; confirm diff touches only
      `internal/ports/{selfmodelrepo,configrepo,staterepo}.go`,
      `test/support/memrepo/{selfmodel,config,state}.go` (+ tests),
      `test/support/repocontract/{selfmodelrepo,configrepo,staterepo}.go`,
      `test/conformance/i03_units_never_deleted_test.go` (widened), `docs/02-cognitive-core.md`
      (§13's `goal_stagnation_days` row, amended). Target ≤240 impl+docs lines.
      **Chain-merge check 1**: `git ls-remote --heads origin feat/ports-selfmodel-config-state`
      returns nothing after merge.
      **Chain-merge check 2**: `gh pr view <PR4> --json baseRefName` names `main`.

---

## PR 4 — `feat/schema-current-state-source` (~80 impl+docs / ~90 test, est.)

Depends on PR 3 (`ports.StateRepo`'s two literals exist; this PR pins them to the migration).
Ships **migration 0003** and its full verified cost — every touched site named explicitly, per the
owner ruling, not summarized as "update the migration tests."

- [ ] **4.1** `internal/store/sqlite/migrations/0003_current_state_source.sql` (new) —
      ```sql
      ALTER TABLE current_state ADD COLUMN source TEXT NOT NULL DEFAULT 'user';  -- user|consolidation
      ```
      exactly as design §3.2 specifies. **A published migration is never modified**
      (`CLAUDE.md` convention) — this is the first and only edit this file will ever receive.
      Requirement: spec's owner ruling (migration 0003 approved, one column); design §3.2 option A.
- [ ] **4.2** `internal/store/sqlite/migrate.go` — register migration 0003 in the embedded
      migration list, bumping the applied `user_version` ceiling from 2 to 3.
      Requirement: owner ruling; design §3.2.
- [ ] **4.3** `test/integration/migrate_test.go` — the five hard-coded sites, each edited
      individually and named here so none is missed: line 64 (`wantVersion`), line 104
      (`wantVersion`), line 172 (`wantVersion`), line 235 (`wantVersion`), line 343
      (`BinaryVersion != 2` assertion) — all become `3`. Verified against `design.md` §1's own
      citation of these five exact lines.
      Verify: `go test -tags integration ./test/integration/... -run TestMigrate`.
      Requirement: owner ruling (full verified cost of migration 0003).
- [ ] **4.4** **No edit** to `test/integration/schema_golden_anchor_test.go` — verified in `design.md`
      §1 (line 66) and restated here as an explicit non-task so a future reader does not go
      looking for one: the anchor list names objects (`table current_state`), never columns, so
      adding a column to an existing table needs no anchor edit. This line exists to make that
      omission deliberate, not overlooked.
      Requirement: owner ruling (scope of migration 0003's cost, stated precisely).
- [ ] **4.5** `make schema-golden` — regenerate `testdata/schema/{structure,ddl}.golden` from the
      now-three-migration embedded set. Commit the two regenerated files alongside the migration.
      Verify: `make schema-golden-clean` (fails if regenerating leaves a dirty tree).
      Requirement: spec's owner ruling; `docs/06-harness.md` §7 (calibration/golden convention).
- [ ] **4.6** `docs/03-data-model.md` — the `current_state` block (currently five columns, doc line
      104-110) gains `source TEXT NOT NULL DEFAULT 'user' -- user|consolidation`, matching the
      migration's own column comment verbatim.
      Requirement: owner ruling; design §10.3, row 3 (§10 amendment).
- [ ] **4.7** Commit 1 (RED): `test/conformance/` (new or extend an existing DDL-pin file,
      following `relation.AllCreatedBy`'s own precedent against `0001_core_tables.sql:37`) — pin
      `ports.StateSourceConsolidation`, `ports.StateSourceUser`, `ports.MoodLoaded` against
      migration 0003's own column comment vocabulary (`user|consolidation`), read off disk via the
      existing `migrationSQLText` helper.
      **Red**: the pin test references migration 0003's text, which does not exist until task 4.1
      — ordered after 4.1 in this PR's own commit sequence, so in practice this is the first
      commit that can be red for "the migration text I'm reading doesn't have this row yet" if the
      migration were absent; since 4.1 already landed by the time this task runs, this is
      disclosed as **not** a missing-symbol/missing-migration red (`m2a` C9) — it is a genuine
      value-pin test proving the three Go constants match the SQL comment's vocabulary exactly.
      Requirement: spec R2.5's sibling for `StateRepo`'s own literals (design §4.4's own comment:
      *"outside `calibration_doc_test.go`'s reach... pinned instead to migration 0003's own
      column comment"*).
- [ ] **4.8** Commit 2 (GREEN, no code — a verification): confirm the pin test passes.
      Verify: `go test ./test/conformance/...`.
      Requirement: design §4.4.
- [ ] **4.9** doc 02 §7 amendment: state the load watcher's `current_state` row is written with
      `source = 'consolidation'`, `mood = 'loaded'`, `energy` left `NULL`, and that its cooldown
      anchors on the previous hypothesis's own `recorded_at` because M2 has no resolution signal
      (`m2b` §9 Q6, mapped) — design §10.3's exact wording for the §7 row.
      Requirement: design §10.3, row 2.
- [ ] **4.10** doc 02 §10 amendment: state `current_state` gains `source`, and that the
      append-only property is now structural at the port (`StateRepo` has no update path), not
      only a convention — design §10.3's exact wording for the §10 row.
      Requirement: design §10.3, row 3.
- [ ] Verify (PR-level): `make check-all` (this is the PR that first exercises the `integration`
      build tag's own migration suite in this chain — confirm `test-integration` passes, not only
      `make check`); confirm diff touches only
      `internal/store/sqlite/migrations/0003_current_state_source.sql`,
      `internal/store/sqlite/migrate.go`, `test/integration/migrate_test.go`,
      `testdata/schema/{structure,ddl}.golden`, `docs/03-data-model.md`,
      `test/conformance/` (the new pin file), `docs/02-cognitive-core.md` (§7, §10). Target ≤80
      impl+docs lines.
      **Chain-merge check 1**: `git ls-remote --heads origin feat/schema-current-state-source`
      returns nothing after merge.
      **Chain-merge check 2**: `gh pr view <PR5> --json baseRefName` names `main`.

---

## PR 5 — `feat/store-unit-relation-repos` (~330 impl+docs / ~520 test, est. — tallest link, 0.83× the ceiling)

Depends on PR 2 (the six port methods this PR implements) and PR 1 (I24's leg 3 needs this PR's
`ApplyBoosts` SQL text). **Pre-drawn split, per design §13**: if real impl+docs measures at or
above ~300 (the same stop-and-report threshold `m2a`/`m2b`'s own PR4/PR4a used), split at
`unitrepo.go` (5a, ~210) | `relationrepo.go` (5b, ~120) — two files, two independent integration
suites, no cross-file dependency to untangle.

- [ ] **5.1** Commit 1 (RED): `internal/store/sqlite/unitrepo_integration_test.go` (extend) —
      run `repocontract.RunApplyBoosts` against the real sqlite `UnitRepo`, `integration` build
      tag.
      **Red**: `sqlite.UnitRepo` does not implement `ApplyBoosts` — package does not compile under
      the `integration` tag.
      Requirement: spec R3.1, R3.3; design §5.2.
- [ ] **5.2** Commit 2 (GREEN): implement `ApplyBoosts` in `internal/store/sqlite/unitrepo.go` —
      one `BEGIN IMMEDIATE` transaction, one `UPDATE units SET weight = ?, last_touched_at = ?,
      updated_at = ? WHERE id = ?` per boost inside it; `0` rows affected on any one boost rolls
      back the whole transaction and returns `ports.ErrUnitNotFound`; non-finite `Weight` is
      refused **before** `BEGIN` (no transaction opened for a batch that cannot land) — design
      §5.2's exact shape.
      Verify: `go test -tags integration ./internal/store/sqlite/... -run TestApplyBoosts`.
      Requirement: spec R3.1, R3.3; design §5.2.
- [ ] **5.3** Commit 1 (RED): `test/conformance/i05_...` — **extend the existing file
      `i05_effective_weight_computed_on_read_test.go`** (design's own naming precedent: `i05`'s
      file already exists from `m2a`'s pure half) with the structural-half source-text scan (spec
      R3.4): no method whose name identifies it as a read (`ByID`, `LiveByIDs`, `LiveDecayStates`)
      contains, in its own SQL text, an assignment to `units.weight` or `units.last_touched_at`
      under `internal/store/sqlite`.
      **Red**: genuinely red before task 5.2's implementation lands with the two-column
      assignment somewhere — the scan asserts the assignment appears in **exactly one** method,
      and before `ApplyBoosts` exists that count is `0`, not `1`.
      Requirement: spec R3.4; design §5.3.
- [ ] **5.4** Commit 2 (GREEN, structural — no new code beyond task 5.2's own): confirm the scan
      passes at exactly one match. This task **is** I24's leg 3 (task 1.6's forward reference,
      discharged here) and I05's structural half in one file, per spec R3.4's own scoping note
      (bulk decay materialization was already declined by `m2b`, so there is no permitted-but-unused
      write for the scan to accidentally forbid — stated in the test's own doc comment, not
      inferred).
      Verify: `go test ./test/conformance/... -run TestI05`.
      Requirement: spec R3.4; design §5.3; discharges task 1.6's I24 leg-3 forward reference.
- [ ] **5.5** Commit 1 (RED): `internal/store/sqlite/unitrepo_integration_test.go` (extend) — run
      `repocontract.RunCountLiveByType`, `RunIncompleteOlderThan`, `RunLiveDecayStates` against the
      real sqlite `UnitRepo`.
      **Red**: none of the three methods exist on the sqlite type yet — does not compile.
      Requirement: spec R1.2, R5.1; design §4.1.
- [ ] **5.6** Commit 2 (GREEN): implement `CountLiveByType`, `IncompleteOlderThan`,
      `LiveDecayStates` in `internal/store/sqlite/unitrepo.go` — the SQL filters are bounds, not
      the decision (task 2.5's doc comment); `LiveDecayStates` selects `pool`-status units and the
      five decay fields only, never a full `unit.Unit` row.
      Verify: `go test -tags integration ./internal/store/sqlite/...`.
      Requirement: spec R1.2, R5.1; design §4.1.
- [ ] **5.7** *(split checkpoint)*: measure `git diff --stat` for `unitrepo.go` +
      `unitrepo_integration_test.go` (tasks 5.1–5.6) in isolation. If this half alone is at risk
      of the ~210 sub-estimate running hot, this is PR 5a's natural boundary — open it now rather
      than let `relationrepo.go` inflate the same PR.
- [ ] **5.8** Commit 1 (RED): `internal/store/sqlite/relationrepo_integration_test.go` (extend) —
      run `repocontract.RunEvidence`, `RunExistingPairs` against the real sqlite `RelationRepo`.
      **Red**: neither method exists on the sqlite type — does not compile.
      Requirement: spec R3.5, R3.6; design §4.2.
- [ ] **5.9** Commit 2 (GREEN): implement `Evidence` (the join over both endpoints'
      `last_touched_at`, one query) and `ExistingPairs` (keyed by `CanonicalPair`, bounded by the
      candidate set the caller passes) in `internal/store/sqlite/relationrepo.go`.
      Verify: `go test -tags integration ./internal/store/sqlite/...`.
      Requirement: spec R3.5, R3.6; design §4.2.
- [ ] **5.10** `make store-api-golden` — regenerate `testdata/schema/store_api.golden` to reflect
      the six widened/new method signatures across `UnitRepo`/`RelationRepo`. Named as its own
      task, not discovered as a fast-loop failure — the same surprise M1's PRs 4 and 9 already
      recorded (spec R3.2).
      Verify: `make check`'s existing golden-diff check.
      Requirement: spec R3.2.
- [ ] **5.11** Purity/coverage: `golangci-lint run` (`sqlite-containment` — this PR's files stay
      inside `internal/store/sqlite`); `go test -tags integration -race ./internal/store/sqlite/...`.
- [ ] Verify (PR-level): `make check-all`; confirm diff touches only
      `internal/store/sqlite/{unitrepo,relationrepo}.go` (+ integration tests),
      `test/conformance/i05_effective_weight_computed_on_read_test.go` (extended),
      `testdata/schema/store_api.golden`. Target ≤330 impl+docs lines; **treat ~300 as the
      stop-and-report checkpoint** per task 5.7 — split at the pre-drawn `unitrepo.go` |
      `relationrepo.go` boundary if crossed.
      **Chain-merge check 1**: `git ls-remote --heads origin feat/store-unit-relation-repos`
      returns nothing after merge (or `feat/store-unit-relation-repos-5a`/`-5b` if split).
      **Chain-merge check 2**: `gh pr view <PR6> --json baseRefName` names `main`.

---

## PR 6 — `feat/store-selfmodel-config-state` (~300 impl+docs / ~450 test, est.)

Depends on PR 3 (the seven port methods this PR implements) and PR 5 (same file-per-repository
convention, sequential in the stack). **Pre-drawn split, per design §13**: if impl+docs measures
at or above ~300, split at `configrepo.go` + `staterepo.go` (6a, ~150) | `selfmodelrepo.go` (6b,
~150) — `SelfModelRepo` is the largest of the three (an eleven-field row) and travels alone.

- [ ] **6.1** Commit 1 (RED): `internal/store/sqlite/configrepo_integration_test.go` (new) — run
      `repocontract.RunConfigRepoLoad` against the real sqlite `ConfigRepo`; additionally, a case
      `RunConfigRepoLoad` does not cover because it is sqlite-specific: on a malformed
      `consolidation_last_run_at` TEXT value (not parseable as `unitTimeLayout`), `Load` returns an
      **error**, never `nil` for "unparseable" (spec's own distinction, design §3.4's "a malformed
      value is an error, not `nil`" rule — a corrupt timestamp read as `nil` would turn
      `SelectConnectSources` into a whole-live-pool read instead of surfacing the corruption).
      **Red**: `sqlite.ConfigRepo` does not exist — package does not compile.
      Requirement: spec R2.4; design §3.4, §5.4.
- [ ] **6.2** Commit 2 (GREEN): implement `internal/store/sqlite/configrepo.go` — `Load` reads the
      six-column singleton row (`id = 1`), returns an all-nil struct when the row is absent, parses
      `consolidation_last_run_at` with `unitTimeLayout` and errors on a malformed value rather than
      swallowing it.
      Verify: `go test -tags integration ./internal/store/sqlite/...`.
      Requirement: spec R2.4; design §3.4, §5.4.
- [ ] **6.3** Commit 1 (RED): `configrepo_integration_test.go` (extend) — run
      `repocontract.RunRecordConsolidationRun`; additionally, the SQL-specific case task 3.11's
      fake could not prove: writing to an absent row creates it with **the migration's own SQL
      `DEFAULT`s** on every other column (`weight_threshold = 0.5`, `hysteresis_margin = 0.05`,
      `consolidation_enabled = 1`, `goal_stagnation_days = 21`, `mental_load_threshold = 7`), read
      off disk via `migrationSQLText` and asserted against the row `Load` returns after the write —
      never a Go literal duplicating them.
      **Red**: `RecordConsolidationRun` does not exist on the sqlite type — does not compile.
      Requirement: spec R2.6; design §3.4, §5.2 (the `UPSERT`).
- [ ] **6.4** Commit 2 (GREEN): implement `RecordConsolidationRun` — the `UPSERT` design §3.4
      specifies exactly (`INSERT INTO config (id, consolidation_last_run_at, updated_at) VALUES (1,
      ?, ?) ON CONFLICT(id) DO UPDATE SET ...`), naming no default in Go.
      Verify: `go test -tags integration ./internal/store/sqlite/...`.
      Requirement: spec R2.6; design §3.4.
- [ ] **6.5** Commit 1 (RED): `internal/store/sqlite/staterepo_integration_test.go` (new) — run
      `repocontract.RunOpenHypothesis`, `RunLastHypothesisAt` against the real sqlite `StateRepo`.
      **Red**: `sqlite.StateRepo` does not exist — package does not compile.
      Requirement: design §4.4.
- [ ] **6.6** Commit 2 (GREEN): implement `internal/store/sqlite/staterepo.go` —
      `OpenHypothesis` appends one `current_state` row with `source = 'consolidation'`, `energy`
      left `NULL`, `active = 1`; `LastHypothesisAt` reads the most recent row with
      `source = 'consolidation'` only.
      Verify: `go test -tags integration ./internal/store/sqlite/...`.
      Requirement: design §4.4.
- [ ] **6.7** *(split checkpoint)*: measure `git diff --stat` for `configrepo.go` + `staterepo.go`
      (tasks 6.1–6.6) in isolation. If this half is at risk of the ~150 sub-estimate running hot,
      this is PR 6a's natural boundary.
- [ ] **6.8** Commit 1 (RED): `internal/store/sqlite/selfmodelrepo_integration_test.go` (new) — run
      `repocontract.RunActiveBeliefs`, `RunUpsertByTopicKey`, `RunReinforceByID` against the real
      sqlite `SelfModelRepo`.
      **Red**: `sqlite.SelfModelRepo` does not exist — package does not compile.
      Requirement: spec R2.1, R2.2, R2.3; design §4.3.
- [ ] **6.9** Commit 2 (GREEN): implement `internal/store/sqlite/selfmodelrepo.go` —
      `ActiveBeliefs` filters `status = 'active'`, no status parameter; `UpsertByTopicKey`
      conflicts on `self_beliefs.topic_key` (`UNIQUE`, migration `0001:75`); `ReinforceByID`
      updates `confidence`/`last_reinforced_at` only, `ports.ErrBeliefNotFound` on a miss.
      Verify: `go test -tags integration ./internal/store/sqlite/...`.
      Requirement: spec R2.1, R2.2, R2.3; design §4.3.
- [ ] **6.10** `make store-api-golden` — regenerate for the seven new methods (`SelfModelRepo`,
      `ConfigRepo`, `StateRepo`).
      Verify: `make check`'s existing golden-diff check.
      Requirement: spec R3.2.
- [ ] **6.11** Purity/coverage: `golangci-lint run`; `go test -tags integration -race
      ./internal/store/sqlite/...`.
- [ ] Verify (PR-level): `make check-all`; confirm diff touches only
      `internal/store/sqlite/{selfmodelrepo,configrepo,staterepo}.go` (+ integration tests),
      `testdata/schema/store_api.golden`. Target ≤300 impl+docs lines; split at the pre-drawn
      `configrepo.go`+`staterepo.go` | `selfmodelrepo.go` boundary if the task 6.7 checkpoint flags
      it.
      **Chain-merge check 1**: `git ls-remote --heads origin feat/store-selfmodel-config-state`
      returns nothing after merge.
      **Chain-merge check 2**: `gh pr view <PR7a> --json baseRefName` names `main`.

---

## PR 7a — `feat/brain-consolidate-runner` (~230 impl+docs / ~430 test, est.)

Depends on PR 6 (every port this PR wires against now has a real sqlite implementation, though
this PR itself uses fakes for its own tests). Ships `ConsolidateService`, `ConsolidateRequest`,
`consolidateRunner`, `passContext`, `ConsolidateReport`, the `Order()` loop and `runPhase`'s
`switch` (design §3.3), and **I11's behavioural half** (spec R4.1).

- [ ] **7a.1** Commit 1 (RED): `internal/brain/consolidate_test.go` (new) — a fixture over fake
      repos seeded so every phase has qualifying input, with a spy `consolidation.Phase` recorder;
      asserts the recorded invocation sequence equals `consolidation.Order()` exactly, including
      that `PhaseLearn`'s slot is reached and reached **last**.
      **Red**: `undefined: brain.ConsolidateService` — package does not compile.
      Stub: `ConsolidateService{clock ports.Clock; run consolidateRunner}`,
      `ConsolidateRequest{Phase *consolidation.Phase}`, `func (s *ConsolidateService)
      Consolidate(ctx, req) (ConsolidateReport, error) { return ConsolidateReport{}, nil }` —
      compiles; the spy sees zero invocations, fails first.
      Requirement: spec R4.1; design §3.3(a)–(b).
- [ ] **7a.2** Commit 2 (GREEN): implement `consolidateRunner.at` — the `for _, p := range
      consolidation.Order() { if req.Phase != nil && p != *req.Phase { continue }; ...
      runPhase(ctx, p, pass) }` filter loop (design §3.3(b)), `runPhase`'s `switch p {
      case consolidation.PhaseArchive: ... }` with a `default` returning an error naming the
      unhandled phase (so a ninth phase added later fails loudly rather than being silently
      skipped). No phase body does real work yet — each `case` is a placeholder the phase-IO PRs
      (8–11) fill in; the spy still fires for each case reached.
      Verify: `go test ./internal/brain/...`.
      Requirement: spec R4.1; design §3.3(b).
- [ ] **7a.3** Commit 1 (RED): `internal/brain/consolidate_test.go` (extend) — a per-phase run
      (`ConsolidateRequest{Phase: &consolidation.PhaseArchive}`) reaches exactly one arm's spy
      entry, no others; an unknown phase value (out of `Order()`'s range) errors through
      `runPhase`'s `default` case.
      **Red**: genuinely red until task 7a.2's filter+switch exists — the stub from 7a.1 runs
      nothing, so the "exactly one" assertion fails against zero.
      Requirement: design §3.3(a)'s `*Phase` sentinel, §3.3(b)'s filter.
- [ ] **7a.4** Commit 2 (GREEN, no new code beyond 7a.2): confirm task 7a.3 passes.
      Verify: `go test ./internal/brain/... -run TestConsolidate_PerPhase`.
      Requirement: design §3.3(a)–(b).
- [ ] **7a.5** Commit 1 (RED): `internal/brain/consolidate_test.go` (extend) — `passContext`'s
      `since` is read from `ConfigRepo.Load` **before any phase runs**, once, and the **same**
      `*time.Time` value is what the runner would hand to any phase that consumes it (this PR has
      no real phase consumer yet — the assertion is against `passContext.since` itself, captured
      via a test-only accessor or by asserting `ConfigRepo.Load` is called exactly once per pass
      regardless of how many phases run); with no `config` row, `since` is `nil`.
      **Red**: the stub from 7a.1/7a.2 does not read config at all — fails the "`Load` called
      exactly once" assertion (zero calls).
      Requirement: spec R5.3; design §3.3(c); discharges `m2b` §9 Q8.
- [ ] **7a.6** Commit 2 (GREEN): implement `passContext` assembly in `consolidateRunner.at` —
      `now := s.clock.Now()` (the **one** clock read this package makes per invocation, guarded by
      `brain_single_clock_read_test.go`, already scoped to every non-test file under
      `internal/brain/**`, no test-file edit needed), `cfg := configRepo.Load(ctx)` once,
      `since := cfg.ConsolidationLastRunAt` held once, `passContext{now, cfg, since}` passed by
      value to every phase.
      Verify: `go test ./internal/brain/...`; `go test ./test/conformance/... -run
      TestBrainSingleClockRead`.
      Requirement: spec R0.2, R5.3; design §3.3(c).
- [ ] **7a.7** Commit 1 (RED): `internal/brain/consolidate_test.go` (extend) — a whole-pass fixture
      asserts `ConfigRepo.RecordConsolidationRun` is called exactly once, with the pass's own
      `now`; a per-phase fixture asserts it is **never** called (spec R5.4's `MUST NOT`).
      **Red**: no call site exists yet in the stub — the whole-pass case fails (0 calls, want 1).
      Requirement: spec R5.4; design §3.3(d).
- [ ] **7a.8** Commit 2 (GREEN): implement the one write site — `if req.Phase == nil {
      r.cfg.RecordConsolidationRun(ctx, pass.now) }` after the `Order()` loop completes, design
      §3.3(d)'s exact shape (one call site, gated on the same field that selected the scope).
      Verify: `go test ./internal/brain/...`.
      Requirement: spec R5.4; design §3.3(d).
- [ ] **7a.9** Doc comment, `internal/brain/consolidate.go`: state design §3.3(d)'s own named
      limit — a pass that fails mid-way leaves `since` pointing at the previous pass, and every
      phase in M2 is idempotent under a re-read, so this is a cost, not a correctness problem
      — written down so it is not rediscovered as a surprise.
      Requirement: design §3.3(d) ("what this does not cover").
- [ ] **7a.10** Purity/coverage: `golangci-lint run` (`brain-boundary` from PR 1 — this PR imports
      only `internal/core/*` and `internal/ports`, never `internal/store`); `go test -race
      ./internal/brain/...`.
- [ ] Verify (PR-level): `make check-all`; confirm diff touches only
      `internal/brain/consolidate.go` (+ test). Target ≤230 impl+docs lines.
      **Chain-merge check 1**: `git ls-remote --heads origin feat/brain-consolidate-runner`
      returns nothing after merge.
      **Chain-merge check 2**: `gh pr view <PR7b> --json baseRefName` names `main`.

---

## PR 7b — `feat/brain-consolidate-decision-log` (~190 impl+docs / ~430 test, est.)

Depends on PR 7a (adds to the same `consolidate.go`; not conditional on the split — design §13
states this split explicitly, not as an overrun response, because the runner and a
fourteen-to-twenty-four-member vocabulary widening are two separately reviewable units). Ships the
ten new `DecisionAction` members (design §7.5), the `record` helper, the `corrupted`-never-logged
rule, and **I12 both directions plus the exclusion** (spec R4.2).

- [ ] **7b.1** `internal/ports/decisionlog.go` — add the ten new `DecisionAction` constants exactly
      as design §7.5 enumerates them (`ActionExpireIncompleteTransitioned` through
      `ActionPatternEvalLoadHypothesisOpened`), and add all ten to `AllDecisionActions()`'s
      returned slice, extending the count from fourteen to twenty-four. Not a behavioral change to
      an existing test — `AllDecisionActions` has no length assertion pinned elsewhere in this
      repository to update (verified: the fourteen-member count lives only in this file's own doc
      comment, updated here).
      Requirement: design §7.5.
- [ ] **7b.2** Commit 1 (RED): `internal/brain/consolidate_test.go` (extend) — a fixture wiring
      the runner over a fake phase producing exactly one persistable effect per invocation slot
      (using `runPhase`'s existing placeholder arms from PR 7a — this PR does not need real phase
      logic, only that each arm can be made to call `record` once) asserts exactly one
      `decision_log` row per persisted effect, and that each row's `Action` distinguishes which
      phase and effect kind produced it (spec R4.2's first `MUST`).
      **Red**: `undefined: consolidateRunner.record` — package does not compile.
      Stub: `func (r consolidateRunner) record(ctx context.Context, now time.Time, action
      ports.DecisionAction, rationale string, detail any) error { return nil }` — compiles; the
      fixture expects `decisionLog.Since(...)` to return one row, gets zero, fails first.
      Requirement: spec R4.2 (direction 1); design §3.3(e).
- [ ] **7b.3** Commit 2 (GREEN): implement `record` — marshals `detail` into `Decision.Context`,
      calls `ports.DecisionLog.Record` with `rationale` as the legible sentence.
      Verify: `go test ./internal/brain/...`.
      Requirement: spec R4.2; design §3.3(e).
- [ ] **7b.4** Commit 1 (RED): `internal/brain/consolidate_test.go` (extend) — a fixture where no
      phase has qualifying input: the pass completes successfully and `decision_log` gains **zero**
      rows (spec R4.2's second `MUST`, direction 2).
      **Red**: if any placeholder phase arm from PR 7a unconditionally calls `record`, this fails
      — disclosed as the actual regression this test guards, not a hypothetical.
      Requirement: spec R4.2 (direction 2).
- [ ] **7b.5** Commit 2 (GREEN, verification — no new production code expected): confirm no
      placeholder arm calls `record` unconditionally.
      Verify: `go test ./internal/brain/... -run TestConsolidate_NoEffects`.
      Requirement: spec R4.2.
- [ ] **7b.6** Commit 1 (RED): `internal/brain/consolidate_test.go` (extend) — a fixture producing
      a `corrupted` entry from a fake phase's decision function: **no** `decision_log` row exists
      for it, regardless of which of the four `corrupted`-capable phases (`archive`, `strengthen`,
      `reweight`, `derive`) produced it (spec R4.2's `MUST NOT`, decided uniformly per design
      §3.3(e)).
      **Red**: genuinely red if `record` were called for a `corrupted` entry by mistake — this
      test is the guard against that regression from the start, not discovered after the phase-IO
      PRs land.
      Requirement: spec R4.2 (`MUST NOT`); design §3.3(e) ("decided uniformly... a `corrupted`
      entry from any phase is surfaced in `ConsolidateReport` and never in `decision_log`").
- [ ] **7b.7** Commit 2 (GREEN): implement `ConsolidateReport`'s `corrupted` field (a set unioned
      across every phase's own corrupted output) as the one place refused entries surface, and
      confirm no call site routes a `corrupted` entry into `record`.
      Verify: `go test ./internal/brain/...`.
      Requirement: spec R4.2; design §3.3(e).
- [ ] **7b.8** Commit 1 (RED): `internal/brain/consolidate_test.go` (extend) — R4.3's fixture: three
      units planned for archival, the fake `UnitRepo.SetStatus` returns `ErrStatusConflict` for the
      second; the pass completes, the first and third are archived, the second is skipped **and**
      logged (one `decision_log` row naming the skip), and no error propagates out of `Consolidate`.
      **Red**: this PR's placeholder `archive` arm does not yet call `SetStatus` at all (PR 8 wires
      the real read/write) — disclosed as a **forward-looking scaffold test**: it is written and
      committed here against a fake phase function this PR controls directly (not the real
      `archive` phase), proving the runner's own skip-and-log mechanism in isolation, before PR 8
      wires the real phase to exercise it end to end. PR 8's own task 8.9 re-runs the identical
      shape against the real `archive` wiring.
      Requirement: spec R4.3; design §3.3(e) ("the line between the two is worth stating").
- [ ] **7b.9** Commit 2 (GREEN): implement the skip-and-log path in `runPhase`'s persistence
      helper — `ErrStatusConflict` from `SetStatus` does not abort the pass; it is caught, the unit
      is skipped, and `record` is called naming the skip.
      Verify: `go test ./internal/brain/...`.
      Requirement: spec R4.3; design §3.3(e).
- [ ] **7b.10** Purity/coverage: `golangci-lint run`; `go test -race ./internal/brain/...`.
- [ ] Verify (PR-level): `make check-all`; confirm diff touches only
      `internal/ports/decisionlog.go` (ten new constants), `internal/brain/consolidate.go` (+
      test, extended from PR 7a). Target ≤190 impl+docs lines.
      **Chain-merge check 1**: `git ls-remote --heads origin feat/brain-consolidate-decision-log`
      returns nothing after merge.
      **Chain-merge check 2**: `gh pr view <PR8> --json baseRefName` names `main`.

---

## PR 8 — `feat/brain-phase-io-transitions` (~200 impl+docs / ~400 test, est.)

Depends on PR 7b (fills three of `runPhase`'s placeholder arms with real I/O). Ships slots 1–3:
`expire_incomplete`, `archive`, `strengthen` — repo-only, no provider call in any of the three.

- [ ] **8.1** Commit 1 (RED): `internal/brain/consolidate_test.go` (extend) — `expire_incomplete`'s
      arm: `brain` derives `cutoff` as `now.Add(-consolidation.IncompleteExpiryHours *
      time.Hour)`, never a literal — asserted by a spy on the fake `UnitRepo.IncompleteOlderThan`
      recording the `cutoff` argument and comparing it against the constant-derived value.
      **Red**: the placeholder `expire_incomplete` arm from PR 7a calls nothing — the spy sees no
      call, fails first.
      Requirement: design §4.1 ("the cutoff duplicates a predicate... an L2 test asserts `brain`
      derives the cutoff from `consolidation.IncompleteExpiryHours`").
- [ ] **8.2** Commit 2 (GREEN): implement the `expire_incomplete` arm — read via
      `IncompleteOlderThan(cutoff)`, call `consolidation.ExpireIncomplete(us, now)`, persist each
      `Transition` via `SetStatus`, `record` one row per transition
      (`ActionExpireIncompleteTransitioned`).
      Verify: `go test ./internal/brain/...`.
      Requirement: spec R4.2, R5.1; design §6.3 (slot 1).
- [ ] **8.3** Commit 1 (RED): `test/integration/` (new or extend) — seed a vault through the
      **real capture path** (no repo-constructed `incomplete` row), run the `expire_incomplete`
      read against it, assert it returns empty (owner ruling Q3's own proof — spec R5.1's second
      `MUST`).
      **Red**: genuinely red only in the sense that the assertion has never run before — the
      expected outcome (empty) is what M1's Q3a already established, so this is a **proof**, not a
      behavior change; disclosed per `m2a` C9 as not a missing-symbol red.
      Requirement: spec R5.1 ("this is stated as a positive, testable claim rather than left as
      an absence").
- [ ] **8.4** Commit 2 (GREEN, no code): confirm the L3 fixture passes.
      Verify: `go test -tags integration ./test/integration/...`.
      Requirement: spec R5.1.
- [ ] **8.5** Commit 1 (RED): `internal/brain/consolidate_test.go` (extend) — `archive`'s arm is
      called with the **configured** `WeightThreshold` (via `consolidation.ResolveWeightThreshold`),
      not the default, when the fixture's `ConfigRepo` returns one that differs from
      `consolidation.DefaultWeightThreshold`.
      **Red**: the placeholder arm calls nothing — fails first.
      Requirement: spec R5.2; design §3.3(c).
- [ ] **8.6** Commit 2 (GREEN): implement the `archive` arm — read via `LiveDecayStates()`, resolve
      the threshold through `ResolveWeightThreshold(pass.cfg.WeightThreshold)` (never
      `DefaultWeightThreshold` directly), call `consolidation.Archive(cs, threshold, pass.now)`,
      persist each `Transition` via `SetStatus`, `record` one row per transition
      (`ActionArchiveArchived`).
      Verify: `go test ./internal/brain/...`.
      Requirement: spec R4.2, R5.2; design §6.3 (slot 2).
- [ ] **8.7** **The `Source` sanitization guard (design §8.1), introduced here for `archive`'s own
      call to `LiveDecayStates`, reused by `connect`/`derive` in PR 9/10b.** Commit 1 (RED):
      `internal/brain/consolidate_test.go` (extend) — a `Cold` row with a non-finite `Weight` (or
      `DecayRate`) is refused before `Archive` ever sees it, surfaced through
      `ConsolidateReport.corrupted`, fixtured with ≥3 units so removing the guard changes the
      fixture's outcome.
      **Red**: no partition step exists in the placeholder arm — the non-finite row would reach
      `Archive` unfiltered and either panic on the `weight.Effective` comparison's own documented
      `NaN` behavior or silently mis-sort; the test's assertion (the corrupt id appears in
      `corrupted`, not in any transition) fails against the unguarded stub.
      Requirement: design §8.1 ("what `m2c` does instead... `consolidateRunner` partitions
      `[]consolidation.Cold` into usable and refused before mapping to `[]Source`").
- [ ] **8.8** Commit 2 (GREEN): implement the partition helper — the same non-finite predicate
      `Archive` applies internally, run once in `brain` before any of `archive`/`connect`/`derive`
      consumes a `LiveDecayStates` read, refused ids folded into `ConsolidateReport.corrupted`.
      Verify: `go test ./internal/brain/...`.
      Requirement: design §8.1.
- [ ] **8.9** Commit 1 (RED): `internal/brain/consolidate_test.go` (extend) — **re-run R4.3's exact
      fixture (PR 7b task 7b.8's shape) against the real `archive` wiring**: three units planned
      for archival, the fake `UnitRepo.SetStatus` returns `ErrStatusConflict` for the second; the
      pass completes, first and third archived, second skipped and logged.
      **Red**: genuinely red until this task's own wiring exists — PR 7b's task 7b.8/7b.9 proved
      the *mechanism* against a fake phase function; this proves the *real* `archive` phase uses
      it.
      Requirement: spec R4.3; design §3.3(e).
- [ ] **8.10** Commit 2 (GREEN, no new mechanism — wiring only): confirm the fixture passes using
      the mechanism PR 7b already built.
      Verify: `go test ./internal/brain/...`.
      Requirement: spec R4.3.
- [ ] **8.11** Commit 1 (RED): `internal/brain/consolidate_test.go` (extend) — `strengthen`'s arm:
      `Strengthen` and (once PR 9 exists) `SelectConnectSources` receive an **identical**
      `*time.Time` for `since`; for this PR alone, assert `strengthen`'s own call receives
      `pass.since` unmodified — a spy on the fake `RelationRepo.Evidence` read plus a spy on the
      core `Strengthen` call's `since` argument.
      **Red**: the placeholder arm calls nothing — fails first.
      Requirement: spec R5.3; design §3.3(c), §6.3 (slot 3).
- [ ] **8.12** Commit 2 (GREEN): implement the `strengthen` arm — read via `Evidence()`, call
      `consolidation.Strengthen(es, pass.since)`, persist each `StrengthChange` via
      `RelationRepo.Upsert`, `record` one row per change (`ActionStrengthenApplied`).
      Verify: `go test ./internal/brain/...`.
      Requirement: spec R4.2, R5.3; design §6.3 (slot 3).
- [ ] **8.13** Purity/coverage: `golangci-lint run` (`brain-boundary` — no `internal/store` import
      anywhere in this PR's files); `go test -race ./internal/brain/...`.
- [ ] Verify (PR-level): `make check-all`; confirm diff touches only
      `internal/brain/consolidate.go` (+ test, extended), `test/integration/` (the new
      real-capture-path `expire_incomplete` fixture). Target ≤200 impl+docs lines.
      **Chain-merge check 1**: `git ls-remote --heads origin feat/brain-phase-io-transitions`
      returns nothing after merge.
      **Chain-merge check 2**: `gh pr view <PR9> --json baseRefName` names `main`.

---

## PR 9 — `feat/brain-phase-io-connect` (~300 impl+docs / ~500 test, est.)

Depends on PR 8. Ships slot 4: the `ScoredUnit → FusedCandidate` adapter shared with
`correction.go`, `ExistingPairs`, the judge call, `ProposeRelation` → `Upsert`; `capture.go:485`'s
one-line adoption; and (for `connect`'s own `LiveDecayStates` read) the guard PR 8 already built.
**Pre-drawn split, per design §13**: if impl+docs measures at or above ~300, split at 9a (the
adapter — `ScoredUnit → FusedCandidate`, `ExistingPairs`, `SelectConnectSources` wiring, §8.1's
guard reuse, ~160) | 9b (the judge call, `ProposeRelation`, `Upsert`, `capture.go:485`, ~140).

- [ ] **9.1** Commit 1 (RED): `internal/brain/consolidate_test.go` (extend) — `connect`'s candidate
      search calls `RecallService.ScoredFor`, never a second fusion implementation — asserted via
      a spy on a fake `RecallService` (or, if `RecallService` is a concrete struct with no
      interface seam today, a source-tree scan confirming no new `sort.Slice`/ranking function is
      added under `internal/brain` for this phase — the shape `correction.go` already reuses).
      **Red**: the placeholder `connect` arm calls nothing — fails first.
      Requirement: spec R5.5; design §7.1.
- [ ] **9.2** Commit 2 (GREEN): implement `connect`'s adapter — `LiveDecayStates()` (through PR
      8's partition guard) → `[]consolidation.Source`, `SelectConnectSources(ss, pass.since,
      pass.now)` → for each source, `RecallService.ScoredFor(ctx, text)` → the existing
      `[]ScoredUnit → []recall.FusedCandidate` mapping `correction.go:117-120` already establishes
      (shared, not copied) → `ExistingPairs` → `consolidation.ConnectPairs`.
      Verify: `go test ./internal/brain/...`.
      Requirement: spec R5.5; design §7.1, §6.3 (slot 4).
- [ ] **9.3** *(split checkpoint)*: measure `git diff --stat` for tasks 9.1–9.2 (the adapter half)
      in isolation. If at risk of the ~160 sub-estimate running hot, this is PR 9a's boundary.
- [ ] **9.4** Commit 1 (RED): `internal/brain/consolidate_test.go` (extend) — `connect`'s persisted
      relations carry `relation.CreatedByConsolidation`; the judged decision routes through
      `relation.Resolve`/`Decide` unchanged — asserted against the identical decision-table
      fixtures `capture`'s own relation tests already prove (I07/I08 regression coverage), not a
      new decision table.
      **Red**: the placeholder arm from 9.2 does not yet call the judge or persist anything — fails
      first.
      Requirement: spec R5.5; design §7.1.
- [ ] **9.5** Commit 2 (GREEN): implement the judge call — the `relation_evaluation` LLM task,
      `relation.DecodeJudgment`, `consolidation.ProposeRelation(from, judgment, thresholds)`, and
      on acceptance `RelationRepo.Upsert` with `CreatedBy = CreatedByConsolidation`.
      Verify: `go test ./internal/brain/...`.
      Requirement: spec R5.5; design §7.1.
- [ ] **9.6** Commit 1 (RED): `internal/brain/consolidate_test.go` (extend) — a judgement that
      decided nothing (`ProposeRelation` returns `false`) writes **no** `decision_log` row for
      `connect` — deliberately differing from capture's own `ActionRelationDiscarded` (design
      §7.1's own stated divergence, flagged for owner review at §12 Q2 but implemented as
      recommended: no row).
      **Red**: genuinely red if `record` were called on every judge result regardless of outcome —
      this test is the guard from the start.
      Requirement: spec R4.2 (a judgment with no effect writes nothing); design §7.1.
- [ ] **9.7** Commit 2 (GREEN, verification): confirm the arm only calls `record` on acceptance
      (`ActionConnectRelationPersisted`), never on a discard.
      Verify: `go test ./internal/brain/...`.
      Requirement: spec R4.2; design §7.1.
- [ ] **9.8** Re-run the `Source` sanitization fixture (PR 8 task 8.7's shape) against `connect`'s
      own `LiveDecayStates` consumption — confirm the same partition guard (built once, shared)
      refuses the identical set of rows `archive` refused, over a fixture that exercises both
      phases from the same seeded state.
      Verify: `go test ./internal/brain/... -run TestConnect_RefusesNonFiniteSources`.
      Requirement: design §8.1 ("`archive` at slot 2 and `connect`/`derive` at slots 4/5 therefore
      refuse the identical set of rows").
- [ ] **9.9** `internal/brain/capture.go:485` — replace the bare `"system"` literal with
      `relation.CreatedBySystem` (`m2b` §8's one-line handoff, discharged here per this document's
      own inherited-handoffs section).
      Verify: `go test ./internal/brain/... -run TestCapture`.
      Requirement: design §7.1 ("discharged rather than carried forward again").
- [ ] **9.10** Purity/coverage: `golangci-lint run`; `go test -race ./internal/brain/...`.
- [ ] Verify (PR-level): `make check-all`; confirm diff touches only
      `internal/brain/{consolidate,capture}.go` (+ test, extended). Target ≤300 impl+docs lines;
      split at the pre-drawn adapter | judge boundary if task 9.3's checkpoint flags it.
      **Chain-merge check 1**: `git ls-remote --heads origin feat/brain-phase-io-connect` returns
      nothing after merge (or the 9a/9b names if split).
      **Chain-merge check 2**: `gh pr view <PR10a> --json baseRefName` names `main`.

---

## PR 10a — `feat/core-derive-prompt` (~150 impl+docs / ~300 test, est.)

Depends on nothing in this chain except `m2b`'s shipped `consolidation.Belief`/`derive.go` (this
PR edits it). **The one `internal/core` addition this entire change makes** (design §10.2,
correcting `spec.md`'s Scope-boundary claim of zero). `docs-sync.yml` fires on this PR — it is the
only PR in the chain that needs a genuine `internal/core/` ↔ doc 02 delta from `docs-sync`'s own
perspective, per this document's header note.

- [ ] **10a.1** Commit 1 (RED): `internal/core/consolidation/prompt_test.go` (new) —
      `BuildDerivePrompt` is a pure function over `([]DeriveSource, []Belief)`, deterministic
      across repeated calls; with ≥3 beliefs and ≥3 units (so ordering is falsifiable), every
      belief's `TopicKey`/`Content` appears in the output; with an empty `existing` slice, the
      output still contains the input units' content **and** names the empty state plainly rather
      than omitting the belief section (spec R5.6's second `MUST`).
      **Red**: `undefined: consolidation.DeriveSource`, `undefined: consolidation.
      BuildDerivePrompt` — package does not compile.
      Stub: `type DeriveSource struct{ UnitID string; Type unit.Type; Content string }`; `func
      BuildDerivePrompt(us []DeriveSource, existing []Belief) string { return "" }` — compiles;
      the non-empty fixture expects the belief's `TopicKey` as a substring, empty string fails
      first.
      Requirement: spec R5.6; design §10.2.
- [ ] **10a.2** Commit 2 (GREEN): implement `BuildDerivePrompt` per doc 02 §6.5's derivation
      prompt shape, rendering every active belief so the judge can decide "this already exists"
      before proposing a new one.
      Verify: `go test ./internal/core/consolidation/...`; `golangci-lint run`
      (`core-purity` — this file imports only stdlib + `internal/core/{unit,selfmodel}`).
      Requirement: spec R5.6; design §10.2.
- [ ] **10a.3** `internal/core/consolidation/derive.go` — add `Content string` to the
      `m2b`-shipped `Belief` type (design §10.2's exact field addition); confirm
      `EvaluateStagnation` and `MergeProposals` (both `m2b`, unchanged callers) continue to ignore
      the new field — no behavior change to either, verified by their existing `m2b` test suites
      staying green with no edit.
      Verify: `go test ./internal/core/consolidation/...`.
      Requirement: design §10.2.
- [ ] **10a.4** No new `internal/core` constant — verification, not a task with an edit: `rg 'const'
      internal/core/consolidation/prompt.go` returns nothing. Confirms spec R0.3's own claim holds
      through this PR (`calibration_doc_test.go`'s §13 sweep is unaffected).
      Requirement: spec R0.3.
- [ ] **10a.5** doc 02 §6.5 amendment: one sentence naming `derive`'s source selection — the units
      a pass derives from are the same recently-touched, effective-weight-ranked,
      `connect_source_limit`-capped set `connect` uses (design §7.3, discharged fully in PR 10b —
      this PR's own doc obligation is limited to the prompt builder's existence and shape; the
      source-selection sentence is added here since it is the natural doc-sync companion to this
      PR's `internal/core/` change, but the §13 `connect_source_limit` annotation itself is task
      10b's, since it is stated alongside the phase that actually reuses the knob).
      Requirement: design §10.3, row 1 (partial — the sentence, not the §13 annotation).
- [ ] **10a.6** Purity/coverage: `golangci-lint run`; `make cover` (`internal/core`'s 90% floor —
      this is the one PR in the chain where it is the binding constraint, per design §9's own
      note).
- [ ] Verify (PR-level): `make check-all`; confirm diff touches only
      `internal/core/consolidation/{prompt,derive}{,_test}.go`, `docs/02-cognitive-core.md` (§6.5).
      Target ≤150 impl+docs lines.
      **Chain-merge check 1**: `git ls-remote --heads origin feat/core-derive-prompt` returns
      nothing after merge.
      **Chain-merge check 2**: `gh pr view <PR10b> --json baseRefName` names `main`.

---

## PR 10b — `feat/brain-phase-io-derive` (~250 impl+docs / ~430 test, est.)

Depends on PR 10a (`BuildDerivePrompt`) and PR 9 (the `Source` guard, reused for `derive`'s own
`LiveDecayStates` read). Ships slot 5: `ActiveBeliefs` → prompt → `belief_derivation` →
embeddings → `MergeProposals` → the two `SelfModelRepo` writes.

- [ ] **10b.1** Commit 1 (RED): `internal/brain/consolidate_test.go` (extend) — `derive`'s prompt
      contains every active belief's `TopicKey`/`Content` when `ActiveBeliefs` returns non-empty;
      with none, the prompt still sends (not a skipped call) and names the empty state — via a
      fake `LLMProvider` capturing the prompt text passed to the `belief_derivation` task.
      **Red**: the placeholder `derive` arm calls nothing — fails first.
      Requirement: spec R5.6; design §6.3 (slot 5).
- [ ] **10b.2** Commit 2 (GREEN): implement the `derive` arm's source selection and prompt call —
      `LiveDecayStates()` (through the guard) → `SelectConnectSources(ss, pass.since, pass.now)` →
      `LiveByIDs` to materialize `[]DeriveSource`; `ActiveBeliefs()` → `BuildDerivePrompt` → the
      `belief_derivation` LLM call.
      Verify: `go test ./internal/brain/...`.
      Requirement: spec R5.6; design §7.3, §6.3 (slot 5).
- [ ] **10b.3** Commit 1 (RED): `internal/brain/consolidate_test.go` (extend) — `derive` calls
      `EmbeddingProvider` exactly `len(activeBeliefs)` times per phase run — a fake
      `EmbeddingProvider` counting calls; separately, a source-tree scan confirms no new port or
      store method persists a belief vector (owner ruling Q2, option A).
      **Red**: fails first (zero calls against the placeholder).
      Requirement: spec R5.7; design §6.3 (slot 5).
- [ ] **10b.4** Commit 2 (GREEN): implement the in-memory embedding step — one
      `EmbeddingProvider` call per active belief, held only for the duration of this phase run's
      `MergeProposals` call, discarded after.
      Verify: `go test ./internal/brain/...`.
      Requirement: spec R5.7; design §6.3 (slot 5).
- [ ] **10b.5** Commit 1 (RED): `internal/brain/consolidate_test.go` (extend) — one
      create-decision and one merge-decision from the same `derive` run produce exactly one
      `SelfModelRepo.UpsertByTopicKey` call and exactly one `ReinforceByID` call, each with the
      correct target (spec R5.8's own split).
      **Red**: fails first (zero calls against the placeholder).
      Requirement: spec R5.8.
- [ ] **10b.6** Commit 2 (GREEN): implement the routing — `MergeInto == ""` →
      `UpsertByTopicKey`; `MergeInto != ""` → `ReinforceByID` with the confidence
      `consolidation.Reinforce` computes from the merged-into belief's current confidence; `record`
      one row per decision (`ActionDeriveBeliefCreated`/`ActionDeriveBeliefReinforced`).
      Verify: `go test ./internal/brain/...`.
      Requirement: spec R4.2, R5.8; design §6.3 (slot 5).
- [ ] **10b.7** Verification, not an edit: confirm no `belief_embeddings` table, migration, or
      port/store method exists anywhere in this PR's diff — `rg -i 'belief.?embedding'
      internal/store internal/ports` returns only doc comments, never a type or table name.
      Discharges the `m2b` §8 handoff ("belief embeddings in memory, no table").
      Requirement: spec R5.7 (`MUST NOT`); design §6.3 (slot 5).
- [ ] **10b.8** doc 02 §13 amendment: annotate the `connect_source_limit` row (§6.4's own product,
      already documented by `m2b`) to state it now governs **two** phases —
      `derive`'s source selection reuses it (design §7.3) — the Default column's number is
      unchanged, so `calibration_doc_test.go` stays green; this is prose-only, discharging the
      remainder of task 10a.5's doc obligation.
      Requirement: design §10.3, row 1 (the §13 annotation half); §7.3, §12 Q5.
- [ ] **10b.9** Purity/coverage: `golangci-lint run`; `go test -race ./internal/brain/...`.
- [ ] Verify (PR-level): `make check-all`; confirm diff touches only
      `internal/brain/consolidate.go` (+ test, extended), `docs/02-cognitive-core.md` (§13's
      `connect_source_limit` row, annotated). Target ≤250 impl+docs lines.
      **Chain-merge check 1**: `git ls-remote --heads origin feat/brain-phase-io-derive` returns
      nothing after merge.
      **Chain-merge check 2**: `gh pr view <PR11> --json baseRefName` names `main`.

---

## PR 11 — `feat/brain-phase-io-reweight-patterns` (~250 impl+docs / ~450 test, est.)

Depends on PR 10b. Ships slots 6–8: `Reweight` → `ApplyBoosts`; `EvaluateStagnation`;
`EvaluateLoad` → `OpenHypothesis` + the `lastHypothesisAt` context; `learn`'s no-op arm — the last
phase-IO PR. **Pre-drawn split, per design §13**: if impl+docs measures at or above ~250, split at
11a (`reweight`, ~110) | 11b (`pattern_eval` + `learn`, ~140 — `learn`'s no-op arm travels with
11b since it is the arm that must be reached and asserted last).

### `reweight` (design §6.3, slot 6)

- [ ] **11.1** Commit 1 (RED): `internal/brain/consolidate_test.go` (extend) — the exact `m2b`
      spec R3.3 scenario re-run through the wired runner: a unit boosted by one origin and
      corrupted by another origin's edge in the same call — the boost is persisted through
      `ApplyBoosts`, and **no** `decision_log` row exists for the corrupted half (restating PR 7b's
      exclusion rule at this specific phase, per spec R5.9).
      **Red**: the placeholder `reweight` arm calls nothing — fails first.
      Requirement: spec R5.9; design §6.3 (slot 6).
- [ ] **11.2** Commit 2 (GREEN): implement the `reweight` arm — `consolidation.Reweight(states,
      newEdges, pass.now)` → every `boosts` entry persisted through `ApplyBoosts`, preserving the
      per-unit `(Weight, LastTouchedAt)` pairing; `record` one row per boost
      (`ActionReweightBoostApplied`); `corrupted` entries never logged (already proven at the
      runner level by PR 7b, exercised here against the real phase).
      Verify: `go test ./internal/brain/...`.
      Requirement: spec R4.2, R5.9; design §6.3 (slot 6).
- [ ] **11.3** *(split checkpoint)*: measure `git diff --stat` for tasks 11.1–11.2 in isolation.
      If at risk of the ~110 sub-estimate running hot, this is PR 11a's boundary.

### `pattern_eval` + `learn` (design §6.3, slots 7–8)

- [ ] **11.4** Commit 1 (RED): `internal/brain/consolidate_test.go` (extend) — every
      `StagnationFinding` `EvaluateStagnation` returns produces one `decision_log` row, correctly
      attributed (spec R5.10's first `MUST`).
      **Red**: the placeholder `pattern_eval` arm calls nothing — fails first.
      Requirement: spec R4.2, R5.10; design §6.3 (slot 7).
- [ ] **11.5** Commit 2 (GREEN): implement the stagnation half of the `pattern_eval` arm —
      `ActiveBeliefs()` → `EvaluateStagnation(bs, ResolveGoalStagnationDays(pass.cfg), pass.now)` →
      `record` one row per finding (`ActionPatternEvalStagnationFound`).
      Verify: `go test ./internal/brain/...`.
      Requirement: spec R4.2, R5.10; design §6.3 (slot 7).
- [ ] **11.6** Commit 1 (RED, L2 + L3) — `internal/brain/consolidate_test.go` (extend, L2) plus a
      new `test/integration/` fixture (L3): `EvaluateLoad` firing produces exactly one
      `current_state` row (`OpenHypothesis`, `source = 'consolidation'`, `mood = 'loaded'`,
      `energy` `NULL`) **plus** one `decision_log` row whose `Context` states the
      `lastHypothesisAt` mapping this phase uses — `lastHypothesisAt` is
      `StateRepo.LastHypothesisAt`'s own return, the previous hypothesis's own `recorded_at`, per
      `m2b` §9 Q6's mapping; not firing produces zero of both.
      **Red**: the placeholder arm calls nothing — fails first, both at L2 (fake) and L3 (real
      sqlite `StateRepo`).
      Requirement: spec R5.10 (second `MUST`); design §3.2 (Q6 mapped), §6.3 (slot 7).
- [ ] **11.7** Commit 2 (GREEN): implement the load half of the `pattern_eval` arm —
      `CountLiveByType(unit.TypeMentalLoad)`, `StateRepo.LastHypothesisAt()` →
      `EvaluateLoad(n, ResolveMentalLoadThreshold(pass.cfg), lastAt, pass.now)` → on firing,
      `OpenHypothesis` **and** `record` (`ActionPatternEvalLoadHypothesisOpened`) with the
      `lastHypothesisAt` mapping in `Context`.
      Verify: `go test ./internal/brain/...`; `go test -tags integration
      ./test/integration/...`.
      Requirement: spec R4.2, R5.10; design §3.2, §6.3 (slot 7).
- [ ] **11.8** `learn`'s arm — no core function to call (ruling 3, already established by `m2b`).
      Confirm `runPhase`'s `switch` has a `case consolidation.PhaseLearn:` that performs no work
      and calls `record` zero times — this is the arm PR 7a's own I11 behavioural test (task 7a.1)
      already asserts is reached and reached last; this task is the verification that `learn`'s
      case body stays empty rather than accidentally growing a call, not a new test.
      Requirement: spec R1.3 (no positive test for an absent function — the absence itself, `m2b`
      spec's own words); design §6.3 (slot 8).
- [ ] **11.9** doc 02 §7 cross-check (verification, not an edit unless found stale): confirm the
      stagnation and load-accumulation predicates described in doc 02 §7 (amended by `m2b` task
      5.9) match this PR's actual wiring — `derive` at slot 5 refreshes `last_reinforced_at`,
      `pattern_eval` at slot 7 reads the refreshed value, and the phase order makes that reading
      sound. No edit expected; recorded as an explicit check per this document's own "every task
      names something a reader could check" standard.
      Requirement: design §6.3 (the pipeline diagram's own ordering guarantee).
- [ ] **11.10** Purity/coverage: `golangci-lint run`; `go test -race ./internal/brain/...`.
- [ ] Verify (PR-level): `make check-all`; confirm diff touches only
      `internal/brain/consolidate.go` (+ test, extended), `test/integration/` (the new
      `EvaluateLoad`-firing L3 fixture). Target ≤250 impl+docs lines; split at the pre-drawn
      `reweight` | `pattern_eval`+`learn` boundary if task 11.3's checkpoint flags it.
      **Chain-merge check 1**: `git ls-remote --heads origin feat/brain-phase-io-reweight-patterns`
      returns nothing after merge.
      **Chain-merge check 2**: `gh pr view <PR12> --json baseRefName` names `main`.

---

## PR 12 — `feat/cli-consolidate` (~230 impl+docs / ~380 test, est.)

Depends on PR 11 (every phase now has real I/O — this PR is the first to run a whole pass end to
end against a real vault). Ships `cmd/nooma/consolidate.go`, `--phase` via `ParsePhase`,
`vaultlock`, `tasksConsolidateConsumes`, report rendering, and the four L4 tests spec §6 requires
— the last link in the chain and `m2c`'s own exit criterion.

- [ ] **12.1** Commit 1 (RED): `test/e2e/consolidate_e2e_test.go` (new, `e2e` build tag) — running
      `nooma consolidate <vault>` against a vault a `serve` process already holds the write lock on
      returns a clean, non-zero-exit error naming the holder; against an unlocked vault, it
      succeeds.
      **Red**: `nooma consolidate` subcommand does not exist — `cmd/nooma` does not build the
      binary this test invokes, or the command errors "unknown command".
      Requirement: spec R6.1; design §11 (n/a — this is CLI-native, no design cross-ref beyond
      spec).
- [ ] **12.2** Commit 2 (GREEN): implement `cmd/nooma/consolidate.go`'s lock acquisition —
      `vaultlock.Acquire(vault)` before opening the store for write, `serve.go`'s own pattern
      (`cmd/nooma/serve.go:71-79`), a clean error naming the holder on failure.
      Verify: `go test -tags e2e ./test/e2e/... -run TestConsolidate_Lock`.
      Requirement: spec R6.1.
- [ ] **12.3** Commit 1 (RED): `test/e2e/consolidate_e2e_test.go` (extend) — the default invocation
      (no `--phase`) runs the full eight-phase pass and writes `consolidation_last_run_at` on
      completion.
      **Red**: fails against the still-incomplete command from task 12.2 (no phase execution yet).
      Requirement: spec R6.2.
- [ ] **12.4** Commit 2 (GREEN): wire `runConsolidate` to call `ConsolidateService.Consolidate`
      with a zero-value `ConsolidateRequest` (whole pass, per design §3.3(a)'s own stated zero
      value).
      Verify: `go test -tags e2e ./test/e2e/...`.
      Requirement: spec R6.2.
- [ ] **12.5** Commit 1 (RED): `test/e2e/consolidate_e2e_test.go` (extend) — `--phase=<known>` runs
      exactly that phase and leaves `consolidation_last_run_at` untouched; `--phase=<unknown>`
      errors cleanly through `consolidation.ErrUnknownPhase`; `cmd/nooma`'s new file contains no
      two-or-more phase-name string literals (I11's own tree scan from `m2b`, already covering
      `cmd/nooma` with no test-file edit needed — verified as a fact about this PR's diff, not a
      new test).
      **Red**: `--phase` flag does not exist — flag parse fails.
      Requirement: spec R6.3.
- [ ] **12.6** Commit 2 (GREEN): implement `--phase`, validated through
      `consolidation.ParsePhase` — never a second CLI-local phase-name vocabulary — building a
      `ConsolidateRequest{Phase: &p}`.
      Verify: `go test -tags e2e ./test/e2e/...`; `rg -c '"expire_incomplete"|"archive"|"strengthen"|
      "connect"|"derive"|"reweight"|"pattern_eval"|"learn"' cmd/nooma/consolidate.go` reports at
      most one match per literal (confirming the tree-scan's own property by hand, in addition to
      the automated I11 test already covering the file).
      Requirement: spec R6.3.
- [ ] **12.7** Commit 1 (RED): `test/e2e/consolidate_e2e_test.go` (extend) — a vault with an unbound
      task (no `relation_evaluation`, `belief_derivation`, or `embedding` provider configured)
      refuses **before** taking the lock, naming the unbound task — `consolidate`'s posture
      diverges deliberately from `serve`'s degrade-and-503 (design §7.2, owner-flagged at §12 Q6
      but implemented as recommended).
      **Red**: fails against the current command, which either hangs, panics, or silently no-ops
      through a nil provider.
      Requirement: design §7.2.
- [ ] **12.8** Commit 2 (GREEN): implement `tasksConsolidateConsumes = []string{"relation_evaluation",
      "belief_derivation", "embedding"}` and the pre-lock task-binding check, reusing
      `internal/config.DocumentedTaskNames`'s existing vocabulary (already contains
      `belief_derivation` — no config-vocabulary change needed, verified `design.md` §1).
      Verify: `go test -tags e2e ./test/e2e/...`.
      Requirement: design §7.2.
- [ ] **12.9** Commit 1 (RED): `test/e2e/consolidate_e2e_test.go` (extend) — **the exit
      criterion**: a minimal fixture vault seeded through the real capture path (not the `m2d`
      demo golden set — explicitly out of `m2c`'s scope), run through `nooma consolidate`, exits 0
      and `decision_log` gains at least one row whose `rationale` is a legible sentence, not a
      code.
      **Red**: fails until the full pass genuinely produces at least one effect on the fixture.
      Requirement: spec R6.4 (the proposal's own stated exit criterion — *"run the pass by hand on
      a vault and read the `decision_log`"*).
- [ ] **12.10** Commit 2 (GREEN, no new mechanism — the fixture and wiring): build the minimal
      fixture vault (seeded via the real capture path, at least one unit qualifying for at least
      one phase's effect — e.g. a unit old enough for `strengthen`'s co-use window, or a relation
      pair `connect` can find) and confirm the exit criterion passes end to end.
      Verify: `go test -tags e2e ./test/e2e/...`.
      Requirement: spec R6.4.
- [ ] **12.11** `cmd/nooma/wiring.go` — add `wireConsolidate`, following `wireBrain`'s own shape
      (`cmd/nooma/wiring.go:149-171`) but **refusing** rather than returning `(nil, nil)` on an
      unbound task, per task 12.8's own decision.
      Requirement: design §7.2; `design.md` §1's own citation of `wireBrain`'s current shape.
- [ ] **12.12** `cmd/nooma/tasks.go` — register `tasksConsolidateConsumes` where `serve`'s own task
      list lives, so `nooma status`/`nooma doctor` (if either inspects task bindings) see
      `consolidate`'s requirement too.
      Verify: `go build ./...`.
      Requirement: design §7.2.
- [ ] **12.13** Purity/lint: `golangci-lint run` (`cmd/nooma` is unconstrained by depguard — it
      legitimately imports everything, per design §10.1's own scoping note); `go vet ./...`.
- [ ] Verify (PR-level): `make check-all` (this PR's own L4 suite is the first `e2e`-tagged run in
      the chain — confirm `make test-e2e` passes, not only `make check`); confirm diff touches only
      `cmd/nooma/{consolidate,tasks,wiring}.go`, `test/e2e/consolidate_e2e_test.go`. Target ≤230
      impl+docs lines.
      **Chain-merge check 1**: `git ls-remote --heads origin feat/cli-consolidate` returns nothing
      after merge. This is the **last** link — there is no next PR to retarget, so chain-merge
      check 2 does not apply here; instead, confirm `main`'s own `git log --oneline -14` shows all
      fourteen (or more, if any link split) `m2c` commits in dependency order.

---

## Cross-cutting close-out (after PR 12 merges, before archive)

- [ ] **X.1** Confirm `docs/06-harness.md` needed no change across all fourteen links — I03, I05,
      I11, I12 and I24 all already had §4 rows before this change started (design §1, §10.3's own
      closing sentence). `git diff main~14..main -- docs/06-harness.md` (or the equivalent range
      once any split PRs are accounted for) is empty.
      Requirement: design §10.3.
- [ ] **X.2** Confirm `internal/core` gained exactly one file's worth of new code across the whole
      chain — `prompt.go` plus the one `Belief.Content` field (PR 10a) — and no new
      `internal/core` constant anywhere else, per spec R0.3's own claim. `rg 'const \(' -A5
      internal/core/consolidation/` reviewed by hand against `m2b`'s already-shipped constant set.
      Requirement: spec R0.3; design §10.2.
- [ ] **X.3** Confirm `docs/02-cognitive-core.md` §13 carries the same row **count** it had before
      this change (no new row — `m2c` introduces no new `internal/core` constant), with exactly
      two rows **amended** in place: `goal_stagnation_days` (PR 3, task 3.14) and
      `connect_source_limit` (PR 10b, task 10b.8). `calibration_doc_test.go`'s symbol floor is
      unchanged by this entire chain.
      Requirement: spec R0.3; design §7.5's own arithmetic correction (fourteen-to-twenty-four for
      `DecisionAction`, not §13).
- [ ] **X.4** Confirm the ten `DecisionAction` members (PR 7b) bring `AllDecisionActions()` from
      fourteen to twenty-four, matching design §7.5's corrected count exactly (not eleven, the
      number an earlier draft of the design mis-stated and corrected in the same document).
      Requirement: design §7.5.

---

## Handoffs `m2c` leaves open (design §11, §12 — carried forward so the archive does not lose them)

Not tasks in this change — recorded here per this project's own convention (`m2b` tasks.md's own
"Handoffs to `m2c`" section), so the next reader inherits them rather than rediscovering them.

- **`m2a` C17, C19, C29** — not discharged by `m2c`. C17 (`Resurface`'s dead `refused` guard) was
  already deleted in `m2b` PR 3 — nothing owed. C19 (`Edge.Strength = +Inf` coerced rather than
  refused inside `weight`) is not reached by any `m2c` call path — `Reweight` refuses non-finite
  edges before `clampStrength` ever runs — and stays open for a future second caller. C29
  (`focus.Displaces` needs a caller to resolve the margin first) is passed on to **M4**: `m2c`
  wires no focus consumer.
- **`SelectConnectSources`'s comparator is not total under `NaN`** (new, found in this design) —
  `m2c` guards at the `brain` boundary (design §8.1, PR 8/9's partition step); the real fix is a
  `corrupted` second return on the core function and belongs in `internal/core/consolidation`, a
  future core change.
- **`SelectConnectSources` now serves two phases and its name says one** (new) — a rename to
  `SelectRecentSources` is a core change, passed on.
- **`recall.Search`'s comparator is not total under `NaN`, reachable since M1** through
  `RecallService.ScoredFor` — `m2c`'s `connect` (PR 9) is a second caller, not a new hazard, and
  adds no guard at that call site. The fix belongs in `internal/core/recall` and would protect
  every existing M1 caller too.
- **`m2c` refuses the whole `consolidate` command when a task is unbound** (design §7.2, PR 12) —
  a finer-grained refusal (run the provider-free phases, refuse only the whole-pass timestamp) is
  possible and is more code; recorded at design §12 Q6 as a decision the owner may want revisited.
- **`consolidation_enabled` gates the scheduler, never the explicit CLI invocation** (design §7.4,
  §12 Q3) — `m2d`'s cron gate inherits this as a decision, not a re-open question.
- **`HysteresisMargin` is read into `VaultConfig` but has no reader in `m2c`** (design §7.4) —
  `m2a` C29's own obligation (resolve the margin once, at the boundary where the config row is
  read) is inherited by M4's first `focus.Displaces` caller.
- **The scheduler, ADR-0009's boot catch-up, `serve` wiring, and the simulated-weeks demo golden
  set** are all `m2d` — `m2c` writes the column the catch-up reads (`consolidation_last_run_at`,
  PR 3/6) and gives it a `ConsolidateService` with one entry point (PR 7a); it starts nothing on a
  timer.
- **Any delivery** (digest, push, `interrupt_level`) — M3. `pattern_eval` (PR 11) writes a
  hypothesis and a finding; nothing in `m2c` carries either to a user.
- **The derive prompt's exact wording and the `decision_log` rationale sentences' exact wording**
  — both asserted for content in this change's tests, never byte-for-byte; free for a future PR to
  refine without breaking the suite.
- **Paging either of the two unbounded reads** (`LiveDecayStates`, `Evidence`) — named as a risk in
  design §4.1/§12, not mitigated; the fix would put a bound in SQL that core's own decision
  functions do not have.

---

## Traceability

| Spec section | Requirements | Tasks |
|---|---|---|
| R0 — cross-cutting (dependency rule, clock, calibration scope) | R0.1, R0.2, R0.3 | 1.11–1.12 (R0.1); 7a.6 (R0.2); 3.13–3.14, 10a.4, X.2–X.3 (R0.3) |
| §1 `ports.UnitRepo` weight write + live count (PR 1) | R1.1–R1.4 | 1.1–1.10 |
| §2 `ports.SelfModelRepo` + `ports.ConfigRepo` (PR 3) | R2.1–R2.7 | 3.1–3.21 |
| §3 `internal/store/sqlite` repos (PR 5, 6) | R3.1–R3.6 | 5.1–5.11, 6.1–6.11 |
| §4 `brain.ConsolidateService` runner (PR 7a, 7b, 8) | R4.1–R4.4 | 7a.1–7a.10, 7b.1–7b.10, 8.7–8.10 |
| §5 Phase I/O wiring (PR 2, 8, 9, 10a, 10b, 11) | R5.1–R5.10 | 2.1–2.9, 8.1–8.13, 9.1–9.10, 10a.1–10a.6, 10b.1–10b.9, 11.1–11.9 |
| §6 `nooma consolidate` (PR 12) | R6.1–R6.4 | 12.1–12.13 |
| §7 Handoffs discharged/deferred | (not spec requirements) | Inherited-handoffs section; Handoffs-left-open section |
| §8 What this spec does not require | (not tasked — `m2d`/M3/M4/M5) | Handoffs-left-open section |
| Migration 0003 (owner ruling, not a spec requirement per se) | design §3.2 | 4.1–4.10 |
| Chain-merge discipline (`nooma-pr`, not a spec requirement) | — | Every PR's two chain-merge-check items |

---

## Review Workload Forecast

| Field | Value |
|-------|-------|
| Estimated changed lines | ~3,070 implementation + docs, ~5,710 test (design's own guess, §13 — summed directly from the fourteen-row forecast table below) |
| 400-line budget risk | **Low across all fourteen links.** Tallest is PR 5 at ~330 (0.83× the ceiling); every other link sits at 0.6× or below. No link is flagged Medium or High |
| Chained PRs recommended | Yes — fourteen links, chained by design (`design.md` §13: *"Chained PRs: required. Yes."*) |
| Delivery strategy | Chained PRs, per owner ruling taken 2026-08-09 — no `size:exception` needed anywhere in the chain |
| Chain strategy | `stacked-to-main`, per owner ruling taken 2026-08-09 |
| Decision needed before apply | No — both the chain strategy and the migration decision were ruled on before this document was written |

**Per-link estimate (implementation + docs / test lines), transcribed from `design.md` §13:**

| # | Branch | Impl + docs | Tests (est.) | vs. 400 ceiling |
|---|---|---|---|---|
| 1 | `feat/ports-unit-weight-count` | ~170 | ~460 | 0.43× |
| 2 | `feat/ports-unit-relation-reads` | ~150 | ~400 | 0.38× |
| 3 | `feat/ports-selfmodel-config-state` | ~240 | ~470 | 0.60× |
| 4 | `feat/schema-current-state-source` | ~80 | ~90 | 0.20× |
| 5 | `feat/store-unit-relation-repos` | ~330 | ~520 | **0.83×** (tallest) |
| 6 | `feat/store-selfmodel-config-state` | ~300 | ~450 | 0.75× |
| 7a | `feat/brain-consolidate-runner` | ~230 | ~430 | 0.58× |
| 7b | `feat/brain-consolidate-decision-log` | ~190 | ~430 | 0.48× |
| 8 | `feat/brain-phase-io-transitions` | ~200 | ~400 | 0.50× |
| 9 | `feat/brain-phase-io-connect` | ~300 | ~500 | 0.75× |
| 10a | `feat/core-derive-prompt` | ~150 | ~300 | 0.38× |
| 10b | `feat/brain-phase-io-derive` | ~250 | ~430 | 0.63× |
| 11 | `feat/brain-phase-io-reweight-patterns` | ~250 | ~450 | 0.63× |
| 12 | `feat/cli-consolidate` | ~230 | ~380 | 0.58× |
| **Total** | | **~3,070** | **~5,710** | — |

**No link crosses the ceiling — confirmed, and stated with the same caution `design.md` §13
states it.** These are pre-code guesses (design's own words: "of the same kind this project has
measured wrong 1.3×–4.3× seven times"). Four split boundaries are **pre-drawn**, not conditional
on a judgment call at apply time, exactly as `design.md` §13 fixes them:

- PR 5 → 5a (`unitrepo.go`, ~210) | 5b (`relationrepo.go`, ~120), checkpoint at task 5.7.
- PR 6 → 6a (`configrepo.go`+`staterepo.go`, ~150) | 6b (`selfmodelrepo.go`, ~150), checkpoint at
  task 6.7.
- PR 9 → 9a (the adapter, ~160) | 9b (the judge, ~140), checkpoint at task 9.3.
- PR 11 → 11a (`reweight`, ~110) | 11b (`pattern_eval`+`learn`, ~140), checkpoint at task 11.3.

PR 7's split (7a/7b) is **not conditional** — it is drawn as two separate links in the fourteen-PR
total from the start, because the runner and the `DecisionAction` vocabulary widening are two
separately reviewable units (`design.md` §13's own stated reason).

**`sdd-apply` should treat each split checkpoint task as a stop-and-report gate**, the same
discipline `m2a`/`m2b`'s own PR4/PR4a checkpoints used: measure real `git diff --stat` at the
checkpoint, and split at the pre-drawn boundary rather than let the PR run to the ceiling before
deciding.
