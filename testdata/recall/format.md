# Recall golden-set case format

This is the regression detector for hybrid recall quality (ADR-0010,
`docs/06-harness.md` §5): a small, self-contained corpus of units with
realistic content, one or more queries against that corpus, and each
query's expected fused result order (Reciprocal Rank Fusion, ADR-0010).
It feeds all three consumers of hybrid recall — answering a `recall`,
finding dedup candidates during capture, and finding pairs during the
`connect` phase.

`cases/` is empty in this change. Populating it with real cases is M1's
responsibility (spec R10.1's MUST NOT); this file exists so whoever adds
the first case does not have to guess the shape.

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
      "status": "pool"
    },
    {
      "id": "unit-002",
      "type": "task",
      "content": "Buy descaling solution for the espresso machine.",
      "status": "pool"
    },
    {
      "id": "unit-003",
      "type": "insight",
      "content": "Descaling only needed every 6 months.",
      "status": "superseded"
    }
  ],
  "queries": [
    {
      "query": "how long does descaling take",
      "expected_unit_ids": ["unit-001", "unit-002"]
    }
  ]
}
```

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
| `queries` | array of query | yes, at least 1 | The searches this case runs against `units` |
| `queries[].query` | string | yes | The natural-language search text |
| `queries[].expected_unit_ids` | array of string | yes, at least 1 | The expected fused order, most relevant first (ADR-0010's RRF output) |

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

**Not checked**:

- `units[].type` and `units[].status` enum membership — a value outside
  the documented domain decodes and validates fine; catching it is a
  review-time concern.
- The cross-field constraint above (`expected_unit_ids` referencing a real,
  `pool` unit).
- `Load` validates one file at a time; "one file per case" is a corpus
  convention this format.md documents, not something `Load` itself checks
  (there is no directory-listing step inside `Load`).
