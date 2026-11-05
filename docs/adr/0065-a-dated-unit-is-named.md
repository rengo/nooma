# ADR-0065 — A dated unit is named

- **Status**: Accepted (the client ruled this direction and its details on 2026-11-05)
- **Date**: 2026-11-05
- **Supersedes**: none
- **Superseded by**: —
- **Extends** (none of them edited): [ADR-0064](0064-replies-are-built-by-code.md) (a reply says an
  item's words beside its instant; this decides which words), [ADR-0032](0032-a-reminder-says-what-is-true-now.md)
  (a reminder says what is true now, never a stale day), [ADR-0033](0033-the-model-understands-the-code-acts.md)
  (the model understands; naming a unit is understanding)

## Context

A reply says an item's words and then the day the code renders. The words are the title, else
the content. The capture prompt asks the model to write the resolved instant into the content as
`YYYY-MM-DD` / `HH:MM`, so a dated unit with no title says its date twice, and can say a relative
word ("mañana") that stopped being true:

```
Acordate: tengo reunión con Juan el 2026-11-04 a las 22:30, hoy a las 22:30.
```

A dated unit has no title when it predates migration 0009 (which left it unnamed on purpose), when
the capture model gave none or `ResolveTitle` dropped it, and when a correction moved its date or
rewrote its body (doc 02 §5 clears the title so it cannot name a gone day). The last case is not
legacy: it happens on every date-only correction without a fresh title.

## Options evaluated

| Option | Real tradeoff |
|---|---|
| Say the content with its date token cut off | A free-text parser with per-language glue words ("el", "a las", "on"); still first person; the class ADR-0064 rejected |
| Quote the content after a generic sentence | Says what, but repeats the date and can quote a stale relative word |
| **The model names every dated unit; the code says a plain sentence when it still has no name** ✅ | One cheap model call where a title is missing and a nightly write the user did not ask for; a unit the model cannot name reads vaguer, never false |

## Decision

1. **A dated unit is named.** A unit with an `event_at` or a `due_at` and no usable title is
   given one by the model: at capture (only when the capture came back without a usable title,
   before its triggers are armed), at correction (where the title was cleared and the unit is
   still dated) and in a nightly `title` consolidation phase. The title goes through the same
   `ResolveTitle` and the same rule text as the capture prompt's `title`.
2. **A title is never overwritten.** The write only happens where the title is still NULL. A
   title a correction or the user wrote in the meantime wins; a lost race writes and logs nothing.
3. **The night is bounded and ordered.** At most `title_backfill_per_pass` (50) units per pass:
   units with a pending reminder first, then future instants, nearest first, then past instants,
   most recent first. A unit the model cannot name is tried again the next night.
4. **Every write is logged** in the `decision_log` with the previous (NULL) and next title, so
   `/ui/activity` shows it and the user can correct it. Nothing is deleted.
5. **A dated unit with no title is never said by its content.** Its reply is a plain sentence
   with the instant and no words of the unit: "Acordate: tenés algo anotado hoy a las 22:30."
   / "Don't forget: you have something noted today at 22:30."; recall "Tenés algo anotado el jueves
   26 de noviembre a las 8:30." / "You have something noted on Thursday 26 November at 8:30.";
   past "Tenías algo anotado ayer a las 9:00." / "You had something noted yesterday at 9:00.";
   a list line "• Algo anotado, hoy a las 22:30" / "• Something noted, today at 22:30". An
   undated unit with no title keeps its content, which states no instant.
6. **No reply waits for a call.** Naming happens where the gap appears (capture, correction) and at
   night, never while a reply is built (ADR-0064 point 1 stands; a title is a stored field).
7. Migration 0009's "no backfill" comment is overtaken. The file is published and stays as it is;
   doc 03's column comment is updated.

## Consequences

### What it enables

- A reply states an instant once and says nothing the vault does not hold.
- The vault's legacy units gain titles that also improve search results and the UI, with the
  units about to ring named first.

### What it costs

- The night makes an unattended model call that writes a field of the user's units, a new kind
  of nightly write beside relations and beliefs. It is bounded by the cap and visible in the log.
- Until a unit is named, or if the model never can, the user learns that something is due and
  when, not what.
- A unit the model cannot name costs one call per night, accepted over storing an attempt count
  (a column and a migration) for a case not yet seen.
