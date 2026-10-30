# ADR-0063 — The model writes slots, and the code fills in every fact

- **Status**: Proposed
- **Date**: 2026-10-30
- **Supersedes**: [ADR-0061](0061-the-model-words-what-the-code-found.md) **in part**: point 2's
  "so the model copies it", point 3's reading of the wording's own numbers and dates, and the
  Consequences' "a small closed list of relative-day words". Everything else ADR-0061 decides
  stands: the code decides, the model words, a pure check sends the wording or the fixed text.
- **Superseded by**: —

## Context

ADR-0061's check read the model's own dates and numbers and matched them to the facts. Three
review rounds on #435 each found a wording it passed that the facts did not back: a day given to
another item, a count read off a day, an invented item borrowing a day in another form, a
negation ("no tenés dentista…"), and an empty recall claiming a finding ("encontré algo"). The
same check sent the fixed text for 8 of 13 honest two-item wordings. Each fix grew a parser of
free text, which is the class of mistake ADR-0033 already ruled on.

## Decision

1. **The model never writes a fact.** It writes slots: `{n}` for item n's own words, `{n.when}`
   for its day and time with their own preposition, `{count}` for how many items there are, and,
   on an empty recall, `{none}` for the finding of nothing ("no encontré nada"). The code fills
   each one with the words the fixed text uses. A swapped, borrowed or miscounted day, or an
   empty recall's claim of a finding, cannot be written.
2. **The check is structural.** Outside the slots: no digit, no day or month name, no day, time or
   number word. Each item's slots appear once, side by side, and two items are parted. `{none}`
   appears exactly once on an empty recall and never otherwise. Every other word is a common word
   of the language or one of the draft's, the message's or an item's own. Negations and singular
   counts ("no", "ni", "nada", "cosa"; "not", "nothing", "thing") are not common words: only the
   draft or an item can license them. On an empty recall, words that claim a finding ("algo",
   "hay", "encontré") are refused even when the message holds them. Anything else, a provider
   error, or a timeout sends the fixed text, as ADR-0061 point 4 says.
3. **An item's words are sent as stored**, capitalised where a sentence starts. Mid-sentence, the
   first letter is lowered only when the user's own message writes that word in lowercase, so a
   name ("Pedro") is never lowercased by guesswork.

## Why the word list does not break ADR-0033

ADR-0033 rules out reading the **user's intent** from word lists, because the user's phrasings
never end. This list reads **Nooma's own output**, in a frame the prompt tells the model to keep.
A word missing from the list sends the fixed text, a safe failure. A word missing from ADR-0033's
lists misread what the user meant. The list grows from the recorded phrasings, which measure the
false rejects.

## Consequences

- A wording cannot state a date, a count or an item the facts do not hold. What it can still do
  is attach a detail made only of the draft's, the message's or the items' own words ("el
  dentista … con Ana"), or be true but odd.
- Natural wordings that use a word missing from the list fall back to the fixed text. The rate is
  measured, not assumed.
- A capital mid-sentence ("tenés el Dentista") where the message does not write the word.
