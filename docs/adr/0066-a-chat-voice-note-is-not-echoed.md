# ADR-0066 — A voice note that ends in chit-chat is not echoed

- **Status**: Accepted (the client ruled this on 2026-11-05)
- **Date**: 2026-11-05
- **Supersedes**: [ADR-0062](0062-a-voice-note-is-read-as-its-transcript.md), point 4 only
  (the other points stand)
- **Superseded by**: —

## Context

ADR-0062 point 4 led the reply to a voice note with "Entendí: «…»" for `conversed`,
`out_of_scope` and `asked` outcomes and a which-timer question. The client tried it: a voice note
that says "Hola, ¿cómo te va?" was answered "Entendí: «Hola, ¿cómo te va?» ¡Hola! Todo bien,
¿y vos?", and found the echo unnatural there. The echo exists so that a mishearing shows when
the reply is odd; a greeting answered in kind has no oddness to explain.

## Decision

Point 4 of ADR-0062 becomes: the reply to a voice note leads with "Entendí: «…»"
(`phrase.VoiceHeard`) only for `out_of_scope` and `asked` outcomes and a which-timer question.
A `conversed` outcome is answered as the same text typed would be. `channels.EchoesTranscript`
stays a switch over the typed outcome, never the reply's words.

## Consequences

- A voice chit-chat reads as a conversation. If the transcript was wrong, the reply to it is
  still a chat reply and the user can say so; nothing was written from it.
- Where the answer can be odd (a refusal, a question back, a which-timer question) the user
  still sees what was heard.

## Reversal criteria

- A mis-heard chit-chat turns out to cause confusion often enough that users ask to see the
  transcript again.
