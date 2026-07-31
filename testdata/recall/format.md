# Recall golden-set case format

This is the regression detector for hybrid recall quality (ADR-0010,
`docs/06-harness.md` §5): a small, self-contained corpus of units with
realistic content, one or more queries against that corpus, and each
query's expected fused result order (Reciprocal Rank Fusion, ADR-0010).
It feeds all three consumers of hybrid recall — answering a `recall`,
finding dedup candidates during capture, and finding pairs during the
`connect` phase.

`cases/` holds the corpus's real case files, once populated (spec R10.1);
this file exists so whoever adds a case does not have to guess the shape.

## File naming convention

One file per case, `cases/<case-id>.json`, where `<case-id>` is the case's
own `id` field verbatim (e.g. `id: "espresso-descale"` →
`cases/espresso-descale.json`).

## Shape

```json
{
  "id": "espresso-descale",
  "units": [
    {
      "id": "unit-001",
      "type": "knowledge",
      "content": "The espresso machine's descale cycle takes about 25 minutes.",
      "status": "pool",
      "vector": [0.9, 0.1, 0]
    },
    {
      "id": "unit-002",
      "type": "task",
      "content": "Buy descaling solution for the espresso machine.",
      "status": "pool",
      "vector": [0.85, 0.2, 0]
    },
    {
      "id": "unit-003",
      "type": "insight",
      "content": "Descaling only needed every 6 months.",
      "status": "superseded",
      "vector": [0.1, 0.9, 0]
    }
  ],
  "queries": [
    {
      "query": "how long does descaling take",
      "vector": [1, 0, 0],
      "lexical_ranking": ["unit-001", "unit-002"],
      "expected_unit_ids": ["unit-001", "unit-002"]
    }
  ]
}
```

`vector` and `lexical_ranking` are both optional; this example carries them
only to illustrate the shape (see "Vector and lexical-ranking fields"
below) — their values here are not a worked fusion example. A real
lexical/vector disagreement case lives under `cases/`.

`unit-003` is deliberately part of this example, not incidental: it shares
the same topic and vocabulary as the query ("descaling") and would be a
lexical-match candidate, but its `status` is `superseded`, so it must never
appear in `expected_unit_ids` — the case I02 (`docs/02-cognitive-core.md`
§1) exists to catch. See "What makes a good case" below.

## Fields

| Field | Type | Required | Meaning |
|---|---|---|---|
| `id` | string | yes | Unique identifier for this case; must match the case's filename (see naming convention above) |
| `units` | array of unit | yes, at least 1 | The self-contained corpus this case's queries run against |
| `units[].id` | string | yes | Unique within this case; referenced by `queries[].expected_unit_ids` |
| `units[].type` | string | yes | One of `docs/03-data-model.md`'s `units.type` taxonomy: `task \| mental_load \| event \| knowledge \| procedural \| emotional \| list \| structured_ref \| insight`. Not validated by the loader — enum membership is a review-time concern, not a mechanized one (see "What the loader does and does not check" below) |
| `units[].content` | string | yes | The unit's text content, realistic enough to exercise both vector and lexical matching |
| `units[].status` | string | yes | `docs/03-data-model.md`'s `units.status` domain: `pool \| archived \| superseded \| incomplete`. Not validated by the loader — enum membership is a review-time concern, same as `units[].type` (see "What the loader does and does not check" below). Presence is enforced (a missing or `null` `status` is rejected); membership in the four-value domain is not |
| `units[].vector` | array of number | no, see below | The unit's embedding vector, stated explicitly by the case author rather than computed by `fakeprovider`'s hash-based embedder — see "Vector and lexical-ranking fields" below |
| `queries` | array of query | yes, at least 1 | The searches this case runs against `units` |
| `queries[].query` | string | yes | The natural-language search text |
| `queries[].vector` | array of number | no, see below | This query's embedding vector, stated the same way as `units[].vector` |
| `queries[].lexical_ranking` | array of string | no | The ranking the real FTS5 leg is expected to produce for this query over `units`, best match first — see "Vector and lexical-ranking fields" below |
| `queries[].expected_unit_ids` | array of string | yes, at least 1 | The expected fused order, most relevant first (ADR-0010's RRF output) |

## Vector and lexical-ranking fields

`vector` and `lexical_ranking` exist because neither ranking can be produced
any other way in this corpus (design §4.2): the only embedder any test may
use, `fakeprovider.NewEmbeddingFake`, hashes the whole input string into an
unnormalized vector with no relationship to content, so a golden order
pinned against it would pin FNV-1a, not recall; and the lexical leg needs
FTS5, which needs SQLite, which is an L3 concern, not this L2 corpus's.

So the case author states both rankings' inputs directly:

- `units[].vector` / `queries[].vector` feed `internal/core/recall.Search`
  (PR 8a) for real, at L2, with no database.
- `queries[].lexical_ranking` states the ranking the real FTS5 leg is
  expected to reproduce; it is not cross-checked against `units[].content`
  by this package (see "What the loader does and does not check" below) —
  `test/integration`'s L3 suite (PR 9c) is what confirms a case's stated
  `lexical_ranking` against the real leg.

Both fields are optional per case — a case with no ranking disagreement to
author needs neither — but **once any unit in a case carries a `vector`,
every unit and every query in that case must carry one too, and every
vector must share one length** (`RecallExample.Validate`,
`test/support/goldenset`). `lexical_ranking` carries no such rule: it may
be stated for some queries and omitted for others, independently of
`vector`.

## Cross-field constraint

Every ID in `queries[].expected_unit_ids` must equal the `id` of some entry
in this same case's `units` array, **and that entry's `status` must be
`pool`** — the same positive filter `docs/02-cognitive-core.md` §1 requires
of every live read surface. **Both of these are documented, not
mechanized**: `Load` (`test/support/goldenset`) validates JSON shape and
required-field presence only — it does not cross-check that a referenced
unit ID exists in the case, nor that it is `pool`. A case with a dangling
reference, or one that lists a `superseded`/`incomplete`/`archived` unit in
`expected_unit_ids`, will load successfully; catching either is left to
review or to a future, separate check.

## What makes a good case

`units` feeds all three consumers of hybrid recall (answering a `recall`,
dedup candidates during capture, pairs during `connect`), so a case that is
rankable by content alone proves nothing about *fusion*: a corpus where the
lexical and vector rankings already agree would produce the same
`expected_unit_ids` whether RRF ran at all or one signal were dropped. A
case earns its place in this corpus only if it also contains at least one
of:

- **A distractor**: a unit sharing vocabulary with the query (like
  `unit-003` above) that must be excluded or ranked low, so a fusion bug
  that over-weights lexical matches has something to fail against.
- **A near-duplicate pair**: two units expressing close to the same content
  differently worded, so a fusion bug that lets embeddings dominate and
  collapses them into one slot has something to fail against.
- **A lexical/vector disagreement**: a query where the best lexical match
  and the best vector match are different units, so RRF's actual
  contribution — reconciling two disagreeing rankings — is what the
  expected order pins down, not either signal alone.

A case built only from unambiguous, topically-isolated units (no
distractor, no near-duplicate, no disagreement) is a valid fixture for the
loader but a weak regression detector for ADR-0010's fusion behavior.

## What the loader does and does not check

**Checked** (`test/support/goldenset/loader.go`, `DecodeStrict`):

- The file is valid JSON, decodable into `goldenset.RecallExample`.
- An unknown field anywhere in the document — top-level or nested inside
  `units`/`queries` entries — is rejected
  (`json.Decoder.DisallowUnknownFields`).
- Every "Required: yes" field above is present and non-null/non-empty, and
  `units`/`queries`/`queries[].expected_unit_ids` each have at least 1
  entry (`RecallExample.Validate`, `RecallUnit.Validate`,
  `RecallQuery.Validate`). A case gutted down to `{}`, or missing a single
  required field, is rejected with that field named in the error.
- The `vector` cross-field rule above: once any unit carries one, every
  unit and every query must carry one too, and every vector must share one
  length (`RecallExample.validateVectors`).

**Not checked**:

- `units[].type` and `units[].status` enum membership — a value outside
  the documented domain decodes and validates fine; catching it is a
  review-time concern.
- The cross-field constraint above (`expected_unit_ids` referencing a real,
  `pool` unit).
- `queries[].lexical_ranking` against `units[].content` — the loader does
  not run FTS5 (it is L2, not L3) and cannot confirm a stated ranking is
  what the real lexical leg would produce; `test/integration`'s L3 suite
  (PR 9c) is what closes that loop.
- `Load` validates one file at a time; "one file per case" is a corpus
  convention this format.md documents, not something `Load` itself checks
  (there is no directory-listing step inside `Load`).
