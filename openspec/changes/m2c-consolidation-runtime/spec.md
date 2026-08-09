# Spec — M2c: consolidation runtime

Delta specification for `m2c-consolidation-runtime`, the third of four chained changes splitting
`openspec/changes/m2-sleep-weight/proposal.md` (owner ruling round 2 #6). States what MUST be
true of the repository after this change is applied, in testable form. It does not prescribe how
(that is `design.md`'s job — this document states the testable property; where the proposal
itself named a signature tension (R2), the property is stated and the signature is left to
design, exactly as instructed).

**Status: written against `m2b`'s shipped surface, not a concurrent draft.** `m2b` closed
2026-08-07 with every exported identifier in `internal/core/consolidation` final. This document
copies types and function names from `m2b`'s shipped code and from `m2b/design.md` §8's own
handoff table — "`m2b` fixes shapes; `m2c` declares the ports and speaks SQL" — verbatim where
that table already names a shape, and states a testable property instead of a name where the
proposal (R2, R2's own `sdd-design` obligation) explicitly reserves the signature for `m2c`'s own
design phase.

Sources: `openspec/changes/m2-sleep-weight/proposal.md` §3.2 items 4–8, §5 (m2c row), §5.1 (m2c's
six PRs), §7 R1/R2/R3/R8/R9/R12, §8 Q1/Q2/Q3; `openspec/changes/m2b-consolidation-core/design.md`
§8 (the handoff table) and §9 Q3/Q6/Q7; `openspec/changes/m2a-weight-focus/tasks.md` C6, C17, C19,
C29 (§7, below, discharges or explicitly defers each); `docs/02-cognitive-core.md` §2, §6, §11,
§13; `docs/06-harness.md` §4 (I01–I24) and §7; owner ruling round 2026-08-06 Q1 (`ConfigRepo`
nil-sentinel shape, no migration 0003), Q2 (belief embeddings in memory, no new table), Q3
(`expire_incomplete`'s producer stays out of scope).

## Scope boundary (binding, from the proposal's §3.2 items 4–8 and §5's m2c row)

> `m2c` is ports, store, `brain/consolidate.go`, and `nooma consolidate`. Depends on `m2b`. Owns
> I12 across eight phases, I03 at the `archive` write, I24's structural test, and R1's
> config-row decision.

Six PRs, per proposal §5.1's m2c row: `feat/ports-unit-weight-count` (~300), `feat/ports-selfmodel-config`
(~300), `feat/store-consolidation-repos` (~400), `feat/brain-consolidate-runner` (~400),
`feat/brain-consolidate-phase-io` (~400), `feat/cli-consolidate` (~250). **These are guesses**
(proposal §5.1's own caveat, measured wrong 1.3×–4.3× across M0/M1) — `sdd-design`'s own
re-derivation, once it runs, is authoritative over this row exactly as it was for `m2a`.

`m2c` introduces **no new package directory** under `internal/core` — it adds no `internal/core`
code at all. Its new files land in `internal/ports`, `internal/store/sqlite`, `internal/brain`,
and `cmd/nooma`. `docs/06-harness.md` §1's tree needs no preflight PR for those four — all four
already exist.

`m2c` writes **no new doc 02 prose**. Every behaviour it wires — the six formulas, the
belief-embedding cost, the two dedup defenses, the eight-phase order, I12's requirement — is
already stated in doc 02 by `m2a`/`m2b`'s amendments, verified by re-reading §2, §6, §11 and §13
in full before writing this document. `m2c`'s job is connecting already-specified decisions to
real I/O, not deciding new behaviour. The one exception, stated where it arises (§5.10 below), is
a genuine schema-fit question `pattern_eval`'s `current_state` write surfaces that neither `m2a`
nor `m2b` could have found, since neither touched a store.

### R0 — General requirements across all six PRs

**R0.1 — the dependency rule holds at the new boundary.** MUST: every new `internal/ports`
interface imports only the standard library and `internal/core/*` types (never `internal/store`).
MUST: every new `internal/store/sqlite` file imports only the standard library, `internal/core/*`,
`internal/ports`, and the sqlite driver (never `internal/brain`). MUST: every new
`internal/brain` file imports only the standard library, `internal/core/*`, and `internal/ports`
(never `internal/store` directly — only through a port). **Verified by**: L2, the existing
`depguard`/`core-purity` gate plus `make check`'s existing lint pass; no new test is needed for
this half of the rule since `.golangci.yml`'s allow-lists already cover these three directories,
and this MUST states what already holds rather than introducing a new gate.

**R0.2 — `brain.ConsolidateService` reads the clock exactly once per pass, split the same way
`CaptureService` is.** MUST: the pass's entry type holds a `ports.Clock` and delegates to a
clockless worker holding none — `internal/brain/capture.go`'s own shell/worker split
(`CaptureService` / `captureRunner`), applied here rather than re-argued. MUST: no file under
`internal/brain` this change adds calls `time.Now` directly. **Verified by**: L2 —
`test/conformance/brain_single_clock_read_test.go` and `brain_no_direct_clock_read_test.go`
already scope to every non-test file under `internal/brain/**`; both extend to the new files with
no test-file change required, and this MUST states that the new code must not make either one
fail rather than proposing a third guard.

**R0.3 — calibration gate scope, stated plainly rather than implied.** MUST: `m2c` introduces
**zero new named constants under `internal/core/`** — every calibratable number `m2c`'s phases
read (`weight_threshold`, `goal_stagnation_days`, `mental_load_threshold`, `hysteresis_margin`)
already has its Go home and its §13 row, shipped by `m2a`/`m2b`. **MUST NOT be read as**: that
`m2c` introduces no numeric constant anywhere — it may (a CLI timeout mirroring `capture.go`'s
`captureTimeout`, a store-layer batch size). Any such constant, because it lives outside
`internal/core/`, is **not** checked by `test/conformance/calibration_doc_test.go` — that gate
reads §13 rows naming an `internal/core/<pkg>.<Symbol>` only (`docs/06-harness.md` §7's own
scoping sentence: "The gate covers the table-to-code direction only"). This document states that
gap rather than implying a coverage it does not have. **Verified by**: manual review at PR time
— there is no automated check for a constant `m2c` might introduce outside `internal/core`, by
design of the existing gate, not an omission this change is asked to close.

---

## 1. `ports.UnitRepo` — the weight write and the live count — PR1 `feat/ports-unit-weight-count`

Traced to proposal §3.2 item 4, R2, and `m2a` spec R2.1 (`internal/core/weight`'s own statement
of the shape this port must have).

### R1.1 — the weight-write method's parameter makes "weight without `last_touched_at`" inexpressible (I24)

**MUST**: `ports.UnitRepo` gains exactly one new method for persisting a weight change, and its
parameter type is `weight.Boost` (or, if design finds a reason to decompose it, a struct carrying
exactly `Boost`'s three fields together — `UnitID`, `Weight`, `LastTouchedAt`) — never two
parameters a caller could supply independently, and never a method that takes a bare `float64`
weight with `last_touched_at` optional or defaulted. This is not `m2c` inventing a shape: `m2a`
spec R2.1 already states it — *"`m2c`'s `UnitRepo` weight-write method takes a `weight.Boost` (or
its three fields), so a `SetWeight` that leaves `last_touched_at` alone is not expressible at the
port. `m2a` does not add the port — it fixes the shape the port must have."* `m2c` fulfils that
promise; it does not re-decide it.

**MUST NOT**: `ports.UnitRepo` gains a second method capable of writing `weight` or
`last_touched_at` alone (mirroring `UpdateContent`/`UpdateEventAt`/`UpdateDueAt`'s own "one method
per field" rule from M1 — except for exactly the field pair I24 requires move together, which
this discipline would otherwise split incorrectly).

**MUST**: whether the method takes one `weight.Boost` per call or a slice — the batch-vs-single
tension proposal R2 named — is `sdd-design`'s decision, not this document's, per R2's own stated
scope ("Named as an `sdd-design` obligation for `m2c`"). What is testable regardless of that
choice: for **every** unit named in a `Reweight` result's `boosts` slice (`m2b` spec R3.3, which
returns `[]weight.Boost` for a whole pass), the write that lands for that unit pairs its `Weight`
and `LastTouchedAt` from the **same** `Boost` value — no implementation may write one unit's
`Weight` paired with a different unit's, or a different pass's, `LastTouchedAt`.

**Note on the risk this closes, stated so it is not silently dropped**: `m2b` R3.3's own
`Reweight` scenario (§4) requires a unit id to be able to appear in **both** `boosts` and
`corrupted` from one call, with neither suppressed — R1.4's write path must therefore write the
`boosts` entry for that unit even though the same unit id also appears in `corrupted` from an
unrelated origin's edge.

**Verified by**: L2, structural — a conformance test (naturally alongside the existing I24 row in
`docs/06-harness.md` §4, which `m2a` already added; no new table row needed) reflects over
`ports.UnitRepo`'s new method and asserts its parameter type is `weight.Boost`, `[]weight.Boost`,
or a struct whose field set is exactly `{UnitID, Weight, LastTouchedAt}` (or a slice thereof) —
failing if a future edit adds a second method reachable with only one of the pair supplied.

### R1.2 — the live-count-by-type method returns one integer, never a list (owner ruling 6)

**MUST**: `ports.UnitRepo` (or a repository this PR names for the purpose) gains exactly one new
method returning `int` — a count of live (`status = pool`) units of a given `unit.Type` — never a
slice of units the caller would count itself. This is `pattern_eval`'s `EvaluateLoad` input
(`m2b` spec R5.2, `openMentalLoad int`), and the point of the ruling is structural: a method
returning `[]unit.Unit` here would put an unbounded read where the phase needs exactly one
integer.

**MUST**: the method's name does not read as a general parameterized list — following
`ports.UnitRepo`'s own existing "no `List(status)`" discipline (its package doc comment), the
name says what it counts, not that it filters by an argument.

**Verified by**: L1 (`repocontract`, described in §3) plus L2 — the same reflection sweep as R1.1
folds in a check that no exported `ports.UnitRepo` method both accepts a `unit.Type` and returns
`[]unit.Unit`.

### R1.3 — I03 holds for the widened interface (no removal verb)

**MUST**: neither of this PR's two new methods, nor any method any later `m2c` PR adds to any
port, has a name beginning `Delete`, `Remove`, `Purge`, `Drop`, or `Destroy`.

**Verified by**: L2 — `test/conformance/i03_units_never_deleted_test.go`'s existing reflection
loop over `ports.UnitRepo` already covers this file's two new methods with no test-file edit
required (the loop iterates `ports.UnitRepo`'s full method set, not a hand-maintained list). This
PR's two new methods are exercised by that test the moment they exist.

### R1.4 — `test/support/memrepo` and `test/support/repocontract` grow with the port

**MUST**: `test/support/memrepo`'s `UnitRepo` fake implements both new methods with the same
semantics the port's doc comment states (weight-write pairs the two fields atomically in the fake
too; the count method counts, never lists). MUST: `test/support/repocontract` gains a shared test
case per new method, exercised against both the fake and — once PR3 ships — the sqlite adapter,
following the existing pattern `repocontract/relationrepo.go` and its siblings already establish.

**Verified by**: L1 for the fake's own unit behaviour; L1+L3 for the shared contract, run against
both implementations.

---

## 2. `ports.SelfModelRepo` and `ports.ConfigRepo` — PR2 `feat/ports-selfmodel-config`

Traced to proposal §3.2 item 4 (owner rulings 5 and Q1), `m2b` design.md §8's handoff table, and
`m2b` design.md §9 Q3/Q6.

### R2.1 — `SelfModelRepo` persists a NEW derived belief by its natural key, upsert-style (owner ruling 5)

**MUST**: `SelfModelRepo` exposes a write method whose conflict target is `self_beliefs.topic_key`
(`UNIQUE`, migration `0001:75`) — `ports.RelationRepo.Upsert`'s own pattern (conflict on
`(from, to, type)`), applied to the one column doc 02 §10 defines as a belief's natural key. A
second write for the same `topic_key` updates in place; it does not create a duplicate row.

**MUST NOT**: this method is used for the *merge* case (R2.2) — `MergeDecision.MergeInto` names
an existing belief's **id**, chosen by embedding similarity (`m2b` spec R4.4), which need not
equal the newly-derived belief's own computed `topic_key`. Routing a merge through the
topic_key-keyed upsert would silently create a second belief instead of reinforcing the one the
merge decision found — this is not a hypothetical, it is the exact shape of bug the two-method
split in R2.1/R2.2 exists to make unreachable.

**Verified by**: L1 (`repocontract`) — writing the same `topic_key` twice with different content
produces one row, not two.

### R2.2 — `SelfModelRepo` reinforces an EXISTING belief by id, touching only confidence and the reinforcement timestamp

**MUST**: `SelfModelRepo` exposes a second write method taking a belief **id** (not a
`topic_key`) and a new confidence value, updating `confidence` and `last_reinforced_at` and
leaving `topic_key`, `content`, `facet`, `origin`, and `source_unit_id` unchanged. This is the
write `consolidation.Reinforce`'s result (`m2b` spec R4.5) feeds, for the
`MergeDecision.MergeInto != ""` case.

**MUST**: it returns a not-found error rather than silently creating a row when the id does not
exist — a reinforcement is a decision about a specific, already-identified belief; a repository
that upserts here would hide the case where `MergeInto` names a belief that has since vanished (an
impossibility today, since nothing deletes, but a fact worth stating as a refusal rather than a
silent create).

**Verified by**: L1 (`repocontract`) — reinforcing an existing id changes only the two named
columns; reinforcing an absent id returns an error, not a new row.

### R2.3 — `SelfModelRepo.ActiveBeliefs` is the one read both `derive` and `pattern_eval` share, and carries no status parameter

**MUST**: `SelfModelRepo` exposes exactly one read method, `ActiveBeliefs`, returning every belief
whose `status = 'active'` — following `LiveByIDs`'s own precedent (`m2b` design §8's own row: "a
read whose **name** carries 'active', never a status parameter"). This one method serves both
consumers: `derive`'s two dedup defenses (§5.6/§5.7 below) need every active belief regardless of
facet, and `pattern_eval`'s `EvaluateStagnation` (`m2b` spec R5.1) filters to `FacetGoal`
**itself**, in `core`, exactly as R5.1's own signature already does (`bs []Belief` — the filtering
is the pure function's job, not the repository's).

**Verified by**: L1 (`repocontract`) — a fixture with beliefs of every facet and every status
returns exactly the active ones, all facets included.

### R2.4 — `ConfigRepo` returns nil-sentinel pointers when the singleton row is absent, and writes nothing to seed it (owner ruling Q1, option C)

**MUST**: `ConfigRepo` exposes a read method returning a struct mirroring `config`'s six columns
as nil-sentinel typed pointers: `WeightThreshold *float64`, `HysteresisMargin *float64`,
`ConsolidationEnabled *bool`, `GoalStagnationDays *int`, `MentalLoadThreshold *int`,
`ConsolidationLastRunAt *time.Time`. When the singleton row (`id = 1`) does not exist, every field
is `nil`. When it exists, every field carries the column's stored value **as stored** — `ConfigRepo`
does not itself decide what a `nil` or an out-of-range value defaults to; that is
`consolidation.Resolve*` and `focus.ResolveMargin`'s job (both already shipped, `m2a`/`m2b`), fed
the pointer this method returns.

**MUST NOT**: `ConfigRepo` inserts, upserts, or seeds row `id = 1` on open, on first read, or
anywhere except the one write R2.6 names below. This is ruling Q1's whole point: **no migration
0003** for the duration of `m2c`. A vault whose `config` table has never been written returns an
all-nil struct, and every `Resolve*` function downstream already has a defined answer for that —
the exact pattern `m1`'s Q1 established for `relation_thresholds`.

**Verified by**: L1 (`repocontract` over the fake) — an empty backing store returns an all-nil
struct; L3 (`internal/store/sqlite`) — a freshly-migrated vault with no `config` row returns the
same, proving the migration itself seeds nothing (confirmed against the current migration text,
which contains no `INSERT INTO config`).

### R2.5 — `goal_stagnation_days` has exactly one schema home for the whole of M2: `config.goal_stagnation_days` (discharges `m2b` design.md §9 Q3)

**MUST**: `ConfigRepo`'s read method reads `config.goal_stagnation_days` (the typed, `NOT NULL
DEFAULT 21` column) into `GoalStagnationDays *int`. It never reads or writes the `calibration`
table's generic `key/value` row of the same name.

**MUST**: the `calibration` table stays **fully unused** through the whole of `m2c` — no `INSERT`,
no `SELECT`, no Go type referencing it. `calibration` exists for M5's learning module (`goal
stagnation... recalibrates it per user`, doc 02 §9) to write **arbitrary future** per-user knobs
that have no dedicated `config` column; `goal_stagnation_days` already has one, so it does not need
the generic table, and giving it two writers (`config` from `m2c`'s reads, `calibration` from a
hypothetical M5 write) would be exactly the drift ruling Q1's option C already declined to accept
for a different pair of sources.

**MUST**: doc 02 §13's `goal_stagnation_days` row is amended in this PR to drop "`m2c` must
decide which table `ConfigRepo` reads" and instead state the decision this requirement makes,
so a later reader — including M5's implementer — inherits a decision rather than an open question.

**Verified by**: L2 (`test/conformance`) — reading the migration text confirms `config` carries
the typed column with its `DEFAULT 21`; a source-tree scan (the same shape `i03`'s DELETE-scan
uses) confirms no `.go` file under `internal/` references the string `"calibration"` as a table
name.

### R2.6 — `ConfigRepo` gains exactly one write: recording the pass's own instant as `consolidation_last_run_at`, lazily creating the row if absent

**MUST**: `ConfigRepo` exposes one write method taking a `time.Time` and persisting it as
`config.consolidation_last_run_at`, `UPSERT`-style: if row `id = 1` does not exist, it is created
with this column set to the given instant and every **other** column left to the SQL `DEFAULT` the
migration already declares (`weight_threshold` 0.5, `hysteresis_margin` 0.05,
`consolidation_enabled` 1, `goal_stagnation_days` 21, `mental_load_threshold` 7); if it exists,
only this column changes.

**MUST NOT**: this write is the **only** exception to R2.4's "no seeding" rule, and it is not a
contradiction of ruling Q1 — the row's lazy creation happens as a side effect of the first
completed consolidation pass, using the SQL `DEFAULT`s for every other column (never a Go literal
duplicating them), so it needs no migration 0003 either. A vault that never runs a pass still has
no `config` row at all.

**Why this is `m2c`'s to add and not left implicit**: without a writer, `consolidation_last_run_at`
stays `NULL` forever, no matter how many times `nooma consolidate` is run by hand — and `m2d`'s
boot catch-up (ADR-0009, out of `m2c`'s scope) reads exactly this column to decide staleness.
Leaving the write out of `m2c` would make every `m2d` boot see a vault that has "never
consolidated" even after `m2c`'s own demo run, silently defeating the catch-up's purpose the first
time it is exercised. This requirement is `m2c`'s because the pass that produces the value it
should hold is `m2c`'s own runner (§4 below); it is not `m2d`'s to retrofit.

**Verified by**: L1 (`repocontract`) — writing to an absent row creates it with the SQL defaults
on every other column, verified against the same migration text R2.5 reads; writing to an existing
row changes only the one column.

### R2.7 — the widened port surface stays I03-clean, structurally, not by convention alone

**MUST**: `test/conformance/i03_units_never_deleted_test.go`'s reflection loop is extended, in
this PR, to also sweep `ports.SelfModelRepo` and `ports.ConfigRepo` (a loop over a slice of
`reflect.Type` values rather than the current single `ports.UnitRepo` literal) — `RelationRepo`'s
own doc comment already states this convention in prose ("keeps
`i03_units_never_deleted_test.go`'s strengthened prefix set satisfied for every ports interface,
not only `ports.UnitRepo`"), and this PR is where the code catches up to that stated intent for
every port that exists by the time it lands, closing the gap between what the comment claims and
what the test actually checks.

**Verified by**: L2 — the widened test fails if any of the three interfaces gains a
denied-prefix method.

---

## 3. `internal/store/sqlite` — the repository implementations — PR3 `feat/store-consolidation-repos`

Traced to proposal §3.2 item 5, R12.

### R3.1 — every new port method has a sqlite implementation passing the shared `repocontract` suite

**MUST**: `internal/store/sqlite` implements `ports.UnitRepo`'s two new methods, `ports.RelationRepo`'s
two new reads (R3.5/R3.6 below), `ports.SelfModelRepo`, and `ports.ConfigRepo` — each one passing
the exact `repocontract` cases §1/§2 require, run against the real sqlite adapter in addition to
the fake.

**Verified by**: L3 (`internal/store/sqlite/*_integration_test.go`, `integration` build tag),
following the existing file-per-repository convention (`unitrepo_integration_test.go`,
`relationrepo_integration_test.go`, …).

### R3.2 — `testdata/schema/store_api.golden` is regenerated in this PR, via its own target

**MUST**: every new or widened port interface is reflected in `testdata/schema/store_api.golden`,
regenerated with `make store-api-golden` — a different target from `make schema-golden` (proposal
R12), named explicitly in this PR's own task list so it is not discovered as a fast-loop failure
after the fact, the way M1's PRs 4 and 9 already recorded it.

**Verified by**: `make check`'s existing golden-diff check, which fails if the committed golden
and the regenerated one disagree.

### R3.3 — I24's proof at the SQL layer: `weight` and `last_touched_at` land from one statement, never two round trips

**MUST**: the sqlite implementation of R1.1's weight-write method executes a single `UPDATE`
statement that sets both `weight` and `last_touched_at` (and `updated_at`, following every other
`UnitRepo` write method's own convention) in one round trip to the database — never two separate
statements a partial failure could leave half-applied.

**Verified by**: L3 — an integration test calls the method once, reads the row back, and asserts
**both** columns equal the given values; a second case verifies that a `weight.Boost` for a
non-existent unit id returns `ports.ErrUnitNotFound` (the existing sentinel, reused rather than a
new one declared) with **neither** column touched on any row.

### R3.4 — I05's structural half, scoped to read paths, proven at the source-text level (proposal R13)

**MUST**: no method whose name identifies it as a read (`ByID`, `LiveByIDs`, the new archive/connect
candidate reads named in §5) contains, in its own SQL text, an assignment to the `weight` or
`last_touched_at` columns of `units`. Only R1.1's weight-write method's own SQL may assign either.

**MUST NOT**: this MUST is read as forbidding `reweight`'s materialization — `m2b` already declined
that option for M2 (doc 02 §6.6's amendment, `m2b` spec R3.3's own "`MUST NOT`" against a
rewrite-to-effective-value function), so there is nothing for this scope note to accidentally
catch; it is stated because proposal R13 named the risk explicitly and the test's own doc comment
should say so rather than leave the scoping to be inferred.

**Verified by**: L2 — a source-text scan over `internal/store/sqlite`'s non-test files, the same
shape `i03`'s DELETE-scan already uses, asserting the two-column assignment appears in exactly the
one file/method R1.1 implements.

### R3.5 — `RelationRepo` gains the evidence-join read `strengthen` needs

**MUST**: `RelationRepo` gains a read returning `[]consolidation.RelationEvidence` — each relation
joined to **both** endpoints' `last_touched_at` in the same query (`m2b` design §8's own row: "a
join no port has today... the alternative is two round trips and a correctness hazard if a unit
moves between them").

**Verified by**: L3 — a fixture with a relation whose one endpoint was touched after the query
begins (simulated via two sequential calls) is out of this requirement's scope to prove
transactional isolation beyond what SQLite's own default already gives; the test proves the join
returns the correct paired timestamps for a static fixture, which is what `Strengthen`'s own input
contract needs.

### R3.6 — `RelationRepo` gains the `map[Pair]bool` exclusion read `connect` needs

**MUST**: `RelationRepo` gains a read returning `map[consolidation.Pair]bool`, keyed by
`consolidation.CanonicalPair`, over the candidate set `connect` is about to judge — bounded by
`ConnectSourceLimit × ConnectCandidateK` (`m2b` design §8's own row).

**Verified by**: L3 — a fixture with an existing relation stored in one direction returns `true`
for a lookup built from the opposite direction's canonical pair, matching doc 02 §4's "direction
is what the judge said, not a canonical form" read into this exclusion-only lookup.

---

## 4. `brain.ConsolidateService` — the runner — PR4 `feat/brain-consolidate-runner`

Traced to proposal §3.2 item 6, success criteria bullets 5/6/7, I11's behavioural half, I12.

### R4.1 — the runner executes `consolidation.Order()`, never a locally declared phase list (I11, behavioural half)

**MUST**: `brain.ConsolidateService`'s pass loop iterates `consolidation.Order()`'s returned slice
directly — it declares no second list of the eight phases. This is `m2b` spec R1.2's "no
non-test file outside `internal/core/consolidation` contains two or more of the eight phase-name
string literals" MUST, restated as a positive requirement on the file this rule was written in
advance of: `m2b`'s own conformance test (`test/conformance/`, R1.2) already tree-scans every
non-test `.go` file outside `internal/core/consolidation` — `internal/brain/consolidate.go` is
covered by that scan the moment it exists, with no test-file edit needed.

**MUST**: a conformance-level test proves the runner cannot silently reorder the eight phases —
I11's behavioural half, which has no test today (`m2b` spec §6 explicitly leaves it to `m2c`).
**Verified by**: L2, `internal/brain` — a test wiring the runner over fake repos seeded so every
phase has qualifying input, with a spy recording each phase's invocation instant, asserting the
recorded sequence equals `consolidation.Order()` exactly, including that `PhaseLearn`'s slot is
reached (even though it performs no work, owner ruling 3) and reached **last**.

### R4.2 — I12, both directions: an effect always logs, and a phase that decided nothing writes nothing

**MUST**: for every `Transition`, `StrengthChange`, accepted `ProposedRelation`, `MergeDecision`
(create or reinforce), `Boost` (from `reweight`), `StagnationFinding`, and `LoadFinding` a phase's
core decision function returns, the runner persists it **and** records exactly one
`ports.DecisionLog.Record` call whose `Action` distinguishes which phase and which kind of effect
produced it — a reader of `decision_log` after a pass can tell which phase wrote which row without
inspecting `Context`.

**MUST**: a phase whose decision functions return nothing to persist for a given input — every
`ExpireIncomplete` call producing zero transitions, `Archive` finding nothing under threshold,
`EvaluateLoad` returning `false` — writes **zero** `decision_log` rows for that phase's run of
that pass. This is doc 02 §11's "a decision with no effect writes nothing," restated at the
runner level because nothing in `m2b` could test it (no runner existed yet) and the proposal names
it explicitly as load-bearing and untested (§6.1's ordering table, row 6).

**MUST NOT**: a `Reweight` result's `corrupted` entries are logged to `decision_log` — a refusal
because an edge in the shared batch was unusable had **no effect** on the vault for that unit (no
write happened), so I12's own "with an effect" qualifier excludes it, even though the doc 02
prose around `Resurface`'s refusal (§2) calls a refused unit "an event worth a `decision_log` row
once `m2c` can write one" for the **pure-function caller's own future use** — that sentence is
about `Resurface`'s general contract, not a mandate that every corrupted-refusal case in every
future caller must log one; `Reweight`'s specific "no vault effect" case governs here, and R4.2's
`MUST NOT` says so rather than leaving the two readings to collide silently.

**Verified by**: L2, `internal/brain` — two fixtures over the same wired runner: one where every
phase has qualifying input, asserting one `decision_log` row per persisted effect and none for any
`Reweight` `corrupted` entry; one where no phase has qualifying input, asserting the pass completes
and `decision_log` gains zero rows.

### R4.3 — `archive`'s concurrent-revive race is skipped and logged, never a pass failure (proposal R8)

**MUST**: when persisting an `Archive`-planned transition, if `ports.UnitRepo.SetStatus` returns
`ports.ErrStatusConflict` (a concurrent capture or correction revived the unit between the phase's
read and its write), the runner does not fail the pass — it skips that one unit, continues with
the remaining transitions, and records a `decision_log` row naming the skip (this **is** an
effect worth logging: the pass decided to archive and a race prevented it, which is exactly the
kind of thing doc 02 §11's glass box exists for).

**Verified by**: L2 — a fixture with three units planned for archival where the fake `UnitRepo`
returns `ErrStatusConflict` for the second asserts the pass completes, the first and third are
archived, the second is skipped and logged, and no error propagates out of the pass.

### R4.4 — the archive write path stays I03-clean at the call site, not only at the port

**MUST**: the runner's archive persistence calls `SetStatus` only — no code path in
`brain/consolidate.go` constructs or issues anything a removal-prefixed method name would suggest.
Already covered structurally by R1.3/R2.7's widened reflection sweep over the ports the runner
calls through; restated here because the discipline is about the **caller's** behaviour, which
reflection over an interface cannot see, only convention and review can.

---

## 5. Phase I/O wiring — PR5 `feat/brain-consolidate-phase-io`

Traced to proposal §4.4's phase table, `m2b` design §8's handoff table, and doc 02 §6/§7 in full.
"Likely splits further" (proposal §5.1) — this section states testable properties per phase; how
many PRs it becomes is `sdd-tasks`'/`sdd-design`'s call, not this document's.

### R5.1 — `expire_incomplete` reads by a named exception, not a parameterized status list, and proves it is a real no-op on real data (owner ruling Q3)

**MUST**: the read feeding `ExpireIncomplete` is named for the exception it is — `m2b` design §8's
own row: *"the one deliberate non-live read in M2... name it so the exception is explicit
(`IncompleteOlderThan`, not `List(status)`)"* — never a general status-parameterized list method
that `ports.UnitRepo`'s own doc comment already forbids.

**MUST**: on a vault produced entirely by M1's capture path — which owner ruling Q3 confirms
creates **no** `incomplete` unit — this read returns an empty slice, and the phase therefore emits
zero transitions on every real pass. This is stated as a positive, testable claim rather than left
as an absence: `expire_incomplete` occupying its slot with no producer to feed it is a proven fact
about this milestone's actual vaults, not an assumption.

**Verified by**: L3 — an integration test seeds a vault through the real capture path (no
repo-constructed `incomplete` row) and asserts the phase's read returns empty and the phase's
persist step is a no-op; separately, `m2b`'s own L1 suite already proves the pure function's
behaviour against a repo-constructed `incomplete` unit (`m2b` spec R2.1) — this requirement does
not repeat that, it proves the **wiring** sees no such unit on the paths M2 actually exercises.

### R5.2 — `archive` resolves its threshold through `ConfigRepo`, never a bare Go constant read directly

**MUST**: the runner reads `ConfigRepo`'s `WeightThreshold` pointer and passes it through
`consolidation.ResolveWeightThreshold` (`m2b` spec R2.3) before calling `Archive` — never reads
`consolidation.DefaultWeightThreshold` directly as if it were the resolved value, which would
silently ignore a user's own configured threshold the moment one exists.

**MUST**: the `since` parameter R5.3 below computes is read from `ConfigRepo` **before** any
phase runs, once per whole pass — not re-read per phase.

**Verified by**: L2 — a fixture with a configured `WeightThreshold` differing from the default
asserts `Archive` is called with the configured value, not the default.

### R5.3 — `since` is the previous whole pass's own recorded instant, read once before any phase runs, and shared by `strengthen` and `connect`

**MUST**: at the start of a **whole-pass** run, before any phase executes, the runner reads
`ConfigRepo`'s `ConsolidationLastRunAt` and holds it as the `since *time.Time` value both
`Strengthen` (`m2b` spec R3.1) and `SelectConnectSources` (`m2b` spec R4.1) take — the **same**
pointer value passed to both, read exactly once, never independently re-derived per phase (which
would risk the two phases disagreeing about which pass they are measuring "since").

**MUST**: on a vault whose `config` row does not exist (never consolidated), this value is `nil`,
and both `Strengthen` (returns nothing for every input, `m2b` R3.1) and `SelectConnectSources`
(takes the whole live pool, `m2b` R4.1) already have a defined, tested answer for that case — this
PR wires the `nil`, it does not invent new handling for it.

**Verified by**: L2 — a fixture with a configured `ConsolidationLastRunAt` asserts both
`Strengthen` and `SelectConnectSources` are called with an identical `*time.Time` value (same
pointed-to instant); a fixture with an absent `config` row asserts both are called with `nil`.

### R5.4 — `consolidation_last_run_at` is written once, after a completed WHOLE pass only, never after a per-phase run

**MUST**: after every phase of a whole-pass run has completed (whether or not any phase produced
an effect), the runner writes the pass's own single clock-read instant (R0.2) to `ConfigRepo` via
R2.6's write method.

**MUST NOT**: a per-phase invocation (`nooma consolidate --phase=X`, §6 below) writes this column.
Writing it there would corrupt `since`'s meaning for the **next** whole pass's `strengthen`/`connect`
— `since` means "the last full pass," not "the last time any one phase ran," and a per-phase write
would silently narrow the co-use/recency window every subsequent whole pass computes against.

**Verified by**: L2 — a whole-pass fixture asserts `ConfigRepo`'s write is called exactly once,
with the pass's own clock instant; a per-phase fixture asserts it is never called.

### R5.5 — `connect` reuses `RecallService`'s fusion and the relation judge unchanged, differing only in `created_by`

**MUST**: the candidate search behind `connect` calls `brain.RecallService`'s existing fused
ranking (`ADR-0010`'s vector-then-lexical order, `recall.FuseScored`) — it does not implement a
second fusion. MUST: the persist decision reuses `internal/core/relation.Resolve`, called through
the same relation-judge `ports.LLMProvider` shape `capture.go`'s own judge call already
establishes, with `created_by = relation.CreatedByConsolidation` the only difference from
capture's own judge call (`m2b` spec R4.7).

**Verified by**: L2 — a test asserting `connect`'s persisted relations carry `CreatedByConsolidation`
while exercising the identical `relation.Resolve` decision table `capture`'s own relation tests
already prove (I07/I08's regression coverage, per §7 below — no new decision-table test is
required here, only that the wiring routes through the same function).

### R5.6 — `derive`'s dedup defense 1 is wired: existing active beliefs enter the derivation prompt (closes doc 02 §6.5's named gap)

**MUST**: the runner's `derive`-phase LLM call includes every belief `SelfModelRepo.ActiveBeliefs`
returns in the prompt it sends, so the judge can decide "this already exists" before proposing a
new belief — doc 02 §6.5's own words: *"required by this document, not yet wired: no derive-phase
prompt builder and no derive-phase orchestration exist anywhere in `internal/brain` yet, owed by
whichever PR wires `brain`'s consolidation pass for `derive`."* This PR is that PR.

**MUST**: when `ActiveBeliefs` returns empty (a fresh vault, or a vault with no beliefs derived
yet), the prompt states that plainly rather than sending an empty or malformed section — the
absence of existing beliefs is itself informative to the judge, not a degenerate case to hide.

**Verified by**: L2 — a fake `LLMProvider` capturing the prompt text: a fixture with active beliefs
asserts every one's `topic_key`/`content` appears in the prompt; a fixture with none asserts the
prompt still sends (not a skipped call) and names the empty state.

### R5.7 — `derive`'s embedding cost is in-memory, per pass, and persists nothing (owner ruling Q2, option A — already amended into doc 02 §6.5)

**MUST**: at the start of each `derive` phase run, the runner calls `ports.EmbeddingProvider`
once per active belief `ActiveBeliefs` returns, holds the resulting vectors only for the duration
of that phase's `MergeProposals` call (`m2b` spec R4.4), and discards them afterward. MUST NOT:
any new repository method or table persists a belief's embedding — no `belief_embeddings` table,
matching doc 02 §6.5's already-shipped amendment exactly (this document adds no new doc 02 text
for this point, per the Scope boundary's own claim, verified by the fact that §6.5 already states
the cost and the "no schema change" decision as of this document's writing).

**Verified by**: L2 — a fake `EmbeddingProvider` counting calls asserts exactly `len(activeBeliefs)`
calls per `derive` phase run, and a source-tree scan confirms no new port or store method whose
name suggests persisting a belief vector exists.

### R5.8 — `derive`'s two `MergeDecision` outcomes route to the two `SelfModelRepo` writes R2.1/R2.2 declare

**MUST**: for each `MergeDecision` `MergeProposals` returns (`m2b` spec R4.4), `MergeInto == ""`
routes to R2.1's topic-key upsert (a new belief), and `MergeInto != ""` routes to R2.2's
reinforce-by-id write, using the confidence `consolidation.Reinforce` (`m2b` spec R4.5) computes
from the merged-into belief's current confidence — never the topic-key upsert for a merge, per
R2.2's own `MUST NOT`.

**Verified by**: L2 — a fixture producing one create-decision and one merge-decision from the same
`derive` run asserts exactly one `SelfModelRepo` upsert call and exactly one reinforce call, each
receiving the correct target.

### R5.9 — `reweight` persists every returned boost through R1.1's weight-write method, and never logs a `corrupted` refusal (restates R4.2's `MUST NOT` at this phase)

**MUST**: every entry in `Reweight`'s `boosts` slice (`m2b` spec R3.3) is persisted through R1.1's
weight-write method, preserving the per-unit `(Weight, LastTouchedAt)` pairing R1.1 requires, even
for a unit id that also appears in the same call's `corrupted` slice (`m2b` spec R3.3's own
scenario: both outputs are independently true and neither suppresses the other).

**Verified by**: L2 — the `Reweight` scenario `m2b` spec §3 R3.3 states (a unit boosted by one
origin and corrupted by another's edge) is re-run through the wired runner, asserting the boost is
persisted and no `decision_log` row exists for the corrupted half (R4.2's `MUST NOT`).

### R5.10 — `pattern_eval`'s writes: `EvaluateStagnation`'s findings are logged directly; `EvaluateLoad`'s `current_state` write is a genuine schema-fit gap, named rather than silently resolved

**MUST**: every `StagnationFinding` `EvaluateStagnation` (`m2b` spec R5.1) returns is recorded to
`decision_log` — doc 02 §7's stagnation check-in is a hypothesis with no delivery in M2 (proposal
§3.3), so `decision_log` is its only recorded trace this milestone.

**MUST**: when `EvaluateLoad` (`m2b` spec R5.2) returns `(finding, true)`, the runner writes one
append-only row — never an `UPDATE` of a prior row — reflecting the doc 02 §7 hypothesis, and
records the same fact to `decision_log`. **`consolidation_last_run_at`'s own read (R5.3) is
`lastHypothesisAt`'s only available anchor in M2**, per `m2b` design §9 Q6's own recorded, undecided
question — `m2c` maps it here: `lastHypothesisAt` is the `recorded_at` of the most recent prior
`current_state` write this phase itself made (not any user-facing `energy`/`mood` reading), and
this mapping — a hypothesis's own cooldown anchored on itself, since M2 has no
`state_confirmed`/`state_denied` resolution signal yet (that is M5) — is stated in the
`decision_log` row's context, per Q6's own instruction ("`m2c` must map it and say so in the
`decision_log` context").

**Named rather than resolved, honestly**: `current_state`'s columns as they exist today
(`id, energy, mood, active, recorded_at` — migration `0001:87-93`) carry no field naturally
shaped to hold a load-accumulation hypothesis's own content (`OpenCount`, `Threshold`) — the table
reads, from its column set, as built for a **user-reported** energy/mood signal (the digest care
gate doc 02 §7 separately describes: "if `current_state.energy` is low"), not for a
consolidation-written structural finding. This document does not silently pick a mapping onto
those columns (e.g., overloading `mood` with a serialized hypothesis, which nothing in doc 02 or
`m2b` sanctions) — it states the gap plainly, the same posture R3's belief-embeddings finding
already modelled for this project. **What is testable regardless of how the gap resolves**: one
append-only row is produced per firing `EvaluateLoad` call, it is distinguishable from a
user-reported `current_state` row (`active = 1` and either `energy` or `mood` populated, per the
existing rows' own apparent convention), and the row's existence is what `decision_log`'s own
entry references — `sdd-design` for this PR owes the exact column mapping or a migration decision
(a fourth schema question this milestone raises, alongside R1/R2/Q6, named here rather than
deferred silently to apply time).

**Verified by**: L2 — `EvaluateStagnation` findings produce one `decision_log` row per finding,
correctly attributed. `EvaluateLoad`'s firing case produces one `decision_log` row whose context
states the `lastHypothesisAt` mapping this requirement names; the `current_state` row's own exact
shape is left to `sdd-design`'s resolution of the named gap and is not asserted by this
requirement beyond "exactly one append-only row exists when the phase fires, zero when it does
not."

---

## 6. `nooma consolidate` — PR6 `feat/cli-consolidate`

Traced to proposal §3.2 item 8, `docs/01-architecture.md`'s existing contract ("a pure subcommand,
also used by the scheduler"), and the proposal's own exit criterion.

### R6.1 — `nooma consolidate [vault]` takes the vault's write lock, and fails cleanly rather than racing `serve`

**MUST**: `nooma consolidate` calls `vaultlock.Acquire` before opening the store for write —
`serve`'s own pattern (`cmd/nooma/serve.go`), not `capture`'s (which never takes the lock because
it proxies over HTTP to a `serve` that already holds it; `consolidate` writes directly, so it
needs the same guarantee `serve` gives itself). MUST: when the lock is already held (a `serve`
process is running, or a second `consolidate` invocation races the first), the command returns a
clear, non-zero-exit error naming the holder — never a silent hang or a corrupted concurrent
write.

**Verified by**: L4 (`test/e2e`, `e2e` build tag) — running `consolidate` against a vault a `serve`
process already holds the lock on returns the clean error; running it against an unlocked vault
succeeds.

### R6.2 — with no phase argument, `nooma consolidate` runs the whole pass

**MUST**: the default invocation (`nooma consolidate [vault]`, no phase flag) runs
`brain.ConsolidateService`'s full pass — all eight phases, in `Order()`'s sequence — and writes
`consolidation_last_run_at` on completion (R5.4).

**Verified by**: L4, part of the demo scenario (R6.5).

### R6.3 — per-phase invocation validates against `consolidation.ParsePhase`, never a second phase-name vocabulary

**MUST**: a per-phase flag (`nooma consolidate [vault] --phase=<name>`, exact flag shape is
`sdd-design`'s to fix) validates its argument through `consolidation.ParsePhase` (`m2b` spec R1.1)
— the same round-tripping vocabulary the core package already declares — and rejects an unknown
name with `consolidation.ErrUnknownPhase` surfaced as a CLI error, rather than declaring a second,
CLI-local list of the eight names.

**MUST**: `cmd/nooma`'s new file for this command does not itself contain two or more of the eight
phase-name string literals — `m2b` spec R1.2's own conformance test (`test/conformance/`) already
tree-scans every non-test `.go` file **outside** `internal/core/consolidation`, `cmd/nooma`
included, with no test-file edit required for this PR to be caught by it if violated.

**MUST**: a per-phase run does not write `consolidation_last_run_at` (R5.4's own `MUST NOT`,
restated at the CLI boundary since this is the command surface a user would reasonably expect
"I ran consolidate, so it updated the timestamp" from — and the reason it does not is stated
there, not invented fresh here).

**Verified by**: L4 — invoking a known phase name runs exactly that phase and leaves
`consolidation_last_run_at` untouched; an unknown name errors cleanly.

### R6.4 — the exit criterion: a hand-run pass against a real vault produces a readable `decision_log`

**MUST**: `nooma consolidate` run against a real, migrated vault (not the `m2d` demo golden set,
which is out of `m2c`'s scope — a small hand-built fixture vault suffices here) completes with
exit code 0 and, when the vault holds at least one unit qualifying for at least one phase's
effect, `decision_log` gains at least one row whose `rationale` is a legible sentence, not a code.
This is the proposal's own stated exit criterion for `m2c`: *"run the pass by hand on a vault and
read the `decision_log`."*

**Verified by**: L4 (`test/e2e`) — a minimal fixture vault (seeded through the real capture path,
not repo-constructed rows, so the run exercises the real read paths R5's requirements wire) run
through `nooma consolidate`, asserting completion and at least one legible `decision_log` row.

---

## 7. Handoffs discharged or explicitly deferred

Every open handoff `m2c` inherits, traced to the requirement that resolves it, per the task's own
instruction that none may be silently dropped.

| Handoff | Discharged by | Disposition |
|---|---|---|
| `goal_stagnation_days`'s two schema homes | R2.5 | **Discharged**: `config.goal_stagnation_days` is the one home; `calibration` stays unused; doc 02 §13's row is amended to state the decision |
| Dedup defense 1 has no code (doc 02 §6.5) | R5.6 | **Discharged**: existing active beliefs enter the derive prompt |
| `ConfigRepo`'s nil-sentinel shape (owner ruling Q1) | R2.4 | **Discharged**: nil-pointer struct, no migration 0003, one lazy-create write (R2.6) as the sole exception |
| `UnitRepo`'s weight write (proposal R2) | R1.1 | **Property stated, signature deferred to `sdd-design`** per R2's own instruction — this document does not pick single-vs-batch |
| Owner ruling Q2 (belief embeddings in memory) | R5.7 | **Discharged**: wired exactly as doc 02 §6.5 already states, no new table |
| Owner ruling Q3 (`incomplete` producer out of scope) | R5.1 | **Discharged, and proven**: the phase's read returns empty on every real M2 vault, tested against the real capture path, not merely asserted |
| `m2a` C6 — I24 unenforced until `m2c` gives the port a write path | R1.1, R3.3 | **Discharged**: the port method and its single-statement SQL implementation are I24's real enforcement |
| `m2a` C17 — `Resurface`'s `refused` guard is dead code | — | **Explicitly deferred, not `m2c`'s**: lives entirely inside `internal/core/weight`, which `m2c` does not touch (Scope boundary). Remains open technical debt for whichever future PR next edits `spread.go`, per C17's own "what a later link should do" |
| `m2a` C19 — `Edge.Strength = +Inf` coerced to 1 inside `Resurface`, not refused | R5.9 (via `m2b` spec R3.3) | **Discharged for M2's actual call path, not fixed at its source**: `m2c`'s only caller into `weight.Resurface` is `consolidation.Reweight`, which already refuses non-finite/out-of-range edge strengths **before** any edge reaches `Resurface`'s `clampStrength` (`m2b` spec R3.3's own MUST). `Resurface`'s own internal coercion — C19's real subject — is never exercised by any M2 code path and stays exactly as recorded, open for a future second caller |
| `m2a` C29 — `Displaces` validates nothing itself; a caller must resolve the margin first | — | **Explicitly deferred, not triggered by `m2c`**: `m2c` wires no caller of `focus.Displaces` or `focus.ResolveMargin` — the focus package has no consumer through the whole of M2 (proposal §4.3), unchanged by this change. `ConfigRepo` exposes `HysteresisMargin` for schema completeness (R2.4) but nothing in `m2c` reads it into `ResolveMargin`. C29's own instruction — "resolve the margin exactly once, at the boundary where the config row is read, and pass the resolved value down, never the raw `*float64`" — is restated here as the requirement `M4`'s first `Displaces` caller inherits, not one `m2c` discharges |

---

## 8. What this spec does not require

Matching the proposal's own scope boundary and `m2b`'s own §6 precedent: `internal/scheduler`'s
cron and ADR-0009's boot catch-up (reading `consolidation_last_run_at` R2.6/R5.4 now write), the
`consolidation_enabled` gate's actual consumer, the simulated-weeks demo golden set and its L4
test, digest/push delivery of any `pattern_eval` finding, any focus consumer, and the learning
module filling `PhaseLearn`'s slot are all `m2d`/M3/M5, and no requirement above depends on any of
them existing. Every requirement in this document is provable against a real (if small) SQLite
vault and fake `ports.LLMProvider`/`ports.EmbeddingProvider` implementations, with no network call
and no real clock.
