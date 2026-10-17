# ADR-0036 — A chat message is read in its recent conversation

- **Status**: Accepted (by the client, 2026-10-17: "if I say it's at 8, it should know in the
  moment from the thread of the conversation, not ask me the next day what I meant"; and, on the
  design, "it's like you: if I say 'it's at 8' you just know, or at most you ask 'do you
  mean…?'. Nooma is an LLM operating over defined entities")
- **Date**: 2026-10-17
- **Supersedes**: —
- **Superseded by**: —
- **Enables**: M4

## Context

On Telegram the client captured a meeting, then sent "Es a las 08:00." as a follow-up. Every
message was classified alone (`classify.BuildPrompt` saw the text and nothing before it), so the
follow-up came back as something new: it was stored as a unit, the relation judge linked it to
the meeting, and the next morning's digest asked "Relacioné «Es a las 08:00.» con «Tengo reunión
con Juan el 2026-10-14 a las 22:30.». ¿Tienen relación?".

The correction fork existed (doc 02 §5 step 4, I28, I30), but only the UI and the API hold the
id its explicit path needs; a chat message reached the recall gate at best, which cannot tell two
dated units apart from "Es a las 08:00." alone.

## Options evaluated

| Option | Real tradeoff |
|---|---|
| Keep reading alone; ask the next day | What happened. The question comes a day late, about a unit that should not exist |
| A stored conversation and a stored clarifying question (`pending_questions` widened, a turns table) | Durable across restarts, but two migrations and a question state machine for a window of minutes. Rejected by the client as overcomplicated |
| **The recent chat in memory, shown to the same classify call; the model names a letter, the code binds only a letter it showed** ✅ | One call, no schema. A restart forgets the chat, which costs one follow-up read alone, as before. The model can misread, so binding is bounded the way ADR-0033 bounds a stop |

## Decision

1. **The serve process keeps the recent chat in memory**, per conversation: what the user wrote,
   what Nooma answered, and what it pushed (a reminder, a check-in, the digest), each with the
   units it concerned. A chat message is read in the last `conversation_context_turns` (6) turns
   no older than `conversation_context_minutes` (30). Nothing of it is stored in the vault.
2. **The model understands.** The prompt shows the turns, each tagged with the letters of the
   items it concerns, and those items as they stand now. The same classify call returns
   `referent` (a letter, or `unclear`) and, beside `unclear`, `clarifying_question`, written by
   the model in the user's language. No phrase list: the prompt says by meaning what a follow-up
   is and what it is not.
3. **The code acts.** Only a `correction` counts. A letter the prompt listed binds that unit,
   which is then corrected through the explicit-referent path with the same call's fields, as if
   the caller had named it (I28, I30, I29 unchanged). An unlisted letter never binds. `unclear`
   with a question sends the question and stores nothing.
4. **No pending-question state.** The answer ("sí", "no, al dentista") arrives with Nooma's
   question in the chat and is read as an ordinary correction with a referent.
5. **A check-in answer stays one.** A short reply to a reminder or check-in answers it as before;
   only a `correction` with a valid referent binds.

## Consequences

### What it enables

- A follow-up moves what it follows in the moment, a reply to a pushed reminder ("no, el
  jueves") included; when it cannot tell, Nooma asks "¿Te referís a…?" right then.

### What it costs

- **The prompt grows** by the field descriptions always and the transcript on a chat message,
  and the whole reminder phrasing set is recorded again (`nooma doctor --record --force`).
- **A misread can correct the wrong item.** It is bounded: it needs a letter the code showed, the
  pre-image keeps what it overwrote (ADR-0016), and the doctor reports items corrected that
  nobody changed.
- **A restart forgets the chat.** A follow-up sent across one is read alone, as before.

### Reversal criteria

Corrections of an item nobody changed reported in use, or a doctor follow-up rate the client
judges too low, point back to asking before applying a follow-up. A need for a chat that
survives a restart points to persisting the turns.
