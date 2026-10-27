# ADR-0054 — A goal check-in is answered by naming it

- **Status**: Proposed
- **Date**: 2026-10-28
- **Supersedes**: [ADR-0044](0044-a-chat-answer-names-the-item-it-is-about.md), in part: the
  kinds of thing a chat answer can name. A goal check-in is named by its own listed position,
  never through `referent`
- **Superseded by**: —
- **Enables**: M5 (goal check-in delivery, link 4b; goal cadence learning, link 5)

## Context

Doc 02 §7 promises a goal check-in ("¿Seguís con correr una media maratón?") and §9 learns
goal cadence from whether the user answers it. Neither works without the other: a check-in
nobody can answer is recorded as ignored, and ignored is exactly what goal learning tunes on.

ADR-0044 binds a chat answer to a check-in through `referent`, the letter of a listed item, and
every listed item is a unit. A goal is a belief, so a goal check-in hangs off no unit and has no
letter. In Telegram, where every message is read beside its chat, "sí" or "dejala descansar"
could never bind to it.

On 2026-10-28 the client rejected a positional rule ("the next message answers it") as too
blunt. The model already sees the last 20 turns and only lacks a way to point at a goal. "sí,
ayer salí a correr 5 km", three messages after the question, must still answer it.

## Decision

1. **Beside a chat, the open goal check-ins are listed in the prompt**, by position, under their
   own heading: what Nooma asked, and nothing else about the goal. With none open, or read
   alone, the prompt is unchanged.
2. **A typed answer names one.** `goal_target` is the listed position (or `unclear`) and
   `goal_outcome` is `engaged`, `declined` or `snooze`. The code binds only a position it
   listed (`classify.ReadGoalAnswer`), so the model cannot invent a goal to close.
3. **Only its answer names a goal.**
   - A goal is never a `referent`, so a correction, a done, a remind-me or leads cannot land on
     one, and a correction or a capture never writes to a goal.
   - The generic check-in answer (`nudge_outcome`, `task_checkin_outcome`) never closes a goal
     check-in.
   - A goal answer that binds is the message's only check-in answer.
4. **The outcomes.**
   - `engaged` closes the check-in (`nudge_engaged`) and the goal stays.
   - `declined` closes it (`nudge_declined`) and the goal stays, never retired (client answer 5
     of 2026-10-27). The next check-in for that goal waits twice `goal_stagnation_days` (link
     4b arms it).
   - `snooze` leaves it open (`nudge_ack`).
   - Unanswered, it closes at the third morning like any check-in about no unit
     (`nudge_ignored`).

## Options considered

- **A positional rule**: a short answer naming nothing, right after the goal's push, answers it.
  Rejected by the client: it misses "sí, ayer salí a correr" three messages later, and would need
  the chat to remember which push asked what.
- **Goals as lettered items beside the units.** Rejected: a letter is what a correction, a done
  and a remind-me bind to, so a goal would become something a correction could land on. Its own
  numbered list and field keep that impossible by construction.

## Consequences

### What it enables

Goal check-ins can be delivered (link 4b) and answered from Telegram, and goal cadence (link 5)
learns from answers rather than from silence the system caused.

### What it costs

Two classify fields, a prompt section shown only while a goal check-in is open, and new
phrasings (`classify-goal-*`) to record. Every other recording's prompt is unchanged.

### Reversal criteria

- The model naming a goal for messages that do not answer it, often enough that the recorded
  set's goal phrasings stop reading as expected.
- A user wanting to retire a goal from the check-in itself, which `declined` deliberately does
  not do.
