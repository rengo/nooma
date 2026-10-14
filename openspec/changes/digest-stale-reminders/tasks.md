# Tasks — digest-stale-reminders

Five links stacked to main (design §6).

PR 1 — `plan/digest-stale-reminders`
- [x] 1. ADR-0032 and its index row; this design and tasks.
- [x] 1a. Client decisions of 2026-10-14: ADR-0032 Accepted; one at-once reminder for an event
      never reminded (D6); the correction-text defect recorded as a follow-up.

PR 2 — `fix/undelivered-reminder-gate`
- [ ] 2. RED: I37 row in doc 06 §4 and `TestI37_AnUndeliveredReminderIsJudgedAgain` with the
      client's seven rows.
- [ ] 3. RED/GREEN, L1: `Reminder`, `ReminderVerdict`, `LapseReason`; `EventReminderVerdict`
      delegates to it (D1).
- [ ] 4. RED/GREEN, L3: `TriggerRepo.Lapse` in sqlite, memrepo and the repo contract (D4).
- [ ] 5. GREEN: the scan judges at fire time through the gate. It retries undelivered one-shot
      rows by push and lapses the stale ones with `check.trigger.lapsed`. The digest carries
      recurring rows only, gated. Today mirrors the digest (D2-D3).
- [ ] 5a. RED/GREEN: a pass with no destination leaves one-shot rows untouched (D3a).
- [ ] 6. Doc 02 §7 (drop the at-once exception, add the lapse) and §11. Doc 03 `triggers`, doc 07.
      Run `make check-all`, then open the PR.

PR 3 — `fix/lapsed-reminder-replans`
- [ ] 7. RED: `TestI37_AnEventNeverRemindedGetsOneReminder` (design §7). I35 widened, so that
      an undelivered row covers no lead.
- [ ] 8. RED/GREEN, L1: `prospection.CatchUp`, covering leads ahead, one at once, nothing, and
      quiet end versus the event (D6).
- [ ] 8a. RED/GREEN, L3: `Create` of an existing id returns `ErrTriggerExists` in sqlite,
      memrepo and the repo contract.
- [ ] 8b. GREEN: `FiredForUnit` reads delivered rows only. Add the per-unit re-plan set,
      `checkCause`, the deterministic catch-up id and `catch_up` payload, and the `covered` lapse.
- [ ] 9. Doc 02 §7 and §11. Run `make check-all`, then open the PR.

PR 4 — `fix/reminder-renders-the-unit`
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
| 2 | ~360 | ~380 | Medium: scan behaviour, one port method |
| 3 | ~240 | ~320 | Medium: arms a reminder from the scan; exactly-once rests on the id |
| 4 | ~260 | ~250 | Low: rendering |
| 5 | ~220 | ~200 | Medium: migration + backfill |

Every link is under 400. Chained PRs recommended: Yes (stacked to main). Decision needed before
apply: none. ADR-0032 is accepted and the open question is answered.

Follow-up, not in this chain: the correction on 2026-10-10T19:35Z that stored "Es a las 08:00."
as the unit's text and left its time at 09:00 (design §2, defect 2) needs its own change.
