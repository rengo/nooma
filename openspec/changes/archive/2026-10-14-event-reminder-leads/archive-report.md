# Archive report — `event-reminder-leads`

**Closed**: 2026-10-14. **`main` at archive**: `2aedd36`. **Verify**: not re-run for this
archive; every PR below is merged and `tasks.md` has 11 ticked items and none open.

## What shipped

An event is reminded 24 hours and 2 hours before, a date the day before at 09:00, at the user's own
leads (ADR-0029, Accepted). The leads are vault preferences read at arm time; a correction moves the
whole set; the reply names every reminder.

| PR | Branch | What |
|---|---|---|
| #314 | `docs/event-reminder-leads-plan` | ADR-0029, design, tasks |
| #316 | `feat/reply-names-every-reminder` | the reply names every firing, days counted on the calendar |
| #317 | `feat/event-reminder-leads` | I33, event firings, `Arm` returning the set, `Follow` over a set |
| #318 | `feat/reminder-preferences` | migration 0006, parsers, `ResolveReminderPrefs` |

## Follow-ups

Continued by `event-reminder-delivery` (when they are sent) and `ui-preferences` (editing them).
