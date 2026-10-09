# Proposal — fix-unit-correction-form: the unit page's correction form corrects that unit

## Why

Observed by the client on a real vault (gpt-4o-mini), 2026-10-09:

1. `nooma capture "dentista el viernes a las 10"` stored an event unit for 2026-10-16T10:00Z and
   armed its trigger.
2. On that unit's page, the correction form submitted "en realidad es hoy viernes 9 a las 10".
3. The decision log shows `capture.classify` ("classified as \"event\" and persisted a pool
   unit"), a `relation.persisted` `date_reference` back to the original, and
   `capture.armed.trigger` for 2026-10-09T10:00Z. No `correction.applied`. The original unit is
   unchanged; a second unit and a second trigger now exist.

`docs/07-functional.md` promises "a correction form whose target is the unit you are looking
at, so the UI never has to guess a referent". The form passes the referent, but the pipeline
only honours it when the model independently classifies the text as `correction`. A text that
states the corrected value ("it is today, Friday the 9th, at 10") reads standalone as an
`event`, so the form silently becomes a second capture.

## What changes

1. Text submitted through a unit page's correction form is always handled as a correction of
   THAT unit, whatever the model would classify it as standalone. It never creates a new unit,
   relation or trigger for a new unit.
2. The unit's changed field updates (e.g. its event date), following doc 02 §5 step 4's
   one-field rule.
3. Activity shows a `correction.applied` row with previous → next.
4. If the model cannot extract a change from the text, the page says plainly that nothing was
   changed and why, and nothing is written except the `correction.ambiguous` row doc 02 requires.
5. Free capture (CLI, API without `unit_id`, `/ui/capture`) behaves exactly as before, including
   its own correction detection.

## Out of scope

- Moving an existing trigger when a correction moves its unit's date. Doc 02 does not say a
  trigger follows a corrected date today; deciding that it should is a product change to doc 02
  §5 step 4 / §7 and is raised to the PM as an open question.
