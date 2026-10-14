# Tasks — digest-stale-reminders

Five links stacked to main (design §6).

PR 1 — `plan/digest-stale-reminders`
- [x] 1. ADR-0032 (Proposed) and its index row; this design and tasks.

PR 2 — `fix/undelivered-reminder-gate`
- [ ] 2. RED: I37 row in doc 06 §4 and `TestI37_AnUndeliveredReminderIsJudgedAgain` with the
      client's seven rows.
- [ ] 3. RED/GREEN, L1: `Reminder`, `ReminderVerdict`, `LapseReason`; `EventReminderVerdict`
      delegates to it (D1).
- [ ] 4. RED/GREEN, L3: `TriggerRepo.Lapse` in sqlite, memrepo and the repo contract (D4).
- [ ] 5. GREEN: the scan judges at fire time through the gate. It retries undelivered one-shot
      rows by push and lapses the stale ones with `check.trigger.lapsed`. The digest carries
      recurring rows only, gated. Today mirrors the digest (D2-D3).
- [ ] 6. Doc 02 §7 (drop the at-once exception, add the lapse) and §11. Doc 03 `triggers`, doc 07.
      Run `make check-all`, then open the PR.

PR 3 — `fix/lapsed-reminder-replans`
- [ ] 7. RED: I37 widened, so that a lapsed reminder for an event ahead arms its leads still
      ahead and none at once. I35 widened, so that an undelivered row covers no lead.
- [ ] 8. GREEN: `FiredForUnit` reads delivered rows only. Add `checkCause` and the re-plan after
      a lapse (D6).
- [ ] 9. Doc 02 §7. Run `make check-all`, then open the PR.

PR 4 — `fix/reminder-renders-the-unit` (after ADR-0032 is Accepted)
- [ ] 10. RED/GREEN, L1: the new `phrase.Set` fields in `en` and `es` (completeness test).
- [ ] 11. RED/GREEN: push, digest and Today render from the unit's current text and instant, one
      line per unit, every row of the unit surfaced (D5).
- [ ] 12. Doc 02 §7 and doc 07. Run `make check-all`, then open the PR.

PR 5 — `feat/proactive-language`
- [ ] 13. RED/GREEN, L3: migration with `units.language` and its backfill, the schema golden,
      `LatestLanguage`.
- [ ] 14. GREEN: capture writes the language. Push uses the unit's language and the digest uses
      the latest one (D7). L2 test in Spanish.
- [ ] 15. Doc 02 §5 (close ADR-0022's gap), doc 03 `units`. Run `make check-all`, then open the PR.

## Review Workload Forecast

| Link | Impl+docs | Tests (apart) | Risk |
|---|---|---|---|
| 2 | ~330 | ~350 | Medium: scan behaviour, one port method |
| 3 | ~150 | ~200 | Low |
| 4 | ~260 | ~250 | Low: rendering |
| 5 | ~220 | ~200 | Medium: migration + backfill |

Every link is under 400. Chained PRs recommended: Yes (stacked to main). Decision needed before
apply: ADR-0032 acceptance (links 4-5), and the PM's answer to the open question in the report.
