# Proposal — Complete the build harness

Finish [`docs/06-harness.md`](../../../docs/06-harness.md) §9 step 4 so that every rule the
harness declares is executed by a machine: embedded migrations producing exactly the schema of
[`docs/03-data-model.md`](../../../docs/03-data-model.md), a schema golden that stops drift, the
four test levels wired with their tags, the golden-set formats defined, conformance tests for
the structural invariants, and the CI gates of §6 that are still listed as comments today.

This is the last piece of scaffolding before M0. It writes no business logic.

---

## 1. Why now

Steps 1–3 of the build order are merged: the repo skeleton exists, the ADR-0001 spike ran, and
ADR-0001 closed accepting `ncruces/go-sqlite3` with ADR-0012 dropping sqlite-vec. Three things
make step 4 the next honest move:

| Fact | Consequence |
|---|---|
| `go.mod` has **zero** dependencies | ADR-0001 is `Accepted` but nothing depends on the driver it accepted. The decision is not yet real. |
| `.github/workflows/ci.yml` ends with a comment enumerating five missing gates | Half the harness is prose. §6 claims "runs on every PR and blocks the merge" for gates that do not exist. |
| No migration exists | The schema in doc 03 is unexecuted. M0 cannot start: its first bullet is "embedded migrations + `PRAGMA user_version`". |

The guiding principle of doc 06 is *rules a machine does not execute are not rules, they are
intentions*. Right now the dependency rule and the clock port are gates (`depguard`,
`forbidigo` — already wired); everything else in §6 is an intention.

The cost of deferring is asymmetric. Scaffolding written **before** the first domain code is
cheap and uncontroversial. The same scaffolding written after M0 has to be retrofitted onto
code that grew without it, and the conformance discipline of §4 — *the test is written before
the implementation* — becomes unprovable retroactively.

---

## 2. Success criteria

The change is done when [`docs/06-harness.md`](../../../docs/06-harness.md) §8 is satisfied for
every point that step 4 owns:

- [ ] §8.4 — the four levels run with their tags: `make test` (L1+L2), `make test-integration`
      (L3), `make test-e2e` (L4).
- [ ] §8.5 — one conformance test per structural invariant (I01, I03, I13) plus I21, each one
      observed failing **for the right reason**, not vacuously green.
- [ ] §8.6 — every gate in §6 that step 4 owns runs on every PR and blocks the merge.
- [ ] §8.7 — migrations apply from scratch and the schema golden matches doc 03.
- [ ] `make check` stays green on `main` at every point in the chain.
- [ ] `docs/README.md`, `docs/02-cognitive-core.md` and `docs/06-harness.md` §4 agree with
      reality (ADR-0001 status, I21 anchor).

Points §8.1, §8.2, §8.3 and §8.8 are already satisfied and are not touched.

---

## 3. Scope

### 3.1 The boundary rule

Doc 06 §9 does not draw the harness/M0 line sharply, and both step 4 and M0 list "embedded
migrations". The rule this change adopts:

> **In scope**: whatever a CI gate in §6 needs in order to be executable *today*, over a repo
> with no business logic.
>
> **Out of scope**: whatever has its first real consumer in a product behavior.

Applied to the ambiguous cases:

| Piece | Verdict | Why |
|---|---|---|
| Migration SQL + `go:embed` runner + `PRAGMA user_version` | **In** | The schema-golden gate cannot exist without it. |
| A SQLite connection opener (PRAGMAs + `fts5.Register`) | **In** | Nothing can apply a migration or run an L3 test without one. Takes a path; resolves nothing. |
| Vault path resolution (arg → env → portable → home) | **Out** | M0. Its first consumer is `nooma init`. |
| Config loader (yml + `.env`) | **Out** | M0. No gate reads config. |
| Single-writer lockfile | **Out** | M0. Doc 06 §3 lists it under L3, but the test arrives with the lockfile. |
| CLI `init` / `serve` / `status` / `doctor` | **Out** | M0. The L4 skeleton uses `nooma version`, which already exists. |
| Any repository, query, or domain type | **Out** | M0+. No gate needs to read or write a domain row. |
| Vector index / brute-force search (ADR-0012) | **Out** | M1. I21 lands as a doc anchor and a pending conformance test, not as an implementation. |
| `templ generate` clean-tree gate | **Out** | M4 / ADR-0008. Stays a documented comment in `ci.yml`. |
| Driver benchmarks as a permanent CI job | **Out** | §6 excludes them from per-PR CI; ADR-0001 criteria 6–7 are a separate follow-up. |

The store surface this change adds is deliberately anaemic: **it can open a vault and migrate
it, and it cannot read or write a single domain row.**

### 3.2 In scope

1. **Driver dependency.** `github.com/ncruces/go-sqlite3` v0.35.x in `go.mod` (ADR-0001).
2. **Connection opener.** WAL, `foreign_keys=ON`, `busy_timeout`, and `fts5.Register` on
   **every** connection (doc 03 §"FTS5 is opt-in per connection").
3. **Embedded migrations**, forward-only, versioned with `PRAGMA user_version`, producing
   exactly the schema of doc 03.
4. **Schema golden** + the gate that compares it against doc 03.
5. **The four test levels** wired with their build tags (§3), each with at least one test that
   proves something real.
6. **Golden-set formats defined, sets empty**: `testdata/recall/` (ADR-0010),
   `testdata/classify/` (ADR-0002), `testdata/llm/`.
7. **Conformance tests** for I01, I03, I13 and I21.
8. **CI completion** per §6: L3 integration, `internal/core` coverage floor, schema golden,
   docs↔code sync label check, cross-compilation matrix.
9. **Doc alignment**: the I21 anchor in doc 02, the §4 table row, the stale ADR-0001 status in
   `docs/README.md:32`.

### 3.3 Explicit non-goals

No business logic. No `internal/core` code — which also means **no PR in this chain triggers
the docs↔code sync gate it installs**, so the gate can land without self-blocking. No provider,
no channel, no HTTP handler, no UI. No change to `docs/06-harness.md` §8 point 5.

---

## 4. Approach

Seven workstreams, each one landing with the CI job that enforces it. Distributing the gates
instead of batching them into a final "CI completion" PR means every slice is self-enforcing
from the moment it merges, and it removes a single PR that would otherwise depend on all the
others.

### 4.1 Migrations and the schema golden

Two migrations, not one: `0001_core_tables.sql` (doc 03 *Core tables*) and
`0002_learning_and_search.sql` (*Learning*, *Measurements*, *System config*, *Search*). This is
a deliberate split, and the reason is not only the 400-line PR ceiling:

- A single migration never exercises the `user_version 0 → 1 → 2` path. Two do, from the first
  day, which is the path every future vault will take.
- Each migration is reviewed against its own section of doc 03 instead of as one 190-line wall.

Both are published by this change and neither is ever modified afterwards (§7, CLAUDE.md).

The golden is a dump of the schema produced by applying every migration to an empty vault. It
is generated by a `make` target and committed, so its **diff** is what a reviewer reads — which
is exactly the review affordance §6 asks for. A golden generated from the implementation is
tautological on its own, which is why it is paired with the doc-03 comparison gate.

### 4.2 The doc-03 comparison gate

Structural, not byte-exact. It extracts every `CREATE TABLE|INDEX|VIRTUAL TABLE|TRIGGER` from
the fenced `sql` blocks of doc 03 and asserts the golden declares an object with the same name
and the same column set. Byte-exact comparison of hand-written markdown SQL against a SQLite
dump would fail on whitespace and normalization noise and would be weakened within a month.

**Known gap**: doc 03 says *"Lexical: FTS5 synchronized with `units.content` via triggers"* but
does not write the trigger DDL. The migration must define those triggers, so doc 03 gains the
DDL in the same PR — the docs↔code rule applied to ourselves. This is doc 03, not doc 02: no
behavioral invariant changes.

### 4.3 The conformance suite and the pending-red problem

The owner's decision stands: **I01 and I03 are written against symbols that do not exist yet**
(e.g. `core.StatusFocus`, the units repository) so they fail to *compile* — real red. A literal
tree scan over an empty tree passes vacuously, which is green for the wrong reason. I21 gets
the same treatment, anchored to the future vector-search symbol. `docs/06-harness.md` §8 point
5 is **not** edited.

This creates a mechanical problem the proposal has to solve: `main` is protected and CI blocks
the merge, so a permanently non-compiling test package cannot simply be committed.

**Resolution — the pending-red gate.** The three tests live in `test/conformance/` behind a
build tag (`pendingimpl`) that the normal suite does not use, and a CI job asserts that they
**fail to compile, for the expected reason**:

1. Compile the tagged package. If it **succeeds**, the job **fails**, with the message: *the
   symbols now exist — move these tests into the untagged L2 suite.*
2. If it fails, assert the compiler error names the expected symbols. A typo also fails to
   compile; a gate that accepts any failure is a gate that proves nothing.

This turns §8 point 5 from a screenshot in a PR description into a machine-executed rule, and it
is **self-dismantling**: the day M0 creates `core.StatusFocus`, this job goes red and forces the
M0 PR to promote the tests into the untagged L2 suite, where they degrade into the literal
scans §4 specifies. Scaffolding that removes itself beats scaffolding somebody has to remember.

**I13 is different** and stays untagged: it inspects the embedded migration SQL, which this
change creates, so it goes red then green *inside* this chain. It sits at L2 rather than L3
because §4 words it as *"the migration **declares** no FK"* — reading the embedded SQL is pure,
and the applied-schema side is already covered by the golden.

**Gotcha to carry into design**: a package whose files are all excluded by a build tag makes
`go build ./...` fail with *build constraints exclude all Go files*. `test/conformance/` keeps
an untagged `doc.go`, and the tagged files land after I13 so the package is never tag-only.

### 4.4 The I21 anchor in doc 02

I21 is the only one of the 21 invariants whose `Doc 02` column cites ADRs instead of a doc-02
section, and CLAUDE.md non-negotiable #1 says doc 02 governs behavior. Proposed wording, as a
sub-bullet under §5 point 2 (*hybrid recall*), matching the surrounding voice:

```markdown
   - **One model per search.** Vector similarity is only defined between embeddings produced by
     the same model. A vault can hold two models at once while a reindex is in progress, so
     every vector search filters by model, and vectors from two models are never compared or
     fused. See [ADR-0003](adr/0003-embeddings.md),
     [ADR-0012](adr/0012-vector-proximity-search.md).
```

`docs/06-harness.md` line 186 then reads `§5` in its `Doc 02` column, like every other row; the
ADR references live inside the doc-02 text where they belong. §5 is the right home because doc
02 itself states that the same recall mechanism serves both answering a `recall` and finding
connection candidates — one statement covers all three consumers named in ADR-0012.

### 4.5 Golden-set formats

*Empty sets, defined formats.* A `README.md` describing a JSON shape is not a definition —
nothing executes it. Each directory gets:

```
testdata/recall/    format.md · format_example.json · cases/   (empty)
testdata/classify/  format.md · format_example.json · cases/   (empty)
testdata/llm/       format.md · format_example.json · cases/   (empty)
```

plus a loader with Go types and a test that parses every `format_example.json` with unknown
fields rejected. The example is the executable definition; `cases/` is where real corpora land
in M1. The example lives outside `cases/` so it never contaminates a set.

`testdata/classify/format.md` must state up front that the corpus has to include the
deliberately broken cases §5 demands — truncated JSON, wrong field type, unknown enum — because
those are what will prove I14.

### 4.6 L4 skeleton

`test/e2e/` behind the `e2e` tag: build the binary, run `nooma version`, assert the output
shape. Small, and not vacuous — it proves the tag, the build, and the binary contract. It runs
on merge to `main`, not on every PR (§6).

### 4.7 CI completion

| Gate | Lands with | Note |
|---|---|---|
| L3 integration (`-race -tags integration`) | PR 2 | |
| Schema golden | PR 3 | |
| Schema ↔ doc 03 | PR 4 | |
| Pending-red conformance | PR 5 | |
| L4 e2e (push to `main` only) | PR 6 | |
| `internal/core` coverage ≥ 90 % | PR 7 | See the honesty note below |
| docs↔code sync label check | PR 7 | No PR in this chain touches `internal/core`, so it cannot self-block |
| Cross-compilation matrix | PR 7 | Unblocked by ADR-0001; `ncruces` is pure Go, so this is the payoff of that decision |

**Honesty note on the coverage floor.** `internal/core` has zero statements today, so a ≥ 90 %
floor is vacuously satisfiable or divides by zero, depending on how it is written. The job is
written to compute total statements and enforce the floor only when statements exist, with the
zero case reported explicitly in the log rather than silently passing. It is wired now so that
M0's first core function arrives into an already-armed gate — but it is the one gate in this
change that proves nothing on the day it lands, and it says so in a comment.

---

## 5. The PR chain

Estimated 800–1400 changed lines against a 400-line soft ceiling (§7), so chained PRs are
required. The exploration proposed five slices; this proposal revises it to **seven**, for
three reasons: the connection opener separates cleanly from the migration runner, the initial
schema splits across two migrations, and the CI jobs distribute into the slices that make them
runnable instead of piling into a terminal PR that depends on everything.

| # | PR | Human-review lines (est.) | Depends on |
|---|---|---|---|
| 1 | `docs`: I21 anchor in doc 02, §4 table row, ADR-0001 status fix | ~15 | — |
| 2 | Driver dependency + connection opener + FTS5 registration + L3 job | ~250 | — |
| 3 | Migration runner + `0001_core_tables` + schema golden + golden job | ~300 | 2 |
| 4 | `0002_learning_and_search` + golden update + doc-03 gate + I13 | ~330 | 3 |
| 5 | Conformance suite + I01/I03/I21 pending-red + gate | ~200 | 1, 4 |
| 6 | Golden-set formats + L4 e2e skeleton + e2e job | ~230 | — |
| 7 | Coverage floor + docs↔code label check + cross-compile matrix | ~150 | — |

Dependency order:

```
1 ─────────────┐
2 ── 3 ── 4 ── 5
6 (independent)
7 (independent)
```

`2 → 3 → 4` is a strict chain. PRs 1, 6 and 7 are independent and can land in parallel; PR 5
needs the I21 anchor from 1 and the untagged package from 4.

**Excluded from the human-review budget**, and to be stated in the PR body of the slice that
carries them:

- `go.sum` — a large generated diff from adding `ncruces/go-sqlite3` (PR 2). Nobody reviews
  hashes; the reviewable artifact is the `go.mod` line and the ADR that justifies it.
- The committed schema golden dump (PRs 3–4) is generated, but its **diff is deliberately
  reviewable** and is the point of the gate — it is counted, not excluded.

---

## 6. Strict TDD ordering

`strict_tdd: true`. CLAUDE.md non-negotiable #4: *a conformance test is written before its
implementation, and when it fails it is not weakened.* Per slice, the first test, its red, and
what turns it green:

| PR | First test | What red looks like | What makes it green |
|---|---|---|---|
| 2 | `TestFTS5RegisteredOnEveryConnection` (L3) | Compile error: `undefined: sqlite.Open` | The opener calling `fts5.Register` on every connection |
| 2 | `TestOpenAppliesPragmas` (L3) | Assertion: `journal_mode = delete`, `foreign_keys = 0` | The PRAGMA setup on open |
| 3 | `TestMigrateFromScratchSetsUserVersion` (L3) | Compile error: `undefined: migrations.Apply` | The `go:embed` runner + `0001_core_tables.sql` |
| 3 | `TestMigrateIsIdempotent` (L3) | Second apply re-runs `0001` and fails on `table units already exists` | `user_version` guarding each step |
| 3 | `TestSchemaGoldenMatches` (L3) | Golden file does not exist | `make schema-golden` output, committed |
| 4 | `TestI13_LearningSignalHasNoFKToTarget` (L2) | Assertion: `learning_signals` not found in the embedded migrations | `0002_learning_and_search.sql`, declaring `target_id` with no `REFERENCES` |
| 4 | `TestSchemaMatchesDoc03` (L2) | Diff: doc 03 declares 8 objects the golden does not | Completing `0002`, regenerating the golden, adding the FTS trigger DDL to doc 03 |
| 5 | `TestI01_FocusIsNeverPersisted`, `TestI03_UnitsAreNeverDeleted`, `TestI21_VectorSearchFiltersOnModel` (L2, tagged) | Compile error naming `StatusFocus` and the missing repository/search symbols | **Nothing in this change.** The CI gate asserts the red and its reason; M0/M1 make them green by creating the symbols, which forces promoting the tests to the untagged suite |
| 6 | `TestGoldenSetFormatExamples` (L1) | Compile error: no loader; then parse failure on the example | Types + loader + `format_example.json` per set |
| 6 | `TestBinaryReportsVersion` (L4) | `go test -tags e2e ./test/e2e/...`: no such package | The e2e skeleton building and running the binary |
| 7 | Coverage / label / matrix gates | Verified by deliberate probe PRs (a core file with no doc-02 change must be blocked), evidenced in the PR body | The three CI jobs |

Two red-discipline rules for the whole chain:

1. A red is **observed and recorded in the PR body** — which command, which failure, which line.
   Doc 06 line 331: *the harness is not proven until you have watched it fail for the right
   reason.*
2. When a conformance test fails there are exactly two exits: fix the code, or change doc 02 and
   its ADR in the same PR. Weakening, skipping, or deleting is not one of them.

---

## 7. Verification

Only the commands the repo already defines. `golangci-lint` is not on `PATH`; always go through
`make`.

| Command | Covers |
|---|---|
| `make check` | Exactly what CI runs: `lint test build` |
| `make test` | L1 + L2, `go test -race -shuffle=on ./...` |
| `make test-integration` | L3, tag `integration` |
| `make test-e2e` | L4, tag `e2e` |
| `make cover` | `internal/core` only, ≥ 90 % floor |

New `make` targets this change needs (name to be settled in design): one to regenerate the
schema golden, one to run the pending-red assertion locally so a contributor sees the same
result CI does.

---

## 8. Risks and open questions

| # | Risk | Mitigation |
|---|---|---|
| R1 | The pending-red gate is a mechanism this repo has never used. It is the one place this proposal resolved an under-specified point on its own. | Q1 below. Fallback is documented and cheap: record the red in the PR body and drop the job. |
| R2 | A "fails to compile" gate can pass for the wrong reason (a typo). | The gate asserts the compiler error names the expected symbols. |
| R3 | The coverage-floor job proves nothing on the day it lands. | Stated in §4.7 and in a comment in the workflow. Alternative in Q3. |
| R4 | The doc-03 comparison gate is the most fragile piece: markdown SQL vs a SQLite dump. | Structural comparison (object names + column sets), never byte-exact. Normalization rules are a `sdd-design` deliverable. |
| R5 | Doc 03 does not specify the FTS sync trigger DDL, so the migration defines schema the doc does not declare. | Doc 03 is updated in the same PR (docs↔code rule). No doc-02 invariant moves. |
| R6 | Seven chained PRs carry coordination cost; `2 → 3 → 4` is strictly serial. | 1, 6 and 7 are independent and can absorb parallel effort. |
| R7 | Cross-compilation is build-only; nothing here *runs* the binary on Windows or ARM. | Explicitly a build matrix. Runtime verification belongs to M0's demo. |
| R8 | Two published initial migrations instead of one, permanently. | Deliberate (§4.1); forward-only is respected and the `0 → 1 → 2` path gets exercised from day one. |

### Proposal question round

This phase could not ask interactively. These are the five product/scope questions whose answers
would change the shape of the work — answer, correct the framing, or skip and the assumptions
below stand.

1. **Pending-red gate (R1).** Accept a CI job that asserts I01/I03/I21 *do not compile* for the
   expected reason and that goes red the day M0 creates the symbols — or keep it simple: record
   the red in the PR body and leave §8 point 5 verified by human discipline?
   *Assumption: accept the gate.*
2. **Migration split.** Two published initial migrations (`0001` core tables, `0002` learning +
   search), which keeps each PR reviewable and exercises `user_version 0 → 1 → 2` — or one
   single `0001` carrying the whole schema, accepting a `size:exception` on that PR?
   *Assumption: two migrations.*
3. **Coverage floor timing.** Wire the ≥ 90 % `internal/core` job now, knowing it proves nothing
   until M0 writes the first core statement — or defer it to the M0 PR that introduces core code
   so it is armed and meaningful on the same day?
   *Assumption: wire it now, with the vacuity stated in the workflow.*
4. **Golden-set format weight.** Define the formats with Go types + a loader + validated example
   fixtures (executable definition, ~120 lines that M1 will consume) — or keep it to `format.md`
   plus an example JSON with no Go code until M1 needs it?
   *Assumption: types + loader + examples.*
5. **I21 anchor placement.** Put the invariant under doc 02 §5 point 2 (*hybrid recall*, which
   already covers all three consumers) — or give it its own line in §1 next to the unit's
   embedding, where a reader looking for schema-adjacent rules would find it?
   *Assumption: §5 point 2, wording drafted in §4.4.*

A second question round is available if any answer changes the scope boundary in §3.

---

## 9. Next step

`sdd-spec` and `sdd-design` can run in parallel from this proposal.

- **Spec** owns: the testable requirements per slice, the exact DoD mapping, the conformance
  test contracts.
- **Design** owns: the golden dump format and its normalization rules, the doc-03 comparison
  algorithm, the pending-red gate script, the migration runner's transaction and
  `user_version` semantics, and package placement for the golden-set loader.
