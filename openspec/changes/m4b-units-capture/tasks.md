# Tasks — m4b: units and capture

Implementation task list for `m4b-units-capture`, derived from `spec.md` (R1–R6, read in full)
and `design.md` (§1–§9, read in full, verified against the tree at `48cac56` — design's own §1
ground-truth table), the second of the six chained changes `openspec/changes/m4-mirror-ui/proposal.md`
splits M4 into. Design §6 fixes the slicing — **five PRs**, ~1,010 budgeted impl+docs lines,
treated as authoritative over any disagreement with spec's own wording (`m3e`/`m4a` precedent:
"design owns the slicing"). Design.md is **APPROVED after three Judgment Day rounds**; it is not
re-derived here, only sliced into checkable tasks.

**Inputs**: `spec.md` (R1–R6, this change's own scope boundary); `design.md` §1–§9 (verified
`48cac56`); `openspec/changes/m4-mirror-ui/proposal.md` §5, §5.1, §6 order 5.

**Delivery parameters** (cached this session): delivery `ask-on-risk`, chain strategy
**`stacked-to-main`** — every branch targets `main` directly and merges in order; the next
branch rebases on `main` after the previous merge (design §6's own fixed order, matching m4a's
precedent). Soft ceiling 400 impl+docs lines per PR (tests, generated `_templ.go` and schema
goldens counted and reported separately, never against the ceiling — `docs/06-harness.md` §7).
Strict TDD is active: every behavioral task states its RED commit strictly ahead of its GREEN
commit inside the same PR; no PR tip is deliberately red — `main`'s ruleset has no bypass.
`make check` between commits; `make check-all` before opening each PR, run **at that PR's own
commit in an isolated worktree** (`git worktree add`) — a branch tip being green says nothing
about whether the combined tree up to that point still builds (verify the link, not the tip).
Branch names are design's exact `feat/...` names. Conventional commits, no AI-attribution
trailer of any kind.

**The five branches, order 1 → 2 → 3 → 4 → 5** (design §6's own fixed order):

| # | Branch | Depends on |
|---|---|---|
| 1 | `feat/ports-store-units-browse` | nothing (touches `ports`/`store` only) |
| 2 | `feat/brain-units-read` | 1 (`UnitsService.Browse` calls `LiveBrowsePage`) |
| 3 | `feat/ui-units-browse` | 2 (`ui.UnitsReader`/`Searcher` wrap `UnitsService`/`RecallService`) |
| 4 | `feat/ui-unit-detail` | 3 (adds a case to PR 3's `r.Pattern` dispatch, a row to PR 3's wiring table) |
| 5 | `feat/ui-capture-correct` | 4 (chain order fixed by design §6; capture does not read `UnitsService`, but the wiring table and `uiDeps` it extends are PR 3/4's) |

PR 3 is the one to watch against the measured 1.3×–2.2× historical overrun (design §6); every
PR below carries its own named overflow cut, not only PR 3.

---

## Cross-PR items (named once, tasked at their PR)

- **Hardened UI wiring gate** (closes m4a's non-literal-pattern/`HandleFunc`/loop/outer-mux
  holes, design N7) — PR 3, task 3.0, as a test-only commit ahead of any PR 3 GREEN.
- **I22** (one recall mechanism) gets its third entrance's doc-comment line and its own
  conformance test in PR 3 — tasks 3.1, 3.3.
- **I02** (live-only) is enforced once, at `UnitRepo.LiveBrowsePage`/`LiveByIDs`, never
  reimplemented — PR 1 (browse), PR 2 (`UnitsService.Detail`'s neighbour drop).
- **I18** (three distinct dates) gets its conformance test in PR 4 — task 4.1.
- Doc 02 is unchanged by this slice; no `docs-sync` fire; no new ADR.

---

## PR 1 — `feat/ports-store-units-browse` (~200 impl+docs; risk: Low)

**Overflow cut** (if over 400): defer the EXPLAIN QUERY PLAN fallback form (the expanded
`created_at < ? OR (...)` predicate) to a follow-up commit inside this same PR, landing only if
the L3 plan test (task 1.4) actually requires it — the row-value predicate is the default, not
assumed to fail.

- [x] **1.1** RED — `internal/ports/unitrepo.go`: add `BrowsePageSize = 50`,
      `BrowseCursor{CreatedAt time.Time, ID string}`, `BrowsePage{Units []unit.Unit, Next
      *BrowseCursor}`, `LiveBrowsePage(ctx, types []unit.Type, after *BrowseCursor) (BrowsePage,
      error)` (signature only); doc comment "Fourteen" methods.
      `test/support/repocontract/unitrepo.go`: `RunLiveBrowsePage(t, repo)` asserting — I02
      excludes `archived`/`superseded`/`incomplete`; empty `types` means all, a non-empty set
      narrows; order `created_at DESC, id DESC` with ties broken by id; a page never exceeds
      `BrowsePageSize`; no unit id appears on two pages and the pages' union equals the live set;
      `Next` is nil on the last page; a cursor taken from a unit archived since the previous page
      still resumes correctly.
      `test/support/memrepo/units.go`: `LiveBrowsePage` stub that always returns an empty
      `BrowsePage{}`. Compiles; `RunLiveBrowsePage` run against memrepo fails on assertions (the
      stub is deliberately wrong, not the test).
      Mutation: none — this commit's own stub is the red state, no separate probe needed.
      Requirement: R1.
- [x] **1.2** GREEN — `test/support/memrepo/units.go`: real `LiveBrowsePage` over the in-memory
      fixture — filters `status == pool`, the type set, orders by `(created_at, id)` descending,
      compares the keyset cursor, bounds the result to `BrowsePageSize+1` to detect `Next`.
      `RunLiveBrowsePage` green against memrepo.
      Verify: `go test ./test/support/... ./test/conformance/...`.
      Requirement: R1.
- [x] **1.3** GREEN — `internal/store/sqlite/unitrepo.go`: `LiveBrowsePage` +
      `buildLiveBrowsePageQuery(types, after)` (`status = ?` bound to `pool`; `type IN (...)`
      when `types` is non-empty; row-value predicate `(created_at, id) < (?, ?)`; `LIMIT
      BrowsePageSize+1`). `internal/store/sqlite/migrations/0005_units_browse_index.sql`:
      `CREATE INDEX idx_units_live_browse ON units(status, created_at, id);`.
      `docs/03-data-model.md`: mirror the new index. Regenerate
      `testdata/schema/{store_api,ddl,structure}.golden`.
      Verify: `go test ./internal/store/...`; `make check` (schema-golden diff clean).
      Requirement: R1; design §3.2.
- [x] **1.4** RED→GREEN — L3: `TestUnitRepo_LiveBrowsePageUsesBrowseIndex` — EXPLAIN QUERY PLAN
      over `buildLiveBrowsePageQuery` names `idx_units_live_browse`, no `USE TEMP B-TREE FOR
      ORDER BY`.
      Mutation: drop the index, or reorder `ORDER BY` onto an unindexed column — the plan test
      must fail red on that mutation, reverted, and the failing output recorded in the PR body.
      Requirement: design §3.7 row "Index-backed page".

**Verify (PR-level)**: `RunLiveBrowsePage` green over both memrepo and SQLite; L3 plan test
green; `make check-all` in an isolated worktree at this branch's tip. Open
`feat/ports-store-units-browse` against `main`; merge only on `mergeStateStatus: CLEAN`; confirm
branch deletion before branching PR 2. Target ≤200 impl+docs lines.

---

## PR 2 — `feat/brain-units-read` (~150 impl+docs; risk: Low)

**Overflow cut** (if over 400): defer `TestUnitsService_ReadsWriteNothing`'s full I27-shaped
fake (every write method overridden) to a minimal subset covering only the methods `Browse`/
`Detail` actually call — the full fake is a completeness nicety, not required for the property.

- [x] **2.1** RED — `internal/brain/units_test.go`: `TestUnitsService_BrowsePassesThrough`
      (`Browse` forwards `types`/`after` to `UnitRepo.LiveBrowsePage` unchanged, returns its
      result unchanged), `TestUnitsService_DetailNotFoundForNonLive` (`LiveByIDs` returning an
      empty slice yields `found=false`), `TestUnitsService_DetailDropsNonLiveNeighbours` (one
      `ByUnit` call, one `LiveByIDs(otherIDs)` call, a relation whose other endpoint is not live
      is dropped from `Detail`'s result), `TestUnitsService_ReadsWriteNothing` (memrepo fakes
      with every write method `Browse`/`Detail` could reach overridden to `t.Fatalf`, I27 shape).
      `internal/brain/units.go`: `UnitsService{}`, `NewUnitsService`, `Browse`, `Detail`
      signatures only, returning zero values — compiles, fails on assertions.
      Mutation: none — the zero-value stub is the red state.
      Requirement: R1, R3.
- [x] **2.2** GREEN — `internal/brain/units.go`: `NewUnitsService(units ports.UnitRepo, rels
      ports.RelationRepo) *UnitsService` — no `ports.Clock` field. `Browse` delegates to
      `UnitRepo.LiveBrowsePage`. `Detail`: one `LiveByIDs([id])` call (`found=false` on empty
      result), one `ByUnit(id)` call, one `LiveByIDs(otherIDs)` call, drops every relation whose
      other endpoint is not live. `UnitDetail{Unit unit.Unit, Relations []RelatedUnit}`;
      `RelatedUnit{RelationID, Type string, Outgoing bool, Confidence float64, Other
      unit.Unit}`. Renders the stored `Weight` only — no clock read, no effective-weight call
      (OR3).
      Verify: `go test ./internal/brain/...`.
      Requirement: R1, R3; design §3.3.
- [x] **2.3** RED — `cmd/nooma/wiring_units_test.go` (new file, matching
      `wiring_today_test.go`'s precedent): `TestWireUnits_BuildsAWorkingService` — fails,
      `wireUnits` undefined.
      Requirement: design §3.3, §6.1.
- [x] **2.4** GREEN — `cmd/nooma/wiring.go`: `wireUnits(db) *brain.UnitsService`, wired
      unconditionally at vault open like `wireToday` (browsing needs no provider) — no call site
      until PR 3 (`wireToday`'s own m4a precedent).
      Verify: `go test ./cmd/nooma/...`.
      Requirement: design §3.3.

**Verify (PR-level)**: brain package and wiring tests green; `UnitsService` touches no
`ports.Clock` field or method; `make check-all` in an isolated worktree at this branch's tip.
Open `feat/brain-units-read` against `main`; merge only on `mergeStateStatus: CLEAN`; confirm
branch deletion before branching PR 3. Target ≤150 impl+docs lines.

---

## PR 3 — `feat/ui-units-browse` (~270 impl+docs; risk: High — watch against the 1.3×–2.2× overrun)

**Overflow cut** (if over 400): the search branch (`q` param, `ui.Searcher`,
`i22_browse_search_test.go`, `TestUnitsView_SearchRendersRecallOrder`/`NilSearchIs503`) splits
into 3b, landing immediately after 3.

- [ ] **3.0** RED — commit 0, test-only. `test/conformance/httpapi_ui_wiring_test.go`: harden
      `TestUIMuxWiringMatchesDeclaredGuardTable`'s `ast.Inspect` walk to cover the **whole**
      `newUIMux` body (any `.Handle`/`.HandleFunc` call whose receiver is not `mux`, or whose
      pattern is not a string literal, is `t.Fatal` — closes the non-literal-pattern/
      `HandleFunc`/loop holes); add a sibling check over `Handler` requiring its outer
      registrations to be exactly `{GET /{$}, /ui, /ui/, /}`.
      Mutation (probe each, record failing, then revert): `mux.Handle(unitsPattern, guardedUI)`
      with a variable pattern; `mux.HandleFunc("GET /ui/x", h)`; a registration inside a `for`;
      `mux.Handle("POST /ui/x", h)` registered inside `Handler` instead of `newUIMux`. The
      hardened gate itself stays green against the current, unwidened tree — the probes prove it
      now discriminates what m4a's narrower walk missed, not that current code is wrong.
      Requirement: design §3.6 (N7), §3.7 row 1.
- [ ] **3.1** RED — `httpapi_ui_wiring_test.go`: `wantUIMuxWiring` gains the `GET /ui/units` row
      (guarded). `test/conformance/ui_entrances_test.go` (new): part (a) reflects over
      `ui.Deps` — every interface method is in `{Today, Browse, Detail, ForText}` (`Capture`
      named later, harmless while unused); part (b) AST-walks non-test `internal/ui` files — no
      `brain.<X>Service` identifier, no `brain.New...` call, no type assertion to any brain type.
      `test/conformance/i22_browse_search_test.go` (new): `TestI22_BrowseSearchIsTheSameMechanism`
      — same `RecallService`, same raw text, `/ui/units?q=` vs `POST /recall`, ordered ids equal;
      below the admission floor both are empty; exactly one `SearchLexical` call per request.
      `internal/ui/units_test.go` (new): `TestUnitsView_{OnePageWithNextLink, FragmentOnHXRequest,
      UnknownTypeIs400, HalfCursorIs400, MalformedCursorIs400, SearchRendersRecallOrder,
      NilSearchIs503}`. `internal/httpapi/server_test.go`: `TestUIGuardedLeavesEachReachAView`
      (stubbed readers, each guarded leaf reaches its own view marker, not 404).
      `cmd/nooma/serve_test.go`: `TestUIDeps_NilServicesStayNilInterfaces` (behavioural, `uiDeps`).
      Fails to compile/fails assertions — `ui.UnitsReader`, `Searcher`, `units.go` don't exist yet.
      Requirement: R1, R2; design §3.1, §3.4, §3.7.
- [ ] **3.2** GREEN — `internal/ui/ui.go`: `UnitsReader`, `Searcher` interfaces; `Deps` gains
      `Units`, `Search` fields; `ServeHTTP` switches on `r.Pattern`, default arm 404.
      `internal/ui/units.go` + `units.templ` (+`_templ.go`): `Browse` parses `unit.ParseType` per
      type (400 on unknown), parses the `(after_created, after_id)` cursor (both-or-neither
      required, `after_created` must be RFC3339, either key present with an empty value is 400),
      renders the rows fragment on `HX-Request`, "more" link is `hx-get` carrying `Next`; search
      mode calls `Searcher.ForText` (no pagination, no type filter — OR6).
      `internal/ui/layout.templ`: nav "Units" link.
      `internal/httpapi/server.go` (`newUIMux`): +1 guarded leaf `GET /ui/units` → `d.UI`.
      `cmd/nooma/serve.go` + `uiDeps(today, units, recall, serving)`: assigns `Search` only when
      the `*brain.RecallService` pointer is non-nil (typed-nil gotcha, §3.4); nil `Search` → 503.
      Verify: `go test ./internal/ui/... ./internal/httpapi/... ./cmd/nooma/...`.
      Requirement: R1, R2; design §3.1–§3.4.
- [ ] **3.3** GREEN — `internal/ui/today_test.go:159`: fixture sets `req.Pattern = "GET /ui"`
      (kept green under the new switch-on-pattern dispatch). `docs/01-architecture.md:121`:
      "every unit" → "every live unit". `docs/06-harness.md` §4: I22 row names the `/ui/units`
      search entrance. `test/conformance/i22_recall_one_mechanism_two_entrances_test.go`: doc
      comment gains one line naming the third entrance and pointing at
      `i22_browse_search_test.go`.
      Requirement: I02, I22 doc parity.
- [ ] **3.4** GREEN — L4: `test/e2e`: `TestServeUIUnitsListsACapturedUnit` (a unit captured via
      the API appears in `/ui/units`).
      Verify: `go test -tags=e2e ./test/e2e/... -run TestServeUIUnitsListsACapturedUnit`.
      Requirement: exit criterion; design §7.

**Verify (PR-level)**: `GET /ui/units` renders a page and an htmx fragment; search below the
admission floor returns no results, identically to `/recall`; unauthenticated GET/foreign-origin
POST posture unchanged from PR 2's tip (design §6.2 tip table, row 3); `make check-all` in an
isolated worktree at this branch's tip. Open `feat/ui-units-browse` against `main`; merge only
on `mergeStateStatus: CLEAN`; confirm branch deletion before branching PR 4. Target ≤270
impl+docs lines.

---

## PR 4 — `feat/ui-unit-detail` (~150 impl+docs; risk: Low)

**Overflow cut** (if over 400): defer `TestUnitsView_RowsLinkToDetail` to PR 5 — it is a
convenience assertion on PR 3's own rows fragment, not required for R3 itself.

- [ ] **4.1** RED — `httpapi_ui_wiring_test.go`: `wantUIMuxWiring` gains the `GET
      /ui/units/{id}` row (guarded). `internal/ui/unit_test.go` (new):
      `TestUnitView_I18ThreeDatesNeverSwap` (`Created:`/`Event:`/`Due:` each keep their own
      label, a nil value renders the literal `none`), `TestUnitView_NotFoundIs404` (archived,
      superseded, incomplete or absent id answers the same 404 class as `GET /units/{id}`, via
      `UnitsService.Detail`'s `found=false`), `TestUnitView_RendersLiveRelations` (a relation to
      a non-live unit is omitted). `internal/ui/units_test.go`: `TestUnitsView_RowsLinkToDetail`.
      Fails — `unit.templ`, the detail handler, and row anchors don't exist yet.
      Requirement: R3.
- [ ] **4.2** GREEN — `internal/ui/unit.templ` (+`_templ.go`) + a detail handler in
      `internal/ui/units.go`: renders the unit's content, type, stored `Weight` labelled "stored
      weight", `Relations` via `UnitsService.Detail`, `Created:`/`Event:`/`Due:` as three
      distinct labels, nil `EventAt`/`DueAt` → literal "none"; 404 on `found=false`.
      `internal/httpapi/server.go` (`newUIMux`): +1 guarded leaf `GET /ui/units/{id}` → `d.UI`.
      `internal/ui/units.templ`: row anchors linking each row to its detail path.
      Verify: `go test ./internal/ui/... ./internal/httpapi/...`.
      Requirement: R3; design §3.1, §3.3.

**Verify (PR-level)**: `GET /ui/units/{id}` is 200 for a live id, 404 for archived/superseded/
incomplete/absent; rows link to detail; foreign-origin POST posture unchanged from PR 3's tip
(still 403 via inherited `xo`); `make check-all` in an isolated worktree at this branch's tip.
Open `feat/ui-unit-detail` against `main`; merge only on `mergeStateStatus: CLEAN`; confirm
branch deletion before branching PR 5. Target ≤150 impl+docs lines.

---

## PR 5 — `feat/ui-capture-correct` (~240 impl+docs; risk: Medium)

**Overflow cut** (if over 400): the correction route (`TestCorrectView_SetsReferentFromPath`,
`unit.templ`'s correction form, its wiring row) splits into 5b, landing immediately after 5 —
the capture form alone still satisfies R4 and R6's capture scenario.

- [ ] **5.1** RED — `httpapi_ui_wiring_test.go`: `wantUIMuxWiring` gains `GET /ui/capture`,
      `POST /ui/capture`, `POST /ui/units/{id}/correct` (all guarded).
      `test/conformance/ui_cross_origin_test.go` (new): `TestUINonGETLeavesRefuseCrossOrigin` —
      iterates `wantUIMuxWiring`'s own non-GET rows; foreign-origin (`Sec-Fetch-Site:
      cross-site`; separately `Origin: http://evil` with no `Sec-Fetch-Site`) → 403 with zero
      `Capturer` calls; same-origin with the right cookie → exactly one call; `POST /ui/login`
      covered by the same test (token configured, no counter).
      `ui_entrances_test.go`: part (c) — AST walk over non-test `internal/ui` files:
      `ReferentID:` appears in exactly one composite literal with value `r.PathValue("id")`; any
      `*ast.AssignStmt` whose LHS is a `.ReferentID` selector (including compound-assignment
      forms) fails the gate.
      Mutation (probe each, record failing, then revert): a `Lexical ports.LexicalSearch` field
      on `Deps`; `ScoredFor` added to `Searcher`; a type assertion to `*brain.RecallService`
      calling `ScoredFor`; `ReferentID: r.FormValue("unit_id")` in the capture handler;
      `in.ReferentID = r.FormValue("unit_id")` assigned after construction. Part (c)'s gate
      itself is red first on its own vacuity guard — zero `ReferentID` literals or assignments
      exist in the tree before this PR's code lands — recorded, not silent (PR 3/4 precedent).
      Requirement: R4, R5, R6.
- [ ] **5.2** RED — `internal/ui/capture_test.go` (new): `TestCaptureView_{CallsCaptureOnceWithUIChannel,
      IgnoresSubmittedUnitID, RendersEveryOutcome, BodyIsBounded, NilCapturerIs503}`.
      `internal/ui/unit_test.go`: `TestCorrectView_SetsReferentFromPath`.
      `internal/httpapi/server_test.go`: `TestUIMutationsWithoutCookieNeverReachCapture` (303,
      zero calls). L4: `test/e2e`: `TestServeUICaptureStoresAUnit`.
      Fails — `capture.go`, `capture.templ`, and the correction form don't exist yet.
      Requirement: R4, R5, R6.
- [ ] **5.3** GREEN — `internal/ui/capture.go` + `capture.templ` (+`_templ.go`): `ui.Capturer`
      interface, `Deps.Capture` field, `captureFormMaxBytes` constant (64 KiB,
      `http.MaxBytesReader`); `POST /ui/capture` builds `CaptureInput{Text, Channel: "ui"}` (a
      submitted `unit_id` is ignored) and calls `Capturer.Capture`; renders through a total
      switch over `brain.AllCaptureOutcomes()` with no `default`; generic 500 + `slog` on error,
      never reflects the raw error.
      Correction form in `unit.templ` + handler: `POST /ui/units/{id}/correct` sets
      `ReferentID: r.PathValue("id")` — the only place `ReferentID` is set in `internal/ui`.
      `internal/ui/layout.templ`: nav "Capture" link; htmx-config meta gains `responseHandling`
      so 4xx/5xx fragments swap (OR7).
      `internal/httpapi/server.go` (`newUIMux`): +3 guarded leaves.
      `cmd/nooma/serve.go` + `uiDeps`: gains a `capture` parameter, assigns `Capture` only when
      non-nil; nil `Capture` → 503.
      `internal/httpapi/cookie.go`: `requireCookie`'s doc comment updated — this PR's `POST`
      routes make its "non-GET never reaches the middleware" claim false.
      Verify: `go test ./internal/ui/... ./internal/httpapi/... ./cmd/nooma/... ./test/conformance/...`.
      Requirement: R4, R5, R6; design §3.5.
- [ ] **5.4** GREEN — L4: `test/e2e`: `TestServeUICaptureStoresAUnit` (a UI capture then appears
      in `/ui/units`).
      Verify: `go test -tags=e2e ./test/e2e/... -run TestServeUICaptureStoresAUnit`.
      Requirement: exit criterion.

**Verify (PR-level)**: `/ui/capture` GET 200, POST 200 same-origin with cookie, 403
foreign-origin, 303 no cookie, 503 if unconfigured; correction POST sets `ReferentID` from the
path and never reaches the scored-candidate margin gate; `make check-all` in an isolated
worktree at this branch's tip. Open `feat/ui-capture-correct` against `main`; merge only on
`mergeStateStatus: CLEAN`. This is the chain's last PR — after merge, re-run the spec's full
exit criterion end to end. Target ≤240 impl+docs lines.

---

## Review Workload Forecast

| Field | Value |
|-------|-------|
| Estimated changed lines | ~1,010 impl+docs across 5 PRs (design §6): PR1 ~200, PR2 ~150, PR3 ~270, PR4 ~150, PR5 ~240 — tests, `_templ.go` and goldens counted separately |
| 400-line budget risk | Medium (each PR's estimate is under 400, but the project's own measured 1.3×–2.2× historical overrun could push PR 3, the largest, toward 350–590) |
| Chained PRs recommended | Yes |
| Suggested split | PR 1 → PR 2 → PR 3 → PR 4 → PR 5, each with a named overflow cut (3b for search, 5b for correction) |
| Delivery strategy | ask-on-risk |
| Chain strategy | stacked-to-main |

Decision needed before apply: Yes
Chained PRs recommended: Yes
Chain strategy: stacked-to-main
400-line budget risk: Medium

### Suggested Work Units

| Unit | Goal | Likely PR | Notes |
|------|------|-----------|-------|
| 1 | Bounded, filterable, paginated live-unit read at the port/store layer | PR 1 | Base `main`; no dependency; migration 0005 included |
| 2 | `brain.UnitsService` read model (`Browse`, `Detail`) | PR 2 | Base `main` after PR 1 merges; depends on PR 1's port method |
| 3 | `/ui/units` browse view + search via `RecallService` + hardened wiring gate | PR 3 | Base `main` after PR 2 merges; depends on PR 2's service; **overflow candidate 3b (search)** |
| 4 | `/ui/units/{id}` detail view (I18 dates, relations) | PR 4 | Base `main` after PR 3 merges; depends on PR 3's dispatch/wiring table |
| 5 | `/ui/capture` + correction form, cross-origin proof | PR 5 | Base `main` after PR 4 merges; depends on PR 3/4's `uiDeps`/wiring table; **overflow candidate 5b (correction)** |

Note: since chain_strategy (`stacked-to-main`) and PR boundaries are already fixed by the
APPROVED design (§6, three Judgment Day rounds) and confirmed by the user for this session, the
"Decision needed" flag above is the `ask-on-risk` default rather than an open question — the
real decision point that remains is whether to accept PR 3's or PR 5's named overflow cut if
either PR's measured impl+docs exceeds 400 lines at apply time.
