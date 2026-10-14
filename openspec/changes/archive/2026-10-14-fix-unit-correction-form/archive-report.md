# Archive report — `fix-unit-correction-form`

**Closed**: 2026-10-14. **`main` at archive**: `2aedd36`. **Verify**: not re-run for this
archive; the PR below is merged and `tasks.md` has 12 ticked items and none open.

## What shipped

The unit page's correction form always corrects that unit: an explicit referent is a correction
before check-ins and arming (I28), an unknown referent answers 404, and the UI says in plain
sentences when nothing was changed.

| PR | Branch | What |
|---|---|---|
| #302 | `fix/ui-unit-correction-targets-unit` | `captureRunner.at` forks on `in.ReferentID`; `correction.IsEdit`; UI sentences |

Two review rework rounds are recorded in `tasks.md`.
