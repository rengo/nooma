# Tasks — event-reminder-leads

Four links stacked to `main` (design §3).

PR 1 — `docs/event-reminder-leads-plan`
- [x] 1. ADR-0029 (Proposed) and its index row; this design and tasks.

PR 2 — `feat/reply-names-every-reminder`
- [x] 2a. RED/GREEN, L1: `phrase.Set.Lead` by calendar day, `Leads`, `And`; `brain.Armed.Later`;
      the reply names every firing (D6).

PR 3 — `feat/event-reminder-leads`
- [x] 2. RED: I31 row in doc 06 §4 and `TestI31_EventRemindersAtTheLeads`; I29 widened to the set.
- [x] 3. RED/GREEN, L1: `ReminderPrefs`, event firings, `Arm` returning the set (D1-D3).
- [x] 4. RED/GREEN, L1: `Follow` over a set, no at-once creation on a correction (D5).
- [x] 5. GREEN: capture writes one trigger and one row per plan, filling `Armed.Later`;
      `lead_minutes` (D4); correction follows the set.
- [x] 6. Doc 02 §5 steps 4-5, §7 "Lead time", §13; doc 07; `make check-all`; PR.

PR 4 — `feat/reminder-preferences`
- [x] 7. RED/GREEN, L1: `ParseEventReminderLeads`, `ParseTimeOfDay`, `ResolveReminderPrefs` (D7).
- [x] 8. RED/GREEN, L3: migration 0006, `ConfigRepo.Load`, memrepo, schema golden.
- [x] 9. RED/GREEN: capture and correction read the preferences at arm time; wiring test (D8).
- [x] 10. Doc 02 §13 "user-overridable", doc 03 `config`; `make check-all`; PR.
