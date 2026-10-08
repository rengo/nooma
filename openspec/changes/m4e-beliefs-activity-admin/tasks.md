# Tasks — m4e: beliefs

Derived from `spec.md` (R1-R4, R9, R11-R13) and `design.md` (§1-§8, APPROVED after Judgment Day
round 3; **§10 carries the round-3 corrections, applied below**). Fifth of six slices under
`openspec/changes/m4-mirror-ui/proposal.md`. Scope is beliefs only (PRs 1-4; activity, the old PRs 5-6, is
[`m4e-activity`](../m4e-activity/tasks.md)); admin is [`m4e2-admin`](../m4e2-admin/tasks.md),
which starts after `m4e-activity`'s PR 6 merges. Shape follows
the archived m4c and m4b tasks.

**Delivery**: `ask-on-risk`, `stacked-to-main` (each branch targets `main`, rebases after the
previous merge). Strict TDD per PR: scaffold (compiles, lints, no behaviour) -> RED (fails on an
assertion, never on "undefined") -> GREEN -> docs -> mutation probes enumerated from the
**production diff**, each with its killing test from design §6. No PR tip is red. Every PR runs
`make check`, then before opening `make check-all` **and** `go vet -tags integration,e2e ./...`,
in an isolated `git worktree` at that PR's own commit. Conventional commits, no AI trailer. Budgets
are impl+docs; tests reported apart. Each GREEN commit body records its probe (mutation applied,
red output, reverted). A probe that never applied is a finding.

## Review Workload Forecast

> **Split on 2026-10-08 (the owner's pre-agreed 7-PR rule, after PR 2 measured 2.06x).** This
> section was written for six PRs (beliefs PRs 1-4 and activity PRs 5-6). Activity is now
> [`m4e-activity`](../m4e-activity/tasks.md), with its old PRs 5 and 6 moved there unchanged
> (numbers kept). This change is **beliefs only**. The forecast below is the old one for PRs 1-4,
> with the measurements and the PR 2 cut recorded.

Impl+docs excludes tests, `test/support/**`, the regenerated golden and `*_templ.go`. Test lines
are estimates (m4c measured tests at roughly 3.5x impl) and are reported apart.

| PR | Branch | Impl+docs (point) | x 1.3 | Tests (apart) | Measured | Cut seam if 1.3x > 400 / pre-defined for 1.8x |
|----|--------|------|------|------|------|------|
| 1 | `feat/ports-store-belief-status` | ~265 | ~345 | ~650 | #289: 318 changed lines when opened (1.2x); tests 1.75x | core `selfmodel` vocabulary + content bound / port + store guards |
| 2 | `feat/brain-derive-shield` | ~270 | ~351 | ~800 | 557 changed lines (2.06x); tests 1,439 (1.8x) | **cut: 2a / 2b, see below** |
| 3 | `feat/brain-belief-edit-retire` | ~260 | ~338 | ~650 | built whole: 393 changed lines (1.51x; 369 without the `tasks.md` edits); tests 1,308 (2.0x). Under 400, no cut | `ByFacet` + `Edit` / `Retire` + `write_landed.go` (unused) |
| 4 | `feat/ui-beliefs` | ~275 | ~358 | ~450 | not built; at 2.06x ~567 | GET view + wiring / the two POSTs + G6 body table |
| 5, 6 | `feat/ports-store-decisionlog-before`, `feat/ui-activity` | ~130, ~290 | ~169, ~377 | ~320, ~450 | | **Moved to [`m4e-activity`](../m4e-activity/tasks.md)** |
| | **Total (4 forecast PRs)** | **~1,070** | **~1,390** | **~2,550** | | |

Arithmetic: impl+docs 265 + 270 + 260 + 275 = 1,070; x 1.3: 345 + 351 + 338 + 358 = 1,392; tests
650 + 800 + 650 + 450 = 2,550. The old six-PR totals were ~1,490 / ~1,940 / ~3,320, and
`m4e-activity` carries the difference: 1,490 - 1,070 = 420 (130 + 290), 1,938 - 1,392 = 546
(169 + 377), 3,320 - 2,550 = 770 (320 + 450).

Sensitivity (not the umbrella's convention): x 1.8 = 477 + 486 + 468 + 495 = 1,926 (~1,930),
x 2.2 = 583 + 594 + 572 + 605 = 2,354 (~2,350); at x 1.8 all four PRs exceed 400.

### Measured (2026-10-08) and the PR 2 cut

PR 1 (#289, merged `b5a60e6`) measured **318 changed lines against ~265: 1.2x** when it was opened
(tests 1.75x). Counting the merged diff by file class after review gives 345 (1.3x); the owner's
ruling and this file use the opening measurement, and the difference does not change any cut.
PR 2 measured **557 changed lines against ~270: 2.06x** (tests 1,439 against ~800: 1.8x), over the
400 ceiling, so it was cut at the seam this file named in advance:

| Part | Branch | PR | Impl+docs | Tests (apart) | Label |
|---|---|---|---|---|---|
| 2a | `feat/brain-derive-shield` | #291, merged `582659c` | ~140 (the pure `shield.go`, no doc 02) | ~245 | `no-spec-change` (nothing calls `RouteProposals` until 2b) |
| 2b | `feat/brain-derive-retired-wiring` | #292, merged `8c88db2` | 468 (445 without 23 lines of `tasks.md` ticks) | 1,320 | `size:exception` |

The parts do not sum to 557: 2b gained about 22 lines in review (the fail-closed branch and its
doc 02 text) and the `tasks.md` ticks add 23. **Why 2b carries `size:exception`**: it measured 468
lines, about 17% over the 400 ceiling, with its one named seam already used by 2a. What remained
was one unit, the `consolidate.go` wiring and the doc 02 §6 item 5 text that describes it;
splitting them would have put the doc ahead of the code, which #291's review had moved this text
into 2b to avoid. The owner approved the exception on 2026-10-08.

**The 7-PR rule.** The umbrella rule (`proposal.md` §5: more than seven PRs or more than 2,400
budgeted lines) reads point lines and did not fire for the six-PR plan (6 PRs, ~1,490 lines). This
file's own earlier forecast named the pre-agreed remedy: if growth forces cuts and the PR count
passes seven, split beliefs (PRs 1-4) from activity (PRs 5-6). After PR 2's cut the slice was
seven PRs (1, 2a, 2b, 3, 4, 5, 6). At the measured 2.06x the later PRs are 260 x 2.06 = ~536 (PR
3), 275 x 2.06 = ~567 (PR 4) and 290 x 2.06 = ~597 (PR 6), each over 400, so three more cuts were
predicted: 7 + 3 = 10 PRs. PR 5 (130 x 2.06 = ~268) stays under. The owner ruled the split on
2026-10-08. After it, this change is PR 1, 2a, 2b, 3 and 4 (five PRs), and at most seven if PRs 3
and 4 are cut at their seams (5 + 2 = 7, which does not pass seven); `m4e-activity` is two PRs
(~420 lines, three if its PR 6 is cut). **Neither fires the rule at point**, and a cut of PR 3 or
PR 4 is a measurement-driven decision for `sdd-apply` (cut rule, below), not a new split.

| Field | Value |
|-------|-------|
| Estimated changed lines | ~1,070 impl+docs across 4 forecast PRs (5 after PR 2's cut; ~1,390 at x 1.3); tests ~2,550 apart |
| 400-line budget risk | High at the measured 2.06x (PR 3 ~536 and PR 4 ~567 exceed 400; Medium at x 1.3, largest ~358) |
| Chained PRs recommended | Yes |
| Suggested split | PR 1 -> 2a -> 2b -> PR 3 -> PR 4, all stacked to `main`; PRs 3 and 4 cut at their seams if the cut rule fires |
| Delivery strategy | ask-on-risk |
| Chain strategy | stacked-to-main |

Decision needed before apply: Yes (PR 3 and PR 4 are over 400 at the measured multiplier: cut each
at its pre-defined seam, or take `size:exception`, per `ask-on-risk`; the owner decides)
Chained PRs recommended: Yes
Chain strategy: stacked-to-main
400-line budget risk: High

The cut rule is unchanged and was exercised once: after a PR is measured, if `estimate x measured
multiplier > 400` for a later PR, cut it at its seam before `sdd-apply`.

### Suggested Work Units

| Unit | Goal | Likely PR | Notes |
|------|------|-----------|-------|
| 0 | Planning: umbrella edits, spec/design corrections | PR 0 `plan/m4e-beliefs-activity-admin`, base `main` | Not counted toward the seven-PR rule; also carries the `m4e2-admin` artifacts. The activity split is `plan/m4e-activity-split`, also not counted |
| 1 | Belief status, content bound, store guards | PR 1, base `main` | Compiles and passes alone; no surface can create a retired row yet |
| 2 | Derive shield: retired never re-derived, edited never overwritten | PR 2a then 2b, base `main` after PR 1 | Lands before any UI so the night knows how to skip first |
| 3 | `BeliefsService` edit / retire / `ByFacet` | PR 3, base `main` after PR 2b | Umbrella §5.2 row 9 lands here |
| 4 | `/ui/beliefs` and two POSTs, gates G6/G12 | PR 4, base `main` after PR 3 | Beliefs nav link only |
| 5, 6 | `DecisionLog.Before`; `/ui/activity` | **`m4e-activity`**, base `main` after PR 4 | Moved; numbers kept |

---

## Pre-task 0 — planning PR `plan/m4e-beliefs-activity-admin` (docs only, no code)

Carries both changes' spec/design/tasks. One commit per bullet group.

- [x] **0.1** Umbrella `openspec/changes/m4-mirror-ui/proposal.md`, the design header's 11 items
  (line numbers at `dc22762`): (1) slicing paragraph :292-294, `/ui/admin` leaves m4e, the :301 rule
  recorded as exercised; (2) §5.1 m4e rows :334-339 and totals: six m4e rows with design §7
  budgets plus the `m4e2-admin` rows (1a, 1b, 2); (3) §2 items 3-4 :104-111 "a config write
  (m4e)" -> m4e2; (4) §2 acceptance line :73-75, admin attributed to `m4e2-admin`, `647-648` ->
  `663-664`; (5) §4.2 row :235 `647-648` -> `663-664`; (6) §5.2 test rows :373-374, "m4e #2" /
  "m4e #4" -> PR 3 / PR 6; (7) Q5 header :462 "blocking `m4e` #5" -> "blocking `m4e2-admin`";
  (8) independence paragraph :261-262, `ConfigRepo` moves to m4e2; (9) dependency rows :258,
  :520-521, `m4e2-admin` after m4e, m4f on m4e only; (10) risk row R10 :505 "m4e #3" -> PR 5;
  (11) Q4 and Q5 rows :483-484 -> **Ruled 2026-10-07**.
- [x] **0.2** Umbrella, the two spots the design list omitted (correction 6): (12) `:150` "No undo.
  ... Doc 02 §5 step 4 (lines 647-648)" -> `663-664`; (13) `:337` row `feat/ui-activity`
  "doc 02 lines 647-648 corrected" -> `663-664`. Verify: `rg '647-648' openspec/changes/m4-mirror-ui/proposal.md`
  returns only text that deliberately quotes the old range (none expected).
- [x] **0.3** `spec.md` R11 (correction 4): list doc 02 §6 item 5 (retired shield and conditional
  embed, PR 2, which licenses the D15/D17 test rewrite) and the doc 02 §11 belief clause (PR 3).
  Verify: R11 names `§6 item 5` and `§11`.
- [x] **0.4** `spec.md` R13 (correction 9): state that a same-key proposal merges into the edited
  belief **even when another active belief is semantically nearer**; add that scenario.
- [x] **0.5** `design.md` in-place wording per its §10 (the §10 text is already appended; this
  applies it to §3.3, §3.9, §4, §5, §6, §7, §8, header): reorder precedence to 1, 2, 3
  user-stated key, 4 active nearest, 5 create (item 9) in §3.3 and §4; `usableVector` reference
  dimension (item 2); fixture p7 and mutants D19d (odd-first case) and D23 (item 9); B19
  (item 8); RK-10 rewrite cross-referencing RK-5 (item 5); §7 forecast basis sentence and table
  cell (item 3); header "depends on" list (item 13); `:1930` ripple row (item 14); `layout.templ`
  in PR 6's §5 row and "two links" -> one per PR (item 1); `server_test.go:117` in PRs 4 and 6
  (item 11); `scripted_embedder_test.go` in PR 2's §5 row (item 12); G6 body-table sentence
  (item 10); A4/A7 moved to PR 6.
- [x] **0.6** `m4e2-admin/design.md` in-place wording per its §10 (items 3, 7, 11): rename §8 risks
  R1-R6 to RK-1..RK-6 and fix references (`rg '\bR[1-6]\b'`, risk meanings only); §7 "1b exceeds
  only at 2.2x"; `server_test.go:117` in PR 2's §5 row.
- [x] **0.7** Verify: `scripts/docs-sync.sh` passes; `rg 'm4e2' openspec/changes/m4-mirror-ui/proposal.md`
  shows the new rows; no `Accepted` ADR touched.

---

## PR 1 — `feat/ports-store-belief-status` (S1-S20, G1-G3, G11)

Files: `internal/core/selfmodel/{status,content}.go`, `internal/ports/selfmodelrepo.go`,
`internal/store/sqlite/selfmodelrepo.go`, `test/support/memrepo/selfmodel.go`,
`test/support/repocontract/selfmodelrepo.go`, `internal/brain/consolidate_test.go` (seeds),
`test/conformance/{i03_units_never_deleted_test,belief_status_doc_test}.go`,
`testdata/schema/store_api.golden`, `docs/03-data-model.md`, `docs/02-cognitive-core.md` §10,
`docs/06-harness.md` §4.

- [x] **1.1** SCAFFOLD — `status.go`: `Status` (`active|retired`), `Origin`
  (`seed|derived|user_stated`), consts, `AllStatuses()`/`AllOrigins()` (the `Facet` pattern,
  `facet.go:10-43`). `content.go`: `MaxBeliefContentRunes = 1000` (doc comment cites the
  `ports.BrowsePageSize` ruling), `ErrEmptyContent`, `ErrContentTooLong`, `NormalizeText` and
  `NormalizeContent` returning their input unchanged. `ports.SelfModelRepo`: `BeliefByID`,
  `RetiredBeliefs`, `SetStatus`, `EditContent`, `ErrBeliefStatusConflict`, `ErrBeliefProtected`;
  `Belief.Status`/`.Origin` typed; rewrite the "Three methods" doc comment. sqlite and memrepo
  bodies return zero values; `scanBelief` scans through local strings.
- [x] **1.2** SCAFFOLD ripples (same commit, behaviour-neutral): `repocontract/selfmodelrepo.go:43`
  `"merged"` -> `selfmodel.StatusRetired`; every `consolidate_test.go` seed that upserts with an
  empty `Origin` (`:1772`, `:1840-1845`, `:1930`, `:2161`) gets `Origin: "derived"` **to avoid
  rule 4 treating an empty-origin seed as non-derived** (design §10 item 14; "rule 4" is the
  edited-key rule in the original numbering, rule 3 after the reorder). Verify: `make check`
  and `go vet -tags integration,e2e ./...` green, no behaviour change.
- [x] **1.3** RED core — `status_test.go`, `content_test.go` (FX-N): CRLF, trim, empty, whitespace
  only, bound-exact, bound+1, multibyte at bound, `"  " + Max + "  "` accepted, `"  a\r\nb  "`
  -> `"a\nb"`.
- [x] **1.4** RED L3 probe first (RK-1) — a sqlite test that a `DO UPDATE ... WHERE false` upsert
  reports `changes() = 0`. If it does not, stop and apply design RK-1's fallback before 1.5.
- [x] **1.5** RED contract (both implementations; FX-B with non-default confidence/timestamps and
  a retired belief in a touched facet): `RunActiveBeliefs` excludes retired; `RetiredBeliefs`;
  `BeliefByID` found (every column) and unknown; `SetStatus` happy, retire twice -> conflict
  unchanged, missing id, `updated_at = at`; `EditContent` marks `user_stated`, stale `from` ->
  conflict, retired -> conflict, full-row snapshot (only content/origin/updated_at move);
  G1: upsert over retired key and over user-stated key -> `ErrBeliefProtected` byte-identical,
  over active derived still overwrites (m2c), `ReinforceByID` retired -> conflict, missing ->
  `ErrBeliefNotFound`.
- [x] **1.6** RED conformance — `belief_status_doc_test.go` `TestBeliefStatusDocMatchesAllStatuses`
  (G3, fails: doc 03 has no comment yet); strengthen `i03_units_never_deleted_test.go` with a
  `DELETE FROM self_beliefs` marker (G2) and its probe rows (`self_beliefs_x` silent).
- [x] **1.7** GREEN core — real `NormalizeText`, `NormalizeContent` (rune count).
- [x] **1.8** GREEN sqlite — `SetStatus` and `EditContent` (SELECT first, guarded UPDATE,
  `requireRowAffected`); `UpsertByTopicKey` `DO UPDATE ... WHERE status='active' AND
  origin='derived'`, zero rows -> `ErrBeliefProtected`; `ReinforceByID ... AND status='active'`
  with disambiguating SELECT; `BeliefByID`; `RetiredBeliefs`. Memrepo mirrors every contract.
  *Applied as: guarded UPDATE first, one disambiguating SELECT only after a zero-row result
  (`explainZeroRows`) for all three, so each WHERE clause is the single decision and S2/S6/S7
  are killable (the design's own equivalent ordering, chosen for that reason).*
- [x] **1.9** `make store-api-golden`; review the diff is exactly the four methods (G11).
- [x] **1.10** DOCS — `docs/03-data-model.md`: `self_beliefs.status` comment `-- active|retired` and
  the prose line (a transition; nothing removed). `docs/02-cognitive-core.md` §10 (`:1217-1228`):
  status vocabulary, retire semantics, edit semantics, **no sentence claims injection exists**.
  `docs/06-harness.md` §4 I03 row names `self_beliefs`. Verify `scripts/docs-sync.sh`.
- [x] **1.11** PROBES from `git diff` of production files, one mutant each, with its killing test:
  S1 drop `status='active'`; S2 drop `SetStatus` `from` guard; S3 swap not-found/conflict;
  S4 omit `updated_at`; S5 omit `origin='user_stated'`; S6 drop `content = from`; S7 drop
  edit `status='active'`; S8 also write confidence; S9 drop upsert `status` guard; S10 drop
  `origin` guard and `= 'user_stated'`; S11 `changes()==0` -> nil; S12 drop reinforce guard;
  S13 `RetiredBeliefs` filter; S14 add a member on one side; S15 swap reinforce not-found/
  conflict; S16 `BeliefByID` unknown -> zero Belief; S17 omit CRLF; S18 omit `TrimSpace`; S19
  accept whitespace-only; S20 `>=`, bytes, check-before-normalise. Reconcile the diff against
  design §6 and add a row for any branch not listed.

## PR 2 — `feat/brain-derive-shield` (D1-D23, G11 n/a)

Files: `internal/core/consolidation/shield.go`(+test), `internal/brain/consolidate.go`,
`internal/brain/consolidate_test.go`, **`internal/brain/scripted_embedder_test.go`** (new),
`internal/ports/decisionlog.go`, `test/support/repocontract/decisionlog.go`,
`docs/02-cognitive-core.md` §6 item 5, `docs/06-harness.md` §4.

**Cut at the named seam (measured 2026-10-08).** The whole of PR 2 measured 557 impl+docs changed
lines (about 2.1x its ~270 forecast, over 400), so it ships as two stacked PRs. **Part 1**,
`feat/brain-derive-shield`: the pure `shield.go` only, with no doc change and the
`no-spec-change` label, since nothing calls `RouteProposals` until part 2 (tasks 2.3, 2.7; the
shield half of 2.1 and 2.10). **Part 2**, `feat/brain-derive-retired-wiring`: the `consolidate.go`
wiring, the vocabulary, the scripted embedder, the D15/D17 rewrite and all of doc 02 §6 item 5 —
the retired-shield routing text and the embedding-cost amendment that licenses that rewrite — so
the doc lands with the code it describes.

**Shipped as** #291 (part 1, merged `582659c`) and #292 (part 2, merged `8c88db2`,
`size:exception`; the Forecast section records why).

- [x] **2.1** SCAFFOLD — `shield.go`: `RouteKind`, `KeyedBelief`, `Route`, `RetiredKeyHits`
  (returns nil), `RouteProposals` (every proposal -> create). `ports/decisionlog.go`:
  `ActionDeriveBeliefSkipped`, `ActionDeriveRetiredEmbedFailed` after
  `ActionDeriveBeliefReinforced`, `AllDecisionActions` entries, comment "forty-eight" -> "fifty";
  `repocontract/decisionlog.go:133` `want` map and subtest title (two hand edits).
- [x] **2.2** SCAFFOLD test helper (correction 12) — `scripted_embedder_test.go`: a test-local
  `ports.EmbeddingProvider` with per-text vectors, per-text errors, NaN / zero / empty /
  wrong-dimension vectors, ctx-cancel support and `EmbedCalls()`.
- [x] **2.3** RED core — `shield_test.go`, precedence **1 retired key, 2 retired nearest (tie to
  retired), 3 user-stated key, 4 active nearest, 5 create** (design §10 item 9). FX-D p0-p6 plus
  **p7** (key == U's, cosine 0.95 to active A: reinforce U). Tests:
  `TestRouteProposals_RetiredKeySkipsEvenWhenFar` (p1), p2, p3, p4, p5, p6 (bitwise-equal
  mirror vectors), p7, D6 variant, `RetiredKeyHits`.
- [x] **2.4** RED brain — **rewrite, not delete or weaken**, `consolidate_test.go:1824-1871`
  `TestConsolidateRunner_Derive_EmbedsExactlyOncePerActiveBelief` -> `TestDerive_NoProposalsMakesNoEmbedCalls`
  (same `{"beliefs":[]}` fixture, asserts `EmbedCalls() == 0`, plus a variant with retired
  seeded); sibling D17 (2 active, 1 retired, one pending proposal, `EmbedCalls() == 4`); D16
  `…AllKeyDecidedMakesNoEmbedCalls`. Verify the rewrite fails on the assertion.
- [x] **2.5** RED brain — FX-D end to end (p1 not first, so remap is exercised): D7, D9 (exact key
  set per reason, `similarity` omitted on key match, `proposed_content`), D10 snapshots, D8
  `TestDerive_RetiredReadErrorFailsPhase`, D11 `TestDerive_ProtectedRaceSkipsAndContinues`, D12
  `TestDerive_RetiredDuringReinforceSkips`, D14.
- [x] **2.6** RED brain — FX-D2 with pA (shares R4's key) **and** pB (new key, far): D18 embed
  error, D19 NaN, D19b zero and empty, D19c wrong-dimension (one short, and consistently wrong),
  D20 still skipped by key, D21 cancelled ctx aborts, D22 active embed failure aborts. **D19d**
  proposed-side unusable vector: zero, non-finite, wrong-dimension; created with rationale
  "semantic comparison skipped"; user-stated-key case reinforces; **odd proposal first** with
  active beliefs present (it is the one excluded); no active beliefs and a zero-vector proposal
  first (the next usable proposal sets the dimension) (design §10 item 2).
- [x] **2.7** GREEN core — `shield.go` real `RetiredKeyHits` and `RouteProposals` in the 1-5 order.
- [x] **2.8** GREEN brain — `derive`: read active then retired (error aborts); pending list; zero
  embed calls when pending is empty; embed active (abort on failure), then retired under
  `usableVector(v, dim) cause` (**reference dimension = first active belief's vector, falling
  back to the first usable proposal when there are no active beliefs**), then pending screened
  the same way; two `MergeProposals` calls remapped to original indices; route; persist by
  `Kind`; `ErrBeliefProtected` / `ErrBeliefStatusConflict` -> skip row `changed_since_read`,
  continue; `retired_embed_failed` rows. Rewrite `embedForMerge` and its doc comment
  (`:839-852`) to the new rule.
  *Applied as: every embed call first (active, retired, pending), then the screen, because a
  retired vector can only be checked against a dimension that, with no active belief, comes from a
  proposal embedded after it. `MergeProposals` is not called at all when no proposal is usable
  (nothing to compare, and the retired vectors have no reference to be screened by). Probes found
  four behaviours the design left implicit and added their tests: a proposal's own embed error
  still aborts, any store error other than the two refusals still aborts, the judge is shown
  active beliefs only, and no usable proposal means no merge.*
- [x] **2.9** DOCS — doc 02 §6 item 5 (`:953-972`) in this same PR as the 2.4 rewrite: third dedup
  rule (retired shield: key, then nearest at 0.85, tie to retired), precedence order,
  "derive may reinforce a user-stated belief, never rewrite its text", cost note "when a
  proposal still needs a semantic comparison" and "every retired belief, which grows only by
  explicit user action", the retired-embed-failure policy sentence, **and the one added clause
  (design §10 item 5): a proposal whose vector is unusable is created, or reinforces a
  user-stated key, without a semantic comparison, and its rationale says so**. `docs/06-harness.md`
  §4 I12 row names the two new actions. Verify `scripts/docs-sync.sh`.
- [x] **2.10** PROBES D1 remove rule 1; D2 remove retired-nearest; D3 any-retired-skips; D4 remove
  user-stated-key reinforce; D5 scope to `origin != derived`; D6 order 3 before 1; **D23 swap
  user-stated-key rule with active-nearest (killed by p7)**; D7 active-only vectors; D8 swallow
  read error; D9 row shape mutants; D10 fall through to create; D11, D12 return err; D13 `>`;
  D14 union / no remap; D15-D17 embed anyway / embed all; D18-D20, D19b-d (including
  reference-dimension = first pending proposal, killed by odd-first), D21, D22. Equivalent
  mutants per design §6 are not spent time on.

## PR 3 — `feat/brain-belief-edit-retire` (B1-B19)

Files: `internal/brain/{beliefs,belief_edit,belief_retire,write_landed}.go`(+tests),
`internal/ports/decisionlog.go`, `test/support/repocontract/decisionlog.go`,
`docs/02-cognitive-core.md` §11, `docs/06-harness.md` §4.

- [x] **3.1** SCAFFOLD — `write_landed.go`: `ErrWriteLanded`, `*WriteLandedError{Record, Signal
  bool; Err error}` with `Is`/`Unwrap` (reused by m4e2; *applied as: the type and the sentinel
  are real, `Error`/`Is`/`Unwrap` start as stubs so their tests are watched failing, GREEN
  makes them real*). `beliefs.go`/`belief_edit.go`/
  `belief_retire.go`: `BeliefsService{clock, ids, beliefs, signals, log}`, `NewBeliefsService`,
  `ByFacet`, `Edit`, `Retire` returning zero values; one file per operation, one `Now()` each
  (finding 4). `ActionBeliefEdited`, `ActionBeliefRetired` appended last; "fifty" -> "fifty-two";
  repocontract `want` map and title.
- [x] **3.2** RED edit (FX-B seeded snapshots, FX-N): B1 `…UnknownIDWritesNothing`, B2
  `…SameContentWritesNothing` and `…CRLFResubmissionWritesNothing`, B3 `…RetiredBeliefWritesNothing`,
  B4 `…LogFailureLeavesBeliefUntouched`, B5 `…WriteFailureEmitsNoSignal`, B6
  `…SignalNamesBeliefAndLogRow` (prior `derived`, prior `user_stated`), B7 JSON keys, B8 bounds
  table, B15 `…SignalFailureAfterWriteIsNonFatal`, B16 normalised storage, B18
  `TestBeliefEdit_OverBoundDerivedBeliefIsEditable`.
- [x] **3.3** RED retire: B9 `TestBeliefRetire_TwiceLogsOnce`, B10, B12
  `…AlreadyRetiredConflictsBeforeAnyWrite` (counting decorators), B13 `…RetireSignalFields` (three
  origins), B14 `…RecordFailureAfterWriteIsNonFatal`, B17 `…SignalFailureAfterWriteIsNonFatal`,
  **B19 `TestBeliefRetire_RecordAndSignalFailureReportsBoth`: `Record` and the signal both fail;
  `errors.Is(err, ErrWriteLanded)` and `errors.As` gives `Record && Signal`; the belief reads
  `retired`** (design §10 item 8).
- [x] **3.4** RED `ByFacet` (B11, B11b-d): all five facets, empty as empty; order fixture over a stub
  `SelfModelRepo` returning the exact reverse, plus a second permutation, plus the 20-run
  `memrepo` flake detector.
- [x] **3.5** GREEN — `ByFacet` (`sort.SliceStable`, confidence desc, `last_reinforced_at` desc,
  `id` asc, in brain); `Edit` (normalise -> read -> no-op against `NormalizeText(stored)` ->
  pre-image `Record` -> `EditContent` -> signal); `Retire` (read -> `SetStatus` -> `Record` ->
  signal, non-fatal follow-ups). Signals: negative valence, `TargetKind` belief, `DecisionAction`
  `ActionDeriveBeliefCreated` only for prior `derived`.
- [x] **3.6** DOCS — doc 02 §11 (`:1237-1240`) belief clause ("a user's write through the mirror
  (a belief edit or retirement) is recorded too, with the value it replaced"); harness §4 I03 and
  I12 rows. Umbrella §5.2 row 9 is satisfied here. Verify `scripts/docs-sync.sh`.
- [x] **3.7** PROBES B1-B19, B11b-d each applied from the production diff (record-first swap, signal
  on failed write, plain-error mutants for windows b and c, compare against `NormalizeContent`,
  drop each sort key, keep only `Record` in the both-fail case).

## PR 4 — `feat/ui-beliefs` (U1-U4b, G6, G7, G12)

Files: `internal/ui/{beliefs.go,beliefs.templ,ui.go,layout.templ}`, `internal/httpapi/server.go`,
`cmd/nooma/{serve.go,wiring.go,wiring_beliefs_test.go,serve_test.go}`,
`test/conformance/{ui_entrances_test,httpapi_ui_wiring_test,ui_cross_origin_test,ui_read_views_write_nothing_test}.go`,
**`internal/httpapi/server_test.go`**, `docs/06-harness.md` §4.

- [ ] **4.1** SCAFFOLD — `ui.Beliefs` interface type; `wireBeliefs(db)` and `uiDeps(..., beliefs)`
  with the typed-nil guard, `serve.go` call site; `beliefs.templ` placeholder; `make templ`.
- [ ] **4.2** RED conformance (G6) — in `ui_cross_origin_test.go`: `uiCrossOriginBodies` with
  entries for the **existing** `POST /ui/capture` and `POST /ui/units/{id}/correct` (`text=hello`)
  and the new `POST /ui/beliefs/{id}/edit` (`content=new+text`) and `…/retire` (empty body);
  `uiCrossOriginBodyExempt` with `POST /ui/login` and its reason, whose refusal subtests use a
  **fixed placeholder body**; `build()` gives every mutating entrance a counting stub on one
  shared counter; `TestUICrossOriginBodiesCoverEveryPOSTRow` (missing entry, stale entry,
  empty reason, exemption with no row, pattern in both: U4, U4b). `wantUIMuxWiring` rows.
- [ ] **4.3** RED gates — add the `Beliefs` field to `ui.Deps` (G12 red: `ByFacet`, `Edit`,
  `Retire` not whitelisted); `TestUIReadViewsWriteNothing` beliefs GET (G7);
  **`internal/httpapi/server_test.go:117` `TestUIGuardedLeavesEachReachAView`: add leaf
  `/ui/beliefs`, marker `<h2>BELIEFS</h2>` and a stub `Beliefs` in its `ui.Deps`.**
- [ ] **4.4** RED ui — U1 (two-belief test; `facet`/`confidence` posted, unchanged), U2 (404 / 409 /
  400 / `WriteLandedError` per variant with the notice per §3.4 table and a recording `slog`
  handler), U3 retire -> `Retire`, R1 view (five facets, empty facet), nil dep 503, HTMX fragment
  vs full page, `MaxBytesReader`. `wiring_beliefs_test.go` over a real empty migrated vault;
  `TestUIDeps_NilServicesStayNilInterfaces` gains a `Beliefs` case.
  **Copy note:** on `ErrBeliefStatusConflict` the UI copy must say the belief "changed or was
  retired": a CAS loss can come from derive rewriting the content, not only from a retire.
- [ ] **4.5** GREEN — routes in `ui.go` and `server.go` `r.Pattern` switch; `beliefs.templ`;
  handlers; **`layout.templ` gains the beliefs link only** (design §10 item 1); whitelist
  `ByFacet`, `Edit`, `Retire` in `ui_entrances_test.go:32` (G12).
- [ ] **4.6** DOCS — harness §4 I22 row names the whitelist additions. `scripts/docs-sync.sh`.
- [ ] **4.7** PROBES U1 constant id / read another field; U2 swaps and the single generic notice;
  U3 retire -> `Edit`; U4 route with no body entry; U4b drop exemption / empty reason / login
  given a body; G6 unguarded POST; a stub not counted.

## PR 5 — `feat/ports-store-decisionlog-before` and PR 6 — `feat/ui-activity`

**Moved to [`m4e-activity`](../m4e-activity/tasks.md)** (tasks 5.1-5.6 and 6.1-6.6, numbers kept).

---

## Closing (each PR)

- [ ] **C.1** `make check-all`, `go vet -tags integration,e2e ./...`, `scripts/docs-sync.sh` in an
  isolated worktree at the PR's commit; PR per `nooma-pr`; PR body lists impl+docs vs test lines
  and the measured multiplier.
- [ ] **C.2** After PR 1 merges: compute actual / estimate; apply the cut rule (Forecast) before
  PR 2 (done: PR 2 was cut, see the Forecast). Apply it again to PRs 3 and 4 (PR 3 done: it
  measured 1.51x, 393 against ~260, under 400, no cut; at 1.51x PR 4 is ~415, so its seam stays
  pre-defined). After PR 4 merges,
  `m4e-activity` may start; `m4e2-admin` starts after `m4e-activity`'s PR 6 merges.
