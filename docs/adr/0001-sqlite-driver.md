# ADR-0001 — SQLite driver and the cross-compilation promise

- **Status**: Accepted — spike run 2026-07-28, results at the end of this document
- **Date**: 2026-07-27
- **Supersedes**: —
- **Superseded by**: —
- **Enables**: M0 (and with it, everything else)
- **Related**: [ADR-0012](0012-vector-proximity-search.md) — the vector search decision this
  one wrongly assumed

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
| 6 | Hybrid recall latency (top-20 vec + top-20 FTS + fusion) | p95 < 100 ms over 10,000 units, on the reference machine below |
| 7 | Capture write throughput | ≥ 50 units/s sustained with embedding + FTS sync |

**Reference machine for criterion 6.** An earlier draft specified a Raspberry Pi 4. That
target is dropped: it was inherited from an early sketch and Nooma is not designed for
single-board hardware. The threshold is measured on a contemporary desktop or server CPU,
and the run records the exact machine so the number can be compared across runs.

The minimum supported hardware is deliberately **not decided here** — it is a product
decision, not a driver decision. But it has a deadline: it must be settled **before M6**,
because a self-hosted binary cannot ship without telling people what it needs to run. Until
then, criterion 6 is a relative measure — a regression detector — not an absolute guarantee
for any particular machine.

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

---

## Spike results — 2026-07-28

Branch `spike/adr-0001-sqlite-driver`, commit `da6a22f`, never merged. Full measurements and
the reproduction commands are in `spike/RESULTS.md` on that branch.

**Decision: `ncruces/go-sqlite3` is accepted, at its current version.**

| # | Criterion | Result |
|---|---|---|
| 1 | Vectors stored, KNN correct over 10k at dim 768 | **PASS** (see below — the mechanism changed) |
| 2 | FTS5 in sync through triggers, 1,000 mixed ops | **PASS** |
| 3 | `journal_mode=WAL`, `foreign_keys=ON`, `busy_timeout` | **PASS** |
| 4 | `VACUUM INTO` + `integrity_check` with WAL open | **PASS** |
| 5 | Cross-compilation with no C toolchain | **PASS** — 6/6 targets, `statically linked` |
| 6 | Hybrid recall p95 < 100 ms over 10k units | **PASS** — p95 17.85 ms |
| 7 | Write throughput ≥ 50 units/s | **PASS** — 2,817 units/s |

### A premise in the Context section was wrong

This document opens by asserting that *"sqlite-vec is not optional in this design"*. The spike
proved otherwise, and that is the most valuable thing it produced.

sqlite-vec only compiles against `ncruces` v0.21.3 (December 2024) and older. The driver
stopped running SQLite as a swappable WebAssembly blob and now machine-translates it to Go
with `wasm2go`, so the `sqlite3.Binary` hook that sqlite-vec depends on no longer exists.
Adopting it would have meant freezing the entire storage layer 20 months in the past,
including SQLite itself at 3.47.0.

Replacing it with a brute-force dot product in Go — vectors as ordinary `BLOB`s, an in-memory
index, roughly forty lines — measured **faster on current software** than the pinned
combination measured on stale software:

| | v0.21.3 + sqlite-vec | v0.35.2 + brute force |
|---|---|---|
| SQLite | 3.47.0 | **3.53.3** |
| Recall p95 | 21.72 ms | **17.85 ms** |
| Write throughput | 1,296 units/s | **2,817 units/s** |

That is a separate decision from the driver, and it gets its own record:
[ADR-0012](0012-vector-proximity-search.md). This ADR should never have folded it into
criterion 1.

### Two implementation facts the criteria did not ask for

- **FTS5 is opt-in per connection**, not compiled in: `ext/fts5.Register(db)`. The store must
  register it on **every** connection it opens. One that skips it fails with
  `no such module: fts5` only when an FTS query runs — late, and far from the cause. This
  needs an integration test, not a code comment.
- **Loadable extensions moved rather than disappeared.** `sqlite3.ExtensionInit(db, mod.New,
  mod.DylinkInfo)` is how the driver's own FTS5 works, so sqlite-vec is not permanently out of
  reach — it would need porting to that mechanism instead of shipping a `.wasm`.

### Cost accepted

The binary grows from ~9 MB to **16 MB**: `wasm2go` emits considerably more Go than the
previous wasm runtime carried. Still one static file, still no toolchain, and recorded here
rather than discovered at release time.

### What this unblocks

The release CI pipeline and the cross-compilation matrix can now be designed. M0 starts.
