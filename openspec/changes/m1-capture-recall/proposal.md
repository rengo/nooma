# Proposal — M1: the brain gets written

Deliver M1 as laid out in [`docs/05-build-plan.md`](../../../docs/05-build-plan.md): the
`LLMProvider` / `EmbeddingProvider` interfaces and their implementations, `tasks:` routing, the
synchronous capture pipeline of [`docs/02-cognitive-core.md`](../../../docs/02-cognitive-core.md)
§5, hybrid recall with RRF fusion, the dedup/relation judge with its thresholds, in-place
corrections, and the HTTP surface for capture, recall and read-only units.

M0 made the binary runnable. M1 makes it a brain. It is the change where `internal/core/`
stops being empty — and therefore the change where three gates that have been armed and
vacuous since the harness landed start firing for real.

---

## 1. Why now

M0 closed with twenty merged PRs. The binary initializes a vault, holds a write lock, serves
HTTP, and reports its own health. What it cannot do is remember anything.

| Fact (verified) | Consequence |
|---|---|
| Every package under `internal/core/` contains only a `doc.go` with a one-line package comment. Zero statements, tree-wide | The dependency rule, the injected clock and the ≥90 % coverage floor have never been tested against real code |
| `internal/ports/` declares exactly two interfaces, `Clock` and `IDGen` (`internal/ports/clock.go`) | There is no `UnitRepo`, no `LLMProvider`, no `EmbeddingProvider`. Nothing can read or write a unit outside a migration test |
| `testdata/{classify,recall,llm}/cases/` each contain only `.gitkeep` | The three golden corpora that `docs/06-harness.md` §5 calls the regression detectors for classify and recall quality do not exist |
| `test/conformance/pending_symbols.txt` lists five symbols, all still undefined | Three conformance tests — I01, I03, I21 — are red on purpose, waiting for M1 |
| `internal/config/validate.go` decodes `providers:` and `tasks:` but interprets neither; its own header says so | The config schema has been a shape with no semantics for a milestone |

The schema has held all fourteen tables since the harness landed. `units`, `relations`,
`unit_embeddings`, `units_fts` and its three sync triggers, `decision_log`, `learning_signals`,
`relation_thresholds` — all of it applies from scratch and is pinned by a golden. M1 writes no
new table into any of them except where §8's open questions force one. **M1 is almost entirely
a code change over a schema that already exists**, which is the single most important scoping
fact in this document.

---

## 2. Success criteria

The change is done when:

- [ ] A message posted to the capture endpoint is classified by a configured provider, persisted
      as a unit with its initial `weight` and `weight_decay_rate`, embedded, and searchable —
      lexically through `units_fts` and semantically through the in-memory vector index.
- [ ] A malformed field in the classifier's response degrades to null and the rest of the
      classification survives, proven by real recorded cases in `testdata/classify/cases/`
      covering all three shapes `format.md` names: truncated JSON, a wrong-typed field, an
      unknown enum value (**I14**).
- [ ] Hybrid recall returns a fused ranking (RRF, `k = 60`, [ADR-0010](../../../docs/adr/0010-hybrid-recall-fusion.md))
      whose order is pinned by `testdata/recall/cases/`, with at least one distractor, one
      near-duplicate pair and one lexical/vector disagreement across the corpus.
- [ ] Every vector search filters on `model`; two models' embeddings never enter the same
      ranking (**I21**), proven by the promoted conformance test and by an L3 case over a vault
      holding rows from two models.
- [ ] The relation judge persists, holds as uncertain, or discards a candidate relation strictly
      by the §4 thresholds (**I08**), and never violates `(from, to, type)` uniqueness (**I07**).
- [ ] A correction edits its referenced unit in place — an `UPDATE`, never a `DELETE` (**I03**)
      — and emits a `correction` row in `learning_signals` with no FK to its target (**I13**).
- [ ] Every automatic decision with an effect writes a `decision_log` row from `internal/brain`,
      never from `internal/core` (**I12**).
- [ ] Every live read surface filters positively on `status = 'pool'` (**I02**).
- [ ] All five lines of `test/conformance/pending_symbols.txt` are gone, their three tests run
      untagged in the L2 suite, and the `pending-red` gate is retired rather than left failing
      (§4.7 — this is not optional; see the trap there).
- [ ] `make check-all` green, and CI green, on every PR in the chain — including the
      `internal/core` coverage floor, which `make check` does not run.
- [ ] No test touches the network or a real LLM. Every provider response comes from
      `testdata/llm/cases/`.
- [ ] **Demo**: capture through the API and through the CLI, then ask "what do you know about
      X?" and get a real recall over what was captured.

---

## 3. Scope

### 3.1 The boundary rule

> **M1 implements the capture pipeline's decisions and the recall mechanism. It implements no
> pass that runs at night and no surface that speaks first.**

Concretely: M1 writes units, embeddings, relations, signals and decision-log rows. It does not
compute `effective_weight` on read, does not archive, does not connect during a nightly pass,
does not derive beliefs, does not arm a trigger, does not fire a timer, and does not push
anything to anybody. Those are M2 and M3, and the build plan says so.

One consequence to state before it is discovered during implementation: classify's taxonomy
includes `timer` and `recurring_reminder`, whose hooks (`docs/02-cognitive-core.md` §5.5) belong
to M3. M1 still classifies them — the taxonomy is classify's contract and the golden corpus
covers it — but §8's Q3a decided what the system *does* with one, and the answer is nothing:
M1 arms no hook and refuses the capture outright.

### 3.2 In scope

1. **Doc corrections that block the work** — `openai` is absent from doc 01's provider types
   while the build plan requires an openai implementation (§4.2), and `tasks.embedding` in doc 01
   points at a literal `...`.

   This item was written listing two more corrections — `docs/README.md` and `CLAUDE.md` saying
   "no code yet" — that were **already fixed** by PRs #41 and #42 before this proposal was
   written. The exploration that surfaced them ran before those merges, and the finding was
   carried forward without being re-checked against the tree. Recorded rather than silently
   edited out, because it is the same defect family this project keeps meeting: **a fact
   inherited from an earlier stage and never re-verified at the stage that uses it.** PR 1's
   budget below is reduced accordingly.
2. **`internal/core/unit`** — the `Status` type, `AllStatuses`, the live-status predicate, the
   type taxonomy, and legal status transitions.
3. **`internal/core/classify`** (new package, see §4.1) — the tolerant decoder that turns a raw
   provider response into a classification with per-field degradation.
4. **`internal/core/recall`** — `VectorQuery`, `VectorIndex`, brute-force top-K
   ([ADR-0012](../../../docs/adr/0012-vector-proximity-search.md)), and RRF fusion.
5. **`internal/core/relation`** — the threshold decision: discard, persist silently, persist as
   uncertain, or assert.
6. **`internal/ports`** — `UnitRepo`, `RelationRepo`, `SignalRepo`, `DecisionLog`,
   `LLMProvider`, `EmbeddingProvider`.
7. **`internal/store/sqlite`** — the repository implementations, embedding write and index load,
   the FTS5 query leg. Each widens `testdata/schema/store_api.golden`.
8. **`internal/providers`** — anthropic, openai, ollama, and the fixture-replaying fake.
9. **`internal/brain`** — the capture service and the recall service: read the clock once, call
   core, persist, log.
10. **`internal/httpapi`** — capture, recall, and read-only units routes.
11. **`cmd/nooma`** — a `capture` subcommand, for the demo and for L4.
12. **The three golden corpora** — real cases in `testdata/classify/cases/`,
    `testdata/recall/cases/` and `testdata/llm/cases/`, plus the inversion of the M0-era test
    that currently requires them to be empty (§4.6).
13. **The `pending-red` promotion and the gate's retirement** (§4.7).
14. **`nooma init`'s two first-class paths** — Cloud (recommended) and Ollama — replacing M0's
    commented `providers:` placeholder with a real block, and never writing a secret: the file
    holds `api_key_env`, the *name* of an environment variable, so a `nooma.yml` stays safe to
    commit, share, or paste into an issue.
15. **`nooma doctor`'s structured-JSON quality gate** — a fixed prompt set against the provider
    configured for each task, verifying the returned JSON validates. A failure names the provider
    unsuitable **for that task**, never in general. Its corpus is the same one feeding
    `testdata/llm/cases/`.

**Items 14 and 15 were added on 2026-07-31, after the proposal was already merged**, and how they
were missing is the part worth keeping. Both are [ADR-0002](../../../docs/adr/0002-default-llm-preset.md)'s
own deliverables — an ADR `Accepted` on 2026-07-27 whose Decision section names them in plain
words. Neither appeared in any milestone's bullets, in this proposal's scope, or in any task list.
They surfaced because the owner asked an ordinary product question — *"is using a cloud LLM
contemplated, and would that be part of the init wizard?"* — and answering it honestly meant
reading the ADR and then failing to find where its decision had been scheduled.

**An `Accepted` ADR is not self-executing.** This project already refuses to edit one and requires
a superseding ADR to change a decision — but nothing was checking that an accepted decision had
somewhere to land. Doc 05's M1 section now carries both, and this is the second time an ADR-0002-
adjacent claim has needed dating rather than defending.

They belong to **Phase C** (`m1c-surface`): both are CLI surface, both need providers to exist
first, and a wizard offering to configure a provider before any provider exists would be offering
a promise.

**And for two days that sentence was the whole schedule.** "They belong to Phase C" was written
here on 2026-07-31; Phase C's table in §6 listed three PRs, none of them these. The paragraph above
diagnosed an `Accepted` ADR with nowhere to land, and then this paragraph gave these two items a
phase without giving them a row. They became **PRs 15 and 16 on 2026-08-02**, and the fix for the
general case is the same one this section already argued for: a decision is scheduled when it is a
row in a table something reads, not when a paragraph says where it belongs.

**Cloud is the path that must work.** Owner direction, 2026-07-31: the local-model story has
comments pending and is deliberately not the priority — the ollama client exists and stays
supported, but M1 is judged on the cloud path running end to end.

### 3.3 Explicit non-goals

- **No `effective_weight`, no priority, no focus, no hysteresis.** Classify *assigns* `weight`
  and λ; nothing reads them back through the decay formula until M2. I05 and I19 are M2.
- **No consolidation.** None of the eight phases. `expire_incomplete` in particular is M2, which
  is why §8's Q3a settled that M1 creates no `incomplete` unit at all — one would be
  invisible to every read surface and immortal until M2 shipped.
- **No triggers, no timers, no digest, no push, no quiet hours.** I04, I15, I16, I17 are M3.
- **No self-model derivation.** Beliefs are injected into classify's prompt *if any exist*;
  nothing in M1 creates one. `derive` is M2, seeding is M4's onboarding.
- **No learning pass.** M1 *emits* signals (I13); consuming them is M5. I09's second half —
  "asked about in the digest" — cannot be satisfied before M3, and I10's trigger — a rejection —
  has no M1 surface that produces one.
- **No Telegram.** Capture arrives over HTTP and the CLI only.
- **No `reindex`.** ADR-0003's amendment makes it an ordinary `UPDATE` loop; it is an M6 command.
  M1 must nonetheless behave correctly on a vault holding two models, because that is what I21
  exists for.
- **No perception, no measurements.** v2, per [ADR-0005](../../../docs/adr/0005-v1-scope.md).

### 3.4 Invariants in scope, traced

| # | Invariant | Doc 02 | M1 status |
|---|---|---|---|
| I01 | `status='focus'` does not exist | §3 | **Test promoted, feature not built.** The test is a tree scan for a literal; it anchors `unit.Status`/`unit.AllStatuses`, which M1 creates. Focus itself is M2 |
| I02 | Live reads exclude `superseded` and `incomplete` | §1 | In scope — every repo read filters positively on `status = 'pool'` |
| I03 | Nothing deleted; archiving is a transition | §1 | In scope — the correction path is the first code that could plausibly emit a `DELETE`. Test promoted |
| I07 | A relation is unique per `(from, to, type)` | §4 | In scope — the judge must upsert, not insert blindly |
| I08 | Below `min_confidence_to_persist` → not stored | §4 | In scope — Q1 supplies the threshold when no `relation_thresholds` row exists |
| I12 | Every automatic decision with an effect logs | §11 | In scope — classify, relation persist/discard, correction |
| I13 | A `learning_signal` outlives its target | §9 | In scope — the first *write* into `learning_signals` |
| I14 | A malformed classify field degrades to null | §5 | In scope — the corpus that proves it is M1's to build |
| I18 | `event_at`, `created_at`, `due_at` never interchanged | §1 | In scope — classify assigns two of the three |
| I21 | Every vector search filters on `model` | §5 | In scope — test promoted |
| I05, I06, I09, I10 | — | §1, §2, §4 | **Partial or out.** I05 needs the read side (M2); I06 needs `incomplete` units to exist (§8 q3a); I09's digest half needs M3; I10 needs a rejection surface (M3/M4) |
| I04, I11, I15–I17, I19, I20 | — | — | Out. M2, M3, and v2 |

---

## 4. Approach

### 4.1 Where the boundary falls, decision by decision

`docs/06-harness.md` §1 states the rule; M1 is the first change that has to apply it to
non-trivial logic. The table below is the proposal's commitment, not a suggestion — every row is
placed by the decision gate in the `nooma-core` skill.

| Decision | Package | Why there |
|---|---|---|
| Is this status live? What transitions are legal? | `core/unit` | Data in, data out |
| Given a raw provider response, what classification survives? | `core/classify` | The degradation rule (I14) is a pure function of the bytes |
| Given a query vector and an index, which K units are nearest? | `core/recall` | ADR-0012 states it explicitly: a pure function over `(query, index)` |
| Given two ranked id lists, what is the fused order? | `core/recall` | ADR-0010 states it explicitly: pure over two lists of ids |
| Given a confidence and a threshold pair, is this relation discarded, uncertain, or asserted? | `core/relation` | The whole of I08 and I09's storage half |
| Read the clock, call the provider, write the unit, log the decision | `brain/` | Orchestration. The clock is read **once** per capture and passed down as a `time.Time` |
| Speak HTTP to Anthropic/OpenAI/Ollama | `providers/` | Adapter |
| Speak SQL, maintain the vector index, run `MATCH` | `store/sqlite/` | Adapter |
| Route `tasks.capture_processing` to a provider instance | `cmd/nooma` | Wiring |

Two placements deserve their reasoning stated:

**`core/classify` is a new package**, which means `docs/06-harness.md` §1's tree gains a line in
the PR that creates it. The alternative — putting the tolerant decoder in `core/unit` — conflates
"what a unit is" with "how a provider's answer becomes one", and `core/unit` is the package the
I01 tree scan reads. Keeping the decoder out of it keeps that structural test's subject narrow.

**The vector index lives in `store/sqlite`, its search lives in `core/recall`.** ADR-0012 says
the index is loaded from SQLite when the vault opens and must not be paid per request; that is
adapter work. What the core owns is the scoring and selection over a `VectorIndex` value handed
to it. This is what lets the whole of vector search be tested at L1 with no database — the ADR's
own claimed consequence.

### 4.2 Providers, and the fake that makes every test possible

`CLAUDE.md` non-negotiable #5 and `docs/06-harness.md` §3 both forbid a test touching a real
LLM. That is not a constraint to work around; it is the reason `testdata/llm/` exists. The fake
provider that replays it is therefore **the first provider M1 builds**, before any HTTP client,
because nothing downstream is testable without it.

`testdata/llm/format.md` already flags the trap, and the proposal adopts its warning as a
requirement: **`prompt` cannot be the replay key.** Classify's real prompt is built from active
self-beliefs plus the local date and the user's timezone (`docs/02-cognitive-core.md` §5), so
literal prompt equality breaks on any fixture drift or clock difference. The fake keys on the
case `id`, selected by the test; the recorded `prompt` stays as documentation of what produced
the response. Design settles the mechanism.

Three verified facts about the real providers:

1. **`openai` is not a documented provider type.** `internal/config/validate.go` declares
   `DocumentedProviderTypes = ["anthropic", "ollama", "whisper_cpp"]`, and doc 01's config block
   documents exactly those three. The build plan requires an openai implementation. No gate fires
   on adding `openai` to the Go list — `TestValidate`'s round-trip only checks that every listed
   type validates — but the list's own comment claims it mirrors doc 01, and that claim would
   become false. **Doc 01 gains an `openai` provider entry in the same PR**, per non-negotiable #1.
2. **`tasks.embedding` in doc 01 reads `{ provider: ... }` — a literal ellipsis.** It decodes and
   validates today because `checkTasks` only validates the task *name*. M1 is where that key
   acquires meaning, so doc 01 gains a real provider there.
3. **No validation checks that a task's `provider` names an existing `providers:` entry.**
   Verified by reading `checkTasks`. Today that is harmless because nothing resolves a task. In
   M1 it becomes a nil provider at capture time, so the check lands with the routing.

### 4.3 The capture pipeline

One synchronous path, entered from HTTP or the CLI, orchestrated in `brain/`:

```
message
  → clock read ONCE                                  (brain)
  → classify: provider call, tolerant decode          (providers → core/classify)   I14
  → persist the unit                                  (brain → ports.UnitRepo)      I18
  → embed and index; FTS is trigger-maintained        (brain → providers, store)
  → hybrid recall for candidates                      (brain → core/recall)         I21
  → dedup/relation judge                              (providers → core/relation)   I07, I08
  → correction, when that is what it was              (brain)                       I03, I13
  → decision_log, at every step with an effect        (brain)                       I12
```

`units_fts` needs no application code: migration 0002 already installs `units_fts_ai`,
`units_fts_ad` and `units_fts_au`, and its own comment block explains why an archived unit stays
indexed and why the triggers must not be "optimized" to skip statuses. M1 filters at query time,
positively, exactly as doc 02 §1 requires. This is a genuine saving and it is worth naming: FTS
synchronization is already done.

### 4.4 Hybrid recall, and the two entrances

ADR-0010 and ADR-0012 fix the mechanism completely: two ranked lists, RRF with `k = 60`, a
brute-force dot product over unit-normalized vectors resident in memory. Nothing here is open.

What was open is the *entrance*, and §8's Q3b closed it: standalone, no classify call on the
read path. What the proposal fixed regardless of that answer:

- **One mechanism, three consumers.** Answering a recall, finding dedup candidates during
  capture, and — later — finding pairs during `connect` all call the same fusion. ADR-0010 says a
  bias here propagates into the entire relation graph; two implementations would be two biases.
- **`recall_top_k` is a new calibratable.** Doc 02 §5.2 says "top-K by vector similarity + top-K
  by FTS" and §13's table lists RRF `k = 60` but **no K**. Verified: the row does not exist.
  `docs/06-harness.md` §7 requires every behavioral number to be a named constant in exactly one
  place *and* to appear in §13. So §13 gains a row in the PR that introduces the constant.

### 4.5 The relation judge

The judge's LLM half is a provider call. Its decision half is `core/relation`, and it is small,
pure, and entirely determined by §4's thresholds:

```
confidence <  min_confidence_to_persist                       → discard, log the discard   (I08)
min_confidence_to_persist ≤ confidence < min_confidence_to_surface → store, mark uncertain (I09, storage half)
confidence ≥ min_confidence_to_surface                        → store, asserted
```

The uncertain band is *stored* by M1 and *asked about* by M3's digest. That split is honest and
worth stating: I09 is half-satisfied at the end of M1 and cannot be more than half-satisfied,
because the surface that asks does not exist yet.

The judge could not be written cleanly until §8's Q1 was answered — there is no value for
`min_confidence_to_persist` when a relation type has no `relation_thresholds` row, and
`relation_thresholds` ships with zero rows. Q1 closed on named constants in `core/relation`,
pinned to migration 0002's column defaults by an L2 test.

### 4.6 The golden corpora, and the M0 test that expires

All three `cases/` directories are empty, and `testdata/*/format.md` each name M1 as responsible
for filling them. What no artifact records — and what a plan will otherwise discover as a red
`make check` — is this:

> `test/conformance/golden_sets_test.go`'s `assertCasesDirIsEmpty` **fails the moment M1 adds
> the first case file**, in any of the three directories.

Verified by reading it: it reads each `cases/` directory and calls `t.Errorf` for every entry
that is not `.gitkeep`, with the message *"this change ships an empty corpus (R10.1's MUST NOT);
real cases are M1's responsibility"*. It is **untagged**, lives in `test/conformance/`, and
therefore runs inside `make check`'s fast loop.

This is a correct M0-era assertion whose expiry date is M1's first golden case. It does not get
deleted: it **inverts** into a non-empty-corpus guard, in the same spirit as design D10's
existing guards elsewhere in that file — a corpus directory that silently empties out must fail
loudly rather than let every consumer test iterate zero cases and report green. The first
golden-set PR carries the inversion.

### 4.7 The pending-red promotion, and the gate's own retirement

`test/conformance/pending_symbols.txt` anchors five symbols across three test files, and
`scripts/pending-red.sh` checks both directions: every listed symbol must be reported
`undefined:` by the compiler, and every reported `undefined:` must be listed. Partial, per-symbol
promotion is supported — the check is line-by-line.

**The helper trap.** `test/conformance/tree_scan_test.go` is itself `//go:build pendingimpl`
(verified, line 1) and provides `scanGoTree` to both I01 and I03. Build tags are additive: an
untagged file is compiled into *every* build, including `-tags pendingimpl`. So:

- Promoting I01 or I03 while the helper stays tagged breaks `make check` outright with
  `undefined: scanGoTree` — not merely `make pending-red`.
- Untagging the helper *alone*, with both callers still tagged, was **measured** to fail lint
  with `unused: func scanGoTree is unused`.

Therefore the helper's tag drops **in the same PR as the first of its two callers**, and not
before. The sequence:

| Step | Creates | Promotes | Drops from `pending_symbols.txt` | Also |
|---|---|---|---|---|
| 1 | `unit.Status`, `unit.AllStatuses` | `i01_focus_never_persisted_test.go` | 2 lines | **Untag `tree_scan_test.go`** |
| 2 | `ports.UnitRepo` | `i03_units_never_deleted_test.go` | 1 line | — |
| 3 | `recall.VectorQuery`, `recall.VectorIndex` | `i21_vector_search_filters_on_model_test.go` | 2 lines | Retire the gate — see below |

I21 is self-contained (it imports only `reflect`), so step 3 is independent of 1 and 2 and can
land in any order relative to them.

**The terminal trap, which no artifact records.** `pending-red.sh` has no empty-list
short-circuit: it runs `go test -c -tags pendingimpl` *first*, and failure mode 1 (lines 13–19)
fails the gate if that build **succeeds**. When the last of the five symbols is promoted and the
file holds only its comment header, the pendingimpl build compiles cleanly and the gate fails —
`make check-all` and CI's job with it. So the PR that promotes the last symbol **must also retire
the gate**: remove `pending-red` from `check-all` and from CI, and delete the script and the
symbols file. That is not incidental cleanup; leaving it is a red `main`.

### 4.8 What doc 02 gains, and why that is not overhead

`docs-sync.yml` fails any PR touching `internal/core/**` that does not also touch
`docs/02-cognitive-core.md`, unless it carries `no-spec-change`. Every M1 core PR has a genuine
delta to write, which is the gate working as designed rather than a tax:

| PR touches | Doc 02 gains |
|---|---|
| `core/unit` | §1 — the live-status predicate stated as the positive filter it is |
| `core/classify` | §5.1 — what "degrades to null" means field by field |
| `core/recall` | §5.2 and §13 — the recall entrance (q3b) and the `recall_top_k` row |
| `core/relation` | §4 — what governs a relation type with no thresholds row (q1) |
| corrections | §5 step 4 — how the referenced unit is resolved (q3c) |
| — | §12 or elsewhere — `units.confidence` (q2) |

The `no-spec-change` label exists for refactors. **No M1 core PR should need it.** If one does,
that is a signal the PR is not actually implementing a behavior doc 02 describes.

---

## 5. The chain

Chain strategy `stacked-to-main`, as M0 used. The 400-line ceiling is a soft ceiling and these
numbers are per-PR budgets chosen to respect it, not predictions — see the note below.

**Phase A — substrate** (nothing here writes a unit)

| # | PR | Content | Est. |
|---|---|---|---|
| 1 | `docs/m1-preflight` | doc 01 gains `openai` and a real `tasks.embedding` provider; doc 06 §1's tree gains `core/classify` | ~90 |
| 2 | `feat/core-unit` | `unit.Status`, `AllStatuses`, live predicate, taxonomy, transitions; L1 to ≥90 %; **promote I01, untag `tree_scan_test.go`**; doc 02 §1 | ~280 |
| 3 | `feat/ports-unitrepo` | `ports.UnitRepo` + an in-memory fake for L2; **promote I03**; doc 02 §1 | ~200 |
| 4 | `feat/store-unitrepo` | The SQLite implementation, positive `status='pool'` filter, L3; `store_api.golden` regenerated | ~380 |
| 5 | `feat/provider-fake` | `ports.LLMProvider`/`EmbeddingProvider`, the fixture-replaying fake, the first `testdata/llm/cases/`; **inverts `assertCasesDirIsEmpty`** | ~350 |
| 6 | `feat/providers-http` | anthropic, openai, ollama clients; `tasks:` routing; the task→provider reference check; `openai` added to `DocumentedProviderTypes` | ~420 |

**Phase B — the pipeline**

| # | PR | Content | Est. |
|---|---|---|---|
| 7 | `feat/core-classify` | The tolerant decoder (I14) + `testdata/classify/cases/` including all three broken shapes; doc 02 §5.1 | ~380 |
| 8 | `feat/core-recall` | `VectorQuery`, `VectorIndex`, brute-force top-K, RRF fusion + `testdata/recall/cases/`; **promote I21 and retire the pending-red gate**; doc 02 §5.2 and §13 | ~400 |
| 9 | `feat/store-search` | Embedding write, index load at vault open, the FTS5 query leg, the `model` filter (I21) at the storage boundary; L3 over a two-model vault | ~350 |
| 10 | `feat/brain-capture` | The pipeline: clock once, classify, persist, embed, log (I12, I18) | ~400 |
| 11 | `feat/relation-judge` | `core/relation` thresholds (I07, I08, I09-storage), the fallback resolved by q1, the judge call; doc 02 §4 | ~400 |

**Phase C — the surface**

| # | PR | Content | Est. |
|---|---|---|---|
| 12 | `feat/corrections` | In-place edit (I03), the `correction` signal (I13), referent resolution per Q3c — a scored fusion in `core/recall` and the margin gate over it, plus the per-field update methods of Q3c-iii; doc 02 §5 step 4 | ~450 |
| 13 | `feat/httpapi-capture-recall` | The capture, recall and read-only units routes; L4 | ~380 |
| 14 | `feat/cli-capture-demo` | `nooma capture`, the demo walked end to end, L4 — **last in time, after 15 and 16** | ~300 |
| 15 | `feat/init-provider-paths` | §3.2 item 14: `nooma init`'s two first-class paths, Cloud (recommended) and Ollama, writing a real `providers:` block that holds `api_key_env` and never a secret | ~300 |
| 16 | `feat/doctor-quality-gate` | §3.2 item 15: `nooma doctor`'s structured-JSON quality gate — the fixed prompt set against each task's configured provider, a failure naming it unsuitable *for that task*, over the `testdata/llm/cases/` corpus | ~350 |
| 17 | `feat/openai-embeddings` | An OpenAI embeddings client implementing `ports.EmbeddingProvider`, in the shape PR 6's three HTTP clients already established | ~200 |

> **PR 17 exists because M1's judged path could not run.** §3.2 records the owner's direction of
> 2026-07-31 — *"Cloud is the path that must work […] M1 is judged on the cloud path running end to
> end"* — and `internal/providers/` holds `anthropic/client.go`, `openai/client.go`,
> `ollama/client.go` and **exactly one embedder, `ollama/embed.go`**. A vault configured for Cloud
> had nothing to embed with: every capture would store a unit with no vector, and every recall
> would run on its lexical leg alone.
>
> Nothing was broken, which is why it survived planning. `EmbeddingProvider` is a port with a real
> implementation, `tasks:` routes per task, and D8's degradation path means a missing embedder
> **degrades rather than fails** — the capture still succeeds. The pipeline works; it just works on
> one leg, silently, in the exact configuration the milestone is judged on. **A degradation designed
> for an outage was absorbing a gap in the build plan**, and a mechanism that turns a missing
> component into a quieter product is the hardest kind of omission to see.
>
> Anthropic publishes no embeddings API, so OpenAI is the provider. PR 6 already built three HTTP
> clients against `ports.LLMProvider`; this is the same shape against `ports.EmbeddingProvider`.
>
> Surfaced by `sdd-design` while reconciling against the spec, and confirmed by listing the tree.

> **PRs 15 and 16 were scheduled on 2026-08-02, and the gap they close is the point.** §3.2 has
> listed both since 2026-07-31 and **no PR in any of this document's three phase tables built
> either** — verified against all fourteen rows, and against the code: `cmd/nooma/doctor.go` has
> existed since M0 with no quality gate, and `nooma init` has no Cloud or Ollama path at all.
>
> The failure shape is worth more than the fix. §3.2's own note already records that these two
> items are [ADR-0002](../../../docs/adr/0002-default-llm-preset.md)'s deliverables, that the ADR
> was `Accepted` on 2026-07-27, and that *"neither appeared in any milestone's bullets, in this
> proposal's scope, or in any task list"*. That note was written — and then the items were added to
> the scope section and to nothing else. **Writing down why something was missed is not scheduling
> it.** An item is scheduled when it is a row in a table that something reads; §3.2 is prose that
> nothing executes.
>
> They land before PR 14 because PR 14 is the demo, and the demo is what tags `v0.1.0`. A demo that
> needs a provider configured by hand, against a provider nothing checked is fit for the task, is
> not the milestone's exit criterion being met — it is the criterion being walked around.
>
> Surfaced by `sdd-spec` while scoping Phase C, which declared it as a conflict instead of
> resolving it unilaterally, and confirmed independently before the owner decided.

> **PR 12 moved from Phase B to Phase C on 2026-07-31**, resolving a contradiction inside this
> document. The table above listed `feat/corrections` under Phase B while §8's own closing
> paragraph said "Q3b and Q3c shape the recall and correction surfaces (Phase C)". Both sentences
> were written here, and they disagreed.
>
> **Q3c decides how a correction finds its referent, and at the time it was still open.** A phase
> cannot build what an unanswered question defines, so the table row was the wrong half. Q3c
> closed in full on 2026-08-02, and PR 12 stayed in Phase C — the move was right for a reason
> that outlived its cause: PR 12 depends on PRs 10 and 11, which are Phase B's last two. Surfaced by
> `sdd-spec`, which noticed the disagreement while scoping Phase B and flagged it instead of
> quietly picking one — the second contradiction inside a merged planning artifact this milestone
> has produced, after `spec.md` R2.3 and `design.md` D3 disagreed on `incomplete → archived`.

Dependencies: `1 → 6`, `2 → 3 → 4`, `5 → 6`, `(4,5) → 7`, `2 → 8`, `(4,8) → 9`,
`(6,7,9) → 10`, `(8,10) → 11`, `(10,11) → 12`, `(10,11,12) → 13`, `6 → 15`, `(5,6) → 16`,
`6 → 17`, `(13,15,16,17) → 14`. PR 1 is independent of everything and goes first. PR 8 can land any
time after PR 2. **PR 14 is last in time despite its number** — it is the demo, and the demo is M1's
exit criterion, so everything it walks through has to exist before it walks.

**On these estimates, and on M1's size.** M0 was planned as ten PRs and shipped as twenty; six
separate measurements put its estimates 1.3x–2.2x low. Read the table above the same way: **~4,300
budgeted lines across 14 PRs is realistically 6,000–9,000 across 20–30.** That is two to three
times M0 in one SDD change.

> That budget was written for fourteen PRs. It is now **seventeen** — PRs 15 and 16 add ~650, PR 17
> adds ~200, and PR 12 moved from ~330 to ~450 once Q3c closed and the scored fusion it needs got
> priced. Three rows, ~850 lines, all of them work M1 already owed and none of them new scope. The
> multiplier
> above is what matters here, not the base: Phase B closed with its own measurement of the same
> effect, recorded as C8 in `m1b-pipeline/tasks.md` — **every one of its estimates was low, and the
> two worst were 4.3x.** The lesson it drew is the one to apply to PRs 15 and 16: estimate a core PR
> from its invariant's proof obligation, not from its implementation.

This proposal's recommendation is therefore to **split M1 into three chained SDD changes along
the phase boundaries above** — `m1a-substrate`, `m1b-pipeline`, `m1c-surface` — sharing this
proposal and this scope, with their own spec, design and task artifacts. The phases are already
dependency-clean: Phase A ships no behavior a user sees and can be verified entirely by L1/L2/L3;
Phase B is where doc 02 becomes executable; Phase C is where the demo lands. One tasks artifact
covering thirty PRs is a tasks artifact nobody re-reads by PR twenty, which is exactly what M0's
retrospective measured. The owner decides; the chain above works either way.

---

## 6. Strict TDD ordering

Strict TDD is active. M1 is the first change where `docs/06-harness.md` §4's rule — *a
conformance test is written before the implementation that satisfies it, and watched failing red
for the right reason* — applies to invariants rather than to unit-level properties.

Three of M1's conformance tests already exist and have been red since the harness landed (I01,
I03, I21); their discipline is the promotion sequence of §4.7. The invariants M1 makes newly
testable need their tests written first:

1. **I14** — a truncated response, a wrong-typed field and an unknown enum each degrade one field
   to null and leave the rest of the classification intact. Written against
   `testdata/classify/cases/` before `core/classify` exists.
2. **I08** — a candidate below `min_confidence_to_persist` produces no `relations` row.
3. **I02** — a `superseded` and an `incomplete` unit are absent from every read surface, including
   recall's fused output. The recall corpus's `format.md` already models this case.
4. **I12** — a capture with an effect leaves a `decision_log` row; a capture with none does not.
5. **I13** — deleting a relation leaves its `relation_reject` signal behind. (The mechanism is
   testable in M1 even though no M1 surface triggers a rejection — see §3.4.)
6. **I18** — a classification carrying a due date writes `due_at`, not `event_at` or `created_at`.

Two floors bite here and neither runs in `make check`: **the ≥90 % `internal/core` coverage floor
fires on the first statement PR 2 lands**, and `docs-sync` fires only once a PR is open on GitHub.
Both are §8 risks, not surprises.

---

## 7. Verification

- `make check-all` green on every PR — explicitly including `make cover`, which `make check` does
  not run and which stops being vacuous at PR 2.
- L1 for every pure decision: the classify degradation, the RRF fusion, the top-K selection, the
  threshold decision, the status predicate.
- L2 for each invariant in §3.4's "in scope" rows, named after its invariant per the
  `nooma-testing` convention.
- L3 for the SQLite repositories, the FTS query leg, and the two-model vault that I21 exists for.
- L4 for capture and recall through the compiled binary.
- `make store-api-golden` re-run and its diff reviewed on PRs 4 and 9 — the golden is untagged
  and inside `test/conformance/`, so forgetting it turns `make check` red immediately.
- The demo run by hand: capture several messages, then ask about one of them.

---

## 8. Open questions

Each of these is a decision the owner makes. The recommendation is the proposal's reasoning, not
a settled answer.

### Q1 — What governs a relation type with no `relation_thresholds` row? **CLOSED — B.**

**Owner decision, 2026-07-31: named constants in `core/relation`, plus an L2 test pinning them to
migration 0002's SQL column defaults.** No migration, no precedence rule, no seed. Doc 02 §4 gains
one sentence naming the fallback.

The reason B beats A is the one the question was asked for: relation `type` is **open text**, so no
seed can ever be exhaustive, and a seed leaves the unseen-type case exactly where it started. C is
the right answer eventually — but eventually is M5, the milestone that actually tunes these knobs,
and a global-vs-per-type precedence rule two milestones before any consumer exists is scope the
loop does not need.

The one real objection to B is that a default then has **two sources** — the Go constant and the
SQL column `DEFAULT` — which can drift silently. That is closed by the L2 test, not argued away:
`i13_learning_signal_test.go` already reads migration SQL text off disk, so the pattern exists and
the cost is one test.



Verified: `relation_thresholds` has column defaults (`0.3` / `0.5`) but migration 0002 **seeds no
rows**, and `config` has **no columns** for these two thresholds. Doc 02 §4 and §13 state global
defaults of 0.30 and 0.50; nothing in the schema supplies them. Relation `type` is open text
(`same_topic`, `derived_from`, "…"), so no seed can be exhaustive.

| Option | Pro | Con |
|---|---|---|
| **A. Seed rows in migration 0003** | The value lives where the learning module already writes | Cannot cover an unseen type — the exact case that motivates the question. Needs doc 03 to document the seed |
| **B. Named constants in `core/relation`, used when no row exists** | Handles an unseen type by construction; no migration; satisfies harness §7's "one place" rule directly | Two sources for one default — the Go constant and the SQL column `DEFAULT` — which can drift |
| **C. Add the two columns to `config`** | A per-vault global that a user or the learning module can move | Migration 0003 + doc 03 edit + a new global-vs-per-type precedence rule, before anything needs one |
| **D. Auto-insert a row on first use** | Every type ends up with a row | A write on a read path, and the inserted values still have to come from A, B or C |

**Recommendation: B, plus an L2 test pinning the Go constants to the SQL column defaults.** The
drift objection is the only real one and it is cheap to close — `i13_learning_signal_test.go`
already establishes the precedent of an L2 test reading migration SQL text off disk. C is the
right answer eventually, but "eventually" is M5, the milestone that actually tunes these knobs;
introducing a precedence rule two milestones before a consumer exists is scope the loop does not
need. Doc 02 §4 gains one sentence naming the fallback.

### Q2 — What is `units.confidence`? **CLOSED — C.**

**Owner decision, 2026-07-31: doc 02 §12 claims the column as the perception gate's storage, and
M1 writes NULL.** `Unit.Confidence` stays `*float64` and is never set. One sentence in doc 02, no
schema change, no golden regenerated, no invented semantics.

A was the tempting one and it is the worse failure mode. If classify occupies the column with "how
sure the classifier was", the first real consumer — §12's `confidence < 0.4 → needs-review` gate in
v2 — arrives to find it already meaning something else, and the migration to fix that is far more
expensive than the sentence being written now.

B was never available on this project's own terms: ADR-0005's entire argument for cutting
perception from v1 is that **its seams are already in the schema**. Dropping one of those seams
would refute the ADR that justified the cut.

The defect was never the column. It was that doc 02 — the document that governs behavior — did not
say what the column is for.



Verified: the column exists in migration 0001 and in doc 03, with no comment on either side, and
**doc 02 never mentions it**. `relations.confidence` and `self_beliefs.confidence` are both
defined behaviorally; this one is not. Doc 02 §12 does define a confidence gate — *"doubt
(confidence < 0.4) → needs-review"* — for **perception**, which is v2.

| Option | Pro | Con |
|---|---|---|
| **A. Doc 02 defines it as classification certainty; classify writes it** | The column stops being undefined | Invents a semantics with no v1 consumer, and occupies the column before its documented v2 consumer arrives. Needs a §13 knob nobody can calibrate |
| **B. Drop the column** (migration 0003, `DROP COLUMN`, doc 03 edit, golden regenerated) | No undefined column | Destroys a v2 seam that ADR-0005 explicitly argues should already be in the schema |
| **C. Doc 02 §12 claims it as the perception gate's storage; M1 writes NULL** | One sentence, no schema change, no invented semantics, the seam survives | The column stays unwritten through all of v1 |

**Recommendation: C.** ADR-0005's entire argument for cutting perception is that the v2 seams are
already present — `measurements`, `ref_unit_id`, the shape-routing door. `units.confidence` is
that seam for §12's 0.40 gate; the defect is that doc 02 never says so. A is the worse failure
mode: if classify occupies the column with "how sure the classifier was", the first real consumer
in v2 finds it already meaning something else. If the owner prefers A, it needs a named consumer
and a §13 row, not just a definition.

### Q3 — Three scope boundaries

**3a. Are §5.5's hooks in M1 at all? CLOSED — out entirely.**

**Owner decision, 2026-07-31.** Classify still returns `timer`, `recurring_reminder` and
`person_ref_status: ambiguous` — that is classify's contract and the golden corpus covers it. M1
**arms nothing**, creates **no `incomplete` unit**, records the classification in `decision_log`,
and tells the caller "not yet" in plain words.

Minimal arming was rejected on the principle this project keeps returning to: **a timer armed by a
system that cannot fire it is worse than a refusal, because it promises silently.** The user asked
to be reminded, the system said yes, and nothing will happen. A refusal is a bad answer; a silent
false promise is a bug the user only finds when it matters.

On `incomplete` specifically: doc 02 §1 says such units are promoted or expired after 24 h **during
consolidation**, which is M2. An `incomplete` unit created in M1 would be invisible to every read
surface (I02) and **immortal** until M2 ships. So an ambiguous person reference produces a `pool`
unit and a `decision_log` entry, and **I06 is honestly out of scope rather than vacuously green** —
which is the difference between a gate that passes because it holds and one that passes because
nothing reaches it.

Doc 02 §5.5 gains a note, and the demo must not be shown a timer.



- *Out entirely (recommended)*: classify still returns `timer`, `recurring_reminder` and
  `person_ref_status: ambiguous` — that is classify's contract and the golden corpus covers it —
  but M1 arms nothing, creates no `incomplete` unit, and records the classification in
  `decision_log` with an honest "not yet" to the caller. Cost: doc 02 §5.5 gains a note; the demo
  must not be shown a timer.
- *Minimal arming*: writes a `timers` row nothing can fire. A timer armed by a system that cannot
  fire it is worse than a refusal, because it silently promises.
- On `incomplete` specifically: doc 02 §1 says such units are promoted or expired after 24 h
  **during consolidation**, which is M2. An `incomplete` unit created in M1 is invisible to every
  read surface (I02) and immortal until M2 ships. **Recommendation: M1 creates none**; an ambiguous
  person reference produces a `pool` unit and a `decision_log` entry. I06 is then honestly out of
  scope rather than vacuously green.

**3b. Is `/recall` standalone, or does it route through classify? CLOSED — standalone.**

**Owner decision, 2026-08-02**, recorded late: Phase B had already built it this way.
`internal/brain/recall.go`'s `RecallService.Candidates(ctx, content, vector, model, excludeID)`
takes the text and its embedding and runs both legs — no classify call on the read path. The
conformance-shaped property below (capture-`recall` and `/recall` return the same answer for the
same text) is unbuilt, because `/recall` itself is Phase C, and it belongs to `m1c-surface`.

The question stayed open in this document for one milestone-phase after the code answered it.
That is the drift doc 02's non-negotiable exists to prevent; the proposal is not doc 02, but the
lesson transfers — **a decision the code has already made is still a decision to write down.**

- *Standalone (recommended)*: `/recall` takes a query string, embeds it, runs both legs, fuses,
  returns units. Capture's `type=recall` calls the same `brain/recall` service. One mechanism, two
  entrances. Routing a query through classify would spend an LLM call to discover that a query is
  a query, and would make the demo's recall fail whenever the classify provider is misconfigured.
- *Through classify*: one entrance, closer to a literal reading of doc 02 §5. It buys prompt
  normalization; it costs latency, an LLM dependency on the read path, and a second failure mode.
- Either way, capture-`recall` and `/recall` must return the same answer for the same text. That
  is a conformance-shaped property worth its own test.

**3c. How does a correction find "the referenced unit"?** Doc 02 §5 step 4 says a correction edits it
in place and never says which one it is.

> **Owner decisions, 2026-08-02.** Q3c turned out to be **three entangled questions**, not one.
> All three are now closed.
>
> **3c-ii, is a wrong referent recoverable? CLOSED — yes.**
> [ADR-0016](../../../docs/adr/0016-correction-pre-image.md): the values a correction is about to
> overwrite are written to `decision_log.context` first, and a failed audit write blocks the edit.
> This resolved a contradiction inside doc 02 — §4 argues that inferring-and-destroying in one act
> is forbidden, while §5 step 4 mandates an in-place edit. The distinction neither paragraph drew:
> a duplicate is inferred **whole**, whereas a correction splits — the user says *what* to change,
> explicitly; only *which unit* is inferred.
>
> **3c-iii, which columns may a correction write? CLOSED — one method per field.**
> `ports.UnitRepo` gains `UpdateEventAt` and `UpdateDueAt` alongside the existing `UpdateContent`.
> Not one `UpdateFields(patch)`: **I18 stays structural rather than careful.** No signature is
> capable of writing the wrong date, which is the same shape `Kind.UnitType()`'s `(value, bool)`
> and `LiveByIDs`'s un-parameterisable status already use — a name that says what it does, and no
> argument that could mean something else. The cost is accepted: the port grows one method per
> correctable field.
>
> This question had to be answered before the referent one, because **PR 12 was unbuildable
> without it**: `UpdateContent(id, content, at)` cannot write the `event_at` that the corpus case
> `correction-not-friday.json` — "no, the dentist is on the 15th, not the 14th" — expects.
>
> **3c-i, which unit? CLOSED — scored fusion, with a margin gate over the top two.**
>
> An explicit `unit_id` wins wherever the caller has one. Chat has none, so there the referent
> comes from running hybrid recall against the correction text and gating on the **ratio** of the
> top two fused scores: below `correction_referent_margin`, the system asks rather than guessing.
> Doc 02 §5 step 4 and §13 carry it.
>
> Two findings shaped the answer, both verified before it was taken:
> - **The recommendation below could not be built as written.** "A pure function over ranked
>   candidates" needs magnitudes, and `recall.Fuse` (`internal/core/recall/fuse.go:55`) returns
>   `[]string`, computing scores into a local map it discards. So the decision carries a cost the
>   question did not price: `core/recall` must expose a fusion that keeps its scores. The
>   ranked-id fusion stays — the scored one is an addition, not a replacement, and ADR-0010's
>   surface grows rather than changes.
> - **The margin is a ratio, not a difference.** RRF at `k = 60` compresses hard: first-on-both-legs
>   scores `2/61` and second-on-both scores `2/62`, so 0.0005 separates a near-tie — while
>   present-on-one-leg scores `1/61`, half the first. An absolute gap means different things
>   depending on how many legs contributed. This is the only part of the answer that was not in
>   the options below, and it is the part that makes the gate work.
>
> Two alternatives were weighed and rejected. An **ordinal-only gate** (one candidate → correct,
> two or more → ask) needs no scored fusion at all, but more than one candidate is the normal
> case, so it would ask almost always — a gate that fires constantly is a refusal wearing a
> question's clothes. **`unit_id`-only** is right for the UI and the API and useless for chat: it
> would push correction-by-chat out of M1 and orphan the corpus case `correction-not-friday.json`,
> which the milestone already committed to answering.
>
> **ADR-0016 made this easier rather than answering it.** A recoverable edit can be authorised by
> a lower margin than an irreversible one, so the number is no longer entangled with the
> destruction question — which is why it lands in §13 as a calibratable default rather than in an
> ADR of its own.

- *Explicit `unit_id`*: right for the UI and the API; chat has no id to send.
- *Hybrid recall against the correction text, with an ambiguity gate (recommended)*: reuses the
  mechanism M1 is already building, and the pick-or-ask decision is a pure function over ranked
  candidates — L1-testable, no LLM. It matches doc 02's product rule exactly: asking is the
  exception, and an ambiguous referent is the exception.
- *LLM inference over recent conversation*: needs a conversation-history concept no doc defines
  and no table stores.
- **Recommendation: recall-based, with an explicit `unit_id` override when the caller has one.**
  Doc 02 §5 step 4 gains the sentence.

### Q4 — Does the classify corpus cover taxonomy values whose hooks are deferred? **CLOSED — yes.**

**Owner decision, 2026-08-02**, recorded late for the same reason as Q3b: Phase B built it this
way. `testdata/classify/cases/` carries `timer-pasta-ten-minutes.json` and
`recurring-reminder-water-plants.json` among its 17 cases, and Q3a's own decision depends on the
corpus covering exactly these — M1 arms nothing, but classify must still return them.

**Recommendation: yes.** `timer`, `recurring_reminder`, `chitchat` and `out_of_scope` are part of
classify's contract regardless of what happens downstream, and the corpus is shared with
`nooma doctor`'s JSON quality gate (ADR-0002) — a provider that cannot produce `timer` is
unsuitable for the task whether or not M1 arms one. It also matters mechanically: proving the
"unknown enum" degradation shape requires a value that is genuinely outside the taxonomy, which
means the taxonomy in the corpus has to be complete.

### Q5 — One SDD change, or three? **CLOSED — three.**

See §5's closing note. **Owner decision, 2026-07-30: three chained SDD changes**, split on the
phase boundaries — `m1a-substrate`, `m1b-pipeline`, `m1c-surface` — sharing this proposal and
this scope, each with its own spec, design and tasks.

What that buys immediately, and why it was the first question answered rather than the last:
**Phase A is not blocked by any other open question here.** Q1 shapes the relation judge (Phase
B), Q3b and Q3c shape the recall and correction surfaces (Phase C). Q2 decides whether Phase A
writes `units.confidence` at all, and its recommended answer — doc 02 claims the column, M1
writes NULL — costs Phase A nothing either way. So `m1a-substrate` can be specified, designed
and started while the remaining four questions are still open, instead of the whole milestone
waiting on decisions that only two of its fourteen PRs depend on. (Fourteen at the time; PRs 15
and 16 were scheduled on 2026-08-02.)

The cost is real and accepted: three planning cycles rather than one, and a proposal that now
lives one directory away from the specs that implement it. This document stays the single
umbrella — the three changes do not restate its scope, they reference it.

---

## 9. Risks

| # | Risk | Mitigation |
|---|---|---|
| R1 | **The coverage floor fires in CI but not locally.** `make check` never runs `scripts/core-coverage.sh`; it is armed and vacuous today (`total=0` → exits 0) and goes live on PR 2's first statement | Every core PR ships its L1 tests in the same commit as the code, and `make check-all` is the pre-PR command, not `make check`. This risk is structural: the fast loop cannot catch it |
| R2 | **`docs-sync` fires only once a PR is open.** It needs `base_ref` and the label array, which no Makefile can produce | §4.8 gives every core PR a real doc 02 delta up front, so the gate is satisfied by design rather than by remembering |
| R3 | **`store_api.golden` fires in the fast loop** and needs `make store-api-golden`, a different target from `make schema-golden` | Named in PRs 4 and 9's task lists explicitly |
| R4 | **Promoting a pendingimpl caller without untagging `tree_scan_test.go` breaks `make check` outright**, and untagging the helper alone fails lint as `unused` | §4.7's sequence, with the helper's untag bound to PR 2 |
| R5 | **The pending-red gate fails when the last symbol is promoted** — verified: no empty-list short-circuit | PR 8 retires the gate in the same PR, not after |
| R6 | **The first golden case turns `make check` red** via `assertCasesDirIsEmpty` | PR 5 inverts it into a non-empty guard |
| R7 | **Estimates run 1.3x–2.2x low**, measured six times across M0 | §5's split recommendation, and a per-PR split decision made *before* apply, never discovered during it |
| R8 | **The fake provider's replay key** — `format.md` already warns that `prompt` is fragile, since real prompts vary with beliefs and the clock | Keyed on case `id`; the recorded `prompt` is documentation. Settled in design |
| R9 | **`exclusions.rules` scopes `forbidigo` to `internal/core/` only.** A `time.Now()` in `brain/` is legal and correct — but a second `Now()` mid-operation is the bug class §2 of the harness exists to prevent, and no lint catches it there | One clock read per capture, at the entry point, passed down as a value. A review property, not a gate — stated so it is known to be one |
| R10 | **A new build tag would silently exempt code from lint.** `.golangci.yml` sets `build-tags: [integration, e2e]`; `pendingimpl` is deliberately excluded and its own comment warns about the pattern | M1 introduces no new build tag. If it must, it goes into `run.build-tags` in the same PR |
| R11 | **RRF discards magnitude**, which matters most in dedup — ADR-0010 names this as its own cost | The ADR's stated mitigation is the judge seeing full candidate content, not scores. The recall corpus's near-duplicate requirement is what keeps this honest |
| R12 | **A migration, if any open question forces one**, requires a hand edit to `schema_doc_test.go`'s hand-written anchor list and to doc 03, and `0001`/`0002` are published and never editable | Both recommended answers to Q1 and Q2 avoid a migration. If the owner picks Q1-C or Q2-B, the migration cost is real and belongs in that PR's estimate |

---

## 10. Next step

**`m1a-substrate`**: `sdd-spec` and `sdd-design` run in parallel over this proposal, scoped to
Phase A's six PRs. Nothing in §8 blocks them.

**`m1b-pipeline` and `m1c-surface`** wait on the owner's remaining answers: Q1 blocks a clean
design for the relation judge (PR 11), Q3b and Q3c shape the recall and correction surfaces
(PRs 8, 12, 13). Those questions are not asked again from scratch when their phase begins —
they are asked here, with options and a recommendation, and answered before that phase's spec.
