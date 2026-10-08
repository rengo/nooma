# Design — m4e2: admin

Technical design for `m4e2-admin`, the slice split off `m4e-beliefs-activity-admin` on
2026-10-08. Requirements are [`spec.md`](spec.md) (R7-R11, owner rulings 2026-10-07). This
document decides HOW. Shape follows the archived
[`m4c` design](../archive/2026-10-07-m4c-focus-hysteresis/design.md) and
[`m4b` design](../archive/2026-09-29-m4b-units-capture/design.md).

> **Split from `m4e-beliefs-activity-admin` on 2026-10-08 by the umbrella rule** (proposal.md
> ~:301, more than seven PRs or more than 2,400 budgeted lines). m4e's design had admin as a
> clean tail (its old PRs 6-7). Splitting m4e's activity PR into "newest-first read" and "activity
> view" made eight implementation PRs in one slice, so the rule fired on measurement (owner
> decision). The sections below are the old m4e §3.7, §3.8, the admin rows of §3.9, gates G4 and
> G5, the admin mutants (C-series) and the old PRs 6-7, with the amendments of the 2026-10-08
> judgment round marked.
> Companion: [`../archive/2026-10-08-m4e-beliefs/design.md`](../archive/2026-10-08-m4e-beliefs/design.md).
>
> **Depends on `m4e-activity` and on m4e (starts after `m4e-activity`'s PR 6 merges, which itself
> follows m4e's PR 4).** Re-pointed on 2026-10-08, when activity split off m4e (the owner's 7-PR rule, after m4e PR 2
> measured 2.06x): `m4e-activity` now owns what the old m4e PRs 5-6 built.
>
> From [`m4e-activity`](../m4e-activity/design.md):
> - `DecisionLog.Before`, `DecisionCursor`, `DecisionRow` (m4e-activity PR 5), used for the
>   recent consolidation effects;
> - the **change decoder** in `ActivityService` (m4e-activity §3.6), which must already accept
>   `{fields, previous, next}` (m4e-activity's A11 test carries a `config.updated`-shaped row).
>
> From [m4e](../archive/2026-10-08-m4e-beliefs/design.md):
> - the action-vocabulary file and its `repocontract` map as m4e leaves them (fifty-two members,
>   after m4e PR 3; m4e-activity adds none); this slice adds the fifty-third, `config.updated`;
> - `brain.ErrWriteLanded` and `*brain.WriteLandedError{Record, Signal}` (m4e PR 3, `internal/brain/write_landed.go`);
> - the cross-origin gate's **body table** `uiCrossOriginBodies` and its coverage test (m4e G6, PR 4);
> - the `ui.Deps` / `uiDeps` / `wiring.go` pattern, the layout nav, and the I22 whitelist test
>   `ui_entrances_test.go:32` (m4e G12, PR 4). `uiDeps` takes its third signature change here,
>   after m4e's PR 4 (`beliefs`) and m4e-activity's PR 6 (`activity`).
>
> **Planning-PR task (recorded, not done here):** the umbrella's slicing paragraph, §5.1 rows
> and dependency rows gain `m4e2-admin` (after m4e); the umbrella is not edited now. The complete
> list of umbrella spots that still put admin inside m4e (acceptance line ~:75, §5.2 rows 9-10,
> the Q5 header "blocking m4e #5", the independence paragraph listing `ConfigRepo` under m4e, and
> more) is in the header of [`../archive/2026-10-08-m4e-beliefs/design.md`](../archive/2026-10-08-m4e-beliefs/design.md).

> **Findings this design made that the spec did not anticipate:**
> 1. **R8 needs a read on a fourth port.** Relation type is open text
>    (`internal/ports/relationrepo.go:76-82`), and `ThresholdsFor(type)` is the only threshold
>    read. Nothing can enumerate learned rows, so the spec's "GIVEN a learned threshold row" is
>    unreachable without `RelationRepo.LearnedThresholds`. m4e's scope line (three ports) was short
>    by one; this slice's spec states the fourth.
> 2. **One `Now()` per file in `internal/brain`** (`brain_single_clock_read_test.go:23-29`). Five
>    config setters cannot each read the clock. They share one door that reads it once (§3.1).
> 3. **`memrepo.Config` and SQLite disagree on a lazily created row.** The fake leaves the other
>    fields nil (`test/support/memrepo/config.go:55-64`), while SQLite fills in SQL DEFAULTs. The
>    "other columns take SQL DEFAULTs" assertion is therefore L3-only, the same as for
>    `RecordConsolidationRun`.
> 4. **There is no run-level `decision_log` row to show as "job status".** A full pass writes
>    `config.consolidation_last_run_at` and nothing else at run level (`consolidate.go:1107`);
>    every `consolidate.*` action is a per-effect row. The first design's "10 recent job rows"
>    would have been ten arbitrary effect rows from one pass, labelled as jobs. Job status is
>    defined honestly in §3.2.
> 5. **The G5 setter-name gate would collide with an `AdminService` method named like a port
>    setter.** The service exposes `Update`, not `Set…` (§3.5).
> 6. **`absent row` is not `changed`.** Comparing against the stored pointer made "submit the
>    default on a vault with no config row" write a row and a log entry for a change that is not
>    one. The comparison is against the effective value (§3.1).

---

## 1. Ground truth (read at `dc22762`)

| Claim | Verified at |
|---|---|
| `ConfigRepo` = `Load` + `RecordConsolidationRun`. The latter's lazy UPSERT names only its own column | `internal/ports/configrepo.go:25-48`; `internal/store/sqlite/configrepo.go:106-119` |
| `config` columns are all `NOT NULL DEFAULT` (0.5 / 0.05 / 1 / 21 / 7); `updated_at` is `NOT NULL` with no default | `internal/store/sqlite/migrations/0002:61-70` |
| The `Resolve*` acceptance sets: weight finite in `[0, WeightCeiling=2.0]`; margin finite `≥ 0`; days and load `≥ 1`; enabled any bool (absent → true) | `consolidation/archive.go:74-83`; `focus/hysteresis.go:79-88`; `consolidation/patterns.go:122-136`; `consolidation/schedule.go:46-51`; `weight/boost.go:19` |
| `FocusKeeper.compute` reads the margin through `cfg.Load` on every computation | `internal/brain/focuskeeper.go:73-77` |
| Test doubles that implement the widened ports: `errConfigRepo` is a value type with `Load` and `RecordConsolidationRun` only (it stops compiling); `i27Config` embeds `*memrepo.Config` and fails only `RecordConsolidationRun` (new setters would be **promoted silently, not failing**); `confirmingRelations`, `failingRelations`, `countingRelations` embed a nil `ports.RelationRepo` (they compile, and panic if `LearnedThresholds` is reached) | `internal/scheduler/cron_test.go:23`; `test/conformance/i27_viewing_is_not_delivering_test.go:236`; `internal/brain/checkin_test.go:383` (via `deletingRelations`, `:207`), `focuskeeper_test.go:262`, `carry_adjacency_test.go:341` |
| `ConfigRepo` is held by `FocusKeeper`, `TodayService` and `ConsolidateService`: widening the port widens what they could call | `focuskeeper.go:22`, `today.go:34`, `consolidate.go:61` |
| The stale comment "no reader in m2c" for `ConsolidationEnabled` | `internal/ports/configrepo.go:17`; the readers are `internal/scheduler/catchup.go:36`, `scheduler.go:247` |
| Relation type is open text; `ThresholdsFor` is the only threshold read; `relation.Resolve(nil)` is the default pair | `internal/ports/relationrepo.go:76-82`; `internal/core/relation` |
| No code writes `relation_thresholds`. `memrepo.Relations.SeedThreshold` is the fixture hook | `internal/store/sqlite/relationrepo.go:105-111`; `test/support/memrepo/relations.go:65` |
| Consolidation writes no run-level `decision_log` row; all `consolidate.*` actions are per-effect | `internal/ports/decisionlog.go:109-149`; `consolidate.go:1107` |
| `BrowsePageSize = 50` is a **transport constant**, explicitly not a §13 row | `internal/ports/unitrepo.go:254-257` |
| The I22 whitelist | `test/conformance/ui_entrances_test.go:32` |
| `uiDeps`, `wireUnits`, `wiring_units_test.go` precedents | `cmd/nooma/serve.go:285-300`; `cmd/nooma/wiring.go:347`; `cmd/nooma/wiring_units_test.go:14` |

**New calibratable constants: none** (`AdminRecentEffectRows` is a transport constant, §3.2).
**New migration: none.** **New ADR: none.** Doc 02 §11 gains one clause (§3.4).

---

## 2. What m4e2 decides, in one paragraph

Admin writes go through five typed `ConfigRepo` setters (m2c's one-method-per-field discipline).
They are reachable only inside one brain door, `AdminService.Update(ctx, field, raw)`, which
parses the raw form string, validates with the `Resolve*` functions themselves (so the form and
the runtime cannot drift), judges "unchanged" against the **effective** value, writes, and only
then records one `config.updated` row shaped like `belief.edited` so the activity view renders it
unchanged. A write that fails logs nothing; a log that fails after a landed write is visible, not
silent. Admin reads show the learned thresholds read-only, the last full consolidation, the
effective `consolidation_enabled`, and the most recent consolidation **effects**, labelled as
effects.

---

## 3. Decisions

### 3.1 Admin writes (R7, R10)

**Port: exactly five setters, one per Q5 field** (m2c's discipline), each the lazy UPSERT
`RecordConsolidationRun` already uses: `INSERT INTO config (id, <col>, updated_at) VALUES (1, ?,
?) ON CONFLICT(id) DO UPDATE SET <col> = excluded.<col>, updated_at = excluded.updated_at`.

```go
SetWeightThreshold(ctx context.Context, v float64, at time.Time) error
SetHysteresisMargin(ctx context.Context, v float64, at time.Time) error
SetGoalStagnationDays(ctx context.Context, v int, at time.Time) error
SetMentalLoadThreshold(ctx context.Context, v int, at time.Time) error
SetConsolidationEnabled(ctx context.Context, v bool, at time.Time) error
```

**Ranges — the accepted set is exactly the set `Resolve*` returns unchanged.** Brain accepts `v`
iff `Resolve(&v) == v`. No range is restated anywhere, so the form and the runtime cannot drift.
Exact equality is sound because every fallback is itself a valid value, and an invalid input can
never equal a valid one (and `NaN != NaN`).

| Field | Parse (brain, from the raw form string) | Accepted (via) | Rejected examples |
|---|---|---|---|
| `weight_threshold` | `strconv.ParseFloat` | finite, `0 ≤ v ≤ 2.0` (`ResolveWeightThreshold`) | `-0.1`, `2.0000001`, `NaN`, `Inf` |
| `hysteresis_margin` | `ParseFloat` | finite, `v ≥ 0` (`ResolveMargin`); `0` is legal and means no hysteresis | `-0.01`, `NaN`, `+Inf` |
| `goal_stagnation_days` | `strconv.Atoi` | `v ≥ 1` (`ResolveGoalStagnationDays`). No upper bound: `EvaluateStagnation` compares floats, so it cannot overflow (`patterns.go:58`) | `0`, `-3`, `2.5` |
| `mental_load_threshold` | `Atoi` | `v ≥ 1` (`ResolveMentalLoadThreshold`) | `0` |
| `consolidation_enabled` | exactly `"true"` / `"false"` (not `ParseBool`'s eight spellings) | any bool | `"1"`, `"yes"` |

**The service surface (amendment, finding 5).** `brain.AdminService` exposes exactly two
exported methods: `View(ctx)` and `Update(ctx, field ConfigField, raw string) (UpdateResult,
error)`. `ConfigField` is a closed type with `ParseConfigField(string) (ConfigField, error)`
(unknown → `ErrUnknownConfigField`, including `relation_thresholds` and
`consolidation_last_run_at`). Parsing lives in brain so it is testable without HTTP; the five
per-field appliers are **unexported** and named `apply…` (never `Set…`), so no service method
shares a name with a port setter.

**The door.** `internal/brain/admin_write.go` holds `write(ctx, field ConfigField, previous,
next any, apply func(at time.Time) error) error`. It is the file's single `s.clock.Now()`.

**"Unchanged" is judged against the effective value (amendment, fix 11).** For each field,
`stored` is the `*T` from `cfg.Load`, `effective := Resolve(stored)`, and the submission is a
no-op iff `effective == v && (stored == nil || *stored == v)`. Consequences:

| State | Submission | Result |
|---|---|---|
| no config row (`stored == nil`), default in force | the default | **no-op**: no row created, nothing logged |
| no config row | a non-default value | write (creates the row with SQL DEFAULTs elsewhere), log with `previous` = the default that was in force |
| stored `21` | `21` | no-op |
| stored corrupt (non-nil, `Resolve(stored) != *stored`, e.g. margin `-5`) | the default | **write and log**: submitting the value in force would otherwise leave the column corrupt forever |

**Write order (amendment, fix 6): write first, then record.** The door calls `apply(now)` first,
then `Record(config.updated)` with the same `now`. `Load` already holds the previous value, so
nothing is lost by writing first, and a rejected or failed write logs nothing (a row would assert
a change that did not happen). This is the **opposite** of the belief edit (record-first,
ADR-0016) on purpose: an edit overwrites text that exists nowhere else; a config write replaces
a number the caller already holds. Failure windows, stated:

- (a) validation fails or `apply` errors: nothing written, nothing logged, the form shows the error.
- (b) `apply` lands, `Record` fails: the value is in force with no row. `Update` returns an error
  wrapping `brain.ErrWriteLanded` (m4e's `*WriteLandedError{Record: true}`: only the log row is
  missing); the UI shows "saved, but not logged" and `slog.Warn`s the cause. The value is **not**
  rolled back.
- (c) a crash between `apply` and `Record`: as (b), without the message. The lost datum is the
  previous value, an ordinary number the user can re-enter.
- (d) two concurrent POSTs: both `Load` the same previous and the last writer wins; two rows may
  carry the same `previous`. Single-user local app; accepted.

**The log row (amendment, fix 1).** Shaped like `belief.edited` so m4e-activity's decoder renders it:
`{fields:["<column>"], previous:{"<column>": <v>}, next:{"<column>": <v>}}`, `previous` being the
value **in force** (the effective value when the column was unset). The rationale says what
happened and notes when the previous value was the default or an invalid stored value
(doc 02 §11: a rationale states what happened, never what was configured). Booleans and numbers
are JSON booleans and numbers. **Cross-slice dependency:** m4e-activity's decoder is shape-driven and its A11
test already carries a `config.updated` row; this slice adds C15, which feeds the row `Update`
actually wrote through m4e-activity's `ActivityService`.

**R10** needs no code. The keeper re-reads the margin per computation. One test pins it (§6, C13).

### 3.2 Admin read (R8)

`brain.AdminService.View(ctx) (AdminView, error)` reads `cfg.Load` (stored and effective value per
field, `consolidation_last_run_at` nil rendered as **never**),
`RelationRepo.LearnedThresholds` (new: every `relation_thresholds` row ordered by
`relation_type`, as `[]ports.LearnedThreshold{RelationType string; relation.Thresholds}`), the
default pair `relation.Resolve(nil)` labelled "every other type", and the recent consolidation
effects. Thresholds render as text with the note "learned by the nightly learning pass (M5); not
editable here". No input names them.

**Job status, defined honestly (amendment, fix 3, finding 4).** No run-level row exists, so the
page does **not** present `decision_log` rows as "jobs". It shows three things, each labelled for
what it is:

1. **Last full consolidation** = `config.consolidation_last_run_at` (nil → *never*). This is the
   authoritative "did the job run", written once per full pass (`consolidate.go:1107`).
2. **Consolidation enabled** = `ResolveConsolidationEnabled(stored)`.
3. **Recent consolidation effects** = `DecisionLog.Before(nil, "consolidate.", AdminRecentEffectRows)`
   (m4e-activity's newest-first read), titled "Recent effects of the nightly pass", newest first, each
   with its action and rationale. `brain.AdminRecentEffectRows = 10` is a transport constant of
   the `ActivityPageSize` class (shapes a list, decides nothing doc 02 governs).

A pass that decides nothing leaves no rows, and the page says so: the effects list is evidence of
activity, never of liveness. The `consolidate.` prefix is exact for this vocabulary (every
consolidation action begins with it; checked against `decisionlog.go:109-149`).

| Option for the "logs"/job status of doc 01 | Verdict |
|---|---|
| **Last run from `config` + effective flag + recent `consolidate.` effects, labelled as effects** — chosen | Uses only what exists; no invented run record |
| Add a run-level `consolidate.run.completed` action written by the pass | Rejected here. It changes the consolidation pass and its action vocabulary for a display need; if the owner wants run history it is its own change |
| Show the 10 newest `decision_log` rows of any kind | Rejected. Not job status |

| Option for R8's per-type rows | Verdict |
|---|---|
| **`RelationRepo.LearnedThresholds`** — chosen | One read with a real caller. `RelationRepo` is already outside I03's sweep only for `Delete`; a read changes nothing there |
| Distinct relation types from `relations`, then `ThresholdsFor` each | Rejected. Still a new read, plus N round trips, and it lists types that have no learned row |
| Show defaults only | Rejected. The R8 scenario ("a learned threshold row") would be unobservable |

### 3.3 UI routes and wiring

| Pattern | Guarded | Brain call | Errors |
|---|---|---|---|
| `GET /ui/admin` | yes | `View` | 503 nil dep; 500 |
| `POST /ui/admin` | yes | `field` → `ParseConfigField`, then `Update(field, value)` | 400 unknown field (incl. `relation_thresholds`, `consolidation_last_run_at`) **with zero brain write calls**; 400 parse/range with the form re-rendered and its error shown; success on `ErrWriteLanded` with the "not logged" notice |

One `POST /ui/admin` route with a closed `field` switch, rather than five routes. The closed set
is enforced at the **port** (G4) and at `ParseConfigField`. Five routes would add five guard rows
and five cross-origin subtests and close no extra hole. The POST answers with an HTMX fragment on
`HX-Request` and the full page otherwise (m4b's split), bounded by `MaxBytesReader` at 64 KiB.
`ui.Deps` gains `Admin`, a narrow interface (`View`, `Update`). The layout nav gains one link.

**Wiring (`cmd/nooma`).** `wiring.go` gains `wireAdmin(db) *brain.AdminService` (`systemClock{}`,
`sqlite.NewConfigRepo`, `NewRelationRepo`, `NewDecisionLog`), provider-free and unconditional at
vault open, `wireUnits`'s precedent. `uiDeps` gains an `admin *brain.AdminService` parameter with
the same typed-nil guard (a third signature change after m4e's two), and `serve.go`'s one call site
passes it. `wiring_admin_test.go` (precedent `wiring_units_test.go`) builds the service over a real
empty migrated vault and asserts `View` answers (never, enabled, no learned rows);
`TestUIDeps_NilServicesStayNilInterfaces` gains an `Admin` case.

### 3.4 Vocabulary and docs

One action is added: `ActionConfigUpdated` (`config.updated`), appended last, with the two hand
edits (the `AllDecisionActions` comment count at `internal/ports/decisionlog.go:169`, fifty-two →
fifty-three, and `test/support/repocontract/decisionlog.go:133`'s `want` map and subtest title).
`ActivityFamilies` (m4e-activity) then yields the `config` family with no edit.

| Doc | Edit | PR |
|---|---|---|
| doc 02 §11 (`:1237-1240`) | adds to m4e's belief sentence: "a config change" is recorded too, with the value it replaced | 1b |
| `docs/06-harness.md` §4 | I12 row names `config.updated`; the I22 row names `View`, `Update` | 1a (I12), 2 (I22) |
| `internal/ports/configrepo.go:17` | the stale comment "no reader in m2c" becomes `internal/scheduler` (`catchup.go:36`, `scheduler.go:247`) | 1a |
| `docs/01-architecture.md:126` | none: the row ("system config, job status, thresholds, logs") is satisfied as §3.2 defines it | — |

### 3.5 Structural gates for invisible properties

| # | Property | Gate | Fires on (probe) | Silent on |
|---|---|---|---|---|
| G4 | No generic config writer; exactly Q5's five | `TestConfigRepo_ExposesExactlyTheQ5Setters` (reflection): the method set is `{Load, RecordConsolidationRun}` ∪ the five, and each setter is `(context.Context, float64\|int\|bool, time.Time) error` | `SetConfig(map…)`, `SetRelationThreshold`, `SetConsolidationLastRunAt`, a `string` key param | the five |
| G5 | No config write without its log row | `TestConfigSettersAreCalledOnlyInsideTheLoggedDoor` (AST, non-test `internal/**` and `cmd/**`, excluding `internal/store/**`): every call to a selector named one of the five port setters sits inside a `FuncLit` passed to a call whose selector is `write`, in `internal/brain/admin*.go`. **No collision (finding 5):** `AdminService` exposes `View` and `Update`, its appliers are unexported `apply…`, and `TestAdminServiceExportsOnlyViewAndUpdate` (reflection) pins the exported set so a `Set…` method cannot appear later. Scoping the AST gate to the `ports.ConfigRepo` receiver would need `go/types`; the naming rule makes that unnecessary. Ordering inside `write` (apply, then record) is pinned behaviourally (C7) | a direct `s.cfg.SetHysteresisMargin(...)` in a service or in `cmd/nooma`; an exported `Set…` on the service | the five closures |
| G6 | The admin POST is guarded and cross-origin protected (R9), **proven with a valid body** | (`POST /ui/login` stays in m4e's explicit exemption map; this slice adds no exemption.) A row in `wantUIMuxWiring`; `Admin` gets a counting stub on m4e's shared counter; `uiCrossOriginBodies["POST /ui/admin"] = "field=weight_threshold&value=0.6"` (a body that passes `ParseConfigField` and the range check, so the stub is reached); m4e's `TestUICrossOriginBodiesCoverEveryPOSTRow` fails if the entry is missing. `TestUIGuardedLeavesEachReachAView` (`internal/httpapi/server_test.go:117`, a hard-coded leaf list over a stub `ui.Deps`, not `test/conformance`) gains the `/ui/admin` leaf, an `ADMIN` marker and a stub `Admin` | the POST unguarded; no body entry; a body the handler rejects (0 calls) | the wiring in §3.3 |
| G7 | The admin GET writes nothing (R8) | m4e's `TestUIReadViewsWriteNothing` gains the admin GET over write-counting decorators of `ConfigRepo`, `RelationRepo` and `DecisionLog` → zero write calls | a GET that records or seeds config | — |
| G11 | Store surface widening is reviewed | `testdata/schema/store_api.golden` regenerated in PR 1a (`make store-api-golden`) | an unreviewed method | — |
| G12 | The I22 entrance whitelist names exactly what this slice adds | `ui_entrances_test.go:32`'s `allowedMethods` gains `View` and `Update` in PR 2 (the `Admin` interface). Already present after m4e and m4e-activity: `Today, Browse, Detail, ForText, Capture, ByFacet, Edit, Retire, Page`. The flat-set decision is m4e's G12 | a `ui.Deps` interface exposing an unlisted method (red before the whitelist edit) | the existing names |

Each GREEN commit body records its probe (mutation applied, red output, reverted).

---

## 4. Data flow

```
GET  /ui/admin ─▶ View ─▶ cfg.Load + RelationRepo.LearnedThresholds + relation.Resolve(nil)
                          + DecisionLog.Before(nil, "consolidate.", 10)  ─▶ page (thresholds read-only)
POST /ui/admin field,value ─▶ xo ─▶ requireCookie ─▶ ParseConfigField ─▶ Update(field, raw):
        parse ─▶ Resolve check ─▶ unchanged vs effective? ─▶ write(): Now ─▶ apply(cfg.SetX) ─▶ Record(config.updated)
                       (reject / unchanged: nothing written, nothing logged;  Record fails after apply: ErrWriteLanded)
GET /ui (Today) ─▶ FocusKeeper.compute ─▶ cfg.Load ─▶ ResolveMargin   (R10: the next request sees the write)
```

## 5. File changes

| File | Action | PR |
|---|---|---|
| `internal/ports/configrepo.go`, `relationrepo.go` | 5 setters; `LearnedThresholds` + `LearnedThreshold`; the `:17` comment | 1a |
| `internal/store/sqlite/configrepo.go`, `relationrepo.go` | the setters (lazy UPSERT) and the read | 1a |
| `test/support/memrepo/config.go`, `relations.go`; `test/support/repocontract/configrepo.go`, `relationrepo.go` | fakes + contract | 1a |
| `internal/scheduler/cron_test.go:23` (`errConfigRepo`) | **Ripple, compile break:** a value-type `ports.ConfigRepo` double with `Load` and `RecordConsolidationRun` only. Add the five setters (returning nil: the cron tests never write config) | 1a |
| `test/conformance/i27_viewing_is_not_delivering_test.go:236` (`i27Config`) | **Ripple, silent hole:** it embeds `*memrepo.Config`, so the five new setters are *promoted and succeed* unless overridden. Add the five overrides, each calling `i27Fail(g.t, "ConfigRepo", "<Setter>")` and **failing, not returning nil**; update the comment at `:25-26` ("ConfigRepo's one write method" -> six) and the header count at `:38-39` ("eighteen distinct method names" -> twenty-three; the five names are all new) and its ConfigRepo read/write list at `:36-37`, and add `LearnedThresholds` to the RelationRepo reads. `i27Relations` needs no override (a read) | 1a |
| `internal/brain/checkin_test.go:383` (`confirmingRelations`) | **Ripple, latent panic:** reaches `ports.RelationRepo` only through `deletingRelations`' embedded nil interface (`:207-208`), so it compiles and would nil-panic if `LearnedThresholds` were called. Add an explicit `LearnedThresholds` returning an error so a stray call fails loudly. `failingRelations` (`focuskeeper_test.go:262`) and `countingRelations` (`carry_adjacency_test.go:341`) follow the same embed pattern and need no edit | 1a |
| `internal/ports/decisionlog.go` + repocontract map | `ActionConfigUpdated` (+1) | 1a |
| `test/conformance/config_repo_shape_test.go` | G4 | 1a |
| `internal/brain/admin.go`, `admin_field.go`, `admin_write.go` | `View`, `ConfigField`/`ParseConfigField`, `Update`, parse/range, `*ConfigRangeError`, the door | 1b |
| `test/conformance/config_door_test.go` | G5 (+ the exported-set pin) | 1b |
| `internal/ui/admin.go`, `admin.templ`, `ui.go`, `layout.templ`; `internal/httpapi/server.go` | routes, view, deps, nav | 2 |
| `cmd/nooma/wiring.go` (`wireAdmin`), `wiring_admin_test.go`, `serve.go`, `serve_test.go` | constructor, test, `uiDeps` signature | 2 |
| `internal/httpapi/server_test.go:117` (`TestUIGuardedLeavesEachReachAView`) | `/ui/admin` leaf, `ADMIN` marker, stub `Admin` | 2 |
| `test/conformance/ui_entrances_test.go`, `httpapi_ui_wiring_test.go`, `ui_cross_origin_test.go`, `ui_read_views_write_nothing_test.go` | G12, G6, G7 | 2 |
| docs | §3.4 | 1a (`configrepo.go:17`), 1b (doc 02 §11 clause), 2 |

## 6. Testing strategy and mutation targets

Levels: L1 brain over memrepo with a fixed clock; L1 `ui` over stubs; L2 conformance for G4-G7,
G12; L3 for every SQL, lazy-row and ordering claim. No browser, network or real LLM.

**FX-C, config.** Before a setter, seed every column to a **distinct non-default** value. Assert
that every other column is unchanged and `updated_at == at`. Lazy creation is L3-only, on an
empty vault, asserting 0.5/0.05/1/21/7 elsewhere (finding 3). A second fixture: **no config row**,
for the effective-value cases.

**FX-H** (R10) is m4c's hysteresis fixture as-is: at least 8 candidates, slot 7 vs slot 8, seeded
incumbent, date-insensitive.

| # | PR | Production branch | Mutant | Killed by |
|---|---|---|---|---|
| C1 | 1a | each setter changes only its column | writes another | L3 FX-C, table over the five |
| C2 | 1a | lazy create names only `id, col, updated_at` | names every column with Go literals | L3 empty vault with defaults (a literal mistyped in Go diverges) |
| C3 | 1b | accept iff `Resolve(&v)==v` | `> 0` for margin; `< 2` for weight | `TestAdmin_Ranges` boundary rows: margin `0` ok; weight `2.0` ok, `nextafter(2,3)` rejected; `NaN`/`±Inf` rejected; days `1` ok, `0` rejected |
| C4 | 1b | unchanged vs **effective**: `effective == v && (stored == nil \|\| *stored == v)` | compare the stored pointer only | `…SameValueLogsNothing` (FX-C seeded equal) and `…DefaultOnAbsentRowWritesNothing` (no row; submit the default; the row is **not created** and the log count is unchanged) |
| C5 | 1b | stored nil, non-default → write; `previous` = the default in force | skip as unchanged; `previous: null` | `…UnsetFieldWriteLogs`: row created with SQL defaults elsewhere, one log row, `previous` equals the default |
| C5b | 1b | corrupt stored value + the default submitted → write and log | treat as no-op | `…CorruptStoredValueIsRepaired` (margin stored `-5`) |
| C6 | 1b | each applier calls its own port method | crossed wiring | table: each field moves exactly its own column |
| C7 | 1b | **write first, then record**; rejected/failed write logs nothing | record-first | `…ApplyFailureLogsNothing` (a failing `ConfigRepo` double: log count unchanged) and `…LogFailureAfterWriteIsVisible` (a failing `DecisionLog`: value stored, error wraps `ErrWriteLanded`) |
| C8 | 1a | `LearnedThresholds ORDER BY relation_type` | no order | contract inserting two rows out of order |
| C9 | 1b | recent effects use prefix `consolidate.` and limit `AdminRecentEffectRows` | prefix `""`; wrong limit | view test with a newer `capture.*` row that must be absent, and 11 effects → 10 |
| C10 | 1b | last run nil → never | zero time | view test |
| C11 | 2 | `ParseConfigField` closed set | accept `relation_thresholds` | 400 with **zero** brain write calls |
| C12 | 1b | parse per field (exact `true`/`false`) | `ParseBool` | `"1"` → rejected |
| C13 | 2 | R10 end to end | (no production branch: a regression pin) | `TestAdmin_MarginWriteReachesNextToday` (FX-H: margin 0.5 holds A; POST margin 0; next Today shows the challenger; the keeper is not reset) |
| C14 | 1b | log row `{fields:[col], previous:{col:v}, next:{col:v}}`, keyed by column; one row per write | positional; missing `fields`; extra row on a no-op | JSON key assertions, all five fields |
| C15 | 1b | the row `Update` writes decodes in m4e-activity's `ActivityService` (`Change` non-nil, `column: previous → next`) | a shape the decoder rejects | cross-slice integration test over memrepo |
| C16 | 1b | a rejected input writes nothing and logs nothing | log first | `…RejectedInputWritesNothing` (FX-C snapshot) |
| C17 | 1b | `AdminService` exports only `View` and `Update` | a `Set…` method | `TestAdminServiceExportsOnlyViewAndUpdate` |
| C18 | 2 | the admin POST body-table entry reaches the stub once | no entry / invalid body | m4e's coverage test and the same-origin subtest |
| C19 | 2 | `ErrWriteLanded` from `Update` renders **success with the "saved, but not logged" notice** and calls `slog.Warn` once | map it to a 500 or a 400; show success but skip the `Warn`; show the generic error text | `TestAdminPost_WriteLandedRendersSuccessNoticeAndWarns`: a stub `Update` returning `&brain.WriteLandedError{Record: true}`, a recording `slog` handler; asserts status, the notice text, no error text, one Warn record carrying the cause |
| C20 | 2 | a range error (`*ConfigRangeError`) answers **400**, re-renders the form with the error and the submitted value, and the **stored value is unchanged** | 200 or 500; re-render without the error; a handler that writes before validating | `TestAdminPost_RangeErrorIs400AndStoresNothing`: the real `AdminService` over memrepo seeded with distinct values (FX-C); snapshot unchanged, log count unchanged, body contains the message and the submitted text |
| C21 | 2 | an unknown `field` never reaches `Update` | call `Update` first and map its error; treat `relation_thresholds` as a known field | `TestAdminPost_UnknownFieldNeverCallsUpdate`: a counting `Admin` stub; `relation_thresholds`, `consolidation_last_run_at`, a bogus name and an empty field each give 400 with `Update` calls == 0 |

Equivalent mutants named so nobody spends time on them: the order of `updated_at` and the value
column inside the UPSERT's `SET` list.

Order per PR, as in m4c: a scaffold commit (signatures with zero-value bodies, compiles), a RED
commit (tests failing on assertions, never on `undefined`), then GREEN.

## 7. The PR chain (stacked-to-main) and the forecast

Budgets are impl+docs. They exclude tests, `test/support/**`, the regenerated golden and
`*_templ.go`. Each PR runs `make check-all` and `go vet -tags integration,e2e ./...`. Columns after
the point estimate are the estimate times 1.3 (the project's lowest measured overrun, umbrella
§5.1), 1.8 and 2.2 (the high end of its "1.3x-2.2x").

| # | Branch | Content | Impl+docs | x 1.3 | x 1.8 | x 2.2 |
|---|---|---|---|---|---|---|
| 0 | planning | spec and design written 2026-10-08 with m4e's planning PR; this slice's `tasks.md` is written by its own `sdd-tasks` run | (planning, **not counted**) | | | |
| 1a | `feat/config-setters-learned-thresholds` | 5 `ConfigRepo` setters, `RelationRepo.LearnedThresholds`, sqlite + memrepo + contract, G4, action +1, `configrepo.go:17` comment, the test-double ripple (§5) | ~140 | ~182 | ~252 | ~308 |
| 1b | `feat/brain-admin-service` | `AdminService` (`View`, `Update`, parse, door), G5, doc 02 §11 clause | ~190 | ~247 | ~342 | ~418 |
| 2 | `feat/ui-admin` | `/ui/admin` view + POST, `wireAdmin`, `uiDeps`, nav, G6/G7/G12 rows, R10 pin | ~275 | ~358 | ~495 | ~605 |
| | **Total (3 PRs)** | | **~605** | **~787** | **~1,089** | **~1,331** |

**The cut of the former PR 1 (~330).** At 1.3x a single ~330-line PR 1 is ~429, over the 400-line
soft ceiling, so it is cut at the port/brain seam: **1a = the persistence surface** (setters and
`LearnedThresholds`, nothing calls them yet except contract tests) and **1b = the brain door that
is the only caller** (`AdminService`). 1a is safe to merge alone: G4 closes the method set and, with
no caller outside tests, nothing can write config. 1b is where G5 (the door) lands, so the
"no caller outside the door" property is enforced from the first commit that has a caller. Rows
C1, C2 and C8 of §6 land in 1a; every other PR-1 row lands in 1b. Estimates are split by the
layer seam, not measured: if 1b measures over 400 after multiplying, `sdd-tasks` cuts it again at
`View` (read) versus `Update` (door), which share no code except `ConfigField`.

At 1.3x every PR is under 400 (largest: PR 2 at ~358). PR 2 exceeds it at 1.8x (~495) and 2.2x (~605); 1b exceeds only at 2.2x (~418); 1a never exceeds (~308 at 2.2x);
the same measured cut rule as m4e applies: after 1a merges, if `estimate x measured multiplier >
400` for a later PR, cut it at the nearest layer seam before `sdd-apply`.

**Planning PR 0 does not count toward the seven-PR rule.** The rule is about implementation PRs
(see m4e design §7).

**Forecast, recomputed on 2026-10-08 (round 2):** **3 PRs, ~605 lines; ~787 at 1.3x, ~1,089 at
1.8x, ~1,331 at 2.2x.** The rule (> 7 PRs or > 2,400 lines) does not fire at point, which is the rule's reading (the umbrella rule, proposal.md ~:301, reads budgeted, i.e. point, lines; the multipliers are sensitivity, and it does not fire at any of the three either). For
the record, m4e + m4e2 as one slice would be 9 PRs with this cut (8 with the original two), ~2,095
points, ~2,720 at 1.3x, ~3,770 at 1.8x, ~4,610 at 2.2x (fires on the PR-count clause only
at point; the lines clause needs 1.3x), which is why the split exists. m4e's own forecast: 6 PRs, ~1,490 lines, ~1,940 /
~2,680 / ~3,280 at 1.3x / 1.8x / 2.2x (since split into m4e, ~1,070 lines, and
`m4e-activity`, ~420: see their design §7). The "old ~1,890" figure and its reconciliation with
1,460 + 605 are in m4e design §7.

## 8. Risks

| # | Risk | Mitigation |
|---|---|---|
| RK-1 | `memrepo.Config` cannot prove SQL defaults | L3 owns that half (finding 3), as for `RecordConsolidationRun` |
| RK-2 | A config write whose log row fails is saved but unlogged | Visible to the user, `slog.Warn`ed, never silent (window b); the previous value is a number the user can re-enter |
| RK-3 | The cross-slice contract with m4e-activity (decoder shape) and m4e (`ErrWriteLanded`, body table, vocabulary file) | m4e-activity's A11 and this slice's C15 pin both ends of the decoder; the dependency list is in the headers of all three changes |
| RK-4 | `ConfigRepo` is now writable by every holder (`FocusKeeper`, `TodayService`, `ConsolidateService`) | G5 fails any call outside the door; G4 closes the method set |
| RK-5 | "Recent consolidation effects" is not job liveness: a quiet night leaves no rows | Stated on the page; liveness is the `last full consolidation` line from `config` |
| RK-6 | A relation type learned by M5 later appears in `LearnedThresholds` with no UI to edit it | Intended (Q5): read-only with the "learned" note |

No open question blocks `sdd-tasks`.

---

## 10. Judgment Day round 3 carry-overs (2026-10-08)

Appended, not rewritten into §1-§8; where one conflicts with an earlier section, this section
wins, and `tasks.md` pre-task 0 applies the in-place wording changes. Numbers are the round-3
correction numbers.

3. **Forecast basis (§7).** The umbrella rule (proposal.md ~:301) reads **budgeted, i.e. point,
   lines**; 1.3x/1.8x/2.2x are sensitivity. At point this slice is 3 PRs and ~605 lines: the
   rule does not fire. (The "does not fire at any of the three" sentence still holds at every
   multiplier, but it is a sensitivity statement, not the rule's reading.) If growth forces cuts
   past seven PRs the next remedy is the one stated in m4e's §10 item 3.
7. **§7 wording.** "At 1.8x and 2.2x PR 2 and 1b exceed it" is wrong for 1b: 1b is ~342 at 1.8x
   (under 400) and ~418 at 2.2x. Corrected: **PR 2 exceeds at 1.8x (~495) and 2.2x (~605); 1b
   exceeds only at 2.2x (~418); 1a never exceeds (~308 at 2.2x).** The risks R1-R6 of §8 are
   renamed **RK-1 to RK-6**, so they cannot be mistaken for spec requirements R1-R13 (R7-R11 are
   this slice's). References to "R3", "R4" etc. that mean a risk follow the rename.
11. **`TestUIGuardedLeavesEachReachAView`** (`internal/httpapi/server_test.go:117`, hard-coded
    leaf list over a stub `ui.Deps`) joins PR 2's file table: it gains the `/ui/admin` leaf, an
    `ADMIN` marker and a stub `Admin`. §3.5 G6's mention of it is read as this file, not
    `test/conformance`.
10. **Body table (consequence of m4e's item 10).** m4e's `uiCrossOriginBodies` already carries
    entries for the existing POST rows and an explicit exemption for `POST /ui/login`; this
    slice adds only `POST /ui/admin` and no exemption, as §3.5 G6 says.
