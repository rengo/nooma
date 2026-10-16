# Tasks — remind-me-reminders, links 1, 1a, 1a′ and 2-4 (links 5-8: `tasks-repeating.md`)

Stacked to main (design §4). Strict TDD. **Apply gate:** every named RED test below is written
and watched failing for the stated reason before its link's GREEN code. Zone in examples:
America/Argentina/Buenos_Aires (-03:00) unless stated. A link measured over 400 is cut before it
opens.

## Review Workload Forecast

| Link | Impl+docs | Tests | Risk |
|---|---|---|---|
| 1 | merged (#346, #347) | — | — |
| 1a | ~380 | ~350 | Medium: classify fields, the prompt, the title, migration 0009 |
| 1a′ | ~260 | ~250 | Medium: recording format, `--record`, `doctor --reading`, the merge gate |
| 2 | ~350 | ~350 | Medium: migration, payload, gate, reply |
| 3 | ~190 | ~230 | Medium: I29, I35 amendments |
| 4 | ~110 | ~120 | Low |

Plan chain, 3 PRs: #350 (ADR-0033, proposal, spec); #356 (design, tasks); #357 (design-repeating,
tasks-repeating).
- [x] 0.1 Written; client answers, the done descope and review rounds 1-3 folded in.

## Link 1 — `feat/remind-me-decision` (D2-D4 core, D6; merged as 1.2-1.4), and links 1a and 1a′ (1.1 and 1.1′: D1, D1b, D1c)

- [x] 1.1 Link 1a — `feat/classify-reminder-fields` (ADR-0033, D1, D1b):
  - [x] RED L1 `TestDecode_ReminderFields`: `remind_request`, `reminder_intent`, `question_answer`, `choice_number` decode; an unknown enum, a wrong type or a `choice_number` outside 1-5 degrades that field alone (I14); `TestAllReminderIntentsAndAnswersSwept`.
  - [x] RED L1 `TestBuildPromptDescribesReminderFields`: both languages' examples; the open question appears only when one is passed.
  - [x] RED L1 `TestDecode_RemindRequestBoolOrNull`: `true`/`false`/`null` decode; `"yes"` → degraded. `TestDecode_ReminderTarget`: 1-5 and `"unclear"` decode; 0, 6, `"2"` → degraded.
  - [x] RED L1 `TestBuildCorrectionPromptCarriesReminderFields`; `TestBuildPromptCandidateListOnlyWhenGiven` (numbered, title or content plus date).
  - [x] RED L1 (round 2): `TestBuildPromptRendersOneListOnly` (a `which_reminder` open plus candidates → only the question's list); `TestPromptListEqualsTheQuestionText` (`RenderReminderList` output).
  - [x] RED L1 (round 2): `TestBuildCorrectionPromptRendersDayOnlyAsBareDate`; `TestBuildCorrectionPromptAsksForAFreshTitleOnDateCorrection`.
  - [x] RED L1 `TestResolveTitle_TrimsAndRejectsControl`: "  Reunión  " → "Reunión"; "a\nb", "a\x07b", "   " → null.
  - [x] RED L1 `TestResolveTitle`: "Reunión con Juan" kept; "" → null; 61 runes → null; "Reunión 2026-10-14", "Reunión a las 22:30" → null.
  - [x] RED L3 `TestMigration0009AddsUnitTitle`; title round trip; goldens.
  - [x] RED `TestRenderUsesTheTitleWhenPresent`: "Recordatorio: Reunión con Juan — mié 14 oct, 22:30"; `TestRenderFallsBackToContentWithoutTitle`.
  - [x] RED `TestCorrectionRewritesTheTitle`: a re-classified correction → the new title in `correction.applied`, with the old one as pre-image. `TestCorrectionContentEditWithoutTitleClearsIt` → `title: null`. `TestCorrectionDateOnlyEditKeepsTitle`. `TestAllFields_EveryEditReportsExactlyOneAccessorTrue` gains `FieldTitle`.
  - [x] RED `TestDigestLineHasNoRecordatorioPrefix` ("• Reunión con Juan — 22:30"); `TestFrozenTextUsesTheTitle`.
  - Asked capture: link 2 reads `asked` from `c.RemindRequest`. The day-only flag (1.2, #346) stays; the phrase reader never merged.
  - [x] GREEN; doc 02 §5 step 1 (the fields, the title), §5.1 lines 483-495 (the narrower injection rule), §5 step 4 and §11 (`FieldTitle`), §7 (the render); doc 03 (`units.title`); §13 `max_title_runes`; `make check-all`.
- [x] 1.1′ Link 1a′, cut in three before opening because it measured about 640 impl+docs (PM approved the first cut 2026-10-15; the second followed the measurement), stacked to main: 1a′-a `feat/reminder-phrasing-replay` (the format and the replay), 1a′-b `feat/doctor-record` (`--record`), and 1a′-c `feat/reminder-phrasing-set` (the phrasing set, `--reading`, the merge gate; red until the maintainer records) (D1c):
  - Link 1a′-a — `feat/reminder-phrasing-replay`:
    - [x] RED `TestValidateAllowsAwaitingRecording` (a `record: true` case needs no response, provider or model; the rest stays strict); `TestLLMFormatDocumentsEveryField`; `TestCasesMatchesTheSourceFiles` covers `reminders/`; `format.md`.
    - [x] RED L1 `internal/reading` (documented in doc 06 §1 as not core): `TestPromptSendsNilBeliefs`, `TestReplayFailsOnPromptHashMismatch`, `TestKnownMisreadStaysMarked`, the strict `Check`; L2 `TestReminderPhrasingSetReadsAsExpected` (replay skips cases awaiting recording and reports the count).
  - Link 1a′-b — `feat/doctor-record`:
    - [x] RED `TestDoctorRecordWritesResponsesWithAFakeProvider`: only `"record": true` cases; `expect` untouched; `provider`, `model`, `now` and `prompt_sha256` written; `record` cleared. `TestRecordSkipsRecordedWithoutForce`; `TestRecordRefusesUnderCI`; `TestRecordSendsNilBeliefs`; `TestRecordValidatesACaseBeforeSpendingACall`; `TestDoctorFlagsWorkWhereverTheyAreWritten` (a flag after the vault is read, not dropped); `TestRecordNeverSendsAnythingFromTheVault` (a seeded vault: a marker in a unit, a belief and `.env` reaches no request body). Doc 06 §5 (the record step).
  - Link 1a′-c — `feat/reminder-phrasing-set`:
    - [x] RED `scripts/recordings-complete.sh` and `TestRecordingsCompleteFailsOnPendingCase`; `TestRecordingsCompleteScript` (the step runs in `ci.yml`'s required `lint` job; doc 06 §6 states why it is not in `check-all`); **Maintainer-reviewed docs edit in this PR:** CLAUDE.md's Workflow section gains the `check-all` exception for `recordings-complete.sh`.
    - [x] RED `TestDoctorReadingIsOptIn` (plain `doctor` makes no reading calls); `TestDoctorReportsReminderReadingRate`; `TestDoctorFalsePositiveLineFailsOnOne` (its negatives include a bare no/sí/listo/ya answering an open relation question or check-in with a non-empty candidate list); `TestDoctorReportsTitleRate`; `TestDoctorReportsCheckinOutcomeRates` and `TestDoctorReportsDayOnlyRate` (fake provider).
    - **Prompt work owed (recorded misses, gpt-6-luna), done in link 1b (1.2′ below):** the first link that changes the classify prompt, link 6 being the natural one, must tell the model that a which-question picks a reminder to stop and that the answer to it must not itself read as a stop (the two `classify-which-answer-*` cases are marked `known_misread` for the stop the model adds), and it re-records the set. The I38 stop tests must still pass unchanged with that prompt: an answer to the which-question stops nothing by its own words, and only the pending stop binds. The check-in outcomes and the instant of a bare-day answer to `remind_when` are marked the same way. Link 1b took all eight marked misses, the bare "no" read as an answer with no question open among them, before link 2, so the prompt changes once and the set is recorded once.
    - [x] Corpus written (49 phrasings, all awaiting); recorded once by the maintainer in one session, against the final prompt of link 1a (which already holds every later link's prompt text):
      - the hand-written question texts and the candidate-list format (dates through `phrase.Set`) in the set are the ones links 5, 6 and 8 must send (noted under each in `tasks-repeating.md`), and every case with a list or an open question carries an authored `now` before the reminders it lists;
      - no existing recording is re-recorded; I18, I26, `arming_capture_keeps_its_unit` and `classify_golden_cases_decode_clean` assert their content;
      - about 40 new in `testdata/llm/reminders/`: every phrasing of spec R1's table, the stop, undo-keep and answer phrasings, "listo" reading `none` (with `open_question` or the candidate list), the negatives for the hard false-positive line, the title cases, the day-only bare dates, the check-in answers, "a single candidate plus a message about something else → `unclear`", and `classify-task-remind-me-coffee` and `…-daily-pills`.
    - [x] Doc 06 §6; `make check-all`.
- [x] 1.2′ Link 1b — `fix/classify-prompt-misreads`: the prompt work the recorded set found, in one link so the prompt changes once and the set is recorded once:
  - [x] L2 `TestCheckInAnswer_TheCodeActsOnlyOnTheModelsOutcome` (the live M3 bug, confirmed): the gpt-6-luna readings of a nudge taken up and a task done carry no outcome, and the open check-in stays open with nothing logged; the drop reading, which has one, closes it. The code needs nothing: the defect is the prompt's.
  - [x] RED L1 `TestBuildPromptSaysWhatEachCheckinOutcomeMeans`: a "Check-in answers" section states what each `task_checkin_outcome` and `nudge_outcome` value means, with no quoted words, and a value added to either vocabulary fails until it is; saying a thing is done is `task_checkin_outcome done`, not a stop. The whole set goes back to `"record": true` (the shared prompt is in every hash); the four check-in `known_misread` marks go; `classify-checkin-done` is added (a done with no list), and the coverage test asks for every outcome value. Doc 02 §5 step 1.
  - [x] RED L1 `TestBuildPromptAWhenAnswerCarriesItsInstant`: a `when` answer puts the instant in `due_at` or `event_at`, a day alone as a bare date, never only in `normalized_content`; the `classify-when-answer-a-day-only` mark goes. Doc 02 §5 step 1.
  - [x] RED L1 `TestBuildPromptWhichAnswerIsNotAStop`: with a `which_reminder` question open (and only then), the prompt says it was asked to place a stop Nooma holds, that the answer sets only `choice` and `choice_number`, and that it is not itself a stop; both `classify-which-answer-*` marks go. The I38 stop tests are link 6's and not written yet; they bind on the pending stop, which this rule leaves as it is. Doc 02 §5 step 1.
  - [x] RED L1 `TestBuildPromptNoQuestionOpenMeansNoAnswer`: with no question open the prompt says so and that `question_answer` is none; with any kind open it does not; the `classify-negative-bare-no-with-candidates` mark goes. Doc 02 §5 step 1.
  - [x] RED L1 `TestBuildPromptListedRemindersAreNotAStop`: the stop candidates are shown with the rule that being listed is not a request to stop (the done descope: a bare "listo" beside them, no check-in open, is not a stop), and only where the list is. Doc 02 §5 step 1.
  - [x] Review of #365, item 1: RED `TestCheckOutcomesWithASideEffectAreStrictWhenUnexpected`: `reading.Check` misses an outcome the case does not name (task check-in, nudge, relation; any state outcome), because each closes, confirms, deletes or signals in production; a case naming either check-in field leaves the other free. Against the 41 recordings that read right at 264e65e, one newly misses: `classify-when-answer-no-hace-falta` read `nudge_outcome declined` beside its `question_answer no`, which would have closed the newest open check-in. `format.md`.
  - [x] Review of #365, item 2: the coverage test asks for a phrasing per value of `classify.AllTaskCheckinOutcomes()` and `AllNudgeOutcomes()`, not a hand list.
  - [x] Review of #365 (second round): RED L1 `TestResolveCheckin`: `classify.ResolveCheckin` is the one mapping from a reading's check-in outcomes to the resolution (nudge over task, snooze resolving nothing), moved out of `brain.checkInResolution`, which now only translates it to the store's vocabulary. Doc 02 §5 step 1.
  - [x] `reading.Check` frees the unexpected check-in field only while `classify.ResolveCheckin` gives the reading the expected effect (a declined beside an expected done, or an engaged beside an expected snooze, now misses); a relation outcome beside an expected check-in outcome misses (the reviewer's surviving mutant). `format.md`; doc 02 §5 step 1.
  - [x] Every `known_misread` mark went and the 50 phrasings were re-recorded by the maintainer against this prompt (882efdf): 45 of 50 read as expected, and the eight old misses all read right.
  - [x] The five that did not, triaged by what production does with the reading with a check-in open and with none (none open: one unmatched log row, nothing else):
    - `classify-negative-bare-no-with-candidates` reads `nudge_outcome declined`: `expect` fixed. The negative was written as a bare "no" answering an open check-in, to pin "not a stop", before outcomes were strict; a no to a check-in declines it.
    - `classify-negative-bare-ya-with-candidates` reads `task_checkin_outcome done`: `expect` fixed, as for the bare "listo" (ADR-0033: a bare "listo" keeps answering an open check-in).
    - `classify-negative-listo-nos-vemos-with-candidates` reads `done`: `known_misread`. It is a farewell, ADR-0033 names this reading as a misread, and it would close the newest open check-in.
    - `classify-nudge-engaged` still reads no outcome: `known_misread`; an open nudge stays open.
    - `classify-stop-listo-no-me-lo-recuerdes` reads the stop and `done`: `known_misread`. The done would close the newest open check-in, which need not be the reminder the stop is about (its target is unclear); link 6's I38 rule that a stop consumes the message is the code guard.
  - [x] `make check-all`.
  - [x] Review of #365 (third round), blocking 1: RED L1 `TestBuildPromptDoneIsAMeaningAndAStopAnswersNoCheckin`: the prompt states done by its meaning with no example (the farewell it listed was read as done, the misread ADR-0033 names) and says a stop carries no check-in outcome; `TestBuildPromptDescribesReminderFields` no longer asks for a quoted "listo". The whole set goes back to awaiting (882efdf's recording is of the old prompt); the farewell and stop-listo marks go. Doc 02 §5 step 1.
  - [x] RED `TestCheckTypeOnlyWhenSet`: `expect` may name the classify `type`, checked only when set and validated against `classify.AllKinds()`, so the replay sees a reply stored as a new unit. `format.md`.
  - [x] RED L1 `TestBuildPromptAReplyIsNotANewUnit`: the prompt says chitchat is a message that only answers Nooma or only talks, and that an answer travels in the outcome fields, never as a new task; the bare no, sí, ya and listo and the farewell expect `type: chitchat`. Doc 02 §5 step 1.
  - [x] RED L1 `TestBuildPromptAShortReplyAnswersACheckin`: with no reminder question open, a message that is only a short reply answers the check-in and carries the outcome it means (by meaning, no quoted words). **The one rule for a bare word** (`format.md`): with no question open it answers an open check-in, `type: chitchat`, "sí" → `nudge_outcome: engaged`, "ya" and "listo" → `task_checkin_outcome: done`; with a question open it answers that question. `classify-negative-bare-si-with-candidates` expects engaged, and the `classify-nudge-engaged` mark goes. Doc 02 §5 step 1.
  - [x] RED L1 `TestBuildPromptAShortDeclineIsNotYet` (the client's ruling on the bare "no": ask again later unless the user explicitly refuses): a short decline alone is `task_checkin_outcome snooze` with no `nudge_outcome`; only an explicit refusal, to do the task or to be asked about it, is `drop` or `declined`. `classify-negative-bare-no-with-candidates` expects `chitchat` and snooze. `format.md`; doc 02 §5 step 1.
  - [x] Review of #365 (fifth round), blocking 2: RED L1 `TestBuildPromptAnotherAnswerIsNotACheckinAnswer`: a message that answers an open reminder question, or says whether two things are related, answers that and carries no check-in outcome (the other half of the bare-word rule, which only `format.md` held). Doc 02 §5 step 1.
  - [x] Blocking 3: RED L1 `TestBuildPromptClosingTheConversationAnswersNoCheckin`: a message that only closes the conversation, a goodbye or a thanks, answers no check-in even when it begins with a word that alone would (by meaning, no example). Doc 02 §5 step 1.
  - [x] Blocking 1: "ahora no, gracias" is a short decline, so by the client's ruling it is not yet: the case is renamed `classify-nudge-not-now` and expects `chitchat` and snooze. `classify-nudge-declined` is now an explicit refusal with no stop wording ("no, no quiero hablar de eso"), expecting `declined`, so the vocabulary coverage still has one.
  - [x] Non-blocking 5a: every pure answer expects `type: chitchat` (the check-in, nudge, undo, `remind_when`, `which_reminder` and relation answers, 24 cases in all): capture resolves the check-in or relation first and the chitchat fork stores no unit. `tasks-repeating.md` notes that links 6 and 8 read their answer before that fork.
  - [x] Review of #365 (sixth round): RED `TestCheckJudgesAClosingAnswerByEffect`: an expected outcome that closes the check-in is judged by effect through `classify.ResolveCheckin` (expected declined read as drop, expected engaged read as done, expected done read as engaged: no miss), since the model never sees whether the open check-in is a task or a nudge; an expected snooze keeps its exact value. `format.md`.
  - [x] Recorded by the maintainer against the final prompt on 2026-10-16 (9135ec4): **51 of 51** read as expected, with no `known_misread`.
  - **Not built, owed:** a snoozed check-in is not asked again. Only `resolveCheckIn` reads `Delivered`; the digest and the push read `Undelivered` (fired, not yet surfaced), and nothing expires or re-arms a delivered, unanswered trigger. Snooze leaves it open and silent, and the next outcome the user gives still attaches to it while it is the newest. Re-asking needs product decisions (when, how often, quiet hours, what it says, a cap) and a constant in §13; estimated at about 130 impl+docs, which would take this PR over 400, so it is its own link.
  - [x] **Prompt work owed (recorded against 882efdf), done in the third to fifth rounds above and read 51 of 51 on the 2026-10-16 recording:** link 1b's line "Saying a thing is done (…, "listo, nos vemos mañana") is none, not stop: it is task_checkin_outcome done" sends the farewell to `done` too, and must not; a message that is a stop is not also a check-in answer; "dale, contame" still reads no `nudge_outcome`; and a bare "listo" or "ya" comes back as a `task` that capture persists ("La tarea está hecha"), which the set does not measure (its `type` is not checked).
- [x] 1.2 RED L1 `Decode` (D2): `TestDecode_BareDateIsDayOnly` → `DueAtDayOnly` true; `TestDecode_MidnightTimestampIsNotDayOnly` ("…T00:00:00-03:00") → false.
- [x] 1.3 RED L1 `prospection` (D3, D4, D6):
  - `TestArmAsked_NamedTime`: due 10-15 10:00 → one plan at 09:45, `Asked`.
  - `TestArmAsked_DayOnly`: day-only 10-15 → 10-15 09:00. `TestArmAsked_DayOnlyFollowsDateOnlyAt`: 08:30 pref → 08:30.
  - `TestArmAsked_NamedMidnightIsNotDayOnly`: 10-16T00:00 timed → 10-15 23:45.
  - `TestArmAsked_DSTDayWithoutMidnightStaysDayOnly` (America/Santiago, its spring-forward date): a bare date → that day at `DateOnlyAt`, not 01:00 minus a lead.
  - `TestArmAsked_EventGetsThreeLeads`: event 10-17 10:00 → 10-16 10:00, 10-17 08:00, 10-17 09:45.
  - `TestArmAsked_PastLeadAtOnce`: 20:10 said at 20:00 → at once.
  - `TestArmAsked_PastLeadInQuietHoursRingsAtOnce`: "a las 6:30" at 06:20, quiet end 08:00 → at once (item 12).
  - `TestArmAsked_NamedTimeInsideQuietHoursNotMoved`: 06:30 → 06:15.
  - `TestArmAsked_TooLateTodayRefused`: "hoy" day-only at 23:50 → at once; at 23:55 → at once (exactly 5 min left); at 23:56 → `too_late`.
  - `TestArmAsked_AnyKindWithADate`: a `mental_load` with a `due_at` → armed. `TestArmAsked_TimerUnchanged`.
  - `TestArmUnasked_EveryExistingArmAndFollowTestUnchanged`: the existing suite stays green.
  - `TestParseRemindMeLead_Bounds`: 0 → error, 1 → ok, 43200 → ok, 43201 → error, NULL → 15.
  - `test/conformance/TestAtOnceMarginMatchesProactiveInterval` (L2, so core imports no scheduler). Link 2 renames it `TestAtOnceMarginExceedsProactiveInterval` (margin > interval).
  - `TestVerdict_NightExemptOverdueFromFireAt`: an exempt 03:00 row at 09:05, with a 6 h window → stale `overdue`; an unexempt one → deferred, then delivered (round 2, item 3).
  - `TestVerdict_AskedNamedMidnightPassesAtMidnight`: an asked row about 10-16 00:00 with no `day_only` → `passed` at 00:00, not at the next midnight (item 4). `TestVerdict_UnaskedMidnightKeepsDayGranular`.
- [x] 1.4 GREEN, including the call-site edits for the new signatures: `Arm` at `brain/capture.go:317`, and `ResolveReminderPrefs` at `brain/capture.go:121` and `brain/preferences.go:207`, plus their test callers; doc 02 §5 step 1, §7, §13; I34 row amended; `make check-all`.

## PR 2 — `feat/remind-me-one-shot` (D5, D7, D8, D4 scan; first usable)

- [ ] 2.1 RED L2, the I36 row:
  - `TestI36_AnAskedReminderIsArmedBeforeItsTime`: the observed message → a `task` unit with `remind_me=1`, one trigger at 09:45, payload `{lead_minutes:15, event_at:10:00, asked, night_exempt}`.
  - `TestI36_ARequestComesFromTheField`: the same classification with `remind_request: false` → no trigger, and the Q4 reply "Anotado. No puse aviso; si querés, decime «recordame»". `TestI36_NoHintWithoutAnInstant`.
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
  - `TestAskedRecurrenceBeforeLink7IsOneShot`: "recordame tomar pastillas todos los días a las 9pm" in links 2-6 → a unit of the type the model's kind maps to (the `task` mapping is link 7's), `remind_me=1`, one one-shot trigger at 20:45 today, never the 7-day recurring row (named time minus the lead), reply "Anotado para el … 21:00. Te aviso 15 minutos antes."; no recurring row (P1). Link 7 replaces this test with its I36 recurring tests.
- [ ] 2.2 RED L3: migration M1; `remind_me` round trip; `SetReminderPrefs` writes only the named columns; goldens.
- [ ] 2.2b RED (round 3): `TestArmAsked_TooLateTodayRefused` updated: "hoy" at 23:53 → at once (7 min left), 23:54 → `too_late`; `TestAtOnceMarginExceedsProactiveInterval`; `TestI36_RepeatingRequestBeforeLink7SaysOnlyThisOnce` (the reply names the one-shot and "solo esta vez").
- [ ] 2.3 GREEN: the margin raised to 7 (doc 02 §13); the store, capture, phrase, channels and gate branch; the I36 row in doc 06 §4; replaying `classify-task-remind-me-coffee.json` (recorded in the 1a′ session); L4 of the observed message; doc 02 §5 step 5, §11; doc 03; doc 07; `make check-all`.

## PR 3 — `feat/remind-me-follows` (D9)

- [ ] 3.1 RED:
  - `TestCorrectedAskedTaskOldRowIsNotSentAtItsOldTime`: an asked task 10:00 → 11:00 → nothing at 09:45; one row at 10:45 (the #343 `currentEvent` seam).
  - `TestI36_AnAskedReminderFollowsItsInstant`: an asked task due 10:00, armed 09:45; "a las 11" → moved to 10:45, one `correction.reminder.moved`.
  - `TestFollowKeepsDayOnlyAcrossDateCorrection`: "recordame el jueves" then "no, el viernes" → Friday 09:00 `day_only`, never Thursday 23:45 night-exempt (round 2, C3).
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
