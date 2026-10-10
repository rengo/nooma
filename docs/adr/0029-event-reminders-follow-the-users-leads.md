# ADR-0029 — Event reminders: the day before and two hours before, as the user's own preference

- **Status**: Proposed
- **Date**: 2026-10-10
- **Supersedes**: —
- **Superseded by**: —
- **Enables**: M4 (the settings page and the chat intent that edit these preferences)

## Context

A dated `event` arms one trigger `event_lead_days` (7) before it, pulled to the capture instant
when that horizon is already behind (doc 02 §7, "Lead time"). For the events people actually
capture — a flight in two days, a dentist on Friday — the horizon is almost always behind, so the
reminder fires the moment the event is captured, and fires at once again every time its date is
corrected (I29 re-arms what a fresh capture arms). The client's verdict: useless for real events.

The client decided the replacement in product terms: a timed event is reminded 24 hours before
and 2 hours before; an event with no time of day is reminded the day before at 09:00 local; leads
already behind are skipped. Then a second decision: **reminder settings are the user's, changeable
from the web UI and by chatting with the agent** — a general principle for user-facing
preferences. This ADR fixes the defaults, the mechanism, and where the preferences live; the UI
page and the chat intent that edit them are later work units.

## Options evaluated

**Where the preferences live**

| Option | Real tradeoff |
|---|---|
| A constant in `internal/core` (today's shape) | Free, and exactly what the client ruled out: nothing a user can change |
| `nooma.toml` | Not in the vault (it does not travel with it), and neither the UI nor chat can write it at runtime |
| The `calibration` key/value table | Reserved by doc 02 §13 for M5's learning module, for knobs that have no `config` column; a preference the user sets is not a learned value |
| **Two nullable columns on the `config` singleton row** ✅ | One migration. Same home, same read (`ConfigRepo.Load`) and same `Resolve*` posture as every other vault setting, and the same write path `m4e2-admin` plans (`config.updated`). `NULL` means "the user never chose", so a later change of default reaches everyone who never chose |

**When a preference changes**

| Option | Real tradeoff |
|---|---|
| Applies to new arms only | Simplest. But "remind me 3 hours before" would not apply to the flight tomorrow the user is thinking of when they say it |
| **Re-arm every armed reminder of an event still ahead** ✅ | Uses the very decision a corrected date already uses (`prospection.Follow`), so a preference change and a date change cannot disagree about what is armed |

## Decision

**A dated `event` is reminded at the user's leads; the leads are vault preferences.**

1. **Timed event**: one reminder per lead in `event_reminder_leads`, default **24 hours and 2
   hours** before the event instant.
2. **Date-only event** — an `event_at` at exactly local midnight in the user's zone, which is
   how a date with no time of day is stored (doc 02 §5.1): one reminder **the day before at
   `date_only_reminder_at`**, default **09:00** local.
3. **A lead already behind is skipped**; only leads ahead are armed. When none is ahead and the
   event still is, a capture arms **one reminder at once** (today's pull-to-now).
4. **A correction re-arms the set** for the new instant (I29, extended from one trigger to the
   set): armed reminders are moved onto the new leads, missing ones created, extra ones
   expired, each recorded first, and the same correction again repairs a partial failure. **A
   correction never creates an at-once reminder**: it may pull an already-armed one to now, but
   when every lead of the new instant is behind and nothing is armed, it arms nothing — one
   at-once reminder per event, not one per correction.
5. **Preferences**: two nullable `config` columns. `NULL`, a missing row, or a stored value that
   fails validation resolve to the default. Valid input: 1 to 5 leads, each a whole number of
   minutes from 1 minute to 30 days, no two equal; a time of day `HH:MM` from `00:00` to `23:59`.
6. **A preference change re-arms** every armed one-shot reminder of every event still ahead,
   through the same decision as point 4. It ships with the first writer of these preferences.
7. **Not changed**: a `recurring_reminder` (a birthday) still arms one trigger `event_lead_days`
   (7) before its next occurrence; a task's `due_at` arms nothing, as before.

**Vaults with reminders already armed** keep them: a 7-day trigger already in a vault fires when
it was armed to. It becomes the new set on the next correction of its event's date or the next
change of these preferences. No migration rewrites a trigger.

## Consequences

- An event genuinely at midnight is read as date-only and reminded the day before at 09:00
  rather than at 22:00 and 00:00 the night before. The classification does not say whether a
  time was stated, and adding that would be a new field, a schema column, and a corpus re-record.
- A unit captured with every lead already behind, then corrected, gets no new reminder unless the
  new instant has a lead ahead. That is point 4's rule, not an oversight.
- Doc 02 §7 "Lead time" and §13 change in the same PR as the code; the §13 rows say these
  defaults are user-overridable.
- No archive path expires a unit's triggers today; this ADR adds none.
