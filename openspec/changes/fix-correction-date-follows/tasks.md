# Tasks — fix-correction-date-follows

One PR, branch `fix/correction-date-follows`.

- [ ] 1. RED/GREEN, L1: `correction.FollowDate`, `correction.CarryText`; prompt states the form (R1, R2).
- [ ] 2. RED/GREEN, L1: `prospection.Follow` over `datedTrigger`/`recurringTrigger` (R3).
- [ ] 3. Port: `TriggerRepo.ArmedForUnit`, `Reschedule`; contract suite, memrepo, sqlite (L2/L3).
- [ ] 4. RED: `TestI29_CorrectedDateCarriesTextAndReminder` (R1, R3, R4) and the I29 row; I28
      wording. GREEN: `correctionRunner` carries text and follows the reminder.
- [ ] 5. Doc 02 §5 step 4, doc 07; `make check-all`; PR.
