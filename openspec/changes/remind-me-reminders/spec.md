# Spec — remind-me-reminders

What MUST be true after this change, in testable form; how is `design.md`'s job. Rules:
ADR-0031 (Accepted by the client, 2026-10-14), its detection superseded by ADR-0033
(Accepted, 2026-10-14), over ADR-0029, ADR-0030 and ADR-0032. Client decisions: `proposal.md`.

**Terms.**

- **Asked**: the classification of the message says `remind_request` (R1).
- **Its instant**: an `event`'s `event_at`. Any other kind uses `due_at`, falling back to the
  other field.
- **Day only**: the model wrote the instant as a bare date (`YYYY-MM-DD`). This is the shape of
  the value, not a judgment, and it is never inferred from a midnight.
- **Named time**: an instant written with a time. Midnight counts.
- **Asked reminder**: a trigger armed because the message asked.
- **Remind-me lead**: the user's preference for how long before a named time a one-shot asked
  reminder fires.
- **Repeating reminder**: an asked recurring reminder (R7), or the daily one a "No" makes (R8).
- **Stopped**: the unit carries a durable stop (R9). No path arms or sends anything of it again;
  only a new explicit ask creates a new reminder (P6).

| ID | Requirement | Invariant |
|----|-------------|-----------|
| R1 | The model reads the request; the code decides what it arms | I36 |
| R2 | One-shot: named time, day only, event, past lead | I36 |
| R3 | The remind-me lead is a vault preference, default 15 minutes | I36 |
| R4 | Pushed and never late; a named time, or an at-once arming, rings through quiet hours | I34, I36, I37 |
| R5 | The reply says exactly when, or why not | I36 |
| R6 | A correction and a lead change move asked reminders | I29, I35, I36 |
| R7 | Explicit recurring reminders are recurring tasks; every recurring trigger re-arms | I36 |
| R8 | No day and no time: Nooma asks back; "No" makes it daily | I38 |
| R9 | A reminder stops only by chat; ambiguity is asked, never guessed | I38 |
| R10 | What changes for reminders nobody asked for | I33, I34 |
| R11 | Docs change with the code | non-neg. 1 |
| R12 | A reminder says a short, date-free title, not the dated content | I37 |

### R1 — The model reads, the code decides (ADR-0033)

**MUST**: the classify call returns the reminder fields: `remind_request` (a bool, or null; any
other value degrades), `reminder_intent`, `question_answer` (with `choice_number`) and
`reminder_target` (a position or `unclear`).

- Each is optional and degrades independently (I14).
- A degraded or absent reminder field is `none`: no reminder action, the plain capture path, and
  the degradation recorded.
- Core holds no phrase list. Day only stays the decoder's flag (the shape of the value).

**MUST**: detection quality is measured on a recorded phrasing set (ADR-0033 point 5) holding
every phrasing below. CI replays it, and `nooma doctor` reports the live rate.

| Message | Expected reading |
|---|---|
| "recordame comprar café mañana a las 10", "Remind me to call Ana at 5pm", "si no, avisame", "si no puedo, avisame", "no te olvides de recordarme X", "recordámelo", "haceme acordar", "acordate de avisarme", "poneme un recordatorio", "que no se me olvide" | `remind_request` |
| "comprar café mañana a las 10"; "recordame qué hablamos con Juan" (a recall); "my dentist sent a reminder to confirm"; "recordé que tenía que comprar café"; "Ana me dijo 'avisame cuando llegues'" | no request |
| "no me lo recuerdes más", "listo, no me lo recuerdes más", "don’t / don't / dont remind me anymore" | `reminder_intent: stop` |
| "listo", "ya lo hice", "listo, nos vemos mañana" | `none` (done handling deferred; M3 check-ins answer them) |

### R2 — One-shot

**MUST**: an asked unit of any kind that persists a unit, not stopped, with its instant ahead,
arms the following:

| Case | Trigger |
|---|---|
| Named time | the instant minus the remind-me lead |
| Day only | that day at the date-only time (default 09:00) |
| `event` | its ADR-0029 leads plus the asked one, merged when they land on one instant |
| Late (PM) | at once, when the asked time is behind and the instant ahead; also day only, said that day after the date-only time |

Midnight said aloud ("a medianoche") is a named time. A bare date on a day whose midnight does
not exist (DST) is still day only.

Scenarios (zone -03:00, captured 2026-10-14 20:00): "recordame comprar café mañana a las 10" →
`task`, 10-15 09:45; "…pagar el gas mañana" → 10-15 09:00; "…el dentista el viernes a las 10" →
`event`, 24 h, 2 h and 15 min before; "…a las 20:10 sacar la ropa" → at once.

### R3 — The lead is the user's

**MUST**: one nullable `config` column holds the lead in whole minutes. `NULL`, a missing row or
an invalid value resolve to 15. Valid: 1 minute to 30 days. Capture, correction and re-arm read
it at arm time. `/ui/preferences` edits it. An invalid value is refused with its own message and
nothing written. A change is recorded in `config.updated`.

### R4 — Delivery

**MUST**: an asked reminder is pushed whatever its `interrupt_level`, never digested. A one-shot
one is never sent after its instant. An asked day-only reminder stays deliverable through its
whole day.

**MUST**: neither of these is moved or held by quiet hours, and lateness is measured from its
own instant:

- a reminder at a **named time**, recurring included;
- an asked reminder armed **at once**.

A day-only one keeps ADR-0030's night rules.

A day-only or "hoy" request reminds at once unless **less than** one check interval (5 minutes)
remains in the local day, when it is refused `too_late`. Scenarios: "todos los días a las 06:30",
quiet end 08:00 → 06:30; "a las 6:30" at 06:20 → at once; "hoy" at 23:50 or 23:55 → at once; at
23:56 → refused.

### R5 — The reply

**MUST**: the reply names the instant and every reminder, with the hour actually armed: "Anotado
para el jue 15 oct 10:00. Te aviso 15 minutos antes.", "… Te aviso ese día a las 09:00.",
"Anotado. Te aviso todos los días a las 21:00.".

When the user asked and nothing was armed, a `capture.arm.refused` row records the reason and the
reply says why. Never a bare "Anotado.".

**MUST** (PM Q4): when a stored unit has an instant, the model did not flag `remind_request`, and
nothing was armed, the reply adds one clause: "Anotado. No puse aviso; si querés, decime
«recordame»." A missed request is visible, never silent.

### R6 — Follow

**MUST**: a correction of an asked unit's instant leaves what a fresh capture of the new instant
arms. It goes through `prospection.Follow`, with `correction.reminder.*` rows. This holds for a
recurring one too: its rule, its anchor time and `asked` are kept ("a las 22" moves 21:00 to
22:00). An asked unit that never had a reminder may be armed at once by its first arming, even
through a correction (an explicit amendment of I29). A day-only asked reminder stays day only
when its date is corrected ("recordame el jueves" → "no, el viernes" rings Friday at 09:00, never
at 23:45 on Thursday).

**MUST**: a different lead re-arms armed one-shot asked reminders still ahead (I35). ADR-0032's
re-plan uses capture's lead set. A stopped unit is never re-armed by any of these.

### R7 — Recurring

**MUST**: an asked message with a `recurrence_rule` and an instant creates a **`task`** unit,
whatever the kind. It arms a recurring trigger at the named time on each occurrence (the
date-only time when none was named). It has no lead and is pushed.

**MUST**: every recurring trigger, asked or not, has a successor armed for the next occurrence
after the later of its own occurrence and now, when it fires, when it expires, and after a crash
interrupted either. Each occurrence is computed in the user's zone. One occurrence is never reminded twice, a
successor never fires before its predecessor's occurrence, and a stopped unit gets none.

### R8 — Ask back

**MUST**: an asked unit with no instant is stored. The reply is "¿Algún día y hora en
particular?", and an open question about the unit is recorded.

**MUST**: only the **next** message within the window answers it, and only when the
classification reads `question_answer` for it:

- **`when`** ("mañana a las 10", "en 1 semana"): the instant the classification resolved. It
  writes the unit's instant field, and R2 arms (at once if late). An answer without a resolvable
  instant is answered "¿Cuándo?" and the question stays open.
- **`no`** ("no", "no hace falta"): the unit becomes daily at the date-only time in force
  when answered, starting today if that time is still ahead, else tomorrow. A later change of
  that preference does not move it.

Any other message is a capture of its own, and the question expires. An expired question leaves
the unit stored with no reminder. A consumed answer's reply names what it did ("Listo: te recuerdo
X todos los días a las 09:00", with the real ring time when quiet hours move it). The user fixes
a misread with a normal stop or correction; an answer has no undo of its own (PM).

### R9 — Stopping, chat first

**MUST**: an **explicit stop** (`reminder_intent: stop`, bound) is the user's confirmation
(ADR-0033 point 3):

- it records first, in `reminder.stopped`, a durable stop on the unit and the id and snapshot of
  every row it retires;
- it dismisses the armed rows (a conflict is tolerated) and lapses the fired, undelivered ones;
- nothing of that unit is sendable afterwards, by any path (the delivery gate, the digest,
  corrections, the re-plan, the re-arm, preferences);
- stopping a one-shot event dismisses its remaining leads.

**Undo (PM).** The stop reply ends "(Si fue un error, decime «no, seguí».)".

- **Window:** 10 minutes, time only. The newer-delivery, not-the-answer and consumed expiries do
  not touch it, and it is idempotent by id.
- **Trigger:** only a message the model reads as `keep` while it is open restores. A bare "sí",
  "ok" or "dale" does not, and a stray "seguí" elsewhere restores nothing.
- **Restore:** the undo clears the stop and restores exactly the retired rows, by id, from the
  snapshot.
- **Rows that passed:** a row whose time passed meanwhile is dropped and named ("te lo sigo
  recordando; el aviso de las 21:00 ya pasó"). A repeating reminder whose row was dropped gets
  its next occurrence armed in the same step, so it keeps repeating.
- **One author:** while an undo is open, the repair pass skips that unit.
- **After the window:** only a **new explicit ask** (P6) brings the reminder back, as a new unit.
- **Corrections:** a correction of a stopped unit applies its field edit and refuses only the
  re-arming. The reply says the reminder stays stopped.

**Deferred (client, 2026-10-14): done handling.** There is no `done` reading, no keep-or-delete
question and no done-for-today mark. A daily reminder rings every day until the user stops it.
A bare "listo" keeps M3's behaviour: it answers an open check-in or nudge. A `keep` or a
`question_answer` with no matching open question does nothing, and is never coerced into a
check-in outcome.

**MUST** (PM Q5, Q6): **binding**. The model never names a reminder.

- **Candidates.** Whenever there are stop candidates, the prompt carries a numbered list
  of at most 5, deduplicated by unit, each with title (or content) and date:
  - for a stop, in this order: the unit of the last delivery, asked or not; then asked
    reminders delivered within 120 minutes or armed; then the rest by next fire time (armed
    recurring reminders).

  The prompt says to answer `unclear` when the described reminder is not in the list.
- **Binding.** The model returns `reminder_target`, and the code binds only on a valid position.
  `unclear`, an out-of-range position, or a missing one asks "¿Cuál?" with the same list, keeping
  the intent. There is no word matching and no blind single-candidate bind.
- **Nothing to bind (PM Q3).** A stop with no candidate is consumed only when no check-in, nudge
  or relation question is open. It then replies "No encontré qué recordatorio cortar" and stores
  nothing. Otherwise it falls through to the check-in resolvers.

**MUST**: one fixed precedence:

- the explicit referent, when a surface gives one (the reminder fields are read there too);
- then the newest open question, answered by `question_answer` or `reminder_intent`;
- then `reminder_intent`;
- then the capture.

A message consumed by a reminder intent skips all three check-in resolvers (task, state and
relation). A stop that also reads `relation_outcome: rejected` deletes no relation (PM Q9).

Questions expire when they are read, and a resolve succeeds only on an open question. A message
consumed by any answer, intent or check-in expires every older open reminder question. Every
reminder question has a window. A newer delivery expires an open `which_reminder`, so a late
answer never stops the wrong reminder. It does not expire a `remind_when` within its
window, but a check-in or nudge delivered after it owns the next message, so that message falls
through to the resolvers and never makes a daily reminder.

Reply: "Listo, no te lo recuerdo más: tomar pastillas — 21:00. (Si fue un error, decime «no,
seguí».)".

### R10 — Reminders nobody asked for

- A unit that did not ask arms what it arms today. A timer is unchanged.
- **Changed (PM)**: a recurring reminder nobody asked for (a birthday) is now re-armed after
  each occurrence, as doc 02 §7 already promised. Its timing and interrupt routing stay the same,
  with R7's guarantees: no double fire and no burst.

### R12 — The reminder's words (client, 2026-10-14)

**MUST**: classify returns an optional `title` for every unit an armed kind stores, in the
message's language.

- **Validation:** it is trimmed. A blank title, one with a newline or control character, one over
  60 runes, or one with an ISO date or time is dropped (I14). The unit stores what is kept.
- **Known limit (PM Q8):** relative and weekday words ("mañana", "jueves", "3pm") cannot be
  caught without a word list. A prompt rule and the doctor measure cover them. A date correction
  shows the old title to the model and asks for a fresh one, so "Reunión del jueves" can become
  "Reunión del viernes". When the model keeps the old one, that is the stated limit.
- **Corrections:** a correction that re-classifies writes the new title with its pre-image. A
  content edit that brings no valid title **clears** it, and a date-only correction keeps it.

**MUST**: a push reads "Recordatorio: <title> — <date>". A digest line and Today read "• <title> —
<when>". All three use the content when the unit has no title, and so does the frozen fallback
text. The stop reply and the "¿Cuál?" options use title and date too. Scenario: "Tengo reunión con Juan hoy a las 22:30" →
"Recordatorio: Reunión con Juan — mié 14 oct, 22:30". The phrasing set holds title cases, and
doctor measures how many titles come back date-free and short.

### R11 — Docs

Each link changes, in the same PR, what its code changes: doc 02 §5 steps 1, 4 and 5, §7, §11,
§13; doc 03; doc 06 §4 (I36, I38, and the I29, I34 and I35 amendments); doc 07.
