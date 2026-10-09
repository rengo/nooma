# Tasks — fix-correction-date-follows

A four-link chain stacked to `main`: #303 `fix/correction-date-follows-text` (tasks 1, 4 text
half, 5 docs), #304 `feat/trigger-reschedule` (task 3), #306 `refactor/arming-builders` (the
`recurringTrigger`/`armedTrigger` extractions), #305 `fix/correction-date-follows` (tasks 2, 4
reminder half).

- [x] 1. RED/GREEN, L1: `correction.FollowDate`, `correction.CarryText`; prompt states the form (R1, R2).
- [x] 2. RED/GREEN, L1: `prospection.Follow` over `datedTrigger`/`recurringTrigger` (R3).
- [x] 3. Port: `TriggerRepo.ArmedForUnit`, `Reschedule`; contract suite, memrepo, sqlite (L2/L3).
- [x] 4. RED: `TestI29_CorrectedDateCarriesTheText`, `TestI29_CorrectedDateMovesTheReminder` (R1, R3, R4) and the I29 row; I28
      wording. GREEN: `correctionRunner` carries text and follows the reminder.
- [x] 5. Doc 02 §5 step 4, doc 07; `make check-all`; PR.

Rework after review: 6. `FollowDate` rewrites only the stated instant, with the reviewers'
adversarial cases and a non-UTC clock fixture; 9. trailing `?`/`#`/`&` are punctuation, dates
glued inside a word or file name are untouched, one case per boundary rule.
7. #304: `TriggerMove` carries the new arming's payload; NULL `fire_at` case; I27 header.
8. #305: reminder rows proven before their writes; partial-failure state documented, a retry
rewrites the text from the trigger's own instant; one trigger builder; anchor shown on a move.
