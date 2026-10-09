# Archive report — `m4e-activity`

**Closed**: 2026-10-08. **`main` at archive**: `6c30bb4`, the merge of PR #300.
**Verify verdict**: no `sdd-verify` report is recorded in the repository for this change; the evidence is the
two PR test plans (each `make check-all` green, `go vet -tags integration,e2e ./...` clean) and a
smoke of the built binary recorded in #300's body. Stated plainly so the archive does not imply a
verdict that was not produced.
**Milestone**: the fifth M4 slice to close (after `m4a`, `m4b`, `m4c`, `m4e-beliefs`). `m4e2-admin`
remains active under `openspec/changes/`, and `openspec/changes/m4-mirror-ui/proposal.md` stays
active for `m4d` and `m4f`.

## What shipped

The user can open `/ui/activity` and read what Nooma did, newest first, 50 rows a page, with an
"older" link and a `?kind=` filter by action family. A row recording an edit shows the pre-image
(`column: previous -> next`). The page is read-only: its only form is a GET filter.

| PR | Branch | What |
|---|---|---|
| #299 | `feat/ports-store-decisionlog-before` | PR 5: `DecisionLog.Before`, the keyset newest-first read (impl+docs 104, 0.80x of ~130) |
| #300 | `feat/ui-activity` | PR 6: `ActivityService`, `/ui/activity`, the shape-driven change decoder, nav link, doc 02 §5 step 4 (impl+docs 391 in the PR body, 1.35x of ~290) |

The split plan that created this change is #294 (`plan/m4e-activity-split`), archived with
`m4e-beliefs`.

## What it leaves behind, beyond the feature

| Gate or contract | What it makes unexpressible |
|---|---|
| `Before` keyset ordered by `(occurred_at, insertion sequence)` | a page that skips or repeats a row inside a tie group larger than a page |
| `EXPLAIN QUERY PLAN` probe on `Before` | a read that scans or sorts in a temp B-tree instead of using `idx_decision_log_occurred` |
| Prefix case `x.check.y` in the repocontract | a `Before` prefix filter that means "contains" instead of "starts with" |
| G7 classification of `Before` as a read | a new `DecisionLog` method that is not classified for the read-views-write-nothing gate |
| G12 I22 whitelist gains `Page` | a UI dependency exposing a brain method beyond what the slice adds |
| Shape-driven decoder | a per-action renderer: `correction.applied`, `belief.edited` and `config.updated` (`m4e2-admin`) share one decoder |

## Cost, stated plainly

- **Multipliers (impl+docs actual over forecast).** PR 5 0.80x. PR 6 1.35x as the PR body states
  it; the merged range (`git diff --numstat 6c30bb4^1 6c30bb4`) measures 381 impl+docs lines
  (added plus deleted; excluded per design §7: tests, which include `test/support/**`, generated
  `*_templ.go` and openspec bookkeeping), the same order. Neither needed a cut; neither carries `size:exception`.
- **Tests** ran over forecast on PR 6: 739 against ~450 (1.64x), 754 by the merged range.
  Generated `*_templ.go` 333 apart.
- Judgment Day round counts are not recorded in anything checked here; not claimed.

## Verification

Per the PR bodies: strict TDD (scaffold, RED, GREEN), mutation probes recorded in the GREEN commit
bodies (A1-A3, A5, A6, A8 on #299; A4, A7 by proxy, A9-A12, G7, G8 and the unguarded-route probe on
#300), and the `x.check.y` carry-over killing `instr(...) > 0` and `strings.Contains`. #300's
smoke against the built binary with a seeded vault: three pages, 123 rows, 123 unique, newest
first; `kind=bogus`, a half cursor and a malformed cursor are 400; no cookie redirects to login;
POST is 405.

## Open follow-ups (not blocking)

- The A7 mutant (reversed `Since`) was verified by proxy in #300, not by a direct mutation.
- `docs/07-functional.md` carried activity as "in progress" until this archive PR; fixed here.

## Next

`m4e2-admin` may start (its precondition, PR 6 merged, is met), then `m4f`. `m4d` stays blocked on
ADR-0019 being `Accepted`.
