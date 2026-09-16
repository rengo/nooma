# Proposal — m3e: the pending question (the digest asks, and the answer lands)

Close `m3d` finding **J24**, and the two gaps underneath it that J24's own wording does not reach.

M3 gave the brain a mouth. It pushes, it digests, it fires timers, and it resolves three of the four
check-ins the build plan names. The fourth — `relation_outcome` — is classified, routed, and then
dropped: `resolveRelationCheckIn` ([`internal/brain/checkin.go:108-122`](../../../internal/brain/checkin.go))
writes one audit row saying *"a relation answer of %q arrived, and naming which relation it answers
is m3e's"* and returns.

**But J24 describes the last link of a chain whose first two links are also missing.** There is no
relation question in any digest to answer, and no confirm path for an answer to reach. This change
builds all three, because each is inert without the others.

---

## 1. Why now

| Fact (verified 2026-09-05 at `988daae`) | Consequence |
|---|---|
| `assembleDigest` ([`internal/brain/digest.go:53`](../../../internal/brain/digest.go)) sources items **only** from `r.triggers.Undelivered(ctx)` | An Uncertain-band relation is stored by `consolidation.ProposeRelation` → `rels.Upsert` and nothing downstream ever asks about it |
| `ports.RelationRepo` declares `Upsert`, `ByUnit`, `ThresholdsFor`, `Evidence`, `ExistingPairs`, `Delete` — and no read for "relations awaiting a question" ([`internal/ports/relationrepo.go:54-131`](../../../internal/ports/relationrepo.go)) | The digest has nothing to query even if it wanted to |
| `internal/core/consolidation/connect.go:151` still says *"the asking is M3's"* | Written during `m2b`, never discharged. It misdescribes shipped behaviour today |
| **`test/conformance/` has no `i09` file.** The only code claiming I09 is `internal/brain/digest_test.go:135`, a low-energy deferral test over *triggers* | **I09 — "the `[persist, surface)` band is stored AND asked about in the digest" — is half-satisfied and reads as green.** M3's proposal claimed it in scope (line 173) and `m3d` claimed to own it; what `m3d` actually built was the digest's anti-starvation counter |
| `confirmed_floor` appears in **exactly one place in the tree**: `docs/02-cognitive-core.md:352`. No constant, no column, no §13 calibration row, no code | Doc 02 §4's confirm rule — `GREATEST(current, confirmed_floor)` — has no producer. See **Q1** and **R1** |
| `ports.SignalRelationConfirm` ([`internal/ports/signalrepo.go:29`](../../../internal/ports/signalrepo.go)) has **zero call sites** | Confirming a relation is, today, a no-op that also teaches the learning module nothing |
| `RejectRelation` ([`checkin.go:130-148`](../../../internal/brain/checkin.go)) is complete, ordered, and tested (`checkin_test.go:221`) | The reject half is the model to mirror. Only the confirm half is missing |
| `resolveCheckIn` ([`checkin.go:22-54`](../../../internal/brain/checkin.go)) already disambiguates nudge and task check-ins: most recent from `triggers.Delivered`, with `open_check_ins: N` written into `decision_log` | **The disambiguation pattern exists and is accepted.** This change reuses its shape rather than inventing one |
| `internal/store/sqlite/migrations/` holds `0001`–`0003` | The next migration is `0004`, and this is the first change since M2 to need one |

The band has been storable since M1 and unaskable ever since. **The relation judge has been talking
to itself for two milestones.**

---

## 2. Success criteria

- [ ] An Uncertain-band relation reaches the morning digest as a question naming **both** endpoints
      — doc 02 §4's own wording, *"I linked X with Y, are they related?"*
- [ ] **I09 gets its first real conformance test**, over the band rather than over the deferral
      counter, and `test/conformance/i09_*.go` exists.
- [ ] An inbound "yes" names **which** relation it answers, and the choice is auditable: the
      `decision_log` row records how many relation questions were open when the answer arrived,
      exactly as `recordCheckIn`'s `open_check_ins` already does.
- [ ] Confirming raises confidence by `GREATEST(current, confirmed_floor)` — computed in
      `internal/core/relation`, pure — and emits `relation_confirm`, giving that vocabulary member
      its first call site.
- [ ] Rejecting still emits `relation_reject` **before** deleting (**I10**, unchanged), and now
      reaches a real relation rather than nothing.
- [ ] A question is **resolved, never deleted**: `resolved_at` + `resolution` are a state
      transition (non-negotiable #6), and a question that was never answered expires as a
      transition too (**Q4**).
- [ ] `connect.go:151`'s *"the asking is M3's"* is corrected in the PR that builds the asking.
- [ ] `docs/02-cognitive-core.md` §4 and §5, `docs/03-data-model.md`, and **ADR-0027** land with the
      code, not after it.
- [ ] `make check-all` green; no test touches the network or a real LLM.
- [ ] **Demo**: a vault with an uncertain relation produces a digest that asks about it; a reply of
      "yes, they're related" raises its confidence out of the band; a reply of "no" deletes it,
      signal first.

---

## 3. Scope

### 3.1 The boundary rule

> **m3e builds the loop from an uncertain relation to a resolved one. It builds no second question
> kind, no new inbound ambiguity, and consumes no signal it emits.**

### 3.2 In scope — the three gaps, as one unit

1. **Surfacing.** A pending-question store (§4), written when `ProposeRelation` returns an
   `Uncertain` verdict ([`internal/brain/consolidate.go`](../../../internal/brain/consolidate.go)),
   and read by `assembleDigest` as a second item source merged with the trigger-sourced items.
   `renderDigest` gains the relation question's own line shape.
2. **Disambiguation.** `resolveRelationCheckIn` implemented for real, mirroring `resolveCheckIn`:
   most recent open question, ambiguity recorded rather than guessed at, and a recorded
   no-match row when an answer arrives with nothing open.
3. **The confirm path.** A pure `internal/core/relation` function for the confidence raise, its
   persistence through the existing `Upsert`, and `SignalRelationConfirm`'s first emission —
   mirroring `RejectRelation`'s shape, including its ordering discipline.
4. **The governing documents.** Doc 02 §4 (the mechanism by which the band is asked about) and §5
   (`relation_outcome`'s resolution semantics), doc 03 (the new table), `docs/06-harness.md`'s I09
   row if its wording needs to name the mechanism, and **ADR-0027** (§5).
5. **Schema.** Migration `0004`, the `store_api.golden` widening, and `schema_doc_test.go`'s anchor
   list — the cost M2 spent a whole open question avoiding, and M3 avoided entirely.

### 3.3 Capabilities (the contract with `sdd-spec`)

This repository keeps **no `openspec/specs/` tree** — requirements live per change (`openspec/README.md`).
So: **New capabilities: none.** **Modified: none.** `sdd-spec` writes
`openspec/changes/m3e-pending-question/spec.md` covering the four in-scope areas above, with I09,
I10, I12 and I03 traced.

### 3.4 Explicit non-goals, each with its reason

- **No change to nudge or task check-in disambiguation.** Their shared "most recent open, ambiguity
  recorded" pool is shipped, accepted behaviour — a nudge and a task check-in already disambiguate
  against one merged pool. m3e mirrors that shape; it does not revisit it.
- **No state check-in change.** `current_state` is append-only and a state answer is about the
  single most recent hypothesis by convention; `StateRepo` has no per-instance question identity to
  match against and needs none.
- **No "open check-ins" injection into the classify prompt.** Doc 02 §5 step 1 lists it and
  `classify.BuildPrompt` does not do it. Real, and **not this change** — see **Q3**.
- **No ambiguous-person-reference producer, and I06 stays out.** M3's Q3 ruled *no*, and its stated
  cost was *"a capture-path change plus a pending-question store"*. m3e builds that store. It still
  does not turn the producer on: the second half of that cost is a capture-path change that reopens
  M1's Q3a, and inheriting it silently because a prerequisite now exists is how scope creeps.
- **No generalisation of the store to other question kinds.** The table is designed so it *could*
  grow one (§4), and grows none here. A method with no caller is what this repository refuses to ship.
- **No learning consumption.** `relation_confirm` is emitted because doc 02 §4 pairs it with the
  raise. Tuning `relation_thresholds` from it is M5.
- **No UI.** Browsing pending questions is M4's surface.
- **No timer list/cancel from chat.** The other deliberately-open M3 item; unrelated to this one.

### 3.5 Invariants in scope, traced

| # | Invariant | Doc 02 | m3e status |
|---|---|---|---|
| I09 | The `[persist, surface)` band is stored **and** asked about in the digest | §4 | **In scope, and this is the change that makes it true.** No conformance file exists; the asking half has never been built |
| I10 | Rejecting emits `relation_reject` before deleting | §4, §9 | **In scope, unchanged and newly reachable.** `checkin_test.go:221` stays; the path it tests gains a real caller |
| I12 | Every effectful automatic decision logs | §11 | Load-bearing: asking, resolving, confirming, expiring are all effects |
| I03 | Nothing deleted; archiving is a transition | §1 | In scope. The new table gets **no** `Delete`-prefixed method, keeping `i03_units_never_deleted_test.go`'s port sweep satisfied — the one carve-out remains `RelationRepo.Delete`, by the 2026-08-24 owner ruling |
| I07 | A relation is unique per `(from, to, type)` | §4 | Untouched — the confirm raise goes through the existing `Upsert`, which revises confidence in place |
| I13 | — | §9 | Emission only. Consumption is M5 |

---

## 4. Approach — a dedicated store, and why not the other two

**Chosen: a new `pending_questions` table** — `(id, kind, relation_id, asked_at, resolved_at,
resolution, created_at)` — with its own port, a migration `0004`, and no `Delete`.

`kind` is present from the first row and holds one member. It costs nothing now and it is the
difference between a table that can hold a second question kind later and a table named
`relation_questions` that would have to be migrated to.

| Rejected | Why |
|---|---|
| **Extend `triggers`** | `TriggerResolution` is `engaged \| declined \| self_healed` — recording a relation confirmation as `engaged` produces an audit row that misdescribes what happened, which doc 02 §11 forbids by name. And `triggers.unit_id` is one FK while a relation has **two** endpoints: a relation question would arrive at `LiveFocusCandidates` with no honest unit to rank |
| **Derive from `decision_log`** | Precedented here (`heldCounts`, `lastDigestAt`) and cheapest — no migration. But `decision_log` is the glass box, not a state store, and this would be its second load-bearing use as one. It also gives M4 no queryable worklist to render |

The dedicated table is the only one of the three where "which relation is this question about" and
"has it been answered" are both a column rather than an inference.

**Where the boundary falls**, per non-negotiable #3:

| Decision | Package |
|---|---|
| Given `(current confidence, confirmed_floor)`, what is the confirmed confidence? | `core/relation` — pure, one comparison, one **Q1** |
| Given open questions and an answer's outcome, which one does it resolve? | `brain` — it is a read of persisted state, exactly as `resolveCheckIn` is |
| Given uncertain relations and pending triggers, what does this digest contain? | `brain` assembles; `core/prospection` keeps its existing gates |
| Rendering the question's text | `brain` — `renderDigest`'s existing home |

---

## 5. ADR-0027 — yes, and what it decides

The ADR index already reserves this lineage: **0020–0026 all carry `Blocks: M3e`**. This change adds
`0027`, and it decides one thing:

> **Where a question the brain asked lives while it waits for an answer** — a dedicated store, not
> `triggers` and not `decision_log`.

That is an architectural fork with two real alternatives, a migration behind it, and a reader six
months out who will otherwise ask why relations did not simply become another trigger kind. It is
exactly the bar 0020–0026 were held to. No `Accepted` ADR is edited (non-negotiable #2).

---

## 6. Rough shape (not the design — that is `sdd-design`'s)

| Area | Impact | What changes |
|---|---|---|
| `internal/store/sqlite/migrations/0004_*.sql` | New | The table and its indexes. A published migration is never modified; this is the next one |
| `internal/ports/` | New + modified | The pending-question port (open questions, ask, resolve). `RelationRepo` gains the uncertain-band read **or** the question carries what the digest needs — a design call |
| `internal/core/relation/` | New | The confirmed-confidence function, pure, plus its constant if **Q1** lands that way |
| `internal/brain/consolidate.go` | Modified | Ask a question when `ProposeRelation` returns `Uncertain` |
| `internal/brain/digest.go` | Modified | Second item source, merged; `renderDigest` gains the question line |
| `internal/brain/checkin.go` | Modified | `resolveRelationCheckIn` implemented; a `ConfirmRelation` beside `RejectRelation` |
| `internal/core/consolidation/connect.go:151` | Modified | The stale *"the asking is M3's"* comment |
| `docs/02-cognitive-core.md` §4, §5, §13 | Modified | The mechanism, the resolution semantics, and `confirmed_floor`'s row |
| `docs/03-data-model.md`, `docs/06-harness.md` | Modified | The table; I09's row if it names the mechanism |
| `docs/adr/0027-*.md` + `docs/adr/README.md` | New | §5 |
| `test/conformance/i09_*.go` | New | The invariant's first real test |
| `internal/store/sqlite/testdata/schema/*.golden` | Modified | Regenerated, and the regeneration-diff gate is in `check-all` |

**Strict TDD**: the conformance test is its own commit ahead of the implementation commit inside
each PR, `sdd-tasks` orders it strictly first, and `sdd-verify` reads the PR's `git log`. Order:
I09's asking half → disambiguation → the confirm raise → I10's path with a real relation on the end.

**Size**: this touches core, ports, store, a migration, three `internal/brain` files, four docs and
an ADR. Against the 400-line soft ceiling (implementation + docs, tests excluded) that is a chain,
not one PR — see **R5**. The forecast is `sdd-tasks`'s to make.

---

## 7. Open questions — the proposal question round

Each is a decision the owner makes. The recommendation is this proposal's reasoning, not a settled
answer. **Q1 blocks `sdd-design`.**

### Q1 — Where does `confirmed_floor` come from? *(blocking)*

It exists in one line of prose and nowhere else. Doc 02 §4 keys the entire confirm path off it.

| Option | Pro | Con |
|---|---|---|
| **A. `confirmed_floor := the type's own `min_confidence_to_surface`** | Invents no number. Self-evidently correct: a confirmed relation is by definition one that should be asserted without asking, and the band is half-open, so landing exactly at the threshold leaves it. Inherits M5's per-type tuning for free | No headroom — a later threshold raise puts a confirmed relation back into the band and asks again. "Confirmed" and "barely asserted" become indistinguishable by confidence alone |
| **B. A named constant `internal/core/relation.ConfirmedFloor`, with a §13 row** | One place to calibrate; the `calibration_doc_test.go` gate starts checking the row the day it names the constant | A hand-picked number of the kind doc 02:585-589 refuses for weight and λ, and it is per-user-invariant while the thresholds around it are per-type |
| **C. A third column on `relation_thresholds`** | Per-type and tunable, consistent with its two neighbours | A migration for a knob nothing tunes until M5, and a third number the user never sets |

**Recommendation: A, with the re-ask under Q4's expiry rule as the accepted consequence.** The
alternative philosophies both introduce a number where doc 02 already has one that means the right
thing. If the owner wants headroom, A + a named epsilon is B in disguise and should be called B.

### Q2 — Does a relation question compete with triggers for the digest's care gate?

`prospection.Carry` ranks `DigestItem`s by `focus.Priority` over a **unit** candidate. A relation has
two units and no single candidate. Either relation questions bypass the ranking and append after the
trigger items, or they need a defined candidate.

**Recommendation: append after the ranked items, capped at one question per digest.** A digest that
asks three graph questions on a low-energy morning is the exact interruption ADR-0009 was written
about, and a cap is a rule rather than a ranking that has to invent a candidate.

### Q3 — Is the classify "open check-ins" injection (doc 02 §5 step 1) in scope?

`BuildPrompt` injects the local date and beliefs, not open check-ins. Real gap, named in exploration.

**Recommendation: no.** With a pending-question store the *store* resolves which relation, not the
model — that is the whole point of the store. Widening the prompt costs the golden corpus and buys
an improvement to a decision the store now makes deterministically. Record it as its own change so
it stays a named gap rather than an implied one.

### Q4 — When does an unanswered question stop being open?

If questions never expire, a "yes" typed three weeks later resolves a question the user has
forgotten, and the disambiguation pool grows without bound.

**Recommendation: ask once, expire after `MaxDigestDeferrals` digests as `unanswered`** — a state
transition (non-negotiable #6), reusing a bound the digest already has rather than inventing a
second one. An expired question is not re-asked and not deleted.

### Q5 — Does an answer with no open question do anything?

`resolveCheckIn` records an unmatched row and continues. **Recommendation: identical treatment**,
including when the outcome is a rejection — refusing to guess which relation to delete is the safe
direction, and deletion is the one irreversible act in the vault.

---

## 8. Risks

| # | Risk | Rank | Mitigation |
|---|---|---|---|
| R1 | **`confirmed_floor` has no producer** — one line of prose, no constant, no column, no §13 row. This is `interrupt_level`'s R1 again, one milestone later | **1** | Q1, answered before design |
| R2 | **I09 is claimed green and is not.** M3's proposal and `m3d` both claim it; the only test asserting it is a trigger-deferral test | **2** | A real `test/conformance/i09_*.go` over the band, written before the surfacing code, is a success criterion |
| R3 | **The first migration since M2**, and `0001`–`0003` are published and immutable | 3 | `0004` only adds a table. `make check-all` runs the schema-golden regeneration diff and `schema_doc_test.go`'s anchors |
| R4 | **Deleting on a guess.** Reject is irreversible and disambiguation is heuristic — "most recent open" | 4 | Q5's rule: an unmatched rejection deletes nothing. `open_relation_questions: N` in `decision_log` makes every resolved choice auditable, as `open_check_ins` already does |
| R5 | **Cached delivery strategy is `single-pr`, and the forecast is not a single PR.** Core + ports + store + migration + three brain files + four docs + an ADR | 5 | `sdd-tasks` runs the 400-line guard and reports `Chained PRs recommended`. The strategy is the owner's to re-rule once the forecast exists |
| R6 | **The `internal/core` coverage floor** bites the new `core/relation` function, and `make check` never runs `scripts/core-coverage.sh` | 6 | `make check-all` before every PR, structurally |
| R7 | **`docs-sync` only fires once a PR is open**, and the `core/relation` PR touches `internal/core/**` | 7 | Its doc 02 §4 delta is genuine; no `no-spec-change` label should be needed |
| R8 | **A stale question outliving its relation.** A relation deleted by a rejection, or by a future path, leaves a question pointing at nothing | 8 | An FK with the resolution rule stated in the design, and the digest read skipping questions whose relation is gone rather than failing |

---

## 9. Rollback

Per PR, and cleanly:

- The `core/relation` function is pure and imported by one caller — deleting both reverts it.
- The digest's second source is an additive branch in `assembleDigest`; removing it restores the
  trigger-only digest exactly.
- `resolveRelationCheckIn` reverts to today's recorded-and-ignored row.
- **The migration does not roll back.** `0004` is forward-only, like every migration here. A revert
  of the code leaves an unused table — inert, empty on a vault that never asked a question, and
  read by nothing. That asymmetry is the reason the migration lands in its own PR, first.

---

## 10. Next step

`sdd-spec` and `sdd-design` run in parallel over this proposal. **Q1 blocks a clean design** — the
confirm path's arithmetic is the one thing neither doc 02 nor the tree answers today. Q2 and Q4
shape the digest and the store's lifecycle and should be answered in the same round.
