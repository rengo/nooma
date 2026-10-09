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
- [ ] 5. `make check-all`, open the PR.
