# Consolidation golden-set case format

This is the corpus M2's demo (`test/e2e`, `feat/demo-simulated-weeks`) drives through the real
capture path under a stepping fake clock, then runs one consolidation pass against — asserting
`archive`, `connect`, and `derive` each leave a legible `decision_log` row (spec R4.4/R4.5,
design §8.1/§8.3).

Unlike `testdata/recall`'s corpus, whose `units` are authored directly as final rows, this
corpus is authored as a **capture script**: raw messages plus the offset (from a simulated
`t0`) each is captured at. The corpus's own units, embeddings, and lexical index only exist
once a test drives `brain.CaptureService.Capture` per entry (design D6) — there is nothing to
author directly here, unlike `testdata/recall`'s or `testdata/classify`'s golden sets.

`cases/` holds the corpus's real case files, once populated (spec R4.1); this file exists so
whoever adds a case does not have to guess the shape.

## File naming convention

One file per case, `cases/<case-id>.json`, where `<case-id>` is the case's own `id` field
verbatim (e.g. `id: "cold-unit-gets-archived"` → `cases/cold-unit-gets-archived.json`).

## Shape

```json
{
  "id": "cold-unit-gets-archived",
  "capture_script": [
    {
      "offset": "0h",
      "text": "Remind me to buy descaling solution tomorrow",
      "llm_case_id": "classify-remind-me-tomorrow"
    },
    {
      "offset": "840h",
      "text": "Pick up the dry cleaning on Friday",
      "llm_case_id": "classify-pick-up-dry-cleaning"
    }
  ],
  "now": "2026-02-01T09:00:00Z",
  "last_run_at": "2026-01-25T09:00:00Z",
  "expected": {
    "archived": [0],
    "relations_created": [[0, 1]],
    "beliefs": [1]
  }
}
```

## Fields

| Field | Type | Required | Meaning |
|---|---|---|---|
| `id` | string | yes | Unique identifier for this case; must match the case's filename (see naming convention above) |
| `capture_script` | array of capture step | yes, at least 1 | The sequence of raw captures that builds this case's corpus, driven through `brain.CaptureService.Capture` |
| `capture_script[].offset` | string | yes | How far after the simulated `t0` this entry is captured, a Go duration string (`time.ParseDuration`, e.g. `"0h"`, `"840h"`) fed to the stepping fake clock |
| `capture_script[].text` | string | yes | The raw message text, becomes `brain.CaptureInput.Text` verbatim |
| `capture_script[].llm_case_id` | string | yes | The id of a `testdata/llm/<id>.json` case — the classify response `fakeprovider.New`'s scripted replay returns for this capture (design D7's selection-by-id, not by prompt content) |
| `now` | string | yes | The single injected instant (RFC3339) the consolidation pass runs at (spec R4.4's "one pass, one instant") |
| `last_run_at` | string | no | When present (RFC3339), seeds `config.consolidation_last_run_at` before the pass via `ConfigRepo.RecordConsolidationRun`, so `strengthen`'s `since` is non-`nil` (R4.4's `MAY`) |
| `expected` | object | yes | The effects `now`'s pass must produce against this corpus (R4.4/R4.5) |
| `expected.archived` | array of int | no | `capture_script` indices expected `ActionArchiveArchived` in `decision_log` |
| `expected.relations_created` | array of `[int, int]` | no | `capture_script` index pairs expected an `ActionConnectRelationPersisted` row between them |
| `expected.beliefs` | array of int | no | `capture_script` indices expected an `ActionDeriveBeliefCreated`/`ActionDeriveBeliefReinforced` row |

## Why indices, not unit IDs

`expected`'s three fields reference `capture_script` array positions, not `unit.Unit.ID` values:
a captured unit's ID does not exist until `CaptureService.Capture` actually runs and persists it
(design D6) — a case file, authored before any test executes, cannot know it in advance. The
corpus-driving test (`feat/demo-simulated-weeks`) is what resolves an index to the real unit ID
`Capture` returned for that entry, before asserting against `decision_log`.

## Cross-field constraint

Every index named in `expected.archived`, `expected.relations_created`, and `expected.beliefs`
must be a valid position into this same case's `capture_script` array. **This is documented, not
mechanized**: the same posture `testdata/recall/format.md`'s own cross-field section already
takes for a comparable rule. A case with a dangling index loads successfully; catching it is
left to review or to the corpus-driving test itself, which fails immediately once it tries to
resolve a nonexistent capture entry.

## What the loader does and does not check

**Checked** (`test/support/goldenset/loader.go`, `DecodeStrict`):

- The file is valid JSON, decodable into `goldenset.ConsolidationExample`.
- An unknown field anywhere in the document — top-level or nested inside `capture_script` or
  `expected` — is rejected (`json.Decoder.DisallowUnknownFields`).
- Every "Required: yes" field above is present and non-empty, and `capture_script` has at least
  1 entry (`ConsolidationExample.Validate`, `ConsolidationCapture.Validate`).

**Not checked**:

- The cross-field constraint above (`expected`'s indices referencing a real `capture_script`
  position).
- `capture_script[].offset` being a valid `time.ParseDuration` string.
- `capture_script[].llm_case_id` resolving to a real `testdata/llm/` case.
- `now` and `last_run_at` being valid RFC3339 timestamps. Parsing either is the corpus-driving
  test's job (`feat/demo-simulated-weeks`), not this format's — the same "documented, not
  mechanized" posture as the cross-field constraint above.
