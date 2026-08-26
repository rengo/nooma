# ADR-0020 — Recall admission: the vector admits, the lexical leg ranks

- **Status**: Accepted
- **Date**: 2026-08-26
- **Supersedes**: —
- **Superseded by**: —
- **Enables**: M3e

## Context

[ADR-0010](0010-hybrid-recall-fusion.md) decided how to **order** two ranked lists. It never
decided which results are **worth answering with**, and nothing else did either: every hit from
either leg reached the reader, ranked.

With one unit in a vault, that means every question is answered with that unit. A maintainer
asked their own brain, over Telegram:

```
Pablo:  y cuando tengo dentista?
Nooma:  Found 1 thing: • Tengo cita con el dentista el 2026-08-28 a las 09:00.
Pablo:  y cuando tengo gym?
Nooma:  Found 1 thing: • Tengo cita con el dentista el 2026-08-28 a las 09:00.
```

This does not improve as the vault grows — it changes shape. With one unit the answer is
obviously wrong. With five hundred, an unrelated question returns a page of plausible things,
confidently, and the reader can no longer tell recall from invention. **A brain that cannot say
"I don't know" is worse than one that knows less**, because nothing it says can be trusted.

Two independent causes were measured against the live vault:

1. **No similarity floor anywhere.** `recall.Search` scores the whole index by dot product and
   returns the top K; RRF then fuses by *position*. Neither has any notion of "too far" — only
   of "nearest".
2. **The lexical leg admits on function words.** `recall.Tokenize` lowercases and splits on
   non-alphanumerics, with no stopword handling. `"cuando tengo gym"` tokenises to
   `[cuando, tengo, gym]`, and against that vault `MATCH 'tengo'` returns the dentist while
   `MATCH 'gym'` returns nothing. A vector floor alone would therefore not have fixed the
   observed answer.

## Options evaluated

| Option | Pro | Con |
|---|---|---|
| Vector floor only | One number, one place | Does not fix it: the lexical leg still admits on `tengo` |
| Stopword lists | Keeps the lexical leg's rescue value intact | Language-specific, and this vault already mixes Spanish and English. Needs language detection or several lists, and ages badly |
| Discriminative-token test (IDF-like) | Language-agnostic, adapts to content | Unstable on a small vault: with one unit every token appears in 100% of it, so the test says nothing until there is volume |
| **The vector admits, the lexical leg ranks** | Closes both causes at once, with no list to maintain and no number that depends on corpus size | Gives up the lexical leg's rescue role: a rare proper noun, code or acronym the embedding misses no longer surfaces on its own |

## Decision

**For answering a recall — and only there — a result is admitted by the vector leg and ordered
with help from the lexical one.**

1. A unit is a candidate answer only if its cosine similarity to the query clears
   `RecallMinSimilarity`. Vectors are unit-normalised at the storage boundary, so the dot
   product already IS a cosine in `[-1, 1]` and a threshold is meaningful.
2. The lexical leg no longer admits. It still contributes its ranking, so an admitted unit that
   also matches lexically ranks above one that does not — which is what hybrid fusion is for
   once admission is settled.
3. `RecallMinSimilarity` is a named constant in `internal/core/recall` and a row in
   `docs/02-cognitive-core.md` §13, like every other behavioural number.

**Scoped to the recall answer, deliberately.** ADR-0010 warns that its fusion feeds three
consumers — recall, capture-time dedup candidates, and the nightly `connect` phase — and that
"a bias here propagates into the entire relation graph". Dedup and `connect` keep ADR-0010
unchanged: their question is "what might be related", which is exactly the question a generous
net should answer, and a judge decides afterwards. Only the reader-facing answer needs a floor,
because only there is a bad match presented as a fact.

This does not supersede ADR-0010. RRF remains the fusion for all three consumers; this answers
a question ADR-0010 left open.

## Consequences

### What it enables

- Nooma can say it does not know, which is a prerequisite for being trusted about what it does
  know.
- M5's learner gets a real signal: a query that admitted nothing is a datum about the vault,
  not silence.

### What it costs

- **The lexical leg stops rescuing.** A query naming a rare proper noun, an error code or an
  acronym whose embedding is poor will no longer surface that unit on lexical evidence alone.
  This is the real price of the decision and it is paid knowingly.
- One more calibratable number, and the wrong value silences legitimate answers. It is set from
  reasoning rather than measurement — there is no recall golden set with similarity scores yet —
  so the first honest calibration comes from use.

### Reversal criteria

Evidence that the lexical rescue mattered: queries that a reader expected to answer and did
not, where the missing unit matched lexically and scored below the floor. That is a concrete,
collectable observation rather than a feeling, and it points at the discriminative-token option
above, which becomes viable once a vault is large enough for token frequencies to mean
something.
