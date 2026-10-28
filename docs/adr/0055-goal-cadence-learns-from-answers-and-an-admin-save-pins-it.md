# ADR-0055 — Goal cadence learns from answers, and an Admin save pins it

- **Status**: Proposed
- **Date**: 2026-10-28
- **Supersedes**: —
- **Superseded by**: —
- **Enables**: M5 (the summary of what was learned, link 6)

## Context

Doc 02 §9 promises that goal check-in cadence learns from whether the user responded: "5 nudges
with no response are 5 ignored ones", systematically ignoring lengthens the interval
(21 → 28 days) and engaging shortens it, with a cooldown so a change can show its effect. It
names no other number.

Since M5 links 4a and 4b, goal check-ins are delivered and answered (ADR-0054), and every one
that closes emits `nudge_engaged`, `nudge_declined` or `nudge_ignored` naming the goal's belief.
The interval is `config.goal_stagnation_days`, also set from /ui/admin, and on 2026-10-27 the
client said an Admin save, even of the value already in force, pins it (answer 6).

## Decision

1. **The evidence** is the newest five goal check-ins closed strictly after the last change. A
   snooze closes nothing and does not count.
2. **All five ignored lengthens** `goal_stagnation_days` by 7 days. **Three or more engaged
   shortens it** by 7. A decline is an answer: it neither lengthens nor shortens, because the
   decline already makes that one goal wait twice the interval (ADR-0054). Anything else, or
   fewer than five, holds.
3. **Bounds and cooldown.** The interval stays within [7, 56] days. After a change the rule
   waits the new interval itself, from `learning_state.goal_calibrated_at`. The change and its
   stamp are written in one transaction at the pass's instant, so the rule is idempotent.
4. **The user's number wins.** An Admin save, unchanged or not, sets
   `config.goal_stagnation_pinned`, and the learner never writes a pinned value. The store
   refuses it too, so a save between the learn pass's read and its write still wins.
   Unpinning belongs to the summary (link 6).
5. **The learned value lives in `config.goal_stagnation_days`**, the column every reader, the
   decline factor included, already reads. `calibration` stays for knobs with no column.

## Options considered

- **Counting a decline as ignoring.** Rejected: the user answered, and "dejala descansar"
  means that goal less often (ADR-0054), not every goal.
- **Storing the learned value in `calibration`**, with `config` as the user's only. Rejected:
  every reader would need a precedence rule between two homes; a pin flag separates them.

## Consequences

### What it enables

A user who ignores goal check-ins is asked less often, one who answers more, and one who sets
the days herself keeps them, each change a `consolidate.learn.goal_cadence` row.

### What it costs

Learning is slow: one change needs five closed check-ins, then a whole interval. A migration adds
the pin, and `ports.ConfigRepo` one write beside the Admin's five.

### Reversal criteria

- Real use where five closed check-ins take so long that nothing ever moves.
