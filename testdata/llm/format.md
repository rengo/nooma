# LLM golden-set case format

Real responses from each provider, recorded once and replayed by a fake
provider (`docs/06-harness.md` §5) — this is what lets every test in this
codebase run without the network and without a real LLM
(CLAUDE.md non-negotiable #5). It backs both `classify` (ADR-0002) and the
dedup/relation judge (`docs/02-cognitive-core.md` §5), and — per
ADR-0002 — the same recordings feed `nooma doctor`'s structured-JSON
quality gate: written once, used in two places.

`cases/` is empty in this change. Populating it with real recorded
responses is M1's responsibility (spec R10.1's MUST NOT); this file exists
so whoever adds the first case does not have to guess the shape.

## File naming convention

One file per case, `cases/<case-id>.json`, where `<case-id>` is the case's
own `id` field verbatim (e.g. `id: "classify-remind-me-tomorrow"` →
`cases/classify-remind-me-tomorrow.json`).

## Shape

```json
{
  "id": "classify-remind-me-tomorrow",
  "provider": "anthropic",
  "model": "claude-sonnet",
  "task": "classify",
  "prompt": "Classify this message: \"Remind me to buy descaling solution tomorrow\"",
  "response": "{\"type\":\"task\",\"normalized_content\":\"Buy descaling solution\"}"
}
```

## Fields

| Field | Type | Required | Meaning |
|---|---|---|---|
| `id` | string | yes | Unique identifier for this case; must match the case's filename (see naming convention above) |
| `provider` | string | yes | The provider this response was recorded from, as configured (e.g. `anthropic`, `ollama`) |
| `model` | string | yes | The model identifier string the provider reports |
| `task` | string | yes | Which pipeline call this recorded response feeds, e.g. `classify` or a judge task (`docs/02-cognitive-core.md` §5's dedup/relation judge). Not a closed enum today — ADR-0002 describes "a fixed set of classify and judge prompts" without naming every judge task, so the loader does not validate `task` against a fixed list |
| `prompt` | string | yes | The exact prompt text sent to the provider when this response was recorded |
| `response` | string | yes | The exact raw response text, recorded once and replayed verbatim by a fake provider — never re-sent to a real provider by a test |

## Cross-field constraint

None required today. `task` should name one of ADR-0002's fixed corpus
categories, but this is a documentation convention, not a mechanized
constraint — `Load` (`test/support/goldenset`) validates JSON shape only.

## Acceptance rules the loader enforces

- The file must be valid JSON, decodable into `goldenset.LLMExample`.
- An unknown field anywhere in the document is rejected
  (`json.Decoder.DisallowUnknownFields`, `test/support/goldenset/loader.go`).
- `Load` validates one file at a time; "one file per case" is a corpus
  convention this format.md documents, not something `Load` itself checks
  (there is no directory-listing step inside `Load`).
