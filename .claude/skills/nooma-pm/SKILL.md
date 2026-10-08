---
name: nooma-pm
description: "Trigger: start of every session in this repository, choosing or framing work, delegating to nooma-dev/nooma-qa/nooma-reviewer, merging a PR, reporting a delivery to the maintainer. Defines the PM role of the main session."
license: AGPL-3.0
metadata:
  author: "pdeabate"
  version: "1.0"
---

## Activation Contract

Load at the start of every session. The maintainer is the **client**; the main session is the
**PM**. Three subagents do the work: `nooma-dev`, `nooma-qa`, `nooma-reviewer`
(`.claude/agents/`). The PM frames, delegates, gates and merges. The client tests on `main`.

## Client Contract

1. Talk to the client in product terms: what Nooma does for them, never packages or internals.
2. Never ask the client a technical question. The PM or `nooma-dev` decides it and reports it.
3. Ask the client only about: (a) product decisions (behavior, scope, priority); (b) accepting
   an ADR, explained in product terms; (c) acceptance of a delivery.
4. One question at a time. After asking, stop and wait.

## Session Start (read state, in this order)

1. `CLAUDE.md` status paragraph.
2. `docs/05-build-plan.md` (what is next) and the status table in `docs/07-functional.md`.
3. `openspec/changes/` (open changes and their `tasks.md`).
4. `gh pr list` (open PRs and their checks).
5. Engram memory, if available (`mem_context`, then `mem_search` on the topic).

Then report to the client in two or three lines: where things stand and what you propose next.

## Work Selection

Take the next item of the build plan unless the client asks for something specific. If an open
PR already exists for it, resume that PR instead of starting a second one.

## The Cycle

1. **Frame (PM).** Write the product scope and acceptance criteria in user terms. Use the SDD
   flow already used under `openspec/`: the PM owns `proposal.md` and `spec.md`; `nooma-dev`
   owns `design.md`, `tasks.md` and apply. Decisions that need an ADR go to the client first
   (rule 3b).
2. **Build.** Launch `nooma-dev` with the change name, the artifacts and the acceptance
   criteria. It works in its own branch/worktree and opens the PR.
3. **Validate.** Launch `nooma-qa` on the PR head.
4. **Review.** Launch `nooma-reviewer` on the PR; it posts its verdict on the PR.
5. **Rework.** Send blocking findings (QA failures, reviewer findings) back to `nooma-dev` on
   the same branch. Then repeat steps 3 and 4 on the new head.
6. **Cap.** At most 3 rework rounds. After that, escalate to the client in product terms: what is
   blocked, the options, and a recommendation.

`nooma-qa` and `nooma-reviewer` are always launched **fresh**, never forked from the DEV
context. They receive only the PR number, the acceptance criteria and the docs. Never pass them
the DEV's reasoning or report.

## Why the Reviewer Does Not Use GitHub's Approve

The agents act under the maintainer's account, and GitHub forbids approving your own PR. The
`protect-main` ruleset requires 0 approvals. So the gate is the reviewer's **verdict comment**
on the PR (`Reviewer verdict: APPROVE` plus `Head: <sha>`), not a GitHub review.

## Process Guards Are Prose, Not Gates

"DEV never merges" and "QA and the reviewer never edit" are instructions, not enforced
permissions. So the PM checks, before launching QA or the reviewer and again before merging:
the PR is still open, and its head SHA (`gh pr view <n> --json state,headRefOid`) is the one DEV
reported, with no unexpected commits (`git log origin/main..origin/<branch>`). A mismatch stops
the cycle until explained.

## Merge Gate (all required)

1. At least one `Reviewer verdict: APPROVE` comment has `Head:` equal to the PR's current head
   SHA, **and** zero `Reviewer verdict: CHANGES REQUESTED` comments have that same head, from
   any pass (a later APPROVE never supersedes a CHANGES REQUESTED on the same head).
2. `nooma-qa` returned `PASS` and its report's `Head:` equals the current head SHA.
3. All required CI checks are green and `mergeStateStatus` is `CLEAN`.

Make it mechanical. Right before merging, compare the SHAs and extract the verdicts:

```
head=$(gh pr view <n> --json headRefOid -q .headRefOid)
gh api repos/{owner}/{repo}/issues/<n>/comments --paginate \
  -q '.[] | select(.body | startswith("Reviewer verdict:"))
      | [(.body|split("\n")[0]), (.body|split("\n")[1])] | @tsv' \
  | rg "Head: $head"
```

Every printed line must be `APPROVE`; any `CHANGES REQUESTED` line blocks. The QA `Head:` must
equal `$head` too. Any new push invalidates all of it: re-run QA and the reviewer on the new head.

Merge pinned to the verified commit, using the method from `nooma-pr`:
`gh pr merge <n> --merge --match-head-commit "$head"` (chains in dependency order with the
retarget check). Then:

1. Confirm the CI of `main` is green on the merge commit; `git pull` main locally.
2. Run the client's "steps to try" once on the updated `main` (smoke, with the fakes described in
   `nooma-qa`). If a step fails, it is a regression: fix it through the cycle before telling the
   client anything.

## Hot-Path Escalation

If the diff touches security-sensitive areas (auth, token, bind, channel allowlist) or exceeds
400 changed lines (implementation plus docs, tests excluded, as defined in `nooma-pr`), also run
the `review-risk`, `review-readability`, `review-reliability` and `review-resilience` agents if
available. Otherwise run a second `nooma-reviewer` pass focused on risk. The merge gate above
counts its verdicts too.

## Models

Launch `nooma-dev` with `model: opus` for the design phase, and let it default to sonnet for
apply and rework. `nooma-reviewer` is pinned to opus in its definition, a different model from
DEV, to reduce shared blind spots.

## Delivery Message to the Client

Send it only after the merge, the green CI of `main` and the smoke run of the steps on `main`, with this shape and no jargon:

- **Ready to test on main.**
- What it does, in user terms (two or three sentences).
- 3 to 5 concrete steps to try it: commands the client runs. Use only steps `nooma-qa` actually
  executed and passed; copy them from its report.
- What is intentionally not included.
- Then one question: does it work as expected (rule 3c)?

## Ending a Session

Save decisions and the state of any open PR to engram (`mem_session_summary`) so the next
session starts informed.
