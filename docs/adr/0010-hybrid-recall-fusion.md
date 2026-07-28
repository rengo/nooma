# ADR-0010 — Hybrid recall fusion

- **Status**: Accepted
- **Date**: 2026-07-27
- **Supersedes**: —
- **Superseded by**: —
- **Enables**: M1

## Context

Hybrid recall produces two ranked lists: top-K by vector similarity (sqlite-vec) and top-K
lexical (FTS5). They must be fused into a single ordered list.

This is not an implementation detail: this mechanism feeds **three** different things in the
brain — answering a `recall`, finding dedup/relation candidates during capture, and finding
candidate pairs during the consolidation `connect` phase. A bias here propagates into the
entire relation graph.

The underlying technical problem: the two scores **are not comparable**. Cosine distance lives
in [0, 2]; FTS5's `bm25()` returns negative values with no fixed bound that depend on the
corpus. Adding them raw means nothing.

## Options evaluated

| Option | Pro | Con |
|---|---|---|
| Reciprocal Rank Fusion (RRF) | Uses position only, not score: immune to incomparable scales. Zero tuning. Robust and well studied | Discards magnitude: an overwhelmingly better semantic match counts the same as a marginally better one |
| Weighted score (α·semantic + β·lexical) | Preserves magnitude; calibratable | Requires normalizing two incomparable scales AND calibrating α/β with no real data. Two compounded sources of error |

## Decision

**RRF with `k = 60`** (the value from Cormack et al.'s original publication, and the de facto
industry default).

```
score(d) = Σ  1 / (k + rank_i(d))
          i
```

where `rank_i(d)` is the position of `d` in list `i` (1-indexed), and documents appearing in
only one list contribute a single term.

**The fusion behavior is pinned by tests against a golden set** before the rest of the recall
pipeline exists. That golden set — a small corpus of units with queries and the expected order
— becomes the regression detector for recall quality. Without it, "recall got worse" is a
feeling instead of a failing test.

`k = 60` and each list's relative weight stay as named constants, not literals buried in the
fusion function: when real data arrives, they get calibrated in exactly one place.

## Consequences

### What it enables

- Zero calibration to get started. RRF performs reasonably well with no data, which is exactly
  the situation of a project with no users.
- Fusion is a **pure function** over two lists of IDs: it gets tested with no SQLite, no
  embeddings, and no network.
- The three consumers (recall, capture dedup, nightly `connect`) share one mechanism with one
  semantics.

### What it costs

- Real information is discarded: RRF cannot distinguish a near-exact match from a mediocre one
  if both ranked first in their list. In dedup — where "is this a duplicate?" depends precisely
  on how strong the match is — that loss matters.
- **Mitigation**: the LLM judge in the dedup/relation step receives the full candidate unit and
  decides with the content in front of it, not with the score. RRF orders the candidates; the
  judge decides. The lost magnitude is recovered where it is needed.

### Reversal criteria

Real usage data showing RRF ordering badly in an identifiable pattern — typically queries where
an exact lexical match should dominate and gets diluted. In that case, a weighted score with
α/β calibrated **against the golden set that will exist by then**, which is what we lack today
and what makes it impossible to choose well now.
