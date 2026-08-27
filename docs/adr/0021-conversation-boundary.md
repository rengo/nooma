# ADR-0021 — Conversation and its boundary: chitchat answers, out of scope refuses

- **Status**: Accepted
- **Date**: 2026-08-27
- **Supersedes**: —
- **Superseded by**: —
- **Enables**: M3e

## Context

`docs/02-cognitive-core.md` named `chitchat` and `out_of_scope` in §5's classification taxonomy
and then said nothing more about either. Two of the thirteen kinds had a name and no behaviour.

The implementation filled the silence, as an implementation always does. `internal/brain`
routed both to a single discard fork and returned one outcome, `discarded`, which
`internal/channels` rendered as one English sentence:

```
Pablo:  hola, todo bien?
Nooma:  Nothing to keep there.
```

Two separate defects sit in that exchange, and only one of them is about language.

**Nooma does not converse.** A greeting is not a filing error, and answering it as one is the
difference between a brain and an inbox. `chat` has been a documented task in
`internal/config` since M1 — bound by the wizard, reported by `nooma doctor`, resolvable to a
provider — and no code has ever called it. The hook was built and left empty.

**Nooma has no notion of language, anywhere.** There is no `language` or `locale` key in
`internal/config`, no language field on a unit, and no per-user setting. Every fixed sentence
the system can say is written in English in a Go source file. A Spanish speaker's brain answers
them in English, and the fix is not one sentence but a policy nobody has decided.

The two defects meet at one question: **who writes the sentence?** A fixed sentence carries the
language of whoever typed it into the source. A model's sentence carries the language of the
message it was handed.

## Options evaluated

| Option | Pro | Con |
|---|---|---|
| Keep both fixed, fix the wording only | No completion cost, no new failure mode | Nooma still does not converse, and the sentence is still English for a Spanish speaker. Defers both defects, resolves neither |
| Route both through the `chat` task | One fork, one path, and the reply lands in the sender's language for free | An `out_of_scope` handed to a model is answered plausibly. "I'll check the weather for you" from a system with no way to check the weather is a promise it cannot keep, and the person only finds out later |
| A `language` config key plus translated fixed sentences | Deterministic, no completion cost, works offline | Decides multilingualism for the whole product from its smallest surface. Needs a key, a default, a translation table and a policy for every fixed string in the binary — and still answers in the configured language when the person switched languages mid-conversation |
| **Chitchat answers through the model, out of scope refuses in a fixed sentence** | Closes both defects where each actually is. The conversational half gets the language property for free; the capability half stays unable to overpromise | Two paths where the code had one, and a greeting now costs a completion |

## Decision

**`chitchat` is answered by the model. `out_of_scope` is refused by Nooma.**

1. A `chitchat` capture calls the `chat` task with a prompt built by `internal/core/chat`, and
   the response is the reply. Nothing is persisted, nothing is embedded, no relation is judged.
2. An `out_of_scope` capture makes no model call. It answers with a fixed sentence saying Nooma
   cannot do that, and persists nothing.
3. The two are distinct `brain.CaptureOutcome` members, not one outcome with a field. Every
   surface that answers a person already switches over that vocabulary totally, so a distinct
   member is what makes a distinct sentence provable rather than conventional.
4. A `chat`-task outage degrades to the refusal sentence and records the outage. §5's product
   rule — Nooma decides with what it has and only asks when ambiguity blocks it — governs this
   call as it governs every other capture-time provider call.

**The prompt is deliberately thin.** It carries the message, the fact that Nooma is a personal
memory rather than a general assistant, and an instruction to answer in the language of the
message. It carries no beliefs, no recall, no vault content. A `chitchat` is by classification
the message that had nothing to keep in it; spending a recall on it would be paying the
expensive half of the pipeline for the kind that was routed out of the pipeline.

**This is not a multilingual policy.** It fixes the conversational half of one and buys nothing
for the other: the refusal above, every `reply.go` sentence, the digest and the CLI are still
English strings in Go files. That decision is still open, and this ADR narrows it rather than
making it — after this, the fixed sentences are the only surface left that has to choose a
language.

## Consequences

### What it enables

- Nooma answers a greeting in the language it was greeted in, with no configuration.
- The `chat` task finally has a consumer, which means `nooma doctor`'s coverage report and the
  wizard's binding of it stop describing a task nothing runs.
- M4's UI inherits a conversational surface rather than having to invent one.

### What it costs

- **A greeting spends a completion**, on a per-user vault whose owner pays per token. There is
  no cache and no local shortcut: the cheapest conversation is still one call per message.
- **A second provider call on the capture path**, and therefore a second thing that can be slow.
  It fails to the refusal rather than to an error, so the ceiling is a wrong-sounding answer,
  never a lost capture — there was nothing to lose.
- The classifier now decides which of the two paths a message takes. A greeting misread as
  `out_of_scope` is refused instead of answered, which reads as coldness rather than as a bug.

### Reversal criteria

Observed cost that the owner judges not worth it — a vault where `chitchat` completions are a
visible share of spend — points back at the first option, a fixed sentence, and forces the
multilingual decision this ADR narrowed rather than made. Symmetrically, evidence that the
refusal is wrong more often than it is right — `out_of_scope` messages a model would have
answered usefully — points at the second option and at merging the two paths.
