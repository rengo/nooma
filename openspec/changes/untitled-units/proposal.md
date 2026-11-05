# Proposal — untitled-units: a dated unit is named, and said without repeating its date

> **PM-owned.** Drafted by nooma-dev from the client's rulings of 2026-11-05 because the PM asked
> for it; the PM owns this file and may rewrite it.

## Why

On the production vault a reminder reads:

```
Acordate: tengo reunión con Juan el 2026-11-04 a las 22:30, hoy a las 22:30.
```

A dated unit with no title is said by its content, which already carries the date (in ISO form,
or as a relative word that may no longer be true), and the reply appends the date again. Units
lack a title because they predate migration 0009, because the model gave none, and because a
correction clears the title when it moves the date (doc 02 §5), which happens on every date-only
correction.

## The client's rulings (2026-11-05)

- ADR-0065 "A dated unit is named" is accepted: the model names dated units without a usable
  title, at capture, at correction and in a nightly `title` phase; it never overwrites a title;
  every write is logged.
- Until a unit is named, a dated unit with no title is said as a plain sentence with its instant
  and none of its words (texts in ADR-0065 point 5).
- The night names past units too, future first.

## What changes

1. Replies (push, recall, digest, Today) never say the content of a dated untitled unit, so a date
   is said once. Ships first.
2. A capture that came back without a usable title, and a correction that cleared one, name a
   still-dated unit with one extra model call; a failure leaves it as it was.
3. A nightly phase names up to 50 dated untitled units per pass, pending reminders first, then
   future, then past.

## Non-goals

Undated units are untouched. No existing title is rewritten. No reply waits for a model call. No
schema change.

## Cost

The user may read "algo anotado" instead of what the unit is until it is named. One extra call at
capture and correction only when the title is missing; up to 50 calls a night.
