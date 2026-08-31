# Design — M2 Phase C: the consolidation runtime

Technical design for `m2c-consolidation-runtime`, the third of the four chained changes
[`m2-sleep-weight/proposal.md`](../m2-sleep-weight/proposal.md) §5 splits M2 into. Scope is that
document's **m2c block only** — ports, `internal/store/sqlite`, `internal/brain/consolidate.go`,
and `nooma consolidate`. `m2c` has no proposal of its own; it inherits scope, risks and rulings
from the umbrella, and its requirements from
[`spec.md`](spec.md), written first and read in full before this document.

`m2c` is the change where `internal/core`'s purity stops being a property of one directory and
becomes a boundary with traffic across it. `m2a` and `m2b` shipped 20 exported decision functions
that no caller reaches. This change gives every one of them a real input, a real write, and a real
`decision_log` row — and it is the first change in M2 whose floats arrive from a **SQLite column
with no `CHECK`** rather than from a Go literal in a test table.

**This design exists to make four decisions**, and §3 makes them: the `UnitRepo` weight-write
signature (spec R1.1), where a `LoadFinding` lands given that `current_state` has no field shaped
for one (spec R5.10), the runner's shape (spec R4.1/R4.2/R5.3/R5.4), and `ConfigRepo`'s shape
(spec R2.4/R2.6). Everything else in this document is the surface those four decisions imply.

> **Where this document contradicts `spec.md`, it says so out loud rather than quietly.** §10
> lists **three** claims `spec.md` makes that this design found to be wider than the code they
> describe — R0.1's lint-coverage claim, the Scope boundary's "no new `internal/core` code" claim,
> and the same boundary's "no new doc 02 prose" claim. Each is corrected there with the evidence
> that overturned it. This repository's recurring defect is not wrong code; it is a description
> wider than the code, and a design phase that inherits three of them silently is how the
> sixteenth becomes the seventeenth.

---

## 1. Ground truth this design was verified against

Every row was read at the named file and line in this session. Nothing below is quoted from
`spec.md`, `proposal.md`, or `m2b/design.md` without being checked at the source.

| Claim | How it was verified |
|---|---|
| `ports.UnitRepo` has exactly seven methods; **not one takes a `float64` parameter**, and not one writes `weight` or `last_touched_at` | `internal/ports/unitrepo.go:30-73` |
| `UnitRepo`'s own doc comment forbids a `List(status)` parameterized read and any `Delete`/`Remove`/`Purge`/`Drop`/`Destroy` prefix | `internal/ports/unitrepo.go:15-25` |
| The three sentinels are `ErrUnitNotFound`, `ErrUnitExists`, `ErrStatusConflict` | `internal/ports/unitrepo.go:76-88` |
| `UpdateEventAt`/`UpdateDueAt` take **two** distinguishable instants (the value and the audit `at`), and `repocontract` fixtures them with different values on purpose so a swapped-argument call site fails | `internal/ports/unitrepo.go:58,64`; `test/support/repocontract/repocontract.go:159-181` |
| `SetStatus` reads the current status, compares against `from`, then `UPDATE ... WHERE id = ? AND status = ?` — two statements, and `ErrStatusConflict` from either | `internal/store/sqlite/unitrepo.go:160-181` |
| `i03_units_never_deleted_test.go` reflects over **`ports.UnitRepo` alone** — a single `reflect.TypeOf((*ports.UnitRepo)(nil)).Elem()`, not a slice of interfaces | `test/conformance/i03_units_never_deleted_test.go:42` |
| `ports.RelationRepo`'s doc comment already *claims* the no-removal-prefix property holds "for every ports interface, not only `ports.UnitRepo`" — a claim the test above does not implement | `internal/ports/relationrepo.go:40-43` vs `i03_...:42` |
| `ports.DecisionLog` has 14 `DecisionAction` members, `Record` and `Since(t, limit)`. **No read by action.** `Decision.Context` is `json.RawMessage` | `internal/ports/decisionlog.go:34-133` |
| `brain_single_clock_read_test.go` fails a non-test file under `internal/brain/**` with **more than one `Now()` call expression in that one file**, and any `Now()` inside a `FuncDecl` that already takes a `time.Time` parameter | `test/conformance/brain_single_clock_read_test.go:77-104` |
| `CaptureService` holds the only `ports.Clock`; `captureRunner` holds none and takes `now time.Time` | `internal/brain/capture.go:45-93,154` |
| `RecallService.ScoredFor(ctx, text) ([]ScoredUnit, bool, error)` exists, and `internal/brain/correction.go:117-120` already maps `[]ScoredUnit` → `[]recall.FusedCandidate` | `internal/brain/recall.go:120`, `internal/brain/correction.go:117-120` |
| `consolidation.ConnectPairs` takes `[]recall.FusedCandidate` — so the mapping above is exactly `connect`'s adapter, already written once | `internal/core/consolidation/connect.go:104` |
| `config` declares six columns with SQL `DEFAULT`s and `consolidation_last_run_at TEXT` (nullable, "NULL = never ran"). **`consolidation_last_run_at` already exists — spec R2.6 needs no migration** | `internal/store/sqlite/migrations/0002_learning_and_search.sql:61-70` |
| No migration contains `INSERT INTO config` | grep over `internal/store/sqlite/migrations/` |
| `current_state` is exactly `(id TEXT PK, energy REAL, mood TEXT, active INTEGER NOT NULL DEFAULT 1, recorded_at TEXT NOT NULL)` — five columns, none of which can hold `LoadFinding.OpenCount`/`.Threshold`, and none of which distinguishes a consolidation-written row from a user-reported one | `internal/store/sqlite/migrations/0001_core_tables.sql:87-93`; `docs/03-data-model.md:104-110` |
| **No Go file anywhere references `current_state`.** The table has never been read or written | grep over `**/*.go` — the only matches are `internal/core/consolidation/patterns.go`'s doc comments |
| doc 02 §7 says the load watcher "writes a tentative hypothesis into `current_state`", and doc 02 §10 says `current_state` rows are **append-only** and "the load watcher opens it as a tentative hypothesis; the user confirms or corrects" | `docs/02-cognitive-core.md:786-792`, `:843-846` |
| doc 02 §7's digest care gate reads **`current_state.energy`** ("if `current_state.energy` is low… it holds back non-urgent items") | `docs/02-cognitive-core.md:770-771` |
| doc 02 §7's tone gate names the fact the brain passes as the word **`loaded`** | `docs/02-cognitive-core.md:772-773` |
| doc 02 §10's product rule: "If forced to choose, keep `energy` (capacity) and drop the mood labels" — `mood` is the column the product is *least* committed to | `docs/02-cognitive-core.md:845-846` |
| `weight.Effective` **deliberately does not sanitize `NaN`/`±Inf`** and says so at length; four reachable shapes return `NaN` | `internal/core/weight/decay.go:43-57,65-77` |
| `consolidation.SelectConnectSources` sorts by `Effective` descending with `if a != b { return a > b }` — **not total under `NaN`**: two `NaN`-scored entries compare "equal" in both directions *and* skip the id tie-break, so `sort.Slice`'s ordering is unspecified | `internal/core/consolidation/connect.go:61-66` |
| `consolidation.Belief` has `{ID, Facet, TopicKey, Confidence, LastReinforcedAt}` — **no `Content`** | `internal/core/consolidation/derive.go:29-35` |
| `weight.Boost` is exactly `{UnitID string, Weight float64, LastTouchedAt time.Time}` | `internal/core/weight/boost.go:36-40` |
| `consolidation.Cold` and `consolidation.Source` declare the **identical five fields**; `Source`'s own doc comment says so | `internal/core/consolidation/connect.go:24-34` |
| `internal/core/consolidation` exports 20 functions; `MergeProposals` validates non-finite vector components on both sides and explains the `sort.Slice` non-total-comparator hazard it closes | `internal/core/consolidation/derive.go:82-99` |
| `.golangci.yml` has exactly **two** depguard rules: `core-purity` (files `**/internal/core/**`) and `sqlite-containment` (denies the driver and `database/sql` outside `internal/store` and `test/integration`). **There is no rule scoped to `internal/ports` or `internal/brain`** | `.golangci.yml:47-92` |
| `test/conformance/calibration_doc_test.go` parses §13 rows matching `internal/core/<pkg>.<Symbol>`, requires a constant holding the documented number, and has a floor of 21 symbols. Constants outside `internal/core/` are not reachable by it | `test/conformance/calibration_doc_test.go:16-47` |
| `test/integration/schema_golden_anchor_test.go`'s hand-written list names **objects** (`table units`, `trigger units_fts_ai`), never columns — so adding a column to an existing table needs no edit there | `test/integration/schema_golden_anchor_test.go:56-60` |
| `test/integration/migrate_test.go` hard-codes `wantVersion = 2` at **four** sites plus a `BinaryVersion != 2` assertion — a migration 0003 edits five test lines there | `test/integration/migrate_test.go:64,104,172,235,343` |
| `internal/config.DocumentedTaskNames` **already contains `belief_derivation`** — `derive`'s LLM task needs no new task name | `internal/config/validate.go:194-202` |
| `cmd/nooma/serve.go` acquires `vaultlock.Acquire(vault)` *after* deciding the binding and *before* opening the store; `wireBrain` returns `nil, nil` (not an error) when any task is unbound | `cmd/nooma/serve.go:71-79,101-104`; `cmd/nooma/wiring.go:149-171` |
| The 400-line ceiling counts **implementation plus docs, separately from test lines**; `m2a`'s four measured links ran 99/117/265/319 impl+docs against 505/439/1486/685 test lines | `docs/06-harness.md:336-339,364-369` |
| `docs/06-harness.md` §4 already carries I03, I05, I11, I12 and I24 — `m2c` adds **no new invariant row** | `docs/06-harness.md:182-196` |

---

## 2. What `m2c` decides, in one paragraph

`m2c` declares **five ports** (two widened, three new), implements them over SQLite, and adds one
`internal/brain` service that reads the clock once, walks `consolidation.Order()`, hands each
phase the data its pure function needs, persists what comes back, and writes one `decision_log`
row per effect. Nothing in it decides *what* to do — every predicate, every formula and every
refusal already shipped in `m2a`/`m2b`. What it decides is **shape**: which method signatures make
an invariant unexpressible rather than merely untested, where a value with no schema home goes,
and how one pass sees one instant. It adds one new file to `internal/core` (§10.2, a
correction to `spec.md`'s scope claim) and one new migration (§3.2, the decision the owner is
most likely to overturn).

---

## 3. The four decisions

### 3.1 D1 — the `UnitRepo` weight write: one batch method taking `[]weight.Boost`

**The tension, restated in one line** (proposal R2): M1's rule is one method per field and no
signature capable of writing the wrong thing; I24 requires `weight` and `last_touched_at` to move
**together**, so they cannot be two methods; and `reweight` produces `[]weight.Boost` for a whole
pass, which the per-field discipline has no shape for.

**Decision.**

```go
// ports.UnitRepo gains exactly one weight-writing method.
ApplyBoosts(ctx context.Context, boosts []weight.Boost, at time.Time) error
```

Four choices are packed in there. Each is argued, and each states what it does not cover.

#### (a) The parameter is `weight.Boost`, and nothing else on this interface can carry a weight

`weight.Boost` is `{UnitID, Weight, LastTouchedAt}` — the three fields I24 binds together, in the
type `m2a` declared for exactly this purpose (`m2a` spec R2.1: *"`m2a` does not add the port — it
fixes the shape the port must have"*). A `SetWeight(id string, w float64)` is not a method this
interface *chooses* not to have; §3.1(d) makes it a method this interface cannot acquire.

**What this does not cover, stated plainly:** the signature makes `LastTouchedAt`
**unomittable**, not **correct**. `time.Time`'s zero value is a legal `time.Time`, so
`weight.Boost{UnitID: "u", Weight: 0.9}` compiles and writes year 1 into `last_touched_at`. I24 as
written ("neither is written alone") is satisfied — both columns move — and the design does not
claim more. Guarding the zero value at the port would be a validation the port has no basis for
(a vault restored from a backdated import legitimately carries old timestamps), so the guard
belongs where the value is produced, and `weight.Revive`/`Resurface` already put `now` there.

#### (b) A slice, not a single `Boost`

Three reasons, in order of weight:

1. **`Reweight` returns `[]weight.Boost` for a whole pass.** A single-`Boost` method forces the
   runner into a loop that zips a unit id to a weight and a timestamp N times, which is precisely
   the shape spec R1.1's testable claim forbids ("no implementation may write one unit's `Weight`
   paired with a different unit's… `LastTouchedAt`"). Handing the port the values already paired
   removes the zip.
2. **Spec R3.3 already fixed all-or-nothing semantics**: a `Boost` naming a non-existent unit
   returns `ports.ErrUnitNotFound` "with **neither** column touched on any row." That is a
   transaction, and a transaction needs a batch to be about.
3. A single write is `ApplyBoosts(ctx, []weight.Boost{b}, at)` — no expressiveness is lost.

**What this does not cover:** batching means one slow phase holds one write transaction over up to
`ConnectSourceLimit × ConnectCandidateK × 2` = 200 rows. That is bounded (`m2b` §4.5) and small.
It is *not* bounded for a future caller that hands `ApplyBoosts` a whole-vault slice; the port's
doc comment says the caller owns the bound, and nothing enforces it.

#### (c) `at` is a separate parameter, distinct from `Boost.LastTouchedAt`

`at` becomes `units.updated_at`; `Boost.LastTouchedAt` becomes `units.last_touched_at`. They are
the same instant in every M2 call path (both are the pass's `now`), and they are still two
parameters, because that is the shape `UpdateEventAt`/`UpdateDueAt` already have and the shape
`repocontract` already exploits: fixture them with **different** instants and a call site that
swaps them, or an implementation that writes one into the other's column, fails
(`repocontract.go:159-165`'s own recorded reasoning — "the contract, not review, decides this").

#### (d) How I24 becomes structural — three legs, only one of which is a proof

| # | Leg | What it makes impossible | Level |
|---|---|---|---|
| **1** | **No method of `ports.UnitRepo` declares a `float64` parameter**, after unwrapping slice/map/pointer/array element types. Verified true today (§1): the seven existing methods take no float at all | `SetWeight(id, w float64)`, `SetWeights(map[string]float64)`, `[]float64` — every shape that can hand a repository a bare weight | L2, reflection |
| **2** | Exactly one method's parameter list mentions `weight.Boost` (bare or sliced) | A second weight-writing method, however named | L2, reflection |
| **3** | The two-column assignment `weight = ?, last_touched_at = ?` appears in exactly one method's SQL text under `internal/store/sqlite` (spec R3.4) | An `UPDATE` that moves one column without the other, at the SQL layer where the interface cannot see | L2, source-text scan |

**Leg 1 is the one that carries the weight**, and it is a genuine structural property rather than
a restatement: a `float64` is the only primitive a weight can be, and the interface has none.

**What legs 1–3 do not cover**, named rather than rounded up: a future method taking a *different*
struct with a `Weight float64` field passes leg 1 (the scan unwraps containers, not struct
fields); a method named `Touch(id string, at time.Time)` that writes `last_touched_at` alone
passes all three, because I24's text is about a *weight write* moving both — a bare timestamp
write is a different question doc 02 §2 does not forbid. Both are recorded here rather than
discovered later.

#### (e) Non-finite `Weight` is refused at the port's door, not left to the column

`units.weight` is `REAL NOT NULL DEFAULT 1.0` with **no `CHECK`** (`0001:11`). Every M2 producer
of a `Boost` already refuses non-finite input upstream (`weight.Revive`, `Reweight`'s edge
validation), so the port's guard has no reachable caller in M2 — and it is added anyway, because
`ApplyBoosts` is a general contract and the store is the first place in this codebase where a
refused value becomes **durable**:

```go
// ErrNonFiniteWeight is returned by ApplyBoosts when any Boost carries a
// NaN or ±Inf Weight. Nothing is written: the whole batch is refused.
var ErrNonFiniteWeight = errors.New("weight is not a finite number")
```

**What this deliberately does not rely on:** any claim about how SQLite or `github.com/ncruces/go-sqlite3`
encodes a `NaN` bound to a `NOT NULL REAL` parameter. That behaviour was **not** verified in this
session, and the refusal above makes it unreachable, which is why the design refuses rather than
reasons about it.

---

### 3.2 D2 — `current_state` has no shape for a `LoadFinding`. **Recommendation: migration 0003, one column.**

This is the highest-risk decision in `m2c` and the one the owner is most likely to overturn, so it
is priced in full and the recommendation is stated plainly rather than hedged.

#### The gap, verified

`current_state` is `(id, energy, mood, active, recorded_at)`. `consolidation.LoadFinding` is
`{OpenCount int, Threshold int}`. Three distinct things have no home:

1. **the evidence** (`OpenCount`, `Threshold`) — no numeric column but `energy`, which doc 02 §7's
   digest care gate reads as the user's own capacity;
2. **the writer** — nothing distinguishes a consolidation-written hypothesis from a user-reported
   reading, and `EvaluateLoad`'s `lastHypothesisAt` anchor is *precisely* a read that must make
   that distinction;
3. **the tentativeness** — doc 02 §7/§10 call the row "a tentative hypothesis" the user
   "confirms or denies", and there is no column for confirmed-vs-open.

Nothing in the vault has ever written this table (§1), so there is no existing convention to
inherit.

#### The options, priced

| Option | What it gives | What it costs | Verdict |
|---|---|---|---|
| **A. Migration 0003 adds `current_state.source TEXT NOT NULL DEFAULT 'user'`.** The row written is `(id=uuid, energy=NULL, mood='loaded', active=1, recorded_at=now, source='consolidation')`; the evidence goes to `decision_log.context` | A **schema-enforced** discriminator for the `lastHypothesisAt` read; `mood='loaded'` becomes content rather than a discriminator, anchored on doc 02 §7's own word; `energy` stays NULL so the digest care gate is never fed a value the watcher did not observe | One migration file; doc 03's `current_state` block; `make schema-golden` (two golden files); **five** hard-coded `wantVersion = 2` sites in `test/integration/migrate_test.go`; doc 02 §7/§10 gain one sentence each. No `schema_golden_anchor_test.go` edit (it is object-level, verified §1) | **Recommended** |
| **B. No migration: `mood='loaded' AND energy IS NULL` is the convention.** Same row, minus `source` | No migration | The anchor read becomes `WHERE mood='loaded' AND energy IS NULL` — **a convention, not a constraint**, correct only while nothing else writes `current_state`, which is true through M2 and false the moment M3 ships the user's confirm/deny. And it writes a system-derived structural claim into the one column doc 02 §10 marks droppable ("if forced to choose… drop the mood labels") | Rejected. This is the exact defect class this repository keeps producing: a claim ("distinguishable from a user-reported row") wider than the code enforcing it |
| **C. No `current_state` write in M2.** The finding goes to `decision_log` only; the cooldown anchors on a new `DecisionLog.LatestByAction` read | No migration, no speculative column | (i) It makes the **audit log load-bearing operational state** — the glass box becomes a functional dependency of the brain's behaviour, which is an architectural change and would need **ADR-0018**, not a doc amendment; (ii) it declines a write doc 02 §7 **and** §10 both describe, so doc 02 must be amended to say the watcher writes nothing — *removing* stated behaviour rather than implementing it (non-negotiable #1) | Rejected, but it is the strongest alternative and the owner may prefer it |
| **D. An `insight` unit** | — | `idx_units_unique_active_insight` keys on `structured_data.$.domain`/`$.metricKey`, which a load hypothesis has neither of; doc 02 §12 reserves insights for perception (v2); and a unit carries weight and decay, so `archive` — five slots earlier in the same pass — would cool the hypothesis it just wrote | Rejected |
| **E. Ship the watcher with no write and no anchor at all** | — | `lastHypothesisAt` is then permanently `nil`, so `EvaluateLoad` fires **every night** the count is above threshold — exactly the spam doc 02 §7's cooldown exists to prevent — and `LoadCooldownDays` ships as a constant no path can exercise | Rejected |

#### Why A, said plainly

1. **"No migration" does not buy "no doc change."** B needs doc 02 to state a convention; C needs
   doc 02 to withdraw a behaviour. Under non-negotiable #1 all three options amend doc 02, so the
   migration is the only *additional* cost A carries — and it is small and fully enumerated above.
2. **The cooldown anchor is a read, and a read needs a discriminator the schema enforces.** This
   is the whole argument. B's discriminator is a sentence in a doc comment; A's is a column.
3. **Ruling Q1's "no migration 0003" was scoped to `config`'s defaults**, on the specific argument
   that a Go constant and a SQL `DEFAULT` are two sources for one value that can drift. That
   argument does not transfer: this is a **missing column**, not a duplicated default. `m2b`
   design §9 Q2 already anticipated a migration becoming right once R1 resolved, and it did.
4. **One column, not two.** A `hypothesis TEXT` JSON column was considered and declined: the
   evidence belongs in `decision_log.context`, which doc 02 §11 already defines as where a
   decision's reasoning lives. `current_state` carries the **claim**; `decision_log` carries the
   **reasoning**. That split is the one doc 02 already draws.

#### The migration, and what it does not do

```sql
-- internal/store/sqlite/migrations/0003_current_state_source.sql
ALTER TABLE current_state ADD COLUMN source TEXT NOT NULL DEFAULT 'user';  -- user|consolidation
```

**What it does not cover, each named:**

- **It does not make `current_state` resolvable.** doc 02 §7's "after a *resolved* check-in" needs
  a `state_confirmed`/`state_denied` learning signal, which is M5. In M2 every hypothesis row
  keeps `active = 1` forever and "resolved" is not representable. `EvaluateLoad`'s cooldown is
  therefore anchored on the **hypothesis itself** — `m2b` §9 Q6's open question, mapped here, and
  the mapping is written into the `decision_log` row's `context` exactly as Q6 instructed.
- **It does not close `mood`'s vocabulary.** `mood` is free text. `'loaded'` and `'consolidation'`
  become Go constants in `internal/ports`, which puts them **outside**
  `calibration_doc_test.go`'s reach — that gate reads §13 rows naming
  `internal/core/<pkg>.<Symbol>` only. Stated rather than implied, per spec R0.3's own posture.
  They are strings, not numbers, so §13 is not their home either; their pin is an L2 test reading
  the migration's own column comment, the shape `relation.AllCreatedBy` already uses against
  `0001:37`.
- **It does not decide M3's read shape.** M3 owns delivery and will decide what the digest reads.
  `source` is the minimum a *writer* needs; it is not a schema for a consumer that does not exist.
- **It is not an ADR.** Adding a discriminator column is a schema decision recorded in doc 03. If
  the owner overturns to **C**, that *is* architectural — `decision_log` becoming operational
  state — and it takes **ADR-0018**.

---

### 3.3 D3 — the runner: one entry point, one clock read, one execution path

#### (a) The shell/worker split, and the constraint that makes it non-obvious

`test/conformance/brain_single_clock_read_test.go` fails a non-test file under `internal/brain/**`
with **more than one `Now()` call expression in that file** (§1). So the obvious API —
`Consolidate(ctx)` for a whole pass and `ConsolidatePhase(ctx, p)` for one — is **not
expressible**: two exported entries each reading `s.clock.Now()` is two `Now()` calls in
`consolidate.go`.

That is not an obstacle to work around. It is the guard doing its job: two entry points are two
places where "one pass, one instant" can rot.

**Decision: one exported entry, and the scope is a parameter.**

```go
// internal/brain/consolidate.go

type ConsolidateService struct {
    clock ports.Clock
    run   consolidateRunner   // holds no ports.Clock, and no field that could produce one
}

// ConsolidateRequest selects what one invocation runs. Its zero value is a
// whole pass — the shape `nooma consolidate` with no flag has.
type ConsolidateRequest struct {
    // Phase, when non-nil, runs exactly that one phase and nothing else.
    // A *Phase and not a Phase: PhaseExpireIncomplete is Phase(0), so a
    // bare field cannot distinguish "not set" from "run expire_incomplete".
    // Same nil-sentinel idiom relation.Resolve and ResolveWeightThreshold
    // already use, for the same reason.
    Phase *consolidation.Phase
}

// Consolidate is this package's second and last ports.Clock.Now() read —
// one per invocation, whole pass or single phase.
func (s *ConsolidateService) Consolidate(ctx context.Context, req ConsolidateRequest) (ConsolidateReport, error) {
    return s.run.at(ctx, req, s.clock.Now())
}
```

The `*consolidation.Phase` sentinel is load-bearing and easy to get wrong: `Phase` is an `int` with
`iota` starting at 0, so a `Phase` field plus "zero means whole pass" would silently make
`--phase=expire_incomplete` run the whole pass.

#### (b) One execution path, filtered — never a second dispatch

The per-phase run **iterates `consolidation.Order()` and skips**; it does not call a phase
function directly.

```go
for _, p := range consolidation.Order() {
    if req.Phase != nil && p != *req.Phase {
        continue
    }
    if err := r.runPhase(ctx, p, pass); err != nil { return report, err }
}
```

Why the filter rather than a direct call: there is then exactly **one** place each phase's body is
reached from, so a per-phase run cannot execute something the whole pass does not, or vice versa.
It also keeps `Order()` the only enumeration in the file — `m2b` §3.2 leg 4's tree scan bans two
or more of the eight phase-name **string literals** outside `internal/core/consolidation`, and
`runPhase`'s `switch p { case consolidation.PhaseArchive: … }` switches over **constants**, which
that leg explicitly permits.

`runPhase`'s `switch` carries a `default` returning an error naming the unhandled `Phase`, so a
ninth phase added to `Order()` fails loudly instead of being silently skipped. The L2 spy test
(spec R4.1) asserts every element of `Order()` is reached, including `PhaseLearn`'s no-op arm, and
that the recorded sequence equals `Order()` exactly.

#### (c) The pass context: one instant, one `since`, one config read

```go
// passContext is everything one invocation reads before any phase runs.
// Assembled once in consolidateRunner.at, passed by value to every phase.
type passContext struct {
    now   time.Time            // the single clock read
    cfg   ports.VaultConfig    // the whole config row, read once (§3.4)
    since *time.Time           // cfg.ConsolidationLastRunAt, held once
}
```

`since` is read **before any phase runs** and the same `*time.Time` value is handed to both
`Strengthen` (slot 3) and `SelectConnectSources` (slot 4) — spec R5.3, and `m2b` §9 Q8's own
recommendation ("one field on the pass context, read once"), taken. `cfg` is read once for the
same reason: `archive`'s threshold (slot 2) and `pattern_eval`'s two knobs (slot 7) must come from
one snapshot, or a pass can straddle a config edit.

`since` is read on a **per-phase** run too — `--phase=strengthen` needs it — and the write below is
what does not happen.

#### (d) `consolidation_last_run_at`: one write site, gated on the same field that chose the scope

```go
if req.Phase == nil {
    if err := r.cfg.RecordConsolidationRun(ctx, pass.now); err != nil { … }
}
```

There is exactly one call to `RecordConsolidationRun` in the tree, and its guard is the same field
that selected the scope — so "whole pass ⇔ timestamp written" is **one branch**, not two facts
that can drift apart. Spec R5.4's `MUST NOT` (a per-phase run never writes it) is satisfied by
there being no second site, and the L2 fixture asserts the call count is 1 for a pass and 0 for a
phase.

The instant written is `pass.now` — the pass's own single clock read — not a fresh one, which the
single-clock-read gate would reject anyway.

**What this does not cover:** the write happens after every phase *returns*, including phases that
produced nothing. A phase that returned an error aborts the pass before the write, which is
correct (an aborted pass did not happen), but it means a pass that fails at slot 8 leaves
`since` pointing at the *previous* pass and re-does slots 1–7's reads next time. Idempotent for
every phase in M2 (`archive` re-reads status, `strengthen` re-computes from the same `since`,
`connect` re-excludes existing pairs), so this is a cost, not a correctness problem — stated so
it is not discovered.

#### (e) I12 in both directions

Every persist site goes through one helper:

```go
func (r consolidateRunner) record(ctx context.Context, now time.Time,
    action ports.DecisionAction, rationale string, detail any) error
```

`detail` is marshalled into `Decision.Context`; `rationale` is the legible sentence spec R6.4's
exit criterion needs. Ten new `DecisionAction` members (§7.5), each naming **phase + effect
kind**, so a reader of `decision_log` can tell which phase wrote which row without opening
`Context` (spec R4.2).

**Honest limit:** nothing structurally forbids a future persist call that skips `record`. The
guard is the L2 fixture pair spec R4.2 requires (every phase fed → one row per effect; no phase
fed → zero rows) plus review. This is a convention, and it is named as one rather than presented
as a gate.

**The refusal rule, decided once and applied to all four `corrupted` slices.** `Archive`,
`Strengthen`, `Reweight` and `MergeProposals` can all report data they refused. Spec R4.2's
`MUST NOT` names only `Reweight`'s. Decided uniformly: **a `corrupted` entry from any phase is
surfaced in `ConsolidateReport` and never in `decision_log`.** A refusal is a decision with no
vault effect, and I12's own "with an effect" qualifier excludes it. `nooma consolidate` prints
them, so `doctor`'s eventual need (`m2b` §4.5) is served by the surface the user actually runs.

`archive`'s `ErrStatusConflict` skip **is** logged (spec R4.3), and the line between the two is
worth stating because it will be re-asked: a `corrupted` entry never became a decision at all,
while a conflict is a decision the pass *made* and a race prevented — which is exactly what doc 02
§11's glass box is for.

---

### 3.4 D4 — `ConfigRepo`: a nil-sentinel struct that feeds `Resolve*` unchanged, and one write

```go
// internal/ports/configrepo.go

// VaultConfig mirrors the config singleton row (migration 0002:61-70) as
// nil-sentinel typed pointers. nil means "the row does not exist, or that
// column is NULL". ConfigRepo never decides what nil means — that is
// consolidation.Resolve*/focus.ResolveMargin's job, and each of them
// already takes exactly the pointer type below (owner ruling Q1, option C).
type VaultConfig struct {
    WeightThreshold        *float64    // → consolidation.ResolveWeightThreshold
    HysteresisMargin       *float64    // → focus.ResolveMargin — NO reader in m2c (§7.4)
    ConsolidationEnabled   *bool       // → no reader in m2c; m2d's cron gate
    GoalStagnationDays     *int        // → consolidation.ResolveGoalStagnationDays
    MentalLoadThreshold    *int        // → consolidation.ResolveMentalLoadThreshold
    ConsolidationLastRunAt *time.Time  // → passContext.since, and m2d's boot catch-up
}

type ConfigRepo interface {
    // Load returns the singleton row (id = 1). When it does not exist every
    // field is nil and the error is nil: an unwritten config row is the
    // normal state of a vault, not a failure. No migration seeds it and
    // this port never creates it except through RecordConsolidationRun.
    Load(ctx context.Context) (VaultConfig, error)

    // RecordConsolidationRun writes at to consolidation_last_run_at,
    // creating row id = 1 with the migration's own SQL DEFAULTs for every
    // other column when it is absent. ConfigRepo's only write (spec R2.6).
    RecordConsolidationRun(ctx context.Context, at time.Time) error
}
```

Four points that are decisions rather than transcription:

**The struct is returned by value and carries no method.** It is data the runner holds for the
length of one pass. `Resolve*` are the interpreters, and they live in `internal/core` where the
defaults and their §13 rows already are.

**`Load` returns values as stored, including corrupt ones** (spec R2.4). The NaN posture is
therefore not `ConfigRepo`'s: `ResolveWeightThreshold` maps non-finite or out-of-`[0,
WeightCeiling]` to the default, and `ResolveGoalStagnationDays`/`ResolveMentalLoadThreshold` map
`nil`/`<= 0` to theirs — all four shipped and tested in `m2b`. §8 tabulates which of the six
fields is actually protected and which is not.

**A malformed `consolidation_last_run_at` is an error, not `nil`.** The column is TEXT; a value
that does not parse as `unitTimeLayout` returns an error and aborts the pass. Reading it as `nil`
would be the silent-widening failure: `Strengthen(es, nil)` returns nothing (harmless), but
`SelectConnectSources(ss, nil, now)` takes the **whole live pool**, which turns a corrupt
timestamp into up to 100 judge calls and a graph built from a parse failure.

**`RecordConsolidationRun` is an `UPSERT` that never names a default in Go.**

```sql
INSERT INTO config (id, consolidation_last_run_at, updated_at)
VALUES (1, ?, ?)
ON CONFLICT(id) DO UPDATE SET consolidation_last_run_at = excluded.consolidation_last_run_at,
                              updated_at = excluded.updated_at;
```

Every other column takes the migration's own `DEFAULT`. This is what keeps ruling Q1's "one source
for one default" true through the lazy create: the Go constants (`DefaultWeightThreshold` and its
three siblings) are pinned to the SQL `DEFAULT`s by `m2b`'s existing L2 tests, and this statement
never restates either.

`config.updated_at` is `TEXT NOT NULL` with no `DEFAULT`, so it must be supplied — with `at`, the
pass's own instant. Verified against `0002:69`.

---

## 4. The rest of the port surface

Everything below follows from §3. Each row states the phase that consumes it and the shape `m2b`
§8 fixed, so nothing here is invented.

### 4.1 `ports.UnitRepo` — four new methods

| Method | Phase | Why this shape |
|---|---|---|
| `ApplyBoosts(ctx, []weight.Boost, at) error` | `reweight` | §3.1 |
| `CountLiveByType(ctx, t unit.Type) (int, error)` | `pattern_eval` | Owner ruling 6. Returns an `int`, never a slice, so an unbounded read cannot enter a phase that needs one integer. The name carries `Live`, so it cannot be asked for archived units. **It does take a filter argument** — `UnitRepo`'s doc comment discourages that, and the discouragement is specifically about **status** (a status parameter is how a live read becomes a non-live one); `unit.Type` is a closed nine-member vocabulary with no live/non-live axis, so the rule is honoured in substance. Stated rather than glossed |
| `IncompleteOlderThan(ctx, cutoff time.Time) ([]consolidation.Incomplete, error)` | `expire_incomplete` | `m2b` §8's own fixed name — the one deliberate non-live read in M2 (I02's exception), named so the exception is explicit rather than hidden in a `List(status)` |
| `LiveDecayStates(ctx) ([]consolidation.Cold, error)` | `archive`, `connect`, `derive` | `consolidation.Cold` and `consolidation.Source` declare the **identical five fields** (§1), so one read shape serves all three and `brain` maps `Cold → Source` at slots 4 and 5. It returns decay fields plus status, never `unit.Unit` — `m2a` D9's "no unit-shaped value a read path could persist", kept one layer up |

Two consequences of `IncompleteOlderThan` and `LiveDecayStates` that are decisions, not details:

**The cutoff duplicates a predicate, and the duplication is bounded on purpose.** `cutoff` is
computed in `brain` as `now.Add(-consolidation.IncompleteExpiryHours * time.Hour)`, and
`ExpireIncomplete` applies the same 24-hour predicate itself. Two implementations of one rule is a
drift risk, so: the port's doc comment states the SQL filter is a **bound, never the decision**,
and an L2 test asserts `brain` derives the cutoff from `consolidation.IncompleteExpiryHours` and
not from a literal. Over-delivering rows changes nothing; under-delivering is the only failure
mode, and it is the one the constant pin catches.

**`RelationRepo.Evidence` (§4.2) takes no `since` for the opposite reason, and the asymmetry is
deliberate.** `IncompleteOlderThan` has an exception to carry in its name and a constant to pin;
`strengthen`'s co-use predicate is a comparison against a *runtime* value with no constant to pin
against, so pushing it into SQL would create a second implementation of `!Before` with nothing to
hold it honest. One predicate, one place.

**`LiveDecayStates` is called three times per pass, at slot 2 (`archive`), slot 4 (`connect`) and
slot 5 (`derive`), and never cached.** `archive` changes unit status at slot 2; a cached snapshot
would hand `connect` and `derive` units it had just archived as live sources. `derive` additionally
re-runs its own `SelectConnectSources` selection rather than reusing `connect`'s slot-4 result
(§7.3) — caching either hop is exactly the kind of thing that gets "optimized" into a bug, so both
are written down here and in the runner's doc comment.

**Both `LiveDecayStates` and `Evidence` are unbounded reads.** `archive` must see every live unit —
that is what the phase *is* — and `strengthen` must see every relation. On a personal vault
(doc 02's model) that is fine; it is O(vault) memory per pass and there is no paging. Named as a
risk (§12 R4) rather than mitigated, because mitigating it would put a bound in SQL that core's
own decision functions do not have.

### 4.2 `ports.RelationRepo` — two new reads

| Method | Phase | Shape |
|---|---|---|
| `Evidence(ctx) ([]consolidation.RelationEvidence, error)` | `strengthen` | The join `m2b` §8 fixed: each relation with **both** endpoints' `last_touched_at`, in one query. The alternative — relations, then units, then zip in `brain` — is two round trips and a correctness hazard if a unit moves between them |
| `ExistingPairs(ctx, pairs []consolidation.Pair) (map[consolidation.Pair]bool, error)` | `connect` | Keyed by `consolidation.CanonicalPair`, over the candidate set only, bounded by `ConnectSourceLimit × ConnectCandidateK` = 100. A stored `a→b` returns `true` for a lookup built from `b→a`'s canonical form (spec R3.6) |

### 4.3 `ports.SelfModelRepo` — new, three methods

```go
// Belief is one self_beliefs row (migration 0001:72-85). It carries Content,
// which consolidation.Belief does not — the derive prompt needs the text
// (spec R5.6) and no core decision function does.
type Belief struct {
    ID               string
    Facet            selfmodel.Facet
    TopicKey         string
    Content          string
    Confidence       float64
    Origin           string
    SourceUnitID     *string
    Status           string
    LastReinforcedAt time.Time
    CreatedAt        time.Time
    UpdatedAt        time.Time
}

type SelfModelRepo interface {
    // ActiveBeliefs returns every belief with status = 'active', every
    // facet included. One read, no status parameter — LiveByIDs's own
    // precedent, and the name is the guard (m2b §8). derive's two dedup
    // defenses and pattern_eval's stagnation watcher share it; the
    // facet filter is EvaluateStagnation's own job, in core.
    ActiveBeliefs(ctx context.Context) ([]Belief, error)

    // UpsertByTopicKey writes b, conflicting on self_beliefs.topic_key
    // (UNIQUE, 0001:75) — RelationRepo.Upsert's pattern applied to the
    // one column doc 02 §10 defines as a belief's natural key. This is
    // the CREATE half of a MergeDecision (MergeInto == "").
    UpsertByTopicKey(ctx context.Context, b Belief) error

    // ReinforceByID updates confidence and last_reinforced_at for the
    // belief named by id, leaving topic_key, content, facet, origin and
    // source_unit_id unchanged. It returns ErrBeliefNotFound rather than
    // creating a row (spec R2.2). This is the MERGE half.
    ReinforceByID(ctx context.Context, id string, confidence float64, at time.Time) error
}
```

**The two names are the guard.** Spec R2.2's `MUST NOT` — routing a merge through the topic-key
upsert silently creates a second belief instead of reinforcing the one the merge found — becomes a
mistake you have to type deliberately: `UpsertByTopicKey` and `ReinforceByID` name the key each
one uses, and `MergeDecision.MergeInto` is an **id**. The L1 `repocontract` case proves the
semantics; the naming is what makes the wrong call read wrong at the call site.

`ErrBeliefNotFound` is a new sentinel (`ports.ErrUnitNotFound`'s shape).

**Float posture:** `self_beliefs.confidence` is `REAL NOT NULL DEFAULT 0.5` with no `CHECK`. Its
only consumer in `m2c` is `consolidation.Reinforce`, which refuses non-finite and out-of-`[0,1]`
values and returns `false` — no write. **No comparison or sort in `m2c` orders beliefs by
confidence**, so a corrupt value cannot make a comparator non-total here; it can only fail to
reinforce, silently. That silence is the residual, and it is named.

### 4.4 `ports.StateRepo` — new, two methods

```go
// StateSourceConsolidation and MoodLoaded are the two literals the load
// watcher writes. Constants in internal/ports, therefore OUTSIDE
// test/conformance/calibration_doc_test.go's reach (§13 covers
// internal/core symbols only) — stated, not implied. They are pinned
// instead to migration 0003's own column comment by an L2 test, the shape
// relation.AllCreatedBy already uses against 0001:37.
const (
    StateSourceUser          = "user"
    StateSourceConsolidation = "consolidation"
    MoodLoaded               = "loaded"
)

type StateHypothesis struct {
    ID         string
    Mood       string     // MoodLoaded for the load watcher
    RecordedAt time.Time
}

type StateRepo interface {
    // OpenHypothesis appends one current_state row with
    // source = 'consolidation', energy NULL and active = 1. Append-only:
    // this port has no update path and no removal-prefixed method, so
    // doc 02 §10's "append-only rows" is structural here rather than a
    // convention the caller remembers.
    OpenHypothesis(ctx context.Context, h StateHypothesis) error

    // LastHypothesisAt returns the recorded_at of the most recent row
    // written with source = 'consolidation', or nil when there is none.
    // It feeds consolidation.EvaluateLoad's lastHypothesisAt parameter
    // directly (m2b §9 Q6, mapped in §3.2).
    LastHypothesisAt(ctx context.Context) (*time.Time, error)
}
```

`energy` is left NULL deliberately: writing a value there would feed doc 02 §7's digest care gate
("if `current_state.energy` is low") a number the watcher never observed.

### 4.5 `ports.DecisionLog` — unchanged

No new method. `Record` and `Since` are enough; the ten new `DecisionAction` members (§7.5) are
data, not surface. This is only true because §3.2 chose **A** over **C** — under C, `DecisionLog`
would gain a `LatestByAction` read and become operational state.

### 4.6 I03 across the widened surface

`i03_units_never_deleted_test.go`'s single `reflect.TypeOf((*ports.UnitRepo)(nil)).Elem()` becomes
a loop over a slice of five `reflect.Type` values — `UnitRepo`, `RelationRepo`, `SelfModelRepo`,
`ConfigRepo`, `StateRepo` — closing the gap between what `RelationRepo`'s doc comment already
*claims* ("for every ports interface, not only `ports.UnitRepo`") and what the test checks (§1).
Spec R2.7; the widening happens in the PR that adds the last of the three new interfaces, so the
test and its subjects land together.

---

## 5. The store layer

### 5.1 One file per repository, the existing convention

`internal/store/sqlite/` gains `configrepo.go`, `selfmodelrepo.go`, `staterepo.go` and additions
to `unitrepo.go`/`relationrepo.go`, each with its `*_integration_test.go` sibling under the
`integration` tag — the file-per-repository convention already in place.

`testdata/schema/store_api.golden` is regenerated with **`make store-api-golden`**, a different
target from `make schema-golden`, named in this PR's own task list rather than discovered as a
fast-loop failure (proposal R12, and M1 recorded the same surprise twice).

### 5.2 `ApplyBoosts` is one statement per boost inside one transaction

Spec R3.3 requires both columns to land from one `UPDATE` — never two statements a partial failure
could leave half-applied — and requires a missing unit id to leave **no** row touched.

```
BEGIN IMMEDIATE
  for each boost:
    UPDATE units SET weight = ?, last_touched_at = ?, updated_at = ?
     WHERE id = ?
    → 0 rows affected ⇒ ROLLBACK, return ports.ErrUnitNotFound
COMMIT
```

`updated_at` travels with them, following every other `UnitRepo` write method. Non-finite `Weight`
is refused **before** `BEGIN` (§3.1(e)), so no transaction is opened for a batch that cannot land.

**What this does not claim:** it is one statement per *unit*, not one statement for the batch. A
single multi-row `UPDATE ... FROM (VALUES ...)` was considered and declined — it is a second SQL
shape to review for the same guarantee the transaction already gives, and `RowsAffected` over a
multi-row update cannot tell which id was missing.

### 5.3 I05's structural half, scoped to read paths

Spec R3.4: no method whose name identifies it as a read contains, in its own SQL text, an
assignment to `units.weight` or `units.last_touched_at`. Verified as an L2 source-text scan over
`internal/store/sqlite`'s non-test files, asserting the two-column assignment appears in exactly
one method — `ApplyBoosts`.

The scoping note the test's own doc comment must carry (proposal R13): `m2b` §4.5(b) **declined**
bulk decay materialization, so there is no permitted-but-unused bulk write for this scan to
accidentally forbid. The scoping is stated anyway, because R13 named the risk and a test whose
scope is inferred is a test whose scope drifts.

### 5.4 Timestamp encoding

Every new repository reuses `unitTimeLayout` (`time.RFC3339`, doc 03's TEXT encoding) and
`formatUnitTime`. `ConfigRepo.Load` and `StateRepo.LastHypothesisAt` parse with the same layout and
**error** on a malformed value (§3.4), never returning `nil` for "unparseable".

---

## 6. Package layout, the dependency map, and how `now` travels

### 6.1 Layout

```
internal/ports/                       PR 1..3 — stdlib + internal/core/* only
  ├── unitrepo.go        PR 1  + ApplyBoosts, CountLiveByType, ErrNonFiniteWeight
  │                      PR 2  + IncompleteOlderThan, LiveDecayStates
  ├── relationrepo.go    PR 2  + Evidence, ExistingPairs
  ├── selfmodelrepo.go   PR 3  Belief, SelfModelRepo, ErrBeliefNotFound
  ├── configrepo.go      PR 3  VaultConfig, ConfigRepo
  └── staterepo.go       PR 3  StateHypothesis, StateRepo, the three literals

internal/store/sqlite/                PR 5..6
  ├── unitrepo.go        PR 5  the four new methods
  ├── relationrepo.go    PR 5  the two new reads
  ├── selfmodelrepo.go   PR 6
  ├── configrepo.go      PR 6
  ├── staterepo.go       PR 6
  └── migrations/0003_current_state_source.sql   PR 4

internal/core/consolidation/          PR 10a — the ONE core addition (§10.2)
  ├── prompt.go          BuildDerivePrompt
  └── derive.go          + Belief.Content

internal/brain/                       PR 7..11
  ├── consolidate.go     ConsolidateService, ConsolidateRequest, consolidateRunner,
  │                      passContext, ConsolidateReport, runPhase, record
  └── consolidate_phases.go   the eight phase arms' I/O (split across PRs 8..11)

cmd/nooma/
  ├── consolidate.go     PR 12  runConsolidate, --phase, vaultlock, report rendering
  ├── tasks.go           PR 12  + tasksConsolidateConsumes
  └── wiring.go          PR 12  + wireConsolidate

.golangci.yml            PR 1  + ports-purity and brain-boundary depguard rules (§10.1)

test/support/memrepo/    PR 1..3  fakes for all 13 new methods
test/support/repocontract/ PR 1..3  shared cases, run against fake (L1) and sqlite (L3)
test/conformance/        PR 1..3, 7  I24 reflection, i03 widened, I11 behavioural, I12
test/integration/        PR 4..6  migration 0003's version bump, the L3 repo suites
test/e2e/                PR 12  R6.1's lock race, R6.3's per-phase, R6.4's exit criterion

docs/02-cognitive-core.md  §6.5 (derive's source selection), §7 + §10 (the current_state row)
docs/03-data-model.md      current_state gains source
docs/06-harness.md         untouched — I03/I05/I11/I12/I24 all already have §4 rows
```

### 6.2 Dependency-rule check

`internal/ports` gains imports of `internal/core/{consolidation, selfmodel, weight}`. No cycle:
`consolidation → {unit, weight, recall, relation, selfmodel}` and none of those imports `ports`.

`internal/brain` imports `internal/core/*` and `internal/ports`, never `internal/store`.
`internal/store/sqlite` imports `internal/core/*`, `internal/ports` and the driver, never
`internal/brain`.

**None of that is currently enforced by lint** — see §10.1, which is where this design's most
useful small change lives.

### 6.3 How `now` and `since` travel

```
cmd/nooma/consolidate.go
  systemClock{} ─┐
                 ▼
ConsolidateService.Consolidate(ctx, req)
  now := s.clock.Now()            ── the ONE read (guarded by brain_single_clock_read_test)
                 ▼
consolidateRunner.at(ctx, req, now)
  cfg   := configRepo.Load(ctx)           ── once, before any phase
  since := cfg.ConsolidationLastRunAt     ── once; the SAME pointer to both consumers
                 ▼
  for p := range consolidation.Order():     (skip when req.Phase != nil && p != *req.Phase)
    1 expire_incomplete  IncompleteOlderThan(now − IncompleteExpiryHours) → ExpireIncomplete(us, now)          → SetStatus
    2 archive            LiveDecayStates()  → Archive(cs, ResolveWeightThreshold(cfg.WeightThreshold), now)    → SetStatus
    3 strengthen         Evidence()         → Strengthen(es, since)                                            → RelationRepo.Upsert
    4 connect            LiveDecayStates()  → SelectConnectSources(ss, since, now)
                         RecallService.ScoredFor → FusedCandidate → ExistingPairs → ConnectPairs
                         → judge (relation_evaluation) → relation.DecodeJudgment → ProposeRelation             → RelationRepo.Upsert
    5 derive             LiveDecayStates() → SelectConnectSources(ss, since, now) → LiveByIDs
                         ActiveBeliefs() → BuildDerivePrompt → llm (belief_derivation)
                         → EmbeddingProvider ×len(active) → MergeProposals → Reinforce   → UpsertByTopicKey / ReinforceByID
    6 reweight           Reweight(states, newEdges, now)                                                       → ApplyBoosts
    7 pattern_eval       ActiveBeliefs() → EvaluateStagnation(bs, ResolveGoalStagnationDays(cfg), now)
                         CountLiveByType(mental_load), StateRepo.LastHypothesisAt()
                         → EvaluateLoad(n, ResolveMentalLoadThreshold(cfg), lastAt, now)                       → OpenHypothesis
    8 learn              nothing — ruling 3. The arm exists, performs no work, writes no decision_log row
                 ▼
  if req.Phase == nil: configRepo.RecordConsolidationRun(ctx, now)     ── the one write site
```

Two properties read straight off this: **the instant enters once and travels as a value**, and
**`derive` re-derives its own source list rather than reusing `connect`'s** — see §7.3.

Proposal §4.5's consequence stands and is restated because it is the pass's semantics:
`archive`'s Δt and `expire_incomplete`'s 24-hour window are both measured from the pass's **start**
instant, not from when each phase reaches each unit.

---

## 7. Decisions the spec left to this document, made here

### 7.1 `connect`'s candidate search reuses `RecallService`, through the adapter that already exists

Spec R5.5 requires `connect` to call the existing fused ranking rather than a second fusion.
`RecallService.ScoredFor(ctx, text) ([]ScoredUnit, bool, error)` is that entrance, and
`internal/brain/correction.go:117-120` **already** maps `[]ScoredUnit` → `[]recall.FusedCandidate`
— which is exactly what `consolidation.ConnectPairs` takes. The adapter is written once and
shared, not copied.

The persist decision is `relation.Decide(confidence, relation.Resolve(thresholdsFor(type)))`
through `consolidation.ProposeRelation`, unchanged, with
`relation.CreatedByConsolidation` the only difference from capture's judge call (spec R5.5).

**`brain/capture.go:485`'s bare `"system"` literal adopts `relation.CreatedBySystem`** in the same
PR — `m2b` §8's one-line handoff, discharged rather than carried forward again.

**A judgement that decided nothing writes no `decision_log` row**, and this **differs from
capture**, which does log `ActionRelationDiscarded`. The difference is deliberate: capture's
discard is one judgement about one message the user just sent, and explaining it is the glass
box working; `connect`'s is up to 100 machine-initiated judgements a night, and logging every
non-decision would bury the rows a reader is actually looking for. Doc 02 §4's "a judgment that
decided nothing writes nothing" and spec R4.2's second `MUST` both point this way. **Flagged for
owner review — §12 Q2.**

### 7.2 `nooma consolidate` refuses to start when a task it needs is unbound

`serve` degrades: `wireBrain` returns `nil, nil` and the HTTP handlers answer 503. `consolidate`
**must not** degrade, and the reason is spec R5.4: the pass writes `consolidation_last_run_at`
after "every phase of a whole-pass run has completed". A pass that silently skipped `connect` and
`derive` because no provider was bound would write a timestamp claiming a full pass ran, and the
next pass's `since` would then exclude everything those two phases never saw. Silent data loss,
one pass later.

So `runConsolidate` resolves its own task set and **fails with a clear error naming the unbound
task** before taking the lock:

```go
// tasksConsolidateConsumes are the three tasks a consolidation pass needs.
// capture_processing is NOT among them — no phase classifies.
var tasksConsolidateConsumes = []string{"relation_evaluation", "belief_derivation", "embedding"}
```

`belief_derivation` is **already** in `internal/config.DocumentedTaskNames` (§1), so this needs no
config-vocabulary change.

**What this does not cover:** a provider that is bound but *down* at pass time. That is an outage,
not a configuration gap, and the phase's own error aborts the pass — which is right, for the same
reason: an aborted pass does not write the timestamp.

### 7.3 `derive`'s source selection — a gap `m2b` left, closed by reuse

`m2b` ships `MergeProposals`, `Reinforce` and `DeriveTopicKey`, and **no function that decides
which units a derivation runs over**. Spec R5.6 covers only what goes *into* the prompt. So
`m2c` must decide it, and deciding it in `brain` would put a decision in the wiring layer.

**Decision: `derive` calls `consolidation.SelectConnectSources` on its own fresh read**, and
materializes the resulting ids through the existing `LiveByIDs`. Three consequences:

- No new core decision function, and no decision in `brain`. "The units that changed since the
  last sleep, most alive first, capped" is the same question `connect` asks.
- **`ConnectSourceLimit` (20) now governs two phases**, which §13's row does not say. It becomes a
  §13 **annotation** (the Default column's number is unchanged, so
  `calibration_doc_test.go` stays green) plus one sentence in doc 02 §6.5 — see §10.3.
- `derive` re-runs the selection rather than reusing `connect`'s slot-4 result, so
  `--phase=derive` behaves identically to slot 5 of a whole pass. Caching connect's list would
  make the two paths differ, which is exactly what §3.3(b)'s single-execution-path rule exists to
  prevent.

The function's name now under-describes its second caller. Renaming it to `SelectRecentSources` is
the right fix and belongs in a **core** change, not here — recorded as a handoff (§11).

### 7.4 `HysteresisMargin` and `ConsolidationEnabled` are read and never used

`VaultConfig` mirrors all six columns for schema completeness (spec R2.4). Two of the six have
**no reader in `m2c`**, and saying so is the point:

- **`HysteresisMargin`** — `m2c` wires no caller of `focus.Displaces` or `focus.ResolveMargin`;
  the focus package has no consumer through the whole of M2 (proposal §4.3). `m2a`'s C29
  obligation — "resolve the margin exactly once, at the boundary where the config row is read, and
  pass the resolved value down, never the raw `*float64`" — is inherited by **M4's first
  `Displaces` caller**, not discharged here.
- **`ConsolidationEnabled`** — `m2d`'s cron gate. **Decision: it gates the scheduler, never the
  explicit CLI invocation.** A user typing `nooma consolidate` is the consent that flag exists to
  represent; a command that silently did nothing because a config row said so would be the worst
  possible answer to "why did nothing happen". Flagged for owner review — §12 Q3.

### 7.5 The `DecisionAction` vocabulary §3.3(e) promised — enumerated, and corrected to ten

§3.3(e) commits to naming "Eleven new `DecisionAction` members... each naming phase + effect
kind," but nowhere enumerates them, and a repo-wide search of this document for `Action[A-Z]`
turns up exactly one hit — a pre-existing capture-time action, not one of the promised new ones.
Counted directly off this document's own commitments — spec R4.2's exhaustive per-effect list
(`Transition`, `StrengthChange`, accepted `ProposedRelation`, `MergeDecision` (create or
reinforce), `Boost`, `StagnationFinding`, `LoadFinding`), spread across the eight phases §6.3's
pipeline diagram fixes, plus spec R4.3's separately-logged conflict skip — the honest count is
**ten**, not eleven. "Eleven" is corrected to "ten" everywhere it appeared (§3.3(e), §4.5), and
§13's "fourteen-to-twenty-five-member" arithmetic is corrected to fourteen-to-twenty-four
(14 existing + 10 new = 24).

**How the count was built, phase by phase:**

| Phase | Core function(s) | Effect kind(s) that persist | New `Action` members |
|---|---|---|---|
| `expire_incomplete` | `ExpireIncomplete` → `[]Transition` | one transition kind — `ReasonIncompleteExpired`/`ReasonIncompletePromoted` (`transition.go:17-18`) both travel in `Context`, not the `Action` (see the open question below) | 1 |
| `archive` | `Archive` → `[]Transition`; R4.3's `ErrStatusConflict` skip | the transition itself, plus the skip R4.3 requires logged separately — a race prevented the transition, a different kind of effect from the transition it did not become | 2 |
| `strengthen` | `Strengthen` → `[]StrengthChange` | one kind | 1 |
| `connect` | `ProposeRelation` → an accepted `ProposedRelation` | one kind — §7.1: a judgement that decided nothing writes nothing, so there is no second `connect` action for a discard, unlike capture's `ActionRelationDiscarded` | 1 |
| `derive` | `MergeProposals`/`Reinforce` → `MergeDecision` | R5.8's own split: `MergeInto == ""` routes to `UpsertByTopicKey` (create), `MergeInto != ""` routes to `ReinforceByID` (reinforce) — two distinguishable vault effects, not one | 2 |
| `reweight` | `Reweight` → `[]weight.Boost` | one kind (`corrupted` entries are never logged, §3.3(e)) | 1 |
| `pattern_eval` | `EvaluateStagnation` → `[]StagnationFinding`; `EvaluateLoad` → `LoadFinding` | R5.10 logs each separately: a stagnation finding and a firing load hypothesis are different effects with different `Context` shapes | 2 |
| `learn` | none (ruling 3) | no persisted effect, no `decision_log` row | 0 |
| **Total** | | | **10** |

**The ten new members, enumerated.** They follow the fourteen existing members' dotted-domain
*structure* (`capture.classify`, `relation.duplicate.recorded`) but not their spelling, and the
difference is worth stating rather than glossing: not one of the fourteen contains an underscore
— every segment is a single lowercase word. Two of the new segments, `expire_incomplete` and
`pattern_eval`, are `Phase.String()`'s own spelling (`internal/core/consolidation/phase.go`) and
are reused deliberately so the logged action names the phase exactly as the code does. The
remaining snake_case segments are newly coined here and have no precedent in the existing
vocabulary. Renaming them to single-word segments is a defensible alternative; what is not
defensible is claiming they match a convention they extend.

```go
// Ten new DecisionAction members — design §7.5, spec R4.2/R4.3/R5.8/R5.10.
const (
    ActionExpireIncompleteTransitioned    DecisionAction = "consolidate.expire_incomplete.transitioned"
    ActionArchiveArchived                 DecisionAction = "consolidate.archive.archived"
    ActionArchiveConflictSkipped          DecisionAction = "consolidate.archive.conflict_skipped"
    ActionStrengthenApplied               DecisionAction = "consolidate.strengthen.applied"
    ActionConnectRelationPersisted        DecisionAction = "consolidate.connect.relation_persisted"
    ActionDeriveBeliefCreated             DecisionAction = "consolidate.derive.belief_created"
    ActionDeriveBeliefReinforced          DecisionAction = "consolidate.derive.belief_reinforced"
    ActionReweightBoostApplied            DecisionAction = "consolidate.reweight.boost_applied"
    ActionPatternEvalStagnationFound      DecisionAction = "consolidate.pattern_eval.stagnation_found"
    ActionPatternEvalLoadHypothesisOpened DecisionAction = "consolidate.pattern_eval.load_hypothesis_opened"
)
```

**What this enumeration does not settle:** whether `expire_incomplete`'s two outcomes
(`ReasonIncompleteExpired` vs `ReasonIncompletePromoted`, `transition.go:17-18`) deserve their own
`Action` members the way `derive`'s create/reinforce split does is a real judgment call, not a fact
this document can read off `spec.md`. Spec R4.2's own list writes `MergeDecision`'s split out
explicitly ("create or reinforce") and does not do the same for `Transition`, so this design
follows the spec's own text rather than forcing symmetry with `derive` — but it is named here as
the one place in this count that is a reading of the spec's silence, not a fact verified against a
return type. If the owner would rather split it, that is one more member and the vocabulary becomes
eleven — a decision this document is naming rather than making silently.

---

## 8. Float posture at every new boundary

The store is a new external float source: `units.weight`, `units.weight_decay_rate`,
`relations.strength`, `relations.confidence`, `self_beliefs.confidence` and
`config.weight_threshold`/`hysteresis_margin` **all carry no `CHECK`**. This table states, per
value, what happens to a non-finite or out-of-domain reading — including the four rows where the
answer is "nothing, and here is what breaks". It also carries one row that is **not new to `m2c`**:
`unit_embeddings.embedding` (migration 0002, shipped in M1) has carried an identical no-`CHECK`,
non-total-comparator hazard since M1, and `connect`'s reuse of `RecallService.ScoredFor` (§7.1)
makes `m2c` a second reachable caller of it, not the first — named here because this table's job
is every float this design's own code touches, not only the columns `m2c` adds.

| Value, on entry from the store | Reaches | Guard | Covered? |
|---|---|---|---|
| `units.weight`, `weight_decay_rate` → `consolidation.Cold` | `Archive` | `Archive` refuses non-finite into `corrupted`, never archives | ✅ |
| `units.weight`, `weight_decay_rate` → `consolidation.Source` | `SelectConnectSources` → `weight.Effective` → `sort.Slice` | **none** | ❌ **see below** |
| `relations.strength` → `RelationEvidence` | `Strengthen` | refuses `NaN`, `±Inf` and finite-outside-`[0,1]` into `corrupted` | ✅ |
| `weight.Edge.Strength` (from this pass's new relations) | `Reweight` | refuses at its own door, before `clampStrength` | ✅ |
| `self_beliefs.confidence` | `consolidation.Reinforce` | refuses non-finite and out-of-`[0,1]`, returns `false` — no write | ✅ (silent) |
| `config.weight_threshold` | `ResolveWeightThreshold` | non-finite or outside `[0, WeightCeiling]` → default | ✅ |
| `config.goal_stagnation_days`, `mental_load_threshold` | `Resolve*` | `nil` or `<= 0` → default. Integers; no NaN axis | ✅ |
| `config.hysteresis_margin` | nothing in `m2c` | — | n/a — no reader (§7.4) |
| Belief embedding vectors (provider, not store) | `MergeProposals` | refuses non-finite components on both sides, and its doc comment explains the `sort.Slice` hazard it closes | ✅ |
| `weight.Boost.Weight`, on the way **out** | `ApplyBoosts` | `ErrNonFiniteWeight`, whole batch refused (§3.1(e)) | ✅ |
| `unit_embeddings.embedding` → `recall.VectorIndex` (read back, not the write path above) | `RecallService.ScoredFor` → `recall.Search` → `sort.Slice`, reached by `connect`'s slot-4 candidate search (§7.1) and by every M1 capture/correction recall call | `EmbeddingRepo.Put` refuses a non-finite component on write (`recall.Normalize`), but nothing guards a row corrupted before that guard shipped or written by a future path that bypasses `Put` — the read side, `recall.Search`'s identical comparator, is unguarded | ❌ **see below** |

### 8.1 The two uncovered paths, named rather than rounded up

Both uncovered rows above share one comparator shape and one root cause: a value with no `CHECK`
at the column, read back into a `sort.Slice` comparator that is not a strict weak ordering under
`NaN`.

**`consolidation.SelectConnectSources`** sorts by `weight.Effective` with

```go
if eligible[i].effective != eligible[j].effective {
    return eligible[i].effective > eligible[j].effective
}
return eligible[i].id < eligible[j].id
```

Under `NaN`, `a != b` is **true**, so the id tie-break is skipped and both `less(i,j)` and
`less(j,i)` are `false`. Incomparability is then not transitive (`NaN ~ 0.5`, `NaN ~ 0.9`, but
`0.9 > 0.5`), so the comparator is not a strict weak ordering and `sort.Slice`'s output is
unspecified. `weight.Effective` returns `NaN` for four documented input shapes and **deliberately
does not sanitize** (`decay.go:43-57`).

This is the **sixth** instance of the non-total-comparator class in M2's pure half —
`weight.clampStrength`, `focus.Displaces`, `focus.Rank`, `focus.clamp` and `MergeProposals`
(`derive.go:82-85`'s own precedent comment names the same five, all shipped before it) came first.
**It is not, however, the first instance of the class that is reachable at all**: `recall.Search`'s
comparator, below, has been reachable since M1. What is true and narrower is that `m2c` is the
first change that feeds `SelectConnectSources` itself from a column rather than a test table, so
it is the first instance *this function's own hazard* becomes live.

**Why it is not fixed in core here:** the fix is a `corrupted` second return on
`SelectConnectSources`, which changes an `m2b`-shipped signature, and `m2c`'s scope adds core code
only where nothing else can hold it (§10.2 — one file, for a different reason).

**What `m2c` does instead**, at the only door it owns: `consolidateRunner` partitions
`[]consolidation.Cold` into usable and refused **before** mapping to `[]Source`, using the same
non-finite predicate `Archive` applies, and reports the refused ids through `ConsolidateReport`'s
`corrupted` set (§3.3(e)). `archive` at slot 2 and `connect`/`derive` at slots 4/5 therefore refuse
the identical set of rows.

**What that does not cover:** the guard lives in `brain`, so a future second caller of
`SelectConnectSources` inherits nothing. The real fix belongs in `internal/core/consolidation` and
is recorded as a handoff (§11) and an owner-review item (§12 Q4).

**`recall.Search`**, reached through `RecallService.ScoredFor` (`internal/brain/recall.go:120-133`),
carries the identical shape: `sort.Slice(scored, func(i, j int) bool { return scored[i].Score >
scored[j].Score })` (`vector.go:113-115`) is not a strict weak ordering once any `Score` is `NaN`,
for the same reason as above. Its own doc comment (`vector.go:66-72`) already names the hazard. The
vector comes from `unit_embeddings.embedding`, a `BLOB NOT NULL` column with **no `CHECK`**
(`migrations/0002_learning_and_search.sql:74-80`).

**`m2c` does not introduce this reachability.** `RecallService.ScoredFor` — and therefore
`recall.Search` — has been reachable through the shipped M1 capture and correction flow since M1
(`internal/brain/correction.go:117-120`, verified §1). `m2c` §7.1 adds a **second** caller:
`connect`'s slot-4 candidate search routes through the same `ScoredFor` method, unchanged, rather
than a new adapter.

**`m2c` leaves this one, honestly.** Today's only write path, `EmbeddingRepo.Put`
(`internal/store/sqlite/embeddingrepo.go:46-50`), calls `recall.Normalize` and refuses a `NaN` or
`±Inf` component before encoding, so the hazard needs a row corrupted before that guard shipped, or
a future writer that bypasses `Put` — the same reachability bar this section already applies to
`units.weight`. `m2c` adds no guard of its own at `connect`'s call site: `ScoredFor`'s result is
consumed as `[]recall.FusedCandidate` through `correction.go`'s existing adapter (§7.1), and no
code in this design inspects a `Score` for non-finiteness before that mapping. Because `m2c`
neither introduces nor closes this gap, it is recorded as a handoff (§11) rather than an owner
open question here — the gap predates `m2c` and belongs to `internal/core/recall`, not to
`internal/core/consolidation`.

---

## 9. Test matrix

| What | Level | Where | PR |
|---|---|---|---|
| **I24 structural, leg 1** — no `ports.UnitRepo` method declares a `float64` parameter, after unwrapping slice/map/pointer/array elements | L2, reflection | `test/conformance/i24_...` | 1 |
| **I24 structural, leg 2** — exactly one method's parameters mention `weight.Boost` or `[]weight.Boost` | L2, reflection | `test/conformance/i24_...` | 1 |
| `ApplyBoosts` writes `Weight` and `LastTouchedAt` from the **same** `Boost` for each of ≥3 units, fixtured with distinguishable values per unit so a cross-unit zip fails | L1 (fake) + L3 (sqlite) | `repocontract` | 1, 5 |
| `ApplyBoosts` with a non-existent id returns `ErrUnitNotFound` and leaves **every** row untouched (≥3 units, one missing, asserted on the other two) | L1 + L3 | `repocontract` | 1, 5 |
| `ApplyBoosts` with a `NaN` or `±Inf` `Weight` returns `ErrNonFiniteWeight` and writes nothing | L1 + L3 | `repocontract` | 1, 5 |
| `at` and `Boost.LastTouchedAt` land in `updated_at` and `last_touched_at` respectively — fixtured with **different** instants, `UpdateEventAt`'s own method | L1 + L3 | `repocontract` | 1, 5 |
| `CountLiveByType` counts, over a fixture holding live and non-live units of ≥2 types; no `ports.UnitRepo` method both accepts a `unit.Type` and returns `[]unit.Unit` | L1 + L2 | `repocontract`, `test/conformance` | 1 |
| **I03 widened** — the reflection loop sweeps all five interfaces; mutation-verified by adding a `PurgeX` method to each in turn | L2 | `i03_units_never_deleted_test.go` | 3 |
| **The two new depguard rules fail for the right reason** — a temporary `internal/brain` import of `internal/store/sqlite`, recorded verbatim in the PR (§10.1) | lint | `.golangci.yml` | 1 |
| `IncompleteOlderThan` returns only `incomplete` units older than `cutoff`; `brain` derives `cutoff` from `consolidation.IncompleteExpiryHours`, never a literal | L1 + L3; L2 for the constant pin | `repocontract`, `test/conformance` | 2, 5 |
| `LiveDecayStates` returns `pool` units only, with the five decay fields, and **no `unit.Unit`-shaped value** | L1 + L3 | `repocontract` | 2, 5 |
| **`LiveDecayStates` is called exactly three times per whole pass** — slot 2 (`archive`), slot 4 (`connect`), slot 5 (`derive`) — and `derive`'s slot-5 read is never `connect`'s cached slot-4 slice: a spy on the fake `UnitRepo` counts the calls, and a fixture that archives a unit at slot 2 asserts neither `connect` nor `derive` sees it as a live source | L2 | `internal/brain` | 8, 9, 10b |
| `Evidence` returns each relation joined to **both** endpoints' `last_touched_at`, over a static fixture (transactional isolation beyond SQLite's default is out of scope — spec R3.5) | L3 | `relationrepo_integration_test.go` | 5 |
| `ExistingPairs` returns `true` for a lookup built from the **opposite** direction's canonical pair | L1 + L3 | `repocontract` | 2, 5 |
| `UpsertByTopicKey` twice with the same key and different content yields **one** row; `ReinforceByID` changes only `confidence` and `last_reinforced_at`; `ReinforceByID` on an absent id returns `ErrBeliefNotFound` and creates **no** row | L1 + L3 | `repocontract` | 3, 6 |
| `ActiveBeliefs` returns every facet's active beliefs and no non-active one | L1 + L3 | `repocontract` | 3, 6 |
| `ConfigRepo.Load` on an empty backing store returns an **all-nil** struct and a nil error; on a freshly-migrated vault with no `config` row, the same — proving the migration seeds nothing | L1 + L3 | `repocontract`, `configrepo_integration_test.go` | 3, 6 |
| `ConfigRepo.Load` on a malformed `consolidation_last_run_at` returns an **error**, never `nil` | L3 | `configrepo_integration_test.go` | 6 |
| `RecordConsolidationRun` on an absent row creates it with the migration's own `DEFAULT`s on every other column, read off disk via `migrationSQLText`; on an existing row changes only that column and `updated_at` | L1 + L3 | `repocontract`, integration | 3, 6 |
| **Migration 0003** — `current_state.source` exists with `DEFAULT 'user'`; `user_version` is 3; the schema golden and doc 03 agree (existing `schema_doc_test.go` covers this once both are updated) | L3 + L2 | `test/integration/migrate_test.go`, `schema_doc_test.go` | 4 |
| `OpenHypothesis` appends (two calls → two rows, neither updated); `LastHypothesisAt` ignores rows with `source = 'user'` and returns `nil` when none exists | L1 + L3 | `repocontract` | 3, 6 |
| `ports.StateSourceConsolidation`/`MoodLoaded` are pinned to migration 0003's column comment, read off disk | L2 | `test/conformance/` | 4 |
| **I05 structural half** — the two-column assignment appears in exactly one method's SQL under `internal/store/sqlite`, scoped to read paths, with the scoping in the test's doc comment | L2, source scan | `test/conformance/i05_...` | 5 |
| **I11 behavioural half** — a spy records each phase's invocation; the sequence equals `consolidation.Order()` exactly, `PhaseLearn`'s arm is reached and reached last; mutation-verified by reordering the `switch` | L2 | `test/conformance/i11_...` | 7a |
| A per-phase run reaches exactly one arm; an unknown phase name errors through `consolidation.ErrUnknownPhase` | L2 | `internal/brain` | 7a |
| **`consolidation_last_run_at`** — whole pass: `RecordConsolidationRun` called exactly once, with the pass's own instant; per-phase: never called | L2 | `internal/brain` | 7a |
| **`since`** — `Strengthen` and `SelectConnectSources` receive an identical `*time.Time`; with no `config` row both receive `nil` | L2 | `internal/brain` | 7a |
| **I12 direction 1** — every phase fed: one `decision_log` row per persisted effect, each `Action` naming phase and effect kind | L2 | `test/conformance/i12_...` | 7b |
| **I12 direction 2** — no phase fed: the pass completes and `decision_log` gains **zero** rows | L2 | `test/conformance/i12_...` | 7b |
| **I12 exclusion** — no `decision_log` row for any `corrupted` entry from any of the four phases that report one | L2 | `test/conformance/i12_...` | 7b |
| **R4.3** — three units planned for archival, `ErrStatusConflict` on the second: pass completes, 1st and 3rd archived, 2nd skipped **and logged**, no error propagates | L2 | `internal/brain` | 7b, 8 |
| `archive` is called with the **configured** `WeightThreshold`, not the default, when one differs | L2 | `internal/brain` | 8 |
| `expire_incomplete`'s read returns empty on a vault seeded through the **real capture path**, and the persist step is a no-op | L3 | `test/integration` | 8 |
| `connect`'s persisted relations carry `CreatedByConsolidation`; the judged decision routes through `relation.Resolve`/`Decide` unchanged; `capture.go:485` uses `relation.CreatedBySystem` | L2 | `internal/brain` | 9 |
| `derive`'s prompt contains every active belief's `topic_key` and `content`; with none, the prompt still sends and **names the empty state** | L2, fake `LLMProvider` capturing the prompt | `internal/brain` | 10b |
| `BuildDerivePrompt` is a pure function over its two slices; deterministic across repeated calls; ≥3 beliefs and ≥3 units so ordering is falsifiable | L1 | `internal/core/consolidation` | 10a |
| `derive` calls `EmbeddingProvider` exactly `len(activeBeliefs)` times per phase run; a source scan confirms no port or store method persists a belief vector | L2 | `internal/brain`, `test/conformance` | 10b |
| One create-decision and one merge-decision from the same run → exactly one `UpsertByTopicKey` and one `ReinforceByID`, each with the right target | L2 | `internal/brain` | 10b |
| `reweight` persists every `boosts` entry through `ApplyBoosts`, including a unit that also appears in the same call's `corrupted`; no `decision_log` row for the corrupted half | L2 | `internal/brain` | 11 |
| **The `Source` sanitization (§8.1)** — a `Cold` row with a non-finite `Weight` is refused before `SelectConnectSources` sees it, by `archive` and `connect`/`derive` alike; fixtured with ≥3 units so removing the guard changes the fixture's outcome | L2 | `internal/brain` | 9, 11 |
| `EvaluateStagnation`'s findings → one `decision_log` row each, correctly attributed | L2 | `internal/brain` | 11 |
| `EvaluateLoad` firing → exactly one `current_state` row with `source = 'consolidation'`, `mood = 'loaded'`, `energy` NULL, plus one `decision_log` row whose `Context` states the `lastHypothesisAt` mapping; not firing → zero of both | L2 + L3 | `internal/brain`, integration | 11 |
| `nooma consolidate` against a vault a `serve` holds the lock on returns a clean non-zero error naming the holder; against an unlocked vault, succeeds | L4 | `test/e2e` | 12 |
| `nooma consolidate --phase=<known>` runs exactly that phase and leaves `consolidation_last_run_at` untouched; `--phase=<unknown>` errors cleanly | L4 | `test/e2e` | 12 |
| **The exit criterion** — a minimal fixture vault seeded through the real capture path, run through `nooma consolidate`, exits 0 with ≥1 `decision_log` row whose `rationale` is a legible sentence | L4 | `test/e2e` | 12 |
| `nooma consolidate` with an unbound task refuses before taking the lock, naming the task | L4 | `test/e2e` | 12 |

No test in `m2c` reaches the network or a real LLM; every provider is a fake. No test under
`internal/brain` or `test/conformance` uses the real clock.

**Coverage.** `m2c` adds almost nothing to `internal/core` (§10.2 is ~1 file), so
`scripts/core-coverage.sh`'s floor is not the binding constraint it was for `m2a`/`m2b` — but
`make check-all` is still the pre-PR command, because the L3 suites and the migration's
version-bump edits live behind the `integration` tag that `make check` does not run.

---

## 10. Three claims in `spec.md` this design corrects

### 10.1 R0.1's lint-coverage claim is false, and closing it is 15 lines of YAML

Spec R0.1 states the dependency rule holds at the new boundary and that "no new test is needed for
this half of the rule since `.golangci.yml`'s allow-lists already cover these three directories."

**Verified false.** `.golangci.yml` has exactly two depguard rules (§1): `core-purity`, scoped to
`**/internal/core/**`, and `sqlite-containment`, which denies the driver and `database/sql`
outside `internal/store`. **Nothing prevents `internal/brain` from importing
`internal/store/sqlite`, and nothing prevents `internal/ports` from importing it either** — such an
import compiles, passes lint, and violates `docs/06-harness.md` §1's dependency rule silently.
`sqlite-containment` catches only a *direct* `database/sql` import, which a brain file reaching for
`sqlite.NewUnitRepo` would not have.

`m2c` is the first change that adds files to `ports`, `store` and `brain` in one milestone, so it
is the right place to close it — and `docs/06-harness.md` §6's own precedence rule says so: *"if a
rule can be an automated gate, it is a gate."* PR 1 adds:

```yaml
        ports-purity:
          files:
            - "**/internal/ports/**"
          allow:
            - $gostd
            - github.com/rengo/nooma/internal/core
            - github.com/rengo/nooma/internal/ports

        brain-boundary:
          files:
            - "**/internal/brain/**"
          deny:
            - pkg: github.com/rengo/nooma/internal/store
              desc: "brain reaches persistence only through a port — docs/06-harness.md §1"
            - pkg: github.com/rengo/nooma/internal/providers
              desc: "brain calls a model only through ports.LLMProvider — docs/06-harness.md §1"
            - pkg: github.com/rengo/nooma/internal/httpapi
              desc: "brain does not know about transport — docs/06-harness.md §1"
            - pkg: github.com/rengo/nooma/internal/channels
              desc: "brain does not know about channels — docs/06-harness.md §1"
```

Verified against the imports these two directories carry today: `internal/ports` imports only
`context`, `encoding/json`, `errors`, `time` and `internal/core/*`, so the allow-list passes as
written; `internal/brain` imports none of the four denied packages. The PR records the
temporary-break run (adding an `internal/store/sqlite` import to a brain file and watching lint
fail) the way `schema_golden_anchor_test.go` records its own.

**What this does not cover:** depguard is import-level. It cannot see a brain file that speaks SQL
through an interface it was handed, and it does not constrain `cmd/nooma`, which legitimately
imports everything.

### 10.2 `m2c` adds one file to `internal/core`, and the Scope boundary says it adds none

Spec's Scope boundary states `m2c` "adds no `internal/core` code at all." That holds for seven of
the eight phases. It fails for `derive`, and the reason is a genuine gap rather than a scope slip.

`derive` needs a prompt built from (a) the units it derives beliefs from and (b) the active
beliefs that are dedup defense 1 (spec R5.6). `internal/core/consolidation` ships no prompt
builder, and `classify.BuildPrompt` sets the precedent that a prompt builder is **core**: it
decides from input data and returns data, which is `nooma-core`'s own placement gate, and putting
it in `brain` would make the one string a judge actually sees untestable at L1.

So `m2c` adds:

```go
// internal/core/consolidation/prompt.go

// DeriveSource is one unit as the derivation judge sees it.
type DeriveSource struct {
    UnitID  string
    Type    unit.Type
    Content string
}

// BuildDerivePrompt renders doc 02 §6.5's derivation prompt, including
// every existing active belief so the judge can decide "this already
// exists" before proposing a new one — dedup defense 1, which §6.5 marks
// "required by this document, not yet wired". When existing is empty the
// prompt says so plainly: the absence of beliefs is informative to the
// judge, not a degenerate case to hide (spec R5.6).
func BuildDerivePrompt(us []DeriveSource, existing []Belief) string
```

and **one field** to an `m2b`-shipped type:

```go
type Belief struct {
    ID               string
    Facet            selfmodel.Facet
    TopicKey         string
    Content          string   // added by m2c: the prompt needs the text;
                              // EvaluateStagnation and MergeProposals ignore it
    Confidence       float64
    LastReinforcedAt time.Time
}
```

No new constant, so **no new §13 row and no `calibration_doc_test.go` interaction**. The prompt's
source cap is `ConnectSourceLimit`, reused (§7.3).

**What this does not cover:** the prompt's *wording* is not fixed by this design. It is an L1
function over two slices, and its output is asserted for content (every belief's `topic_key` and
`content` present; the empty-state sentence present), never byte-for-byte.

### 10.3 `m2c` does write new doc 02 prose — in three places

Spec's Scope boundary states `m2c` "writes **no new doc 02 prose**." Three amendments are owed,
each in the PR that implements it, per non-negotiable #1:

| Section | Amendment | PR |
|---|---|---|
| **§6.5** | One sentence naming `derive`'s source selection: the units a pass derives from are the same recently-touched, effective-weight-ranked, `connect_source_limit`-capped set `connect` uses (§7.3). §13's `connect_source_limit` row is **annotated** to say it governs two phases — the Default column's number is unchanged, so the calibration gate is unaffected | 10b |
| **§7** | The load watcher's `current_state` row is written with `source = 'consolidation'`, `mood = 'loaded'` and `energy` left NULL, and its cooldown is anchored on the previous hypothesis's own `recorded_at` because M2 has no resolution signal (`m2b` §9 Q6, mapped) | 4 |
| **§10** | `current_state` gains `source`; the append-only property is now structural at the port (`StateRepo` has no update path) | 4 |

Doc 03's `current_state` block gains the column in PR 4, and `make schema-golden` regenerates both
golden files there.

**`docs/06-harness.md` is untouched.** I03, I05, I11, I12 and I24 all already have §4 rows (§1),
so `m2c` adds no invariant — it makes five existing ones load-bearing.

---

## 11. Handoffs `m2c` receives, discharges, or passes on

| Handoff | Disposition |
|---|---|
| `m2b` §8 — `ConfigRepo`'s nil-sentinel shape | **Discharged** — §3.4 |
| `m2b` §8 — `UnitRepo` weight write takes a `weight.Boost` | **Discharged** — §3.1 |
| `m2b` §8 — live-count-by-type returns an `int` | **Discharged** — §4.1 |
| `m2b` §8 — `[]Cold`/`[]Source`, never `unit.Unit` | **Discharged** — one `LiveDecayStates` read, §4.1 |
| `m2b` §8 — `IncompleteOlderThan`, not `List(status)` | **Discharged** — §4.1, with the duplicated-predicate risk named and pinned |
| `m2b` §8 — the `RelationEvidence` join and the `map[Pair]bool` exclusion | **Discharged** — §4.2 |
| `m2b` §8 — `SelfModelRepo`'s upsert-by-`topic_key` + `ActiveBeliefs` | **Discharged** — §4.3, with a third method (`ReinforceByID`) the merge case needs |
| `m2b` §8 — belief embeddings in memory, no table | **Discharged** — §6.3 slot 5, no new port or column |
| `m2b` §8 — `brain/capture.go:485` adopts `relation.CreatedBySystem` | **Discharged** — §7.1, PR 9 |
| `m2b` §8 — `current_state`: one append-only row per `LoadFinding` | **Discharged, at the cost of migration 0003** — §3.2 |
| `m2b` §9 Q3 — `goal_stagnation_days`'s two schema homes | **Discharged** — `ConfigRepo` reads `config.goal_stagnation_days`; the `calibration` table stays entirely unused through `m2c` (spec R2.5), and doc 02 §13's row is amended to state the decision rather than leave the question open for M5 |
| `m2b` §9 Q6 — which instant starts the load cooldown | **Discharged** — the previous hypothesis's own `recorded_at`, structurally identified by `source = 'consolidation'`, stated in the `decision_log` row's `context` (§3.2) |
| `m2b` §9 Q8 — `since` read once, one field on the pass context | **Discharged** — §3.3(c) |
| `m2a` C6 — I24 unenforced until the port has a write path | **Discharged** — §3.1(d)'s three legs plus §5.2's single statement |
| `m2a` C17 — `Resurface`'s dead `refused` guard | Already deleted in `m2b` PR 3. Nothing owed |
| `m2a` C19 — `Edge.Strength = +Inf` coerced inside `Resurface` | **Not reached** — `Reweight` refuses non-finite edges before any edge reaches `clampStrength`. Stays open for a future second caller |
| `m2a` C29 — `Displaces` needs a caller to resolve the margin first | **Passed on to M4** — `m2c` wires no focus consumer (§7.4) |
| **New — `SelectConnectSources`'s comparator is not total under `NaN`** | **Passed on to a future core change.** `m2c` guards at the `brain` boundary (§8.1); the real fix is a `corrupted` return on the core function and belongs in `internal/core/consolidation` |
| **New — `SelectConnectSources` now serves two phases and its name says one** | **Passed on.** A rename to `SelectRecentSources` is a core change |
| **New — `recall.Search`'s comparator is not total under `NaN`, reachable since M1 through `RecallService.ScoredFor`** | **Passed on, not introduced by `m2c`.** `connect`'s reuse of `ScoredFor` (§7.1, §8.1) is a second caller, not a new hazard, and `m2c` adds no guard at that call site. The fix belongs in `internal/core/recall` and would protect every existing M1 caller of `ScoredFor` too |

---

## 12. Open questions this design could not close

**Q1 — migration 0003, for one column on `current_state`.** §3.2 recommends it and prices the four
alternatives. The owner may prefer **C** (no write, cooldown anchored on `decision_log`), which
costs no migration but makes the audit log operational state and needs **ADR-0018** plus a doc 02
retreat. **This is the decision most worth overturning before apply, and the one this design most
wants looked at.**

**Q2 — `connect` logs no `decision_log` row for a judgement that decided nothing, and capture
does.** §7.1's argument is volume: 1 user-visible judgement versus up to 100 machine-initiated
ones a night. The counter-argument is that "why did the pass not connect these two?" is a question
the glass box exists to answer. **Recommendation: no row**, revisit when the judge's accept rate is
observable.

**Q3 — `consolidation_enabled` gates the scheduler, never the explicit CLI invocation** (§7.4).
The alternative reading — a global off switch that also silences the command — is defensible and
this design rejects it, because a command that silently does nothing is the worst answer to "why
did nothing happen". Recorded so `m2d` inherits a decision rather than re-asking.

**Q4 — the `NaN` guard on `[]Source` lives in `brain`, not in `core`** (§8.1). This is the
pragmatic answer to a real gap `m2c` cannot fix without changing an `m2b`-shipped signature. If
the owner would rather fix it at the source, that is one extra core PR (a `corrupted` second
return on `SelectConnectSources`, plus its L1 table) and it should land **before** PR 9, not after.

**Q5 — `derive`'s source selection reuses `connect`'s knob** (§7.3). `ConnectSourceLimit` = 20 now
bounds two phases, so the per-night provider budget doc 02 §6.4 states as a *product*
(20 × 5 = 100 judge calls) is joined by 20 derivation calls the section does not mention. The
alternative is a separate `DeriveSourceLimit` constant with its own §13 row —
`ConnectCandidateK`'s own precedent against collapsing two knobs because they start equal.
**Recommendation: reuse for M2**, split when the two phases' costs are measurable.

**Q6 — `nooma consolidate` refuses rather than degrades when a task is unbound** (§7.2). This
diverges from `serve`'s posture on purpose. The consequence: a vault with no providers configured
cannot run *any* phase, including the four that need no provider at all. A finer-grained refusal
(run the provider-free phases, refuse the whole-pass timestamp) is possible and is more code.
**Recommendation: refuse the whole command**, because a partial pass whose partiality is invisible
is the failure mode R5.4 exists to prevent.

---

## 13. Review Workload Forecast

Estimates are **implementation plus docs, separately from test lines**, per `docs/06-harness.md`
§7. **They are guesses**, of the same kind this project has measured wrong 1.3×–4.3× seven times.
The claim made with confidence is the *shape* of the chain and the pre-drawn split lines, not the
sizes.

**Chained PRs: required. Yes.** Fourteen links, and the proposal's own six-PR row is the number
this design supersedes (spec's Scope boundary grants `sdd-design` that authority explicitly).

| # | Branch | Contents | Impl + docs | Tests (est.) |
|---|---|---|---|---|
| **1** | `feat/ports-unit-weight-count` | `ApplyBoosts`, `CountLiveByType`, `ErrNonFiniteWeight`; **the two depguard rules** (§10.1); I24's two reflection legs; memrepo + repocontract | ~170 | ~460 |
| **2** | `feat/ports-unit-relation-reads` | `IncompleteOlderThan`, `LiveDecayStates`, `Evidence`, `ExistingPairs`; memrepo + repocontract | ~150 | ~400 |
| **3** | `feat/ports-selfmodel-config-state` | `SelfModelRepo` (3), `ConfigRepo` (2), `StateRepo` (2), `ErrBeliefNotFound`, the three state literals; **I03's widened sweep** | ~240 | ~470 |
| **4** | `feat/schema-current-state-source` | **migration 0003**; doc 03's `current_state` block; doc 02 §7 + §10; `make schema-golden`; the five `wantVersion` edits; the literals' DDL pin | ~80 | ~90 |
| **5** | `feat/store-unit-relation-repos` | sqlite for PRs 1–2's six methods; `store_api.golden`; **I05's structural half** | ~330 | ~520 |
| **6** | `feat/store-selfmodel-config-state` | sqlite for PR 3's seven methods; `store_api.golden` | ~300 | ~450 |
| **7a** | `feat/brain-consolidate-runner` | `ConsolidateService`, `ConsolidateRequest`, `consolidateRunner`, `passContext`, `ConsolidateReport`, the `Order()` loop and `runPhase`'s `switch`; **I11 behavioural**; the `since` and `last_run_at` tests | ~230 | ~430 |
| **7b** | `feat/brain-consolidate-decision-log` | the 10 `DecisionAction` members (§7.5), `record`, the `corrupted`-never-logged rule, R4.3's conflict skip; **I12 both directions + the exclusion** | ~190 | ~430 |
| **8** | `feat/brain-phase-io-transitions` | slots 1–3: `expire_incomplete`, `archive`, `strengthen` — repo-only, no provider | ~200 | ~400 |
| **9** | `feat/brain-phase-io-connect` | slot 4: the `ScoredUnit → FusedCandidate` adapter shared with `correction.go`, `ExistingPairs`, the judge call, `ProposeRelation` → `Upsert`; `capture.go:485`'s one-line adoption; §8.1's `Source` guard | ~300 | ~500 |
| **10a** | `feat/core-derive-prompt` | `internal/core/consolidation/prompt.go`, `Belief.Content` (§10.2) | ~150 | ~300 |
| **10b** | `feat/brain-phase-io-derive` | slot 5: `ActiveBeliefs` → prompt → `belief_derivation` → embeddings → `MergeProposals` → the two writes; doc 02 §6.5 + §13's annotation | ~250 | ~430 |
| **11** | `feat/brain-phase-io-reweight-patterns` | slots 6–8: `Reweight` → `ApplyBoosts`; `EvaluateStagnation`; `EvaluateLoad` → `OpenHypothesis` + the `lastHypothesisAt` context; `learn`'s no-op arm | ~250 | ~450 |
| **12** | `feat/cli-consolidate` | `cmd/nooma/consolidate.go`, `--phase` via `ParsePhase`, `vaultlock`, `tasksConsolidateConsumes`, report rendering; the four L4 tests | ~230 | ~380 |

**Total: ~3,070 implementation + docs, ~5,710 test lines, across 14 links.** (Summed directly from
the fourteen rows above: 170+150+240+80+330+300+230+190+200+300+150+250+250+230 = 3,070; the test
column's own sum, 460+400+470+90+520+450+430+430+400+500+300+430+450+380 = 5,710, already matched
the stated total.) The proposal budgeted six PRs at ~2,050 under the *old total-line* convention,
so the two numbers are not comparable; what is comparable is that this design predicts **2.3× the
PR count** (14 ÷ 6) the proposal sketched. Checked against `docs/06-harness.md` §7's 400-line
impl+docs ceiling, every one of the fourteen links stays under it — PR 5 is the tallest at ~330
(0.83× the ceiling, discussed below) — so the correction to the total changes no PR's own estimate
and flags no additional link.

**Where PR 5 sits**, since it is the tallest: ~330 is 0.83× the ceiling, above every measured
`m2a` link, 3a (319, 0.80×) included — 3a is the closest of them, not an exception to the
comparison. `m2a`'s own history says the impl+docs column is the one that
grows — `spread.go` went 183 → 371 lines (~103%) across three hardening rounds. So:

**Split lines drawn in advance**, so a crossing is never an unplanned decision:

- **PR 5 splits at `unitrepo.go` | `relationrepo.go`** — 5a ~210 / 5b ~120, two files, two
  independent integration suites.
- **PR 6 splits at `configrepo.go` + `staterepo.go` | `selfmodelrepo.go`** — 6a ~150 / 6b ~150.
  `SelfModelRepo` is the largest of the three (an eleven-field row) and travels alone.
- **PR 9 splits at the adapter | the judge** — 9a is `ScoredUnit → FusedCandidate` + `ExistingPairs`
  + `SelectConnectSources` wiring + §8.1's guard (~160); 9b is the judge call, `ProposeRelation`,
  `Upsert` and `capture.go:485` (~140).
- **PR 11 splits at `reweight` | `pattern_eval`** — 11a ~110 / 11b ~140. `learn`'s no-op arm
  travels with 11b, since it is the arm that must be *reached* and asserted last.

**PR 7's split is not conditional — it is drawn as 7a/7b above**, because the runner and the
`decision_log` vocabulary are two reviewable units (a control-flow decision and a fourteen-to-
twenty-four-member vocabulary widening, §7.5) and reviewing them together is how the vocabulary
gets approved by association.

**Test-first ordering.** `scripts/pending-red.sh` is retired, so every PR that satisfies an
invariant puts the conformance test in its own commit **ahead** of the implementation commit, with
a stub carrying the final signature returning zero values so the suite compiles and the assertion
— not the compiler — is what fails. `sdd-tasks` encodes this as ordered items per PR, never as a
single "implement X with tests".

**Review budget.** `m2a` C36 binds: two Judgment Day rounds per PR is the ceiling. The three places
this design spends effort in advance are the three that have cost this repository a CRITICAL each:
every externally-sourced float validated at its entry point and tabulated (§8); every comparison
that could go non-total under `NaN` named, including the two that are **not** fixed (§8.1); and
every ordering guarantee fixtured with ≥3 entries so removing the sort fails the test (§9).

---

## 14. What this design does not decide

- **The scheduler.** `internal/scheduler`'s cron at 03:00, ADR-0009's boot catch-up, `serve`
  wiring and shutdown ordering are all `m2d`. `m2c` writes the column the catch-up reads and gives
  it a `ConsolidateService` with one entry point; it starts nothing on a timer.
- **`consolidation_enabled`'s consumer.** §7.4 decides it gates the scheduler; the gate itself is
  `m2d`'s code.
- **The simulated-weeks demo golden set and its L4 test.** `m2d`. PR 12's exit criterion is a
  small hand-built fixture vault, deliberately not the demo corpus.
- **Any delivery.** No digest, no push, no `interrupt_level`. `pattern_eval` writes a hypothesis
  and a finding; nothing carries either. M3.
- **Any focus consumer.** M4 (§7.4).
- **The learning module.** `learn` is slot eight's reached-and-empty arm until M5, and the
  `calibration` table stays entirely unwritten (spec R2.5).
- **I06.** No producer of `incomplete` units — out of scope honestly, per ruling Q3, and R5.1
  proves the phase's read is empty on every real M2 vault rather than assuming it.
- **The derive prompt's wording** (§10.2) and the `decision_log` rationale sentences' wording. Both
  are asserted for content, never byte-for-byte.
- **Paging any of the two unbounded reads** (§4.1). Named as a risk; the fix would put a bound in
  SQL that core's own decision functions do not have.
