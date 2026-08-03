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
| `normalized_content` | no content | The unit has nothing to store or embed. Recall cannot reach a unit with no content, so this is the second field whose loss is not survivable downstream |
| `structured_data` | absent | The one field that cannot fail on shape: it is free-form by definition (§5 step 1), so any value the stream completed is valid. It is opaque to the brain and stays opaque |
| `weight`, λ | no value | Both are required on a stored unit, so the base priors below fill them. A degraded weight is not a zero weight — that distinction is why the fields are optional at the type level rather than defaulting to `0` |
| dated fields | no date | The unit is stored undated. Nothing is armed for it (§7), because arming a trigger on a guessed date is worse than not arming one |
| the six orthogonal fields | absent | The resolution that field carried is ignored — the pending check-in, relation or state question stays open. The capture still becomes a unit |

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
| Base weight when classify does not supply one | 1.0 |
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
| `dedup_candidate_k` | 5 |
| `correction_referent_margin` (ratio of the top two fused scores) | 1.5 |

Exact values get calibrated with real usage; the mechanisms in this document do not.
