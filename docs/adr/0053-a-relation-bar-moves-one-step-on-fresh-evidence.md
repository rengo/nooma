# ADR-0053 — A relation bar moves one step on fresh evidence, never on a pinned type

- **Status**: Proposed
- **Date**: 2026-10-28
- **Supersedes**: —
- **Superseded by**: —
- **Enables**: M5 (the `learn` pass, link 2c; the summary, link 6)

## Context

Doc 02 §9 promises that repeated rejections of a relation type raise that type's
`min_confidence_to_persist` for this user ("6 of the last 8 `same_topic`", 0.30 → 0.35). It names
no window, step bounds, cooldown or minimum evidence, and M5 has to choose them.

The client's real use shapes the choice. On 2026-10-26 they found many low-value links on the
graph and said they would not confirm links one by one: they delete what is wrong. So deletions
are the main signal, and a rule that waits for confirmations would never move for them. From
ADR-0052 on, every verdict is a signal typed by the relation's type, carrying who made the
relation.

## Decision

1. **The evidence is a count of verdicts.** The rule reads the newest 8 `relation_reject` /
   `relation_confirm` signals on the type's relations. Only relations Nooma made count
   (`created_by` system or consolidation), and only verdicts made strictly after the type's last
   change. A relation that survived is not counted as approved.
2. **Six of eight move the bar one step.** Six rejections raise persist by 0.05, six
   confirmations lower it by 0.05, and anything between holds. Six is a majority of eight, so one
   sample can move the bar one way only.
3. **Bounds.** Learning keeps persist on a hundredth within [0.20, 0.90]. A step that would leave
   it unchanged, or move it the wrong way at a bound, is no change. Surface rises to persist when
   persist would pass it, and learning never lowers surface.
4. **Hysteresis and cooldown.** After a change, the verdicts it counted never count again, and the
   type is not judged for 7 days. A night of twelve deletions moves the bar one step, and a second
   step needs a week and six fresh verdicts.
5. **The user's word wins.** A type the user pinned is never written. The pin itself, and undoing
   a learned value, belong to the summary (M5 link 6). The rule only honours a pin.
6. **Pure and idempotent.** `internal/core/learning.StepRelation` decides from its inputs. The
   nightly `learn` slot (link 2c) reads the signals and the type's state, applies a change, records
   it at the pass's instant with one `decision_log` row carrying the counts and the signal ids, and
   so cannot apply it twice.

## Options considered

- **A rejection rate over every relation the type ever produced, survivors counted as
  approvals.** Rejected. Nothing records whether the user saw a relation, and viewing writes
  nothing, so unseen relations would count as approvals. A type the nightly pass produces in bulk
  would then never move.
- **A step proportional to how far the rejected confidences sit above the bar.** It moves faster,
  but one night could swing a type from asserted to never stored, which is exactly what this
  decision prevents. It is also unnecessary for the case that prompted it: the client's
  `same_date` links are stopped at the judge (link 0's rule, recorded in doc 02 §4), not learned
  away a step at a time.
- **No ceiling, so a type can be learned out of existence.** Rejected. Switching a type off is a
  decision the user can make by pinning a high bar, and a count should not make it for them.
- **Re-judging or deleting existing relations when a bar rises.** Rejected: nothing is deleted
  without the user. A relation below its type's new bar is dormant at read time (M5 link 3,
  client answer 2): hidden, kept, and back if the bar falls.

## Consequences

### What it enables

The M5 demo: rejecting six `same_topic` links raises that type's bar 0.30 → 0.35, and the
`decision_log` row explains it ("6 of the last 8"). A user who confirms gets a bolder type in the
same way.

### What it costs

The bar moves slowly. From 0.30 to the 0.90 ceiling takes twelve changes, so at least twelve
weeks. A user who only deletes moves bars up and never down. Six numbers join §13's calibration
table.

### Reversal criteria

- Real use showing that one week is too short for a new bar to show its effect, or too long for a
  user to see Nooma listen.
- A type the client keeps deleting at the ceiling, which would argue for letting a count switch
  it off.
