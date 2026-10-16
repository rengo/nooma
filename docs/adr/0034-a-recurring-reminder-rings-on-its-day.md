# ADR-0034 — A recurring reminder rings on its day, and moves on to the next one

- **Status**: Accepted (by the client, 2026-10-16, through the PM)
- **Date**: 2026-10-16
- **Supersedes**: [ADR-0029](0029-event-reminders-follow-the-users-leads.md) point 7, and
  [ADR-0030](0030-event-reminders-are-pushed-and-respect-the-night.md) point 6's "a recurring
  reminder keeps its timing and its interrupt-level routing" (ADR-0031 point 8 repeats it).
- **Superseded by**: —

## Context

A recurring trigger fired once, seven days before its occurrence, and was never re-armed: a
weekly item rang at capture and never again; a birthday got one notice a week early and
nothing after. Doc 02 §7 and I17 promised the re-arm; no code did it.

## Decision

1. **It rings on its occurrence**, with no lead: at the event's own time, or at
   `date_only_reminder_at` (09:00) for a bare date. The anchor stores that time.
2. **One armed row per series.** Firing or expiring it arms the next occurrence in the same
   write, on the same unit, rule and anchor. The fired row stays fired: delivery, check-ins and
   the digest's re-ask read fired rows.
3. **Missed dates ring at most once**: the next occurrence is the first after the row's
   `fire_at` that is not over. A row armed a week ahead before this decision re-arms for its
   occurrence itself.
4. **Every trigger is pushed.** One that rings on its day and waits for the digest would reach
   it the next morning and be lapsed as past. Quiet hours and ADR-0032's gate still apply.

## Consequences

### What it enables

I17 as a person lives it: a conformance test walks 400 days and reads every message.

### What it costs

No week's notice of a birthday unless asked for. A low-interrupt recurring item interrupts once
on its day instead of waiting for the digest.

### Reversal criteria

The client asks for a lead per recurring series, or for quiet recurring items back in a digest
that runs before their day.
