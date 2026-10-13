# Tasks — event-reminder-delivery

Three links stacked on `event-reminder-leads` (design §3).

PR 1 — `docs/event-reminder-delivery-plan`
- [x] 1. ADR-0029 → Accepted; ADR-0030 (Proposed) and its index row; this design and tasks.

PR 2 — `feat/event-reminder-delivery`
- [ ] 2. RED: I32 row in doc 06 §4 and `TestI32_EventRemindersArePushedAndNeverLate`; I29 widened.
- [ ] 3. RED/GREEN, L1: `deliverAt`, collapse, `RefusalQuietHours` in `eventReminders` (D2-D3).
- [ ] 4. GREEN: capture records a `quiet_hours` drop (D5).
- [ ] 5. RED/GREEN, L1: `RouteTrigger`, `EventReminderVerdict`; the scan uses both (D6-D7).
- [ ] 6. Doc 02 §7 and §13, doc 07; `make check-all`; PR.

PR 3 — `feat/quiet-hours-preferences`
- [ ] 7. RED/GREEN, L1: quiet end and evening parsers, `ResolveReminderPrefs` (D8).
- [ ] 8. RED/GREEN, L3: migration 0007, `ConfigRepo.Load`, schema golden.
- [ ] 9. RED/GREEN: `QuietHours` in the verdicts and `DigestDue`; the scan reads it (D9).
- [ ] 10. Doc 02 §7/§13, doc 03 `config`; `make check-all`; PR.
