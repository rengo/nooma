# Nooma

A personal digital brain: a self-contained Go binary over a portable per-user vault (a folder
with SQLite inside). Licensed AGPL-3.0.

**Status**: M3 closed (2026-08-24). The binary captures, recalls, sleeps and speaks. It captures
and recalls (M1): `nooma init`, configure a provider, `nooma serve`, capture via the API and via
`nooma capture`, ask a real question and get a real recall, correct what was captured. It sleeps
(M2, closed 2026-08-19): a scheduled nightly consolidation that archives what went cold, connects
what belongs together and derives beliefs, with the `decision_log` telling the story. It speaks
(M3): a Telegram channel that pushes a due trigger, delivers a morning digest, fires an ephemeral
timer and asks its own check-ins. All against a real migrated vault, on Linux and Windows.

Two M3 list items are deliberately open rather than left to be noticed — a timer's list and cancel
from chat, and which relation an inbound confirmation answers; both are named in
[`docs/05-build-plan.md`](docs/05-build-plan.md). `internal/core/` holds the brain's decision
logic; **M4 is the mirror: the complete UI**.

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
| `nooma-pr` | Naming a branch, opening a PR, or merging one |

Skills are a **pre-gate**: they keep you from reaching the gate. The CI gate is what
guarantees. If a rule can be an automated gate, it is a gate — not a skill.

## Workflow

`main` is protected by a GitHub ruleset: direct pushes are rejected for everyone, with no
bypass. Every change goes through a branch and a PR — one PR per step of
`docs/06-harness.md` §9, or per work unit thereafter.

Two commands, and the difference matters:

- **`make check`** — the fast loop: lint, L1/L2 tests, build. Seconds. Run it constantly.
- **`make check-all`** — every gate CI blocks on that a Makefile can run locally: adds L3, the
  schema-golden regeneration-diff check, the `internal/core` coverage floor, the seven-target
  cross-compile matrix (ADR-0013), and L4. **Run this before opening a PR.**

`make check` is deliberately not full CI parity, because L3, the coverage floor, the matrix and
L4 all cost real time. If you add a blocking CI job, add it to `check-all` too — unless it needs
PR metadata a Makefile cannot produce.

One CI gate `check-all` cannot cover: `docs-sync.yml`'s `internal/core/` <->
`docs/02-cognitive-core.md` sync check. It decides on a pull request's base branch and label
list, which only exist once a PR is open on GitHub. Its logic still ships as
`scripts/docs-sync.sh` and is tested directly, without GitHub Actions.

Skills that cover the details: `nooma-pr` (branch naming, opening the PR, merging),
`work-unit-commits` (how to slice commits), `chained-pr` (splitting when implementation plus docs
exceed 400 lines).

`nooma-pr` lives in this repository on purpose. An earlier version of this line pointed at a
generic `branch-pr` skill installed in one maintainer's home directory, which demanded an
approved linked issue, `type:*` labels and a PR template — none of which exist here, and one of
whose rules would have rejected `spike/` and `plan/` branches this repo already uses. A project
that expects outside contributions cannot keep its PR rules outside the project.

## Conventions

- Conventional commits. One commit = one reviewable unit of work (change + tests + doc).
- PRs with a soft ceiling of 400 lines, counted as implementation plus docs — the lines a
  reviewer must judge against the design — separately from test lines; above that, chained PRs.
  `docs/06-harness.md` §7 carries the measurement that produced the split.
- **Everything in the repository is in English**: code, identifiers, comments, commit messages,
  docs, skills, and UI copy. This is an AGPL project expecting community contributions.
- A published migration is never modified: write the next one.
