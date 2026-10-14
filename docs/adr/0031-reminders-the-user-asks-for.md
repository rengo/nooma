# ADR-0031 — A reminder the user asks for is set from their own words, asked about when unclear, and stopped only when they say so

- **Status**: Accepted (by the client, 2026-10-14)
- **Date**: 2026-10-14
- **Supersedes**: [ADR-0027](0027-pending-question-store.md) **in part**: the table's shape
  only. ADR-0027's decision that a question lives in a dedicated store stands. Its own reversal
  criterion ("a second question kind … with no relation to name") is the reason.
- **Superseded by**: —
- **Extends** (none edited):
  - [ADR-0029](0029-event-reminders-follow-the-users-leads.md) points 3 and 7;
  - [ADR-0030](0030-event-reminders-are-pushed-and-respect-the-night.md) points 1-4;
  - [ADR-0032](0032-a-reminder-says-what-is-true-now.md) (its gate reads asked reminders).

## Context

On 2026-10-14 the client sent "recordame comprar café mañana a las 10". It was classified `task`
with `due_at` 10:00 the next day, as the 2026-10-09 rule says. A task arms nothing (ADR-0029
point 7), and nothing carries "the user asked to be reminded". No reminder was set, and the reply
was "Anotado.".

The client then decided how an explicit request behaves: one-shot, a day alone, no time at all,
recurring, on an event, at night, and how it stops. The PM set three defaults. This ADR records
them and the mechanism that keeps them from depending on the model for what the user wrote
literally.

## Options evaluated

**How Nooma knows the user asked, wants to stop, or is answering**

| Option | Real tradeoff |
|---|---|
| Fields the model fills (`remind`, `stop`, "this answers question X") | Catches paraphrases. But it trusts the model for what the user wrote, each field can degrade, and the classify recordings predate them |
| **Closed phrase lists per language on the raw message, and the pending store to bind** ✅ | Deterministic and testable without a model, as m3e binds relation answers. A paraphrase is missed until its phrase is added. The model still gives the kind and resolves an instant |

**Where "asked" is stored**

| Option | Real tradeoff |
|---|---|
| Only on the reminder | Lost when nothing is armed: a refusal, an open question, a stop |
| **On the unit, with the reminder marked too** ✅ | A column. Correction, re-plan and stop know the unit asked |

**Where a question about a reminder lives**

| Option | Real tradeoff |
|---|---|
| A new table | A second store for the same asked/open/resolved lifecycle |
| **`pending_questions`, widened to a unit or a list of units** ✅ | One store. The table is rebuilt once. Relation questions read as before |

**Quiet hours for a time the user named**

| Option | Real tradeoff |
|---|---|
| ADR-0030 as for events | "a las 06:30" would ring at 22:00 the evening before, or at 07:00 |
| **A named time rings at that time** ✅ (client) | Doc 02 §7 already ranks an explicit instruction above the window (timers). It rings at night if asked for at night |

## Decision

1. **Asked.** A message asks when its own words hold a remind phrase from a closed per-language
   list ("recordame", "avisame", "remind me", …), not negated. The model's rewording is never
   read for this. Any kind that stores a unit and has a date can carry it.
2. **One-shot.**
   - **A named time:** one reminder **15 minutes before** by default. This lead is a vault
     preference (1 minute to 30 days, edited on `/ui/preferences`), separate from the event
     leads.
   - **A day alone:** **that day at 09:00**.
   - **An event:** the asked lead is **added** to its 24 h and 2 h.
   - **A lead already past** while the time is still ahead: the reminder goes out **at once**.
3. **Recurring.** An asked repetition ("todos los días a las 9pm") is a **recurring task** that
   reminds **at the named time** on each occurrence (09:00 with no time named). After each
   reminder the next one is armed, which makes doc 02 §7's promise true for every recurring
   reminder.
4. **No day and no time.** Nooma stores the request and asks "¿Algún día y hora en particular?".
   - Only the **next** message within an hour answers, and only if it is made of date and time
     words, or a "no".
   - A time sets the reminder; the model resolves it as it resolves a correction.
   - "No" makes it **daily at 09:00**.
   - Anything else is a new capture, and the question expires.
5. **Stopping, chat first.**
   - **An explicit stop** ("no me lo recuerdes más", also "listo, no me lo recuerdes más") stops
     a reminder for good.
   - **A bare "listo" / "ya lo hice"** on a daily or recurring reminder is ambiguous. The first
     time, Nooma asks "¿Querés que te lo siga recordando o borramos el recordatorio?":
     - **"Seguí"** keeps it and marks it, so a later bare "listo" on it means done for today,
       with no question, and it keeps recurring.
     - **"Borralo"** stops it.
     - Any other answer leaves it unmarked, to be asked again next time.
   - A reminder made daily by "No" follows the same rule.
   - Which reminder is meant: the one the message names; else the one Nooma just sent; else,
     for an explicit stop, the only one there is. Otherwise Nooma asks which, with a numbered
     list.
   - Every answer is read from phrase lists, never by the model. Every stop is recorded with
     what it changed.
6. **Delivery.**
   - An asked reminder is always pushed, never held for the digest.
   - A one-shot one is never sent after its time (ADR-0030, ADR-0032).
   - **A time the user named rings even inside quiet hours**, recurring ones included. A time
     Nooma chose (09:00) keeps the night rules.
7. **The reply** says exactly when: "Anotado para el jue 15 oct 10:00. Te aviso 15 minutos
   antes." / "Te aviso todos los días a las 21:00.". When nothing was set, it says why.
8. **Not changed:** timers, recurring reminders that were not asked for, and events and tasks
   that were not asked about.

## Consequences

- The observed message reminds at 09:45 the next day, and the reply says so.
- A request worded outside the lists gets no reminder until its phrase is added. A bare "listo"
  with nothing just sent and nothing named stops nothing.
- Migrations:
  - the request on the unit and the lead in `config`;
  - the rebuild of `pending_questions`;
  - the "keep going" mark on the unit.
- On acceptance, ADR-0027's header gains `Superseded by: ADR-0031 (shape)`.
- Doc 02: §5 step 4's "a `due_at` correction moves no trigger" now applies only to a unit that
  did not ask, and §7's re-arm on firing becomes true. Doc 06 gains I36 and I38.

### Reversal criteria

Missed requests for paraphrases a list cannot reasonably hold point at adding a model signal as a
second source. Unwanted night reminders point at point 6. A stop applied to the wrong reminder
points at narrowing point 5 to "ask whenever more than one is active".
