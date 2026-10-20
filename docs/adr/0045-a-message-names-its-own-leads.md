# ADR-0045 — A capture names its own leads, and a follow-up can add one

- **Status**: Proposed
- **Date**: 2026-10-20
- **Supersedes**: [ADR-0038](0038-an-event-can-carry-its-own-reminder-leads.md) points 1 and 2,
  in part (the leads are read for the event a chat message tells, and a capture writes them;
  a follow-up's leads may add to the item's instead of replacing them), and
  [ADR-0039](0039-a-kept-ring-is-never-at-once.md) point 3, in part (an event's own leads are
  never sent at once on the told day either)
- **Superseded by**: —
- **Enables**: M4

## Context

Live, with a real model, on a copy of the client's vault (2026-10-20):

```
← dentista mañana a las 10, avisame 1 hora antes
→ Anotado para el mié 21 oct, 10:00. Te aviso unas horas antes y 15 minutos antes.
← el jueves cena con Ana a las 21, avisame el día antes y media hora antes
→ … Te aviso el día anterior, unas horas antes y 15 minutos antes.
← mañana tengo dentista a las 10
→ … Te aviso unas horas antes.
← también 1 hora antes
→ Te aviso 1 hora antes.
```

`reminder_leads_minutes` was read only beside a `referent`, so a capture that named its leads
armed the preferences' leads plus an asked one 15 minutes before. And a follow-up's leads always
replaced the item's, so "también" dropped the reminder it was meant to keep. ADR-0038 named both
as its reversal criteria.

## Options evaluated

| Option | Real tradeoff |
|---|---|
| Show each listed item's current leads and have the model answer the whole set | The model does arithmetic on a list, and every item line in every chat prompt grows |
| Decide add or replace in code from the words ("también", "además") | A word list in code, which ADR-0033 rules out |
| **The model reads `reminder_leads_add`; the code joins the item's current leads** ✅ | One boolean in the read; the code already knows what the item is reminded at (its own leads, else what its armed reminders serve) |

## Decision

1. **Beside a chat, `reminder_leads_minutes` is read for the event a message tells as well as
   for a listed item.** A capture whose event has a time of day and whose leads §7 accepts
   stores them as the unit's `reminder_leads` and arms exactly those rings. Leads win (ADR-0043
   point 5): a `remind_request` beside them is ignored, and the unit is not asked. Leads that
   cannot apply (a task, a day alone, leads a preference may not hold) leave the capture as it
   was without them. The answer names them as said ("Te aviso 1 hora antes.").
2. **`reminder_leads_add`**, beside a `referent` and leads: true when the message adds those
   leads to the reminders the item has. The code joins them to the item's own leads, or, with
   none, to every lead its armed one-shot reminders serve (`payload.user_leads_minutes`), each
   once, the furthest first (`prospection.AddEventLeads`), and the result is the item's own leads
   as in ADR-0038. Absent, false or degraded, the leads replace, as before.
3. **An own lead never rings at the moment the event is told**, on the told day of an event on a
   later day: one already behind, or pulled to now by the night, is dropped, and with nothing
   left the one reminder rings on the event's own day (ADR-0039 point 1). The told-day rule
   still leaves own leads that are ahead (ADR-0038 point 4). The answer names what will
   ring: "dentista mañana a las 10, avisame el día antes", told after 10:00, is answered "Te
   aviso ese día a las 09:00." (the client's ruling, 2026-10-20).
4. **Leads are read only beside a chat, and the prompt with no chat is unchanged.** A capture
   from the CLI, the API or the UI, or the first chat message ever, does not read leads (the
   client's ruling, 2026-10-20: the client captures on Telegram).

## Consequences

### What it enables

- "dentista mañana a las 10, avisame 1 hora antes" rings once, at 09:00.
- "también 1 hora antes" keeps the hours-before ring and adds the hour-before one.

### What it costs

- **Every chat-shown phrasing is recorded again**, since the Follow-ups section changed.
- The asked reminder 15 minutes before is not armed beside named leads; the user named when.
- **Known limit: a task's named leads are not read.** "llamar a Ana mañana a las 10, avisame 1
  hora antes" is a task, so its leads cannot apply (ADR-0038 point 3) and it keeps its asked
  reminder 15 minutes before, as before (out of scope, the client's ruling, 2026-10-20).
- **Known limit: no chat, no leads.** The CLI, the API, the UI and a chat's very first message
  read the capture as before.
- A ring on the event's own day that serves no lead (ADR-0039 point 1) is not kept by an add:
  it serves no lead to join.

### Reversal criteria

Recorded readings that set `reminder_leads_add` on a message that names the whole set ("solo",
"nada más que"), or leads read on a capture for an event the message did not tell.
