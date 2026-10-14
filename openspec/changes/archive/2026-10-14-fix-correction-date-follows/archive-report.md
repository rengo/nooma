# Archive report — `fix-correction-date-follows`

**Closed**: 2026-10-14. **`main` at archive**: `2aedd36`. **Verify**: not re-run for this
archive; every PR below is merged and `tasks.md` has 5 ticked items plus the rework notes, none open.

## What shipped

Correcting an event's date rewrites the date its own text states and moves the unit's reminder
with it; a correction that changes text and time together keeps the new text.

| PR | Branch | What |
|---|---|---|
| #303 | `fix/correction-date-follows-text` | `correction.FollowDate`, `CarryText` |
| #304 | `feat/trigger-reschedule` | `TriggerRepo.ArmedForUnit`, `Reschedule` |
| #306 | `refactor/arming-builders` | the recurring and trigger builders extracted |
| #305 | `fix/correction-date-follows` | `prospection.Follow`, `TestI29_CorrectedDate*` |

Related fixes landed after it and are not part of it: #312, #313, #327.
