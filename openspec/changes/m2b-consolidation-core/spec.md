# Spec — M2 Phase B: the consolidation core

Delta specification for `m2b-consolidation-core`, the second of four chained changes splitting
`openspec/changes/m2-sleep-weight/proposal.md` (owner ruling round 2 #6). States what MUST be true
of the repository after this change is applied, in testable form. It does not prescribe how (that
is `design.md`, which ran first on this change and is authoritative for every identifier below).

**Status: written against a settled design, not reconciled against one.** `m2a` ran `sdd-spec` and
`sdd-design` concurrently and had to adjudicate four naming and formula conflicts afterward. On
`m2b` design ran first, specifically to avoid repeating that cost (`design.md`'s own preface).
Every exported identifier, struct field, constant and error value below is copied from
`design.md` §6 — "the complete surface `spec.md` writes against" — verbatim. **No name in this
document was invented**: every place this spec needed a name, `design.md` §6 already declared it.
Where design left a value **chosen rather than derived** (`StrengthenGain`, `BeliefReinforceGain`,
`ConnectSourceLimit`, `ConnectCandidateK`, `LoadCooldownDays`), this spec states the value and, if
design attached one, its compatibility check — never an entailment design itself did not claim.

## Scope boundary (binding, from the proposal's §5 and `design.md` §1–§2)

> `m2b` ships one package that was `doc.go`-only — `internal/core/consolidation` — plus two small
> vocabulary additions in `internal/core/relation` and `internal/core/selfmodel`. Zero ports, zero
> store, zero `brain`, zero I/O, no clock read inside a decision. `m2b` depends on `m2a`
> (`archive` and `reweight` both call `weight.Effective`/`weight.Resurface`).

Five PRs, per `design.md` §10: `feat/core-consolidation-order`, `-expire-archive`,
`-strengthen-reweight`, `-connect-derive`, `-pattern-eval`. Both `internal/core/consolidation` and
its two vocabulary additions are inside `docs/06-harness.md` §1's tree already — no preflight tree
PR. `docs/06-harness.md` §4 already carries I11 and I12's rows — this change adds no new
invariant-table row, only calibration rows (§13, below).

Nothing in this change touches `internal/ports`, `internal/store`, `internal/brain`,
`internal/scheduler`, `cmd/nooma`, or any migration. The runner, the ports, `decision_log` writes,
and I11's behavioural half are `m2c`'s (`design.md` §8, §11).

### R0 — General purity and calibration requirements, across all five PRs

**R0.1 — `internal/core/consolidation` stays pure.** MUST: no file in the package calls
`time.Now`, `time.Since`, `time.Until`, `rand.*`, `uuid.*`, or `os.Getenv`. MUST: the package
imports only the standard library and `internal/core/{unit,weight,recall,relation,selfmodel}` —
never `internal/ports`, `internal/store`, or `internal/brain`. MUST: `Strengthen`, `MergeProposals`
and `Reinforce` take no `time.Time` **by value** — the instant itself never travels into these three
decisions doc 02's "accumulated evidence" phases do not need an instant to make. A `*time.Time`
**resolved-absence sentinel** — nil meaning "no value at all" rather than an instant to compute
from, the same shape as `focus.ResolveMargin(configured *float64)` — is permitted: it is
`Strengthen`'s own `since` parameter, which design.md §5.4 lists among the three decisions that
"take no clock at all" for exactly this reason.
**Verified by**: L2, `depguard`/`forbidigo` (existing `core-purity` gate) plus a tree-scan
asserting exactly these three functions have no `time.Time`-by-value parameter — a `*time.Time`
resolved-absence sentinel is exempt by design, not a gap in the scan.

**R0.2 — every new calibrated number gets exactly one `docs/02-cognitive-core.md` §13 row.** MUST:
`IncompleteExpiryHours`, `StrengthenGain`, `ConnectSourceLimit`, `ConnectCandidateK`,
`BeliefReinforceGain`, and `LoadCooldownDays` each land as a new §13 row in the PR that introduces
the constant. MUST: the `weight_threshold`, `goal_stagnation_days`, `mental_load_threshold`, and
"Semantic belief merge" rows are annotated with their `consolidation.*` Go home in the same PR.
**Verified by**: manual review at PR time (docs-sync.yml fires per-PR on `internal/core/`; there is
no automated §13 row-count check).

---

## 1. The phase sequence (I11, pure half) — PR1 `feat/core-consolidation-order`

Traced to `docs/02-cognitive-core.md` §6's arrow line and `docs/06-harness.md` §4's I11 row.

### R1.1 — `Order()` enumerates exactly the eight phases, ascending, with `PhaseLearn` last

**MUST**: `internal/core/consolidation` exposes `type Phase int` with the eight constants
`PhaseExpireIncomplete, PhaseArchive, PhaseStrengthen, PhaseConnect, PhaseDerive, PhaseReweight,
PhasePatternEval, PhaseLearn`, and `func Order() []Phase` returns a fresh slice of exactly these
eight values, ascending from `Phase(0)`, with `Order()[7] == PhaseLearn`.

**MUST**: `(p Phase) String()` is total over every `int` value — an out-of-range `Phase` renders
`"Phase(n)"` rather than panicking.

**MUST**: `ParsePhase(s.String())` round-trips to `s` for every `s` in `Order()`, and
`ParsePhase` returns `ErrUnknownPhase` for any string that is not one of the eight names.

**Verified by**: L1 — `Order()` length and ascending order; `String()` never panics across a
swept range including negative and above-range values; `ParsePhase ∘ String` round-trip table;
unknown-text rejection.

### R1.2 — the shipped order matches doc 02 §6's own arrow line, checked against the document, not restated as a literal (I11, pure half)

**MUST**: joining `Order()`'s `String()` names with `" → "` equals doc 02 §6's arrow line, parsed
off disk at test time — not compared against a second copy of the eight names written into the
test file.

**MUST**: no non-test file outside `internal/core/consolidation` contains two or more of the eight
phase-name string literals. A caller (a future CLI, a runner) may switch over the `Phase`
**constants**; it may not keep its own list of the eight names.

**Verified by**: L2, `test/conformance/` — one test reads `docs/02-cognitive-core.md` off disk for
the first assertion, and tree-scans every non-test `.go` file outside the package for the second.

### R1.3 — `PhaseLearn` occupies slot eight with no decision function; the absence is the no-op

**MUST NOT**: `internal/core/consolidation` exports any function that performs `PhaseLearn`'s
decision. The slot exists as a `Phase` constant only; a runner that reads new signals or writes to
`decision_log` for this phase is out of scope until M5 (owner ruling 3).

**Verified by**: L2, structural — the existing `core_exported_decls_have_tests_test.go` presence
guard combined with a review note; there is no positive test for an absent function, only the
absence itself, checked by `rg` over the package's exported surface at PR review.

### R1.4 — `Transition` carries a machine-readable `Reason`, and every emitted pair is legal

**MUST**: `internal/core/consolidation` exposes `type Reason string` with exactly
`ReasonIncompletePromoted`, `ReasonIncompleteExpired`, `ReasonBelowWeightThreshold`, and
`AllReasons() []Reason` returning all three as a fresh slice.

**MUST**: `Transition{UnitID, From, To, Reason}` is the sole payload for a planned status change,
and every `Transition` any producer in this package emits is a `(From, To)` pair that
`unit.ValidateTransition` accepts without error.

**Verified by**: L1 — an exhaustiveness table driving every `Transition` any producer emits (from
`ExpireIncomplete`, §2, and `Archive`, §2) through `unit.ValidateTransition`, rather than asserting
the legal pairs by hand a second time.

---

## 2. `expire_incomplete` and `archive` — PR2 `feat/core-consolidation-expire-archive`

Traced to doc 02 §1's two-outcome text, §6.1 (the contradiction this PR fixes), §6.2, and
`design.md` §4.2/§4.4.

### R2.1 — `ExpireIncomplete` resolves doc 02's §1/§6.1 contradiction: promotion is the default, archival is the exception a caller must evidence

**MUST**: `ExpireIncomplete(us []Incomplete, now time.Time) []Transition` computes, per unit,
`elapsed = now.Sub(u.CreatedAt)` clamped at zero when negative (clock skew, a backdated import).
A unit with `elapsed < IncompleteExpiryHours` (24) produces no transition.

**MUST**: for a unit with `elapsed >= IncompleteExpiryHours`, the emitted transition is
`incomplete → archived` with `Reason: ReasonIncompleteExpired` when `u.Unresolved == true`, and
`incomplete → pool` with `Reason: ReasonIncompletePromoted` when `u.Unresolved == false`.

**MUST**: `elapsed` clamped to zero by a `CreatedAt` after `now` produces no transition, by the
same rule as R2.1's own clamp — a unit that does not yet exist has waited no time.

**Domain restriction, stated rather than left implicit**: `Incomplete.Unresolved` is a field this
package declares and no code in M2 produces — every caller in this milestone passes `false` for
every unit (`design.md` §9 Q1). This requirement is proven against a repo-constructed
`Incomplete{Unresolved: true}` input; it does not claim a producer exists.

**MUST**: output is sorted by `UnitID`.

**MUST**: doc 02 §6.1 is amended in this PR to state both outcomes — promotion by default,
archival only when the caller marks the ambiguity `Unresolved` — resolving the contradiction with
§1 (`CLAUDE.md` non-negotiable #1).

**Scenario: a unit ambiguous for over 24 hours and marked resolved-nothing is archived**
- GIVEN an `Incomplete` unit with `CreatedAt` 25 hours before `now` and `Unresolved: true`
- WHEN `ExpireIncomplete` runs
- THEN it returns exactly one `Transition{From: pool-incomplete, To: archived, Reason:
  ReasonIncompleteExpired}` for that unit

**Verified by**: L1 — a boundary table at `elapsed` just under 24h (nothing), exactly 24h (a
transition, both branches of `Unresolved`), and just over; a `CreatedAt` after `now` case; every
emitted pair driven through `unit.ValidateTransition`; sorted output.

### R2.2 — `Archive` cools a unit whose effective weight is strictly below the threshold, and refuses rather than coerces a corrupt read

**MUST**: `Archive(cs []Cold, threshold float64, now time.Time) (transitions []Transition,
corrupted []string)` plans `pool → archived` (`Reason: ReasonBelowWeightThreshold`) for every
`Cold` whose `Status == unit.StatusPool` and whose
`weight.Effective(c.Weight, c.DecayRate, c.LastTouchedAt, now)` is **strictly less than**
`threshold`. A `Cold` at exactly `threshold` is **not** archived — both sides of the boundary are
load-bearing, not only the `<` operator's literal reading.

**MUST**: a `Cold` whose `Status` is not `unit.StatusPool` produces no transition and no corrupted
entry — `Archive` only ever cools a live unit.

**MUST**: a `Cold` whose `Weight` or `DecayRate` is non-finite (`NaN` or `±Inf`) is refused — not
archived — and its `UnitID` is reported through `corrupted`. Archiving is a state transition; a
read error MUST NOT cause one.

**MUST**: both returned slices are sorted by `UnitID`.

**Verified by**: L1 — `e < threshold` archives, `e == threshold` does not (both sides); a
non-`pool` status produces neither output; each of `NaN`/`+Inf`/`-Inf` in `Weight` or `DecayRate`
refuses into `corrupted`; both slices sorted.

### R2.3 — `ResolveWeightThreshold` falls back to the default for an absent or corrupt configured value

**MUST**: `ResolveWeightThreshold(configured *float64) float64` returns `DefaultWeightThreshold`
(0.5) when `configured` is `nil`.

**MUST**: it also returns `DefaultWeightThreshold` when `configured` is non-finite (`NaN`, `±Inf`)
or **finite but outside** `[0, weight.WeightCeiling]` — a value core cannot interpret is treated
identically to no value at all, never trusted as-is.

**MUST**: any other `*configured` value passes through unchanged.

**Verified by**: L1 — `nil` → default; a finite in-range value passes through; `NaN`, `+Inf`,
`-Inf`, a negative value, and a value above `weight.WeightCeiling` each → default.

### R2.4 — the default weight threshold composes with `m2a`'s revive and resurface guarantees, at the shipped defaults only

**MUST**: `weight.ReviveGain × weight.WeightCeiling > DefaultWeightThreshold` holds at the shipped
defaults (`0.35 × 2.0 = 0.70 > 0.5`) — one direct revive always clears the archive band.

**MUST**: `weight.ResurfaceAttenuation^weight.ResurfaceMaxHops × weight.WeightCeiling <=
DefaultWeightThreshold` holds at the shipped defaults (`0.5² × 2.0 = 0.5 ≤ 0.5`), and because
`Resurface`'s boost is asymptotic (never reaching its target), a two-hop-only propagation lands
**strictly below** the threshold — so `Archive`'s strict `<` always classifies a pure two-hop
result as archivable and never treats it as an edge case at exact equality.

**MUST NOT**: either assertion is read as a general property — both constants involved are ⚙
(recalibratable per user), and the doc comment on the test says so. The relation holds at the
defaults; a user who raises `weight_threshold` can break it without breaking the code.

**Verified by**: L1 — both inequalities computed from the named Go constants, never from repeated
literals; the doc comment states the ⚙ caveat.

### R2.5 — `DefaultWeightThreshold` is pinned to the migration's own `DEFAULT`

**MUST**: `consolidation.DefaultWeightThreshold` equals the literal in
`internal/store/sqlite/migrations/0002_learning_and_search.sql:63`
(`config.weight_threshold ... DEFAULT 0.5`), read off disk via the existing `migrationSQLText`
helper.

**Verified by**: L2, `test/conformance/`.

---

## 3. `strengthen` and `reweight` — PR3 `feat/core-consolidation-strengthen-reweight`

Traced to doc 02 §6.3, §6.6, and `design.md` §4.1 (the shared reinforcement law), §4.3, §4.5.

### R3.1 — `Strengthen` raises a relation's strength only when both endpoints were used since the last pass, and never lowers one

**MUST**: `Strengthen(es []RelationEvidence, since *time.Time) (changes []StrengthChange,
corrupted []string)` returns nothing at all — for every input — when `since == nil` (the vault has
never consolidated). Accumulated evidence over no interval is no evidence.

**MUST**: for `since != nil`, a `RelationEvidence` produces a `StrengthChange` only when **both**
`FromLastTouchedAt` and `ToLastTouchedAt` are at or after `*since` (`!Before(*since)`, i.e. the
comparison is not-strictly-before, so equality to `since` qualifies). A relation with either
endpoint touched before `*since` produces no change.

**MUST**: for a qualifying relation, `StrengthChange.Strength = s + StrengthenGain × (1 - s)` —
the shared asymptotic reinforcement law (§4.1), applied once per pass.

**MUST**: a relation already at strength `1` produces **no row** — a decision with no effect
writes nothing (doc 02 §11).

**MUST NOT**: `Strengthen` ever lowers a relation's strength, for any input whatsoever. The only
two ways doc 02 §4 lowers a relation's strength are rejection (deletion, I10) and never-consulted
decay; neither is this function's job.

**MUST**: `confidence` is never read or returned by `Strengthen` — §6.3 says *strength*, and
co-use evidence says nothing about the judge's original certainty.

**Domain restriction**: a `RelationEvidence.Strength` that is non-finite (`NaN`, `±Inf`) **or
finite but outside `[0,1]`** is refused at the door — not used to compute a change — and its
`RelationID` is reported through `corrupted`, before any comparison against `1` or against
`StrengthenGain`'s formula runs.

**MUST**: output is sorted by `RelationID`.

**Scenario: co-use since the last pass strengthens a relation asymptotically**
- GIVEN a relation at strength 0.5 whose both endpoints' `LastTouchedAt` are after `since`
- WHEN `Strengthen` runs
- THEN it returns `StrengthChange{Strength: 0.5 + 0.10 × (1 - 0.5) = 0.55}` for that relation

**Verified by**: L1 — `since == nil` → empty for every input; one endpoint stale → nothing; both
at exactly `since` → a change (`Before` is strict, so equality qualifies); asymptotic and never
reaches 1; already at 1 → no row; the four corrupt shapes (`NaN`, `+Inf`, `-Inf`, `1.5`, `-0.5`)
refuse into `corrupted`; sorted output.

### R3.2 — `StrengthenGain` is fixed at 0.10, checked for compatibility against the default stagnation horizon, never claimed as derived from it

**MUST**: `consolidation.StrengthenGain == 0.10`. This is a **chosen** constant — this
specification does not claim, and no test may assert, that any fixed count of nightly passes
uniquely entails this value.

**MUST**: `ceil(ln(0.1/0.9) / ln(1 - StrengthenGain)) == DefaultGoalStagnationDays`, computed from
both named Go constants at test time — never from repeated literals. This is a **compatibility
check between two independently chosen defaults**, stated as such in the test's own doc comment;
it is not evidence that either value determines the other, and the admissible range of gains
producing the same count is wider than the single chosen value (`design.md` §4.3).

**Verified by**: L1 — the identity above, computed from the constants; a boundary pin from both
sides (`n = 20` nights: `1 - 0.9·0.9²⁰ ≈ 0.8906`, below 0.9; `n = 21`: `≈ 0.9015`, at or above).

### R3.3 — `Reweight` spreads activation over the pass's new edges only, by re-calling `weight.Resurface` unchanged, and never introduces a new calibration constant

**MUST**: `Reweight(states map[string]weight.Current, newEdges []weight.Edge, now time.Time)
(boosts []weight.Boost, corrupted []string)` calls `weight.Resurface` once per origin, where an
origin is every endpoint of an edge in `newEdges`, over a `Neighbourhood` built from `states` and
`newEdges`.

**MUST**: `Reweight` builds `Neighbourhood.States` from the `states` map as a plain slice before
calling `Resurface`. A duplicate `UnitID` is unrepresentable in a `map[string]weight.Current` by
construction — that is what closes m2a C18, not slice order: `weight.Resurface` immediately
re-keys `Neighbourhood.States` into its own map (`spread.go`), so no `Resurface`-observable outcome
depends on the order `Reweight` hands it the slice in. `Reweight` does not sort this
intermediate slice (Judgment Day round 1 Fix B correction — an earlier draft of this requirement
claimed a sort here was mutation-verified; it never was, see the corrected Verified-by line below).

**MUST**: `boosts` is merged across origins per unit by the **maximum** boosted weight — the same
`max` rule `weight.Resurface` and `focus.AdjacencyStrengths` already use for combining graph
evidence.

**MUST**: an `Edge` whose `Strength` is non-finite or outside `[0,1]` is refused at `Reweight`'s
own door — before `weight.clampStrength` or any downstream comparison runs — and **both**
endpoints of that edge are reported through `corrupted`.

**MUST**: `corrupted` is merged across all of the pass's origin calls by **union, deduplicated** —
a unit id appears in `Reweight`'s output `corrupted` at most once, regardless of how many origin
calls independently flag it.

**MUST**: a unit id **may** appear in both `boosts` and `corrupted` from the same `Reweight` call,
and **neither suppresses the other**. "At least one origin moved this unit's weight" and "at least
one origin could not explain this unit because an edge in the shared batch was unusable" are
independent facts about the pass's data health, both reported when both are true.

**MUST**: both returned slices are sorted by `UnitID`.

**MUST NOT**: `Reweight` declares or reads any calibration constant beyond `weight.ReviveGain`,
`weight.WeightCeiling`, `weight.ResurfaceMaxHops` and `weight.ResurfaceAttenuation` — it introduces
zero new numbers.

**MUST NOT**: `internal/core/consolidation` exposes a function that rewrites a unit's
`(weight, last_touched_at)` pair to its currently-computed effective value ("materialize"). Doc 02
§6.6 is amended in this PR to read *"post-connection weight adjustments (decay materialization
remains optional and is not exercised by M2's `reweight`)"* — the option stays legal in doc 02;
M2 does not exercise it.

**Scenario: a unit corrupted by one origin and legitimately boosted by another reports both**
- GIVEN two `connect` sources A and B whose `Resurface` neighbourhoods share one `NaN`-strength
  edge, and unit V is reachable from A only through a valid path and from B only through the
  corrupted edge
- WHEN `Reweight` runs over both origins' results
- THEN V's `UnitID` appears in `boosts` (A's valid boost) **and** in `corrupted` (B's refusal),
  and neither list omits it because of the other

**Verified by**: L1 — both endpoints of a new edge are boosted; multi-origin results merge by max;
a corrupt edge strength refuses both endpoints; `corrupted` deduplicates across origins; a unit
present in both outputs from one call; `boosts` and `corrupted` sorted by `UnitID` (eight elements
each — fewer accidentally lands already sorted too often under Go's randomized map iteration, see
Fix C below), mutation-verified by removing the final sort and measuring the kill rate (Judgment
Day round 1 Fix C — a prior draft of this line claimed this coverage for an intermediate
`Neighbourhood.States` sort that has since been deleted as dead per C13, and whose removal never
actually changed any test outcome); no reference to any constant beyond the four named above.

---

## 4. `connect` and `derive` — PR4 `feat/core-consolidation-connect-derive`

Traced to doc 02 §6.4, §6.5, §10, and `design.md` §4.4/§4.6/§5.3.

### R4.1 — `SelectConnectSources` ranks live, recently-touched units by effective weight, capped at `ConnectSourceLimit`

**MUST**: `SelectConnectSources(ss []Source, since *time.Time, now time.Time) []string` includes
only `Source` values with `Status == unit.StatusPool`. When `since != nil`, a source is eligible
only if `LastTouchedAt` is at or after `*since`; when `since == nil`, every live source is
eligible (the first pass over an existing vault).

**MUST**: eligible sources are ordered by `weight.Effective(...)` descending, ties broken by
`UnitID` ascending, and the result is capped at `ConnectSourceLimit` (20) entries.

**Verified by**: L1 — `since == nil` takes the whole live pool; non-live sources excluded; a
since-touched-before source excluded; ordering by `Effective` with the id tie-break; the cap;
determinism under `-shuffle=on`.

### R4.2 — `ConnectPairs` bounds each source to `ConnectCandidateK` unjudged candidates, excluding itself and already-connected pairs by their canonical form

**MUST**: `ConnectPairs(source string, fused []recall.FusedCandidate, existing map[Pair]bool)
[]Pair` returns at most `ConnectCandidateK` (5) pairs `{From: source, To: candidate}`, preserving
`fused`'s order, never including `source` itself as a candidate, and excluding any candidate `c`
for which `existing[CanonicalPair(source, c)]` is `true`.

**MUST**: `CanonicalPair(a, b)` is symmetric (`CanonicalPair(a,b) == CanonicalPair(b,a)`, ordered
lexicographically) and is used **only** for the `existing` lookup — a stored or proposed relation
always carries `From: source, To: candidate` (doc 02 §4's rule that direction is what the judge
said).

**Verified by**: L1 — the source never appears as its own candidate; `existing` excludes by
`CanonicalPair` regardless of which direction it was stored; the cap at `ConnectCandidateK`; fused
order preserved.

### R4.3 — `ProposeRelation` writes a plan only for a judgment that decided something, and never stores below the persist threshold

**MUST**: `ProposeRelation(from string, j relation.Judgment, t relation.Thresholds)
(ProposedRelation, bool)` returns `(_, false)` — no plan, no `decision_log` row — when
`*j.Outcome == relation.OutcomeNew`, when `relation.Decide(*j.Confidence, t) ==
relation.Discard`, or when any of `j.TargetUnitID`, `j.Type`, `j.Strength`, `j.Confidence` is
`nil` after tolerant decode (doc 02 §4: "a judgment that decided nothing writes nothing").

**MUST**: it returns `(_, true)` when `relation.Decide(...)` is `relation.Uncertain` or
`relation.Asserted` and every one of the four pointer fields is present — the `Uncertain` band is
stored *and* asked about (I09); the asking is M3's.

**MUST**: the returned `ProposedRelation.CreatedBy` is always `relation.CreatedByConsolidation`.

**Scenario: a judgment with a missing confidence writes nothing**
- GIVEN a `relation.Judgment` with `Outcome` = `duplicate`, `TargetUnitID` and `Type` present, and
  `Confidence` nil (degraded on tolerant decode)
- WHEN `ProposeRelation` is called
- THEN it returns `(ProposedRelation{}, false)`

**Verified by**: L1 — `new` → false; `Discard` → false; each of the four missing-field cases →
false, individually; `Uncertain` and `Asserted` with all fields present → true;
`ProposedRelation.CreatedBy` always `CreatedByConsolidation` when true is returned.

### R4.4 — `MergeProposals` merges a proposed belief into the nearest existing belief at cosine ≥ `BeliefMergeCosine`, and creates one otherwise

**MUST**: `MergeProposals(model string, existing, proposed []BeliefVector) ([]MergeDecision,
error)` returns one `MergeDecision` per proposed belief. For each, it finds the existing belief
with the highest cosine similarity — computed via `recall.Search`/`recall.Normalize` over
unit-normalized vectors, never a second similarity implementation — and sets `MergeInto` to that
belief's id when the similarity is **at or above** `BeliefMergeCosine` (0.85); otherwise
`MergeInto == ""` (create).

**MUST**: the boundary is inclusive — a similarity **exactly equal** to `BeliefMergeCosine`
merges; a similarity a hair below does not (both sides tested).

**MUST**: an empty `existing` slice always creates — every `MergeDecision.MergeInto == ""`.

**MUST**: a model mismatch between compared vectors surfaces `recall.ErrModelMismatch`; a
zero-magnitude vector surfaces `recall.ErrZeroVector`. `MergeProposals` normalizes every vector
itself, so an un-normalized input still scores as cosine — normalization is internal, never a
caller obligation.

**Verified by**: L1 — cosine exactly `BeliefMergeCosine` merges (boundary, both sides); the
nearest existing belief wins among several; empty `existing` creates for every proposal; a model
mismatch surfaces `ErrModelMismatch`; a zero vector surfaces `ErrZeroVector`.

### R4.5 — `Reinforce` raises a merged belief's confidence via the shared reinforcement law, and refuses a corrupt or saturated input

**MUST**: `Reinforce(confidence float64) (float64, bool)` returns
`(confidence + BeliefReinforceGain × (1 - confidence), true)` for a finite `confidence` in
`[0, 1)`.

**MUST**: it returns `(confidence, false)` — no write — when `confidence == 1` exactly (a decision
with no effect writes nothing, doc 02 §11).

**MUST**: it returns `(_, false)` when `confidence` is non-finite (`NaN`, `±Inf`) or finite but
outside `[0, 1]` — refused, never clamped.

**MUST**: `consolidation.BeliefReinforceGain == 0.10`, a **chosen** constant. Unlike
`StrengthenGain`, no compatibility check is attached to it in `design.md`, and this specification
does not invent one.

**Verified by**: L1 — asymptotic and never reaches 1 under repetition; no write at exactly 1;
refuses `NaN`, `+Inf`, `-Inf`, a negative value and a value above 1.

### R4.6 — `DeriveTopicKey` renders doc 02 §10's derived-key format for every self-model facet

**MUST**: `DeriveTopicKey(f selfmodel.Facet, key string) string` returns
`"derived/" + string(f) + "/" + key`, for every `f` in `selfmodel.AllFacets()`.

**Verified by**: L1 — driven by `selfmodel.AllFacets()` itself, asserting its own exhaustiveness,
so a sixth facet added later is exercised automatically.

### R4.7 — `relation.CreatedBy` and `selfmodel.Facet` are closed vocabularies matching the house pattern

**MUST**: `relation.AllCreatedBy()` returns a fresh slice of exactly `CreatedBySystem`,
`CreatedByConsolidation`, `CreatedByUser`. `relation.ParseCreatedBy` round-trips every member's
string value and returns `ErrUnknownCreatedBy` for any other text.

**MUST**: `selfmodel.AllFacets()` returns a fresh slice of exactly `FacetIdentity`, `FacetValue`,
`FacetGoal`, `FacetSocial`, `FacetPreference`. `selfmodel.ParseFacet` round-trips every member and
returns `ErrUnknownFacet` for any other text.

**MUST**: `relation.AllCreatedBy()`'s three string values match migration
`0001_core_tables.sql:37`'s `created_by` column comment vocabulary (`system|consolidation|user`),
read off disk.

**Verified by**: L1 — fresh-slice and round-trip tests for both vocabularies; L2 — the
`created_by` comment pin.

---

## 5. `pattern_eval` — PR5 `feat/core-consolidation-pattern-eval`

Traced to doc 02 §7 and `design.md` §4.6.

### R5.1 — `EvaluateStagnation` finds goal-facet beliefs unreinforced for at least the stagnation window

**MUST**: `EvaluateStagnation(bs []Belief, stagnationDays int, now time.Time) []StagnationFinding`
returns one `StagnationFinding{BeliefID, TopicKey, StagnantDays}` per `Belief` whose
`Facet == selfmodel.FacetGoal` and whose `stagnantFor = now.Sub(LastReinforcedAt)` (in days,
clamped at zero when negative) is **at or above** `stagnationDays`. A belief of any other facet is
never included, regardless of how stagnant it is.

**MUST**: the boundary is inclusive both ways — exactly `stagnationDays` produces a finding; a
hair under does not.

**MUST**: a `LastReinforcedAt` after `now` (clock skew, backdated import) clamps to zero elapsed
and is never stagnant.

**MUST**: output is sorted by `BeliefID`.

**Verified by**: L1 — non-goal facets skipped regardless of elapsed time; exactly
`stagnationDays` fires (both sides); a future `LastReinforcedAt` clamps and does not fire; sorted
output.

### R5.2 — `EvaluateLoad` fires a tentative load finding at or above threshold, gated by a cooldown since the last hypothesis

**MUST**: `EvaluateLoad(openMentalLoad, threshold int, lastHypothesisAt *time.Time, now
time.Time) (LoadFinding, bool)` returns `(LoadFinding{OpenCount: openMentalLoad, Threshold:
threshold}, true)` when `openMentalLoad >= threshold` **and** either `lastHypothesisAt == nil` or
`now.Sub(*lastHypothesisAt) >= LoadCooldownDays × 24h`.

**MUST**: it returns `(_, false)` when `openMentalLoad < threshold`, regardless of the cooldown.

**MUST**: it returns `(_, false)` — no finding — when `openMentalLoad >= threshold` but the
cooldown since `lastHypothesisAt` has not yet elapsed. A count above threshold, inside the
cooldown, is a decision with no effect and writes nothing (doc 02 §11).

**MUST**: both boundaries are inclusive — exactly `threshold` fires; exactly `LoadCooldownDays`
elapsed since `lastHypothesisAt` fires.

**MUST**: `consolidation.LoadCooldownDays == 7`, a **chosen** constant, and no test may assert a
relationship between it and `mental_load_threshold` (also 7 by coincidence — the two are a
duration and a count, and nothing ties them, exactly as `m2a` recorded for `focus_size` and
`mental_load_threshold`).

**Scenario: above threshold but inside the cooldown produces no finding**
- GIVEN `openMentalLoad = 9`, `threshold = 7`, and `lastHypothesisAt` 3 days before `now`
  (`LoadCooldownDays = 7`)
- WHEN `EvaluateLoad` runs
- THEN it returns `(LoadFinding{}, false)` — no finding, despite the count qualifying

**Verified by**: L1 — exactly `threshold` fires, one below does not; inside the cooldown returns
false even above threshold; `lastHypothesisAt == nil` fires unconditionally on count; exactly
`LoadCooldownDays` elapsed fires (both sides).

### R5.3 — `ResolveGoalStagnationDays` and `ResolveMentalLoadThreshold` fall back to their defaults for a missing or non-positive configured value

**MUST**: `ResolveGoalStagnationDays(configured *int) int` returns `DefaultGoalStagnationDays`
(21) when `configured` is `nil` or `<= 0`; any positive value passes through unchanged.

**MUST**: `ResolveMentalLoadThreshold(configured *int) int` returns
`DefaultMentalLoadThreshold` (7) when `configured` is `nil` or `<= 0`; any positive value passes
through unchanged.

**Verified by**: L1 — `nil`, `0`, and a negative value each fall back for both functions; a
positive value passes through for both.

### R5.4 — the new `Default*` constants are pinned to their migration `DEFAULT`s

**MUST**: `consolidation.DefaultGoalStagnationDays` equals migration
`0002_learning_and_search.sql:66`'s `config.goal_stagnation_days ... DEFAULT 21`, read off disk.

**MUST**: `consolidation.DefaultMentalLoadThreshold` equals migration
`0002_learning_and_search.sql:67`'s `config.mental_load_threshold ... DEFAULT 7`, read off disk.

**MUST**: `consolidation.BeliefMergeCosine` equals `0.85`, doc 02 §13's existing "Semantic belief
merge" row's value — this one has no schema `DEFAULT` to pin against (it is not a `config` column),
so it is asserted as a literal equality against the documented number, not against SQL text.

**Verified by**: L2, `test/conformance/`, for the two DDL-backed constants; L1 literal assertion
for `BeliefMergeCosine`.

---

## 6. What this spec does not require

Matching `design.md` §11: the runner (reading the clock once, executing `Order()`, persisting,
writing `decision_log`), I11's behavioural half, I12, I03's write path, I24's structural test, the
`nooma consolidate` subcommand, the scheduler, and the learning module are all `m2c`/`m2d`/M5, and
no requirement above depends on them existing. Every requirement in this document is provable
against a repo-constructed input, with no database, no network, and no real clock.
