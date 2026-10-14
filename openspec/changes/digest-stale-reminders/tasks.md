# Tasks — digest-stale-reminders

Six links stacked to main (design §6); each leaves main consistent.

PR 1 — `plan/digest-stale-reminders`
- [x] 1. ADR-0032 (Accepted by the client, 2026-10-14), its index row, this design and tasks;
      review round 1 folded in.

PR 2 — `fix/never-remind-of-the-past`
- [ ] 2. RED: I37 row in doc 06 §4 and `TestI37_ThePastIsNeverSent`, using the client's seven rows.
- [ ] 3. RED/GREEN, L1: `Reminder`, `ReminderVerdict` with the `passed` reason, and `About`
      resolution (D1). `EventReminderVerdict` delegates to it.
- [ ] 4. RED/GREEN, L3: `TriggerRepo.Lapse` in sqlite, memrepo and the repo contract. Name
      `expired` + `fired_at` in the status-vocabulary test (D4).
- [ ] 5. RED/GREEN: `FiredForUnit` returns delivered rows only, and I35 is widened.
- [ ] 6. GREEN: the gate at fire time, in the digest (which lapses `passed` rows, recurring
      included) and in Today (read-only, counts after the gate). Units are read per row (D1-D2).
- [ ] 7. `check.trigger.lapsed` (actions, label), `TriggersLapsed` (report, I12, CLI), dry run.
      Doc 02 §7/§11, doc 03 `triggers`, doc 07. Run `make check-all`, then open the PR.

PR 3 — `fix/late-reminders-replan`
- [ ] 8. RED: `TestI37_ALateReminderIsReplannedOnce` (design §7, link 3), and a recurring row
      waiting 18 h is carried. Amend I15 (channel and nil-channel cases, doc 06 row).
- [ ] 9. RED/GREEN, L1: the `superseded` (second precision) and `overdue` reasons, `Waiting`,
      and `prospection.CatchUp` (D6).
- [ ] 10. GREEN: D3a, nil channel only. Add the per-unit re-plan set and its three conditions,
      `checkCause`, `armCatchUp` (create, then record; `ErrTriggerExists` means claimed),
      `TriggerPayload.CatchUp` in `payloadJSON`, the flag kept by `moveReminder`, and the
      `covered` lapse.
- [ ] 11. Add `check.reminder.armed`/`refused` (actions, labels) and `RemindersArmed`/`Refused`
      (report, I12, CLI).
- [ ] 12. Doc 02 §5 (ADR-0029 point 4 amended) and §7 (ADR-0009 amended, nil channel), §11. Run
      `make check-all`, then open the PR.

PR 4 — `fix/push-retries-undelivered`
- [ ] 13. RED: the retry pushes once per pass and skips rows already touched. A `Surface`
      conflict is benign, with a case in
      `due_scan_concurrent_test.go`.
- [ ] 14. GREEN: the retry, the sent-set, and the digest carrying recurring rows only (D3).
- [ ] 15. Doc 02 §7. Run `make check-all`, then open the PR.

PR 5 — `fix/reminder-renders-the-unit`
- [ ] 16. RED/GREEN, L1: the new `phrase.Set` fields and `Set.Date` in `en` and `es`.
- [ ] 17. RED/GREEN: push, digest and Today render the unit's current text and instant. Date-only
      events show no time. Rows of one unit sent in one pass become one message, each surfaced (D5).
- [ ] 18. Doc 02 §7, doc 07. Run `make check-all`, then open the PR.

PR 6 — `feat/proactive-language`
- [ ] 19. RED/GREEN, L3: migration with `units.language` and its backfill (earliest row,
      `NULLIF`), the schema golden, and `LatestLanguage` in the port, sqlite and memrepo.
- [ ] 20. GREEN: capture writes the language. Push and digest pick it as D7 says. L2 test in
      Spanish.
- [ ] 21. Doc 02 §5 (ADR-0022's gap), doc 03 `units`. Run `make check-all`, then open the PR.

## Review Workload Forecast

| Link | Impl+docs | Tests (apart) | Risk |
|---|---|---|---|
| 2 | ~300 | ~350 | Medium: gate, one port method |
| 3 | ~380 | ~400 | High: arms from the scan; exactly-once rests on the id |
| 4 | ~170 | ~250 | Medium: retry and its bounds |
| 5 | ~260 | ~250 | Low |
| 6 | ~220 | ~200 | Medium: migration + backfill |

Every link is under 400. Decision needed before apply: none.

Follow-up, not in this chain: the correction on 2026-10-10T19:35Z that stored "Es a las 08:00."
as the unit's text and left its time at 09:00 (design §2) needs its own change.
