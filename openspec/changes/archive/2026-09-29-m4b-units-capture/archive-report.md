# Archive report — `m4b-units-capture`

**Closed**: 2026-09-29. **Verdict inherited from verify**: PASS — 0 CRITICAL, 0 WARNING,
2 SUGGESTION (both carried forward, below).
**`main` at archive**: `fa3bf77`, the merge of PR #281.
**Milestone**: the second of M4's six slices. `openspec/changes/m4-mirror-ui/proposal.md` stays
active — it covers `m4c` through `m4f` and is not archived with this slice.

## What shipped

The UI can find, read and write units. `/ui/units` browses live units newest first, 50 per page,
filterable by type and resumable from the last row seen; `?q=` searches through
`RecallService.ForText`, the same mechanism as `/recall`. `/ui/units/{id}` shows one live unit
with its stored weight, its three dates under their own labels, and its live relations with
direction and confidence. `/ui/capture` captures through `CaptureService.Capture` unchanged, and
the detail page's correction form sends a capture whose referent comes only from the path.

| PR | Branch | What |
|---|---|---|
| #276 | `plan/m4b-units-capture-artifacts` | spec, design and tasks |
| #277 | `feat/ports-store-units-browse` | `UnitRepo.LiveBrowsePage`, keyset on `(created_at, id)`, migration `0005` and its index |
| #278 | `feat/brain-units-read` | `brain.UnitsService` (`Browse`, `Detail`), `wireUnits` |
| #279 | `feat/ui-units-browse` | `/ui/units` browse and search, `ui.Handler` dispatch on `r.Pattern`, the hardened wiring gate |
| #280 | `feat/ui-unit-detail` | `/ui/units/{id}`, relation direction and confidence |
| #281 | `feat/ui-capture-correct` | `/ui/capture`, the correction route, entrances gate (c), the cross-origin table test |

## What it leaves behind, beyond the feature

Structural gates, each re-proven by `sdd-verify` at `fa3bf77` by live mutation in both
directions:

| Gate | Mutation it was proven against |
|---|---|
| Index-backed browse page (`TestUnitRepo_LiveBrowsePageUsesBrowseIndex`, four query shapes) | `idx_units_live_browse` removed from migration `0005` |
| Wiring gate, hardened | a non-literal pattern; `HandleFunc`; a loop registration; a route on `Handler`'s outer mux |
| Entrances gate (a) | `ScoredFor` added to `ui.Searcher` |
| Entrances gate (b) | a type assertion to `*brain.UnitsService` inside `internal/ui` |
| Entrances gate (c) | `ReferentID` set from a form value, as a literal or an assignment |
| I22 browse search | `ForText` called twice per request |
| Cross-origin table test | the `xo` wrap removed from the `/ui` subtree |
| Nil services stay nil | `uiDeps` assigning a typed-nil `*RecallService` |

## Cost, stated plainly

- Six PRs. Implementation plus docs per link: 131, 126, 286, 113, 274 — every link under the
  400-line ceiling, so neither overflow cut (3b, 5b) was needed. The design's ~1,010 estimate
  landed at ~930.
- The design took three Judgment Day rounds (real warnings per round: 3, 1, 0). Every
  implementation PR took two or three rounds, and **every one of them failed its first round on
  the same class**: tests a one-line mutation survived. PR 1 dropped the cursor's `id` tie-break;
  PR 2 had a negative `== archived` check and unasserted `RelatedUnit` fields; PR 3 had no
  last-page case and an untested filter-forwarding loop; PR 4 derived direction from position;
  PR 5 left both routes' error branches and the htmx config unasserted. Making apply agents
  self-mutate did not stop it, because they enumerated mutations from their own tests. Enumerating
  them from the production diff narrowed it without closing it.
- Two process incidents, neither touching real data: an apply agent ran `nooma init` with no path
  during a binary drive, creating and then deleting `~/.nooma` (it did not exist before — audited
  from the agent's own listing); and a fix agent left an `until … pgrep -f` waiter spinning,
  because `pgrep -f` matched the loop's own command line.

## Carried forward

- A plain (non-htmx) correction submit lands on the full CAPTURE page with no link back to the
  unit. Design §3.5's stated scope; a UX follow-up for later M4 work.
- OR8: a correction the classifier does not read as a correction stores a new unit. Existing
  `CaptureService` semantics, rendered truthfully by the UI.
- N8: `idx_units_live_browse` omits `type`, so a narrow type filter on a type-skewed vault walks
  many index rows per page. Accepted at personal-vault scale; revisit with
  `(status, type, created_at, id)` if measured.
- Unchanged from m4a's archive: the Go toolchain bump (govulncheck), a hash-pinning gate for the
  vendored htmx, and ADR-0019's status, which blocks `m4d`.
