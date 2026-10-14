# Tasks — remind-me-reminders, links 5-9 (`design-repeating.md`; plan PR 4 of 4)

**Apply gate:** every named RED test below is written and watched failing for its stated reason
before its link's GREEN code. Order (P1): 5 → 6 (stop) → 7 (the first link that repeats) → 8 → 9.

| Link | Impl+docs | Tests | Risk |
|---|---|---|---|
| 5 | ~220 | ~220 | Medium: a rebuild of published data |
| 6 | ~390 | ~480 | High: a durable stop through every path |
| 7 | ~380 | ~420 | High: every recurring trigger changes (P2) |
| 8 | ~300 | ~300 | High: binding without the model |
| 9 | ~260 | ~260 | Medium |

## PR 5 — `feat/question-store-widens` (D10-D13)

- [ ] 5.1 RED:
  - L3 `TestM2_RebuildKeepsEveryRelationQuestionAndIndex`: rows and both indexes are the same after.
  - `TestPendingQuestionVocabularyDDL`: the new kinds and resolutions, matching the column comments.
  - Contract `TestAskExpiresTheOpenReminderQuestion`: two `Ask` calls → one open.
  - `TestRelationReadsIgnoreReminderKinds`.
  - `TestLatestOpenAskedAtSpansKinds`.
  - `TestEveryReminderKindHasAWindow`: a sweep over the new kinds → each has its §13 window; `ask_back_window_minutes`, `which_or_keep_window_minutes` and `stop_reply_window_minutes` all land in this link (round 3, item 3).
- [ ] 5.2 GREEN: port, sqlite, memrepo, goldens; doc 03; §11 actions; `make check-all`.

## PR 6 — `feat/remind-me-stop` (D20-D24)

- [ ] 6.1 RED L1 classify `StopsReminding`:
  - "no me lo recuerdes más" and "listo, no me lo recuerdes más" → stop;
  - "don’t/don't/dont remind me anymore" → stop, all three alike;
  - "no me recuerdes la clave" → not a stop.
- [ ] 6.2 RED, the I38 row:
  - `TestI38_StopStopsTheNamedReminder`: two asked reminders; "no me recuerdes más lo del gas" → only the gas one is dismissed.
  - `TestI38_StopIsDurableAgainstCorrection`: stop, then a date correction → nothing armed (item 3).
  - `TestI38_StopIsDurableAgainstRePlanAndPreferences`: #344's re-plan (`replanUnit`) and a lead change → nothing armed.
  - `TestI38_StopWinsTheRaceWithReArm`: a guarded insert after the tombstone → `ErrUnitStopped`.
  - `TestI38_LastDeliveryBindsOnlyIfNothingLaterWasSent`: the asked reminder delivered, then an unasked push → `which_reminder` asked (item 10).
  - `TestI38_AmbiguousStopAsksWhichAndKeepsTheIntent`: answer "2" → the second is stopped.
  - `TestI38_ABirthdayCanBeStopped` (P1).
  - `TestI38_PhraseConsumedMessageSkipsCheckIn`: "no me recuerdes más" with an unasked check-in open → the check-in is untouched (item 9).
  - `TestI38_MixedPhraseAndCaptureIsConsumedKnownLimit` (pinned).
  - `TestI38_StoppedBirthdayNotCarriedByTheDigest`: a digest-routed birthday, fired, stopped before the digest → not carried, lapsed `stopped` (round 2, item 1).
  - `TestI38_CrashAfterTombstoneSendsNothing`: the tombstone is written, then the pass aborts before the dismissals; the next due scan and the digest → nothing sent.
  - `TestI38_DigestTieAlwaysAsksWhich`: a digest surfaced 3 **unasked** birthdays with one `now`; "no me lo recuerdes más" → `which_reminder` listing all three, nothing stored as a note (round 3, item 1).
  - `TestI38_TiePlusOneAskedCandidateStillAsks`: the same tie, plus one armed asked reminder → `which_reminder`, the asked one not stopped.
  - `TestI38_BirthdayStoppedByNameADayLater`: "no me recuerdes más lo del cumple de Ana" a day after its delivery → that birthday stopped (round 3, item 6).
  - `TestI38_UnreadableUnitHoldsTheRow`: `ByID` fails → `Defer`, not sent; a missing unit → judged as usual (round 3, item 2).
  - `TestI38_StopCheckReadsTheUnitForEveryRow`: a recurring row and a lead row of a stopped unit → `LapseStopped`.
  - `TestI38_WhichListCappedAtFive`: 12 birthdays → 5 options, newest first.
  - `TestI38_ReAskAfterStopIsANewUnit`: stop, then "recordame tomar pastillas todos los días a las 9pm" → a new unit is armed and the reply says when; the old unit stays stopped (P6).
  - `TestI38_AskedMessageClassifiedCorrectionIsANewCapture`: the model says `correction` for an asked re-ask → no correction fork; a new unit (round 3, minor).
  - `TestI38_CorrectionOfStoppedUnitIsRefusedWithReason`: the reply is the `stopped` sentence, never a bare "Anotado." (item 5).
  - `TestEveryCreateCallerSkipsErrUnitStopped`: an AST sweep of `brain` (item 6).
  - `TestI38_ConsumedRelationAnswerExpiresOlderRemindWhen`: `remind_when`, then a newer relation question, then "sí" → the relation is answered and `remind_when` expired; a later "no" → no daily (item 7).
  - `TestI38_NewerDeliveryExpiresKeepOrStop`: `keep_or_stop` asked, then a push delivered, then "no" → nothing deleted (item 13).
  - `TestI38_NewerDeliveryExpiresRemindWhen` : `remind_when` seeded through `Ask`, then a check-in delivered, then "no" → the question is expired and no daily reminder is made (round 3, item 7).
  - `TestI38_ListoWithOnlyAnAskedDeliveryLeavesUnaskedCheckInAlone`: `resolveCheckIn` filters asked rows; when none remain → no-op (item 9).
  - `TestI38_ExplicitReferentBindsTheStop`: a UI or API stop with `unit_id` → that unit, even with other candidates.
- [ ] 6.3 RED, precedence (D24), one per pair:
  - `TestPrecedence_RemindWhenNewerThanRelation_NoGoesToRemindWhen`;
  - `TestPrecedence_RelationNewerThanRemindWhen_NoGoesToModel`;
  - `TestPrecedence_KeepOrStopVsRelation`;
  - `TestPrecedence_WhichReminderVsRelation_TwoIsAnOption`;
  - `TestPrecedence_NewReminderQuestionExpiresTheOlder` (each reminder-kind pair);
  - `TestPrecedence_TieOrderByKind`.
- [ ] 6.4 RED L3 contract: `TriggerRepo.Dismiss` armed → dismissed, any other status → conflict; `LastDeliveries` returns every tied row; guarded `Create`. The I03 sweep stays green. The link-6 tests seed open questions through `PendingQuestionRepo.Ask`.
- [ ] 6.5 GREEN: M3, the guards on every arming path, the reply; the I38 row in doc 06 §4; the `stopped` lapse reason in doc 02 §7/§11 and doc 03; doc 02 §5; doc 07; `make check-all`.

## PR 7 — `feat/recurring-rearm` (D30-D35)

- [ ] 7.1 RED L1 prospection:
  - `TestSuccessor_DailyNamedTimeInMinusThree`: 21:00 at -03:00 (UTC is the next day) → the next day 21:00 local, never 18:00 (item 1).
  - `TestSuccessor_WeeklyAndMonthlyLocalDayBoundary`: a Sunday 22:00 at -03:00 → Sunday, not Monday.
  - `TestSuccessor_DSTSpringForward` and `TestSuccessor_DSTFallBack` (America/New_York, 02:30 and 01:30 daily): one per local day, no skip, no repeat.
  - `TestSuccessor_AfterOutageIsAfterNow`: three days off → the first occurrence after now.
  - `TestSuccessor_UnaskedDailyNoBurst`: a clamped daily with a 7-day lead → successive `fire_at`s are one day apart, never two the same day (item 11).
  - `TestLegacyOccurrence_ProvablyUnclampedYearly`: a yearly row whose local `fire_at` + 7 calendar days is the anchor day at 12:00 → that instant, with the zone at -03:00 so UTC hours would miss it (round 3, item 5).
  - `TestLegacyOccurrence_ClampedThenCorrectedFallsBack`: `fire_at` moved before this link, `created_at` unchanged → `NextOccurrence(fire_at)`; the occurrence is written back on first touch.
  - `TestSuccessor_RefusesNilAnchor` → `ErrNoAnchor`; the pass goes on (item 11).
  - `TestSuccessor_ClampedDailyNoSecondFireOnDayOne`: a daily captured at 10:00 for 12:00 (clamped, fired 10:00) → the successor is not before 00:00 the next local day (round 3, item 8).
  - `TestRouteTrigger_AskedRecurringIsPushed`: degraded interrupt → push (round 3, item 9). `TestGate_AskedRecurringIsNotWaiting`: a fired asked recurring row → not `Waiting`, judged as a push.
  - `TestGate_AskedRecurringJudgedByOccursAt`: an asked night-exempt daily, `occurs_at` 06:30, quiet end 08:00 → at 06:30, `about` is `occurs_at` (not `NextOccurrence(fire_at)`) and `Deliver`, never `passed` nor `Defer`; at 12:31 → `Stale` `overdue` (6 h from `fire_at`); an asked "No" daily at 09:00 with quiet end 10:00 → `Defer` until 10:00.
  - `TestGate_UnaskedRecurringJudgedByOccursAtDayGranular`: an unasked birthday row with `occurs_at` 10-20 12:00 → not passed at 23:59 on 10-20; `passed` from 10-21 00:00 local; `Waiting` when fired.
  - `TestAnchorTimeRoundTrip`, `TestNextOccurrenceUsesAnchorTime`.
- [ ] 7.2 RED, I36 recurring:
  - `TestI36_AnAskedRecurrenceIsARecurringTask`: "todos los días a las 9pm" → a `task`; the row has `occurs_at` 21:00 and `night_exempt`.
  - `TestI36_RecurringReArmsOnFire` and `TestI36_RecurringReArmsOnExpire` (stale >6 h, and `passed`).
  - `TestI36_CrashBetweenCreateAndFire`: create succeeds, fire is not reached; the next pass → `ErrTriggerExists` and one fire, no double.
  - `TestI36_RepairArmsAFiredRootWithNoSuccessor`.
  - `TestI36_SuccessorIdIsRootNotGrowing`.
  - `TestI36_CorrectingAnAskedRecurrenceMovesItsTime`: 21:00 → 22:00; anchor 22:00, `asked` kept (item 4).
  - `TestI36_RecurringRendersItsOccurrence`: `About` is `occurs_at`, not the unit's first date (item 14).
  - `TestI36_ArchivedRecurringKeepsReminding`.
  - `TestI36_StoppedRecurringGetsNoSuccessor`.
  - `TestI36_BirthdayReArmsYearlyOnce` (P2).
  - `TestI36_BirthdayCorrectionKeepsOccursAt`: a corrected unasked recurring row → `occurs_at` is the new occurrence (item 12).
  - `TestI36_CorrectedLegacyClampedRowIsNotLate`: a clamped legacy daily, corrected → `occurs_at` written, the successor is not 7 days late (item 11).
  - `TestRecurringRootsLatestStatus` (L3, by id prefix); `TestCheckReportCountsReArms` (I12); `TestDryRunCreatesNoSuccessorAndNoRepair`.
- [ ] 7.3 GREEN: `LegacyOccurrence`, `RecurringRoots`, `RouteTrigger`'s asked branch, the gate rule (design-repeating D30); doc 03 (anchor `hour`/`minute`, the `occurs_at` key); doc 02 §11 (`check.trigger.rearmed`); doc 02 §7 "Recurrence" (the re-arm becomes true; P2 stated as a change); doc 07; corpus case `classify-task-remind-me-daily-pills.json`; `make check-all`.

## PR 8 — `feat/remind-me-ask-back` (D40-D43; needs links 3, 5, 6, 7)

- [ ] 8.1 RED L1 classify:
  - `IsOnlyWhen`: "mañana a las 10", "en 1 semana", "en 3 días", "in 2 hours" → true; "mañana llamo a Juan" → false.
  - `IsNoAnswer`: "no", "no, gracias", "nope" → true; "no sé" → false.
- [ ] 8.2 RED, I38 ask-back:
  - `TestI38_NoDayNoTimeAsksBack`: the reply, and the question asked.
  - `TestI38_ATimeAnswerSetsTheReminder`: a note gets `due_at`, armed 15 min before.
  - `TestI38_ATimeAnswerAlreadyLateRemindsAtOnce`: "hoy 20:10" at 20:00 → at once (item 13).
  - `TestI38_AnswerWithNoInstantAsksCuando`: nothing written; the question stays open.
  - `TestI38_NoMakesItDailyFromDateOnlyAtCopied`: `DateOnlyAt` 08:30 → "…todos los días a las 08:30"; a later pref change → it does not move (item 19).
  - `TestI38_NoAfterTheHourStartsTomorrow`.
  - `TestI38_AnUnrelatedMessageIsNotAnAnswer`: the question expires; the message is captured.
  - `TestI38_ScanExpiresAStaleQuestion`: `check.question.expired`; the unit is kept with no reminder.
- [ ] 8.3 GREEN; doc 02 §5; doc 07; `make check-all`.

## PR 9 — `feat/remind-me-done` (D50-D52; needs 6, 7)

- [ ] 9.1 RED:
  - `TestSaysDone_WholeMessageOnly`: "listo" and "ya lo hice." → true; "listo, nos vemos mañana" → false.
  - `TestI38_ABareListoAsksKeepOrDeleteOnce`.
  - `TestI38_SeguiMakesListoDoneForToday`: the next "listo" → no question, it keeps recurring.
  - `TestI38_BorraloStops`.
  - `TestI38_BareNoToKeepOrStopDeletes` (P3).
  - `TestI38_OtherAnswerLeavesUnmarkedAndAsksAgain`.
  - `TestI38_TheDailyFromNoFollowsTheSameRule`.
  - `TestI38_ABareListoWithNothingRecentStopsNothing`.
  - `TestI38_DoneOnOneShotResolvesWithoutAsking`.
  - `TestI38_DoneOnADeliveredOneShotNotArmed`: the reminder fired and was delivered, nothing armed → it binds (done candidates, item 10).
  - `TestI38_DoneOnAnAskedEventDismissesTheRemainingLeads`: "ya lo hice" after the 24 h lead → the 2 h and the +15 min leads are dismissed.
- [ ] 9.2 GREEN: M4, `reminder.kept`, `reminder.done_today`; doc 02 §5, §11; doc 03; doc 07; `make check-all`.
