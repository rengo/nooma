# Spec — M4e-activity: the activity view

Specification for `m4e-activity`, split off `m4e-beliefs-activity-admin` on 2026-10-08. States what
MUST be true after this change, in testable form; not how (`design.md`'s job). A slice sharing
`openspec/changes/m4-mirror-ui/proposal.md`.

> **Split from m4e-beliefs-activity-admin on 2026-10-08 by the owner's 7-PR rule after PR 2 measured 2.06x.**
> The owner's pre-agreed rule was: if m4e passes seven PRs, split beliefs from activity. PR 1 of
> m4e measured 1.2x its forecast (318 changed lines against ~265); PR 2 measured 2.06x (557
> against ~270) and was cut into 2a (#291) and 2b (#292). At 2.06x every later PR of m4e would
> need its own cut (PR 3 ~536, PR 4 ~567, PR 6 ~597), so the slice would grow past seven PRs.
> The activity half (the old PRs 5-6: the newest-first `DecisionLog.Before` read and the
> `/ui/activity` view) is this change. **R5 and R6, the activity part of R9 and R11, OQ4, and the
> A-series mutants moved here with their text, scenarios and rulings intact.** R-numbers are kept
> (R1-R4, R7, R8, R10, R12 and R13 are absent here by design) so a citation stays valid across
> the three files. `m4e-beliefs-activity-admin` now holds beliefs only; its directory name is kept
> because other artifacts reference it.
>
> **Depends on m4e (this slice starts after m4e's PR 4 merges, as the old PRs 5-6 did):**
> - **PR 1's vocabulary** (`selfmodel.Status`, `selfmodel.Origin`) and the `store_api.golden` it
>   regenerated: a soft dependency. No code in this slice imports the belief vocabulary; the
>   activity view renders any change-shaped row (R6), so `belief.edited` rows appear without an
>   edit here. It is listed because PR 5 regenerates the same golden after PR 1's.
> - **The action-vocabulary file** (`internal/ports/decisionlog.go`, its `repocontract` map) as
>   m4e leaves it (fifty-two members after PR 3). This slice adds **no** action. The `?kind=`
>   filter derives its families from that list (design §3.5, A12), so `belief` appears once m4e
>   PR 3 lands.
> - **The `ui.Deps` / `uiDeps` / `wiring.go` pattern, the layout nav and the I22 whitelist test**
>   (m4e PR 4, design §3.9 and G12). PR 6 makes the second `uiDeps` signature change and adds
>   `Page` to the whitelist. This is why PR 6 cannot start before m4e's PR 4 merges.
> - **`brain.ErrWriteLanded` / `*WriteLandedError`: not used.** The activity view is read-only and
>   has no write failure window.
> - **The cross-origin body table** (m4e G6, PR 4): **activity has no POST**, so it adds no entry
>   and needs no exemption. `TestUICrossOriginBodiesCoverEveryPOSTRow` fails for an entry with no
>   row, so adding one would be an error. The activity GET is covered by the `wantUIMuxWiring`
>   row (guarded) and by `TestUIGuardedLeavesEachReachAView`.
>
> **What depends on this slice:** `m4e2-admin` (admin lists recent consolidation effects through
> `DecisionLog.Before`; the `config.updated` row it writes must decode through this slice's
> shape-driven change decoder, R6; this slice's A11 test carries a `config.updated`-shaped
> fixture). m4e2 still depends on m4e for `WriteLandedError`, the I22 whitelist pattern, the
> `uiDeps` pattern and the body table. `m4f` depends on m4e for the view shell (layout, nav) and
> does not pick up this slice.

Sources: umbrella §2 (acceptance lines 73-75), §4.2 row "Newest-first activity", §5.2 test row
10, R10; `docs/02-cognitive-core.md` §5 step 4 (lines 644-664), §11; `docs/03-data-model.md`
(`decision_log`); `internal/ports` (`DecisionLog`); `internal/httpapi/server.go` (cookie guard).

## Scope boundary

**In**: `/ui/activity` (newest-first, pre-image rendering); the brain service behind it; the new
`DecisionLog.Before` read (no new port, no migration; it widens
`testdata/schema/store_api.golden`). Doc 02 lines 663-664 are corrected in the activity-view PR
(non-negotiable 1).

**Not this change**: `/ui/beliefs` and the derive shield (`m4e-beliefs-activity-admin`);
`/ui/admin`, any `ConfigRepo` write (`m4e2-admin`); any reader of `learning_signals` (M5); an
undo of a correction (umbrella §3.4); timers (m4f); the graph (m4d); `/ui/tracking`; a new
migration; a date filter (OQ4).

## Requirements

| ID | Requirement | Invariants |
|----|-------------|-----------|
| R5 | `/ui/activity` reads `decision_log` newest-first through a new bounded read | I12 (read side) |
| R6 | The activity view renders a correction's (or any change row's) `previous` read-only | I23 |
| R9 | **Activity part: none by design.** `/ui/activity` has no non-GET route. Its GET sits behind the cookie guard. The mutating-route gates are `m4e-beliefs-activity-admin`'s R9 | m4a/m4b gates |
| R11 | **Activity part:** doc 02 lines 663-664 reworded; `store_api.golden` regenerated for `Before`; `scripts/docs-sync.sh` passes | non-neg. 1 |

R1-R4, R12 and R13 are in `m4e-beliefs-activity-admin`; R7, R8 and R10 in `m4e2-admin`.

### R5 — Newest-first activity

**MUST**: `DecisionLog` gains a read returning rows ordered `occurred_at` descending, rows sharing
one `occurred_at` in **reverse write order** (newest write first), strictly before a cursor,
bounded by `limit`. `occurred_at` has one-second resolution and ids are random UUIDs, so an id
tie-break would order a consolidation pass's rows arbitrarily; the tie-break is the row's
insertion sequence. It is NOT a client-side reverse of `Since` (umbrella R10). `Since`'s
forward-only contract is unchanged. A page boundary on rows sharing one `occurred_at` neither
skips nor repeats a row, including a tie group larger than a page. Page size and filters: OQ4.

- GIVEN 5 rows, three sharing an instant (written in a known order, ids not in that order), and
  page size 2
- WHEN pages are read by cursor until empty
- THEN all 5 rows appear exactly once, newest first, the tied three in reverse write order
- GIVEN a tie group larger than one activity page
- WHEN the pages are walked
- THEN every row appears exactly once

### R6 — Pre-image rendering

**MUST**: a `correction.applied` row renders `context.previous` beside `context.next`, keyed by
column name, read-only: no control offers the previous value back (umbrella §3.4). The rendering
is **shape-driven**: any row whose context carries `fields`, `previous` and `next` in the
documented shape is rendered the same way. That covers `belief.edited` (this change) and
`config.updated` (`m4e2-admin`; its context uses this shape so this decoder renders it with no
change). A row whose context lacks `previous` or is malformed JSON renders its rationale without
failing the page. The `go/ast` test asserting `applyWithPreImage` is the only writer is untouched
and green.

- GIVEN a `correction.applied` row with `previous.event_at`
- WHEN `/ui/activity` renders it
- THEN both values are visible and no form or button restores the old one
- GIVEN a row shaped `{fields:["weight_threshold"], previous:{weight_threshold:0.5},
  next:{weight_threshold:0.6}}` (the `config.updated` shape)
- WHEN `/ui/activity` renders it
- THEN `weight_threshold: 0.5 → 0.6` is shown, read-only

### R9 — Mutation gates (activity part)

**MUST**: there is nothing to gate. `GET /ui/activity` is the only route, behind the cookie guard
like every `/ui/` leaf, and the rendered page contains no `method="post"` and no `hx-post` (design
G8). The mutating-route gate (`http.CrossOriginProtection`, the per-route valid body table) is
specified for the beliefs routes in `m4e-beliefs-activity-admin` R9; this view adds no row to it.

- GIVEN the rendered activity page
- WHEN its markup is inspected
- THEN it holds no form that posts and no `hx-post` (the GET filter form is the only form)

### R11 — Sync (activity's part)

**MUST**: doc 02 lines 663-664 reworded (a surface now shows the previous value; none offers it
back); `store_api.golden` regenerated for the `Before` method; `scripts/docs-sync.sh` passes. The
beliefs doc edits (doc 02 §10, §6 item 5, §11, doc 03, the umbrella Q4/Q5 rows) belong to
`m4e-beliefs-activity-admin`; admin's to `m4e2-admin`.

## Verified by

L1 brain and `internal/ui` tests over fakes and a fake clock (R6); L3 for the SQL (R5 tie-break
and cursor); umbrella §5.2 row 10 (R6, `go/ast` test untouched). Each transition test is watched
failing first. No test opens a browser, the network or a real LLM. Activity fixtures use
whole-second timestamps (design FX-A) so the in-memory fake and SQLite agree.

## Open questions (genuine product decisions, not decided here)

Open, for design and tasks:

- **OQ4 — Activity page size and filters.** Page size (umbrella names no number; doc 02 §13 names
  none, so none may be invented) and filters (by action prefix, date). Default if unruled: fixed
  page, no filters, "older" cursor link. For design.

Closed: OQ4 by design §3.5 (written while this was still part of m4e): a 50-row transport page,
an action-family filter, no date filter. OQ1-OQ3 and OQ8 are in `m4e-beliefs-activity-admin`;
OQ5 and OQ6 in `m4e2-admin`; OQ7 was closed by the earlier split.

## Exit criterion

The activity view pages newest-first and shows pre-images read-only; it has no mutating route;
docs and golden are in sync; `make check-all` is green. Beliefs are
`m4e-beliefs-activity-admin`'s exit; admin is `m4e2-admin`'s exit.
