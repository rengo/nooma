# ADR-0012 — Vector proximity search: brute force in Go

- **Status**: Accepted
- **Date**: 2026-07-28
- **Supersedes**: —
- **Superseded by**: —
- **Enables**: M1

## Context

Vectorizing memory is two separate jobs, and only the second one is decided here:

1. **Generating** the embedding — turning text into a vector. That is
   [ADR-0003](0003-embeddings.md): `EmbeddingProvider`, Ollama or cloud, the vault recording
   model and dimension. **Untouched by this decision.**
2. **Storing those vectors and finding the nearest ones.** This ADR.

[ADR-0001](0001-sqlite-driver.md) assumed the answer to (2) was sqlite-vec and folded it into
a spike criterion instead of deciding it. The spike proved the assumption was not just
optional but actively costly, which is what forced this ADR into existence.

Whatever is chosen feeds **three** consumers: answering a `recall`, finding dedup candidates
during capture, and finding candidate pairs in the consolidation `connect` phase. A weakness
here propagates into the whole relation graph.

## Options evaluated

| Option | Pro | Con |
|---|---|---|
| **Brute force in Go** | No dependency; works on any SQLite; vectors are ordinary BLOBs; ~40 lines; measured faster than the alternative at target scale | Vectors resident in RAM; linear in vault size; a ceiling around 50k units |
| sqlite-vec | Vectors stay on disk; scales past brute force; a real index | Only compiles against `ncruces` ≤ v0.21.3, freezing the storage layer and SQLite itself ~20 months in the past |
| A pure-Go ANN index (HNSW and similar) | Scales well, stays cgo-free | Real complexity, real dependency, and approximate results — for a problem that measurement says does not exist yet |

### The measurement

Both real options were measured on the same corpus (10,000 units, dim 768) in the ADR-0001
spike:

| | sqlite-vec (pinned stack) | Brute force (current stack) |
|---|---|---|
| SQLite | 3.47.0 (Oct 2024) | **3.53.3** |
| Recall p95 | 21.72 ms | **17.85 ms** |
| Write throughput | 1,296 units/s | **2,817 units/s** |

Brute-force scaling, measured separately:

| Units | Vector RAM | p95 |
|---|---|---|
| 1,000 | 3 MB | 0.88 ms |
| 10,000 | 29 MB | 8.30 ms |
| 50,000 | 146 MB | 43.12 ms |
| 100,000 | 293 MB | 84.74 ms |
| 500,000 | 1.4 GB | 434.57 ms |

Loading the index from SQLite at startup costs **42 ms per 10,000 vectors**.

## Decision

**Vector proximity search is a brute-force dot product over an in-memory index.** Embeddings
are stored as ordinary `BLOB` columns in a normal table; no virtual table, no extension.

Vectors are unit-normalized on write, so cosine similarity is a dot product and top-K is a
selection over the scored slice. The index is loaded from SQLite when the vault opens.

The decisive argument is not the benchmark — it is that this removes the only dependency that
was forcing the entire storage layer to stay pinned in 2024. Nooma is a personal brain: one
human, one vault. Fifty thousand units is years of continuous capture by one person, and the
measured numbers say the naive approach is not the bottleneck anywhere near that.

**The recall interface hides which mechanism is used.** `internal/core/recall` receives ranked
lists of ids and fuses them ([ADR-0010](0010-hybrid-recall-fusion.md)); it does not know
whether the vector list came from a linear scan, an index, or a virtual table. Replacing the
implementation must not touch the core.

## Consequences

### What it enables

- No dependency beyond the SQLite driver, so the storage layer tracks current SQLite and its
  security fixes.
- Vector search becomes a **pure function** over `(query, index)` — testable at L1 with no
  database, no network and no model.
- Exact results. Brute force has no recall/precision tradeoff to tune, which removes a whole
  class of "why did it not find that" investigations while the cognitive model is still being
  calibrated.
- Embeddings stay ordinary BLOBs, so the glass-box promise survives: the vault opens with
  `sqlite3` and nothing needs an extension to be read.

### What it costs

- **Vectors live in RAM.** 29 MB per 10k units at dim 768. This is the real limit, and it is
  the reason the ceiling is a memory number rather than a latency one.
- **Linear in vault size.** Comfortable to ~50k units; past that both latency and residency
  degrade together.
- **Startup pays an index load**, 42 ms per 10k vectors. Acceptable for a long-running
  process, and it must not be paid per request.
- **Multi-tenant hosting is worse off.** [ADR-0011](0011-contributor-licensing.md) records
  that a hosted service is a real plan; N resident vaults multiply the memory cost in a way
  sqlite-vec would not. Multi-tenant is deferred and out of v1 scope
  ([ADR-0005](0005-v1-scope.md)), and the LRU connection pool bounds it, but this is the
  known place where this decision ages worst.

### A consequence for ADR-0003

[ADR-0003](0003-embeddings.md) states that changing embedding model means recreating the
`vec0` table, making `nooma reindex` a schema operation. With a `BLOB` column that is no
longer true: dimension is per-row data, so reindex becomes an ordinary data operation and can
be resumable and incremental without touching the schema. Recorded as an amendment there.

### Reversal criteria

Any of:

- A real vault crossing ~50k units, or memory residency becoming a complaint.
- Multi-tenant hosting stopping being hypothetical.
- sqlite-vec being ported to the driver's current extension mechanism
  (`sqlite3.ExtensionInit` with `DylinkInfo`), which would remove the pinning objection
  entirely.

In every case the replacement lands behind the recall interface, so the cognitive core does
not change.
