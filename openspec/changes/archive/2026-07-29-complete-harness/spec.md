# Spec — Complete the build harness

Delta specification for the `complete-harness` change. This document states what MUST be true
of the repository after the change is applied, in testable form. It does not prescribe how
(that is `design.md`). Source: `proposal.md`, `docs/06-harness.md`, `docs/03-data-model.md`,
`docs/02-cognitive-core.md`, `CLAUDE.md`.

Scope boundary (binding, from the proposal): the store surface this change adds **can open a
vault and migrate it, and MUST NOT be able to read or write a single domain row.** Every
requirement below is bounded by that line.

---

## 1. Driver dependency

### R1.1 — `ncruces/go-sqlite3` is a real dependency

**MUST**: `go.mod` declares `github.com/ncruces/go-sqlite3` at v0.35.x (per ADR-0001), and at
least one non-test source file imports it.

**Verified by**: `go build ./...` succeeds; `go.sum` contains the driver's hashes.

**Scenario**:
- GIVEN a clean checkout with no extra tools installed
- WHEN `go build ./...` runs
- THEN it succeeds and the resulting binary/packages link the driver

### R1.2 — No CGO requirement

**MUST**: the driver is pure Go/wasm — no `CGO_ENABLED=1` requirement anywhere in the build,
CI, or Makefile.

**Verified by**: CI builds and tests run with default `CGO_ENABLED` (unset/0) and pass; the
cross-compilation matrix (§8) builds for non-native GOOS/GOARCH without a C toolchain.

---

## 2. Connection opener

### R2.1 — PRAGMAs applied on every open

**MUST**: opening a vault connection sets `journal_mode=WAL`, `foreign_keys=ON`, and a
`busy_timeout` greater than zero, per `docs/03-data-model.md` Conventions.

**Verified by**: two L3 tests, because `journal_mode` and the other two PRAGMAs cannot be
observed the same way — `journal_mode` is a persistent property of the database file,
while `foreign_keys` and `busy_timeout` are per-connection.

- `TestOpenAppliesPragmas` (`test/integration/`) opens a *second*, independent connection
  to the same vault file and reads `PRAGMA journal_mode` back from it — valid because
  `journal_mode=wal` persists in the file itself.
- `TestOpenPRAGMAsReadBack` (white-box, `internal/store/sqlite/`, PR 2) reads `PRAGMA
  foreign_keys` and `PRAGMA busy_timeout` back from a connection `Open` itself produced,
  reached through the package's own unexported handle — real observation, not a DSN-string
  assertion. It lives inside `internal/store/sqlite` rather than `test/integration/`
  specifically because `Vault` exposes no query surface (design D7), so reading a
  per-connection PRAGMA off a connection `Open` produced is only possible from inside the
  package.

**Note on scope**: an earlier version of this requirement's design note (design.md §4.4)
described the `foreign_keys` mitigation as an FK-violation test "against the migrated
schema." No migrated schema exists until PR 3/4 (the migration runner and its tables), so
that behavioral test cannot be written in PR 2 — it is a separate, later requirement
(tracked as a PR 4 task in `tasks.md`), not a substitute for this one. R2.1 itself is fully
proven in PR 2 by the two tests above, without needing the migrated schema.

**Note on the driver's own default**, because it changes what a test here can prove:
upstream SQLite defaults `foreign_keys` to OFF, but this driver does not. Its WASM build
compiles `SQLITE_DEFAULT_FOREIGN_KEYS 1`
(`ncruces/go-sqlite3-wasm/v3@v3.2.35303/build/sqlite_opt.h:23`), so a bare `file:` DSN with
no `_pragma` at all already reads `PRAGMA foreign_keys` back as `1` — verified in the
vendored source and empirically against a fresh vault. Two consequences:

- **This MUST is still real.** The store requests `foreign_keys=on` explicitly rather than
  inheriting it, because a vendored compile flag is not a contract this project controls;
  a driver upgrade could change it without any signal here.
- **A test whose red assumes the default is off cannot fail**, and would be a mirror test
  in the same family as the DSN-string assertion this change already had to remove. Any
  test proving this requirement must construct the negative case deliberately — an explicit
  `_pragma=foreign_keys(off)` control, or dropping the `REFERENCES` clause from the schema
  under test — and must be watched failing that way.

This corrects a claim carried in design §4.4 and in `tasks.md`'s task 4.3 that
`foreign_keys` "defaults to off per SQLite connection". It was true of SQLite in general and
false of the build ADR-0001 selected.

**Scenario**:
- GIVEN an empty temporary directory
- WHEN the store opens a connection to a vault file inside it
- THEN `journal_mode` reads `wal`, `foreign_keys` reads `1`, and `busy_timeout` reads a
  positive value

### R2.2 — FTS5 registered on every connection

**MUST**: every connection the store opens has `ext/fts5.Register` called on it before the
connection is handed to any caller. This includes connections opened for migrations and for
any future pooled/repeated opens — "every connection" is a per-`Open` guarantee, not a
first-connection-only guarantee.

**Why this is a requirement and not an implementation note**: `docs/03-data-model.md` states
FTS5 is opt-in per connection and a connection that skips it fails late ("no such module:
fts5") only when an FTS query runs, far from the cause. The requirement exists to make that
failure impossible rather than diagnosable.

**Verified by**: L3 test (`TestFTS5RegisteredOnEveryConnection`) that opens a connection
against a real temporary vault and executes a query against the `fts5` virtual table module
(e.g. `CREATE VIRTUAL TABLE ... USING fts5(...)` in a scratch table, or an equivalent
module-presence probe), asserting it does not fail with "no such module: fts5". No test
touches the network or a real LLM (non-negotiable #5).

**Scenario**:
- GIVEN a fresh temporary vault path
- WHEN the store opens a connection to it
- THEN a query that exercises the `fts5` module succeeds without a "no such module" error

### R2.3 — Opener takes a path and resolves nothing

**MUST**: the connection opener's public surface accepts a filesystem path to the vault file
and returns a connection (or error). It MUST NOT resolve a vault path from CLI args, env vars,
or a "portable vault" convention — that resolution is out of scope (§3.1 of the proposal,
owned by M0's `nooma init`).

**Verified by**: inspection of the opener's exported signature (design-time contract test or
compile-time check that the function accepts a path parameter and has no CLI/env/portable
lookups in its body — covered structurally, not behaviorally, since this is a scope boundary
rather than a behavioral invariant).

---

## 3. Migrations

### R3.1 — Migrations are embedded, not read from disk at runtime

**MUST**: migration SQL files are embedded into the binary via `go:embed`. No migration is
read from the filesystem at runtime.

**Verified by**: L3 test that runs against a binary/test process with the vault directory
containing no `.sql` files, confirming migrations still apply (proves they came from the
embed, not the disk).

### R3.2 — Migrations are versioned with `PRAGMA user_version`

**MUST**: the vault's `PRAGMA user_version` reflects the highest migration version applied. An
empty vault starts at `user_version = 0`.

**Verified by**: L3 assertion of `PRAGMA user_version` before and after migration.

### R3.3 — Applying from scratch on an empty vault

**MUST**: opening a brand-new (zero-byte or non-existent) vault file applies every published
migration in order and leaves `PRAGMA user_version` at the highest published version.

**Verified by**: `TestMigrateFromScratchSetsUserVersion` (L3).

**Scenario**:
- GIVEN a path to a vault file that does not yet exist
- WHEN the store opens it
- THEN the file is created, every migration runs in ascending version order, and
  `PRAGMA user_version` equals the number of published migrations

### R3.4 — Applying incrementally (`0 → 1 → 2`)

**MUST**: a vault already at an intermediate version (e.g. `user_version = 1`) applies only
the migrations after that version when opened again, in order, and never re-runs an already
applied migration.

**Verified by**: L3 test that pre-seeds a vault at `user_version = 1` (by applying only
`0001_core_tables.sql` directly), reopens it through the store, and asserts `0002` ran exactly
once and `user_version` becomes 2.

**Scenario**:
- GIVEN a vault at `user_version = 1` (only `0001_core_tables` applied)
- WHEN the store opens it
- THEN only `0002_learning_and_search` runs, and `user_version` becomes 2

### R3.5 — Idempotence when already at the target version

**MUST**: opening a vault that is already at the highest published `user_version` applies zero
migrations and does not error.

**Verified by**: `TestMigrateIsIdempotent` (L3): apply all migrations once, close, reopen,
assert no migration re-executes (e.g. by asserting a second run does not fail on
`table units already exists`, which is what a naive re-run would produce) and
`user_version` is unchanged.

**Scenario**:
- GIVEN a vault already migrated to the latest version
- WHEN the store opens it again
- THEN no `CREATE TABLE` or other migration statement re-executes, and the open succeeds

### R3.6 — Refusing to run backwards

**MUST**: if a vault's `PRAGMA user_version` is higher than the highest version this binary
knows how to apply (an old binary opening a newer vault), the store refuses to open the vault
and returns an explicit error. It MUST NOT attempt to apply migrations, skip forward, or
silently proceed.

**Verified by**: L3 test that sets `PRAGMA user_version` on a temporary vault to a value higher
than any embedded migration, then opens it through the store, asserting an error is returned
and no schema-mutating statement runs.

**Scenario**:
- GIVEN a vault whose `user_version` exceeds the number of embedded migrations
- WHEN the store opens it
- THEN the open fails with an explicit "vault is newer than this binary" class of error, and
  the vault file is not modified

### R3.7 — Forward-only, one file per version, never modified after publication

**MUST**: each migration is a single embedded SQL file named `NNNN_description.sql` where
`NNNN` is a strictly increasing four-digit version matching a corresponding `user_version`
target (`0001_core_tables.sql`, `0002_learning_and_search.sql`, and so on for any future
migration). Once a migration file is merged to `main`, its content is never edited in a later
PR — a schema change is always a new, higher-numbered file (CLAUDE.md non-negotiable #7 in
spirit; §7 conventions: "a published migration is never modified — write the next one").

**Verified by**: this is a process rule the CI cannot fully verify inside a single PR's test
run (it is a property over the git history), but the two initial migrations published by this
change MUST individually match the naming pattern, and their combined effect MUST equal the
schema golden (R4). Design may add a lint/gate over migration file history if it chooses to;
this spec does not require one.

### R3.8 — Migration count for this change

**MUST**: this change publishes exactly two migrations:
- `0001_core_tables.sql` — the *Core tables* section of `docs/03-data-model.md` (`units`,
  `relations`, `triggers`, `timers`, `self_beliefs`, `current_state`, `decision_log`, and their
  indexes).
- `0002_learning_and_search.sql` — the *Learning*, *Measurements*, *System config*, and
  *Search* sections (`learning_signals`, `learning_state`, `relation_thresholds`,
  `calibration`, `measurements`, `config`, `unit_embeddings`, `units_fts`, and their indexes
  and triggers).

**Verified by**: R3.4 (the incremental path exercises exactly this split); the schema golden
(R4) after both migrations equals the full schema of doc 03.

---

## 4. Schema golden

### R4.1 — What is dumped

**MUST**: the schema golden is a dump of every object SQLite reports in `sqlite_schema` (or
equivalent introspection) after applying every published migration to a freshly created, empty
vault: every `TABLE`, `INDEX`, `VIRTUAL TABLE`, and `TRIGGER`, in the form of their defining SQL
or an equivalent structural representation (name, kind, and column set at minimum).

**MUST NOT**: the dump does not include any row data — the vault used to produce it is never
written to beyond migration DDL.

### R4.2 — Normalization is structural, not byte-exact

**MUST**: the comparison between the golden and the doc-03 schema (R4.3) compares **object
names and column sets**, never raw SQL text. Whitespace, statement ordering (beyond dependency
order), and SQLite's own DDL reformatting on storage MUST NOT cause a false failure.

**MUST NOT**: no gate in this change performs a byte-for-byte string comparison between
hand-written markdown SQL and a SQLite-generated dump.

### R4.3 — Golden matches `docs/03-data-model.md`

**MUST**: a CI gate (`schema-golden` job, or equivalent name — exact naming is a design
decision) parses every `CREATE TABLE | INDEX | VIRTUAL TABLE | TRIGGER` statement out of the
fenced ```sql``` blocks in `docs/03-data-model.md`, and for each one asserts the schema golden
(R4.1) declares an object of the same name with the same column set (R4.2). A doc-03 object
absent from the golden, or a golden object with a mismatched column set, fails the gate.

**Verified by**: `TestSchemaMatchesDoc03` (L2, since it reads the embedded migration SQL and
the committed golden file — no live SQLite connection required at this level; L2 rather than
L3 because §4's `nooma-testing` rule: "when torn between L1 and L3, it is L1" — here nothing
requires an open database, so it does not need L3 either).

**Scenario**:
- GIVEN the schema golden generated from the two published migrations
- AND the SQL fenced blocks of `docs/03-data-model.md`
- WHEN the doc-03 comparison runs
- THEN every table, index, virtual table, and trigger declared in doc 03 has a matching object
  in the golden, with the same columns

### R4.4 — Golden is committed and regenerated by a `make` target

**MUST**: the golden file is committed to the repository (not generated fresh on every CI run
from scratch and discarded) and a `make` target regenerates it deterministically from the
current migrations. A PR that changes a migration and forgets to regenerate the golden fails
CI (`TestSchemaGoldenMatches` / the golden-diff gate), because the freshly generated schema
will not match the committed one.

**Verified by**: `TestSchemaGoldenMatches` (L3, since it applies real migrations to a real
temporary vault and dumps the live schema for comparison against the committed file).

**Scenario**:
- GIVEN the committed golden file
- WHEN CI applies the current embedded migrations to a fresh temporary vault and dumps the
  schema
- THEN the dump is identical (per R4.2's structural comparison) to the committed golden

### R4.5 — Golden location

**MUST**: the golden file lives under version control at a fixed, documented path (exact path
is a design decision, e.g. `internal/store/testdata/schema.golden` or `test/golden/schema.sql`)
so its diff is what a PR reviewer sees, per the proposal's review-affordance goal.

---

## 5. The four test levels

Per `docs/06-harness.md` §3 and the `nooma-testing` skill's decision table — restated here as
binding requirements for this change:

### R5.1 — L1, Pure

**MUST**: tests live next to the code they test inside `internal/**` (e.g. `internal/store/`).
No build tag. Touch nothing external — no filesystem beyond what `t.TempDir()` provides, no
network.

**Applies in this change to**: any pure helper this change introduces (e.g. golden-set loader
parsing, if a pure function — see §7).

### R5.2 — L2, Conformance

**MUST**: tests live in `test/conformance/`. No build tag, except the `pendingimpl`-tagged
subset (§6). Untagged L2 tests may use `core` and `brain` fakes but MUST NOT open a real
SQLite connection to a temporary file (that is L3) — reading embedded migration SQL as text is
permitted (that is pure Go string/parse work, not a live connection).

**Applies in this change to**: I13, the doc-03 comparison test (R4.3), the `pendingimpl` I01,
I03, I21 tests (§6).

### R5.3 — L3, Integration

**MUST**: tests live in `test/integration/`, behind the `integration` build tag. Each test
starts from a real, empty temporary SQLite vault (`t.TempDir()` + a fresh file). This is the
only level permitted to open a live SQLite connection.

**Applies in this change to**: connection-opener tests (R2.1, R2.2), migration tests (R3.3–R3.6),
schema-golden generation test (R4.4).

### R5.4 — L4, Smoke E2E

**MUST**: tests live in `test/e2e/`, behind the `e2e` tag. They build and exercise the compiled
binary as a subprocess. They run on merge to `main`, not on every PR (§6 of doc 06).

**Applies in this change to**: `TestBinaryReportsVersion` — builds the binary and runs
`nooma version`, asserting the output shape. This change does not add any other L4 coverage
(the CLI surface beyond `version` is out of scope, §3.1 of the proposal).

### R5.5 — No test touches the network or a real LLM (non-negotiable #5)

**MUST**: at every level, no test makes a network call or invokes a real LLM provider. This
change introduces no provider code, so this requirement is satisfied vacuously for this
change's own tests, and is restated here because it binds every future test added under the
directories this change creates.

---

## 6. Conformance requirements: I01, I03, I13, I21

| Invariant | Level | Tag | Asserts | Red today | Turns green |
|---|---|---|---|---|---|
| I01 | L2 | `pendingimpl` | `status='focus'` never exists as a persisted value — Focus is a query, never a stored status | Compile error: the test references a symbol (e.g. `core.StatusFocus` or the units repository) that does not exist | M0 creates the referenced symbol(s); the pending-red gate then forces promotion to the untagged L2 literal-scan form doc 06 §4 specifies |
| I03 | L2 | `pendingimpl` | Nothing is deleted from `units`: no code path outside the migrations emits `DELETE FROM units` | Compile error: references a symbol (e.g. the units repository) that does not exist | Same as I01 |
| I13 | L2 | none (untagged) | `learning_signals.target_id` has no `REFERENCES` clause — the signal outlives the deletion of its target | Before `0002_learning_and_search.sql` exists: assertion failure — `learning_signals` is not found in the embedded migrations at all | `0002_learning_and_search.sql` declares `target_id` with no FK, inside this change |
| I21 | L2 | `pendingimpl` | Every vector search filters on `model`; embeddings produced by two different models are never compared or fused | Compile error: references the future vector-search symbol that does not exist | M0/M1 creates the vector-search symbol; pending-red gate forces promotion |

### R6.1 — I01 and I03 anchor to non-existent symbols

**MUST**: the I01 and I03 test source files reference at least one symbol from the future
domain layer (e.g. a `core.StatusFocus` constant, or a units repository type) that does not
exist in the repository as of this change. Their red is a compile failure, not a runtime
assertion failure and not a vacuous pass. (Owner decision, not reopened — `docs/06-harness.md`
§8 point 5 is not edited by this change.)

### R6.2 — I21 anchors to the future vector-search symbol

**MUST**: the I21 test references the not-yet-existing vector-search symbol (the function or
type that will perform model-filtered similarity search) and fails to compile for the same
reason as I01/I03.

**Necessary but not sufficient**: once promoted, I21's reflection check proves the invariant is
*expressible* — that `VectorQuery` carries a model and `VectorIndex` is keyed by one — not that
every call site actually honours it. An implementation could add `VectorIndex.Model` and still
ship a `Search()` that ignores it; I21 alone would not catch that. The behavioural proof (a
search against a model-A index rejects or ignores a model-B query) is a separate, non-pending
requirement that arrives with M1's real vector search implementation, not with this change.

### R6.3 — I13 is real, not pending

**MUST**: the I13 test is untagged and reads the embedded migration SQL text (from
`0002_learning_and_search.sql`) to assert `target_id` in `learning_signals` carries no
`REFERENCES`. It goes red before `0002` is written (because `0002` does not exist / the table
is absent from the embed) and green once `0002` is merged, entirely inside this change's PR
chain.

**Verified by**: `TestI13_LearningSignalHasNoFKToTarget` (L2, untagged).

### R6.4 — Test identifiers name the invariant (nooma-testing hard rule 6)

**MUST**: each conformance test's identifier names the invariant and what it verifies, e.g.
`TestI01_FocusIsNeverPersisted`, `TestI03_UnitsAreNeverDeleted`,
`TestI13_LearningSignalHasNoFKToTarget`, `TestI21_VectorSearchFiltersOnModel`.

---

## 7. The pending-red CI job

### R7.1 — Two failure modes, both required

**MUST**: a CI job compiles the `pendingimpl`-tagged package in `test/conformance/` in
isolation (i.e. with the `pendingimpl` build tag active) and:

1. **If compilation succeeds** → the job **fails**, with a message stating that the symbols
   now exist and the tests must be moved into the untagged L2 suite.
2. **If compilation fails** → the job inspects the compiler error output and asserts it names
   every symbol the pending tests are anchored to (R6.1, R6.2). If the compiler error does
   **not** name at least one of the expected symbols (e.g. a typo elsewhere in the tagged
   package produced an unrelated failure), the job **fails**.

Only "compilation fails, and the failure names the expected symbols" is a passing run of this
job.

**Verified by**: this job is itself the verification mechanism — its own correctness is proven
by two probe scenarios recorded in the PR body per the strict-TDD table (§4.7 note in the
proposal): (a) temporarily making the tagged package compile (by stubbing the missing symbol)
must turn the job red; (b) introducing an unrelated compile error (e.g. a syntax typo) must
also turn the job red, distinct in message from (a).

**Scenario A — package compiles (symbols now exist)**:
- GIVEN the `pendingimpl`-tagged package compiles cleanly
- WHEN the pending-red job runs
- THEN it fails, with a message instructing promotion to the untagged L2 suite

**Scenario B — compile error does not name the expected symbols**:
- GIVEN the `pendingimpl`-tagged package fails to compile
- AND the compiler error text does not mention any of the symbols I01/I03/I21 are anchored to
- WHEN the pending-red job runs
- THEN it fails, because a failure for the wrong reason proves nothing

**Scenario C — expected red (passing run)**:
- GIVEN the `pendingimpl`-tagged package fails to compile
- AND the compiler error names at least one of the anchored symbols
- WHEN the pending-red job runs
- THEN it passes

### R7.2 — `test/conformance/` is never tag-only

**MUST**: `test/conformance/` contains at least one file with no build tag (an untagged
`doc.go`, plus the untagged I13 test) at all times during and after this change, so that
`go build ./...` and the untagged `make test` never fail with "build constraints exclude all
Go files". The `pendingimpl`-tagged files (I01, I03, I21) land only after the untagged I13 test
exists in the package, so the package is never tag-only at any commit in the chain.

### R7.3 — Self-dismantling property

**MUST**: the pending-red job's only inputs are (a) the tagged package's compile result and
(b) the compiler error text. It requires no manual update when M0 creates the referenced
symbols — it goes red automatically (Scenario A) the first time any of the anchored symbols
exist, without this change or a future change needing to touch the job's logic.

---

## 8. CI gates (§6 of doc 06)

Every gate below runs on every PR and blocks merge, except where marked otherwise.

| Gate | Requirement | Pass condition | Fail condition |
|---|---|---|---|
| `golangci-lint` | Already wired (out of scope to change); this change's new code must pass it, including `depguard`/`forbidigo` if it ever touches `internal/core` (it does not — §9) | `golangci-lint run` (via `make lint`) exits 0 | Any lint violation, including a hypothetical future `internal/core` import from this change's packages |
| `go vet` | Already wired | `go vet ./...` exits 0 | Any vet finding |
| L1 + L2 tests (`-race`) | R5.1, R5.2, R6, R4.3 | `go test -race -shuffle=on ./...` exits 0 | Any test failure, race, or a `pendingimpl` file leaking into the untagged build |
| L3 tests (`-race -tags integration`) | R5.3, R2, R3, R4.4 | `go test -race -tags integration ./test/integration/...` exits 0 | Any integration test failure |
| `internal/core/` coverage ≥ 90% | R8.1 | Job computes statement coverage; when `internal/core` has zero statements, the job reports that explicitly in the log and does not fail (vacuous case is stated, not hidden); when statements exist, coverage must be ≥ 90% | Coverage below 90% when statements exist; or the zero-statement case silently reported as a pass with no comment |
| `templ generate` clean tree | Out of scope (M4/ADR-0008) | N/A — stays a documented comment in `ci.yml` | N/A |
| Schema golden | R4.4 | Freshly generated dump matches committed golden structurally | Mismatch |
| docs↔code sync | R8.2 | A PR touching `internal/core/**` also touches `docs/02-cognitive-core.md`, or carries the `no-spec-change` label | A PR touches `internal/core/**` without a doc-02 change and without the label |
| Pending-red conformance | §7 (R7.1) | See §7 | See §7 |
| Cross-compilation matrix | R1.2 | Binary builds for the documented target GOOS/GOARCH set without a C toolchain | Any target fails to build |
| L4 e2e | R5.4 | `go test -tags e2e ./test/e2e/...` exits 0, runs on push to `main` only | Any e2e test failure |

### R8.1 — Coverage floor wired now, vacuity stated explicitly

**MUST**: the coverage job is present and active in CI from this change onward. Because
`internal/core` has zero statements at the time this change lands, the job MUST detect the
zero-statement case and report it explicitly in its log output (e.g. "internal/core has 0
statements; coverage floor not yet meaningful") rather than passing silently with no
indication. It MUST NOT divide by zero or error opaquely. It MUST enforce the ≥90% floor once
any statement exists in `internal/core`.

**Verified by**: a probe PR (or equivalent CI dry run recorded in the PR body) demonstrating
both states — zero statements (job passes with the explicit log line) and, if simulated, a
sub-90% state (job fails).

### R8.2 — docs↔code sync gate cannot self-block in this chain

**MUST**: this change introduces the docs↔code sync label-check gate itself, but no PR in this
change's chain touches `internal/core/**` (§9, non-goals) — so the gate, once active, does not
block any PR of this change. This is a consistency requirement on the change's own scope, not
a new mechanism beyond the gate description in the table above.

### R8.3 — Gates land distributed, each self-enforcing on merge

**MUST**: each CI gate in the table above becomes active in the same PR (or an earlier PR in
the chain) as the functionality it verifies — no gate is deferred to a later, unrelated PR
such that a merged PR temporarily lacks the check for the code it just added. (This governs PR
sequencing; the exact PR-to-gate mapping is a `sdd-tasks`/`sdd-design` concern, not restated
here as a numbered PR plan.)

---

## 9. FTS5 registration and the doc-03 DDL gap

### R9.1 — Migration defines the FTS5 sync triggers

**MUST**: `docs/03-data-model.md` states "Lexical: FTS5 synchronized with `units.content` via
triggers" but as of the start of this change contains no trigger DDL. `0002_learning_and_search.sql`
MUST define the triggers that keep `units_fts` synchronized with `units.content` (at minimum:
insert, update, and delete/archival paths that touch `content`).

**Verified by**: L3 behavioral tests (`test/integration/fts5_search_test.go`) against a real,
migrated vault — not just the schema golden's structural proof that the trigger DDL text was
written. At minimum: inserting a `units` row makes its content findable via `units_fts MATCH`
(`units_fts_ai`); updating `units.content` makes the new content findable and the old content
not (`units_fts_au`, delete-then-insert — external-content FTS5 tables have no UPDATE path); a
`DELETE` removes the row from the index (`units_fts_ad`); and archiving a unit — an `UPDATE` of
`units.status`, never a `DELETE` (CLAUDE.md non-negotiable #6) — leaves it exactly as findable
as before, at the same rowid, because archived units are not excluded from read surfaces
(`docs/02-cognitive-core.md`).

### R9.2 — Doc 03 gains the DDL in the same PR

**MUST**: `docs/03-data-model.md`'s Search section is updated, in the same PR that publishes
`0002_learning_and_search.sql`, to include the trigger DDL the migration defines — verbatim or
structurally equivalent to what is committed. This is CLAUDE.md non-negotiable #1 applied to
doc 03 (doc 02 governs *behavior*; this is a schema-doc, but the same "code and doc never
drift silently in different PRs" principle applies, and the proposal explicitly frames it as
"the docs↔code rule applied to ourselves").

**MUST NOT**: no invariant in `docs/02-cognitive-core.md` changes as part of this requirement —
this is doc 03 only.

**Verified by**: R4.3 (the doc-03 comparison gate) — once doc 03 declares the trigger objects,
they become part of what the golden must match, so a missing or mismatched trigger fails the
existing gate rather than needing a separate check.

---

## 10. Golden-set formats

### R10.1 — Three directories, each with a defined, empty format

**MUST**: the following directories exist with the structure below, per the proposal's
resolved assumption (types + loader + validated example fixtures):

```
testdata/recall/    format.md · format_example.json · cases/   (empty)
testdata/classify/  format.md · format_example.json · cases/   (empty)
testdata/llm/       format.md · format_example.json · cases/   (empty)
```

**MUST NOT**: `cases/` contains no files in any of the three directories as of this change —
real corpora are M1's responsibility (proposal §4.5).

### R10.2 — `format.md` defines the format precisely enough to add a case without guessing

**MUST**: each `format.md` documents, at minimum: the file naming convention for a case inside
`cases/`, every required and optional field with its type, and any cross-field constraint
(e.g. which fields are mutually exclusive).

**MUST**: `testdata/classify/format.md` additionally states up front that the eventual corpus
must include deliberately broken cases — truncated JSON, a field with the wrong type, an
unknown enum — because those are what prove I14 (`docs/06-harness.md` §5).

### R10.3 — `format_example.json` is a valid instance, parsed with unknown fields rejected

**MUST**: a Go type plus a loader parses `format_example.json` for each of the three
directories successfully, and rejects a modified copy of that example containing an added,
undocumented field (unknown-field rejection).

**Verified by**: `TestGoldenSetFormatExamples` (L1 — pure parsing, no I/O beyond reading the
committed fixture file, which is filesystem access to a fixed test asset rather than a live
external dependency).

**Scenario**:
- GIVEN `testdata/recall/format_example.json` (and the equivalent for `classify` and `llm`)
- WHEN the loader parses it
- THEN it succeeds and produces a populated Go value

- GIVEN a copy of `format_example.json` with one extra, undocumented field
- WHEN the loader parses it
- THEN it fails

### R10.4 — Example lives outside `cases/`

**MUST**: `format_example.json` is a sibling of `cases/`, not inside it, so it is never
mistaken for real corpus data by anything that iterates `cases/`.

---

## 11. L4 skeleton

### R11.1 — `test/e2e/` proves the tag, the build, and the binary contract

**MUST**: `test/e2e/` contains at least one test, behind the `e2e` build tag, that builds the
`nooma` binary and runs it with an argument that already exists today (`version`), asserting
the output shape (non-empty, matches an expected pattern).

**MUST NOT**: this change does not add e2e coverage for `init`, `serve`, `capture`, `recall`,
`doctor`, or `export` — those commands do not exist yet (§3.1, out of scope).

**Verified by**: `TestBinaryReportsVersion` (L4).

---

## 12. Scope boundary as a requirement

### R12.1 — The store surface cannot touch domain rows

**MUST**: no package this change introduces exposes a function or method that reads or writes
a row in `units`, `relations`, `triggers`, `timers`, `self_beliefs`, `current_state`,
`decision_log`, `learning_signals`, `learning_state`, `relation_thresholds`, `calibration`,
`measurements`, `config`, or `unit_embeddings`. Migrations write DDL only (schema, not rows);
the connection opener returns a connection; the schema-golden tooling reads `sqlite_schema`
introspection, not domain tables.

**Verified by**: inspection at design/tasks time (no repository type, no query builder, no
domain struct is introduced by this change) and by the L3 test suite never asserting against
domain-row content — every L3 assertion in this change's scope targets PRAGMAs, `user_version`,
or schema introspection, never a `SELECT` against a domain table's rows.

### R12.2 — No repository, query, or domain type

**MUST**: this change introduces no Go type representing a domain concept (`Unit`, `Relation`,
`Trigger`, etc.) and no repository interface or implementation for one.

---

## 13. Non-requirements (explicitly out of scope)

This change does **not** deliver, and `sdd-tasks` MUST NOT schedule work for:

- Vault path resolution (arg → env → portable → home convention). First consumer: `nooma init`
  (M0).
- The config loader (`.yml` + `.env`). No CI gate reads config in this change.
- The single-writer lockfile (`nooma.lock`). Listed under L3 in doc 06 §3, but its test arrives
  with the lockfile itself (M0).
- Any CLI command beyond the pre-existing `nooma version` (`init`, `serve`, `status`,
  `doctor`).
- Any repository, query, or domain type (`Unit`, `Relation`, `Trigger`, `Belief`, and so on) —
  see R12.2.
- The vector index or brute-force dot-product search implementation (ADR-0012). I21 lands as a
  doc-02 anchor (§14) and a `pendingimpl` conformance test (§6) — never as working code.
- `templ generate` clean-tree gate (M4, ADR-0008) — stays a documented comment in `ci.yml`.
- Driver benchmarks as a permanent CI job (separate ADR-0001 follow-up).
- Populating `testdata/*/cases/` with real corpus entries (M1).
- Any change to `docs/06-harness.md` §8 point 5 wording.
- Any behavioral invariant change in `docs/02-cognitive-core.md` other than the I21 anchor
  (§14) — no I01–I20 wording changes.

---

## 14. Doc alignment

### R14.1 — I21 anchor added to doc 02

**MUST**: `docs/02-cognitive-core.md` gains a sub-bullet under §5 point 2 (hybrid recall)
stating the "one model per search" rule, matching the wording drafted in the proposal §4.4:

> **One model per search.** Vector similarity is only defined between embeddings produced by
> the same model. A vault can hold two models at once while a reindex is in progress, so every
> vector search filters by model, and vectors from two models are never compared or fused. See
> ADR-0003, ADR-0012.

**MUST**: `docs/06-harness.md` line 186's `Doc 02` column for I21 changes from
`ADR-0003, ADR-0012` to `§5`, consistent with every other invariant row's format.

**Verified by**: doc review (no automated gate is required by this spec beyond what R4.3/§8
already check; this is a documentation-content requirement, not a schema requirement).

### R14.2 — Stale ADR-0001 status fixed

**MUST**: `docs/README.md:32` (or wherever it currently states ADR-0001 is `Proposed`) is
corrected to `Accepted`, matching `docs/adr/README.md` and `docs/adr/0001-sqlite-driver.md`.

### R14.3 — `docs/06-harness.md` §4 table row for the new invariants stays consistent

**MUST**: no row in the §4 invariant table (I01–I21) changes its **behavioral** wording as part
of this change — only I21's `Doc 02` column (R14.1). §8 point 5 is untouched (owner decision).

---

## 15. Verification commands

Only the commands the repository already defines are used to verify this change; no new
top-level command is invented beyond the two `make` targets design may add for golden
regeneration and local pending-red assertion (proposal §7):

| Command | Verifies |
|---|---|
| `make check` | Exactly what CI runs: `lint test build` — the full gate set except L3/L4/coverage, which have their own targets |
| `make test` | L1 + L2, `go test -race -shuffle=on ./...` |
| `make test-integration` | L3, tag `integration` |
| `make test-e2e` | L4, tag `e2e` |
| `make cover` | `internal/core` only, ≥ 90% floor (vacuous today, R8.1) |

`golangci-lint` is not assumed to be on `PATH`; verification always goes through `make lint` or
`make check`.

---

## 16. Traceability

| Requirement group | doc 06 DoD point | Proposal §3.2 item |
|---|---|---|
| §1 Driver dependency | §8.1 | 1 |
| §2 Connection opener | §8.1 | 2 |
| §3 Migrations | §8.7 | 3 |
| §4 Schema golden | §8.7 | 4 |
| §5 Four test levels | §8.4 | 5 |
| §6 Conformance (I01/I03/I13/I21) | §8.5 | 7 |
| §7 Pending-red gate | §8.5 | 7 |
| §8 CI gates | §8.6 | 8 |
| §9 FTS5 / doc-03 DDL | §8.7 | 2, 9 |
| §10 Golden-set formats | (not in §8, proposal-only) | 6 |
| §11 L4 skeleton | §8.4 | 5 |
| §12 Scope boundary | §3.1 boundary rule | — |
| §14 Doc alignment | (README/doc06 consistency) | 9 |
