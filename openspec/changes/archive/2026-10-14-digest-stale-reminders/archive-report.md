# Archive report — `digest-stale-reminders`

**Closed**: 2026-10-14. **`main` at archive**: `4569c73`, the merge of PR #354. **Verify**: not
re-run for this archive; every PR below is merged, and `tasks.md` has 13 ticked items and none
open. The three follow-ups below shipped in #354.

## What shipped

ADR-0032 (Accepted 2026-10-14): a reminder says what is true when it is sent. It is never sent once
its event is past, a late or superseded one is re-planned, a vault with no destination keeps its
reminders and an event whose reminders all lapsed gets one catch-up. A failed push is retried
within bounds. A reminder says its unit's text and instant now, one message per unit, in the
user's language.

| PR | Branch | What |
|---|---|---|
| #339 | `plan/digest-stale-reminders` | ADR-0032 Accepted, design, tasks |
| #340 | `fix/never-remind-of-the-past` | link 2: `passed`, `Lapse`, `FiredForUnit` |
| #343 | `fix/late-reminders-held` | link 3a: judge and hold, retire only `passed` rows |
| #344 | `fix/late-reminders-replan` | link 3b: re-plan, then retire the late and superseded |
| #348 | `fix/no-destination-and-catch-up` | link 4: no destination, the catch-up |
| #351 | `fix/push-retries-undelivered` | link 5: the bounded retry |
| #352 | `fix/reminder-renders-the-unit` | link 6: render from the unit, one message per unit |
| #353 | `feat/proactive-language` | link 7: `units.language`, the sentences in the user's language |
| #354 | `fix/proactive-followups` | the three follow-ups below |

## Follow-ups (PM decisions, shipped in #354)

- A timer's wording follows the newest unit's language; the doc 02 §5 exception is removed.
- A digest over 4096 UTF-16 units drops its last lines, ends with "and N more" / "y N más", keeps
  its relation question and leaves the dropped reminders undelivered for tomorrow. `DigestCarried`
  and the `check.digest.sent` row count only what was sent; the row mentions lines left for tomorrow
  only when some were.
- Today's lines render from the unit, in the newest unit's language. Today is a page, so it lists
  every waiting reminder; the bound applies to the digest message alone.

## Repointed

`design.md`'s link to ADR-0032 now climbs four levels (`../../../../docs/adr/`). Status docs:
`docs/07-functional.md` no longer says the rest of ADR-0032 is pending. `docs/README.md` and
`CLAUDE.md` do not mention the change. `openspec/changes/remind-me-reminders/design-repeating.md`
names it in prose only.

## Leaves behind

- A dry run's digest count can differ from the real pass's only on a low-energy morning (Carry
  returns its items untouched otherwise) whose kept lines run past the bound: the dry run reads no
  focus adjacency (doc 02 §7).
