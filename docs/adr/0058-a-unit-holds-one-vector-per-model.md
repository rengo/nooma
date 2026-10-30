# ADR-0058 — A unit holds one vector per model

- **Status**: Proposed
- **Date**: 2026-10-30
- **Supersedes**: [ADR-0003](0003-embeddings.md)'s amendment, in part: reindex as "an ordinary
  `UPDATE` loop", complete when no row of the old model remains
- **Superseded by**: —
- **Enables**: M6 (`nooma reindex`, link D2)

## Context

[ADR-0003](0003-embeddings.md) ratified three invariants: the vault records the embedding model,
vectors from two models never mix, and a change of model needs an explicit `nooma reindex`. Its
amendment, written when [ADR-0012](0012-vector-proximity-search.md) replaced sqlite-vec with an
ordinary table, turned reindex into an `UPDATE` loop over `unit_embeddings`. The table was keyed
by `unit_id` alone, so re-embedding a unit under a new model **replaced** its old vector.

That shape has a cost the amendment did not weigh. Serve reads the configured model's vectors
only (I21). While a reindex runs, every unit it has rewritten has left the old model's index
and every unit it has not reached is missing from the new one. Whichever model serve reads, an
interrupted reindex leaves recall degraded until it finishes. Going back to the old model means
paying for every vector a second time. And the old vectors, derived from the user's own words,
are gone. They can be recomputed, but nothing is deleted in this vault (non-negotiable 6) does
not argue for discarding them.

## Options evaluated

| Option | Tradeoff |
|---|---|
| Keep the key on `unit_id`; reindex overwrites | No migration; a reindex in progress degrades the running model's recall, and a switch back re-pays every vector |
| **Key on `(unit_id, model)`; reindex adds the new model's rows** | One table rebuild (migration 0016); the running index stays whole during a reindex, a switch back is free, nothing is lost; each model costs its own space (about 6 KB per unit at 1536 dimensions) |
| A separate table per model | Schema changes per model, and the query filtering I21 already relies on would become table selection |

## Decision

1. **`unit_embeddings` is keyed by `(unit_id, model)`.** Migration 0016 rebuilds the table:
   it copies every row, drops the old table and renames the copy. The model index and the
   `ON DELETE CASCADE` from `units` are kept. Nothing is lost.
2. **A write replaces the unit's vector under its own model only** (`ON CONFLICT(unit_id,
   model)`). A unit's vectors under other models are kept.
3. **Reads are unchanged in meaning.** `LoadIndex(model)` returns that model's vectors.
   `CountLiveWithoutEmbedding(model)` counts the live units with no row under that model. Recall
   filters on the model, as I21 requires.
4. **A reindex (D2) adds the new model's rows beside the old ones.** It is complete for a model
   when no live unit lacks a row under it. It is resumable from the data alone, and it never
   takes away the index the running model reads.
5. **No old model's rows are removed by any command in this decision.** A way to drop a model's
   vectors on purpose, if storage ever demands one, is a separate decision.

## Consequences

### What it enables

- A reindex can be interrupted at any point, and serve keeps the complete index of the model it
  was running.
- Returning to an earlier model costs nothing: its vectors are still there.
- Doctor's coverage stays exact per model, and its line about other models counts every live
  unit holding a vector under each.

### What it costs

- The table holds one row per unit per model ever used: roughly 6 KB per unit per 1536-dimension
  model. A vault of 10 000 units that tried two models holds about 120 MB of vectors.
- Migration 0016 rewrites the table once, on the first serve after the upgrade; for a large vault
  that is a few seconds at start.

### Reversal criteria

A vault whose vectors under unused models matter for space, which would call for an explicit
command to drop one model's rows, never an automatic one.
