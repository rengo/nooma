# Design — M2 Phase B: the consolidation core

Technical design for `m2b-consolidation-core`, the second of the four chained changes
[`m2-sleep-weight/proposal.md`](../m2-sleep-weight/proposal.md) §5 splits M2 into. Scope is that
document's **m2b block only** — the five PRs `feat/core-consolidation-order`,
`-expire-archive`, `-strengthen-reweight`, `-connect-derive`, `-pattern-eval`. `m2b` has no
proposal of its own; it inherits scope, risks and rulings from the umbrella.

`m2b` ships one package that is `doc.go`-only today — `internal/core/consolidation` — plus two
small vocabulary additions in `internal/core/relation` and `internal/core/selfmodel` that
`connect` and `derive` need and that no package declares today (§5.3). Zero ports, zero store,
zero `brain`, zero I/O, no clock. Exactly the discipline `m2a` held.

**This design is mostly not about code.** Owner ruling round 2 #1 made M2 a behaviour-*defining*
milestone and `m2b` owns three of its six undefined formulas — **`strengthen`**, **`reweight`**,
and the **incomplete-resolution predicate** — plus the two `pattern_eval` watchers doc 02 §7
names without predicates. §4 decides them. Where a number is chosen rather than derived, the row
says so.

It does not restate requirements — that is `spec.md`, which runs **after** this document and
against the identifiers §6 declares. It does not edit `docs/`; it *describes* the doc 02
amendments each PR will make.

> **Sequencing note, and why this document is written the way it is.** On `m2a`, `sdd-spec` and
> `sdd-design` ran concurrently, never saw each other, and disagreed on four substantive points —
> costing an adjudication round and a reconciliation pass (`m2a/design.md` §8 R15). The
> adjudication's own conclusion was *"parallelise phases that share inputs, not phases that must
> both invent the same thing."* So on `m2b` they are sequential, and this document's §6 is a
> complete declaration of every identifier, signature, constant and error value `spec.md` is to
> write requirements against. If `spec.md` needs a name §6 does not declare, that is a defect in
> this document, not a licence to invent one.

---

## 1. Ground truth this design was verified against

Every row was read at the named file and line in this session.

| Claim | How it was verified |
|---|---|
| `internal/core/consolidation` contains exactly one file, a 4-line `doc.go` with no declarations | `internal/core/consolidation/doc.go` |
| doc 02 §6's phase line is `expire_incomplete → archive → strengthen → connect → derive → reweight → pattern_eval → learn`, and §6 calls each phase "a pure function over the vault, individually invocable" | `docs/02-cognitive-core.md:657-673` |
| doc 02 §6.1 describes **only** promotion ("promoted with what they have"); §1 describes **two** outcomes ("promoted with what it has, or archived if still unresolved, after 24 h") and names the `incomplete → archived` transition explicitly | `docs/02-cognitive-core.md:664` vs `:17-18`, `:25-30` — the self-contradiction proposal §4.2 found, confirmed |
| doc 02 §6.2's archive predicate is `effective_weight < weight_threshold`, strictly less | `docs/02-cognitive-core.md:665` |
| doc 02 §6.3 says `strengthen` "re-evaluates relation **strength** with accumulated evidence" — strength, not confidence | `docs/02-cognitive-core.md:666` |
| doc 02 §6.5 requires **two** dedup defenses: existing beliefs in the prompt **and** semantic merge at cosine ≥ 0.85 | `docs/02-cognitive-core.md:669-670` |
| doc 02 §6.6 is `reweight`: "post-connection weight adjustments (**and optional decay materialization**)" | `docs/02-cognitive-core.md:671` |
| doc 02 §7's two watchers: goal stagnation over `goal_stagnation_days` (21); load accumulation at open `mental_load` units ≥ `mental_load_threshold` (7), writing a `current_state` hypothesis, "**a cooldown of days** after a resolved check-in" — with no number for the cooldown | `docs/02-cognitive-core.md:708-717` |
| doc 02 §4: relation `strength` is 0–1, "0.1 a passing mention, 0.9 the new node IS about that relation"; direction is "what the judge said, not a canonical form"; "a judgment that decided nothing writes nothing" | `docs/02-cognitive-core.md:318-321`, `:350-354`, `:356-359` |
| doc 02 §10: derived beliefs use `topic_key` = `derived/{facet}/{key}`; facets are `identity \| value \| goal \| social \| preference` | `docs/02-cognitive-core.md:757-760` |
| §13 has **33** rows and no row for any strengthen, reweight, connect-budget or cooldown knob; `weight_threshold` (0.5), `goal_stagnation_days` (21 ⚙), `mental_load_threshold` (7) and `Semantic belief merge cosine ≥ 0.85` all exist with **no Go home named** | `docs/02-cognitive-core.md:800-834` |
| `config` declares `weight_threshold REAL NOT NULL DEFAULT 0.5`, `goal_stagnation_days INTEGER NOT NULL DEFAULT 21`, `mental_load_threshold INTEGER NOT NULL DEFAULT 7`, `consolidation_last_run_at TEXT` (NULL = never ran) | `internal/store/sqlite/migrations/0002_learning_and_search.sql:61-69` |
| **`goal_stagnation_days` has two homes in the schema** — the `config` column above *and* `calibration`'s own comment (`key TEXT PRIMARY KEY, -- e.g. 'goal_stagnation_days'`) | `0002:37-40` vs `:66`. New finding — §9 Q3 |
| `relations.strength` and `.confidence` carry **no `CHECK`**; `created_by TEXT NOT NULL DEFAULT 'system' -- system\|consolidation\|user` | `0001_core_tables.sql:35-37` |
| No Go package declares a `created_by` vocabulary. `brain/capture.go:485` writes the bare literal `"system"` | grep over `internal/` |
| `self_beliefs` has `facet`, `topic_key TEXT NOT NULL UNIQUE`, `confidence REAL NOT NULL DEFAULT 0.5`, `origin`, `status`, `last_reinforced_at` — and **no embedding column**, and `unit_embeddings.unit_id` is `REFERENCES units(id)` so a belief's vector cannot live there | `0001:72-85`, `0002:74-80` — confirms proposal R3 / ruling Q2 |
| `weight.Effective`, `weight.Revive`, `weight.Resurface`, `weight.Boost`, `weight.Current`, `weight.Edge`, `weight.Neighbourhood`, `weight.ReviveGain` (0.35), `weight.WeightCeiling` (2.0), `weight.ResurfaceMaxHops` (2), `weight.ResurfaceAttenuation` (0.5) all shipped in `m2a` | `internal/core/weight/{decay,boost,spread}.go` |
| `Revive` returns `(Boost, bool)`; `Resurface` returns `(boosts []Boost, corrupted []string)`, both sorted by `UnitID` | `boost.go:95`, `spread.go:166` |
| `Resurface` validates `Current.Weight`/`DecayRate` and `Edge.Strength` **at the entry point**, not mid-computation, and refuses rather than coerces | `spread.go:183-187`, `:255-276` — C15's ruling, shipped |
| `focus.clamp` is total under `NaN` (maps to `lo`); `focus.Priority` clamps `adjacency` at its own door | `internal/core/focus/priority.go:91-102`, `:233` — C22/C24, shipped |
| `recall.Search` is a pure top-K dot product over a `VectorIndex` that carries a `Model` and refuses a mismatch (`ErrModelMismatch`) — so cosine over unit-normalized vectors comes with I21's model filter attached; `recall.Normalize` is exported, `dot` is not | `internal/core/recall/vector.go:66-141` |
| `recall.FuseScored` is the one implementation of ADR-0010's RRF and its three-level tie-break | `internal/core/recall/fuse.go:72-104` |
| `relation.Resolve(*Thresholds) Thresholds` and `relation.Decide(confidence, Thresholds) Verdict` are the shipped persist decision, with `Discard \| Uncertain \| Asserted` | `internal/core/relation/{thresholds,verdict}.go` |
| `relation.DecodeJudgment` yields a `Judgment` whose `Outcome`, `TargetUnitID`, `Type`, `Strength`, `Confidence` are all **pointers**, so "absent" and "a legitimate zero" are distinguishable | `internal/core/relation/judgment.go:46-59` |
| `unit.ValidateTransition` permits `incomplete → pool` and `incomplete → archived`; `unit.Status.IsLive()` is `status == pool`, positively | `internal/core/unit/{transition,status}.go`, `docs/02-cognitive-core.md:19-30` |
| `docs/06-harness.md` §4 already carries **I11** ("The 8 consolidation phases run in order, and `learn` is always last", §6) and **I12** — so `m2b` adds no new invariant row | `docs/06-harness.md:183-184` |
| The 400-line ceiling is counted as **implementation plus docs, separately from test lines** (amended in PR #142 during `m2a`) | `docs/06-harness.md:336-378` |
| The house pattern for a closed vocabulary is a **function** returning a fresh slice, never an exported `var`, so an importer cannot mutate what a conformance check reads | `unit.AllStatuses`, `classify.AllKinds`, `relation.allOutcomes`, `focus.AllKinds` |
| The house pattern for a `config`-backed default is `Default*` + `Resolve*(*T)` with `nil` meaning "no row", pinned to the column `DEFAULT` by an L2 test that reads the SQL off disk | `relation/thresholds.go:14-38`, `focus/hysteresis.go`, helper `migrationSQLText` at `test/conformance/i13_learning_signal_test.go:24` |

---

## 2. What `m2b` decides, in one paragraph

`core/consolidation` owns **the ordered phase sequence as a type** and **seven pure decision
functions** — `learn` has none, and that absence *is* ruling 3's no-op. Every phase decision
takes data and the pass's single `now`, and returns a **plan** (a transition, a strength change, a
boost, a proposed relation, a merge decision, a finding) that only `brain` can persist. Nothing in
the package takes a repository, a context, a clock, or a provider. Two of the eight phases —
`connect` and `reweight` — are deliberately **thin**: their arithmetic already exists in
`core/recall`, `core/relation` and `core/weight`, and this design's contribution to them is
naming what they reuse and refusing to write a second copy.

---

## 3. The phase sequence as a value — and why I11 becomes structural

I11 is *"the 8 consolidation phases run in order, and `learn` is always last"*. The failure mode it
guards is a runner that reorders, drops, or appends. The weak way to satisfy it is a slice literal
plus a test asserting the slice equals the same literal — which proves nothing, because the test
and the code are the same sentence written twice.

### 3.1 The shape

```go
// internal/core/consolidation/phase.go

// Phase is one of doc 02 §6's eight nightly phases. Its VALUE is its
// position: Phase(0) runs first and phaseCount-1 runs last. The order is
// not data the sequence carries, it is the type's own numbering.
type Phase int

const (
    PhaseExpireIncomplete Phase = iota
    PhaseArchive
    PhaseStrengthen
    PhaseConnect
    PhaseDerive
    PhaseReweight
    PhasePatternEval
    PhaseLearn

    phaseCount // not a phase: the count, and Order's upper bound
)

// Compile-time proof of doc 02 §6's "learn is ALWAYS last" (I11).
// If any phase constant is ever declared between PhaseLearn and phaseCount,
// this expression goes negative and uint() refuses to convert a negative
// constant, and the package stops compiling. Reordering the block so learn
// is not the greatest value fails the same way.
const _ uint = uint(int(PhaseLearn) - int(phaseCount) + 1)

func Order() []Phase                       // fresh slice, Phase(0)..phaseCount-1
func (p Phase) String() string             // "expire_incomplete" … "learn"
func ParsePhase(s string) (Phase, error)   // for `nooma consolidate --phase`, m2c
var ErrUnknownPhase = errors.New("consolidation: unknown phase")
```

with

```go
func phaseNames() [phaseCount]string {
    return [phaseCount]string{
        PhaseExpireIncomplete: "expire_incomplete",
        PhaseArchive:          "archive",
        PhaseStrengthen:       "strengthen",
        PhaseConnect:          "connect",
        PhaseDerive:           "derive",
        PhaseReweight:         "reweight",
        PhasePatternEval:      "pattern_eval",
        PhaseLearn:            "learn",
    }
}

func Order() []Phase {
    order := make([]Phase, 0, phaseCount)
    for p := Phase(0); p < phaseCount; p++ {
        order = append(order, p)
    }
    return order
}
```

### 3.2 Why this makes I11 structural rather than remembered

Four legs, in descending order of strength. **Only the first is a proof; the rest are tests, and
the difference is stated so nobody reads the set as uniformly strong.**

| # | Leg | What it makes impossible |
|---|---|---|
| **1** | The `const _ uint` assertion | **`learn` in any position other than last is a compile error.** Not a failing test — a package that does not build. A test can be skipped, weakened or deleted; this cannot, and it fires on `go vet`, `go build`, every editor, and every CI job at once |
| **2** | Order is the type's numbering, and `Order()` is *generated* from `[0, phaseCount)` | There is **no slice literal listing eight names in an order** anywhere in the tree. Permuting the pass requires renumbering the constants, which simultaneously changes every `String()` output and every `ParsePhase` round-trip — so a reorder cannot be a quiet one-line diff, it is a rename of the whole vocabulary |
| **3** | L2: `Order()`'s names, joined, are pinned to **doc 02 §6's own arrow line, parsed off disk** | The code's order agreeing with the governing document is *checked*, not asserted. This is the non-tautological leg: the expected value is not a literal in the test, it is `docs/02-cognitive-core.md:661`. Same mechanism `relation_thresholds_ddl_test.go` and `i13_learning_signal_test.go` already use against migration SQL — first application to doc 02, and the reason `docs/02-cognitive-core.md` governs stops being a slogan |
| **4** | L2 tree scan: no non-test file **outside** `internal/core/consolidation` may contain two or more of the eight phase-name literals | A runner or a CLI that keeps its own list of phases fails. `m2c`'s runner switches over `Phase` **constants** (legal — that is the type, not a list) and `cmd/nooma`'s `--phase` help text is rendered from `Order()` + `String()` (legal, and the right design anyway) |

Leg 3 also carries the totality check: `phaseNames()` is an **array sized by `phaseCount`**, so a
ninth phase declared before `PhaseLearn` and left unnamed yields `""`, and the test fails on an
empty name rather than on a subtle mis-order.

**What leg 1 does *not* prove**, said plainly: it proves `learn` occupies the last slot. It does
not prove the *runner* executes the slots in `Order()`'s order — that is the behavioural half of
I11 and it belongs to `m2c` (proposal §6.1 row 5 already splits it this way). What `m2b` gives
`m2c` is a sequence with no other way to be read.

**Why there is no `AllPhases()` beside `Order()`.** Two exported spellings of one fact are two
things that can drift (`m1a` D3). `Order()` is the enumeration and the order at once, because for
this type they are the same fact.

### 3.3 `learn` has no function, and the absence is the no-op

Owner ruling 3 ships `learn` as *"a true no-op that still occupies slot eight"*. The literal
reading — `func Learn() {}` — would put an exported declaration in the tree whose only test could
be vacuous, and `test/conformance/core_exported_decls_have_tests_test.go` would be satisfied by a
test that proves nothing.

**Decision: `PhaseLearn` exists; no `Learn` function does.** The slot is the phase constant; the
no-op is that `m2c`'s runner has an arm for it that performs no work and — per doc 02 §11 — writes
no `decision_log` row. The doc 02 §6.8 amendment says so in one sentence, so the next reader does
not go looking for a missing function. M5 fills it.

---

## 4. The formulas `m2b` owes

`m2a` established the standard: a formula's shape must be **entailed** by something already
fixed, not preferred. §4.1 is the entailment three of the four rest on; §4.2–§4.6 are the phases.

### 4.1 One reinforcement law, and why it is not a preference

`m2a` derived the asymptotic form for `Revive` and had it confirmed by adjudication ruling 2:

```
x' = x + gain × (bound − x)
```

The argument it won on was **not** aesthetic. It was that of the three candidate shapes for a
quantity bounded above that receives repeated discrete evidence:

- `min(x + B, bound)` — the clamp is a discontinuity: **every value above the clamp collapses onto
  the same number**, destroying the ordering among exactly the items with the most evidence. On
  `Revive` this manufactured the jitter hysteresis exists to suppress (ruling 2's own finding);
- `min(x × M, bound)` — same clamp, **plus an absorbing state at 0**, which is precisely the value
  a decayed quantity approaches;
- `x + gain × (bound − x)` — bounded by construction, no clamp, no discontinuity, strictly
  increasing in `x` (so it preserves the ordering it acts on), with a diminishing increment.

**Every quantity `m2b` reinforces has the same three properties**: bounded above (relation
`strength` ∈ [0,1] by doc 02 §4; belief `confidence` ∈ [0,1] by doc 02 §10; `weight` by
`WeightCeiling`), receiving repeated discrete evidence, and needing its ordering preserved. So the
shape is **inherited, not re-derived** — the only per-instance decision is the gain, and the only
thing this design has to justify per formula is that number.

This is stated as a section rather than repeated three times because it is the single reason
`m2b` introduces no new *shape*: three of its formulas are one law at three gains, and the fourth
(`reweight`) is not a formula at all (§4.5).

### 4.2 `expire_incomplete` — the predicate that resolves doc 02's contradiction

**The contradiction, verified.** §1 gives two outcomes at 24 h ("promoted with what it has, or
archived if still unresolved") and names the `incomplete → archived` transition as the one the
status vocabulary reserves for it. §6.1 gives one ("promoted with what they have"). Per
`CLAUDE.md` non-negotiable #1 this is fixed in the PR implementing the phase.

**The resolution: §6.1 gains §1's missing branch, and promotion is the default.** The predicate:

```
elapsed = now − created_at                                 (clamped at 0)
elapsed <  IncompleteExpiryHours   → no decision
elapsed >= IncompleteExpiryHours   → incomplete → archived   when Unresolved
                                   → incomplete → pool       otherwise
```

Three decisions inside that, each argued:

**`created_at`, never `last_touched_at`.** Identical reasoning to `focus.AgeRamp` (`m2a` §3.1):
`last_touched_at` is resettable by use and `created_at` is not, and the quantity here is *how long
the ambiguity has stood*, which no boost should be able to reset. One rule, now in three places
(`AgeRamp`, this, and §4.6's stagnation window).

**Promotion is the default; archival is the exception, and the exception must be evidenced.**
This is a choice and it is the one the owner should look at. The alternatives:

| Option | Verdict |
|---|---|
| Archive by default; promote only when resolved | Rejected. Nothing in M2 can record a resolution (ruling Q3), so this makes M2's shipped behaviour "silently cool everything the user wrote that was ambiguous" — the opposite of "cautious to capture", and a behaviour §6.1's own current text contradicts |
| Infer resolvability from vault data — the unit has gained a relation, or its `confidence` is high | Rejected, and worth naming because it is the plausible-sounding one. I06 says an `incomplete` unit has **no embedding until promoted**, so it can never be a recall candidate, so capture's judge never proposes edges for it: *every* incomplete unit has zero relations, and the inference would archive all of them while looking principled. `units.confidence` is nil on every row M1 writes (`unit.Unit.Confidence`'s own comment) |
| **Promote by default; archive only when a caller says the ambiguity was put to the user and not settled** — chosen | Matches §6.1's shipped text, implements §1's branch, and puts the one fact core cannot compute where core cannot invent it: a field |

**`Unresolved` is a field `m2b` declares and does not produce.** Exactly `m2a`'s move with
`weight.Boost` — *"`m2a` does not add the port; it fixes the shape the port must have"*. In M2
`m2c` passes `false` for every unit, because no column and no surface can set it (ruling Q3), and
the archive branch is proven against a repo-constructed input, which proposal R11 already accepts.
**This is §9 Q1** — the owner may prefer to drop the field and defer §1's branch to M3 outright;
this design recommends keeping it, because a branch doc 02 §1 states and the code cannot express is
the divergence non-negotiable #1 forbids.

**`IncompleteExpiryHours = 24` is quoted, not chosen.** doc 02 §1 fixes it. It gains a §13 row so
the one number the phase has is calibratable in the one place §13 promises.

### 4.3 `strengthen` — Hebbian co-use, and a gain chosen, then checked for compatibility against two numbers doc 02 already fixes

**What counts as evidence.** doc 02 §6.3's word is *accumulated* — evidence that accrues *between*
passes. Enumerating what actually accumulates in the vault between two consolidations without a
new provider call: unit `last_touched_at` writes (doc 02 §2's discrete events), new `relations`
rows, new `learning_signals` rows, new units. Of those:

- `learning_signals` are the **`learn` phase's** input (doc 02 §9), and M2 does not consume them
  (ruling 3). Using them in `strengthen` would put slot eight's job in slot three.
- New relations are `connect`'s output, and `connect` runs **after** `strengthen`. Using them here
  would make the phase order wrong or the phase idempotent-hostile.
- New units have no relations yet.

That leaves **co-use**: both endpoints' `last_touched_at` moved since the previous pass. And doc 02
§2 already defines a `last_touched_at` write as "direct use", so the evidence is not invented — it
is the only accumulating quantity the document already gives a meaning to.

> **The entailment, stated once:** `connect` creates a relation from *one* judgement at *one*
> instant. `strengthen` exists because one judgement is thin. The only thing the vault accumulates
> between passes that bears on whether two units belong together is that the user kept using both.

**The rule.** §4.1's law, with the co-activation predicate as the gate:

```
co-active(r) = !from.LastTouchedAt.Before(since) && !to.LastTouchedAt.Before(since)

s' = s + StrengthenGain × (1 − s)      when co-active
   = (no change, no row)               otherwise
```

**`strengthen` never lowers a strength, and that is entailed rather than lazy.** doc 02 §4 gives
exactly two ways a relation's strength moves downward: the user rejects it, and then the relation
is **deleted** and a `relation_reject` signal is emitted (I10) — there is no "weaken" outcome
anywhere in the document. A nightly decay on relations would be a *third* mechanism doc 02 does not
have, and a second decay model beside §2's. Declining it costs nothing observable: a relation
between two units that stop being used becomes invisible when `archive` cools the units.

**`confidence` is untouched.** §6.3 says *strength*. Confidence is the judge's certainty about its
own claim; the user using two things together does not make a past model call more certain. This is
also what keeps `strengthen` from silently moving a relation across `min_confidence_to_surface`
(I09's band), which is a *learning* decision (§9, M5).

**`since` is a `*time.Time`, and `nil` means "no evidence at all".** `consolidation_last_run_at` is
`NULL` on a vault that has never consolidated (`0002:68`). Treating that as "the epoch" would
strengthen **every relation in the vault on the first pass** — evidence conjured from the absence
of a previous pass. "Accumulated evidence" over no interval is no evidence, so `Strengthen(es,
nil)` returns nothing. Same `*T`-as-sentinel idiom as `relation.Resolve` and `focus.ResolveMargin`.

#### `StrengthenGain = 0.10` — chosen, and checked for compatibility, not derived

`StrengthenGain` is a **chosen** constant. Labelling it derived was this design's own defect
(Judgment Day, both blind judges, independently): the arithmetic below solves *how many nightly
passes* a given gain takes, not *which gain* a fixed count of nights entails — it runs the
entailment backwards, and running it forwards does not pin a single value.

doc 02 §4 fixes the two ends of the strength scale in prose: **0.1 is "a passing mention"** and
**0.9 is "the new node IS about that relation"**. For a gain `g`, the number of unbroken nightly
co-use passes it takes to carry a relation from the first to the second is:

```
n(g) = ceil( ln(0.1 / 0.9) / ln(1 − g) )
```

At `g = 0.10`, `n(0.10) = ceil(−2.1972 / −0.10536) = ceil(20.85) = 21`, which happens to equal
`DefaultGoalStagnationDays`. **That is a compatibility check, not a derivation**: inverting the
formula for `n = 21` gives `g ≥ 1 − (1/9)^(1/21) ≈ 0.0994`, and any gain in roughly
`[0.0994, 0.1040)` produces the same `n(g) = 21` — a range the L1 test, which only asserts the
identity at the chosen value, cannot discriminate between. `0.10` is a round number picked from
inside that admissible range, not the unique value the two prose anchors entail.

**The pin is to `DefaultGoalStagnationDays`, the default, not to the live per-user value, and that
distinction matters because the knob on the other side of the check personalizes and this one
cannot.** `goal_stagnation_days` is marked ⚙ in doc 02 §13: the learning module recalibrates it per
user — "systematically ignoring lengthens the interval (21 → 28 days)" (doc 02 §9). `StrengthenGain`
is a fixed Go constant that reads no config at runtime (§5.4's own "no clock, no config" list
already states `Strengthen` takes none). So the compatibility check holds against the *default* 21
and goes silently stale the first time a real user's horizon personalizes away from it: the L1 test
keeps passing — it asserts against the constant, never against any resolved per-user value — while
the informal claim "co-use carries a relation from 0.1 to 0.9 in about as long as this user's
stagnation horizon" quietly stops being true for that user. Recorded here rather than hidden behind
the word "derived".

Checked from both sides at the default: at n = 20, `s = 1 − 0.9·0.9²⁰ = 0.8906` — below 0.9; at
n = 21, `s = 1 − 0.9·0.9²¹ = 0.9015` — at or above. **The L1 test asserts this identity against
`DefaultGoalStagnationDays` rather than against the literal 21**, so a change to either constant
breaks the check loudly instead of silently decoupling — that discipline survives the relabeling
above.

The *reason* the default interval is a reasonable one to check against, since the arithmetic alone
admits a whole range: doc 02 §9 makes "how often does the user delete relations from the nightly
job" **the** quality metric for `connect`. A gain that promoted a passing mention to an assertion in
three nights would outrun the user's ability to notice and reject it, and the metric would measure
the gain rather than the judge. Twenty-one nights is the horizon the product already uses, by
default, for "you have had a fair chance to react".

Two properties worth stating because they are testable: **strength never reaches 1** (asymptotic),
and a relation already at 1 produces **no row** (§11 — a decision with no effect writes nothing).

#### Entry-point validation — C15/C22/C24's rule, applied at `Strengthen`'s door

`relations.strength` is an LLM-judge output (`relation.Judgment.Strength`, a `*float64` validated
only as *a number*) stored in a column with **no `CHECK`**. `m2b` inherits `m2a`'s hard-won rule
without re-learning it: **validate where the value enters, before any comparison can skip past it.**

`Strengthen` refuses — it does not coerce — a `Strength` that is `NaN`, `±Inf`, **or finite but
outside [0,1]**, and reports the relation id through a second return value, exactly
`Resurface`'s `corrupted` shape. Two reasons the refusal extends to the out-of-domain finite case,
where `m2a`'s `clampStrength` merely clamps:

1. `StrengthChange` is a value a caller **persists**, like `Boost` and unlike `Effective`'s
   transient read result. `m2a` drew that line itself (`Revive` refuses, `Effective` does not), and
   this is the same side of it. A coerced value written back makes the corruption durable, and
   nothing in the vault is ever deleted.
2. doc 02 §4's own reading, quoted in `clampStrength`'s comment: *"a strength above 1 is not a
   stronger relation, it is a corrupt one."* Repairing it silently is a decision nobody asked for;
   refusing leaves it visible for `doctor`.

`m2a`'s C19 recorded that `Edge.Strength = +Inf` is coerced to 1 by `clampStrength` rather than
refused — the only non-finite input in `core/weight` that is. `m2b` does not reach into
`clampStrength` to change a shipped contract; it makes its **own** doors consistent (§4.5 does the
same for `reweight`'s edges), which is what the entry-point rule actually asks for. C19 stays a
`core/weight` record.

### 4.4 `connect` — the phase whose design is what it does *not* contain

Proposal §4.4 is explicit: `connect`'s candidate search is `core/recall`'s fusion and its persist
decision is `core/relation`, unchanged, because **two implementations are two biases** and ADR-0010
warns that a bias here propagates into the whole relation graph. So this subsection's job is to
name the reuse precisely enough that an implementer has nothing to improvise.

| `connect` needs | Comes from | New code |
|---|---|---|
| Rank the two recall legs into one candidate list | `recall.FuseScored(vectorIDs, lexicalIDs)` — ADR-0010's RRF and its three-level tie-break | **none** |
| Decide whether a judged pair is stored | `relation.Decide(confidence, relation.Resolve(row))` — I08's own path, `Discard` stores nothing | **none** |
| Tolerantly read the judge's answer | `relation.DecodeJudgment` — I14's mechanism | **none** |
| Vector leg, model-filtered | `recall.Search` — I21 attached | **none** |
| Which units this pass asks about, and how many candidates each gets | — | `SelectConnectSources`, `ConnectPairs` |
| Turn a judgement into a plan `brain` can persist with `created_by='consolidation'` | — | `ProposeRelation` |

**Source selection reuses `since`, and that is a cost decision, not a taste one.** A nightly pass
that re-proposes connections across the whole vault every night burns N provider calls to
re-derive last night's answer. The units worth asking about are the ones that changed. Same
parameter `strengthen` already takes, so the pass has **one** notion of "since the last sleep"
rather than two. On the first pass (`since == nil`) the whole live pool is eligible — which is
right: the first consolidation of an existing vault is exactly when the graph should be built.

**And that is why the bound is a named constant rather than "however many units there are".**
`ConnectSourceLimit = 20` sources × `ConnectCandidateK = 5` candidates each = **at most 100 judge
calls per pass**, and *that product* is the number the owner is really calibrating. It goes into
doc 02 §6.4 in those words, because a per-night provider budget stated as a product is auditable
and two separate knobs are not.

- **`ConnectSourceLimit = 20`** — chosen. Sources are ranked by `weight.Effective` descending (tie
  by id, for a total order under `-shuffle=on`), so the cap takes the most alive.
- **`ConnectCandidateK = 5`** — chosen, and deliberately **equal to `relation.DedupCandidateK`**
  because it is the same human question ("how many candidates is one judge call shown"), declared
  as a **separate §13 row** for `m2a`'s own stated reason (`urgency_lead_days` vs "Event lead
  time"): collapsing two knobs because they start equal is how a calibration table becomes
  un-tunable. Capture's budget is per message; this one is per night.

**Pairs already joined by an edge are not re-asked, and the key is the *unordered* pair.** doc 02
§4 says direction is what the judge said and that two units related both ways hold two rows — so an
existing `a→b` does not formally preclude asking about `b→a`, nor about a second relation *type*
between the same two units. Excluding by unordered pair regardless of type is a **choice**: it
spends a possible second relation type to save a judge call, on a pass whose cost is the thing
§6.4 is bounding. It is reversible by widening the key to `(pair, type)` and it is recorded in §9.
The proposed edge itself still runs `source → candidate`, per §4's direction rule — so the
canonical form is used for *lookup only*, never for storage. Two uses, two spellings,
`CanonicalPair` for the first and `Pair{From: source, To: candidate}` for the second.

**`ProposeRelation` emits nothing for a judgement that decided nothing** — doc 02 §4's own
sentence, and `capture`'s shipped behaviour: outcome `new`, or any of `TargetUnitID`/`Type`/
`Strength`/`Confidence` absent after tolerant decode, produces no plan and no `decision_log` row.
`Uncertain` **does** produce a plan (I09: the band is stored *and* asked about; the asking is M3's).

### 4.5 `reweight` — no new formula, and one option deliberately declined

doc 02 §6.6 is two things joined by "and optional": post-connection weight adjustments, **and**
optional decay materialization. They get opposite answers.

#### (a) The post-connection adjustment is spreading activation over the edges this pass created

A relation created tonight is evidence about **both** its endpoints: the judge just said this unit
is about that one. doc 02 §2 already has a mechanism for "a related signal lifts a unit" —
spreading activation — and `m2a` shipped it, hardened it across three adversarial rounds, and
pinned its two-hop reach to the archive threshold. Writing a second weight-adjustment formula here
would be a second bias in the same place ADR-0010 warns about, one layer down.

**So `reweight` is `weight.Resurface`, run once per newly-connected unit, over the pass's new edges
and no others, with the per-unit results merged by `max`.**

```
origins  = every endpoint of an edge connect created this pass
for each origin: weight.Resurface(Neighbourhood{origin, states, newEdges}, now)
merge:   per UnitID, the highest boosted weight — the same max rule Resurface
         already uses for multi-path gain and focus.AdjacencyStrengths uses for
         graph evidence
```

Four consequences, all good, all stated because they are what makes the reuse defensible rather
than lazy:

- **`reweight` introduces zero new calibration constants.** It inherits `ReviveGain`,
  `WeightCeiling`, `ResurfaceMaxHops` and `ResurfaceAttenuation`. That is the strongest available
  evidence that it is the same mechanism and not a coincidence of shape.
- **The gain scales the target, never the step** — `m2a` §3.3's hardest choice, inherited whole.
  A unit adjacent to a busy node cannot converge on the ceiling; propagation caps *where* it can
  hold a unit.
- **It is bounded, once the origins multiplier is counted.** New edges ≤
  `ConnectSourceLimit × ConnectCandidateK` = 100, so origins ≤ 200, and **each** of those ~200
  `Resurface` calls re-runs `buildAdjacency` over the same ≤100-edge set. `buildAdjacency` is
  O(edges) per call and `spreadGains` is O(branching²) per call — additive, not multiplicative — so
  the whole phase is O(origins × (edges + branching²)) ≈ O(200 × (100 + branching²)) with
  `ResurfaceMaxHops = 2`, not O(100 × branching²) and not O(origins × edges × branching²) either.
  The bound is still `connect`'s budget, already named; it is just larger than the edge count alone
  suggests.
- **A two-hop reach through two *new* edges is attenuated to 0.25 and targets 0.5** — exactly
  `m2a`'s stated guarantee that propagation alone cannot lift a unit above the archive threshold.
  It composes.

**The semantic difference is real and is flagged, not buried.** `Resurface`'s doc comment says the
origin "already received its direct revive" — here nothing was directly used; the origin's warrant
is a judge's new edge rather than a user's touch. The mechanism is unchanged and the *cap* is what
makes it safe either way, but this is a use `m2a` did not anticipate. **§9 Q4**, owner-review.

**`m2b` is `Resurface`'s first caller, so `m2a`'s C18 handoff lands here, not on `m2c`.** C18: a
duplicate `UnitID` in `Neighbourhood.States` silently masks corruption, and which reading survives
depends on slice order. C18's own instruction was *"either guarantee uniqueness at the query and
say so, or reject a duplicate outright — choosing silently is what this entry exists to prevent."*
**Chosen: make it unrepresentable.** `Reweight` takes `map[string]weight.Current`, not a slice, and
builds `Neighbourhood.States` from it **sorted by `UnitID`** so the value handed to `Resurface` is
deterministic regardless of map iteration order. C18 closed at the type level, at the first caller,
for free.

**C20/C21 (`corrupted` is not scoped to the origin's reachable component) do bite here, and the fix
belongs at `Reweight`'s merge, not inside `Resurface`.** `reweight` passes one shared, unfiltered
`newEdges` set — up to `ConnectSourceLimit × ConnectCandidateK` = 100 edges — to *every* origin's
`Resurface` call, across up to 20 independent, possibly disjoint `connect` sources, over up to
~200 origin calls. `buildAdjacency` builds its corrupt-edge map over the **entire** edge list it is
handed, and `Resurface`'s reporting loop gates only on origin/refused/gains-reachability, never on
graph distance from that call's own origin. So a single `NaN`-strength edge anywhere in the pass's
batch is reported by every origin call that does not otherwise explain that unit — not only by the
call nearest the corruption. `reweight` inherits this rather than closing it inside `Resurface`,
because the shared-batch shape is `reweight`'s own choice (§4.5(a) above), and the same
reuse-not-reimplement argument this section already makes for the boost formula applies to the
refusal path too: the fix is how `Reweight` merges `corrupted` across its ~200 calls (below), not a
change to a shipped, hardened function. Boosts are unaffected by this: `spreadGains` only walks
neighbours reachable from each call's own `Origin`, so a call that cannot reach a unit never boosts
it, corrupted or not.

**`corrupted` merges by union, deduplicated and sorted — never by count, and this is stated because
`Reweight`'s doc comment was silent on it.** The doc comment already says how `boosts` merge across
origins (the highest boosted weight); it said nothing about `corrupted`. Decided: a unit id appears
in `Reweight`'s output `corrupted` at most once, regardless of how many of the ~200 origin calls
flag it — the same "reported once" property a single `Resurface` call already gives internally
(`corrupted` is a plain, deduplicated slice, not a per-source count), extended across the batch
rather than re-invented. §6.6 states this so `spec.md` does not have to invent a merge rule
`sdd-design` left unnamed.

**A unit id can appear in both `boosts` and `corrupted` from the same `Reweight` call, and neither
suppresses the other — this is decided, not left for `sdd-tasks` to invent.** `Resurface`'s own
single-call contract makes the two mutually exclusive per call: a unit that gets no boost is either
unreached (no effect, no entry anywhere) or refused (an entry in `corrupted`), never both, because
one call has one origin and one edge set. That guarantee is exactly what a shared, unfiltered
`newEdges` batch across ~200 origin calls breaks: a unit legitimately boosted by origin A's valid
two-hop path can simultaneously be flagged by origin B, whose only path to that unit ran through the
same batch's bad edge. **Decided: report both.** `boosts` answers "did at least one origin move this
unit's weight"; `corrupted` answers "did at least one origin fail to explain this unit because an
edge in the shared batch was unusable" — both are true, independent facts about the pass's data
health, and this repo's own posture is that a corruption is refused and surfaced, never silently
repaired or silently dropped (§4.3's entry-point rule; C15/C22/C24). Letting a legitimate boost
cancel the flag would make a real data-quality signal disappear behind an unrelated origin's better
luck — the corrupted edge is still corrupt, and `doctor` still needs to see it. Scoping each origin's
edge set so the overlap cannot arise was considered and rejected for M2: it changes `Reweight`'s
shape from one shared batch to per-origin edge sets, which costs a second pass over the candidate
graph per origin to compute reachability before calling `Resurface` at all — a real cost, paid to
avoid reporting a fact that costs nothing to report honestly instead. `m2c` reads a unit id present
in both outputs as two separate, simultaneously true findings for `decision_log`, not one resolved
into the other.

**C17 is collected on the way past.** `spread.go`'s `refused[unitID]` guard is provably dead
(`refused ⊆ gains`, and the next line already skips everything in `gains`) and C17's ruling was
*"a later link should delete it and confirm the package stays green"*. The `strengthen-reweight` PR
is that link — it is the PR that makes `Resurface` reachable — and the deletion is three lines.

#### (b) Decay materialization is declined, and doc 02 §6.6's "optional" is resolved to "not taken"

This is the design's most consequential reversal and it gets the full argument.

Materialization means rewriting a unit's `(weight, last_touched_at)` pair as `(Effective(...), now)`.
That is **effective-weight-neutral by construction** — the two pairs denote the same curve, which
`m2a` proved and tests at L1 for `Revive`'s ceiling branch. Neutrality is not the problem.

The problem is `last_touched_at`, and it cannot be sidestepped by writing only `weight`: I24
requires that a weight write move `weight` and `last_touched_at` together (proposal §4, `m2a`'s
definition, `docs/06-harness.md` §4), so any materializing write — bulk or single — touches the
timestamp whether or not that is what the write is really about. `m2a`'s adjudication ruling 2
settled, and doc 02 §2 now **states**, that `last_touched_at` is not only a decay anchor:

> *"`last_touched_at` is the vault's record of **direct use**, and it is read as one."*

That reading is load-bearing in shipped code and shipped prose: `focus.AgeRamp` exists *because*
`last_touched_at` is reset by use while `created_at` is not (which is what keeps the age term from
being decay under a second name); `Revive` writes the timestamp even when it cannot raise the
weight, precisely so a direct use at the ceiling is still recorded; and §4.3 above reads it a third
time as co-use evidence.

**A nightly bulk write that moves `last_touched_at` for every sufficiently old unit makes that
reading false for exactly the units where it matters** — the untouched ones. Every future "not
touched in N days" question would misread them, `strengthen`'s co-activation predicate would see
the consolidation pass as a user, and `m2a`'s ruling-2 argument would have to be retracted one
milestone after it was taken.

| Option | Verdict |
|---|---|
| Materialize, accept that `last_touched_at` is only a decay anchor | Rejected. It reverses a decision the owner took on `m2a` and that is now written into doc 02 §2, and it silently breaks `strengthen`'s own evidence predicate three phases later in the same pass |
| Materialize into a separate `last_decayed_at` column | Rejected **for M2**, on cost and sequencing, not on an appeal to settled authority: it needs a new migration, and whether the **consolidation** half of M2 may take one is **R1, and it is open** (`m2-sleep-weight/proposal.md:373-375`) — ruling 2 closed that question only for the *scheduler* half, and Q1's option C is about seeding **existing** `config` columns' defaults on a row-less vault, not about a new column for a new purpose. This is the honest form of the feature; revisit it once R1 resolves, not because a ruling forbids it now |
| **Do not materialize; amend doc 02 §6.6 to say the option is not taken and why** — chosen | Costs nothing observable (the read path already computes decay correctly — that is I05), removes a `reweight` code path with no consumer, and **makes proposal R13 moot**: I05's structural half no longer has to be carefully scoped around a permitted-but-unused bulk write. This verdict stands on I24 plus the "record of direct use" reading above, independent of R1's status |

**The literal doc 02 §6.6 replacement, so `spec.md` locks in wording rather than re-deriving this
section's argument.** §6.6 currently reads *"post-connection weight adjustments (and optional decay
materialization)"*; this PR amends it to:

> *"post-connection weight adjustments (decay materialization remains optional and is not exercised
> by M2's `reweight`)."*

This keeps the "optional" wording the reconciliation below depends on, states plainly that M2 does
not exercise it, and names the phase that could without implying `reweight` ever will on its own.

Consequences to carry forward, stated so they are not discovered: this **narrows** proposal R13 and
`m2a` §9's last bullet, both of which assumed materialization would ship. `m2c` still scopes I05's
structural test to read paths — that scoping is correct independently — but it no longer has to
leave a hole for a feature nobody calls. **§9 Q2**, owner-review, because it declines something doc
02 currently permits.

**Reconciled against proposal §2's own exit criterion, explicitly** — every other load-bearing claim
in this document is line-cited, and this one was not. That checkbox reads: *"`effective_weight` is
computed on read … while consolidation's optional bulk decay materialization — which doc 02 §2 and
§6.6 explicitly permit — remains legal"* (`m2-sleep-weight/proposal.md:47-49`). **"Not taken,
revisit with a future migration" satisfies "remains legal."** The checkbox requires that the *option*
still exist in doc 02, not that `m2b` exercise it: this design's own doc 02 §6.6 amendment records
that materialization is not taken *in M2*, and does not withdraw the "optional" wording the section
keeps — a future consolidation half that takes migration 0003 (once R1 resolves) can still
materialize without doc 02 changing again. Nothing here requires the exit criterion's wording to
change; declining to exercise a legal option is not the same as making it illegal.

**This reconciliation is itself provisional on Q2, not a second settled fact.** Q2 — decline in M2,
or materialize after all — is flagged owner-review two paragraphs above, and the "remains legal"
reading here assumes Q2 lands where this design recommends. Most owner outcomes on Q2 still leave
"remains legal" true even if reversed: an owner who chooses to materialize in M2 after all keeps the
option legal by exercising it, which trivially satisfies the checkbox too. The one outcome this
reconciliation does not survive is an owner ruling that strikes the "optional" wording from doc 02
§6.6 outright — at that point the option no longer exists to remain legal, and this paragraph needs
redoing, not restating.

### 4.6 `pattern_eval` — two watchers, each half-satisfied on purpose

Proposal §3.3 is explicit: `pattern_eval` produces the *hypothesis* and nothing delivers it. So
each watcher's output is a **finding**, not a trigger and not a digest entry.

#### Goal stagnation

doc 02 §7: a `goal`-facet belief with **no related activity** for `goal_stagnation_days` (21 ⚙).

"Related activity" needs a purely computable reading, and the schema offers exactly one:
`self_beliefs.last_reinforced_at`. A belief is reinforced when `derive` re-derives it from a unit —
so the column *is* "activity related to this belief", and it is the only such column
(`source_unit_id` is a single nullable FK to the belief's origin, not an activity log).

**And this reading is only sound because of the phase order**, which is worth stating as the first
concrete payoff of I11 being more than bookkeeping:

> `derive` runs at slot five and `pattern_eval` at slot seven. The stagnation watcher therefore
> reads a `last_reinforced_at` that **this same pass** has already refreshed. Run `pattern_eval`
> before `derive` and every belief the pass was about to reinforce looks stagnant for one more
> night. The order is not alphabetical or historical; it is a data dependency.

```
stagnantFor = now − last_reinforced_at          (clamped at 0 — the saturate rule again)
stagnant    = facet == goal && stagnantFor >= stagnationDays × 24 h
```

Boundary: `>=`, per doc 02 §7's own "for `goal_stagnation_days`". Future `last_reinforced_at`
(clock skew, backdated import) clamps to zero elapsed and is not stagnant — the same
saturate-rather-than-invert rule `Effective`, `AgeRamp` and §4.2 already apply. Four places, one
rule, stated once here and cross-referenced rather than re-argued.

#### Load accumulation

doc 02 §7: open `mental_load` units **≥** `mental_load_threshold` (7) → a tentative `current_state`
hypothesis, **"a cooldown of days after a resolved check-in"** — with no number.

The count arrives as an `int` from the live-count-by-type method owner ruling 6 already assigned to
`UnitRepo`; core does not filter a unit list, because counting live units of one type is a query,
not a decision, and passing a slice to count it would put an unbounded read in a phase that needs
one integer.

```
fires = openMentalLoad >= threshold
     && (lastHypothesisAt == nil || now − *lastHypothesisAt >= LoadCooldownDays × 24 h)
```

Above threshold but inside the cooldown → **nothing**, per doc 02 §11: a decision with no effect
writes nothing. That branch is why the cooldown is in core and not in `brain`.

**`LoadCooldownDays = 7` — chosen, not derived**, and labelled as such. The constraint that shaped
it: the cooldown must be long enough for the user's answer to change the count, and the count
changes when they close loops, which is a days-to-weeks activity. Seven is the shortest interval
the product already treats as a planning horizon. It is **not** related to `mental_load_threshold`
= 7 — one is a count and one is a duration — and no test ties them, exactly as `m2a` said of
`focus_size` and the same knob.

**Whose resolution starts the cooldown is not decided here.** doc 02 §7 says "after a *resolved*
check-in", and resolution is a `state_confirmed`/`state_denied` learning signal — M5. In M2 the
only instant available is the last `current_state` row's `recorded_at`. `m2b` takes
`lastHypothesisAt *time.Time` and says nothing about which instant `m2c` maps into it. **§9 Q6.**

---

## 5. Package layout, dependency map, and how `now` travels

### 5.1 Layout

```
internal/core/consolidation/          PR 1..5 — pure; stdlib + internal/core/* only
  ├── doc.go          PR 1  rewritten: the eight phases, learn's absence, §13 rows
  ├── phase.go        PR 1  Phase, the 8 constants, phaseCount, the const _ uint
  │                         assertion, phaseNames, Order, String, ParsePhase,
  │                         ErrUnknownPhase
  ├── transition.go   PR 2  Transition, Reason and its vocabulary
  ├── expire.go       PR 2  IncompleteExpiryHours, Incomplete, ExpireIncomplete
  ├── archive.go      PR 2  DefaultWeightThreshold, ResolveWeightThreshold,
  │                         Cold, Archive
  ├── strengthen.go   PR 3  StrengthenGain, RelationEvidence, StrengthChange,
  │                         Strengthen
  ├── reweight.go     PR 3  Reweight (no new constants — §4.5)
  ├── connect.go      PR 4  ConnectSourceLimit, ConnectCandidateK, Source, Pair,
  │                         CanonicalPair, ProposedRelation, SelectConnectSources,
  │                         ConnectPairs, ProposeRelation
  ├── derive.go       PR 4  BeliefMergeCosine, BeliefReinforceGain, Belief,
  │                         BeliefVector, MergeDecision, DeriveTopicKey,
  │                         MergeProposals, Reinforce
  ├── patterns.go     PR 5  DefaultGoalStagnationDays, ResolveGoalStagnationDays,
  │                         DefaultMentalLoadThreshold, ResolveMentalLoadThreshold,
  │                         LoadCooldownDays, StagnationFinding, LoadFinding,
  │                         EvaluateStagnation, EvaluateLoad
  └── *_test.go       PR 1..5  L1 tables — most of m2b's coverage numerator
      imports: errors, fmt, math, sort, time,
               core/{unit, weight, recall, relation, selfmodel}

internal/core/relation/
  └── createdby.go    PR 4  CreatedBy, CreatedBySystem/Consolidation/User,
                            AllCreatedBy — §5.3

internal/core/selfmodel/
  ├── doc.go          PR 4  rewritten
  └── facet.go        PR 4  Facet, the five members, AllFacets, ParseFacet,
                            ErrUnknownFacet — §5.3

test/conformance/     PR 1..5 — L2, untagged
  ├── i11_consolidation_phase_order_test.go   PR 1  legs 2, 3 and 4 of §3.2
  └── consolidation_defaults_ddl_test.go      PR 2, 5  the four Default* constants
                                                    pinned to 0002's config DEFAULTs

docs/02-cognitive-core.md   §6 amended in every PR; §1/§7/§10 in PR 2/5/4;
                            §13 gains 6 rows and annotates 4 (33 → 39)
docs/06-harness.md          untouched — I11 and I12 already have their §4 rows
```

`docs/06-harness.md` §1's tree already lists `consolidation/`, `selfmodel/` and `relation/`, so
`m2b` adds **no directory** and needs no preflight tree PR.

### 5.2 Dependency-rule check

`internal/core/consolidation` imports `errors`, `fmt`, `math`, `sort`, `time` and five
`internal/core` packages — all inside `depguard`'s `core-purity` allow-list (`$gostd` +
`internal/core`). No file calls `time.Now`, `time.Since`, `time.Until`, `rand.*`, `uuid.*` or
`os.Getenv`; `forbidigo` bans those **by call pattern**, so `time.Time` values and fields are
legal. Nothing imports `internal/ports`, `internal/store`, `internal/brain` or `internal/scheduler`.
No timezone is read: every elapsed time is a duration ratio (`Sub(...).Hours() / 24`), never a
calendar-day count.

Arrows, and there is no cycle: `consolidation → {unit, weight, recall, relation, selfmodel}`;
`weight → unit`; `focus → {unit, weight}`; `relation → classify`; `recall → ∅`. `consolidation`
does **not** import `focus`, and that is deliberate — proposal §4.3's finding is that nothing in
M2's eight phases reads a focus, and `connect`'s "recent/hot units" is read as `weight.Effective`
descending, which needs no focus (`weight.ZoneOf`'s Hot arm is defined *in terms of* focus
membership and would drag the whole read side into the sleep side to select 20 ids).

### 5.3 The two vocabulary additions, priced rather than smuggled

Both are `unit.AllStatuses()`'s shape verbatim: a defined string type, its members, a fresh-slice
`All*()` function (never an exported `var`), a `Parse*` entry point from untrusted text, and a
sentinel error.

- **`relation.CreatedBy`** — `connect` must plan a relation with `created_by='consolidation'`
  (proposal §4.4). Today the column's three-value vocabulary exists only as a SQL comment, and
  `brain/capture.go:485` writes the bare literal `"system"`. Declaring it in `core/relation` is
  ~30 lines and puts the closed vocabulary where the other relation vocabularies live. Adopting it
  at `capture.go:485` is a one-line `brain` change and is therefore **`m2c`'s**, recorded in §8.
- **`selfmodel.Facet`** — the stagnation watcher selects `goal`-facet beliefs. The alternative is
  for `brain` to pre-filter and for core's function to be named `EvaluateStagnation(goals []Belief,
  …)`, which makes goal-ness a promise the caller remembers. `internal/core/selfmodel` is the
  package named for exactly this and is `doc.go`-only today.

Both are ~65 lines together and are called out here so `sdd-tasks` does not treat them as scope
creep discovered at apply time.

### 5.4 How `now` travels

```
brain (m2c, not in this change)                   core/consolidation (m2b)
  now   := clock.Now()   ── once per pass ──┐
  since := cfg.ConsolidationLastRunAt        │   (*time.Time; nil = never ran)
                                             ├─1→ ExpireIncomplete(us, now)
                                             ├─2→ Archive(cs, ResolveWeightThreshold(cfg), now)
                                             ├─3→ Strengthen(es, since)          ← no clock at all
                                             ├─4→ SelectConnectSources(ss, since, now)
                                             │      → recall.FuseScored → ConnectPairs
                                             │      → judge (brain) → ProposeRelation
                                             ├─5→ MergeProposals(model, existing, proposed)
                                             │      → Reinforce                  ← no clock at all
                                             ├─6→ Reweight(states, newEdges, now)
                                             │      └→ weight.Resurface(…, now)
                                             ├─7→ EvaluateStagnation(bs, days, now)
                                             │    EvaluateLoad(n, thr, lastAt, now)
                                             └─8→ (nothing — ruling 3)
```

Two properties read off this: **the instant enters once and travels as a value**, and **three
decisions take no clock at all** (`Strengthen`, `MergeProposals`, `Reinforce`) — which is the same
observation `m2a` made about `Displaces`, and it means those three are testable with no fake clock.

`grep -rn 'now time.Time' internal/core/consolidation` enumerates every time-dependent decision the
package ships, exhaustively — which is why no input struct carries the instant the decision is made
(`Incomplete` carries `CreatedAt`, `Cold` carries `LastTouchedAt`, `Belief` carries
`LastReinforcedAt`: data about the subject, never about the moment).

Proposal §4.5's consequence stands and is restated because it is the pass's semantics: `archive`'s
Δt and `expire_incomplete`'s 24-hour window are both measured from the pass's **start** instant, not
from when each phase reaches each unit. That is what makes a pass over weeks of simulated data
reproducible, which is the demo's own requirement.

---

## 6. Declared vocabulary — the complete surface `spec.md` writes against

Every exported identifier `m2b` introduces. Nothing else is exported from
`internal/core/consolidation`.

### 6.1 The sequence (PR 1)

```go
type Phase int

const (
    PhaseExpireIncomplete Phase = iota
    PhaseArchive
    PhaseStrengthen
    PhaseConnect
    PhaseDerive
    PhaseReweight
    PhasePatternEval
    PhaseLearn
)

var ErrUnknownPhase = errors.New("consolidation: unknown phase")

func Order() []Phase                        // fresh slice, len 8, ascending
func (p Phase) String() string              // out-of-range renders "Phase(n)", never panics
func ParsePhase(s string) (Phase, error)    // exact match on String()'s names
```

### 6.2 Transitions (PR 2)

```go
type Reason string

const (
    ReasonIncompletePromoted   Reason = "incomplete_promoted"
    ReasonIncompleteExpired    Reason = "incomplete_expired"
    ReasonBelowWeightThreshold Reason = "below_weight_threshold"
)

func AllReasons() []Reason

// Transition is a planned status change. core names the machine-readable
// Reason; brain renders decision_log.rationale's prose from it (doc 02 §11,
// nooma-core rule 5 — core never writes the log and never carries its copy).
type Transition struct {
    UnitID string
    From   unit.Status
    To     unit.Status
    Reason Reason
}
```

Every `Transition` any producer emits is a pair `unit.ValidateTransition` accepts; an L1 table
drives every emitted transition through it rather than asserting the pairs by hand.

### 6.3 `expire_incomplete` (PR 2)

```go
const IncompleteExpiryHours = 24

type Incomplete struct {
    UnitID     string
    CreatedAt  time.Time
    Unresolved bool   // §4.2: declared here, produced by nobody in M2
}

// ExpireIncomplete returns one Transition per unit whose ambiguity has stood
// for IncompleteExpiryHours, sorted by UnitID. Units younger than that
// produce nothing.
func ExpireIncomplete(us []Incomplete, now time.Time) []Transition
```

### 6.4 `archive` (PR 2)

```go
const DefaultWeightThreshold = 0.5

// ResolveWeightThreshold returns DefaultWeightThreshold when configured is
// nil (the config singleton row has never existed — ruling Q1 option C) AND
// when configured points at a value core cannot interpret: non-finite, or
// outside [0, weight.WeightCeiling]. A value core cannot interpret is
// treated as no value, which is the same outcome an absent row already has.
func ResolveWeightThreshold(configured *float64) float64

type Cold struct {
    UnitID        string
    Status        unit.Status
    Weight        float64
    DecayRate     float64
    LastTouchedAt time.Time
}

// Archive plans pool → archived for every unit whose effective weight at now
// is strictly below threshold (doc 02 §6.2). Non-live units are skipped.
// A unit whose Weight or DecayRate is non-finite is refused, not archived,
// and reported through corrupted — archiving is a state transition and a
// read error must not cause one. Both slices sorted by UnitID.
func Archive(cs []Cold, threshold float64, now time.Time) (transitions []Transition, corrupted []string)
```

**The comparison is strictly `<`, and it is load-bearing beyond doc 02 quoting it.** `m2a` §3.3
guarantees that two-hop spreading activation targets exactly `weight_threshold` and therefore
"can never lift a unit above the archive threshold". Because the boost is asymptotic, a two-hop
recipient lands *strictly below* 0.5 and `<` archives it — the guarantee composes exactly. Under
`<=`, a unit sitting at exactly the threshold from any source would be archived, which is not what
§6.2 says. The operator is quoted from the document *and* checked against `m2a`'s promise.

### 6.5 `strengthen` (PR 3)

```go
const StrengthenGain = 0.10

type RelationEvidence struct {
    RelationID        string
    Strength          float64
    FromLastTouchedAt time.Time
    ToLastTouchedAt   time.Time
}

type StrengthChange struct {
    RelationID string
    Strength   float64
}

// Strengthen returns one StrengthChange per relation whose BOTH endpoints
// were touched at or after since, sorted by RelationID. since == nil (the
// vault has never consolidated) returns nothing at all. A relation already
// at strength 1 produces no row. A Strength that is non-finite or outside
// [0,1] is refused and reported through corrupted.
func Strengthen(es []RelationEvidence, since *time.Time) (changes []StrengthChange, corrupted []string)
```

### 6.6 `reweight` (PR 3)

```go
// Reweight applies doc 02 §6.6's post-connection adjustment: every unit that
// gained a relation this pass spreads activation to the units it was just
// joined to, through weight.Resurface, over newEdges and no others.
//
// states is a map, not a slice: a duplicate UnitID in a slice silently masks
// corruption and its outcome depends on slice order (m2a C18). The map makes
// the duplicate unrepresentable, and Reweight sorts states by UnitID before
// handing Neighbourhood.States to Resurface so the value is deterministic
// regardless of map iteration order.
//
// An Edge whose Strength is non-finite or outside [0,1] is refused at this
// door and both endpoints are reported, before weight.clampStrength or any
// comparison downstream can skip past it (m2a C15's rule; C19's asymmetry is
// not inherited).
//
// Both slices sorted by UnitID. boosts is merged per-unit by max boosted
// weight — the same max rule Resurface and focus.AdjacencyStrengths use.
// corrupted is merged by UNION, deduplicated: a unit id refused by any one
// origin's Resurface call is reported at most once in Reweight's output,
// regardless of how many of the pass's origin calls flag it (m2a C20/C21 —
// a shared, unfiltered newEdges set means a corrupt edge can be reported by
// every origin call that does not otherwise explain that unit; the merge is
// where that is resolved, not the call).
//
// A unit id MAY appear in both boosts and corrupted from the same call:
// one origin's legitimate boost never cancels another origin's refusal, and
// neither suppresses the other. They are independent facts about the pass —
// "at least one origin moved this weight" and "at least one origin could not
// explain this unit" both hold at once, and both are reported (§4.5(a)).
func Reweight(states map[string]weight.Current, newEdges []weight.Edge, now time.Time) (boosts []weight.Boost, corrupted []string)
```

There is no `Materialize` — §4.5(b).

### 6.7 `connect` (PR 4)

```go
const (
    ConnectSourceLimit = 20
    ConnectCandidateK  = 5
)

type Source struct {
    UnitID        string
    Status        unit.Status
    Weight        float64
    DecayRate     float64
    LastTouchedAt time.Time
}

// Pair is an ordered pair. Storage direction is what the judge said (doc 02
// §4), so a planned edge runs source → candidate; CanonicalPair is used for
// LOOKUP only.
type Pair struct{ From, To string }

func CanonicalPair(a, b string) Pair   // lexicographically ordered

type ProposedRelation struct {
    From       string
    To         string
    Type       string
    Strength   float64
    Confidence float64
    CreatedBy  relation.CreatedBy   // always CreatedByConsolidation
}

// SelectConnectSources returns the ids this pass runs recall for: live units
// touched at or after since (every live unit when since is nil), ranked by
// weight.Effective descending, ties broken by id, capped at
// ConnectSourceLimit.
func SelectConnectSources(ss []Source, since *time.Time, now time.Time) []string

// ConnectPairs turns one source's fused recall result into the pairs the
// judge is asked about: the first ConnectCandidateK candidates, excluding
// the source itself and every pair whose CanonicalPair is in existing.
func ConnectPairs(source string, fused []recall.FusedCandidate, existing map[Pair]bool) []Pair

// ProposeRelation applies doc 02 §4's persist decision to a judge's answer,
// through relation.Decide unchanged. It returns false — no plan, no
// decision_log row — for outcome "new", for relation.Discard (I08), and for
// a judgment missing any of TargetUnitID, Type, Strength or Confidence after
// tolerant decode ("a judgment that decided nothing writes nothing", §4).
// relation.Uncertain returns true: the band is stored AND asked about (I09),
// and the asking is M3's.
func ProposeRelation(from string, j relation.Judgment, t relation.Thresholds) (ProposedRelation, bool)
```

### 6.8 `derive` (PR 4)

```go
const (
    BeliefMergeCosine    = 0.85   // existing §13 row, first Go home
    BeliefReinforceGain  = 0.10
)

type Belief struct {
    ID               string
    Facet            selfmodel.Facet
    TopicKey         string
    Confidence       float64
    LastReinforcedAt time.Time
}

type BeliefVector struct {
    BeliefID string
    Vector   []float32
}

type MergeDecision struct {
    ProposedIndex int      // index into the proposed slice
    MergeInto     string   // existing belief id; "" means create a new belief
    Similarity    float64  // cosine against MergeInto; 0 when creating
}

// DeriveTopicKey renders doc 02 §10's derived key: "derived/{facet}/{key}".
func DeriveTopicKey(f selfmodel.Facet, key string) string

// MergeProposals is doc 02 §6.5's SECOND dedup defense (the first is the
// prompt-side one, brain's). For each proposed belief it finds the nearest
// existing belief and merges when cosine >= BeliefMergeCosine.
//
// It builds the comparison through recall.NewVectorIndex + recall.Search
// rather than a second similarity implementation: Search is a dot product,
// which IS cosine once both sides are unit-normalized, and it carries I21's
// model filter with it (ErrModelMismatch) — so belief vectors inherit
// "embeddings from two models never compare" at no cost. MergeProposals
// normalizes every vector itself via recall.Normalize, so normalization is
// structural here rather than a caller obligation; a zero-magnitude vector
// is refused (recall.ErrZeroVector), never scored.
//
// Ruling Q2 (option A): brain embeds every active belief in memory at the
// start of the phase and discards after. No schema change, and the nightly
// provider cost is written into doc 02 §6.5 as part of this change.
func MergeProposals(model string, existing, proposed []BeliefVector) ([]MergeDecision, error)

// Reinforce raises a merged belief's confidence by §4.1's law. It returns
// false — no write — for a belief already at 1, and refuses a non-finite or
// out-of-[0,1] confidence outright.
func Reinforce(confidence float64) (float64, bool)
```

### 6.9 `pattern_eval` (PR 5)

```go
const (
    DefaultGoalStagnationDays  = 21
    DefaultMentalLoadThreshold = 7
    LoadCooldownDays           = 7
)

func ResolveGoalStagnationDays(configured *int) int    // nil or <= 0 → default
func ResolveMentalLoadThreshold(configured *int) int   // nil or <= 0 → default

type StagnationFinding struct {
    BeliefID     string
    TopicKey     string
    StagnantDays float64
}

type LoadFinding struct {
    OpenCount int
    Threshold int
}

// EvaluateStagnation returns one finding per goal-facet belief not reinforced
// for stagnationDays, sorted by BeliefID. Beliefs of other facets are skipped.
// A LastReinforcedAt after now clamps to zero elapsed and is not stagnant.
func EvaluateStagnation(bs []Belief, stagnationDays int, now time.Time) []StagnationFinding

// EvaluateLoad returns doc 02 §7's tentative current_state hypothesis when
// the live mental_load count is at or above threshold AND the cooldown since
// the last hypothesis has elapsed. Above threshold but inside the cooldown
// returns false — a decision with no effect writes nothing (§11).
func EvaluateLoad(openMentalLoad, threshold int, lastHypothesisAt *time.Time, now time.Time) (LoadFinding, bool)
```

### 6.10 `internal/core/relation` and `internal/core/selfmodel` (PR 4)

```go
// internal/core/relation
type CreatedBy string
const (
    CreatedBySystem        CreatedBy = "system"
    CreatedByConsolidation CreatedBy = "consolidation"
    CreatedByUser          CreatedBy = "user"
)
func AllCreatedBy() []CreatedBy
func ParseCreatedBy(s string) (CreatedBy, error)
var ErrUnknownCreatedBy = errors.New("relation: unknown created_by")

// internal/core/selfmodel
type Facet string
const (
    FacetIdentity   Facet = "identity"
    FacetValue      Facet = "value"
    FacetGoal       Facet = "goal"
    FacetSocial     Facet = "social"
    FacetPreference Facet = "preference"
)
func AllFacets() []Facet
func ParseFacet(s string) (Facet, error)
var ErrUnknownFacet = errors.New("selfmodel: unknown facet")
```

### 6.11 §13 calibration rows

**Six new rows**, taking §13 from 33 to 39, and **four existing rows annotated** with the Go home
they finally get:

| Knob | Default | Status |
|---|---|---|
| `incomplete_expiry_hours` (`consolidation.IncompleteExpiryHours`) | 24 | **new**, quoted from doc 02 §1 |
| `strengthen_gain` (`consolidation.StrengthenGain`) | 0.10 | **new**, **chosen** (§4.3) — checked for compatibility against `DefaultGoalStagnationDays`, not derived from it |
| `connect_source_limit` (`consolidation.ConnectSourceLimit`) | 20 | **new**, chosen |
| `connect_candidate_k` (`consolidation.ConnectCandidateK`) | 5 | **new**, chosen — separate from `dedup_candidate_k` despite the identical default |
| `belief_reinforce_gain` (`consolidation.BeliefReinforceGain`) | 0.10 | **new**, chosen (inherits §4.3's argument, different quantity) |
| `load_cooldown_days` (`consolidation.LoadCooldownDays`) | 7 | **new**, chosen — doc 02 §7 says "a cooldown of days" and names no number |
| `weight_threshold` (archiving) | 0.5 | existing — gains `consolidation.DefaultWeightThreshold` + `ResolveWeightThreshold` (ruling 4's handoff, discharged) |
| `goal_stagnation_days` ⚙ | 21 | existing — gains `consolidation.DefaultGoalStagnationDays` + `ResolveGoalStagnationDays`. **Two schema homes — §9 Q3** |
| `mental_load_threshold` | 7 | existing — gains `consolidation.DefaultMentalLoadThreshold` + `ResolveMentalLoadThreshold` |
| Semantic belief merge | cosine ≥ 0.85 | existing — gains `consolidation.BeliefMergeCosine` |

`reweight` adds **nothing** to this table, which is §4.5's whole argument in one row.

---

## 7. Test matrix

| What | Level | Where | PR |
|---|---|---|---|
| `Order()` has 8 elements, strictly ascending, `Order()[7] == PhaseLearn`; `String()` is total over `Order()` and never panics out of range; `ParsePhase ∘ String` round-trips and rejects unknown text with `ErrUnknownPhase` | L1 | `internal/core/consolidation/` | 1 |
| **I11 (pure half)** — `Order()`'s names joined by `" → "` equal doc 02 §6's arrow line **parsed off disk**; every name in `phaseNames()` is non-empty | L2 | `test/conformance/i11_...` | 1 |
| **I11 (structural)** — no non-test file outside `internal/core/consolidation` contains two or more phase-name literals | L2, tree scan | `test/conformance/i11_...` | 1 |
| The `const _ uint` assertion is **not tested** — it is a compile-time property. Its existence is asserted only by a doc comment cross-reference, and the honest reason is written in the conformance file: a test that "checks" a compile error would have to shell out to the compiler | — | — | 1 |
| `ExpireIncomplete`: under 24 h → nothing; at exactly 24 h → a transition (boundary from both sides); `Unresolved` chooses `archived` vs `pool`; `created_at` after `now` clamps and produces nothing; output sorted; every emitted pair accepted by `unit.ValidateTransition` | L1 | `internal/core/consolidation/` | 2 |
| `Archive`: `e < threshold` archives, `e == threshold` does **not** (both sides); non-live statuses skipped; non-finite `Weight`/`DecayRate` refused into `corrupted`, never archived; both slices sorted | L1 | `internal/core/consolidation/` | 2 |
| `ResolveWeightThreshold`: `nil` → default; a finite in-domain value passes through; `NaN`, `±Inf`, negative, and `> WeightCeiling` all → default | L1 | `internal/core/consolidation/` | 2 |
| `DefaultWeightThreshold` equals migration `0002:63`'s column `DEFAULT`, read off disk via `migrationSQLText` | L2 | `test/conformance/` | 2 |
| **The composition with `m2a`** — `weight.ReviveGain × weight.WeightCeiling > DefaultWeightThreshold` (one revive lifts a fully-decayed unit clear of archiving) and a two-hop `Resurface` result is strictly below `DefaultWeightThreshold`, hence archived. `m2a` could only assert the first against SQL text because it had no Go constant (ruling 4); with the constant here, this is an L1 assertion for the first time | L1 | `internal/core/consolidation/` | 2 |
| `Strengthen`: `since == nil` → empty; one endpoint stale → nothing; both at exactly `since` → a change (`Before` is strict); asymptotic and never reaches 1; already at 1 → no row; sorted | L1 | `internal/core/consolidation/` | 3 |
| **`StrengthenGain`'s compatibility check** — `ceil(ln(0.1/0.9)/ln(1−StrengthenGain)) == DefaultGoalStagnationDays`, computed from the constants, never from literals; pinned from both sides (n=20 below 0.9, n=21 at or above) | L1 | `internal/core/consolidation/` | 3 |
| `Strengthen` refuses `NaN`, `±Inf`, `-0.5` and `1.5` into `corrupted` and emits no change for them — the four shapes at the door, mutation-verified | L1 | `internal/core/consolidation/` | 3 |
| `Reweight`: both endpoints of a new edge are boosted; multi-origin results merged by max; a corrupt edge strength refuses both endpoints; output deterministic across repeated calls with the same map (the sort, mutation-verified by removing it with ≥3 units — `m2a` C16's own method); no new constants referenced | L1 | `internal/core/consolidation/` | 3 |
| `spread.go`'s `refused` guard removed and the `weight` package stays green (C17's own closing criterion) | L1 | `internal/core/weight/` | 3 |
| `SelectConnectSources`: `since == nil` takes the whole live pool; non-live skipped; ordering by `Effective` with the id tie-break; the cap at `ConnectSourceLimit`; determinism under `-shuffle=on` | L1 | `internal/core/consolidation/` | 4 |
| `ConnectPairs`: the source is never its own candidate; `existing` excludes by `CanonicalPair` in **both** stored directions; the cap at `ConnectCandidateK`; fused order preserved | L1 | `internal/core/consolidation/` | 4 |
| `ProposeRelation`: `new` → false; `Discard` → false (I08); each of the four missing fields → false; `Uncertain` and `Asserted` → true; `CreatedBy` is always `CreatedByConsolidation` | L1 | `internal/core/consolidation/` | 4 |
| `MergeProposals`: cosine exactly `BeliefMergeCosine` merges (boundary, both sides); the nearest existing belief wins; empty `existing` creates; a zero vector surfaces `recall.ErrZeroVector`; un-normalized input still scores as cosine (normalization is internal). A model mismatch is inherited from `recall.Search`'s own contract and verified there, NOT a reachable `MergeProposals`-level scenario: this function's single-`model` call surface (index and every query built from the same parameter) cannot manufacture `idx.Model != q.Model` | L1 | `internal/core/consolidation/` | 4 |
| `DeriveTopicKey` renders `derived/{facet}/{key}` for every member of `selfmodel.AllFacets()`, driven by the vocabulary and asserting its own exhaustiveness | L1 | `internal/core/consolidation/` | 4 |
| `Reinforce`: asymptotic, never reaches 1, no write at 1, refuses non-finite and out-of-domain | L1 | `internal/core/consolidation/` | 4 |
| `relation.AllCreatedBy` / `selfmodel.AllFacets` return fresh slices; both `Parse*` round-trip and reject; `AllCreatedBy` matches `0001:37`'s column comment vocabulary read off disk | L1 + L2 | package + `test/conformance/` | 4 |
| `EvaluateStagnation`: non-goal facets skipped; exactly `stagnationDays` is stagnant (`>=`, both sides); future `LastReinforcedAt` clamps and is not stagnant; sorted | L1 | `internal/core/consolidation/` | 5 |
| `EvaluateLoad`: exactly `threshold` fires (`>=`, both sides); below does not; inside the cooldown returns false even above threshold; `lastHypothesisAt == nil` fires; exactly `LoadCooldownDays` elapsed fires | L1 | `internal/core/consolidation/` | 5 |
| `ResolveGoalStagnationDays` / `ResolveMentalLoadThreshold`: `nil`, `0` and negative → default; positive passes through | L1 | `internal/core/consolidation/` | 5 |
| Both `Default*` integers equal migration `0002:66` / `0002:67`'s column `DEFAULT`s | L2 | `test/conformance/` | 5 |

No test in `m2b` opens a database, reaches a network, calls a provider, or reads a real clock.
Every L1 test is a pure function over literal inputs. The L2 tests exist because they name an
invariant (I11) or pin a Go constant to schema or doc text — `nooma-testing`'s decision gate.

**Coverage.** `m2b` is almost entirely `internal/core/**` and `make check` never runs
`scripts/core-coverage.sh`, so `make check-all` is the pre-PR command structurally (proposal R6).
Every function here is total over a small enumerable domain — three boundary predicates, four
asymptotic laws, one enum — which is what makes exhaustive tables possible rather than aspirational.

**Test-first, now that `pending-red.sh` is retired.** `m2a`'s D11 procedure is inherited verbatim
and `sdd-tasks` must encode it as ordered items: commit 1 is the test plus a stub with the final
signature returning zero values (the suite compiles, the assertion fails — red for the right
reason); commit 2 is the implementation. A conformance test naming an undefined core symbol breaks
the **untagged** build, and a compile error is not "watched failing red for the right reason".

---

## 8. What `m2b` leaves for `m2c`, and in what shape

`m2b` fixes shapes; `m2c` declares the ports and speaks SQL. Each row is a shape, not a
suggestion — stated here so `m2c` designs it rather than discovering it at apply time.

| `m2c` must supply | Shape `m2b` fixes | Notes |
|---|---|---|
| `ConfigRepo` over the `config` singleton | Every knob as a **nil-sentinel pointer**: `*float64` for `weight_threshold`, `*int` for `goal_stagnation_days` and `mental_load_threshold`, `*time.Time` for `consolidation_last_run_at`, plus `consolidation_enabled` | Ruling Q1 option C. `m2b` supplies the meaning of `nil` in all four `Resolve*` functions; `m2c` supplies the pointer. No migration 0003 |
| `UnitRepo` weight write | Takes a `weight.Boost` (or its three fields) so a weight without a timestamp is inexpressible | I24, `m2a` D3's handoff. `Reweight` is the first producer of a `[]weight.Boost` with a real caller |
| `UnitRepo` live-count-by-type | Returns an `int`, not a slice | Owner ruling 6. `EvaluateLoad` takes the count precisely so an unbounded read never enters a phase that needs one integer |
| `UnitRepo` reads for `archive` and `connect` | `[]Cold` and `[]Source` — decay fields plus status, never whole `unit.Unit` values | Keeps I05's "no unit-shaped value a read path could persist" property (`m2a` D9) intact one layer up |
| `UnitRepo` read for `expire_incomplete` | The one deliberate non-live read in M2: `incomplete` units by status. **Name it so the exception is explicit** (`IncompleteOlderThan`, not `List(status)`) | Proposal §3.4's I02 note — "the one deliberate exception, which must be named rather than discovered". `UnitRepo`'s own doc comment already forbids a `List(status)` parameterized read |
| `RelationRepo` read for `strengthen` | `[]RelationEvidence` — the relation joined to **both** endpoints' `last_touched_at` | A join no port has today. The alternative (load relations, then load units, then zip in `brain`) is two round trips and a correctness hazard if a unit moves between them |
| `RelationRepo` read for `connect` | `map[Pair]bool` over the candidate set, keyed by `CanonicalPair` | §4.4's exclusion. Bounded by `ConnectSourceLimit × ConnectCandidateK` |
| `SelfModelRepo` | Upsert by `topic_key` (`RelationRepo.Upsert`'s shape, ruling 5) plus a read whose **name** carries "active", never a status parameter — `ActiveBeliefs()`, not `Beliefs(status)` | `LiveByIDs`'s own precedent. `m2b` deliberately declares no belief-status vocabulary; the port name is the guard |
| `goal_stagnation_days`'s one schema home | `m2b` declares one Go constant (`DefaultGoalStagnationDays`) and one `Resolve*`, reading whichever table `m2c`'s `ConfigRepo` decides | §9 Q3: two schema homes exist today — `config.goal_stagnation_days` (`0002:66`) and `calibration`'s own example key (`0002:38`) — and §13/§6.11 mark the knob ⚙ (learning-tunable). `m2c` must pick the table `ConfigRepo` reads; **M5's learning module must write the same one**, or a recalibration silently stops being read on the next pass |
| Belief embeddings | `[]BeliefVector`, computed in memory at the start of `derive` and discarded | Ruling Q2 option A. **The nightly provider cost must be written into doc 02 §6.5 by this change**, not left implicit — that is part of the ruling, not an aside |
| The two recall legs for `connect` | Two ranked `[]string`, vector leg first | `recall.FuseScored`'s argument order is load-bearing for its tie-break (`fuse.go:33-42`) |
| `current_state` write | One append-only row per `LoadFinding` | doc 02 §10. No delivery — M3 |
| `decision_log` | Written from `brain` only, from the `Reason` codes `m2b` returns | I12, `nooma-core` rule 5. `m2b` returns codes; `brain` renders `rationale` |
| Concurrency at `archive`'s write | `SetStatus`'s `from` precondition; `ErrStatusConflict` is **skipped and logged**, never a pass failure | Proposal R8, and §2's own success criterion |
| `brain/capture.go:485` | Adopt `relation.CreatedBySystem` in place of the bare `"system"` literal | One line. `m2b` declares the vocabulary (§5.3) but touches no `brain` file |
| I05's structural half | Scoped to read paths — and **simpler than planned**, since §4.5(b) declines bulk materialization | Narrows proposal R13 |

---

## 9. Open questions this design could not close

Each is named rather than assumed. The first two change shipped behaviour and are owner-review
items; the rest are handoffs with a recommendation attached.

**Q1 — `Incomplete.Unresolved` is a field with no producer, in a milestone that ruled out its
producer.** §4.2 keeps it so doc 02 §1's archive branch is expressible and testable; in M2 `m2c`
passes `false` for every unit and the branch is proven against a repo-constructed input. The
alternative is to drop the field, ship `expire_incomplete` as promotion-only, and leave §1's branch
unimplemented until M3 — which would mean amending §6.1 to state one outcome and §1 to stop
claiming two, i.e. **removing** a behaviour doc 02 describes rather than filling a gap.
**Recommendation: keep the field.** *(This is the same shape as `m2a`'s `adjacency` term, which
shipped with no producer and was accepted for the same reason — §8 R5 there.)*

**Q2 — decay materialization is declined, reversing an assumption both the proposal and `m2a`
carried.** §4.5(b) has the argument: I24 forces any materializing write to touch `last_touched_at`,
which would falsify the "record of direct use" reading `m2a`'s ruling 2 put into doc 02 §2 and that
`focus.AgeRamp` and §4.3's co-activation predicate both depend on. Its honest form needs a
`last_decayed_at` column, i.e. a migration — and whether the **consolidation** half of M2 may take
one is **R1, still open** (`m2-sleep-weight/proposal.md:373-375`); ruling 2 and Q1 settled the
question only for the scheduler half and for seeding existing `config` defaults, not for a new
column here. **Recommendation: decline in M2 on the merits above, amend §6.6 to say so and why,
revisit with the column once R1 resolves.**

**Q3 — `goal_stagnation_days` has two homes in the schema.** `config.goal_stagnation_days INTEGER
NOT NULL DEFAULT 21` (`0002:66`) **and** `calibration`'s own column comment names it as its example
key (`0002:38`). §13 marks it ⚙ (learning-tunable), which points at `calibration`; the typed column
points at `config`. `m2b` declares one Go constant and one `Resolve*`; **`m2c` must decide which
table `ConfigRepo` reads**, and M5's learning module must write the same one. Recorded, not decided.

**Q4 — `reweight` uses `weight.Resurface` under a semantics `m2a` did not anticipate**: the origin's
warrant is a judge's new edge rather than a user's direct touch. The mechanism, the cap and the
attenuation are unchanged and the safety argument (target-scaling, §4.5) carries over intact, but
this is a second meaning for one function. **Recommendation: reuse, and record the second meaning
in `Resurface`'s doc comment and in doc 02 §6.6.** The alternative is a fourth boost formula.

**Q5 — `connect` excludes candidate pairs by *unordered pair*, not by `(pair, type)`.** It spends a
possible second relation type between two units to save a judge call against a bounded nightly
budget. Reversible by widening the key. **Recommendation: unordered pair**, revisit when the judge's
type distribution is observable.

**Q6 — which instant starts the load watcher's cooldown.** doc 02 §7 says "after a *resolved*
check-in", and resolution is a `state_confirmed`/`state_denied` learning signal — M5. In M2 the only
available instant is the last `current_state` row's `recorded_at`, which starts the cooldown on the
*hypothesis* rather than on its resolution. `m2b` takes `lastHypothesisAt *time.Time` and is
agnostic. **`m2c` must map it and say so in the `decision_log` context.**

**Q7 — `ResolveWeightThreshold` and `focus.ResolveMargin` both sanitize a corrupt configured value,
but land on different fallbacks, and that is a recorded difference, not an oversight.**
`focus.ResolveMargin` already refuses a non-finite or negative configured margin — shipped in `m2a`
(`internal/core/focus/hysteresis.go:79-88`) and stated in doc 02 §3 — but resolves it to `0`, the
neutral "no anti-jitter protection" value, deliberately never to `DefaultHysteresisMargin`:
assigning the calibrated default to data core never validated would assert a confidence the
corruption does not support (`ResolveMargin`'s own doc comment). `ResolveWeightThreshold` (§6.4)
resolves the same shape of corruption — non-finite or out of `[0, weight.WeightCeiling]` — to
`DefaultWeightThreshold` instead, because a threshold has no neutral value the way a margin does:
`0` would archive nothing ever, and `+Inf` would archive everything, so "no configured value" and
"a corrupted one" collapse onto the same reading core already gives an absent row. The two
`Resolve*` functions differ on purpose, for a reason specific to each knob's own domain, not because
one sanitizes and the other does not.

**Q8 — `Strengthen`'s `since` and `SelectConnectSources`'s `since` are the same value, and nothing
structural says so.** Both take `*time.Time`; `m2c` passes `consolidation_last_run_at` to both. A
runner that passed different values to the two would produce a pass whose phases disagree about
when the last night was. **Recommendation for `m2c`: one field on the pass context, read once**, the
same discipline the single clock read already has. Not enforceable from `core`.

---

## 10. The five PRs

Estimates are **implementation plus docs, separately from test lines**, per `docs/06-harness.md`
§7 as amended in PR #142 during `m2a`. The old total-line convention is not used.

| # | Branch | Contents | Impl + docs | Tests (est.) |
|---|---|---|---|---|
| **1** | `feat/core-consolidation-order` | `phase.go`, `doc.go`; doc 02 §6 preamble + §6.8's "no-op" sentence; I11's L2 file | **~135** | ~200 |
| **2** | `feat/core-consolidation-expire-archive` | `transition.go`, `expire.go`, `archive.go`; doc 02 §6.1's **contradiction fix**, §6.2's `<` and its `m2a` composition, §1 cross-reference; §13 ×2 (1 new, 1 annotated); the DDL pin | **~235** | ~380 |
| **3** | `feat/core-consolidation-strengthen-reweight` | `strengthen.go`, `reweight.go`, C17's deletion in `weight/spread.go`; doc 02 §6.3's formula + evidence definition + "strength never falls", §6.6's adjustment **and the materialization decline**, §2 cross-reference; §13 ×1 new | **~230** | ~400 |
| **4** | `feat/core-consolidation-connect-derive` | `connect.go`, `derive.go`, `relation/createdby.go`, `selfmodel/facet.go`; doc 02 §6.4's budget-as-a-product, §6.5's merge rule **and ruling Q2's nightly cost**, §10's key format; §13 ×4 (3 new, 1 annotated) | **~350** | ~500 |
| **5** | `feat/core-consolidation-pattern-eval` | `patterns.go`; doc 02 §7's two predicates and the cooldown, §6.7; §13 ×3 (1 new, 2 annotated); the DDL pins | **~180** | ~300 |

**Total: ~1,130 implementation + docs, ~1,780 test lines, across five PRs.** No PR crosses the
400-line ceiling under the current convention; PR 4 is the closest at 0.88×.

**Where the guessing is.** These are guesses of the same kind that were wrong before: M0 measured
1.3×–2.2×, M1 Phase B 4.3×, `m2a` 1.75× against its own proposal row. The figures above run
0.6×–1.0× of proposal §5.1's numbers, but **that comparison is meaningless** because §5.1 was
written under the total-line convention and these are implementation-plus-docs. The honest
statement: `m2a`'s **measured** ratio of test lines to implementation-plus-docs across its first
four links is **3.9×** (3,115 test vs 800 impl+docs, `docs/06-harness.md:359-362`). The table above
assumes 1.6×. **The test column is the number most likely to be wrong, and by roughly 2.5×** — at
`m2a`'s measured ratio `m2b` ships closer to 4,400 test lines than 1,780. That does not move any
PR past the ceiling, since the ceiling no longer counts test lines; it moves the calendar.

**The impl+docs column carries a matching risk, and it was not hedged above — it is now.** `m2a`'s
four measured links never produced an impl+docs figure above 319 (0.80×, link 3a, the table in
`docs/06-harness.md` §7) — yet PR 4's estimate here already assumes ~350 (0.88×), above every actual
`m2a` PR, before a single line of `m2b` is written. This project's own doc-comment density adds to
that risk rather than offsetting it: `weight.Resurface`'s single doc comment runs to ~130 lines
(`internal/core/weight/spread.go:37-165`), and §4's formula arguments above are written at
comparable length before they are quoted into `doc.go` and doc 02. If PR 4's actual impl+docs runs
even 15% over its ~350 estimate — a rate far gentler than the growth this same design already cites:
`internal/core/weight/spread.go` went **183 → 371 lines, ~103%**, across the three Judgment Day
rounds that hardened `Resurface` (`git diff --stat` between the first green implementation and the
final C15 fix) — it crosses 400 outright, not merely tightens the margin.

**The pre-drawn split below still holds — but the headroom is thinner than the 15% hedge suggests,
and that is worth saying honestly rather than papering over.** PR 4 splits at `connect.go` (~190) |
`derive.go` (~165); a 15% overrun distributed proportionally puts each half at roughly 218 and 190
lines. But 15% is this design's working hedge, not the worst case its own evidence supports: at
`spread.go`'s measured ~103% growth, `connect.go` alone would land at ~386 — still under 400, but
with only **~3.5% headroom**, not comfortably under. The split absorbs the overrun either way, which
is exactly why it is drawn in advance rather than decided mid-PR, but "comfortably under" only holds
at the 15% hedge, not at the growth rate this repository has actually shown. PR 3's split
(`strengthen.go` | `reweight.go`) starts smaller (~230 total) and has more headroom regardless of
which half an overrun lands on.

**Split lines drawn in advance**, so a crossing does not become an unplanned decision:

- **PR 4 splits at `connect.go` | `derive.go`** — two independent files, two independent doc 02
  subsections, and `relation/createdby.go` travels with `connect` while `selfmodel/facet.go`
  travels with `derive`. The split is 4a ~190 / 4b ~165.
- **PR 3 splits at `strengthen.go` | `reweight.go`** if the materialization-decline amendment runs
  long. C17's deletion travels with `reweight`, which is what makes `Resurface` reachable.

**Review budget.** `m2a` C36's ruling binds: severity is weighted by reachability, **two Judgment
Day rounds per PR is the ceiling**, and a third is justified only by a finding reproducible against
production code. This design is written to leave the second round nothing to find, and the three
places it spends effort on that are the three that cost `m2a` a CRITICAL each:

1. **Every externally-sourced float is validated at its entry point**, never mid-computation, and
   never behind a comparison that can skip it — `Strengthen`'s strength, `Reweight`'s edge
   strengths, `ResolveWeightThreshold`'s configured value, `Reinforce`'s confidence (C15, C22).
2. **Every comparison-based helper is total over its domain including `NaN`** — `m2b` reuses
   `focus.clamp`'s lesson rather than writing a fourth bare-comparison helper; where it needs one it
   guards `math.IsNaN` first (C24).
3. **Every ordering guarantee has a fixture that can fail without it** — `m2a` C16 found a
   `sort.Strings` whose removal left the whole suite green because every fixture produced one
   element. Every sorted output above is fixtured with **at least three** entries and
   mutation-verified by removing the sort.

---

## 11. What this design does not decide

- **The runner.** Reading the clock once, executing `Order()`, persisting, and writing
  `decision_log` at every effect is `m2c`'s `brain/consolidate.go`. `m2b` gives it a sequence with
  no other way to be read (§3) and plans with no other way to be persisted (§6).
- **I11's behavioural half.** `m2b` proves `learn` is last at compile time and that the sequence
  matches doc 02; that the *runner* honours it is `m2c`'s test.
- **I12, I03's write, I24's structural test.** All need the store layer.
- **`nooma consolidate` and its `--phase` flag.** `ParsePhase` is declared for it; the subcommand
  is `m2c`'s.
- **The scheduler and ADR-0009's catch-up.** `m2d`. Note that proposal §4.1 places the *staleness
  decision* in `core/consolidation` — it is **not** in this change, because ruling 1 scoped it to
  the boot catch-up, whose only caller is `m2d`'s scheduler. `m2b` deliberately does not ship a
  function with no caller two changes early.
- **The learning module.** `learn` is slot eight and nothing else until M5.
- **Any delivery.** No trigger, no digest, no push, no `interrupt_level`. `pattern_eval` produces
  findings and nothing carries them (proposal §3.3).
- **I06.** Out of scope, honestly, per ruling Q3 — no producer of `incomplete` units.
