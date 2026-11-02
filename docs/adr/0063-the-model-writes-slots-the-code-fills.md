# ADR-0063 — The model writes slots, and the code fills in every fact

- **Status**: Accepted
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
   of the language (the frame) or one of the draft's own fixed words. **Neither the user's message
   nor an item's text licenses a word**: the message is in the prompt for its tone, and an item's
   words reach the wording only through its slot. So a negative question ("¿No tengo nada esta
   semana?") lends no negation, and a message naming the dentist lends no "dentista" to an empty
   recall. The frame holds no negation ("no", "ni", "nada", "sin"; "not", "nothing", "nor"), no
   noun that can stand as an item or a count ("cosa", "pendiente", "tema", "turnos", "algo";
   "thing", "topic", "something"), no past tense of the answer's verb ("tenías", "had") and no
   "if" ("si"): only the draft's fixed text says them, so a draft that says "Encontré 2 cosas"
   licenses its own count. On an empty recall, words that claim a finding are refused whatever
   the draft or the message holds: the indefinites and verbs ("algo", "hay", "encontré",
   "found"), and the copulas, locatives and "but" that would present something as there ("más",
   "es", "está", "queda", "ahí"; "but", "is", "it", "there"). Anything else, a provider error, or
   a timeout sends the fixed text, as ADR-0061 point 4 says.
3. **An item's words are sent as stored, with the case the facts support.** A sentence's first
   word is capitalised. Elsewhere an item's first letter is lowered ("el dentista", titles are
   stored capitalised), unless the facts write that word with a capital: in the user's message, or
   inside an item's text past its first word ("Regalo para Pedro"). Then it is a name and keeps its
   capital.

## Why the word list does not break ADR-0033

ADR-0033 rules out reading the **user's intent** from word lists, because the user's phrasings
never end. This list reads **Nooma's own output**, in a frame the prompt tells the model to keep.
A word missing from the list sends the fixed text, a safe failure. A word missing from ADR-0033's
lists misread what the user meant. The list grows from the recorded phrasings, which measure the
false rejects.

## Consequences

- A wording cannot state a date, a count or an item the facts do not hold, and cannot negate a
  found recall or claim a finding on an empty one. These are the guarantees the code keeps, each
  pinned by a must-fail test (I56).
- Because the message licenses no word, a wording that repeats the topic on an empty recall
  ("no encontré nada sobre el dentista") falls back to the fixed text; "sobre eso" / "about that"
  is sent. This is a behaviour change from the first draft of this ADR: the topic was the one use
  of the message's words, and it also let the message put words such as "más que el dentista" in
  an empty recall's mouth.
- Known limits, documented rather than chased because none can state a false date or count: a
  wording can be true but odd ("el dentista, y antes, la reunión"); a name the facts never write
  with a capital is lowered ("pedro cumple años"); a natural wording that uses a word outside the
  frame falls back to the fixed text, a rate the recorded phrasings measure; and a future
  "será"/"will" or an infinitive is in the frame while a past or conditional form is not, so
  tense and mood are only partly ruled.
