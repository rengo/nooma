# ADR-0064 — A reply that carries a fact is built by code, in warm fixed sentences

- **Status**: Accepted (the client ruled this direction and its details on 2026-11-04)
- **Date**: 2026-11-04
- **Supersedes**: [ADR-0061](0061-the-model-words-what-the-code-found.md) **in full**, and
  [ADR-0032](0032-a-reminder-says-what-is-true-now.md) point 4's fixed frame **wording**
  ("Reminder: <text> — <when>"). ADR-0032 point 4's rule stands and is restored: a reminder says
  its unit as it is now, never through a model call.
- **Superseded by**: —
- **Extends** (none of them edited): [ADR-0009](0009-scheduler-downtime.md) (the delay note of a
  late timer moves into the delivered text), [ADR-0022](0022-reply-language.md) (every sentence lives in
  `internal/core/phrase`, per language; the glass box stays English),
  [ADR-0035](0035-nooma-speaks-first-in-the-usual-language.md) (which language a push and the
  digest speak), [ADR-0033](0033-the-model-understands-the-code-acts.md) (the model understands,
  the code acts, and now the code also words)

## Context

Nooma's replies read cold (the client, 2026-10-30 and 2026-11-04):

```
← cuando tengo turno al gastroenterologo?
→ Encontré 1 cosa:
  • Turno para estudios con el gastroenterólogo — jue 26 nov, 08:30
(push) → Recordatorio: Tomar las pastillas — jue 29 oct
```

ADR-0061 had a model reword each reply from the code's facts, with a pure check deciding whether
the wording could be sent (`internal/core/voice`, merged in #433 with no production caller).
#435 proposed narrowing the model to slots the code fills (an ADR-0063 that was never merged).
Five review rounds of #435 each found a wording that passed the check and stated something false:
an undated item borrowing another's date, "más de 2 cosas", "a las 10:00 o después", fullwidth
digits. Each fix grew a parser of free text; none closed the class.

On 2026-11-04 the client ruled: no model words a reply that carries a fact; Nooma builds warm
sentences in code. Model wording may return after 1.0, measured on recordings, under a new ADR.

## Options evaluated

| Option | Real tradeoff |
|---|---|
| Keep ADR-0061's model wording and harden the check further (#435) | Natural and varied, but five rounds show the check cannot be made complete over free text; a reminder system that states a false day is worse than a cold one |
| **Warm fixed sentences in `internal/core/phrase`, built from typed facts, deterministic** ✅ | Nothing false can be written and tests pin the exact text; less variety, no echo of the user's phrasing, and frames must avoid guessing a noun's gender |
| A template engine or message catalogue (e.g. ICU MessageFormat) | Plural and gender rules for free, but an external dependency `internal/core` may not import, and two languages do not need it |

## Decision

1. **No model words a reply that carries a fact.** A recall answer (found and empty), a reminder
   push, a ringing timer, the digest, a capture confirmation and a correction's answer are built by
   `internal/core/phrase` from typed facts (the item's words, its instant, whether it is a day
   alone or a due day, now). No provider call words them and none waits for one.
2. **Days and times are said as a person says them.** Yesterday, today, tomorrow and the day after
   tomorrow by name; any other day by weekday, day of the month and full month name, with the
   year only when it is not now's; a 24-hour time with no leading zero, and Spanish "a la 1:00".
   The relative day is counted on the calendar in now's zone.
3. **Frames never guess a gender.** No article or gendered word stands right before an item's
   own words. In Spanish an item's words inside a sentence start lower-case unless their first
   word is an acronym or appears capitalised in the item's own title or content past that text's
   first word (a
   name); English keeps the item's words as stored and quotes them inside a sentence.
4. **Tense follows the instant.** A recalled item whose instant is behind now is said in the past.
5. **Deterministic.** The same facts give the same bytes. Every surface in point 1 is pinned in
   both languages by an exact text (I59).
6. **What stays as it is**: the timers' numbered list and its which-question (ADR-0051), the
   out-of-scope refusal (ADR-0021), the bot commands and the voice-note answers, the client's
   re-ask and goal wording (2026-10-16), and the texts a model already writes and that carry no
   stored fact (the chitchat answer, a follow-up's clarifying question).
7. **A ringing timer is a fixed sentence** (the client, 2026-11-04): "Es la hora: sacar la ropa." /
   "Time's up: Take the clothes out.", the timer's words inside it as point 3 says, with the
   lateness note appended when it is late. A timer with no words keeps its generic nudge. The
   `timer_rephrase` task is retired: no provider call words a timer. `timers.rendered_text`
   stays in the schema (a published migration is never modified) and is no longer written; the
   decision-log action that recorded a failed rephrasing stays defined so old rows still label.
   [ADR-0009](0009-scheduler-downtime.md)'s note that `rendered_text` mentions the delay now
   rides in the delivered text instead. [ADR-0022](0022-reply-language.md)'s measurement of
   which tasks ran is read without `timer_rephrase`.
8. **`internal/core/voice` is retired**, with its four calibration rows (`reply_phrasing_timeout`,
   `reply_max_sentences`, `reply_min_runes`, `reply_runes_factor`). It has no caller; it stays in
   history (b3af1e82, and #435's branch) for the post-1.0 measurement.

## Consequences

### What it enables

- A reply cannot state a day, a time, a count or an item the vault does not hold: the code that
  found the fact is the code that writes it.
- No added latency or tokens on any reply, and no provider needed to answer well.
- Exact-text tests in both languages; a regression in a sentence is a red test, not a review
  round.

### What it costs

- Less variety: the same outcome reads the same every time.
- No adaptation to the user's phrasing: the answer to "¿cuándo tengo turno?" does not echo
  "turno" unless the item's own words do.
- Somewhat telegraphic in places ("Tenés anotado turno con el oculista…", "Acordate: tomar las
  pastillas, hoy."), because a frame cannot know whether an item's words are a noun or a verb,
  or their gender.
- A name that is written capitalised only as the first word of its title or content is lowered in
  Spanish ("ana cumple años"): the code has nothing else to tell it is a name. A capitalised word
  later in a text, including one that opens a second sentence, is read as a name and kept.
- A timer's ring no longer varies: the same words give the same sentence.
- The classification prompt shows the model Nooma's recent turns as they were said. The recorded
  cases in `testdata/llm/reminders/` carry the old wording in their fixture turns; they stay valid
  (their prompt hash is built from the fixture, not from `phrase`), but they no longer show the
  model what Nooma says now. Re-recording a few with the new wording is measured, not required.
