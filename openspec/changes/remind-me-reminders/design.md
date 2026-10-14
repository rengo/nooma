# Design — remind-me-reminders (shared decisions, one-shot links 1-4)

- **Rules:** ADR-0031 (Accepted by the client, 2026-10-14).
- **Requirements:** `spec.md`.
- **Links 5-9** (question store, stop, recurring, ask-back, done) are in `design-repeating.md`.
- **Lines** refer to `8f45531` (#340, ADR-0032 link 2, merged).

## 1. Ground truth

| Claim | Where |
|---|---|
| Observed: a `task` with a `due_at`, no trigger, and the reply "Anotado." | client vault, 2026-10-14 |
| `Arm` arms `timer`, `event` and `recurring_reminder`, and nothing else | `core/prospection/arm.go` |
| `eventReminders` plans the leads (`reminderInstants`), moves each out of the night (`dueAt`), and **drops** a past lead when now is quiet (`deliverAt`, `reminders.go:61-71`) | `prospection/reminders.go` |
| Day only is inferred from local midnight (`isDateOnly`, `reminders.go:189`). A bare date on a DST day without midnight lands at 01:00 (Chile, `classify/prompt.go:290-296`) | `reminders.go` |
| The decoder knows whether the model wrote `YYYY-MM-DD` or a timestamp (`dateOnlyLayout`) | `classify/decode.go:11` |
| A correction follows `event_at` of an `event` only, and never creates an at-once reminder (I29) | `brain/correction_reminder.go:23-41`, `follow.go:56-57` |
| The preference re-arm keeps `event` units with `event_at` ahead | `brain/preferences_rearm.go:49` |
| Check-in resolution engages `Delivered()[0]` from the model's outcome, before any phrase logic could run | `brain/checkin.go:19-55`, `capture.go:302` |
| The migration runner already rejects a gap or a duplicate version | `internal/store/sqlite/migrate_test.go:55` |
| `ReminderVerdict` reads the payload's `event_at` first, treats day-granular events (`isDateOnly`, or a rule) as passed from the next local midnight, and defers through quiet hours, measuring overdue from quiet end (`staleness.go:116`) | `core/prospection/reminder.go:53-110` |
| `DueTrigger` carries no `created_at` | `ports/triggerrepo.go` |

## 2. Decisions

**D1. The phrase reader** (`core/classify/phrase.go`, pure).

- **Normalisation:** lower case. A hand-written fold table (`á→a`, `é→e`, `í→i`, `ó→o`, `ú→u`,
  `ü→u`, `ñ→n`), since core has no `x/text`. `’` and `'` become `'`, and `dont` is listed beside
  `don't`.
- **Tokens and clauses:** words are split into clauses at `, ; . ! ? :` and at the conjunctions
  `y`, `pero`, `si`, `and`, `but`, `if`.
- **Negation:** governs a cue only inside its own clause, when the cue's previous words are a
  negation (`no`, `nunca`, `don't`, `do not`, `never`) followed only by clitics (`me`, `te`,
  `lo`, `la`, `le`, `se`). "no te olvides de recordarme" is not negated, because `de` is no
  clitic.
- **Cue lists** (`RemindCues(Language)`, swept over `AllLanguages()`):

  | Language | Cues |
  |---|---|
  | es | `recordame`, `recuérdame`, `recordarme`, `recordámelo`, `recuérdamelo`, `acordame`, `avisame`, `avisarme`, `haceme acordar`, `hazme acordar`, `poneme un recordatorio`, `ponme un recordatorio`, `que no se me olvide`, `no te olvides de` + (`recordarme`, `avisarme`) |
  | en | `remind me`, `set a reminder`, `don't let me forget` |

  `reminder to` is out (the dentist message).
- **Exclusions:** a cue followed by an interrogative (`qué`, `cuándo`, `dónde`, `cómo`, `quién`,
  `si`, `what`, `when`, `where`, `who`) is a recall. Any stop phrase (`design-repeating.md` D21)
  wins over a cue.
- **API:** `AsksToBeReminded(text) bool`, read on `CaptureInput.Text` only.
- **P4:** quoted speech counts as asked, pinned by `TestQuotedSpeechCountsAsAskedKnownLimit`.

**D2. Day only from the wire.** `Classification` gains `EventAtDayOnly` and `DueAtDayOnly`,
which `Decode` sets when the value parsed with `dateOnlyLayout`. This records the value's shape,
not a model claim. Asked leads read it, never midnight. The ADR-0029 event path keeps its
midnight rule, which this change does not touch.

**D3. `Arm(c, asked bool, prefs, now)`** for any kind that persists a unit with an instant (its
own field, else the other). `leadSet(kind, asked, at, dayOnly, prefs, zone)` returns the nominal
instants, each tagged `asked`:

| Case | Asked lead |
|---|---|
| Named time | `at − RemindMeLead` |
| Day only | that local day at `DateOnlyAt` |
| `event` | `reminderInstants` plus the asked lead |

`reminders(at, leads, …)` replaces `eventReminders`, with merging and `FollowSent` coverage
unchanged. An asked lead that is behind while `at` is ahead becomes **at once** (PM default).
**Too late:** an at-once asked arming whose instant, or whose day for a day-only one, ends within
`AtOnceMarginMinutes` (5, §13, asserted equal to `scheduler.ProactiveCheckInterval`) is refused
`too_late`, and the reply says so (spec R4, the 23:55 case).

**D4. The night.** An asked lead at a named time, and any asked at-once arming, skip both
`dueAt`'s move and `deliverAt`'s quiet drop. So "a las 6:30" said at 06:20 rings at once, and
`Follow` and `CatchUp`, which share the planner, agree. At the scan, `ReminderVerdict` skips
`Defer` for a row whose payload says `night_exempt` (`Reminder` gains `NightExempt`, `DayOnly`
and, in link 7, `OccursAt`). **I34 is amended** in link 1:
"…except an asked reminder at a named time or armed at once".

**D5. Storage, migration M1** (link 2):

- `units.remind_me INTEGER NOT NULL DEFAULT 0`;
- `config.remind_me_lead_minutes INTEGER` (nullable).

Migration numbers are assigned at apply. A collision with #339's `units.language` fails the
runner test on rebase (ground truth). Payload keys, per row shape:

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

**D7. Capture.** `asked` is computed once, before `Arm`.

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
  carrying `dayOnly` from the correction's classification.
- **First arming at once (I29 amended):** `Follow` may create an at-once reminder when the unit
  asked and has **no trigger row of any status** (#339's `OneShotForUnit`, or a `CountForUnit`
  read if that has not landed). A capture refusal followed by a correction, and the ask-back time
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

§13 constants: `default_remind_me_lead_minutes` (15), `at_once_margin_minutes` (5), plus
`remind_question_window_minutes` (60) and `stop_reply_window_minutes` (120) in links 6 and 8.

## 4. Chain (stacked to main; P1: the stop lands before anything repeats)

| Link | Branch | Content | Impl+docs | Usable after it |
|---|---|---|---|---|
| 1 | `feat/remind-me-decision` | D1-D4 (core), D6 | ~260 | — |
| 2 | `feat/remind-me-one-shot` | D5, D7, D8, D4 scan; I36 | ~350 | **First usable**: "a las H", "mañana", asked event |
| 3 | `feat/remind-me-follows` | D9 | ~190 | corrections and lead changes move it |
| 4 | `feat/remind-me-preference-ui` | the lead on `/ui/preferences` | ~110 | lead editable |
| 5 | `feat/question-store-widens` | `design-repeating.md` D10-D13 | ~220 | — |
| 6 | `feat/remind-me-stop` | D20-D24 | ~390 | "no me lo recuerdes más" |
| 7 | `feat/recurring-rearm` | D30-D35 | ~380 | "todos los días a las 9pm"; birthdays re-arm |
| 8 | `feat/remind-me-ask-back` | D40-D43 | ~300 | "recordame X" asks; "No" makes it daily |
| 9 | `feat/remind-me-done` | D50-D52 | ~260 | bare "listo" |

Dependencies: 1 → 2 → 3 → 4. Link 5 stands alone. Link 6 needs 2 and 5. Link 7 needs 6. Link 8
needs 3, 5, 6 and 7. Link 9 needs 6 and 7.

**Seam with ADR-0032's unlanded links 3-7** (link 3 is in progress):

- `CatchUp` and `OneShotForUnit`: if they are absent when link 3 lands, D9 uses a
  `CountForUnit` read, and `CatchUp` takes `leadSet` when it lands.
- The renderer's `About`: until it lands, the frozen `payload.action` is sent. Link 7's
  `occurs_at` is read by whichever renderer exists.
- `units.language`: independent.
- **Every ADR-0032 link that arms or sends honours `reminder_stopped_at` from link 6 on**, by
  structure. The guard sits inside the SQL of `Create`/`Reschedule`, and the stop verdict inside
  `judgeReminder`, so no caller can bypass either. `TestEveryCreateCallerSkipsErrUnitStopped`, an
  AST sweep of `brain`, fails a caller that aborts its pass on `ErrUnitStopped` instead of
  skipping and recording it.
