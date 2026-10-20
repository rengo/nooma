# ADR-0042 — A turn of the chat shows when it was said

- **Status**: Accepted (by the client through the PM, 2026-10-19: show each turn's local time
  so the model judges staleness, as the ruling behind ADR-0041 assumed)
- **Date**: 2026-10-19
- **Supersedes**: [ADR-0041](0041-the-chat-is-stored-and-read-by-its-last-turns.md), the cost
  "the prompt shows each turn's text, not when it was said" and its reversal criterion's first
  step
- **Superseded by**: —
- **Enables**: M4

## Context

ADR-0041 dropped the chat's time bound: a message is read in the chat's last twenty turns,
however old. The ruling assumed the model sees when each turn was said and judges staleness
itself (ADR-0033), but the prompt showed only who spoke and what. Yesterday's "reunión con Juan a
las 22:30" read as if it had just been said.

## Decision

1. **Each turn is shown at the local time it was said**, in the user's zone, the calendar's
   date form with its weekday: `Tue 2026-10-20 07:00 Nooma, pushed (a reminder or check-in):
   "…"`. The section says the oldest turn may be days before the message.
2. A phrasing case states when each of its turns was said (`at`), and a recorded prompt must be
   the one production sends for the same inputs (`TestReminderPrompts_AreTheProductionPrompt`).
3. A prompt with no chat is unchanged, byte for byte.

## Consequences

- Every chat-shown phrasing is recorded again: the transcript lines and the section's wording
  changed. A prompt with no chat keeps its recordings.
- Each turn costs about 21 more characters.
- A misread against a stale turn now points to a time bound, the remaining step of ADR-0041's
  reversal criterion.
