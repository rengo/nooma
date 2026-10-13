# Tasks — ui-preferences

Four links stacked to main (design §3).

PR 1 — `fix/digest-once-per-date` (#329)
- [x] 1. RED/GREEN L1 core + brain: at most one digest per local date (D1); doc 02 §7.
- [x] 2. This design and tasks.

PR 2 — `feat/reminder-prefs-save` (#330)
- [ ] 3. RED/GREEN L3 + contract: `ConfigRepo.SetReminderPrefs` (D2); memrepo, doubles, store API golden.
- [ ] 4. RED/GREEN L1 brain: `PreferencesService.Preferences/Save`, validation, effective no-op, `config.updated` (D3, D4, D6); doc 02 §11.

PR 3 — `feat/reminder-prefs-rearm`
- [ ] 5. RED: I35 row in doc 06 §4 and `TestI35_APreferenceChangeReArmsEventsAhead`.
- [ ] 6. RED/GREEN L3 + contract: `TriggerRepo.ArmedUnitIDs`.
- [ ] 7. GREEN: re-arm on save through `Follow` with `config.reminder.*` rows (D5, D6); doc 02 §7.

PR 4 — `feat/ui-preferences`
- [ ] 8. RED/GREEN L1 ui: page, leads editor, errors, saved notice, pending button (D7, D8).
- [ ] 9. Wiring, nav, UI gates; doc 07; `make check-all`.
