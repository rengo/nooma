# ADR-0024 — The vault keeps your words: a memory is stored in the language it was written in

- **Status**: Accepted
- **Date**: 2026-08-30
- **Supersedes**: —
- **Superseded by**: —
- **Enables**: M3e

## Context

[ADR-0022](0022-reply-language.md) made Nooma answer in the language it was written to. It said
nothing about what Nooma **stores**, and the omission was found the way the others were — by
reading a live vault:

```
message:  "recordame comprar cafe"
content:  "Record to buy coffee."
```

Two separate failures in five words.

**It is translated.** §5 asks for `normalized_content` as "the message rewritten as a clean,
self-contained statement" and says nothing about language. The prompt is written in English, so
the model rewrites in English. A vault whose purpose is to hold one person's memory stopped
holding what that person wrote.

**It is mistranslated.** "recordame" is *remind me*; the model read it as *record*. The stored
sentence does not mean what the message meant. Every translation is a chance to be wrong, and
this one took it on a five-word message.

**And it breaks recall, measurably.** Two facts meet:

- `brain.embedAndStore` embeds `u.Content` — the normalized, translated text.
- `RecallService.ForText` embeds the query **raw**. I22 requires exactly that: capture's own
  recall entrance and `/recall` are one mechanism, called with the same raw text, never
  `normalized_content`.

So a Spanish question is embedded in Spanish and compared against content embedded in English.
[ADR-0020](0020-recall-admission.md) then makes that vector leg the **admitting** one, with a
similarity floor: whether an answer is given at all is decided by a comparison handicapped by a
translation nobody asked for. The lexical leg cannot rescue it either — it tokenises the raw
words, which are not in the stored text.

## Options evaluated

| Option | Pro | Con |
|---|---|---|
| Store a canonical language (English), as today | One embedding space; a vault mixing languages never has to be reasoned about | It is the status quo, and the status quo is what produced the defect above. It also does not deliver the consistency it promises: the *query* is raw (I22), so canonical storage guarantees a cross-language comparison on **every** recall rather than avoiding one |
| Store both — the original and a translation | Fidelity for the reader, canonical text for the index | Two contents that can disagree, a second field on every unit, and a migration — to solve a problem the option below does not have |
| Translate the query instead, to match the store | Keeps one stored language | Adds a provider call to every recall, on the read path, and breaks I22's "one mechanism, same raw text" outright |
| **Store the message's own language** | The vault holds what its owner wrote; query and content land in the same language, so the vector comparison stops being cross-language; no new field, no migration, no extra call | A vault genuinely holds mixed languages once its owner writes in more than one, and two memories about the same thing in two languages will not recall each other well |

## Decision

**`normalized_content` is written in the language of the message. The model is told not to
translate.**

1. §5's prompt asks for the rewrite "in the same language the message was written in", and says
   why: this is what the person reads back, and what their own words are searched against.
2. Normalization keeps its job — a clean, self-contained statement — and loses only the silent
   language change. "recordame comprar cafe" becomes "Comprar café", not "Record to buy coffee."
3. Nothing else moves. No new column, no migration, no second embedding, and I22 stands
   untouched: the query stays raw, and now the stored side matches it.

**Mixed-language vaults are accepted, not solved.** Someone who writes in two languages gets two
regions of their own vault that recall each other poorly. That is a real cost and it is smaller
than the one being paid: today *every* recall is cross-language, including for a person who only
ever writes one.

## Consequences

### What it enables

- A vault that reads back in the words its owner used, which is the whole premise of a personal
  memory rather than a summarizer.
- Vector recall stops paying a translation penalty under ADR-0020's admission floor, and the
  lexical leg starts working at all — it tokenises the raw query against stored text now written
  in the same language.
- One fewer place a model can be silently wrong. A mistranslation is a corruption that no later
  correction can detect, because nothing records what the original said.

### What it costs

- **Existing units stay as they were stored.** Everything captured before this is English, and
  this decision does not rewrite it: doc 02's own rule is that nothing in the vault is deleted or
  quietly rewritten. Those units keep answering less well than the ones after them, and a
  re-normalization pass — if it is ever wanted — is its own change with its own ADR.
- A vault can hold several languages at once, and two memories about one thing in two languages
  will not find each other. Stated rather than discovered.

### Reversal criteria

Evidence that mixed-language storage costs more than the translation did: recall failing between
two memories a person considers the same, traceable to their being written in different
languages. That points at the store-both option, which becomes affordable once there is a
migration in flight for another reason.
