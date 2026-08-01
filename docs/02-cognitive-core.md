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

**What is persisted changes only on discrete events**; decay is computed on read, never
written on every read:

- Persisted: `weight` (value at the last event), `last_touched_at`, `weight_decay_rate` (λ).
- **Revive** (direct use): writes a new boosted weight and resets `last_touched_at`.
- **Resurface** (related signal): propagates a boost along the graph edges, proportional to
  each relation's `strength` (spreading activation).
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
| Cold | `status='archived'` (its effective weight crossed the threshold during a consolidation) |

Warm→cold is done by consolidation; cold→warm/hot by a strong resurface.

## 3. Focus — computed, NEVER persisted

"Being in focus" is a **view**: sort the pool by effective priority and take the first N. A
persisted flag would create two sources of truth that desynchronize on their own (priority
rises with time). Invariant: `status='focus'` does not exist.

```
priority = f(effective_weight, temporal_urgency(due_at), type, age, relation_to_active_focus)
```

- Weight ≠ priority: weight is intrinsic and persisted; priority is contextual to the moment
  and computed. The formula lives in the application layer (typed, testable, heavily
  calibrated) — migrate it to SQL only if volume justifies it.
- **Two focuses, one table**: task focus (top-N of actionable types) and load focus (top-N of
  `mental_load`) are two queries with different criteria over `units`. A third focus = another
  query, not another schema.
- **Anti-jitter hysteresis**: a challenger must beat the incumbent by more than
  `hysteresis_margin` (default 0.05) to displace it from the focus. This requires remembering
  the previous focus (in-process state or a state row — not a flag on units).

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

## 5. Capture

Synchronous pipeline on receiving a message (from any channel or the UI):

1. **classify** (LLM, a single call): returns `type` + `normalized_content` +
   `structured_data` + initial weight/λ + fields resolving pending answers. Classification
   taxonomy: `task | mental_load | event | knowledge | procedural | emotional | chitchat |
   out_of_scope | recall | correction | timer | recurring_reminder | list`.
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
4. **corrections**: a `correction` edits the referenced unit in place and emits a learning
   signal with the correction.
5. **hooks**: dated events arm triggers (§7); `timer` arms an ephemeral timer (§8); a
   recurring `event` (a birthday) arms a recurring trigger; ambiguous references to people
   leave the unit `incomplete` until the disambiguation answer arrives.

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

## 6. Nightly consolidation ("sleep")

One pass per night (default 03:00), phases IN ORDER — each one a pure function over the
vault, individually invocable:

```
expire_incomplete → archive → strengthen → connect → derive → reweight → pattern_eval → learn
```

1. **expire_incomplete**: `incomplete` units older than 24 h are promoted with what they have.
2. **archive**: `effective_weight < weight_threshold` (default 0.5) → `archived`.
3. **strengthen**: re-evaluates relation strength with accumulated evidence.
4. **connect**: finds candidate pairs (hybrid recall among recent/hot units) and runs them
   through the LLM judge. New relations get `created_by='consolidation'`.
5. **derive**: derives/updates self-beliefs from units (§10). Dedup with two defenses:
   existing beliefs in the prompt + semantic merge when cosine ≥ 0.85.
6. **reweight**: post-connection weight adjustments (and optional decay materialization).
7. **pattern_eval**: runs the pattern watchers (§7): goal stagnation, mental load accumulation.
8. **learn**: the learning module consumes new signals (§9). ALWAYS last.

Every decision with an effect (archive, connect, derive, adjust) writes to the
`decision_log` (§11).

## 7. Prospection — the proactive lobe

**Triggers** have their own lifecycle (a separate table, not a field on units — one event can
have N nudges; a pattern watcher does not hang off any unit):

- `kind`: `time_based` (fire_at; armed by capture or by the user) | `event_based` (condition
  evaluated in the capture hook) | `pattern_based` (condition evaluated during consolidation).
- `status`: `armed → fired → …` | `dismissed` | `expired`.
- Delivery lifecycle: `fired_at` (fired) → `surfaced_at` (delivered to the user) →
  `responded_at` + `resolution` (`engaged` | `declined` | `self_healed` — fresh activity
  resolved it before the user answered).
- **Recurrence**: `recurrence_rule` (`yearly` | `monthly`) + `recurrence_anchor`
  (`{month, day}`). On firing, the next one is created automatically pointing at the SAME
  source unit — memory is not duplicated, only the nudge is re-armed.
- **Lead time**: default 7 days before the event, stored in `payload.lead_days` (the re-arm
  propagates it). Policy per event class; migrating it to a self-model preference is a
  deferred decision.

**Delivery — digest vs push** (`interrupt_level` 0–1, persisted on the trigger so the glass
box can audit it):

- `interrupt_level >= 0.7` → **immediate push**: a dedicated poll (the proactive_check),
  skipping cadence and gates. Quiet hours `[00:00, 07:00)` local time: deferred and
  resurfacing on waking. Mutually exclusive with the digest (no double delivery).
- Below that → **digest** (pull, accumulates): with a cadence, and two care gates:
  - if `current_state.energy` is low (recent reading), it holds back non-urgent items and only
    lets important ones through; deferred items resurface on recovery (anti-starvation).
  - TONE softens when the user is loaded: the brain passes the fact (`loaded`), the render
    layer picks the words. Urgent push is NOT softened.

**Pattern watchers buildable from day one**:

- **Goal stagnation**: a `goal`-facet belief with no related activity for
  `goal_stagnation_days` (default 21, recalibratable per user, §9) → check-in "Still on this,
  or shall we let it rest?".
- **Load accumulation**: open `mental_load` units ≥ `mental_load_threshold` (default 7) →
  writes a tentative hypothesis into `current_state` ("feeling loaded lately?"), the user
  confirms/denies in the next conversation; a cooldown of days after a resolved check-in. The
  nudge offers to **close one loop**: it lists the open ones, the user picks, it gets archived
  (soft, recoverable).

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
`active`. The load watcher opens it as a tentative hypothesis; the user confirms or corrects.
Consumers: digest cadence and tone. **Product rule**: LOAD is cared for (observable), emotions
are not interpreted. If forced to choose, keep `energy` (capacity) and drop the mood labels.

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
| `weight_threshold` (archiving) | 0.5 |
| `hysteresis_margin` (focus) | 0.05 |
| λ per type (`weight_decay_rate`) | prior per type, base 0.01/day |
| `min_confidence_to_persist` ⚙ | 0.30 |
| `min_confidence_to_surface` | 0.50 |
| `goal_stagnation_days` ⚙ | 21 |
| `mental_load_threshold` | 7 |
| Push threshold (`interrupt_level`) | 0.70 |
| Quiet hours | [00:00, 07:00) local |
| Event lead time | 7 days |
| Semantic belief merge | cosine ≥ 0.85 |
| Perception confidence gate | 0.40 |
| Consolidation / proactive check | 03:00 daily / every 5 min |
| `boot_consolidation_delay` | 120 s |
| `trigger_staleness_hours` | 6 |
| `timer_staleness_hours` | 3 |
| RRF `k` | 60 |
| `recall_top_k` | 20 |
| RRF vector-leg weight (`weight_vector`) | 1.0 |
| RRF lexical-leg weight (`weight_lexical`) | 1.0 |

Exact values get calibrated with real usage; the mechanisms in this document do not.
