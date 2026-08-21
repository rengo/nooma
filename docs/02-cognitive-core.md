# 02 — Cognitive core (canonical specification)

This document defines the **invariants of the brain**, independent of the stack. It is the
source of truth for behavior: any implementation (or change) is validated against it. The
NUMBERS are calibratable defaults (see §13); the MECHANISMS are fixed.

## 1. The unit — the atom

Everything captured is a **unit**: `type`, `content` (normalized text), embedding, `status`,
weight, structured data, provenance, and timestamps.

- **Types** (plain text, no DB enum): `task`, `mental_load`, `event`, `knowledge`,
  `procedural`, `emotional`, `list`, plus the derived `structured_ref` (anchor for N
  measurements) and `insight` (trend derived from a metric).
- **Status**: `pool` (active) | `archived` (cold, weight ≈ 0) | `superseded` (replaced
  insight) | `incomplete` (a capture waiting to resolve an ambiguity, e.g. a reference to a
  person; no embedding until promoted; promoted with what it has, or archived if still
  unresolved, after 24 h during consolidation — see §6.1).
  - Hard rule: every LIVE read surface excludes `superseded` and `incomplete`. Filtering
    positively (`status = 'pool'`) excludes them for free.
  - This is `unit.Status.IsLive()` (`internal/core/unit`): `true` for exactly `pool`, defined
    as the positive test `status == pool` rather than a negation list — a negation list would
    silently admit a status added later, a positive filter excludes it for free. The predicate
    takes no clock and no arguments; liveness is a property of the status value, not of time.
  - **Legal transitions** (`unit.ValidateTransition`, `internal/core/unit`): `pool → archived`,
    `pool → superseded`, `archived → pool`, `incomplete → pool`, `incomplete → archived`. No
    other pair is legal, including every self-transition (`pool → pool` would let a write
    change nothing while still logging an effect). `incomplete → archived` is where an
    unresolved `incomplete` unit lands when it expires: the status vocabulary has no `expired`
    member, and nothing is ever deleted, so `archived` is the only place left for it.
- **`event_at` vs `created_at`**: when the thing happens/happened vs when it was ingested.
  `due_at` for deadlines. Never mix them.
- **Nothing is deleted.** Archiving is a state transition, not a removal.

## 2. Weight, decay, temperature (lazy-write model)

Weight decays continuously (Ebbinghaus curve):

```
effective_weight = weight * exp(-decay_rate * Δt)     # Δt since last_touched_at, in days
```

`Δt` is clamped at zero: when `now` is before `last_touched_at` — clock skew across a restart,
a backdated import — the formula behaves as though `Δt` were 0 and returns `weight` undecayed
rather than inverting into a value larger than what was stored. `decay_rate` and `weight` are
sanitized the same way, and for the same reason — `classify`'s LLM output validates only that
each is a number, no sign and no range, so `core` cannot vouch for either any more than it can
vouch for `now`: a negative `decay_rate` is treated as 0 (no decay), and a negative `weight` is
treated as 0 (a negative weight has no meaning in this model — weight is how much something
matters, not a signed magnitude). `effective_weight ≤ max(weight, 0)` holds for every **finite**
input, after this sanitization, including `decay_rate ≤ 0`: this is a postcondition over the
sanitized inputs, not a claim about whatever was passed in (`internal/core/weight.Effective`).

The word `finite` is load-bearing and was earned twice. `NaN` and `±Inf` are **not** sanitized,
because every comparison against `NaN` is false — so `weight < 0` and `decay_rate < 0` both
no-op and `NaN` propagates to the result, which satisfies no ordering at all. Nothing on the
ingestion path can produce one: `classify` decodes through `encoding/json`, which cannot read a
`NaN` or `Infinity` token. The exposure is the columns themselves, which carry no `CHECK`, so a
corrupted row or a future arithmetic slip elsewhere could still store one — including a route
easy to miss: `weight = +Inf` combined with a `decay_rate * Δt` product large enough for
`exp(-decay_rate * Δt)` to underflow to exactly `0.0` also reaches `NaN` (`+Inf * 0.0` is `NaN`
under IEEE 754), the same rule that already applies to `decay_rate = +Inf` with `Δt = 0`. A bare
`weight = +Inf` does **not**, on its own, avoid this: with a `decay_rate`/`Δt` pair too small to
underflow the exponential, `weight * exp(...)` stays `+Inf`, not `NaN` — which shape a given
`weight = +Inf` reaches depends on the accompanying `decay_rate` and `Δt`, not on `weight` alone.
The guarantee above
is stated over finite inputs rather than widened to cover a case the code does not handle,
because an earlier revision of this very paragraph claimed the postcondition held "for every
input" when it did not, and the correction must not reintroduce the same over-claim one
boundary further out.

**What is persisted changes only on discrete events**; decay is computed on read, never
written on every read:

- Persisted: `weight` (value at the last event), `last_touched_at`, `weight_decay_rate` (λ).
- **Revive** (direct use): boosts **asymptotically toward a ceiling**, never additively with a
  clamp: `weight' = e + revive_gain * (weight_ceiling - e)`, where `e` is the unit's effective
  weight at the instant of use. **The boost applies to the effective weight at that instant,
  never to the persisted `weight`.** Boosting the persisted value would make decay freely
  reversible and `weight` a monotone ratchet, and this document's entire lazy-write model would
  be decorative. The asymptotic shape is bounded by construction — **for `e < weight_ceiling`**,
  `weight'` never reaches or exceeds `weight_ceiling` — needs no clamp, and is strictly
  increasing in `e`. That bound is over the boost's own contribution, not over every input: when
  `e` is already at or above `weight_ceiling`, the gain term floors at zero and `weight'` equals
  `e` exactly, which can equal or exceed `weight_ceiling` itself if `e` already did. An additive
  boost with a clamp collapses every unit already near the ceiling onto the same value,
  destroying the ordering the hysteresis margin exists to protect. `last_touched_at` is always
  reset to the instant of use, **even when `e` is already at or above `weight_ceiling`** and the
  boost therefore raises nothing: `last_touched_at` is the vault's record of *direct* use, and a
  direct use at the ceiling is still a real decision worth recording, not the no-effect no-write
  case §11 describes. That write is effective-weight-neutral by construction — the pair
  `(weight, last_touched_at)` and the pair `(e, now)` denote the same decay curve, so the
  formula at the top of this section returns the same value at every future instant from either
  — which is exactly why moving the clock there costs nothing observable while still recording
  that the use happened. When `weight'` would be `NaN` or `±Inf` — `weight` or `weight_decay_rate`
  carries no `CHECK` constraint, so a corrupted row or a future arithmetic slip could still
  produce one — **Revive refuses to persist it** rather than coercing it to a finite number:
  coercing to 0 would drive the unit under `weight_threshold` and archive it on the strength of a
  read error, a destructive state transition; refusing leaves the corruption visible and
  untouched for `doctor` or a later repair path to find (`internal/core/weight.Revive`).
- **Resurface** (related signal): F2's asymptotic boost applied to a **target that shrinks
  with graph distance**, instead of Revive's fixed `weight_ceiling`. For every unit `v`
  reachable from a boosted unit within `resurface_max_hops` hops: `gain(v)` is the
  **maximum**, over every path `p` to `v` no longer than `resurface_max_hops`, of the
  product of that path's edge strengths times `resurface_attenuation^|p|`; `target(v) =
  gain(v) * weight_ceiling`; and the write is `weight' = e + revive_gain * (target(v) - e)`,
  the same asymptotic shape Revive uses, only toward a lower ceiling. **The gain scales the
  target, never the step**: scaling the step instead would let a unit merely adjacent to
  something used daily converge on the full ceiling over repeated passes, so the
  neighbourhood of anything hot would become permanently hot and decay would never bite.
  A unit reachable by more than one path takes the **maximum** gain among them, never the
  sum — one rule for combining graph evidence, so a unit's boost never depends on how many
  redundant edges happen to connect it to the origin. Traversal is **undirected** (§4: a
  relation's direction is what the judge said, not a canonical form) and, where two units
  are joined by more than one edge, the strongest is used, by the same max rule. The origin
  of a resurface pass is never itself a recipient. Propagation terminates on a cyclic
  relation graph **by the hop bound alone**, never a runtime timeout: gain strictly
  decreases along a path, and depth is capped.
  **Both halves of the write rule matter together, because the first alone reads as a
  bug**: a resurfaced unit's `last_touched_at` **is reset** to the instant of the pass —
  `weight` is defined as the value at `last_touched_at`, so writing one without the other
  would let the very next read re-apply the whole stale `Δt` to a value that was never true
  at its own timestamp — **and** a unit already at or above its own target gets **no write
  at all**, not a zero-delta entry: no weight write, no `last_touched_at` reset, no
  `decision_log` row downstream. The second half is what stops propagation from making a
  unit *look* directly used — most neighbours of a hot unit are already warmer than
  propagation could make them, and their clocks are never touched — and it is safe together
  with the reset because a resurfaced unit converges on `gain * weight_ceiling`, never on
  `weight_ceiling` itself, so restarting its clock from a graph-distance-bounded level is
  harmless. **At the default calibration, spreading activation alone can never lift a unit
  above the archive threshold at maximum hop distance**: `resurface_attenuation ^
  resurface_max_hops * weight_ceiling ≤ weight_threshold`, which happens to land as an exact
  equality at the shipped defaults — a coincidence of the chosen numbers, not a designed
  identity, since `weight_threshold` is ⚙ recalibratable per user. This is the guarantee that
  makes it safe to run resurface on every capture: only direct use, or a strong immediate
  neighbourhood, keeps something out of the cold. That guarantee assumes **relation strength
  stays in its own domain, `[0, 1]`** (§4): the cycle-termination argument above depends on
  `strength ≤ 1` explicitly, and `strength` carries the same "no sign, no range" exposure as
  `weight` and `weight_decay_rate` — the relation judge's JSON decode validates only that it is
  a number, and the schema's `strength` column carries no `CHECK`. Resurface clamps an edge's
  strength to at most `1` before it reaches the gain formula, for the same reason Effective
  sanitizes weight and decay_rate: an unclamped strength above 1 is not a stronger relation, it
  is a corrupt one, and left unclamped it can inflate a target past `weight_ceiling` itself,
  defeating the guarantee this paragraph states. **Resurface also refuses rather than
  coerces** when a neighbour's own state, or the graph reaching it, is corrupt — the same
  posture Revive takes above, and for the same reason: a corrupted input would otherwise flow
  straight into an ordinary write. Where Revive's refusal has a natural home in its own return
  value (a single unit, a bool), Resurface fans out over a whole neighbourhood: it reports each
  refused unit's id through a second return value, `corrupted`, separate from the boosts slice,
  so a caller can tell "no boost because the unit is already at its target" (an ordinary no-op,
  above) apart from "no boost because the unit's own state is corrupt" — the second is an event
  worth a `decision_log` row once `m2c` can write one, not a silent drop
  (`internal/core/weight.Resurface`). Two structurally distinct inputs are corrupt, and each is
  validated **where it enters**, not by inspecting the fully-computed boosted weight: a
  `Current`'s `weight` or `weight_decay_rate` that is `NaN` or `±Inf`, checked directly before
  the target/effective-weight comparison runs (an earlier version of this refusal instead tested
  the computed write for non-finiteness, and missed `weight = +Inf` alone — the one shape
  `Effective`'s own non-finite arithmetic does **not** turn into `NaN` — because that comparison
  is a valid, ordinary `true` for `+Inf`, not the `NaN`-always-false quirk this refusal exists
  to catch); and an edge `strength` that is `NaN`, which the graph-building step now detects and
  reports explicitly before the edge is ever discarded, rather than letting it vanish the same
  way an edge to a unit genuinely outside the graph would — a `NaN`-strength edge used to be
  indistinguishable from an absent one, silently making its neighbour unreachable instead of
  reported corrupt (`internal/core/weight.Resurface`, `internal/core/weight.buildAdjacency`).
- Nightly consolidation may materialize decay in bulk (optional, an optimization — the truth
  is always the on-demand formula).

**Initial assignment**: during `classify`, the LLM assigns `weight` and λ. Type orients the
direction (`emotional` → low λ, `task` → high λ); the active beliefs of the self-model
(injected as context) personalize the value: something touching a central goal is born with
high weight and very low λ. Type acts as a prior when the self-model is empty (cold start) and
improves as beliefs get derived.

**Thermal zones** — emergent, not persisted:

| Zone | Determination |
|---|---|
| Hot | `status='pool'` and appears in the focus (top-N by priority) |
| Warm | `status='pool'` but does not reach the focus |
| Cold | `status='archived'` (its effective weight crossed the threshold during a consolidation), `'superseded'` or `'incomplete'` (both `inFocus` values) |

Warm→cold is done by consolidation; cold→warm/hot by a strong resurface.

The zone classification is **total** over every unit status, not only `pool` and `archived`:
`superseded` and `incomplete` also classify as Cold, regardless of focus membership. This is a
choice, not a derivation — the zone vocabulary is about attention, and neither status is a
candidate for attention — recorded here so it is a property a reader can check rather than a
rule inferred from an untested arm (`internal/core/weight.ZoneOf`). For `archived`, the
parenthetical "its effective weight crossed the threshold during a consolidation" is causal
history, not something re-derived on read: zone classification takes no clock and no other
input besides status and focus membership — **temperature is not a function of time**, it is a
function of two decisions already made.

## 3. Focus — computed, NEVER persisted

"Being in focus" is a **view**: sort the pool by effective priority and take the first N. A
persisted flag would create two sources of truth that desynchronize on their own (priority
rises with time). Invariant: `status='focus'` does not exist.

```
priority = f(effective_weight, temporal_urgency(due_at), age, relation_to_active_focus)
           over the units the focus's type criterion already selected
```

`type` is **not** a term of `f`. It already did its job one step earlier — the task focus and
the load focus are two queries with different criteria over `units` (below), and `type` is the
criterion, not a number inside the formula that ranks what the criterion selected. Reading it as
both would count it twice: once to decide which contest a unit is in, and again to decide where
it places in that contest. A `task` and an `event` of equal effective weight, equal urgency,
equal age and equal adjacency **tie** on priority; the tie is broken by `due_at`, then `id`, never
by type. Reinstating a numeric type term is **additive** — a new term with its own calibration
rows — not a rewrite of this one.

This `due_at`/`id` sequence is the ranking's entire tie-break, reached only when two units'
priorities are exactly equal, not only the type question above. A unit with no due date sorts
after every due unit at an equal priority, however far away that due date actually is, because
`due_at` is compared only between two units that both carry one; when both are due-less, `id`
decides. Priority first, then this sequence, makes the ranking a genuine total order over the
whole pool **when every unit's `id` is unique** — the ordinary case, since `id` is `units.id`'s
primary key and a query drawn from the table returns at most one row per `id`. Two units sharing
an `id` is a caller error the ranking has no way to detect from the data alone: `id` is the last
level this sequence defines, so two entries with the same `id` are indistinguishable once
priority and `due_at` also tie, and their relative order is then left unspecified rather than
resolved by a fourth level that does not exist.

`f` is a **multiplicative envelope over `effective_weight`**, not a weighted sum of normalized
terms:

```
priority = effective_weight
         × (1 + (urgency_max - 1) × temporal_urgency(due_at))     # deadline: multiplicative
         × (1 + age_weight × age + focus_adjacency_weight × relation_to_active_focus)  # nudges: additive, bounded
```

A sum makes the terms commensurable, and they are not: `effective_weight` is the intrinsic
quantity the whole of §2 exists to maintain, and the other three are contextual modulators of
the moment. Under a sum, a unit whose effective weight has decayed to near zero can be lifted to
the top of the ranking by context alone — a deadline on something the brain has already
forgotten would outrank the thing the user actually cares about, collapsing the very distinction
this section opens with. Multiplying by the intrinsic term makes context **amplify** memory
rather than **substitute** for it: every factor is ≥ 1, so `priority ≥ effective_weight` for every
**finite** `weight` and `decay_rate` — the same restriction §2 already states for
`effective_weight` itself, since `priority` computes it as its first step and inherits whatever it
returns — context can promote a unit and can never demote one, and the ranking is monotone in
`effective_weight` at fixed context. `relation_to_active_focus` carries no equivalent finiteness
caveat: it is saturated to `[0, 1]` unconditionally before it enters the envelope, over its own
entire domain (out-of-range, `NaN`, and `±Infinity` alike), so no value it can take ever breaks
this guarantee on its own — a corrupt or out-of-domain adjacency value contributes no promotion
at all rather than an unbounded or undefined one. A deadline is allowed to dominate — it
multiplies, with unbounded relative leverage up to `urgency_max` — while age and adjacency are
nudges whose combined contribution is capped at `1 + age_weight + focus_adjacency_weight` no
matter how many of them fire, because a due date is a hard external constraint and the other two
are not.

A `priority` that is itself `NaN` — inherited from `effective_weight`'s own `NaN`-producing
shapes (§2), since `priority` computes `effective_weight` as its first step and does not
sanitize what it returns — cannot be ranked by an ordinary `>` any more than a raw
`effective_weight` can: every IEEE 754 comparison against `NaN` is false, so it satisfies no
ordering at all. For ranking purposes only, a `NaN` priority is remapped to negative infinity, so
it sorts after every other unit in the pool, including one whose effective weight has decayed to
zero — the same "a corrupted input contributes no promotion, never a crash, never an arbitrary
position" posture this section already takes for a corrupt `relation_to_active_focus` above. The
remap exists only inside the ordering: the `priority` a caller reads back for that unit is the
literal `NaN`, never the substituted `-∞` — hiding the corruption behind a manufactured number
would defeat the entire point of surfacing it rather than coercing it away (§11).

`age` means **ANTI-STARVATION**: it rises `0 → 1` over `age_horizon_days` (15) and stays at 1
beyond it — **the older a unit, the higher it ranks on this term** — reading `created_at`, never
`last_touched_at`. This is the first time this document defines the word: `last_touched_at` is
reset by use and `created_at` never is, so the two together disambiguate "has this been
revisited since capture" from decay's own signal, which already reads `last_touched_at`. A term
reading `last_touched_at` here would count decay a second time under a different name.

Anti-starvation is **bounded and transient**, not a floor. Under the multiplicative envelope,
the age term multiplies `effective_weight` instead of adding to it, and that shape caps how far
it can reach: its entire lifetime leverage is `age_weight` (20 %) over `effective_weight`, it
never grows past `age_horizon_days`, and it re-ranks only among units that still hold weight — a
unit sitting at the archive floor gains no more power from age than a unit brand new does, and
neither is rescued once its weight is gone. At the base decay rate, an untouched unit's priority
genuinely **rises**, peaking at `age_horizon_days` at a few percent above its own day-zero value,
and **declines monotonically thereafter** exactly as decay alone would, scaled by the saturated
nudge. The lift is real but small and time-boxed: a two-week grace window of a few percent
during which a re-ranking among live units can happen, not a mechanism that rescues anything —
an item that has decayed under `weight_threshold` is designed to be archived (§1, §6), and only
an additive term outside this envelope could change that, which is exactly the shape this
section rejects above.

- Weight ≠ priority: weight is intrinsic and persisted; priority is contextual to the moment
  and computed. The formula lives in the application layer (typed, testable, heavily
  calibrated) — migrate it to SQL only if volume justifies it.
- **Two focuses, one table**: task focus (top-N of actionable types — `task` and `event`; a
  meeting in two hours is the strongest possible answer to "what should I be doing", and a
  `list` is a container rather than a thing that can itself be done) and load focus (top-N of
  `mental_load`) are two queries with different criteria over `units`. A third focus = another
  query, not another schema.
- **Anti-jitter hysteresis**: a challenger must beat the incumbent by more than
  `hysteresis_margin` (default 0.05, **relative** — a challenger displaces only when it exceeds
  `incumbent × (1 + hysteresis_margin)`, never an absolute band, because `priority` has no fixed
  scale under the multiplicative envelope above: an absolute 0.05 would mean a 5% margin at
  priority 1.0 and a 1.25% margin at priority 4.0, damping weakest exactly where the contested
  values are largest) to displace it from the focus. `hysteresis_margin`'s stored column carries
  no `CHECK` constraint, so a configured value outside `[0, +Inf)` — non-finite, or negative,
  which would invert the margin's own direction — resolves to 0 (no anti-jitter protection that
  round) rather than being trusted as-is (`internal/core/focus.ResolveMargin`, Judgment Day round
  1). Note that this is a deliberate discontinuity, not a limit: an arbitrarily large **finite**
  margin passes through and makes the incumbent effectively unseatable, while `+Inf` resolves to 0
  and protects nothing. `+Inf` is not the end of that trend, it is a value with no valid
  arithmetic — a `priority` of exactly 0 is reachable, and `0 × (1 + ∞)` is `NaN`, which would
  make a corrupted incumbent permanent. This requires remembering the previous focus — in process, at the cost of one un-damped
  transition immediately after every restart,
  since there is no incumbent yet to compare a challenger against. The previous focus is also
  what `relation_to_active_focus` reads (above), so that same first ranking after a restart has
  `previous` empty for both mechanisms at once: `relation_to_active_focus` is 0 for every unit
  and the term vanishes entirely, not only hysteresis. Two effects from one restart, not one.

## 4. Relations

Directed edges between units: `type` (text: `same_topic`, `derived_from`, …), `strength`
(0–1, returned by the judge: 0.1 a passing mention, 0.9 the new node IS about that relation),
`confidence` (0–1), `created_by` (`system` | `consolidation` | `user`). Unique per
`(from, to, type)`.

- `created_by` matters: explicit user corrections are the strongest learning signal, and "how
  often does the user delete relations from the nightly job?" is THE quality metric for
  connect.
- **Per-user, per-relation-type thresholds** (`relation_thresholds`):
  - `min_confidence_to_persist` (default 0.30) — below this, it is not even stored.
  - `min_confidence_to_surface` (default 0.50) — above this, it is asserted without asking.
  - **Uncertain band [persist, surface)**: it is stored AND asked about in the digest ("I
    linked X with Y, are they related?"). Confirming raises confidence
    (`GREATEST(current, confirmed_floor)`); rejecting deletes the relation and emits a
    `relation_reject` signal.
  - These thresholds are what the learning module tunes per user (§9).
  - When `relation_thresholds` holds no row yet for a given type — relation type is open text,
    so no seed could ever be exhaustive — the two defaults above come from named constants in
    `core/relation`, not a migration-seeded row.

**A duplicate is recorded, not merged.** When the judge answers that a new capture duplicates an
existing unit, the duplication becomes a **relation** between the two and nothing else happens:
the new unit is not superseded, the existing one is not revived, and neither is edited. Both
survive, and the fact that they say the same thing is stored as an edge rather than acted on.

That is deliberate. Merging is a destructive decision made from one model call, and §1's rule that
nothing is deleted applies to content the user actually wrote. This argument is about a decision the
model makes **whole** — it infers that two units are the same, and infers that they should become
one. It does not cover a change the **user asked for** in words: there, only the target is
inferred, and §5 step 4 says what that costs. Recording the duplication keeps
both texts and leaves the merge available later, to a pass that can weigh more than one judgment.

**The direction of a relation is what the judge said, not a canonical form.** An edge runs from
the new unit to the one the judge named, and no ordering is imposed on the pair. Two units
related in both directions therefore hold two rows rather than one. This is a known limitation
rather than a design: deduplicating symmetric edges needs a rule for which direction survives, and
that rule belongs with the consolidation pass that can see the whole graph.

**A judgment that decided nothing writes nothing.** If the judge's answer is `new`, or if it
degrades so far that the outcome, its confidence or its target is missing, no relation is stored
and no decision is recorded. There is no effect to record — the same reason a read writes no row
(§11).

## 5. Capture

Synchronous pipeline on receiving a message (from any channel or the UI):

1. **classify** (LLM, a single call): returns `type` + `normalized_content` +
   `structured_data` + initial weight/λ + fields resolving pending answers +
   prospection's two capture fields, `interrupt_level` (0-1, §7's push/digest split) and
   `recurrence_rule` (`yearly | monthly`, §7's recurrence, decoded rather than left inside
   `structured_data`). Classification
   taxonomy: `task | mental_load | event | knowledge | procedural | emotional | chitchat |
   out_of_scope | recall | correction | timer | recurring_reminder | list`.
   - `recall`, `knowledge` and `correction` are separated by **what the message does, not
     what it is about**: `recall` asks for something already held, `knowledge` tells a fact to
     keep, `correction` alters something captured earlier. The distinction is load-bearing
     rather than cosmetic — a `recall` persists no unit, so classifying a question as
     `knowledge` files the question away instead of answering it, and the user never gets an
     answer. Symmetrically, an imperative that moves an existing thing ("move the renewal to
     the 20th") is a `correction`, not a new `task`. The prompt states all three, because a
     bare vocabulary list lets a model match the topic word instead of the act.
   - Injected context: active self-beliefs, local date + user timezone (to resolve "tomorrow",
     "on Friday"), open check-ins.
   - One message can resolve a check-in **and** be a capture at the same time ("yes, I
     practiced yesterday" → `nudge_outcome: engaged` + a `knowledge` unit). These are
     orthogonal fields, not types: `nudge_outcome (engaged|declined)`,
     `relation_outcome (confirmed|rejected)`, `state_outcome (confirmed|denied)`,
     `task_checkin_outcome (done|snooze|drop)`, `list_op (append|delete|mark_done|remove)`,
     `person_ref_status (resolved|new|ambiguous)`.
   - Robustness: a malformed field degrades to null (that resolution is ignored), it never
     brings down the whole classification.
2. **hybrid recall**: top-K by vector similarity + top-K by FTS, fused. Same mechanism serves
   both answering a `recall` and finding connection candidates.
   - **One model per search.** Vector similarity is only defined between embeddings produced by
     the same model. A vault can hold two models at once while a reindex is in progress, so
     every vector search filters by model, and vectors from two models are never compared or
     fused. See [ADR-0003](adr/0003-embeddings.md),
     [ADR-0012](adr/0012-vector-proximity-search.md).
     - Mechanism (`internal/core/recall`): a `VectorQuery` names one model; a `VectorIndex`
       holds embeddings for exactly that one model. A vault holding two models therefore holds
       two `VectorIndex` values — never one index serving both — so there is no shared score
       for two different models' embeddings to be compared through.
3. **dedup/relation judge** (LLM): against the recall candidates it decides
   `new | duplicate | related` — and if `related`, with what strength/confidence (subject to
   the thresholds in §4).
   - Robustness: a provider outage on this call degrades the capture rather than refusing
     it — the unit stays stored, no relations are evaluated for it, and the outage is recorded
     in the trail. The product rule below ("asking is the EXCEPTION") governs this the same way
     it governs every other capture-time provider outage; this step is not a special case.
4. **corrections**: a `correction` edits the referenced unit in place and emits a learning
   signal with the correction.
   - **Which unit it edits.** A caller holding an identifier passes it, and that identifier
     wins — the UI and the API both have one. Chat has none, so there the referent is resolved
     by running step 2's hybrid recall against the correction text and applying a gate over the
     **scored** candidates: with no candidate, or with the top two closer together than
     `correction_referent_margin`, the system asks instead of guessing. That is the product rule
     below rather than an exception to it — an ambiguous referent is exactly the ambiguity that
     blocks.
     - The margin is a **ratio** between the top two scores, not a difference between them. RRF
       compresses: at `k = 60` a candidate ranked first on both legs scores `2/61` and one
       ranked second on both scores `2/62`, so 0.0005 separates a near-tie — while a candidate
       present on only one leg scores `1/61`, half the first one. An absolute gap would mean
       something different depending on how many legs contributed; a ratio does not.
     - The boundary is **inclusive**: "closer together than `correction_referent_margin`" is a
       strict inequality on the *ask* side only, so a ratio exactly equal to the margin picks,
       not asks. Only the top two scored candidates ever participate — a third or later
       candidate never changes the answer.
     - The gate runs over the **live** candidates, after archived/superseded units are dropped
       from the recall result — never before. A ratio computed before that filter would gate
       the surviving top candidate against a score that belonged to a unit nobody can correct,
       and the failure is invisible: the gate just picks differently.
     - The gate is a pure function of the scored candidates: no LLM, no I/O, no clock. It
       therefore needs `internal/core/recall` to expose a fusion that keeps its scores instead
       of only its ranked identifiers.
   - **The classification has to carry the corrected value, and the prompt has to ask for it.**
     Everything below turns on whether the classification resolved a date, so a correction that
     comes back with only `normalized_content` silently takes the content branch and leaves the
     date it was correcting untouched — the worst outcome available, since it overwrites the
     unit's body *and* fixes nothing. Observed against a live model on 2026-08-04: asked to
     classify "no, the dentist is on the 15th, not the 14th", it returned no `event_at` at all,
     reasoning that a correction is not an event. §5's prompt therefore states plainly that a
     correction carries the corrected **value** rather than a description of the change, that a
     corrected date belongs in `event_at`/`due_at` resolved against the local date, and that a
     correction still answers every required field like any other type — that last clause is not
     decoration: without it the model traded the required fields away for the date.
   - **Which field it writes.** A correction writes **exactly one** field of the referent unit,
     never more: `event_at` if the classification resolved it and not `due_at`; `due_at` if it
     resolved that and not `event_at`; `content` only when **neither** date resolved, as the
     no-date fallback. Dates win over content whenever either is present — writing `event_at`
     or `due_at` from the classification's own fields of the same name requires no inference,
     while writing `content` from `normalized_content` requires inferring that the model's
     normalization of the correction *utterance* is the referent's new *body*, licensed only
     when there is nothing else to write. **Two dated fields present, or neither a date nor
     content, is an ask**, the same ask-shaped result an ambiguous referent already produces —
     ambiguity over *what* to write is exactly the ambiguity the product rule below blocks on,
     the same way ambiguity over *which* unit is.
     - **Accepted cost, stated rather than hidden.** A correction that moves a date leaves the
       referent's body stale: the content still reads whatever it read before, while `event_at`
       or `due_at` now carries the corrected value. This is deliberate — an earlier revision of
       this rule wrote every field the classification resolved, which meant a correction like
       "no, it's the 15th, not the 14th" also overwrote the unit's content with the correction
       utterance itself, destroying the thing the unit was there to remember. Inconsistent but
       useful beats consistent but empty. ADR-0016's pre-image keeps the previous value of the
       one field a correction actually changes, so the staleness is visible in the audit trail
       even before a later correction fixes the body itself.
   - **What it overwrites is recorded before it is overwritten** ([ADR-0016](adr/0016-correction-pre-image.md)).
     The user asked for the change, so the edit is authorised; but *which* unit it lands on is
     inferred, and an inference that destroys is the thing §4 refuses. Writing the previous values
     into the decision's own glass-box row separates the two: the instruction is honoured, and the
     inference stays reversible. The row is written first — if it fails, the edit does not happen.
     - **The pre-image's shape**, settled by the PR that implements it, since ADR-0016 leaves its
       exact keys open: one `correction.applied` row whose `context` carries
       `{unit_id, fields, previous, next, referent}`, `previous`/`next` objects keyed by column
       name (never by position, so a reader never consults a tag to know what a value means) —

       ```json
       {
         "unit_id": "u-8f1c…",
         "fields": ["event_at"],
         "previous": { "event_at": "2026-08-14T09:00:00-03:00" },
         "next":     { "event_at": "2026-08-15T09:00:00-03:00" },
         "referent": { "source": "recall", "score": 0.0328, "runner_up_score": 0.0164, "margin": 2.0 }
       }
       ```

       `previous.event_at` is `null` when the column was empty before the edit. `referent.source`
       is `"recall"` or `"explicit"`; the three score keys are **omitted** on the explicit path
       rather than written as zeros — an absent key is the truth, a zero score is a claim nobody
       computed.
   - Recording is not undoing. The previous value is retrievable; no surface offers it back until
     the UI exists.
5. **hooks**: dated events arm triggers (§7); `timer` arms an ephemeral timer (§8); a
   recurring `event` (a birthday) arms a recurring trigger; ambiguous references to people
   leave the unit `incomplete` until the disambiguation answer arrives.
   - **M1 note**: M1 classifies `timer`, `recurring_reminder`, and `person_ref_status:
     ambiguous` per this contract, but arms nothing and creates no `incomplete` unit —
     arming a timer or trigger is M3, and the `incomplete` promotion path is M2. Until
     then, a `timer`/`recurring_reminder` capture is refused outright (no `units` row —
     §8's "a timer is NEVER a unit"), and an ambiguous person reference persists as an
     ordinary `pool` unit with the ambiguity logged, not held as `incomplete`.

**Product rule: asking is the EXCEPTION.** Nooma captures with what it has, decides on its
own, leaves an auditable trace, and only asks when ambiguity blocks it (e.g. two different
"Ana"s).

### 5.1 What "degrades to null" means, field by field

Step 1's robustness rule above is a claim about *every* field independently. This section says
what it means in practice, because "degrades to null" has to be true of a truncated stream, a
wrong-typed value and an out-of-vocabulary value alike — three different failures that must not
be allowed to collapse into "the classification failed".

**A classification is never all-or-nothing.** Each field is optional at the type level and
carries its own record of what was lost, so a capture with one broken field still produces a
unit from the fields that survived. The record exists because §11 requires the reasoning behind
an automatic decision to be written down: a decoder that discarded *why* a field vanished would
force the rest of the pipeline to guess.

**Truncation is a per-field event, not a per-response one.** A model whose output is cut off
mid-sentence has still emitted complete fields before the cut. Those fields are read and kept;
only what never arrived is missing. The rule is that a member is either read in full or treated
as absent — a half-read value is never stored, because a plausible-looking fragment is worse
than a recorded absence.

The floor: a response from which **no** field can be read at all is not a classification with
every field null. It is a failed classification, and it is reported as one. A payload with no
fields has nothing to degrade.

**A response may carry a preamble around its object, and the preamble is discarded, not
treated as a malformed field.** A model asked to answer with one JSON object and nothing else can
still wrap that object in a markdown code fence, or add a line of its own prose before it — a live
OpenAI model did exactly this, ignoring the prompt's own instruction, and every one of that run's
prompts failed before this was fixed. The decoder locates the object by its first `{` and reads
from there; everything before it is discarded unread and never inspected for meaning. A response
is judged **by the object it carries**, not by whether that object arrived bare. When the first
`{` the decoder finds does not open a decodable object — a stray brace inside ordinary prose, for
instance — that is the same "nothing could be salvaged" failure a response with no object at all
reports: the decoder does not search further for a *later*, better `{`, because guessing which
brace is the real one would trade a loud failure for a silent, possibly wrong, classification.

**A value outside its vocabulary degrades; it is never coerced.** `type` and the six orthogonal
fields above each name a **closed** set of values. A model that answers `type: "grocery"`, or
`list_op: "prepend"`, has said something Nooma has no meaning for, and the honest response is to
drop that one field — not to guess the nearest member, and not to invent a fourteenth type.

This is why the vocabularies are closed in the first place. An open vocabulary would have
nothing to detect against: every answer would be valid by construction, and a model drifting
away from the taxonomy would look exactly like a model using it correctly. Closing the set is
what converts "the model said something odd" from an unnoticed silent event into a recorded one.

The distinction that matters here is between a value of the **wrong shape** and a value of the
right shape that is **not a member** — a `type` recorded as a number versus a `type` recorded as
`"grocery"`. Both degrade the field, and they are recorded as different events, because they say
different things about the model: one is a formatting failure, the other is a vocabulary
failure, and §9's learning loop should not confuse them.

Two vocabularies may share a value without being the same vocabulary — `relation_outcome` and
`state_outcome` both admit `confirmed`, and they diverge on `rejected` versus `denied`. A value
belonging to *some* vocabulary is not the test; belonging to *this field's* vocabulary is.

**Field by field**, then — what a degraded value becomes, and what is lost with it:

| Field | Degrades to | And that means |
|---|---|---|
| `type` | no type | The capture has no taxonomy value, so **no unit is created from it**. Every other field still decodes; what to do about a typeless capture is a decision for the pipeline, not the decoder |
| `normalized_content` | no content | The unit has nothing to store or embed. Recall cannot reach a unit with no content, so this is the second field whose loss is not survivable downstream. **"No content" includes an explicit `null`**, which degrades rather than becoming a claimed empty string — and this is the field where that distinction bites hardest, because `ToUnit` refuses a nil content and a claimed `""` walks straight past that refusal into a stored unit |
| `structured_data` | absent | The one field that cannot fail on shape: it is free-form by definition (§5 step 1), so any value the stream completed is valid. It is opaque to the brain and stays opaque |
| `weight`, λ | no value | Both are required on a stored unit, so the base priors below fill them. A degraded weight is not a zero weight — that distinction is why the fields are optional at the type level rather than defaulting to `0`. **"No value" includes an explicit `null`**, which is a degradation and not a claimed zero: a null arrives as a *present* field, so it never reaches the missing-field branch, and a decoder that reads it into a non-pointer number silently stores the zero instead. The distinction is the field's whole reason for being a pointer, and a pointer field alone does not secure it — the decoder has to read into a pointer too |
| dated fields | no date | The unit is stored undated. Nothing is armed for it (§7), because arming a trigger on a guessed date is worse than not arming one |
| the six orthogonal fields | absent | The resolution that field carried is ignored — the pending check-in, relation or state question stays open. The capture still becomes a unit |
| `interrupt_level` | no value | Absent or out-of-range degrades like any other field (owner ruling 1's Option A); `internal/core/prospection.ResolveInterrupt` (§7) then supplies `default_interrupt_level`, never this decoder — a degraded reading is never coerced into a claimed number here, only at that later layer, and it stays marked degraded there too |
| `recurrence_rule` | no value | A value outside `yearly \| monthly` degrades like any closed vocabulary; `internal/core/prospection.Arm` (§7) still arms the dated occurrence as a one-shot trigger — the capture is honoured, the recurrence itself is not invented |

**A date is degraded in two distinguishable ways**, and they are recorded separately: a value
that is not text at all, and text that is not a date Nooma reads. Only two date formats are
accepted — a full timestamp with its own zone, and a bare calendar date. A bare date has no zone
of its own, so it becomes midnight **in the user's timezone**, which is supplied with the
request (§5 step 1's injected context) and never read from the machine Nooma happens to run on.
The same vault syncing between two machines in two zones must classify a date identically.

**Absent and truncated are different events**, and the record says which. "The model did not
emit this field" is a fact about the model; "the stream ended before this field arrived" is a
fact about the transport. §9's learning loop draws opposite conclusions from them, and §11's
audit trail would be misleading if it merged them.

That distinction has a limit, stated here rather than papered over: truncation is detectable for
the response as a whole, so a *required* field missing from a cut response is recorded as
truncated. Which **optional** fields a cut response would have carried is unknowable — the model
may never have intended to emit them — and Nooma does not guess. Only the required fields'
absence is reported; an optional field's absence is the ordinary case, not a loss.

**Two base priors fill a degraded weight or λ, and there are exactly two.** §2 says type orients
the direction and the self-model personalizes the value — both of which the *model* does, through
the prompt. So a degraded weight does not fall back to a per-type table of hand-tuned numbers:
inventing nine of those would be inventing calibration this document never stated, in the one
place it says the model decides.

The two numbers are the ones the schema already declares as its column defaults, and they are
pinned to it by a test that reads the migration off disk. One number, in two places that cannot
drift apart — not two numbers that agree today. §13 carries them in the calibration table.

Neither may be zero, and that is the failure worth naming: a unit born at weight 0 is
indistinguishable from one the user ignored for months, and a λ of 0 never decays at all, so §6's
archiving pass can never reach it. Both look like ordinary data and neither violates a NOT NULL
constraint.

**The user's timezone reaches the model inside the instant, never from the environment.** §5
step 1 injects the local date and zone so the model can resolve "tomorrow" and "on Friday", and
the brain is forbidden from reading either from the machine it runs on. Both travel inside the
single timestamp the pipeline reads once per capture: its calendar date is the local date, its
location is the user's zone.

This is why there is no timezone setting anywhere in Nooma's configuration. Adding one would
create a second source of truth for a fact the timestamp already carries, and the two would
disagree the first time either changed. The known limit: a vault hosted for a user in a zone
other than the process's would need this revisited — which is multi-tenancy, deliberately out of
scope for v1.

**The vocabularies the model is offered are the same ones the decoder accepts.** The prompt does
not restate the taxonomy in prose; it renders each closed set from the same declaration the
decoder matches against. A value added to a vocabulary therefore reaches the model with no second
edit, and the model can never be asked for a value that would then be rejected as
out-of-vocabulary — a failure that would look like the model misbehaving when it was the prompt
that was stale.

**Two degradations stop a capture from becoming a unit at all**, and they are the two §5.1's table
already marks as unsurvivable. A capture with no `type` has nothing to decide from; a capture with
no `normalized_content` has nothing to store or embed, and storing it empty would create a unit
that exists and can never be found — the full-text index holds nothing for it and its embedding is
of the empty string. Both are reported as failures rather than written as rows, because a capture
that failed loudly can be retried and an unreachable row cannot even be noticed.

Every other degradation still produces a unit. That asymmetry is the point: the classification
degrades field by field, and only the two fields the unit cannot exist without are allowed to stop
it.

**Provenance is the caller's fact, never the brain's.** Which channel a capture arrived through —
chat, the UI, anything added later — is known only at the edge that received it, and it travels
inward as data. Nothing in the decision layer names a channel, so nothing has to be revisited when
a new one is added; a default baked in at the centre would not fail when it was wrong, it would
record the wrong provenance and look correct.

### 5.2 The scored fusion — a named output of the same mechanism

Step 2's hybrid recall and step 4's correction referent gate share one fusion mechanism, not
two: Reciprocal Rank Fusion, `score(d) = Σ w_i/(k + rank_i(d))` (§5 step 2,
[ADR-0010](adr/0010-hybrid-recall-fusion.md)). Step 2 has only ever needed the resulting order;
step 4's gate needs the scores themselves, to compute the ratio between the top two candidates
that decides whether the referent is unambiguous (§5 step 4). So the mechanism gains a second,
additive output alongside its existing ranked-ids form: the same computation, keeping each
candidate's score. The ranked-ids form is a projection of the scored one, not a second
implementation — one place for ADR-0010's own bias to live.

**Every fused score is strictly positive**, and that is load-bearing, not incidental: a
candidate is returned only when it is present in at least one of the fused lists, and every term
`w_i/(k + rank_i(d))` is positive whenever the list's weight `w_i` is positive (§13's
`weight_vector` and `weight_lexical` both default to 1.0), `k = 60` is positive, and
`rank_i(d)` is at least 1 — so a sum of one or more positive terms is positive. §5 step 4's gate
divides the top candidate's score by the runner-up's; strict positivity is what makes that
division always defined, for any candidate set the gate ever sees.

## 6. Nightly consolidation ("sleep")

One pass per night (default 03:00), phases IN ORDER — each one a pure function over the
vault, individually invocable:

```
expire_incomplete → archive → strengthen → connect → derive → reweight → pattern_eval → learn
```

`config.consolidation_enabled = 0` suppresses the nightly pass **and** [ADR-0009](adr/0009-scheduler-downtime.md)'s
boot catch-up — the two are one body of work behind two triggers.

1. **expire_incomplete**: `incomplete` units older than 24 h are resolved, and promotion is the
   default: **promoted with what they have**, unless the ambiguity was put to the user and left
   unresolved, in which case the unit is **archived** instead — the `incomplete → archived`
   transition §1 already names. This is §1's own two-outcome text, restated here rather than
   left to contradict it: an earlier revision of this section described only promotion, and §1
   described both outcomes and named the transition the status vocabulary reserves for the
   second one. Promotion is the default and archival is the exception a caller must evidence,
   never the reverse — nothing in M2 can record that an ambiguity was put to the user and
   settled (a producer for that evidence arrives later), so defaulting to archival here would
   silently cool every ambiguous capture, the opposite of "cautious to capture"
   (`internal/core/consolidation.ExpireIncomplete`).
2. **archive**: `effective_weight < weight_threshold` (default 0.5) → `archived` — the
   comparison is **strictly** less than; a unit sitting at exactly `weight_threshold` is not
   archived. At the shipped defaults, this composes with §2's revive and resurface guarantees:
   one direct revive always clears the archive band (`revive_gain * weight_ceiling >
   weight_threshold`), and spreading activation alone, at maximum hop distance, never does
   (`resurface_attenuation ^ resurface_max_hops * weight_ceiling <= weight_threshold`) — both
   are properties of the chosen defaults, not a general guarantee, since `weight_threshold` is
   configurable per vault through the `config` row
   (`internal/core/consolidation.Archive`/`ResolveWeightThreshold`). `weight_ceiling` is not:
   §13 marks it neither ⚙ nor configurable, and no `Resolve*` reads it — it is a bare constant.
3. **strengthen**: re-evaluates relation strength with accumulated evidence — both endpoints'
   `last_touched_at` at or after the previous pass (`since`) counts as co-use, and a qualifying
   relation's strength rises asymptotically toward 1: `s' = s + strengthen_gain * (1 - s)`.
   `since == nil` (the vault has never consolidated) evaluates nothing — accumulated evidence over
   no interval is no evidence. Strength never falls here: this document gives exactly one way it
   moves down — the user rejects the relation, which deletes it (§4) — and decay is never consulted
   by this phase (`internal/core/consolidation.Strengthen`).
4. **connect**: ranks up to `connect_source_limit` (20) live, recently-touched units by
   effective weight, and for each runs hybrid recall (§5.2's fused ranking) to find up to
   `connect_candidate_k` (5) unjudged candidates. The per-night provider cost is **one
   product**, `connect_source_limit * connect_candidate_k` = at most 100 judge calls, and that
   product — not the two factors separately — is the number actually calibrated
   (`internal/core/consolidation.SelectConnectSources`/`ConnectPairs`). A candidate already
   related to its source is excluded regardless of which direction the existing relation runs
   or its type — the exclusion key is the **unordered** pair, a stated and reversible choice
   that spends a possible second relation type between the same two units to save a judge call
   against this per-night budget (`CanonicalPair`, used for this lookup only — a stored or
   proposed relation still runs `source → candidate`, per §4's own direction rule). New
   relations get `created_by='consolidation'`
   (`internal/core/consolidation.ProposeRelation`).
5. **derive**: derives/updates self-beliefs from units (§10), rendering each one's key as
   `derived/{facet}/{key}` (`internal/core/consolidation.DeriveTopicKey`). The units a pass
   derives from are the same recently-touched, effective-weight-ranked, `connect_source_limit`-
   capped selection `connect` uses (item 4 above) — the identical function, but `derive` re-runs
   it over its own fresh read rather than reusing `connect`'s, so `--phase=derive` alone behaves
   the same as slot 5 of a whole pass (`internal/brain`'s own single-execution-path rule: caching
   one phase's read for another would make the two paths differ). Dedup runs **two defenses**:
   (1) existing beliefs placed in the derivation prompt itself, so the judge sees what already
   exists before proposing something new — the prompt builder
   (`internal/core/consolidation.BuildDerivePrompt`, rendering every active belief's
   `topic_key`/`content`, or stating plainly that none exist yet when there are none) and the
   `internal/brain` orchestration that calls it are both **shipped**; (2) a semantic merge over
   embeddings, cosine ≥ 0.85, for whatever the first defense's prompt-side judgment would still
   let through (`internal/core/consolidation.MergeProposals`) — **shipped**: a proposed belief
   merges into the nearest existing one at or above that threshold, or becomes a new belief
   otherwise. A belief that merges is **reinforced**, not duplicated: its confidence rises toward
   1 by the same asymptotic law `strengthen` uses for relation strength, at
   `belief_reinforce_gain` (default 0.10) — `internal/core/consolidation.Reinforce`.
   **The embedding cost, stated rather than left implicit (owner ruling Q2, option A)**: `derive`
   embeds every **active** belief in memory at the start of the phase and discards the vectors
   after — no schema change, no `belief_embeddings` table, no stale-vector problem when a belief's
   text is later edited. The cost is one provider call per active belief, every night, growing
   with the belief count; this is accepted because the self-model is a handful of facets by
   construction (§10's five-facet vocabulary), not an open-ended corpus. If belief counts ever
   reach the hundreds, a persisted `belief_embeddings` table (option B) becomes the right trade —
   that migration is `m2c`'s to make if it ever becomes true, not this change's.
6. **reweight**: post-connection weight adjustments (decay materialization remains optional and is
   not exercised by M2's `reweight`) — every unit a new relation joined this pass spreads
   activation to its new neighbours through §2's resurface mechanism, over this pass's new edges
   only, merged per unit by the highest boosted weight across origins. Materialization is declined
   here, not forbidden: §2 already defines `last_touched_at` as the vault's record of *direct* use,
   and a bulk write moving it for every sufficiently old unit would make that reading false for
   exactly the untouched units where it matters — `strengthen`'s own co-use predicate, three phases
   earlier in the same pass, would then see the consolidation pass itself as a user
   (`internal/core/consolidation.Reweight`).
7. **pattern_eval**: runs the pattern watchers (§7): goal stagnation, mental load accumulation.
8. **learn**: the learning module consumes new signals (§9). ALWAYS last. In M2 this slot
   performs no work and writes no `decision_log` row — the phase exists as a no-op placeholder
   occupying the slot; M5 fills it.

Every decision with an effect (archive, connect, derive, adjust) writes to the
`decision_log` (§11).

## 7. Prospection — the proactive lobe

**Triggers** have their own lifecycle (a separate table, not a field on units — one event can
have N nudges; a pattern watcher does not hang off any unit):

- `kind`: `time_based` (fire_at; armed by capture or by the user) | `event_based` (condition
  evaluated in the capture hook) | `pattern_based` (condition evaluated during consolidation).
- `status`: `armed → fired → …` | `dismissed` | `expired`.
- **Staleness** ([ADR-0009](adr/0009-scheduler-downtime.md)): overdue is measured from the first
  instant the item could actually have been delivered, not from `fire_at` directly —
  `DeliverableFrom(fire_at)` for a trigger (that day's quiet-hours end, if `fire_at` falls
  inside them), `fire_at` itself for a timer, which is exempt from quiet hours (see below).
  This exists because the quiet-hours window (seven hours, `[00:00, 07:00)`) is longer than
  `trigger_staleness_hours` (six): measured naively from `fire_at`, every trigger armed
  between 00:00 and 01:00 would expire before the user woke, every night. Past that first
  deliverable instant, `trigger_staleness_hours` (6,
  `internal/core/prospection.TriggerStalenessHours`) and `timer_staleness_hours` (3,
  `internal/core/prospection.TimerStalenessHours`) govern expiry, quiet hours are evaluated
  before staleness, and overdue by more, a trigger goes `expired` and a timer goes
  `cancelled` — never fired (ADR-0009). A delivered-but-late item mentions the delay
  explicitly once overdue reaches `delay_caveat_minutes` (15 minutes — three shipped
  `proactive_check` ticks, `internal/core/prospection.DelayCaveatMinutes`).
- Delivery lifecycle: `fired_at` (fired) → `surfaced_at` (delivered to the user) →
  `responded_at` + `resolution` (`engaged` | `declined` | `self_healed` — fresh activity
  resolved it before the user answered).
- **Recurrence**: `recurrence_rule` (`yearly` | `monthly`) + `recurrence_anchor`
  (`{month, day}`). On firing, the next one is created automatically pointing at the SAME
  source unit — memory is not duplicated, only the nudge is re-armed.
  The next occurrence is **always re-derived from the anchor**, never advanced from the previous
  one, and a day the target month does not have **clamps to that month's last** rather than
  overflowing into the next. The two rules hold each other up: 29 February advanced by a year is
  28 February, and advancing *that* is 28 February forever, so the anniversary drifts off its own
  date after one leap cycle — and a day-31 monthly reminder does the same after its first
  February. Re-deriving makes occurrence *N* the same instant however many times the trigger has
  re-armed, which is what lets re-arming be a pure function of `(rule, anchor, now)` rather than
  of the trigger's own history. Skipping the months that lack the day is rejected for the obvious
  reason: a day-31 reminder would fire seven times a year and never in February.
  Occurrences land at `recurrence_anchor_hour` local, which is deliberately not midnight. How many
  days a month has is read from the calendar, never from the user's zone: a zone that deleted the
  very day being looked up would otherwise report the wrong length — Pacific/Kiritimati skipped
  1994-12-31, so asking it for December's last day answers "the 1st", and every anchor above the
  first would clamp there. An anchor whose month or day is out of range is clamped into range
  rather than normalised, because normalising moves the occurrence into a month, or a year, the
  user never named.
- **Lead time**: default 7 days before the event, stored in `payload.lead_days` (the re-arm
  propagates it). Policy per event class; migrating it to a self-model preference is a
  deferred decision.

**Delivery — digest vs push** (`interrupt_level` 0–1, persisted on the trigger so the glass
box can audit it):

- `interrupt_level >= 0.7` → **immediate push**: a dedicated poll (the proactive_check),
  skipping cadence and gates. Quiet hours `[00:00, 07:00)` local time: deferred and
  resurfacing on waking — **except a timer**. A timer's instant was set by an explicit user
  instruction at capture ("remind me in 15 min"); an inferred trigger's instant was not, and an
  inferred trigger always defers through quiet hours regardless of its own `interrupt_level`. An
  explicit instruction outranks the quiet-hours policy window; an inference does not. Mutually
  exclusive with the digest (no double delivery).
- Below that → **digest** (pull, accumulates): with a cadence, and two care gates:
  - **Cadence**: once daily, at `digest_hour` (`internal/core/prospection.DigestHour`), decided by
    the instant the last digest went out rather than by a queue — so a vault that was off for three
    days owes one digest, never three. The hour is not free: before quiet hours end, every digest
    would be born deferred and the cadence would be decorative; after, a dead window opens in which
    quiet-hours-deferred pushes have resurfaced and the digest has not, making the user's first
    morning contact the lane this design says should be rarer. The hour quiet hours end is the only
    one that is neither, and `digest_hour` is a separate knob from `quiet_hours_end_hour` with the
    relation `digest_hour >= quiet_hours_end_hour` asserted, not their present equality.
  - if `current_state.energy` is low (recent reading), it holds back non-urgent items and only
    lets important ones through; deferred items resurface on recovery.
    **Low** is `energy < low_energy_max`, strict — this gate suppresses delivery, so the burden of
    proof is on low, and no reading at all is not low: silence is not an observation of depletion.
    **Recent** is within `energy_reading_max_age_hours`, one digest cycle, because a reading from
    two digests ago would hold items back on a day it never observed. **Important** cannot be an
    absolute cut: `focus.Priority` is homogeneous in effective weight, so a fixed threshold means
    something different in every vault. It is a relative truncation to `low_energy_digest_size`
    items by `focus.Priority` (owner ruling 4), and a trigger with no source unit — a pattern
    watcher, whose `unit_id` is NULL — has priority zero and therefore ranks last, so *"still on
    this goal, or shall we let it rest?"* is the first thing a depleted user stops being asked.
    **Anti-starvation** here is the digest's own bound, and it is a different mechanism from §3's
    `age` term, which carries the same name for the ranking. That one lifts a neglected unit's
    priority continuously; this one guarantees delivery outright after a fixed count, independent
    of rank and independent of whether energy ever recovers. It is a bound, not a delay: an item
    held back `max_digest_deferrals` times is
    carried regardless of rank, and carried *in addition to* the truncation rather than inside it —
    if it competed for the same slots, a low-ranked item could be starved by fresher ones forever,
    which is the thing the rule exists to prevent.
  - TONE softens when the user is loaded: the brain passes the fact (`loaded`), the render
    layer picks the words. Urgent push is NOT softened — `Interrupt.Route() == RoutePush` is the
    one exemption to the softening above.

**Degradation** (owner ruling 1; `internal/core/prospection.ResolveInterrupt`,
`Interrupt.Route`): classify emits `interrupt_level` per message. A `NULL` or unparseable
reading — absent, non-finite, or outside `[0,1]` — resolves to `default_interrupt_level` (0.0,
`internal/core/prospection.DefaultInterruptLevel`), never clamped: clamping a corrupt 1.7 to 1.0
would manufacture a push out of a number core cannot vouch for. The resolution itself is marked
degraded, a fact `Interrupt` carries separately from the level it resolved to — not an invented
sentinel weight (§5.1's own warning against reading a degraded number as a claimed zero). **A
degraded classification never produces a push**, structurally rather than arithmetically:
`Interrupt.Route()` checks the degraded flag before comparing the level against the push
threshold, so the guarantee survives a future recalibration of either number, and even a
forgotten resolution — an `Interrupt` nobody explicitly built — still cannot reach push. `brain`
persists `triggers.interrupt_level` as `NULL` exactly when the resolution degraded, and as the
claimed float otherwise; the round trip is exact in both directions, so an auditor reading the
glass box can always tell a claimed 0.0 from an absent reading. This is `m3a`'s contract to
state; `m3b` implements the store-layer round trip.

**Pattern watchers buildable from day one**:

- **Goal stagnation**: a `goal`-facet belief with no related activity for
  `goal_stagnation_days` (default 21, recalibratable per user, §9) → check-in "Still on this,
  or shall we let it rest?". "Related activity" is read off `self_beliefs.last_reinforced_at`
  — the only column that records it — and that reading is sound only because of the phase
  order (`internal/core/consolidation.EvaluateStagnation`, §6): `derive` runs before
  `pattern_eval` in every nightly pass and refreshes `last_reinforced_at` for every belief it
  re-derives; `pattern_eval` then reads the value THIS SAME PASS already refreshed. Reversing
  the order would make every reinforced belief look stagnant one more night — this is a data
  dependency, not an arbitrary sequence.
- **Load accumulation**: open `mental_load` units ≥ `mental_load_threshold` (default 7) →
  writes a tentative hypothesis into `current_state` ("feeling loaded lately?"), the user
  confirms/denies in the next conversation; a cooldown of `load_cooldown_days` (7,
  `internal/core/consolidation.LoadCooldownDays`, chosen — unrelated to
  `mental_load_threshold`'s own coincidentally-equal 7, a duration versus a count) after a
  resolved check-in. The nudge offers to **close one loop**: it lists the open ones, the user
  picks, it gets archived (soft, recoverable). The load watcher's `current_state` row is written
  with `source = 'consolidation'`, `mood = 'loaded'` and `energy` left NULL, and its cooldown is
  anchored on the previous hypothesis's own `recorded_at` because M2 has no resolution signal
  (`m2b` §9 Q6, mapped).

## 8. Ephemeral timers — infrastructure, NOT memory

"Remind me in 15 minutes to turn off the stove" is born from the `timer` outcome of classify
and goes into its own table. **A timer is NEVER a unit**: no weight, no decay, no graph, no
belief derivation. `pending | fired | cancelled`. `action_text` is nullable ("remind me in
15 min" with no object → a generic nudge). On firing, the LLM rephrases the text
(`rendered_text`) — the request is stored verbatim and only worded at delivery time. Listable
and cancellable from chat and from the UI.

## 9. Learning — the prediction-error loop

The system records how the user reacts to automatic decisions and adjusts PER USER. Two
tables: `learning_signals` (the raw record) and the tuned knobs (`relation_thresholds`,
`calibration`).

**Signals** (`signal_type` + `valence` positive/negative/neutral + target):
`correction`, `nudge_ack`, `nudge_ignored`, `nudge_engaged`, `nudge_declined`,
`belief_delete`, `belief_edit`, `relation_reject`, `relation_confirm`, `state_confirmed`,
`state_denied`. No FK to the target: the signal outlives the target's deletion (a
`relation_reject` is emitted right before the relation dies).
**The signal layer is channel-agnostic**: the same signal arrives from chat or from the UI.

**Nightly pass** (phase `learn`, incremental and idempotent via the `last_run_at` checkpoint):

- **Relation thresholds**: repeated rejections of a type (e.g. 6 of the last 8 `same_topic`)
  → raises `min_confidence_to_persist` for THAT user and THAT type (0.30 → 0.35): more
  conservative for whoever rejects, bolder for whoever accepts. Same brain, personalized.
- **Goal check-in cadence**: learns from whether the user RESPONDED (`responded_at`), not from
  whether it was delivered (`surfaced_at`) — 5 nudges with no response are 5 ignored ones.
  Systematically ignoring lengthens the interval (21 → 28 days); engaging shortens it.
  **Cooldown**: after adjusting, do not re-evaluate until the new interval has had time to
  show its effect.
- **Transparency**: a natural-language summary of what it learned and why ("I raised the
  same_topic threshold 0.30→0.35 — you rejected 6 of 8"), queryable and correctable (UI +
  API). What it learns is never a black box.

## 10. The self-model

Beliefs about the user that feed relevance: `facet`
(`identity | value | goal | social | preference`), `topic_key` (unique per user — derived ones
use `derived/{facet}/{key}`), `content`, `confidence`, `origin`
(`seed | derived | user_stated`), `status`, `last_reinforced_at`.

- Hybrid construction: seeding at onboarding + nightly derivation + direct correction.
- **Injection into classify**: on every capture, active beliefs enter as context → personalized
  weights and λ. The cycle capture → derive → inject → capture better is THE mechanism by
  which relevance improves over time.
- Editing or deleting a belief emits a learning signal (`belief_edit` / `belief_delete`).

**`current_state`** (the delicate facet): append-only rows with `energy` (0–1), `mood` (text),
`active`, `source` (`user` | `consolidation`). The load watcher opens it as a tentative
hypothesis; the user confirms or corrects. The append-only property is now structural at the
port — `StateRepo` has no update path. Consumers: digest cadence and tone. **Product rule**:
LOAD is cared for (observable), emotions are not interpreted. If forced to choose, keep `energy`
(capacity) and drop the mood labels.

## 11. The glass box

`decision_log`: ONE table for all modules (`action`, `rationale`, `context` JSON,
`occurred_at`). Every automatic decision with an effect is recorded with its reasoning.

- **Pull**: everything is recorded and explorable in the activity UI.
- **Push**: only the big or the uncertain is proactively mentioned (low confidence or
  high-impact decision). "Cautious to capture, selective to speak", applied to its own
  transparency — relief must not turn into an auditing chore.

## 12. Perception (phase 2 — design reserved)

A single multi-format door: any file (image, digital/scanned PDF, DOCX, XLSX) is normalized,
extracted, and routed **by its SHAPE**, not by domain: measurement → the `measurements` store
(time series by `domain`/`metric_key`, with `ref_unit_id` pointing at a `structured_ref`
anchor unit); meaning → the cognitive fabric; doubt (confidence < 0.4) → needs-review. Derived
insights per metric: one active per metric, the previous one becomes `superseded`. Responsible
framing guardrail: it describes trends, it NEVER diagnoses. The `channel` travels as caller
data, never decided by the client request. Robustness rules already learned: never fabricate
numeric values, bias confidence downward under doubt, zip-bomb guards on office formats, page
caps.

## 13. Calibration — numbers vs mechanisms

Initial defaults (global config; those marked ⚙ are recalibratable per user by the learning
module):

| Knob | Default |
|---|---|
| `weight_threshold` (archiving; `internal/core/consolidation.DefaultWeightThreshold` + `ResolveWeightThreshold`) | 0.5 |
| `incomplete_expiry_hours` (`internal/core/consolidation.IncompleteExpiryHours`) | 24 |
| `catch_up_staleness_hours` (`internal/core/consolidation.CatchUpStalenessHours`) | 24 — ADR-0009's boot catch-up gate; coincides with `incomplete_expiry_hours` above by coincidence, not by relation (a startup staleness window versus a phase's expiry window), no test ties them |
| `strengthen_gain` (`internal/core/consolidation.StrengthenGain`) | 0.10 — chosen, not derived; checked for compatibility (not entailment) against `goal_stagnation_days`'s default below |
| `connect_source_limit` (`internal/core/consolidation.ConnectSourceLimit`) | 20 — chosen; governs two phases as of `m2c` — `connect`'s own candidate search (item 4 above) and, separately, `derive`'s own fresh source selection (item 5 above, design §7.3: `derive` re-runs the identical selection over its own read rather than reusing `connect`'s, so `--phase=derive` alone behaves the same as slot 5 of a whole pass) |
| `connect_candidate_k` (`internal/core/consolidation.ConnectCandidateK`) | 5 — chosen; a separate knob from `dedup_candidate_k` below despite the identical default, per the same reasoning as `urgency_lead_days` above: one bounds capture's per-message judge calls, this one bounds connect's per-night budget |
| `hysteresis_margin` (focus, relative; `internal/core/focus.DefaultHysteresisMargin` + `ResolveMargin`) | 0.05 |
| `revive_gain` (`internal/core/weight.ReviveGain`) | 0.35 |
| `weight_ceiling` (`internal/core/weight.WeightCeiling`) | 2.0 |
| `resurface_max_hops` (`internal/core/weight.ResurfaceMaxHops`) | 2 |
| `resurface_attenuation` (`internal/core/weight.ResurfaceAttenuation`) | 0.5 |
| `urgency_lead_days` (`internal/core/focus.UrgencyLeadDays`) | 7 — a separate knob from "Event lead time" below, despite the identical default: one is prospection's notification horizon, this is the ranking's |
| `urgency_max` (`internal/core/focus.UrgencyMax`) | 3.0 |
| `age_weight` (`internal/core/focus.AgeWeight`) | 0.20 |
| `age_horizon_days` (`internal/core/focus.AgeHorizonDays`) | 15 — owner ruling 10; was 30 under ruling 9 |
| `focus_adjacency_weight` (`internal/core/focus.AdjacencyWeight`) | 0.25 |
| `focus_size` (`internal/core/focus.DefaultSize`) | 7 — a human attention bound, 7±2; coincides with `mental_load_threshold` below by coincidence, not by relation — no test ties them |
| λ per type (`weight_decay_rate`) | prior per type, base 0.01/day |
| Base weight when classify does not supply one | 1.0 |
| `min_confidence_to_persist` ⚙ | 0.30 |
| `min_confidence_to_surface` | 0.50 |
| `goal_stagnation_days` ⚙ (`internal/core/consolidation.DefaultGoalStagnationDays` + `ResolveGoalStagnationDays`) | 21 — `config.goal_stagnation_days` is this knob's one schema home for the whole of M2; `ports.ConfigRepo` reads it, never `calibration`'s own generic key/value row (`m2c` spec R2.5, discharging `m2b design.md` §9 Q3). `calibration` stays unused through `m2c`, verified by a source-tree scan (`test/conformance`); it remains reserved for M5's learning module to write arbitrary future per-user knobs that have no dedicated `config` column |
| `mental_load_threshold` (`internal/core/consolidation.DefaultMentalLoadThreshold` + `ResolveMentalLoadThreshold`) | 7 |
| `load_cooldown_days` (`internal/core/consolidation.LoadCooldownDays`) | 7 — chosen; unrelated to `mental_load_threshold`'s own coincidentally-equal 7 (a duration versus a count), no test ties them |
| Push threshold (`internal/core/prospection.PushThreshold`) | 0.70 — `interrupt_level >= this value` routes to push (R3.2), inclusive; gains a constant here, value unchanged |
| `default_interrupt_level` (`internal/core/prospection.DefaultInterruptLevel`) | 0.0 — fills a degraded or out-of-range `interrupt_level`; behaviourally inert below the push threshold, chosen so an audit reads "no claim was made" (design §3.4) |
| `recurrence_anchor_hour` (`internal/core/prospection.RecurrenceAnchorHour`) | 12 — derived: the local wall clock a recurring occurrence lands on. Not midnight, because a DST gap there normalises *backward* onto the previous calendar date (`internal/core/consolidation.NextDailyRun` records Havana mapping local 00:00 to 23:00 the previous evening), which would nudge an anniversary a day early once a year. Noon clears every transition shorter than twelve hours, and the only known longer ones delete the whole calendar date |
| `digest_hour` (`internal/core/prospection.DigestHour`) | 7 — the local hour the daily digest becomes due (owner ruling 2). Equals `quiet_hours_end_hour` today and is deliberately a separate knob: one is a delivery window's edge, the other a cadence. The asserted relation is `digest_hour >= quiet_hours_end_hour`, not their equality |
| `low_energy_max` (`internal/core/prospection.LowEnergyMax`) | 0.5 — chosen: `energy` is declared on [0,1] with no calibration data behind it, and the midpoint is the only point on such a scale that is not an invention. The comparison is strict, because the gate suppresses delivery |
| `energy_reading_max_age_hours` (`internal/core/prospection.EnergyReadingMaxAgeHours`) | 24 — derived from the cadence: the digest is once daily, so its input may not be older than one cycle. Coincides with `incomplete_expiry_hours` and `catch_up_staleness_hours` by coincidence, not by relation, and no test ties them |
| `low_energy_digest_size` (`internal/core/prospection.LowEnergyDigestSize`) | 3 — half `focus_size`, by the same reading that puts `low_energy_max` at the midpoint. Declared in Go as `focus.DefaultSize / 2`, so a recalibration of `focus_size` carries it |
| `max_digest_deferrals` (`internal/core/prospection.MaxDigestDeferrals`) | 3 — chosen inside a derived band: more than 1, or anti-starvation is a one-day delay wearing the name; strictly less than `load_cooldown_days` (7), or an item could be silenced across exactly the window in which the load watcher has stopped looking |
| `quiet_hours_start_hour` (`internal/core/prospection.QuietHoursStartHour`) | 0 — local hour at which quiet hours open, inclusive; **replaces the former "Quiet hours" row**, split in two because a Default cell starting with `[` fails the calibration gate's anchored numeric parse |
| `quiet_hours_end_hour` (`internal/core/prospection.QuietHoursEndHour`) | 7 — local hour at which quiet hours close, exclusive; the other half of the same split |
| Event lead time | 7 days |
| `belief_reinforce_gain` (`internal/core/consolidation.BeliefReinforceGain`) | 0.10 — chosen; inherits `strengthen_gain`'s reinforcement-law argument above, no compatibility check attached (a different quantity, no fixed night count ties to it) |
| Semantic belief merge (`internal/core/consolidation.BeliefMergeCosine`) | 0.85 — the minimum cosine similarity at which two beliefs merge |
| Perception confidence gate | 0.40 |
| Consolidation / proactive check (`internal/scheduler.ConsolidationHour`; not calibration-gate-checkable this way — the Default cell's leading `03:00` reads as `03` under the gate's anchored numeric parser, not the constant's own value `3`; splitting this row so the consolidation half can be checked is M3's job, the same PR that fills the proactive-check half) | 03:00 daily / every 5 min |
| `boot_consolidation_delay` (`internal/scheduler.BootConsolidationDelay`) | 120 s |
| `trigger_staleness_hours` (`internal/core/prospection.TriggerStalenessHours`) | 6 — ADR-0009's catch-up threshold for a time_based trigger; gains a constant here, value unchanged |
| `timer_staleness_hours` (`internal/core/prospection.TimerStalenessHours`) | 3 — ADR-0009's tighter catch-up threshold for an ephemeral timer; gains a constant here, value unchanged |
| `delay_caveat_minutes` (`internal/core/prospection.DelayCaveatMinutes`) | 15 — chosen: three shipped `proactive_check` ticks (`*/5 * * * *`), so scheduler granularity never produces a caveat on its own; the relation `delay_caveat_minutes >= 3 × proactive_check period` cannot be asserted yet — `internal/config/defaults.go` declares no schedule default today — and lands as an L2 test in `m3d` #1 once the tick has a Go home |
| RRF `k` | 60 |
| `recall_top_k` | 20 |
| RRF vector-leg weight (`weight_vector`) | 1.0 |
| RRF lexical-leg weight (`weight_lexical`) | 1.0 |
| `dedup_candidate_k` | 5 |
| `correction_referent_margin` (ratio of the top two fused scores) | 1.5 |

Exact values get calibrated with real usage; the mechanisms in this document do not.

**This table is executable.** Every row that names a constant under `internal/core/` is checked
against that constant by `test/conformance/calibration_doc_test.go`: the symbol must exist, be a
constant, and hold exactly the number written here. A row's Default column therefore leads with
its value, and any prose follows after an em dash. Rows naming no constant yet — `RRF k`,
`recall_top_k` — are not yet implemented, and each one starts being checked on the day
its row names the constant that implements it.
