# Spec — remind-me-reminders

What MUST be true after this change, in testable form; how is `design.md`'s job. Rules:
ADR-0031 (Accepted by the client, 2026-10-14), over ADR-0029, ADR-0030 and ADR-0032. Client
decisions: `proposal.md`.

**Terms.**

- **Asked**: the message the user wrote contains a remind request (R1).
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
| R1 | "Asked" is decided from the user's own words | I36 |
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

### R1 — The user's own words

**MUST**: "asked" is a pure function of the raw text, never of `normalized_content` or a model
field. It is true when a remind phrase from a closed per-language list appears as whole words,
case-, accent- and apostrophe-insensitive (`don’t` = `don't` = `dont`), unless a negation governs
it in its own clause, a stop phrase (R9) is present, or an interrogative follows it (a recall).
Clauses end at punctuation and at "y", "pero", "si", "and", "but", "if".

| Message | Asked? |
|---|---|
| "recordame comprar café mañana a las 10", "Remind me to call Ana at 5pm", "si no, avisame", "si no puedo, avisame", "no te olvides de recordarme X", "recordámelo", "haceme acordar", "acordate de avisarme", "poneme un recordatorio", "que no se me olvide" | yes |
| "comprar café mañana a las 10" (even when the model writes "Recordarme…"); "no me recuerdes más" (a stop); "recordame qué hablamos con Juan" (a recall); "my dentist sent a reminder to confirm" | no |

**Known limit (PM)**: quoted or reported speech ("Ana me dijo 'avisame…'") counts as asked. A
test pins this.

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

Scenarios: "todos los días a las 06:30", quiet end 08:00 → 06:30; "a las 6:30" said at 06:20 →
at once; "hoy" said at 23:55 → at once, before midnight.

### R5 — The reply

**MUST**: the reply names the instant and every reminder, with the hour actually armed: "Anotado
para el jue 15 oct 10:00. Te aviso 15 minutos antes.", "… Te aviso ese día a las 09:00.",
"Anotado. Te aviso todos los días a las 21:00.".

When the user asked and nothing was armed, a `capture.arm.refused` row records the reason and the
reply says why. Never a bare "Anotado.".

### R6 — Follow

**MUST**: a correction of an asked unit's instant leaves what a fresh capture of the new instant
arms. It goes through `prospection.Follow`, with `correction.reminder.*` rows. This holds for a
recurring one too: its rule, its anchor time and `asked` are kept ("a las 22" moves 21:00 to
22:00). An asked unit that never had a reminder may be armed at once by its first arming, even
through a correction (an explicit amendment of I29).

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

**MUST**: only the **next** message within the window answers it, and only as one of these:

- **A time answer**, made only of date and time words, durations included ("en 1 semana"). It
  writes the unit's instant field, and R2 arms (at once if late). An answer without a resolvable
  instant is answered "¿Cuándo?" and the question stays open.
- **A no-answer** ("no", "no hace falta"). The unit becomes daily at the date-only time in force
  when answered, starting today if that time is still ahead, else tomorrow. A later change of
  that preference does not move it.

Any other message is a capture of its own, and the question expires. An expired question leaves
the unit stored with no reminder.

### R9 — Stopping, chat first

**MUST**: an **explicit stop** ("no me lo recuerdes más", "listo, no me lo recuerdes más"):

- records a durable stop on the unit first;
- dismisses the unit's armed triggers and lapses its fired, undelivered ones;
- leaves nothing of that unit sendable afterwards, by any path (the delivery gate, the digest,
  corrections, the re-plan, the re-arm, preferences).

Only a **new explicit ask** (P6) brings the reminder back, as a new unit. A correction of a
stopped unit is refused with the reason `stopped`, and the reply says so.

**MUST**: a **bare done** (the whole message is "listo" / "ya lo hice" / "done"; "listo, nos vemos
mañana" is not one) acts on the asked reminder it binds:

| Reminder | Effect |
|---|---|
| Repeating, not marked | asks once "¿Querés que te lo siga recordando o borramos el recordatorio?" |
| Repeating, marked | done for today; it keeps recurring |
| One-shot | resolves it; an event's remaining leads are dismissed |

The answer to the question:

- "seguí" marks it;
- "borralo" or a bare "no" (PM) stops it;
- anything else leaves it unmarked, and the next bare done asks again.

**MUST**: the binding uses no model, and binds without asking only on **one unambiguous
candidate** (P7). The rules apply in order:

1. The message's content words match exactly one candidate.
2. The single newest delivery of any trigger, with no tie on `surfaced_at`, is a candidate and
   lies inside the window.
3. For a stop only: exactly one candidate exists.

The candidates are:

- **for a stop:** asked units with an armed or recently delivered trigger, units with an armed
  recurring trigger (a birthday, by name), and the units of every row tied for the last delivery
  (up to 5), so a tie always asks;
- **for a done:** asked triggers delivered inside the window.

Otherwise Nooma asks "¿Cuál?" with at most the 5 most recent candidates, keeping the intent. A
done that no rule binds does nothing.

**MUST**: one fixed precedence:

- the explicit referent, when a surface gives one;
- then the newest open question;
- then the phrases;
- then the model.

Check-in resolution never takes a message the phrases consumed. A message consumed by any
answer, phrase or check-in expires every older open reminder question. Every reminder question
has a window, and a newer delivery expires every open reminder question, so a late "no" never
deletes nor makes a daily reminder.

Replies: "Listo, no te lo recuerdo más: tomar pastillas." / "Bien, te lo sigo recordando." /
"Hecho por hoy.".

### R10 — Reminders nobody asked for

- A unit that did not ask arms what it arms today. A timer is unchanged.
- **Changed (PM)**: a recurring reminder nobody asked for (a birthday) is now re-armed after
  each occurrence, as doc 02 §7 already promised. Its timing and interrupt routing stay the same,
  with R7's guarantees: no double fire and no burst.

### R11 — Docs

Each link changes, in the same PR, what its code changes: doc 02 §5 steps 1, 4 and 5, §7, §11,
§13; doc 03; doc 06 §4 (I36, I38, and the I29, I34 and I35 amendments); doc 07.
