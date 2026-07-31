# Spec — M1a: the substrate

Delta specification for the `m1a-substrate` change, the first of three chained SDD changes
that split `openspec/changes/m1-capture-recall/proposal.md` (owner decision, 2026-07-30,
proposal §8 Q5). This document states what MUST be true of the repository after this change is
applied, in testable form. It does not prescribe how (that is `design.md`).

Sources: `openspec/changes/m1-capture-recall/proposal.md` (§3.2 items 1–2 and 6, §4.1, §4.2,
§4.7, §5 Phase A table), `docs/02-cognitive-core.md` §1, §4, §5.1 (taxonomy only), `docs/03-data-model.md`,
`docs/06-harness.md` §1, §3, §4, §6, `CLAUDE.md`, `.golangci.yml`,
`test/conformance/pending_symbols.txt`, `test/conformance/tree_scan_test.go`,
`test/conformance/i01_focus_never_persisted_test.go`, `test/conformance/i03_units_never_deleted_test.go`,
`test/conformance/golden_sets_test.go`, `testdata/llm/format.md`, `internal/config/validate.go`,
`testdata/schema/store_api.golden`, `scripts/pending-red.sh`.

## Scope boundary (binding, from the umbrella proposal §3.1 and §5's Phase A table)

> Nothing in Phase A writes a unit or is visible to a user.

Phase A is the umbrella proposal's own six-PR table (§5): `docs/m1-preflight`,
`feat/core-unit`, `feat/ports-unitrepo`, `feat/store-unitrepo`, `feat/provider-fake`,
`feat/providers-http`. Every requirement below is bounded by that table — Phase A does not
implement `core/classify`, `core/recall`, `core/relation`, the capture pipeline, or any HTTP
route. Those are Phase B (`m1b-pipeline`) and Phase C (`m1c-surface`), specified separately.

**Verification note on the proposal's own doc-correction claim**: proposal §3.2 item 1 states
"`docs/README.md` and `CLAUDE.md` still say 'no code yet'". Read directly, this is no longer
true: `CLAUDE.md` (root) already reads "M0 closed (2026-07-30)…" and `docs/README.md`'s Status
section already reads "M0 closed on 2026-07-30…". Both were corrected by the M0 chain before
this spec was written. §1's requirements below scope the doc-preflight PR to what is still
actually wrong — doc 01's `openai` gap and `tasks.embedding`'s placeholder — and do not require
touching `docs/README.md` or `CLAUDE.md` for this reason, since there is nothing left to fix
there.

---

## 1. Documentation preflight (PR1 — `docs/m1-preflight`)

### R1.1 — `docs/01-architecture.md` documents `openai` as a provider type

**MUST**: `docs/01-architecture.md` names `openai` alongside `anthropic`, `ollama` and
`whisper_cpp` as a documented `providers.<name>.type` value, consistent with
`docs/05-build-plan.md`'s own claim ("`LLMProvider` / `EmbeddingProvider` interfaces +
implementations (anthropic, openai, ollama)").

**MUST**: `internal/config.DocumentedProviderTypes` (`internal/config/validate.go`) is extended
to include `"openai"` in the same PR as R6.4 below (Phase A's PR6), not in this PR — doc 01
gains the type name here; the Go list widens when the client exists, per the proposal's own
sequencing (§4.2 point 1, §5's Phase A table row 6). This requirement covers only the doc side.

**Verified by**: reading `docs/01-architecture.md`'s provider-type list; the phrase `openai`
appears where `anthropic`, `ollama` and `whisper_cpp` are named.

**Scenario**:
- GIVEN a reader comparing `docs/01-architecture.md`'s documented provider types against
  `docs/05-build-plan.md`'s stated M1 scope
- WHEN they check whether `openai` is named on both sides
- THEN they find it on both, not just the build plan

### R1.2 — `tasks.embedding` gains a real provider in doc 01's example

**MUST**: `docs/01-architecture.md`'s `nooma.yml` example replaces `embedding: { provider: ... }`
(the literal ellipsis, verified present at line 199) with a real provider name drawn from the
same example's `providers:` block, so the key stops being a documentation placeholder.

**MUST NOT**: this requirement be satisfied by adding semantics to `checkTasks` — proposal
§4.1's decision-gate table places "route `tasks.capture_processing` to a provider instance" in
`cmd/nooma` (wiring), and R6.5 below is the config-side check; this requirement is documentation
only.

**Verified by**: reading the example; `provider: ...` no longer appears anywhere in
`docs/01-architecture.md`'s `nooma.yml` block.

### R1.3 — `docs/06-harness.md` §1's tree gains a `core/classify` line

**MUST**: `docs/06-harness.md` §1's `internal/core/` tree diagram gains a `classify/` entry,
alongside the existing `unit/`, `weight/`, `focus/`, `recall/`, `relation/`, `consolidation/`,
`prospection/`, `selfmodel/`, `learning/` lines — verified absent today by reading the tree.

**MUST NOT**: this PR create `internal/core/classify/` itself, or any file under it. Proposal
§4.1 places `core/classify` outside `core/unit` deliberately (to keep I01's tree scan narrow);
Phase A documents the future location, Phase B (`feat/core-classify`) creates the package.

**Verified by**: reading `docs/06-harness.md` §1; `test/conformance` `TestI01_*` and
`TestI03_*` continue to pass unaffected (this PR touches no `internal/core/**` file, so
`docs-sync.yml` does not fire and no `no-spec-change` label is needed).

### R1.4 — No other M1a-relevant doc claim is left contradicted

**MUST**: this PR does not need to touch `docs/README.md` or `CLAUDE.md` — see the
verification note above the boundary. If a reviewer of a later PR in this chain finds either
file re-drifted (for example, a stale "still empty" claim about `internal/core/`), that PR fixes
it in the same PR, per non-negotiable #1; it is not a Phase A requirement to pre-emptively touch
either file when they are already accurate.

**Verified by**: reading both files at the time each PR in the chain is opened.

---

## 2. `internal/core/unit` (PR2 — `feat/core-unit`)

Traced to `docs/02-cognitive-core.md` §1 ("The unit — the atom").

### R2.1 — `unit.Status` is a string-kind vocabulary type with exactly four members

**MUST**: `unit.Status` is defined with underlying kind `string` (`reflect.String`), and
`unit.AllStatuses()` returns exactly `pool`, `archived`, `superseded`, `incomplete` — the four
values doc 02 §1 names, no more and no fewer.

**MUST NOT**: `unit.AllStatuses()` include `"focus"` as a member, and no line anywhere under
`internal/` or `cmd/` may pair the literal `"focus"` with the substring `Status` (I01).

**Verified by**: L1 — `internal/core/unit/status_test.go` (new) asserts the four-member set and
kind; the existing `test/conformance/i01_focus_never_persisted_test.go`, promoted into the
untagged L2 suite by this PR (see §6 below), continues to assert the tree-scan half of I01.

**Scenario**:
- GIVEN `unit.AllStatuses()` called with no arguments
- WHEN its result is compared against `{pool, archived, superseded, incomplete}` as a set
- THEN the two sets are equal, and `"focus"` is not a member of either

### R2.2 — The live predicate filters positively, matching doc 02 §1's "hard rule"

**MUST**: `internal/core/unit` exposes a pure function (a predicate over `unit.Status`, e.g.
`IsLive(unit.Status) bool`) that returns `true` for exactly `pool` and `false` for `archived`,
`superseded` and `incomplete`.

**MUST**: the predicate is defined as a *positive* test (`status == pool`), not as a negation
list (`status != superseded && status != incomplete`) — doc 02 §1 states the rule this way
specifically because a negative list silently admits a fifth status added later; a positive
filter excludes it for free.

**Verified by**: L1 — a table test over all four `unit.AllStatuses()` members plus a
deliberately unknown string value, asserting the predicate returns `false` for the unknown value
too (a positive filter has no "unless-listed" branch to fall into by accident).

**Scenario**:
- GIVEN a hypothetical fifth status value never added to `unit.AllStatuses()`
- WHEN `IsLive` is called with it
- THEN it returns `false`, because the predicate never special-cases the excluded statuses by
  name

### R2.3 — Legal status transitions are the four doc 02 names, no others

**MUST**: `internal/core/unit` exposes a pure function deciding whether a `(from, to)` pair of
`unit.Status` values is a legal transition, and it returns `true` for exactly these pairs,
traced to doc 02:

| From | To | Doc 02 basis |
|---|---|---|
| `incomplete` | `pool` | §1 ("promoted … after 24 h"), §6.1 (`expire_incomplete`: "promoted with what they have") |
| `pool` | `archived` | §1 ("archived (cold, weight ≈ 0)"), §2 (thermal zones), §6.2 (`archive` phase) |
| `archived` | `pool` | §2 ("cold→warm/hot by a strong resurface") |
| `pool` | `superseded` | §1 ("superseded (replaced insight)"), §12 (insight supersession) |
| `incomplete` | `archived` | §1 ("expired after 24 h") + I03. See the correction below |

**MUST NOT**: the function return `true` for any pair not in this table — in particular,
`incomplete → superseded`, `superseded → pool` (or any other outbound edge from `superseded`),
and any pair involving `"focus"` are all illegal.

> **Corrected against design D3 (2026-07-31), and the correction is recorded rather than
> edited in silently.**
>
> This section originally listed four pairs and named `incomplete → archived` as *illegal*.
> Design D3 reached the opposite conclusion, and its reasoning is the one that holds: doc 02
> says an unresolved `incomplete` unit is **expired after 24 h**, the vocabulary has no
> `expired` member, and **I03 forbids deletion** — so `archived` is the only status an expired
> unit can land in. Inventing a fifth status would mean a new migration, a doc 03 edit and a
> regenerated golden, for a transition M2 performs and M1 never reaches.
>
> Why the two artifacts disagreed is worth keeping. This section was **derived**, not read —
> it says so above: doc 02 never enumerates a transition matrix, so the four pairs came from
> cross-referencing §1, §2, §6 and §12. Design D3 found the fifth by asking a question this
> derivation never asked: *where does a unit go when it expires?* An inference is only as
> complete as the questions put to it.
>
> **This makes doc 02's silence load-bearing.** The governing document does not name the
> status an expired `incomplete` unit lands in. PR 2 writes that sentence into doc 02 §1 — an
> owner-visible change to the document that governs behavior, not a planning-artifact edit.

**MUST**: `(s, s)` — a status transitioning to itself — is explicitly decided (either legal or
not) rather than left to fall through as an untested default; this spec does not mandate which
answer, only that the function have one and that an L1 test pin it.

**Verified by**: L1 — a table test over all 16 ordered pairs from the four-member vocabulary,
asserting the exact five-pair legal set above and rejecting every other pair, including both
diagonal and reverse-of-listed pairs (`pool → incomplete`, `superseded → pool`, etc.).

**Scenario**:
- GIVEN a proposed transition `superseded → pool`
- WHEN the transition function evaluates it
- THEN it returns `false` — doc 02 describes no path back from `superseded`

**Note on scope**: this function is pure data/logic. Phase A ships no caller that invokes an
`archived`, `superseded`, or `pool→archived`/`archived→pool` transition in production — those
callers are consolidation (M2, per the umbrella proposal §3.3's explicit non-goals) and the
insight-supersession path (also out of Phase A). Phase A pins the function's correctness by L1
test alone; nothing in Phase A calls it end-to-end against a real vault.

### R2.4 — The type taxonomy is a closed, plain-text vocabulary matching doc 02 §1

**MUST**: `internal/core/unit` declares the nine type values doc 02 §1 lists: `task`,
`mental_load`, `event`, `knowledge`, `procedural`, `emotional`, `list`, `structured_ref`,
`insight`.

**MUST NOT**: this PR add `timer` or `recurring_reminder` to `unit.Type` (or an equivalent) —
those are classify *outcomes* (doc 02 §5.1's taxonomy of the classify response), not unit types;
doc 02 §1's type list and doc 02 §5.1's classification taxonomy are two different vocabularies,
verified by reading both — conflating them here would be inventing scope Phase B's
`core/classify` (not Phase A) is responsible for.

**Verified by**: L1 — a test asserting the nine-member set, no more, no fewer.

---

## 3. `ports.UnitRepo` and its in-memory fake (PR3 — `feat/ports-unitrepo`)

Traced to `docs/02-cognitive-core.md` §1 ("Nothing is deleted. Archiving is a state
transition, not a removal") and `CLAUDE.md` non-negotiable #6.

### R3.1 — `ports.UnitRepo` declares no method whose name starts with `Delete`

**MUST**: `ports.UnitRepo` is a non-empty interface (at minimum a way to persist a unit and a
way to read one back), and no exported method on it has a name beginning with `Delete`.

**MUST NOT**: any method on `ports.UnitRepo` accept a request whose only observable effect is
the removal of a row — the interface's shape is what makes "nothing deletes a unit" a
compile-time property of the port (per `test/conformance/i03_units_never_deleted_test.go`'s own
reflection check), not a discipline every call site has to remember.

**Verified by**: L2 — `test/conformance/i03_units_never_deleted_test.go`, promoted into the
untagged suite by this PR (see §6 below), reflects over `ports.UnitRepo` and fails if any method
name has the `Delete` prefix.

**Scenario**:
- GIVEN `ports.UnitRepo`'s method set, once this PR lands
- WHEN `reflect.TypeOf((*ports.UnitRepo)(nil)).Elem()` is enumerated
- THEN no method name begins with `Delete`

### R3.2 — A repo read exposes a status filter capable of the positive `pool` query

**MUST**: `ports.UnitRepo` includes at least one read method whose signature lets a caller
request only `status = 'pool'` rows (matching R2.2's positive predicate) — a "list live units"
or equivalent shape, not only a by-id lookup. This is the port-level contract R4.2 (below) tests
against a real store.

**MUST NOT**: the read method's contract be satisfiable by a caller filtering client-side after
fetching every status — the port itself is what carries the positive-filter obligation forward
to every future implementation (I02's storage half, in scope per the umbrella proposal §3.4).

**Verified by**: L1 — the in-memory fake's own test exercises the status-filtered read against
a fixture containing units of all four statuses, asserting only `pool` rows come back.

### R3.3 — An in-memory fake implements `ports.UnitRepo` for L2 use

**MUST**: `internal/ports` or a test-support package provides an in-memory implementation of
`ports.UnitRepo`, constructible with no external dependency (no SQLite, no file I/O), for use by
L2 conformance tests and by future `brain/` unit tests.

**MUST**: writing a unit through the fake and reading it back by id returns a value equal to
what was written (round-trip fidelity) — including on a second write to the same id (an
update-in-place, since the interface has no delete).

**MUST NOT**: the fake's internal representation make a previously-written unit unreachable by
id after any operation exposed by `ports.UnitRepo` — the fake's obligation as a test double is
to make "nothing is deleted" observably true through the interface, the same guarantee R3.1's
interface shape makes structurally true for every implementation.

**MUST**: two instances of the fake, constructed independently, share no mutable state — a
write through one MUST NOT be observable through the other. This is what makes the fake safe for
parallel L1/L2 tests (`-race`, per `docs/06-harness.md` §3).

**Verified by**: L1 — round-trip test (write, read, assert equality); update-in-place test
(write twice with the same id, assert the second write's content is what reads back and the
first write's content is not silently retained as a separate row); isolation test (two fakes,
write through one, assert the other's read misses it); `-race` run.

**Scenario**:
- GIVEN a fresh in-memory fake and a unit written to it
- WHEN the same unit id is written again with different content
- THEN a subsequent read by that id returns the second content, not the first, and the fake
  exposes no way to retrieve the first content afterward

---

## 4. `internal/store/sqlite`'s `UnitRepo` implementation (PR4 — `feat/store-unitrepo`)

### R4.1 — The SQLite implementation satisfies `ports.UnitRepo`

**MUST**: `internal/store/sqlite` provides a type implementing `ports.UnitRepo` against a real
migrated vault (migrations `0001`/`0002`, already applied — this PR adds no migration, see R4.4).

**Verified by**: L3 — `test/integration/` (or `internal/store/sqlite/*_integration_test.go`,
matching this package's existing convention) exercises the implementation against a real
temporary SQLite vault, per `docs/06-harness.md` §3's L3 definition.

### R4.2 — The live-read method filters positively on `status = 'pool'`, at the SQL boundary

**MUST**: the SQL text backing R3.2's status-filtered read method contains a positive predicate
equivalent to `WHERE status = 'pool'` (or an equivalent parameterized/positive form) — not a
negative exclusion (`WHERE status NOT IN ('superseded', 'incomplete')` or similar).

**Verified by**: L3 — a test that seeds a vault with one unit per status (all four) directly via
SQL, calls the live-read method, and asserts exactly the `pool` unit comes back — the same
guarantee R2.2's L1 test pins at the pure-function level, now proven against a real table.

**Scenario**:
- GIVEN a vault holding one unit each with `status = 'pool'`, `'archived'`, `'superseded'`, and
  `'incomplete'`
- WHEN the live-read method is called
- THEN exactly the `pool` unit is returned

### R4.3 — No code path in this implementation issues `DELETE FROM units`

**MUST**: no method on the SQLite `UnitRepo` implementation executes `DELETE FROM units`, in any
form (literal, parameterized, or built by string concatenation).

**Verified by**: L2 — `test/conformance/i03_units_never_deleted_test.go`'s tree-scan half
(already promoted by PR3, see §6) continues to pass once this PR's `.go` files exist under
`internal/store/sqlite/`, since the scan covers `internal/` tree-wide; plus L3 — an update-path
test asserting the underlying row count in `units` is unchanged after an update-style call
through the repo.

### R4.4 — This PR adds no migration and widens `store_api.golden` deliberately

**MUST NOT**: this PR add a new migration file or modify `0001_core_tables.sql` or
`0002_learning_and_search.sql` — the `units` table already exists (per `docs/03-data-model.md`);
Phase A's SQL is queries against an existing table, not schema.

**MUST**: `testdata/schema/store_api.golden` is regenerated in this PR (`make store-api-golden`)
to include the new exported surface this implementation adds — matching the precedent set by
R8.5 of the `m0-skeleton` spec (the lock's own golden widening), applied here to the repo type.

**Verified by**: `TestHarness_StoreAPIUnchanged` (or its successor) against the regenerated
golden; `git diff` over `internal/store/sqlite/migrations/` is empty for this PR.

---

## 5. The provider port surface and its fixture-replaying fake (PR5 — `feat/provider-fake`)

Traced to `docs/06-harness.md` §5 ("`testdata/llm/` — recorded responses") and CLAUDE.md
non-negotiable #5.

### R5.1 — `ports.LLMProvider` and `ports.EmbeddingProvider` are declared

**MUST**: `internal/ports` declares two interfaces, `LLMProvider` and `EmbeddingProvider`,
covering at minimum: a call taking a task identifier and a prompt and returning a raw response
(or a recorded provider-level error, matching `testdata/llm/format.md`'s `response`/`error`
cross-field constraint), and a call taking text and returning an embedding vector.

**MUST NOT**: either interface's method signatures name a specific vendor (Anthropic, OpenAI,
Ollama) — the port is vendor-neutral; PR6 (§6 below) is where vendor-specific adapters exist.

**Verified by**: L1 — a compile-time assertion (a fake or stub implementing both interfaces)
plus a signature test where feasible.

### R5.2 — The fixture-replaying fake never touches the network, and keys on case `id`

**MUST**: a fake implementation of `ports.LLMProvider` (and, if `EmbeddingProvider` needs one,
of that interface too) replays recordings loaded from `testdata/llm/cases/` via
`test/support/goldenset`, selected by the test-supplied case `id` — not by matching the live
`prompt` text against the recording's `prompt` field.

**MUST NOT**: the fake, or any test exercising it, open a real network connection or call a real
LLM (CLAUDE.md non-negotiable #5; `docs/06-harness.md` §3: "No level calls an LLM or an external
API. Ever.").

**MUST**: when a loaded case has its `error` field set (not `response`), the fake surfaces that
as an error from the provider call, not as a successful response containing the error text.

**Verified by**: L1 — a test selecting a fixture by id and asserting the fake returns exactly
its recorded `response` (or surfaces its recorded `error`), with no network dependency in the
test's own setup (no `httptest.Server`, no real client).

**Scenario**:
- GIVEN a `testdata/llm/cases/` recording with `id: "case-x"` and `response` set
- AND a live prompt text that differs from the recording's `prompt` field (fixture drift, or a
  different clock reading)
- WHEN the fake is asked to replay `"case-x"` by id
- THEN it returns the recorded `response`, unaffected by the prompt-text mismatch — proposal
  §4.2's stated reason this corpus cannot be keyed on `prompt`

### R5.3 — `testdata/llm/cases/` gains its first real, valid case(s)

**MUST**: at least one file exists under `testdata/llm/cases/` (beyond `.gitkeep`), decodable by
`goldenset.LLMExample` under `DecodeStrict` (per `testdata/llm/format.md`'s documented shape and
cross-field constraint).

**MUST NOT**: this PR be required to cover doc 02 §5.1's full classify taxonomy, or all three of
I14's malformed-field shapes (truncated JSON, wrong type, unknown enum) — that corpus-completion
work belongs to Phase B's `feat/core-classify` (`m1b-pipeline`), per the umbrella proposal §5's
Phase A/B split. Phase A's obligation is a non-empty, schema-valid corpus sufficient to prove the
fake (R5.2) and to trigger R5.4's inversion below — not taxonomy completeness.

**Verified by**: `test/support/goldenset.Load` (or equivalent) successfully loads every file
under `testdata/llm/cases/`; `TestHarness_GoldenSetFormatMatchesType` (existing) continues to
pass.

### R5.4 — `assertCasesDirIsEmpty` inverts for `llm`, and only for `llm`

**MUST**: `test/conformance/golden_sets_test.go`'s per-directory empty-corpus assertion is
restructured so that, for the `llm` golden-set directory specifically, it asserts `cases/`
contains at least one entry beyond `.gitkeep` (an inversion of today's "MUST be empty"
assertion) — matching design D10's existing non-empty-corpus guard pattern elsewhere in that
file (a moved or emptied-out corpus must fail loudly, not pass vacuously).

**MUST NOT**: this PR change the `recall` or `classify` golden-set directories' assertion —
both remain asserted empty (today's `assertCasesDirIsEmpty` behavior, verified current at
`test/conformance/golden_sets_test.go:251-275`) until their own populating PRs (`m1b-pipeline`'s
`feat/core-classify` and `feat/core-recall`) invert them in turn. A single shared function
applied uniformly across all three directories would either fail Phase A (if left as "must be
empty") or wrongly demand `recall`/`classify` cases this change does not create (if inverted
wholesale) — this requirement exists specifically to prevent that scope error.

**Verified by**: `go test ./test/conformance/` — `TestHarness_GoldenSetFormatsDeclared` (or its
restructured successor) passes with `llm/cases/` non-empty and `recall/cases/`,
`classify/cases/` still holding only `.gitkeep`; the test is observed failing first (per
`docs/06-harness.md` §4's before-the-implementation discipline) against the pre-PR5 state where
`llm/cases/` is still empty.

**Scenario**:
- GIVEN `testdata/llm/cases/` populated by R5.3 and `testdata/recall/cases/`,
  `testdata/classify/cases/` both still holding only `.gitkeep`
- WHEN `TestHarness_GoldenSetFormatsDeclared` runs
- THEN the `llm` subtest passes because `cases/` is non-empty, and the `recall`/`classify`
  subtests pass because their `cases/` directories are still empty — both are the *correct*
  outcome at this point in the chain, not a contradiction

---

## 6. Provider HTTP clients and task routing (PR6 — `feat/providers-http`)

### R6.1 — `internal/providers` gains anthropic, openai and ollama clients

**MUST**: `internal/providers` provides a client type per vendor (anthropic, openai, ollama)
implementing `ports.LLMProvider`, speaking HTTP to that vendor's API.

**MUST NOT**: any L1/L2/L3 test in this repository exercise these clients against a live
endpoint — per CLAUDE.md non-negotiable #5, every test of provider *behavior* runs against the
fake (R5.2), never these adapters; if these clients carry any test at all in this PR, it is
limited to request-shaping/parsing logic against fixed, in-memory request/response bytes (an L1
concern), never a real socket.

**Verified by**: `git grep` / code review confirming no test in the chain opens a network
connection; the request/response-shaping tests, if present, run as L1.

### R6.2 — `internal/providers` gains an `EmbeddingProvider` implementation

**MUST**: at least one of the three vendor clients (per doc 01's `tasks.embedding` intent,
ADR-0003) implements `ports.EmbeddingProvider` in addition to `ports.LLMProvider`, or a
dedicated embedding client exists — this spec does not mandate which vendor, since that is a
design/config decision (doc 01's `tasks.embedding` binding), not a WHAT-level requirement.

**Verified by**: compile-time — at least one type in `internal/providers` implements
`ports.EmbeddingProvider`.

### R6.3 — `"openai"` becomes a documented provider type in the Go validator

**MUST**: `internal/config.DocumentedProviderTypes` includes `"openai"`, in the same PR that
ships the openai client (R6.1) — completing R1.1's doc-side half from PR1.

**Verified by**: L1 — `TestValidate`'s existing round-trip (`internal/config`, per proposal
§4.2 point 1) continues to pass with `openai` added to the list; a new case asserting
`providers.x.type: openai` validates.

### R6.4 — A task naming a provider that does not exist in `providers:` fails validation

**MUST**: config validation gains a check — verified absent today by reading `checkTasks`
(`internal/config/validate.go:155-163`), which validates only the task *name*, never the
referenced `provider` value — that fails when a `tasks.<name>.provider` value does not match any
key present in the `providers:` map, for a task whose consuming component is enabled (following
the same enabled-gating shape `checkTelegram`/R5.2 of the `m0-skeleton` spec already established
for `*_env` keys).

**MUST NOT**: this check fire for `tasks.embedding: { provider: ... }`-shaped placeholder
configs in a way that blocks a *documented example* from decoding — R1.2 already removes the
literal ellipsis from doc 01's own example, so this tension is resolved by R1.2, not by weakening
this check.

**Verified by**: L1 — a test with a `tasks:` entry naming a `provider` absent from `providers:`,
asserting validation fails naming both the task and the missing provider key; a test with a
task's provider present in `providers:`, asserting it passes.

**Scenario**:
- GIVEN `tasks: { capture_processing: { provider: nonexistent_llm } }` and a `providers:` map
  that does not contain `nonexistent_llm`
- WHEN the config validates
- THEN it fails, naming `capture_processing` and `nonexistent_llm`

**MUST NOT**: this PR wire the check's output to an actual capture-time provider resolution —
routing a task name to a live provider *instance* at capture time is `cmd/nooma` wiring work
(proposal §4.1's decision-gate table) consumed by Phase B's `feat/brain-capture`; Phase A's
obligation is the config-validation half only, since Phase A writes no unit and runs no capture
pipeline.

**Verified by**: absence — no `internal/brain/**` file exists in this chain (Phase A's proposal
table lists no `brain/` PR).

---

## 7. The `pending-red` promotion sequence (spans PR2 and PR3; constraint from proposal §4.7)

This section states the ordering the umbrella proposal's §4.7 and its own Risk R4 name as a
measured trap, restated here as testable requirements scoped to what Phase A actually promotes.

### R7.1 — PR2 promotes I01 and untags `tree_scan_test.go` in the same PR

**MUST**: the PR that creates `unit.Status` and `unit.AllStatuses` (PR2, §2 above) also, in the
same PR:

1. drops the `//go:build pendingimpl` tag from `test/conformance/i01_focus_never_persisted_test.go`,
   moving it into the untagged L2 suite;
2. drops the `//go:build pendingimpl` tag from `test/conformance/tree_scan_test.go` — the shared
   `scanGoTree` helper both I01 and I03 depend on;
3. removes the two lines `unit.Status` and `unit.AllStatuses` from
   `test/conformance/pending_symbols.txt`.

**MUST NOT**: `unit.Status`/`unit.AllStatuses` be promoted while `tree_scan_test.go` stays
tagged `pendingimpl` — build tags are additive (an untagged file compiles into every build), so
an untagged `i01_focus_never_persisted_test.go` calling `scanGoTree` from a still-tagged
`tree_scan_test.go` fails the default (`make check`) build with `undefined: scanGoTree`, not
merely `make pending-red`.

**MUST NOT**: `tree_scan_test.go` be untagged *before* I01's promotion (as an independent, earlier
change) — with `i01_focus_never_persisted_test.go` still tagged `pendingimpl` and
`i03_units_never_deleted_test.go` also still tagged, an untagged `scanGoTree` would be
unreferenced by any untagged caller and `golangci-lint`'s `unused` check would fail (measured by
the proposal, §4.7).

**Verified by**: `scripts/pending-red.sh` (`make pending-red`), run against this PR's diff,
reports `unit.Status` and `unit.AllStatuses` no longer undefined and passes because
`recall.VectorQuery`, `recall.VectorIndex` and `ports.UnitRepo` are still correctly reported
undefined (three remaining lines); `make check` compiles cleanly (no `undefined: scanGoTree`);
`golangci-lint run` reports no `unused` finding on `scanGoTree`.

**Scenario**:
- GIVEN PR2's diff, which adds `unit.Status`/`unit.AllStatuses`, untags both
  `i01_focus_never_persisted_test.go` and `tree_scan_test.go`, and removes two lines from
  `pending_symbols.txt`
- WHEN `make check` and `make pending-red` both run
- THEN both pass — `make check` because `scanGoTree` is defined in every build that references
  it, `make pending-red` because the three still-pending symbols are still reported `undefined`

### R7.2 — PR3 promotes I03 without re-touching `tree_scan_test.go`'s tag

**MUST**: the PR that creates `ports.UnitRepo` (PR3, §3 above) also, in the same PR, drops the
`//go:build pendingimpl` tag from `test/conformance/i03_units_never_deleted_test.go` and removes
the single line `ports.UnitRepo` from `test/conformance/pending_symbols.txt`.

**MUST NOT**: this PR attempt to untag `tree_scan_test.go` again — it is already untagged by
R7.1's PR2. `tree_scan_test.go`'s tag state is a one-time transition, not a per-caller action.

**Verified by**: `scripts/pending-red.sh`, run against this PR's diff, reports `ports.UnitRepo`
no longer undefined and passes with exactly two lines remaining
(`recall.VectorQuery`, `recall.VectorIndex`); `make check` compiles cleanly.

### R7.3 — Phase A does not touch I21 or retire the `pending-red` gate

**MUST NOT**: any PR in this change (`m1a-substrate`) promote `recall.VectorQuery` or
`recall.VectorIndex`, touch `test/conformance/i21_vector_search_filters_on_model_test.go` (if it
exists) or remove its lines from `pending_symbols.txt` — I21 and `core/recall` are Phase B's
`feat/core-recall` (`m1b-pipeline`), per the umbrella proposal §5's Phase A/B table.

**MUST NOT**: any PR in this change remove `pending-red` from `check-all` or CI, or delete
`scripts/pending-red.sh` or `test/conformance/pending_symbols.txt` — the umbrella proposal's §4.7
states the gate retires only when the *last* of the five symbols is promoted, and that is I21's
promotion (Phase B).

**Verified by**: `git diff --name-only` over this change's full chain contains no
`i21_vector_search_filters_on_model_test.go`, no `Makefile` line removing `pending-red` from
`check-all`, and no deletion of `scripts/pending-red.sh`; `test/conformance/pending_symbols.txt`
holds exactly two lines (`recall.VectorQuery`, `recall.VectorIndex`) at the end of this chain.

**Scenario**:
- GIVEN the full `m1a-substrate` chain merged (PRs 1–6)
- WHEN `test/conformance/pending_symbols.txt` is read
- THEN it contains exactly `recall.VectorQuery` and `recall.VectorIndex`, and
  `sh scripts/pending-red.sh` still passes, reporting both as `undefined:` under
  `-tags pendingimpl`

---

## 8. Cross-cutting constraints

### R8.1 — `internal/core/unit` respects the dependency rule and the injected clock

**MUST**: every file under `internal/core/unit/**` imports only the standard library and its own
package — `depguard`'s `core-purity` rule (`.golangci.yml`, scoped to `**/internal/core/**`)
enforces this; no file imports `internal/store`, `internal/providers`, `internal/ports`, or any
non-stdlib dependency.

**MUST**: no file under `internal/core/unit/**` calls `time.Now`, `time.Since`, `time.Until`,
`rand.*`, `uuid.*`, or `os.Getenv` — `forbidigo`, scoped to `internal/core/` by
`.golangci.yml`'s exclusion rule, enforces this. Since none of R2.1–R2.4's functions need the
current instant, an id, or randomness, this is satisfiable by construction, not by a workaround.

**Verified by**: `golangci-lint run` (part of `make check`, so this fires on the fast loop, not
only `make check-all`).

### R8.2 — `internal/core/unit` reaches the ≥90% coverage floor

**MUST**: `internal/core/unit`'s statement coverage is ≥ 90%, measured by
`scripts/core-coverage.sh` (`make cover`, part of `make check-all` only — this is the risk
proposal §9 R1 names: the floor is invisible to `make check`'s fast loop and fires for the first
time on this PR, since `internal/core/` has held only `doc.go` files until now).

**MUST**: PR2 ships its L1 tests in the same commit as the code they cover, per
`docs/06-harness.md` §7's commit convention — a work-unit commit is the change and its tests
together, not two commits.

**Verified by**: `scripts/core-coverage.sh` exits zero; `make check-all` (not `make check`) is
the gate that proves this before a PR opens.

### R8.3 — PR2 gives `docs/02-cognitive-core.md` §1 a real delta

**MUST**: PR2 (the only Phase A PR touching `internal/core/**`) also touches
`docs/02-cognitive-core.md` §1 in the same PR — stating the live-status predicate as the positive
filter R2.2 implements — satisfying `docs-sync.yml`'s rule (a PR touching `internal/core/**`
must also touch doc 02, or carry `no-spec-change`) by design, per the umbrella proposal §4.8's
table.

**MUST NOT**: PR2 carry the `no-spec-change` label — per the umbrella proposal §4.8, "no M1 core
PR should need it"; this PR has a genuine behavioral delta to document (the taxonomy, the
predicate, and the transition table are new facts about the brain, not a refactor).

**Verified by**: `docs-sync.yml` passes on PR2 without the label (verifiable only once a PR is
open on GitHub, per proposal §9 R2 — not locally reproducible by a Makefile target).

**MUST NOT**: PR3, PR4, PR5, or PR6 touch `internal/core/**` — none of them add a `core/` file
per this spec's own scoping (`ports.UnitRepo` lives in `internal/ports`, the SQLite
implementation in `internal/store/sqlite`, the provider fake and clients in `internal/ports`/
`internal/providers`). `docs-sync.yml` therefore does not fire on PRs 3–6, and none of them need
`no-spec-change` either — there is nothing in `internal/core/**` for them to have touched.

**Verified by**: `git diff --name-only` per PR contains no `internal/core/` path for PRs 3–6.

### R8.4 — No test in this change touches the network or a real LLM

**MUST NOT**: any L1, L2, or L3 test added by this change open a network connection or call a
real LLM provider (CLAUDE.md non-negotiable #5; `docs/06-harness.md` §3).

**Verified by**: review — no test in the chain imports an HTTP client configured against a
non-loopback, non-fixture endpoint; the provider fake (R5.2) is the only thing any pipeline-level
test talks to.

---

## 9. Boundaries this change must not cross

### R9.1 — No unit is ever written by this change's own code paths

**MUST NOT**: any PR in this change wire a caller that creates, classifies, embeds, or persists
a unit as part of a running pipeline — `core/classify`, `core/recall`, `core/relation`,
`brain/capture`, and every HTTP route are Phase B/C, not Phase A. Test code exercising R3.3's
fake or R4's SQLite implementation directly (to prove the port's contract) is not "a pipeline";
it is the port's own conformance test.

**Verified by**: no `internal/brain/**` or `internal/httpapi/**` file exists at the end of this
chain; `cmd/nooma`'s command set is unchanged by this change (`m0-skeleton`'s R10.1 five
commands, still exactly five — this change adds no CLI subcommand).

### R9.2 — The schema is not extended

**MUST NOT**: this change add a migration, alter `0001`/`0002`, or change
`docs/03-data-model.md`'s schema sections — R4.4 already states this for PR4 specifically; this
requirement makes it a whole-of-Phase-A property, matching `m0-skeleton`'s R14.2 precedent.

**Verified by**: `git diff` over `internal/store/sqlite/migrations/` is empty across the full
chain; `make schema-golden-clean` (if this target exists, per the M0 precedent) leaves a clean
tree.

### R9.3 — `core/classify`, `core/recall`, and `core/relation` remain empty

**MUST NOT**: this change create any file under `internal/core/classify/`,
`internal/core/recall/`, or `internal/core/relation/` beyond what already exists today (each
holds only a `doc.go`, verified present). R1.3 documents `core/classify`'s future location in
doc 06's tree; it does not create the package.

**Verified by**: `git diff --name-only` over the full chain contains no path under those three
directories except, if touched at all, `docs/06-harness.md` itself (R1.3).

---

## 10. Test levels

### R10.1 — Level assignment for this change

**MUST**: the unit taxonomy, the live predicate, the transition table, and the in-memory fake's
own round-trip/isolation behavior are **L1** — pure functions or in-memory state, no database, no
network, no process.

**MUST**: `ports.UnitRepo`'s structural contract (no `Delete*` method) and the promoted I01/I03
tree scans are **L2** (`test/conformance/`, untagged).

**MUST**: the SQLite `UnitRepo` implementation's behavior against a real vault (R4.1–R4.3) is
**L3** (`integration` tag).

**MUST**: no L4 test is added by this change — Phase A exposes no new CLI subcommand and no HTTP
route for a compiled-binary test to drive.

**Verified by**: file placement and build tags, per `docs/06-harness.md` §3.

### R10.2 — Every new test is observed failing for the right reason first

**MUST**: each requirement's test in this spec is written before its implementation and observed
failing with the expected message or compiler error, per non-negotiable #4 and strict TDD mode —
explicitly including R7.1/R7.2's `pending-red` promotions, whose "observed failing" state is
`scripts/pending-red.sh` reporting the not-yet-created symbol as `undefined:`, and R2's/R3's L1
tests, which must be written against the not-yet-existing `unit.Status`/`ports.UnitRepo` first
(the same compile-error-as-red-state pattern the pendingimpl tests already establish).

**MUST NOT**: a failing test be weakened to pass. The two legitimate exits are fixing the code or
changing the governing document (doc 02, plus its ADR if one is affected) in the same PR.

**Verified by**: the commit sequence within each PR — a work-unit commit contains the test and
the code that satisfies it.

---

## 11. Open items this spec deliberately leaves to design or to Phase B

These are named here so they are not silently assumed by a reader of this document alone:

- **Which type backs `unit.Status`** (a defined string type vs. an enum-like int with a
  `String()` method) — R2.1 only requires `reflect.String` kind; the exact Go type is design's
  choice.
- **The exact method names and signatures on `ports.UnitRepo`** beyond the two structural
  constraints (R3.1: no `Delete*`; R3.2: a status-filtered read) — design's choice.
- **Which vendor backs `ports.EmbeddingProvider`** (R6.2) — doc 01's `tasks.embedding` binding is
  a config/design decision this spec does not preempt.
- **`units.confidence`'s semantics** (umbrella proposal §8 Q2) — open at the umbrella level,
  explicitly costs Phase A nothing either way (proposal §8 Q5's closing note), since Phase A
  writes no unit through a production code path.
