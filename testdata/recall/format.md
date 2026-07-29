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
      "content": "The espresso machine's descale cycle takes about 25 minutes."
    },
    {
      "id": "unit-002",
      "type": "task",
      "content": "Buy descaling solution for the espresso machine."
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

## Fields

| Field | Type | Required | Meaning |
|---|---|---|---|
| `id` | string | yes | Unique identifier for this case; must match the case's filename (see naming convention above) |
| `units` | array of unit | yes, at least 1 | The self-contained corpus this case's queries run against |
| `units[].id` | string | yes | Unique within this case; referenced by `queries[].expected_unit_ids` |
| `units[].type` | string | yes | One of `docs/03-data-model.md`'s `units.type` taxonomy: `task \| mental_load \| event \| knowledge \| procedural \| emotional \| list \| structured_ref \| insight`. Not validated by the loader — enum membership is a review-time concern, not a mechanized one (see "What the loader does not check" below) |
| `units[].content` | string | yes | The unit's text content, realistic enough to exercise both vector and lexical matching |
| `queries` | array of query | yes, at least 1 | The searches this case runs against `units` |
| `queries[].query` | string | yes | The natural-language search text |
| `queries[].expected_unit_ids` | array of string | yes, at least 1 | The expected fused order, most relevant first (ADR-0010's RRF output) |

## Cross-field constraint

Every ID in `queries[].expected_unit_ids` must equal the `id` of some entry
in this same case's `units` array. **This is documented, not mechanized**:
`Load` (`test/support/goldenset`) validates JSON shape only — it does not
cross-check that a referenced unit ID actually exists in the case. A case
with a dangling reference will load successfully; catching that is left to
review or to a future, separate check.

## Acceptance rules the loader enforces

- The file must be valid JSON, decodable into `goldenset.RecallExample`.
- An unknown field anywhere in the document — top-level or nested inside
  `units`/`queries` entries — is rejected
  (`json.Decoder.DisallowUnknownFields`, `test/support/goldenset/loader.go`).
- `Load` validates one file at a time; "one file per case" is a corpus
  convention this format.md documents, not something `Load` itself checks
  (there is no directory-listing step inside `Load`).
