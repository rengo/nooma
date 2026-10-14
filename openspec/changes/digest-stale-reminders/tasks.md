# Tasks — digest-stale-reminders

Seven links stacked to main (design §6). Each leaves main consistent. **Acceptance gate:** every
named RED test below is written and watched failing, for the reason stated, before its link's
GREEN code. These tests, not the prose, are what pin D6.

PR 1 — `plan/digest-stale-reminders`
- [x] 1. ADR-0032 (Accepted, 2026-10-14), the index row, design, tasks; review rounds 1-3.

PR 2 — `fix/never-remind-of-the-past` (D1 `passed`, D4, `FiredForUnit`)
- [x] 2. RED tests:
  - `TestI37_ClientsSevenRowsSendNothingPast`: the client's seven rows → the three past flights
    lapse `passed`, the four ahead are carried.
  - `TestI37_ArmedOverdueKeepsTodaysVerdict`: an armed row overdue in link 2 → expired as today.
  - `TestI37_DigestIgnoresOverdueUntilLink3`: a fired row 7 h old, event ahead → carried.
  - `TestI37_PastBirthdayIsNeverSent`: a recurring row whose occurrence passed → lapsed.
  - `TestReminderVerdict_RecurringOccurrenceInClockZone`: anchor plus fire_at in UTC-3 → the
    local occurrence, not the UTC one.
  - `TestReminderVerdict_ZeroLeadIsNotPassed`: `lead_minutes` 0, no `event_at` → About unknown.
  - `TestTodayCountsAfterTheGate`: a past row → not counted, and no write.
  - `TestDryRunCountsALapseAndWritesNothing`: a past fired row → `TriggersLapsed` 1, no row.
  - `TestFiredForUnit_LapsedRowDoesNotCover` (L3): a lapsed row → absent; a fired row → present.
  - Repo contract: `Lapse` only `fired`/unsurfaced, else a conflict.
- [x] 3. GREEN: `ReminderVerdict` (`passed`, `About`), `Lapse`, the gate (fire time, digest, Today),
      `FiredForUnit`, `check.trigger.lapsed` (action, label, counter, dry run). Doc 02 §7/§11,
      doc 03, doc 07, I37 row. `make check-all`, open the PR.

PR 3a `fix/late-reminders-held` and PR 3b `fix/late-reminders-replan` (D1 rest, D8, D6 without the catch-up).
Split (the client's vault would otherwise lose its reminders): 3a judges and HOLDS, retiring only `passed` rows;
3b writes the plan first and then retires the late and superseded rows. 3a owns the digest-skip, verdict, D8,
`BirthdayWaiting`, `LowEnergyHeld`, `CannotDeliverExpiresAsToday` and I35 tests below; 3b owns the rest.
- [ ] 4. RED tests:
  - `TestI37_StaleL24HealthyL2KeepsOneAtTheTwoHourLead` (C1): stale 24 h + healthy 2 h →
    exactly one armed row, at the 2 h lead.
  - `TestI37_SupersededRowReplansTheNewInstant`: a fired row about the old date, the unit
    corrected ahead → the leads of the new date.
  - `TestI37_AbortMidPassThenCleanPassCreatesNothingTwice`: `Lapse` fails after the plan is
    written → the next pass carries the plan, with no duplicate.
  - `TestI37_DentistLapsedInDigestPathIsReplanned`: an overdue fired row with a 2 h lead ahead →
    the 2 h row is armed before the digest runs.
  - `TestI37_DigestSkipsHeldAndStaleOneShotRows`: neither is sent nor lapsed by the digest.
  - `TestI37_UnitOwedOnlyACatchUpIsHeld`: every lead behind, nothing sent → no writes.
  - `TestI37_BirthdayWaitingForTheDigestIsCarried`: recurring, fired 18 h before the digest →
    carried.
  - `TestI37_LowEnergyHeldRowIsNotOverdue`: held three digests → still carried.
  - `TestI37_LegacyRowSupersededByNewerArming` (D8): a legacy fired row + an armed lead → the
    legacy row lapses `superseded`.
  - `TestI37_CanDeliverDefersOverdueArmedToTheStep`; `TestI37_CannotDeliverExpiresAsToday`.
  - `TestI37_DryRunEqualsTheRealScan`: same report, zero writes.
  - `TestI35_QuietPulledLeadInFlightCovers`: lead fired at 22:00, push failed, preferences
    changed at 23:00 → no at-once arm while the row is in flight.
  - `TestReminderVerdict_SupersededAtSecondPrecision`: nanosecond-different instants → not
    superseded.
  - `TestReminderVerdict_SupersededOnlyFromEventAt`: About from `lead_minutes` → never superseded.
- [ ] 5. GREEN: the reasons, "can deliver", the due loop's deferral, the D6 step (`live`/`sent`
      exclusions, plan before retire), `checkCause` with its actions and counters, the digest skip.
      Doc 02 §7/§11. Run `make check-all`, then open the PR.

PR 4 — `fix/no-destination-and-catch-up`
- [ ] 6. RED tests:
  - `TestI37_LoneStaleFiredRowYieldsACatchUp` (C2): one stale fired row, every lead behind →
    one at-once row.
  - `TestI37_TwoLapsedLeadsOnePassOneCatchUp`, and `ThreeMorePassesNoSecond`.
  - `TestI37_CatchUpAfterUndeliveredLapseIsAttemptTwo`: catch-up 1 lapses undelivered → one
    `:2` row; once one is delivered → none.
  - `TestI37_ViableSiblingBlocksTheCatchUp`: a fired row still deliverable → no catch-up.
  - `TestI37_SupersededInQuietHoursCatchUpAtQuietEnd`, and `QuietEndAfterEventIsRefused`.
  - `TestI37_NilChannelTouchesNoRowAheadAndArmsNothing`; `TestI37_NoConversationOneLoudRow`.
  - `TestI37_TodayListsArmedRemindersWithoutAChannel` (I27 still holds).
  - `TestI15_OverdueExpiresWithAChannel`, and `IsKeptWithANilChannel` (unit-less included).
  - `TestMoveReminderClearsCatchUpOnANewInstant`.
  - Repo contract: `OneShotForUnit` returns expired rows; `Create` of a taken attempt id →
    `ErrTriggerExists`.
- [ ] 7. GREEN: cannot-deliver, Today's list, `OneShotForUnit` (sqlite, memrepo), `CatchUp`,
      `armCatchUp`, `TriggerPayload.CatchUp`, the refused writer, `check.reminder.refused` (doc 02
      §11, `ui/activitylabel.go`). Doc 02 §5/§7, doc 07. `make check-all`, open the PR.

PR 5 — `fix/push-retries-undelivered`
- [ ] 8. RED tests: `TestI37_FailedPushRetriedOncePerPass` (not twice in the pass that fired
      it); `TestI37_SurfaceFailureIsNotResentByTheProcess`; `TestI37_OutageWritesOneRowPerReminder`;
      `TestDueScanConcurrent_SurfaceConflictIsBenign`; `TestI37_DigestCarriesRecurringOnly`.
- [ ] 9. GREEN: the retry and its bounds. Doc 02 §7. Run `make check-all`, then open the PR.

PR 6 — `fix/reminder-renders-the-unit`
- [ ] 10. RED tests: `TestPhraseSetsComplete`; `TestRenderUsesCurrentContentAndInstant`;
      `TestDateOnlyEventRendersWithoutATime`; `TestOneMessagePerUnitArmedSiblingUntouched`;
      `TestMissingUnitFallsBackToAction`.
- [ ] 11. GREEN: render from the unit. Doc 02 §7, doc 07. Run `make check-all`, then open the PR.

PR 7 — `feat/proactive-language`
- [ ] 12. RED tests: `TestBackfillTakesTheEarliestClassifyByRowid`; `TestBackfillEmptyLanguageIsNull`;
      `TestLatestLanguageIncludesArchived`; `TestI37_SpanishUnitPushAndDigestInSpanish`;
      `TestPushFallsBackToLatestThenEnglish`.
- [ ] 13. GREEN: the migration, the golden, `LatestLanguage`, capture writing the language. Doc 02
      §5, doc 03. Run `make check-all`, then open the PR.

## Review Workload Forecast

| Link | Impl+docs | Tests (apart) | Risk |
|---|---|---|---|
| 2 | ~330 | ~380 | Medium |
| 3 | ~390 | ~520 | High: the step, pinned by the list above |
| 4 | ~400 | ~450 | High: exactly one delivered rests on the attempt id |
| 5 | ~200 | ~280 | Medium |
| 6 | ~260 | ~250 | Low |
| 7 | ~220 | ~200 | Medium |

Estimates: a link measured over 400 at apply time stops for a split. Follow-up: design §2.
