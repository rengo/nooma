# Tasks — m4e2: admin

Derived from `spec.md` (R7-R11) and `design.md` (§1-§8, APPROVED after Judgment Day round 3;
**§10 carries the round-3 corrections, applied below**). Split from
[`m4e-beliefs-activity-admin`](../m4e-beliefs-activity-admin/tasks.md) on 2026-10-08; **starts
after `m4e-activity`'s PR 6 merges** (which itself follows m4e's PR 4; activity split off m4e on
2026-10-08, so `DecisionLog.Before` and the change decoder are `m4e-activity`'s; `WriteLandedError`,
the I22 whitelist and the `uiDeps` pattern stay m4e's). Shape follows the archived m4c and m4b tasks.

**Delivery**: `ask-on-risk`, `stacked-to-main` (each branch targets `main`, rebases after the
previous merge). Strict TDD per PR: scaffold (compiles, lints, no behaviour) -> RED (fails on an
assertion, never on "undefined") -> GREEN -> docs -> mutation probes enumerated from the
**production diff**, each with its killing test from design §6. No PR tip is red. Every PR runs
`make check`, then before opening `make check-all` **and** `go vet -tags integration,e2e ./...`,
in an isolated `git worktree` at that PR's own commit. Conventional commits, no AI trailer. Budgets
are impl+docs; tests reported apart. Each GREEN commit body records its probe.

## Review Workload Forecast

Impl+docs excludes tests, `test/support/**`, the regenerated golden and `*_templ.go`. Test lines
are estimates, reported apart.

| PR | Branch | Impl+docs (point) | x 1.3 | Tests (apart) | Cut seam (none needed at x 1.3) |
|----|--------|------|------|------|------|
| 1a | `feat/config-setters-learned-thresholds` | ~140 | ~182 | ~220 | none (port + store only) |
| 1b | `feat/brain-admin-service` | ~190 | ~247 | ~380 | `View` (read) / `Update` (door); share only `ConfigField` |
| 2 | `feat/ui-admin` | ~275 | ~358 | ~420 | GET view + `wireAdmin` / POST + R10 pin |
| | **Total (3 PRs)** | **~605** | **~787** | **~1,020** | |

Sensitivity (not the umbrella's convention): x 1.8 = ~1,089, x 2.2 = ~1,331. Per design §10 item 7:
**PR 2 exceeds 400 at x 1.8 (~495) and x 2.2 (~605); 1b exceeds only at x 2.2 (~418); 1a never
(~308 at x 2.2).** Every PR is under 400 at x 1.3 (largest PR 2, ~358).

Umbrella split rule (`proposal.md:301`: more than seven PRs or more than 2,400 budgeted lines),
evaluated on **point** budgeted lines: 3 PRs and ~605 lines. **Does not fire**, and does not fire
at x 1.3, x 1.8 or x 2.2 either. m4e + m4e2 as one slice would be 9 PRs and ~2,095 lines at point:
it fires on the PR-count clause only (lines need x 1.3, ~2,720), which is why the split exists.
After 1a merges, compute actual / estimate; if `estimate x measured multiplier > 400` for a later
PR, cut it at its seam before `sdd-apply`.

| Field | Value |
|-------|-------|
| Estimated changed lines | ~605 impl+docs across 3 PRs (~787 at x 1.3); tests ~1,020 apart |
| 400-line budget risk | Medium (all under 400 at x 1.3, PR 2 at ~358; PR 2 over at x 1.8, 1b over at x 2.2) |
| Chained PRs recommended | Yes |
| Suggested split | PR 1a -> PR 1b -> PR 2, all stacked to `main` |
| Delivery strategy | ask-on-risk |
| Chain strategy | stacked-to-main |

Decision needed before apply: Yes
Chained PRs recommended: Yes
Chain strategy: stacked-to-main
400-line budget risk: Medium

The decision is a confirmation: chained PRs and `stacked-to-main` are cached; what remains for the
user is to accept the post-1a cut rule. Apply cannot start until `m4e-activity`'s PR 6 has merged.

### Suggested Work Units

| Unit | Goal | Likely PR | Notes |
|------|------|-----------|-------|
| 0 | Planning (shared with m4e PR 0) | `plan/m4e-beliefs-activity-admin` | Not counted toward the seven-PR rule |
| 1a | Persistence surface: five setters, `LearnedThresholds`, G4 | PR 1a, base `main` after m4e-activity PR 6 | Safe alone: no caller outside tests |
| 1b | `AdminService` door, G5, doc 02 §11 config clause | PR 1b, base `main` after 1a | The only caller of the setters |
| 2 | `/ui/admin`, wiring, G6/G7/G12, R10 pin | PR 2, base `main` after 1b | Admin nav link |

---

## Pre-task 0 — rides m4e's planning PR (docs only)

Tracked in [`../m4e-beliefs-activity-admin/tasks.md`](../m4e-beliefs-activity-admin/tasks.md) 0.6;
repeated here so this slice is self-contained.

- [x] **0.1** `design.md` §8: rename risks R1-R6 to RK-1..RK-6 and fix references (`rg '\bR[1-6]\b'`,
  risk meanings only; spec R7-R11 untouched).
- [x] **0.2** `design.md` §7: "At 1.8x and 2.2x PR 2 and 1b exceed it" -> "PR 2 exceeds at 1.8x and
  2.2x; 1b exceeds only at 2.2x" (design §10 item 7); forecast sentence reads point as the rule's
  basis (item 3).
- [x] **0.3** `design.md` §5 PR 2 row and §3.5 G6: name `internal/httpapi/server_test.go:117`
  `TestUIGuardedLeavesEachReachAView` (item 11).
- [ ] **0.4** Precondition check before 1a: `m4e-activity`'s PR 5 and PR 6 merged
  (`DecisionLog.Before` and the shape-driven change decoder) and m4e's PR 3 and PR 4 merged
  (`brain.ErrWriteLanded`, `uiCrossOriginBodies`, the I22 whitelist, `uiDeps`); all exist on
  `main` (`rg` each).

---

## PR 1a — `feat/config-setters-learned-thresholds` (C1, C2, C8, G4, G11)

Files: `internal/ports/{configrepo,relationrepo,decisionlog}.go`,
`internal/store/sqlite/{configrepo,relationrepo}.go`, `test/support/memrepo/{config,relations}.go`,
`test/support/repocontract/{configrepo,relationrepo,decisionlog}.go`, `internal/scheduler/cron_test.go:23`,
`test/conformance/i27_viewing_is_not_delivering_test.go:236`, `internal/brain/checkin_test.go:383`,
`test/conformance/config_repo_shape_test.go`, `testdata/schema/store_api.golden`, `docs/06-harness.md`.

- [ ] **1a.1** SCAFFOLD — `ConfigRepo`: `SetWeightThreshold`, `SetHysteresisMargin` (`float64`),
  `SetGoalStagnationDays`, `SetMentalLoadThreshold` (`int`), `SetConsolidationEnabled` (`bool`),
  each `(ctx, v, at time.Time) error`; `RelationRepo.LearnedThresholds` and
  `LearnedThreshold{RelationType; relation.Thresholds}`; sqlite and memrepo return nil.
  `ActionConfigUpdated` appended last, "fifty-two" -> "fifty-three", repocontract `want` map and
  subtest title.
- [ ] **1a.2** SCAFFOLD ripples (same commit): `cron_test.go:23` `errConfigRepo` gains the five
  setters returning nil; `i27Config` gets five overrides that **fail** via
  `i27Fail(g.t, "ConfigRepo", "<Setter>")`, comments at `:25-26`, `:36-39` updated ("six" writes,
  "twenty-three" names), `LearnedThresholds` added to the RelationRepo reads; `checkin_test.go:383`
  `confirmingRelations` gains an explicit `LearnedThresholds` returning an error. Verify
  `make check` and tagged vet green.
- [ ] **1a.3** RED contract (both implementations; FX-C: every column seeded to a distinct
  non-default, `updated_at == at`): C1 table over the five setters, each moves only its column;
  C2 lazy create on an empty vault, L3 only, asserts 0.5 / 0.05 / 1 / 21 / 7 elsewhere; C8
  `LearnedThresholds` ordered by `relation_type` from two rows inserted out of order.
- [ ] **1a.4** RED/guard conformance — `config_repo_shape_test.go` `TestConfigRepo_ExposesExactlyTheQ5Setters`
  (G4, reflection: method set is `{Load, RecordConsolidationRun}` plus the five, each
  `(context.Context, float64|int|bool, time.Time) error`). It passes on arrival (the scaffold
  already has the five); its discrimination is proven by the 1a.7 probe.
- [ ] **1a.5** GREEN — sqlite lazy UPSERT per setter
  (`INSERT INTO config (id, <col>, updated_at) VALUES (1, ?, ?) ON CONFLICT(id) DO UPDATE SET
  <col> = excluded.<col>, updated_at = excluded.updated_at`); `LearnedThresholds ORDER BY
  relation_type`; memrepo mirrors (leaves other fields nil, finding 3). `make store-api-golden`
  (G11); review the diff is exactly six methods.
- [ ] **1a.6** DOCS — `internal/ports/configrepo.go:17` "no reader in m2c" -> `internal/scheduler`
  (`catchup.go:36`, `scheduler.go:247`); `docs/06-harness.md` §4 I12 row names `config.updated`.
  Verify `scripts/docs-sync.sh`.
- [ ] **1a.7** PROBES C1 write another column; C2 name every column with Go literals; C8 drop the
  order; G4 probe file adding `SetConfig(map...)`, `SetRelationThreshold`,
  `SetConsolidationLastRunAt` and a `string`-key parameter (fires), against the five (silent).

## PR 1b — `feat/brain-admin-service` (C3-C7, C9, C10, C12, C14-C17, G5)

Files: `internal/brain/{admin,admin_field,admin_write}.go`(+tests),
`test/conformance/config_door_test.go`, `docs/02-cognitive-core.md` §11.

- [ ] **1b.1** SCAFFOLD — `ConfigField` closed type, `ParseConfigField` (unknown ->
  `ErrUnknownConfigField`), `*ConfigRangeError`, `AdminRecentEffectRows = 10`, `AdminService`
  exporting only `View` and `Update` (zero values), `NewAdminService(clock, cfg, rels, log)`,
  `admin_write.go` `write(ctx, field, previous, next any, apply func(at time.Time) error) error`
  holding the file's single `s.clock.Now()`; five unexported `apply...` appliers (never `Set...`).
  Per the m4c scaffold precedent, reference unexported symbols from the stubs so `unused` stays
  quiet.
- [ ] **1b.2** RED brain (FX-C; second fixture with no config row): C3 `TestAdmin_Ranges` (margin
  `0` ok, weight `2.0` ok and `nextafter(2,3)` rejected, `NaN`/`±Inf` rejected, days `1` ok / `0`
  rejected), C4 `…SameValueLogsNothing` and `…DefaultOnAbsentRowWritesNothing`, C5
  `…UnsetFieldWriteLogs`, C5b `…CorruptStoredValueIsRepaired` (margin `-5`), C6 each field moves
  exactly its own column, C7 `…ApplyFailureLogsNothing` and `…LogFailureAfterWriteIsVisible`
  (`ErrWriteLanded`, `Record` set), C9 view with a newer `capture.*` row absent and 11 effects ->
  10, C10 last run nil -> *never*, C12 `"1"` rejected for the bool, C14 JSON keys for all five
  fields and one row per write, C15 the row `Update` writes decodes through
  m4e-activity's `ActivityService` (`Change` non-nil), C16 `…RejectedInputWritesNothing`, C17
  `TestAdminServiceExportsOnlyViewAndUpdate`.
- [ ] **1b.3** RED conformance — `config_door_test.go` `TestConfigSettersAreCalledOnlyInsideTheLoggedDoor`
  (G5, AST over non-test `internal/**` and `cmd/**` excluding `internal/store/**`: every call to
  one of the five setter selectors sits in a `FuncLit` passed to `write` in
  `internal/brain/admin*.go`) with self-proof rows: a direct `s.cfg.SetHysteresisMargin(...)`
  fires, the five closures are silent.
- [ ] **1b.4** GREEN — parse per field (exact `true`/`false`, `Atoi`, `ParseFloat`); accept iff
  `Resolve(&v) == v`; unchanged iff `effective == v && (stored == nil || *stored == v)`; `write`:
  `apply(now)` then `Record(config.updated)` with the same `now`; log row
  `{fields:[col], previous:{col:v}, next:{col:v}}`, `previous` the value in force; `View`:
  `Load`, `LearnedThresholds`, `relation.Resolve(nil)`, `Before(nil, "consolidate.", 10)`.
- [ ] **1b.5** DOCS — doc 02 §11 (`:1237-1240`) adds "a config change" to m4e's belief sentence.
  Verify `scripts/docs-sync.sh`.
- [ ] **1b.6** PROBES C3 `> 0` margin / `< 2` weight; C4 compare stored pointer only; C5 skip as
  unchanged / `previous: null`; C5b treat as no-op; C6 crossed wiring; C7 record-first; C9
  prefix `""` / wrong limit; C10 zero time; C12 `ParseBool`; C14 positional / no `fields` /
  extra row on no-op; C15 a shape the decoder rejects; C16 log first; C17 add a `Set...` method;
  G5 a direct setter call in a service and in `cmd/nooma`. If 1b measures over 400 after the
  multiplier, cut at `View` / `Update`.

## PR 2 — `feat/ui-admin` (C11, C13, C18-C21, G6, G7, G12)

Files: `internal/ui/{admin.go,admin.templ,ui.go,layout.templ}`, `internal/httpapi/server.go`,
`cmd/nooma/{wiring.go,wiring_admin_test.go,serve.go,serve_test.go}`,
`test/conformance/{ui_entrances_test,httpapi_ui_wiring_test,ui_cross_origin_test,ui_read_views_write_nothing_test}.go`,
**`internal/httpapi/server_test.go`**, `docs/06-harness.md` §4.

- [ ] **2.1** SCAFFOLD — `ui.Admin` interface type (`View`, `Update`); `wireAdmin(db)`
  (`systemClock{}`, `NewConfigRepo`, `NewRelationRepo`, `NewDecisionLog`) and `uiDeps(..., admin)`
  with the typed-nil guard, `serve.go` call site; `admin.templ` placeholder; `make templ`.
- [ ] **2.2** RED conformance — `wantUIMuxWiring` rows `GET /ui/admin` and `POST /ui/admin`;
  counting `Admin` stub on m4e's shared counter; `uiCrossOriginBodies["POST /ui/admin"] =
  "field=weight_threshold&value=0.6"` and no new exemption (C18; m4e's coverage test fails if the
  entry is missing); `Admin` field on `ui.Deps` (G12 red: `View`, `Update`); G7 admin GET over
  write-counting decorators of `ConfigRepo`, `RelationRepo`, `DecisionLog`;
  **`internal/httpapi/server_test.go:117` `TestUIGuardedLeavesEachReachAView`: add leaf `/ui/admin`,
  marker `ADMIN` and a stub `Admin`.**
- [ ] **2.3** RED ui — C11 unknown field never reaches `Update`, C21
  `TestAdminPost_UnknownFieldNeverCallsUpdate` (`relation_thresholds`, `consolidation_last_run_at`,
  bogus, empty: 400, `Update` calls == 0), C19 `…WriteLandedRendersSuccessNoticeAndWarns`
  (`&brain.WriteLandedError{Record: true}`, recording `slog` handler: status, notice, no error
  text, one Warn with the cause), C20 `…RangeErrorIs400AndStoresNothing` (real `AdminService`,
  FX-C snapshot, form re-rendered with message and submitted text), C13
  `TestAdmin_MarginWriteReachesNextToday` (FX-H: margin 0.5 holds A, POST margin 0, next Today
  shows the challenger, keeper not reset; a regression pin with no production branch),
  `wiring_admin_test.go` over a real empty migrated vault (`View` answers never / enabled / no
  learned rows), `TestUIDeps_NilServicesStayNilInterfaces` `Admin` case, nil dep 503, relation
  thresholds shown with the learned note and no input.
- [ ] **2.4** GREEN — routes in `ui.go` and the `server.go` switch; `ParseConfigField` before
  `Update`; 400 handling; HTMX fragment vs full page; `MaxBytesReader`; `admin.templ`;
  **`layout.templ` gains the admin link**; whitelist `View`, `Update` (G12).
- [ ] **2.5** DOCS — harness §4 I22 row names `View`, `Update`. Verify `scripts/docs-sync.sh`.
- [ ] **2.6** PROBES C11 accept `relation_thresholds`; C18 no body entry / invalid body (0 calls);
  C19 map to 500 or 400 / skip the Warn / generic error text; C20 200 or 500 / write before
  validating; C21 call `Update` first; G6 POST unguarded; G7 a GET that records or seeds.

---

## Closing (each PR)

- [ ] **C.1** `make check-all`, `go vet -tags integration,e2e ./...`, `scripts/docs-sync.sh` in an
  isolated worktree at the PR's commit; PR per `nooma-pr`; PR body lists impl+docs vs test lines
  and the measured multiplier.
- [ ] **C.2** After 1a merges compute actual / estimate and apply the cut rule before 1b.
