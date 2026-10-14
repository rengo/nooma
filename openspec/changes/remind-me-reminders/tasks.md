# Tasks — remind-me-reminders, links 1-4 (links 5-9: `tasks-repeating.md`)

Stacked to main (design §4). Strict TDD. **Apply gate:** every named RED test below is written
and watched failing for the stated reason before its link's GREEN code. Zone in examples:
America/Argentina/Buenos_Aires (-03:00) unless stated. A link measured over 400 is cut before it
opens.

## Review Workload Forecast

| Link | Impl+docs | Tests | Risk |
|---|---|---|---|
| 1 | ~260 | ~420 | Medium: the planner every event uses |
| 2 | ~350 | ~350 | Medium: migration, payload, gate, reply |
| 3 | ~190 | ~230 | Medium: I29, I35 amendments |
| 4 | ~110 | ~120 | Low |

Plan chain, 4 PRs: ADR-0031 + proposal + spec; design + tasks; design-repeating; tasks-repeating.
- [x] 0.1 Written; client answers and review rounds 1-2 folded in.

## PR 1 — `feat/remind-me-decision` (D1-D4 core, D6)

- [ ] 1.1 RED L1 `core/classify`, the cue reader (D1). Each test asserts `AsksToBeReminded`:
  - `TestAsks_ObservedMessage`: "recordame comprar café mañana a las 10" → true.
  - `TestAsks_ConditionalClauseIsNotNegation`: "si no, avisame" and "si no puedo, avisame" → true.
  - `TestAsks_NoTeOlvidesDeRecordarme` → true. `TestAsks_NoMeRecuerdesMasIsAStopNotARequest` → false.
  - `TestAsks_Variants`: "recordámelo", "haceme acordar", "acordate de avisarme", "poneme un recordatorio", "que no se me olvide" → true.
  - `TestAsks_RecallQuestion`: "recordame qué hablamos con Juan" → false.
  - `TestAsks_DentistReminderToConfirm`: "my dentist sent a reminder to confirm" → false.
  - `TestAsks_ApostropheForms`: "don’t remind me", "don't remind me", "dont remind me" → all false, the same.
  - `TestAsks_AccentFoldTable`: "RECUÉRDAME" and "recuerdame" → true; every fold-table entry is covered.
  - `TestQuotedSpeechCountsAsAskedKnownLimit`: "Ana me dijo 'avisame cuando llegues'" → true (P4, pinned).
  - `TestRemindCuesCoverEveryLanguage`: a sweep over `AllLanguages()`.
- [ ] 1.2 RED L1 `Decode` (D2): `TestDecode_BareDateIsDayOnly` → `DueAtDayOnly` true; `TestDecode_MidnightTimestampIsNotDayOnly` ("…T00:00:00-03:00") → false.
- [ ] 1.3 RED L1 `prospection` (D3, D4, D6):
  - `TestArmAsked_NamedTime`: due 10-15 10:00 → one plan at 09:45, `Asked`.
  - `TestArmAsked_DayOnly`: day-only 10-15 → 10-15 09:00. `TestArmAsked_DayOnlyFollowsDateOnlyAt`: 08:30 pref → 08:30.
  - `TestArmAsked_NamedMidnightIsNotDayOnly`: 10-16T00:00 timed → 10-15 23:45.
  - `TestArmAsked_DSTDayWithoutMidnightStaysDayOnly` (America/Santiago, its spring-forward date): a bare date → that day at `DateOnlyAt`, not 01:00 minus a lead.
  - `TestArmAsked_EventGetsThreeLeads`: event 10-17 10:00 → 10-16 10:00, 10-17 08:00, 10-17 09:45.
  - `TestArmAsked_PastLeadAtOnce`: 20:10 said at 20:00 → at once.
  - `TestArmAsked_PastLeadInQuietHoursRingsAtOnce`: "a las 6:30" at 06:20, quiet end 08:00 → at once (item 12).
  - `TestArmAsked_NamedTimeInsideQuietHoursNotMoved`: 06:30 → 06:15.
  - `TestArmAsked_TooLateTodayRefused`: "hoy" day-only at 23:56 → `too_late`; at 23:50 → at once.
  - `TestArmAsked_AnyKindWithADate`: a `mental_load` with a `due_at` → armed. `TestArmAsked_TimerUnchanged`.
  - `TestArmUnasked_EveryExistingArmAndFollowTestUnchanged`: the existing suite stays green.
  - `TestParseRemindMeLead_Bounds`: 0 → error, 1 → ok, 43200 → ok, 43201 → error, NULL → 15.
  - `test/conformance/TestAtOnceMarginMatchesProactiveInterval` (L2, so core imports no scheduler).
  - `TestVerdict_NightExemptOverdueFromFireAt`: an exempt 03:00 row at 09:05, with a 6 h window → stale `overdue`; an unexempt one → deferred, then delivered (round 2, item 3).
  - `TestVerdict_AskedNamedMidnightPassesAtMidnight`: an asked row about 10-16 00:00 with no `day_only` → `passed` at 00:00, not at the next midnight (item 4). `TestVerdict_UnaskedMidnightKeepsDayGranular`.
- [ ] 1.4 GREEN, including the call-site edits for the new signatures: `Arm` at `brain/capture.go:317`, and `ResolveReminderPrefs` at `brain/capture.go:121` and `brain/preferences.go:207`, plus their test callers; doc 02 §5 step 1, §7, §13; I34 row amended; `make check-all`.

## PR 2 — `feat/remind-me-one-shot` (D5, D7, D8, D4 scan; first usable)

- [ ] 2.1 RED L2, the I36 row:
  - `TestI36_AnAskedReminderIsArmedBeforeItsTime`: the observed message → a `task` unit with `remind_me=1`, one trigger at 09:45, payload `{lead_minutes:15, event_at:10:00, asked, night_exempt}`.
  - `TestI36_TheWordsDecideNotTheModel`: the same classification without the cue → no trigger.
  - `TestI36_ADayAloneRemindsThatDayAtNine`: payload `{lead_minutes:0, event_at:midnight, day_only}`.
  - `TestI36_AskedDayOnlyIsNotPassedAtNine` (the gate): a 09:00 delivery → `Deliver`, not `passed`.
  - `TestI36_ANamedTimeRingsThroughTheNight`: an asked 03:00 row at 02:46 → `Deliver`, not `Defer`.
  - `TestI36_TheReplyNamesTheAskedReminder`: exact "Anotado para el jue 15 oct 10:00. Te aviso 15 minutos antes." and the English counterpart.
  - `TestI36_TheReplyStatesTheArmedHour`: `DateOnlyAt` 08:30 → "…ese día a las 08:30."
  - `TestI36_AskedButNotArmedSaysWhy`: past, quiet, too late → the refusal row and the reason sentence.
- [ ] 2.1b RED, the seams with #343 and #344 (round 3, item 4):
  - `TestReplanKeepsTheAskedLeadOfAnEvent`: an asked event with a stale 24 h row → `replanUnit` keeps the +15 row, with `asked` and `night_exempt`.
  - `TestReplanOfAskedDayOnlyKeepsDateOnlyAt`: the re-plan of an asked day-only unit → at `DateOnlyAt`, `day_only` kept.
  - `TestCurrentEventReadsDueAtForRemindMe`: an asked task → `currentEvent` is `due_at`; an unasked task → nil.
  - `TestCorrectedAskedTaskOldRowIsNotSentAtItsOldTime` (link 3 expectation, red until link 3): 10:00 → 11:00 → nothing at 09:45; one row at 10:45.
- [ ] 2.2 RED L3: migration M1; `remind_me` round trip; `SetReminderPrefs` writes only the named columns; goldens.
- [ ] 2.3 GREEN: the store, capture, phrase, channels and gate branch; the corpus case `classify-task-remind-me-coffee.json`; L4 of the observed message; doc 02 §5 step 5, §11; doc 03; doc 07; `make check-all`.

## PR 3 — `feat/remind-me-follows` (D9)

- [ ] 3.1 RED:
  - `TestI36_AnAskedReminderFollowsItsInstant`: an asked task due 10:00, armed 09:45; "a las 11" → moved to 10:45, one `correction.reminder.moved`.
  - `TestI29_FirstArmingOfAnAskedUnitMayBeAtOnce`: refused as past at capture; corrected to 2 minutes ahead → armed at once.
  - `TestI29_SecondCorrectionNeverCreatesAtOnce`: a unit that had a trigger, every lead behind → nothing.
  - `TestI36_UnaskedTaskDueAtEditMovesNothing`.
  - `TestI35_ALeadChangeMovesAskedReminders`: 15 → 30 min → 09:30, `config.reminder.moved`.
  - `TestI35_LeadOnlySaveReArms`; `TestI35_NoOpSaveTouchesNoAskedReminder`.
  - `TestI35_LeadOnlySaveLeavesAskedDayOnlyAtDateOnlyAt`: an asked day-only reminder at 09:00; the lead goes 15 → 30 → still 09:00, not 23:30 the day before (item 8).
  - `TestI35_DateOnlyAtSaveMovesAskedDayOnly`: 09:00 → 08:30 → moved to 08:30, `config.reminder.moved`.
  - `TestCatchUpUsesTheAskedLeadSet` (if #339 link 4 is in).
- [ ] 3.2 GREEN; I29 and I35 rows amended; doc 02 §5 step 4, §7; `make check-all`.

## PR 4 — `feat/remind-me-preference-ui`

- [ ] 4.1 RED: `TestSave_RemindMeLeadValidatedAndRecorded` (`config.updated` reads "15 minutes" → "30 minutes"); `TestPreferencesPage_RemindMeLeadRowErrorAndSaved`.
- [ ] 4.2 GREEN; doc 07; `make check-all`.
