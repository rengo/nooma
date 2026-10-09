# Design — fix-unit-correction-form

## 1. Root cause

`internal/ui/capture.go` `serveCorrect` already sets `brain.CaptureInput.ReferentID` from the
path. But `internal/brain/capture.go` only reaches the correction path at its
`*c.Kind == classify.KindCorrection` fork, and `CaptureInput.ReferentID` was documented as
"meaningful only when the classification resolves to classify.KindCorrection" (m1c spec R1.5).
Every other kind ignores it: an `event` classification passes the arming fork, persists a unit,
judges relations against recall candidates (the original unit becomes a `date_reference`) and arms
a trigger. That is exactly the client's decision log.

## 2. What the docs say

- Doc 02 §5 step 4: "A caller holding an identifier passes it, and that identifier wins — the UI
  and the API both have one." It settles *which* unit; it does not say what happens when the
  model types the text as something other than `correction`.
- Doc 02 §5 step 1: `correction` is told apart from the other types "by what the message does".
  A message sent through a unit's correction form has already said what it does.
- Doc 07: "a correction form whose target is the unit you are looking at, so the UI never has to
  guess a referent."
- ADR-0016 (pre-image) is unaffected: the same `applyWithPreImage` runs. No ADR is superseded.

The fix fills the gap doc 02 left, in the direction step 4 and doc 07 already describe, and adds
that sentence to doc 02 §5 step 4 in the same PR, plus invariant I28.

## 3. Decision: the referent decides the fork, in brain

`captureRunner.at` routes to the correction path immediately after decoding (and stamping the
reply language) whenever `in.ReferentID != ""`. Placed before check-in resolution and before
`prospection.Arm`, so none of those side effects can happen.

Alternatives rejected:

- **A separate `CaptureService.Correct` method.** It needs its own clock read, and
  `brain_single_clock_read_test.go` allows exactly one `Now()` in the package. The fork is one
  `if`, not a second entry point.
- **A `Correct bool` beside `ReferentID`.** Four combinations for one meaning; the referent alone
  already states the intent. `ReferentID` has no other use.
- **Overwriting `c.Kind` to `correction` in the UI or before routing.** The UI cannot see the
  classification, and rewriting the model's answer hides what it said.

Consequence, stated: `POST /capture` with `unit_id` now also always corrects that unit. Its
comment said the id was "ignored unless the classification resolves to a correction"; doc 02
step 4 puts the API and the UI on the same footing, and a caller sending a unit id has no reason
to want a new unit. Without `unit_id` the API is unchanged.

## 3a. A text that is not a change (PM ruling on review note 4)

`correction.IsEdit(k)` (pure, `internal/core/correction`) is false for exactly `recall`,
`chitchat`, `out_of_scope` and `timer`. It is derived, not listed: those are the kinds whose
`UnitType` maps to nothing, minus `correction`, so a later kind lands on the side its memory
mapping puts it, and `TestIsEdit` sweeps `classify.AllKinds` so the table must name it.
`correctionRunner.at` checks it after the referent resolves and before `PlanEdit`; only the
explicit path can reach it with such a kind. It writes one `correction.ambiguous` row
`{reason: not_an_edit, unit_id, kind}`.

## 3b. Unknown referent

`resolveReferent` wraps `ports.ErrUnitNotFound` in `brain.ErrUnknownReferent`. `POST /capture`
answers 404 `{"error": "no unit has that id"}` and the form answers 404 "No unit has that id."
Both adapters already import `brain`; neither needs `ports` for this.

## 4. The "nothing changed" message

`brain.Correction.Why` (`brain.AskReason`: `referent_ambiguous`, `plan_ambiguous`,
`not_an_edit`, the same strings the `correction.ambiguous` row carries as `reason`) tells the
asks apart. `captureOutcome` renders each in plain words instead of "That correction was
ambiguous.":

- not an edit: "Nothing was changed: that read as a question or a request, not a change. Write
  the new value, a date or the new wording."

- referent ambiguous: "Nothing was changed: that looked like a correction, but it was not clear
  which entry it meant. Open the entry and use its correction form."
- plan ambiguous: "Nothing was changed: it was not clear what to change. Write the one new value,
  a date or the new wording."

The corrected outcome keeps its sentence and gains a link to the unit page.

## 5. Tests

- L2 `test/conformance/i28_explicit_referent_is_correction_test.go`, written first: the R1, R2 and
  R3 scenarios over memrepo with a fake provider replaying cases written to a temp dir (not the
  shared `testdata/llm/cases` corpus, which `nooma doctor` also sends live).
- `internal/ui/capture_test.go`: both ask sentences render.
