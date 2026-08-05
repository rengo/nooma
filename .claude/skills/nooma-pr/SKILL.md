---
name: nooma-pr
description: "Trigger: opening a PR, pushing a branch, preparing a change for review, naming a branch, merging into main. Enforces Nooma's real PR gates."
license: AGPL-3.0
metadata:
  author: "pdeabate"
  version: "1.0"
---

## Activation Contract

Load before naming a branch, opening a pull request, or merging one in this repository.

This skill governs Nooma. Do **not** apply a generic PR skill here: Nooma has no issue tracker
in use, no `PULL_REQUEST_TEMPLATE.md`, and no `type:*` or `status:approved` labels. A skill that
demands them describes a different repository.

## Hard Rules

1. `main` is protected by a ruleset with no bypass. Every change goes through a branch and a PR.
2. Run **`make check-all`** before opening — not `make check`, which is deliberately not CI parity.
3. Branch name is `<type>/<kebab-description>`. Types in use: `ci`, `docs`, `feat`, `fix`, `plan`,
   `spike`, `test`.
4. Conventional commits. One commit = one reviewable work unit: change + tests + doc together.
5. Never add `Co-Authored-By` or any AI-attribution trailer to a commit.
6. Everything is in English — code, comments, commit messages, PR title and body.
7. Never invent an issue link or a `type:*` label to satisfy an external convention. Do not create
   labels that do not exist.
8. A PR touching `internal/core/**` must change `docs/02-cognitive-core.md` in the same PR, or
   carry the `no-spec-change` label. That label does **not exist yet** — create it before relying
   on it.

## Decision Gates

| Situation | Action |
|---|---|
| Implementation + docs > 400 changed lines | Split with `chained-pr`. Test lines are counted and reported separately, not against this ceiling (`docs/06-harness.md` §7). If splitting is genuinely wrong, add the `size:exception` label and justify it in the body |
| Slicing commits | Follow `work-unit-commits` |
| `main` advanced while the PR sat | Rebase onto `origin/main` and `git push --force-with-lease`. Do not use GitHub's "Update branch" — it injects a merge commit into the diff |
| Another session holds the working tree | `git worktree add` before building or testing; `make check-all` runs `TestSchemaGolden -update` and will overwrite in-flight golden edits |
| Merging | `gh pr merge <n> --merge`. The repository has `delete_branch_on_merge` **on**, so the branch is deleted automatically — do not fight it, and do not pass `--delete-branch` either |
| Merging a chained PR | Merge links in dependency order, and after each one confirm the branch was deleted **and** that the next PR's base retargeted. See below |

## Execution Steps

1. Branch from an up-to-date `origin/main`.
2. Commit in work units, conventional format.
3. Run `make check-all`; do not open the PR until it is green.
4. `gh pr create --base main` with: summary, a changes table, and a test plan stating what was
   actually run.
5. Re-run `make check-all` after any rebase — zero textual conflicts does not prove the combined
   tree compiles.
6. Wait for all checks. Merge only on `mergeStateStatus: CLEAN`.

## Merging a Chain

**GitHub only retargets a pull request's base when the base branch is deleted.** That single fact
is the whole reason this section exists, and it is not a footnote.

On 2026-08-01 a three-link chain (#71 → #72 → #73) merged in about three minutes and **two links
landed in the wrong place**: #72 merged into `feat/core-classify-salvage` and #73 into
`feat/core-classify-vocab` instead of both reaching `main`. `main` kept 274 of the chain's 1565
lines. `delete_branch_on_merge` was off, so no base was ever deleted, so nothing retargeted, and
every merge reported success.

It is on now. That makes the mechanism work — it does not make it self-verifying. After merging
each link:

1. Confirm the merged branch is gone: `git ls-remote --heads origin <branch>` returns nothing.
2. Confirm the next link retargeted: `gh pr view <next> --json baseRefName` names `main`, not the
   branch just merged.
3. Only then merge the next link.

A chain that merges without step 2 reproduces the incident. Speed was not the cause — three
minutes was fast enough to look fine and fast enough to land two PRs in the wrong branch.

## Output Contract

Report the PR URL, the changed-line count split into implementation + docs (against the 400
ceiling) and test lines, the `make check-all` result, and any label applied with its reason.

## References

- `CLAUDE.md` — non-negotiables, workflow, conventions.
- `docs/06-harness.md` §9 — build order; §3 — the gates CI blocks on.
- `.github/workflows/docs-sync.yml` — the `internal/core` <-> doc-02 gate.
