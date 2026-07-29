# Tasks — Complete the build harness

Implementation checklist for `complete-harness`, derived from `proposal.md`, `spec.md` (R1–R16)
and `design.md` (D1–D12, §12 per-PR manifest). `strict_tdd: true` — CLAUDE.md non-negotiable #4
applies to every task below that adds behavior: the test is written first, its red is described
concretely, and a red is never weakened, only fixed or turned into a doc-02 + ADR change (neither
applies to this change, since it touches no invariant wording other than the I21 anchor).

Delivery: **chained PRs**, 7 PRs, dependency order `2 → 3 → 4 → 5` (strict), PRs `1`, `6`, `7`
independent (PR 5 also needs PR 1's I21 anchor). Artifact store: hybrid — this file plus engram
topic_key `sdd/complete-harness/tasks`.

```
1 ─────────────┐
2 ── 3 ── 4 ── 5
6 (independent)
7 (independent)
```

---

## Conflicts surfaced (do not silently resolve)

### C1 — Proposal §6 vs design §5: the red for `TestMigrateFromScratchSetsUserVersion`

Proposal §6's strict-TDD table states this test's red is a **compile error**,
`undefined: migrations.Apply`, implying an exported `migrations.Apply` in a separate package.
Design D1 places all migration logic **unexported**, inside `internal/store/sqlite` (file
`migrate.go`), invoked internally by `Open` — there is no `migrations` package and no exported
`Apply`. Since PR 2 already creates `sqlite.Open`, by PR 3 the true red is a **runtime assertion
failure** (`PRAGMA user_version` reads `0` after `Open`, because no migration logic runs yet),
not a compile error. Task 3.2 below follows the design-accurate red; the proposal's row for this
test is treated as superseded and should not be followed literally during `sdd-apply`.

### C2 — Spec §10 vs design §10/D11: golden-set format weight — RESOLVED in favour of spec.md

`spec.md` R10.1, R10.3 and R10.4 require "types + loader + validated example fixtures"
(`format_example.json`, unknown-field rejection, `TestGoldenSetFormatExamples`) — the proposal's
resolved assumption for open question 4 ("Golden-set format weight," proposal.md lines 367–370).
During planning, the orchestrator mis-relayed that resolved assumption to the `sdd-design` and
`sdd-tasks` phases as "format.md and no loader." Design D11 and this file's original PR 6
(task 6.1) followed that incorrect relay, dropping the loader, the Go types and
`format_example.json`, and replacing `TestGoldenSetFormatExamples` with
`TestHarness_GoldenSetFormatsDeclared`. `spec.md` §10, written directly from the proposal, was
never touched by the mis-relay and stayed correct throughout.

**Resolution**: the owner has now decided explicitly, against the corrected assumption, that
types + loader + `format_example.json` ARE in scope. Rationale: a `format.md` alone is prose, and
prose drifts; `format_example.json` plus a loader that rejects unknown fields is a machine-checked
contract — the same reasoning that already put the exported-API golden (design §7.3) in scope.
`design.md` D11 and this file's PR 6 have been corrected to match `spec.md` §10 (no change to
`spec.md` or `proposal.md` was needed — they were correct). See design.md §10 for the concrete
shape (`test/support/goldenset` holds the types and loader; `json.Decoder.DisallowUnknownFields`
does the rejection) and PR 6 below for the restored tasks.

### C3 — Verification-command allowlist gap

This phase's instructions restrict "the exact verification command per task" to five: `make
check`, `make test`, `make test-integration`, `make test-e2e`, `make cover`. Design introduces
gates that run under none of them: the `pending-red` job (PR 5, task 5.5) and every CI-workflow
wiring task (`.yml` files, PRs 2/3/6/7). These are flagged **"outside the 5-command allowlist"**
in the task text below rather than mapped to a nearby command that would misrepresent what
actually verifies them. `make schema-golden`, `make store-api-golden`, `make pending-red` and
`make check-all` are real, design-mandated targets (§9.2, D12) — they are the regeneration/gate
mechanism, not invented; the five-command constraint governs only the "Verify" line's phrasing.

---

## PR 1 — Docs (independent, ~15 lines)

No behavioral test — R14.1/R14.2 are verified by doc review (spec's own "Verified by"), not an
automated gate.

- [x] **1.1** Add the I21 "one model per search" sub-bullet to `docs/02-cognitive-core.md` §5
      point 2, wording from proposal §4.4 verbatim.
      Requirement: R14.1.
- [x] **1.2** Update `docs/06-harness.md` line 186's `Doc 02` column for I21 from
      `ADR-0003, ADR-0012` to `§5`, matching every other invariant row's format. Do not touch any
      other row's behavioral wording (R14.3, §8 point 5 stays untouched — owner decision).
      Requirement: R14.1, R14.3.
- [x] **1.3** Fix `docs/README.md:32` — ADR-0001 status `Proposed` → `Accepted`, matching
      `docs/adr/README.md` and `docs/adr/0001-sqlite-driver.md`.
      Requirement: R14.2.
- [x] **1.4** Update `docs/06-harness.md` §1's repo tree to list `test/` (with its `conformance/`,
      `integration/`, `e2e/` children) and `scripts/`, which the tree currently omits (design F2).
      Requirement: design finding F2 (not a numbered spec requirement, but assigned to PR 1 by
      design §12).
- [x] Verify: `make check` (lint/build only touch markdown; this confirms nothing else broke).

---

## PR 2 — Driver + connection opener (~250 lines)

- [x] **2.1** [setup, not TDD] Add `github.com/ncruces/go-sqlite3` v0.35.x to `go.mod`; create
      `internal/store/sqlite/doc.go` (package contract: "opens a vault, migrates it, reads no
      domain row" per D1) and `test/integration/doc.go` (untagged marker so the L3 directory is
      never tag-only, mirroring R7.2's rule for `test/conformance/`).
      Verify: `go build ./...` succeeds; `go.sum` gains the driver's hashes (excluded from the
      human-review line budget per proposal §5).
      Requirement: R1.1, R1.2.
- [x] **2.2** [TDD] Write `TestFTS5RegisteredOnEveryConnection` and `TestFTS5MissingWithoutRegistration`
      (control, design §4.3) in `test/integration/open_test.go` (tag `integration`).
      RED: compile error `undefined: sqlite.Open` — the package exists (2.1's `doc.go`) but `Open`
      does not.
      Implement: `open.go` — `Open(ctx, dbPath) (*Vault, error)` building a bare `file:` DSN via
      `driver.Open(dsn, initConn)` where `initConn` calls `fts5.Register(c)` on every connection
      (D2); `Vault.Check(ctx)` running the `temp.nooma_fts5_probe` FTS5 probe (design §4.3);
      `Vault.Close()`, `Vault.Path()`.
      GREEN: `make test-integration`.
      Requirement: R2.2, R2.3.
- [x] **2.3** [TDD] Write `TestFTS5AvailableAcrossPoolConnections` (belt-and-braces, design §4.3).
      RED: compile error `undefined: (*Vault).Stats`.
      Implement: `Vault.Stats() sql.DBStats`.
      GREEN: `make test-integration`.
      Requirement: R2.2 (pooling evidence for D2).
- [x] **2.4** [TDD] Write `TestOpenAppliesPragmas` (L3).
      RED: assertion failure — `journal_mode = delete`, `foreign_keys = 0` (2.2's bare DSN carries
      no `_pragma` parameters yet, per proposal §6's recorded red).
      Implement: `buildDSN` (D3) — `net/url`-escaped `file:` DSN, PRAGMAs in order
      `busy_timeout=5000`, `journal_mode=wal`, `foreign_keys=on`, `_txlock=immediate`.
      GREEN: `make test-integration`.
      Requirement: R2.1.
- [x] **2.5** [structural] Add `errors.go` declaring `VersionError{VaultVersion, BinaryVersion int}`
      and its `Error() string` (used starting PR 3's runner; declared now per D1's package layout
      so `internal/store/sqlite` has its full file set from PR 2).
      Verify: `go build ./...`.
      Requirement: supports R3.6 (consumed in PR 3).
- [x] **2.6** Add the `sqlite-containment` depguard rule to `.golangci.yml` (design §7.2): deny
      `github.com/ncruces/go-sqlite3` and `database/sql` for `$all` except `internal/store/**` and
      `test/integration/**`.
      Verify: run `make lint` **first**, in isolation, to confirm the `$all` + `!glob` combination
      actually excludes `internal/store/**` and `test/integration/**` as documented (residual risk
      R11 — this was written from documented `depguard` behavior, not a run). If it does not behave
      as documented, fall back to a positive `files:` allow-list of every non-store package instead.
      Then `make check`.
      Requirement: R12.1, R12.2 (import layer), risk R11.
- [x] **2.7** [CI wiring] Add `.github/workflows/ci.yml` `integration` job, step 1: `make
      test-integration`.
      Verify: outside the 5-command allowlist (CI workflow config, see C3) — confirmed by the
      job's own run on this PR.
      Requirement: R5.3, R8 gate table row "L3 tests".

**Follow-up work below (2.8–2.14) landed after 2.1–2.7 were first checked off**, either as a
post-hoc bug fix caught before the PR was opened, or as remediation from a pre-PR four-lens
review. Recorded here per CLAUDE.md non-negotiable #4 — a test is not real if the plan that was
supposed to schedule it doesn't know it exists.

- [x] **2.8** [bugfix, TDD] Write `TestOpenTxlockIsImmediate` (L3, white-box,
      `internal/store/sqlite/txlock_integration_test.go`, tag `integration`) — a genuine
      lock-contention regression test for `_txlock=immediate`, confirmed RED against the
      pre-fix `buildDSN` (which folded `_txlock` into a `_pragma` value — silently a no-op,
      `_txlock` is not a PRAGMA) and GREEN after `dsn.go` sends it as its own top-level DSN
      parameter.
      Placement exception (recorded, not silent): this test lives inside package `sqlite`, not
      `test/integration/`, because it must call the real unexported `buildDSN` directly — a
      `test/integration` test would have to reconstruct the DSN by hand, which would pass
      unconditionally regardless of what `buildDSN` actually returns and could never have caught
      the historical bug. Same placement rationale as 2.9 below.
      Requirement: R2.1 (transaction-mode half).
- [x] **2.9** [TDD] Write `TestBuildDSNAbsolutePathHandling` (L1, `dsn_test.go`) + fix `buildDSN`
      to reject a non-absolute `dbPath` with `ErrRelativeDBPath` instead of silently building a
      `file:` URI whose authority component swallows the path's first segment
      (`url.URL{Path: p}.String()`'s structural quirk when `p` does not start with `/`) — a
      relative or `./`-prefixed path previously opened a different, unintended file with no
      error anywhere.
      RED: `TestBuildDSNAbsolutePathHandling`'s relative/`./`-prefixed cases pass against the
      unfixed `buildDSN` (wrong: they open the wrong file silently) and the Windows-drive-letter
      case fails `url.Parse` outright (`invalid port`).
      GREEN: `make test` — `buildDSN` now returns `(string, error)`; absolute POSIX and Windows
      drive-letter paths build a correct `file:///...` URI with an empty authority (verified by
      re-parsing the produced DSN, not by asserting on the raw string); relative paths are
      rejected.
      Requirement: safe-defaults-are-structural (CLAUDE.md non-negotiable #7), R2.3.
- [x] **2.10** [TDD] Write `TestOpenPRAGMAsReadBack` (L3, white-box,
      `internal/store/sqlite/pragma_integration_test.go`) — reads `PRAGMA foreign_keys` and
      `PRAGMA busy_timeout` back from a connection `Open` itself produced, reached through the
      package's own unexported `db` field, closing residual risk R12 (design §4.4) without
      needing PR 3/4's migrated schema. See spec.md R2.1 and design.md §4.4 for the corrected
      proof chain — design §4.4 previously (and incorrectly) described this as proven by an
      FK-violation test, which needs tables that do not exist until PR 4 (see PR 4 task 4.3
      below).
      RED: compile error until `pragma_integration_test.go` exists; the read-back values
      themselves already pass against 2.4's implementation (this task adds observation, not new
      behavior).
      GREEN: `make test-integration`.
      Requirement: R2.1, risk R12 (closed).
- [x] **2.11** [TDD] Write `TestOpenCorruptVaultNamesThePath`
      (`test/integration/corrupt_vault_test.go`) + wrap `Open`'s errors with the vault path and
      what was being attempted (`open.go`); fix stale doc comments in `open.go`/`errors.go` that
      claimed `Open` already migrates the vault (it does not until PR 3).
      RED: opening a garbage-bytes file returns the driver's bare error
      (`sqlite3: invalid _pragma: sqlite3: file is not a database`) with no path and no context.
      GREEN: `make test-integration` — the error now names the vault path.
      Requirement: docs/06-harness.md's own stated concern ("fails late and far from the cause").
- [x] **2.12** [TDD] Restructure `TestBuildDSN`'s two independent assertions (PRAGMA-set presence,
      `_txlock` not folded into `_pragma`) into subtests instead of chaining a second check behind
      a `t.Fatalf` — the prior shape made the second check unreachable (`t.Fatalf` calls
      `runtime.Goexit`), a dead assertion that could never have failed even if the bug it named
      recurred. Also drop the PRAGMA-ordering assertion (incidental string layout with no
      behavior riding on it — presence, not order, is what design D3 requires); presence is kept.
      Requirement: test-quality, no spec requirement (regression-proofing an existing test).
- [x] **2.13** [TDD] Make `TestFTS5AvailableAcrossPoolConnections` deterministic: replace the
      fire-8-goroutines-and-hope shape (a race against scheduling — a fast CI runner can serve
      all 8 `Check` calls from a single pooled connection with no contention at all) with a
      start barrier plus polling `Stats().OpenConnections` for its peak during a bounded burst,
      instead of reading it once after every goroutine has already finished and possibly been
      evicted back down to Go's default idle-connection ceiling.
      Requirement: R2.2 (pooling evidence for D2) — same requirement, hardened test.
- [x] **2.14** [gate fix] Wire `run.build-tags: [integration]` into `.golangci.yml` — without it,
      `golangci-lint run` / `make lint` never see any `//go:build integration` file, silently
      exempting `test/integration/**` and `internal/store/sqlite/*_integration_test.go` from
      `errcheck`, `staticcheck`, `unused`, `ineffassign`, `govet` **and** `depguard`, despite
      docs/06-harness.md §6's "a PR that violates the dependency rule never reaches review"
      promise. Fix the 7 real `errcheck` violations this exposed (unchecked `Close()` in
      `defer`). Also enable `nolintlint` (require-explanation, require-specific) so a bare
      `//nolint:depguard`/`//nolint:errcheck` can never bypass a gate with zero trail — it
      immediately caught one pre-existing unexplained `//nolint:errcheck` in this same PR.
      Verify: `golangci-lint run --build-tags integration ./...` went from 7 issues to 0 after
      the fixes; probed live by temporarily introducing an unused-variable violation in a tagged
      file, confirming `make lint` fails on it, then reverting (output recorded in the PR body).
      Requirement: docs/06-harness.md §6 gate table, row "golangci-lint" (updated in this PR to
      state build-tag coverage explicitly).
- [x] **2.15** [build fix] Add `-shuffle=on` to `test-integration` in the `Makefile`, matching
      `test`'s existing `-race -shuffle=on` — PR 2 is what makes L3 a CI gate for the first time,
      so this is the first PR where the omission mattered.
      Requirement: consistency with the `test` target, no numbered spec requirement.
- [x] **2.16** [bugfix, TDD] Replace 2.9's bidirectional shape-guessing (`isAbsoluteVaultPath`
      recognizing BOTH POSIX and Windows-drive-letter forms regardless of build GOOS) with an
      injected `pathStyle`, mirroring the `Clock` port (design D3 addendum). On POSIX, a backslash
      is an ordinary filename byte and a directory can be named `C:`, so `C:/data/vault.db` and
      `C:\data\vault.db` are valid *relative* POSIX paths — the old regex accepted both as
      absolute and silently rewrote them to `/C:/data/vault.db`, the same defect class task 2.9
      closed, just narrower.
      RED (watched, verbatim): a probe test asserting `buildDSN("C:/data/vault.db")` and
      `buildDSN("C:\data\vault.db")` return `ErrRelativeDBPath` FAILED against the pre-fix code —
      `err=<nil> dsn=file:///C:/data/vault.db?...` for both inputs (confirmed empirically, including
      that `mkdir -p 'C:'/data && echo ok > 'C:'/data/vault.db` creates a real directory on this
      Linux runner, so the case is not hypothetical).
      Implement: `pathStyle` (unexported, `posixStyle`/`windowsStyle`); `buildDSN(dbPath string,
      style pathStyle) (string, error)` takes it as a parameter instead of guessing from `dbPath`'s
      shape; `pathStyleForGOOS()` is the single named place production resolves it from
      `runtime.GOOS`; `Open`'s exported signature is unchanged. `dsn_test.go`'s
      `TestBuildDSNAbsolutePathHandling` is now a single table driven over BOTH styles explicitly
      (including the POSIX-rejects-Windows-shape regression case), asserting on the re-parsed URI's
      `Host`/`Path`, never the raw string; `TestPathStyleForGOOS` asserts the `runtime.GOOS`
      mapping.
      GREEN: `make test` — POSIX rejects Windows-shaped input, Windows accepts drive-letter forms
      and rejects bare/driveless relative paths, both from the same Linux test binary.
      Requirement: safe-defaults-are-structural (CLAUDE.md non-negotiable #7), R2.3 (supersedes
      2.9's mechanism, not its requirement).
- [x] Verify: `make check`, `golangci-lint run --build-tags integration ./...` (0 issues),
      `make test-integration` after `go clean -testcache`.

---

## PR 3 — Migration runner + `0001` + schema golden (~380 lines — watch the 400 ceiling)

Depends on PR 2 (`sqlite.Open` must exist).

- [x] **3.1** [setup] Add `test/conformance/doc.go` (untagged marker, package contract) — lands
      here so `test/conformance/` is never tag-only ahead of PR 5's `pendingimpl` files (R7.2).
      Verify: `go build ./...`.
- [x] **3.2** [TDD] Write `TestMigrateFromScratchSetsUserVersion` (L3, `migrate_test.go`).
      RED (see Conflict C1 — this is the design-accurate red, not the proposal's literal wording):
      runtime assertion failure — `PRAGMA user_version` reads `0` after `Open` on a fresh vault,
      because no migration logic runs yet.
      Implement (minimal, classic TDD): `migrations/0001_core_tables.sql` — doc 03's *Core tables*
      section (`units`, `relations`, `triggers`, `timers`, `self_beliefs`, `current_state`,
      `decision_log` + their indexes, R3.8); `migrate.go` — `//go:embed migrations/*.sql`,
      `parseMigrations` (naming/gap/duplicate/`COMMIT`-`ROLLBACK`-`BEGIN`-`PRAGMA user_version`
      rejection, design §5.1), and the simplest algorithm that applies every migration
      unconditionally, wired into `Open` after PRAGMA setup.
      GREEN: `make test-integration` — `user_version == 1` (only `0001` published at this stage).
      Requirement: R3.1, R3.2, R3.3, R3.7, R3.8 (0001 half).
- [x] **3.3** [TDD] Write `TestMigrateIsIdempotent` (L3).
      RED: second `Open` on an already-migrated vault re-runs `0001` unconditionally (3.2's
      minimal implementation has no version guard) and fails on `table units already exists`.
      Implement: the full state-matrix algorithm (design §5.2/§5.3, D4) — read
      `current := PRAGMA user_version`, compute `target`, skip when `current == target`, one
      `BeginTx` (`BEGIN IMMEDIATE`) per pending migration with `current` **re-read inside** the
      transaction (D4's race guard).
      GREEN: `make test-integration`.
      Requirement: R3.4, R3.5.
- [x] **3.4** [TDD] Write `TestVaultNewerThanBinaryRefusesToOpen` (L3) — R3.6's downgrade-refusal
      scenario. Not named explicitly in proposal §6's table; added here because spec R3.6 requires
      the behavior and design §5.3's state matrix already designs for it.
      RED: assertion failure — nothing in 3.2/3.3's algorithm yet checks `current > target`.
      Implement: `if current > target { return &VersionError{current, target} }`, checked before
      any transaction opens, so a downgrade attempt modifies nothing.
      GREEN: `make test-integration` — error is `*VersionError`, vault file unmodified.
      Requirement: R3.6.
- [x] **3.5** [TDD] Write `TestSchemaGolden` (L3, `schema_golden_test.go`) + the
      `test/support/schema` package (`Kind`, `Object`, `Marshal`, `ParseGolden` — stdlib only;
      `ParseMarkdown`/`Diff` land in PR 4 when the doc-03 side is needed; `FromSQLite` stays in the
      L3 test file per D1, since it needs `database/sql`).
      RED: `testdata/schema/structure.golden` / `ddl.golden` do not exist yet.
      Implement: migrate an empty vault in `t.TempDir()`, open a raw connection registering FTS5,
      dump `sqlite_master` + `PRAGMA table_info`, apply the normalization rules (design §6.2/§6.3
      — drop `sqlite_autoindex_*`/`sqlite_*`, drop FTS5 shadow tables, sort by `(kind rank,
      name)`); add the `make schema-golden` target (design §9.2, run with `-update`).
      GREEN: run `make schema-golden` once to generate and commit both golden files, then `make
      test-integration` passes (fresh dump matches committed golden).
      Requirement: R4.1, R4.2, R4.4, R4.5, R3.8 (0001-only golden at this stage). Residual risk
      R13 noted here (golden coupled to `ncruces`'s SQLite version) — bounded by the shadow-table
      exclusion rule and by keeping the SQLite version out of the golden file, per design §6.2.
- [ ] **3.6** [TDD, ▲ design delta over the proposal] Write `TestHarness_StoreAPIUnchanged` (L2,
      `test/conformance/store_api_test.go`) — owner-approved as the mechanism that makes the
      §3.1 scope boundary machine-checkable.
      RED: `testdata/schema/store_api.golden` does not exist / does not match the actual walked
      exported surface of `internal/store/**`.
      Implement: `go/parser`-based walk of `internal/store/**` (skip `_test.go`), collect exported
      top-level declarations with rendered signatures, sort, compare; add `make
      store-api-golden` target.
      GREEN: run `make store-api-golden` to generate and commit the golden (expected surface:
      `Open`, `Close`, `Path`, `SchemaVersion`, `Check`, `Stats`, `Vault`, `VersionError`,
      `(*VersionError).Error`, per design §4.1/§7.3), then `make test` passes.
      Requirement: R12.1, R12.2 (§7.3, the third and mechanized layer of the scope boundary).
      **Split point**: if this PR's diff approaches or crosses 400 lines, move this task alone to
      its own chained PR/commit — it is independent of the migration runner (design §12).
- [x] **3.7** [CI wiring] Add `.github/workflows/ci.yml` `integration` job, step 2: `make
      schema-golden && git diff --exit-code -- testdata/schema`.
      Verify: outside the 5-command allowlist (see C3) — confirmed by the job's own run.
      Requirement: R4.4 gate.
      Landed ahead of 3.6 (out of numeric order): 3.6 (store-API golden) is being split into its
      own chained PR (see the note below this PR's task list) because the running diff for
      3.1-3.5 alone already crossed the 400-line ceiling before 3.6 was even started; 3.7 is
      cheap CI wiring for the gate 3.1-3.5 already built and belongs with them, not with 3.6.

### PR 3 line-budget finding: the ~380-line forecast was wrong, measured before 3.6 even started

`git diff main --numstat`, excluding `go.sum`, after 3.1-3.5 and 3.7 (3.6 not yet started):
~1,220 review lines (code + tests + CI wiring) plus ~181 lines of committed golden fixtures
(`testdata/schema/{structure,ddl}.golden`, stated separately per the same convention PR 2's
measurement used) — against this file's own ~380-line forecast for the **whole** PR including
3.6. The overage is real and is reported here rather than absorbed silently:

- `migrate.go` (227) + `migrate_test.go` in `internal/store/sqlite` (190, L1 table-driven tests
  for `parseMigrations`/`validateMigrationSQL` — not explicitly scheduled by name in 3.2/3.3's
  task text, but written test-first per strict-TDD discipline for genuinely new parsing/validation
  logic) + `migrations/0001_core_tables.sql` (102) account for ~519 lines.
- `test/integration/migrate_test.go` (165) covers 3.2-3.4's three L3 tests.
- `test/integration/schema_golden_test.go` (278) + `test/support/schema/{schema,schema_test}.go`
  (138 + 87) account for ~503 lines for task 3.5 alone — heavier than its design-estimated share,
  because the L3 test carries its own sqlite_master classification/normalization logic (design
  explicitly keeps `FromSQLite`-shaped code out of the stdlib-only `schema` package, §6.4), plus
  L1 round-trip tests for `Marshal`/`ParseGolden`.

None of this is scope creep — every line traces to a task's explicit "Implement" text or to
strict-TDD discipline applied to new logic the task introduced — but the **estimate** was wrong
by roughly 3x for 3.1-3.5+3.7 alone, before 3.6's own ~80-line forecast is even added. Per this
phase's own instructions, task 3.6 (`TestHarness_StoreAPIUnchanged`) is deliberately **not**
implemented in this PR: it is independent of the migration runner (design §12's designated split
point) and is deferred to its own chained PR. This PR (3.1-3.5, 3.7) is submitted as its own
`size:exception` — the alternative (further splitting 3.1-3.5, e.g. separating the schema-golden
support package from the migration runner) was considered and rejected here because 3.1-3.5 are
strictly sequential within a single migration-and-golden story (schema golden can't exist without
the migration runner that produces the schema it dumps) and splitting them would produce a PR
that doesn't build or pass on its own — unlike 3.6, which genuinely is independent. Owner decision
needed: accept `size:exception` for 3.1-3.5+3.7 as submitted, or direct a different split.

---

## PR 4 — `0002` + doc-03 gate + I13 (~330 lines)

Depends on PR 3.

- [ ] **4.1** [TDD] Write `TestI13_LearningSignalHasNoFKToTarget` (L2, untagged,
      `i13_learning_signal_test.go`).
      RED: assertion failure — `learning_signals` is not found in the embedded migrations at all
      (`0002` doesn't exist yet).
      Implement: `migrations/0002_learning_and_search.sql` — doc 03's *Learning*, *Measurements*,
      *System config*, *Search* sections: `learning_signals` (`target_id` with **no**
      `REFERENCES` clause — this is I13's own invariant), `learning_state`,
      `relation_thresholds`, `calibration`, `measurements`, `config`, `unit_embeddings`,
      `units_fts`, plus the FTS5 sync triggers (insert/update/delete-archival paths touching
      `content` — R9.1) and every doc-03 index for this section.
      GREEN: `make test` — I13 passes because `target_id` genuinely carries no FK (D10 guard: the
      test first asserts `learning_signals` was found before asserting the FK property).
      Requirement: R3.8 (0002 half), R6.3, R6.4, R9.1.
- [ ] **4.2** [TDD] Write `TestHarness_SchemaMatchesDoc03` (L2, `schema_doc_test.go`) + finish
      `test/support/schema.ParseMarkdown`/`.Diff` (fence extraction, string-aware comment
      stripping, trigger-aware statement splitting, column parsing — design §6.4, with its own
      L1 table-driven tests per §6.6 landing in the same commit).
      RED, two stages (both real, both worth recording in the PR body):
        (a) before 4.1's `0002` SQL exists: doc 03 already declares the *Learning/Measurements/
            Search* objects (doc 03 predates this change) that the golden — still 0001-only —
            does not: "declared in doc 03 but absent from the schema: [8 objects]" (matches
            proposal §6's recorded red).
        (b) after 4.1 lands (0002 SQL + regenerated golden) but before this task's doc edit: the
            mismatch flips to "present in the schema but not declared in doc 03: trigger
            units_fts_ai/au/ad" — the R9.1/R9.2 FTS-trigger-DDL gap (matches design §6.5's
            illustrative failure output verbatim).
      Implement: regenerate both schema goldens (`make schema-golden`, now that `0002` exists);
      edit `docs/03-data-model.md`'s Search section to add the FTS5 sync-trigger DDL, verbatim or
      structurally equivalent to what `0002` commits (R9.2) — **in the same PR**; add, at zero
      extra gate cost, the one-line note that `units.rowid` is not stable across `VACUUM` and the
      FTS index must be rebuilt afterward (design finding F1 / residual risk R10, assigned to PR 4
      by design §12).
      GREEN: `make test` (L2 gate) and `make test-integration` (golden regeneration parity).
      Requirement: R4.3, R9.1, R9.2; residual risk R10 (F1).
- [ ] **4.3** [TDD] Write `TestOpenForeignKeysRejectViolation` (L3, `test/integration/`) — the
      FK-violation behavioral test design §4.4 originally described as PR 2's proof of
      `foreign_keys=on`, deferred here because it needs a real foreign-key relationship to
      violate, and no migrated schema exists until `0002` (this PR) lands. PR 2 instead closed
      that gap with `TestOpenPRAGMAsReadBack` (white-box PRAGMA read-back, task 2.10) — this task
      adds the behavioral proof design §4.4 originally wanted, now that it is actually possible.
      RED: insert a row that violates one of `0002`'s foreign keys (e.g. `unit_embeddings` or
      `measurements` against a non-existent `units.id`) against a vault opened by `sqlite.Open`
      and observe it succeeds — `foreign_keys` defaults to off per SQLite connection, so an
      opener that forgot to request it would let this through silently.
      GREEN: after confirming the violation is rejected (`FOREIGN KEY constraint failed`) against
      a vault opened normally through `sqlite.Open`.
      Requirement: R2.1 (behavioral half, closing design §4.4's original intent); spec.md R2.1
      "Note on scope".
- [ ] Verify (whole PR): `make test`, `make test-integration`.

---

## PR 5 — Pending-red conformance (I01, I03, I21) (~200 lines)

Depends on PR 1 (I21 anchor) and PR 4 (untagged `test/conformance/` package already exists, so
adding tagged files never leaves the package tag-only).

- [ ] **5.1** [pending, red-only in this chain] Write
      `test/conformance/i01_focus_never_persisted_test.go` (`//go:build pendingimpl`), identifier
      `TestI01_FocusIsNeverAPersistedStatus`, anchored to `unit.Status`/`unit.AllStatuses` (R6.1).
      RED: compile error `undefined: unit.Status` (and/or `unit.AllStatuses`) — this **is** the
      expected, passing state of the gate (Scenario C in R7.1), not a defect.
      Green (within this change): **none.** It turns green only when a future M0 PR creates
      `unit.Status`, which simultaneously trips the pending-red gate (task 5.5) and forces
      promotion into the untagged L2 suite in that same M0 PR.
      Also add the anchor comment to `internal/core/unit/doc.go`, naming the pending symbol and
      pointing at `pending_symbols.txt` (design §8.5 mitigation 2; residual risk R9).
      Requirement: R6.1, R6.4.
- [ ] **5.2** [pending, red-only] Write `test/conformance/i03_units_never_deleted_test.go`,
      identifier `TestI03_UnitsAreNeverDeleted`, anchored to `ports.UnitRepo`.
      RED: compile error `undefined: ports.UnitRepo`.
      Anchor comment in `internal/ports/doc.go`.
      Requirement: R6.1, R6.4.
- [ ] **5.3** [pending, red-only] Write
      `test/conformance/i21_vector_search_filters_on_model_test.go`, identifier
      `TestI21_VectorSearchFiltersOnModel`, anchored to `recall.VectorQuery`/`recall.VectorIndex`.
      RED: compile error `undefined: recall.VectorQuery` (and/or `recall.VectorIndex`).
      Anchor comment in `internal/core/recall/doc.go`. Requires PR 1's doc-02 §5 "one model per
      search" wording to already exist (conceptual dependency the test's own comment references).
      Requirement: R6.2, R6.4.
- [ ] **5.4** [scaffold] `test/conformance/pending_symbols.txt` — five lines: `unit.Status`,
      `unit.AllStatuses`, `ports.UnitRepo`, `recall.VectorQuery`, `recall.VectorIndex`.
      Verify: consumed by 5.5's script; no standalone command.
      Requirement: R7.1.
- [ ] **5.5** [TDD — the gate itself, self-verifying] `scripts/pending-red.sh` (both failure
      modes, design §8.2) + `make pending-red` target + `.github/workflows/ci.yml`
      `pending-red` job.
      RED before this task: no gate exists — 5.1–5.3's compile failures are unverified by CI.
      GREEN: `make pending-red` exits 0 (Scenario C — the tagged package fails to compile, and
      the failure names every symbol in `pending_symbols.txt`).
      Verify: `make pending-red` — **outside the 5-command allowlist** (see Conflict C3); this
      gate has no equivalent among `make check/test/test-integration/test-e2e/cover`.
      Self-test (R7.1's own "Verified by" requirement — run locally, recorded in the PR body, not
      committed): (a) temporarily stub one missing symbol to prove `make pending-red` goes red
      with the "symbols now exist, promote to untagged L2" message (Scenario A); (b) introduce an
      unrelated syntax typo in a tagged file to prove `make pending-red` goes red with a distinct
      "expected undefined: X, did not find it" message (Scenario B); revert both probes before
      committing.
      Requirement: R7.1, R7.2, R7.3; risk R2 (closed by failure-mode 2); risk R9 (self-dismantling,
      not self-updating — mitigated, not eliminated, by 5.1–5.3's anchor comments).

---

## PR 6 — Golden-set formats + L4 skeleton (~230 lines)

Independent. See Conflict C2 (resolved in favour of spec.md) — types, loader and
`format_example.json` are in scope; design D11 and this PR were corrected to match spec.md's
R10.1–R10.4.

- [ ] **6.1** [TDD] Write `TestHarness_GoldenSetFormatsDeclared` (L2,
      `test/conformance/golden_sets_test.go`).
      RED: `testdata/recall/`, `testdata/classify/`, `testdata/llm/` do not exist (D10 guard: the
      test first asserts it found three directories before asserting anything about their
      content).
      Implement: create the three directories, each with `format.md` (JSON shape in a fenced
      ` ```json ` block, field semantics, the acceptance rules the loader enforces — unknown
      fields rejected, one file per case in `cases/`), `format_example.json` as a sibling of
      `cases/` (R10.4) validating against that shape, and `cases/.gitkeep`; `testdata/classify/format.md`
      additionally states up front that the eventual corpus must include truncated JSON, a
      wrong-typed field, and an unknown enum (R10.2 — these are what prove I14 once M1 populates
      the corpus).
      GREEN: `make test` — three directories found, each `format.md` has a non-empty fenced json
      block, each has a `format_example.json` sibling of `cases/`, each has a `cases/` directory.
      Requirement: R10.1, R10.2, R10.4.
- [ ] **6.2** [TDD] Write `TestGoldenSetFormatExamples` (L1, `test/support/goldenset/loader_test.go`).
      RED: `undefined: goldenset.Load` — compile error, package `test/support/goldenset` does not
      exist yet.
      Implement: `test/support/goldenset/types.go` defines `RecallExample`, `ClassifyExample` and
      `LLMExample` (field shape per each format's `format.md`, task 6.1); `loader.go` defines
      `Load(path string, v any) error`, opening `path` and decoding into `v` with
      `json.NewDecoder(f).DisallowUnknownFields()` (design §10). The test table covers, per
      format: `Load` on the checked-in `testdata/{recall,classify,llm}/format_example.json`
      (task 6.1's fixture) succeeds and populates a value; `Load` on a copy of that file with one
      added, undocumented field returns an error.
      GREEN: `make test`.
      Requirement: R10.3.
- [ ] **6.3** [TDD] Write `TestBinaryReportsVersion` (L4, `test/e2e/version_test.go`) +
      `test/e2e/doc.go` (untagged marker).
      RED: `go test -tags e2e ./test/e2e/...` — no such package.
      Implement: `go build -buildvcs=false -o <tmp>/nooma ./cmd/nooma`, run `nooma version`,
      assert output matches `^nooma \S+ \(\S+\)\n$`.
      GREEN: `make test-e2e`.
      Requirement: R5.4, R11.1.
- [ ] **6.4** [CI wiring] `.github/workflows/main.yml` `e2e` job (`make test-e2e`, push-to-`main`
      only, per doc 06 §6 — not on every PR).
      Verify: outside the 5-command allowlist (see C3) — confirmed by the job's own run on merge.
      Requirement: R5.4 gate table row.

---

## PR 7 — Remaining CI gates (~150 lines)

Independent for code purposes; task 7.5's own verification has a soft ordering dependency on
PRs 2/3/5 (see 7.5's note).

- [ ] **7.1** [probe-verified, no go-test red/green] `scripts/core-coverage.sh` (R8.1) — sums
      statement counts from `coverage.out` directly (each non-header line is `file:from,to
      numStmt count`), not from `go tool cover -func` output; prints the explicit vacuity log
      line and exits 0 when total statements == 0, else enforces the ≥90% floor.
      Verify: `make cover` — today prints `internal/core has no statements yet — the >=90% floor
      is armed but vacuous (docs/06-harness.md §3)` and exits 0.
      Probe (recorded in the PR body, per R8.1's own "Verified by"): simulate a sub-90% state
      locally (point `-coverpkg` at a throwaway low-coverage package) to confirm the script fails
      as expected, then revert before committing.
      Requirement: R8.1.
- [ ] **7.2** [CI wiring] `.github/workflows/ci.yml` `coverage` job (`sh
      scripts/core-coverage.sh`).
      Verify: outside the 5-command allowlist (see C3) — confirmed by the job's own run.
      Requirement: R8.1 gate; R8.2 (no PR in this chain touches `internal/core/**`, so the gate —
      once active — cannot self-block; a consistency check on this change's own scope, not a new
      mechanism).
- [ ] **7.3** [CI wiring] `.github/workflows/docs-sync.yml` — `pull_request` with
      `types: [opened, synchronize, reopened, labeled, unlabeled]`; diffs against
      `github.base_ref`; requires the `no-spec-change` label on any PR touching
      `internal/core/**` without a matching `docs/02-cognitive-core.md` change.
      Verify: outside the 5-command allowlist (see C3) — confirmed by the job's own run. A
      deliberate probe PR (proposal §4.7) should demonstrate a core-only change gets blocked
      without the label; record it in the PR body.
      Requirement: R8.2.
- [ ] **7.4** [CI wiring] `.github/workflows/main.yml` `cross-compile` job — matrix over
      `linux/amd64`, `linux/arm64`, `darwin/arm64`, `windows/amd64`, `CGO_ENABLED=0`,
      `go build ./...`.
      Verify: outside the 5-command allowlist (see C3) — confirmed by the job's own run. This is
      the direct payoff of ADR-0001 (pure-Go/wasm driver, no C toolchain needed).
      Requirement: R1.2; R8 gate table row "Cross-compilation matrix".
- [ ] **7.5** [mechanical, D12] Fix the `Makefile`'s `check` target comment (stop claiming "runs
      everything CI runs" — it does not, once PRs 2/3/5/7's jobs exist) and add
      `check-all: check test-integration pending-red cover`.
      Verify: `make check` and `make check-all` both exit 0. **Note**: `check-all` genuinely
      depends on `test-integration` (PRs 2/3), `pending-red` (PR 5) and `cover` (this PR's own
      7.1) already existing. Although design lists PR 7 as dependency-independent for code
      purposes, this specific task cannot be verified end-to-end until PRs 2, 3 and 5 have
      merged — flagged for `sdd-apply` to sequence accordingly (author the diff any time; run the
      final `make check-all` verification last).
      Requirement: doc 06 harness consistency (D12) — not a numbered spec requirement.

---

## Review Workload Forecast

| PR | Est. changed lines | 400-line budget risk | Notes |
|---|---|---|---|
| 1 | ~15 | Low | Docs only |
| 2 | **~1,056 measured** (`git diff --numstat main...HEAD`, excluding `go.sum`: ~862 code — `_txlock` fix task 2.8 plus tasks 2.9–2.15 remediating a pre-PR four-lens review — + ~184 in this same branch's `spec.md`/`design.md`/`tasks.md` corrections + ~10 in `docs/04-decisions.md`) | **`size:exception` — owner-accepted** | Owner decision: fix every finding in this single PR rather than dropping scope or re-splitting an already-implemented PR. Documented here, not hidden. Superseded the ~750 pre-measurement estimate |
| 3 | ~380 (300 + 80 ▲ for the store-API golden, task 3.6) | **High** | Design's own designated split point is task 3.6 (store-API golden) — independent of the migration runner, move it to its own chained link if the diff crosses 400 |
| 4 | ~345 (330 + ~15 for task 4.3, the FK-violation test deferred from PR 2 — see task 2.10's note) | Medium | |
| 5 | ~200 | Low | |
| 6 | ~230 (restored — Conflict C2 resolved in favour of spec.md; types, loader and `format_example.json` back in scope) | Low | |
| 7 | ~150 | Low | |
| **Total** | **~2,376** (PR 2 now measured, not estimated) | — | `go.sum` (PR 2) excluded from the human-review budget; the schema golden dumps (PRs 3–4) are **not** excluded — their diff is the point of the gate |

- **Chained PRs recommended: Yes** (already the resolved delivery strategy for this change).
- **Decision needed before apply: Yes** — specifically whether PR 3 preemptively splits off
  task 3.6 (store-API golden) into its own chained link, or whether `sdd-apply` authors PR 3 as
  planned and splits only if the running diff actually crosses 400 lines. Recommend the latter:
  monitor PR 3's diff at the 3.5/3.6 boundary and split there if needed, per design §12's own
  guidance.
- **PR 2 crossed the ceiling after the fact**: it left `sdd-apply` at ~531 lines (already over
  400) before this review; the review's own remediation adds more on top. The owner explicitly
  chose `size:exception` over re-splitting a PR that was already implemented and reviewed as one
  unit, rather than dropping any finding to stay under budget.

---

## Residual risks mapped to tasks

| Risk | Where it lands | Task |
|---|---|---|
| R9 — the pending-red gate is self-dismantling, not self-updating (a differently-named anchor symbol leaves it silently green) | Anchor comments in `internal/core/unit/doc.go`, `internal/core/recall/doc.go`, `internal/ports/doc.go` | 5.1, 5.2, 5.3 |
| R11 — depguard's `$all` + `!glob` in `sqlite-containment` is written from documented behavior, not a run | `make lint` confirmation on PR 2's first commit; fallback is a positive `files` list | 2.6 |
| R13 — the schema golden is coupled to whichever SQLite version `ncruces` ships | Bounded by the shadow-table exclusion rule and keeping the SQLite version out of the golden file | 3.5 |
| R10 / F1 — `units.rowid` is unstable across `VACUUM`, `units_fts` is keyed on it | Recorded as a one-line note in `docs/03-data-model.md`'s Search section, zero-cost in this change | 4.2 |
| R2 (proposal) — a "fails to compile" gate can pass for the wrong reason | Closed by `scripts/pending-red.sh` failure-mode 2 (compiler error must name an expected symbol) | 5.5 |
| R12 (design) — `TestOpenAppliesPragmas` cannot read a per-connection PRAGMA from outside the pool | **Closed** — `journal_mode` asserted directly (file property) by `TestOpenAppliesPragmas`; `foreign_keys`/`busy_timeout` asserted by real observation in the white-box `TestOpenPRAGMAsReadBack`, not an FK violation (that behavioral test needed a migrated schema that did not exist yet — see task 4.3) | 2.10 (closed), 4.3 (the behavioral test design §4.4 originally wanted) |

---

## Traceability

Requirement groups → PR, consistent with `spec.md` §16:

| Requirement group | PR |
|---|---|
| §1 Driver dependency | 2 |
| §2 Connection opener | 2 (R2.1's PRAGMA presence + white-box read-back), 4 (R2.1's FK-violation behavioral half, task 4.3) |
| §3 Migrations | 3, 4 |
| §4 Schema golden | 3, 4 |
| §5 Four test levels | 2, 3, 4, 6 |
| §6 Conformance (I01/I03/I13/I21) | 4, 5 |
| §7 Pending-red gate | 5 |
| §8 CI gates | 2, 3, 5, 6, 7 |
| §9 FTS5 / doc-03 DDL | 4 |
| §10 Golden-set formats | 6 |
| §11 L4 skeleton | 6 |
| §12 Scope boundary | 3 (task 3.6) |
| §14 Doc alignment | 1, 4 (partial — F1 note) |

Non-requirements (spec §13): no task above schedules vault-path resolution, the config loader,
the single-writer lockfile, any CLI command beyond `version`, any repository/query/domain type,
the vector index/search implementation, the `templ generate` gate, driver benchmarks as a
permanent job, or populating `cases/` with real corpus data.
