# Nooma — Foundational documentation

Nooma is a personal digital brain: a self-contained Go binary operating on a portable
**vault** (a folder with SQLite inside) per user. It captures what you tell it, connects it,
lets it decay or revives it, consolidates at night, reaches out in time, and learns from how
you react — all auditable, all yours.

These documents are the basis for development. Read them in order, except that newcomers should
read [07-functional.md](07-functional.md) right after 00:

| Doc | What it defines |
|-----|-----------------|
| [00-vision.md](00-vision.md) | What Nooma is, principles, positioning, license |
| [07-functional.md](07-functional.md) | What Nooma does, in narrated flows and a status table. Newcomers: read it right after 00 |
| [01-architecture.md](01-architecture.md) | Binary + vault, three layers, CLI, config, channels, providers |
| [02-cognitive-core.md](02-cognitive-core.md) | **The canonical specification of the brain** — stack-independent invariants |
| [03-data-model.md](03-data-model.md) | Complete SQLite schema (embeddings + FTS5), conventions |
| [04-decisions.md](04-decisions.md) | Status board for decisions D1–D10 |
| [05-build-plan.md](05-build-plan.md) | Milestone order for v1 |
| [06-harness.md](06-harness.md) | How it gets built: layout, tests, CI gates |
| [adr/](adr/README.md) | Architecture Decision Records — the reasoning behind each decision |

## Reading rule

`02-cognitive-core.md` is the source of truth for behavior. If the code and that document
diverge, either the code gets fixed or the document gets updated **in the same PR** — never
left to drift silently.

## Status

M0 to M3 are closed: the binary captures, recalls, sleeps (a nightly consolidation) and speaks
(a Telegram channel with a morning digest, check-ins and ephemeral timers). `m3e` also closed:
the digest asks about a relation Nooma is unsure of and the answer settles it. All of it runs
against a real migrated vault, on Linux and Windows.

**M4, the mirror (the complete UI), is in progress.** `m4a` (Today), `m4b` (units, capture and
correction), `m4c` (focus held by hysteresis), `m4e` (beliefs) and `m4e-activity` (activity) have shipped.
`m4d` (graph) is blocked on ADR-0019, still `Proposed`. `m4e2` (admin) and `m4f`
(timer list and cancel) are not built yet. M5 is the learner.

[`07-functional.md`](07-functional.md) carries the per-capability status table, and
[`05-build-plan.md`](05-build-plan.md) the milestone criteria.

Decisions D1-D10 are closed as ADRs (see the board in `04-decisions.md`).

An accepted ADR **is never edited**: if the decision changes, a new ADR supersedes it.
