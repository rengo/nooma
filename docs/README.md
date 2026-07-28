# Nooma — Foundational documentation

Nooma is a personal digital brain: a self-contained Go binary operating on a portable
**vault** (a folder with SQLite inside) per user. It captures what you tell it, connects it,
lets it decay or revives it, consolidates at night, reaches out in time, and learns from how
you react — all auditable, all yours.

These documents are the basis for development. Read them in order:

| Doc | What it defines |
|-----|-----------------|
| [00-vision.md](00-vision.md) | What Nooma is, principles, positioning, license |
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

Foundation. No code yet.

Decisions D1–D10 are closed as ADRs (see the board in `04-decisions.md`). **ADR-0001** (SQLite
driver) was the last to close, accepted after the M0 spike measured `ncruces/go-sqlite3`
against the alternatives (see ADR-0012, which replaced the `sqlite-vec` assumption). No code
exists yet.

An accepted ADR **is never edited**: if the decision changes, a new ADR supersedes it.
