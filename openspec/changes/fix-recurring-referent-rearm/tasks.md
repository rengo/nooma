# Tasks — fix-recurring-referent-rearm

One PR, `fix/recurring-referent-rearm`.

- [x] 1. RED: `TestI40_ARemindMeOfARecurringReminderSaysItAlreadyDoes`,
      `TestI40_ARemindMeOfAOneOffWhoseTimePassedStillSaysSo` (green already: pins today's behavior),
      `TestI40_AMoveOfARecurringReminderReanchorsIt`; L1 `TestFollowAsked_ARecurringReminderStaysRecurring`,
      `TestSet_AlreadyEvery`.
- [x] 2. GREEN: `prospection.FollowAsked` keeps a live recurrence (D2).
- [x] 3. GREEN: `brain.remindRecurring`, `Armed.Already`, `phrase.AlreadyEvery`, reply (D1, D3).
- [x] 4. Mutation: each fix reverted turns its tests red.
- [x] 5. Doc 02 §5 step 4, doc 06 I40 row, doc 07; `make check-all`; PR.
