# Spec — remind-me-reminders

What MUST be true after this change, in testable form. How is `design.md`'s job. The rules are
ADR-0031 (Proposed), on top of ADR-0029, ADR-0030 and ADR-0032 (Accepted). The client decisions
are in `proposal.md`.

**Terms.**

- **Asked**: the message the user wrote contains a remind request (R1).
- **Its instant**: an `event`'s `event_at`; for any other kind, `due_at`, falling back to the
  other field.
- **Named time**: the user stated a time of day. A day alone is not one.
- **Asked reminder**: a trigger armed because the message asked.
- **Remind-me lead**: the user's preference for how long before a named time a one-shot asked
  reminder fires.
- **Repeating reminder**: an asked recurring reminder (R7) or a daily one made by a "No" (R8).

| ID | Requirement | Invariant |
|----|-------------|-----------|
| R1 | "Asked" is decided from the user's own words | I36 |
| R2 | One-shot: named time, day only, event, past lead | I36 |
| R3 | The remind-me lead is a vault preference, default 15 minutes | I36 |
| R4 | Pushed, never late; a named time rings through quiet hours | I34, I36, I37 |
| R5 | The reply says exactly when, or why not | I36 |
| R6 | A correction and a lead change move asked reminders | I29, I35, I36 |
| R7 | Explicit recurring reminders are recurring tasks at the named time | I36 |
| R8 | No day and no time: ask back; "No" makes a daily 09:00 reminder | I38 |
| R9 | A repeating reminder stops only by chat; ambiguity is asked, never guessed | I38 |
| R10 | What does not change | I33, I34 |
| R11 | Docs change with the code that changes behaviour | non-neg. 1 |

### R1 — The user's own words

**MUST**: "asked" is a pure function of the raw message text, never of `normalized_content` or a
model field. It is true when the text contains a remind phrase from a closed per-language list,
matched as whole words, case- and accent-insensitive, and not negated. Every language in
`classify.AllLanguages()` declares a list.

Scenarios:

- "recordame comprar café mañana a las 10" and "Remind me to call Ana at 5pm" → asked.
- "comprar café mañana a las 10" is not asked, even when the model writes "Recordarme…".
- "no me recuerdes lo del café" and "recordé que…" → not asked.
- A `recall`, `chitchat`, `correction` or `timer` arms nothing new.

### R2 — One-shot

**MUST**: an asked unit of any kind that persists a unit, with its instant ahead, arms:

- **Named time:** one trigger at the instant minus the remind-me lead.
- **Day only:** one trigger that day at the date-only time (default 09:00).
- **`event`:** its ADR-0029 leads plus the asked one, merged when they land on one instant.
- **Late (PM):** when the asked trigger's time is behind and the instant is ahead, it is armed at
  once. A day-only request said that day after 09:00 is armed at once.

**MUST**: the trigger's payload records `asked`, `event_at` (read by ADR-0032's gate) and the lead
it serves. The unit records that it asked. `capture.armed.trigger` says so.

Scenarios (zone -03:00, captured 2026-10-14 20:00):

- "recordame comprar café mañana a las 10" → `task`, trigger at 10-15 09:45.
- "recordame pagar el gas mañana" → 10-15 09:00.
- "recordame el dentista el viernes a las 10" → `event`; 24 h, 2 h and 15 min before.
- "recordame a las 20:10 sacar la ropa" → at once.

### R3 — The lead is the user's

**MUST**: the lead is stored in one nullable `config` column, in whole minutes.

- `NULL`, a missing row or an invalid value resolve to 15.
- Valid values run from 1 minute to 30 days.
- It is read at arm time by capture, correction and re-arm.

`/ui/preferences` edits it. An invalid value is refused with its own message and nothing is
written. A change is recorded in `config.updated`.

### R4 — Delivery

**MUST**: an asked reminder, one-shot or recurring, is pushed whatever its `interrupt_level`.
It never waits for the digest. A one-shot one is never sent after its instant.

**MUST**: a reminder at a **named time** is neither moved out of quiet hours nor deferred by the
scan, recurring ones included. A day-only one keeps ADR-0030's night rules.

Scenario: "recordame todos los días a las 06:30 tomar la pastilla", quiet end 08:00 → 06:30 daily.

### R5 — The reply

**MUST**: the reply names the instant and every reminder:

- one-shot: "Anotado para el jue 15 oct 10:00. Te aviso 15 minutos antes.";
- day only: "Anotado para el jue 15 oct. Te aviso ese día a las 09:00.";
- recurring: "Anotado. Te aviso todos los días a las 21:00.".

When the user asked and nothing was armed, a `capture.arm.refused` row records the reason and the
reply says why. Never a bare "Anotado.".

### R6 — Follow

**MUST**: a correction of an asked unit's instant leaves exactly what a fresh capture of the new
instant arms, through `prospection.Follow` with `correction.reminder.*` rows. A unit that was
refused at capture gets its reminder when the correction allows it.

**MUST**: a different lead re-arms the armed one-shot asked reminders still ahead, through I35's
decision. ADR-0032's re-plan uses the same lead set as capture.

### R7 — Explicit recurring

**MUST**: an asked message whose classification carries a `recurrence_rule` and an instant
creates a **`task`** unit, whatever the kind. It arms one recurring trigger at the named time on
each occurrence (09:00 when no time was named). It has no lead and is pushed.

**MUST**: when a recurring trigger fires, the next occurrence is armed for the same unit (doc 02
§7). This applies to every recurring trigger. Main does not do it today.

### R8 — Ask back

**MUST**: an asked unit with no instant is stored. Nooma replies "¿Algún día y hora en
particular?" and records an open pending question about the unit.

**MUST**: only the **next** message within the question window answers it:

- **Time answer** (only date and time words): sets the unit's instant, as an explicit correction
  of it. The model resolves the instant; R2 arms the reminder.
- **No-answer** ("no", "no hace falta", "nope"): the unit becomes a daily reminder at 09:00 (R7's
  mechanism).
- **Anything else:** a capture of its own. The question expires unanswered.

### R9 — Stopping, chat first

**MUST**: an **explicit stop** ("no me lo recuerdes más", "listo, no me lo recuerdes más", "stop
reminding me") stops an asked reminder for good:

- its armed triggers are `dismissed`;
- the unit no longer asks;
- the change is recorded first, with its pre-image.

**MUST**: a **bare done** ("listo", "ya lo hice", "done") on a repeating reminder:

- **Not yet marked:** Nooma asks "¿Querés que te lo siga recordando o borramos el
  recordatorio?" through the pending store. A keep answer ("seguí", "sí, seguí", "keep going")
  marks the reminder and keeps it. A delete answer ("borralo", "delete it") stops it as above.
  Any other next message leaves it as it was, and the question expires.
- **Marked:** it means done for today. No question; the delivered reminder is resolved and the
  reminder keeps recurring.
- A bare done on a one-shot reminder resolves it and asks nothing.

**MUST**: the binding, and the keep or delete answer, use closed per-language phrase lists, never
a model field. The reminder is chosen in this order:

1. The message's content words match exactly one active asked reminder.
2. Exactly one was delivered within the reply window and is the most recent delivery.
3. For an explicit stop only: exactly one is active.

Otherwise Nooma asks "¿Cuál?" with a numbered list, answered by number or by words. A bare done
that matches nothing named or recent stops nothing.

**MUST**: the reply confirms the outcome:

- "Listo, no te lo recuerdo más: tomar pastillas.";
- "Bien, te lo sigo recordando.";
- "Hecho por hoy.".

### R10 — Not changed

- A unit that did not ask arms what it arms today.
- A timer keeps its instant, no lead, and its exemption.
- A recurring reminder that was not asked keeps 7 days ahead and its interrupt routing.

### R11 — Docs

Each implementation link changes, in the same PR, what its code changes:

- doc 02 §5 steps 1, 4 and 5, §7 ("Lead time", "Recurrence", "Delivery"), §11 and §13;
- doc 03;
- doc 06 §4 (I36, I38);
- doc 07.
