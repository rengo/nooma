# ADR-0037 — No default reminder rings on the day the user told Nooma about the event

- **Status**: Accepted (by the client, 2026-10-19: "si el recordatorio es mañana, te dice te aviso
  un día antes, sería hoy mismo .. ahi deberia solo decir de unas horas antes")
- **Date**: 2026-10-19
- **Supersedes**: [ADR-0029](0029-event-reminders-follow-the-users-leads.md) points 1, 2, 4 and
  6, in part: a lead nobody asked for that lands on the day the event was told is not armed.
- **Superseded by**: —
- **Enables**: M4

## Context

"mañana tengo dentista a las 10", said at 08:44, armed the 24-hour lead for 10:00 the same
morning, 75 minutes later, and the reply promised it as "el día anterior". The user had just
said it: a reminder that day tells them nothing, and the promise reads as false.

## Decision

1. **A lead nobody asked for** (the event's own leads, default or the user's preference) whose
   nominal instant is ahead and on the **local day the user told Nooma about the event**, for an
   event on a later local day, **is not armed**. The reply names only what is armed.
2. **Never left with no reminder**: when that would leave no lead ahead and none asked, the
   nearest such lead (the latest, closest to the event) stays. A date-only event tomorrow, told
   before 09:00, keeps its one reminder today at 09:00.
3. **An asked lead is untouched** ("avisame 1 día antes" rings when asked, ADR-0031).
4. **The told day** is the capture's for a capture; a **correction** is the user telling Nooma
   about the event again, so its own day is the told day (`prospection.Follow`). A **re-arm under
   new preferences** and the check pass's **re-plan** take the unit's capture instant
   (`created_at`): on the capture day the dropped ring does not come back; on a later day the
   day-before ring armed earlier stays. The at-once catch-up (`CatchUp`, `CatchUpAsked`) is
   unchanged: it arms only when every lead is behind.
5. The nominal instant decides, before any quiet-hours move (ADR-0030).
6. **The reply names a ring on the told day as today at its hour** ("Te aviso hoy a las
   09:00."), whichever rule kept it (the fallback of point 2, or an asked lead), for an event on
   a later day. It reads the capture instant, never `FireAt` minus `About`. An unasked date-only
   event's reply names its date alone, not "00:00".

## Consequences

- An event captured today for tomorrow gets one reminder by default (2 hours before), not two.
- A correction on a day after the capture drops a lead landing that day; a later preference save
  the same day, keyed on the capture, may arm it again. Accepted: both are the user acting on
  that event that day, and the case needs a correction and a preference change on one day.
- Reminders already armed in a vault are not rewritten; one on its capture day is dropped the
  next time a correction or a capture-day re-arm re-plans it.
