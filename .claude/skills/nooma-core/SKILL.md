---
name: nooma-core
description: "Trigger: touching internal/core, internal/brain, ports, adding brain logic or calibratable constants. Enforces Nooma's dependency rule and injected clock."
license: AGPL-3.0
metadata:
  author: "pdeabate"
  version: "1.1"
---

## Activation Contract

Load before writing or modifying code in `internal/core/**` or `internal/brain/**`, before
creating a new package under `internal/`, or before adding a port in `internal/ports/`.

## Hard Rules

1. `internal/core/**` may import the standard library and its own packages, and nothing else —
   not `internal/store`, not `internal/providers`, not `internal/ports`, no external
   dependency. The `depguard` allow-list enforces this.
2. `internal/core/**` must not call `time.Now`, `time.Since`, `time.Until`, `rand.*`,
   `uuid.*`, or `os.Getenv`. The current instant and generated ids arrive as plain parameters.
   `brain/` reads `ports.Clock` **once** per operation and passes the `time.Time` down, so one
   decision sees exactly one instant.
3. The user's timezone is a parameter. Never read it from the operating system inside `core`.
4. Every behavioral number is a named constant in exactly one place and appears in the
   calibration table of `docs/02-cognitive-core.md` §13. No literals buried in functions.
5. Changing an invariant in `docs/02-cognitive-core.md` requires updating that doc and its ADR
   in the same PR.
6. An ADR with `Accepted` status is never edited. A new decision means a new ADR superseding it.

## Decision Gates

| The code... | Goes in |
|---|---|
| Decides from input data and returns data | `internal/core/` |
| Reads or writes the repo, calls a provider, publishes to a channel | `internal/brain/` |
| Defines a contract `core` or `brain` needs | `internal/ports/` |
| Speaks SQLite, HTTP, Telegram, or an LLM | the matching adapter |
| Does wiring | `cmd/nooma/` |

A consolidation phase splits in two: the decision logic in `core/consolidation`, the pass that
runs it and persists in `brain/`.

## Execution Steps

1. Place the code using the table above **before** writing it.
2. If it needs the current instant, an ID, or randomness, take it as a parameter or port.
3. If it introduces a behavioral number, declare it as a named constant and verify it is in the
   §13 table.
4. If it changes an invariant, update doc 02 and the ADR in the same PR.
5. Write to `decision_log` from `brain/`, never from `core/`.
6. Run `golangci-lint run` before considering the task done.

## Output Contract

Report: where each piece landed and under which table row, new ports, calibratable constants
added, and any invariants or ADRs touched.

## References

- `docs/06-harness.md` — §1 dependency rule, §2 the clock as a port, §7 conventions
- `docs/02-cognitive-core.md` — brain invariants, calibration table §13
- `docs/adr/README.md` — decisions in force
