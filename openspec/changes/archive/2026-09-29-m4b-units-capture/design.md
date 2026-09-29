# Design — m4b: units and capture

Technical design for `m4b-units-capture`, the second of six slices sharing
[`m4-mirror-ui/proposal.md`](../m4-mirror-ui/proposal.md). Requirements are `spec.md` (R1–R6);
this document decides HOW. Shape follows the archived
[`m4a` design](../archive/2026-09-23-m4a-ui-foundation/design.md): ground truth, decisions, a
symbol→PR table, a tip-behaviour table, and a structural gate per invisible property.

> **Four things this design decides that the umbrella did not anticipate**, named up front:
> 1. **One migration, `0005`** — an index for the browse order (§3.2). Umbrella §3.2 item 4 said
>    "no migration"; without it every page is a full scan plus sort, which spec R1's "without
>    re-scanning" does not describe. Owner-review OR1.
> 2. **No `feat/httpapi-cross-origin` PR** (spec OQ2, confirmed). The wrap exists at
>    `internal/httpapi/server.go:116-121`. Its budget becomes one table-driven test (§3.6).
> 3. **m4a's UI wiring gate has holes.** It skips non-literal patterns, `HandleFunc`, loops, and
>    the whole outer mux in `Handler`. All of them are closed in PR 3's commit 0, before any new
>    route lands (§3.6, §3.7).
> 4. **The detail page reads through `UnitRepo.LiveByIDs`**, the method `RecallService.LiveByIDs`
>    passes straight through to (`internal/brain/recall.go:207-209`). This is not `RecallService`
>    itself, so detail works on a vault with no providers, where `wireBrain` returns a nil
>    `RecallService` (`cmd/nooma/wiring.go:323-327`). I02 is still enforced once (§3.3).

---

## 1. Ground truth (read at `48cac56`)

| Claim | Verified at |
|---|---|
| The whole `/ui` subtree is `securityHeaders(xo.Handler(newUIMux(d)))`, with `xo := http.NewCrossOriginProtection()` | `internal/httpapi/server.go:116-121` |
| `newUIMux` registers only literal `mux.Handle` leaves. The guarded leaves are `requireCookie(d.Token)(d.UI)` | `server.go:148-166` |
| The wiring gate skips a non-literal pattern (`continue`), skips any method other than `Handle` (so `HandleFunc` is skipped too), skips a receiver not named `mux`, and only descends into `if` blocks, not `for`/`switch`/block statements | `test/conformance/httpapi_ui_wiring_test.go:217-247` |
| The gate pins every guarded target to exactly `d.UI` | same file, `:174-178` |
| `requireCookie` answers 303 to `/ui/login` on any method. Its body is pinned by a template. Any second declaration of `requireCookie`/`requireToken`/`presentedSecret` fails the gate | `internal/httpapi/cookie.go:65-89`, `test/conformance/httpapi_secret_compare_test.go:43-67` |
| `ui.Handler.ServeHTTP` renders Today unconditionally. `ui.Deps` is `{Today, Serving}` | `internal/ui/ui.go:26-81` |
| `ui-boundary` lets `internal/ui` import `brain`, `core`, `ports` and `templ` | `.golangci.yml` (m4a §3.1) |
| `UnitRepo` has 13 methods and no paged read. "Live" means `status = pool`, filtered positively. The rule "no `List(status)`" is stated | `internal/ports/unitrepo.go:18-33`, `:203-222` |
| `units` has one index, `idx_units_status_touched(status, last_touched_at)`, plus the insight partial index. Nothing covers `created_at`. Ids are UUIDv4, so random | `migrations/0001_core_tables.sql:22-28`, `cmd/nooma/wiring.go:32` |
| Timestamps are TEXT RFC3339 UTC at second precision, so they sort lexically. `status` is bound as `?`, never a literal | `internal/store/sqlite/unitrepo.go:39-45`, `:98`, `:525` |
| An EXPLAIN QUERY PLAN precedent exists: `TestUnitRepo_LiveFocusCandidatesByTypeUsesStatusIndex`, run over `buildLiveFocusCandidatesByTypeQuery` | `unitrepo.go:508-528`, `unitrepo_integration_test.go:203-217` |
| `docs/03-data-model.md` mirrors every index, and `schema_doc_test` compares it to the migrations | `docs/03-data-model.md:41-245`, `test/conformance/schema_doc_test.go` |
| `RecallService.ForText` is the admitting entrance (ADR-0020). `ScoredFor` does not admit. Both legs are bounded by `recall.RecallTopK = 20` | `internal/brain/recall.go:125-229`, `internal/core/recall/fuse.go:14` |
| `CaptureInput{Text, Channel, ReferentID}`. When `ReferentID` is non-empty it wins in `resolveReferent` (resolved via `ByID`, at any status), and it is ignored unless the text classifies as a correction | `internal/brain/result.go:23-36`, `internal/brain/correction.go:103-107` |
| `RelationRepo.ByUnit` returns both directions, unbounded, and has **no production caller yet** | `internal/ports/relationrepo.go:67-74` |
| `wireBrain` returns nil `capture`/`recall` on an unconfigured vault. `serve.go` builds `ui.Deps` inline | `cmd/nooma/wiring.go:323-327`, `cmd/nooma/serve.go:144-151` |
| Existing I22 test: two production entrances. I27 write-guard shape: embedded memrepo fakes with failing write overrides | `test/conformance/i22_recall_one_mechanism_two_entrances_test.go`, `i27_viewing_is_not_delivering_test.go:119-150` |
| Doc 01 says `/ui/units` browses "every unit in the vault". I02 says it browses live units only | `docs/01-architecture.md:121` |

**New calibratable constants: none.** `ports.BrowsePageSize` is a transport bound (§3.2, OR2).

---

## 2. What `m4b` decides, in one paragraph

**One read port method** `LiveBrowsePage`: a fixed page size, a keyset cursor
`(created_at, id)`, newest first, a type filter, and one index (§3.2). **One brain read model**
`UnitsService`: `Browse` and `Detail`, with no clock (§3.3). **Search is `RecallService.ForText`,
reached through a one-method `ui.Searcher`** (§3.4). **Capture and correction are
`CaptureService.Capture`, reached through a one-method `ui.Capturer`.** `ReferentID` comes only
from the correction route's path (§3.5). **Every new route is a literal, guarded leaf dispatching
to `d.UI`**, and `ui.Handler` switches on `r.Pattern` (§3.1). **Cross-origin is inherited**, and
proven by a test generated from the wiring table (§3.6). m4b adds no middleware, no ADR, no
auth path, and does not change `requireCookie`.

---

## 3. Decisions

### 3.1 Routing: every new route dispatches to `d.UI`; `ui.Handler` switches on `r.Pattern`

```
uiMux (additions, all guarded by requireCookie(d.Token)(d.UI))
├── GET  /ui/units                     browse page | rows fragment | search      PR 3
├── GET  /ui/units/{id}                detail                                    PR 4
├── GET  /ui/capture                   capture form                              PR 5
├── POST /ui/capture                   CaptureService.Capture                    PR 5
└── POST /ui/units/{id}/correct        Capture with ReferentID = {id}            PR 5
```

| Option | Verdict |
|---|---|
| **Same `guardedUI` target for every leaf; `ServeHTTP` switches on `r.Pattern`** (Go ≥1.23; `ServeMux` sets it on the request it forwards, and `requireCookie` passes that same `*Request` on) — chosen | The outer mux stays the only router (m4a §3.2's principle). The wiring gate's constants (`requireCookie`, `d.Token`, `d.UI`) are unchanged; only table rows are added. The default arm answers 404 |
| A per-view target (`d.UI.Units()`) | Rejected. It widens the gate from one target to N, and weakens the gate that caught round 5's exploit |
| An inner mux inside `ui.Handler` | Rejected. That is two routers that must agree, which is exactly what m4a collapsed |

The drift risk is a pattern registered in `newUIMux` with no matching `case`. It is caught by
`TestUIGuardedLeavesEachReachAView` (httpapi L1): for each guarded leaf, a request through
`Handler(d)` with stubbed readers must return its view marker and not 404. Unauthenticated
`POST` is left as it is: `requireCookie` answers 303 and the handler never runs. m4a left
this answer to "the first guarded non-GET route". The answer here is to keep 303, and not
reopen a template gate that took five rounds to harden (OR5).

### 3.2 The browse read

```go
// internal/ports/unitrepo.go — UnitRepo gains its 14th method.

// BrowsePageSize is the fixed page size LiveBrowsePage returns. A transport
// bound, not a brain decision: nothing in doc 02 depends on it.
const BrowsePageSize = 50

// BrowseCursor is the last row of a page. The next page starts strictly after it.
type BrowseCursor struct {
	CreatedAt time.Time
	ID        string
}

type BrowsePage struct {
	Units []unit.Unit      // at most BrowsePageSize, created_at DESC, id DESC
	Next  *BrowseCursor    // nil on the last page
}

// LiveBrowsePage returns one page of unit.StatusPool units, newest first.
// An empty types means every type. This is deliberately the opposite of
// LiveFocusCandidatesByType's empty-means-none, and repocontract pins both
// postures. after == nil is the first page.
LiveBrowsePage(ctx context.Context, types []unit.Type, after *BrowseCursor) (BrowsePage, error)
```

| Decision | Choice | Rejected |
|---|---|---|
| **Bound** | No `limit` parameter at all, so an unbounded read cannot be expressed at the port. SQL uses `LIMIT BrowsePageSize+1` to detect `Next` | A `limit int` parameter, which needs a clamp in every implementation plus a test that the clamp holds |
| **Order** | `created_at DESC, id DESC`. `created_at` is immutable, so the keyset never repeats or skips a row that already existed. `id` breaks ties within one second | `id` order, which is random because ids are UUIDv4. `last_touched_at` (already indexed) moves under consolidation, and it is not one of I18's three dates |
| **Cursor** | Row values `(created_at, id) < (?, ?)`, with the time formatted by `formatUnitTime` (the column's own layout). Over HTTP it travels as two query params, `after_created` (RFC3339) and `after_id`. Supplying only one of them is a 400; supplying both but with `after_created` not parseable as RFC3339, or either key present with an empty value, is also a 400. `after_id` is otherwise opaque text, as every id is at `ports.UnitRepo.ByID` and `GET /units/{id}`: no format check is invented for it, and an id that matches no row simply yields the rows after it in the ordering | An opaque base64 cursor, which needs an encoding and decoding pair and hides nothing worth hiding. `OFFSET`, which re-scans |
| **Filter** | `status = ?` bound to `pool`, positive (I02), plus `type IN (…)` when types is non-empty. Type only, no status axis (**spec OQ1 resolved**: `unitrepo.go:24-33`'s "no `List(status)`") | A status parameter, which could only ever be `pool` |
| **Index** | Migration `0005_units_browse_index.sql`: `CREATE INDEX idx_units_live_browse ON units(status, created_at, id);`, plus the same line in doc 03 | A partial index `WHERE status='pool'`, which the planner cannot use because `status` is bound as `?`. `(status, type, created_at, id)`, where `IN` over several types forces a temp B-tree for the order |

`buildLiveBrowsePageQuery(types, after)` is factored out, `LiveFocusCandidatesByType`'s
precedent, so the L3 plan test runs the exact production SQL. **If the planner does not use
the index for the row-value predicate**, the fallback is the expanded form
`created_at < ? OR (created_at = ? AND id < ?)`. The plan test decides; neither form is assumed.

### 3.3 `brain.UnitsService`: the read model for both views

```go
// internal/brain/units.go — holds no ports.Clock, because nothing here is a function of time.
func NewUnitsService(units ports.UnitRepo, rels ports.RelationRepo) *UnitsService
func (s *UnitsService) Browse(ctx context.Context, types []unit.Type, after *ports.BrowseCursor) (ports.BrowsePage, error)
func (s *UnitsService) Detail(ctx context.Context, id string) (UnitDetail, bool, error)

type UnitDetail struct {
	Unit      unit.Unit
	Relations []RelatedUnit   // ByUnit's order, and only relations whose other endpoint is live
}
type RelatedUnit struct {
	RelationID, Type string
	Outgoing         bool      // true when Unit is FromUnitID
	Confidence       float64
	Other            unit.Unit // resolved with LiveByIDs
}
```

`Detail` makes one `LiveByIDs([id])` call and returns `found=false` for an empty result. That
`found=false` is the same 404 class as `GET /units/{id}`. It then makes one `ByUnit(id)` call
and one `LiveByIDs(otherIDs)` call, and drops every relation whose other endpoint is not
live. That drop is I02 for neighbours, done by the same positive filter, never by a
negative check in `ui`.

It renders the **stored** `Weight` labelled "stored weight", not an effective weight. An
effective weight would need a clock read and a `core/weight` call, which is m4c's
territory (OR3). `wireUnits(db)` is wired unconditionally at vault open, like `wireToday`.
Browsing needs no provider.

### 3.4 Search: `ui.Searcher` is `RecallService.ForText` and nothing else (I22)

```go
// internal/ui/ui.go
type UnitsReader interface { Browse(…) (ports.BrowsePage, error); Detail(…) (brain.UnitDetail, bool, error) }
type Searcher    interface { ForText(ctx context.Context, text string) ([]unit.Unit, bool, error) }
type Capturer    interface { Capture(ctx context.Context, in brain.CaptureInput) (brain.CaptureResult, error) }
type Deps struct { Today TodayReader; Units UnitsReader; Search Searcher; Capture Capturer; Serving Serving }
```

`GET /ui/units?q=…` calls `Search.ForText(q)`. `ForText` is the admitting entrance, so
ADR-0020's floor applies exactly as it does for `/recall`. Search mode renders recall's own
order: no pagination (the result is already bounded, at most 2×`RecallTopK` before the live
filter) and no type filter. Filtering would make the UI decide something recall did not decide
(OR6). `Searcher` is declared without `ScoredFor`, so the non-admitting net cannot be reached.

**Typed-nil gotcha.** Assigning a nil `*brain.RecallService` to `Deps.Search` produces a
non-nil interface, and calling it panics. `cmd/nooma` builds the struct in a single
function, `uiDeps(…) ui.Deps` (it takes `recall` from PR 3 and gains `capture` in PR 5, §6.1),
which assigns `Search`/`Capture` only when the pointer is non-nil. A nil `Search`/`Capture` then answers
503, `captureHandler`'s posture.

### 3.5 Capture and correction

- `POST /ui/capture`: the body is wrapped in `http.MaxBytesReader(w, r.Body, captureFormMaxBytes)`
  (64 KiB, a named `ui` constant, the same kind of transport bound as login's 4096), then
  `ParseForm`. Empty `text` → 400. The handler calls `Capture(ctx, brain.CaptureInput{Text,
  Channel: "ui"})`. **A submitted `unit_id` is ignored**, so this route never becomes a second
  way to supply a referent.
- `POST /ui/units/{id}/correct` works the same way, except `ReferentID: r.PathValue("id")`. That
  line is the only place `ReferentID` is set in `internal/ui` (gate, §3.7).
  `resolveReferent`'s explicit branch is used unchanged. If the text does not classify as a
  correction, the classifier's outcome stands. That is existing API behaviour, and the UI
  reports it truthfully (OR8).
- The result renders through a total switch over `brain.AllCaptureOutcomes()` with no `default`,
  following `renderCaptureResult`'s precedent (`internal/httpapi/capture.go:166-230`). It never
  shows a field that `CaptureResult` does not carry. On error it answers a generic 500 and
  logs through `slog`, never reflecting the raw error (commit `049c456`).
- htmx: a request carrying `HX-Request: true` gets the fragment; a plain navigation or form post
  gets the full page. `layout.templ`'s `htmx-config` gains `responseHandling` so that 4xx/5xx
  fragments are swapped rather than dropped silently. This is JSON in a `<meta>` tag, so the CSP
  is unchanged (OR7).

### 3.6 Cross-origin (R6): inherited, proven from the wiring table

No new middleware. `TestUINonGETLeavesRefuseCrossOrigin` (conformance) iterates
**`wantUIMuxWiring`'s own rows** whose method is not `GET`. For each row it substitutes `{id}` with
a fixture id and sends two requests through `httpapi.Handler`, using a counting `Capturer`:

- a foreign-origin request (`Sec-Fetch-Site: cross-site`; and, separately, `Origin: http://evil`
  with no `Sec-Fetch-Site`) must answer **403 with zero calls**;
- a same-origin request with the right cookie must reach the handler with **exactly one call**.

The wiring gate forces every `newUIMux` leaf into that table, so **any future non-GET leaf
registered in `newUIMux` is cross-origin-tested by construction**. `POST /ui/login` is covered by
the same test (with a token configured and no counter).

**Scope, stated:** a `POST /ui/…` pattern registered on `Handler`'s *outer* mux would be more
specific than `/ui/`, would bypass `xo`, and would appear in no table. m4a gates none of
`Handler`'s outer registrations. **This is closed in PR 3's commit 0**: the hardened wiring gate
also parses `Handler` and asserts that its outer mux registers exactly `GET /{$}`, `/ui`, `/ui/`
and `/` (§3.7).

### 3.7 Structural gates for invisible properties

| Property | Gate (structural) | Fires on (probe) | Silent on |
|---|---|---|---|
| Every UI registration is checked | **Hardened `TestUIMuxWiringMatchesDeclaredGuardTable`**: `ast.Inspect` walks the whole body of `newUIMux`. Any `*.Handle`/`*.HandleFunc` call whose receiver is not `mux`, or whose pattern is not a string literal, is `t.Fatal`. A sibling check over `Handler` requires its outer registrations to be exactly `{GET /{$}, /ui, /ui/, /}` | `mux.Handle(unitsPattern, guardedUI)`, `mux.HandleFunc("GET /ui/x", h)`, a registration inside a `for`, `mux.Handle("POST /ui/x", h)` in `Handler` | Current `server.go:102-128`, `:151-163` |
| Unbounded browse | Port signature (no limit parameter) + `RunLiveBrowsePage` asserts `len ≤ BrowsePageSize` over `BrowsePageSize+1` seeded units | An implementation that ignores `LIMIT` | The correct implementation |
| Index-backed page | L3 `TestUnitRepo_LiveBrowsePageUsesBrowseIndex`: the plan names `idx_units_live_browse` and has no `USE TEMP B-TREE FOR ORDER BY` | Index dropped; `ORDER BY` on an unindexed column | The index present |
| Browse, search and capture reach only declared entrances (I22, R4) | `TestUIReachesTheBrainOnlyThroughDeclaredEntrances` (conformance), in three parts. (a) reflect over `ui.Deps`: every interface method is in `{Today, Browse, Detail, ForText, Capture}`, and non-interface fields are plain data. (b) AST over non-test `internal/ui` files: no `brain.<X>Service` identifier, no `brain.New…` call, and no type assertion to any brain type. (c) from PR 5: AST over non-test `internal/ui` files walks both composite literals and assignment statements. `ReferentID:` appears in exactly one composite literal, with the value `r.PathValue("id")`; any `*ast.AssignStmt` whose LHS is a `.ReferentID` selector — including compound-assignment forms, wherever the selector appears — fails the gate | A `Lexical ports.LexicalSearch` field; `ScoredFor` added to `Searcher`; `s.(*brain.RecallService).ScoredFor`; `ReferentID: r.FormValue("unit_id")` in capture; `in.ReferentID = r.FormValue("unit_id")` assigned after construction | The §3.4 interfaces |
| Search agrees with `/recall` (I22 behaviour) | `TestI22_BrowseSearchIsTheSameMechanism`: the same `RecallService`, the same raw text, `/ui/units?q=` versus `POST /recall`, ordered ids equal (`data-unit-id`). Below the floor, both are empty. Exactly one `SearchLexical` call per request | A search path that skips admission, or searches twice | `ForText` |
| Every non-GET `newUIMux` leaf is cross-origin-refused | §3.6, driven from the table | The `xo` wrap removed, or moved inside `requireCookie` (a foreign request gets 303, not 403) | Current wrap |
| Views write nothing | `TestUnitsService_ReadsWriteNothing` (brain L2): `UnitRepo`/`RelationRepo` memrepo fakes with every write method overridden to `t.Fatalf`, following I27's shape | A `Detail` that touches or boosts | Pure reads |
| Nil services stay nil | `TestUIDeps_NilServicesStayNilInterfaces` (cmd L1). Weaker than the other rows: a behavioural test of `uiDeps`, not a structural gate — a second `ui.Deps{...}` literal elsewhere in `cmd/nooma` is not caught; accepted because `ui.Deps` is built at exactly one call site | An inline `ui.Deps{Search: recall}` with nil `recall` | `uiDeps` |
| I18 | `TestUnitView_I18ThreeDatesNeverSwap` (ui L1), following `TestTodayView_I18DatesLabelled`'s precedent: `Created:`/`Event:`/`Due:` each keep their own label, and a nil value renders the literal `none` | Labels swapped; one date shown in place of a nil one | Correct template |

"Proven in both directions" means that each GREEN PR body records its probe run (a temporary
mutation applied, the gate failing red, the mutation reverted), following m4a's
`templ-clean` and `ui-boundary` precedent. A probe that never applied is itself a finding.
The body therefore records the failing output, not just the claim.

---

## 4. Data flow

```
browser ─GET /ui/units?type=task&after_created=…&after_id=…─▶ headers ▶ xo ▶ requireCookie ▶ ui.Handler
   ui: parse types (unit.ParseType, 400 on unknown) + cursor ─▶ UnitsService.Browse ─▶ UnitRepo.LiveBrowsePage
   ◀─ rows fragment (HX-Request) | full page; "more" button hx-get with Next cursor

GET /ui/units?q=…      ─▶ ui ─▶ Searcher = RecallService.ForText ─▶ (vector admit + lexical rank) ─▶ LiveByIDs
GET /ui/units/{id}     ─▶ ui ─▶ UnitsService.Detail ─▶ LiveByIDs / ByUnit / LiveByIDs(others) ─▶ 404 | page
POST /ui/capture       ─▶ xo(403 foreign) ─▶ requireCookie(303) ─▶ ui ─▶ Capturer.Capture{Text, "ui"}
POST /ui/units/{id}/correct ─▶ … ─▶ Capturer.Capture{Text, "ui", ReferentID: {id}}
```

## 5. File changes

| File | Action | PR |
|---|---|---|
| `internal/ports/unitrepo.go` | `BrowsePageSize`, `BrowseCursor`, `BrowsePage`, `LiveBrowsePage`; doc comment "Fourteen" | 1 |
| `internal/store/sqlite/unitrepo.go` | `LiveBrowsePage`, `buildLiveBrowsePageQuery` | 1 |
| `internal/store/sqlite/migrations/0005_units_browse_index.sql`; `docs/03-data-model.md` | Create; one index line | 1 |
| `test/support/memrepo/units.go`, `test/support/repocontract/unitrepo.go` (`RunLiveBrowsePage`) | Modify | 1 |
| `testdata/schema/{store_api,ddl,structure}.golden` | Regenerated | 1 |
| `internal/brain/units.go` (+ `units_test.go`) | Create | 2 |
| `cmd/nooma/wiring.go` (`wireUnits`) | Modify | 2 |
| `test/conformance/httpapi_ui_wiring_test.go` | Hardened walk (PR 3); rows added (PR 3, 4, 5) | 3–5 |
| `test/conformance/ui_entrances_test.go` | Create (a, b); part (c) added in PR 5 | 3, 5 |
| `test/conformance/i22_browse_search_test.go` | Create | 3 |
| `test/conformance/i22_recall_one_mechanism_two_entrances_test.go` | Doc comment gains one line naming the third entrance and pointing at `i22_browse_search_test.go` | 3 |
| `internal/ui/ui.go` | Interfaces, `Deps` fields, `r.Pattern` dispatch | 3 (cases added in 4, 5) |
| `internal/ui/units.go`, `units.templ` (+`_templ.go`) | Browse, search, rows fragment | 3 (row anchors in 4) |
| `internal/ui/layout.templ` | Nav "Units" (3), "Capture" + `responseHandling` (5) | 3, 5 |
| `internal/ui/today_test.go:159` | Fixture sets `req.Pattern = "GET /ui"` | 3 |
| `internal/httpapi/server.go` (`newUIMux`) | +1 leaf (3), +1 (4), +3 (5) | 3–5 |
| `cmd/nooma/serve.go` + `uiDeps` | Modify | 3 (Capture assigned in 5) |
| `docs/01-architecture.md:121` "every unit" → "every live unit"; `docs/06-harness.md` §4 I22 row names the `/ui/units` search entrance | Modify | 3 |
| `internal/ui/unit.templ` (+`_templ.go`), detail handler | Create | 4 |
| `internal/ui/capture.go`, `capture.templ` (+`_templ.go`); correction form in `unit.templ` | Create / modify | 5 |
| `test/conformance/ui_cross_origin_test.go` | Create | 5 |

Doc 02 is unchanged. Browse search is "answering a recall" (§5 step 2), and nothing touches
`internal/core/`, so `docs-sync` does not fire. No ADR is needed.

---

## 6. The PR chain (stacked-to-main)

Each PR puts the RED test commit ahead of GREEN; no tip is red. Budgets are impl+docs. Tests,
generated `_templ.go` files and goldens are reported beside the budget, not inside it.

| # | Branch | RED | GREEN | Impl+docs |
|---|---|---|---|---|
| 1 | `feat/ports-store-units-browse` | `RunLiveBrowsePage` against memrepo (port method + a memrepo stub returning an empty page: compiles, fails on assertions). It asserts: I02 excludes `archived`/`superseded`/`incomplete`; the type filter; empty types means all types; order; ties broken by id; page ≤ `BrowsePageSize`; no id on two pages and the union equals the live set; `Next` nil on the last page; a cursor at a unit archived since the previous page still resumes. L3: the same run on SQLite plus the plan test | Port, SQL, memrepo, migration 0005, doc 03, goldens | ~200 |
| 2 | `feat/brain-units-read` | `TestUnitsService_BrowsePassesThrough`, `…_DetailNotFoundForNonLive`, `…_DetailDropsNonLiveNeighbours`, `…_ReadsWriteNothing` | `units.go`, `wireUnits` + `TestWireUnits_BuildsAWorkingService` (m4a `wireToday` precedent: a function with no call site until PR 3) | ~150 |
| 3 | `feat/ui-units-browse` | Commit 0 (test-only): the hardened wiring gate, with its probe recorded. RED: wiring row `GET /ui/units`; `ui_entrances_test` (a, b); `TestI22_BrowseSearchIsTheSameMechanism`; `TestUnitsView_{OnePageWithNextLink, FragmentOnHXRequest, UnknownTypeIs400, HalfCursorIs400, MalformedCursorIs400, SearchRendersRecallOrder, NilSearchIs503}`; `TestUIGuardedLeavesEachReachAView`; `TestUIDeps_NilServicesStayNilInterfaces`; L4 `TestServeUIUnitsListsACapturedUnit` | Interfaces, dispatch, `units.go`/`.templ`, nav, 1 leaf, `uiDeps`, doc 01/06 | ~270. **Overflow**: search (the `q` branch, `Searcher`, and the I22 test) splits into 3b |
| 4 | `feat/ui-unit-detail` | Wiring row `GET /ui/units/{id}`; `TestUnitView_{I18ThreeDatesNeverSwap, NotFoundIs404, RendersLiveRelations}`; `TestUnitsView_RowsLinkToDetail` | `unit.templ`, handler, 1 leaf, row anchors | ~150 |
| 5 | `feat/ui-capture-correct` | Three wiring rows; `ui_cross_origin_test`; `ui_entrances_test` (c), red on its own vacuity guard: zero `ReferentID` literals or assignments found; `TestCaptureView_{CallsCaptureOnceWithUIChannel, IgnoresSubmittedUnitID, RendersEveryOutcome, BodyIsBounded, NilCapturerIs503}`; `TestCorrectView_SetsReferentFromPath`; `TestUIMutationsWithoutCookieNeverReachCapture` (303, zero calls); L4 `TestServeUICaptureStoresAUnit` | `capture.go`/`.templ`, correction form, 3 leaves, nav, `responseHandling`, `uiDeps` assigns `Capture`; `internal/httpapi/cookie.go`'s `requireCookie` doc comment, updated — PR 5's POST routes make its "non-GET never reaches the middleware" claim false | ~240. **Overflow**: the correction route splits into 5b |

Total is about 1,010 lines across five PRs. Order is 1 → 2 → 3 → 4 → 5. PR 3 is the one to
watch against the measured 1.3×–2.2× overrun. This is lower than the umbrella proposal's §5.1
`m4b` guess of roughly 1,600 lines (excluding the dropped cross-origin PR) because
`feat/httpapi-cross-origin` was folded into R6's inherited-wrap tests instead of shipping as a
standalone PR (spec OQ2), and browse search rides inside PR 3 rather than its own PR; the
umbrella's documented 1.3×–2.2× historical underestimate still applies to this lower figure,
which is why each PR above carries its own named overflow cut instead of treating ~1,010 as a
hard ceiling.

### 6.1 Symbol → PR (no compiled reference precedes the PR that creates it; the one exception is the string `"Capture"` in PR 3's entrances whitelist, a test-file literal until PR 5)

| Symbol / artifact | Created | Later modified |
|---|---|---|
| `ports.BrowsePageSize`, `BrowseCursor`, `BrowsePage`, `UnitRepo.LiveBrowsePage` | 1 | — |
| `sqlite.(*UnitRepo).LiveBrowsePage`, `buildLiveBrowsePageQuery`, `0005_…sql`, `idx_units_live_browse` | 1 | — |
| `memrepo.(*Units).LiveBrowsePage`, `repocontract.RunLiveBrowsePage`, `TestUnitRepo_LiveBrowsePageUsesBrowseIndex` | 1 | — |
| `brain.UnitsService`, `NewUnitsService`, `UnitDetail`, `RelatedUnit`; `wireUnits` | 2 | — |
| Hardened wiring walk | 3 | rows added in 4, 5 |
| `ui.UnitsReader`, `ui.Searcher`, `Deps.Units`, `Deps.Search`, `r.Pattern` dispatch | 3 | cases added in 4, 5 |
| `ui.Capturer`, `Deps.Capture`, `captureFormMaxBytes` | **5** | — |
| `uiDeps` (cmd/nooma) | 3: `uiDeps(today, units, recall, serving)` | 5 gains a `capture` parameter and assigns `Capture` |
| `ui_entrances_test` (a, b) | 3; the whitelist names `Capture` from PR 3, which is harmless while unused | (c) in 5 |
| `TestI22_BrowseSearchIsTheSameMechanism`, `TestUIGuardedLeavesEachReachAView` | 3 | leaves added in 4, 5 |
| `unit.templ`, detail handler | 4 | correction form in 5 |
| `capture.templ`, `capture.go`, `ui_cross_origin_test` | 5 | — |

### 6.2 Tip behaviour (no tip regresses a shipped route)

| Tip | `/ui` (Today), `/ui/login`, static, API | `GET /ui/units` | `GET /ui/units/{id}` | `/ui/capture`, `POST …/correct` | Test changed to stay green |
|---|---|---|---|---|---|
| 1, 2 | Unchanged | 404 from `uiMux`, token or not: no leaf matches, so `requireCookie` never runs | 404 | 404 (a foreign-origin POST gets 403, because `xo` runs before the mux) | None |
| 3 | Unchanged | 200 page/fragment; search 503 if unconfigured; token without cookie → 303 | 404 (rows carry no link yet) | 404 (foreign-origin POST 403, as tips 1, 2) | `today_test.go:159` sets `req.Pattern` |
| 4 | Unchanged | 200, rows link to detail | 200 live / 404 non-live | 404 (foreign-origin POST 403, as tips 1, 2) | None |
| 5 | Unchanged | Unchanged | 200 + correction form | GET 200; POST 200 same-origin with cookie; 403 foreign; 303 no cookie; 503 if unconfigured | None |

`--no-ui` at every tip: every `/ui/*` path falls through to `requireToken`, 404/401, m4a §3.9.

---

## 7. Testing strategy

Levels follow `docs/06-harness.md` §3. `ui`/`httpapi` handlers are L1. `repocontract`/memrepo
and the brain service are L2. Gates live in `test/conformance` (L2). SQLite and the plan are L3.
The binary is L4 (browse lists an API-captured unit; a UI capture then appears in browse).
Every template test asserts structure (`data-unit-id`, labels, `hx-get` query), never a page
golden. `<script>` content is asserted escaped in both new views. No browser, no network, no
real LLM: e2e uses its existing fake providers.

## 8. Risks

| # | Risk | Mitigation |
|---|---|---|
| N1 | The planner ignores the index for the row-value predicate | The L3 plan test decides; the fallback is the expanded predicate (§3.2) |
| N2 | `r.Pattern` dispatch drifts from `newUIMux` | `TestUIGuardedLeavesEachReachAView`; the default arm returns 404, never Today |
| N3 | `ByUnit` is unbounded per unit | Named. Relations per unit are few; the m4d neighbourhood bound is the real cap |
| N4 | An htmx POST without a cookie follows the 303 and swaps the login form into the target | Accepted (OR5). No effect occurs; this is asserted |
| N5 | A correction the classifier reads as a new capture stores a new unit | Existing API semantics, rendered as the real outcome (OR8) |
| N6 | Estimates overrun | Each PR carries a named overflow cut |
| N7 | `Handler`'s outer mux registrations were ungated. A `/ui/…` leaf placed there would bypass `xo` and `requireCookie` (§3.6) | Closed in PR 3 commit 0 (§3.7, first row) |
| N8 | The browse index `idx_units_live_browse` is `(status, created_at, id)`, with no `type` column. A narrow `type` filter on a type-skewed vault still walks every live row in `created_at` order before discarding rows of the wrong type, so a page can cost many more index rows than it returns | Accepted for M4 personal-vault sizes. Revisit with `(status, type, created_at, id)` if measured |

## 9. Owner-review items (a default ships if the owner says nothing)

| # | Item | Default |
|---|---|---|
| OR1 | Migration 0005, against umbrella §3.2's "no migration" | Ship it. Reverting it costs only speed: the SQL, contract and semantics stay the same, and the plan test is deleted with it |
| OR2 | `BrowsePageSize = 50` lives in `ports` and is not a §13 row | Transport bound, like login's 4096 |
| OR3 | Detail shows stored weight, not effective weight | **Settled in spec R3.** Stored, and labelled as stored |
| OR4 | Detail reads `UnitRepo.LiveByIDs` rather than `RecallService.LiveByIDs` | **Settled in spec R3.** The same filter, and it works without providers |
| OR5 | Unauthenticated non-GET stays 303 | `requireCookie` untouched |
| OR6 | Search ignores the type filter and pagination | Recall's order is shown unfiltered |
| OR7 | htmx swaps 4xx/5xx fragments | `responseHandling` in the layout meta |
| OR8 | The correction route cannot force a classification | Unchanged `Capture` semantics |

No open question blocks `sdd-tasks`.
