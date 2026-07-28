# 04 — Decision status board

Decisions D1–D10 were closed as ADRs on 2026-07-27. **The full reasoning — context, options
evaluated, consequences, and reversal criteria — lives in [`adr/`](adr/README.md).** This
document is only the status board: what is decided, what is missing, and what blocks what.

No argumentation is duplicated here. Reasoning kept in two places desynchronizes on its own.

## Board

| # | Decision | ADR | Status | Blocks |
|---|---|---|---|---|
| D1 | SQLite driver and cross-compilation | [0001](adr/0001-sqlite-driver.md) | Accepted — spike run 2026-07-28 | M0 |
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

**Nothing.** D1–D10 are all closed. ADR-0001 was the last one and it closed by measurement on
2026-07-28, not by discussion.

Two open items live outside this board:

- **Minimum supported hardware** — a product decision, due before M6
  ([ADR-0001](adr/0001-sqlite-driver.md)). A self-hosted binary cannot ship without stating
  what it needs to run.
- **A contributor CLA** — deliberately not adopted, with the deadline recorded
  ([ADR-0011](adr/0011-contributor-licensing.md)).

## New decisions

Any later architectural decision is a new ADR numbered from 0011, with no correspondence to
this board. This document does not grow: D1–D10 is a closed set.

Already added: [0011](adr/0011-contributor-licensing.md) (contributor licensing) and
[0012](adr/0012-vector-proximity-search.md) (vector proximity search — a decision ADR-0001
assumed rather than made, which the spike forced into the open).

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
- **A crash, `SIGKILL`, or power loss leaves `-wal` and `-shm` behind with no live process.**
  A backup script that globs `*.db` and nothing else silently drops every transaction that
  still lives only in the WAL file — the vault looks backed up and is quietly missing recent
  writes. Any backup path (naive or `nooma export`) must carry the `-wal`/`-shm` siblings, or
  checkpoint first.
- **WAL requires working shared-memory locking**, which is unreliable on network filesystems
  and some FUSE/cloud-sync mounts (SQLite documents this as a **corruption** risk, not an
  error it can detect and refuse). Nooma's stated vision — "I take my brain with me = I copy
  the folder" — makes a synced folder (Dropbox, cloud-sync drives, network shares) a plausible
  place a user puts a vault, so this is a real, not theoretical, risk for this project.
- **Synchronous `classify` latency** with a slow local provider: chat capture must acknowledge
  receipt quickly even if processing takes longer. This is a channel adapter requirement, not
  a core one.
