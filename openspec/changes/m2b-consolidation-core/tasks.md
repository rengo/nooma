# Tasks — M2 Phase B: the consolidation core

Implementation task list for `m2b-consolidation-core`, derived from `spec.md` (25 requirements,
R{section}.{n}, sections nominally 1:1 with the five PRs) and `design.md` (§1–§11, design ran
first this time). Both artifacts are treated as authoritative; two places where the spec's own
section numbering does not match design's PR/file assignment are recorded below as findings
(F1, F2) rather than silently resolved — sequencing is decided in favour of design's dependency
order, since a PR cannot compile a test against a constant a later PR declares.

Chain strategy **`stacked-to-main`**, delivery strategy **`auto-chain`** — matching `m1a`, `m1b`,
`m1c` and `m2a`'s own precedent; not re-asked here because no override was given. Five PRs, in a
strict stack (each depends on the previous): `feat/core-consolidation-order` →
`-expire-archive` → `-strengthen-reweight` → `-connect-derive` → `-pattern-eval`.

**Strict TDD is active** (`CLAUDE.md` non-negotiable #4); `scripts/pending-red.sh` does not exist
in this repository (confirmed). Every behavioral task states the two-commit shape `m2a` D11
established: **commit 1** is the test plus a stub with the final signature returning zero values
(the suite compiles, the assertion fails — red for the right reason, guarded against C14's "a
zero-value stub must fail on a length/presence assertion before any content check"); **commit 2**
is the implementation (green). Where a test's own assertions cannot be red against a missing
symbol — a doc-parsed L2 pin, a cross-constant compatibility check, a tree-scan over code that
already compiles within the same PR — this document says so explicitly at the task, per this
project's own convention (`m2a` C9) against claiming a red step that cannot occur.

Every PR runs `make check-all` before opening, not `make check`. `docs-sync.yml` fires on
`internal/core/`, and every PR below carries its own genuine `docs/02-cognitive-core.md` delta —
no PR needs a `no-spec-change` label.

---

## Findings — spec/design disagreements (per the assignment's own instruction: report, don't paper over)

**F1 — `Transition`/`Reason` (R1.4) is written under spec's PR1 section, but design assigns
`transition.go` to PR 2 and R1.4's own verification text depends on `ExpireIncomplete` and
`Archive`, both PR 2 functions.** `design.md` §5.1's package-layout table is explicit:
`transition.go PR 2 Transition, Reason and its vocabulary`. PR 1's own code block (§3.1) declares
only `Phase`, `Order`, `String`, `ParsePhase` — no `Transition` producer exists in PR 1 at all.
Spec's R1.4 verification line reads: *"an exhaustiveness table driving every `Transition` any
producer emits (from `ExpireIncomplete`, §2, and `Archive`, §2) through `unit.ValidateTransition`"*
— citing PR 2's own functions by name, from inside a section spec itself titled "PR1". **Resolved
here in design's favour**: `Transition`/`Reason`'s declaration and its full exhaustiveness test are
tasked into PR 2 (tasks 2.1–2.2, 2.10), not PR 1. Spec's "R1" prefix is a numbering artifact, not
evidence PR 1 must ship it — a PR cannot dependency-satisfy a test against functions it never
declares.

**F2 — R3.2 (the `StrengthenGain`/`DefaultGoalStagnationDays` compatibility check) is written
under spec's PR3 section, but `DefaultGoalStagnationDays` is declared in `patterns.go`, PR 5, per
both `design.md` §5.1's layout table and §6.9's vocabulary declaration.** PR 3 (`strengthen.go`)
cannot compile a test referencing a constant that does not exist until two PRs later. This is a
harder version of F1: F1 could plausibly ship early if the type existed; here the constant
genuinely does not exist. **Resolved by deferring the check itself to PR 5** (task 5.7, "R3.2
close-out"), the identical pattern this repo already used in `m2a` (task 4b.7, "R3.8 close-out",
closing a PR3a requirement from PR4b). `StrengthenGain` itself still ships on schedule in PR 3
(tasks 3.1–3.2); only the two-constant check waits for its second operand to exist.

**Minor note, not a disagreement**: `design.md` §5.1 describes
`consolidation_defaults_ddl_test.go` as covering "the four `Default*` constants," but only three
are DDL-pinned per R2.5/R5.4 (`DefaultWeightThreshold`, `DefaultGoalStagnationDays`,
`DefaultMentalLoadThreshold`) — `BeliefMergeCosine`, the fourth `Default*`-shaped value, has no
schema `DEFAULT` to pin against and is asserted by R5.4 as a literal equality against doc 02 §13's
documented number instead. Task 5.8 states this explicitly so nobody miscounts DDL pins as four.

**No other disagreement found.** Every other identifier, signature, PR assignment, and formula in
spec traces cleanly to design's §6 declarations and §5.1 layout table.

---

## Handoffs to `m2c` (design.md §8, carried forward so they survive the archive)

Not `m2b`'s tasks — recorded here per the assignment's own instruction, so the archive keeps them
rather than losing them inside `design.md` alone:

- `ConfigRepo` over `config`: every knob a nil-sentinel pointer (`*float64`/`*int`/`*time.Time`);
  `m2b` supplies the meaning of `nil` in every `Resolve*`, `m2c` supplies the pointer. No new
  migration.
- `UnitRepo` weight write: takes a `weight.Boost` so a weight without a timestamp is inexpressible
  (I24). `Reweight` (PR 3) is the first producer of a `[]weight.Boost` with a real caller.
- `UnitRepo` live-count-by-type: returns an `int`, not a slice (owner ruling 6).
- `UnitRepo` reads for `archive`/`connect`: `[]Cold`/`[]Source`, decay fields only, never a whole
  `unit.Unit` (keeps I05's read-side property one layer up).
- `UnitRepo` read for `expire_incomplete`: the one deliberate non-live read in M2 — must be named
  `IncompleteOlderThan`, not a parameterized `List(status)`.
- `RelationRepo` read for `strengthen`: `[]RelationEvidence`, the relation pre-joined to **both**
  endpoints' `last_touched_at` — a join no port has today.
- `RelationRepo` read for `connect`: `map[Pair]bool` keyed by `CanonicalPair`, bounded by
  `ConnectSourceLimit × ConnectCandidateK`.
- `SelfModelRepo`: upsert by `topic_key`, plus `ActiveBeliefs()` — a name that carries "active",
  never a status parameter.
- `goal_stagnation_days`'s schema home (§9 Q3, unresolved): two candidate tables exist
  (`config.goal_stagnation_days` and `calibration`'s own example key); `m2c` must pick one and
  M5's learning module must write the same one.
- Belief embeddings: `[]BeliefVector`, computed in memory at the start of `derive` and discarded
  (ruling Q2 option A) — the nightly provider cost is written into doc 02 §6.5 by task 4.20, not
  left for `m2c` to discover.
- The two recall legs for `connect`: two ranked `[]string`, **vector leg first** — load-bearing for
  `recall.FuseScored`'s tie-break.
- `current_state` write: one append-only row per `LoadFinding` (doc 02 §10). No delivery — M3.
- `decision_log`: written from `brain` only, from the `Reason` codes `m2b` returns (I12).
- `archive`'s write concurrency: `SetStatus`'s `from` precondition; `ErrStatusConflict` is skipped
  and logged, never a pass failure (proposal R8).
- `brain/capture.go:485`: adopt `relation.CreatedBySystem` in place of the bare `"system"` literal
  — one line, `m2c`'s.
- I05's structural half: scoped to read paths, simpler than planned since PR 3 declines bulk
  materialization (§4.5(b)).

**`m2a` handoffs this change closes or inherits**: C17 (`spread.go`'s dead `refused` guard) is
deleted in task 3.5, the PR that makes `Resurface` reachable, per C17's own instruction. C18
(duplicate `UnitID` masking corruption) is closed at the type level by `Reweight` taking
`map[string]weight.Current` (task 3.4's stub, task 3.6's amendment) rather than a slice — no code
fix needed here, the shape makes the duplicate unrepresentable. C20/C21 (`corrupted` not scoped to
the origin's reachable component) are resolved by `Reweight`'s union-dedup merge (task 3.4/3.6),
not by changing `Resurface` itself — recorded as inherited-and-resolved-at-the-caller, the shape
C20/C21 asked for. C19 (`Edge.Strength = +Inf` coerced rather than refused inside `weight`) is
**not** inherited — `Reweight`'s own door refuses non-finite/out-of-range edge strengths before
`clampStrength` ever runs (task 3.3/3.4), so `m2b` does not repeat C19's asymmetry even though it
does not fix it inside `core/weight` either.

---

## PR 1 — `feat/core-consolidation-order` (~135 impl+docs)

Depends on nothing outside this change. Goes first. **Does not** ship `Transition`/`Reason` (F1)
or any doc02-vocabulary function beyond `Phase` — no `internal/core` import beyond `errors`.

- [x] **1.1** Commit 1 (RED): `internal/core/consolidation/phase_test.go` — `Order()` has exactly 8
      elements, strictly ascending from `Phase(0)`, `Order()[7] == PhaseLearn`; `String()` swept
      across a range including negative and above-range ints never panics and is total; `ParsePhase
      ∘ String` round-trips for every `s` in `Order()`; `ParsePhase` rejects unknown text with
      `ErrUnknownPhase`.
      **Red**: `undefined: consolidation.Order`, `undefined: consolidation.ParsePhase`,
      `undefined: consolidation.ErrUnknownPhase`.
      Stub (in the same commit): `type Phase int` with the 8 constants and `phaseCount`, **the
      `const _ uint = uint(int(PhaseLearn) - int(phaseCount) + 1)` assertion** (already true, given
      the constants' declared order — this line does not wait for GREEN), `var ErrUnknownPhase =
      errors.New(...)`, `func Order() []Phase { return nil }`, `func (p Phase) String() string {
      return "" }`, `func ParsePhase(s string) (Phase, error) { return 0, ErrUnknownPhase }` —
      compiles; `len(Order()) != 8` fails first (C14 guard), before any name assertion runs.
      Requirement: R1.1.
- [x] **1.2** Commit 2 (GREEN): implement `phaseNames()` (array sized by `phaseCount`), `Order()`
      (generated from `[0, phaseCount)`, never a slice literal), `String()`, `ParsePhase()`.
      Verify: `make test`; `golangci-lint run`.
      Requirement: R1.1; design §3.1.
- [x] **1.3** Structural, not a test: confirm the `const _ uint` assertion from task 1.1 reads
      exactly as design §3.1 states, and record in `phase.go`'s own comment what it proves and what
      it does not. **Proves**: `PhaseLearn` at any position other than `phaseCount - 1` is a compile
      error — not a failing test, a package that does not build. **Does not prove**: that there are
      exactly eight phases (that is leg 3, task 1.4's doc-parse half) or that a runner executes
      `Order()`'s sequence (I11's behavioural half, `m2c`'s). Verify: `go build ./...` succeeds; as
      a one-time author check (not part of CI — "the test is not tested," design §7), temporarily
      move `PhaseLearn` earlier in the const block and confirm `go build` fails.
      Requirement: R1.1 (leg 1); design §3.2.
- [x] **1.4** `test/conformance/i11_consolidation_phase_order_test.go` (new) — two assertions in one
      file, per spec R1.2's own grouping. **Leg 3**: a new helper (reusing `repoRootFromCaller` from
      `test/conformance/store_api_test.go`, the same helper `migrationSQLText` already reuses)
      reads `docs/02-cognitive-core.md` off disk, extracts the phase arrow line (the line containing
      `→` under §6's "phases IN ORDER" preamble, currently line 661), and asserts
      `strings.Join(names, " → ")` — built from `Order()`'s `String()` — equals it exactly; also
      asserts every entry in `phaseNames()` is non-empty (totality). **Leg 4**: a tree scan over
      every non-test `.go` file outside `internal/core/consolidation`, asserting no file contains
      two or more of the eight phase-name string literals.
      **Not a missing-symbol red**: `Order`/`String`/`phaseNames` already compile and pass their own
      tests earlier in this same PR (task 1.2) — disclosed per this project's own convention (`m2a`
      C9) rather than claimed as a TDD red step. This is the first application of the
      migration-text-parsing trick to doc 02 prose rather than to SQL DDL.
      Requirement: R1.2; design §3.2 legs 3–4.
- [x] **1.5** R1.3 — `PhaseLearn` occupies slot eight with no decision function. **No test is
      possible for an absent function** (spec's own words: "there is no positive test for an absent
      function, only the absence itself"). Verify at PR review: `rg 'func Learn' internal/core`
      returns nothing; `core_exported_decls_have_tests_test.go`'s existing presence guard is
      unaffected because there is no new exported declaration to guard.
      Requirement: R1.3.
- [x] **1.6** Rewrite `internal/core/consolidation/doc.go`: name the eight phases in order, state
      that `learn` has no function and why (the no-op is the absence, not a vacuous body), and
      cross-reference §13's incoming rows (this PR adds none — the first calibrated number arrives
      in PR 2).
      Verify: `golangci-lint run`; `go build ./...`.
      Requirement: design §3.3.
- [x] **1.7** doc 02 §6 amendment: no change needed to the arrow line itself (task 1.4 pins the
      existing text); add §6.8's one-sentence amendment — `learn`'s slot performs no work and writes
      no `decision_log` row, M5 fills it.
      Verify: read the section; `docs-sync.yml` not locally verifiable.
      Requirement: design §3.3.
- [x] **1.8** Purity/lint: `golangci-lint run` (`depguard`'s `core-purity` — this PR imports only
      `errors` beyond stdlib; `forbidigo` — no `time.Now`/`Since`/`Until`/`rand.*`/`uuid.*`/
      `os.Getenv`).
      Requirement: `nooma-core` hard rules 1–2.
- [x] Verify (PR-level): `make check-all`; confirm `git diff --name-only` touches only
      `internal/core/consolidation/{doc,phase}{,_test}.go`,
      `test/conformance/i11_consolidation_phase_order_test.go`, `docs/02-cognitive-core.md`.
      `docs/06-harness.md` is **not** touched — I11 and I12 already have their §4 rows.

---

## PR 2 — `feat/core-consolidation-expire-archive` (~235 impl+docs)

Depends on PR 1 (`Order`/`Phase` unused directly, but the package now grows its first real
producers). Ships `transition.go` per F1's resolution, `expire.go`, `archive.go`.

- [x] **2.1** Commit 1 (RED): `internal/core/consolidation/transition_test.go` — `AllReasons()`
      returns exactly `{ReasonIncompletePromoted, ReasonIncompleteExpired,
      ReasonBelowWeightThreshold}`, a fresh slice (mutating the returned slice does not affect a
      second call).
      **Red**: `undefined: consolidation.Reason`, the three `Reason` constants, `undefined:
      consolidation.AllReasons`, `undefined: consolidation.Transition`.
      Stub: `type Reason string`; the three consts; `type Transition struct{ UnitID string; From,
      To unit.Status; Reason Reason }`; `func AllReasons() []Reason { return nil }` — compiles;
      `len(AllReasons()) != 3` fails first.
      Requirement: R1.4 (see Finding F1 — declared and tested here, not in PR 1, despite the
      spec's own "R1" prefix).
- [x] **2.2** Commit 2 (GREEN): implement `AllReasons()` as a fresh 3-element slice, `unit.
      AllStatuses()`'s own house pattern.
      Verify: `make test`; `golangci-lint run`.
      Requirement: R1.4; design §6.2.
- [x] **2.3** Commit 1 (RED): `internal/core/consolidation/expire_test.go` — `elapsed < 24h` → nil;
      `elapsed == 24h` → a transition (both `Unresolved` branches: `true` → `incomplete →
      archived`/`ReasonIncompleteExpired`, `false` → `incomplete → pool`/`ReasonIncompletePromoted`);
      `elapsed > 24h` same both branches; `CreatedAt` after `now` (clock skew) → nil; output sorted
      by `UnitID` (≥3 units, mutation-guard by removing the sort).
      **Red**: `undefined: consolidation.IncompleteExpiryHours`, `undefined:
      consolidation.Incomplete`, `undefined: consolidation.ExpireIncomplete`.
      Stub: `const IncompleteExpiryHours = 24`; `type Incomplete struct{ UnitID string; CreatedAt
      time.Time; Unresolved bool }`; `func ExpireIncomplete(us []Incomplete, now time.Time)
      []Transition { return nil }` — compiles; `len(got) != 1` on the ≥24h fixture fails first
      (C14 guard).
      Requirement: R2.1.
- [x] **2.4** Commit 2 (GREEN): implement `ExpireIncomplete` per §4.2's predicate — `elapsed`
      clamped at zero, branch on `Unresolved` only once `elapsed >= IncompleteExpiryHours`.
      Verify: `make test`; `golangci-lint run` (now imports `internal/core/unit`).
      Requirement: R2.1; design §4.2.
- [x] **2.5** Commit 1 (RED): `internal/core/consolidation/archive_test.go` — `e < threshold`
      archives (`pool → archived`/`ReasonBelowWeightThreshold`); `e == threshold` does **not**
      (both sides); a non-`pool` status produces neither output; `NaN`/`+Inf`/`-Inf` in `Weight` or
      `DecayRate` refuses into `corrupted`, never archives; both slices sorted (≥3 units).
      **Red**: `undefined: consolidation.Cold`, `undefined: consolidation.Archive`.
      Stub: `type Cold struct{ UnitID string; Status unit.Status; Weight, DecayRate float64;
      LastTouchedAt time.Time }`; `func Archive(cs []Cold, threshold float64, now time.Time)
      (transitions []Transition, corrupted []string) { return nil, nil }` — compiles; `len(got) !=
      1` on the below-threshold fixture fails first.
      Requirement: R2.2.
- [x] **2.6** Commit 2 (GREEN): implement `Archive` — `weight.Effective(...) < threshold`, with
      entry-point `NaN`/`Inf` refusal on `Weight`/`DecayRate` before the comparison ever runs
      (C15's rule).
      Verify: `make test`; `golangci-lint run` (now imports `internal/core/weight`).
      Requirement: R2.2; design §4.4/§6.4.
- [x] **2.7** Commit 1 (RED): `archive_test.go` (continued) — `ResolveWeightThreshold`: `nil` →
      `0.5`; a finite in-range value passes through; `NaN`/`+Inf`/`-Inf`/negative/`>
      weight.WeightCeiling` all → default.
      **Red**: `undefined: consolidation.DefaultWeightThreshold`, `undefined: consolidation.
      ResolveWeightThreshold`.
      Stub: `const DefaultWeightThreshold = 0.5`; `func ResolveWeightThreshold(configured *float64)
      float64 { return 0 }` — compiles; the `nil` case expects `0.5`, stub returns `0`, fails first.
      Requirement: R2.3.
- [x] **2.8** Commit 2 (GREEN): implement `ResolveWeightThreshold`'s nil/non-finite/out-of-range
      fallback.
      Verify: `make test`; `golangci-lint run`.
      Requirement: R2.3; design §6.4.
- [x] **2.9** `archive_test.go` (continued) — the `m2a`-composition check:
      `weight.ReviveGain × weight.WeightCeiling > DefaultWeightThreshold` (0.70 > 0.5) and
      `weight.ResurfaceAttenuation^weight.ResurfaceMaxHops × weight.WeightCeiling <=
      DefaultWeightThreshold` (0.5 ≤ 0.5), computed from the named Go constants, never repeated
      literals; the test's own doc comment states the ⚙ caveat (holds at defaults, not generally).
      **Not a missing-symbol red**: all four constants already exist (three from `m2a`, one from
      task 2.7) — disclosed per `m2a` C9 as a compatibility check, not a TDD red step.
      Requirement: R2.4.
- [x] **2.10** `transition_test.go` (continued) — the R1.4 exhaustiveness table: drive every
      `Transition` `ExpireIncomplete` and `Archive` can emit through `unit.ValidateTransition`,
      asserting no error, instead of re-asserting the legal `(From, To)` pairs by hand.
      **Not a missing-symbol red**: both producers already compile and pass their own tests earlier
      in this PR — disclosed per `m2a` C9.
      Requirement: R1.4 (see Finding F1 for why this lives in PR 2).
- [x] **2.11** `test/conformance/consolidation_defaults_ddl_test.go` (new) — pin
      `DefaultWeightThreshold` to migration `0002_learning_and_search.sql:63`'s `config.
      weight_threshold ... DEFAULT 0.5`, read off disk via the existing `migrationSQLText` helper.
      Requirement: R2.5.
- [x] **2.12** doc 02 §6.1 amendment: state both outcomes — promotion by default, archival only
      when the caller marks the ambiguity `Unresolved` — resolving the §1/§6.1 contradiction
      (`CLAUDE.md` non-negotiable #1); cross-reference §1's own two-outcome text.
      Verify: read the section; `docs-sync.yml` not locally verifiable.
      Requirement: design §4.2.
- [x] **2.13** doc 02 §6.2 amendment: state the strict `<` operator explicitly and its composition
      with `m2a`'s revive/resurface guarantees (the identity task 2.9 proves), so the doc states
      what the code and test now enforce together.
      Requirement: design §6.4.
- [x] **2.14** §13: add `incomplete_expiry_hours` (24, `consolidation.IncompleteExpiryHours`,
      quoted from doc 02 §1) as a new row; annotate the existing `weight_threshold` row with
      `consolidation.DefaultWeightThreshold` + `ResolveWeightThreshold`. Two rows touched this PR
      (1 new, 1 annotated).
      Requirement: R0.2.
- [x] **2.15** Purity/coverage: `golangci-lint run`; `make cover`.
- [x] Verify (PR-level): `make check-all`; confirm diff touches only
      `internal/core/consolidation/{transition,expire,archive}{,_test}.go`,
      `test/conformance/consolidation_defaults_ddl_test.go`, `docs/02-cognitive-core.md`.
      `docs/06-harness.md` untouched (no new invariant row). Target ≤235 impl+docs lines.

---

## PR 3 — `feat/core-consolidation-strengthen-reweight` (~230 impl+docs)

Depends on PR 2 (`Transition`/`unit.ValidateTransition` unused here directly, but PR 3 is the
first to call `weight.Resurface`). Ships `strengthen.go`, `reweight.go`, and C17's deletion in
`internal/core/weight/spread.go`. **Does not** ship R3.2's compatibility check — see Finding F2;
that task is 5.7.

- [ ] **3.1** Commit 1 (RED): `internal/core/consolidation/strengthen_test.go` — `since == nil` →
      empty for any input; one endpoint's `LastTouchedAt` before `*since` → nothing; both endpoints
      at exactly `*since` → a change (`Before` is strict, so equality qualifies); asymptotic and
      never reaches 1 under repetition; already at strength 1 → no row; refuses `NaN`, `+Inf`,
      `-Inf`, `-0.5`, `1.5` into `corrupted` (five shapes, individually), never computing a change
      for them; output sorted by `RelationID` (≥3 relations, mutation-guard the sort).
      **Red**: `undefined: consolidation.StrengthenGain`, `undefined: consolidation.
      RelationEvidence`, `undefined: consolidation.StrengthChange`, `undefined: consolidation.
      Strengthen`.
      Stub: `const StrengthenGain = 0.10`; `type RelationEvidence struct{ RelationID string;
      Strength float64; FromLastTouchedAt, ToLastTouchedAt time.Time }`; `type StrengthChange
      struct{ RelationID string; Strength float64 }`; `func Strengthen(es []RelationEvidence, since
      *time.Time) (changes []StrengthChange, corrupted []string) { return nil, nil }` — compiles;
      the qualifying-co-use fixture expects `len(changes) == 1`, stub nil fails first.
      Requirement: R3.1.
- [ ] **3.2** Commit 2 (GREEN): implement `Strengthen` — `since == nil` short-circuits to empty;
      co-active gate `!from.Before(*since) && !to.Before(*since)`; entry-point refusal of
      non-finite/out-of-`[0,1]` strength **before** the `== 1` check or the formula runs; the
      asymptotic law `s + StrengthenGain*(1-s)`; no row at exactly 1; never lowers.
      Verify: `make test`; `golangci-lint run`.
      Requirement: R3.1; design §4.3.
- [ ] **3.3** Commit 1 (RED): `internal/core/consolidation/reweight_test.go` — both endpoints of a
      new edge are boosted; multi-origin results merge by max; a corrupt edge strength (non-finite
      or outside `[0,1]`) is refused at `Reweight`'s own door, both endpoints reported into
      `corrupted`, before `weight.clampStrength` or any comparison downstream can run; `corrupted`
      is merged by union, deduplicated, across all origin calls; a unit id may appear in both
      `boosts` and `corrupted` from the same call, neither suppressing the other; `states` sorted by
      `UnitID` before building `Neighbourhood`, deterministic regardless of map order
      (mutation-verified by removing the sort with ≥3 units — `m2a` C16's own method); `rg` check
      that no constant beyond `weight.ReviveGain`/`WeightCeiling`/`ResurfaceMaxHops`/
      `ResurfaceAttenuation` is referenced.
      **Red**: `undefined: consolidation.Reweight`.
      Stub: `func Reweight(states map[string]weight.Current, newEdges []weight.Edge, now
      time.Time) (boosts []weight.Boost, corrupted []string) { return nil, nil }` — compiles;
      the two-endpoints-boosted fixture expects `len(boosts) == 2`, stub nil fails first.
      Requirement: R3.3.
- [ ] **3.4** Commit 2 (GREEN): implement `Reweight` — origins are every endpoint of `newEdges`;
      refuse non-finite/out-of-range edge strengths at `Reweight`'s own entry point (reporting both
      endpoints into `corrupted` directly, not relying on `weight.clampStrength`); build
      `Neighbourhood.States` from `states` sorted by `UnitID`; call `weight.Resurface` once per
      origin over the (validated) `newEdges`; merge `boosts` per unit by max; merge `corrupted` by
      union, deduplicated, sorted.
      Verify: `make test`; `golangci-lint run`.
      Requirement: R3.3; design §4.5(a).
- [ ] **3.5** `internal/core/weight/spread.go`: delete C17's dead `refused` guard — remove
      `refused := make(map[string]bool)`, `refused[unitID] = true`, and `|| refused[unitID]` from
      the corrupt-edge sweep's skip condition, leaving `if unitID == n.Origin { continue }`. This
      PR is the one that makes `Resurface` reachable through a real caller (`Reweight`), so C17's
      deletion travels here per `design.md` §10.
      Verify: `go test ./internal/core/weight/... -race` stays green (C17's own closing criterion —
      no fixture in `resurface_test.go` was pinning the dead branch).
      Requirement: design §4.5(a); closes `m2a` C17.
- [ ] **3.6** `test/conformance/consolidation_purity_test.go` (new, scaffold) — AST tree-scan
      asserting `Strengthen` has no `time.Time` parameter among `internal/core/consolidation`'s
      exported functions (R0.1's first of three; `MergeProposals`/`Reinforce` don't exist yet — PR 4
      extends this file, mirroring `m2a`'s own i05 scaffold-then-extend precedent, task 1.5 there).
      Requirement: R0.1 (partial).
- [ ] **3.7** doc 02 §6.3 amendment: state the co-use evidence definition (both endpoints'
      `last_touched_at` at or after `since`), the reinforcement formula, and the "strength never
      falls" sentence (rejection deletes the relation; decay is never consulted here).
      Verify: read the section; `docs-sync.yml` not locally verifiable.
      Requirement: design §4.3.
- [ ] **3.8** doc 02 §6.6 amendment: the literal replacement text — *"post-connection weight
      adjustments (decay materialization remains optional and is not exercised by M2's
      `reweight`)."* — plus a §2 cross-reference restating `last_touched_at` as "the vault's record
      of direct use."
      Requirement: R3.3 (the MUST NOT `Materialize` clause); design §4.5(b).
- [ ] **3.9** §13: add `strengthen_gain` (0.10, `consolidation.StrengthenGain`, chosen — its
      compatibility check against `DefaultGoalStagnationDays` closes in PR 5, task 5.7) as a new
      row.
      Requirement: R0.2.
- [ ] **3.10** Purity/coverage: `golangci-lint run`; `make cover`.
- [ ] Verify (PR-level): `make check-all`; confirm diff touches only
      `internal/core/consolidation/{strengthen,reweight}{,_test}.go`,
      `internal/core/weight/spread.go` (C17's 3-line deletion), `test/conformance/
      consolidation_purity_test.go`, `docs/02-cognitive-core.md`. Target ≤230 impl+docs lines. **If
      it runs long**, split at `strengthen.go` | `reweight.go` per design's pre-drawn line — C17's
      deletion travels with `reweight.go`'s half, since that is what makes `Resurface` reachable.

---

## PR 4 — `feat/core-consolidation-connect-derive` (~350 impl+docs, closest to the ceiling)

Depends on PR 3. Ships `connect.go`, `relation/createdby.go` (the **connect half**, ~190) and
`derive.go`, `selfmodel/facet.go` (the **derive half**, ~165) — **the pre-drawn split line from
`design.md` §10, stated here explicitly** so a mid-PR overrun does not become an improvised
decision. Both halves are tasked below in that order; **the boundary is the line between task
4.13 and task 4.14**.

### Connect half (~190) — `connect.go`, `relation/createdby.go`

- [ ] **4.1** Commit 1 (RED): `internal/core/relation/createdby_test.go` — `AllCreatedBy()` returns
      a fresh 3-slice `{CreatedBySystem, CreatedByConsolidation, CreatedByUser}`; `ParseCreatedBy`
      round-trips each member's `string()` value; unknown text → `ErrUnknownCreatedBy`.
      **Red**: `undefined: relation.CreatedBy`, the three constants, `undefined: relation.
      AllCreatedBy`, `undefined: relation.ParseCreatedBy`, `undefined: relation.
      ErrUnknownCreatedBy`.
      Stub: `type CreatedBy string`; the three consts; `var ErrUnknownCreatedBy = errors.New(...)`;
      `func AllCreatedBy() []CreatedBy { return nil }`; `func ParseCreatedBy(s string) (CreatedBy,
      error) { return "", ErrUnknownCreatedBy }` — compiles; `len(AllCreatedBy()) != 3` fails first.
      Requirement: R4.7 (relation half).
- [ ] **4.2** Commit 2 (GREEN): implement `AllCreatedBy`/`ParseCreatedBy`, `unit.AllStatuses()`'s
      house pattern.
      Verify: `make test`; `golangci-lint run`.
      Requirement: R4.7; design §5.3.
- [ ] **4.3** `test/conformance/` (extend an existing DDL-pin file or add one) — pin
      `relation.AllCreatedBy()`'s three values against migration `0001_core_tables.sql:37`'s
      `created_by` column comment vocabulary (`system|consolidation|user`), read off disk.
      Requirement: R4.7 (L2 half).
- [ ] **4.4** Commit 1 (RED): `internal/core/consolidation/connect_test.go` —
      `SelectConnectSources`: `since == nil` takes the whole live pool; non-live sources excluded;
      a source touched before `*since` excluded; ordered by `weight.Effective` descending, tie by
      `UnitID`; capped at `ConnectSourceLimit`; determinism under `-shuffle=on` (≥3 sources).
      **Red**: `undefined: consolidation.ConnectSourceLimit`, `undefined: consolidation.
      ConnectCandidateK`, `undefined: consolidation.Source`, `undefined: consolidation.
      SelectConnectSources`.
      Stub: `const (ConnectSourceLimit = 20; ConnectCandidateK = 5)`; `type Source struct{ UnitID
      string; Status unit.Status; Weight, DecayRate float64; LastTouchedAt time.Time }`; `func
      SelectConnectSources(ss []Source, since *time.Time, now time.Time) []string { return nil }` —
      compiles; a live-and-eligible fixture expects `len(got) == 1`, stub nil fails first.
      Requirement: R4.1.
- [ ] **4.5** Commit 2 (GREEN): implement `SelectConnectSources`.
      Verify: `make test`; `golangci-lint run` (now imports `internal/core/recall` transitively via
      the package, though not this function directly).
      Requirement: R4.1; design §4.4.
- [ ] **4.6** Commit 1 (RED): `connect_test.go` (continued) — `ConnectPairs`: the source is never
      its own candidate; `existing` excludes a candidate regardless of which direction it was
      stored (both `a→b` and `b→a` forms tested against the same `CanonicalPair` key); capped at
      `ConnectCandidateK`; `fused`'s order is preserved; `CanonicalPair` is symmetric and
      lexicographically ordered, and is asserted to be used **only** for the `existing` lookup — the
      returned `Pair` is always `{From: source, To: candidate}`, never the canonical form.
      **Red**: `undefined: consolidation.Pair`, `undefined: consolidation.CanonicalPair`,
      `undefined: consolidation.ConnectPairs`.
      Stub: `type Pair struct{ From, To string }`; `func CanonicalPair(a, b string) Pair { return
      Pair{} }`; `func ConnectPairs(source string, fused []recall.FusedCandidate, existing
      map[Pair]bool) []Pair { return nil }` — compiles; an unexcluded-candidate fixture expects
      `len(got) == 1`, stub nil fails first.
      Requirement: R4.2.
- [ ] **4.7** Commit 2 (GREEN): implement `CanonicalPair` (lexicographic order) and `ConnectPairs`
      (self-exclusion, `existing` lookup via `CanonicalPair`, storage direction `source →
      candidate`, cap at `ConnectCandidateK`).
      Verify: `make test`; `golangci-lint run`.
      Requirement: R4.2; design §4.4.
- [ ] **4.8** Commit 1 (RED): `connect_test.go` (continued) — `ProposeRelation`: outcome `new` →
      `false`; `relation.Discard` → `false` (I08); each of the four missing pointer fields
      (`TargetUnitID`/`Type`/`Strength`/`Confidence` nil) → `false`, tested individually;
      `Uncertain` and `Asserted` with all four fields present → `true`; the returned
      `ProposedRelation.CreatedBy` is always `relation.CreatedByConsolidation`.
      **Red**: `undefined: consolidation.ProposedRelation`, `undefined: consolidation.
      ProposeRelation`.
      Stub: `type ProposedRelation struct{ From, To, Type string; Strength, Confidence float64;
      CreatedBy relation.CreatedBy }`; `func ProposeRelation(from string, j relation.Judgment, t
      relation.Thresholds) (ProposedRelation, bool) { return ProposedRelation{}, false }` —
      compiles; the `Uncertain`-with-all-fields fixture expects `true`, stub always `false`, fails
      first.
      Requirement: R4.3.
- [ ] **4.9** Commit 2 (GREEN): implement `ProposeRelation` via `relation.Decide`/`relation.Resolve`
      unchanged, tolerant-decode nil-field checks, `CreatedBy` always `CreatedByConsolidation`.
      Verify: `make test`; `golangci-lint run` (now imports `internal/core/relation` directly for
      `Judgment`/`Thresholds`/`Decide`).
      Requirement: R4.3; design §4.4.
- [ ] **4.10** doc 02 §6.4 amendment: state the per-night budget as **one product**
      (`ConnectSourceLimit × ConnectCandidateK = 100` judge calls), not two separate knobs; record
      the unordered-pair exclusion choice (§9 Q5) as a stated, reversible decision.
      Verify: read the section; `docs-sync.yml` not locally verifiable.
      Requirement: design §4.4.
- [ ] **4.11** §13: add `connect_source_limit` (20, chosen) and `connect_candidate_k` (5, chosen —
      a separate row from `dedup_candidate_k` despite the identical default) as two new rows.
      Requirement: R0.2.
- [ ] **4.12** Purity/lint (connect half): `golangci-lint run`.
- [ ] **4.13** *(split checkpoint — see the PR-level Verify note below)*: measure
      `git diff --stat` for the connect half in isolation (`connect.go`, `relation/createdby.go`,
      their tests, task 4.10/4.11's doc deltas). If this half alone is at risk of the ~190 estimate
      running hot, this is the natural PR4a boundary.

### Derive half (~165) — `derive.go`, `selfmodel/facet.go`

- [ ] **4.14** Commit 1 (RED): `internal/core/selfmodel/facet_test.go` — `AllFacets()` returns a
      fresh 5-slice `{FacetIdentity, FacetValue, FacetGoal, FacetSocial, FacetPreference}`;
      `ParseFacet` round-trips each member; unknown text → `ErrUnknownFacet`.
      **Red**: `undefined: selfmodel.Facet`, the five constants, `undefined: selfmodel.AllFacets`,
      `undefined: selfmodel.ParseFacet`, `undefined: selfmodel.ErrUnknownFacet`.
      Stub: analogous to task 4.1's shape — `type Facet string`, the five consts, `var
      ErrUnknownFacet`, zero-value `AllFacets`/`ParseFacet` stubs — compiles; `len(AllFacets()) !=
      5` fails first.
      Requirement: R4.7 (selfmodel half).
- [ ] **4.15** Commit 2 (GREEN): implement `AllFacets`/`ParseFacet`. Also rewrite
      `internal/core/selfmodel/doc.go` (currently `doc.go`-only) to describe the five-facet
      vocabulary.
      Verify: `make test`; `golangci-lint run`.
      Requirement: R4.7; design §5.3.
- [ ] **4.16** Commit 1 (RED): `internal/core/consolidation/derive_test.go` — `DeriveTopicKey`
      renders `"derived/" + facet + "/" + key"` for every `f` in `selfmodel.AllFacets()` (driven by
      the vocabulary itself, asserting its own exhaustiveness — a sixth facet added later is
      exercised automatically).
      **Red**: `undefined: consolidation.DeriveTopicKey`.
      Stub: `func DeriveTopicKey(f selfmodel.Facet, key string) string { return "" }` — compiles;
      the exact-string assertion fails against `""` first.
      Requirement: R4.6.
- [ ] **4.17** Commit 2 (GREEN): implement `DeriveTopicKey`.
      Verify: `make test`; `golangci-lint run`.
      Requirement: R4.6; design §6.8.
- [ ] **4.18** Commit 1 (RED): `derive_test.go` (continued) — `MergeProposals`: cosine exactly
      `BeliefMergeCosine` merges (boundary, both sides — a hair below does not); the nearest
      existing belief wins among several; an empty `existing` slice always creates (every
      `MergeInto == ""`); a model mismatch surfaces `recall.ErrModelMismatch`; a zero-magnitude
      vector surfaces `recall.ErrZeroVector`; an un-normalized input still scores as cosine
      (normalization happens inside `MergeProposals`, never a caller obligation).
      **Red**: `undefined: consolidation.BeliefMergeCosine`, `undefined: consolidation.
      BeliefVector`, `undefined: consolidation.MergeDecision`, `undefined: consolidation.
      MergeProposals`.
      Stub: `const BeliefMergeCosine = 0.85`; `type BeliefVector struct{ BeliefID string; Vector
      []float32 }`; `type MergeDecision struct{ ProposedIndex int; MergeInto string; Similarity
      float64 }`; `func MergeProposals(model string, existing, proposed []BeliefVector)
      ([]MergeDecision, error) { return nil, nil }` — compiles; `len(decisions) != len(proposed)`
      on a non-empty `proposed` fails first.
      Requirement: R4.4.
- [ ] **4.19** Commit 2 (GREEN): implement `MergeProposals` via `recall.NewVectorIndex`/`recall.
      Search`/`recall.Normalize`, boundary inclusive at `BeliefMergeCosine`.
      Verify: `make test`; `golangci-lint run` (now imports `internal/core/recall`).
      Requirement: R4.4; design §6.8.
- [ ] **4.20** Commit 1 (RED): `derive_test.go` (continued) — `Reinforce`: asymptotic, never
      reaches 1 under repeated calls; no write at exactly `confidence == 1` (`false`, no value
      change asserted); refuses `NaN`/`+Inf`/`-Inf`/negative/`> 1` (each `false`, no write).
      **Red**: `undefined: consolidation.BeliefReinforceGain`, `undefined: consolidation.
      Reinforce`.
      Stub: `const BeliefReinforceGain = 0.10`; `func Reinforce(confidence float64) (float64, bool)
      { return confidence, false }` — compiles; an in-domain case expects `(raised, true)`, stub
      always `false`, fails first.
      Requirement: R4.5.
- [ ] **4.21** Commit 2 (GREEN): implement `Reinforce` per §4.1's shared reinforcement law.
      Verify: `make test`; `golangci-lint run`.
      Requirement: R4.5; design §4.1/§6.8.
- [ ] **4.22** `test/conformance/consolidation_purity_test.go` (extend, from task 3.6) — complete
      R0.1's tree-scan: assert exactly `{Strengthen, MergeProposals, Reinforce}` among
      `internal/core/consolidation`'s exported functions take no `time.Time` parameter.
      **Not a missing-symbol red**: extends an already-passing scaffold over functions that already
      compile — disclosed per `m2a` C9.
      Requirement: R0.1 (complete).
- [ ] **4.23** doc 02 §6.5 amendment: state the merge rule (the second dedup defense) explicitly,
      and **ruling Q2's nightly embedding cost** — `brain` embeds every active belief in memory at
      the start of the phase and discards after, no schema change — written out rather than left
      implicit, per the ruling's own requirement.
      Requirement: R4.4; design §8 (the belief-embedding handoff row), ruling Q2.
- [ ] **4.24** doc 02 §10 cross-reference: confirm the derived-key format text already reads
      `derived/{facet}/{key}`; correct it in this PR if it diverges from `DeriveTopicKey`'s shipped
      format.
      Requirement: R4.6.
- [ ] **4.25** §13: add `belief_reinforce_gain` (0.10, chosen — inherits §4.1's argument, no
      compatibility check attached) as a new row; annotate the existing "Semantic belief merge" row
      with `consolidation.BeliefMergeCosine`'s first Go home. Two rows touched (1 new, 1
      annotated).
      Requirement: R0.2.
- [ ] **4.26** Purity/coverage (derive half): `golangci-lint run`; `make cover`.
- [ ] Verify (PR-level): `make check-all`; confirm diff touches only
      `internal/core/consolidation/{connect,derive}{,_test}.go`,
      `internal/core/relation/createdby{,_test}.go`,
      `internal/core/selfmodel/{doc,facet}{,_test}.go`,
      `test/conformance/consolidation_purity_test.go` (extended), a `relation.CreatedBy` DDL-pin
      addition, `docs/02-cognitive-core.md`. **Split checkpoint**: once real impl+docs lines are
      measured, if the total is at or above ~300 (the same stop-and-report threshold `m1a`, `m1b`,
      `m1c` and `m2a`'s own PR4a used), split at the boundary between task 4.13 and task 4.14 —
      connect half (`connect.go`, `relation/createdby.go`, tasks 4.1–4.13) as PR4a, derive half
      (`derive.go`, `selfmodel/facet.go`, tasks 4.14–4.26) as PR4b — matching `design.md` §10's
      pre-drawn line exactly. Treat 350 as a hard stop, not a target: this repository's own measured
      doc-comment growth across review rounds is ~103% (`spread.go`, 183 → 371 lines), and a 15%
      overrun on this PR's own ~350 estimate already crosses 400.

---

## PR 5 — `feat/core-consolidation-pattern-eval` (~180 impl+docs)

Depends on PR 4 (no direct call, but completes the chain). Ships `patterns.go`. Also closes
Finding F2's deferred check (task 5.7).

- [ ] **5.1** Commit 1 (RED): `internal/core/consolidation/patterns_test.go` — `EvaluateStagnation`:
      non-`goal`-facet beliefs skipped regardless of elapsed time; exactly `stagnationDays` fires
      (`>=`, both sides — a hair under does not); a future `LastReinforcedAt` (clock skew) clamps to
      zero elapsed and is never stagnant; output sorted by `BeliefID` (≥3 beliefs).
      **Red**: `undefined: consolidation.DefaultGoalStagnationDays`, `undefined: consolidation.
      StagnationFinding`, `undefined: consolidation.EvaluateStagnation`.
      Stub: `const DefaultGoalStagnationDays = 21`; `type StagnationFinding struct{ BeliefID,
      TopicKey string; StagnantDays float64 }`; `func EvaluateStagnation(bs []Belief,
      stagnationDays int, now time.Time) []StagnationFinding { return nil }` — compiles; a
      genuinely-stagnant goal-facet fixture expects `len(got) == 1`, stub nil fails first.
      Requirement: R5.1.
- [ ] **5.2** Commit 2 (GREEN): implement `EvaluateStagnation` — facet gate, `>=` boundary, the
      zero-clamp for a future `LastReinforcedAt`.
      Verify: `make test`; `golangci-lint run`.
      Requirement: R5.1; design §4.6.
- [ ] **5.3** Commit 1 (RED): `patterns_test.go` (continued) — `EvaluateLoad`: exactly `threshold`
      fires (both sides — one below does not); inside the cooldown returns `false` even above
      threshold; `lastHypothesisAt == nil` fires unconditionally on count; exactly
      `LoadCooldownDays` elapsed since `lastHypothesisAt` fires.
      **Red**: `undefined: consolidation.DefaultMentalLoadThreshold`, `undefined: consolidation.
      LoadCooldownDays`, `undefined: consolidation.LoadFinding`, `undefined: consolidation.
      EvaluateLoad`.
      Stub: `const (DefaultMentalLoadThreshold = 7; LoadCooldownDays = 7)`; `type LoadFinding
      struct{ OpenCount, Threshold int }`; `func EvaluateLoad(openMentalLoad, threshold int,
      lastHypothesisAt *time.Time, now time.Time) (LoadFinding, bool) { return LoadFinding{},
      false }` — compiles; a qualifying-count-with-nil-`lastHypothesisAt` fixture expects `(finding,
      true)`, stub always `false`, fails first.
      Requirement: R5.2.
- [ ] **5.4** Commit 2 (GREEN): implement `EvaluateLoad` — threshold gate **and** cooldown gate,
      both required.
      Verify: `make test`; `golangci-lint run`.
      Requirement: R5.2; design §4.6.
- [ ] **5.5** Commit 1 (RED): `patterns_test.go` (continued) — `ResolveGoalStagnationDays`/
      `ResolveMentalLoadThreshold`: `nil`, `0`, and a negative value each fall back to the default
      for both functions; a positive value passes through for both.
      **Red**: `undefined: consolidation.ResolveGoalStagnationDays`, `undefined: consolidation.
      ResolveMentalLoadThreshold`.
      Stub: `func ResolveGoalStagnationDays(configured *int) int { return 0 }`; `func
      ResolveMentalLoadThreshold(configured *int) int { return 0 }` — compiles; the `nil` case
      expects `21`/`7` respectively, stub returns `0`, fails first.
      Requirement: R5.3.
- [ ] **5.6** Commit 2 (GREEN): implement both `Resolve*` fallbacks (`nil` or `<= 0` → default).
      Verify: `make test`; `golangci-lint run`.
      Requirement: R5.3.
- [ ] **5.7** `patterns_test.go` (continued) — **R3.2 close-out** (Finding F2): the
      `StrengthenGain`/`DefaultGoalStagnationDays` compatibility check, moved here from PR 3 because
      `DefaultGoalStagnationDays` does not exist until this PR — `ceil(ln(0.1/0.9) /
      ln(1-consolidation.StrengthenGain)) == consolidation.DefaultGoalStagnationDays`, computed from
      both named constants; boundary pinned from both sides (`n=20`: `1 - 0.9·0.9²⁰ ≈ 0.8906`, below
      0.9; `n=21`: `≈ 0.9015`, at or above).
      **Not a missing-symbol red**: `StrengthenGain` shipped in PR 3, `DefaultGoalStagnationDays` is
      implemented earlier in this same PR (task 5.2) — disclosed per `m2a` C9.
      Requirement: R3.2 (see Finding F2).
- [ ] **5.8** `test/conformance/consolidation_defaults_ddl_test.go` (extend, from task 2.11) — pin
      `DefaultGoalStagnationDays` to migration `0002:66`'s `DEFAULT 21` and
      `DefaultMentalLoadThreshold` to `0002:67`'s `DEFAULT 7`, both read off disk. **Note**: this
      brings the file to three DDL-pinned `Default*` constants total (`DefaultWeightThreshold` from
      PR 2 plus these two), not four — `BeliefMergeCosine` (task 4.18's constant) has no schema
      `DEFAULT` and is pinned by literal equality instead (task 4.18 already covers it in
      `derive_test.go`, not here).
      Requirement: R5.4.
- [ ] **5.9** doc 02 §7 amendment: state both watcher predicates explicitly — the stagnation
      window read off `last_reinforced_at`, and **why the phase order makes that reading sound**
      (`derive` at slot five refreshes it, `pattern_eval` at slot seven reads the refreshed value —
      reversing the order would make every reinforced belief look stagnant one more night); the
      load-accumulation cooldown, naming `LoadCooldownDays = 7` and stating it is unrelated to
      `mental_load_threshold`'s own coincidentally-equal 7.
      Verify: read the section; `docs-sync.yml` not locally verifiable.
      Requirement: design §4.6.
- [ ] **5.10** §13: add `load_cooldown_days` (7, chosen — doc 02 §7 names no number) as a new row;
      annotate `goal_stagnation_days` with `consolidation.DefaultGoalStagnationDays` +
      `ResolveGoalStagnationDays` (note §9 Q3's two-schema-homes caveat in the row's own comment);
      annotate `mental_load_threshold` with `consolidation.DefaultMentalLoadThreshold` +
      `ResolveMentalLoadThreshold`. Three rows touched (1 new, 2 annotated).
      Requirement: R0.2.
- [ ] **5.11** Purity/coverage: `golangci-lint run`; `make cover`.
- [ ] **5.12** Cross-cutting close-out (last PR of the chain): confirm §13 now carries 39 rows
      (33 + 6 new, matching design §6.11's count); confirm `docs/06-harness.md` needed no change
      across all five PRs (I11/I12 rows already present before this change started); `rg 'now
      time\.Time' internal/core/consolidation` enumerates exactly the time-dependent decisions
      design §5.4's diagram names, confirming `Strengthen`/`MergeProposals`/`Reinforce` (R0.1)
      carry none.
      Requirement: R0.1, R0.2 (final check).
- [ ] Verify (PR-level): `make check-all`; confirm diff touches only
      `internal/core/consolidation/patterns{,_test}.go`,
      `test/conformance/consolidation_defaults_ddl_test.go` (extended), `docs/02-cognitive-core.md`.
      Target ≤180 impl+docs lines.

---

## Review Workload Forecast

| Field | Value |
|-------|-------|
| Estimated changed lines | ~1,130 implementation + docs, ~1,780 test (design's own guess) — design itself flags the test-line guess as likely ~2.5× low against `m2a`'s measured 3.9× impl+docs-to-test ratio (closer to ~4,400 test lines), which does not move any PR past the ceiling since tests are counted separately |
| 400-line budget risk | **Medium–High for PR 4** (~350 impl+docs, 0.88× of the ceiling, before a line is written — above every `m2a` PR's *actual* impl+docs figure, the highest of which was 319); **Low–Medium for PR 1, 2, 3, 5** |
| Chained PRs recommended | Yes — five links, already a chain by design |
| Suggested split | PR 4 has a pre-drawn split at `connect.go` (+`relation/createdby.go`) \| `derive.go` (+`selfmodel/facet.go`) — tasks 4.1–4.13 vs 4.14–4.26; PR 3 has a smaller-risk fallback split at `strengthen.go` \| `reweight.go` if the materialization-decline doc amendment runs long, with C17's deletion travelling with `reweight.go` |
| Delivery strategy | `auto-chain` (assumed, matching `m1a`/`m1b`/`m1c`/`m2a` precedent — not explicitly re-specified for this run) |
| Chain strategy | `stacked-to-main` (assumed, same precedent) |

Decision needed before apply: No
Chained PRs recommended: Yes
Chain strategy: stacked-to-main
400-line budget risk: Medium

**Why PR 4 is flagged despite design's own "no PR crosses 400" claim.** Design's §10 states no PR
crosses the ceiling under the impl+docs convention, but its own risk paragraph undercuts the
comfort: `m2a`'s highest measured impl+docs figure across four links was 319 (0.80×), yet PR 4's
*pre-code* estimate here is already 350 (0.88×) — higher than anything `m2a` actually produced.
Combined with this repository's own measured doc-comment growth (`spread.go`, 183 → 371 lines,
~103% across three review rounds), a 15% overrun on PR 4's own estimate crosses 400 outright. The
split line (task 4.13/4.14 boundary) is carried into the tasks explicitly for this reason, per the
assignment's own instruction — `sdd-apply` should treat ~300 impl+docs as a stop-and-report
checkpoint for PR 4, the same threshold `m2a`'s own PR4a used, not wait for 350 to be reached
before considering the split.

---

## Traceability

| Spec section | Requirements | Tasks |
|---|---|---|
| R0 — purity and calibration, cross-cutting | R0.1, R0.2 | 3.6, 4.22 (R0.1); 2.14, 3.9, 4.11, 4.25, 5.10 (R0.2); 5.12 (final check) |
| §1 The phase sequence (PR 1) | R1.1–R1.3 | 1.1–1.5 |
| §1 `Transition`/`Reason` (spec's R1.4, placed here per design's PR 2 — Finding F1) | R1.4 | 2.1–2.2, 2.10 |
| §2 `expire_incomplete`/`archive` (PR 2) | R2.1–R2.5 | 2.3–2.9, 2.11–2.13 |
| §3 `strengthen`/`reweight` (PR 3) | R3.1, R3.3 | 3.1–3.6 |
| §3 `StrengthenGain` compatibility check (spec's R3.2, placed here per PR 5 — Finding F2) | R3.2 | 5.7 |
| §4 `connect`/`derive` (PR 4) | R4.1–R4.7 | 4.1–4.26 |
| §5 `pattern_eval` (PR 5) | R5.1–R5.4 | 5.1–5.11 |
| §6 What this spec does not require | (not tasked — `m2c`/`m2d`/M5) | Handoffs table above |
| Doc 02 amendments | (design §7-equivalent, per PR) | 1.7, 2.12–2.13, 3.7–3.8, 4.10, 4.23–4.24, 5.9 |
| §13 calibration rows | (design §6.11) | 2.14, 3.9, 4.11, 4.25, 5.10, 5.12 |
| C17/C18/C20/C21 (`m2a` handoffs closed here) | (not spec requirements) | 3.4 (C18/C20/C21), 3.5 (C17) |
