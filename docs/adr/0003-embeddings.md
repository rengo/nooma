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
