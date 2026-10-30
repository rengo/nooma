# ADR-0062 — A voice note is read as its transcript, and the audio is never kept

- **Status**: Proposed (the client ruled on 2026-10-30 that voice ships before v1.0)
- **Date**: 2026-10-30
- **Supersedes**: —
- **Superseded by**: —
- **Enables**: v1 (voice notes over Telegram, moved forward from after v1)

## Context

The Telegram channel reads only `message.text`. A voice note, the fastest way to tell Nooma
something while walking, got no reply at all. `docs/05-build-plan.md` put voice transcription
after v1. The client moved it before v1.0 and ruled what it must do: transcribe with OpenAI,
never store the audio, read the transcript exactly as typed text, echo it only when the reading
did nothing actionable, ask for it typed when it cannot be heard, and never fetch a stranger's.

The vault already documents a task for this, `audio_transcription` (doc 01's example binds it to
a `whisper_cpp` provider), and `conversation_turns` has no column for where a turn came from.

## Options evaluated

| Where the note is heard | Verdict |
|---|---|
| **In the Telegram adapter, behind the allow-list, before a `ChannelMessage` exists** | Chosen. Admission already happens there, so a stranger's note cannot be fetched by anything above it; the adapter already answers `/start` itself, and a note it cannot hear is answered the same way. Capture sees text, and one flag |
| In `CaptureHandler`, from a file reference on the message | Rejected: `ports.Channel` would grow a download method, and the capture path would carry audio |
| A brain service | Rejected: the brain would hold a vendor's file ids |

| Model | Verdict |
|---|---|
| **`gpt-4o-mini-transcribe`** | Chosen. Half `whisper-1`'s price per minute, a lower word error rate on OpenAI's multilingual evaluation (Spanish included), and it takes Telegram's OGG/Opus as uploaded |
| `whisper-1` | Rejected: dearer and less accurate for the same call |
| A local whisper.cpp | Not now: no client exists. The task name and provider type are already documented, so it can come later without a new key |

| Where "this was a voice note" is recorded | Verdict |
|---|---|
| **One `capture.voice.transcribed` decision row, naming outcome and unit** | Chosen. No migration, it shows on Activity, and it is one row whatever the outcome |
| A `source` column on `conversation_turns` | Rejected: a migration for a fact one row already carries |
| A key in every capture row's context | Rejected: a dozen writers, each a place to forget it |

## Decision

1. **The allow-list runs before any download.** A voice note from a chat outside
   `allowed_chat_ids` is refused like any message; its file is never requested.
2. **The adapter hears the note**: `getFile`, the download into memory (at most
   `max_voice_bytes`), one `ports.TranscriptionProvider` call, and the bytes are dropped. Nothing
   is written to disk or to the vault. The trimmed transcript becomes `ChannelMessage.Text`, with
   `Voice` set.
3. **The transcript takes the typed path**: the same `CaptureHandler`, the same capture, the
   same conversation turn (the transcript is the user's text), the same reply. `CaptureInput.Voice`
   makes capture write one `capture.voice.transcribed` row; a failure to write it is warned
   about and does not fail the capture, which would redeliver the note and duplicate its unit.
4. **No echo by default.** The reply leads with "Entendí: «…»" (`phrase.VoiceHeard`) only for
   `conversed`, `out_of_scope` and `asked` outcomes and a which-timer question:
   `channels.EchoesTranscript`, a switch over the typed outcome.
5. **The adapter answers, once, and captures nothing** when `audio_transcription` is unbound or
   cannot transcribe (`VoiceOff`), when the note is longer than `max_voice_seconds` or declares
   more than `max_voice_bytes` (`VoiceTooLong`, before any download), and when it is not heard:
   an error, a timeout, an empty transcript, a download past the cap (`VoiceUnheard`). All in the
   sender's Telegram language.
6. **`audio_transcription` is the task** (the documented name, not a new `transcription` key).
   An `openai` provider transcribes with `gpt-4o-mini-transcribe` whatever its own `model`
   names, so the provider the chat task uses serves it with no second entry. A binding to any
   other type is warned about at serve start and leaves voice off.

## Consequences

- An Ollama-only vault says it cannot listen, and points to typing.
- The transcript is the only trace of the audio. A mishearing that was acted on is corrected
  as typed text is; one that was not is shown to the user by the echo.
- A note held behind an unconfirmed message is transcribed again on each redelivery: one more
  call, never a second capture (the runner's dedup holds).
- Transcription adds the download and one provider call to the poll that received the note.

## Reversal criteria

- Users ask to hear their own notes back, which means storing audio and a new decision.
- A local transcriber is wanted: add a `whisper_cpp` client behind the same port.
