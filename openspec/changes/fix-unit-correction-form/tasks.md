# Tasks — fix-unit-correction-form

One PR, branch `fix/ui-unit-correction-targets-unit`.

- [x] 1. RED: `TestI28_ExplicitReferentIsAlwaysACorrection` (R1, R2, R3) and the I28 row in
      `docs/06-harness.md` §4. Watch R1/R2 fail for the right reason (a second unit, a trigger).
- [x] 2. GREEN: `captureRunner.at` forks on `in.ReferentID` before check-ins and arming; one
      helper shared with the `KindCorrection` fork. `CaptureInput.ReferentID` and the httpapi
      `unit_id` comments state the new meaning.
- [x] 3. Doc 02 §5 step 4: the identifier also settles that the message is a correction.
      Doc 07 item 6 follows.
- [x] 4. UI: plain "Nothing was changed" sentences for both asks; regenerate templ; UI test.
- [x] 5. `make check-all`, open the PR (#302).

Rework after review of 2f5b549:

- [x] 6. I28 subtest: an answer-shaped classification beside a referent resolves no open
      check-in; fork-after-`resolveRelationCheckIn` mutation goes red. Pre-image dates asserted.
- [x] 7. R2a: `correction.IsEdit`, `brain.AskReason`, the not-an-edit ask, doc 02 sentence, UI
      sentence.
- [x] 8. UI test: the corrected outcome links to the unit page.
- [x] 9. R4: unknown referent answers 404 on the API and the form.
- [x] 10. `make check` runs `test/conformance` (`make test` is `go test ./...`): confirmed, no
      change.
