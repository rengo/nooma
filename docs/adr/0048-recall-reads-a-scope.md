# ADR-0048 — A recall reads a scope: the model names the topic's words and the window, the code filters

- **Status**: Accepted (by the client through the PM, 2026-10-22, after the live run: "si digo
  dentista, que sepa que es odontólogo también")
- **Date**: 2026-10-21
- **Supersedes**: [ADR-0020](0020-recall-admission.md) in part (beside a topic the model read,
  the lexical leg admits beside the terms the model wrote), and [ADR-0024](0024-the-vault-keeps-your-words.md) point 3's "I22
  stands", in part (the chat entrance may narrow by the scope its reading carries)
- **Superseded by**: —
- **Enables**: M4

## Context

Live, with a real model, on main at 5a95833 (2026-10-21, about 18:30 -03), over a copy of the
client's vault of 24 units, five of them told on 10-19:

```
← ¿qué te conté ayer?          → No encontré nada sobre eso.
← ¿y anteayer?                 → No encontré nada sobre eso.
← ¿qué tengo esta semana?      → Encontré 2 cosas: (a past dentist, and a raw "dentista el viernes a las 10")
← ¿cuándo era lo del oculista? → Encontré 6 cosas: (the oculista and five dentist units)
```

A recall is answered by embedding the raw message and searching with it (I22). Nothing on the
path reads time: no field carries it, and nothing compares a unit's `created_at`, `event_at` or
`due_at` with anything. "¿y anteayer?" has no topic to embed, so nothing cleared ADR-0020's
floor; "ayer" was right only because nothing was told on 10-20. "¿qué tengo esta semana?" was
answered by similarity to a sentence about time, and "lo del oculista" by a floor that every
health appointment clears, since the one leg that tells the oculista apart, the lexical one, may
only rank (ADR-0020).

The client's direction: Nooma is a model operating on defined entities. The model understands
and the code acts (ADR-0033): no Go list of words for "ayer" or "esta semana".

## Decision

1. **A recall's classification carries `recall_scope`**, read by the model and optional:
   `{"terms": [...], "window": {"on": ..., "from": ..., "to": ...}}`, either part left out when
   the question does not name it.
   - `terms` are the topic asked about as single words a note about it may use, in the
     message's own language: the word, its synonyms, and each in singular and plural, with no
     question words and no time words. "lo del dentista" is `dentista`, `dentistas`,
     `odontólogo`, `odontóloga`. **The model writes the synonyms; the code never expands a
     word** (ADR-0033). The client's reason, through the PM on 2026-10-21: "si digo dentista,
     que sepa que es odontólogo también". This replaces a single `about` topic, which this ADR
     first proposed and which could only find the word the user happened to say. Single words,
     because a phrase brings its function words back into the lexical search.
   - `window` is a span of time, `from` inclusive and `to` exclusive, each written as `event_at`
     is (a timestamp with its offset, or a bare day at its midnight in the user's zone). `on`
     is `told` (the unit's `created_at`: "¿qué te conté ayer?") or `when` (its `event_at`, else
     its `due_at`: "¿qué tengo el viernes?").
   - **What the user has starts now.** "¿qué tengo esta semana?" is from the current instant
     to the Monday that starts next week: something earlier today that already happened is not
     in it (the client's ruling through the PM, 2026-10-21). The prompt says so; the code does
     not clip.
   - **The week is data, not a count.** The prompt's calendar states this week's and next
     week's spans, Monday to Sunday, computed from the clock ("This week: Monday 2026-10-19 to
     Sunday 2026-10-25"). Measured on the first prompt of this decision, with the real model,
     "¿qué tengo esta semana?" said on Wednesday 2026-10-21 ended on 2026-11-02 in two runs of
     three. Computing the span is arithmetic on the clock's instant, not a reading of words, so
     point 1's division holds.
   - A follow-up question ("¿y anteayer?") keeps the question it follows (ADR-0036) and changes
     only what it names.
2. **The scope degrades whole** (I14): a malformed part leaves no scope, and the recall answers
   as one that named none. An empty object, or terms all blank and no window, is no scope and
   no loss.
3. **The code acts on the scope, and only on it**:
   - a window and no terms lists every live unit whose instant falls in it, in order of that
     instant, up to `recall_window_limit` (20);
   - terms narrow the answering ranking, and **the lexical leg admits beside them**: a unit
     that lexically matches any term and that the vector also admitted is the answer. The
     vector leg still embeds the raw message. ADR-0020 kept the lexical leg from admitting
     because a message's function words match almost anything; a term the model wrote carries
     none. When no term matches anything the vector admitted, the answer is the vector floor's,
     as with no scope: a word the model chose is not a reason to answer with nothing. A window
     beside terms then keeps what falls in it;
   - neither is today's answer, unchanged.
4. **I22 narrows, it does not break.** With no scope, the chat, `POST /recall` and `/ui/units`
   are one mechanism over the same raw text, as before. The chat entrance alone also passes the
   scope its reading carries, since only it has a reading.

The reading (points 1 and 2) shipped first, and the code ignored it until point 3 shipped.

## Consequences

### What it enables

"¿y anteayer?", "¿qué tengo esta semana?" and "lo del oculista" each get the answer the question
asks for, decided by fields a test can check rather than by a similarity score.

### What it costs

- Every classify prompt grows by the scope's lines, so every recorded phrasing is recorded
  again.
- A synonym the model does not write still misses lexically and falls back to the vector
  floor, as today; the model's vocabulary, not a list in Go, bounds what a term finds.
- The week ends on Sunday, a convention stated in the prompt rather than configured.

### Reversal criteria

Recorded phrasings whose window or topic the model misreads in more than one case of ten, or a
live recall the scope narrows to nothing while the unsought answer was right, would call for
the ADR that supersedes this one.
