# Spec — M3e: the pending question

Specification for `m3e-pending-question`, which has its own `proposal.md` (not a slice of the
`m3-mouth-telegram` umbrella). States what MUST be true of the repository after this change is
applied, in testable form. It does not prescribe how (`sdd-design`'s job).

Sources: `proposal.md` §1–§9; `docs/02-cognitive-core.md` §4, §5, §11; `docs/06-harness.md` I09,
I10, I12, I03; `internal/brain/checkin.go` (`resolveCheckIn`'s disambiguation pattern, lines
22–54; `RejectRelation`, lines 130–148); `internal/core/relation/thresholds.go` (`Thresholds`,
`Resolve`); `internal/core/relation/verdict.go` (`Decide`, `Uncertain`); `internal/core/consolidation/connect.go:150-151`.

## Annotation — four MUSTs below were superseded by `design.md`, and are left standing

Added after implementation, per this repository's **annotate, don't edit** convention for
OpenSpec artifacts: a spec records what was required *when it was written*, and rewriting it
after the fact destroys the evidence that a decision was revisited at all.

Four of the MUSTs below do **not** describe the shipped code. Each was overruled by `design.md`
during implementation, each disagreement is argued in full in `tasks.md`'s own **Findings**
section (F1–F4), and each resolution was independently re-verified against running code by
`sdd-verify`. **Read `tasks.md` F1–F4 before treating any of these four as binding:**

| Requirement | What this spec says | What ships, and why |
|---|---|---|
| **R1** | the Uncertain verdict is computed by the caller, *"never inside `core/consolidation` or `core/relation`"* | `ProposedRelation.Band`, computed inside `core/consolidation` — `ProposeRelation` already calls `relation.Decide` and discarded the result; two computations of one rule is the drift design refused (**F2**) |
| **R3** | the digest surfaces *"the oldest by `asked_at`"* | oldest by `created_at`, then `id`. This spec's wording is **unreachable**, not merely different: the digest reads queued rows, whose `asked_at` is NULL by construction (**F4**) |
| **R5** | `ConfirmedConfidence(current, floor float64) float64`, signal emitted **before** the `Upsert` | `ConfirmedConfidence(current float64, t Thresholds) float64`, signal emitted **after** the raise — the raise is idempotent, so the recoverable error points the other way than I10's (**F1**, **F3**) |

Everything else below describes the shipped code as written.

## Scope boundary (binding)

> `m3e` builds the loop from an uncertain relation to a resolved one: surfacing a pending
> question in the digest, disambiguating an inbound answer against it, and the confirm path's
> confidence raise. It builds no second question kind, no new inbound ambiguity mechanism, no
> classify-prompt injection, and consumes no signal it emits.

**Not this change**: nudge/task check-in disambiguation (shipped, unchanged); `current_state`
check-ins; classify's "open check-ins" prompt injection (Q3 — out of scope, a named future gap);
the ambiguous-person-reference producer (I06); a second question kind in the store; learning
consumption of `relation_confirm`; any UI; timer list/cancel from chat.

## Owner rulings in force (binding, not re-litigated)

| # | Ruling |
|---|---|
| Q1 | `confirmed_floor` **is** the relation type's own resolved `min_confidence_to_surface` (`relation.Thresholds.Surface`, from `relation.Resolve`). No new constant, no new column, no new §13 row. |
| Q2 | At most **one** relation question is appended per digest, **after** `prospection.Carry`'s ranked items — it does not compete with unit ranking. |
| Q3 | The classify "open check-ins" prompt injection is out of scope. Disambiguation is resolved deterministically by the store, mirroring `resolveCheckIn`, never by widening the model's context. |
| Q4 | An unanswered question expires after `MaxDigestDeferrals` digests, as a state transition (`resolved_at` set, resolution `expired`) — never a delete. |
| Q5 | An inbound answer with no matching open question resolves nothing and deletes nothing. |

---

## R1 — An Uncertain-band relation is recorded as a pending question when it is proposed

**MUST**: when `consolidation.ProposeRelation` returns `(_, true)` and `relation.Decide(proposed.Confidence, resolvedThresholds) == relation.Uncertain` — computed by the caller from the same `resolvedThresholds` it already reads via `RelationRepo.ThresholdsFor` + `relation.Resolve` (`internal/brain/consolidate.go:493-498`), never inside `core/consolidation` or `core/relation` — the persisting caller writes one row to a `pending_questions` store, `kind = relation`, carrying the persisted relation's id, in the same transaction/step as `rels.Upsert`. An `Asserted`-band relation writes no question.

**MUST**: `pending_questions` gains one FK-referenced column pointing at `relations.id` (or equivalent read), so a question is always resolvable back to the specific `(from, to, type)` pair it asks about.

**Scenario: a relation judged into the Uncertain band gets a question, an Asserted one does not**
- GIVEN two judged pairs, one with confidence inside `[Persist, Surface)` and one at or above `Surface`
- WHEN each is persisted through `ProposeRelation` + `Upsert`
- THEN exactly the Uncertain-band relation gains a `pending_questions` row; the Asserted one does not

**Verified by**: L1/L2 — a fixture sweeping confidence across `Discard`/`Uncertain`/`Asserted`, asserting a question exists iff the verdict is `Uncertain`; `test/conformance/i09_*.go`, the invariant's first real test, over the band itself rather than over the digest's deferral counter (`digest_test.go:135` stays as-is and proves a different thing).

---

## R2 — `pending_questions` is a state machine: created once, resolved or expired, never deleted

**MUST**: a row's lifecycle is `asked_at` set at creation; `resolved_at` and `resolution` are both `NULL` while open; both are set together, exactly once, on confirm, reject, or expiry. No method on the store's port is prefixed `Delete`/`Remove`/`Purge`/`Drop`/`Destroy` — the port joins `RelationRepo`, `UnitRepo` and the rest of `test/conformance/i03_units_never_deleted_test.go`'s reflection sweep (I03), with no carve-out.

**MUST**: `resolution` is a closed vocabulary of exactly `{confirmed, rejected, expired}`, distinct from `ports.TriggerResolution` (`engaged|declined|self_healed`) — recording a relation confirmation as `engaged` would misdescribe what happened, the same reasoning the proposal's Approach section (§4) gives for rejecting `triggers` as the store.

**Scenario: an open question is never removed by any code path**
- GIVEN a `pending_questions` row with `resolved_at IS NULL`
- WHEN the store's port surface is reflected over (mirroring the I03 sweep's method)
- THEN no exported method can delete it — only a transition to a resolved state exists

**Verified by**: L1/L2, extending `i03_units_never_deleted_test.go`'s prefix sweep to the new port; L3 against a real migrated vault (migration `0004`).

---

## R3 — the digest surfaces at most one open relation question, after the ranked items (Q2)

**MUST**: `assembleDigest` (`internal/brain/digest.go:32`) reads open `pending_questions` rows as a second item source. When at least one is open, **exactly one** — the oldest by `asked_at` — is appended to the digest's rendered output after `prospection.Carry`'s own ranked/held split; it does not enter `Carry`'s candidate list and is never counted toward, or against, the low-energy or anti-starvation gates.

**MUST**: `renderDigest` gains a distinct line shape for the relation question, naming **both** endpoints, matching doc 02 §4's own wording: *"I linked X with Y, are they related?"*.

**MUST**: an empty-otherwise digest (`carry` from `Carry` is empty) that has an open relation question is still sent — the "an empty digest is not sent" rule (`digest.go:57-64`) is about items `Carry` produces having nothing to say; a pending question is not nothing to say.

**Scenario: two open relation questions produce one digest line, not two**
- GIVEN two `pending_questions` rows open at digest time, asked at different instants
- WHEN the digest assembles
- THEN it names the older one; the newer stays open, unmentioned, for a later digest

**Verified by**: L2 — a digest with N ranked trigger items and M open relation questions renders exactly `min(1, M)` question lines, always last; a digest with zero trigger items and one open question is still sent.

---

## R4 — disambiguating an inbound answer mirrors `resolveCheckIn`'s pattern

**MUST**: `resolveRelationCheckIn` (`internal/brain/checkin.go:108-122`), given `c.RelationOutcome != nil`, reads every currently-open `pending_questions` row (mirroring `r.triggers.Delivered(ctx)`'s read), and — when at least one is open — resolves the **most recently asked** one, recording how many were open in the `decision_log` row's context as `open_relation_questions: N`, the same shape `open_check_ins` already takes (`checkin.go:159`).

**MUST**: when more than one question was open, the choice is a recorded fact, not a silent pick — the ambiguity itself is auditable from the `decision_log` row alone, exactly as `resolveCheckIn`'s own comment states ("ambiguity is not resolved by guessing at meaning").

**Scenario: an inbound "yes" resolves the most recently asked open question**
- GIVEN two open relation questions, asked five minutes apart
- WHEN a `relation_outcome: confirmed` classification arrives
- THEN the more recently asked question is resolved, and its `decision_log` row records `open_relation_questions: 2`

**Verified by**: L2 — one open question resolved trivially (`open_relation_questions: 1`); two or more resolved to the most recent, with the count recorded; zero open questions taking R7's path instead.

---

## R5 — confirming raises confidence to `GREATEST(current, min_confidence_to_surface)` and emits `relation_confirm`

**MUST**: `internal/core/relation` exposes a pure function computing the confirmed confidence from `(current, floor float64) float64` as `max(current, floor)` — no I/O, no port read, per non-negotiable #3. The caller supplies `floor` as the relation's own type's resolved `Thresholds.Surface` (Q1) — never a new constant.

**MUST**: a confirmed resolution (a) emits one `SignalRelationConfirm` signal, `ports.SignalRelationConfirm`'s first call site, mirroring `RejectRelation`'s shape — `TargetKind = relation`, `TargetID` the relation's id, `Valence = positive`; (b) persists the raised confidence through the existing `RelationRepo.Upsert` (which revises `Strength`/`Confidence` in place, per its own doc comment, `relationrepo.go:61-64`) — no new persistence method; (c) resolves the `pending_questions` row, `resolution = confirmed`.

**MUST**: the signal is emitted before the `Upsert`, mirroring `RejectRelation`'s own ordering discipline (`checkin.go:143`, "only now") — the worst case of a signal for a raise that then failed is a learning signal ahead of a retryable write, never evidence for something that reverted.

**Scenario: confirming a relation already above the floor is a no-op raise**
- GIVEN a relation at confidence 0.55 and its type's `min_confidence_to_surface = 0.50`
- WHEN its pending question is confirmed
- THEN its stored confidence is unchanged at 0.55 (`GREATEST(0.55, 0.50) = 0.55`), and `SignalRelationConfirm` is still emitted — the signal records the confirmation, not the delta

**Verified by**: L1 — the pure function over below/at/above-floor inputs; L2 — the emit-then-persist ordering, the resolved `pending_questions` row, and the `Upsert` call carrying the raised (never lowered) confidence.

---

## R6 — rejecting reaches a real relation through the same disambiguation, `RejectRelation` unchanged

**MUST**: `resolveRelationCheckIn` wires a `rejected` resolution to the existing `RejectRelation` (`checkin.go:130-148`, unchanged: signal emitted before delete — I10), passing it the relation R4 disambiguated, and then resolves the `pending_questions` row, `resolution = rejected`. `RejectRelation`'s own body, ordering, and test (`checkin_test.go:221`) are untouched by this change; only its caller changes, from nothing to R4's disambiguation.

**Scenario: rejecting deletes the relation and its question is resolved, not deleted**
- GIVEN one open pending question over relation `rel-1`
- WHEN a `relation_outcome: rejected` classification arrives and disambiguates to it
- THEN `SignalRelationReject` is emitted, `rel-1` is deleted (I10, unchanged), and the `pending_questions` row itself survives with `resolution = rejected`, `resolved_at` set

**Verified by**: L2 — extends `checkin_test.go:221`'s existing assertions with the `pending_questions` row's post-state.

---

## R7 — an unanswered question expires after `MaxDigestDeferrals` digests (Q4)

**MUST**: a question surfaced in `MaxDigestDeferrals` (`internal/core/prospection.MaxDigestDeferrals`, currently 3) distinct digests without being resolved transitions to `resolution = expired`, `resolved_at` set to that digest pass's instant — a state transition, never a delete (non-negotiable #6). An expired question is never re-surfaced and never counted in R4's disambiguation pool.

**MUST**: the surfaced-count is derived the same way digest deferrals already are (`heldCounts`, `digest.go:186-211`) — counted from `decision_log` rows rather than a new column — or, if design chooses a column, the choice is stated in `design.md` with its migration cost; this spec states only the observable property.

**Scenario: a question surfaced on three consecutive mornings with no answer expires**
- GIVEN a pending question surfaced in `MaxDigestDeferrals` digests, none of them answered
- WHEN the next digest pass runs
- THEN the question transitions to `expired`, is not rendered again, and does not appear in R4's open-question read

**Verified by**: L2 — a fixture surfacing a question `MaxDigestDeferrals - 1` times (still open, still asked) and `MaxDigestDeferrals` times (expires); an expired question excluded from both R3's render and R4's disambiguation pool.

---

## R8 — an answer with no open question resolves nothing (Q5)

**MUST**: when `resolveRelationCheckIn` finds zero open `pending_questions` rows, it writes one `decision_log` row via the existing `recordCheckIn` (`checkin.go:151`) with `open_relation_questions: 0` and takes no further action — no relation is deleted, no confidence is changed, no signal is emitted. This holds identically for both `RelationOutcomeConfirmed` and `RelationOutcomeRejected` — refusing to guess which relation to delete is the safe direction (proposal §7, Q5).

**Verified by**: L2 — both outcome values, zero open questions, asserting no `RejectRelation`/confirm-path call and exactly one recorded, unmatched `decision_log` row.

---

## R9 — the stale comment is corrected, in this PR

**MUST**: `internal/core/consolidation/connect.go:151`'s doc comment (*"the asking is M3's"*) is corrected to name this change, in the same PR that ships R1 — `CLAUDE.md` non-negotiable #1's "same PR" rule, applied to a comment rather than doc 02 itself.

---

## R10 — doc sync (non-negotiable #1)

**MUST**: `docs/02-cognitive-core.md` §4 is corrected so `confirmed_floor` reads as an alias for the relation type's own `min_confidence_to_surface` (Q1), not an undefined term; §5 states the store-based (not model-based) disambiguation rule for `relation_outcome`, matching Q3. `docs/03-data-model.md` gains the `pending_questions` table. `docs/06-harness.md`'s I09 row is corrected if its wording needs to name the surfacing mechanism. **ADR-0027** records the store-vs-`triggers`-vs-`decision_log` fork (proposal §5). All land in the PR(s) that implement the behaviour they describe, never after.

**Verified by**: `scripts/docs-sync.sh` (on a PR touching `internal/core/**`); manual review that §4/§5's prose matches R1–R8 above.

---

## What this spec does not require

Matching proposal §3.4: no second `pending_questions` kind; no classify-prompt "open check-ins" injection; no ambiguous-person-reference producer (I06); no learning consumption of `relation_confirm`; no UI for browsing pending questions; no state check-in change; no nudge/task check-in disambiguation change. No requirement above depends on any of them existing.

## Exit criterion (this change's own success condition)

A vault with one Uncertain-band relation produces a digest naming both endpoints (R1, R3); a reply
of "yes, they're related" raises its confidence via `GREATEST(current, min_confidence_to_surface)`
and emits `relation_confirm` (R5); a reply of "no" emits `relation_reject` and deletes it (R6, I10
unchanged); an unanswered question expires after `MaxDigestDeferrals` digests as a transition, never
a delete (R7); every one of these effects has exactly one `decision_log` row (I12); `test/conformance/i09_*.go`
exists and is green; `make check-all` is green; no test touches the network or a real LLM.
