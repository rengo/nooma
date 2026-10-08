# Tasks — m4e-activity: the activity view

Derived from `spec.md` (R5, R6, the activity part of R9 and R11) and `design.md` (§1-§8; the
design is the activity half of the m4e design that was APPROVED after Judgment Day round 3, and
**§10 carries the round-3 corrections that touch it**). A slice sharing
`openspec/changes/m4-mirror-ui/proposal.md`. Beliefs are
[`m4e-beliefs-activity-admin`](../archive/2026-10-08-m4e-beliefs/tasks.md); admin is
[`m4e2-admin`](../m4e2-admin/tasks.md), which starts after this slice's PR 6 merges. Shape follows
the archived m4c and m4b tasks.

> **Split from m4e-beliefs-activity-admin on 2026-10-08 by the owner's 7-PR rule after PR 2 measured 2.06x.**
> These are the old m4e PRs 5 and 6, **numbers kept** (PR 5, PR 6; tasks 5.x, 6.x; branch names;
> mutant A-series) so a citation of "m4e PR 5" or "task 6.3" stays valid. Their text is moved
> unchanged. The forecast section below is recomputed for the two PRs.
>
> **Depends on m4e** (full list in `spec.md`'s header):
> - **PR 1's vocabulary** and `store_api.golden` state (soft: PR 5 regenerates the golden after
>   PR 1's regeneration; nothing here imports `selfmodel`);
> - **the action-vocabulary file** and its `repocontract` map as m4e PR 3 leaves them (fifty-two
>   members); this slice adds no action, and `ActivityFamilies` reads the list;
> - **the I22 whitelist test** (G12), **the `ui.Deps` / `uiDeps` / `wiring.go` pattern and the
>   layout nav** (m4e PR 4); PR 6 cannot start before PR 4 of m4e merges;
> - **`WriteLandedError`: not used** (read-only view);
> - **the cross-origin body table: not used.** Activity has no POST, so no entry is added.

**Delivery**: `ask-on-risk`, `stacked-to-main` (each branch targets `main`, rebases after the
previous merge). Strict TDD per PR: scaffold (compiles, lints, no behaviour) -> RED (fails on an
assertion, never on "undefined") -> GREEN -> docs -> mutation probes enumerated from the
**production diff**, each with its killing test from design §6. No PR tip is red. Every PR runs
`make check`, then before opening `make check-all` **and** `go vet -tags integration,e2e ./...`,
in an isolated `git worktree` at that PR's own commit. Conventional commits, no AI trailer. Budgets
are impl+docs; tests reported apart. Each GREEN commit body records its probe (mutation applied,
red output, reverted). A probe that never applied is a finding.

## Review Workload Forecast

Impl+docs excludes tests, `test/support/**`, the regenerated golden and `*_templ.go`. Test lines
are estimates (m4c measured tests at roughly 3.5x impl; m4e measured tests at 1.75x and 1.8x their forecasts) and are
reported apart. The point estimates are the old m4e rows 5 and 6, unchanged.

| PR | Branch | Impl+docs (point) | x 1.3 | x 1.8 | x 2.06 (m4e PR 2) | Tests (apart) | Cut seam |
|----|--------|------|------|------|------|------|------|
| 5 | `feat/ports-store-decisionlog-before` | ~130 | ~169 | ~234 | ~268 | ~320 | none needed (largest margin) |
| 6 | `feat/ui-activity` | ~290 | ~377 | ~522 | ~597 | ~450 | `ActivityService` / view + wiring + nav |
| | **Total (2 PRs)** | **~420** | **~546** | **~756** | **~865** | **~770** | |

Arithmetic: point 130 + 290 = 420. x 1.3: 169 + 377 = 546. x 1.8: 234 + 522 = 756. x 2.06
(the multiplier m4e PR 2 measured, 557 against ~270): 130 x 2.06 = 267.8 and 290 x 2.06 = 597.4,
so ~268 + ~597 = ~865. Tests: 320 + 450 = 770. (The old m4e table's totals were ~1,490 / ~1,940 /
~2,680 / ~3,320 over six PRs; beliefs keep 1,070 / 1,392 / 1,926 / 2,550 and these two PRs the
rest: 1,070 + 420 = 1,490, 1,392 + 546 = 1,938, 1,926 + 756 = 2,682, 2,550 + 770 = 3,320.)

Umbrella split rule (`proposal.md` §5, the "If `sdd-tasks`'s forecast" paragraph: more than seven PRs or more than 2,400 budgeted lines),
evaluated on **point** budgeted lines: 2 PRs and ~420 lines. **Does not fire**, at any
multiplier (x 2.06 is ~865). **PR 6 exceeds the 400-line ceiling at x 1.8 and x 2.06** (~522,
~597), and m4e measured 2.06x on its brain-heavy PR 2. PR 5 is a port and store PR of the shape
m4e PR 1 measured at 1.2x, so it is the likelier to run near the lower multiplier. **Re-measure
after PR 5 merges, not after PR 6:** compute actual / estimate for it; if `estimate x measured
multiplier > 400` for PR 6 (it is ~597 at 2.06x, ~377 at 1.3x), cut PR 6 at its seam above
(`ActivityService` | view + wiring + nav) before `sdd-apply`. That cut makes this slice three PRs,
still far from seven.

| Field | Value |
|-------|-------|
| Estimated changed lines | ~420 impl+docs across 2 PRs (~546 at x 1.3); tests ~770 apart |
| 400-line budget risk | Medium (PR 5 under 400 at every multiplier; PR 6 under at x 1.3, over at x 1.8 and x 2.06) |
| Chained PRs recommended | Yes |
| Suggested split | PR 5 -> PR 6, both stacked to `main`, after m4e's PR 4 |
| Delivery strategy | ask-on-risk |
| Chain strategy | stacked-to-main |

Decision needed before apply: No (the cut rule above is inherited and pre-agreed; apply it on the
PR 5 measurement)
Chained PRs recommended: Yes
Chain strategy: stacked-to-main
400-line budget risk: Medium

### Suggested Work Units

| Unit | Goal | Likely PR | Notes |
|------|------|-----------|-------|
| 5 | `DecisionLog.Before` | PR 5, base `main` after m4e PR 4 | Port + store only; independent of m4e PRs 1-4 but serial to avoid vocabulary-file and golden conflicts |
| 6 | `/ui/activity` | PR 6, base `main` after PR 5 | Umbrella §5.2 row 10; activity nav link |

---

## Pre-task 0 — preconditions (no code)

- [ ] **0.1** Before PR 6: m4e PR 4 is merged, so `ui.Deps` has `Beliefs`, `uiDeps` has the
  `beliefs` parameter, `ui_entrances_test.go` has the I22 whitelist, `wantUIMuxWiring` and
  `TestUIGuardedLeavesEachReachAView` list `/ui/beliefs`, and `layout.templ` has the beliefs link
  (`rg` each on `main`). Before PR 5: `main` carries m4e PR 3, so the action vocabulary is fifty-two
  members. Verify: `scripts/docs-sync.sh` passes on `main`.

---

## PR 5 — `feat/ports-store-decisionlog-before` (A1-A3, A5, A6, A8, G11)

Files: `internal/ports/decisionlog.go`, `internal/store/sqlite/decisionlog.go`,
`test/support/memrepo/decisionlog.go`, `test/support/repocontract/decisionlog.go`,
`internal/brain/check_test.go:323`, `test/conformance/i27_viewing_is_not_delivering_test.go`,
`testdata/schema/store_api.golden`.

- [x] **5.1** SCAFFOLD — `DecisionCursor{OccurredAt, Seq}`, `DecisionRow{Decision; Seq}`, `Before`
  on the port; sqlite and memrepo return empty; memrepo carries an insertion sequence;
  `recordingLog` (`check_test.go:323`) gains `Before` returning an empty page; i27 header comment
  adds `Before` among the reads.
- [x] **5.2** RED L3 probe first — `EXPLAIN QUERY PLAN` uses `idx_decision_log_occurred` with no
  `USE TEMP B-TREE FOR ORDER BY`, unfiltered and cursor forms. If the row-value form fails,
  switch to `occurred_at < ? OR (occurred_at = ? AND rowid < ?)` (same semantics).
- [x] **5.3** RED contract (both implementations; FX-A, whole-second builder fails the test on
  sub-second input; ids chosen so id order differs from write order; tied group straddles the
  page boundary): A1 order, A2 exactly-once walk including a group larger than a page, A3 prefix
  (`capture.checkin.*` absent from `check.`), A5 `limit` 0 empty and 2 over 5, A6 nil vs
  zero-value cursor, A8 `Seq` strictly increasing with write order.
- [x] **5.4** GREEN — sqlite `ORDER BY occurred_at DESC, rowid DESC`, keyset
  `(occurred_at, rowid) < (?, ?)`, `substr(action, 1, ?) = ?`; memrepo sort by
  `(OccurredAt, Seq)` desc. `make store-api-golden` (G11).
- [x] **5.5** DOCS — none (no doc names the read); `scripts/docs-sync.sh` n/a.
- [x] **5.6** PROBES A1 ASC / drop rowid / `id DESC`; A2 `<=` and `occurred_at < ?`; A3 ignore /
  `LIKE`; A5 `LIMIT -1`; A6 nil as zero cursor; A8 `Seq` 0 or unordered.

## PR 6 — `feat/ui-activity` (A4, A7, A9-A12, G7, G8, G12)

Files: `internal/brain/activity.go`(+test), `internal/ui/{activity.go,activity.templ,ui.go,layout.templ}`,
`internal/httpapi/server.go`, `cmd/nooma/{wiring.go,wiring_activity_test.go,serve.go,serve_test.go}`,
`test/conformance/{ui_entrances_test,httpapi_ui_wiring_test,ui_read_views_write_nothing_test}.go`,
**`internal/httpapi/server_test.go`**, `docs/02-cognitive-core.md` §5 step 4, `docs/06-harness.md` §4.

- [ ] **6.1** SCAFFOLD — `ActivityPageSize = 50`, `ActivityService`, `NewActivityService`, `Page`,
  `ActivityFamilies(actions)` returning zero values; `ui.Activity` interface type; `wireActivity(db)`
  and `uiDeps(..., activity)`; `activity.templ` placeholder; `make templ`.
- [ ] **6.2** RED brain — FX-A brain cases: pass-sized tie group (`2 x ActivityPageSize + 10`, ids in
  descending write order) walked exactly once in reverse write order; exactly 50 rows -> no
  "older" link; 51 -> link and the 51st absent (A4); page one holds the **newest** (A7, not a
  reversed `Since`); A9 unknown `kind` -> 400 with zero reads; A10
  `TestActivityView_CorrectionShowsPreviousAndNext` and `…MalformedContextStillRenders`; A11
  `TestActivityView_ConfigUpdatedShapeRenders` (`goal_stagnation_days: 21 -> 28`,
  `consolidation_enabled: true -> false`, `weight_threshold: 0.5 -> 0.6`, no trailing `.0`); A12
  `TestActivityFamilies_DerivedFromVocabulary` (synthetic family `zzz`).
- [ ] **6.3** RED gates — `Activity` field on `ui.Deps` (G12 red: `Page`); `wantUIMuxWiring` GET row;
  G7 activity GET; G8 `TestActivityView_HasNoMutatingForm` (no `method="post"`, no `hx-post`;
  the GET filter form is silent); **`server_test.go:117` gains leaf `/ui/activity`, marker
  `ACTIVITY` and a stub `Activity`**; `wiring_activity_test.go`; `TestUIDeps_NilServicesStayNilInterfaces`
  `Activity` case; I23 `go/ast` test untouched and green.
- [ ] **6.4** GREEN — `ActivityService.Page` (ask `size+1`, cursor = last shown row, shape-driven
  `Change` decode, number rendering without `.0`); view and handler (400 on bad kind or
  half/malformed cursor, `parseBrowseCursor` shape); routes; **`layout.templ` gains the activity
  link (this PR only)** (design §10 item 1); whitelist `Page` (G12).
- [ ] **6.5** DOCS — doc 02 §5 step 4 `:663-664` -> "Recording is not undoing. `/ui/activity` shows
  the previous value beside the new one, read-only; no surface offers it back."; harness §4 I23
  and I22 rows. Umbrella §5.2 row 10 satisfied. `scripts/docs-sync.sh`.
- [ ] **6.6** PROBES A4 ask `size`, cursor = extra row; A7 reversed `Since`; A9 read all; A10 show
  `next` only / 500 on bad JSON; A11 action-keyed decoder, `21.0`; A12 hard-coded families; G8 a
  restore button.

---

## Closing (each PR)

- [ ] **C.1** `make check-all`, `go vet -tags integration,e2e ./...`, `scripts/docs-sync.sh` in an
  isolated worktree at the PR's commit; PR per `nooma-pr`; PR body lists impl+docs vs test lines
  and the measured multiplier.
- [ ] **C.2** After PR 5 merges: compute actual / estimate; apply the cut rule (Forecast) before
  PR 6. After PR 6 merges, `m4e2-admin` may start.
