# Tasks — digest-stale-reminders

Seven links, stacked to main (design §6). Each one leaves main consistent.

PR 1 — `plan/digest-stale-reminders`
- [x] 1. ADR-0032 (Accepted by the client, 2026-10-14), its index row, this design and tasks.
      Review rounds 1-2 folded in.

PR 2 — `fix/never-remind-of-the-past`
- [ ] 2. RED: the I37 row in doc 06 §4, and `TestI37_ThePastIsNeverSent` over the client's
      seven rows.
- [ ] 3. RED/GREEN, L1: `ReminderVerdict` with `passed` only; `About` resolution, including
      `NextOccurrence` for recurring rows. `EventReminderVerdict` delegates to it.
- [ ] 4. RED/GREEN, L3: `Lapse` in sqlite, memrepo and the repo contract; status vocabulary.
- [ ] 5. GREEN: the gate at fire time (armed rows keep today's `overdue`), in the digest (only
      `passed` lapses) and in Today (read-only, counted after the gate).
- [ ] 6. `check.trigger.lapsed`: action, label, `TriggersLapsed`, I12, CLI, dry run. Doc 02 §7
      and §11, doc 03, doc 07. Run `make check-all`, then open the PR.

PR 3 — `fix/late-reminders-replan`
- [ ] 7. RED: L2 re-plan, abort mid-pass, a waiting birthday carried, dry run equal to the
      real scan, D8. I35 widened.
- [ ] 8. RED/GREEN, L1: `superseded` (`FromEvent`, second precision, `Newer`), `overdue`,
      `Waiting`, and `CatchUp` without the at-once branch.
- [ ] 9. GREEN: `FiredForUnit` returns `status = 'fired'` rows. The reminder step re-plans
      before it lapses, runs before the digest, holds a unit owed only a catch-up, and
      `checkCause` covers armed, moved and cancelled.
- [ ] 10. Actions, labels and report counters for the three. Doc 02 §7 and §11. Run
      `make check-all`, then open the PR.

PR 4 — `fix/no-destination-and-catch-up`
- [ ] 11. RED: amend I15 (a channel case, a nil-channel case, the doc 06 row text). L2: Today
      lists armed one-shot rows when there is no channel. One catch-up across passes, and an
      undelivered catch-up gets exactly one successor.
- [ ] 12. RED/GREEN, L1: the `CatchUp` at-once branch through `deliverAt`, and the attempt
      count.
- [ ] 13. GREEN: "can deliver", the cannot-deliver branch, the no-conversation row once per
      process, Today's list, `armCatchUp`, `TriggerPayload.CatchUp` in `payloadJSON` (cleared by
      `moveReminder`), and the refused writer.
- [ ] 14. `check.reminder.refused`: action, label, counter. Doc 02 §5 (ADR-0029 point 4) and §7
      (ADR-0009), doc 07. Run `make check-all`, then open the PR.

PR 5 — `fix/push-retries-undelivered`
- [ ] 15. RED: one push per pass, skipping touched rows; a benign `Surface` conflict
      (`due_scan_concurrent_test.go`); the sent-set; one failure row per outage.
- [ ] 16. GREEN: the push retry and its bounds; the digest carries recurring rows only. Doc 02
      §7. Run `make check-all`, then open the PR.

PR 6 — `fix/reminder-renders-the-unit`
- [ ] 17. RED/GREEN, L1: the new `phrase.Set` fields and `Set.Date`, in `en` and `es`.
- [ ] 18. RED/GREEN: push, digest and Today render the unit as it is now; date-only events have
      no time; one message per unit per pass. Doc 02 §7, doc 07. Run `make check-all`, then
      open the PR.

PR 7 — `feat/proactive-language`
- [ ] 19. RED/GREEN, L3: the migration and its grouped backfill (`rowid`, `NULLIF`), the
      golden, and `LatestLanguage` in the port, sqlite and memrepo.
- [ ] 20. GREEN: capture writes the language; push and digest choose it per D7; an L2 test in
      Spanish. Doc 02 §5, doc 03. Run `make check-all`, then open the PR.

## Review Workload Forecast

| Link | Impl+docs | Tests (apart) | Risk |
|---|---|---|---|
| 2 | ~320 | ~350 | Medium |
| 3 | ~390 | ~420 | High: the re-plan from the scan, crash-safe by its ordering |
| 4 | ~380 | ~380 | High: exactly one delivered rests on the attempt id |
| 5 | ~200 | ~280 | Medium |
| 6 | ~260 | ~250 | Low |
| 7 | ~220 | ~200 | Medium |

Links 3 and 4 sit close to 400, and the estimates are not measured. If one exceeds it at apply
time, I stop and propose the split before pushing. Decision needed before apply: none.

Follow-up, its own change: the "Es a las 08:00." correction (design §2).
