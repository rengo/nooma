# ADR-0039 — On the day an event on a later day is told, no reminder rings at that moment

- **Status**: Accepted (by the client, 2026-10-19, after "mañana tengo dentista a las 1" was
  answered "Te aviso ahora mismo." and rang at once about what they had just said)
- **Date**: 2026-10-19
- **Supersedes**: [ADR-0037](0037-no-default-ring-on-the-day-an-event-is-told.md) point 2, in
  part: the reminder kept so an event is never left without one is never one sent at once; and,
  on that day only, ADR-0029 point 3's at-once reminder and ADR-0030's lead pulled to now.
- **Superseded by**: [ADR-0045](0045-a-message-names-its-own-leads.md), point 3 in part (an
  event's own leads are never sent at once on the told day either);
  [ADR-0047](0047-nothing-unasked-rings-at-once-on-the-told-day.md), point 0 in part (the
  principle now covers an event on the told day itself)
- **Enables**: M4

## Context

ADR-0037 keeps one reminder when its rule would drop them all. It never dropped a lead that was
already behind, so an event tomorrow told after every lead had passed — a date-only one after
09:00, the client's case at 13:32 — fell through to ADR-0029 point 3's at-once reminder: Nooma
answered "ahora mismo" and rang about something the user had just said.

## Decision

0. **The principle**: on the day the user tells Nooma about an event on a later local day (a
   capture, a correction, or a re-arm that same day), a reminder they did not ask for never
   rings at the moment they told it — not ADR-0029's at-once reminder, and not a lead the night
   pulled to now (ADR-0030: "05:00 tomorrow", said at 22:30, whose 2-hour lead would go out at
   22:00 the evening before, already behind).
1. When that leaves no reminder, the one reminder rings **on the event's own day**: at `date_only_reminder_at` (09:00), or at quiet
   end (07:00) when that is not before the event, the event of a date-only one being its whole
   day. Quiet hours apply to it as to any reminder. It serves no lead of the user's.
2. **When neither is before the event** (an event at 00:30 tomorrow told at 23:00, or at 05:00
   tomorrow told at 22:30), no
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
