# ADR-0039 — The reminder kept for an event on a later day is never sent at once

- **Status**: Accepted (by the client, 2026-10-19, after "mañana tengo dentista a las 1" was
  answered "Te aviso ahora mismo." and rang at once about what they had just said)
- **Date**: 2026-10-19
- **Supersedes**: [ADR-0037](0037-no-default-ring-on-the-day-an-event-is-told.md) point 2, in
  part: the reminder kept so an event is never left without one is never one sent at once.
- **Superseded by**: —
- **Enables**: M4

## Context

ADR-0037 keeps one reminder when its rule would drop them all. It never dropped a lead that was
already behind, so an event tomorrow told after every lead had passed — a date-only one after
09:00, the client's case at 13:32 — fell through to ADR-0029 point 3's at-once reminder: Nooma
answered "ahora mismo" and rang about something the user had just said.

## Decision

1. **On the day the user tells Nooma about an event on a later local day** (a capture, a
   correction, or a re-arm that same day), when every lead nobody asked for is behind, the one
   reminder rings **on the event's own day**: at `date_only_reminder_at` (09:00), or at quiet
   end (07:00) when that is not before the event, the event of a date-only one being its whole
   day. Quiet hours apply to it as to any reminder. It serves no lead of the user's.
2. **When neither is before the event** (an event at 00:30 tomorrow, told at 23:00), no
   reminder is set and the capture records `capture.arm.refused` (`quiet_hours`), as ADR-0030
   does when only the night is left.
3. A lead still ahead is kept as ADR-0037 decides (a date-only event tomorrow told before 09:00
   keeps "hoy a las 09:00"). An asked lead, and an event's own leads (ADR-0038), are untouched.
4. A correction arms that reminder as a capture does. A re-plan on a later day is unchanged: its
   catch-up stays ADR-0032's.
5. The reply names it with the existing phrasing: "Te aviso ese día a las 09:00." for a
   date-only event.

## Consequences

- An event on a later day told on a given day never gets a reminder at once that same day.
- An event today keeps ADR-0029 point 3's at-once reminder when every lead is behind.
