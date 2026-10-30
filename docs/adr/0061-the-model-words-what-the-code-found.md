# ADR-0061 — The model words what the code found or did, and a check decides it may be sent

- **Status**: Accepted (by the client, 2026-10-30: no emojis, one or two sentences with no added
  remarks, natural but brief confirmations, no offers or added questions, a natural digest, and
  a wait of up to 5 s before the fixed text)
- **Date**: 2026-10-30
- **Supersedes**: [ADR-0032](0032-a-reminder-says-what-is-true-now.md) **in part**: point 4's
  fixed frame and its rejected option "re-word the text with a model at delivery". What a
  reminder says stays the unit as it is now; only its wording changes.
- **Superseded by**: [ADR-0063](0063-the-model-writes-slots-the-code-fills.md), in part (point 2's copying, point 3's reading of dates, and the word list)
- **Extends** (none of them edited): [ADR-0033](0033-the-model-understands-the-code-acts.md)
  (the model understands, the code acts: now the model also words what the code did),
  [ADR-0022](0022-reply-language.md) (the fixed sentence in the reply's language is the fallback,
  and the glass box stays English)

## Context

Real Telegram use on v0.5/0.6 (client, 2026-10-30):

```
← cuando tengo turno al gastroenterologo?
→ Encontré 1 cosa:
  • Turno para estudios con el gastroenterólogo — jue 26 nov, 08:30
(push) → Recordatorio: Tomar las pastillas — jue 29 oct
```

The client finds this deterministic and cold, and wants "Tenés el turno el jueves 26 de
noviembre a las 8:30, para los estudios": friendly and natural, like a conversation. It ships in
v1.0, before the release candidate.

Only two texts are model-written today: the chitchat reply (`chat`) and a fired timer's
rephrasing (`timer_rephrase`, sent to the consolidation provider and checked only for being
non-empty). Every other text is a fixed sentence in `internal/core/phrase`.

## Options evaluated

| Option | Real tradeoff |
|---|---|
| Keep the fixed sentences, and write warmer ones | No model call and nothing to check, but one sentence per outcome per language cannot answer the question the way it was asked |
| The model writes the reply freely from the vault | Natural, and the model would state dates and items it inferred or invented: a reminder system that says the wrong day is worse than a cold one |
| **The model words the code's facts and the fixed text; a pure check decides whether that wording may be sent, else the fixed text is** ✅ | One more call per message, and a check that reads only what it can read mechanically |
| Word the reply inside the classify call | Impossible: the facts (what recall found, what was armed) exist only after the code has acted on the reading |

## Decision

1. **The code decides and acts; the model only words it.** Nothing the model writes binds,
   arms, closes or stores anything. A follow-up still binds by the items the code listed
   (ADR-0041, ADR-0044), never by words.
2. **The model gets the facts and the draft.** `voice.Facts` holds the intent, the language, the
   operation's instant, the user's message (for register only, never a fact), the items (title
   else content, type, instant, day alone or not) and the literals to keep. The draft is the
   fixed text the code would send. The prompt names each day as the code counted it ("jueves 26
   de noviembre, 08:30 — mañana") so the model copies it, and asks for the user's variety and
   register (voseo, as in ADR-0044's context).
3. **A pure check decides** (`voice.Check`, I56). The wording is sent only when:
   - it is not empty, has no emoji, and has no markdown or JSON (Telegram is sent plain text);
   - it is no longer than the larger of 280 runes and twice the draft;
   - it asks no more questions than the draft (no offers, no added questions);
   - it has at most two sentences outside list lines, or the draft's count when that is more;
   - it carries every literal to keep;
   - every number, time, date, weekday or month name, and relative day ("mañana", "tomorrow")
     in it is one the facts or the draft hold. A number in the user's message is not a fact;
   - it says every item's day (by number, date, relative day, or weekday within the week) and,
     unless a day alone, its time.

   What it cannot read, a fact invented in words alone ("también tenés…", a number spelled out),
   is bounded by the prompt, the length rule and the recorded phrasings, which measure it.
4. **Otherwise the draft is sent**, and so is it on a provider error or after
   `reply_phrasing_timeout` (5 s). Brain records each fallback with its reason. The person always
   gets an answer, and never one the vault does not back.
5. **What stays fixed**: the timers' numbered list and its "¿Cuál cancelo?" (a reply binds by
   position, ADR-0051), the `out_of_scope` refusal (ADR-0021), the Telegram commands, and a text
   that is already the model's (a chitchat, a clarifying question).
6. **On by default, off by choice.** The wording runs on the `reply` task's provider; with
   `reply` unbound, on the `chat` task's; with neither, there is no voice and every text is the
   fixed one. `voice: false` in `nooma.yml` turns it off, and so does a toggle in `/ui/admin`,
   logged as every other setting (`config.updated`). Tests and e2e run without it structurally:
   a fixture passes no phraser, and an e2e vault's config turns the voice off, so a stand-in
   provider never receives a wording call.
7. **Cost.** One more completion per message worded: about 450 to 650 input tokens (the classify
   prompt alone is about 14.5 KB, some 3,600 tokens) and at most about 80 output tokens, so about
   15 % more input per chat message, plus one round trip a chat reply waits for, bounded by 5 s.
   A pushed reminder or digest that failed to send is resent with the wording already made. The
   prompt is below the size providers' prefix caches start at, so it gains nothing from them;
   its rules come first anyway.
8. **Rollout**, by value: the check (this ADR's first change), then recall answers, then pushes
   and the timer rephrasing (moved onto this check and the `reply` task), then the digest, then
   confirmations and corrections.

## Consequences

### What it enables

- A reply that answers the question as it was asked, in the user's voice, with no false date.
- The timer rephrasing gains the check it never had.

### What it costs

- One completion per message worded, and a chat reply that waits for it.
- A check that is mechanical, not semantic: an invented item with no number in it passes it,
  and so does a number spelled out in words ("tres cosas", "a las ocho"). The prompt asks for
  digits, and the recorded phrasings measure how often either happens.
- A small closed list of relative-day words per language, in `internal/core/voice`. It reads
  Nooma's own output for its facts, not a user's message for intent, so ADR-0033's rule against
  word lists does not apply to it.
