# Design — remind-me-reminders, links 5-8: question store, stop, recurring, ask-back

This continues `design.md` (D1-D9). The rules are ADR-0031, with detection by ADR-0033
(Accepted): the classify fields of D1 replace every phrase list below. P1-P5 are the PM's decisions in
`proposal.md`, with P6 and P7 added in round 2. Lines are at `dd1ba9a`.

## 1. Ground truth

| Claim | Where |
|---|---|
| A fired recurring trigger is never re-armed; `NextOccurrence` has no caller in `brain` | `rg NextOccurrence internal/brain` → none |
| `NextOccurrence` reads its zone from its argument, and `fire_at` is stored in UTC | `recurrence.go:88-89`, `sqlite/triggerrepo.go:427` |
| An unasked recurring row fires 7 days before its occurrence, clamped to now (`arm.go:190`), so for a daily rule `NextOccurrence(fire_at)` is the **same** occurrence | `arm.go:147-171` |
| The due loop expires a stale trigger with no follow-up (`check.go:121-152`) | `brain/check.go` |
| `Follow`'s recurring branch re-anchors with `recurringTrigger(eventAt, rule, …)`: lead 7 days, anchor hour 12 | `follow.go:52-54`, `correction_reminder.go:129` |
| `TriggerMove` can rewrite `RecurrenceAnchor` | `ports/triggerrepo.go:204` |
| `TriggerStatusDismissed` exists, but no port method writes it | `ports/triggerrepo.go:26-33` |
| `Delivered` breaks a `surfaced_at` tie by uuid, and a digest surfaces many rows with one `now` | `sqlite/triggerrepo.go:354`, `digest.go:182` |
| `resolveCheckIn` engages `open[0]` whatever it is | `checkin.go:48` |
| `armedPayload` writes `event_at` only for one-shot plans, so a corrected recurring row loses any payload instant | `capture.go:976-986` |
| `DueTrigger` has no `created_at`, and `Reschedule` keeps the original | `ports/triggerrepo.go` |
| `pending_questions.relation_id` is `NOT NULL`, the resolutions are confirmed, rejected and expired, and both reads inner-join `relations` | migration 0004; `ports/pendingquestionrepo.go:34-49`; the join in `internal/store/sqlite/pendingquestionrepo.go` |

## 2. Decisions

### Link 5: the question store (D10-D13)

**D10. Migration M2 rebuilds `pending_questions`.** It is created anew, copied and renamed, with
the indexes recreated:

- `relation_id` becomes nullable;
- `unit_id` and `options` (JSON) are added, with no FK (ADR-0027's reasons).

The `kind` vocabulary gains `remind_when`, `which_reminder` and `undo`. The resolution vocabulary
gains `answered`, `declined` and `restored`. `options` holds `{unit_ids: [...]}` for
`which_reminder`, and `{unit_id, snapshot}` for
`undo`. The M2 column comments, the vocabulary DDL test
(`pending_question_vocabulary_ddl_test.go`) and doc 03 change together.

**D11. Port.**

- `Ask(ctx, ReminderQuestion, at)` creates a reminder question already asked. It expires the open
  non-undo reminder question it replaces, and returns it so the caller logs the expiry row.
- `OpenReminderQuestion(ctx, now)` is a **pure read**. It returns the open non-undo question
  whose window, judged from `now`, has not passed. `OpenUndos(ctx, now)` returns the undos, most
  recent first.
- `Expire(ctx, id, at)`: expiry is explicit, logged, and done only by callers that may write
  (capture, the due scan). A dry run and Today never write.
- `Resolve(ctx, id, resolution, at)`: compare-and-set on `resolved_at IS NULL`. It returns
  `ErrQuestionClosed` when another path closed the question first, and the caller then does
  nothing (S1).
- `LatestOpenAskedAt(ctx)` returns the newest `asked_at` across every kind, relations included.

The relation reads keep `kind = 'relation'` and are unchanged, and the m3e tests stay green.
**At most one non-undo reminder question is open at a time.** An `undo {unit_id, snapshot}` has
its own slot (window `stop_undo_window_minutes`, 10): a new `Ask` never expires it. Two stops
inside 10 minutes leave two undos. A `keep` restores the most recent, and a second `keep` the
earlier one.

**D12. Windows and expiry, for every reminder kind.**

| Kind | Window (§13) |
|---|---|
| `remind_when` | `ask_back_window_minutes` (60) |
| `which_reminder` | `which_reminder_window_minutes` (60) |
| `undo` | `stop_undo_window_minutes` (10): time only, exempt from (b), (c) and (d) |

All four constants, and `stop_reply_window_minutes` (120), land in link 5 with the sweep that
checks them.

A reminder question expires when any of these happens:

- **(a) Window:** its window passes. The due scan writes `check.question.expired` through
  `OpenReminderQuestion`, and never through the digest's relation sweep.
- **(b) Not the answer:** a message is not its answer (D24). Capture writes
  `capture.question.expired`.
- **(c) Consumed:** a message is consumed by any other answer, a reminder intent or a check-in. The older
  open reminder question expires in the same step (round 2, item 7).
- **(d) Newer delivery:** a reminder or check-in delivered after a `which_reminder` was asked
  expires it, so a late answer never stops the wrong reminder. It does **not** expire a
  `remind_when` inside its window. Instead, D24 step 1 lets a check-in or nudge delivered after
  it own the next message, which falls through to the resolvers. A check-in's "no" therefore
  never makes a daily reminder, and the user can still answer the ask-back afterwards.

**D13. Actions** (§11 entries, `AllDecisionActions`, `ui/activitylabel.go`, I12):

- `capture.question.asked`
- `capture.question.answered`
- `capture.question.expired`
- `check.question.expired`
- `capture.reminder.unbound` (a stop that found nothing, Q3)
- `reminder.stopped` and `reminder.restored` (an undo), with I12 and the `AllDecisionActions` sweep

### Link 6: the stop (D20-D24) — lands before anything repeats (P1)

**D20. Durable stop (P5), migration M3.** `units.reminder_stopped_at TEXT` (nullable), a
tombstone with its instant. `remind_me` keeps its history.

**Nothing of a stopped unit is armed or sent, by structure:**

- **Arming:** `TriggerRepo.Create` and `Reschedule` are guarded in SQL (`… WHERE NOT EXISTS
  (SELECT 1 FROM units WHERE id = ? AND reminder_stopped_at IS NOT NULL)`) and return
  `ErrUnitStopped`. Every caller treats it as skip-and-record (`*.refused`, reason `stopped`) and
  never aborts its pass: the capture correction fork, `Follow`, the preference re-arm, ADR-0032's
  re-plan and `CatchUp`, and D33.
- **Sending:** today `reminderGate.judge` reads the unit for a row that names its event in the
  payload and for a legacy row that names none (`reminder_gate.go:80`), but not for recurring or
  lead-only rows. Link 6 adds an **unconditional** unit read for the stop check. `prospection.Reminder` gains `Stopped`, and `ReminderVerdict`
  answers `Stale`, `LapseStopped`, before any other rule. A unit that cannot be read (any error
  but `ErrUnitNotFound`) **holds** the row: `Defer`, fail closed. Push, digest carry and Today all
  pass through `reminderGate.judge`, so a crash between the tombstone and the dismissals, or a
  fired row that was never delivered, sends nothing.
- **The digest path fails closed too:** `splitPast` (`reminder_gate.go:110`) and `replan`
  (`reminder_step.go:41`) gain an explicit stopped-or-held branch. A `Defer` never lands in
  `kept`, and a `LapseStopped` row is lapsed rather than left.
- **P6:** a new explicit ask is a new capture and a new unit, armed normally. With `asked`, the
  free-capture correction fork is skipped even when the model says `correction`, so a re-ask
  never lands on the stopped unit. A correction that reaches a stopped unit **applies its field
  edit** and refuses only the re-arming (`correction.reminder.refused`, reason `stopped`). The
  reply says so: "Corregido. El recordatorio sigue detenido; pedímelo de nuevo con
  «recordame…»". Nothing but the undo clears the tombstone.

**D21. Stop intent** (ADR-0033 points 2-3). A stop is `reminder_intent: stop`. The prompt states
that "listo, no me lo recuerdes más" is a stop, and the phrasing set
measures it. A bound stop is its own confirmation.

**D22. Candidates and binding by position** (PM Q5, Q6).

- **Stop candidates** (`brain.reminderCandidates`, built before the classify call), in this order
  (PM):
  1. the unit of the last delivery within `stop_reply_window_minutes` (`TriggerRepo.
     LastDeliveries`), asked or not. Check-in and nudge deliveries count, so "no me lo recuerdes
     más" said to a task check-in binds that unit (Q6);
  2. asked units with a trigger delivered within `stop_reply_window_minutes` (120) or armed;
  3. the rest by next fire time: armed recurring reminders.

  They are deduplicated by unit, so a tie inside one unit is one candidate, and cut to 5. Each is
  rendered by `RenderReminderList` as "<n>) <title or content> — <date>". The prompt says to
  answer `unclear` when the described reminder is not listed. **Known limit:** the 6th candidate
  and later cannot be stopped by chat until m4f's UI.
- **Prompt:** the classify prompt carries the list whenever it is non-empty.
- **Binding:** the model answers `reminder_target`. Code binds only on a position inside the list
  it sent. `unclear`, out of range or absent asks `which_reminder {intent, unit_ids}` with the
  same list. There is no word matching and no blind single-candidate bind.
- **A stop with a request** ("no me lo recuerdes más, y recordame llamar a Juan mañana"): the
  stop is applied and the capture continues, as `resolveCheckIn` lets it (`checkin.go:15-18`).
  The reply names both.
- **Unbound (Q3, round 2 C1):** a stop that binds nothing is consumed **only when no check-in,
  nudge or relation question is open**. It then replies "No encontré qué recordatorio cortar" and
  stores nothing. Otherwise it falls through to the resolvers (`checkin.go:23-55`).

**D23. Stopping, and its undo** (PM Q1, Q2).

1. `reminder.stopped {unit_id, rows: [{id, status, fire_at, payload, rule, anchor,
   interrupt_level}], previous: {reminder_stopped_at: null}}` is recorded first (ADR-0016).
2. The unit's `reminder_stopped_at` is set.
3. Armed rows are `Dismiss`ed. `ports.ErrTriggerStatusConflict` (the row fired meanwhile) is tolerated, and
   the row goes to step 4 (S2). `Dismiss` is new: port, sqlite, memrepo, contract, and the I03
   sweep, which it passes with no `Delete` prefix.
4. Fired, undelivered rows are `Lapse`d (reason `stopped`). Stopping a one-shot event retires all
   of its leads.
5. An `undo {unit_id, snapshot}` question is asked with the reply "Listo, no te lo recuerdo más:
   <title> — <date>. (Si fue un error, decime «no, seguí».)".

**Undo:**

- **Trigger:** only `reminder_intent: keep` read while an undo is open (D24 step 1), and it
  applies to the most recent undo. A bare "sí", "ok" or "dale" is not keep, and a stray "seguí"
  elsewhere restores nothing.
- **Core decides:** `prospection.PlanUndo(snapshot, unit, now, zone)` returns a fully decided
  plan.
  - A snapshot row still ahead by its `about` (payload `event_at` or `occurs_at`) is restored by
    id: a dismissed row through `Restore(id)` (dismissed→armed), a lapsed one recreated as
    `<id>:restored`.
  - A row that is still `armed`, because a crash fell between the tombstone and the dismissals,
    is kept as is when ahead.
  - A row whose `about` passed is dropped `passed_during_stop` and named in the reply ("te lo
    sigo recordando; el aviso de las 21:00 ya pasó"). A recurring root whose row was dropped
    gets `Successor(row, now, zone)` (round 2, C2).
  - **If the unit's instant or anchor changed since the stop** (a correction, which P6 allows),
    the plan ignores the stale rows and plans afresh from the unit as it is now, with no at-once
    arming. The reply names the new time.
- **The port writes, in one transaction:** `ports.StopStore.UndoStop(plan)` runs as one SQLite
  transaction across `units`, `triggers` and `decision_log`.
  - It first checks that the tombstone is still set and equals the snapshot's `stopped_at`.
  - It then resolves the undo question `restored` (compare-and-set), records `reminder.restored
    {unit_id, restored, dropped, rearmed}`, clears the tombstone, and writes the rows.
  - `ErrTriggerExists` on a recreate or a successor counts as done, not a rollback. Any other
    failure rolls back everything.
  - sqlite shares the executor-agnostic decision-log insert helper. memrepo moves units,
    triggers and the log under one lock (`memrepo.NewStore`). `NewTriggers()` keeps its 66
    callers as a wrapper over a private store, and tests of the guarded `Create` use
    `NewStore`.
  - Doc 02 §5 step 4's "share no transaction" (line 767) is amended, and doc 06 §1 notes the
    first cross-table port.
- **One author:** the repair pass (D34) skips a unit with an open undo.

**D24. Precedence — one total order.** The single classify call runs first, with the open
reminder question in its prompt (D1). Then the code reads the fields in this order:

0. **The intent step runs above the correction fork** (`capture.go:283-290`), and
   `BuildCorrectionPrompt` carries the fields. An explicit referent (UI or API `unit_id`; chat
   has none today) binds a stop to that unit directly, skipping D22.
1. **The open question that owns the message.** Q is the most recently asked open question of
   any kind (`LatestOpenAskedAt`). Ties go `which_reminder` > `undo` > `remind_when` > `relation`.
   - If Q is a reminder kind inside its window and the reading fits Q, the message is consumed.
     The fitting readings are: `question_answer` `when` or `no` for `remind_when` (D42);
     `question_answer` `choice` within the list for `which_reminder`; and `reminder_intent: keep`
     for `undo` (never a bare "sí").
   - A check-in or nudge delivered after Q was asked outranks Q: the message goes to step 3's
     resolvers (D12 (d)).
   - If Q is a reminder kind and the reading does not fit, Q expires (D12) and the steps go on.
   - If Q is a relation question, the steps go on: `relation_outcome` binds it as in m3e.
2. **`reminder_intent` `stop`** binds through D22. If it binds or asks,
   the message is consumed; if it finds nothing, see D22's unbound rule. **None of the three check-in resolvers runs** (task, state and
   relation), so a stop that also reads `relation_outcome: rejected` deletes nothing (PM Q9).
   A `keep` or a `question_answer` with no matching open question does nothing, and is never
   coerced into `nudge_outcome` (PM Q10).
3. **Capture.** With done handling deferred, `resolveCheckIn` keeps M3's behaviour exactly. An
   asked reminder that was delivered is a check-in like any other, and "listo" resolves it
   `engaged`. The round-2 filter is dropped, because only the phrase table needed it. "A
   check-in is open" in D22's unbound rule is M3's `Delivered()` set.

A stop that binds is consumed as a stop, and when the same message also reads `remind_request`
the capture continues after it (D22).

### Link 7: recurring (D30-D35)

**D30. Rows.** **Every** recurring plan writes `occurs_at`, the UTC instant of its occurrence
computed in the clock's zone: capture, correction (`armedPayload` and `moveReminder`, round 2,
item 12) and D33. A legacy row gets it on first touch (D32), in Go and in the clock's zone; SQL
cannot, because it knows no zone, so there is no backfill migration (round 3, item 5).

| Row | `fire_at` | `lead_days` | `asked` | `night_exempt` |
|---|---|---|---|---|
| Asked, named time | `occurs_at` | 0 | ✓ | ✓ |
| Asked, no time ("No" daily, D43) | `occurs_at` | 0 | ✓ | — |
| Unasked | `max(LeadTime(occ), previous occurrence)` | 7 | — | — |

**How the gate judges a recurring row.** In `ReminderVerdict` (#340), `about` is `occurs_at` when
present, and `NextOccurrence(fire_at)` (D32) otherwise.

- **Unasked row:** day-granular, as #340 has it. It is passed from the next local midnight after
  `occurs_at`, and keeps `Waiting` and its digest routing.
- **Asked row:** never judged `passed`. Main's rule would pass it from the next local midnight
  after `occurs_at` (`reminder.go:78-85`), so a late-evening occurrence delivered after midnight
  would be dropped. It is judged by lateness only, and its successor (D33) carries the next
  occurrence.
  - With `night_exempt` (a named time): no quiet-hours `Defer`, and stale `TriggerStalenessHours`
    after `fire_at`. So "todos los días a las 06:30" rings at 06:30, and is dropped after 12:30 if
    the vault was off.
  - Without it (the "No" daily): `TriggerVerdict` as it is, deferred through the night and overdue
    from quiet end.

**Asked recurring rows are pushed:** `RouteTrigger(recurring, asked, i)` returns `RoutePush` for
an asked row (`delivery.go:97` today routes every recurring row by interrupt level), and
`reminderGate.judge` sets `Waiting` only for unasked recurring rows (#343 sets it for every fired
recurring row).

**D31. An asked repetition is a recurring `task`.** `ToUnit` builds a `task` when `asked` and a
`recurrence_rule` are both present. `Anchor` gains optional `hour` and `minute` in its JSON, set
from the instant's local time (or from `DateOnlyAt` for a day-only instant). `NextOccurrence`
lands on the anchor's time when it has one, and on `RecurrenceAnchorHour` otherwise.

**D32. The occurrence of a row.** It is `occurs_at` when present. A legacy row has none, and
`prospection.LegacyOccurrence(row, zone)` decides it. A yearly or monthly row whose local
`fire_at` plus 7 **calendar** days lands on the anchor's day at `RecurrenceAnchorHour` is
provably unclamped, and that instant is its occurrence. Every other legacy row, clamped or moved
or daily or weekly, takes `NextOccurrence(rule, anchor, fire_at.In(zone))`. The occurrence is
written back as `occurs_at` when the row is next touched. `created_at` is not read, since
`Reschedule` keeps it. `Successor` refuses a nil anchor (`ErrNoAnchor`): the row is recorded and
left alone.

**D33. The successor.** `prospection.Successor(row, now, zone)` is pure. Its next occurrence is
`NextOccurrence(rule, anchor, max(occ, now).In(zone))`, built on the wall clock, so it follows DST
and the user's local date (21:00 at -03:00 is 00:00 UTC on the next day). It is armed with D30's
`fire_at`, floored at the start of the local day after the predecessor's `fire_at`, so a clamped
daily never fires twice on its first day (round 3, item 8). As a result:

- an occurrence is never reminded twice;
- no successor fires before its predecessor's occurrence, so there is no burst;
- after an outage, the successor is the first occurrence after now.

**D34. When it runs, crash-safe.**

- **Create before fire:** before the scan fires or expires a recurring row, it creates the
  successor with id `<root>:<next occurs_at UTC>`. The root is the first id with any `:<…>`
  suffix stripped, so ids never grow. `ErrTriggerExists` means it was already done, and
  `ErrUnitStopped` means nothing is armed. Expire covers a stale row (>6 h) and a `passed` one.
- **Repair** (each pass): `TriggerRepo.RecurringRoots(ctx)` (new) returns each root, by id prefix,
  with its latest row's status. A root whose latest row is `fired` or `expired`, with no armed
  successor, its unit not stopped and no `undo` open for it, gets one. So does a root whose latest
  row is `dismissed` while its unit is not stopped and has no open undo. This heals vaults that fired before this link.
- The re-arm is recorded as `check.trigger.rearmed`, counted in `CheckReport.TriggersReArmed`
  (I12, `cmd/nooma/check.go`). **DryRun** computes successors and repairs and writes neither.

**D35. Corrections and rendering.**

- **Correction:** `Follow`'s recurring branch takes `asked` and the new local time. For an asked
  row it rewrites `RecurrenceAnchor` (with `hour` and `minute`) through `TriggerMove`, keeps
  `asked`, and recomputes `occurs_at`: "a las 22" moves 21:00 to 22:00.
- **Preference re-arm:** it keeps skipping recurring rows (`preferences_rearm.go:68-74`); an asked
  recurring row has no lead to re-arm.
- **Rendering:** ADR-0032's renderer (link 6 of `digest-stale-reminders`) prints `About`, which is now `occurs_at`, so a recurring task
  shows that day's occurrence and never its first date.
- **Archive:** archiving a unit does not touch its reminders. Only the user stops them (client).

### Link 8: ask-back (D40-D43)

**D40. Ask.** An asked unit with no instant is stored. `remind_when {unit_id}` is created already
asked (`capture.question.asked`), and the reply is "¿Algún día y hora en particular?".

**D41. The window.** `ask_back_window_minutes` is 60 (§13, link 5). Only the next message inside
the window can answer (D24).

**D42. Answers.**

- Each consumed answer's reply names what it did ("Listo: te recuerdo X todos los días a las
  09:00"), with the real ring time when quiet hours move it. An answer has no undo of its own
  (PM): a misread is fixed with a normal stop or correction.
- **`question_answer: no`** ("no", "no hace falta", "nope"): the unit becomes daily (D43), the question is resolved `declined`, and the reply
  states the actual hour.
- **`question_answer: when`** ("mañana a las 10", "en 1 semana"): the same classification
  resolved the instant, because the prompt showed the question's unit. The correction path's
  patch writer (link 3) writes the unit's instant field (`event_at` for an `event`, `due_at`
  otherwise) from whichever field the model filled, recorded as `correction.applied` with its
  pre-image. D9's first-arming rule allows at once.
  - With no resolvable instant: the reply is "¿Cuándo?", nothing is written, and the question
    stays open for the rest of its window.
  - Otherwise: the question is resolved `answered`, with R5's reply.
- **Neither:** the question expires and the message is a normal capture. An expired
  `remind_when` leaves the unit stored with `remind_me = 1` and no reminder. A later correction
  that gives it a date arms one.

**D43. The "No" daily.** The anchor time is the `DateOnlyAt` in force at that moment, **copied
into the anchor**, so a later change of the preference does not move it. The first occurrence is
today when that time is still ahead, otherwise tomorrow. The reply reads "Te aviso todos los días
a las <anchor time>".

### Deferred: done handling (old link 9, D50-D52)

Client, 2026-10-14. This drops the bare "listo" and "ya lo hice" on a reminder, the
`keep_or_stop` question, its done-for-today mark (and its migration), and the done candidate set.
A daily rings every day until stopped. It is a follow-up, to be planned when the client asks.

## 3. Invariant I38 and doc changes

I38: a reminder question and a stop are bound without the model, the stop is durable, and
nothing repeats unstoppably. Doc 02 changes: §5 (links 6 and 8), §7 "Recurrence" (link 7: the
re-arm is made true, an anchor's time, unasked rows changed per P2), and §11 (each link's
actions). Doc 03 changes with links 5, 6 and 7, and doc 07 with each link. The §13 constants
are `ask_back_window_minutes`, `which_reminder_window_minutes`, `stop_reply_window_minutes` and
`stop_undo_window_minutes`, all in link 5. New lapse reason: `stopped` (link 6).
