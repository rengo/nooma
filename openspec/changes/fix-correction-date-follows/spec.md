# Spec — fix-correction-date-follows

States what MUST be true after this change. `design.md` says how.

## R1 — The text follows a corrected date

When a correction writes `event_at` or `due_at`, the content's statement of the previous instant
— a `YYYY-MM-DD` date token, and the `HH:MM` time token that follows it in the same phrase — MUST
be rewritten to the new instant in the same form. A bare time, a range and URL or query text MUST
NOT be touched, and the content edit MUST be part of the same
`correction.applied` row (`fields`, `previous`, `next`). A content that does not write the
previous instant in that form MUST stay as it was.

**Scenario.** A unit "dentista el 2026-10-16 a las 10:00" at 2026-10-16T10:00Z corrected to
2026-10-09T11:00Z reads "dentista el 2026-10-09 a las 11:00", and one `correction.applied` row
carries `fields: [event_at, content]` with both previous values.

## R2 — Capture writes resolved dates in one form

The classify prompt MUST ask that `normalized_content`, when it names a date or time it resolved,
write the date as `YYYY-MM-DD` and the time as `HH:MM` (24-hour).

## R3 — The reminder follows a corrected event date

When a correction writes `event_at` on an `event` unit, the unit's armed triggers MUST end as
exactly what a fresh capture of that date arms, with the same functions:

- New date ahead: one armed trigger at the lead time, pulled to now when the horizon is behind.
  An armed trigger is moved (its `fire_at`, its text per R1 and, when recurring, its anchor);
  with none armed, one is created. Any further armed trigger on the unit is expired.
- New date past (one-shot): every armed trigger on the unit is expired; none is created.
- A recurring trigger stays recurring, re-anchored on the new date.
- A `due_at` correction, or a correction of a non-event unit, touches no trigger.

Each move, creation and expiry MUST write one `decision_log` row (`correction.reminder.moved`,
`correction.reminder.armed`, `correction.reminder.cancelled`) before the trigger write, with
change-shaped `previous`/`next` so activity shows them.

**Scenario: the client's case.** At 2026-10-09T08:00Z, a unit at 2026-10-16T10:00Z with a trigger
armed for 2026-10-09T10:00Z, corrected to 2026-10-09T10:00Z: the trigger is moved to fire at
08:00Z (at once) and one `correction.reminder.moved` row is written.

**Scenario: past.** The same correction at 11:00Z expires the trigger, writes one
`correction.reminder.cancelled` row, and arms nothing.

## R4 — Every path, nothing deleted

R1 and R3 MUST hold on the explicit-referent path and on the chat recall path. A failed
`correction.applied` write MUST leave the unit and its triggers untouched (I23).
