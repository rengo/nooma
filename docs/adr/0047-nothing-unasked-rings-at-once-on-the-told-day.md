# ADR-0047 — On the day an event is told, nothing it did not ask for rings at once, an event that same day included

- **Status**: Proposed
- **Date**: 2026-10-20
- **Supersedes**: [ADR-0029](0029-event-reminders-follow-the-users-leads.md) point 3, in part
  (a capture no longer arms the at-once reminder when every lead is behind), and point 4, in
  part (a correction on its own day no longer pulls an armed reminder to now);
  [ADR-0039](0039-a-kept-ring-is-never-at-once.md) point 0 and its consequence "an event today
  keeps ADR-0029 point 3's at-once reminder", in part (the principle now covers an event on the
  told day itself); and [ADR-0032](0032-a-reminder-says-what-is-true-now.md) point 3's
  "the one path besides capture that arms an at-once reminder" (the catch-up is now the one
  unasked at-once path)
- **Superseded by**: —
- **Enables**: M4

## Context

Live, with a real model, on main at cf68791 (2026-10-20, 17:14 -03):

```
← hoy a las 21 tengo turno con el kinesiólogo
→ Anotado para el mar 20 oct, 21:00. Te aviso ahora mismo.
```

The vault's one lead, 4 hours, was already behind (17:00), so ADR-0029 point 3 armed one
reminder at once, and it rang about what the user had just said. ADR-0037, ADR-0039 and
ADR-0045 already rule this out for an event on a later day. ADR-0039 left an event today with
the at-once reminder on purpose. The client's standing rule is that nothing rings immediately.

## Decision

1. **On the local day the user tells Nooma about an event** (a capture, a correction, or a
   re-arm or re-plan whose told day is the capture's, ADR-0037 point 4), **a reminder nobody
   asked for never goes out at that moment**, whatever day the event is on. That covers the
   at-once reminder when every lead is behind, a lead the night pulled to now, and an event's
   own leads (ADR-0045).
2. **A lead still ahead is kept.** A lead that is behind is dropped. For an event on a later day
   with nothing left, ADR-0039 point 1 applies unchanged (one ring on the event's own day). **For
   an event on the told day with nothing left, there is no reminder.** The unit is stored,
   `capture.arm.refused` records the reason `leads_behind`, and the reply names what was read
   and promises no ring: "Anotado para el mar 20 oct, 21:00." (the client's ruling through the
   PM, 2026-10-20). An unasked event refused for quiet hours (ADR-0030 point 4) is answered
   the same way (same ruling). A correction expires
   the reminders it no longer carries for that same reason.
3. **A reminder already due is not rung by the telling.** A live reminder whose instant has
   already arrived keeps it (ADR-0029 point 4's "one already due keeps its instant"), so the
   same correction again changes nothing.
4. **An asked ring is untouched.** An asked lead that is behind still rings at once, or is refused
   `too_late` (ADR-0031 point 2). The user asked to be reminded, and the event is minutes away
   (the client's ruling through the PM, 2026-10-20). An
   asked event's leads nobody asked for follow point 1: "recordame, mañana a las 5 tengo vuelo",
   said at 22:30, rings at 04:45 only. Before this ADR it also rang at once, its 2-hour lead
   pulled to now by the night.
5. **Unchanged**: a timer (it fires at the instant named, never before it, and one already past
   is refused); ADR-0032's catch-up (`CatchUp`, `CatchUpAsked`), which repairs a reminder that
   lapsed rather than reacting to a telling; and a re-plan or a preference re-arm on a later day
   than the told day, which may still pull an armed reminder to now (ADR-0029 points 4 and 6).

## Consequences

- "hoy a las 21 tengo turno con el kinesiólogo", told at 17:14, rings at 19:00 under the default
  leads and not at all under a single 4-hour lead. Nothing rings at 17:14.
- An event minutes away, told unasked, gets no reminder. The user is looking at it.
- A lapse on the capture day still owes ADR-0032's catch-up: the check pass reads
  `leads_behind` as it reads every lead behind.
- The reply still confirms the instant it read, with no ring clause.
- **Known limit**: a preference re-arm on a later day than the told day can still pull an armed
  reminder to now. For example, an event in two days whose leads change to one week rings at
  the save. The client accepted this for now (through the PM, 2026-10-20).

### Reversal criteria

The user asks why an event told today got no reminder, or a missed same-day event is traced to
a capture that armed nothing. Either one points at a ring at the event's own instant instead of
no ring at all.
