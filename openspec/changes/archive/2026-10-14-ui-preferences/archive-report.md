# Archive report — `ui-preferences`

**Closed**: 2026-10-14. **`main` at archive**: `2aedd36`. **Verify**: not re-run for this
archive; every PR below is merged and `tasks.md` has 15 ticked items and none open.

## What shipped

`/ui/preferences` edits the reminder leads and the quiet-hours times. Saving logs `config.updated`
and re-arms the reminders of every event ahead, without sending again what was already sent (I35).
A digest is sent at most once per local date.

| PR | Branch | What |
|---|---|---|
| #329 | `fix/digest-once-per-date` | at most one digest per local date |
| #330 | `feat/reminder-prefs-save` | `ConfigRepo.SetReminderPrefs`, `PreferencesService` |
| #331 | `feat/reminder-prefs-rearm` | I35, re-arm through `Follow`, `TriggerRepo.ArmedUnitIDs` |
| #333 | `feat/reminder-prefs-delivered` | a re-arm counts as sent by lead and date delivered |
| #332 | `feat/ui-preferences` | the page, the leads editor, errors, nav |

## Follow-ups

Editing the preferences from chat is pending.
