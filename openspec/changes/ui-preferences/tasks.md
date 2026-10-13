# Tasks — ui-preferences

Five links stacked to main (design §3); 3b split from 3 on size.

PR 1 — `fix/digest-once-per-date` (#329)
- [x] 1. RED/GREEN L1 core + brain: at most one digest per local date (D1); doc 02 §7.
- [x] 2. This design and tasks.

PR 2 — `feat/reminder-prefs-save` (#330)
- [x] 3. RED/GREEN L3 + contract: `ConfigRepo.SetReminderPrefs` (D2); memrepo, doubles, store API golden.
- [x] 4. RED/GREEN L1 brain: `PreferencesService.Preferences/Save`, validation, effective no-op, `config.updated` (D3, D4, D6); doc 02 §11.

PR 3 — `feat/reminder-prefs-rearm` (#331)
- [x] 5. RED: I35 row in doc 06 §4 and `TestI35_APreferenceChangeReArmsEventsAhead`.
- [x] 6. RED/GREEN L3 + contract: `TriggerRepo.ArmedUnitIDs`.
- [x] 7. GREEN: re-arm on save through `Follow` with `config.reminder.*` rows (D5, D6); doc 02 §7.
- [x] 7a. RED/GREEN: re-arm only on a change or a pending re-arm (`config.reminders.followed`); the created reminder's level; a re-arm failing after one write.
- [x] 7b. RED/GREEN: pending by insertion order; the double failure said; I35's pending clause.

PR 3b — `feat/reminder-prefs-delivered`
- [x] 7c. RED/GREEN: a reminder already sent is never sent again by a save — each reminder records its event instant and every user lead (collapsed leads included; legacy rows with no lead cover by time), and `FollowSent` treats a lead served for the same instant as covered (`TriggerRepo.FiredForUnit`).
- [x] 7d. RED/GREEN: a carried reminder is rewritten when its event instant or lead list changes (a merged lead, an old row gaining the keys); the residual risk for old rows with a lead stated; the covered reason pinned.

PR 4 — `feat/ui-preferences` (#332)
- [ ] 8. RED/GREEN L1 ui: page, leads editor, errors, saved notice, pending button (D7, D8).
- [ ] 9. Wiring, nav, UI gates; doc 07; `make check-all`.
