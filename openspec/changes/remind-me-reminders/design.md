# Design — remind-me-reminders (shared decisions, one-shot links 1-4)

- **Rules:** ADR-0031 (Accepted by the client, 2026-10-14); its detection is superseded by ADR-0033
  (Accepted, 2026-10-14): the model understands, the code acts.
- **Requirements:** `spec.md`.
- **Links 5-8** (question store, stop, recurring, ask-back) are in `design-repeating.md`. Done
  handling (old link 9) is deferred (client, 2026-10-14).
- **Lines** refer to `dd1ba9a`.

## 1. Ground truth

| Claim | Where |
|---|---|
| Observed: a `task` with a `due_at`, no trigger, and the reply "Anotado." | client vault, 2026-10-14 |
| `Arm` arms `timer`, `event` and `recurring_reminder`, and nothing else | `core/prospection/arm.go` |
| `eventReminders` plans the leads (`reminderInstants`), moves each out of the night (`dueAt`), and **drops** a past lead when now is quiet (`deliverAt`, `reminders.go:61-71`) | `prospection/reminders.go` |
| Day only is inferred from local midnight (`isDateOnly`, `reminders.go:291`). A bare date on a DST day without midnight lands at 01:00 (Chile, `classify/prompt.go:290-296`) | `reminders.go` |
| The decoder knows whether the model wrote `YYYY-MM-DD` or a timestamp (`dateOnlyLayout`) | `classify/decode.go:11` |
| A correction follows `event_at` of an `event` only, and never creates an at-once reminder (I29) | `brain/correction_reminder.go:23-41`, `follow.go:56-57` |
| The preference re-arm keeps `event` units with `event_at` ahead | `brain/preferences_rearm.go:49` |
| Check-in resolution engages `Delivered()[0]` from the model's outcome, before any reminder binding could run | `brain/checkin.go:19-55`, `capture.go:302` |
| The migration runner already rejects a gap or a duplicate version | `internal/store/sqlite/migrate_test.go:55` |
| `ReminderVerdict` reads the payload's `event_at` first, treats day-granular events (`isDateOnly`, or a rule) as passed from the next local midnight, and defers through quiet hours, measuring overdue from quiet end (`staleness.go:116`) | `core/prospection/reminder.go:53-100` |
| `DueTrigger` carries no `created_at` | `ports/triggerrepo.go` |

## 2. Decisions

**D1. Reminder fields in the classify call** (ADR-0033; replaces the phrase reader; link 1a).

- **Wire** (optional, each degrading on its own, I14):
  - `remind_request`: a JSON bool or null, anything else degrades;
  - `reminder_intent` ∈ `AllReminderIntents()` = {none, stop, keep}, `keep` only as the answer
    to an open undo;
  - `question_answer` ∈ {none, when, no, choice} with `choice_number`;
  - `reminder_target`: a position 1-5 or `"unclear"`;
  - `title` (D1b).
- **Prompt** (final in link 1a, so link 1a′ records against it):
  - `BuildPrompt` and `BuildCorrectionPrompt` both describe the fields, so an explicit-referent
    stop is read too.
  - Examples in es and en cover a recall, a third party's reminder, reported speech, and
    "listo" (which reads `none`: done handling is deferred).
  - **One numbered list per prompt:** the open `which_reminder` question's options when it is
    open, otherwise the stop candidates (design-repeating D22). It appears only when
    there is one. The list, and the question text Nooma sends, come from one renderer,
    `classify.RenderReminderList`. `TestPromptListEqualsTheQuestionText` compares the two, and
    the recordings are made from the same output.
  - `BuildCorrectionPrompt` renders a day-only instant as a bare `YYYY-MM-DD`, never as
    midnight. A date correction shows the unit's current title and asks for a fresh one.
  - No phrase list in core.
- **Doc 02 §5.1 (483-495)** is edited in link 1a. Its rule "the prompt is never widened to inject
  open questions so the model can name an id" gains the narrower reminder rule: a position in a
  code-built list, validated by code, undoable, and never a relation.
- **Brain:** `asked := c.RemindRequest != nil && *c.RemindRequest`. A degraded field reads as
  none and takes the plain capture path, recorded in `capture.classify`. A failed call saves
  nothing (I31).
- **Missed request (Q4):** a stored unit with an instant, not asked, nothing armed, gets
  `CaptureResult.NoReminderHint`, rendered as "No puse aviso; si querés, decime «recordame»".

**D1b. A title** (client and Q8; link 1a).

- **Wire and check:** classify returns `title` for every unit an armed kind stores.
  `classify.ResolveTitle` trims it and drops one that is blank, holds a newline or control
  character, exceeds `max_title_runes` (60, §13), or holds an ISO date or `HH:MM`. Relative words
  are a known limit, covered by a prompt rule and the doctor measure.
- **Storage:** migration **0009** (0008 shipped as `units.language`): `units.title TEXT NULL`.
  `unit.Unit.Title`, `ToUnit`, the store and memrepo carry it. There is no backfill, and later
  migrations take fixed numbers: **0010** `units.remind_me` and the lead (link 2), **0011** the
  `pending_questions` rebuild (link 5), **0012** `units.reminder_stopped_at` (link 6). The
  runner's gap and duplicate test catches a collision.
- **Correction:** `correction.Field` gains `FieldTitle`, and `AllFields` and its sweep
  (`edit_test.go`) follow. A re-classifying correction with a valid title writes it. A content
  edit without one writes `title: null`, which clears it. A date-only edit leaves the title
  alone. `correction.applied` lists `title` in `fields` with `previous` and `next`. Doc 02 §5
  step 4 and §11 change.
- **Render:** `unitWords` (`render.go:137`) and `frozenText` use the title, falling back to the
  content. A push reads "Recordatorio: <title> — <date>", and a digest line or Today "• <title> —
  <when>".

**D1c. Recording, once** (link 1a′).

- **Format:**
  - `testdata/llm/reminders/*.json` is embedded by `corpus.go` beside `cases/*.json`.
  - `Case` and `goldenset.LLMExample` gain `record`, `now`, `open_question`, `candidates_list`,
    `expect` and `known_misread`, still under `DisallowUnknownFields`.
  - `format.md` and its format test change, and `TestCasesMatchesTheSourceFiles` covers the new
    directory.
- **Before recording:** a case reads `"record": true` with no `response`. `Validate` allows that,
  and replay skips it and prints how many await recording, so the branch stays green.
  - **The block:** `scripts/recordings-complete.sh` runs as a step of the existing required
    `lint` job in `ci.yml`, so a PR with any `record: true` left cannot merge. No ruleset edit is
    needed: the `protect-main` ruleset already requires `lint`.
  - **Not in `check-all`, on purpose:** `check-all` runs before every push, and it must pass
    while cases still await recording. This one exception to CLAUDE.md's rule is written in doc
    06 §6. The script is tested directly, like `docs-sync.sh`.
- **Prompt hash:** each recording stores `prompt_sha256` of the prompt it was recorded against.
  Replay rebuilds that prompt through `BuildPrompt`, with the case's `now`, question and list,
  and fails on a mismatch, so a prompt change cannot leave stale recordings green.
- **Command:** `nooma doctor --record <dir> [vault]`.
  - It refuses when `CI` is set.
  - It sends each `record: true` case through the production `BuildPrompt`, with
    `beliefs = nil` set explicitly and pinned by a test, and with the case's `now`, which it
    stores.
  - It writes `response`, `provider` and `model`, then clears `record`.
  - A case that already has a response is skipped unless `--force` is given.
  - Cases are synthetic only; no vault content is used.
- **Known misread:** `known_misread: "<why>"` keeps a misread phrasing in the corpus, visibly
  marked. Replay asserts that it still misreads, and doctor counts it as a miss. Nobody
  hand-edits a response.
- **Existing recordings:** none is re-recorded. I18, I26, `arming_capture_keeps_its_unit` and
  `classify_golden_cases_decode_clean` assert their content, and the new fields are absent from
  them, which reads as none.
- **Doctor:** `nooma doctor --reading` is opt-in, about 40 calls and a minute or two. It prints
  "reminder reading: k of n" (90% is the reversal threshold), a **hard** line "stop false
  positives: 0 of N negatives" that fails doctor when non-zero, "titles: k of n valid",
  "day only: k of n bare dates", and the `task_checkin_outcome`, `nudge_outcome` and
  `relation_outcome` rates over the check-in answers in the set, so a regression link 1a
  introduces there shows.

**D2. Day only from the wire.** `Classification` gains `EventAtDayOnly` and `DueAtDayOnly`,
which `Decode` sets when the value parsed with `dateOnlyLayout`. This records the value's shape,
not a model claim. Asked leads read it, never midnight. The ADR-0029 event path keeps its
midnight rule, which this change does not touch.

**D3. `Arm(c, asked bool, prefs, now)`** for any kind that persists a unit with an instant (its
own field, else the other); with `asked` and a `recurrence_rule`, `Arm` takes the recurring branch
whatever the kind (link 7, `design-repeating.md` D31), and until link 7 it plans the one-shot
occurrence. `leadSet(kind, asked, at, dayOnly, prefs, zone)` returns the nominal
instants, each tagged `asked`:

| Case | Asked lead |
|---|---|
| Named time | `at − RemindMeLead` |
| Day only | that local day at `DateOnlyAt` |
| `event` | `reminderInstants` plus the asked lead |

`reminders(at, leads, …)` replaces `eventReminders`, with merging and `FollowSent` coverage
unchanged. An asked lead that is behind while `at` is ahead becomes **at once** (PM default).
**Too late:** an at-once asked arming is refused `too_late` only when **less than**
`AtOnceMarginMinutes` (5, §13, equal to `scheduler.ProactiveCheckInterval`) remain before its
instant, or before the end of its local day for a day-only or "hoy" request (`end − now < 5 min`,
strict): 23:55 → at once, 23:56 → refused. The reply says so (spec R4).

**D4. The night.** An asked lead at a named time, and any asked at-once arming, skip both
`dueAt`'s move and `deliverAt`'s quiet drop. So "a las 6:30" said at 06:20 rings at once, and
`Follow` and `CatchUp`, which share the planner, agree. At the scan, `ReminderVerdict` skips
`Defer` for a row whose payload says `night_exempt` (`Reminder` gains `NightExempt`, `DayOnly`
and, in link 7, `OccursAt`). **I34 is amended** in link 1:
"…except an asked reminder at a named time or armed at once".

**D5. Storage, migration M1** (link 2):

- `units.remind_me INTEGER NOT NULL DEFAULT 0`;
- `config.remind_me_lead_minutes INTEGER` (nullable).

Migration **0010** (D1b's fixed numbering; 0008 shipped as `units.language`). Payload keys, per
row shape:

| Row | `lead_minutes` | `event_at` | `occurs_at` | `asked` | `day_only` | `night_exempt` |
|---|---|---|---|---|---|---|
| Named time | the lead | the instant | — | ✓ | — | ✓ |
| Named time, at once | 0 | the instant | — | ✓ | — | ✓ |
| Day only | 0 | that day's local midnight | — | ✓ | ✓ | only at once |
| Asked lead of an event | the lead | the event | — | ✓ | — | ✓ |
| Recurring (links 7-8) | 0 | — | the occurrence | `design-repeating.md` D30 | — | D30 |

**How the gate (`ReminderVerdict`) reads them:**

- `about` is `event_at`, else `occurs_at` (link 7), else as it does today.
- A row with `day_only` is passed from the next local midnight, whatever the hour of its midnight
  (the DST case). So an asked day-only row firing at 09:00 is not passed.
- `night_exempt` skips `Defer`, and its overdue is measured from `fire_at`, not from quiet end, so
  an exempt 03:00 reminder is not sent at 09:00 (`verdict(fireAt, fireAt, …, nil)`, as for timers).
- An asked row (`asked` set) is judged day-granular **only** by its `day_only` key, so an asked
  named midnight passes at midnight. Rows without `asked` keep the `isDateOnly` heuristic.

**D6. Prefs.** `RemindMeLead` and `DefaultRemindMeLeadMinutes = 15`. `ParseRemindMeLead` accepts
1 minute to `max_event_lead_days`, and `ResolveReminderPrefs` gains the argument.

**D7. Capture.** `asked` is read once from the classification (D1), before `Arm`.

- **Armed:** `capture.armed.trigger` with `asked`.
- **Asked, kept, not armed:** `capture.arm.refused {why, unit_id, asked}`, with
  `CaptureResult.NotReminded`. The reasons are `already_past`, `quiet_hours` and `too_late`.
- **Asked, no instant:** `design-repeating.md` D40.

**D8. The reply.** `Armed.Later` becomes `Armed.Firings []{FireAt, Asked, Lead, DayOnly,
AtOnce}`. New `phrase.Set` fields:

- `LeadBefore` and duration words;
- `ThatDayAt` ("ese día a las %s"), rendered from the **armed** instant, so a `DateOnlyAt` of
  08:30 says 08:30;
- `AtOnce` ("ya te lo recuerdo");
- `NotReminded` plus one sentence per reason.

Completeness is swept over both languages.

**D9. Follow** (link 3).

- **Correction:** `followReminder` also runs for a `RemindMe` unit whose instant field moved,
  carrying `dayOnly` from the correction's classification. When the corrected instant is local
  midnight and the unit's armed rows carry `day_only`, `Follow` keeps it day only, so it never
  arms a 23:45 night-exempt lead (round 2, C3).
- **First arming at once (I29 amended):** `Follow` may create an at-once reminder when the unit
  asked and has **no trigger row of any status** (`TriggerRepo.OneShotForUnit`, landed). A capture refusal followed by a correction, and the ask-back time
  answer, both rely on this.
- **Re-arm:** the preference re-arm adds `RemindMe` units with their instant ahead. Lead-only
  saves re-arm. **I35 is amended** (asked units re-arm). It reads `asked` and `day_only` from the
  unit's **armed rows' payload**, never from the unit's midnight, so a lead-only save leaves an
  asked day-only reminder at `DateOnlyAt`, and a `DateOnlyAt` save moves it to the new time.
  A unit with no armed row is not re-armed (ADR-0029 point 6).
- **Stop:** from link 6, every one of these paths skips a stopped unit.

## 3. Invariants and doc amendments

| Link | Doc 06 §4 / doc 02 |
|---|---|
| 1 | I34 amended (D4); doc 02 §5 step 1, §7, §13 |
| 2 | I36 new (capture, reply); doc 02 §5 step 5, §11; doc 03 |
| 3 | I29 amended (first arming); I35 amended; doc 02 §5 step 4, §7 |
| 5-9 | `design-repeating.md` §3 |

§13 constants: `default_remind_me_lead_minutes` (15) and `at_once_margin_minutes` (5) in link 1;
`max_title_runes` (60) in link 1a;
`ask_back_window_minutes` (60), `which_reminder_window_minutes` (60) and
`stop_reply_window_minutes` (120) and `stop_undo_window_minutes` (10, ADR-0033), all in link 5.

## 4. Chain (stacked to main; P1: the stop lands before anything repeats)

| Link | Branch | Content | Impl+docs | Usable after it |
|---|---|---|---|---|
| 1 | `feat/remind-me-decision` | D2-D4 (core), D6 | merged (#346, #347) | — |
| 1a | `feat/classify-reminder-fields` | D1, D1b: the fields and the final prompt, the title, migration 0009, render, correction, doc 02 §5.1 | ~380 | **Reminders read "Recordatorio: <title> — <date>"** |
| 1a′ | `feat/doctor-record-and-reading` | D1c: the format, `--record`, `doctor --reading`, the recorded set, the merge check | ~260 | detection is measured |
| 2 | `feat/remind-me-one-shot` | D5, D7, D8, D4 scan; I36 | ~350 | **First usable**: "a las H", "mañana", asked event |
| 3 | `feat/remind-me-follows` | D9 | ~190 | corrections and lead changes move it |
| 4 | `feat/remind-me-preference-ui` | the lead on `/ui/preferences` | ~110 | lead editable |
| 5 | `feat/question-store-widens` | `design-repeating.md` D10-D13 | ~220 | — |
| 6 | `feat/remind-me-stop` | D20-D24 | ~390 | "no me lo recuerdes más" |
| 7 | `feat/recurring-rearm` | D30-D35 | ~380 | "todos los días a las 9pm"; birthdays re-arm |
| 8 | `feat/remind-me-ask-back` | D40-D43 | ~300 | "recordame X" asks; "No" makes it daily |

Dependencies: 1 → 1a → 1a′ → 2 → 3 → 4. Link 5 stands alone. Link 6 needs 2 and 5. Link 7 needs 6. Link 8
needs 3, 5, 6 and 7. Old link 9 (done) is deferred.

**Seam with ADR-0032 (landed: #343, #344, `OneShotForUnit`, the renderer, `units.language`):**

- **#344's re-plan** (`replanUnit`, `reminder_step.go`) calls `FollowSent` with event leads only.
  From link 2 it plans through `leadSet`, with `asked`, `day_only` and `night_exempt` read from the
  unit's armed rows' payload, so it never moves or cancels the +15 lead nor drops its keys.
- **#343's `currentEvent`** returns `event_at` only. From link 2 it returns the instant field of a
  `RemindMe` unit (`due_at` for a task), so a corrected asked task is judged against its new
  instant and its old row lapses `superseded` only after link 3 has re-armed it.
- **Every ADR-0032 link that arms or sends honours `reminder_stopped_at` from link 6 on**, by
  structure. The guard sits inside the SQL of `Create`/`Reschedule`, and the stop verdict inside
  #343's `reminderGate.judge`, so no caller can bypass either. `TestEveryCreateCallerSkipsErrUnitStopped`, an
  AST sweep of `brain`, fails a caller that aborts its pass on `ErrUnitStopped` instead of
  skipping and recording it.
