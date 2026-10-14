# ADR-0032 — A reminder says what is true when it is sent, in the user's language

- **Status**: Proposed
- **Date**: 2026-10-14
- **Supersedes**: —
- **Superseded by**: —
- **Extends**: [ADR-0022](0022-reply-language.md) (its "anything Nooma says first" gap),
  [ADR-0030](0030-event-reminders-are-pushed-and-respect-the-night.md) point 4 (never late)

## Context

On 2026-10-14 the client enabled Telegram on a vault that had run without a channel for five
days. The first digest read "Here are 7 things for today", listed three flights that had already
departed, said "09:00" for a flight the user had corrected to 08:00, listed one flight twice, and
asked its relation question in English to a user whose every reply comes back in Spanish.

Four decisions were missing, not one:

1. **When a reminder is checked against the world.** A trigger is judged once, when it moves
   `armed → fired`. A fired trigger nobody could deliver (no channel, `nooma check` run by hand,
   a failed send) waits in `Undelivered`, and the digest carries everything there without asking
   whether its event has passed.
2. **What a reminder says.** The text is `payload.action`, the user's sentence frozen when the
   trigger was armed. A correction of the unit's text does not reach a trigger already fired.
3. **What happens to a backlog.** Nothing decides it: whatever piled up is sent at once.
4. **Which language Nooma speaks first.** ADR-0022 named the gap: the digest and a pushed
   reminder have no message to read a language from, so they speak the fallback, English.

## Options evaluated

| Question | Option | Verdict |
|---|---|---|
| Backlog | Send every undelivered reminder whose event is still ahead, late | Rejected: ADR-0009 expires a trigger overdue past its window rather than send it out of context, and a five-day-old "you have a flight" is that |
| Backlog | Expire it and send nothing | Rejected: the user was never reminded of tomorrow's dentist |
| Backlog | **Expire what is late; re-plan the event's leads still ahead** | Chosen: nothing is sent late, and the user gets exactly the reminders a capture made now would get |
| Text | Re-word the frozen text with a model at delivery | Rejected: a model call per digest line, and a correction already gives the current text for free |
| Text | **Render from the unit as it is now: its text and its instant, in a fixed frame** | Chosen |
| Language | A `language` setting | Rejected for ADR-0022's reasons |
| Language | The language of the user's last message, read from the audit trail | Rejected: `decision_log` is an audit, and a behaviour reading it for state couples the two |
| Language | **The language is stored on the unit at capture**; a reminder speaks its unit's, the digest speaks the newest unit's | Chosen: the shape ADR-0022 itself named |

## Decision

1. **Every delivery attempt is judged, not only the first.** A reminder is sent only while its
   event is still ahead, the unit still has that event (a correction to another instant
   supersedes it), and it is no later than ADR-0009's window. Otherwise it **lapses**: it is
   never sent, and the trail says why. This applies to the pushed path and to the digest alike.
2. **A one-shot reminder that could not be pushed is pushed again on the next pass**, while (1)
   holds. It does not fall back to the digest: ADR-0030 already says a reminder that waits for
   tomorrow's digest arrives after the thing it is about.
3. **A backlog is re-planned, not replayed.** When a reminder lapses and its event is still
   ahead with no other reminder armed for it, the event gets the reminders the user's leads still
   have ahead, by the same rule a corrected date uses (no at-once reminder). A reminder counts as
   sent only once it reached the user.
4. **A reminder says the unit as it is now**: its current text and its current instant, framed as
   a reminder ("Recordatorio: … — jue 15 oct, 11:00"). One unit is one line, however many of its
   triggers are due together.
5. **What Nooma says first speaks the user's language.** The classification's language is stored
   on the unit. A pushed reminder speaks its unit's language; the digest (header, lines' frame,
   relation question) speaks the language of the most recently captured unit that has one; none
   known, the fallback. The glass box stays English (ADR-0022 point 4).

## Consequences

### What it enables

- A vault can run for weeks without a channel and the first message it sends is still true.
- A correction reaches every reminder not yet delivered, not only armed ones.

### What it costs

- A migration (`units.language`), backfilled from the `capture.classify` rows that already
  recorded each unit's language.
- A due scan now reads the units of the reminders it delivers, one read per reminder.
- The frame is fixed text; a reminder reads as "Reminder: <your sentence> — <when>", not as a
  sentence written for the occasion.
- A person who writes in two languages gets the digest frame in whichever they captured in last.

### Reversal criteria

A user who wants late reminders delivered rather than re-planned (evidence: complaints about a
missed reminder after an outage, not about a stale one) points at relaxing point 3. Replies in a
language the person did not write the unit in point at the reversal ADR-0022 already names.
