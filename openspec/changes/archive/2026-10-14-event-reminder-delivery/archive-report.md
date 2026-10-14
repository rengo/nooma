# Archive report — `event-reminder-delivery`

**Closed**: 2026-10-14. **`main` at archive**: `2aedd36`. **Verify**: not re-run for this
archive; every PR below is merged and `tasks.md` has 10 ticked items and none open.

## What shipped

Event reminders are pushed by day, held through the night and never sent after the event
(ADR-0030, Accepted). The quiet end (07:00) and the evening time (22:00) are vault preferences.

| PR | Branch | What |
|---|---|---|
| #320 | `docs/event-reminder-delivery-plan` | ADR-0029 Accepted, ADR-0030, design, tasks |
| #322 | `feat/event-reminder-delivery` | I34, `deliverAt`, `RouteTrigger`, `EventReminderVerdict`, `quiet_hours` drop |
| #323 | `feat/quiet-hours-preferences` | migration 0007, quiet-hours parsers, the scan and digest read them |

## Follow-ups

Editing the preferences from chat is pending; `ui-preferences` shipped the UI editor.
