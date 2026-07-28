# Design — Complete the build harness

Technical design for the `complete-harness` change. It answers **how**, at architectural level,
the proposal ([`proposal.md`](proposal.md)) gets built. It writes no implementation code; the
snippets below exist only to pin an API shape or a file format.

Reading order: §1 fixes the ground truth this design was verified against; §2 is the decision
record; §3–§10 are the concrete designs; §11 surfaces what the docs do not say or say wrong;
§12 is the per-PR manifest that `sdd-tasks` consumes.

---

## 1. Ground truth this design was verified against

Every claim below was read from the repository or from the module cache, not assumed.

| Fact | Where verified | Consequence for this design |
|---|---|---|
| `go.mod` has zero dependencies, `go 1.26.4` | `go.mod` | PR 2 introduces the first one |
| `depguard.core-purity` allows only `$gostd` and `github.com/rengo/nooma/internal/core` | `.golangci.yml:33-35` | `internal/core/**` **cannot** import `internal/ports`. Confirmed. Nothing in this design makes it |
| `forbidigo` is scoped by `path-except: internal/core/` | `.golangci.yml:79-81` | It applies **only** inside the core. Store code and tests may call `time.Now` |
| `depguard.core-purity` matches `**/internal/core/**` | `.golangci.yml:28-29` | `test/conformance/` is outside it and may import `internal/core/...` **and** `internal/ports` |
| `driver.Open(dsn string, fn ...func(*sqlite3.Conn) error) (*sql.DB, error)`; the first callback runs on **every** connection the pool opens, and a non-nil error from it fails the connection open | `ncruces/go-sqlite3@v0.35.2/driver/driver.go:132,255-260` | This is the whole structural guarantee for FTS5. See §4.2 |
| `fts5.Register(db *sqlite3.Conn) error` | `.../ext/fts5/fts5.go:12` | ADR-0001 writes `Register(db)`, doc 03 writes `Register(conn)`; both informal, the real signature is this one |
| `_pragma=` is honoured only for DSNs starting with `file:`, and is executed at `sqlite3_open` time on every connection | `.../conn.go:121-140` | The opener must build the DSN itself; a bare path silently drops every PRAGMA |
| When any `_pragma` is present, the driver **stops** applying its default `busy_timeout(1m)` | `.../driver/driver.go:249-254` | If we set PRAGMAs we must set `busy_timeout` ourselves or there is no busy handler at all |
| `conn.ExecContext` with **zero** arguments calls `sqlite3_exec` and runs *all* statements; with arguments it falls back to `Prepare`, which rejects a trailing statement (`TailErr`) | `.../driver/driver.go:398-436` | Migrations must be executed argument-free. `PRAGMA user_version` cannot take a bind parameter anyway |
| `BeginTx` with default isolation and `!ReadOnly` emits `BEGIN <_txlock>` | `.../driver/driver.go:343-374` | `_txlock=immediate` is what makes `db.BeginTx(ctx, nil)` take the write lock upfront |
| Every `internal/**` subpackage contains only `doc.go`; `internal/ports/clock.go` defines `Clock` and `IDGen` | repo tree | The pending-red anchors must live in packages that already exist. `internal/core/vector` does **not** exist |
| `cmd/nooma` prints `nooma <version> (<revision>)` for `nooma version` | `cmd/nooma/main.go:34-52` | The L4 assertion is on shape, not value |
| `test/`, `testdata/` are untracked empty directories | `git`, repo tree | Every directory this change adds must carry a tracked file |

---

## 2. Decision record

Twelve decisions. Each states what was chosen, why, and what was rejected.

### D1 — The store lives in `internal/store/sqlite`, one package, with the SQL as embedded data

**Decision.** A single new package `internal/store/sqlite` (package name `sqlite`) holding the
opener, the migration runner and the errors, with the migration SQL in a data subdirectory
`internal/store/sqlite/migrations/` embedded from the runner file.

```
internal/store/
  doc.go                      (exists — namespace doc)
  sqlite/
    doc.go                    package contract: opens a vault, migrates it, reads no domain row
    open.go                   Open, Vault, DSN construction, PRAGMAs, the fts5 init callback
    migrate.go                //go:embed migrations/*.sql — parse, plan, apply
    errors.go                 VersionError
    migrations/
      0001_core_tables.sql
      0002_learning_and_search.sql
```

**Why.** `go:embed` can reach a subdirectory of the package directory, so the SQL files can be
plain reviewable `.sql` next to the runner without a package of their own. Keeping opener and
runner in one package is what lets `Vault` hold its `*sql.DB` **unexported** (§7): the runner
needs the handle, and a separate package could only get it through an exported accessor — the
exact hole this design is trying to close.

**Rejected — a flat `internal/store` package.** `internal/store` is a namespace in
`docs/06-harness.md` §1 ("migrations, repos, vec, fts, lockfile"); making it also a package
forecloses `internal/store/lock` and `internal/store/backup` later, and turns the exported-surface
golden (§7.3) into a list nobody can keep.

**Rejected — a separate `internal/store/migrations` package.** It would require
`migrations.Apply(ctx, *sql.DB)` to be exported, which means any package that can build a
`*sql.DB` can migrate a vault, and the boundary of §7 becomes a comment again.

**Note on naming.** `Open` takes the path to the `nooma.db` **file**, not the vault directory.
Resolving where that file lives is M0's job (proposal §3.1). The type is still called `Vault`
because that is doc 03's vocabulary; when the lockfile lands it will be a sibling concern
(`internal/store/lock`) that takes the vault *directory*. `doc.go` says so, so the distinction
is recorded rather than discovered.

### D2 — One `*sql.DB` per vault, obtained through `driver.Open` with an init callback

**Decision.** `Open` builds a `file:` DSN, calls `driver.Open(dsn, initConn)` where `initConn`
registers FTS5, and stores the resulting `*sql.DB` in an unexported field.

**Why.** The init callback is invoked by the driver's `connector.Connect` for *every* connection
the pool creates, and an error from it aborts that connection's open (verified, §1). So
"a connection without FTS5" is not a discouraged state — it is an unrepresentable one. See §4.2.

**Rejected — the low-level `sqlite3.Open` returning a single `*sqlite3.Conn`.** It gives no pool
and, decisively, it puts `fts5.Register` back in the caller's hands, which is precisely the
failure mode ADR-0001's spike notes warn about ("late, and far from the cause").

**Deferred, with an explicit trigger — a split reader/writer handle.** The standard SQLite/Go
shape is a writer pool of 1 plus a reader pool of N. No gate in this change needs it and no read
path exists yet. Trigger to revisit: M0's first concurrent read path, or the moment `Stats()`
shows more open connections than expected. Each ncruces connection carries its own translated
SQLite instance, so connection count is a memory decision, and `Stats()` exists so it can be
measured rather than guessed.

### D3 — The PRAGMA set, and what is deliberately not set

| PRAGMA | Value | Why |
|---|---|---|
| `busy_timeout` | `5000` (ms) | WAL allows exactly one writer; a second must **wait**, not fail. Set **first** because the driver's own docs order it first, and because the driver drops its default 1-minute timeout the moment any `_pragma` appears — omitting it would leave the vault with *no* busy handler. 1 minute is far too long for an interactive brain; 5 s rides out any single write (the spike measured 2,817 writes/s) |
| `journal_mode` | `wal` | Doc 03 conventions. Readers do not block the writer, and it is the precondition for the hot backup of ADR-0001 criterion 4. It is a persistent property of the file, so this is a no-op after the first open — issuing it per connection is idempotent and keeps a freshly created vault correct |
| `foreign_keys` | `on` | Doc 03 conventions. It is **per connection and off by default in SQLite**. `relations`, `unit_embeddings`, `self_beliefs.source_unit_id` and `measurements.ref_unit_id` all carry `ON DELETE` behaviour that simply does not exist on a connection that skipped it |
| `_txlock` | `immediate` | `database/sql` starts `BEGIN DEFERRED`. A deferred transaction that reads and then writes must upgrade its lock, and if another connection wrote in between SQLite returns `SQLITE_BUSY_SNAPSHOT` **immediately — the busy handler does not retry it**. `BEGIN IMMEDIATE` takes the write lock upfront so `busy_timeout` actually applies |
| `synchronous` | **not set** | SQLite's default (`FULL`) is kept. Trading durability for throughput in a personal memory vault is the wrong trade, and the spike hit 2,817 units/s at the default. Recorded here so a future PR that sets `NORMAL` has to argue for it instead of inheriting it |
| `SetMaxOpenConns` etc. | **not set** | See D2 |

Cost of `_txlock=immediate`, stated: read-only transactions also take the write lock. Today the
only transactions are migrations, so the cost is zero. It is revisited with D2's trigger.

The DSN is built with `net/url` escaping, never with string concatenation — a vault path with a
space, a `?` or a `#` would otherwise be silently truncated or reinterpreted as query parameters.

### D4 — Two migrations, one transaction each, `user_version` re-read inside the transaction

**Decision.** Forward-only migrations named `NNNN_snake_case.sql`, contiguous from `0001`.
`0001_core_tables.sql` and `0002_learning_and_search.sql` (owner decision). One transaction per
migration; the version is re-read **inside** each transaction before applying. Full algorithm in §5.

**Why one transaction per migration and not one for the whole run.** A failure at `0002` leaves a
vault at version 1: a consistent, resumable, *representable* state that the runner is designed to
continue from — and therefore a state that gets tested. One big transaction would roll back to 0,
throw away correct work, and hold a write lock for the length of the whole chain on a large vault.
`docs/06-harness.md` §7 ("an old vault migrates itself") describes a stepwise runner.

**Why the version is re-read inside the transaction.** Two `nooma` processes can open the same
vault before the single-writer lockfile exists (it is M0, proposal §3.1). Both would read
`user_version = 0` outside a transaction and both would try to apply `0001`; the second one dies
on `table units already exists`. Re-reading under `BEGIN IMMEDIATE` — which is a write lock —
makes the loser observe the winner's commit and skip. **This is what makes the runner safe without
the lockfile**, which is exactly the scope boundary the proposal drew.

**Rejected — a `schema_migrations` table.** `PRAGMA user_version` is mandated by doc 03 and
doc 06 §7, is a single header integer written transactionally, and needs no bootstrap migration.
A table would need its own creation step outside any version, which is the one migration that can
never be transactional.

### D5 — Two goldens, because the two comparisons have different tolerances

**Decision.** The schema golden is **two** generated files:

- `testdata/schema/structure.golden` — a structural projection: object inventory plus sorted
  column sets. Compared against `docs/03-data-model.md`.
- `testdata/schema/ddl.golden` — the DDL as SQLite stored it, lightly normalized and sorted.
  Compared only against its own previous self.

**Why.** The proposal's R4 says the doc-03 comparison must never be byte-exact. The generalization
that makes this design work: **byte-exactness is correct when both sides are machine-generated and
fatal when one side is hand-written prose.** Splitting the artifact by audience gets both — the
markdown gate stays structural and unbreakable by whitespace, and the drift detector keeps the
resolution to notice a dropped `NOT NULL` or a changed partial-index predicate, which a structural
projection cannot express.

**Rejected — one golden used for both.** Either the markdown gate becomes fragile or the drift
detector becomes blind. There is no single tolerance that serves both readers.

### D6 — The gate is a chain of two links, each machine-checked

```
migrations ──(L3: apply to an empty vault, dump, compare)──▶ structure.golden + ddl.golden
                                                                      │
                                        (L2: parse both sides, compare)│
                                                                      ▼
                                                       docs/03-data-model.md
```

The proposal's objection — "a golden generated from the implementation is tautological" — is
answered by the second link, not by making the first one cleverer. Link 1 is L3 (needs SQLite).
Link 2 is **L2**: it compares two text artifacts and needs no database, which is why it is the
cheapest level that still proves something real (`nooma-testing` decision gate).

### D7 — The scope boundary is enforced in three layers, none of them a comment

Type (unexported handle) → imports (depguard) → surface (an exported-API golden). Full design in
§7. The load-bearing layer is the depguard rule: **only `internal/store/**` and
`test/integration/**` may import `github.com/ncruces/go-sqlite3...` or `database/sql`**. It is
repo-wide, enforced by `make lint` for every contributor including an external one, and it is the
executable form of the tree comment in `docs/06-harness.md` §1.

### D8 — The pending-red gate compiles the tagged package and matches the compiler output against a committed symbol list

`go test -c -o /dev/null -gcflags=-e -tags pendingimpl ./test/conformance/`, driven by
`scripts/pending-red.sh` and `test/conformance/pending_symbols.txt`. Full mechanism, both failure
modes and the residual risk in §8.

**Rejected — `go build -tags pendingimpl ./test/conformance/...`.** `go build` does not compile
`_test.go` files, so it would succeed unconditionally: a gate that can never fail.

### D9 — Anchor symbols must live in packages that already exist

If a pendingimpl test imports a package that does not exist, the compiler emits a module/package
resolution error and **never prints `undefined:`**, so the matcher of §8 would have nothing to
match. All anchors are therefore chosen from `internal/core/unit`, `internal/core/recall` and
`internal/ports`, which exist today (with only `doc.go`). `internal/core/vector` does **not**
exist and must not be used as an anchor.

### D10 — Every structural scan asserts a non-empty corpus before asserting its property

I13 reads migration files from disk; I01/I03 include a literal tree scan; the golden-set test
walks `testdata/`. Each of them first asserts it found at least one file (and, for I13, that it
found the `learning_signals` table at all). Without that guard, moving a directory turns the test
green — the vacuous pass the owner already rejected once. This is a repo-wide rule for this change,
not a per-test nicety.

### D11 — Golden sets get types + a loader + `format_example.json` (owner decision, spec-aligned)

Go types, a stdlib-only loader, and `format_example.json` per directory (proposal §4.5; spec
R10.1/R10.3/R10.4). This supersedes an earlier owner-decision-5 call that had this design specify
`format.md` only — that call traced back to a mis-relay during planning (the proposal's resolved
open question 4 assumption, "types + loader + examples," reached this phase as "format.md and no
loader"; see tasks.md Conflict C2 for the full record). The owner has since decided explicitly,
directly against the corrected assumption, that types + loader + `format_example.json` are in
scope, so this decision now agrees with spec §10 rather than overriding it.

**Alternative considered and rejected**: `format.md` only, carrying the JSON shape as prose plus
the acceptance rules an eventual M1 loader would have to satisfy. Rejected because prose drifts
from the format it describes with no build-time signal when it does — the whole point of I14
(doc 06 §5) is that a broken case must be *caught*, and a markdown description catches nothing.
`format_example.json` plus a loader that calls `json.Decoder.DisallowUnknownFields` turns the
format into a machine-checked contract the moment this change lands, and M1 consumes the types
directly instead of restating the shape from prose. This is the same reasoning that already put
the exported-API golden (§7.3) in scope rather than leaving the store's surface as prose in a doc.

**Placement**: the loader is I/O — it opens and reads `format_example.json` from disk — so it
cannot live in `internal/core` (`.golangci.yml`'s `forbidigo` block bans `os`/`io`/`ioutil` calls
there, scoped by `path-except: internal/core/`, and `depguard`'s `sqlite-containment` rule keeps
`internal/core/**` from importing anything this package would need). It lives in
`test/support/goldenset` instead, alongside this change's existing `test/support/schema`
precedent: a stdlib-only support package, outside `internal/core`, that is not itself
`internal/core` and so is free to touch the filesystem, and that both `test/conformance` (this
change) and M1's implementation can import later without internal/core ever seeing it. See §10 for
the concrete shape.

`TestHarness_GoldenSetFormatsDeclared` (L2, D10's non-empty-directory guard) and
`TestGoldenSetFormatExamples` (L1, proposal §6's original test, restored) are **both** kept —
they are not redundant: the former asserts the three directories exist with a non-empty `format.md`
and a `cases/` directory, the latter asserts the loader actually parses the example and rejects an
unknown field. Neither subsumes the other.

### D12 — `make check` stops claiming to be CI, and gains `make check-all`

`check: lint test build` is documented as "Run everything CI runs" and will be false the moment CI
gains the integration, golden, pending-red and coverage jobs. `check` stays the fast loop (its
comment is corrected to say so) and `check-all` mirrors the blocking PR jobs. Leaving the current
comment in place would be the same class of lie this whole change exists to remove.

---

## 3. Package layout and dependency map

```
cmd/nooma/                      (unchanged this change)
internal/
  core/**                       untouched — no PR in this chain adds a core statement
  ports/                        untouched — anchor package only
  store/
    doc.go                      (exists)
    sqlite/                     NEW — the only package in the repo that speaks SQLite
      migrations/*.sql          NEW — data, embedded
test/
  conformance/                  L2 — untagged doc.go + untagged tests + pendingimpl tests
  integration/                  L3 — untagged doc.go + `integration` tests
  e2e/                          L4 — untagged doc.go + `e2e` tests
  support/
    schema/                     NEW — stdlib-only parsers for both sides of the doc-03 gate
    goldenset/                  NEW — stdlib-only types + loader for the golden-set formats (§10)
testdata/
  schema/                       NEW — structure.golden, ddl.golden, store_api.golden
  recall/ classify/ llm/        NEW — format.md + format_example.json + cases/.gitkeep
scripts/
  pending-red.sh                NEW
  core-coverage.sh              NEW
```

Allowed import edges introduced by this change:

| From | May import | Enforced by |
|---|---|---|
| `internal/store/sqlite` | `database/sql`, `github.com/ncruces/go-sqlite3{,/driver,/ext/fts5}`, stdlib | depguard `sqlite-containment` (§7.2) |
| `test/integration` | the above **plus** `internal/store/sqlite`, `test/support/schema` | same rule, explicit exception |
| `test/conformance` | `internal/core/**`, `internal/ports`, `test/support/schema`, `test/support/goldenset`, stdlib | not restricted (outside `internal/core/**`) |
| `test/support/schema` | stdlib **only** | reviewed; it has no reason to grow one |
| `test/support/goldenset` | stdlib **only** | reviewed; it has no reason to grow one |
| anything else | **not** `database/sql`, **not** the driver | depguard `sqlite-containment` |

`internal/core/**` gains nothing and imports nothing new. `internal/core` still cannot import
`internal/ports`, and this design never asks it to.

---

## 4. The connection opener

### 4.1 Exported surface

```go
package sqlite // internal/store/sqlite

// Vault is an open SQLite database file, migrated to the schema this binary
// carries. It reads and writes no domain row: the handle is unexported and this
// package exports no way to run arbitrary SQL. See docs/06-harness.md §1.
type Vault struct {
    db   *sql.DB
    path string
}

// Open opens dbPath, applies the operational PRAGMAs, registers FTS5 on every
// connection, and migrates the vault forward. Resolving where dbPath lives is
// the caller's job.
func Open(ctx context.Context, dbPath string) (*Vault, error)

func (v *Vault) Close() error
func (v *Vault) Path() string
func (v *Vault) SchemaVersion(ctx context.Context) (int, error)   // PRAGMA user_version
func (v *Vault) Check(ctx context.Context) error                  // connection self-check, §4.3
func (v *Vault) Stats() sql.DBStats                               // pool counters, D2

// VersionError reports a vault whose schema is newer than this binary supports.
// Open returns it having modified nothing.
type VersionError struct{ VaultVersion, BinaryVersion int }
```

That list is the whole surface, and it is frozen by `testdata/schema/store_api.golden` (§7.3).
None of it can read or write a domain row.

### 4.2 Why a connection without FTS5 is unrepresentable

```go
func Open(ctx context.Context, dbPath string) (*Vault, error) {
    dsn := buildDSN(dbPath)                       // file: URI, url-escaped, PRAGMAs of D3
    db, err := driver.Open(dsn, func(c *sqlite3.Conn) error {
        return fts5.Register(c)                   // runs on EVERY connection the pool opens
    })
    ...
}
```

Three properties, each verified against the driver source (§1), compose into the guarantee:

1. The callback runs on every connection `connector.Connect` creates — not on the first one, on
   every one.
2. A non-nil error from the callback makes that connection's open **fail**. There is no degraded
   connection to hand out.
3. `buildDSN` is unexported and `Open` is the only constructor, so no caller can produce a
   `*sql.DB` for a vault by another route — and depguard (§7.2) makes "another route" a lint error
   anywhere else in the repo.

So the invariant is held by the driver plus a lint rule, not by a test and not by discipline. The
tests exist to prove the mechanism is wired, not to be the mechanism.

There is a fourth, quieter guarantee worth naming: **migration `0002` creates `units_fts`**, so
the earliest consumer of FTS5 is the migration itself. A vault opened by a connection without the
extension fails at `Open`, not months later on a recall query. The "late, and far from the cause"
failure mode ADR-0001 records collapses to "the vault does not open".

### 4.3 `Check` and the tests that prove it

`Check` runs the same probe the real failure would hit, on a connection taken from the pool:

```sql
CREATE VIRTUAL TABLE temp.nooma_fts5_probe USING fts5(c);
DROP TABLE temp.nooma_fts5_probe;
```

`temp.` keeps it out of the vault entirely, and it touches no domain row. On a connection without
the extension it fails with `no such module: fts5` — the exact production symptom.

Three L3 assertions, and the second is the one that matters:

| Test | Asserts | Why it is not vacuous |
|---|---|---|
| `TestOpenRegistersFTS5` | `v.Check(ctx)` returns nil | — |
| `TestFTS5MissingWithoutRegistration` | a raw `driver.Open(dsn)` on the *same file*, with **no** init callback, fails the same probe with `no such module: fts5` | This is the control. It demonstrates the failure the opener prevents, so the first test is proving something |
| `TestFTS5AvailableAcrossPoolConnections` | `Check` from many goroutines all return nil, and `Stats().OpenConnections > 1` was observed | Belt and braces. Stated as such in the test comment: the guarantee is D2, not this test |

### 4.4 PRAGMA verification

`TestOpenAppliesPragmas` (L3) reads back `journal_mode`, `foreign_keys` and `busy_timeout` through
a raw connection opened by the test on the same file — because `foreign_keys` and `busy_timeout`
are **per connection**, the test must assert them on a connection the *opener* produced, not on its
own. The seam: the test asserts `journal_mode` (a file property) on its own raw connection, and
asserts the per-connection PRAGMAs by observing behaviour through the vault — an FK violation
against the migrated schema must be rejected. Proposal §6 records the red as
`journal_mode = delete, foreign_keys = 0`, which is exactly what a DSN missing its `_pragma`
parameters produces.

---

## 5. The migration runner

### 5.1 Embedding, naming, validation

```go
//go:embed migrations/*.sql
var migrationFS embed.FS
```

`NNNN_snake_case.sql`, zero-padded to four digits, contiguous from `0001`, version = `atoi(NNNN)`.

`parseMigrations` runs at `Open` (not in `init()`; a panic at import time is hostile and untestable)
and rejects:

- a gap, a duplicate version, a version `< 1`, or a name that does not match the pattern;
- a file containing `COMMIT` or `ROLLBACK`, or `BEGIN` used as a transaction verb (`BEGIN;`,
  `BEGIN TRANSACTION|DEFERRED|IMMEDIATE|EXCLUSIVE`) — the runner owns the transaction. Note the
  deliberate carve-out: `BEGIN ... END` inside a `CREATE TRIGGER` body is legal and must pass;
- a file containing `PRAGMA user_version` — the runner owns the version.

Each of those is an L1 table-driven test over synthetic inputs, plus one L1 test that runs
`parseMigrations` on the **real** embedded set and asserts it yields exactly versions 1..2 with
non-empty SQL (D10's guard).

### 5.2 The algorithm

```
current := PRAGMA user_version                      (read on the pool, no transaction)
target  := max(migration versions)

if current > target  -> return &VersionError{current, target}      // nothing is opened, nothing is written
if current == target -> return nil                                 // no transaction is opened at all

for each migration m with m.Version > current, in ascending order:
    tx := db.BeginTx(ctx, nil)                      // BEGIN IMMEDIATE — the write lock, upfront
        v := PRAGMA user_version                    // re-read INSIDE the transaction (D4)
        if v >= m.Version { rollback; continue }    // another process won the race; skip
        if v != m.Version-1 { rollback; return fmt.Errorf(...) }   // unreachable-by-design, so it is checked
        tx.ExecContext(ctx, m.SQL)                  // argument-free: sqlite3_exec runs every statement
        tx.ExecContext(ctx, "PRAGMA user_version = " + strconv.Itoa(m.Version))
    tx.Commit()
```

Two non-obvious constraints, both verified in §1 and both easy to break later:

- **`ExecContext` must be called with no arguments.** With arguments the driver falls back to
  `Prepare`, which refuses anything after the first statement. A migration file is many statements.
- **`PRAGMA user_version = N` cannot be parameterized.** `N` is formatted from an `int` parsed out
  of a file name embedded in the binary, so there is no injection surface; the design records why
  the string concatenation is safe rather than leaving a reader to wonder.

`PRAGMA user_version` is stored in the database header and written transactionally, so the version
bump and the DDL commit or roll back together. SQLite DDL is transactional, so a failed migration
leaves the vault at the previous version with no partial objects.

### 5.3 State matrix

| Vault state | `user_version` | Behaviour |
|---|---|---|
| Empty / newly created | 0 | Applies 1, then 2. Two transactions |
| Partially migrated | 1 | Applies 2 only. `0001` is not re-run |
| Already current | 2 | No transaction is opened. `Open` is a read |
| Ahead of the binary | 3+ | `*VersionError`. Refuses. No transaction, no write, no `Close` side effect. Message names both numbers and says nothing was modified |
| Two processes racing | 0 / 0 | The loser blocks on `BEGIN IMMEDIATE`, then observes the winner's version and skips. Both `Open` calls succeed; the schema is applied once |

The downgrade case is the one where being wrong corrupts a real person's vault, so it is checked
before anything else and it opens nothing.

### 5.4 A constraint this design accepts and records

`foreign_keys=ON` is set per connection (D3) and cannot be toggled inside a transaction, so a
future migration that needs SQLite's 12-step table-rebuild procedure cannot turn FKs off the
documented way. The escape hatch needs **no runner change**: such a migration begins with
`PRAGMA defer_foreign_keys = ON;`, which *is* settable inside a transaction and defers enforcement
to `COMMIT`. Recorded here so the first person who hits it does not conclude the runner is broken.

---

## 6. The schema goldens

### 6.1 Generation (L3)

`TestSchemaGolden` in `test/integration/`:

1. Migrate an empty vault in `t.TempDir()` through `sqlite.Open`.
2. Open a **raw** connection to that file (the test is allowed to; §7.2), registering FTS5 on it so
   that reading a schema containing a virtual table cannot surprise us.
3. `SELECT type, name, sql FROM sqlite_master` plus `PRAGMA table_info(<name>)` per table.
4. Project, normalize, sort (§6.2), and compare against the two golden files — or rewrite them when
   `-update` is passed.

`make schema-golden` is exactly that test with `-update`. CI then runs
`make schema-golden && git diff --exit-code -- testdata/schema`, the same shape as the
`templ generate` gate doc 06 §6 already describes: the gate is "regenerating changes nothing".

### 6.2 Normalization rules — `structure.golden`

**What is extracted.** For every row of `sqlite_master`:

| Rule | Effect |
|---|---|
| Drop rows where `sql IS NULL` | Removes `sqlite_autoindex_*` and other auto-created objects. They are consequences of `UNIQUE`/`PRIMARY KEY`, not declared schema; keeping them couples the golden to SQLite's internals |
| Drop names with the `sqlite_` prefix | SQLite's own bookkeeping |
| Drop FTS5 shadow tables: collect the set `V` of virtual-table names first, then drop any object whose name is `v + "_" + suffix` for some `v ∈ V` | `units_fts_data`, `units_fts_idx`, `units_fts_docsize`, `units_fts_config` are FTS5 internals whose set changes between SQLite versions. This is the rule that keeps the golden stable across a driver bump |
| Kind is read from the normalized DDL prefix | `CREATE TABLE` → `table`, `CREATE VIRTUAL TABLE` → `virtual_table`, `CREATE UNIQUE INDEX` → `unique_index`, `CREATE INDEX` → `index`, `CREATE TRIGGER` → `trigger`, `CREATE VIEW` → `view` |
| Columns come from `PRAGMA table_info(<name>)` — **not** `table_xinfo` | `table_info` returns declared columns only; `table_xinfo` would add FTS5's hidden `units_fts` and `rank` columns, which nobody declares in doc 03 |
| Only the column **name** is projected | Types, `NOT NULL`, defaults, FK clauses and index predicates live in `ddl.golden`. Asserting them against hand-written markdown means writing an SQL parser, and R4 says what happens to that gate within a month |

**How it is sorted.** Objects by `(kind rank, name)` with rank
`table < virtual_table < index < unique_index < trigger < view`. Columns **by name**, not by
ordinal: column order is not what the doc-03 comparison is about, and the ordinal order is
preserved in `ddl.golden` anyway. So both properties are covered and neither gate is fragile.

**The format.** Two-space indent, one object or column per line, `\n` endings:

```
# nooma schema structure golden — generated by `make schema-golden`, do not edit.
# Compared against docs/03-data-model.md by TestHarness_SchemaMatchesDoc03 (L2).
schema_version 2

table config
  column consolidation_enabled
  column consolidation_last_run_at
  ...
table units
  column confidence
  column content
  ...
virtual_table units_fts
  column content
index idx_decision_log_occurred
unique_index idx_units_unique_active_insight
trigger units_fts_ad
trigger units_fts_ai
trigger units_fts_au
```

The SQLite version is **not** written into the file — it is logged by the job instead. Putting it
in the golden would produce a diff on every driver bump and train reviewers to accept golden diffs
without reading them.

### 6.3 Normalization rules — `ddl.golden`

Minimal, because both sides of this comparison are machine-generated:

- take `sqlite_master.sql` verbatim, including its `--` comments and its line structure;
- trim trailing whitespace per line, trim the statement, append `;`;
- sort by the same `(kind rank, name)`; one blank line between objects;
- the same exclusion rules as §6.2 (null `sql`, `sqlite_` prefix, shadow tables).

Line structure is preserved on purpose: a changed column must be a one-line diff, not a
150-character line that a reviewer scrolls past. Comments are kept because migrations are
immutable, so a comment-only churn is impossible by construction.

### 6.4 Parsing the doc-03 side

`test/support/schema` (stdlib only, package `schema`):

```go
type Kind string  // "table" | "virtual_table" | "index" | "unique_index" | "trigger" | "view"
type Object struct { Kind Kind; Name string; Columns []string }

func Marshal(objs []Object) []byte              // structure.golden
func ParseGolden(b []byte) ([]Object, error)
func ParseMarkdown(b []byte) ([]Object, error)  // docs/03-data-model.md
func Diff(doc, golden []Object) []Difference
```

`FromSQLite` deliberately does **not** live here: it needs `database/sql`, and keeping this package
stdlib-only means the L2 gate links no driver. It lives in the L3 test file.

`ParseMarkdown`, precisely:

1. **Fence extraction.** Take fenced blocks whose info string is exactly `sql`. Doc 03's ` ```go `
   block (the `fts5.Register` snippet) is skipped by that filter.
2. **Comment stripping.** Remove `--` to end of line, **string-aware** (a `--` inside a
   single-quoted literal is data). `''` is the escape.
3. **Statement splitting.** Character scan tracking string state. A `;` ends the statement, except
   inside a `CREATE TRIGGER`: for those, count `BEGIN`/`END` word tokens and terminate at the `;`
   that follows the balancing `END`. This is the fiddly part, and it is why the package has its own
   L1 tests (§6.6).
4. **Filter.** Only statements beginning with `CREATE` are considered. Anything else in a `sql`
   fence — a sample query added later — is ignored rather than mis-parsed. This is a documented
   convention for doc 03, not an accident.
5. **Kind and name.** `(?is)^CREATE\s+(TABLE|VIRTUAL\s+TABLE|UNIQUE\s+INDEX|INDEX|TRIGGER|VIEW)\s+
   (?:IF\s+NOT\s+EXISTS\s+)?([A-Za-z_][A-Za-z0-9_]*)`. Case- and newline-insensitive, because doc
   03 writes `CREATE UNIQUE INDEX idx_units_unique_active_insight` with its `ON` clause on the next
   line.
6. **Columns, `CREATE TABLE`.** Text between the outermost matching parens (depth counting,
   string-aware), split on depth-0 commas. Drop items whose first word is `PRIMARY`, `UNIQUE`,
   `CHECK`, `FOREIGN` or `CONSTRAINT` — that is what makes `relations`'
   `UNIQUE (from_unit_id, to_unit_id, type)` a constraint and not a column. The column name is the
   first token, unquoted.
7. **Columns, `CREATE VIRTUAL TABLE ... USING fts5(...)`.** Same split; an item is a column iff it
   contains no depth-0 `=`. So `content` is a column and `content='units'`,
   `content_rowid='rowid'` are options.
8. **Indexes and triggers.** Name and kind only.

### 6.5 What the L2 gate asserts, and what it deliberately does not

Asserts:

- every object declared in doc 03 exists in the golden, with the same kind (**missing** → fail);
- every object in the golden is declared in doc 03 (**undeclared** → fail — this is what forces the
  FTS trigger DDL into doc 03, proposal §4.2);
- for `table` and `virtual_table`, the column **sets** are equal.

Does **not** assert: types, `NOT NULL`, defaults, FK clauses, index columns, index predicates,
trigger bodies. Listed explicitly so that a future contributor "strengthening" the gate has to
argue against a recorded decision instead of drifting into an SQL parser. All of it is visible in
`ddl.golden`, reviewed as a diff.

Failure output is a three-part report, not a boolean:

```
schema does not match docs/03-data-model.md:
  declared in doc 03 but absent from the schema:  trigger units_fts_del
  present in the schema but not declared in doc 03: trigger units_fts_ad
  table units — column sets differ:
    only in doc 03: (none)
    only in schema: archived_at
```

### 6.6 The fragile piece has its own safety net

R4 calls the doc-03 comparison the most fragile piece in the change. The mitigation is not care, it
is coverage: `test/support/schema` is an ordinary untagged package with **L1 table-driven tests**
over synthetic markdown — a trigger whose body contains `;`, a `--` sequence inside a string
literal, a multi-line partial index, a table constraint that looks like a column, an fts5 option
that looks like a column, a `sql` fence containing a `SELECT`. The parser is the thing most likely
to be wrong, so it is the thing with the most tests. Those tests run in `make test`.

---

## 7. The scope boundary, mechanically enforced

> "The store surface this change adds is deliberately anaemic: it can open a vault and migrate it,
> and it cannot read or write a single domain row." — proposal §3.1

Three layers. Each one alone is defeatable; together they make the sentence executable.

### 7.1 Type layer — the handle is unexported

`Vault.db` is unexported and `internal/store/sqlite` exports no method that takes SQL. Go's own
visibility rules mean no package outside `internal/store/sqlite` can execute a statement against a
vault. Adding a repository therefore means adding code **inside** that package — which layer 7.3
makes visible.

### 7.2 Import layer — depguard, repo-wide

New rule in `.golangci.yml`, alongside `core-purity`:

```yaml
sqlite-containment:
  files:
    - $all
    - "!**/internal/store/**"
    - "!**/test/integration/**"
  deny:
    - pkg: github.com/ncruces/go-sqlite3
      desc: "only internal/store opens a vault; FTS5 is opt-in per connection — docs/03-data-model.md"
    - pkg: database/sql
      desc: "only internal/store speaks SQL — docs/06-harness.md §1"
```

depguard matches `deny` entries by import-path prefix, so the single `github.com/ncruces/go-sqlite3`
entry also covers `/driver` and `/ext/fts5`. The rule is scoped to `internal/store/**` rather than
`internal/store/sqlite/**` so that M0's repositories are not blocked by a harness decision, while
everything outside `store` still is.

`test/integration/**` is a **recorded exception, not an oversight**: L3's job is to disprove what
only SQLite can disprove, and the control test of §4.3 must be able to open a connection *without*
the opener's guarantees. The `desc` fields say so.

*Verification note:* depguard's `$all` + `!glob` combination must be confirmed with `make lint` at
apply time — `golangci-lint` is not on `PATH` in the design environment, so this is stated as a
shape, not as a tested config.

### 7.3 Surface layer — an exported-API golden

`TestHarness_StoreAPIUnchanged` (L2) walks `internal/store/**` with `go/parser`, skipping `_test.go`,
collects every exported top-level declaration with its rendered signature, sorts, and compares
against `testdata/schema/store_api.golden`:

```
# nooma store API golden — regenerate with `make store-api-golden`.
# Adding a line here is a deliberate widening of the store surface. Read it as such.
internal/store/sqlite: func Open(ctx context.Context, dbPath string) (*Vault, error)
internal/store/sqlite: func (*Vault) Check(ctx context.Context) error
internal/store/sqlite: func (*Vault) Close() error
internal/store/sqlite: func (*Vault) Path() string
internal/store/sqlite: func (*Vault) SchemaVersion(ctx context.Context) (int, error)
internal/store/sqlite: func (*Vault) Stats() sql.DBStats
internal/store/sqlite: type Vault
internal/store/sqlite: type VersionError
internal/store/sqlite: func (*VersionError) Error() string
```

This is the only piece of the design that goes beyond the proposal's eight in-scope items, and it
is proposed for a specific reason: it is the **only** mechanism that makes "anaemic" a property a
machine can check, and without it §3.1's boundary is a sentence in a document. It costs ~60 lines
plus a golden, and its failure message points at `make store-api-golden`, so a legitimate widening
is one command and a reviewable diff — "a conscious act that gets recorded, not an oversight", in
doc 06 §6's own words about the `no-spec-change` label.

**It is cheap to drop.** If the owner judges it scope creep, layers 7.1 and 7.2 stand on their own
and PR 3 simply loses one test and one golden file.

---

## 8. The pending-red gate

### 8.1 The command

```
go test -c -o /dev/null -gcflags=-e -tags pendingimpl ./test/conformance/
```

- `go test -c` compiles the test binary **including** `_test.go` files, and does not run it.
  `go build` would ignore the test files entirely and always succeed — a gate that cannot fail.
- `-o /dev/null` discards the binary. It is never produced anyway when compilation fails.
- `-gcflags=-e` removes the compiler's "too many errors" cutoff, so every expected symbol appears
  in the output even if the list grows past ten.

### 8.2 `scripts/pending-red.sh`

```sh
#!/usr/bin/env sh
# Asserts that test/conformance's pendingimpl tests FAIL to compile, and fail for
# the expected reason. docs/06-harness.md §8 point 5, made executable.
set -u

PKG=./test/conformance/
SYMBOLS=test/conformance/pending_symbols.txt

out=$(go test -c -o /dev/null -gcflags=-e -tags pendingimpl "$PKG" 2>&1)
status=$?

# Failure mode 1: it compiles. The symbols now exist.
if [ "$status" -eq 0 ]; then
  echo "FAIL: $PKG compiles under -tags pendingimpl."
  echo "The anchor symbols now exist. Promote I01/I03/I21 into the untagged L2"
  echo "suite (docs/06-harness.md §4) and drop the pendingimpl tag, in the same PR"
  echo "that created them."
  exit 1
fi

# Failure mode 2: it fails, but not for the expected reason. A typo also fails to
# compile, and a gate that accepts any failure proves nothing.
missing=0
while IFS= read -r sym; do
  case "$sym" in ''|\#*) continue ;; esac
  if ! printf '%s\n' "$out" | grep -qF "undefined: $sym"; then
    echo "FAIL: expected the compiler to report 'undefined: $sym'. It did not."
    missing=1
  fi
done < "$SYMBOLS"

if [ "$missing" -ne 0 ]; then
  echo "--- compiler output ---"; printf '%s\n' "$out"
  echo "Fix the test, or update $SYMBOLS in the same commit."
  exit 1
fi

echo "OK: $PKG is pending-red for every symbol in $SYMBOLS."
```

Run by `make pending-red` and by the CI job, so a contributor sees locally exactly what CI sees.
POSIX `sh` with `grep`: the script has to run on a bare GitHub runner, so it uses tools that are
guaranteed to be there.

### 8.3 `test/conformance/pending_symbols.txt`

```
# Symbols the pendingimpl conformance tests anchor to. They do not exist yet.
# The compiler must report each one as `undefined:`; scripts/pending-red.sh checks it.
#
# The M0/M1 PR that creates any of these MUST, in the same PR, promote the matching
# test into the untagged L2 suite and remove the line from this file.
unit.Status
unit.AllStatuses
ports.UnitRepo
recall.VectorQuery
recall.VectorIndex
```

The file is self-checking in both directions: a test that stops referencing a symbol makes the
script report "expected `undefined: X`, it did not", and a symbol that comes into existence makes
the package compile and trips failure mode 1.

### 8.4 The three tests and what each anchors to

| Test (file, `//go:build pendingimpl`) | Anchor | What it will assert once it compiles |
|---|---|---|
| `TestI01_FocusIsNeverAPersistedStatus` | `unit.Status`, `unit.AllStatuses` | No member of the status vocabulary is `"focus"`, plus the literal tree scan of doc 06 §4 with D10's non-empty guard |
| `TestI03_UnitsAreNeverDeleted` | `ports.UnitRepo` | Reflection over the interface: no method named `Delete*`. Plus the `DELETE FROM units` tree scan, migrations excluded, with D10's guard |
| `TestI21_VectorSearchFiltersOnModel` | `recall.VectorQuery`, `recall.VectorIndex` | Reflection: a vector query carries a `Model`, and the index is keyed by it |

I21's honest limitation, stated in the test file: reflection proves the invariant is *expressible*,
not that every call site honours it. The behavioural half arrives at M1. Doc 06 §4 already accepts
structural proxies for I01/I03/I13; the owner's decision extends the same treatment to I21.

### 8.5 The failure mode this gate does not catch

If M0 creates the unit status vocabulary under **different names** — `unit.Kind`, `unit.States()` —
the package still does not compile, the gate stays green, and the test is never promoted. Two
mitigations, and an accepted residual:

1. **Renaming or deleting an anchor package is caught automatically.** The compiler would emit a
   package-resolution error instead of `undefined:`, and failure mode 2 fires with the full output.
2. **The contract is placed where the implementer will be looking.** `internal/core/unit/doc.go`,
   `internal/core/recall/doc.go` and `internal/ports/doc.go` each gain a short comment naming the
   pending symbol and pointing at `test/conformance/pending_symbols.txt`. Three comment blocks,
   landing in PR 5.
3. **Residual, accepted and recorded**: adding a *differently named* symbol leaves the gate
   silently green. The gate is self-dismantling, not self-updating.

### 8.6 Two consequences of the build tag

- The tagged files are **never linted**: `golangci-lint` and `go vet` run untagged, so `gofmt`,
  `staticcheck` and the rest never see them. `make pending-red` at least compiles them. Recorded,
  not fixed — running the linter with the tag would make it report the undefined symbols as errors,
  which is the state we are deliberately in.
- `test/conformance/` must always contain at least one **untagged** file or `go build ./...` fails
  with *build constraints exclude all Go files*. `doc.go` is that file, and it lands in PR 3, before
  any tagged file (PR 5). This applies to `test/integration/` and `test/e2e/` identically: **every
  directory under `test/` carries an untagged `doc.go`**, which is also where the level's contract
  is documented.

---

## 9. Test levels, build tags and CI

### 9.1 Wiring, checked against doc 06 §3 and the existing Makefile

| Level | Directory | Tag | Files | Run by |
|---|---|---|---|---|
| L1 | next to the code (`internal/**`, `cmd/**`, `test/support/**`) | none | `*_test.go` | `make test` |
| L2 | `test/conformance/` | none | `doc.go` + untagged `*_test.go` | `make test` |
| L2-pending | `test/conformance/` | `pendingimpl` | tagged `*_test.go` | `make pending-red` (compile only) |
| L3 | `test/integration/` | `integration` | `doc.go` + tagged `*_test.go` | `make test-integration` |
| L4 | `test/e2e/` | `e2e` | `doc.go` + tagged `*_test.go` | `make test-e2e` |

`make test` is `go test -race -shuffle=on ./...`, untagged, so it picks up L1 + L2 and nothing else.
The existing `test-integration` and `test-e2e` targets already point at the right directories and
tags and need no change. No Makefile target is rewritten; three are added.

`test/support/schema` is fully untagged: it is a library with L1 tests, and both the L2 gate and the
L3 generator import it.

### 9.2 New `make` targets

| Target | Command | Why |
|---|---|---|
| `schema-golden` | `go test -tags integration ./test/integration/ -run TestSchemaGolden -update` | Regenerates both goldens. The `-update` flag is registered in the L3 test package |
| `store-api-golden` | `go test ./test/conformance/ -run TestHarness_StoreAPIUnchanged -update` | Same idiom, one flag per test package |
| `pending-red` | `sh scripts/pending-red.sh` | A contributor sees exactly what CI sees |
| `check-all` | `check test-integration pending-red cover` | D12 — mirrors the blocking PR jobs |

`check: lint test build` keeps its behaviour and loses its false comment (D12).

### 9.3 CI job map

Three workflow files, split by trigger rather than by topic — because the triggers genuinely differ.

**`.github/workflows/ci.yml`** (`pull_request` + `push: main`, blocking):

| Job | Steps | §6 gate it satisfies |
|---|---|---|
| `lint` | unchanged | `golangci-lint`, `go vet` |
| `test` | unchanged | L1 + L2 with `-race`. Now also carries `TestHarness_SchemaMatchesDoc03`, `TestI13_*` and `TestHarness_StoreAPIUnchanged` |
| `build` | unchanged | — |
| `integration` | `make test-integration`, then `make schema-golden && git diff --exit-code -- testdata/schema` | **L3 tests** and **Schema golden**, as two named steps in one job. Two jobs would migrate the same vault twice for nothing; the step names keep both gates visible in the UI |
| `pending-red` | `make pending-red` | doc 06 §8 point 5 |
| `coverage` | `sh scripts/core-coverage.sh` | `internal/core` ≥ 90 % |

**`.github/workflows/docs-sync.yml`** (`pull_request` with
`types: [opened, synchronize, reopened, labeled, unlabeled]`): diffs against `github.base_ref`; if
any path matches `internal/core/**` and none matches `docs/02-cognitive-core.md`, the PR must carry
the `no-spec-change` label.

**Why its own file.** `labeled`/`unlabeled` are **not** in the default `pull_request` types, so
without them applying the label would never re-run the job and the PR would stay red forever. Adding
those types to `ci.yml` would re-run lint, test, build, integration, pending-red and coverage on
every label change. A separate workflow is the difference between a gate that works and a gate
people learn to ignore.

**`.github/workflows/main.yml`** (`push: branches: [main]`, non-blocking for PRs, per doc 06 §6
"what does not run on every PR"):

- `e2e` — `make test-e2e`.
- `cross-compile` — matrix over the four ADR-0001 criterion-5 targets (`linux/amd64`,
  `linux/arm64`, `darwin/arm64`, `windows/amd64`) with `CGO_ENABLED=0`, running `go build ./...`.
  This is the payoff of ADR-0001: `ncruces` is pure Go, so the matrix needs no toolchain.

The trailing comment block in `ci.yml` shrinks to the two gates that remain genuinely deferred:
`templ generate` (M4, ADR-0008) and the driver benchmarks (ADR-0001 criteria 6–7).

### 9.4 The coverage job, written to be honest on the day it lands

`scripts/core-coverage.sh`:

1. `go test -coverprofile=coverage.out -coverpkg=./internal/core/... ./internal/core/...`
2. Sum the statement counts from `coverage.out` directly (each non-header line is
   `file:from,to numStmt count`), rather than parsing `go tool cover -func` output.
3. **If the total is 0**: print
   `internal/core has no statements yet — the >=90% floor is armed but vacuous (docs/06-harness.md §3)`
   and exit 0. The vacuity is a log line, not a silent pass.
4. Otherwise compute `covered/total` and fail below 90.0.

Today `internal/core` has zero statements, so `coverage.out` contains only `mode: set` and branch 3
fires. That is the whole point of proposal §4.7's honesty note, and the workflow carries the same
sentence as a comment.

### 9.5 L4 skeleton

`TestBinaryReportsVersion` (`test/e2e/`, tag `e2e`): `go build -buildvcs=false -o $(t.TempDir())/nooma
./cmd/nooma`, run `nooma version`, assert the output matches `^nooma \S+ \(\S+\)\n$`.

`-buildvcs=false` is deliberate: VCS stamping fails on runners with a "dubious ownership" git
config, and the assertion is on the binary's **contract** (`nooma <version> (<revision>)`), not on a
revision value that is not reproducible. Small, and not vacuous: it proves the tag, the build and
the output shape.

---

## 10. Golden-set formats

Per D11 (spec R10.1/R10.3/R10.4), types + loader + `format_example.json`:

```
testdata/recall/    format.md · format_example.json · cases/.gitkeep
testdata/classify/  format.md · format_example.json · cases/.gitkeep
testdata/llm/       format.md · format_example.json · cases/.gitkeep
```

`.gitkeep` is required — `test/` and `testdata/` are untracked today precisely because git does not
track empty directories. `format_example.json` is a sibling of `cases/`, not inside it, so nothing
that walks `cases/` for real corpus data ever mistakes the example for a case (R10.4).

Each `format.md` carries: the JSON shape in a fenced `json` block, the field semantics, and the
acceptance rules the loader enforces (unknown fields rejected; one file per case in `cases/`).
`testdata/classify/format.md` states up front that the corpus must include truncated JSON, a field
with the wrong type and an unknown enum, because those are what will prove I14 (doc 06 §5).

**Types and loader** (`test/support/goldenset`, stdlib only — see §3 for the placement rationale):

```go
package goldenset // test/support/goldenset

type RecallExample struct {
    Query           string   `json:"query"`
    ExpectedUnitIDs []string `json:"expected_unit_ids"`
}

type ClassifyExample struct {
    Input    string `json:"input"`
    Label    string `json:"label"`
}

type LLMExample struct {
    Prompt   string `json:"prompt"`
    Response string `json:"response"`
}

// Load reads path, decodes it as JSON into v, and rejects any field in the
// document that v does not declare.
func Load(path string, v any) error {
    f, err := os.Open(path)
    if err != nil {
        return err
    }
    defer f.Close()
    dec := json.NewDecoder(f)
    dec.DisallowUnknownFields()
    return dec.Decode(v)
}
```

The three field lists above are illustrative placeholders — sdd-apply fills them in from the final
`format.md` field tables (R10.2), which this design does not itself author. The load-time contract
is what matters here: `dec.DisallowUnknownFields()` is what turns "an added, undocumented field"
into a decode error (R10.3's second scenario), and it is the only mechanism in this design that
does — nothing else in the pipeline inspects the JSON's field set.

`TestGoldenSetFormatExamples` (L1, `test/support/goldenset`, R10.3): for each of the three formats,
`Load` on the checked-in `format_example.json` succeeds and populates a value; `Load` on a copy of
that file with one added, undocumented field returns an error.

`TestHarness_GoldenSetFormatsDeclared` (L2, `test/conformance`, R10.1/R10.2/R10.4): the three
directories exist, each has a `format.md` with a non-empty fenced `json` block, each has a
`format_example.json` sibling of `cases/`, each has a `cases/` directory. D10's guard means the
test asserts it found three directories before asserting anything about their content. This test
and `TestGoldenSetFormatExamples` are not redundant (D11): one proves the directories and docs are
declared, the other proves the example actually parses under the loader's rules.

**What M1 inherits.** Proposal §4.5's argument was that a markdown description is not a definition
because nothing executes it. This design no longer concedes that: the loader executes the format
the moment this change lands, so M1 does not restate the shape as Go types from prose — it imports
`test/support/goldenset`'s types directly and gets unknown-field rejection for free.

---

## 11. Findings the docs do not cover, or cover wrongly

Surfaced rather than papered over, as the design brief requires.

### F1 — `units.rowid` is not stable across `VACUUM`, and `units_fts` is keyed on it (real, latent)

`units` has `id TEXT PRIMARY KEY`, so its `rowid` is an ordinary implicit rowid, and SQLite
documents that `VACUUM` **may renumber rowids for tables without an INTEGER PRIMARY KEY**.
`units_fts` is declared `content='units', content_rowid='rowid'`. Meanwhile ADR-0001 criterion 4 and
doc 03's "Operational properties" commit `nooma export` to `VACUUM INTO`.

So a vacuum — including `VACUUM INTO` — can produce a vault whose FTS index points at the wrong
rows. Silent wrong results, not an error.

**Recommendation, and why this change does not fix it.** No gate in this change touches it, and the
fix is cheap *later*: an FTS index is derived data, so a future migration or `nooma doctor` repairs
it with `INSERT INTO units_fts(units_fts) VALUES('rebuild')`. Changing the schema shape now would
mean changing doc 03's declared schema before its first publication, which is a bigger decision
than this change owns.

**What this change should do, at zero cost**: PR 4 already edits doc 03's Search section to add the
trigger DDL. It adds one line there recording that `units.rowid` is not stable across a vacuum and
that the FTS index must be rebuilt afterwards. A landmine becomes a recorded constraint, and the
owner can decide whether it deserves an ADR before M0's `export` lands.

### F2 — `docs/06-harness.md` §1's tree omits `test/` entirely

§3, §4 and the `Makefile` all reference `test/conformance`, `test/integration` and `test/e2e`, but
the tree in §1 lists only `testdata/`. This change also adds `scripts/`. Both belong in that tree, in
PR 1 (the docs slice), so the tree stops being a partial map.

### F3 — `Makefile`'s `check` target claims to run what CI runs

Already false in spirit (CI has three jobs, `check` runs all three), and about to be false in fact.
D12 fixes the comment and adds `check-all`.

### F4 — `docs/03-data-model.md` says "a reasonable `busy_timeout`" without a number

This design fixes 5000 ms and justifies it (D3). It is **not** a doc 02 §13 calibratable: it is
operational, not behavioural, so it does not enter the calibration table. Recorded so nobody later
"discovers" an uncalibrated number.

### F5 — `fts5.Register` is written two ways in the docs

ADR-0001's spike results write `fts5.Register(db)`; doc 03 writes `fts5.Register(conn)`. The real
signature is `Register(db *sqlite3.Conn) error`. Both docs are informally correct and neither needs
editing; the design pins the actual signature so nobody has to go looking.

### F6 — `config` and `learning_state` declare `id INTEGER PRIMARY KEY DEFAULT 1`

SQLite ignores `DEFAULT` on an `INTEGER PRIMARY KEY` (a NULL rowid is auto-assigned). The intent —
a singleton row addressed as `WHERE id = 1` — is documented in doc 03 and enforced by the
application. The migration reproduces doc 03 verbatim and the golden records it faithfully. Noted
only so the reviewer of `0002` does not stop on it.

### F7 — doc 06 §3 lists the single-writer lockfile under L3, which this change does not deliver

Not a contradiction: the lockfile is M0 (proposal §3.1) and its test arrives with it. Recorded so
the L3 level landing without it is not read as an omission. D4's in-transaction version re-read is
what makes the runner correct in the meantime.

---

## 12. Per-PR manifest

The proposal's seven slices, with the files each one owns. Deltas from the proposal are marked ▲.

| PR | Files | Notes |
|---|---|---|
| **1 — docs** | `docs/02-cognitive-core.md` §5 (I21 anchor), `docs/06-harness.md` line 186 + ▲§1 tree (`test/`, `scripts/`), `docs/README.md:32` | ~25 lines |
| **2 — driver + opener** | `go.mod`/`go.sum`, `internal/store/sqlite/{doc,open,errors}.go`, `test/integration/doc.go`, `test/integration/open_test.go`, ▲`.golangci.yml` (`sqlite-containment`), `.github/workflows/ci.yml` (`integration` job, step 1) | L3 red first: `TestFTS5RegisteredOnEveryConnection`, `TestFTS5MissingWithoutRegistration`, `TestOpenAppliesPragmas` |
| **3 — runner + `0001` + golden** | `internal/store/sqlite/migrate.go`, `migrations/0001_core_tables.sql`, `test/integration/migrate_test.go`, `test/integration/schema_golden_test.go`, `test/support/schema/**`, `testdata/schema/{structure,ddl}.golden`, `test/conformance/doc.go`, ▲`test/conformance/store_api_test.go`, ▲`testdata/schema/store_api.golden`, `Makefile` (`schema-golden`, ▲`store-api-golden`), `ci.yml` (`integration` step 2) | `test/conformance/doc.go` lands here so the package is never tag-only |
| **4 — `0002` + doc-03 gate + I13** | `migrations/0002_learning_and_search.sql`, `testdata/schema/*` regenerated, `docs/03-data-model.md` (FTS trigger DDL ▲+ the rowid/VACUUM line, F1), `test/conformance/schema_doc_test.go`, `test/conformance/i13_learning_signal_test.go` | The doc-03 gate is L2 and joins the existing `test` job — no new CI job |
| **5 — pending-red** | `test/conformance/i01_*_test.go`, `i03_*_test.go`, `i21_*_test.go` (all `//go:build pendingimpl`), `test/conformance/pending_symbols.txt`, `scripts/pending-red.sh`, `Makefile` (`pending-red`), `ci.yml` (`pending-red` job), ▲comments in `internal/core/{unit,recall}/doc.go` and `internal/ports/doc.go` | Needs PR 1 (I21 anchor) and PR 4 (untagged package) |
| **6 — golden sets + L4** | `testdata/{recall,classify,llm}/{format.md,format_example.json}` + `cases/.gitkeep`, `test/support/goldenset/{types,loader,loader_test}.go`, `test/conformance/golden_sets_test.go`, `test/e2e/{doc.go,version_test.go}`, `.github/workflows/main.yml` (`e2e` job) | Types + loader + `format_example.json` restored (D11, Conflict C2 resolved in favour of spec.md) |
| **7 — remaining gates** | `scripts/core-coverage.sh`, `ci.yml` (`coverage` job), `.github/workflows/docs-sync.yml`, `main.yml` (`cross-compile` job), ▲`Makefile` (`check-all` + `check` comment) | Independent of 2–6 |

Dependency order is unchanged: `2 → 3 → 4 → 5`, with 5 also needing 1; 1, 6 and 7 independent.

Line-budget impact of the ▲ deltas: PR 3 gains ~80 lines (store-api golden + test), PR 6 stays at
its proposal-baseline size — the loader, types and example fixtures are in scope per D11, so no
delta applies there. PR 3 is the one to watch against the 400-line ceiling; if it crosses, the
store-api golden (§7.3) is the designated split point, since it is independent of the migration
runner.

---

## 13. Risks

Carried from the proposal where still open, plus what this design added.

| # | Risk | State after this design |
|---|---|---|
| R1 | The pending-red gate is a mechanism this repo has never used | Mechanized in §8, both failure modes covered. Fallback unchanged and still cheap |
| R2 | A "fails to compile" gate can pass for the wrong reason | Closed by §8.2 failure mode 2 plus the committed symbol list |
| R3 | The coverage job proves nothing on the day it lands | Unchanged and deliberate; §9.4 makes the vacuity a log line rather than a silent pass |
| R4 | The doc-03 comparison is the most fragile piece | Reduced: two goldens with different tolerances (D5), a projection that cannot see whitespace (§6.2), an explicit list of non-assertions (§6.5), and L1 table-driven tests over the parser itself (§6.6) |
| R5 | Doc 03 does not specify the FTS trigger DDL | Unchanged: PR 4 adds it, same PR |
| R6 | Seven chained PRs, `2→3→4` strictly serial | Unchanged |
| R7 | Cross-compilation is build-only | Unchanged |
| R8 | Two published initial migrations, permanently | Owner decision; D4 states the compensating benefit |
| **R9** | **§8.5**: the gate cannot detect M0 naming the anchor symbols differently — it stays silently green | Mitigated by anchor-package comments and by automatic detection of package renames. Residual risk accepted and recorded |
| **R10** | **F1**: `units.rowid` is unstable across `VACUUM` while `units_fts` is keyed on it | Not fixed here. Recorded in doc 03 by PR 4; recommended for an ADR before `nooma export` lands |
| **R11** | depguard's `$all` + `!glob` combination in `sqlite-containment` (§7.2) is written from the documented behaviour, not from a run — `golangci-lint` is not on `PATH` in the design environment | Must be confirmed with `make lint` on the first commit of PR 2. If the combination does not match, the fallback is a positive `files` list of the packages that may not import the driver |
| **R12** | `TestOpenAppliesPragmas` cannot read a per-connection PRAGMA from outside the pool (§4.4) | Designed around: `journal_mode` is asserted directly, `foreign_keys` is asserted behaviourally through an FK violation. Stated so nobody "simplifies" it into a test that asserts the wrong connection |
| **R13** | The golden is coupled to whichever SQLite version `ncruces` ships | Bounded by the shadow-table exclusion rule (§6.2) and by keeping the version out of the golden file. A driver bump that changes stored DDL text will show as a `ddl.golden` diff — which is the correct place for it to show |

## 14. Verification

Only commands the repo defines, plus the three this change adds.

| Command | Covers |
|---|---|
| `make check` | The fast loop: `lint test build` |
| `make check-all` | ▲ Everything blocking on a PR: the above plus L3, pending-red and coverage |
| `make test` | L1 + L2 — including the doc-03 gate, I13, the store-API golden and the parser's own tests |
| `make test-integration` | L3 — opener, PRAGMAs, FTS5 and its control, the migration state matrix, the schema golden |
| `make test-e2e` | L4 |
| `make schema-golden` | Regenerates both schema goldens; CI asserts the tree stays clean |
| `make store-api-golden` | ▲ Regenerates the store surface golden |
| `make pending-red` | ▲ The §8 assertion, locally identical to CI |
| `make cover` | `internal/core` only |

Red-discipline for the chain is unchanged (proposal §6): every red is observed and recorded in the
PR body — which command, which failure, which line — and a failing conformance test has exactly two
exits, fix the code or change doc 02 and its ADR in the same PR.
