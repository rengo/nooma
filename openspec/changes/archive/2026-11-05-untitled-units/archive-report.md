# Archive report — `untitled-units`

**Closed**: 2026-11-05. **`main` at archive**: the merge of link 3 (this PR). **Verify**: not
re-run for this archive; every link is merged or in this PR and `tasks.md` has every task ticked.

## What shipped

ADR-0065 (Accepted 2026-11-05): a reply never says a dated unit by its content beside its instant,
and a dated unit with no title is named by the model. Until it is named, a reply says a plain
sentence with the instant ("Acordate: tenés algo anotado hoy a las 22:30.").

| PR | Branch | What |
|---|---|---|
| #445 | `plan/untitled-units` | link 0: ADR-0065, design, tasks |
| #446 | `feat/untitled-fallback` | link 1: `phrase.ItemOf` and the unnamed frames on push, recall, digest and Today |
| #447 | `feat/untitled-naming` | link 2: `unit_title`, `NameIfUntitled`, naming at capture and at correction |
| this PR | `feat/untitled-nightly` | link 3: the `title` phase (ninth, before `learn`), `UntitledDated`, `consolidate.title.*` |

## Decisions taken in link 3

- `UntitledDated(ctx)` takes no limit and returns live (`pool`) units only: the selection needs
  every candidate to order them, and the cap is `SelectToName`'s. Design D1 is updated.
- A provider error, an unusable answer or a failed title write is one unit's failure: logged as
  `consolidate.title.unnamed` and the pass goes on. A store or log failure aborts the phase, as in
  every other.
- The night's provider is capture_processing, bound optionally (`resolveNamer`): a vault without
  it still sleeps and the phase writes nothing; no consolidation task list gained it.
- ADR-0005's "eight phases" is a scope statement, so the ninth is recorded in doc 02 §6 and not
  in an edited Accepted ADR.

## Left open

- Link 2's task list was ticked here; its mutation probe (drop the NULL guard) was re-run in this
  PR and turned the repo-contract case red.

## What this evidence cannot prove

- Merge-time CI state of each link is not recoverable from a read today.
- No real model named a unit: every test serves a scripted provider. How well a production model
  names a legacy unit is not measured; `consolidate.title.unnamed` rows in `/ui/activity` are the
  signal.
