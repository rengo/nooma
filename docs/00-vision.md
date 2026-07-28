# 00 — Vision

## What Nooma is

A **personal digital brain** that runs as a single self-contained binary over a portable
per-user vault. It is not a filing cabinet that answers: it is a brain that **acts** — it
weighs what you capture, lets what you don't touch decay, connects what's related,
consolidates at night, reaches out when something warrants it, and learns from how you respond.

Two objects, never to be confused:

- **The binary** (`nooma`) — the program. Generic, identical for everyone. Like `vlc`.
- **The vault** (`pablo.nooma/`) — the brain. A folder holding one person's entire memory.
  Like the movie file. "I'm taking my brain with me" = copying the folder.

## Principles (carried into topology)

1. **Private by default as topology, not as policy.** Each brain is a separate file. There is
   no shared table to leak from: isolation is structural.
2. **Glass box as a filesystem object.** The vault opens with standard tools (`sqlite3`, an
   editor). Every automatic decision is recorded in a queryable `decision_log`. Nothing hidden.
3. **Nothing is deleted, it goes quiet.** Weight drops to ≈0, the unit is archived, but it is
   still there and can resurface. The whole vault is an object the user carries.
4. **Complexity on demand.** Zero external services by default: embedded SQLite, no cluster,
   no orchestration, no external queues. One process.

## What it is not

- **Not a GPT wrapper.** The differentiator is the dynamic model: weights, decay, relations
  with confidence, nightly consolidation, active prospection, per-user learning. An LLM is a
  replaceable part of the system, not the system.
- **Not a living wiki** (the "LLM that maintains navigable markdown" genre): that is RAG++
  without cognition. Markdown may be an optional input channel for the user's raw sources,
  never the internal structure of memory.
- **Not passive agent memory** (the save + search for coding agents genre). Nooma serves a
  human and has internal processes of its own (consolidation, proactive checking).
- **Not a therapist.** Closed product decision: Nooma takes care of **LOAD**, not emotions. It
  watches open loops and mental load (observable, practical); it does not infer or comment on
  feelings. "It looks after you" means looking after the list, not the heart.

## Reference contrast (internal use, not manifesto)

| | Nooma | Agent memory (engram-like) | LLM wiki |
|---|---|---|---|
| User | Human | Coding agent | Human (reads files) |
| Role | Active cognition (nudges, alerts) | Passive memory (recalls) | Composed wiki (synthesizes) |
| Storage | SQLite + FS | SQLite | Markdown |
| Dynamic model | Weights, decay, relations with confidence | save + search | None |
| Internal processes | Consolidation + proactive check | None | None |
| Interface | Own web UI + chat channel | API | File editor |

**Positioning note**: the public manifesto defines Nooma by what it IS, not by what it isn't.
The comparisons are internal design reference.

## The UI is part of the product

Nooma solves its own front end: **the same binary that thinks serves the user's complete
interface** — capture, focus, connection graph, self-beliefs, activity (the glass box),
measurement tracking, and administration. There is no separate frontend and no second
service: a single process delivers brain + UI. The chat channel (Telegram) is the *lean-in*
surface (capture, receive nudges); the web UI is the *lean-back* surface (explore, curate,
audit). Two surfaces, one brain.

## Distribution model

- **Open source**, downloadable binary, self-hosted. The user downloads the binary, runs
  `nooma init`, and has a brain running on their own machine or home server. Minimum hardware
  is an open product decision, due before the first public release
  ([ADR-0001](adr/0001-sqlite-driver.md)).
- **License**: AGPL-3.0 (see [ADR-0004](adr/0004-license.md)). It protects against a third
  party building a closed SaaS on top, without bothering the individual user.
- The same binary anticipates a future **multi-tenant mode** (`--multi-tenant`) for an
  eventual hosted service: one vault-file per user, connection pool with LRU, isolation by
  construction. Golden rule: any service on top talks to the brain through its public HTTP
  API — never privileged internal access, never service-specific features inside the binary's
  core.
