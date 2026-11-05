# Spec — untitled-units

> **PM-owned.** Drafted by nooma-dev from the client's rulings of 2026-11-05 because the PM asked
> for it; the PM owns this file and may rewrite it. Exact texts live in ADR-0065 point 5 and in
> the conformance tests.

Zone: America/Argentina/Buenos_Aires (-03:00). Now is Wed 2026-11-04 10:00 unless stated.

## R1 — A dated unit with no title is never said by its content

A unit with an event or due instant and no title is said as a plain sentence carrying its instant
and none of its words, on every surface that puts an instant beside an item's words: the
reminder push, the recall answer (one and many), the digest and Today.

- **Given** a unit with content "tengo reunión con Juan el 2026-11-04 a las 22:30", due today
  22:30, no title, **when** its push is built, **then** it is exactly "Acordate: tenés algo
  anotado hoy a las 22:30." (EN "Don't forget: you have something noted today at 22:30.") and
  contains neither the content nor an ISO date.
- **Given** the same kind of unit on Thu 2026-11-26 08:30, **when** recalled alone, **then**
  "Tenés algo anotado el jueves 26 de noviembre a las 8:30." (EN "You have something noted on
  Thursday 26 November at 8:30."); yesterday 9:00 reads "Tenías algo anotado ayer a las 9:00."
  (EN "You had something noted yesterday at 9:00.").
- **Given** such a unit among many, **when** listed in a recall, the digest or Today, **then**
  its line is "• Algo anotado, hoy a las 22:30" (EN "• Something noted, today at 22:30").
- **Given** an undated unit with no title, **then** it is said by its content as before.
- **Given** a unit with a title, **then** nothing changes.

## R2 — A dated unit is named

- **Given** a capture of a dated unit, or one asked to be reminded, whose model answer had no
  usable title, **when** it is stored, **then** the model is asked once for the title before the
  reminder is armed, the reminder's text is that title, and a provider failure still captures the
  unit with no title.
- **Given** a correction that clears the title of a unit that is still dated, **then** the unit
  is named against its new body.
- **Given** a title written by someone else in the meantime, **then** it is not overwritten and
  nothing is logged.
- Every title Nooma writes is in the `decision_log` with the previous (null) and next value.

## R3 — The night names

- **Given** more than 50 dated units without a title, **when** the night runs, **then** it names
  50: those with a pending reminder first (soonest first), then future instants (nearest first),
  then past (most recent first). The rest wait for the next night.
- A unit the model cannot name is logged with the reason and tried again; a provider error on one
  unit does not stop the night; with no provider the phase writes nothing.
- A titled unit, any content and any instant are never touched.
