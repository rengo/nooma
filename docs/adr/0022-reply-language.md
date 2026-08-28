# ADR-0022 — The reply language follows the message, not a setting

- **Status**: Accepted
- **Date**: 2026-08-27
- **Supersedes**: —
- **Superseded by**: —
- **Enables**: M3e

## Context

[ADR-0021](0021-conversation-boundary.md) fixed the conversational half of a problem it named
and deliberately did not solve: **Nooma has no notion of language anywhere.** There is no
`language` or `locale` key in `internal/config`, no language field on a unit, and no per-user
setting. Every fixed sentence the binary can say is written in English in a Go source file.

A `chitchat` now comes back in the sender's language because a model wrote it. Nothing else
does. Measured against the surfaces that actually reach a person:

| Surface | Sentences | Written by |
|---|---|---|
| `internal/channels/reply.go` | ~13 | Go source, English |
| `internal/channels/runner.go` | 1 (`"Done."`) | Go source, English |
| `internal/brain/digest.go` | the digest header | Go source, English |
| `internal/channels/reply.go:115` | `t.Format("Mon 2 Jan, 15:04")` | Go stdlib, English day and month names |
| `chitchat`, `timer_rephrase` | 2 | a model — one of which says nothing about language |

So the question is not "how do we translate" but **where does the language come from**, and
that answer decides everything downstream.

## Options evaluated

| Option | Pro | Con |
|---|---|---|
| A `language` key in `nooma.yml` | Deterministic, free, works offline, and covers surfaces that arise from a clock rather than a message — the digest included | A decision per vault, not per message: writing in English gets an answer in the configured language anyway. And it is the first configuration key that decides product behaviour rather than infrastructure, which is a door worth not opening casually |
| A `self_beliefs` row, facet `preference` | Enters through the channel §10 already defines for everything Nooma knows about the person, and can be **derived** from how they write rather than configured. Would fix register, not just language | The self-model cycle is cut at both ends: nobody seeds it (onboarding is M4) and `internal/brain/capture.go` passes `nil` beliefs into every classification. Both would have to be repaired first, for a property the classifier can report today |
| Ask a second model "what language is this" | No new field in the classification | A second completion per capture to answer something the first one already had in front of it |
| **The classifier returns `language`** | `classify` already runs on **every** capture and already returns fourteen fields. This is a fifteenth: no new call, no new cost, no configuration. And the reply language follows the message, which is the same property ADR-0021 bought and for the same reason | The classifier can be wrong, and a wrong language is an answer in the wrong one. And anything Nooma says on its own initiative has no message to read, so it is not covered |

## Decision

**The classification carries the language of the message, and the answer to that message is
rendered in it.**

1. `classify.Language` is a closed vocabulary — `en`, `es` — and it names **the languages
   Nooma can speak, not the languages a person can write**. A fixed sentence exists in this
   repository or it does not; a classification naming a language no sentence exists in is a
   value nothing downstream can act on. Widening the list means writing the sentences first,
   which is the order that keeps the vocabulary honest.
2. The field is **optional on the wire**. An absent or out-of-vocabulary value degrades to
   null like every other field (I14) and the answer renders in `classify.Fallback()`, which is
   English. Optional rather than required for a concrete reason: `nooma doctor`'s quality gate
   counts a clean case as one with zero degradations, and every recording in
   `testdata/llm/cases/` predates this field — marking it required would turn a green 22/22
   into 0/22 overnight, and the alternative is editing twenty-two files whose entire value is
   being real recorded responses.
3. **The fallback is a function of nothing** — not a configured default. A fallback that read
   configuration would reintroduce the setting through the back door, on exactly the path
   where the classifier already failed.
4. **The glass box stays English.** `decision_log` rationales, error messages and every
   developer-facing string are unaffected. They serve an audit, not a person, and CLAUDE.md
   already settles their language. Where one string currently serves both audiences — the
   arming refusal — it splits in two: the typed reason travels to the renderer, and the
   English sentence stays in the trail.

## Consequences

### What it enables

- Nooma answers in the language it was written to, per message, with nothing to configure and
  nothing to keep in sync. Writing in Spanish today and English tomorrow works.
- The vocabulary becomes the honest register of what Nooma can actually say, so "add a
  language" is one reviewable act rather than a setting that quietly promises coverage that
  does not exist.

### What it costs

- **A wrong classification is now a wrong language.** Every other misclassification produces a
  wrong action; this one produces a right action said the wrong way, which is harder to notice
  and reads as a bug in something else.
- **It does not cover anything Nooma says first.** The digest arises from a clock, not from a
  message, and has no classification to read; the same is true of a fired trigger. Those speak
  the fallback until a language is carried on the unit itself — a migration, and deliberately
  not this decision's scope.
- **Dates are not fixed by this and cannot be.** Go's standard library has no locale-aware
  time formatting, so `Mon 2 Jan` stays English until a table of day and month names exists.
  Stated here rather than discovered later.
- Two sentences now exist per phrase, and nothing mechanical keeps a new one from being added
  in English only. A total switch over the vocabulary is what turns that into a compile error
  rather than a silent hole.

### Reversal criteria

Evidence that per-message detection is wrong more often than it is right — replies in a
language the person did not write in, traceable to the classification rather than to the
fallback. That points at the configuration key, whose whole advantage is that it cannot be
wrong about a person who never changes language. Symmetrically, if the surfaces Nooma speaks
first come to outnumber the ones it speaks second, the language belongs on the unit and this
decision is the wrong shape rather than an incomplete one.
