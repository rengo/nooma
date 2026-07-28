# ADR-0005 — v1 scope

- **Status**: Accepted
- **Date**: 2026-07-27
- **Supersedes**: —
- **Superseded by**: —
- **Enables**: M1 onward (it defines what gets built)

## Context

The complete system has many modules: capture, recall, relations, consolidation, prospection,
timers, self-model, learning, glass box, UI, channels, multi-format perception, measurements,
multi-tenant. Building everything before a release means never shipping; cutting badly means
shipping something indistinguishable from a filing cabinet with an LLM attached.

The question is not "how much fits" but "what is the minimum that is still Nooma".

## Options evaluated

| Cut | What remains | Risk |
|---|---|---|
| Complete cognitive loop, no perception | The differentiator intact | Large v1: 6 milestones before release |
| Capture + recall + UI only | Fast release | It is RAG on steroids. The differentiator disappears |
| Everything, perception included | Complete product | v1 never ships |

## Decision

**v1 is the complete cognitive loop. Perception moves to v2.**

- **v1**: capture + `classify`, hybrid recall, relations + judge, nightly consolidation with
  its 8 phases, prospection (3 trigger kinds, digest + push, quiet hours), ephemeral timers,
  self-model (seed + derive + injection), learning (signals + thresholds + cadence), glass box,
  complete UI, Telegram channel.
- **v2**: multi-format perception + `measurements` + tracking UI + voice transcription.
- **Deferred with no date**: multi-tenant mode, extra channels, markdown as input.

The criterion: Nooma's differentiator is the **loop** — weigh → decay → connect → consolidate →
nudge → learn. Every link you cut does not make v1 smaller, it makes it something else. Remove
learning and it is a brain that never improves. Remove consolidation and it is a database with
embeddings. Remove prospection and it is a filing cabinet. Perception, by contrast, is an
**organ** attached to the loop, not a link in it: it can be amputated without the system
ceasing to be what it is.

## Consequences

### What it enables

- v1 can be demonstrated in one sentence: "you capture for three weeks and the system starts
  telling you things you never asked for". No smaller cut demonstrates that.
- The v2 seams are already in the design (`measurements` in the schema, a perception door with
  shape-based routing, channel = adapter): v2 does not require redesigning v1.

### What it costs

- **v1 is large and the risk of never shipping is real.** That is this ADR's primary risk, and
  it is mitigated in two places: every milestone in
  [`../05-build-plan.md`](../05-build-plan.md) ends in something runnable and demonstrable,
  and there is a cut order defined in advance (below).

### Cut order under pressure

If scope has to shrink, it shrinks in this order and no other:

1. **UI (M4) down to a subset**: only `/ui` (today/focus), `/ui/capture`, and `/ui/activity`.
   Graph, beliefs, and admin defer to v1.1. The UI is surface, not cognition.
2. **Pattern watchers**: keep goal stagnation only, defer load accumulation.
3. **Nothing else.** Capture, consolidation, prospection, and learning are not cut: they are
   the loop.

### Reversal criteria

M1 or M2 revealing that the complete loop is more expensive than estimated by a large factor.
In that case a new ADR is written that cuts explicitly, using the order above — scope is not
reduced silently during implementation.
