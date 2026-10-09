# Spec — fix-unit-correction-form

States what MUST be true after this change, in testable form. `design.md` says how.

## R1 — An explicit referent makes the capture a correction

A capture that carries an explicit referent (the unit page's correction form, or `POST /capture`
with `unit_id`) MUST be handled as a correction of that unit regardless of the classification's
`type`. Classification still runs, for the corrected value.

- It MUST NOT persist a unit, arm a trigger or timer, judge or write a relation, or resolve a
  check-in (a nudge, the load hypothesis or a relation question).
- It MUST answer `corrected` or `asked`, never `stored`, `armed`, `recalled` or `conversed`.

**Scenario: a standalone-event text corrects the unit's date.** Given an `event` unit with
`event_at` 2026-10-16T10:00Z, when the text "en realidad es hoy viernes 9 a las 10" arrives with
that unit as referent and the model classifies it as `event` with `event_at`
2026-10-09T10:00Z, then the unit's `event_at` is 2026-10-09T10:00Z, the vault still holds one
unit, no trigger and no relation were written, and the decision log holds exactly one
`correction.applied` row whose `previous.event_at` is 2026-10-16T10:00Z and `next.event_at`
2026-10-09T10:00Z.

## R4 — An unknown referent is a 404

A correction naming a unit that does not exist MUST answer 404 with a plain message on
`POST /capture` (`unit_id`) and on `POST /ui/units/{id}/correct`, never 500. Nothing is
written.

## R2 — Nothing to extract is an ask, and the page says so

When the classification resolves no edit (doc 02 §5 step 4: two dates, or neither a date nor
content), the unit MUST stay unchanged, the only row written MUST be `correction.ambiguous`, and
the UI MUST tell the user in plain words that nothing was changed and what to write instead.

**Scenario.** The same referent with a classification carrying both `event_at` and `due_at`
answers `asked`, leaves the unit as it was, writes one `correction.ambiguous` row, and the page
reads "Nothing was changed".

## R2a — A text that is not a change asks

With an explicit referent, when the classification's type is `recall`, `chitchat`,
`out_of_scope` or `timer` (the kinds that are neither memory nor a correction), the unit MUST
stay unchanged — including its content — the only row written MUST be `correction.ambiguous`
(reason `not_an_edit`), and the UI MUST say that nothing was changed because the text read as a
question or a request. Every other type, and `correction`, keeps applying.

**Scenario.** "when is this?" sent with the unit as referent and classified as `recall` answers
`asked`, the unit's content and `event_at` are unchanged, and one `correction.ambiguous` row
carries `{reason: not_an_edit, unit_id, kind: recall}`.

## R3 — Free capture is unchanged

A capture with no explicit referent MUST behave exactly as before: the same `event`
classification persists a new unit and arms its trigger; a `correction` classification still
resolves its referent through recall and the margin gate.

**Scenario.** The R1 classification with no referent persists a second unit and arms one
trigger.
