# ADR-0038 — An event can carry its own reminder leads, set from chat

- **Status**: Accepted (by the client, 2026-10-19, through the PM: "the user can change an
  event's reminder leads from chat"; "Nooma is an LLM operating over defined entities")
- **Date**: 2026-10-19
- **Supersedes**: [ADR-0029](0029-event-reminders-follow-the-users-leads.md), points 1 and 6 in
  part (an event with its own leads is reminded at those, and a preference change leaves them)
- **Superseded by**: —
- **Enables**: M4

## Context

On Telegram the client moved the dentist with "es a las11", then wrote "avisame solo 1 hora
antes". Nooma answered "Corregido." and no reminder changed: a correction could only write
`content`, `event_at`, `due_at` and `title`, and ADR-0029's leads are one vault preference for
every event. Nothing could hold "this event, one hour before".

## Options evaluated

| Option | Real tradeoff |
|---|---|
| Change the vault preference from chat | Changes every event to fix one; not what was asked |
| Keep the leads only on the armed triggers | No schema, but every later re-plan plans from the preferences and brings the old leads back |
| **A nullable `units.reminder_leads`, written by a correction, read by every re-plan** ✅ | One column; the same `prospection.Follow`, with the preferences' leads replaced (`ReminderPrefs.WithEventLeads`) |

## Decision

1. **The model reads the leads as a value** (ADR-0033): beside a chat follow-up's `referent`,
   `reminder_leads_minutes`, every lead asked for, in minutes. Offered only in a prompt that
   shows a chat, so the prompt without one is unchanged.
2. **A correction writes them** as their own field (`reminder_leads`, pre-image first). Alone
   they are the whole plan, the model's text never taken as the body; beside a new date they
   travel with it.
3. **They apply only to an `event` whose `event_at` has a time of day**, with leads ADR-0029
   point 5 accepts. Otherwise nothing is written and the answer says the reminders were not
   changed, never "Corrected." (I40).
4. **Every re-plan of that event uses them**: the correction, a later date correction, a
   preference change and the due scan. `NULL` follows the preferences. The user named them, so
   they are asked leads: [ADR-0037](0037-no-default-ring-on-the-day-an-event-is-told.md)'s
   told-day rule leaves them.
5. **The answer names the reminders now armed**, as the user said them ("Te aviso 1 hora
   antes."), or that none is left.

## Consequences

- Migration 0011 and a fifth correction field. Every chat-shown phrasing is recorded again.
- Without a recent chat the field is not offered, and a lead request reaches the ordinary
  correction path.
- **Reversal criteria**: users adding a lead to the ones an event has ("también 1 hora antes"),
  which needs its current leads shown to the model; or per-event leads wanted at capture.
