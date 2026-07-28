# 04 — Decision status board

Decisions D1–D10 were closed as ADRs on 2026-07-27. **The full reasoning — context, options
evaluated, consequences, and reversal criteria — lives in [`adr/`](adr/README.md).** This
document is only the status board: what is decided, what is missing, and what blocks what.

No argumentation is duplicated here. Reasoning kept in two places desynchronizes on its own.

## Board

| # | Decision | ADR | Status | Blocks |
|---|---|---|---|---|
| D1 | SQLite driver and cross-compilation | [0001](adr/0001-sqlite-driver.md) | **Proposed** — pending the spike | M0 |
| D2 | Default LLM preset | [0002](adr/0002-default-llm-preset.md) | Accepted | M1 |
| D3 | Embedding generation | [0003](adr/0003-embeddings.md) | Accepted | M1 |
| D4 | License: AGPL-3.0 | [0004](adr/0004-license.md) | Accepted (amended 2026-07-28) | Public release |
| D5 | v1 scope | [0005](adr/0005-v1-scope.md) | Accepted | M1 |
| D6 | v1 channel: Telegram | [0006](adr/0006-v1-channel-telegram.md) | Accepted | M3 |
| D7 | HTTP API and UI auth | [0007](adr/0007-http-auth.md) | Accepted | M4 |
| D8 | Concrete UI stack | [0008](adr/0008-ui-stack.md) | Accepted | M4 |
| D9 | Scheduler semantics under downtime | [0009](adr/0009-scheduler-downtime.md) | Accepted | M2 |
| D10 | Hybrid recall fusion | [0010](adr/0010-hybrid-recall-fusion.md) | Accepted | M1 |

## What is still open

**ADR-0001 (SQLite driver)** — the only one left, and it does not close by discussion: it
closes by measurement. The spike is the first coding task of the project and has seven
explicit acceptance criteria in the ADR. Until it runs, M0 does not start and the release CI
cannot be designed.

## New decisions

Any later architectural decision is a new ADR numbered from 0011, with no correspondence to
this board. This document does not grow: D1–D10 is a closed set.

## Known risks

These are not decisions and have no ADR, but they must not be forgotten:

- **Small-model JSON quality** in `classify` and in the judge: mitigated by the `nooma doctor`
  quality gate ([ADR-0002](adr/0002-default-llm-preset.md)) and by the explicit degradation
  already specified in [`02-cognitive-core.md`](02-cognitive-core.md) §5 — a malformed field
  degrades to null, it never aborts the whole classification.
- **Write concurrency**: SQLite has a single writer. The scheduler and capture share a process,
  so writes must be serialized (WAL + `busy_timeout` + internal per-goroutine queues). This is
  a store-layer design problem, not a config one.
- **Naive user backup** — copying the folder with the process alive and WAL open.
  `nooma export` (with `VACUUM INTO`) is the blessed path: document it early and checkpoint
  the WAL often.
- **Synchronous `classify` latency** with a slow local provider: chat capture must acknowledge
  receipt quickly even if processing takes longer. This is a channel adapter requirement, not
  a core one.
