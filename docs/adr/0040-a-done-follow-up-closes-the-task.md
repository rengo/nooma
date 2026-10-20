# ADR-0040 — A chat follow-up that says a shown task is done closes it

- **Status**: Accepted (by the client, 2026-10-19: "a follow-up in chat that says a shown task is
  done closes it: it is no longer named by the digest and no longer live. If it's unclear which
  task the user means, Nooma asks '¿Te referís a …?'")
- **Date**: 2026-10-19
- **Supersedes**: [ADR-0036](0036-a-chat-message-is-read-in-its-conversation.md) points 3 and 5,
  in part: a `task_checkin_outcome: done` with a listed `referent` binds too, not only a
  `correction`.
- **Superseded by**: [ADR-0041](0041-the-chat-is-stored-and-read-by-its-last-turns.md), the cost
  "a done past the conversation window closes nothing" (the chat has no time bound);
  [ADR-0046](0046-a-done-follow-up-closes-the-event-too.md), points 2 and 3 in part (a bound
  event is closed as a task is)
- **Enables**: M4

## Context

Since the morning digest names the tasks due (doc 02 §7), "Hoy hay 1 cosa: • Renovar el
pasaporte — mar 20 oct" is a turn of the chat about that task (ADR-0036). A task arms no
reminder, so it has no check-in, and the answer "ya lo hice" matched nothing open: the task
stayed `pool` and was named again the next two mornings. ADR-0036 point 3 bound a listed item
only for a `correction`, and a done is not one.

## Options evaluated

| Option | Real tradeoff |
|---|---|
| Arm a check-in for every dated task, so the existing check-in done closes it | A trigger per task nobody asked for, delivered by the digest; the store would still pick "the newest open" rather than the task the user named |
| A new field, `done_referent` | A second letter field the model must keep apart from `referent`; two fields that mean "which listed item" invite one being answered for the other |
| **`referent` also names the item a `task_checkin_outcome: done` is about; the code closes a bound task** ✅ | The same field, the same binding, the same ask on `unclear`. The prompt with no chat is unchanged; every chat-shown phrasing is recorded again |

## Decision

1. **The model reads the done, the code binds the item.** Beside a chat, `referent` is also the
   letter of the listed item a `task_checkin_outcome: done` of any type says is done, or
   `unclear` with a `clarifying_question`. A `correction` carrying done stays a correction. The
   code binds only a letter it listed (`classify.ReadFollowUp`), as for a correction.
2. **A bound task is closed by a state transition.** A `task` still `pool` whose reminders recur
   on no rule (`prospection.DoneCloses`) moves `pool → archived`: the status vocabulary has no
   `done`, archiving is the finished state it has, and nothing is deleted. Its armed reminders
   are expired and its open check-ins answered `engaged`. One `capture.task.closed` row records
   the status before and after and what went with it.
3. **Anything else keeps its path.** A bound item that is not such a task (an event, a recurring
   series, whose one occurrence being done must not end it) is not closed; its open check-in, if
   any, is answered from the store as before.
4. **Never a false confirmation.** The reply names the task closed. A chat message whose done
   closed nothing (no item named and no check-in open, with turns shown or read alone) says so
   rather than letting the chat model answer. A capture from the UI or the API is no chat answer
   and keeps the chitchat's reply.

## Consequences

### What it enables

- "ya lo hice" after the digest closes the task, and the next mornings do not name it; "ya está"
  after a digest of two asks which.
- A done answering a pushed reminder of an asked task closes the task too, not only its check-in.

### What it costs

- **A misread can close the wrong task.** It is bounded: it needs a listed letter, the task is
  archived rather than deleted, so it can come back, and the trail records it.
- **An archived task can be revived** by a strong resurface (§2) like any archived unit, and is
  then named again if still inside its digest days.
- **The chat-shown phrasings are recorded again**, since the Follow-ups section changed.
- **A done past the conversation window closes nothing.** The digest turn is shown for
  `conversation_context_minutes`; an answer hours later is read alone and says nothing was
  closed.

### Reversal criteria

Tasks closed that the user did not finish, reported in use, point back to asking before
closing.
