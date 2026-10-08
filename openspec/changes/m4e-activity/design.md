# Design — m4e-activity: the activity view

Technical design for `m4e-activity`, split off `m4e-beliefs-activity-admin` on 2026-10-08.
Requirements are [`spec.md`](spec.md) (R5, R6, the activity part of R9 and R11). This document
decides HOW and closes OQ4. Shape follows the archived
[`m4c` design](../archive/2026-10-07-m4c-focus-hysteresis/design.md) and
[`m4b` design](../archive/2026-09-29-m4b-units-capture/design.md). Companion:
[`../m4e-beliefs-activity-admin/design.md`](../m4e-beliefs-activity-admin/design.md).

> **Split from m4e-beliefs-activity-admin on 2026-10-08 by the owner's 7-PR rule after PR 2 measured 2.06x.**
> The sections below are the old m4e §3.5, §3.6, the activity row of §3.9, the activity rows of
> §3.11 and §3.12 (G7, G8, G11, G12), fixture FX-A, the A-series mutants, the old PRs 5-6, risks
> RK-7 to RK-9 and the activity items of §10, **moved with their text** (numbers kept: §3.5,
> §3.6, finding 5, finding 8, G-numbers, A1-A12, RK-8, RK-9, PR 5, PR 6). Rows shared with m4e
> are copied and marked. The measured multipliers and the PR 2 cut are recorded in
> [`tasks.md`](tasks.md) and in the beliefs `tasks.md`.
>
> **Depends on m4e (starts after m4e's PR 4 merges):**
> - **PR 1's vocabulary** and `store_api.golden` state (soft: no import of `selfmodel`; PR 5
>   regenerates the golden after PR 1's regeneration);
> - **the action-vocabulary file** and its `repocontract` map as m4e PR 3 leaves them (fifty-two
>   members). This slice adds no action. `ActivityFamilies` derives families from
>   `ports.AllDecisionActions()`;
> - **the `ui.Deps` / `uiDeps` / `wiring.go` pattern, the layout nav and the I22 whitelist test**
>   (`ui_entrances_test.go:32`; m4e design §3.9 and G12, PR 4): PR 6 is the second `uiDeps`
>   signature change and adds `Page` to the whitelist;
> - **`brain.ErrWriteLanded` / `*WriteLandedError`: not used.** The view is read-only;
> - **the cross-origin body table** (m4e G6): **not used, because activity has no POST.** No
>   body-table entry, no exemption; the coverage test would fail an entry with no row.
>
> **Depended on by `m4e2-admin`**: `DecisionLog.Before`, `DecisionCursor`, `DecisionRow` (PR 5),
> the change decoder (§3.6) and the `config.updated`-shaped fixture of A11 (PR 6). m4e2 depends on
> m4e for `WriteLandedError`, the I22 whitelist pattern, the `uiDeps` pattern and the body table.
> **`m4f` does not depend on this slice.**

> **Findings this design made that the spec did not anticipate** (the numbers are the old m4e
> design's; the other findings are beliefs' and stay there):
> 5. **The doc 02 sentence R11 names is at `:663-664`, not `:647-648`.** Lines 647-648 are the
>    pre-image's JSON shape. "Recording is not undoing … no surface offers it back until the UI
>    exists" sits at `docs/02-cognitive-core.md:663-664` at `dc22762`.
> 8. **`decision_log` order inside one second is random under the first design's tie-break.**
>    `occurred_at` is RFC3339 text, one-second resolution (`unitTimeLayout = time.RFC3339`,
>    `unitrepo.go:45`; written with `.UTC().Format`, `decisionlog.go:45`, `:51`), and ids are random UUID
>    v4. A nightly pass stamps every effect with one instant (`pass.now`), so `ORDER BY
>    occurred_at DESC, id DESC` would show a pass in arbitrary order. The tie-break is the row's
>    insertion sequence (§3.5).

---

## 1. Ground truth (read at `dc22762`)

| Claim | Verified at |
|---|---|
| `decision_log` is a **rowid table**: `id TEXT PRIMARY KEY` with no `WITHOUT ROWID`, indexed only by `idx_decision_log_occurred(occurred_at)`, so every index entry carries the rowid as its trailing column | `0001_core_tables.sql:95-102` (`rg 'WITHOUT ROWID'` over all migrations: no match) |
| `occurred_at` is stored `time.RFC3339` (seconds), UTC. Nothing in non-test code runs `VACUUM` (`rg -i vacuum`, docs and openspec excluded: no match) | `unitrepo.go:45`; `decisionlog.go:45`, `:51` |
| `DecisionLog` has `Record` and `Since` (ascending, `occurred_at > t`, tie-break on id) | `internal/ports/decisionlog.go:238-261`; `internal/store/sqlite/decisionlog.go:71-109` |
| `memrepo.DecisionLog` keeps full-precision `time.Time` and sorts `Since` by `(OccurredAt, ID)`; SQLite truncates to the second | `test/support/memrepo/decisionlog.go:17-20`, `:72-95` |
| `BrowsePageSize = 50` is a **transport constant**, explicitly not a §13 row | `internal/ports/unitrepo.go:254-257` |
| Keyset paging precedent: `(created_at, id) < (?, ?)`, `DESC, DESC`, `LIMIT n+1` | `internal/store/sqlite/unitrepo.go:550-571` |
| UI leaves are listed twice: `newUIMux` registrations and `Handler.ServeHTTP`'s `r.Pattern` switch, pinned by `wantUIMuxWiring` and `TestUIGuardedLeavesEachReachAView` | `internal/httpapi/server.go:150-173`; `internal/ui/ui.go:94-111`; `test/conformance/httpapi_ui_wiring_test.go:158-174` |
| The I22 reflection gate whitelists exactly `{Today, Browse, Detail, ForText, Capture}` as methods any `ui.Deps` interface field may expose | `test/conformance/ui_entrances_test.go:32` |
| `uiDeps` takes `(today, units, recall, capture, serving)` and exists to keep typed-nil services out of interfaces; `wireUnits`/`wireToday` are the constructor precedent, `wiring_units_test.go` the test precedent | `cmd/nooma/serve.go:285-300`; `cmd/nooma/wiring.go:310-352`; `cmd/nooma/wiring_units_test.go:14` |

The rows on `BrowsePageSize`, the UI leaves listed twice, the I22 whitelist and `uiDeps` are
copied from the m4e design §1, where they are shared; the four rows before them moved.

**New calibratable constants: none** (`ActivityPageSize` is a transport constant, §3.5, not a §13
row). **New migration: none.** **New ADR: none.** Doc 02 gains text in §5 step 4 (§3.11).

---

## 2. What m4e-activity decides, in one paragraph

Activity reads a new newest-first keyset read, `DecisionLog.Before`, ordered by `(occurred_at,
insertion sequence)`, at a 50-row transport page with an action-family filter (OQ4). The view
renders any change-shaped row's `previous` beside its `next`, read-only, by shape rather than by
action, so `correction.applied`, `belief.edited` (m4e) and `config.updated` (`m4e2-admin`) share
one decoder and no surface offers a pre-image back.

---

## 3. Decisions

### 3.5 OQ4 — the newest-first read and its page

```go
type DecisionCursor struct { OccurredAt time.Time; Seq int64 }
type DecisionRow struct { Decision; Seq int64 } // Seq: the row's insertion sequence (rowid)

// Before returns up to limit decisions strictly older than before, ordered
// (occurred_at DESC, insertion sequence DESC); nil before starts at the newest.
// actionPrefix "" matches every row; otherwise only rows whose action begins
// with it. limit < 1 returns an empty page without reading.
Before(ctx context.Context, before *DecisionCursor, actionPrefix string, limit int) ([]DecisionRow, error)
```

**Ordering, decided (finding 8).** `ORDER BY occurred_at DESC, rowid DESC`, cursor
`(occurred_at, rowid) < (?, ?)`. `decision_log` is a rowid table (§1), `rowid` is monotone in
insertion order (new rowids are max+1 and nothing deletes from the log, I12), and a pass writes
its rows sequentially at one instant, so the tie group comes back in **reverse write order**,
which is the order a reader of "newest first" expects. `Decision` and `Since` are untouched:
`DecisionRow` embeds `Decision` and adds `Seq`, so the cursor is built from a row the reader
already holds. `rowid` is also the trailing column of `idx_decision_log_occurred`, so the order
is satisfiable from that index (probe below).

| Option for the tie-break | Verdict |
|---|---|
| **`rowid` (insertion sequence)** — chosen | Deterministic, meaningful (write order), index-trailing, no migration |
| `id DESC` | Rejected. UUID v4: the order inside a pass would be arbitrary and unrelated to what happened first |
| Add a `seq`/microsecond column | Rejected. A migration for a property `rowid` already has |
| Client-side reverse of `Since` | Rejected by umbrella R10 |

*Honest limit:* for a table without an `INTEGER PRIMARY KEY`, SQLite may renumber `rowid`s on
`VACUUM`. Nothing in the code runs `VACUUM` (§1), a cursor lives only in one reader's URL, and the
worst case is one skipped or repeated row across a page boundary during a manual `VACUUM`. Recorded
as risk RK-8.

The SQL follows m4b's keyset shape: `WHERE (occurred_at, rowid) < (?, ?)` (only with a cursor)
`AND substr(action, 1, ?) = ?` (only with a prefix), `ORDER BY occurred_at DESC, rowid DESC LIMIT
?`. The prefix uses `substr`, not `LIKE`, so `_` in the vocabulary is never a wildcard. `Since` is
untouched (R5, umbrella R10). An `EXPLAIN QUERY PLAN` test pins the use of
`idx_decision_log_occurred` and the absence of `USE TEMP B-TREE FOR ORDER BY` for the unfiltered
and cursor forms (the m4b precedent), so **no migration** is needed. Written first as a probe: if
SQLite will not order from the index with `rowid` in a row-value comparison, the fallback is the
expanded predicate `occurred_at < ? OR (occurred_at = ? AND rowid < ?)`, same semantics.

**Whole-second fixtures.** SQLite stores seconds; `memrepo` keeps nanoseconds. Every FX-A
fixture time is a whole second, and the fixture builder fails the test on a sub-second input, so
the two implementations cannot diverge on a boundary. `memrepo` is not changed to truncate
(other tests rely on its full precision). The brain builds the next cursor from the last *shown*
row's own `OccurredAt` and `Seq`, so a cursor round-trips within one implementation.

| OQ4 question | Decision | Grounding |
|---|---|---|
| Page size | `brain.ActivityPageSize = 50`, a **transport constant, not a §13 row** | `ports.BrowsePageSize = 50`'s ruling (`unitrepo.go:254-256`): it shapes a page and decides nothing doc 02 governs. Same number, same reasoning, same classification. No §13 row is invented |
| "Older" link | Brain asks for `ActivityPageSize+1` rows. The extra row only signals that more exist. The next cursor is the **last shown** row | m4b's `LIMIT n+1` |
| Filter by family | `?kind=<family>`, where families = the first dot-segment of `ports.AllDecisionActions()`, derived in brain by `ActivityFamilies(actions)` (today: `capture`, `check`, `relation`, `correction`, `consolidate`; `belief` after PR 3; `config` after `m4e2-admin`). An unknown kind → 400 with no read | Derived from the vocabulary, so a new family needs no edit. The function takes the action list as a parameter so a test can feed it a synthetic family |
| Filter by date | **None** | The cursor already reaches any date. No requirement asks for a jump |

### 3.6 Activity rendering (R6)

`brain.ActivityService.Page(ctx, kind, before) (ActivityPage, error)` returns rows plus a decoded
`Change` for any row whose context is a JSON object carrying `previous` and `next` objects. That
covers `correction.applied`, `belief.edited` and `config.updated` (m4e2's row, shaped
`{fields:[col], previous:{col:v}, next:{col:v}}` like `belief.edited`; this decoder must already
accept it, which is the cross-slice dependency in the header). Decoding is **shape-driven, not
action-driven**: a new change-shaped action needs no edit here. Rendering iterates `fields` when it
is a string array whose members key both objects, otherwise the sorted union of keys, as
`column: previous → next`. A value renders by JSON kind: a string as text, a number without a
trailing `.0` (`21`, not `21.0`; `0.05` stays), a bool as `true`/`false`, `null` as "(none)".
Malformed JSON or a missing key yields `Change == nil` and the row renders its rationale only;
the page never fails (R6). The view contains **no `method="post"` and no `hx-post`** anywhere
(G8). The only form is the GET filter. `applyWithPreImage` and its I23 gate are not touched.

### 3.9 UI route and wiring (activity's part)

| Pattern | Guarded | Brain call | Errors |
|---|---|---|---|
| `GET /ui/activity` | yes | `Page(kind, before)` | 400 bad kind / half or malformed cursor (`before_at` + `before_seq`, m4b's `parseBrowseCursor` shape) |

(`/ui/beliefs` routes: `m4e-beliefs-activity-admin`; `/ui/admin`: `m4e2-admin`.) The route is a
GET only, so there is no `MaxBytesReader` and no HTMX fragment split to specify. `ui.Deps` gains
`Activity`, a narrow interface (m4a §3.1). The layout nav gains the activity link in this PR only
(m4e's PR 4 added the beliefs link).

**Wiring (`cmd/nooma`).** `wiring.go` gains `wireActivity(db) *brain.ActivityService`
(`NewDecisionLog`), provider-free and wired unconditionally at vault open, `wireUnits`'s
precedent. `uiDeps` gains an `activity *brain.ActivityService` parameter with the same typed-nil
guard (the second signature change, after m4e's PR 4 added `beliefs`), and its one call site in
`serve.go` passes it. The constructor has a test over a real empty migrated vault
(`wiring_units_test.go`'s precedent), and `TestUIDeps_NilServicesStayNilInterfaces`
(`serve_test.go`) gains a case for it. `wireActivity` and the second `uiDeps` change land in PR 6.

### 3.10 Vocabulary edits

None. This slice adds no `decision_log` action; the family filter reads the list m4e leaves.

### 3.11 Doc edits (activity's part)

| Doc | Edit | PR |
|---|---|---|
| doc 02 §5 step 4 (`:663-664`) | "Recording is not undoing. `/ui/activity` shows the previous value beside the new one, read-only; no surface offers it back." | 6 |
| `docs/06-harness.md` §4 | the I23 row and the I22 row name the whitelist addition (`Page`, §3.12 G12) | 6 |

`docs-sync.sh`: PR 5 and PR 6 touch no `internal/core/**`, so the gate does not fire; doc 02 §5
step 4 is edited in PR 6 on its own account (non-negotiable 1: code and doc agree in the same PR).

### 3.12 Structural gates for invisible properties (activity's part)

| # | Property | Gate | Fires on (probe) | Silent on |
|---|---|---|---|---|
| G6 | *(n/a)* The activity GET adds no mutating route, so the cross-origin body table gets **no** entry | The route is a `wantUIMuxWiring` GET row with `guarded: true`; `TestUIGuardedLeavesEachReachAView` (`internal/httpapi/server_test.go:117`, a hard-coded leaf list over a stub `ui.Deps`) gains the `/ui/activity` leaf and an `ACTIVITY` marker in PR 6. `TestUICrossOriginBodiesCoverEveryPOSTRow` (m4e G6) fails for an entry with no row, so an entry for activity would fail it | the GET row unguarded; the leaf missing | the body table |
| G7 | The activity GET writes nothing (R5) | `TestUIReadViewsWriteNothing` (conformance, I27's shape; created by m4e's PR 4 for the beliefs GET): the activity GET over a write-counting decorator of the `DecisionLog` the service holds → zero calls to any write method | a GET that records, signals or seeds | — |
| G8 | Pre-images are never offered back (R6) | `TestActivityView_HasNoMutatingForm`: rendered page contains no `method="post"` (case-insensitive) and no `hx-post` | a "restore" button | the GET filter form |
| G9 | I23 stays untouched (R6) | The existing `applyWithPreImage` `go/ast` test, untouched and green. The naming discipline behind it (`EditContent`, never `UpdateContent`) is m4e's G9 and finding 3 | — | — |
| G11 | Store surface widening is reviewed | `testdata/schema/store_api.golden` regenerated in PR 5 (`make store-api-golden`; m4e's PR 1 regenerated it for the belief methods) | an unreviewed method | — |
| G12 | The I22 entrance whitelist names exactly what this slice adds | `ui_entrances_test.go:32`'s `allowedMethods` (a flat name set over every interface field of `ui.Deps`) is edited **per PR, by exact name**: this slice's PR 6 adds `Page` (the `Activity` interface); m4e's PR 4 adds `ByFacet`, `Edit`, `Retire`; `m4e2-admin` adds `View`, `Update`. Decision: keep the flat set (the gate's declared semantics, no scope creep); the tighter per-field map is a possible later hardening, not this change's | a `ui.Deps` interface exposing any method not on the list (the existing test fires; PR 6's RED commit shows it red before the whitelist edit) | the names already present |

Each GREEN commit body records its probe (mutation applied, red output, reverted), following m4b
and m4c.

---

## 4. Data flow

```
GET /ui/activity?kind&before_at&before_seq ─▶ Page ─▶ DecisionLog.Before(cursor, prefix, 51) ─▶ 50 rows + older?
```

## 5. File changes

| File | Action | PR |
|---|---|---|
| `internal/ports/decisionlog.go`, `internal/store/sqlite/decisionlog.go`, memrepo, repocontract | `DecisionCursor`, `DecisionRow`, `Before` | 5 |
| `internal/brain/check_test.go:323` (`recordingLog`) | **Ripple:** a hand-written `ports.DecisionLog` double with `Record` and `Since` (`:357`) only; it stops compiling when the port gains `Before`. Add a `Before` that returns an empty page (its tests never page). The other `ports.DecisionLog` users (`i27DecisionLog`, the `memrepo` embeds) promote the new read and need no edit; `test/conformance/i27_viewing_is_not_delivering_test.go`'s header comment ("DecisionLog: Record writes; Since reads") gains `Before` among the reads | 5 |
| `internal/brain/activity.go`; `internal/ui/activity.go`, `activity.templ`, `ui.go`, `layout.templ`; `cmd/nooma/wiring.go` (`wireActivity`), `wiring_activity_test.go`, `serve.go`, `serve_test.go` | page service + view + wiring + `uiDeps` | 6 |
| `internal/httpapi/server_test.go:117` (`TestUIGuardedLeavesEachReachAView`) | `/ui/activity` leaf, `ACTIVITY` marker, stub `Activity` | 6 |
| `test/conformance/ui_entrances_test.go` | whitelist addition `Page` (G12) | 6 |
| `test/conformance/httpapi_ui_wiring_test.go`, `ui_read_views_write_nothing_test.go` | the GET row; G7 | 6 |
| docs | §3.11 | 6 |

## 6. Testing strategy and mutation targets

Levels: L1 brain over memrepo with a fixed clock, L1 `ui` over stubs, L2 conformance for G7 to
G12, and L3 for every SQL ordering and cursor claim. No browser, network or real LLM.

### Fixture rules

**FX-A, activity.** At L3/contract: five rows, **three sharing one `occurred_at`**, written in a
known order with **ids chosen so that id order differs from write order**, the tied group
**straddling a page boundary**, page size 2; whole-second times only (the builder fails the test
on a sub-second input). A `capture.checkin.*` row beside a `check.*` row for the prefix test. At
brain level: **a pass-sized tie group** (`2 × ActivityPageSize + 10` rows at one instant, ids in
descending write order) walked page by page: every row exactly once, in reverse write order; and
exactly `ActivityPageSize` rows (no "older" link) and `ActivityPageSize+1` (link present, the
51st row absent from page one).

### Mutation targets, enumerated from the production diff

| # | PR | Production branch | Mutant | Killed by |
|---|---|---|---|---|
| A1 | 5 | `ORDER BY occurred_at DESC, rowid DESC` | ASC; drop the rowid tie-break; `id DESC` | FX-A (ids chosen so id order ≠ write order) |
| A2 | 5 | `(occurred_at,rowid) < (?,?)` | `occurred_at < ?` (skips the tie); `<=` (repeats) | FX-A exactly-once walk, tied group straddling the boundary |
| A3 | 5 | `substr` prefix | ignore; `LIKE 'check%'` | FX-A `capture.checkin` row absent from `check.` |
| A4 | 6 | brain asks `size+1`; cursor = last *shown* row | ask `size`; cursor = extra row | FX-A brain cases |
| A5 | 5 | `limit < 1` → empty page, no read | pass `LIMIT -1` (SQLite: no limit) | contract: limit 0 → empty; limit 2 over 5 rows → the 2 newest |
| A6 | 5 | `nil` cursor starts at the newest | treat nil as a zero cursor (returns nothing) | contract: nil returns the newest rows; a zero-value non-nil cursor returns none |
| A7 | 6 | `Before`, not reversed `Since` | `Since` reversed | brain test with > page rows: page one holds the **newest** |
| A8 | 5 | `Seq` populated from rowid and equal to write order in both implementations | `Seq` 0; memrepo unordered | contract `Seq` strictly increasing with write order |
| A9 | 6 | unknown `kind` → 400, no read | read all | ui test with a counting reader |
| A10 | 6 | `Change` decoded by key; malformed → rationale only | show `next` only; 500 on bad JSON | `TestActivityView_CorrectionShowsPreviousAndNext`, `…MalformedContextStillRenders` |
| A11 | 6 | **`config.updated`-shaped row decodes** (`{fields:["goal_stagnation_days"], previous:{…:21}, next:{…:28}}`; a bool and a float case) | decoder keyed to `correction.applied`'s action; numbers rendered `21.0` | `TestActivityView_ConfigUpdatedShapeRenders`: `goal_stagnation_days: 21 → 28`, `consolidation_enabled: true → false`, `weight_threshold: 0.5 → 0.6`, no trailing `.0` |
| A12 | 6 | kind families derived from the vocabulary | hard-coded list | `TestActivityFamilies_DerivedFromVocabulary`: a synthetic action list with family `zzz` accepts `?kind=zzz`; the real list yields exactly the first segments; the order is stable |

Equivalent mutant named so nobody spends time on it: `substr(action,1,?)` vs
`substr(action,1,length(?))` (same for an ASCII vocabulary).

Order per PR, as in m4c: a scaffold commit (signatures with zero-value bodies, compiles), a RED
commit (tests failing on assertions, never on `undefined`), then GREEN. Umbrella §5.2 row 10
(I23 read-only) lands in PR 6.

## 7. The PR chain (stacked-to-main) and the split forecast

Budgets are impl+docs. They exclude tests, `test/support/**`, the regenerated golden and
`*_templ.go` (`linguist-generated`, `.gitattributes:31`). Each PR runs `make check-all` and `go
vet -tags integration,e2e ./...`.

Columns after the point estimate are the estimate times 1.3 (the project's lowest measured
overrun, umbrella §5.1), 1.8 and 2.2 (the high end of its six measured overruns, "1.3x-2.2x"; a
4.3x outlier is recorded in the umbrella and not modelled here).

| # | Branch | Content | Impl+docs | x 1.3 | x 1.8 | x 2.2 |
|---|---|---|---|---|---|---|
| 5 | `feat/ports-store-decisionlog-before` | `DecisionCursor`, `DecisionRow`, `Before` (port, sqlite, golden): the umbrella's "newest-first read" row | ~130 | ~169 | ~234 | ~286 |
| 6 | `feat/ui-activity` | `ActivityService` + `/ui/activity` + G8 + G12 (`Page`) + `wireActivity` + doc 02 `:663-664`: the umbrella's "glass box" row | ~290 | ~377 | ~522 | ~638 |
| | **Total (2 PRs)** | | **~420** | **~546** | **~756** | **~924** |

Arithmetic: 130 + 290 = 420; 169 + 377 = 546; 234 + 522 = 756; 286 + 638 = 924.

**The 400-line soft ceiling.** At 1.3x both PRs are under 400 (PR 6 at ~377). At 1.8x and 2.2x
PR 6 is over it (~522, ~638) and PR 5 is not (~234, ~286). m4e measured 2.06x on its PR 2, which
puts PR 6 at ~597. **Cut rule (inherited from m4e's design §7):** after the first PR of this
slice merges, compute actual-over-estimate; if `estimate x measured multiplier > 400` for PR 6,
cut it at its nearest layer seam before `sdd-apply`: "`ActivityService`" and "view + wiring". The
umbrella rule is re-run on the new PR count (three at most; far from seven).

**Umbrella rule on this slice (budgeted, i.e. point, lines):** 2 PRs and ~420 lines; it does not
fire, and does not fire at x 1.3, x 1.8, x 2.06 or x 2.2 (~924 lines).

## 8. Risks

| # | Risk | Mitigation |
|---|---|---|
| RK-7 | The cross-slice contract with `m4e2-admin`: the decoder accepts `config.updated`'s shape | A11 pins the decoder here; m4e2 adds the integration test (its C15) that the row it writes decodes; the dependency list is in both headers |
| RK-8 | `rowid` of `decision_log` is not stable across `VACUUM` | Nothing runs `VACUUM` (verified); a manual one can skip or repeat one row at a page boundary of a held cursor; no migration is justified for that |
| RK-9 | **INFO.** The family filter `substr(action, 1, ?) = ?` is not index-assisted, so a rare family scans `decision_log` backwards from the cursor until the page fills | Acceptable at current volume (a personal vault, one table); the unfiltered and cursor forms are index-ordered and pinned by `EXPLAIN QUERY PLAN`. Revisit if a filtered page is ever measured slow |

No open question blocks `sdd-tasks`. OQ4 is closed above.

---

## 10. Judgment Day round 3 carry-overs (activity's part)

Binding corrections from round 3 that touch this slice, moved from the m4e design's §10 with the
numbers kept. Where one conflicts with an earlier section, this section wins.

1. **No dead nav link.** PR 4 adds only the beliefs link to `layout.templ`. PR 6 adds the
   activity link, so `internal/ui/layout.templ` joins PR 6's files (§5 row for PR 6). §3.9's
   "the layout nav gains two links" reads "one link per PR".
11. **`TestUIGuardedLeavesEachReachAView`** lives in `internal/httpapi/server_test.go:117`
    (hard-coded leaf list over a stub `ui.Deps`), not in `test/conformance`. PR 4 adds the
    `/ui/beliefs` leaf and a `BELIEFS` marker with a stub `Beliefs`; PR 6 adds `/ui/activity` and
    `ACTIVITY` with a stub `Activity`. The file joins both PRs' tables (§5).

Tasks-phase notes (not round-3 corrections): mutants A4 and A7 (§6) are tagged PR 5 but their
production branch is `ActivityService` (`internal/brain/activity.go`), which is PR 6; `tasks.md`
places them in PR 6.
