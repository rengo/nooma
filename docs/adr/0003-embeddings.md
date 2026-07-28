# ADR-0003 — Embedding generation

- **Status**: Accepted
- **Date**: 2026-07-27
- **Supersedes**: —
- **Superseded by**: —
- **Enables**: M1

## Context

sqlite-vec **stores and searches** vectors; it does not generate them. Hybrid recall needs them
on every capture (to index) and on every recall (to query). It is the hottest path in the
system: if embedding is slow, all of Nooma feels slow.

## Options evaluated

| Option | Pro | Con |
|---|---|---|
| Ollama's embedding API (`nomic-embed-text`, etc.) | Local, simple, good quality, no cgo | Requires Ollama running |
| In-process ONNX (MiniLM/BGE small) | Genuinely self-contained | A cgo binding, or immature pure-Go ONNX — back to the ADR-0001 problem |
| Cloud (OpenAI, Voyage) | Quality, zero local setup | Every memory passes through the cloud: direct tension with private-by-default |

## Decision

**`EmbeddingProvider` is a first-class interface, in the same hierarchy as `LLMProvider`.** v1
ships two backends: Ollama and cloud. In-process ONNX stays as later exploration, behind the
same interface.

Three invariants already stated in [`../03-data-model.md`](../03-data-model.md) are ratified:

1. **The vault records the embedding model and its dimension** in metadata, at creation time.
2. **Embeddings from different models never mix in the same table.** A `nomic-embed-text`
   vector and a `text-embedding-3-small` vector do not live in the same space; the distance
   between them is noise shaped like a number.
3. **Changing model requires `nooma reindex`**: an explicit command, with progress and
   confirmation. It is not an automatic migration at startup.

A technical consequence of (2) to keep in mind at implementation time: the `vec0` virtual table
fixes its dimension at `CREATE`. Reindexing to a different dimension means **recreating the
table**, not a bulk `UPDATE`. `reindex` is a schema operation, not a data operation.

## Consequences

### What it enables

- Changing embedding provider is config plus one command, not a manual migration.
- Recall unit tests use a deterministic fake `EmbeddingProvider`: hybrid recall gets tested with
  no network and no model.

### What it costs

- A user who picks cloud embeddings sends the content of every unit to a third party. The
  wizard has to say so plainly — it is the most direct tension with the private-by-default
  principle, and hiding it would betray the product.
- `reindex` over a large vault is expensive (one embedding call per unit). It has to be
  resumable, not an all-or-nothing operation.

### Reversal criteria

Pure-Go ONNX maturing enough to run a MiniLM/BGE small in-process with acceptable latency. It
would be the only option satisfying *self-contained* and *private* at the same time, and would
justify a new ADR.

---

## Amendment — 2026-07-28

The decision stands: `EmbeddingProvider` as a first-class interface, Ollama and cloud in v1,
and the three invariants about recording the model and requiring an explicit `nooma reindex`.

### Correction: reindex is not a schema operation

The Decision section says the `vec0` virtual table fixes its dimension at `CREATE`, so
reindexing to a different dimension means recreating the table, making `reindex` a schema
operation rather than a data operation.

That was true of sqlite-vec. [ADR-0012](0012-vector-proximity-search.md) dropped sqlite-vec:
embeddings are now an ordinary `BLOB` column with `model` and `dim` alongside them, per row.

The consequences invert:

- Dimension is **per-row data**, not table structure. Nothing needs recreating.
- `reindex` becomes an ordinary `UPDATE` loop — which makes it **resumable and incremental**
  for free, satisfying the requirement already stated under "What it costs" that it must not
  be all-or-nothing.
- A vault can briefly hold rows from two models mid-reindex. Invariant (2) — never mix
  embeddings from different models — therefore moves from being enforced by the schema to
  being enforced by the query: **searches filter on `model`**, and reindex is complete when no
  row carries the old one.

That last point is the one worth watching. Invariant (2) used to be guaranteed by construction
and is now a rule the code has to keep. It belongs in the conformance suite.
