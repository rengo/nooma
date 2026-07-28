---
name: nooma-testing
description: "Trigger: writing or changing Nooma tests, adding invariants, failing conformance, golden sets, testdata. Enforces the L1-L4 taxonomy and conformance discipline."
license: AGPL-3.0
metadata:
  author: "pdeabate"
  version: "1.1"
---

## Activation Contract

Load before writing or modifying any test in the repo, before adding an invariant to the
conformance suite, when a conformance test fails, or before touching `testdata/`.

## Hard Rules

1. No test calls the network or a real LLM. Providers are served from `testdata/llm/`.
2. No core test uses the real clock. Always a fake `Clock`.
3. A conformance test is written **before** the implementation that satisfies it, and is
   watched failing red for the right reason.
4. When a conformance test fails there are exactly two legitimate exits: fix the code, or change
   `docs/02-cognitive-core.md` **and** its ADR in the same PR. Weakening, skipping, or deleting
   the test is not one of the two.
5. Coverage floor ≥ 90 % applies only to `internal/core/`. Never write tests to inflate global
   coverage.
6. Each conformance test names the invariant and what it verifies in its identifier
   (e.g. `TestI15_OverdueTriggerExpiresAndDoesNotFire`).

## Decision Gates

| The test verifies... | Level | Location | Build tag |
|---|---|---|---|
| A pure decision function | L1 | next to the code in `internal/core/` | none |
| An invariant from doc 02 | L2 | `test/conformance/` | none |
| Migrations, `vec0`, FTS5, transactions, lockfile | L3 | `test/integration/` | `integration` |
| The compiled binary end to end | L4 | `test/e2e/` | `e2e` |

A test lives at the cheapest level where it still proves something real. If it touches SQLite
and could have been pure, it is misplaced. When torn between L1 and L3, it is L1.

## Execution Steps

1. Pick the level with the table before writing.
2. For a new invariant: add it to the table in `docs/06-harness.md` §4 with its doc 02 section
   reference, and only then write the test.
3. For structural invariants (I01, I03, I13), inspect the code tree or the schema, not behavior.
4. If the test needs data, use or extend the golden sets in `testdata/`. Never inline a large
   corpus.
5. Run with `-race`.

## Output Contract

Report: level chosen and why, invariants covered with their doc 02 section, golden sets
touched, and what was left red on purpose.

## References

- `docs/06-harness.md` — §3 taxonomy, §4 invariants I01–I20, §5 golden sets, §6 CI gates
- `docs/02-cognitive-core.md` — source of truth for behavior
- `docs/adr/0010-hybrid-recall-fusion.md` — recall golden set
- `docs/adr/0002-default-llm-preset.md` — JSON gate corpus
