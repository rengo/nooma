# ADR-0035 — What Nooma says first speaks the language the user usually writes in

- **Status**: Accepted (by the client, 2026-10-16)
- **Date**: 2026-10-17
- **Supersedes**: [ADR-0032](0032-a-reminder-says-what-is-true-now.md) **in part**: point 5's
  "the digest … speaks the language of the most recently captured unit that has one". The rest
  of point 5 stands: a pushed reminder speaks its unit's language, none known is the fallback,
  and the glass box stays English.
- **Superseded by**: —

## Context

ADR-0032 had the digest speak the newest unit's language. The days-pass gate showed what that
costs. A user writing to Nooma in Spanish sent one English message ("Remind me to call Ana at
5pm"), and every digest after it asked again in English: `Asking again: • Is "Llamar al médico"
done?`. ADR-0032's own consequences named the risk: "A person who writes in two languages gets
the digest frame in whichever they captured in last." On 2026-10-16 the client ruled that the
digest, re-asks included, speaks the language the user usually writes to Nooma in.

## Options evaluated

| Option | Real tradeoff |
|---|---|
| The newest unit's language (ADR-0032) | One message in another language flips every later digest |
| A `language` setting | Deterministic, but one more thing to configure, and ADR-0022 already rejected it for replies |
| Word lists over the user's messages | ADR-0033 rules them out: the model reads the words |
| **The most frequent among the recent units' languages, a tie going to the newest** ✅ | Reads only what the model already wrote per capture (`units.language`). One window number to calibrate |

## Decision

1. **The usual language** is the most frequent language among the
   `usual_language_window` (20) most recently created units that have one, archived or not. A tie
   goes to the language written most recently (`classify.UsualLanguage`).
2. **It is what Nooma says first in**, wherever ADR-0032 point 5 said "the newest unit's": the
   digest (header, lines' frame, relation question, re-asks), a reminder whose own unit knows no
   language, a timer's wording, and Today.
3. **Unchanged:** a pushed reminder speaks its own unit's language; a reply speaks its message's
   (ADR-0022); with no language known, the fallback; the glass box stays English.

## Consequences

- One capture in another language no longer flips the digest. A real switch is followed once
  the new language is the more frequent among the 20 newest units.
- `ports.UnitRepo.LatestLanguage` becomes `RecentLanguages(limit)`, and the decision is in core.

### Reversal criteria

A user who writes in two languages about equally sees the digest alternate between them. That
points at a setting, or at weighting by recency.
