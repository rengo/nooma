# Tasks — M1a: the substrate

Implementation task list for `m1a-substrate`, derived from `spec.md` (11 sections) and
`design.md` (9 decisions, D1–D9). Chain strategy **`stacked-to-main`**, matching M0 and the
umbrella proposal §5. Scope is Phase A only — the umbrella proposal's six-PR table, which design's
own Risk #1 already expands to seven merges once PR 2's split is applied.

**Strict TDD is active.** Every behavioral task states the test first and what its red looks
like. Several tasks below have no natural pre-implementation red — they are structural proxies or
review-only confirmations over code that is already correct by the time they are written (D9's
presence guard, the `test/support` import scan, the store's no-`time.Now` scan). Each of those
says so honestly rather than inventing a red that does not exist; §"On the tasks with no natural
red" below states the discipline applied instead.

Verification commands are drawn from the project's real targets, confirmed present in `Makefile`
during this session: `make check`, `make check-all`, `make test`, `make test-integration`,
`make test-e2e`, `make cover`, `make store-api-golden`, `make pending-red`. No task cites
`make cross-compile` — Phase A adds no OS-dependent code.

---

## Conflicts surfaced (do not silently resolve)

### C1 — `spec.md` R2.3 and `design.md` D3 disagree about `incomplete → archived`

`spec.md` R2.3's legal-transition table lists four pairs (`incomplete→pool`, `pool→archived`,
`archived→pool`, `pool→superseded`) and its own **MUST NOT** clause names `incomplete → archived`
explicitly as illegal, in the same sentence as `incomplete → superseded` and `superseded → pool`.

`design.md` D3 lists **five** legal pairs — the same four plus `incomplete → archived` — and
argues for it directly from doc 02 §1's own text: an unresolved `incomplete` unit is "expired
after 24 h", the status vocabulary has no `expired` member (R2.1 closes it at four), and I03
forbids deleting it. The only status left for an expired `incomplete` unit to land in is
`archived`. Design's own Risk #6 names this as an open acceptance point for the owner and commits
to writing the landing status into doc 02 §1 in PR 2.

Both documents were read directly, at the lines cited above — this is not an inference. **The
tasks below follow `design.md` D3's five-pair table**, for two reasons: it is the design's own
resolution of a case the spec's derivation did not have an answer for at the time R2.3 was
written (nothing else in the vocabulary can hold an expired `incomplete` unit without inventing a
fifth status, a migration, and a doc 03 edit R9.2 forbids), and design's Risk #6 already frames it
as an owner-facing acceptance rather than a silent override. Task 2.11 below writes the exact
sentence into `docs/02-cognitive-core.md` §1, and its commit message records this resolution.
**`spec.md`'s R2.3 MUST NOT clause needs a follow-up correction** to drop `incomplete → archived`
from the illegal list — flagged here per `CLAUDE.md` non-negotiable #1 (code and spec must not
diverge silently), and left for whoever next revises `spec.md`, since this tasks artifact cannot
edit it.

### C2 — Phase A is seven merges, not six, once design's own PR 2 split lands

`m1-capture-recall/proposal.md` §5's Phase A table lists six rows (PRs 1–6). `design.md` Risk #1
mandates splitting PR 2 into **2a** and **2b** before apply, with the exact split line drawn in
design itself. The tasks below number the chain **1, 2a, 2b, 3, 4, 5, 6** — seven PRs landing
Phase A's six-PR table. This is not a new finding (design already states it as accepted), but a
reader comparing this document's headings against the proposal's own table would otherwise wonder
why the counts differ, so it is named here rather than left implicit.

### C3 — The proposal's Phase A table cites a doc 02 delta for PR 3 that spec and design both reject

`m1-capture-recall/proposal.md` §5's Phase A row for PR 3 (`feat/ports-unitrepo`) reads
"`ports.UnitRepo` + an in-memory fake for L2; **promote I03; doc 02 §1**". Both later artifacts
disagree with the trailing clause: `spec.md` R8.3's own **MUST NOT** states plainly that PR3,
PR4, PR5, and PR6 must not touch `internal/core/**`, and `design.md` §3's dependency-rule check
says explicitly, having read `docs-sync.sh:45-51`, that "PR 2 is the only Phase A PR that needs a
doc 02 delta … the proposal's §5 rows suggesting otherwise for PR 3 can be read as optional."
`ports.UnitRepo` lives in `internal/ports`, not `internal/core`, so `docs-sync.yml` never fires on
PR 3 regardless. **The tasks below carry no doc 02 task for PR 3** — the two later, more-verified
artifacts agree, and the proposal's own table entry is read as stale per design's own instruction.

### C4 — Design leaves which vendor implements `EmbeddingProvider` out of its package diagram

`spec.md` §11 explicitly defers "which vendor backs `ports.EmbeddingProvider`" to design.
`design.md` §3's package layout for `internal/providers/` lists `anthropic/ openai/ ollama/` with
no annotation of which one carries `Embed`. The answer is not actually open, though: D7's prose
says PR 1 rebinds doc 01's `tasks.embedding` placeholder to `local_llama` — the one declared
`providers:` entry ADR-0003 says can embed, and `local_llama`'s `type` is `ollama`
(`docs/01-architecture.md:184-187`, confirmed by reading). **Task 6.2 below makes this explicit**:
the `ollama` client is the one that implements `ports.EmbeddingProvider` in Phase A. This is a gap
between design's diagram and its own prose, not a contradiction between two decisions — recorded
so it is not silently rediscovered mid-apply.

### On the tasks with no natural red

Tasks 2.6, 3.5, and 4.5 add L2 guards that `design.md` itself frames as proxies (D9) or as this
design's own invented scope (§7 Risk #10) over code that, at the moment each guard is written, is
already correct — there is no pending implementation gap for them to expose. Strict TDD's
"observed failing for the right reason" discipline is honored differently for these three: each
task states explicitly that it has no pre-implementation red, and its own verification step
includes a **temporary-break check** — introduce the violation the guard exists to catch (an
untested exported symbol, an import of `test/support` from production code, a stray `time.Now()`
under `internal/store/`), confirm the guard reports it, then revert before committing. This is the
same honesty the D9 presence-guard's own doc comment is required to carry (design §2 D9,
mirroring `golden_sets_test.go:164-176`'s existing proxy-announcement precedent).

---

## PR 1 — `docs/m1-preflight` (~40 lines — reduced from the proposal's ~90; two of its
original items were already fixed before this spec was written, per `spec.md`'s scope boundary)

Docs only. Touches no `internal/core/` path, so `docs-sync.yml` does not fire and no
`no-spec-change` label is needed. No behavioral test — each requirement's own "Verified by" is
reading the document.

- [x] **1.1** `docs/01-architecture.md`: add an `openai` provider entry to the example's
      `providers:` block (design D7 — "PR 1 adds an `openai` entry to doc 01's `providers:`
      block"), naming `openai` as a `type` alongside the existing `anthropic`/`ollama`/
      `whisper_cpp` entries.
      Verify: read the file — `openai` appears in the provider-type list; `make check` (confirms
      `TestHarness_ConfigMatchesDoc01`, the M0 config↔doc gate, still passes — an `openai` entry
      introduces no field name the existing four entries do not already use, per design D7).
      Requirement: R1.1.
- [x] **1.2** `docs/01-architecture.md`: replace the `nooma.yml` example's
      `embedding: { provider: ... }` (confirmed present, line 199) with
      `embedding: { provider: local_llama }` — the only declared `providers:` entry ADR-0003 says
      can embed (design D7).
      Verify: `rg "provider: \.\.\." docs/01-architecture.md` returns nothing; `make check`.
      Requirement: R1.2.
- [x] **1.3** `docs/06-harness.md` §1's `internal/core/` tree diagram: add a `classify/` line
      alongside the existing `unit/`, `weight/`, `focus/`, `recall/`, `relation/`,
      `consolidation/`, `prospection/`, `selfmodel/`, `learning/` lines (confirmed absent today by
      reading the tree — grepped for `classify` in the file and found no hit). Do **not** create
      `internal/core/classify/` itself.
      Verify: read the tree; `git diff --name-only` for this task contains no path under
      `internal/core/classify/`.
      Requirement: R1.3.
- [ ] **1.4** Confirm by reading, not by editing: `docs/README.md` and `CLAUDE.md` do not need a
      change in this PR (both were corrected by the M0 chain before this spec was written, per
      `spec.md`'s own verification note). This task is a review check, not new code.
      Requirement: R1.4.
- [ ] Verify: `make check`; `rg "next to the executable" docs/` unaffected (M0's own invariant,
      unrelated but cheap to re-confirm nothing regressed); every edit read against `spec.md` §1's
      "Verified by" clauses.

---

## PR 2a — `feat/core-unit`, part 1: `Status`, `AllStatuses`, `ParseStatus`, `IsLive`, I01's
promotion (~180 lines estimated — **shipped at 465**, `size:exception`, owner decision
2026-07-31)

> **The estimate table needs a new row, and this is it.** 465 changed lines against a ~180
> ceiling is **2.6×** — outside the 1.3×–2.2× band this project measured six times across M0 and
> wrote into every artifact in this chain as its correction factor. The band was measured on M0's
> slices, all of which were adapter or harness work. This is the first slice of **pure core code
> with its conformance guards**, and it came in higher than the band predicts. One measurement is
> not a new band, but it is a reason to stop treating 2.2× as the ceiling of the correction.
>
> Where the lines actually went: 203 of production and doc, and **262 of new test code across
> three files** — `status_test.go`, `parse_status_test.go`, `is_live_test.go`, plus the two new L2
> guards (`unit_status_ddl_test.go` at 51, `core_exported_decls_have_tests_test.go` at 142). The
> guards are the part the estimate did not see: design called them "roughly 200 lines across PRs
> 2, 3 and 4" and two of the three landed here.
>
> A clean split existed and was offered — `2a-core` (~272: the vocabulary, its L1 tests, I01's
> promotion, the doc 02 delta) and `2a-guards` (~193: the two L2 guards). **Owner chose the
> exception**: the guards exist to pin exactly what 2a introduces, and reviewing them apart from
> the vocabulary they watch means reading both twice.
>
> **For the PRs still ahead**: PR 4's ~380 ceiling was already flagged high-risk with no
> pre-drawn line. At 2.6× that is 990 lines. Draw its split before its diff exists.

Depends on nothing outside this chain. This is the first PR to land a statement in
`internal/core/`, so the ≥90 % coverage floor (`make cover`, `make check-all` only) and
`docs-sync.yml` both fire for real for the first time. `docs/02-cognitive-core.md` §1 gains its
first real delta here (task 2.5) — the PR must not carry `no-spec-change` (spec R8.3).

- [x] **2.1** Test first: `internal/core/unit/status_test.go` — `Status`'s `reflect.Kind()` is
      `reflect.String`; `AllStatuses()` returns exactly `{pool, archived, superseded, incomplete}`
      as a set, `"focus"` not among them. Alongside it, an L2 guard,
      `test/conformance/unit_status_ddl_test.go` (untagged): reads migration 0001's `units.status`
      column comment via the existing `migrationSQLText` helper
      (`test/conformance/i13_learning_signal_test.go:24-57`, confirmed present and reusable) and
      asserts it names exactly `AllStatuses()`'s members (design D1's third bullet — one source of
      truth in Go, one in SQL, pinned together). **Red**: both files fail to build —
      `undefined: unit.Status`, `undefined: unit.AllStatuses`.
      Then implement `internal/core/unit/status.go`: `type Status string`, the four constants
      (`StatusPool`, `StatusArchived`, `StatusSuperseded`, `StatusIncomplete`), `AllStatuses()
      []Status` returning a fresh slice each call (design D1 — not an exported `var`, which would
      be mutable by any importer and defeat I01's own vocabulary check from outside the package).
      Verify: `make test`; `golangci-lint run` (confirms `depguard`'s `core-purity` and
      `forbidigo` stay clean — the file imports nothing beyond stdlib and needs no clock).
      Requirement: R2.1; design D1.
- [x] **2.2** Test first: `TestParseStatus_RoundTripsAndRejectsUnknown` — every `AllStatuses()`
      member parses back to itself; an unrecognized string returns `ErrUnknownStatus` naming the
      value. **Red**: `undefined: unit.ParseStatus`.
      Implement `ParseStatus(string) (Status, error)` — the sole entry point from untrusted text
      (design D1).
      Verify: `make test`.
      Requirement: R2.1 (boundary-validity half); design D1.
- [x] **2.3** Test first: `TestStatus_IsLive` — a table over all four `AllStatuses()` members plus
      a deliberately unknown `Status("bogus")` value, asserting `true` for exactly `pool`. **Red**:
      `undefined: unit.Status.IsLive` (unknown method).
      Implement `func (s Status) IsLive() bool { return s == StatusPool }` (design D2 — no clock,
      no arguments; liveness is a property of the status value, not a function of time).
      Verify: `make test`.
      Requirement: R2.2; design D2.
- [x] **2.4** In the **same PR** as 2.1–2.3 (spec R7.1's MUST, design D8's sequencing — the helper
      trap): drop the `//go:build pendingimpl` tag from
      `test/conformance/i01_focus_never_persisted_test.go` **and** from
      `test/conformance/tree_scan_test.go` in the same commit; remove the two lines `unit.Status`
      and `unit.AllStatuses` from `test/conformance/pending_symbols.txt`; delete
      `internal/core/unit/doc.go`'s "Pending conformance anchor" paragraph (confirmed present,
      lines 5–14), leaving the package-comment lines above it intact.
      **Do not** untag `tree_scan_test.go` before this task lands `unit.Status`/`unit.AllStatuses`
      (leaves `scanGoTree` with no untagged caller, fails lint as `unused`, per the proposal's own
      measured finding, §4.7) — this task's ordering after 2.1 is load-bearing, not incidental.
      Verify: `make pending-red` (reports `OK` — `recall.VectorQuery`, `recall.VectorIndex`, and
      `ports.UnitRepo` still correctly `undefined:`, three lines); `make check` compiles cleanly
      (no `undefined: scanGoTree`); `golangci-lint run` reports no `unused` finding on
      `scanGoTree`.
      Requirement: R7.1.
- [x] **2.5** `docs/02-cognitive-core.md` §1: state the live-status predicate as the positive
      filter it is — `IsLive()` is `status == pool`, and every read surface filters positively
      (design D2). This is the doc 02 delta that keeps this PR off `no-spec-change` (spec R8.3).
      Verify: read the section; `docs-sync.yml` cannot be verified locally (needs an open PR, per
      spec R8.3's own "Verified by" — noted, not claimed).
      Requirement: R8.3 (this PR's share).
- [x] **2.6** Design D9's presence-guard proxy — **no natural pre-implementation red** (see
      "On the tasks with no natural red" above). Add
      `test/conformance/core_exported_decls_have_tests_test.go` (untagged L2): walks
      `internal/core/**`, and for every directory holding an exported top-level declaration,
      asserts a sibling `_test.go` file exists and names that declaration; reports "armed but
      vacuous" when it finds zero declarations, mirroring `scripts/core-coverage.sh:102-105`'s own
      wording, rather than passing with a bare OK when the tree is still empty (announced as a
      proxy in its own doc comment, following `golden_sets_test.go:164-176`'s precedent — it says
      what it does not check: branch coverage, or a name mentioned only in a comment).
      Verify: `make test`; **temporary-break check** — comment out `status_test.go`'s reference to
      one exported name (e.g. `IsLive`), confirm the guard fails naming it, then restore before
      committing.
      Requirement: design §2 D9, §6 test matrix row 3 (not itself spec-numbered).
- [x] **2.7** `make cover` at this point in the chain (only `status.go`, `ParseStatus`, `IsLive`
      exist under `internal/core/unit`) confirms the ≥90 % floor. This is the first PR where
      `scripts/core-coverage.sh` reports a real number instead of `total=0` — read its output, not
      only its exit code.
      Verify: `make cover`.
      Requirement: R8.2 (this PR's share).
- [x] Verify (PR-level): `make check-all`.

---

## PR 2b — `feat/core-unit`, part 2: `Type`, `Unit`, transitions (~220 lines — design's split
line, part 2 of 2; carries C1's resolution)

Depends on PR 2a (same package, same file set). Still the only Phase A PR-half touching
`internal/core/**` besides 2a — `docs/02-cognitive-core.md` §1 gains its second Phase A delta here
(task 2.11).

- [x] **2.8** Test first: `internal/core/unit/type_test.go` — `AllTypes()` returns exactly the
      nine values doc 02 §1 lists (`task`, `mental_load`, `event`, `knowledge`, `procedural`,
      `emotional`, `list`, `structured_ref`, `insight`), and asserts `"timer"` and
      `"recurring_reminder"` are **not** members (design D4 — those are classify *outcomes*, a
      different vocabulary, Phase B's). **Red**: `undefined: unit.Type`, `undefined: unit.AllTypes`.
      Implement `internal/core/unit/type.go`: `Type` (identical shape to `Status` — design D1's
      pattern repeated per D4), the nine constants, `AllTypes()`, `ParseType`, `ErrUnknownType`.
      Verify: `make test`; `golangci-lint run`.
      Requirement: R2.4; design D4.
- [x] **2.9** Test first: `internal/core/unit/transition_test.go` — an exhaustive table over all
      16 ordered pairs from `AllStatuses() × AllStatuses()`, asserting `ValidateTransition` returns
      `nil` for exactly the **five** legal pairs `design.md` D3 names (`pool→archived`,
      `pool→superseded`, `archived→pool`, `incomplete→pool`, `incomplete→archived` — **per C1's
      resolution above**, which departs from `spec.md` R2.3's four-pair table) and
      `ErrIllegalTransition` for the other eleven, including all four self-pairs (`(s, s)` is
      explicitly illegal, not a no-op — design D3: permitting `pool→pool` would let the brain write
      a no-op `UPDATE` while logging an effect, violating I12). The test asserts its own
      completeness — the expectation map's size equals `len(AllStatuses())²` (design D9 point 3),
      so an added status without a matching expectation fails loudly instead of silently reading as
      illegal. **Red**: `undefined: unit.ValidateTransition`.
      Implement `internal/core/unit/transition.go`: an unexported `map[Status][]Status` (or
      equivalent closed table — design D3 rejects an exported map as mutable-from-outside) plus the
      one exported `func ValidateTransition(from, to Status) error`, returning
      `ErrIllegalTransition` or `ErrUnknownStatus` (reusing 2.2's sentinel for out-of-vocabulary
      inputs).
      Verify: `make test`; `golangci-lint run`.
      Requirement: R2.3, as resolved by C1; design D3.
- [x] **2.10** `internal/core/unit/unit.go`: the `Unit` struct, fixed by design §4 — nullable
      columns as pointers and `json.RawMessage`, never `sql.NullX` (`database/sql` is denied inside
      `internal/core` by `depguard`); `Confidence *float64`, always `nil` in Phase A per the
      umbrella proposal §8 Q2's recommended answer, costing Phase A nothing either way. No
      dedicated L1 test: the struct carries no behavior of its own in Phase A (no constructor, no
      validation) — its round-trip fidelity becomes testable through PR 3's fake (R3.3) and PR 4's
      SQLite implementation (R4.1), not here. This task is a review check on shape, not a red/green
      pair.
      Verify: `go build ./...`; `golangci-lint run` (confirms no `time.Now`, no `database/sql`
      import).
      Requirement: design §4 (traced informally to R2's package as a whole; no independent
      spec-numbered requirement for the struct shape itself).
- [x] **2.11** `docs/02-cognitive-core.md` §1: add the transition table (the five pairs from 2.9)
      and the sentence naming `archived` as the landing status for an expired `incomplete` unit
      (design D3 — "the only status left"). State C1's resolution in one sentence here, so the next
      reader of doc 02 does not have to re-derive it from `transition.go`'s table.
      Verify: read the section; `docs-sync.yml` not locally verifiable (per spec R8.3).
      Requirement: R8.3 (this PR's share).
- [x] **2.12** `make cover` over the now-complete `internal/core/unit` package (status, type, unit,
      transition all present) confirms the ≥90 % floor holds with the full package, not just 2a's
      slice.
      Verify: `make cover`.
      Requirement: R8.2 (this PR's share).
- [x] Verify (PR-level): `make check-all`; confirm `git diff --name-only` for this PR contains no
      path under `internal/core/classify/`, `internal/core/recall/`, or `internal/core/relation/`
      (R9.3); confirm `tree_scan_test.go`'s tag is untouched by this PR (already untagged by 2a —
      R7.1's MUST NOT against re-touching it, restated here as a PR-boundary check).

---

## PR 3 — split into 3a and 3b during apply (~200 lines estimated — **3a shipped at 353**, 1.77×;
3b's own size note follows the task list)

> **The estimate table needs another row.** PR 3 shipped as two merges, not one — a package
> boundary (the port + contract suite vs. the fake + its wiring + a boundary guard), not a
> line-count negotiation. **3a** (`feat/ports-unitrepo`, tasks 3.1–3.2, merged PR #50, `4ed182e`)
> landed `ports.UnitRepo`, its three sentinel errors, `repocontract.RunUnitRepo`, and I03's
> promotion with its strengthened denied-prefix set — **353 changed lines against this section's
> own ~200-line ceiling, 1.77×**, inside the 1.3×–2.2× band this chain has now measured five
> times. **3b** (`feat/ports-unitrepo-fake`, tasks 3.3–3.5) is this document's second half — its
> own size note follows the task list below.

Depends on PR 2b (the `Unit` struct 3.1's repocontract cases construct). Touches no
`internal/core/**` file — **no doc 02 task in this PR**, per C3's resolution above.

- [x] **3.1** Test first: `test/support/repocontract/repocontract.go`'s
      `RunUnitRepo(t *testing.T, newRepo func(t *testing.T) ports.UnitRepo)` — a shared contract
      suite, written and watched failing before either implementation exists (design D6's own
      ordering). Cases: `Create`/`ByID` round-trip; `Create` on a duplicate id returns
      `ErrUnitExists`; `ByID` on a missing id returns `ErrUnitNotFound`; `LiveByIDs` excludes
      `archived`/`superseded`/`incomplete` and preserves the caller's `ids` order (this is also
      R3.2's positive-filter proof — a fixture with one unit per status, asserting only `pool`
      rows return, satisfies both); `UpdateContent` leaves every other column unchanged;
      `SetStatus` with a mismatched `from` returns `ErrStatusConflict`. **Red**: writing
      `repocontract.go` itself fails to compile — `undefined: ports.UnitRepo` (the same
      compile-error-as-red pattern spec R10.2 names explicitly for this chain, since the interface
      does not exist yet).
      Then implement `internal/ports/unitrepo.go`: the five-method interface and the three
      sentinel errors (design D5's exact shape — `Create`, `ByID`, `LiveByIDs`, `UpdateContent`,
      `SetStatus`; no `List(status)` parameterized method, per design D5's "I02 made structural"
      argument). This makes `repocontract.go` compile; nothing calls it yet.
      Verify: `go build ./...`; `golangci-lint run`.
      Requirement: R3.1, R3.2; design D5, D6.
- [x] **3.2** In the same commit as 3.1 (spec R7.2): drop the `//go:build pendingimpl` tag from
      `test/conformance/i03_units_never_deleted_test.go`; remove the line `ports.UnitRepo` from
      `test/conformance/pending_symbols.txt`; delete `internal/ports/doc.go`'s "Pending
      conformance anchor" paragraph (confirmed present, lines 9–18). **Do not** re-touch
      `tree_scan_test.go`'s tag — already untagged by PR 2a (R7.2's MUST NOT). Also strengthen
      I03's own reflection prefix set from `{Delete}` to
      `{Delete, Remove, Purge, Drop, Destroy}` — a conformance test may be strengthened, never
      weakened (`docs/06-harness.md` §4; design D5's own stated gap: the prefix check alone would
      let `Purge`/`Remove`/`Drop` slip past it).
      Verify: `make pending-red` (reports exactly two lines remaining: `recall.VectorQuery`,
      `recall.VectorIndex`); `make check` compiles cleanly.
      Requirement: R3.1 (I03 promotion); R7.2.
- [x] **3.3** Test first: `test/support/memrepo/units_test.go` — `TestUnits_TwoInstancesShareNoMutableState`:
      two independently constructed fakes, a write through one is not observable through the other
      (R3.3's isolation requirement — the one property the shared contract suite does not cover,
      since it is specific to the fake's own construction, not the port's contract). **Red**:
      `undefined: memrepo.NewUnits` (new package).
      Implement `test/support/memrepo/units.go`: `NewUnits() *Units`, mutex-guarded (the suite runs
      `-race -shuffle=on`, `Makefile:48`), deep-copying `unit.Unit` values on the way in and out so
      no caller can reach the fake's interior (design D6).
      Verify: `go test -race ./test/support/memrepo/...`.
      Requirement: R3.3; design D6.
- [x] **3.4** In the same commit as 3.3: `test/conformance/unitrepo_memrepo_test.go` (untagged
      L2) — the first caller of 3.1's contract suite:
      `repocontract.RunUnitRepo(t, func(t *testing.T) ports.UnitRepo { return memrepo.NewUnits() })`.
      This is where the contract's every case actually runs for the first time, and where I03's
      *behavioral* half (not just 3.2's structural reflection check) gets proven against a real
      implementation. **Red**, before 3.3 lands: `undefined: memrepo.NewUnits`.
      Verify: `make test` (every `repocontract.RunUnitRepo` subtest green against `memrepo`,
      `-race -shuffle=on` via `make test`'s own flags).
      Requirement: R3.1, R3.2, R3.3 (wired together).
- [x] **3.5** Design's own invented scope (§7 Risk #10, not spec-numbered) — **no natural
      pre-implementation red**: add `test/conformance/no_test_support_import_test.go` (untagged
      L2) — a tree scan over non-`_test.go` files under `internal/` and `cmd/`, failing if any
      imports `github.com/rengo/nooma/test/support/...` (the boundary `test/support` exists to
      cross: available to L2/L3/L4 and to `internal/brain`'s own tests later, never to production
      code).
      Verify: `make test`; **temporary-break check** — add a throwaway import of
      `test/support/memrepo` to a scratch `.go` file under `internal/`, confirm the scan fails
      naming it, then delete the scratch file before committing.
      Requirement: design §6 test matrix ("No non-test file under `internal/` or `cmd/` imports
      `test/support/`", attributed to PR 3).

> **3b's own size-discipline stop.** `git diff --stat main` after 3.3+3.4 (one commit, per 3.4's
> own "same commit as 3.3" instruction) measured **272 changed lines** — under this session's
> 280-line stop-and-report gate. Adding 3.5's guard (implemented, its temporary-break check run
> and observed failing correctly, then reverted — see the apply-progress artifact for the exact
> failure line) brought the cumulative diff to **340 changed lines against 3b's own ~200-line
> estimate, 1.70×** — over the 280-line gate. Per this session's own instruction ("not advisory"),
> apply stopped **before** committing 3.5: 3.3 and 3.4 are committed on `feat/ports-unitrepo-fake`;
> 3.5's code is written, verified, and left uncommitted in the working tree, pending the owner's
> split decision (`delivery_strategy: ask-on-risk`) — commit it into this same PR as
> `size:exception` (340 lines is 1.70×, inside the 1.3×–2.2× band this chain keeps measuring), or
> land it as its own small follow-up PR (68 lines, no dependency on the fake beyond the tree it
> scans already existing).

- [ ] Verify (PR-level): `make check-all`; confirm `git diff --name-only` contains no
      `internal/core/` path (R8.3's MUST NOT for PR 3, restated per C3).

---

## PR 4 — `feat/store-unitrepo` — **SPLIT into 4a and 4b, drawn 2026-07-31, before any code
existed**

> The forecast flagged PR 4 as the last high-risk entry with no pre-drawn line: ~380 against a
> 400 ceiling, which the measured 1.3×–2.2× band puts at 500–840, and PR 2a's 2.6× outlier puts
> near 990. PR 2a overran because its split was considered only once the diff existed. This one
> is drawn first.
>
> | PR | Tasks | Content | Est. |
> |---|---|---|---|
> | **4a** | 4.1, 4.2, 4.3, 4.4 | The SQLite `UnitRepo`, the contract suite run at L3, the positive-filter and row-count assertions, and the `store_api.golden` regeneration | ~310 |
> | **4b** | 4.5 | The store's clock guard — an L2 tree scan failing on `time.Now(` in non-test files under `internal/store/**` | ~70 |
>
> **Why the line falls there, and not somewhere more even.** Tasks 4.1, 4.2 and 4.3 share one
> red — `undefined: sqlite.NewUnitRepo` — so they are a single commit family and no line can pass
> between them. 4.4 is the golden regeneration that the same PR's new exported surface forces,
> and a golden that lags its own code by one PR is a golden that briefly lies. 4.5 is an
> independent L2 guard in the shape 3.5 and 2.6 already took, and the two previous guards came in
> at 68 and 142 lines.
>
> **The fallback line, in case 4a still crosses 400.** Move 4.2 and 4.3 to 4b. They are *extra*
> assertions beyond what the contract suite already pins, and they stay green once 4a's
> implementation exists — so they can follow it without ever being red for the wrong reason. That
> is a worse split, because R4.2's positive-filter proof should land with the filter it proves,
> which is why it is the fallback and not the plan.

Depends on PR 3. Adds no migration (R4.4) — the `units` table already exists.

> **4a's own size-discipline stop, this session (2026-07-31), before any commit.** Tasks 4.1,
> 4.2 and 4.3 were implemented together — they share one red (`undefined: sqlite.NewUnitRepo`),
> per this section's own "single commit family" reasoning — and all three passed, along with the
> full `repocontract.RunUnitRepo` suite at L3, `make pending-red` (OK, unchanged), `make cover`
> (100%, unchanged, this PR touches no `internal/core/**`), and I01's tree scan (did not trip).
> `git diff --stat main` measured **439 changed lines** (`unitrepo.go` 273, the integration test
> file 139, this document's own split-note bookkeeping 27) — over this session's 330-line
> stop-and-report gate, and this is *before* task 4.4's golden regeneration is even added.
> Per this session's own instruction ("not advisory"), apply stopped **before committing
> anything** — no commit exists on `feat/store-unitrepo` from this session; `unitrepo.go` and
> `unitrepo_integration_test.go` are verified, working-tree, untracked files, not deleted.
>
> **The fallback already drawn above was checked, not assumed.** Trimming task 4.2's
> (`TestUnitRepo_LiveByIDsFiltersPositively`) and 4.3's
> (`TestUnitRepo_UpdateContentDoesNotChangeRowCount`) test functions and their two helpers
> (`seedRawUnit`, `countUnits`) out of the test file, leaving only 4.1's
> `TestUnitRepo_Contract` plus `openTestVault`, reduces the test file from 139 to roughly 35
> lines — a ~104-line saving. That still leaves **`unitrepo.go` at 273 lines on its own**,
> because the production implementation is identical either way (`LiveByIDs`'s positive filter is
> exercised by 4.1's own contract suite through `Create`, regardless of whether 4.2's raw-`INSERT`
> fixture also exists) — so even the fallback split lands close to or over 330 once this
> document's own bookkeeping and task 4.4's golden diff (~7 new golden lines) are added. The
> overrun is dominated by the SQL adapter's own size, not by 4.2/4.3's extra assertions.
>
> **Left for the owner, not decided here**: (a) accept `size:exception` for 4a as originally
> scoped (4.1–4.4 together, ~410–450 lines once the golden lands, inside the 1.3×–2.2× band this
> chain keeps measuring — 439/310 is 1.42×), or (b) apply the fallback split (move 4.2 and 4.3 to
> 4b) knowing it saves real lines but does not bring 4a under 330 on its own. Either way, 4.1's
> implementation and 4.4's golden regeneration are the part that cannot shrink further.

- [ ] **4.1** Test first: `internal/store/sqlite/unitrepo_integration_test.go` (`-tags
      integration`) calling `repocontract.RunUnitRepo(t, func(t *testing.T) ports.UnitRepo {
      return sqlite.NewUnitRepo(vault) })` against a real temporary migrated vault (migrations
      0001/0002 already applied — this PR adds none). **Red**: `undefined: sqlite.NewUnitRepo`.
      Implement `internal/store/sqlite/unitrepo.go`: the five `ports.UnitRepo` methods against real
      SQL, satisfying every case the same `repocontract.RunUnitRepo` suite already pins at L2
      (design D6's "answered twice" standing rule — a port widening lands its contract case and
      both implementations together; this PR's own obligation is only the second implementation,
      since the contract itself was PR 3's).
      Verify: `make test-integration`.
      Requirement: R4.1; design D6.
- [ ] **4.2** In the same test file: `TestUnitRepo_LiveByIDsFiltersPositively` — seeds one unit per
      status (`pool`, `archived`, `superseded`, `incomplete`) via a raw `INSERT INTO units`
      statement (bypassing the repo's own `Create`, so the fixture cannot accidentally rely on the
      repo already excluding non-`pool` rows), calls `LiveByIDs`, asserts exactly the `pool` unit
      returns. This distinguishes the correct positive filter (`status = 'pool'`) from the specific
      wrong negative form spec R4.2's own MUST NOT names (`status NOT IN ('superseded',
      'incomplete')`, which would wrongly include the `archived` row on this exact fixture).
      **Red**: same as 4.1 — `undefined: sqlite.NewUnitRepo` (same commit).
      Verify: `make test-integration`.
      Requirement: R4.2.
- [ ] **4.3** No new test for the tree-scan half of I03 — `test/conformance/i03_units_never_deleted_test.go`
      (promoted, untagged since PR 3) already scans `internal/` tree-wide and will cover this PR's
      new `.go` files automatically; this is a review check, not new code (M0's task 9.3
      precedent). Add one L3 case, `TestUnitRepo_UpdateContentDoesNotChangeRowCount`: asserts
      `SELECT COUNT(*) FROM units` is unchanged after an `UpdateContent` call through the repo.
      **Red**: same commit family as 4.1 — `undefined: sqlite.NewUnitRepo`.
      Verify: `make test-integration`; `make test` (confirms I03's promoted scan still passes with
      the new file present).
      Requirement: R4.3.
- [ ] **4.4** No migration file added or edited — confirm `git diff` over
      `internal/store/sqlite/migrations/` stays empty across this PR (R4.4's MUST NOT). Run `make
      store-api-golden` and review the diff: it should contain only the exported surface this
      PR's `unitrepo.go` adds (the constructor and the concrete `UnitRepo` type name in
      `internal/store/sqlite` — the port's own sentinels live in `internal/ports` and do not
      appear here).
      Verify: `make store-api-golden`; `git diff -- internal/store/sqlite/migrations/` empty;
      `git diff -- testdata/schema` reviewed and committed.
      Requirement: R4.4.
- [ ] **4.5** Design's own invented scope (§7 Risk #10, second guard) — **no natural
      pre-implementation red**, per the ground-truth row confirming no non-test file under
      `internal/store/**` currently references `time.Now` (only two `_test.go` hits and two
      comments, verified by grep during design). Add
      `test/conformance/store_no_direct_clock_read_test.go` (untagged L2): a tree scan over
      non-`_test.go` files under `internal/store/**`, failing on any `time.Now(` call — the store's
      half of R9 (the proposal's own review-only risk, made a compile-visible gate here since
      `forbidigo` does not reach outside `internal/core`).
      Verify: `make test`; **temporary-break check** — add a throwaway `time.Now()` call to a
      scratch file under `internal/store/`, confirm the scan fails, then revert.
      Requirement: design §6 test matrix ("No non-test file under `internal/store/**` references
      `time.Now`", attributed to PR 4).
- [ ] Verify (PR-level): `make check-all`; `store_api.golden`'s diff reviewed and named in the PR
      description, following the M0 PR6 precedent of stating exactly what widened and why.

---

## PR 5 — `feat/provider-fake` (~350 lines)

Depends on PR 3 (needs no `UnitRepo`, but the chain positions it after ports groundwork lands).
Touches no `internal/core/**` file.

- [ ] **5.1** Test first: `test/support/fakeprovider/fakeprovider_test.go` — compile-time
      assertions (`var _ ports.LLMProvider = (*Fake)(nil)`, `var _ ports.EmbeddingProvider =
      (*Fake)(nil)`), plus behavior tests: a scripted, ordered list of case ids; an unscripted
      extra call fails immediately; a test that scripts more calls than the pipeline makes fails
      at cleanup; a recorded `error` case surfaces as a Go error, not a successful response
      containing the error text; selection is by case `id`, never by matching the live `prompt`
      text (the scenario `spec.md` R5.2 states explicitly — a live prompt differing from the
      recording's `prompt` field must not affect replay). **Red**: `undefined: ports.LLMProvider`
      (the fake references the not-yet-declared interfaces).
      Then implement `internal/ports/provider.go`: `LLMRequest{Prompt, Task string}`,
      `LLMResponse{Text, Model string}`, `LLMProvider.Complete`; `EmbedRequest{Text string}`,
      `EmbedResponse{Vector []float32, Model string}`, `EmbeddingProvider.Embed` — design D7's
      exact shapes (`Text` raw, never parsed; `Model` is what actually answered; `[]float32`,
      matching ADR-0012's own memory arithmetic; no `Dim` field — `len(Vector)`).
      Verify: `go build ./...`; `golangci-lint run`.
      Requirement: R5.1; design D7.
- [ ] **5.2** In the same commit as 5.1: implement `test/support/fakeprovider/fakeprovider.go` —
      the scripted-replay fake itself, loading recordings via `goldenset.Load` from
      `testdata/llm/cases/`, recording every prompt it saw so a test can assert on it without ever
      using it as the lookup key.
      Verify: `go test -race ./test/support/fakeprovider/...`.
      Requirement: R5.2; design D7.
- [ ] **5.3** Add the first real case file(s) under `testdata/llm/cases/` (currently only
      `.gitkeep`, confirmed): at least one `response`-only case and at least one `error`-only case,
      decodable by `goldenset.LLMExample` under `DecodeStrict` (per `testdata/llm/format.md`'s
      shape and cross-field constraint — exactly one of `response`/`error`). This is the change
      that makes `test/conformance/golden_sets_test.go`'s existing `assertCasesDirIsEmpty`
      **fail** for the `llm` directory — confirmed by reading it: it `t.Errorf`s on the first
      non-`.gitkeep` entry, untagged, inside `make check`'s fast loop. That failure **is** this
      task's red for task 5.4, not an invented one — proposal §4.6 names this exact mechanism.
      Verify: `go test ./test/support/goldenset/...` (existing `TestHarness_GoldenSetFormatMatchesType`
      continues to pass, per spec's own "Verified by"); observe `make check` go red on
      `TestHarness_GoldenSetFormatsDeclared`'s `llm` subtest before task 5.4 lands.
      Requirement: R5.3.
- [ ] **5.4** Fix the red 5.3 created: restructure `test/conformance/golden_sets_test.go`'s
      per-directory empty-corpus assertion so that, for `llm` specifically, it asserts `cases/`
      holds at least one entry beyond `.gitkeep` — an inversion, matching design D10's existing
      non-empty-corpus guard pattern elsewhere in the same file. `recall` and `classify` keep
      today's "must be empty" assertion unchanged (spec R5.4's MUST NOT — their own populating
      PRs are Phase B's).
      Verify: `make test` — the `llm` subtest passes because `cases/` is non-empty, the
      `recall`/`classify` subtests pass because theirs still hold only `.gitkeep`.
      Requirement: R5.4.
- [ ] **5.5** Test first: `test/support/fakeprovider/embed_test.go` —
      `TestFakeEmbeddingProvider_DeterministicByModel`: the same input text against the same
      configured model name returns the same vector; two fakes constructed with different model
      names report different `Model` values in `EmbedResponse` (design D7 — this is the Phase B
      seam I21 needs, a vault holding two models' embeddings). **Red**:
      `undefined: fakeprovider.NewEmbeddingFake` (or the fake's embedding constructor, named at
      implementation time).
      Verify: `go test -race ./test/support/fakeprovider/...`.
      Requirement: R6.2 (fake-side precondition), design D7.
- [ ] Verify (PR-level): `make check-all`; confirm no test added by this PR opens a real network
      connection or `httptest.Server` — `internal/ports` and `test/support/fakeprovider` both stay
      free of `net/http` client usage (R5.2's MUST NOT, R8.4).

---

## PR 6 — `feat/providers-http` — **SPLIT into 6a and 6b by owner decision (2026-07-31)**

Depends on PR 1 (R1.1's doc-side `openai` entry must land first — design D7's load-bearing
ordering) and PR 5 (the fake and `ports.LLMProvider`/`EmbeddingProvider` must exist).

**The split, and where the line falls.** ~420 lines was already over the ceiling on the
un-adjusted estimate, before M0's measured 1.3×–2.2× multiplier — which puts the real figure at
550–900. Design drew no cut line here the way D3's §7 drew one for PR 2, and this document
deliberately did not invent one. Owner decision under `delivery_strategy: ask-on-risk`:

| PR | Tasks | Content | Est. ceiling |
|---|---|---|---|
| **6a** | 6.1, 6.3 | The three vendor `LLMProvider` clients, and `"openai"` added to `DocumentedProviderTypes` | ~230 |
| **6b** | 6.2, 6.4, 6.5 | The `ollama` `EmbeddingProvider`, the task→provider reference check, and the absence confirmation | ~190 |

The cut follows design D7's own load-bearing ordering rather than splitting on file count.
**6.3 stays with 6.1 and that pairing is the point**: adding `"openai"` to
`DocumentedProviderTypes` is what makes config validation *accept* `type: openai`, and the
client is what makes that acceptance mean something. Landing the entry without the client would
ship a config the validator welcomes and the binary cannot serve — this project's own defect
family, where a component announces a capability it does not have.

6a's dependency on PR 1 is unchanged. Both halves are independently reviewable, and 6b depends
on 6a only through the chain, not through a symbol.

- [ ] **6.1** Test first, per vendor: `internal/providers/{anthropic,openai,ollama}/client_test.go`
      — each using `httptest.Server` (an in-process loopback listener, not "the network" in
      `docs/06-harness.md` §3's sense, per design §6's own note) to assert request shaping
      (headers, body) and response parsing into `ports.LLMResponse{Text, Model}` — raw text,
      never a parsed classification (design D7 — I14's degradation rule stays a pure function in
      `core/classify`, Phase B's). **Red**: `undefined: anthropic.NewClient` (and per-vendor
      equivalents).
      Implement `internal/providers/{anthropic,openai,ollama}/client.go`: each type implements
      `ports.LLMProvider` over real HTTP to its vendor's API.
      Verify: `go test -race ./internal/providers/...` (httptest only — no real socket).
      Requirement: R6.1.
- [ ] **6.2** Test first: `internal/providers/ollama/embed_test.go` — request/response shaping for
      `Embed` against `httptest`, plus `var _ ports.EmbeddingProvider = (*ollama.Client)(nil)`.
      **The `ollama` client is the one that implements `EmbeddingProvider` in Phase A** — per C4's
      resolution above: doc 01's `tasks.embedding` is bound to `local_llama` (task 1.2), whose
      `type` is `ollama`. **Red**: `undefined: ollama.Client.Embed` (or the compile-time assertion
      failing).
      Verify: `go test -race ./internal/providers/ollama/...`.
      Requirement: R6.2; C4's resolution of design D7's package-diagram gap.
- [ ] **6.3** Test first: extend `internal/config`'s existing `TestValidate` round-trip with a
      `providers.x.type: openai` case. **Red** (a real, pre-existing failure, not invented): before
      this task, `checkProviders` rejects it — `DocumentedProviderTypes` is confirmed
      `["anthropic", "ollama", "whisper_cpp"]` (`validate.go:168`) — so the new case fails with
      "type is \"openai\", which is not one of the documented types: anthropic, ollama,
      whisper_cpp".
      Add `"openai"` to `internal/config.DocumentedProviderTypes` — completing R1.1's doc-side half
      from PR 1, in the same PR as the client that makes the claim true (design D7's load-bearing
      ordering — reversed, `DocumentedProviderTypes`'s own comment claiming it mirrors doc 01 would
      be false for the interval between merges).
      Verify: `go test ./internal/config/...`.
      Requirement: R6.3.
- [ ] **6.4** Test first: `TestValidate_TaskProviderMustExist` — `tasks: {capture_processing:
      {provider: nonexistent_llm}}` with a `providers:` map that does not contain
      `nonexistent_llm` fails validation, naming both the task and the missing provider; the same
      task with its provider present in `providers:` passes. **Red** (pre-existing, not invented):
      `checkTasks` today validates only the task name (`validate.go:155-163`, confirmed by
      reading) — a config naming a nonexistent provider currently validates cleanly; the new test
      case fails with "expected an error, got nil".
      Implement a new named check, `{"task-provider", checkTaskProviders}`, added to the `checks`
      slice per D10's checks-as-data pattern (`validate.go`'s existing shape). Note for whoever
      implements this: `TaskBinding` (confirmed in `internal/config/config.go`) carries no
      `Enabled` field the way `channels.telegram` does — every entry present in `c.Tasks` is
      "enabled" by presence, so R6.4's "for a task whose consuming component is enabled" gates on
      nothing beyond map membership; there is no separate flag to check, unlike
      `checkTelegram`. Doc 01's own example (`tasks.embedding: { provider: local_llama }`, fixed
      by task 1.2) must continue to validate cleanly — R6.4's MUST NOT is satisfied by 1.2 already
      having removed the ellipsis placeholder, not by weakening this check.
      Verify: `go test ./internal/config/...`; confirm doc 01's own example still validates
      (existing `TestValidate` case, or the config↔doc round-trip test, green).
      Requirement: R6.4.
- [ ] **6.5** Confirm by absence: no `internal/brain/**` file exists anywhere in this chain
      (R6.4's second MUST NOT — task→provider resolution at capture time is `cmd/nooma` wiring
      work for Phase B, not this PR's). Review check, not new code.
      Verify: `git diff --name-only` for the full `m1a-substrate` chain contains no
      `internal/brain/` path.
      Requirement: R6.4 (second MUST NOT), R9.1.
- [ ] Verify (PR-level): `make check-all`; confirm `git grep` over this PR's own diff finds no test
      opening a connection outside `httptest` (R6.1's MUST NOT, R8.4).

---

## Handoff to Phase A → Phase B

At the end of this chain, `test/conformance/pending_symbols.txt` holds exactly two lines —
`recall.VectorQuery`, `recall.VectorIndex` (R7.3) — and `scripts/pending-red.sh` still passes,
reporting both `undefined:` under `-tags pendingimpl`. **No task in this chain touches
`i21_vector_search_filters_on_model_test.go`, retires the `pending-red` gate, or removes
`pending-red` from `check-all` or CI** — that is Phase B's `feat/core-recall`
(`m1b-pipeline`), per the umbrella proposal §4.7's own terminal rule: whichever PR removes the
*last* line from `pending_symbols.txt` retires the gate in that same PR.

Recorded here because design's own Risk #5 makes it explicit and it belongs at this chain's
boundary, not buried in Phase B's own tasks artifact: **retiring `pending-red` must also drop the
gate's required status context from the GitHub branch ruleset in the same movement.** A required
context whose job no longer exists never posts, and a context that never posts never becomes
satisfied — every future merge to `main` blocks forever if this step is skipped. This is the same
failure mode M0's design §7 records for un-registered matrix legs, running in the opposite
direction (a check remaining registered after its job disappears, rather than a check never being
registered at all).

---

## Review Workload Forecast

**Chained PRs recommended: yes — already chained, seven of them** (design's own PR 2 split makes
it seven, not the proposal's original six; C2 above).

**400-line budget risk:**

| PR | Ceiling stated above | Risk |
|---|---|---|
| 1 | ~40 | Low |
| 2a | ~180 | Low |
| 2b | ~220 | Low–medium |
| 3 | ~200 | Low |
| 4 | ~380 | **High** — close to the ceiling on its own stated estimate, before any underestimate multiplier |
| 5 | ~350 | Medium |
| 6a | ~230 | Low–medium — **split by owner decision**, see PR 6's section |
| 6b | ~190 | Low |

**Decision needed before apply: CLOSED.** Under `delivery_strategy: ask-on-risk` this artifact
raised PR 6 rather than inventing a cut line, and the owner decided on 2026-07-31: **split into
6a and 6b**, along design D7's own load-bearing ordering. The split, and why task 6.3 stays
paired with 6.1, are recorded in PR 6's section above.

**The eight remaining PRs are cleared for apply.** PR 4 (~380) stays the one to watch: it is the
only remaining entry close enough to the ceiling that the measured multiplier could push it over
without a pre-drawn line, and unlike PR 6 nobody has drawn one. Re-check it against its own
ceiling once its L1 tests are actually written, and split *before* the diff exists rather than
after.

**On the estimates themselves**, per this task's own governing instruction: the umbrella
proposal's own retrospective on M0 measured its estimates low by **1.3×–2.2×, six separate
times**. Every ceiling in this document is stated as the ceiling it was chosen to fit against the
400-line soft rule, never as a prediction. Applying that multiplier to Phase A's raw total (~1,790
lines across the seven PRs above) puts the realistic range at **roughly 2,300–3,900 review
lines**, which reads as: expect at least PR 4 and PR 6 to require a mid-apply split beyond what is
already flagged here, and expect PR 2b (the transition table's exhaustive 16-pair test alone is
sizable) to be worth re-checking against its own ceiling once the L1 tests are actually written.

---

## Traceability

| Spec section | Requirements | Tasks |
|---|---|---|
| §1 doc preflight | R1.1–R1.4 | 1.1–1.4 |
| §2 `core/unit` | R2.1–R2.4 | 2.1–2.3, 2.8–2.10 |
| §3 `ports.UnitRepo` + fake | R3.1–R3.3 | 3.1, 3.3–3.4 |
| §4 SQLite `UnitRepo` | R4.1–R4.4 | 4.1–4.4 |
| §5 provider port + fake | R5.1–R5.4 | 5.1–5.5 |
| §6 provider HTTP clients + routing | R6.1–R6.4 | 6.1–6.5 |
| §7 `pending-red` sequence | R7.1–R7.3 | 2.4, 3.2, and the Handoff section |
| §8 cross-cutting | R8.1–R8.4 | 2.1–2.3 (R8.1, via lint), 2.7 & 2.12 (R8.2), 2.5 & 2.11 (R8.3), 5's PR-level verify & 6's PR-level verify (R8.4) |
| §9 boundaries | R9.1–R9.3 | 6.5 (R9.1), 4.4 (R9.2), 2b's PR-level verify (R9.3) |
| §10 test levels | R10.1–R10.2 | every "Test first" line; the compile-error-as-red pattern named explicitly in 3.1 and 5.1 |
| §11 open items | (design's choices) | C1, C4, and design D1/D5/D7 as cited per task |
