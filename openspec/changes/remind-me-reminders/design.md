# Design — remind-me-reminders

When a message asks to be reminded, Nooma:

- arms a reminder: at the user's remind-me lead before a named time, that day at 09:00 for a day
  alone, or every occurrence for a recurrence;
- asks back when no day or time was given;
- stops the reminder when the user says so in chat;
- names in the reply exactly when the reminder will come.

Rules: ADR-0031 (Proposed). Product: `proposal.md`. Requirements: `spec.md` R1-R11. Lines at `9771ee1`.

## 1. Ground truth

| Claim | Where |
|---|---|
| Observed: `task`, `due_at` 2026-10-15T13:00Z, content "Recordarme comprar café…", no trigger, reply "Anotado." | client vault, 2026-10-14 |
| `Arm` arms `timer`, `event` and `recurring_reminder`. Every other kind is `RefusalKindNotArming` | `core/prospection/arm.go` |
| `eventReminders` takes the leads from `reminderInstants`, moves each out of the night (`dueAt`/`deliverAt`), merges leads that land on one instant, and arms at once when every lead is behind | `prospection/reminders.go` |
| Nothing carries "the user asked". The prompt only keeps "recordame" untranslated | `classify/classification.go`, `prompt.go:107-112` |
| Capture arms after the unit is stored (`armAll`). Only a quiet-hours refusal of a kept unit writes `capture.arm.refused` | `brain/capture.go:317-500` |
| A correction follows `event_at` of an `event` only | `brain/correction_reminder.go:23-41` |
| The preference re-arm keeps `event` units with `event_at` ahead | `brain/preferences_rearm.go:49` |
| One-shot triggers are pushed; recurring ones follow the interrupt level | `prospection/delivery.go` `RouteTrigger`, `brain/deliver.go:77` |
| The scan defers every trigger through quiet hours; a timer is exempt | `prospection/staleness.go`, `brain/check.go:410-418` |
| **A fired recurring trigger is never re-armed.** `NextOccurrence` has no caller outside `Arm`, despite doc 02 §7 ("On firing, the next one is created automatically") | `rg NextOccurrence internal/brain` → nothing |
| Occurrences land at `RecurrenceAnchorHour` (12) and the anchor is JSON `{month, day, weekday}` | `recurrence.go:37-54`, migration 0001:56 |
| A check-in answer binds to the **most recent** open item, with the model only giving the outcome; m3e's relation answers do the same | `brain/checkin.go:23-54, 116-176` |
| `pending_questions` holds `relation_id TEXT NOT NULL` and one kind. ADR-0027's reversal criterion names "a second question kind with no relation to name" as the reason to supersede | migration 0004, ADR-0027 |
| The reply for an event uses `NotedFor` + `Leads` in calendar words | `core/phrase/phrase.go`, `channels/reply.go` |
| Migrations end at 0007 | `internal/store/sqlite/migrations/` |

### What #339 merged (9771ee1): a plan, not code

- ADR-0032 is Accepted. I37 and the `digest-stale-reminders` links 2-7 are planned, none
  implemented. Its `ReminderVerdict(Reminder{FireAt, About, FromEvent, Current, Newer, Waiting})`
  will gate every delivery: `Current` is the unit's `event_at` (`due_at` for a task), a one-shot
  row whose `event_at` differs is `superseded`, and a recurring row's `About` is
  `NextOccurrence(rule, anchor, fire_at)`, "assuming a period longer than the lead".
- Its §4-5 fixes the split: this change keeps ADR-0031, I36 and the first migration number; it
  takes ADR-0032, I37 and the next. **No migration has merged**, so the migration that merges
  second renumbers. This plan names them M1-M3. I38 is free (doc 06 §4 ends at I35).
- It asks this change to write `event_at` on task reminders, and to put the named-time
  exemption inside `ReminderVerdict`.

## 2. Decisions

### One-shot (links 1-4)

**D1. "Asked" comes from the raw message, through a closed list in core.**
`classify.AsksToBeReminded(text) bool` in `core/classify/remind.go` checks the text against
`RemindCues(Language)`, one list per member of `AllLanguages()`. A sweep fails for a language
without a list.

- **Matching:** lower-cased, accents folded, whole words, multi-word cues matched as word
  sequences. A negation earlier in the same clause cancels a cue: es `no`, `nunca`; en `don't`,
  `do not`, `never`.
- **Initial lists**, each cue pinned by a test: es `recordame`, `recuérdame`, `recordarme`,
  `acordame`, `avisame`, `avisarme`; en `remind me`, `reminder to`.
- **Scope:** the cue is read from `CaptureInput.Text` and checked against every language's list.

A model field was rejected (ADR-0031, options): it trusts the model for what the user wrote.

**D2. `Arm(c, asked bool, prefs, now)`.** With `asked`, any kind that persists a unit and has an
instant plans the asked lead. The instant comes from its kind's own field (`event_at` for an
`event`, `due_at` otherwise), falling back to the other field.

- An `event` adds the asked lead to its event leads.
- A `timer` ignores `asked`.
- A message with a `recurrence_rule` goes to D10.

**D3. One lead set, one planner.** `eventReminders` becomes `reminders(at, leads, …)`.
`leadSet(kind, asked, at, prefs, zone)` returns one nominal instant per lead, each tagged as
asked or not:

| Case | Leads |
|---|---|
| `event` | `reminderInstants`, as today |
| asked, named time | `at − RemindMeLead` |
| asked, day only (local midnight, as ADR-0029) | that day at `DateOnlyAt` |

The rest is unchanged: leads on one instant merge, `Plan.Leads`, `FollowSent` coverage, and the
at-once fallback. At-once now covers the PM default: an asked lead is behind and the instant is
ahead. A day-only lead past 09:00 that same day is armed at once while the day is ahead. Plans
serving the asked lead carry `Plan.Asked`. `Follow`, `FollowSent` and #339's `CatchUp` take
their lead set from `leadSet`, so every path plans the same reminders.

**D4. Named time and the night.** An asked lead at a named time skips `dueAt`'s quiet move.
Merges key on the delivered instant, so an asked lead no longer merges with an event lead the
night moved.

- **Scan:** if #339's link 2 is in, `Reminder` gains `Asked bool` and `ReminderVerdict` skips
  the quiet-hours `Defer` for it, with `passed` and `superseded` unchanged. If not,
  `check.go:410-418` routes an asked row to `AskedReminderVerdict` (no defer, event-start expiry),
  and #339's link 2 folds it into `ReminderVerdict`.

**D5. Storage, migration M1 (the next free number when link 2 merges).**

- `units.remind_me INTEGER NOT NULL DEFAULT 0`. The unit holds the request because a correction,
  a re-plan and a stop need it when nothing is armed. Example: "a las 10" sent at 10:05 is
  refused as past, and the correction to tomorrow must still know the user asked.
- `config.remind_me_lead_minutes INTEGER`, nullable.
- `TriggerPayload.Asked` (`asked`, omitted when false) is the only payload change. A one-shot
  asked row already writes `event_at` and `user_leads_minutes` (`capture.go:976-986`), which is
  what #339's gate reads.

No `CHECK` constraints (schema convention, ADR-0027).

**D6. Prefs.** `ReminderPrefs.RemindMeLead`, `DefaultRemindMeLeadMinutes = 15` (§13), and
`ParseRemindMeLead`, valid from 1 minute to `max_event_lead_days`. `ResolveReminderPrefs` gains
the argument.

**D7. Capture.** `asked` is computed once, before `Arm`.

- **Armed:** `capture.armed.trigger` gains `asked: true` in its context.
- **Asked, unit kept, nothing armed:** a `capture.arm.refused` row `{why, unit_id, asked}`, and
  `CaptureResult.NotReminded`.
- **Asked with no instant:** D12.

**D8. The reply.**

- `Armed.Later []time.Time` becomes `Armed.Firings []ArmedFiring{FireAt, Asked, Lead, DayOnly}`.
- New `phrase.Set` fields: `LeadBefore` ("%s antes" / "%s before"), duration words, `ThatDayAt`
  ("ese día a las %s"), `EveryDayAt` and `EveryWeekOn`/`EveryMonthOn`/`EveryYearOn`, and
  `NotReminded`.
- The completeness sweep covers both languages.

**D9. Correction and re-arm.**

- **Correction:** `followReminder` runs when an `event`'s `event_at` moved, or when a `RemindMe`
  unit's instant field moved. A unit that did not ask still ignores a `due_at` edit.
- **Re-arm:** the preference re-arm adds `RemindMe` units with their instant ahead. A save that
  changes only the lead re-arms.

### Recurring (link 5)

**D10. An explicit recurrence is a recurring task at the named time.**

- **Unit:** with `asked` and a `recurrence_rule`, `ToUnit` builds a `task` whatever the kind.
- **Trigger:** `recurringTrigger` gains `asked` and `at TimeOfDay`. The anchor gains an optional
  `hour`/`minute` in its JSON (no migration), taken from the instant's local time, or
  `DateOnlyAt` when the instant is day-only. `NextOccurrence` lands on the anchor's time when it
  has one, and on `RecurrenceAnchorHour` otherwise. The trigger fires **at** the occurrence:
  `lead_days` 0, payload `asked` and `occurs_at`.
- **Delivery:** `RouteTrigger` pushes an asked recurring row. A named time is exempt from quiet
  hours.
- **#339's gate:** in `ReminderVerdict`, an asked recurring row's `About` is `occurs_at`. It is
  never `FromEvent`, so never `superseded`, and never `Waiting`. Without this, `NextOccurrence`
  strictly after a zero-lead `fire_at` would name the *next* day.
- A birthday that was asked ("recordame el cumple de Ana todos los años") becomes a `task` that
  reminds at 09:00 on the day. Not asked, it stays an `event` with 7 days ahead.

**D11. Re-arm on fire, for every recurring trigger.** When the scan fires a recurring row, it
creates the next occurrence through `recurringTrigger(anchor, rule, after=occurrence)` for the
same unit, keeping the payload's `asked`, `lead_days` and interrupt level, and records
`check.trigger.rearmed`. The trigger id is `<id-of-first>:<occurrence UTC>`, and
`ErrTriggerExists` means a racing pass already did it. This closes the doc 02 §7 divergence
above.

### Ask-back (link 6)

**D12. The question store widens.** This supersedes ADR-0027's table shape only. The decision to
keep questions in a dedicated store stands. Migration M2 rebuilds `pending_questions`:

- `relation_id` becomes nullable;
- `unit_id` is added (nullable, no FK, for ADR-0027's reasons);
- `options` is added (JSON, nullable);
- the indexes are rebuilt.

Three kinds join the vocabulary: `remind_when {unit_id}`, `keep_or_stop {unit_id}` and
`which_reminder {options: [unit_id…]}`.
The relation reads keep `kind = 'relation'`. The port gains `CreateAbout(unit/options)` and
`OpenOfKind(kind)`.

**D13. Asking.** An asked unit with no instant is stored with `remind_me = 1`. Then:

- `remind_when` is created **already asked** (`asked_at = now`), because the reply carries it;
- `CaptureResult.Outcome = OutcomeAskedWhen` replies "¿Algún día y hora en particular?";
- a newer `remind_when` expires the older one: one is open at a time.

**D14. Binding the answer, deterministically.** Before classification, if a `remind_when` was
asked within `remind_question_window_minutes` (named constant, 60, §13) and nothing was captured
after it:

1. **No-answer:** `classify.IsNoAnswer(text)`, a closed list ("no", "nop", "no hace falta",
   "no, gracias", "nope", "no thanks"). The unit gets the daily rule at `DateOnlyAt` through D10,
   the question is resolved `declined`, and the reply is "Anotado. Te aviso todos los días a las
   09:00.".
2. **Time answer:** `classify.IsOnlyWhen(text)` holds when nothing is left after the date and time
   words, numbers and stopwords are removed. A closed per-language lexicon, swept. The message is
   then run as an **explicit correction** of the unit (`ReferentID`). The model resolves the
   instant from "mañana a las 10" exactly as I30 already does. The correction path (D9) arms the
   reminder, the question is resolved `answered`, and the reply is R5's.
3. **Anything else** is a normal capture, and the question expires (`expired`) once the window
   passes.

The model never chooses the question. There is at most one open question and it is bound by
time. The lexicon only tells an answer from a new capture.

### Stop (link 7)

**D15. Four phrase lists**, in `core/classify/stop.go`. Each is closed, swept over
`AllLanguages()`, and negation-aware like D1:

| Function | es | en |
|---|---|---|
| `StopsReminding` | "no me lo recuerdes más", "no me recuerdes más", "dejá de recordarme", "no me avises más" | "stop reminding me", "don't remind me anymore" |
| `SaysDone` | "listo", "ya lo hice", "ya está", "hecho" | "done", "I did it" |
| `SaysKeep` | "seguí", "seguilo", "sí, seguí", "que siga" | "keep going", "keep it" |
| `SaysDelete` | "borralo", "borrá el recordatorio", "eliminalo" | "delete it", "remove it" |

**A stop wins over a done**: "listo, no me lo recuerdes más" is a stop.

**D16. Which reminder, deterministically.** The candidates are units with `remind_me = 1` that
have an armed trigger, or one delivered within `stop_reply_window_minutes` (named constant, 120,
§13). The binding tries, in order:

1. **Named.** The message's content words, with the phrase words and stopwords removed, overlap
   the content words of exactly one candidate.
2. **Most recent delivery.** No content words are left, and exactly one candidate was the most
   recent delivery within the window.
3. **Only candidate.** For a stop only: exactly one candidate exists.

If none binds and there are several candidates, a `which_reminder` question lists them ("¿Cuál?
1) tomar pastillas 2) regar las plantas"). Within D14's window it is answered by a number or by
rule 1 against the listed options. With no candidates, the message is captured as usual.

A done phrase binds only through rules 1 and 2, so a bare "listo" with nothing recent is
chitchat.

**D16b. What a bound phrase does** (client, NQ1). A **repeating** reminder is a unit with an
armed recurring asked trigger (D10, including the daily one D14 creates).

| Phrase | Reminder | Effect | Reply |
|---|---|---|---|
| stop | any | stops it | "Listo, no te lo recuerdo más: <content>." |
| done | one-shot | resolves the delivered trigger `engaged` | "Hecho." |
| done | repeating, not marked | opens `keep_or_stop {unit_id}`, already asked | "¿Querés que te lo siga recordando o borramos el recordatorio?" |
| done | repeating, marked | resolves the delivered trigger `engaged` and keeps recurring | "Hecho por hoy." |
| keep (next message, in the window) | the question's unit | sets `units.done_means_today = 1` (M3) and resolves the question `kept` | "Bien, te lo sigo recordando." |
| delete (next message, in the window) | the question's unit | stops it and resolves the question `deleted` | as stop |

Any other next message is a normal capture, and the question expires, so the next bare done
asks again. Nooma asks only until the user answers. A reminder made daily by a "No" follows the
same table, with no special case.

Stopping writes `reminder.stopped {unit_id, dismissed: [ids], previous: {remind_me: 1}}` first
(ADR-0016). Its armed triggers then go to `dismissed`, and `remind_me` to 0. Setting the mark
writes `reminder.kept {unit_id, previous: {done_means_today: 0}}` first.

Phrases are checked after check-in resolution and before the forks. A message the table consumed,
when it is classified `chitchat`, gets the table's reply instead of a conversation.

**Why the mark lives on the unit** (`units.done_means_today INTEGER NOT NULL DEFAULT 0`,
migration M3, in link 7): a recurring trigger row is replaced on every occurrence (D11). A flag on
the row would have to be copied forward forever, and would be lost by a correction's re-plan.

### Docs

**D17. Doc 02 changes per link**:

| Link | Sections |
|---|---|
| 1 | §5 step 1 (D1), §7 "Lead time" and "Delivery" (D3, D4), §13 |
| 2 | §5 step 5 (what an asked capture arms, the reply), §11 |
| 3 | §5 step 4 ("a `due_at` correction moves no trigger" now applies to a unit that did not ask) |
| 5 | §7 "Recurrence" (an anchor's time, re-arm on fire made true, asked recurring) |
| 6 | §5, the ask-back |
| 7 | §5 and §11, the stop and the keep-or-delete question |

Doc 03 changes with links 2, 6a and 7. Doc 06 §4 gains I36 (link 2, with clauses added in 3 and 5)
and I38 (links 6b, 7a and 7b). Doc 07 changes with links 2, 4, 5, 6b, 7a and 7b.

## 3. Invariants

- **I36:** an asked message arms its reminder before its time and the reply says when.
  - Capture: `TestI36_AnAskedReminderIsArmedBeforeItsTime`, `TestI36_ADayAloneRemindsThatDayAtNine`,
    `TestI36_AnAskedEventGetsTheAskedLeadToo`, `TestI36_APastLeadRemindsAtOnce`,
    `TestI36_TheWordsDecideNotTheModel`, `TestI36_ANamedTimeRingsThroughTheNight`,
    `TestI36_TheReplyNamesTheAskedReminder`.
  - Follow: `TestI36_AnAskedReminderFollowsItsInstant`, `TestI36_ALeadChangeMovesAskedReminders`.
  - Recurring: `TestI36_AnAskedRecurrenceIsARecurringTask`, `TestI36_ARecurringTriggerReArmsWhenItFires`.
- **I38:** a reminder question or stop is bound without the model.
  - Ask-back: `TestI38_NoDayNoTimeAsksBack`, `TestI38_ATimeAnswerSetsTheReminder`,
    `TestI38_NoMakesItDaily`, `TestI38_AnUnrelatedMessageIsNotAnAnswer`.
  - Stop: `TestI38_StopStopsTheNamedReminder`, `TestI38_ListoNoMeLoRecuerdesMasIsAStop`,
    `TestI38_AmbiguousStopAsksWhich`.
  - Done: `TestI38_ABareListoAsksKeepOrDeleteOnce`, `TestI38_SeguiMakesListoDoneForToday`,
    `TestI38_BorraloStops`, `TestI38_TheDailyFromNoFollowsTheSameRule`,
    `TestI38_ABareListoWithNothingRecentStopsNothing`.

I37 belongs to #339. If #339 renumbers, these follow the next free numbers.

## 4. Tests

| Level | What |
|---|---|
| L1 | Core: every phrase list (negation, accents, sweeps), `leadSet`/`Arm` per R2 row, the anchor time, verdicts, exact sentences. Brain over memrepo: capture, refusal, ask-back, D16b row by row, correction, re-arm, re-arm on fire |
| L2 | I36, I38, with I29 and I33-I35 (and I37 once #339 ships) kept green |
| L3 | The three migrations (M2 keeps every row and index), round trips, goldens |
| Corpus, L4 | `classify-task-remind-me-coffee.json`, `classify-task-remind-me-daily-pills.json`; the observed message through the binary |

## 5. Chain (stacked to main)

| Link | Branch | Content | Impl+docs | Usable after it |
|---|---|---|---|---|
| 0 | `plan/remind-me-reminders` | This plan, ADR-0031 Proposed | docs | — |
| 1 | `feat/remind-me-decision` | D1-D4 (core), D6; doc 02 §5.1, §7, §13 | ~240 | — |
| 2 | `feat/remind-me-one-shot` | D5, D7, D8 (one-shot), D4 (scan); I36 capture; corpus, L4; docs 02, 03, 06, 07 | ~350 | **First usable:** "recordame X a las H", "…mañana", an asked event |
| 3 | `feat/remind-me-follows` | D9, `CatchUp` lead set; I36 follow | ~170 | corrections and lead changes move it |
| 4 | `feat/remind-me-preference-ui` | the lead on `/ui/preferences`; doc 07 | ~110 | the lead is editable |
| 5 | `feat/remind-me-recurring` | D10, D11, D8 (recurring words); I36 recurring; docs 02, 07 | ~330 | "todos los días a las 9pm" |
| 6a | `feat/question-store-widens` | D12: migration M2, port, memrepo, goldens; doc 03 | ~180 | — |
| 6b | `feat/remind-me-ask-back` | D13, D14; I38 ask-back; docs 02, 07 | ~300 | "recordame X" asks back; "No" makes it daily |
| 7a | `feat/remind-me-stop` | D15 (stop), D16, D16b stop row; I38 stop; docs 02, 07 | ~280 | "no me lo recuerdes más" stops it; ambiguity is asked |
| 7b | `feat/remind-me-done` | D15 (done, keep, delete), D16b other rows, migration M3; I38 done; docs 02, 03, 07 | ~260 | a bare "listo" asks once; "seguí" makes it done-for-today |

Order: 1 → 2 → 3 → 4. Link 5 needs only link 2. 6a is independent. 6b needs 5 and 6a. 7a needs
5 and 6a, and 7b needs 7a. With #339: whichever lands second between link 2 and #339's link 2
(`ReminderVerdict`) moves D4's branch into it. The migration that merges second takes the next
number.
