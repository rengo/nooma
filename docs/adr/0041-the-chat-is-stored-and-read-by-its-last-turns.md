# ADR-0041 — The chat is stored in the vault and read by its last turns, at any age

- **Status**: Accepted (by the client, 2026-10-19: "¿Por qué recuerda por tiempo? Que recuerde
  por almacenamiento, los últimos N chats."; N set to 20 the same day)
- **Date**: 2026-10-19
- **Supersedes**: [ADR-0036](0036-a-chat-message-is-read-in-its-conversation.md) point 1 (the
  chat kept in memory only, and the 30-minute bound on it), and its "A restart forgets the
  chat" cost; [ADR-0040](0040-a-done-follow-up-closes-the-task.md)'s cost "a done past the
  conversation window closes nothing": an answer hours after the digest is read with it shown
- **Superseded by**: [ADR-0042](0042-a-turn-shows-when-it-was-said.md), the cost "the prompt
  shows each turn's text, not when it was said" (each turn now shows its local time)
- **Enables**: M4

## Context

ADR-0036 kept the chat in the serve process's memory: the last six turns no older than thirty
minutes. A digest pushed at 07:00 was gone by 07:30, so a reply to it at 09:00 ("ya lo hice")
was read alone and, under ADR-0040, closed nothing. Everything was lost when serve restarted.

ADR-0036 had rejected a stored conversation as overcomplicated, but that option came bundled
with a stored clarifying question and a question state machine. A table of turns alone needs
no state machine: it is appended to, and the last turns are read.

## Options evaluated

| Option | Real tradeoff |
|---|---|
| Keep memory, widen the time window | A restart still forgets; any time bound still cuts a reply at the bound's edge |
| **The turns stored in the vault, the read bounded by count only** ✅ | One migration and one append-only port. The model can be shown a turn from yesterday beside a new message |
| Stored turns pruned to the last N | Deletes from the vault, which non-negotiable 6 refuses; the read already takes the last N |

## Decision

1. **Every turn of a chat is stored in the vault** (`conversation_turns`, migration 0012): the
   chat, who spoke (`user`, `nooma`, `push`), the text, the units it concerned, and when. The
   port only appends; nothing changes, prunes or removes a turn (I03's sweep covers both the
   port and the table).
2. **A chat message is read in the chat's last `conversation_context_turns` (20) turns, at any
   age.** `conversation_context_minutes` is gone: the bound is a count, never a time. The order
   is the order the turns were appended in. At most as many items are lettered (`A` to `T`);
   when the turns name more live units, the ones the newest turns name are lettered, so the
   exchange a follow-up answers can always be bound.
3. **A restart forgets nothing.** A new process over the same vault reads the same turns.
4. **Storage never fails what was already said.** A turn that cannot be stored, or a chat that
   cannot be read, is warned about and the message is read without it.
5. Everything else in ADR-0036 stands: the prompt shows the turns and the live items they
   concern as they stand now, the code binds only a letter it showed, and a unit no longer live
   is never a letter.

## Consequences

### What it enables

- A reply to the morning digest at any hour is read with the digest shown, so "ya lo hice" at
  09:00 closes the task the 07:00 digest named (ADR-0040); a follow-up sent across a restart is
  read in its chat.

### What it costs

- **An old turn can sit beside a new message.** The model judges whether a message follows up
  on it (ADR-0033). The prompt shows each turn's text, not when it was said; the items are shown
  with their instants as they stand now.
- **The prompt grows** by at most about 330 characters per turn shown (`max_turn_runes` and the
  speaker label), about 6,300 more at 20 full turns than at one, plus the lettered items.
- **The table grows** by a few rows per exchange, unpruned, like `decision_log`.
- **Prompt text does not change**: only which turns are shown. No recorded phrasing changes.

### Reversal criteria

Follow-ups misread against a turn from a previous day, reported in use, point first to showing
each turn's time in the prompt, and only then to a time bound.
