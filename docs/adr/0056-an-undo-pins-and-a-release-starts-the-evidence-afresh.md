# ADR-0056 — An undo pins, and a release starts the evidence afresh

- **Status**: Accepted (by the client through the PM, 2026-10-28: undo restores the previous
  value and pins it; a release lets Nooma learn again, counting only what comes after it)
- **Date**: 2026-10-28
- **Supersedes**: —
- **Superseded by**: —
- **Enables**: M5 (the summary page and API, link 6b)

## Context

Doc 02 §9 promises that what the learner learns is queryable and correctable. ADR-0053 and
ADR-0055 left pinning as the user's word and deferred releasing a pin to the summary. Two
questions remained: what "correct" does, and what happens to the evidence when the user gives a
value back.

## Decision

1. **Undo restores the value before the learner's last change, and pins it.** A user who undoes
   has decided, and an unpinned undo would be redone by the same verdicts on the next night. It
   is offered only while that change's value is still in force and not pinned.
2. **Release unpins the value as it stands and stamps the release as its last change.** The
   rules count only verdicts after a change and wait their cooldown from it (ADR-0053,
   ADR-0055), so a released value is moved only by what the user does from then on. The old
   verdicts the user already overruled cannot move it the next night.
3. **One sentence per value, built from the decision rows.** The row's structured context
   (previous, next, counts) is read, never a stored sentence, so the wording can change without
   rewriting history.
4. **Nothing is deleted.** Both acts write their row after the act lands, a refusal writes
   nothing, and no signal or decision row is removed.

## Options considered

- **Undo without a pin.** Rejected: the learner would redo it on the same evidence.
- **Release that keeps the old evidence.** Rejected: the value would move again the next night
  on the verdicts the user had just set aside.

## Consequences

### What it enables

The summary page and `GET /learning` (link 6b), and a way back from every pin.

### What it costs

After a release a value waits a full cooldown and fresh evidence before it moves.

### Reversal criteria

- A user who releases expecting the learner to act on the past straight away.
