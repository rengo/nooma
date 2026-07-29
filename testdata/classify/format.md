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
  }
}
```

## Fields

| Field | Type | Required | Meaning |
|---|---|---|---|
| `id` | string | yes | Unique identifier for this case; must match the case's filename (see naming convention above) |
| `input` | string | yes | The raw message text as received by capture, unmodified |
| `expected` | object | yes | The classification output `classify` must produce for `input` |
| `expected.type` | string | yes | The taxonomy type, `docs/02-cognitive-core.md` §5: `task \| mental_load \| event \| knowledge \| procedural \| emotional \| chitchat \| out_of_scope \| recall \| correction \| timer \| recurring_reminder \| list`. Not validated by the loader — enum membership is a review-time concern, not a mechanized one |
| `expected.normalized_content` | string | yes | The classifier's normalized rendering of `input` |
| `expected.structured_data` | JSON value | no | Free-form structured payload — its shape varies by `expected.type` and is not fixed by a single schema in doc 02, so the loader stores it as an opaque `json.RawMessage` rather than a typed struct |
| `expected.weight` | number | yes | Initial weight, `docs/02-cognitive-core.md` §2 |
| `expected.decay_rate` | number | yes | Initial decay rate (λ), `docs/02-cognitive-core.md` §2 |
| `expected.nudge_outcome` | string | no | `engaged \| declined` |
| `expected.relation_outcome` | string | no | `confirmed \| rejected` |
| `expected.state_outcome` | string | no | `confirmed \| denied` |
| `expected.task_checkin_outcome` | string | no | `done \| snooze \| drop` |
| `expected.list_op` | string | no | `append \| delete \| mark_done \| remove` |
| `expected.person_ref_status` | string | no | `resolved \| new \| ambiguous` |

## Cross-field constraint

The six `expected.*_outcome`/`list_op`/`person_ref_status` fields are
**orthogonal to `expected.type` and to each other**
(`docs/02-cognitive-core.md` §5: "orthogonal fields, not types" — one
message can resolve a check-in and be a capture of a different type at the
same time). None of the six is required, and there is no mutual-exclusion
rule among them today: **this is documented, not mechanized** — `Load`
(`test/support/goldenset`) validates JSON shape only, not which
combination of these fields a case populates.

## Acceptance rules the loader enforces

- The file must be valid JSON, decodable into `goldenset.ClassifyExample`.
- An unknown field anywhere in the document — top-level or nested inside
  `expected` — is rejected (`json.Decoder.DisallowUnknownFields`,
  `test/support/goldenset/loader.go`). A **truncated** JSON file is
  rejected too, by the same `json.Decoder.Decode` call, since truncated
  JSON is not valid JSON — no special-casing needed for that specific kind
  of broken case.
- `Load` validates one file at a time; "one file per case" is a corpus
  convention this format.md documents, not something `Load` itself checks
  (there is no directory-listing step inside `Load`).
