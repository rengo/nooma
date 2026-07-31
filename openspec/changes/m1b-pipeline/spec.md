# Spec — M1b: the pipeline

Delta specification for the `m1b-pipeline` change, the second of three chained SDD changes
that split `openspec/changes/m1-capture-recall/proposal.md` (owner decision, 2026-07-30,
proposal §8 Q5). This document states what MUST be true of the repository after this change is
applied, in testable form. It does not prescribe how (that is `design.md`'s job, run in
parallel over the same proposal).

Sources verified by direct reading, not inferred: `openspec/changes/m1-capture-recall/proposal.md`
(§3.4, §4.1–§4.8, §5 Phase B table, §8 Q1/Q2/Q3a/Q3b/Q3c/Q4/Q5), `docs/02-cognitive-core.md` §1,
§2, §4, §5, §8, §9, §11, §13, `docs/03-data-model.md` (`relations`, `relation_thresholds`,
`unit_embeddings`, `units_fts` + triggers, `decision_log`, `learning_signals`),
`docs/06-harness.md` §3–§7, `docs/adr/0002-default-llm-preset.md`,
`docs/adr/0003-embeddings.md` (+ its 2026-07-28 amendment), `docs/adr/0010-hybrid-recall-fusion.md`,
`docs/adr/0012-vector-proximity-search.md`, `openspec/changes/m1a-substrate/spec.md`, and the
Phase A tree as it exists today: `internal/core/unit/*.go`, `internal/ports/{unitrepo,provider,clock}.go`,
`internal/store/sqlite/{unitrepo.go,open.go,migrations/*.sql}`,
`test/support/{memrepo,fakeprovider,goldenset}/*.go`, `internal/providers/{anthropic,openai,ollama}/*.go`,
`internal/config/validate.go`, `test/conformance/{pending_symbols.txt,tree_scan_test.go,
i13_learning_signal_test.go,i21_vector_search_filters_on_model_test.go,golden_sets_test.go}`,
`Makefile`, `scripts/pending-red.sh`, `.github/workflows/ci.yml`, `testdata/{classify,recall,llm}/format.md`.

---

## 0. Scope boundary, and one deliberate narrowing recorded rather than silently applied

> Phase B is the capture pipeline's decisions and the recall mechanism: classify, unit
> persistence, embedding + FTS sync, hybrid recall (RRF), and the dedup/relation judge with its
> persist/surface thresholds. `internal/core` grows beyond `unit/` for the first time.

**Honour, do not reopen, the three CLOSED decisions** (umbrella proposal §8, owner decisions
2026-07-31):

- **Q1**: a relation type absent from `relation_thresholds` falls back to named constants in
  `core/relation`, pinned to migration 0002's SQL column `DEFAULT`s by an L2 test reading the
  migration text off disk. No migration, no seed, no config columns.
- **Q2**: `units.confidence` stays `*float64`, always `nil` through this change (already true of
  `unit.Unit.Confidence` since Phase A — verified in `internal/core/unit/unit.go`). Nothing in
  Phase B writes it.
- **Q3a**: classify's taxonomy still returns `timer`, `recurring_reminder`, and
  `person_ref_status: ambiguous` — that is classify's contract. Phase B arms nothing, creates no
  `incomplete` unit, records the classification in `decision_log`, and refuses in plain words.
  I06 is honestly out of scope, not vacuously green (§4 below states exactly what this means for
  each shape).

**A scope narrowing relative to the umbrella proposal's own Phase B table (§5), recorded here
rather than applied silently**: the proposal's §5 table lists `feat/corrections` as Phase B's PR
12, covering I03/I13's correction path and referent resolution "per q3c". This spec's own
governing instructions state that Q3b (the recall entrance) and Q3c (correction referent
resolution) **both belong to Phase C**, and Q3c is not closed. A correction-referent
requirement cannot be written without either reopening Q3c (out of bounds for this document) or
inventing an answer the proposal never gave (also out of bounds — "verify, do not infer").
**This spec therefore covers only PRs 7–11 of the proposal's Phase B table**
(`feat/core-classify`, `feat/core-recall`, `feat/store-search`, `feat/brain-capture`,
`feat/relation-judge`); PR 12 (`feat/corrections`) and its invariants I03's correction half and
I13 move to `m1c-surface` alongside Q3b/Q3c, where the proposal itself says the answers land.
The consequence is stated plainly in §8 below rather than left for a reader to discover.

**Every requirement below is bounded by PRs 7–11.** This change does not implement HTTP routes,
the CLI capture command, corrections, or anything from Phase A (already delivered) or the
explicit non-goals of umbrella proposal §3.3 (`effective_weight`, consolidation, triggers,
timers actually firing, self-model derivation, the learning pass, Telegram, `reindex`,
perception).

---

## 1. `internal/core/classify` (PR7 — `feat/core-classify`)

Traced to `docs/02-cognitive-core.md` §5 ("Capture", step 1) and `docs/06-harness.md` §5's
JSON gate corpus (ADR-0002).

### R1.1 — The classification taxonomy is the thirteen values doc 02 §5 names

**MUST**: `internal/core/classify` declares (or otherwise makes decodable into a closed
vocabulary) exactly the thirteen taxonomy values doc 02 §5 lists: `task`, `mental_load`,
`event`, `knowledge`, `procedural`, `emotional`, `chitchat`, `out_of_scope`, `recall`,
`correction`, `timer`, `recurring_reminder`, `list`.

**MUST NOT**: this taxonomy be confused with, or merged into, `unit.Type` (`internal/core/unit`)
— m1a-substrate's R2.4 already forbids `unit.Type` from gaining `timer`/`recurring_reminder`;
this requirement is classify's own vocabulary, a separate concern, verified by reading both
lists side by side.

**Verified by**: L1 — a table test asserting the thirteen-member set, no more, no fewer.

### R1.2 — A malformed field degrades to null; the rest of the classification survives (I14)

**MUST**: `internal/core/classify` exposes a pure decoding function — data (the provider's raw
response bytes) in, a classification result out — that, for each of the shapes below, degrades
**only the affected field** to its zero/absent value and returns a classification the caller can
still use for every other field:

1. **Truncated JSON**: the response is not a complete JSON document.
2. **A wrong-typed field**: a field decodes to a JSON value of the wrong Go-side type (e.g.
   `weight` recorded as a string).
3. **An unknown enum value**: `type` (or one of the six orthogonal resolution fields) names a
   value outside its documented vocabulary.

**MUST NOT**: any of the three shapes above cause the whole classification to fail, panic, or
return no result at all — I14's own wording ("it never aborts the classification",
`docs/06-harness.md` §4).

**MUST**: the six orthogonal resolution fields (`nudge_outcome`, `relation_outcome`,
`state_outcome`, `task_checkin_outcome`, `list_op`, `person_ref_status`) degrade independently of
`type`, `normalized_content`, `weight`, and `decay_rate` and of each other — doc 02 §5's own
"orthogonal fields, not types" framing, matching `testdata/classify/format.md`'s cross-field
section.

**Verified by**: L1 — one test per shape (truncated JSON, wrong-typed field, unknown enum value),
each asserting the degraded field is the zero/absent value and every other field survives intact;
L2 — `test/conformance/`, a named `TestI14_*` test driving the decoder against the real cases
R1.6 below adds to `testdata/classify/cases/`.

**Scenario**:
- GIVEN a provider response whose `weight` field is the JSON string `"high"` instead of a number
- WHEN the decoder processes it
- THEN the returned classification carries `weight = nil` (or the type's absent-value
  equivalent), `type`/`normalized_content`/`decay_rate` are populated as recorded, and no error
  aborts the call

### R1.3 — Classify assigns initial `weight` and `decay_rate` (λ), never `event_at`/`created_at`/`due_at` confusion (I18)

**MUST**: the decoded classification carries the provider-assigned `weight` and `decay_rate`
(λ) — doc 02 §2's "initial assignment during classify" — as distinct fields from any of the
three timestamp-shaped fields.

**MUST NOT**: the decoder ever populate `event_at` from a value the provider intended as
`due_at`, or `created_at` from either — I18. Since classify's raw response names these fields
explicitly and the decoder does no date arithmetic of its own (that is out of scope: doc 02 §5
says the local date and timezone are *injected context* for the provider, not something
`core/classify` computes), this is satisfiable by the decoder simply not aliasing three
distinctly-named JSON fields onto one Go field.

**Verified by**: L1 — a case carrying a due date asserts the decoded result's due-date-shaped
field is populated and the event/created-shaped fields are not, and vice versa for a case
carrying an event date.

### R1.4 — Classify's contract includes `timer`, `recurring_reminder`, and `person_ref_status: ambiguous` — it returns them, it does not act on them (Q3a)

**MUST**: `internal/core/classify` decodes `type: "timer"`, `type: "recurring_reminder"`, and
`person_ref_status: "ambiguous"` exactly as it decodes any other taxonomy value — a pure
function of the bytes, per R1.1/R1.2.

**MUST NOT**: `internal/core/classify` itself arm anything, create a `timers` row, or decide
what `internal/brain` does with these values — that is §4 below's job (Q3a's closed decision:
Phase B "arms nothing"). `core/classify` returning these values is what proves the taxonomy is
still classify's contract; what happens next is orchestration, not decoding.

**Verified by**: L1 — a case with `type: "timer"` and a case with `person_ref_status: "ambiguous"`
both decode successfully with no special-cased branch differentiating them from any other
taxonomy value at the decoder level.

### R1.5 — `testdata/classify/cases/` gains real cases covering the full taxonomy and all three I14 shapes (Q4)

**MUST**: `testdata/classify/cases/` (beyond `.gitkeep`) contains at least one case exercising
every one of the thirteen taxonomy values from R1.1, including `timer`, `recurring_reminder`,
`chitchat`, and `out_of_scope` — proposal §8 Q4's "yes": these are part of classify's contract
regardless of what happens downstream, and the corpus is shared with `nooma doctor`'s
structured-JSON quality gate (ADR-0002), which needs a provider proven capable of producing
every taxonomy value, not just the ones M1 acts on.

**MUST**: the corpus contains at least one case per I14 shape (truncated JSON, a wrong-typed
field, an unknown enum value), each with `llm_case_id` set to a `testdata/llm/cases/` recording
of the same defect — `testdata/classify/format.md`'s documented cross-field constraint.

**MUST**: the corpus contains at least one case exercising `person_ref_status: "ambiguous"`, to
back R4.6 below's behavioral requirement on the ambiguous-person-reference path.

**Verified by**: `test/support/goldenset.Load` successfully loads every file under
`testdata/classify/cases/`; a review-level check (per `testdata/classify/format.md`'s own "not
mechanized" note on enum coverage) that all thirteen taxonomy values and all three I14 shapes
appear across the corpus.

### R1.6 — `testdata/classify/cases/`'s empty-corpus guard inverts, and only for `classify`

**MUST**: `test/conformance/golden_sets_test.go`'s `casesDirMustBeEmpty["classify"]` becomes
`false` in this PR (the same rules-as-data pattern R5.4 of `m1a-substrate`'s spec already used
to invert `llm`'s entry) — `testdata/classify/cases/` now holding real files (R1.5) must be
asserted non-empty, not empty.

**MUST NOT**: this PR touch `casesDirMustBeEmpty["recall"]` — `testdata/recall/cases/` stays
asserted empty until PR8 (§2 below) inverts it in turn; a single change touching both would
either fail this PR's own vacuous-empty gate (if `classify`'s cases exist before the entry
flips) or wrongly demand `recall` cases this PR does not create.

**Verified by**: `go test ./test/conformance/ -run TestHarness_GoldenSetFormatsDeclared`, with
`classify/cases/` non-empty and `recall/cases/` still holding only `.gitkeep`; observed failing
first against the pre-PR7 state (`docs/06-harness.md` §4's before-the-implementation discipline).

**Scenario**:
- GIVEN `testdata/classify/cases/` populated by R1.5 and `testdata/recall/cases/` still holding
  only `.gitkeep`
- WHEN `TestHarness_GoldenSetFormatsDeclared` runs
- THEN the `classify` subtest passes because `cases/` is non-empty and the `recall` subtest
  passes because it is still empty — both are correct at this point in the chain

### R1.7 — Doc 02 §5.1 gains the field-by-field degradation definition

**MUST**: `docs/02-cognitive-core.md` §5 gains prose stating, field by field, what "degrades to
null" means for classify's output — proposal §4.8's table entry for `core/classify`, satisfying
`docs-sync.yml`'s rule that a PR touching `internal/core/**` also touches doc 02.

**MUST NOT**: this PR carry the `no-spec-change` label — it has a genuine behavioral delta to
document.

**Verified by**: `docs-sync.yml` (verifiable only once a PR is open on GitHub, not locally
reproducible by a Makefile target, per the precedent `m1a-substrate` R8.3 already states for
this same gate).

---

## 2. `internal/core/recall` (PR8 — `feat/core-recall`)

Traced to `docs/02-cognitive-core.md` §5 (step 2, "hybrid recall") and §13,
[ADR-0010](../../../docs/adr/0010-hybrid-recall-fusion.md),
[ADR-0012](../../../docs/adr/0012-vector-proximity-search.md).

### R2.1 — `recall.VectorQuery` and `recall.VectorIndex` exist, each carrying a string-kind `Model` field

**MUST**: `internal/core/recall` declares an exported `VectorQuery` type and an exported
`VectorIndex` type, each with an exported `Model` field of string kind (`reflect.String`) —
exactly the shape `test/conformance/i21_vector_search_filters_on_model_test.go` already anchors
to (verified: the pendingimpl test's own "Assumed shape" comment states this precisely; the
symbol names and field shape are not this spec's invention, they are already fixed by the
existing conformance anchor and `test/conformance/pending_symbols.txt`).

**Verified by**: L1 — `TestI21_VectorSearchFiltersOnModel`, promoted from `pendingimpl` into the
untagged suite by this PR (see R2.8 below), passes.

### R2.2 — Vector search is a pure top-K selection over `(VectorQuery, VectorIndex)`

**MUST**: `internal/core/recall` exposes a pure function taking a `VectorQuery` and a
`VectorIndex` and returning the top-K nearest entries by dot product over unit-normalized
vectors — ADR-0012's brute-force decision, restated as a core-level contract: exact results, no
tuning, no I/O.

**MUST**: this function performs no SQLite access, no file I/O, and no network call — it is
tested at L1 with no database, exactly the property ADR-0012 states as its own payoff.

**Verified by**: L1 — a table test over a small in-memory `VectorIndex` fixture asserting the
returned top-K order matches a hand-computed dot-product ranking.

### R2.3 — Vector search never compares or returns entries from a different model than the query's (I21, behavioral half)

**MUST**: top-K selection never ranks an entry whose model differs from `VectorQuery.Model`.

**Corrected 2026-07-31 against the conformance test that already anchors this.** This requirement
originally described `VectorIndex` as holding entries from more than one model, with the filter
applied at query time. `test/conformance/i21_vector_search_filters_on_model_test.go:52-57` states
the opposite shape, and it was written first: *"`VectorIndex` is itself scoped to one model via
its own exported string-kind `Model` field (the 'vault can hold two models at once' case is then
two `VectorIndex` values, one per model, **never one index serving both**)."*

The conformance test wins, and not merely because it is older. `docs/06-harness.md` §4 allows
exactly two exits when a conformance test and an implementation disagree — fix the code, or change
doc 02 and its ADR — and "widen the index so the spec's wording fits" is neither. A spec written
after the anchor does not get to redefine what the anchor pins.

The mid-reindex case doc 02 §5 and ADR-0003 describe is still covered: a vault holding two models
holds **two indexes**, and a query naming one model reaches one of them. The scenario below is
satisfied unchanged.

**MUST NOT**: two embeddings from different models ever be compared against each other by this
function, directly or via a shared score — ADR-0003: "the distance between them is noise shaped
like a number".

**Verified by**: L1 — a `VectorIndex` fixture seeded with entries from two distinct model
strings, asserting a query naming one model returns only that model's entries, ranked correctly,
with the other model's entries absent from the result regardless of their raw similarity score.
This is I21's behavioral half; R2.1 above proves only that the invariant is *expressible*
(the pendingimpl test's own stated "honest limitation").

**Scenario**:
- GIVEN a `VectorIndex` holding three entries from model `"a"` and two from model `"b"`, where one
  of the model-`"b"` entries has a higher raw dot product against the query vector than every
  model-`"a"` entry
- WHEN a `VectorQuery` naming model `"a"` runs against this index
- THEN the result contains only model-`"a"` entries, and the higher-scoring model-`"b"` entry
  never appears, regardless of its score

### R2.4 — RRF fusion is a pure function over two ranked ID lists, `k = 60` a named constant

**MUST**: `internal/core/recall` exposes a pure fusion function taking two ranked lists of unit
IDs (a vector-leg ranking and a lexical-leg ranking) and returning one fused ranking, computed
per [ADR-0010](../../../docs/adr/0010-hybrid-recall-fusion.md)'s formula:
`score(d) = Σ 1/(k + rank_i(d))` over the lists `d` appears in, 1-indexed ranks, `k = 60`.

**MUST**: `k = 60` is a single named constant in `internal/core/recall`, not a literal repeated
at each call site — `docs/06-harness.md` §7's calibratable-number rule.

**MUST NOT**: this function read a database, a network, or any I/O — ADR-0010: "Fusion is a pure
function over two lists of IDs: it gets tested with no SQLite, no embeddings, and no network."

**Verified by**: L1 — a table test reproducing ADR-0010's formula by hand over a small pair of
ranked lists, including at least one ID present in only one list (contributing a single term,
per the ADR's own wording).

### R2.5 — `recall_top_k` is a new named calibratable, added to doc 02 §13

**MUST**: `internal/core/recall` (or its caller, per design's placement choice) is driven by a
single named constant for "how many results each leg returns before fusion" —
`docs/02-cognitive-core.md` §5 already says "top-K by vector similarity + top-K by FTS" but names
no K; verified: §13's calibration table lists RRF's `k = 60` but has no `recall_top_k` row.

**MUST**: `docs/02-cognitive-core.md` §13 gains a `recall_top_k` row in this PR — `docs/06-harness.md`
§7: "every number in the doc 02 §13 table is a named constant in exactly one place."

**MUST**: the same K value governs both legs (vector and lexical) — doc 02 §5's own phrasing
implies symmetry; an asymmetric K would be an invented behavior this spec does not authorize.

**Verified by**: L1 — a test asserting the constant is referenced (not re-literaled) by both the
vector-leg and lexical-leg call sites, where those call sites exist in this change (§3 below);
review — `docs/02-cognitive-core.md` §13's new row matches the constant's value.

### R2.6 — `testdata/recall/cases/` gains real cases with a distractor, a near-duplicate pair, and a lexical/vector disagreement

**MUST**: `testdata/recall/cases/` (beyond `.gitkeep`) contains at least one case satisfying
`testdata/recall/format.md`'s "what makes a good case" section: at minimum one distractor (a
unit sharing vocabulary with a query but excluded or ranked low), one near-duplicate pair, and
one lexical/vector disagreement (the best lexical match and the best vector match differ) —
across the corpus, not necessarily all three in one case file.

**MUST**: every unit ID appearing in any case's `expected_unit_ids` names a unit in that same
case whose `status` is `pool` — `testdata/recall/format.md`'s documented cross-field constraint,
which is the storage-level twin of I02.

**Verified by**: `test/support/goldenset.Load` successfully loads every file under
`testdata/recall/cases/`; review confirming the distractor/near-duplicate/disagreement
properties are present somewhere in the corpus (the loader does not mechanize this, per
`testdata/recall/format.md`'s own "What the loader does and does not check").

### R2.7 — `testdata/recall/cases/`'s empty-corpus guard inverts, and only for `recall`

**MUST**: `casesDirMustBeEmpty["recall"]` becomes `false` in this PR. `casesDirMustBeEmpty["classify"]`
is already `false` from PR7; this PR does not re-touch that entry.

**Verified by**: `TestHarness_GoldenSetFormatsDeclared` with `recall/cases/` non-empty; observed
failing first against the pre-PR8 state.

### R2.8 — I21 is promoted, and this PR retires the `pending-red` gate (§4.7's terminal trap)

**MUST**: this PR, in the same PR that creates `recall.VectorQuery`/`recall.VectorIndex`:

1. drops the `//go:build pendingimpl` tag from
   `test/conformance/i21_vector_search_filters_on_model_test.go`, moving it into the untagged L2
   suite;
2. removes both lines (`recall.VectorQuery`, `recall.VectorIndex`) from
   `test/conformance/pending_symbols.txt`, leaving that file with **zero** tracked symbols.

**MUST**: because this promotion empties `pending_symbols.txt` — verified: it holds exactly
these two lines today, per `m1a-substrate`'s own R7.3 — this PR also, in the same PR:

3. removes the `pending-red` target from `Makefile`'s `check-all` dependency list (and the
   `pending-red` `.PHONY` target itself);
4. removes the `pending-red` job from `.github/workflows/ci.yml`;
5. deletes `scripts/pending-red.sh` and `test/conformance/pending_symbols.txt`;
6. deletes `test/conformance/tree_scan_test.go`'s `pendingimpl` support entirely if nothing else
   references it — verified: by the end of Phase A, `tree_scan_test.go` is already untagged and
   its `scanGoTree` helper is called by the now-untagged I01/I03 tests, so this step is scoped to
   confirming no reference to the retired pendingimpl machinery remains, not to deleting
   `scanGoTree` itself (I01/I03 still need it).

**MUST NOT**: this PR retire the gate *before* promoting I21, or promote I21 without retiring the
gate in the same PR — `scripts/pending-red.sh` has no empty-list short-circuit (verified: it
runs `go test -c -tags pendingimpl` first and fails if that build **succeeds**), so once the last
symbol is promoted, the pendingimpl build compiles cleanly and the gate fails `make check-all`
and CI outright unless retired in the same PR.

**Verified by**: `make check` and `make check-all` both pass after this PR with no `pending-red`
target present; `git diff --name-only` shows `scripts/pending-red.sh` and
`test/conformance/pending_symbols.txt` deleted.

**Scenario**:
- GIVEN this PR's diff, which adds `recall.VectorQuery`/`recall.VectorIndex`, untags
  `i21_vector_search_filters_on_model_test.go`, and removes `pending-red` from `Makefile`,
  `ci.yml`, and deletes `scripts/pending-red.sh` and `pending_symbols.txt` — all in one PR
- WHEN `make check-all` runs
- THEN it passes: there is no `pending-red` target left to fail on the now-compiling
  `pendingimpl` build, and I21 runs as an ordinary, untagged L2 test

### R2.9 — Retiring `pending-red` also drops it from the GitHub branch ruleset's required status checks

**MUST**: the `pending-red` status context is removed from `main`'s branch ruleset's required
status checks on GitHub, in the same change that retires the CI job (R2.8) — `pending-red` is a
required context per `openspec/changes/m1a-substrate/design.md` (verified: its §D8/risk table
states this explicitly); a required context that stops posting because its job was deleted
blocks every future merge to `main` indefinitely, not just this PR's.

**MUST NOT**: this half be treated as optional cleanup — it is the second half of one retirement,
the same way `docs-sync.yml` and `make check-all` are two separately-verified halves of one gate
elsewhere in this project.

**Verified by**: review of the GitHub ruleset configuration — this is a GitHub-side setting no
Makefile or workflow file can express or verify locally, the same category of gate
`m1a-substrate`'s own R8.3 already names for `docs-sync.yml`. Not mechanically verifiable by
`make check-all`.

### R2.10 — Doc 02 §5 and §13 gain their deltas; core purity and coverage hold

**MUST**: `docs/02-cognitive-core.md` §5 states the RRF mechanism (already largely present) is
unchanged by this PR beyond what R2.5 requires for `recall_top_k`; §13 gains the `recall_top_k`
row (R2.5). This PR does **not** resolve proposal §8 Q3b (the recall entrance) — doc 02 §5's
"answering a `recall`" sentence stays as-is; Q3b's answer and its doc delta belong to
`m1c-surface`.

**MUST**: every file under `internal/core/recall/**` imports only the standard library and its
own package (`depguard`'s core-purity rule) and calls none of `time.Now`, `time.Since`,
`time.Until`, `rand.*`, `uuid.*`, `os.Getenv` (`forbidigo`).

**MUST**: `internal/core/recall`'s statement coverage is ≥ 90% (`make cover`, part of
`make check-all` only).

**Verified by**: `golangci-lint run` (part of `make check`); `scripts/core-coverage.sh` (part of
`make check-all`); `docs-sync.yml` on GitHub.

---

## 3. `internal/store/sqlite` search additions (PR9 — `feat/store-search`)

Traced to `docs/03-data-model.md`'s "Search: embeddings + FTS5" section and I21's storage
boundary.

### R3.1 — A repository method writes an embedding: model, dimension, and an L2-normalized vector

**MUST**: `internal/store/sqlite` provides a way to persist one row into `unit_embeddings`
(`unit_id`, `model`, `dim`, `embedding` BLOB) per `docs/03-data-model.md`'s schema, given a unit
ID and an `ports.EmbeddingProvider`-shaped result (vector + model, per Phase A's
`ports.EmbedResponse`).

**MUST**: the stored vector is L2-normalized before the BLOB is written — `docs/03-data-model.md`:
"Vectors are L2-normalized before storage. Cosine similarity is then a plain dot product" — this
is what makes ADR-0012's brute-force dot product correct; normalization is a storage-boundary
obligation, not something callers are trusted to have already done.

**MUST**: `dim` is written as `len(vector)`, not as a value carried separately from the vector
that could drift from it — matching Phase A's own `ports.EmbedResponse` design note ("no `Dim`
field: the dimension is `len(Vector)`... a second source of truth with nothing keeping them
equal").

**Verified by**: L3 — a test writing an embedding and reading the raw row back via a direct SQL
query, asserting the vector's L2 norm is 1 (within floating-point tolerance) and `dim` matches
`len(vector)`.

### R3.2 — The vector index is loaded from SQLite at vault open, not paid per request

**MUST**: `internal/store/sqlite` provides a way to load every `unit_embeddings` row into an
in-memory `recall.VectorIndex`-shaped value when the vault opens — ADR-0012: "The index is
loaded from SQLite when the vault opens... it must not be paid per request."

**MUST NOT**: any code path in this PR reload the full index from SQLite once per recall call —
the load-once-at-open shape is the property ADR-0012's own measured cost (42 ms per 10,000
vectors) depends on being paid rarely, not per request.

**Verified by**: L3 — a test that opens a vault holding N pre-seeded embedding rows, loads the
index once, and asserts the loaded index's entry count and vector values match the rows; a
review-level check (this PR's own code shape) that no per-recall-call SQL read of
`unit_embeddings` exists — the load path and the query path are distinct.

### R3.3 — The FTS5 query leg runs `MATCH` against `units_fts` and filters positively on `status = 'pool'` (I02, storage half)

**MUST**: `internal/store/sqlite` provides a way to run an FTS5 `MATCH` query against
`units_fts`, joined back to `units`, returning ranked unit IDs.

**MUST**: this query's SQL text contains a positive predicate equivalent to `WHERE status =
'pool'` — never a negative exclusion — matching the precedent `m1a-substrate` R4.2 already
established for `LiveByIDs`. `units_fts` itself indexes every unit row regardless of status
(`docs/03-data-model.md`: "an archived unit stays indexed... this exclusion is applied
positively at query time, never at index time"), so the positive filter belongs to this query,
not to the trigger-maintained index.

**Verified by**: L3 — a test seeding a vault with one unit per status (all four) sharing matching
vocabulary, running the FTS5 query, and asserting only the `pool` unit's ID is returned.

**Scenario**:
- GIVEN a vault holding a `pool` unit and a `superseded` unit that both contain the word
  "descaling" in their content
- WHEN the FTS5 query leg searches for "descaling"
- THEN only the `pool` unit's ID appears in the result

### R3.4 — Both the embedding read/query boundary and the FTS5 leg respect I21 against a real two-model vault

**MUST**: whichever store-level method backs the vector leg's read (feeding R3.2's index load,
or an equivalent per-query read if design chooses not to cache) filters on `model` at the SQL
boundary — matching `idx_unit_embeddings_model`'s existence in migration 0002 and I21's own
storage-level obligation stated in ADR-0003's amendment ("searches filter on `model`").

**Verified by**: L3 — a vault seeded with `unit_embeddings` rows from two distinct `model`
values (the "reindex in progress" case), asserting a query naming one model never returns or
mixes in rows from the other — the two-model vault the umbrella proposal's own success criteria
(§2) name explicitly for I21.

### R3.5 — No migration is added; `store_api.golden` is regenerated

**MUST NOT**: this PR add a new migration file or modify `0001_core_tables.sql` or
`0002_learning_and_search.sql` — `unit_embeddings`, `units_fts`, and its three sync triggers
already exist (verified: migration 0002, read in full above); this PR is queries and writes
against existing tables, not schema.

**MUST**: `testdata/schema/store_api.golden` is regenerated (`make store-api-golden`) to include
this PR's new exported surface — the same obligation `m1a-substrate` R4.4 already established
for the `UnitRepo` implementation.

**Verified by**: `git diff` over `internal/store/sqlite/migrations/` is empty for this PR;
`TestHarness_StoreAPIUnchanged` against the regenerated golden.

### R3.6 — No test in this PR touches the network or a real LLM/embedding provider

**MUST NOT**: any L1/L2/L3 test added by this PR open a network connection or call a real
provider — every embedding value these tests write or read is a fixture-derived vector (Phase
A's `test/support/fakeprovider` already provides a deterministic embedding fake).

**Verified by**: review — no test in this PR imports an HTTP client against a non-loopback,
non-fixture endpoint.

---

## 4. `internal/brain`'s capture pipeline (PR10 — `feat/brain-capture`)

Traced to `docs/02-cognitive-core.md` §5 (the full numbered pipeline) and `docs/06-harness.md`
§2 (the clock is a port). This is the first PR in the `m1b-pipeline` change — and, per Phase A's
own tree, the first PR in the whole `m1-capture-recall` umbrella — to create anything under
`internal/brain/`.

### R4.1 — The clock is read exactly once per capture, and passed down

**MUST**: `internal/brain`'s capture orchestration calls `ports.Clock.Now()` exactly once per
capture operation, at the start, and passes the resulting `time.Time` value down into every core
call and every repository write this pipeline makes (`LastTouchedAt`, `CreatedAt`, `UpdatedAt`,
`decision_log.occurred_at`, etc.) — `docs/06-harness.md` §2's rule, made concrete for the first
time by real orchestration code.

**MUST NOT**: any core package this pipeline calls (`classify`, `recall`, `relation`) read a
clock itself — already guaranteed structurally by `depguard`'s `core-purity` rule denying
`internal/ports` to `internal/core/**`, restated here as the property this PR's orchestration
must uphold at the call-site level (`docs/06-harness.md` §9's R9 risk: "a review property, not a
gate", so this MUST is proven by code review of the call sites, not by a lint).

**Verified by**: L2 — a conformance-level test using a fake `Clock` that panics or fails the test
if `Now()` is called more than once during a single capture invocation; review of call sites for
a second, independent clock read.

### R4.2 — Capture calls the configured LLM provider, decodes with `core/classify`, and persists the resulting unit

**MUST**: `internal/brain`'s capture path calls `ports.LLMProvider.Complete` with the message
text and the `capture_processing` task (per `internal/config.DocumentedTaskNames`, already
listing `capture_processing`), decodes the raw response with `core/classify` (§1 above), and — for
every classification that is not one of R4.6/R4.7's excluded shapes below — persists a
`unit.Unit` via `ports.UnitRepo.Create`, with `Weight`/`WeightDecayRate` taken from classify's
output and `LastTouchedAt`/`CreatedAt`/`UpdatedAt` taken from R4.1's single clock read.

**MUST**: the persisted unit's `Status` is `unit.StatusPool` — every capture in this PR produces
a live unit; nothing in Phase B ever sets `unit.StatusIncomplete` (Q3a's closed decision, R4.6
below).

**Verified by**: L2 — a conformance test driving capture end-to-end against `memrepo` (Phase A's
in-memory fake) and `fakeprovider` (replaying a `testdata/llm/` recording), asserting the
persisted unit's fields match the classification.

### R4.3 — Capture embeds the persisted unit and writes the embedding

**MUST**: after persisting the unit (R4.2), capture calls `ports.EmbeddingProvider.Embed` on the
unit's content and writes the result via R3.1's store method, associating the embedding with the
unit's ID.

**MUST NOT**: an embedding be written for a unit this pipeline does not also persist (Phase B
creates no orphaned `unit_embeddings` row) — `unit_embeddings.unit_id` already carries `ON
DELETE CASCADE` in the schema, but nothing deletes units, so this is a write-ordering obligation
on the capture path itself, not something the schema enforces independently.

**Verified by**: L2 — a conformance test asserting a captured unit is embedded exactly once, with
a model string matching the fake embedding provider's configured model.

### R4.4 — Capture runs hybrid recall for dedup/relation candidates, using the one fusion mechanism (§2's RRF)

**MUST**: after embedding (R4.3), capture runs hybrid recall — the vector leg (R2.2/R2.3) and the
FTS5 leg (R3.3), fused by `core/recall`'s RRF function (R2.4) — to produce a ranked list of
candidate units the new unit might relate to. This reuses `core/recall`'s fusion, it does not
reimplement it — ADR-0010's "one mechanism, three consumers" (Phase B is the first consumer;
answering a standalone `/recall` and consolidation's `connect` phase are Phase C and M2
respectively, out of this PR's scope).

**MUST**: the candidate search excludes the just-persisted unit itself from its own candidate
list.

**Verified by**: L2 — a conformance test seeding the fake repo with existing units, capturing a
new one with overlapping content, and asserting the candidate list surfaces the existing units
via the same RRF-fused order R2.4's L1 test already pins the mechanism for.

### R4.5 — Every automatic decision with an effect writes a `decision_log` row, from `internal/brain` (I12)

**MUST**: `internal/brain` (never `internal/core`) writes a row to `decision_log` for each of the
following capture-time decisions, when that decision has an effect: classify producing a
persisted unit (R4.2), the timer/recurring_reminder refusal (R4.6), and the ambiguous-person-reference
capture (R4.7). Each row's `action` names the decision bucket (e.g. `capture.classify`, matching
`docs/03-data-model.md`'s own example comment), `rationale` is a human-readable sentence, and
`context` carries whatever structured detail the decision needs for audit — `docs/02-cognitive-core.md`
§11's glass box.

**MUST**: `internal/brain` uses a new port — `ports.DecisionLog` or equivalent, since no such
port exists after Phase A (verified: `internal/ports/` holds exactly `clock.go`, `unitrepo.go`,
`doc.go`, `provider.go` today) — to persist these rows. This spec does not mandate the port's
exact method signature; it mandates that a port exists, is used from `internal/brain`, and is
never imported by `internal/core/**`.

**MUST NOT**: `internal/core/**` write to `decision_log`, directly or through a port — `core`
imports no port at all, per the established dependency rule; every `decision_log` write in this
PR originates from `internal/brain`.

**Verified by**: L2 — a conformance test asserting a capture with an effect (a normal persisted
unit) leaves exactly one relevant `decision_log` row, and — for the refusal paths (R4.6/R4.7) —
that the row is written even though no `unit` (R4.6) or an unresolved-ambiguity `unit` (R4.7) is
what the effect actually was.

**Scenario**:
- GIVEN a capture that classifies as `task` and persists successfully
- WHEN the pipeline completes
- THEN exactly one `decision_log` row exists naming the capture decision, written by
  `internal/brain`, and no `internal/core` package has imported the port that wrote it

### R4.6 — `timer`/`recurring_reminder` classifications arm nothing and persist no `timers` row; the pipeline refuses in plain words (Q3a)

**MUST**: when classify's `type` is `timer` or `recurring_reminder`, capture does **not** create
a `timers` table row (arming) and does **not** create or update anything in the `triggers` table
— both are M3 (proposal §3.3's explicit non-goal list: "No triggers, no timers... I04, I15,
I16, I17 are M3").

**MUST**: capture writes a `decision_log` row recording the classification and the refusal
(R4.5).

**MUST**: the pipeline's response to the caller states in plain words that this capability is
not yet available — Q3a's own wording: "and tells the caller 'not yet' in plain words". This
spec does not mandate the exact response shape (that is Phase C's HTTP/CLI surface); it mandates
that `internal/brain`'s capture function returns a result distinguishable from an ordinary
successful capture, carrying that refusal.

**MUST NOT**: capture write a `units` row for a `timer` or `recurring_reminder` classification.

This was raised as an open item, on the correct grounds that the umbrella proposal does not decide
it with the explicitness it gives the ambiguous-person-reference case (R4.7). **It is closed here
by the document that governs**, not by the proposal: `docs/02-cognitive-core.md` §8 is titled
"Ephemeral timers — infrastructure, NOT memory" and states, in bold, **"A timer is NEVER a unit:
no weight, no decay, no graph, no belief derivation."** An unarmed timer is still, in substance, a
timer; nothing in Q3a's refusal converts it into memory.

Recorded rather than resolved silently, because the reasoning matters more than the verdict: the
question was not open, it was **unsearched**. Doc 02 governs behavior, and a spec that reaches for
the proposal when doc 02 already answers is reading the wrong document — the proposal describes
what a change intends, doc 02 describes what the brain *is*.

**Verified by**: L2 — a conformance test driving a `timer`-classified capture through
`fakeprovider`/`memrepo`, asserting no `timers`-table-shaped write occurs, exactly one
`decision_log` row is written, and the returned result is distinguishable from a successful
capture.

**Scenario**:
- GIVEN a message that classify decodes as `type: "recurring_reminder"`
- WHEN capture processes it
- THEN no trigger or timer is armed, a `decision_log` row records the classification and the
  refusal, and the caller receives a result that plainly states the capability is not available
  yet — never a silent success that promises a reminder nothing will fire

### R4.7 — An ambiguous person reference produces a `pool` unit and a `decision_log` entry, never `incomplete` (Q3a, I06 honestly out of scope)

**MUST**: when classify's `person_ref_status` is `"ambiguous"`, capture persists the unit as
`unit.StatusPool` — the same status R4.2 already requires for every Phase B capture — never
`unit.StatusIncomplete`. Doc 02 §1's `incomplete` status exists for exactly this case in the
full design, but its promotion/expiry mechanism (`expire_incomplete`, doc 02 §6.1) is M2; an
`incomplete` unit created now would be invisible to every live read surface (I02) and immortal
until M2 ships — Q3a's own reasoning, restated as a testable requirement here.

**MUST**: capture writes a `decision_log` row noting the reference was ambiguous and was not
resolved.

**MUST NOT**: this PR implement disambiguation, a check-in question, or any mechanism that
blocks capture pending a user's answer — doc 02 §5's "Product rule: asking is the EXCEPTION"
does not apply here in the M1-honest sense: Phase B has no surface to ask through yet, so it
does not ask; it captures with what it has and logs the gap.

**MUST**: this PR's own conformance suite names I06 as *out of scope*, not passing vacuously —
matching the umbrella proposal's own framing ("I06 is honestly out of scope rather than
vacuously green") — for example, by a comment or an explicit skip/absence rather than silence,
so a future reader does not mistake "no test fails" for "I06 holds".

**Verified by**: L2 — a conformance test driving an ambiguous-person-reference capture (using
R1.5's corpus case) through `fakeprovider`/`memrepo`, asserting the persisted unit's `Status` is
`pool`, and a `decision_log` row records the unresolved ambiguity.

**Scenario**:
- GIVEN a message classify resolves with `person_ref_status: "ambiguous"` and an otherwise normal
  `knowledge`-type classification
- WHEN capture processes it
- THEN a `pool` unit is persisted with the content as classified, a `decision_log` row records
  that the person reference was ambiguous and unresolved, and no `incomplete` unit is ever
  created by this pipeline

### R4.8 — This PR wires no HTTP route and no CLI subcommand

**MUST NOT**: this PR add any file under `internal/httpapi/**` beyond what M0 already
established, or add a subcommand to `cmd/nooma` — the capture, recall, and read-only routes are
`m1c-surface`'s PR13; the CLI demo is PR14. `internal/brain`'s capture function is called
directly by this PR's own tests, not through any external surface.

**Verified by**: `git diff --name-only` for this PR contains no `internal/httpapi/` or
`cmd/nooma/` path.

### R4.9 — Doc 02 §5's hooks item gains a note

**MUST**: `docs/02-cognitive-core.md` §5's numbered "hooks" item (the proposal's own shorthand
"§5.5" — verified: doc 02 has no literal §5.5 header, this is item 5 within the numbered list
inside §5 "Capture") gains a note stating that M1 classifies `timer`/`recurring_reminder`/
`person_ref_status: ambiguous` per its contract but arms nothing and creates no `incomplete`
unit until M3/M2 respectively — the umbrella proposal's own Q3a text: "Doc 02 §5.5 gains a note".

**MUST NOT**: this PR carry `no-spec-change` — it has a genuine behavioral delta (R4.6, R4.7) to
document, and it is the PR that actually implements the capture pipeline touching
`internal/core/**` most directly.

**Verified by**: `docs-sync.yml` on GitHub.

### R4.10 — `internal/brain`'s new code respects the dependency rule from the `core` side

**MUST**: every file under `internal/core/**` this PR might still touch (it should not need to —
R4.1–R4.9 are orchestration) continues to satisfy `depguard`/`forbidigo`. `internal/brain/**`
itself is not subject to the `core`-purity `depguard` rule (it is expected to import `ports`,
`store`, `providers`) — this requirement exists to state explicitly that this PR's orchestration
code living outside `core/` does not exempt it from `docs/06-harness.md` §2's single-clock-read
rule (R4.1), which is a review property specifically because no lint enforces it in `brain/`.

**Verified by**: `golangci-lint run`; code review of `internal/brain/` call sites for a second
clock read.

---

## 5. `internal/core/relation` and the relation judge (PR11 — `feat/relation-judge`)

Traced to `docs/02-cognitive-core.md` §4 ("Relations") and §5 (step 3, "dedup/relation judge").

### R5.1 — The threshold decision is a pure function: discard, persist-uncertain, or persist-asserted

**MUST**: `internal/core/relation` exposes a pure function taking a candidate relation's
`confidence` and the applicable `(min_confidence_to_persist, min_confidence_to_surface)` pair and
returning one of exactly three outcomes, per doc 02 §4's thresholds:

| Condition | Outcome |
|---|---|
| `confidence < min_confidence_to_persist` | discard |
| `min_confidence_to_persist ≤ confidence < min_confidence_to_surface` | persist, uncertain |
| `confidence ≥ min_confidence_to_surface` | persist, asserted |

**MUST NOT**: this function perform any I/O, read `relation_thresholds` itself, or read a clock
— it is data (confidence, thresholds) in, a decision out, matching proposal §4.1's own
decision-gate placement ("the whole of I08 and I09's storage half").

**Verified by**: L1 — a table test covering all three bands plus both boundary values
(`confidence == min_confidence_to_persist` and `confidence == min_confidence_to_surface`,
asserting which side of each boundary is inclusive, per doc 02 §4's own wording: "below this, it
is not even stored" for persist, "above this, it is asserted without asking" for surface — this
spec requires the test to pin the exact boundary behavior, not merely assert three bands exist).

**Scenario**:
- GIVEN `min_confidence_to_persist = 0.30`, `min_confidence_to_surface = 0.50`, and a candidate
  with `confidence = 0.30` exactly
- WHEN the threshold function evaluates it
- THEN it returns persist-uncertain, not discard — doc 02 §4's persist threshold is a lower
  bound the value at-or-above satisfies

### R5.2 — Q1's fallback: named constants in `core/relation`, pinned to migration 0002's SQL defaults by an L2 test

**MUST**: `internal/core/relation` declares two named constants for the default
`min_confidence_to_persist` (0.30) and `min_confidence_to_surface` (0.50) — Q1's closed decision
(option B): "named constants in `core/relation`... No migration, no precedence rule, no seed."

**MUST**: whichever layer resolves a relation type's actual thresholds (this spec does not
mandate whether that resolution logic lives in `core/relation` or `internal/brain` — a
config/lookup concern design decides) uses these constants when `relation_thresholds` holds no
row for the candidate's `type` — relation `type` is open text (doc 02 §4: `same_topic`,
`derived_from`, "…"), so no seed can ever be exhaustive; this is the reason Q1 rejected seeding.

**MUST**: an L2 test reads `internal/store/sqlite/migrations/*.sql` text directly off disk (the
same pattern `test/conformance/i13_learning_signal_test.go` already establishes:
`migrationSQLText`/`extractTableBody`-shaped helpers, verified present) and asserts the two Go
constants equal `relation_thresholds`'s column `DEFAULT` clauses (`0.3` and `0.5` — verified
present in migration 0002, read above) — Q1's own stated closure of its one real objection ("two
sources for one default... closed by the L2 test, not argued away").

**Verified by**: L1 — the constants' values; L2 — a new `test/conformance/` test, named per
`docs/06-harness.md` §4's convention (e.g. naming the relation-threshold fallback invariant it
pins), reading the migration SQL text and comparing it against the Go constants.

**Scenario**:
- GIVEN migration 0002's `relation_thresholds` table declares
  `min_confidence_to_persist REAL NOT NULL DEFAULT 0.3` and `min_confidence_to_surface REAL NOT
  NULL DEFAULT 0.5`
- WHEN the L2 test compares these literal values against `core/relation`'s Go constants
- THEN they are equal; a future PR that edits one without the other fails this test

### R5.3 — The judge persists via upsert, never violating `(from, to, type)` uniqueness (I07)

**MUST**: `internal/brain`'s relation-judge orchestration, when the threshold decision (R5.1) is
persist-uncertain or persist-asserted, writes the relation such that a second judge run over the
same `(from_unit_id, to_unit_id, type)` triple updates the existing row rather than violating
`relations`'s `UNIQUE (from_unit_id, to_unit_id, type)` constraint (verified present in migration
0001) or producing a duplicate row — I07.

**MUST**: `internal/brain` uses a new port — `ports.RelationRepo` or equivalent, since no such
port exists after Phase A — to persist relations. This spec does not mandate the port's exact
method signature, only that an upsert-shaped write satisfying I07 is reachable through it.

**Verified by**: L2 — a conformance test running the judge twice over the same candidate pair and
type, asserting exactly one `relations` row exists afterward (via the port's own read, or the
fake repo's internal state) with the second run's `strength`/`confidence` reflected, not a
uniqueness-constraint error and not two rows.

### R5.4 — Discard, persist-uncertain, and persist-asserted each write a `decision_log` row (I08, I12)

**MUST**: for every candidate relation the judge evaluates, `internal/brain` writes a
`decision_log` row recording the outcome (discard/persist-uncertain/persist-asserted) and its
rationale — I08's own wording ("discard, log the discard") generalized to all three outcomes per
I12's blanket rule ("every automatic decision with an effect").

**MUST NOT**: a discarded candidate leave any `relations` row — I08: "below this, it is not even
stored."

**Verified by**: L2 — a conformance test with a candidate below `min_confidence_to_persist`,
asserting no `relations` row is created and exactly one `decision_log` row records the discard.

**Scenario**:
- GIVEN a candidate relation with `confidence = 0.10` and `min_confidence_to_persist = 0.30`
- WHEN the judge evaluates it
- THEN no `relations` row is created, and a `decision_log` row records the discard and its
  rationale

### R5.5 — The judge's LLM half is a provider call from `internal/brain`; its decision half is `core/relation`

**MUST**: `internal/brain`'s relation-judge orchestration calls `ports.LLMProvider.Complete` with
the `relation_evaluation` task (already documented in `internal/config.DocumentedTaskNames`) to
obtain a confidence value for a candidate pair, then calls `core/relation`'s pure threshold
function (R5.1) with that confidence — proposal §4.1's decision-gate table: "the judge's LLM half
is a provider call... its decision half is `core/relation`."

**MUST NOT**: `core/relation` call a provider or read `relation_thresholds` itself — the pure
function's only inputs are the confidence and the resolved threshold pair, per R5.1.

**Verified by**: L2 — a conformance test driving the full judge path through `fakeprovider`
(a recorded confidence-bearing response) and `memrepo`, asserting the persisted outcome matches
the recorded confidence's band.

### R5.6 — Doc 02 §4 gains Q1's one sentence

**MUST**: `docs/02-cognitive-core.md` §4 gains one sentence naming the fallback R5.2 implements —
Q1's closed decision: "Doc 02 §4 gains one sentence naming the fallback."

**MUST NOT**: this PR carry `no-spec-change`.

**Verified by**: `docs-sync.yml` on GitHub.

### R5.7 — Core purity and coverage hold for `core/relation`

**MUST**: every file under `internal/core/relation/**` imports only the standard library and its
own package, and calls none of the forbidden clock/random/env functions.

**MUST**: `internal/core/relation`'s statement coverage is ≥ 90%.

**Verified by**: `golangci-lint run`; `scripts/core-coverage.sh`.

---

## 6. Cross-cutting constraints

### R6.1 — No test in this change touches the network or a real LLM/embedding provider

**MUST NOT**: any L1, L2, or L3 test added by PRs 7–11 open a network connection or call a real
provider — CLAUDE.md non-negotiable #5; `docs/06-harness.md` §3.

**Verified by**: review — every provider-facing test in this change goes through
`test/support/fakeprovider`.

### R6.2 — Every PR touching `internal/core/**` gives doc 02 a real delta, and carries no `no-spec-change`

**MUST**: PR7 (`core/classify`), PR8 (`core/recall`), PR11 (`core/relation`) each touch
`docs/02-cognitive-core.md` in the same PR, per R1.7, R2.10, R5.6 above — proposal §4.8: "No M1
core PR should need [`no-spec-change`]."

**MUST NOT**: PR9 (`store/sqlite`) or PR10 (`brain/`) be *required* to touch `docs/02-cognitive-core.md`
merely because they exist in this chain — `docs-sync.yml` fires only on PRs touching
`internal/core/**`; PR9 and PR10 touch `internal/store/sqlite/**` and `internal/brain/**`
respectively. PR10 does, in fact, carry a doc 02 delta (R4.9), because its own behavioral scope
(Q3a's timer/ambiguous-reference handling) documents a fact about the brain, not merely an
adapter detail — stated here so the "MUST NOT be required" clause is not read as "MUST NOT
touch it at all".

**Verified by**: `docs-sync.yml` per PR, on GitHub.

### R6.3 — `internal/core/unit` and Phase A's delivered surface are not modified by this change

**MUST NOT**: any PR in this change modify `internal/core/unit/**`, `internal/ports/unitrepo.go`,
or `internal/store/sqlite/unitrepo.go` beyond what R4.5's `DecisionLog` port and R5.3's
`RelationRepo` port addition require in `internal/ports/` (new files, not edits to
`unitrepo.go`/`provider.go`) — Phase A's own surface is closed; this change extends
`internal/ports/` and `internal/store/sqlite/` with new files, it does not edit Phase A's.

**Verified by**: `git diff --name-only` per PR; `store_api.golden`'s diff (R3.5) shows only
additions, never a modified line inside Phase A's existing exported surface.

---

## 7. Test levels

### R7.1 — Level assignment for this change

**MUST**: classify's tolerant decoder, the RRF fusion function, the vector top-K selection, and
the relation threshold decision are **L1** — pure functions, no database, no network, no clock.

**MUST**: I14, I21 (behavioral half), I06 (explicitly out-of-scope), I08, I12 (`decision_log`
writes), and Q1's constant-pinning test are **L2** (`test/conformance/`, untagged) — invariant-
or pipeline-level, exercised against `memrepo`/`fakeprovider`, per `docs/06-harness.md` §3's L2
definition and its own naming convention (`TestI14_*`, etc. where a promoted or newly-written
conformance test names its invariant).

**MUST**: the embedding write, vector-index load, and FTS5 query leg (R3.1–R3.4) are **L3**
(`integration` tag) — they require a real migrated SQLite vault.

**MUST**: no L4 test is added by this change — Phase B exposes no new CLI subcommand and no HTTP
route (R4.8); a compiled-binary test has nothing new to drive until `m1c-surface`.

**Verified by**: file placement and build tags, per `docs/06-harness.md` §3.

### R7.2 — Every new test is observed failing for the right reason first

**MUST**: each requirement's test in this spec is written before its implementation and observed
failing with the expected message or compiler error, per non-negotiable #4 and strict TDD mode —
explicitly including R2.8's I21 promotion (whose "observed failing" state is
`scripts/pending-red.sh` reporting `recall.VectorQuery`/`recall.VectorIndex` as `undefined:`,
until this same PR retires the gate), and every L1/L2 test in §1–§5, written against the
not-yet-existing types/functions first.

**MUST NOT**: a failing test be weakened to pass. The two legitimate exits are fixing the code or
changing the governing document (doc 02, plus its ADR if affected) in the same PR.

**Verified by**: the commit sequence within each PR — a work-unit commit contains the test and
the code that satisfies it together.

---

## 8. Boundaries this change must not cross

### R8.1 — No PR in this change implements corrections, or resolves Q3b/Q3c

**MUST NOT**: any PR in this change create a correction-referent-resolution mechanism, an
explicit `unit_id`-based correction path, or an in-place content edit driven by a `type:
"correction"` classification — that is `m1c-surface`'s `feat/corrections`, contingent on Q3c
(open), per §0's scope narrowing above.

**MUST NOT**: any PR in this change decide whether `/recall` is standalone or routes through
classify (Q3b) — that is an HTTP-surface decision belonging to `m1c-surface`'s PR13; nothing in
PRs 7–11 requires that answer (verified against the proposal's own dependency graph: PR8 depends
only on PR2, PR9 on (4,8), PR10 on (6,7,9), PR11 on (8,10) — none names PR12/13 as a
dependency).

**MUST NOT**: this change write to `learning_signals` — that table's first write is corrections'
`correction` signal (I13), deferred alongside corrections per §0.

**Verified by**: `git diff --name-only` over the full chain contains no
`internal/core/correction*` or equivalent path; no test in this change asserts a
`learning_signals` row was written; no `ports.SignalRepo`-shaped port is introduced by this
change (its only consumer, corrections, is out of scope).

### R8.2 — No PR implements anything from the umbrella proposal's explicit non-goals

**MUST NOT**: any PR in this change compute or persist `effective_weight`, priority, focus,
hysteresis (I05, I19 — M2); implement any of the eight consolidation phases (M2); arm, evaluate,
or fire a trigger or timer (I04, I15–I17 — M3, beyond R4.6's explicit non-arming requirement);
derive a self-belief (M2); consume a `learning_signal` (M5); add a Telegram channel; implement
`nooma reindex` as a command (M6, though I21's *behavior* against a two-model vault is in scope
per R3.4); or touch perception/`measurements` (v2, ADR-0005).

**Verified by**: `git diff --name-only` over the full chain contains no path under
`internal/core/consolidation/**` (beyond its existing `doc.go`), `internal/core/prospection/**`,
`internal/core/selfmodel/**`, `internal/core/learning/**`, `internal/channels/telegram/**`, or a
`nooma reindex` subcommand.

---

## 9. Open items this spec deliberately leaves to design or to `m1c-surface`

- **The exact port method signatures for `ports.DecisionLog` and `ports.RelationRepo`** (R4.5,
  R5.3) — design's choice, following the structural precedent `ports.UnitRepo` already set in
  Phase A (no `Delete*`-prefixed method, per CLAUDE.md non-negotiable #6, is a reasonable
  default for `DecisionLog`; `RelationRepo` is not bound by the same prohibition, since I10's
  relation-rejection delete is a real, doc-02-sanctioned operation, just one this change does not
  reach).
- **Where threshold resolution (relation type → its actual `(persist, surface)` pair, falling
  back to Q1's constants when absent) lives** — `core/relation` vs. a lookup `internal/brain`
  performs before calling the pure decision function (R5.2) — design's choice; either satisfies
  this spec as long as `core/relation`'s own function stays pure per R5.1.
- ~~Whether a `timer`/`recurring_reminder` classification persists an ordinary `pool` unit~~ —
  **closed, and it was never open.** R4.6 now carries a MUST NOT, decided by `docs/02-cognitive-core.md`
  §8's bold sentence: *"A timer is NEVER a unit: no weight, no decay, no graph, no belief
  derivation."* This list originally deferred it on the grounds that the umbrella *proposal* did
  not say so explicitly — which was the wrong document to consult. **Doc 02 governs behavior; a
  proposal describes what one change intends.** When both speak to the same question, reaching for
  the proposal is a reading-order error, not a genuine ambiguity.
- **RRF's tie-breaking rule** when two candidates score identically after fusion (R2.4) —
  ADR-0010's formula does not specify one; design's choice, pinned by whatever L1 test the
  implementation needs.
- **Corrections (I03's correction half, I13), and Q3b/Q3c themselves** — explicitly deferred to
  `m1c-surface` per §0 and R8.1, not silently dropped.
