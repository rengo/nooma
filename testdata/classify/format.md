# Classify golden-set case format

This is the JSON gate corpus (ADR-0002, `docs/06-harness.md` §5): input
messages with their expected structured classification output, covering
the taxonomy in `docs/02-cognitive-core.md` §5 and its orthogonal
resolution fields. Written once, used in two places — the capture
pipeline tests, and the `nooma doctor` provider quality gate.

**Before any case is added**: the eventual corpus (`cases/`) must include
deliberately broken cases — truncated JSON, a field with the wrong type,
and an unknown enum value — because those are exactly what prove
invariant I14 (`docs/06-harness.md` §5): a malformed `classify` field
degrades to `null` and never aborts the whole classification. A corpus
made only of well-formed cases cannot prove I14.

`cases/` is empty in this change. Populating it — including the
deliberately broken cases above — is M1's responsibility (spec R10.1's
MUST NOT); this file exists so whoever adds the first case does not have
to guess the shape.

## File naming convention

One file per case, `cases/<case-id>.json`, where `<case-id>` is the case's
own `id` field verbatim (e.g. `id: "remind-me-tomorrow"` →
`cases/remind-me-tomorrow.json`).

## Shape

```json
{
  "id": "remind-me-tomorrow",
  "input": "Remind me to buy descaling solution tomorrow",
  "expected": {
    "type": "task",
    "normalized_content": "Buy descaling solution",
    "structured_data": {"due_at": "2026-07-30"},
    "weight": 1.0,
    "decay_rate": 0.01
  },
  "llm_case_id": "classify-remind-me-tomorrow"
}
```

## Fields

| Field | Type | Required | Meaning |
|---|---|---|---|
| `id` | string | yes | Unique identifier for this case; must match the case's filename (see naming convention above) |
| `input` | string | yes | The raw message text as received by capture, unmodified |
| `expected` | object | yes | The classification output `classify` must produce for `input` |
| `expected.type` | string | yes | The taxonomy type, `docs/02-cognitive-core.md` §5: `task \| mental_load \| event \| knowledge \| procedural \| emotional \| chitchat \| out_of_scope \| recall \| correction \| timer \| recurring_reminder \| list`. Not validated by the loader — enum membership is a review-time concern, not a mechanized one (see "What the loader does and does not check" below) |
| `expected.normalized_content` | string | yes | The classifier's normalized rendering of `input` |
| `expected.structured_data` | JSON value | no | Free-form structured payload — its shape varies by `expected.type` and is not fixed by a single schema in doc 02, so the loader stores it as an opaque `json.RawMessage` rather than a typed struct |
| `expected.weight` | number | yes | Initial weight, `docs/02-cognitive-core.md` §2. Persisted as `docs/03-data-model.md`'s `weight` column (same name on both sides) |
| `expected.decay_rate` | number | yes | Initial decay rate (λ), `docs/02-cognitive-core.md` §2's formula (line 29 names it `decay_rate`). **Persisted as `docs/03-data-model.md`'s `weight_decay_rate` column** — the two docs use different names for the same value; this golden set follows the formula's short name, not the column's. A missing or `null` value is rejected by the loader, not silently decoded to `0.0` (see "What the loader does and does not check" below) |
| `expected.nudge_outcome` | string | no | `engaged \| declined` |
| `expected.relation_outcome` | string | no | `confirmed \| rejected` |
| `expected.state_outcome` | string | no | `confirmed \| denied` |
| `expected.task_checkin_outcome` | string | no | `done \| snooze \| drop` |
| `expected.list_op` | string | no | `append \| delete \| mark_done \| remove` |
| `expected.person_ref_status` | string | no | `resolved \| new \| ambiguous` |
| `llm_case_id` | string | no | The `id` of a `testdata/llm/` case that recorded the malformed provider response this case's `expected` degrades from — the structural link I14 needs between the JSON gate corpus and the recorded-response corpus (see "Cross-field constraint" below) |

## Cross-field constraint

The six `expected.*_outcome`/`list_op`/`person_ref_status` fields are
**orthogonal to `expected.type` and to each other**
(`docs/02-cognitive-core.md` §5: "orthogonal fields, not types" — one
message can resolve a check-in and be a capture of a different type at the
same time). None of the six is required, and there is no mutual-exclusion
rule among them today: **this is documented, not mechanized** — `Load`
(`test/support/goldenset`) validates JSON shape only, not which
combination of these fields a case populates.

**`llm_case_id`, when set, must equal the `id` of an existing
`testdata/llm/` case** — that recording is what a malformed-degradation
case (I14) traces back to, instead of leaving the connection to an
informal naming echo between the two corpora. This is proven for the two
checked-in `format_example.json` fixtures by
`TestClassifyExampleLinksToLLMExample`
(`test/support/goldenset/loader_test.go`) — a structural, not cosmetic,
link. Once `cases/` is populated (M1), the equivalent check across real
case files is left to review or to a future, separate check, the same way
the recall corpus's dangling-reference check is.

## What makes a good case

A well-formed case with no malformed input at all cannot prove I14
(`docs/06-harness.md` §5): the corpus must include, at minimum, one case
per malformed shape —

- **Truncated JSON**: an incomplete document, backed by an `llm/` recording
  whose `response` is itself truncated.
- **A field with the wrong type**: e.g. `expected.weight` recorded as a
  string, backed by an `llm/` recording whose `response` has the same
  defect.
- **An unknown enum value**: `expected.type` set to a value outside the
  taxonomy above.

Each of these should set `llm_case_id` to the recording that produced the
malformed shape, so the connection between "classify received this" and
"the provider actually said this" is explicit, not asserted informally.

## What the loader does and does not check

**Checked** (`test/support/goldenset/loader.go`, `DecodeStrict`):

- The file is valid JSON, decodable into `goldenset.ClassifyExample`. A
  **truncated** JSON file is rejected too, by the same
  `json.Decoder.Decode` call, since truncated JSON is not valid JSON — no
  special-casing needed for that specific kind of broken case.
- An unknown field anywhere in the document — top-level or nested inside
  `expected` — is rejected (`json.Decoder.DisallowUnknownFields`).
- `id`, `input`, `expected.type`, `expected.normalized_content`,
  `expected.weight` and `expected.decay_rate` are present and
  non-null/non-empty (`ClassifyExample.Validate`,
  `ClassifyExpected.Validate`). `expected.weight`/`expected.decay_rate` use
  pointer fields internally so an absent value and an explicit `null` are
  both rejected, distinct from a legitimately present `0`.

**Not checked**:

- `expected.type` enum membership, and the six `expected.*_outcome` /
  `list_op` / `person_ref_status` fields' allowed values — a review-time
  concern, not a mechanized one.
- `llm_case_id` resolving to a real, existing `testdata/llm/` case file —
  proven only for the two checked-in `format_example.json` fixtures today
  (see "Cross-field constraint" above); a future, separate check would be
  needed once `cases/` holds real files.
- `Load` validates one file at a time; "one file per case" is a corpus
  convention this format.md documents, not something `Load` itself checks
  (there is no directory-listing step inside `Load`).
