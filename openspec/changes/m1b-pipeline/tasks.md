# Tasks — M1b: the pipeline

Implementation task list for `m1b-pipeline`, derived from `spec.md` (9 numbered sections, R1–R8
plus test-level and boundary sections) and `design.md` (11 decisions, D1–D11). Chain strategy
**`stacked-to-main`**, matching M0 and `m1a-substrate`. Scope is Phase B only — the umbrella
proposal's five-PR table (PR7–PR11), which `design.md` §5 splits into **15 merges**, verified by
direct count against the design document's own chain table (grep confirms exactly fifteen rows:
`8a,8b,8c,7a,7b,7c,9a,9b,9c,10a,10b,10c,11a,11b,11c`). A sixteenth, non-code item — the GitHub
branch-ruleset edit R2.9/D10 require — is carried as its own numbered task ahead of PR 8a, per
this document's own governing instructions; see **C5** below for why "sixteen" is the honest
total once that item is counted alongside the fifteen code PRs, not a sixteenth code PR.

**Strict TDD is active.** Every behavioral task states the test first and what its red looks
like. Two tasks (10b's clock guards) have no natural pre-implementation red — they are structural
proxies over code that is already correct by the time they are written, in the same shape
`m1a-substrate/tasks.md` established for `store_no_direct_clock_read_test.go` and
`no_test_support_import_test.go`. §"On the tasks with no natural red" below states the discipline
applied instead.

Verification commands are drawn from the project's real targets, confirmed present in `Makefile`
during this session: `make check`, `make check-all`, `make test`, `make test-integration`,
`make test-e2e`, `make cover`, `make store-api-golden`, `make pending-red`. **`make pending-red`
stops existing after PR 8a** (R2.8/D10 retire it in that PR's own diff) — no task in PR 8b onward
cites it. No task cites `make cross-compile` — Phase B adds no OS-dependent code.

---

## Conflicts surfaced (do not silently resolve)

### C1 — `design.md`'s own dependency-graph string states `11c → 10b`; its content descriptions and its own chain-table row order say the opposite

`design.md` §5's dependency line reads: `` `11a → 11b → 11c`, and `11c → 10b` ``. Read literally,
this means 11c must land *before* 10b — i.e. before `internal/brain` exists at all.

That contradicts two things in the same document. First, the chain table (§5) lists `10a, 10b,
10c` **before** `11a, 11b, 11c`, and `m1a-substrate/tasks.md`'s own precedent (its Handoff
section, and every PR-numbered row in this document) treats table row order as merge order absent
a stated exception. Second, and more directly: 11c's own content column reads "`DecodeJudgment`,
**the judge call wired into capture**, the `duplicate` handling" — wiring a call *into* capture
requires `captureRunner` (created by 10b, per §3's package layout: `internal/brain/capture.go`,
PR 10) to already exist. The test matrix (§6) confirms the same reading: 10b's own rows list no
judge/relation content at all; 11c's rows ("the judge is not called when the candidate list is
empty", "a candidate below `min_confidence_to_persist`...") are additions to an already-working
capture pipeline, not a precondition for building one.

**Resolution: the arrow is backwards. The correct dependency is `10b → 11c`.** The tasks below
follow the table's own row order (10 before 11) and 11c's own content description, both of which
agree with each other and disagree only with the isolated dependency-string typo. Flagged here
per `CLAUDE.md` non-negotiable #1 rather than silently reordered without comment; `design.md`'s
own dependency line needs a follow-up correction from whoever next revises it.

### C2 — `spec.md` R2.3 vs. I21's anchor: already resolved in `spec.md` itself; `design.md`'s own "Conflict surfaced — C1" note describes the pre-correction wording as if still live

`design.md` §2 D5 raises its own "Conflict surfaced — C1": that `spec.md` R2.3 asks for a single
`VectorIndex` holding two models' entries, which I21's anchor comment
(`i21_vector_search_filters_on_model_test.go:52-57`, re-read directly in this session, confirmed
unchanged) says must never exist, and resolves it in the anchor's favor.

Re-reading `spec.md` R2.3 directly (not `design.md`'s summary of it) shows the resolution already
happened **inside spec.md itself**: R2.3's own heading reads "Corrected 2026-07-31 against the
conformance test that already anchors this," and its body states plainly "The conformance test
wins... A spec written after the anchor does not get to redefine what the anchor pins," describing
a `VectorIndex` scoped to one model — the exact shape design's D5 also lands on. **There is no
live disagreement between the current text of `spec.md` and `design.md` on this point.**
`design.md`'s own C1 callout, read in isolation, describes a wording that no longer exists in
`spec.md`'s current text, and its closing sentence ("R2.3's wording needs a follow-up correction")
is stale — the correction is already in the document. Tasks below implement the anchor's shape
(one `VectorIndex`, one `Model` field, two indexes for a two-model vault), consistent with both
documents' actual, current text. No follow-up spec edit is needed for this point.

### C3 — `spec.md` R4.6's timer question: already resolved in `spec.md`'s own §9; `design.md`'s own "Conflict surfaced — C2" note describes the pre-correction tension as if still live

The same pattern repeats. `design.md` §2 D9 raises "Conflict surfaced — C2": that `spec.md` R4.6
states a MUST NOT (no `units` row for a `timer`/`recurring_reminder` classification) while
`spec.md` §9's open-items list "says the spec does not decide it."

Re-reading `spec.md` §9 directly shows the item is not left open: it appears struck through, with
the prose "**closed, and it was never open.** R4.6 now carries a MUST NOT, decided by
`docs/02-cognitive-core.md` §8's bold sentence... This list originally deferred it on the grounds
that the umbrella *proposal* did not say so explicitly — which was the wrong document to consult."
**`spec.md` has already corrected its own internal tension.** `design.md`'s C2 callout is,
again, describing an earlier state of the document it cites. The two documents agree in their
current text: no `units` row for either classification, ever, in Phase B. Tasks below (10c.1)
implement this without treating it as an open question, and pin it with the new I04 conformance
test design's own D9 adds.

### C4 — `dedup_candidate_k` has no assigned §13 doc-02 task anywhere in this chain, and `design.md`'s own "four rows in PR 8b" count does not match its own content

`design.md` §2 D5 closes with: "`docs/06-harness.md` §7 then requires them in §13, so §13 gains
four rows in PR 8b." But D5's own code block and prose name exactly **three** new §13-bound
constants landing in PR 8b: `RecallTopK`, `WeightVector`, `WeightLexical` (`RRFK` already exists
in §13 today, confirmed by reading `docs/02-cognitive-core.md:309` directly — it is not a new row).
The fourth number D5 discusses, `DedupCandidateK`, is explicitly placed in `core/relation` (PR
11a), not `core/recall` (PR 8b) — "It lives in `core/relation`, because it bounds what the judge
is asked about." Design's own Risk #7 table entry calls both `dedup_candidate_k` and
`recall_top_k` "unmeasured numbers... this is what §13 is for," language that only makes sense if
both get a §13 row — yet PR 11a's chain-table content column names only "doc 02 §4," never §13.

**This is a genuine gap, not a stale note**: `CLAUDE.md` non-negotiable #4 and the `nooma-core`
skill's Hard Rule 4 both require *every* behavioral number to be a named constant **and** appear
in the §13 table — no exception for a number whose home package differs from where its sibling
constants live. Left uncorrected, `DedupCandidateK` would exist as a Go constant with no §13 row,
violating a non-negotiable this project has never let slide. **Task 11a.3 below closes this gap
explicitly.** PR 8b's own doc task is corrected to state three rows, not four (task 8b.3).

### C5 — "Sixteen merges" reconciled against a verified count of fifteen

This document's own governing instructions describe "16 merges against the proposal's 5 rows."
A direct grep over `design.md`'s §5 chain table returns exactly **fifteen** PR rows (listed at the
top of this document). The fifteenth-plus-one is not a code PR: it is the GitHub branch-ruleset
edit R2.9 and D10 both require — an operator action against
`gh api repos/rengo/nooma/rulesets/19863542`, expressible in no Makefile or workflow file, that
must land **before** PR 8a merges (D10's own sequencing argument: PR 8a's diff deletes the
`pending-red` job, so the job never posts on PR 8a's own head, and an unremoved required context
blocks that PR, not merely future ones). Counted together — fifteen code PRs plus this one
operator action — the total is sixteen action items in this chain, which is the number this
document's instructions state. Task **8a.0** below is that action, given its own number and its
own verification, positioned before PR 8a's code tasks per instruction #5.

### C6 — `spec.md` R2.7's own prose assumes PR7 (classify) lands before PR8 (recall); `design.md`'s D10-driven chain order reverses this, and the map's state at PR 8c still holds even though the prose describing it does not

`spec.md` R2.7 states: "`casesDirMustBeEmpty['recall']` becomes `false` in this PR.
`casesDirMustBeEmpty['classify']` **is already `false` from PR7**, this PR does not re-touch that
entry." This sentence is only true if PR7 (classify) merges before PR8 (recall).

`design.md` §5's chain table lists `8a, 8b, 8c` **before** `7a, 7b, 7c`, and D10 gives an explicit,
load-bearing reason: "**PR 8a goes first**... running it first makes the window during which
`main` lacks the `pending-red` context empty." This is not incidental ordering — it is the
document's own stated reason PR 8 precedes PR 7. Carried forward (per this document's own
instruction not to re-derive design's split lines), when task 8c.3 below flips
`casesDirMustBeEmpty["recall"]` to `false`, `classify`'s entry is **still `true`** — 7c has not
yet landed under this order. The underlying *mechanism* — the rules-as-data map holding
`{recall: false, classify: true, llm: false}` at that moment — is exactly as correct and as safe
as `spec.md` R2.7 requires (`recall`'s non-empty guard fires, `classify`'s empty guard still
correctly fires because `testdata/classify/cases/` genuinely still holds only `.gitkeep` at this
point in the chain). Only the *prose describing the state* is written for the opposite order.
Task 8c.3's own verify step below states the actual, correct condition rather than repeating
`spec.md`'s order-dependent phrasing. No functional defect follows from this; it is a documentation
mismatch between two artifacts written under different assumed orderings, surfaced rather than
silently worked around.

### On the tasks with no natural red

Task 10b.2 adds two L2 guards (`design.md` D4 Layers 2 and 3) over `internal/brain/` code that,
at the moment each guard is written, is already correct by construction — the clockless-worker
shape (D4 Layer 1) makes a second clock read require *adding a field*, a reviewable act, so there
is no pending implementation gap for the guards to expose. This mirrors
`store_no_direct_clock_read_test.go` and `no_test_support_import_test.go`'s own precedent from
`m1a-substrate/tasks.md` tasks 4.5 and 3.5. Each sub-task states explicitly that it has no
pre-implementation red, and its own verification step includes a **temporary-break check** —
introduce the violation the guard exists to catch, confirm the guard reports it, then revert
before committing — the same honesty `core-coverage.sh`'s own "armed but vacuous" wording and
design's own D9 presence-guard doc comment already carry.

### C7 — `Decode`'s signature had nowhere to put the location D2 requires. **Resolved: option B. 7a.4 unblocked.**

Found by PR 7a's apply run, which stopped at its size checkpoint after tasks 7a.1 and 7a.2 and
reported this rather than improvising a signature.

`design.md` D1 fixes the entry point (`design.md:152`):

```go
func Decode(raw string) (Classification, error)   // ErrNoFieldsSalvaged only
```

One argument. And D2 (`design.md:281-282`) says of `event_at` / `due_at`:

> a date-only value parses to midnight **in the instant's own location, which is passed in, never
> read from the OS**.

**Both cannot hold.** `Decode` receives no instant and no location, and `internal/core` cannot
obtain one on its own: `forbidigo` forbids `time.Now` and `os.Getenv` inside it, and
`time.Local` is the OS's answer by another name.

**The purity rule already decides the direction** — the location must arrive as a parameter, so
D1's signature is the half that gives. What is genuinely open is *how*, and both options are
real:

| Option | Shape | Note |
|---|---|---|
| **A. Pass the location** | `Decode(raw string, loc *time.Location)` | Narrowest — `Decode` gets exactly what it needs and nothing else. But a second date-adjacent need later means a third parameter |
| **B. Pass the instant** | `Decode(raw string, at time.Time)` | `at.Location()` supplies the location, and D4's pipeline **already reads the clock once** at the entry point, so the value is in hand at the call site with no new plumbing. Wider than needed today |

**Recommendation: B**, on the same reasoning D2 used to forbid reading the OS. The pipeline's one
clock read is the project's existing answer to "where does the instant enter", and `Decode` taking
that instant keeps one concept rather than introducing a second, location-shaped one beside it.
A caller who has `at` cannot accidentally pass a location from somewhere else.

**Resolution — B, corrected at its source in `design.md`.** The signature is now

```go
func Decode(raw string, now time.Time) (Classification, error)
```

named `now` rather than `at` for the reason that decided the option itself: D4's pipeline already
calls that value `now` when it hands it to `BuildPrompt` and to `ToUnit`, and a third word for one
value is a second concept wearing a disguise. `design.md` D1 now carries the signature and a
**"Why `now` is a parameter, and not a location"** paragraph; D2's "which is passed in" names it.

The conflict stays recorded above rather than being edited away, because it is the only place that
explains why a *decoder* takes an instant at all — a reader who found only the corrected signature
would reasonably try to delete the parameter as unused. A decoder that silently used `time.Local`
would pass every test written on a machine in the author's own timezone and be wrong everywhere
else.

---

## PR 8a — `feat/core-recall-vector` (~380 lines — the ceiling design draws, not a prediction; the first PR of this chain to be watched closely, per the Review Workload Forecast below)

Depends on nothing outside this chain beyond Phase A PR 2 (`internal/core/unit`, already shipped —
confirmed present). This PR creates `internal/core/recall/vector.go`, the last two symbols
`test/conformance/pending_symbols.txt` tracks, and is therefore the PR that retires the
`pending-red` gate in its own diff (D10). `docs/02-cognitive-core.md` §5 gains its first Phase B
delta here — this PR must not carry `no-spec-change` (spec R6.2).

- [ ] **8a.0** **Operator action, not code — must complete before this PR merges (C5, R2.9,
      design D10).** Remove the `pending-red` status context from `main`'s branch ruleset's
      required status checks on GitHub. Confirm the ruleset's current required contexts first
      with `gh api repos/rengo/nooma/rulesets` and the ruleset's own id (`19863542`, per this
      document's own instructions) rather than trusting this artifact's inherited claim that the
      context is required — that claim traces to `m1a-substrate/design.md` D8 and was never
      independently executed in either design session (design.md §1.2 says so plainly). If the
      confirm step shows `pending-red` is *not* currently required, this task is a no-op and the
      confirmation itself is the record of that. **Do not merge PR 8a before this task is
      confirmed done** — PR 8a's own diff deletes the `pending-red` CI job (task 8a.3), so the job
      never posts a status on PR 8a's own head, and an unremoved required context blocks this PR,
      not merely a future one (D10's own stated trap).
      Verify: review of the GitHub ruleset configuration — not mechanically verifiable by any
      Makefile target (same category of gate as `docs-sync.yml`, per spec R2.9's own "Verified
      by").
      Requirement: R2.9; design D10.
- [x] **8a.1** Test first: `internal/core/recall/vector_test.go` — `Search` over a hand-seeded
      `VectorIndex`, asserting the returned top-K order matches a hand-computed dot-product
      ranking; `ErrModelMismatch` when `q.Model != idx.Model`; `ErrDimMismatch` for a
      length-mismatched query vector; `ErrZeroVector` from `Normalize`. In the same file, I21's
      **behavioral** half (spec R2.3, distinct from the reflective anchor): a `VectorIndex` fixture
      holding entries from two distinct model strings, where one model-`b` entry outscores every
      model-`a` entry on raw dot product, asserting a model-`a` query returns only model-`a`
      entries in ranked order, the higher-scoring model-`b` entry absent regardless of score.
      **Red**: `undefined: recall.VectorQuery`, `undefined: recall.VectorIndex`,
      `undefined: recall.Search`, `undefined: recall.Normalize` — the same symbols
      `scripts/pending-red.sh` has tracked since M0; this is the compile-error-as-red pattern spec
      R7.2 names, now producing an ordinary (untagged) compile failure rather than the tagged one
      `pending-red.sh` watches for.
      Then implement `internal/core/recall/vector.go`: `VectorQuery{Model, Vector, K}`,
      `VectorIndex{Model, IDs, Vectors}`, `NewVectorIndex`, `Search`, `Normalize` — one index, one
      model, per I21's own anchor comment and C2's resolution above (design D5, D6).
      Verify: `make test`; `golangci-lint run` (confirms `depguard`/`forbidigo` — stdlib only, no
      clock).
      Requirement: R2.1, R2.2, R2.3; design D5, D6.

      **Done.** Observed RED verbatim before implementing (`go test -c -o /dev/null
      ./internal/core/recall/...`): `undefined: NewVectorIndex`, `undefined: VectorQuery`,
      `undefined: Search`, `undefined: ErrModelMismatch`, "too many errors" (compiler cutoff) —
      matching the predicted symbol set. Implemented `vector.go`; the I21 behavioral test
      (`TestSearch`'s "ranks by dot product descending" case, exact-ID assertion against a
      separately-constructed model-`b` index that scores higher and is never passed to `Search`)
      is the `TestSearch` table in `vector_test.go`, trimmed from an earlier, more verbose draft
      to manage this PR's size (see PR-level note below). `make test`, `golangci-lint run` both
      green.
- [x] **8a.2** In the **same PR** as 8a.1 (spec R2.8's MUST, design D10 — no partial split): all
      six of R2.8's numbered sub-steps land together, because there is no meaningful intermediate
      green state — a partially-retired gate is the "terminal trap" both spec and design describe.
      1. Drop `//go:build pendingimpl` from
         `test/conformance/i21_vector_search_filters_on_model_test.go`; rewrite its "Promotion:"
         paragraph (which currently instructs the reader to do exactly what this task is doing) to
         past tense; confirm 8a.1's behavioral test satisfies the file's own stated obligation
         ("this test never turns green inside this change... that same PR still needs its own,
         non-pending test for the actual filtering behaviour").
      2. Remove both lines (`recall.VectorQuery`, `recall.VectorIndex`) from
         `test/conformance/pending_symbols.txt`, then delete the file (design D10 edit #5 — it now
         tracks zero symbols and has no further job).
      3. Delete `scripts/pending-red.sh` (D10 edit #6).
      4. `Makefile`: drop `pending-red` from `check-all`'s prerequisite list, delete the
         `pending-red` target and its `.PHONY` line, fix the header comment's mention of the gate
         (D10 edit #7).
      5. `.github/workflows/ci.yml`: delete the `pending-red` job (confirmed present at
         lines 107–115 in this session) (D10 edit #8).
      6. `docs/06-harness.md`: move §6's table row and §8 point 5 to past tense — the harness *was*
         proven by watching it fail (D10 edit #9).
      7. `CLAUDE.md`'s Workflow section: remove the mention of the `pending-red` gate from the
         `check-all` description (confirmed present in this session's own reading) (D10 edit #10).
      8. `.golangci.yml`'s `run.build-tags` comment: remove the paragraph explaining the
         `pendingimpl` exclusion, since no file carries that tag after this PR (D10 edit #11).
      9. Fix the stale comment at `tree_scan_test.go:3-7` (still says I03 is
         `//go:build pendingimpl` "until PR 3 promotes it" — PR 3 already did, per Phase A).
         **Do not** delete `scanGoTree` itself — I01 and I03's untagged tests still call it.
      Verify: `make check-all` — green, with no `pending-red` target present anywhere in its
      dependency chain; `git diff --name-only` shows `scripts/pending-red.sh` and
      `test/conformance/pending_symbols.txt` deleted; `rg pending-red Makefile .github/workflows
      docs/06-harness.md CLAUDE.md .golangci.yml` returns nothing.
      Requirement: R2.8; R7.1's own scenario (both cited verbatim above).

      **Done**, all nine sub-steps in the same commit as 8a.1's implementation. Also fixed three
      present-tense references to the retired gate this task's own list did not name, found by a
      repo-wide `rg pending-red|pendingimpl` sweep after the nine listed edits:
      `internal/core/recall/doc.go`'s own "Pending conformance anchor" paragraph (the exact
      package this PR adds symbols to — left stale, it would have told a future reader a deleted
      script still gates this package), `test/harness/doc.go`'s live comparison to
      `scripts/pending-red.sh`, and `i01_focus_never_persisted_test.go`'s "see
      pending_symbols.txt and pending-red.sh" pointer — reworded to past tense rather than left
      dangling. `test/conformance/doc.go`'s general "(pendingimpl-tagged) domain symbols" phrase
      was left alone: it describes the package's still-valid build-tag mechanism in the abstract,
      not a specific claim about this now-retired gate. `make check-all` green with no
      `pending-red` target anywhere in its dependency chain; `git diff --name-only` shows
      `scripts/pending-red.sh` and `test/conformance/pending_symbols.txt` deleted; `rg pending-red
      Makefile .github/workflows docs/06-harness.md CLAUDE.md .golangci.yml` returns nothing.
- [x] **8a.3** `docs/02-cognitive-core.md` §5: prose stating the vector-leg mechanism concretely —
      what "one model per search" means at the `VectorQuery`/`VectorIndex` level (design's stated
      home for R2.10's §5 half; the numeric knobs land in PR 8b's own doc task, 8b.3).
      Verify: read the section; `docs-sync.yml` not locally verifiable (spec R2.10's own "Verified
      by").
      Requirement: R2.10 (§5 half).

      **Done.** Added a "Mechanism" bullet under §5's existing "One model per search" point,
      naming `VectorQuery`/`VectorIndex` and the two-indexes-not-one shape.
- [x] **8a.4** `internal/core/recall`'s purity and coverage, for the first time this package holds
      statements. `make cover` reports a real number rather than `total=0` for this package.
      Verify: `golangci-lint run`; `make cover` — read the reported number, not only the exit
      code (`m1a-substrate` task 2.7's precedent).
      Requirement: R2.10 (purity/coverage half, this PR's share).

      **Done.** `golangci-lint run` — 0 issues (depguard/forbidigo confirm stdlib-only, no
      clock/rand/uuid/os.Getenv). `make cover` (via `make check-all`) —
      `internal/core` statement coverage is 98% (52/53), at or above the 90% floor.
- [x] Verify (PR-level): `make check-all`; confirm `git diff --name-only` contains no path under
      `internal/core/classify/` or `internal/core/relation/` (scope boundary, R8.2); confirm the
      removed `pending-red` target leaves no dangling reference anywhere in the tree.

      **Done.** `make check-all` fully green (lint, vet, `-race -shuffle=on` L1/L2, build, L3
      integration, schema-golden clean, coverage 98%, all seven cross-compile targets, L4). No
      `pending-red` step appears anywhere in `check-all`'s own output — the target no longer
      exists. `git diff --name-only main` touches no path under `internal/core/classify/` or
      `internal/core/relation/`.

      **8a.0 is NOT done by this batch — it is the owner's operator action against the GitHub
      branch ruleset (C5), out of scope for `sdd-apply`.** It remains outstanding and MUST be
      confirmed before this PR is allowed to merge (D10's own stated trap: this PR's diff deletes
      the `pending-red` CI job, so an unremoved required status context blocks this PR's own
      merge, not merely a future one).

      **PR-level size note, surfaced rather than silently absorbed.** Final `git diff --stat
      main`: 15 files changed, 298 insertions(+), 168 deletions(-) — 466 changed lines against
      this PR's own ~380 ceiling (1.23x). 8a.1's own code+test (`vector.go` +
      `vector_test.go`) was trimmed once already during this session (from an initial 378-line
      draft down to 246) specifically to leave room under the ceiling, after `git diff --stat`
      crossed the 300-line stop-and-report checkpoint on 8a.1 alone. The remainder is 8a.2's own
      gate-retirement diff (a ten-file, mostly-deletion diff whose size the task list itself
      states is "fixed regardless of code size") plus three additional stale-comment fixes this
      session found and corrected (see 8a.2's own note above) that the task list did not
      enumerate. 8a.1 and 8a.2 cannot be split into separate PRs — spec R2.8's own MUST NOT
      forbids promoting I21 without retiring the gate in the same PR, and design D10 states the
      same. There is therefore no valid split line to propose within this PR's own scope; the
      1.23x overrun is inherent to what PR 8a is, matches the Review Workload Forecast's own "High
      risk" flag (set before any code existed), and is milder than the 1.3x–2.2x band
      `m1a-substrate` already measured repeatedly with `size:exception` (e.g. PR 5a at 1.74x).
      Flagged here for the owner to accept as `size:exception` or split differently, per
      `delivery_strategy: ask-on-risk` — not silently absorbed.

---

## PR 8b — `feat/core-recall-fuse` (~200)

Depends on PR 8a.

- [x] **8b.1** Test first: `internal/core/recall/fuse_test.go` — `Fuse` reproduces ADR-0010's
      formula by hand over a small pair of ranked id lists, including at least one id present in
      only one list (contributing a single term); the tie-break rule design D5 states (higher
      score first; on a tie, the id appearing earliest across the lists in argument order; on a
      further tie, lexicographic by id) pinned by a fixture engineered to produce an exact float
      tie. **Red**: `undefined: recall.Fuse`, `undefined: recall.RRFK`, `undefined:
      recall.RecallTopK`, `undefined: recall.WeightVector`, `undefined: recall.WeightLexical`.
      Then implement `internal/core/recall/fuse.go`: `Fuse(lists ...[]string) []string`, `RRFK =
      60` (already exists in doc 02 §13, no new row), `RecallTopK = 20`, `WeightVector = 1.0`,
      `WeightLexical = 1.0` (design D5; ADR-0010's own requirement that `k` and each list's
      relative weight be named constants, `0010:48-49`).
      Verify: `make test`; `golangci-lint run`.
      Requirement: R2.4, R2.5; design D5.

      **Done.** Observed RED verbatim before implementing (`go test -c -o /dev/null
      ./internal/core/recall/...`): `undefined: Fuse` (x3 call sites), `undefined: RRFK` (x2),
      `undefined: RecallTopK` (x2), `undefined: WeightVector` (x2), `undefined: WeightLexical`,
      "too many errors" — matching the predicted symbol set exactly (the constants only appear as
      `undefined` once the test file references them directly, via a dedicated
      `TestFuseConstants`, rather than only baking their values into hand-computed expectations).
      Implemented `fuse.go`: the four constants, `fuseWeight(listIndex)` mapping list 0 to
      `WeightVector`, list 1 to `WeightLexical`, anything beyond to 1.0 (Phase B always passes
      exactly two lists, vector first, per design's own wording), and `Fuse` via a score map + an
      earliest-list map, sorted by (score desc, earliest-list-in-argument-order asc, lexicographic
      asc). The tie-break fixture (`TestFuse_BreaksTiesDeterministically`) engineers two *exact*
      float ties in one table: `x`/`y` tie because swapping two operands in a two-term float64 sum
      is bit-identical, both reaching the lexicographic level since both first appear in list 0;
      `z`/`w` tie because both are single-list contributions at the same rank (`RRFK+3`) in
      different lists, resolved at the argument-order level alone (chosen names spell the opposite
      of lexicographic order — `w` < `z` — so this case would fail if the code fell through past
      argument-order into lexicographic instead of stopping there). `golangci-lint run`: 0 issues.
- [x] **8b.2** In the same commit as 8b.1: `internal/core/recall/tokenize_test.go` — `Tokenize`
      splits text into the lowercase word tokens the lexical leg searches for (design D5 places
      this in `core` because it is the recall-quality decision the golden corpus pins, and it is
      pure; no spec MUST constrains the exact algorithm beyond "what words the lexical leg
      searches for" — this task's own test is what pins the chosen behavior). **Red**:
      `undefined: recall.Tokenize`.
      Implement `internal/core/recall/tokenize.go`.
      Verify: `make test`.
      Requirement: design D5 (the lexical leg's own decision, feeding R3.3 at the store boundary).

      **Done**, same commit as 8b.1 (`04e2261`). `Tokenize` lowercases first, then splits on any
      rune that is not a Unicode letter or digit (`strings.FieldsFunc` + `unicode.IsLetter` /
      `unicode.IsDigit`), so punctuation is discarded and repeated separators collapse to zero
      tokens between words. `make test` green.
- [x] **8b.3** `docs/02-cognitive-core.md` §13 gains **three** new rows — `recall_top_k`,
      `weight_vector`, `weight_lexical` — not the four `design.md`'s own closing sentence states
      (C4 above: the fourth number, `dedup_candidate_k`, belongs to PR 11a, not this PR, and is
      closed there by task 11a.3).
      Verify: read the table; `docs-sync.yml` not locally verifiable.
      Requirement: R2.5, R2.10 (§13 half); C4's correction of design's own row count.

      **Done**, own commit (`ba34100`). Exactly three new §13 rows added, matching C4's
      correction, not design's own uncorrected "four rows" sentence.
- [x] Verify (PR-level): `make check-all` (no `pending-red` target exists as of PR 8a — confirm it
      is absent from this PR's own `make check-all` run too); `make cover`.

      **Done.** `make check-all` fully green: lint (0 issues), vet, `-race -shuffle=on` L1/L2 (all
      packages, including the new `TestFuse_*`/`TestTokenize` tests), build, L3 integration,
      `TestSchemaGolden -update` clean diff (no migration touched), coverage, all seven
      cross-compile targets, L4. `internal/core` statement coverage: **100% (77/77)** — matches
      PR 8a's own floor, no regression; the one gap `make cover` first reported (`fuseWeight`'s
      `default` branch, 75%, from having no test call `Fuse` with a third list) was closed by
      adding `TestFuse_ThirdListDefaultsToWeightOne` rather than left as an untested decision.
      `rg pending-red Makefile .github/workflows docs/06-harness.md CLAUDE.md .golangci.yml`
      returns nothing (still true as of this PR, confirmed fresh).

      `git diff --stat main`, excluding this file's own bookkeeping edit: 5 files changed, 267
      insertions(+), 0 deletions — `fuse.go` 83, `fuse_test.go` 113, `tokenize.go` 21,
      `tokenize_test.go` 47, `docs/02-cognitive-core.md` +3. 1.34x this PR's own ~200-line
      ceiling, milder than PR 8a's 1.23x-on-a-380-ceiling and well under the 280-line
      stop-and-report checkpoint; the overrun is mostly `fuse_test.go`'s own doc-comment density
      (matching `vector_test.go`'s precedent) proving the engineered tie's exact-equality claim in
      prose rather than asserting it silently.

---

## PR 8c — `feat/recall-corpus` (~250)

Depends on PR 8b.

- [x] **8c.1** Test first (fixture-format widening, design §4.2 — not itself a spec MUST, but the
      precondition R2.6's corpus needs to be authorable at all): `test/support/goldenset` —
      `RecallExample`/`RecallUnit`/`RecallQuery` gain `Vector []float32` per unit and `Vector`,
      `LexicalRanking []string` per query; `Validate` gains the mechanizable cross-field check (if
      any unit carries a vector then all must, the query must, and every vector shares one
      length). `testdata/recall/format.md`'s field table and fenced example move together, in the
      same commit — `TestHarness_GoldenSetFormatMatchesType` decodes the fence under
      `DecodeStrict`, so a mismatch fails immediately if the two drift apart. **Red**: the existing
      fence no longer round-trips against the widened type until both are updated together (a
      compile-clean-but-behaviorally-red state — the loader accepts the old fence but the new
      cross-field `Validate` check has nothing to exercise until a vector-bearing case exists,
      which 8c.2 adds).
      Verify: `go test ./test/support/goldenset/...`; `TestHarness_GoldenSetFormatMatchesType`
      green.
      Requirement: design §4.2 (fixture format, the precondition R2.6 needs).

      **Done.** Resolved the ambiguous "Red" wording as the standard compile-red this chain's own
      precedent uses (8a/8b): wrote `TestRecallExample_ValidateVectorCrossField` in
      `validate_test.go` first, referencing `RecallUnit.Vector`/`RecallQuery.Vector` struct fields
      that did not yet exist. Observed RED verbatim (`go test -c -o /dev/null
      ./test/support/goldenset/...`): `unknown field Vector in struct literal of type RecallUnit`,
      `unknown field Vector in struct literal of type RecallQuery`. Implemented the widening:
      `RecallUnit.Vector`, `RecallQuery.Vector`/`LexicalRanking` (all `omitempty`), and
      `RecallExample.validateVectors()` — the mechanizable cross-field rule design §4.2 states.
      `format.md`'s field table gained three rows and a new "Vector and lexical-ranking fields"
      section; its fenced `Shape` example gained illustrative (not worked-example) vectors on all
      three units and the query, so the type and the fence moved together as instructed. `go test
      ./test/support/goldenset/...` and `TestHarness_GoldenSetFormatMatchesType` both green;
      `golangci-lint run` 0 issues.
- [x] **8c.2** Add real cases under `testdata/recall/cases/` (currently only `.gitkeep`, confirmed)
      satisfying `format.md`'s three named properties across the corpus: at least one distractor,
      one near-duplicate pair, one lexical/vector disagreement (the best lexical match and the
      best vector match differ) — each authored with explicit `vector`/`lexical_ranking` fields per
      8c.1's widened format, since `fakeprovider.NewEmbeddingFake`'s hash-based vectors cannot
      author a genuine disagreement (design §4.2's own rejected-option table). Every unit id in any
      case's `expected_unit_ids` names a unit in that same case whose `status` is `pool`. This is
      the change that makes `TestHarness_GoldenSetFormatsDeclared`'s `recall` subtest **fail** —
      `casesDirMustBeEmpty["recall"]` is still `true` at this point in the chain (confirmed:
      `classify`'s own inversion is PR 7c, which per C6 above has not yet landed under this
      chain's order) — confirmed by reading `assertCasesDirEmptiness`: it `t.Errorf`s on the first
      non-`.gitkeep` entry in a directory still marked `true`, untagged, inside `make check`'s fast
      loop. **This failure is task 8c.3's own red.**
      Verify: `go test ./test/support/goldenset/...` (loader accepts every case); observe
      `make check` go red on `TestHarness_GoldenSetFormatsDeclared`'s `recall` subtest before 8c.3
      lands.
      Requirement: R2.6.

      **Done.** Added `testdata/recall/cases/oncall-shift-swap.json`: 3 units, 1 query, all three
      required properties in one case (format.md allows this — "not necessarily all three in one
      case file"). Distractor: `unit-oncall-reimbursement` shares "on call"/"shift" vocabulary with
      the query but is semantically unrelated (hand-computed RRF ranks it last of three). Near-dup
      pair: `unit-oncall-swap-request`/`unit-oncall-swap-handoff`, same procedure worded
      differently. Lexical/vector disagreement: stated `lexical_ranking` puts
      `unit-oncall-swap-request` first (literal "swap" match); hand-computed vector `Search` (raw
      dot product against query `[1,0,0,0]`) puts `unit-oncall-swap-handoff` first (0.95 vs 0.85) —
      the two legs disagree on which of the pair leads. Verified `Load` accepts the file via a
      throwaway `go run` script (scratchpad, deleted after use — no permanent "load every case"
      test exists in this PR's scope; `assertCasesDirEmptiness` only checks presence/count, not
      decode success). Observed RED verbatim (`go test ./test/conformance/... -run
      TestHarness_GoldenSetFormatsDeclared`): `golden_sets_test.go:53:
      .../testdata/recall/cases contains "oncall-shift-swap.json" — this change ships an empty
      corpus (R10.1's MUST NOT); real cases are M1's responsibility` — `recall` subtest FAILs,
      `classify` and `llm` subtests PASS, confirming `classify`'s guard is untouched.
- [x] **8c.3** Fix the red 8c.2 created: flip `casesDirMustBeEmpty["recall"]` to `false` in
      `test/conformance/golden_sets_test.go`. **`classify`'s entry stays `true`, unchanged by this
      PR** — per C6 above, this is the *correct* state at this point in the chain (7c has not yet
      landed), not the "already `false` from PR7" state `spec.md` R2.7's own prose describes; the
      map itself — `{recall: false, classify: true, llm: false}` — is exactly right regardless of
      which prose describes it.
      Verify: `make test` — the `recall` subtest passes because `cases/` is non-empty; the
      `classify` subtest passes because `testdata/classify/cases/` genuinely still holds only
      `.gitkeep` at this point.
      Requirement: R2.7.

      **Done**, same commit as 8c.2 (`test(recall): populate the corpus and invert its empty-corpus
      guard`) — landing either half alone breaks `main` or asserts a corpus that does not exist, so
      both go in one commit. `TestHarness_GoldenSetFormatsDeclared` green across all three
      subtests; `classify`'s map entry left untouched at `true`.
- [x] Verify (PR-level): `make check-all`.

---

## PR 7a — `feat/core-classify-decode` (~330)

Depends on nothing outside this chain beyond Phase A PR 2 (`internal/core/unit`).

- [ ] **7a.1** Test first: `internal/core/classify/salvage_test.go` — `Salvage` over a truncated
      payload (returns every field completed before the stream ended, flags the rest
      `ReasonTruncated`), a non-object payload (returns zero completed members), and a payload
      truncated before its first value. **Red**: `undefined: classify.Salvage`.
      Implement `internal/core/classify/salvage.go`: the streaming, truncation-tolerant object
      reader (design D1 — not `json.Unmarshal`, which fails a truncated document wholesale).
      Verify: `make test`; `golangci-lint run`.
      Requirement: R1.2 (truncated-JSON shape); design D1.
- [ ] **7a.2** Test first: `internal/core/classify/kind_test.go` — `AllKinds()` returns exactly the
      thirteen values `spec.md` R1.1 lists, no more, no fewer, the table asserting its own
      completeness; `Kind.UnitType()` returns `false` for the six non-persisting outcomes
      (`chitchat`, `out_of_scope`, `recall`, `correction`, `timer`, `recurring_reminder`). **Red**:
      `undefined: classify.Kind`, `undefined: classify.AllKinds`.
      Implement `internal/core/classify/kind.go` (design D1).
      Verify: `make test`.
      Requirement: R1.1; design D1.
- [ ] **7a.3** Test first: `internal/core/classify/outcomes_test.go` — the six orthogonal
      vocabularies (`NudgeOutcome`, `RelationOutcome`, `StateOutcome`, `TaskCheckinOutcome`,
      `ListOp`, `PersonRefStatus`) each have an `AllX()` and no `ParseX` (design D11 point 2 — one
      generic `decodeEnum[T]` serves all seven enum fields). **Red**: `undefined:
      classify.AllNudgeOutcomes` (etc.).
      Implement `internal/core/classify/outcomes.go`.
      Verify: `make test`.
      Requirement: R1.1 (adjacent — the orthogonal-fields half); design D1, D11.
- [ ] **7a.4** Test first: `internal/core/classify/decode_test.go` — one test per I14 shape over
      inline payload literals (not the corpus, which is L2 and PR 7c's): a wrong-typed field (e.g.
      `weight` as a JSON string) degrades only that field, every other field intact; an unknown
      enum value degrades only that field; the six orthogonal fields degrade independently of each
      other and of `Kind`. A `Kind` degraded by an unknown enum leaves every other field
      populated. `Salvage` returning zero fields yields `ErrNoFieldsSalvaged`, not a degraded
      classification (design D1's stated floor — a payload with no fields has none to degrade).
      The table asserts its own completeness (design D11 point 4). **Red**: `undefined:
      classify.Decode`, `undefined: classify.Classification`, `undefined:
      classify.ErrNoFieldsSalvaged`.
      Implement `internal/core/classify/decode.go` — signature `Decode(raw string, now time.Time)
      (Classification, error)` per D1 as C7 resolved it, the `fieldSpec` table + `decodeEnum[T]`
      (design D11 point 1–2) — and `internal/core/classify/classification.go`
      (`Classification`, `Degradation`, `Reason`). A date-only `event_at`/`due_at` parses to
      midnight in `now.Location()`; `time.Local` must appear nowhere, and the test passes a `now`
      in a fixed non-UTC location so a machine-timezone regression fails rather than hides.
      Verify: `make test`; `golangci-lint run` (confirms the decoder stays stdlib-only).
      Requirement: R1.2; design D1, D2, D11; conflict C7.
- [ ] **7a.5** `internal/core/classify`'s purity and partial coverage (this PR's own slice — 7b
      adds `ToUnit`/priors, the package's coverage floor is only fully meaningful once that lands,
      but this task confirms no regression at this PR's boundary).
      Verify: `golangci-lint run`; `make cover`.
      Requirement: R1.2 (purity/coverage half, this PR's share).
- [ ] Verify (PR-level): `make check-all`; confirm `git diff --name-only` contains no path under
      `internal/core/recall/` or `internal/core/relation/`.

---

## PR 7b — `feat/core-classify-unit` (~280)

Depends on PR 7a.

- [ ] **7b.1** Test first: `internal/core/classify/tounit_test.go` — `ToUnit(c, id, now, priors)`
      driven with three distinguishable instants proves `CreatedAt`/`UpdatedAt`/`LastTouchedAt` all
      equal `now`, `EventAt` and `DueAt` are never crossed with each other or with `CreatedAt`
      (I18); `Status` is always `unit.StatusPool`; `Confidence` is always `nil` (Q2); a
      classification whose `Kind` maps to no `unit.Type` (via `Kind.UnitType()`'s `false` return)
      makes `ToUnit` return an error, not a zero-value unit the caller could forget to check.
      **Red**: `undefined: classify.ToUnit`.
      Implement `internal/core/classify/tounit.go` (design D2, D4).
      Verify: `make test`; `golangci-lint run`.
      Requirement: R1.3; design D2, D4.
- [ ] **7b.2** In the same commit: `internal/core/classify/prior_test.go` — `PriorWeight = 1.0`,
      `PriorDecayRate = 0.01` supply `ToUnit`'s fallback when `c.Weight`/`c.DecayRate` are `nil`.
      An L2 test, `test/conformance/classify_priors_ddl_test.go` (untagged), reads migration
      0001's `units.weight`/`units.weight_decay_rate` column `DEFAULT`s off disk via
      `migrationSQLText`/`extractTableBody` (confirmed present and reusable,
      `i13_learning_signal_test.go:24-57`) and asserts the two Go constants equal them, compared as
      **parsed floats**, not source text (design D3's own warning: SQL reads `0.3`/`0.01`, Go
      writes `0.30`/`0.01`). **Red**: `undefined: classify.PriorWeight`, `undefined:
      classify.PriorDecayRate`.
      Implement `internal/core/classify/prior.go`.
      Verify: `make test`.
      Requirement: (design D3's own stated shape — not itself spec-numbered; feeds R1.3's
      weight/decay-rate half via `ToUnit`).
- [ ] **7b.3** Test first: `internal/core/classify/prompt_test.go` — `BuildPrompt(text, beliefs,
      now)` renders `now.Format(...)` and `now.Location().String()` (design D4's timezone
      mechanism — the zone travels inside the clock's instant, never read from the OS inside
      `core`); a test `Clock` fixing a `Location` makes the assertion stable. `beliefs` is always
      `nil` in Phase B (design D4 — nothing in M1 reads `self_beliefs`). **Red**: `undefined:
      classify.BuildPrompt`, `undefined: classify.Belief`.
      Implement `internal/core/classify/prompt.go`.
      Verify: `make test`.
      Requirement: R1.3 (adjacent, the prompt-construction half feeding R4.2); design D4.
- [ ] **7b.4** `docs/02-cognitive-core.md` §5.1: the field-by-field degradation definition — for
      each field, what "degrades to null" means (proposal §4.8's table entry for `core/classify`).
      Verify: read the section; `docs-sync.yml` not locally verifiable.
      Requirement: R1.7.
- [ ] Verify (PR-level): `make check-all`; `make cover` (the package's coverage floor is now
      meaningful across `kind.go`, `outcomes.go`, `salvage.go`, `decode.go`, `classification.go`,
      `prior.go`, `tounit.go`, `prompt.go`).

---

## PR 7c — `feat/classify-corpus` (~250)

Depends on PR 7b.

- [ ] **7c.1** Test first (fixture-format widening, design §2 D2 — the wire shape carries
      `event_at`/`due_at` as top-level, separately-named fields, `Classification` has no
      `CreatedAt`): `test/support/goldenset` — `ClassifyExpected` gains `EventAt *string`,
      `DueAt *string`, both optional; `testdata/classify/format.md`'s field table and fenced
      example move the date out of `structured_data` in the same commit (the widening must not
      drift from the fence `TestHarness_GoldenSetFormatMatchesType` decodes). **Red**: the fence no
      longer matches the type until both move together.
      Verify: `go test ./test/support/goldenset/...`; `TestHarness_GoldenSetFormatMatchesType`
      green.
      Requirement: design D2 (the fixture-format precondition R1.5's dated cases need).
- [ ] **7c.2** Add real cases under `testdata/classify/cases/` (currently only `.gitkeep`,
      confirmed) covering: all thirteen taxonomy values from R1.1 (including `timer`,
      `recurring_reminder`, `chitchat`, `out_of_scope`); all three I14 shapes (truncated JSON, a
      wrong-typed field, an unknown enum value), each with `llm_case_id` naming a
      `testdata/llm/cases/` recording of the same defect (Phase A's `llm` corpus already has real
      cases — confirmed present); at least one case exercising `person_ref_status: "ambiguous"`.
      This is the change that makes `TestHarness_GoldenSetFormatsDeclared`'s `classify` subtest
      **fail** — `casesDirMustBeEmpty["classify"]` is still `true` — this failure is task 7c.3's
      own red.
      Verify: `go test ./test/support/goldenset/...` (every case loads); observe `make check` go
      red on the `classify` subtest before 7c.3 lands; review-level check (per `format.md`'s own
      "not mechanized" note) that all thirteen values and all three shapes appear across the
      corpus.
      Requirement: R1.5.
- [ ] **7c.3** Fix the red: flip `casesDirMustBeEmpty["classify"]` to `false`. At this point in the
      chain **`recall`'s entry is already `false`, from PR 8c** — this is the one direction where
      the chain's actual order (8 before 7) does match a later PR's expectation, since nothing in
      `spec.md` or `design.md` requires this PR to leave `recall` untouched in a specific state,
      only that it not *re-touch* the entry (R1.6's own MUST NOT), which this task honors by
      construction.
      Verify: `make test` — the `classify` subtest passes because `cases/` is non-empty; the
      `recall` subtest continues to pass unaffected.
      Requirement: R1.6.
- [ ] **7c.4** L2: `test/conformance/i14_classify_field_degrades_to_null_test.go` — `Decode` driven
      against the real corpus, each broken case's `Reason` matching the shape its case id names.
      This is I14's invariant proof over real data, distinct from 7a.4's inline-literal L1 tables
      (design D11's L1/L2 split — the corpus tests are deliberately L2 because `depguard` denies
      `os` to `internal/core/**` with no `$test` selector, so a corpus-reading test cannot live
      inside `internal/core/classify` even as a `_test.go` file).
      Verify: `make test`.
      Requirement: R1.2 (corpus-level proof); design D11.
- [ ] Verify (PR-level): `make check-all`.

---

## PR 9a — `feat/ports-embedding` (~280)

Depends on PR 8a (the `EmbeddingRepo.LoadIndex` signature returns a `recall.VectorIndex`) and
Phase A's `repocontract` precedent.

- [ ] **9a.1** Test first: a `repocontract`-shaped suite for `EmbeddingRepo` —
      `Put` upserts on `unit_id` (the primary key); `LoadIndex(model)` over a two-model fixture
      returns a `VectorIndex` scoped to exactly the requested model (I21's storage precondition).
      **Red**: `undefined: ports.EmbeddingRepo`.
      Implement `internal/ports/embeddingrepo.go`: `Embedding{UnitID, Model, Vector, At}`,
      `EmbeddingRepo{Put, LoadIndex}` (design D8).
      Verify: `go build ./...`; `golangci-lint run`.
      Requirement: R3.1, R3.2 (interface half); design D8.
- [ ] **9a.2** Same commit: `test/support/memrepo`'s `EmbeddingRepo` fake, mutex-guarded,
      deep-copying on the way in and out (matching Phase A's `memrepo.Units` shape); the contract
      suite's first caller. **Red**: `undefined: memrepo.NewEmbeddings` (or the fake's chosen
      constructor name).
      Verify: `go test -race ./test/support/memrepo/...`; `make test` (the contract's every case
      runs against the fake).
      Requirement: R3.1, R3.2 (fake half).
- [ ] **9a.3** Test first: `ports.LexicalSearch{SearchLexical(tokens []string, k int) ([]string,
      error)}` + its memrepo fake + contract suite (design D5, D8 — this is the port the store's
      FTS5 leg, PR 9c, implements). **Red**: `undefined: ports.LexicalSearch`.
      Implement `internal/ports/lexicalsearch.go`.
      Verify: `go build ./...`; `go test -race ./test/support/memrepo/...`.
      Requirement: design D5 (the lexical leg's own port — feeds R3.3).
- [ ] Verify (PR-level): `make check-all`; confirm `git diff --name-only` contains no path under
      `internal/core/**` (this PR is ports + fakes only — no `docs-sync.yml` task is forced, per
      R6.2's own MUST NOT).

---

## PR 9b — `feat/store-embedding` (~380 — the second PR of this chain to watch closely, per the
Review Workload Forecast below)

Depends on PR 9a.

- [ ] **9b.1** Test first (L3, `-tags integration`):
      `internal/store/sqlite/embeddingrepo_integration_test.go` — `Put` writes a row whose stored
      vector has L2 norm 1 within floating-point tolerance, `dim == len(vector)`, and round-trips
      through the little-endian `float32` codec. **Red**: `undefined: sqlite.NewEmbeddingRepo` (or
      the constructor design's own naming settles on).
      Implement `internal/store/sqlite/embeddingrepo.go`: `Put` (`ON CONFLICT(unit_id) DO
      UPDATE`), the BLOB codec (`math.Float32bits` + `binary.LittleEndian`), calling
      `recall.Normalize` immediately before encoding — the store→core import direction Phase A's
      `unitrepo.go:14` already establishes (design D6).
      Verify: `make test-integration`.
      Requirement: R3.1; design D6.
- [ ] **9b.2** In the same commit: `LoadIndex(model)` over a vault seeded with `unit_embeddings`
      rows from two distinct `model` values, asserting the returned index holds only the requested
      model's rows and vector values match — I21's storage half (R3.4), and R3.2's own MUST NOT
      (no per-recall-call SQL read — the load path and the query path are distinct code paths,
      confirmed by this task's own review of the diff).
      Verify: `make test-integration`; review — no call site outside vault-open reads
      `unit_embeddings` per request.
      Requirement: R3.2, R3.4.
- [ ] **9b.3** No migration added or edited (`unit_embeddings` already exists — confirmed,
      migration 0002 lines 74–81). Regenerate `testdata/schema/store_api.golden`.
      Verify: `git diff -- internal/store/sqlite/migrations/` empty; `make store-api-golden`;
      `git diff -- testdata/schema` reviewed and committed.
      Requirement: R3.5.
- [ ] Verify (PR-level): `make check-all`; confirm every test added by this PR writes only
      fixture-derived vectors, never a real embedding call (R3.6, R6.1).

---

## PR 9c — `feat/store-search` (~220)

Depends on PR 9a. Task 9c.2 additionally needs PR 8c's corpus to exist — an implicit dependency
`design.md`'s own dependency-graph string does not state (it names only `9a → 9c`), but which
`design.md` §4.2's prose requires directly ("PR 9c's integration test asserts the real FTS5 leg
produces the `lexical_ranking` a case states"). The chain's own row order (8c before 9c) already
satisfies this in practice; noted here so the gap in the stated graph does not surprise whoever
schedules this PR differently.

- [ ] **9c.1** Test first (L3): `internal/store/sqlite/search_integration_test.go` — a vault seeded
      with one unit per status (all four) sharing matching vocabulary; the FTS5 leg's query
      returns only the `pool` unit's id (I02's storage half, R3.3). This is also the L3 test that
      **confirms** ADR-0010's assumption that `bm25()` returns negative values — `ORDER BY
      bm25(units_fts)` ascending — since design's own session could not execute SQLite to check
      the sign convention (design Risk #2). **Red**: `undefined: sqlite...SearchLexical` (name per
      design's own choice).
      Implement `internal/store/sqlite/search.go`: `SearchLexical(tokens, k)` — quote each token,
      join with `OR`, `WHERE units_fts MATCH ? AND u.status = 'pool'`, `ORDER BY
      bm25(units_fts)`, `LIMIT ?` (design D5).
      Verify: `make test-integration` — if this test fails on ordering rather than on the
      positive-filter assertion, that is design Risk #2 resolving in the wrong direction; fix the
      `ORDER BY` clause, do not weaken the test.
      Requirement: R3.3; design D5, Risk #2.
- [ ] **9c.2** L3 case: the real FTS5 leg reproduces at least one PR 8c corpus case's stated
      `lexical_ranking` exactly (design §4.2's closing loop — without this, `lexical_ranking` is a
      number a case author invented; with it, it is a recording).
      Verify: `make test-integration`.
      Requirement: design §4.2 (closes the loop R2.6's corpus opens).
- [ ] Verify (PR-level): `make check-all`.

---

## PR 10a — `feat/ports-decisionlog` (~250)

Depends on Phase A's `repocontract` precedent only — no dependency on PRs 7/8/9's packages
(`DecisionAction`'s vocabulary is standalone). Positioned here per the chain's own row order.

- [ ] **10a.1** Test first: a `repocontract`-shaped suite for `DecisionLog` — `Record` writes a
      row; `Since(t, limit)` returns rows after `t`, bounded by `limit`; the `DecisionAction`
      vocabulary is closed and `AllDecisionActions()` returns every constant (design D9's own
      closed-vocabulary pattern, following `unit.Status`'s precedent). **Red**: `undefined:
      ports.DecisionLog`.
      Implement `internal/ports/decisionlog.go`: `DecisionAction` + its eleven named constants,
      `Decision{ID, Action, Rationale, Context, OccurredAt}`, `DecisionLog{Record, Since}` (design
      D9).
      Verify: `go build ./...`; `golangci-lint run`.
      Requirement: R4.5 (port existence half); design D8, D9.
- [ ] **10a.2** Same commit: `test/support/memrepo`'s `DecisionLog` fake + the contract suite's
      first caller.
      Verify: `go test -race ./test/support/memrepo/...`.
      Requirement: R4.5.
- [ ] **10a.3** Test first (L3): `internal/store/sqlite/decisionlog_integration_test.go` —
      `Record`/`Since` round trip against a real vault; `context` defaults to `'{}'` when absent,
      matching migration 0001's own DDL default (confirmed present, `0001:95-102`).
      Implement `internal/store/sqlite/decisionlog.go`.
      Verify: `make test-integration`; `make store-api-golden`.
      Requirement: R4.5 (store half).
- [ ] Verify (PR-level): `make check-all`; confirm `DecisionAction`'s vocabulary lives in
      `internal/ports`, never `internal/core` — I12's "never from `internal/core`" half is already
      `depguard`-enforced structurally (design D9's own note: "one of the few places where an
      invariant is free"), this task confirms no file under `internal/core/**` imports the port.

---

## PR 10b — `feat/brain-capture` (~400 — the third PR of this chain to watch closely; the first
PR to create anything under `internal/brain/` beyond `doc.go`)

Depends on PR 7c, PR 9b, PR 9c, PR 10a (design's own stated dependency set).

- [ ] **10b.1** Test first: `CaptureService.Capture` reads `ports.Clock.Now()` exactly once per
      capture — a fake `Clock` that fails the test if `Now()` is called a second time during one
      invocation. **Red**: `undefined: brain.CaptureService`.
      Implement `internal/brain/capture.go`, `internal/brain/result.go`: the clockless-worker
      shape — `CaptureService{clock, run}` owns the only `ports.Clock` field in the package;
      `captureRunner` has no way to obtain a clock (design D4 Layer 1).
      Verify: `make test`.
      Requirement: R4.1; design D4.
- [ ] **10b.2** In the same PR: two structural L2 guards over `internal/brain/**`, both **with no
      natural pre-implementation red** (see "On the tasks with no natural red" above):
      (a) `test/conformance/brain_no_direct_clock_read_test.go` — a tree scan failing on any
      `time.Now(` in a non-test file under `internal/brain/**` (mirrors
      `store_no_direct_clock_read_test.go`);
      (b) `test/conformance/brain_single_clock_read_test.go` — a `go/ast` walk failing on either
      more than one `Now()` call expression in one file, or any `Now()` call inside a `FuncDecl`
      whose signature already takes a `time.Time` parameter (design D4 Layer 3 — the real bug
      class R9 names). Both announce their own honest limitation in their doc comment (design's
      own precedent, mirroring `golden_sets_test.go:164-176`).
      Verify: `make test`; **temporary-break check** for each — (a) add a throwaway `time.Now()`
      call to a scratch file under `internal/brain/`, confirm the scan fails, revert; (b) add a
      second `Now()` call inside `Capture` or a `Now()` call inside a function already taking
      `now time.Time`, confirm the AST scan fails naming it, revert.
      Requirement: design D4 (converts R9 from a review property into a gate); §6 test matrix,
      "10b" rows for the clock guards.
- [ ] **10b.3** Test first: capture driven end-to-end against `memrepo`/`fakeprovider` — an
      ordinary classification (e.g. `task`) persists a `unit.StatusPool` unit whose
      `Weight`/`WeightDecayRate` come from classify's output (or PR 7b's priors when absent) and
      whose `CreatedAt`/`UpdatedAt`/`LastTouchedAt` all equal the single clock read from 10b.1.
      **Red**: `undefined:` whatever `captureRunner`'s entry method is named, before this task's
      wiring exists.
      Implement the `classify.BuildPrompt` → `llm.Complete` → `classify.Decode` → `classify.ToUnit`
      → `units.Create` chain inside `captureRunner.at` (design D4's pipeline diagram, steps 1–6).
      Verify: `make test`.
      Requirement: R4.2.
- [ ] **10b.4** Test first: a captured unit is embedded exactly once, the recorded `Model` matches
      the fake embedding provider's configured model; no embedding is written for a unit this
      pipeline does not also persist.
      Implement `embed.Embed` → `embeds.Put` → `index.Add` wiring, persist-before-embed ordering
      (design D8's deliberate, named ordering — a local provider outage degrades the index, never
      refuses the capture).
      Verify: `make test`.
      Requirement: R4.3; design D8.
- [ ] **10b.5** Test first: capture runs hybrid recall for dedup/relation candidates via the one
      RRF-fused mechanism PR 8's `core/recall` already proves at L1 — this task reuses it, does not
      reimplement it; the candidate search excludes the just-persisted unit from its own candidate
      list; a capture into an otherwise-empty vault makes zero LLM calls beyond the one
      `capture_processing` call (the judge is not invoked when the candidate list is empty —
      `fakeprovider`'s unscripted-call failure enforces this for free, design D4's own stated
      property).
      Implement `internal/brain/recall.go` (`RecallService`) and `internal/brain/index.go`
      (`Index`, the in-memory `VectorIndex` holder loaded once at vault open, per ADR-0012's own
      MUST).
      Verify: `make test`.
      Requirement: R4.4; design D4, D5.
- [ ] **10b.6** Test first: a capture with an effect (a normal persisted unit) leaves exactly one
      relevant `decision_log` row, written by `internal/brain`; a recall (a read) writes none
      (design D9 — "there is no `recall.answered` action").
      Implement `log.Record` calls at each effectful step this PR's own pipeline reaches
      (`capture.classify` / `capture.unit.created`; `capture.embedding.failed` on a scripted
      embedding failure).
      Verify: `make test`.
      Requirement: R4.5 (behavioral half).
- [ ] **10b.7** Test first: a `superseded` and an `incomplete` unit, seeded directly into the fake
      repo, are absent from recall's fused output (I02) — proving the `LiveByIDs` boundary (design
      D5's "one filter, for both legs, before fusion") holds inside the brain pipeline, not merely
      inside `sqlite.UnitRepo` itself.
      Verify: `make test`.
      Requirement: R7.1 (I02's test-level assignment); design D5.
- [ ] **10b.8** `CaptureResult` carries `Embedded bool`; a scripted embedding-provider failure
      leaves the unit persisted (`Embedded: false`), a `capture.embedding.failed` `decision_log`
      row with the provider error in `context`, and the capture call itself does not return an
      error (design D8's accepted, named gap — a half-synced unit is real, and the alternative
      — an atomic transaction — would refuse the capture on an index outage, which doc 02 §5's
      product rule forbids).
      Verify: `make test`.
      Requirement: design D8 (the risk this PR must not silently avoid).
- [ ] Verify (PR-level): `make check-all`; confirm `git diff --name-only` contains no path under
      `internal/core/**` — this PR touches no core file, so `docs-sync.yml` is not forced here
      (R6.2's own MUST NOT for this specific PR); code review of every `internal/brain/` call site
      for a second, independent clock read the AST guard's own honest limitation cannot catch
      (R4.1's review-property half, R4.10).

---

## PR 10c — `feat/brain-hooks` (~200)

Depends on PR 10b.

- [ ] **10c.1** Test first: a **new** conformance test,
      `test/conformance/i04_timer_never_a_unit_test.go` (design D9 — I04 has no existing test;
      confirmed by glob, and I04 already sits in `docs/06-harness.md` §4's invariant table, so no
      doc-06 edit is needed): a `timer` or `recurring_reminder` classification leaves zero `units`
      rows, zero `timers` rows, zero `triggers` rows, exactly one `decision_log` row (`action =
      capture.hook.deferred`, `rationale` naming the refusal, `context` carrying the
      classification verbatim), and a `CaptureResult{Stored: false, Deferred: &Deferred{Kind,
      Message}}` with `Message` in plain words. **Red**: this shape does not yet exist in
      `captureRunner`'s routing.
      Implement Q3a's refusal path in the `route on c.Kind` step (design D9 — decided by doc 02
      §8's bold sentence, per **C3**'s resolution above, not by a still-open question).
      Verify: `make test`.
      Requirement: R4.6; design D9.
- [ ] **10c.2** Test first: an ambiguous person reference (`person_ref_status: "ambiguous"`)
      persists a `pool` unit (never `incomplete`) and writes **two** `decision_log` rows —
      `capture.unit.created` and `capture.hook.deferred` with `context.kind =
      "ambiguous_person_ref"`; the suite's own file carries a paragraph stating I06 is explicitly
      out of scope (not exercised, because this PR creates no `incomplete` unit) rather than
      passing silently (spec R4.7's own MUST).
      Implement the ambiguous-reference branch alongside 10c.1's timer branch (design D9).
      Verify: `make test`; review — the file's own comment names I06 as out of scope.
      Requirement: R4.7.
- [ ] **10c.3** `docs/02-cognitive-core.md` §5's numbered "hooks" item (item 5 inside the "Capture"
      list) gains a note: M1 classifies `timer`/`recurring_reminder`/`person_ref_status: ambiguous`
      per contract but arms nothing and creates no `incomplete` unit until M3/M2 respectively.
      Verify: read the section; `docs-sync.yml` not locally verifiable.
      Requirement: R4.9.
- [ ] **10c.4** Confirm by absence, across the whole of PR 10 (10a+10b+10c): no file under
      `internal/httpapi/**` or a `cmd/nooma` subcommand exists on this chain (R4.8). Review check,
      not new code.
      Verify: `git diff --name-only` for the full PR 10 chain contains no such path.
      Requirement: R4.8.
- [ ] Verify (PR-level): `make check-all`.

---

## PR 11a — `feat/core-relation` (~250)

Depends on nothing outside this chain beyond Phase A PR 2.

- [ ] **11a.1** Test first: `internal/core/relation/verdict_test.go` — `Decide` over all three
      bands (discard, persist-uncertain, persist-asserted) plus both boundary values exactly
      (`confidence == min_confidence_to_persist` → `Uncertain`, inclusive; `confidence ==
      min_confidence_to_surface` → `Asserted`, inclusive — the band-notation reading design D7
      settles against doc 02 §4's ambiguous prose); the table asserts its own completeness.
      **Red**: `undefined: relation.Verdict`, `undefined: relation.Decide`.
      Implement `internal/core/relation/verdict.go` (design D7).
      Verify: `make test`; `golangci-lint run`.
      Requirement: R5.1; design D7.
- [ ] **11a.2** In the same commit: `DefaultMinConfidenceToPersist = 0.30`,
      `DefaultMinConfidenceToSurface = 0.50`, `DedupCandidateK = 5`; an L2 test,
      `test/conformance/relation_thresholds_ddl_test.go`, reads migration 0002's
      `relation_thresholds` column `DEFAULT`s off disk (`migrationSQLText`/`extractTableBody`,
      confirmed reusable) and asserts the two threshold constants equal them, compared as parsed
      floats. `Resolve(row *Thresholds) Thresholds` returns the defaults when `row` is `nil` —
      Q1's closed answer, the fallback a relation type with no `relation_thresholds` row needs.
      **Red**: `undefined: relation.Thresholds`, `undefined: relation.Resolve`, `undefined:
      relation.DefaultMinConfidenceToPersist` (etc).
      Implement `internal/core/relation/thresholds.go` (design D7, Q1).
      Verify: `make test`.
      Requirement: R5.2; design D7, Q1.
- [ ] **11a.3** **Closes the gap C4 identifies above.** `docs/02-cognitive-core.md` §13 gains a
      `dedup_candidate_k` row. `DedupCandidateK` is a behavioral number per `CLAUDE.md`
      non-negotiable #4 and the `nooma-core` skill's Hard Rule 4 ("every behavioral number is a
      named constant in exactly one place and appears in the calibration table of doc 02 §13");
      `design.md`'s own Risk #7 names it as unmeasured and in need of future calibration — the
      row is where that calibration will happen. `design.md`'s chain table attributes no §13 task
      to PR 11a for this constant; this task supplies it rather than carrying the gap forward
      silently.
      Verify: read the table; `docs-sync.yml` not locally verifiable.
      Requirement: `CLAUDE.md` non-negotiable #4 / `nooma-core` Hard Rule 4 (a design gap this
      phase closes; not itself spec-numbered).
- [ ] **11a.4** `docs/02-cognitive-core.md` §4 gains Q1's one sentence naming the fallback R5.2
      implements.
      Verify: read the section; `docs-sync.yml` not locally verifiable.
      Requirement: R5.6.
- [ ] Verify (PR-level): `make check-all`; `make cover` (`core/relation`'s first statements).

---

## PR 11b — `feat/ports-relation` (~300)

Depends on PR 11a.

- [ ] **11b.1** Test first: a `repocontract`-shaped `RelationRepo` suite — `Upsert` run twice over
      the same `(from, to, type)` triple leaves exactly one row, with the second run's
      `strength`/`confidence` reflected, never a uniqueness-constraint error and never two rows
      (I07); `ByUnit` returns a unit's relations; `ThresholdsFor` returns `(nil, nil)` for an
      absent row (this is where Q1's "no row" case actually originates, per design D8). **Red**:
      `undefined: ports.RelationRepo`.
      Implement `internal/ports/relationrepo.go`: `Relation`, `RelationRepo{Upsert, ByUnit,
      ThresholdsFor}` (design D8). No `Delete*`-prefixed method — keeps
      `test/conformance/i03_units_never_deleted_test.go`'s strengthened prefix set satisfied.
      Verify: `go build ./...`; `golangci-lint run`.
      Requirement: R5.3 (interface half); design D8.
- [ ] **11b.2** Same commit: `test/support/memrepo`'s `RelationRepo` fake + the contract suite's
      first caller.
      Verify: `go test -race ./test/support/memrepo/...`.
      Requirement: R5.3 (fake half).
- [ ] **11b.3** Test first (L3): `internal/store/sqlite/relationrepo_integration_test.go` — the SQL
      upsert (`ON CONFLICT (from_unit_id, to_unit_id, type) DO UPDATE SET strength =
      excluded.strength, confidence = excluded.confidence`) against the real `UNIQUE` constraint
      (confirmed present, `0001:39`) — I07 proven against real SQLite, not only the fake.
      Implement `internal/store/sqlite/relationrepo.go`.
      Verify: `make test-integration`; `make store-api-golden`.
      Requirement: R5.3 (store half).
- [ ] Verify (PR-level): `make check-all`.

---

## PR 11c — `feat/relation-judge` (~280)

Depends on PR 11b **and PR 10b** — per **C1**'s corrected dependency direction above (this PR
wires the judge call into `captureRunner`, created by 10b; `design.md`'s own stated `11c → 10b`
arrow is backwards).

- [ ] **11c.1** Test first: `internal/core/relation/judgment_test.go` — `DecodeJudgment` reuses
      `classify.Salvage` over a judge-shaped response (the same tolerant-decode mechanism I14
      already proves, a second consumer per design D7's "one project, two tolerant decoders that
      can disagree" reasoning against reimplementing it). **Red**: `undefined:
      relation.DecodeJudgment`, `undefined: relation.Judgment`.
      Implement `internal/core/relation/judgment.go`: `Judgment{Outcome, TargetUnitID, Type,
      Strength, Confidence, Degradations}`, `Outcome` (`new`/`duplicate`/`related`) — `core/relation`
      importing `core/classify` is legal under `core-purity`'s prefix allow (design D7's named,
      short-lived reversal criterion: a third consumer outside the capture pipeline).
      Verify: `make test`; `golangci-lint run` (confirms `depguard` allows `core/relation` →
      `core/classify`).
      Requirement: R5.5 (decode half); design D7.
- [ ] **11c.2** Test first: the judge's full path — `ports.LLMProvider.Complete` with
      `Task:"relation_evaluation"` (already in `internal/config.DocumentedTaskNames`, confirmed) →
      `relation.DecodeJudgment` → `relation.Decide` against the resolved `Thresholds` — driven
      through `fakeprovider`/`memrepo` end to end, asserting the persisted outcome matches the
      recorded confidence's band.
      Implement the wiring into `captureRunner.at`, extending 10b's pipeline with the remaining
      steps of design D4's diagram (`jr := judge.Complete(...)`, `j, _ :=
      relation.DecodeJudgment(...)`, `v := relation.Decide(...)`, `rels.Upsert(...)`).
      Verify: `make test`.
      Requirement: R5.5 (wiring half).
- [ ] **11c.3** Test first: a candidate below `min_confidence_to_persist` leaves no `relations` row
      and exactly one `decision_log` row recording the discard and its rationale (I08).
      Verify: `make test`.
      Requirement: R5.4 (discard half).
- [ ] **11c.4** Test first: a `duplicate`-outcome judgment writes a `duplicate`-typed relation from
      the new unit to the existing one and a `decision_log` row stating plainly the duplicate was
      recorded, not merged (design D7's own resolution — neither superseding the new unit nor
      reviving the existing one is in scope; the direction is not canonicalized, an accepted,
      named risk with an M2 owner).
      Verify: `make test`.
      Requirement: R5.4 (adjacent — the `duplicate` case design D7 resolves); design D7.
- [ ] **11c.5** Confirm: the judge is never called when the candidate list is empty — already
      enforced for free by `fakeprovider`'s unscripted-call failure (review check, not new code,
      per design D4's own framing); add one explicit assertion of zero `LLMProvider.Complete` calls
      beyond `capture_processing` for a capture into an otherwise-empty vault.
      Verify: `make test`.
      Requirement: design D4 (§6 test matrix, PR 11c row).
- [ ] Verify (PR-level): `make check-all`; confirm no code in this PR canonicalizes relation
      direction and no `relations.uncertain` column is added (design D7's named, accepted risks —
      an M2 concern, not this PR's).

---

## Cross-cutting verification (spec §6, §8)

- **R6.1/R6.2/R6.3 (no network, providers)**: every PR above whose tests touch a provider goes
  through `test/support/fakeprovider`; reviewed per-PR at each PR-level verify line, restated here
  because it spans the whole chain.
- **R6.2 (doc 02 delta per core-touching PR, no `no-spec-change`)**: PR 7a/7b/7c, PR 8a/8b, and PR
  11a each carry a real doc 02 delta (tasks 7b.4, 7c's own I14 addition needs none beyond 7b.4,
  8a.3, 8b.3, 11a.3, 11a.4). PR 9a/9b/9c and PR 10a/10b touch no `internal/core/**` file and are
  not forced by `docs-sync.yml`. PR 10c carries a real delta anyway (task 10c.3) — a fact about the
  brain, not merely an adapter detail, per `design.md` §3's own note that "not required" must not
  be read as "not wanted."
- **R7.2 (every test observed failing first)**: each task above states its own red; verified at
  apply time by the commit sequence, per this document's own strict-TDD framing.
- **R8.1 (no correction/Q3b/Q3c work)**: no task above creates a correction path, resolves Q3b, or
  writes to `learning_signals` — confirmed by the package layout in §3 of `design.md`, carried
  here without alteration.
- **R8.2 (no non-goal work)**: no task computes `effective_weight`, implements consolidation,
  arms/fires a trigger or timer beyond 10c.1's explicit non-arming, derives a self-belief, or
  touches Telegram/`reindex`/perception.
- **R8.3 (`incomplete → archived` and other Phase A landings)**: not this chain's concern —
  `m1a-substrate`'s own C1 resolved it; no task above touches `internal/core/unit/**`.

---

## Review Workload Forecast

**Chained PRs recommended: yes — fifteen code PRs plus one operator action (C5), all pre-split by
`design.md` §5 before any code exists**, following the discipline `m1a-substrate`'s own
retrospective earned after its PR 2a shipped at 2.6x its estimate with no split line drawn in
advance.

**400-line budget risk, per PR (ceilings are the lines design chose to fit the 400-line soft
rule, never predictions):**

| PR | Ceiling | Risk |
|---|---|---|
| 8a | ~380 | **High** — at the ceiling before any measured multiplier; also carries the `pending-red` retirement's ten-file diff, whose line count is fixed regardless of code size |
| 8b | ~200 | Low |
| 8c | ~250 | Low–medium |
| 7a | ~330 | Medium |
| 7b | ~280 | Low–medium |
| 7c | ~250 | Low–medium |
| 9a | ~280 | Low–medium |
| 9b | ~380 | **High** — the store adapter class (BLOB codec, index load, L3 fixtures) is exactly the shape that overran in Phase A's PR 4 (measured 1.42x–1.53x even after a pre-drawn split) |
| 9c | ~220 | Low |
| 10a | ~250 | Low–medium |
| 10b | ~400 | **High** — at the ceiling on its own stated estimate; the first PR to create real `internal/brain/` code, the pipeline orchestration Phase A never exercised, and the PR carrying both new L2 guards |
| 10c | ~200 | Low |
| 11a | ~250 | Low–medium |
| 11b | ~300 | Medium |
| 11c | ~280 | Medium — depends on 10b per C1's correction, so any 10b overrun or mid-apply split propagates into how 11c's own diff is scoped |

**Decision needed before apply: no — the chain is already fully pre-split, with every line drawn
in `design.md` §5 before any code exists**, which is the single correction Phase A's own 2.6x
outlier earned. Three PRs (8a, 9b, 10b) sit at or past what `m1a-substrate`'s own measured band
would call risky even from a correctly-drawn ceiling — none of Phase A's seven pre-drawn splits
(2.6x, 1.77x, 1.70–1.84x, 1.42x, 1.74x, 1.87x) landed under 1.4x, and 8a/9b/10b's content
(a ten-file gate retirement; a BLOB-codec store adapter; the first real orchestration package) each
match a category that has already overrun once in this project. **Recommendation, following
`m1a-substrate`'s own real precedent rather than inventing a new rule**: apply should treat these
three PRs as stop-and-report checkpoints once their own cumulative diff crosses roughly 300
lines — the same threshold Phase A's PR 5a/PR 6a used — and report to the owner under
`delivery_strategy: ask-on-risk` rather than pushing through to the ceiling and discovering the
overrun only at PR-open time.

**On the estimates themselves.** This chain's own governing instructions state the realized
multipliers Phase A actually measured: 2.6x, 1.68x, 1.77x, 1.84x, 1.53x, 1.74x, and two PRs under
estimate. Applying the tighter end of that band (1.4x) to this chain's raw total of **~4,250
budgeted lines across fifteen PRs** puts the realistic range at **roughly 5,500–7,500 review
lines**, and applying the outlier end (2.6x, PR 2a's own measured ceiling for "pure core plus its
conformance guards" — a description that matches 7a, 8a, and 11a in this chain more closely than
any Phase A PR did) pushes parts of this chain toward **9,000+** if the pattern repeats on
`core/classify` specifically, which `design.md`'s own D11 names as "the one at risk." Every
ceiling above is stated as a target to design against, never as a prediction.

---

## Traceability

| Spec section | Requirements | Tasks |
|---|---|---|
| §1 `core/classify` | R1.1–R1.7 | 7a.1–7a.5, 7b.1–7b.4, 7c.1–7c.4 |
| §2 `core/recall` | R2.1–R2.10 | 8a.0–8a.4, 8b.1–8b.3, 8c.1–8c.3 |
| §3 `store/sqlite` search additions | R3.1–R3.6 | 9a.1–9a.3, 9b.1–9b.3, 9c.1–9c.2 |
| §4 `internal/brain` capture pipeline | R4.1–R4.10 | 10a.1–10a.3, 10b.1–10b.8, 10c.1–10c.4 |
| §5 `core/relation` and the judge | R5.1–R5.7 | 11a.1–11a.4, 11b.1–11b.3, 11c.1–11c.5 |
| §6 cross-cutting | R6.1–R6.3 | Cross-cutting verification section; every PR-level verify line |
| §7 test levels | R7.1–R7.2 | every "Test first" line; R7.1's L1/L2/L3 assignment matches each task's own stated level |
| §8 boundaries | R8.1–R8.2 | Cross-cutting verification section |
| §9 open items | (design's choices) | C1–C6 above; design D2 (fixture formats), D5 (tie-break, threshold-resolution home), D7 (`duplicate` handling, port signatures) as cited per task |
