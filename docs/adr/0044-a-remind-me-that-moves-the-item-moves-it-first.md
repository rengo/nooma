# ADR-0044 — A remind-me that moves the item moves it first

- **Status**: Accepted (by the client through the PM, 2026-10-20: "es a las 11, recordámelo"
  must not drop the change; apply the correction first, then arm the asked reminder on the
  corrected item, and name both in the reply)
- **Date**: 2026-10-20
- **Supersedes**: [ADR-0043](0043-a-remind-me-follow-up-reminds-of-the-item.md), its first
  consequence: a remind-me that also changes the item's instant is no longer reminded of as the
  item stands
- **Superseded by**: —
- **Enables**: M4

## Context

ADR-0043 made a bound follow-up with `remind_request: true` an asked reminder of the item it
names, on the unit as it stands. Its first consequence accepted that "es a las 11,
recordámelo" would be reminded of at the old time, with the change not applied. The reply then
confirmed a reminder ("Te aviso 15 minutos antes.") of an instant the user had just said was
wrong: a false confirmation, of the item rather than of the reminder.

## Decision

1. **A bound remind-me whose reading names a new instant for the item moves it first.** The
   reading's `event_at` or `due_at`, when it is not the one the unit already holds
   (`correction.MovesDate`; a repeated instant is an echo, I30), is applied through the existing
   explicit-referent correction path: its pre-image row, its carried text, its re-planned
   reminders (I29). The asked reminder is then armed on the moved unit as ADR-0043 arms it.
2. **The answer names both**, in the existing sentences: the move ("Movido al mar 20 oct,
   11:00.") and then the asked reminder ("Te aviso 15 minutos antes."), or why none was set
   ("Anotado, pero no puse aviso: …").
3. **A correction that asks instead of applying is answered as it is and arms nothing**: the
   item is not where the user said, so a reminder of it would be of the wrong instant.
4. **Text alone is not a change here.** A remind-me's reading restates its item's text (the
   recorded `classify-request-recordamelo` does), and a restatement cannot be told from an
   edit, so a remind-me with no new instant still rewrites nothing (ADR-0043 point 1).

## Consequences

- No prompt changes: the model already returns the new instant beside `remind_request`, so no
  recording is affected.
- A remind-me that changes only the item's wording ("es con Pedro, recordámelo") is reminded of
  with its wording unchanged; correcting the wording takes its own message.
