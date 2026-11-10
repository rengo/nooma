# Design — fix-recurring-referent-rearm

## Root cause (confirmed by probe)

A recurring reminder's unit is stored as an `event` (`classify.KindRecurringReminder.UnitType`)
whose `event_at` is the anchor day; the rule lives only on its trigger row. Two paths read that
unit back and planned its anchor as a one-shot instant:

1. **Referent remind-me** (`brain.remindReferentOnly`): `asCapture` rebuilds the unit as an
   `event`, `askedOnly` turns it into a task due at the anchor, and `prospection.Arm` refuses it
   `already_past`. The live 2026-11-10 report, reproduced by
   `TestI40_ARemindMeOfARecurringReminderSaysItAlreadyDoes` before the fix.
2. **Correction of an asked unit** (`correctionRunner.followReminder` -> `prospection.FollowAsked`):
   `Follow` already kept a live recurrence, `FollowAsked` did not. Moving an asked daily reminder
   to 22:00 carried a 2-hour one-shot lead onto the recurring row (`fire_at` 20:00, anchor left at
   21:30, so the move was lost after one ring) and armed a second, asked one-shot. A recurring
   unit captured with `remind_request` ("recordarme ... todos los días") is asked.

## Decisions

- **D1. The live recurring trigger is the source of truth.** `remindRecurring` runs before the
  one-shot path: a unit with an armed trigger carrying a rule and an anchor is marked asked and
  answered from that row; nothing is armed, refused or logged. A recurring unit with no live
  recurring trigger keeps today's path: its rule is gone.
- **D1b. The answer names when the row actually rings** (PM ruling on review of #450).
  `prospection.QuietHours.NextRing` runs the due scan's own `ReminderVerdict` on the row in the
  user's night: pending rings at its instant, or quiet end when the night holds it (unless
  `night_exempt`); deferred rings at quiet end; deliverable goes out at the next pass, named as
  now; stale is replaced by `Rearmed`, as the scan arms the successor, and judged again; a
  successor that lapses too (a missed day whose today's occurrence is also overdue) gives way to
  the first occurrence after now, which is ahead. Two explicit steps, no loop bound. The
  rule is said at the occurrence's clock (`Armed.About`), the ring is `Armed.FireAt`.
- **D2. `FollowAsked` keeps a live recurrence** through the same helper `FollowSent` uses
  (`followRecurring`), so both re-anchor by `recurringTrigger`, the function capture arms with.
- **D3. Code-built answer.** `Armed.Already` marks the result; `phrase.Set.AlreadyEvery` says
  "Eso ya te lo recuerdo <rule>; el próximo aviso es <Dated next>." `EveryDay..EveryYear` became
  rule fragments framed by `EveryNew` (the old sentences, unchanged) or `EveryAlready`.
- **D4. One-off past items are untouched** (client decides later); pinned by
  `TestI40_ARemindMeOfAOneOffWhoseTimePassedStillSaysSo`.

## Class sweep (probes run on this branch)

| Site | Probe | Result |
|---|---|---|
| `remindReferent` | conformance, 4 cases es/en | fixed (D1) |
| `moveThenRemind` -> correction | conformance, remind-me + move | fixed (D2) |
| plain correction moving `event_at` | conformance, move alone es/en | fixed (D2); before: row at 20:00, stray asked one-shot |
| leads follow-up on a recurring item | throwaway probe | trigger kept daily 21:30; reply "Ya no queda ningún aviso antes de esa hora." (pre-existing, reported) |
| nightly naming retext | throwaway probe | `fire_at`, rule, anchor unchanged |
| preferences re-arm | read | skips units that recur (`recurs`) |
| due-scan re-plan | read | only stale one-shot rows; recurring rows excluded from `live` |
| delivery re-arm | read | `prospection.Rearmed` by rule |
| done follow-up | read | `DoneCloses` with `recurring` |
