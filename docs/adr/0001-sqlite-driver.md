# ADR-0001 — SQLite driver and the cross-compilation promise

- **Status**: Proposed — conditional on the M0 spike
- **Date**: 2026-07-27
- **Supersedes**: —
- **Superseded by**: —
- **Enables**: M0 (and with it, everything else)

## Context

Nooma promises a single self-contained binary, cross-compilable to Linux/macOS/Windows/ARM.
That promise collides head-on with cgo: Go's most mature SQLite driver requires it, and cgo
forces a C toolchain per target platform.

The problem is not just SQLite. The vault needs three things from the engine:

1. **sqlite-vec** — a C extension. It is what makes semantic recall work.
2. **FTS5** — compiled into SQLite. It is the lexical half of hybrid recall.
3. **`VACUUM INTO` and `PRAGMA integrity_check`** — used by `nooma export` and `nooma doctor`.

A driver that solves SQLite but cannot load C extensions is useless here: sqlite-vec is not
optional in this design.

## Options evaluated

| Option | Pro | Con |
|---|---|---|
| `mattn/go-sqlite3` (cgo) | Mature, battle-tested for years; sqlite-vec loads directly | cgo kills trivial cross-compilation; C toolchain per platform; releases depend on CI |
| `modernc.org/sqlite` (pure Go) | Clean cross-compilation, no toolchain | Loading C extensions is its weak point; sqlite-vec may not load at all |
| `ncruces/go-sqlite3` (WASM) | No cgo, supports extensions via wasm builds, a sqlite-vec build exists | A wasm layer in between: performance and maturity to be validated |

## Decision

**`ncruces/go-sqlite3` is the primary candidate, validated by a blocking spike that is the
first coding task of the project.** No other line of M0 gets written before it.

The spike is accepted only if it meets ALL of these criteria:

| # | Criterion | Threshold |
|---|---|---|
| 1 | sqlite-vec loads and `vec0` answers KNN | 10,000 vectors at the target dimension |
| 2 | FTS5 available and synchronizable via `INSERT`/`UPDATE`/`DELETE` triggers | No drift after 1,000 mixed operations |
| 3 | Operational PRAGMAs | `journal_mode=WAL`, `foreign_keys=ON`, `busy_timeout` |
| 4 | `VACUUM INTO` and `PRAGMA integrity_check` | Work on a vault with WAL open |
| 5 | Cross-compilation without a C toolchain | `linux/amd64`, `linux/arm64`, `darwin/arm64`, `windows/amd64` |
| 6 | Hybrid recall latency (top-20 vec + top-20 FTS + fusion) | p95 < 100 ms over 10,000 units, on a Raspberry Pi 4 or equivalent |
| 7 | Capture write throughput | ≥ 50 units/s sustained with embedding + FTS sync |

Criterion 6 uses modest hardware on purpose: the design contemplates running on a Raspberry.
Measuring on a development laptop hides the problem until fixing it is expensive.

**If the spike fails**, the fallback is `mattn/go-sqlite3` with per-platform releases via CI
(goreleaser + zig cc), and the public promise changes from *"cross-compile it yourself
trivially"* to *"there are official binaries for every target"*. That change of promise gets
recorded in the ADR that supersedes this one — it does not degrade silently in a README.

`modernc.org/sqlite` is discarded unless the ncruces spike fails AND mattn turns out unviable:
its weak point is precisely the non-negotiable requirement.

## Consequences

### What it enables

- The release CI pipeline can only be designed once this ADR moves to `Accepted`. Until then,
  CI runs lint and tests on `linux/amd64` only.
- Criteria 6 and 7 become permanent benchmarks in the repo, not a one-off measurement. A driver
  performance regression detects itself.

### What it costs

- The project starts with a task that may end in "this doesn't work, back up". Accepted:
  finding out in week 1 costs days; finding out in M3 costs the project.
- If ncruces wins, we accept a wasm layer on the critical path of every read and write.

### Reversal criteria

- Any spike criterion not met.
- In production: a p95 recall degrading more than 3× against the spike benchmark at real
  volumes, or a corruption bug attributable to the wasm layer.
