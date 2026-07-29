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
| `prompt` | string | yes | The exact prompt text sent to the provider when this response was recorded. **Fragile as a replay key**: classify's real prompt is built from active self-beliefs plus the local date and timezone (`docs/02-cognitive-core.md` §5), so matching a live prompt against this field by literal string equality will break on any fixture drift or clock difference — flagged here for whoever builds the fake provider, not solved by this corpus |
| `response` | string | exactly one of `response`/`error` | The exact raw response text, recorded once and replayed verbatim by a fake provider — never re-sent to a real provider by a test |
| `error` | string | exactly one of `response`/`error` | A recorded provider-level failure (timeout, HTTP error, rate limit) the fake provider must surface instead of a response. `docs/06-harness.md` §3 says providers are always served from fixtures, so a failure path can only ever be exercised from a recording like this one |

## Cross-field constraint

Exactly one of `response` or `error` must be set — never both, never
neither. This is mechanized: `LLMExample.Validate`
(`test/support/goldenset/types.go`) rejects a case with both empty or both
set.

`task` should name one of ADR-0002's fixed corpus categories, but this is a
documentation convention, not a mechanized constraint — `Load`
(`test/support/goldenset`) does not validate `task` against a fixed list.

A `testdata/classify/` case may set its own `llm_case_id` to this case's
`id`, to trace a malformed classify output back to the exact recording it
degrades from (see `testdata/classify/format.md`'s cross-field section).
No field here points back to `classify/` — the reference is one-directional
by design, since one recording can back more than one classify case.

## What makes a good case

Real classify prompts vary with self-beliefs and the clock (see `prompt`
above), so a corpus of only clean, well-formed responses cannot exercise
the two things this corpus actually exists for:

- **A malformed response for each I14 shape** `testdata/classify/format.md`
  lists (truncated JSON, wrong type, unknown enum) — one recording per
  shape, linked from its classify case via `llm_case_id`.
- **At least one recorded `error` case** per plausible provider failure
  (timeout, HTTP error, rate limit) — otherwise a fake provider's failure
  path is never exercised at all, since providers never touch the network
  in a test (CLAUDE.md non-negotiable #5).
- **Coverage across the providers/models this project actually configures**
  (`provider`/`model`), not just one, so a provider-specific quirk in a
  real response's shape has a fixture to catch it.

## What the loader does and does not check

**Checked** (`test/support/goldenset/loader.go`, `DecodeStrict`):

- The file is valid JSON, decodable into `goldenset.LLMExample`.
- An unknown field anywhere in the document is rejected
  (`json.Decoder.DisallowUnknownFields`).
- `id`, `provider`, `model`, `task` and `prompt` are present and non-empty,
  and exactly one of `response`/`error` is set (`LLMExample.Validate`). A
  case gutted down to `{}`, or missing a single required field, is
  rejected with that field named in the error.

**Not checked**:

- `task` against ADR-0002's fixed corpus categories — a documentation
  convention, not a mechanized one.
- `Load` validates one file at a time; "one file per case" is a corpus
  convention this format.md documents, not something `Load` itself checks
  (there is no directory-listing step inside `Load`).
