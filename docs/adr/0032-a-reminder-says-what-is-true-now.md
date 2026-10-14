# ADR-0032 — A reminder says what is true when it is sent, in the user's language

- **Status**: Accepted (by the client, 2026-10-14)
- **Date**: 2026-10-14
- **Supersedes**: —
- **Superseded by**: —
- **Amends** (none of them edited): [ADR-0009](0009-scheduler-downtime.md) (judged at
  every delivery attempt, by a pass that can deliver), [ADR-0022](0022-reply-language.md) (its
  "anything Nooma says first" gap), [ADR-0029](0029-event-reminders-follow-the-users-leads.md)
  points 3-4 (an at-once reminder only at capture), and
  [ADR-0030](0030-event-reminders-are-pushed-and-respect-the-night.md) point 4 (never late)

## Context

On 2026-10-14 the client enabled Telegram on a vault that had run without a channel for five
days. The first digest read "Here are 7 things for today", listed three flights that had already
departed, said "09:00" for a flight the user had corrected to 08:00, listed one flight twice, and
asked its relation question in English to a user whose every reply comes back in Spanish.

## Options evaluated

| Question | Option | Verdict |
|---|---|---|
| Backlog | Send every undelivered reminder whose event is still ahead, late | Rejected: ADR-0009 expires a trigger overdue past its window rather than send it out of context, and a five-day-old "you have a flight" is that |
| Backlog | Expire it and send nothing | Rejected: the user was never reminded of tomorrow's dentist |
| Backlog | Expire what is late; re-plan only the event's leads still ahead | Rejected by the client: an event whose every lead passed would get no reminder at all |
| Backlog | **Expire what is late; re-plan the leads still ahead, or one reminder at once when none is** | Chosen: nothing is sent late, and an upcoming event the user was never reminded of gets one reminder |
| Text | Re-word the frozen text with a model at delivery | Rejected: a model call per digest line, and a correction already gives the current text for free |
| Text | **Render from the unit as it is now: its text and its instant, in a fixed frame** | Chosen |
| Language | A `language` setting | Rejected for ADR-0022's reasons |
| Language | The language of the user's last message, read from the audit trail | Rejected: `decision_log` is an audit, and a behaviour reading it for state couples the two |
| Language | **The language is stored on the unit at capture**; a reminder speaks its unit's, the digest speaks the newest unit's | Chosen: the shape ADR-0022 itself named |

## Decision

1. **Every delivery attempt is judged, not only the first.** A reminder is sent only while its
   event is still ahead, the unit still has that event (a correction to another instant
   supersedes it), and it is no later than ADR-0009's window. Otherwise it **lapses**: it is
   never sent, and the trail says why. ADR-0009's window is for a push. A recurring reminder
   waiting for its digest is judged only on its occurrence having passed.
2. **A one-shot reminder that could not be pushed is pushed again on a later pass**, while (1)
   holds. It does not fall back to the digest: ADR-0030 already says a reminder that waits for
   tomorrow's digest arrives after the thing it is about.
3. **A backlog is re-planned, not replayed.** When a reminder lapses and its event is still
   ahead, the event gets the reminders the user's leads still have ahead, by the rule a corrected
   date uses; a reminder superseded by a correction re-plans for the unit's new instant. **When
   every lead has passed and nothing about that event reached the user, it gets one reminder at
   once** (client, 2026-10-14): never one per lapsed lead, never more than one outstanding, and
   never another once one was delivered; one that could not be delivered may be followed by
   another while the event is ahead ("más vale tarde"). This is the one path besides capture
   that arms an at-once reminder. It obeys quiet hours: inside them it waits for quiet end, and
   **when quiet end is at or after the event nothing is sent**, so an event inside the night whose
   leads all lapsed gets no reminder, and the trail says so. The replacement is written before
   the late reminder is retired, so an interrupted check loses nothing. A reminder counts as sent
   once it reached the user, or while it is still being delivered on time. A check that cannot
   deliver (no channel, or no conversation) only retires reminders whose event passed: it
   re-plans nothing and leaves the rest for the first check that can.
4. **A reminder says the unit as it is now**: its current text and its current instant, framed as
   a reminder ("Recordatorio: … — jue 15 oct, 11:00"). One unit is one line, however many of its
   triggers are due together.
5. **What Nooma says first speaks the user's language.** The classification's language is stored
   on the unit. A pushed reminder speaks its unit's language; the digest (header, lines' frame,
   relation question) speaks the language of the most recently captured unit that has one; none
   known, the fallback. The glass box stays English (ADR-0022 point 4).

## Consequences

### What it enables

- After weeks without a channel the first message is still true, and a correction reaches every
  reminder not yet delivered.

### What it costs

- A migration (`units.language`), backfilled from the `capture.classify` rows.
- A due scan now reads the units of the reminders it delivers, one read per reminder.
- On a vault with no channel, reminders for events still ahead stay armed until one exists (at
  most one row per lead per event), and Today lists them so a UI-only vault still sees them.
- A transport outage writes one failure row per reminder, not one per check.
- The frame is fixed text: "Reminder: <your sentence> — <when>".
- Under point 1, a catch-up for an event less than one check interval (5 min) ahead may not be
  sent; the trail says `passed`.
- A person who writes in two languages gets the digest frame in whichever they captured in last.

### Reversal criteria

A user who wants late reminders delivered rather than re-planned (evidence: complaints about a
missed reminder after an outage, not about a stale one) points at relaxing point 3. Replies in a
language the person did not write the unit in point at the reversal ADR-0022 already names.
