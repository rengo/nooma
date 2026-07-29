# Nooma

A personal digital brain: a self-contained Go binary over a portable per-user vault (a folder
with SQLite inside). Licensed AGPL-3.0.

**Status**: foundation. The documentation is complete; the code does not exist yet.

## Documentation

| Doc | What it defines |
|---|---|
| `docs/02-cognitive-core.md` | **Source of truth for behavior.** The brain's invariants |
| `docs/03-data-model.md` | Complete SQLite schema |
| `docs/06-harness.md` | How it gets built: layout, tests, CI gates |
| `docs/adr/` | Architecture decisions in force |
| `docs/README.md` | Full index and reading order |

## Non-negotiables

1. **`docs/02-cognitive-core.md` governs.** If the code and that doc diverge, either the code
   gets fixed or the doc gets updated **in the same PR**. Never left to drift silently.
2. **An `Accepted` ADR is never edited.** A new decision means a new ADR that supersedes it.
3. **`internal/core/` is pure**: zero I/O, zero `time.Now()`, zero external dependencies. The
   current instant arrives through the `Clock` port.
4. **A conformance test is written before its implementation**, and when it fails it is not
   weakened: fix the code, or change doc 02 + its ADR.
5. **No test touches the network or a real LLM.**
6. **Nothing is deleted in the vault**: archiving is a state transition.
7. **Safe defaults are structural, not warnings.** Without `allowed_chat_ids` the channel does
   not start; with a non-loopback bind and no token, the server does not start.

## Project skills

Load them with the `Skill` tool when their trigger applies:

| Skill | When |
|---|---|
| `nooma-core` | Touching `internal/core/**`, `internal/brain/**`, or `internal/ports/` |
| `nooma-testing` | Writing or changing tests, invariants, or `testdata/` |

Skills are a **pre-gate**: they keep you from reaching the gate. The CI gate is what
guarantees. If a rule can be an automated gate, it is a gate — not a skill.

## Workflow

`main` is protected by a GitHub ruleset: direct pushes are rejected for everyone, with no
bypass. Every change goes through a branch and a PR — one PR per step of
`docs/06-harness.md` §9, or per work unit thereafter.

Run `make check` before opening the PR. It is the fast loop (lint, L1/L2 tests, build), not
full CI parity — CI also runs `make test-integration` and `make pending-red`, plus the
coverage floor (`docs/06-harness.md` §6). Run those too before opening the PR; a `make
check-all` target that folds them into one command is tracked as a harness task, not yet
wired up.

Skills that cover the details: `work-unit-commits` (how to slice commits), `branch-pr`
(opening the PR), `chained-pr` (splitting when it exceeds 400 lines).

## Conventions

- Conventional commits. One commit = one reviewable unit of work (change + tests + doc).
- PRs with a soft ceiling of 400 lines; above that, chained PRs.
- **Everything in the repository is in English**: code, identifiers, comments, commit messages,
  docs, skills, and UI copy. This is an AGPL project expecting community contributions.
- A published migration is never modified: write the next one.
