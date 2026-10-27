# ADR-0052 — Every verdict is a typed signal, from every surface

- **Status**: Proposed
- **Date**: 2026-10-27
- **Supersedes**: —
- **Superseded by**: —
- **Enables**: M5 (the learner's `learn` pass)

## Context

M5's learner tunes per-user knobs from `learning_signals` (doc 02 §9). Mapping what the vault held
on main at 5332eb5 showed three gaps.

- **No check-in verdict emitted anything.** No answer (engaged, declined, snoozed) and no expiry
  unanswered wrote a signal. The four `nudge_*` members were declared and never produced, so goal
  cadence (doc 02 §9: "learns from whether the user RESPONDED") had no input.
- **Relation signals had no type.** `relation_reject` and `relation_confirm`, from the chat answer
  and from the graph, recorded only the relation's id. The learner raises a bar *per relation type*,
  and a rejected relation is deleted, so its type survived nowhere in the signal.
- **The client deletes and does not confirm.** On 2026-10-26 they said they would not confirm links
  one by one and only delete what is wrong. Their past graph deletions are the only evidence that
  exists. On 2026-10-27 they asked that the learner use it (answer 1).

## Decision

1. **Every verdict emits its signal on every surface it can be given from** (I47). Doc 02 §9 holds
   the table of verdicts, signals, valences and surfaces. A check-in answered done or engaged
   emits `nudge_engaged`, and so does a done follow-up that answers it (I42). Drop or declined
   emits `nudge_declined`. A snooze emits `nudge_ack`. A check-in that reached the user and closed
   unanswered emits `nudge_ignored`. All four target the trigger.
2. **`nudge_ack` is neutral.** The user saw the check-in and put it off, which says neither that
   it landed nor that it missed.
3. **A relation signal is typed.** It carries `relation_type`, and its context holds the
   relation's `created_by`, the `confidence` the user judged (before a confirm raised it) and its
   `created_at`. The shared curator reads the relation before rejecting it, so a relation already
   gone emits nothing.
4. **A timer cancel emits nothing.** A timer is infrastructure, not memory (doc 02 §8), and
   cancelling one judges nothing the brain decided.
5. **The past is typed where it can be, by migration 0013.**
   - A graph deletion's signal takes its type, maker and confidence from its `relation.rejected`
     row, which recorded the whole relation before the delete.
   - A confirm's signal takes its type and maker from the relation, which is still alive.
   - The backfill only fills untyped signals and inserts no row, so it cannot duplicate or
     rewrite a signal.
   - **A deletion answered in chat before M5 is lost to the learner.** Its relation is gone, and
     its `capture.relation_checkin.resolved` row names no type. The signal stays untyped, and the
     learner does not count it.

## Options considered

- **A one-time pass at startup** instead of a migration. Rejected: migration 0008 already
  backfilled `units.language` from `decision_log` in SQL. A migration runs exactly once inside the
  runner's transaction, and its L3 test can seed a vault of the right age.
- **Inserting a signal for each `relation.rejected` row** instead of typing the existing one.
  Rejected: the curator records the signal before the delete (I10), so every such row whose delete
  went through already has its signal. An insert would duplicate it.
- **Signalling a timer cancel.** Rejected for decision 4's reason, and no knob in doc 02 §9 or §13
  would read it.

## Consequences

### What it enables

The `learn` pass (M5 link 2) can count rejections and confirmations per type, restricted to
Nooma's own relations, from the first night it runs. The client's past graph deletions count. Goal
cadence has responses to learn from once goal check-ins are delivered (M5 link 4).

### What it costs

Two signal rows per check-in at most, and one extra relation read per rejection.
`NewCheckService` gains a parameter, so every caller spells out its signal store, and `nil`
records none.

### Reversal criteria

- A verdict the learner should hear that this table does not list.
- Evidence that `nudge_ack` carries a polarity: snoozes that predict a later engage or a later
  ignore strongly enough to tune on.
