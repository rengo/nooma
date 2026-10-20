# ADR-0044 — Beside a chat, an answer names the item it is about

- **Status**: Accepted (by the client through the PM, 2026-10-20: a chat message is read "en el
  momento por el hilo de la conversación", ADR-0036)
- **Date**: 2026-10-20
- **Supersedes**: [ADR-0043](0043-a-remind-me-follow-up-reminds-of-the-item.md) points 3 and 4
  in part (the answer names every ring the item has, not the asked one alone), and, beside a
  chat only, doc 02 §5 step 1's "a check-in outcome closes the most recently delivered open
  check-in"
- **Superseded by**: [ADR-0046](0046-a-done-follow-up-closes-the-event-too.md), point 3 in part
  (reminder leads beside a listed letter bind whatever the type, as a remind-me does)
- **Enables**: M4

## Context

Live, on a copy of the client's vault (main at 378c65a), with a reminder of "Turno con el
oculista" pushed at 09:07 and its check-in still open:

```
← el viernes tengo que llevar el auto al service
→ Anotado para el vie 23 oct. Te lo recuerdo en el resumen de esa mañana.
← recordámelo
→ ¿Qué quieres que te recuerde y cuándo?
```

The model read "recordámelo" as a `chitchat` with `nudge_outcome: engaged`, and the code closed
the oculista check-in (`capture.checkin.resolved`), the newest open one. The service task got no
reminder. Two homes competed for the same message:

- The check-in section told the model that "which one is open is not shown here" and that a
  short reply answers the check-in, with `engaged` meaning "agrees to it, or asks to go on with
  it". Beside a chat that was false: the push is a turn of it, lettered.
- Follow-ups gave `referent` a meaning for a `correction` and a done only. The recorded
  `classify-request-recordamelo` bound its task by coming back as a `correction`, which the
  prompt never asked for.

And the code closed the newest open check-in whatever the message followed.

The next message, "el jueves cena con Ana a las 21", came back with `remind_request: true`: the
chat now ended in Nooma asking "what and when should I remind you of", so the model read the
event as that answer. Then "es a las 22, recordámelo" moved it and answered "Te aviso 15
minutos antes.", while the day-before and hours-before rings stayed armed and unnamed.

## Options evaluated

| Option | Real tradeoff |
|---|---|
| A clause in the check-in section: a remind-me is not a check-in answer | A fourth per-section clause about one message, the shape ADR-0036 retired for the which-item answer; the code still closes the newest check-in on any misread |
| Show the open check-ins as their own list | A second list of the same items, with the push already in the chat; two homes for one thing |
| **`referent` is the item the message is about, for every use; beside a chat a check-in answer closes only the check-in of the item it names** ✅ | One field, one home (Follow-ups), and the code checks the model's letter against what it showed, as for a correction and a done. A check-in whose push fell out of the last turns cannot be answered from the chat; the morning digest asks it again, as a push |

## Decision

1. **One meaning for `referent`, beside a chat**: the letter of the listed item the message is
   about. It is given for a `correction` (the item changed), beside `remind_request: true` (the
   item to be reminded of), and beside a check-in answer other than `snooze` (the item said to
   be done, or the pushed item whose check-in it answers). A message answers the latest
   exchange it follows: a short reply naming no item is about the item of the newest turns, and
   answers a pushed check-in only while that push is the latest exchange. A message that only
   asks to be reminded asks it of the item it follows.
2. **The check-in section says, beside a chat**, that what Nooma asked is a pushed turn of the
   chat and that an answer names that turn's item. With no chat it is unchanged, byte for byte.
3. **A bound remind-me binds whatever its type** (`classify.ReadFollowUp`): a reading with
   `remind_request: true` and a listed letter is a follow-up, as a done is (ADR-0040).
4. **Beside a chat, a closing check-in answer closes only the open check-in of the item its
   `referent` names.** One that names no item, or an item with no open check-in, closes
   nothing and is recorded as `capture.checkin.unmatched`. A `snooze` closes nothing: one that
   names a shown item puts off that item's check-in (`capture.checkin.snoozed`), or none when it
   has none, and one naming no item puts off the newest open check-in, as before. Read alone (no
   chat shown), the newest open check-in, as before.
5. **A remind-me's answer names every ring the item has** once the asked one is armed, as a
   capture's answer names all it armed: "Te aviso unas horas antes y 15 minutos antes.", never
   the asked one alone while the others ring unnamed.
6. **A bare date on the item's own local day is no move** for a remind-me
   (`correction.MovesDate`). The real model restates the item's day beside "recordámelo"
   (`due_at: "2026-10-17"` for a task due that day at 10:00); read as a move, it put the task at
   midnight, rewrote its text to "a las 00:00" and answered "Movido al sáb 17 oct.". A timed
   instant still moves unless it is the same instant (I30), and a correction's own plan keeps
   the exact echo.

## Consequences

### What it enables

- "recordámelo" after a task told since a push reminds of that task, and leaves the push's
  check-in open.
- A misread check-in outcome beside a chat cannot close a check-in the message did not name.

### What it costs

- **Every chat-shown phrasing is recorded again**, since Follow-ups and the check-in section
  changed beside a chat.
- **A check-in whose push is no longer among the last turns** cannot be answered from the
  chat: an answer to it closes nothing and is recorded unmatched. The morning digest asks it
  again, and that digest is a push turn the answer can name.

### Reversal criteria

Check-in answers recorded as unmatched in use, for a push the user plainly answered, point back
to the store choosing when the model names nothing.
