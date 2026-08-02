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
| Diff > 400 changed lines | Split with `chained-pr`. If splitting is genuinely wrong, add the `size:exception` label and justify it in the body |
| Slicing commits | Follow `work-unit-commits` |
| `main` advanced while the PR sat | Rebase onto `origin/main` and `git push --force-with-lease`. Do not use GitHub's "Update branch" — it injects a merge commit into the diff |
| Another session holds the working tree | `git worktree add` before building or testing; `make check-all` runs `TestSchemaGolden -update` and will overwrite in-flight golden edits |
| Merging | `gh pr merge <n> --merge`. Do not delete the branch — merged branches are kept |

## Execution Steps

1. Branch from an up-to-date `origin/main`.
2. Commit in work units, conventional format.
3. Run `make check-all`; do not open the PR until it is green.
4. `gh pr create --base main` with: summary, a changes table, and a test plan stating what was
   actually run.
5. Re-run `make check-all` after any rebase — zero textual conflicts does not prove the combined
   tree compiles.
6. Wait for all checks. Merge only on `mergeStateStatus: CLEAN`.

## Output Contract

Report the PR URL, the changed-line count against the 400 ceiling, the `make check-all` result,
and any label applied with its reason.

## References

- `CLAUDE.md` — non-negotiables, workflow, conventions.
- `docs/06-harness.md` §9 — build order; §3 — the gates CI blocks on.
- `.github/workflows/docs-sync.yml` — the `internal/core` <-> doc-02 gate.
