# Tasks — fix-correction-date-follows

A four-link chain stacked to `main`: #303 `fix/correction-date-follows-text` (tasks 1, 4 text
half, 5 docs), #304 `feat/trigger-reschedule` (task 3), #306 `refactor/arming-builders` (the
`recurringTrigger`/`armedTrigger` extractions), #305 `fix/correction-date-follows` (tasks 2, 4
reminder half).

- [x] 1. RED/GREEN, L1: `correction.FollowDate`, `correction.CarryText`; prompt states the form (R1, R2).
- [ ] 2. RED/GREEN, L1: `prospection.Follow` over `datedTrigger`/`recurringTrigger` (R3).
- [ ] 3. Port: `TriggerRepo.ArmedForUnit`, `Reschedule`; contract suite, memrepo, sqlite (L2/L3).
- [ ] 4. RED: `TestI29_CorrectedDateCarriesTheText`, `TestI29_CorrectedDateMovesTheReminder` (R1, R3, R4) and the I29 row; I28
      wording. GREEN: `correctionRunner` carries text and follows the reminder.
- [ ] 5. Doc 02 §5 step 4, doc 07; `make check-all`; PR.

Rework after review of #303/#305:

- [x] 6. `FollowDate` rewrites only the stated instant (date token, anchored time); adversarial
      cases; zone-order case; a non-UTC clock fixture.
- [x] 9. #303 second review: trailing `?`/`#`/`&` are punctuation; dates glued inside a word or
      file name are not rewritten; a discriminating case per boundary rule and connector.
