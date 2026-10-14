# Archive report — `m4e2-admin`

**Closed**: 2026-10-14. **`main` at archive**: `2aedd36`, the merge of PR #336. **Verify**: not
re-run for this archive; the evidence is the three merged PRs (CI green, reviewed) and a
`tasks.md` with 25 ticked items and none open.
**Milestone**: the sixth M4 slice to close (after `m4a`, `m4b`, `m4c`, `m4e`, `m4e-activity`).
`openspec/changes/m4-mirror-ui/proposal.md` stays active for `m4d` and `m4f`.

## What shipped

The user can open `/ui/admin`, see the five hand-set settings with the value in force, change one
at a time (validated, logged to `decision_log` as `config.updated`, and shown in `/ui/activity`),
and see the learned relation thresholds read-only with a "learned" note and the last nightly job.

| PR | Branch | What | Impl+docs / tests |
|---|---|---|---|
| #288 | `plan/m4e-beliefs-activity` | planning, shared with the m4e split | n/a |
| #334 | `feat/config-setters-learned-thresholds` | PR 1a: one typed setter per field, learned-threshold read | 117 / 471 |
| #335 | `feat/brain-admin-service` | PR 1b: `AdminService`, the logged door | 385 / 619 |
| #336 | `feat/ui-admin` | PR 2: the page, the five saves, the nav, the I22 names `View`, `Update` | 283 / 564 |

All three landed under 400 impl+docs lines against a forecast of ~605 in total (measured 785).

## Leaves behind

- The I22 whitelist in `docs/06-harness.md` carries `View` and `Update` for the admin interface.

## Follow-ups (not blocking)

- Reviewer nits on #336, fixed in the housekeeping PR that archived this change: the literal
  `"consolidation_enabled"` in `admin.templ` now uses `brain.ConfigConsolidationEnabled`, and a
  refused `consolidation_enabled` value re-renders with the value in force selected.
- `m4f` (timer list and cancel) and `m4d` (graph, blocked on ADR-0019) remain.
