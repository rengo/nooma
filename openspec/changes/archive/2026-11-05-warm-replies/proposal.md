# Proposal — warm-replies: Nooma's replies are warm sentences the code builds

> **PM-owned.** Drafted by nooma-dev from the client's ruling of 2026-11-04 because the PM asked
> for it; the PM owns this file and may rewrite it.

## Why

Nooma's replies read cold and mechanical. The client's examples (Telegram, 2026-11):

```
← cuando tengo turno al gastroenterologo?
→ Encontré 1 cosa:
  • Turno para estudios con el gastroenterólogo — jue 26 nov, 08:30
(push) → Recordatorio: Tomar las pastillas — jue 29 oct
```

The client expected "Tenés turno para estudios con el gastroenterólogo el jueves 26 de noviembre
a las 8:30."

ADR-0061 tried having a model reword each reply around facts the code produced, and #435
(ADR-0063, never merged) narrowed the model to slots the code fills. Five review rounds each found
a wording that passed the check and stated something false: an undated item borrowing another's
date, "más de 2 cosas", "a las 10:00 o después", fullwidth digits.

## The client's ruling (2026-11-04)

- **No model words a reply that carries a fact.** Nooma builds warm sentences in code.
- Model wording may return **after 1.0**, measured on recordings, under a new decision.
- A ringing timer is a fixed sentence too; the `timer_rephrase` model call is retired.
- **Accepted costs** (the client confirmed 2026-11-04 "Tenés anotado …" for a recalled item, and
  that a name written capitalised only as an item's first word is lowered in Spanish): less
  variety; no adaptation to the user's own phrasing; replies can be somewhat telegraphic; the code does not know a noun's gender, so it avoids articles before an
  item's text where needed.

## What changes

Natural, friendly voseo Spanish and plain English, deterministic so tests pin the exact text, for:

1. recall answers, found and empty;
2. reminder and check-in pushes, and a ringing timer ("Es la hora: sacar la ropa.");
3. the morning digest;
4. capture confirmations ("Anotado…", "Te aviso…") and corrections.

Days and times read as a person says them: "mañana a las 10:00", "el jueves 26 de noviembre a
las 8:30", "a la 1:00". Nothing is invented, and an empty result is said honestly.

## Out of scope

- Texts that carry no fact and are not cold today: the out-of-scope refusal, the outage sentence,
  the bot commands, the voice-note answers.
- The timers' numbered list and its "¿Cuál cancelo?" (a reply binds by position, ADR-0051).
- Texts the model already writes and that carry no stored fact: the chitchat answer and a
  follow-up's clarifying question.
- The `/ui` pages' own date formats (Today's lines follow the digest's, since they preview it).

## Success

The client reads the examples in design.md §6 and accepts the voice; on main, every surface above
answers with the exact text its conformance test pins, and no reply on them waits for a model.
