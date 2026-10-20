# ADR-0046 — A chat follow-up that says a shown event already happened closes it, and leads name their item

- **Status**: Proposed
- **Date**: 2026-10-20
- **Supersedes**: [ADR-0040](0040-a-done-follow-up-closes-the-task.md) points 2 and 3, in part:
  a bound `event` still `pool` with no recurring reminder is closed as a task is, rather than
  keeping its path; and [ADR-0044](0044-a-chat-answer-names-the-item-it-is-about.md) point 3,
  in part: reminder leads beside a listed letter bind whatever the type, as a remind-me does
- **Superseded by**: —
- **Enables**: M4

## Context

Live, on a copy of the client's vault (main at 411bd36, real model):

```
← hoy a las 21 tengo turno con el kinesiólogo
→ Anotado para el mar 20 oct, 21:00. Te aviso unas horas antes.
← listo, ya fui
→ No encontré nada pendiente para cerrar.
```

The 17:00 ring stayed armed and would have gone off for a visit the user had already made. The
same for "ya la hice, la adelantamos" after "mañana a las 10 reunión con el contador": the 07:00
ring stayed armed. ADR-0040 point 3 kept a bound event on the path it took before (answer its
open check-in from the store, if any), so an event told and not yet pushed had nothing to
answer, closed nothing, and said so. The prompt's done meaning named a task only.

Recording the chat-shown phrasings again for this change surfaced a second gap, in the same
binding. "avisame 2 horas antes" after "reunión con Ana" came back typed `event`, with its
leads, referent `A` and a stray `remind_request: true`. It bound its item only through that
stray request (ADR-0044 point 3 binds a remind-me whatever its type). Without it, as the prompt
asks, the same reading stored a second unit and answered "Anotado." while the meeting's leads
stayed unset (review on #385). Leads beside a referent are a follow-up by what they mean, and
the prompt already says so: "Beside a referent, reminder_leads_minutes says when to be reminded
of that listed item".

## Options evaluated

| Option | Real tradeoff |
|---|---|
| Expire the event's rings and leave it `pool` | The rings stop, but the event stays live: the digest and Today keep naming a visit that happened; "done" would mean two different things by type |
| **A done of a bound event closes it as a task's does** ✅ | The same transition (`pool → archived`, nothing deleted), the same rings expired and check-ins answered, the same reply. A misread closes an event the user still means to attend; it is archived, not deleted, and the trail names it |

## Decision

1. **A bound event is closed like a task.** `prospection.DoneCloses` is true for a `task` or an
   `event` still `pool` whose reminders recur on no rule. The event moves `pool → archived`,
   its armed reminders are expired so none of them goes off, its open check-ins are answered
   `engaged`, and one **`capture.event.closed`** row records it, with the same change-shaped
   context as `capture.task.closed` (status before and after, the reminders and check-ins it
   took). A recurring series is still not ended by a done of one occurrence.
2. **The reply is the task's**: "Listo, saqué «Turno con el kinesiólogo».". It names no ring,
   since none is left.
3. **The model says so beside a chat.** The Follow-ups section says a listed item is done when
   it is a task finished or an event that already took place, and the check-in section's
   `done` meaning adds "or that a listed event already took place" beside a chat. With no chat
   the prompt is unchanged, byte for byte: nothing is listed, so no event can be named.
4. **Leads bind like a remind-me.** `classify.ReadFollowUp` binds a reading carrying
   `reminder_leads_minutes` beside a listed letter whatever its type, as it binds a
   `remind_request`; leads still win over a stray request (ADR-0043). It is code reading a field
   the model fills by meaning (ADR-0033); the prompt does not change.
5. Everything else ADR-0040 decided holds: no item named still says "No encontré nada pendiente
   para cerrar.", two candidates still ask which, and a task closes exactly as before.

## Consequences

### What it enables

- "listo, ya fui" after an event told or pushed today closes it, and its remaining rings never
  go off.

- "avisame 2 horas antes" sets the item's leads whatever type the model gives it, with or
  without a stray `remind_request`.

### What it costs

- **A misread can close an event the user still means to attend**, and its rings with it. It
  needs a listed letter and a done reading; the event is archived, not deleted, and the trail
  records it.
- **The chat-shown phrasings are recorded again**, since Follow-ups and the check-in section
  changed beside a chat.
- **A stray referent beside a new event's own leads patches the listed item** instead of storing
  the new event. It needs the model to name a letter where the prompt says to leave it out, the
  same exposure a stray `remind_request` already had.
- **One more decision action**, `capture.event.closed`, labelled "Event done" in
  `/ui/activity`.

### Reversal criteria

Events closed in use that the user had not attended point back to expiring the rings and
leaving the event live.
