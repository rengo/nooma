# Design — M1 Phase B: the pipeline

Technical design for `m1b-pipeline`, the second of the three chained changes
[`m1-capture-recall/proposal.md`](../m1-capture-recall/proposal.md) §5 splits M1 into. Phase A
(`m1a-substrate`) is **complete** — thirteen merges, every task `[x]`. Phase C (`m1c-surface`)
is out.

This design settles how doc 02 §5's capture pipeline becomes executable: where classify's
tolerant decode ends and orchestration begins, how the one clock read is made structural, what
`recall.VectorQuery` / `recall.VectorIndex` actually are, how the two recall legs produce their
candidate sets and fuse, how the relation judge applies its thresholds, and how the last
`pending-red` anchor is retired without blocking `main` forever. It does not restate
requirements — that is [`spec.md`](spec.md), written in parallel over the same proposal.

**Scope, taken from `spec.md` §0 and honoured here.** The umbrella proposal's Phase B table
lists six PRs (7–12). `spec.md` §0 narrows Phase B to **PRs 7–11**, moving `feat/corrections`
(PR 12, with I03's correction half and I13's first `learning_signals` write) to `m1c-surface`,
because Q3c — how a correction finds its referent — is open and belongs to Phase C. This design
follows that narrowing. Consequences: no `ports.SignalRepo`, no `learning_signals` write, and
no correction path anywhere in §3's layout.

**A scope note, stated before anything rests on it.** Phase B ships a capture service, a recall
service, four new ports, five new store implementations and three new core packages — and **no
user path reaches any of them.** The HTTP routes and the `nooma capture` subcommand are Phase
C's PRs 13 and 14 (`spec.md` R4.8). Every Phase B behaviour is proven by a test, never by a
running binary. This is the same shape Phase A shipped in, and it matters when reading §6: an
L4 row does not exist because there is nothing compiled to drive.

---

## 1. Ground truth this design was verified against

Every row was checked by reading the named file at the named line, or by a `Glob`/`Grep` over
the working tree, **in this session**. **This session had no shell**, so nothing here was
verified by running a command; where an earlier artifact records a *measured* result that could
only come from execution, the row says so and attributes it rather than re-asserting it as
freshly observed. Rows that could not be verified at all say so in plain words — this chain
has already shipped one requirement asserting "verified present" about something that was not.

### 1.1 The Phase A tree, as it actually exists

| Claim | How it was verified |
|---|---|
| `unit.Status` is a defined string type with four constants; `AllStatuses()` is a function; `ParseStatus`; `Status.IsLive()` returns `s == StatusPool` | `internal/core/unit/status.go:14`, `:20-25`, `:33-35`, `:45-52`, `:60-62` |
| `unit.Type` carries exactly nine persisted values; `AllTypes()`, `ParseType`, `ErrUnknownType`; its doc comment states classify's taxonomy is a **different** thirteen-member vocabulary living in `core/classify` | `internal/core/unit/type.go:10-13`, `:17-27`, `:32-37`, `:47-54` |
| `unit.ValidateTransition` exists over an unexported `legalTransitions` map holding five legal pairs, including `incomplete → archived`; self-transitions absent | `internal/core/unit/transition.go:17-22`, `:35-46` |
| `unit.Unit` has 14 fields; `EventAt`/`DueAt` are `*time.Time`, `Confidence *float64` "always nil in Phase A", `StructuredData json.RawMessage` | `internal/core/unit/unit.go:18-33` |
| `ports.UnitRepo` declares exactly five methods — `Create`, `ByID`, `LiveByIDs`, `UpdateContent`, `SetStatus` — plus `ErrUnitNotFound` / `ErrUnitExists` / `ErrStatusConflict` | `internal/ports/unitrepo.go:30-57`, `:60-72` |
| `ports.LLMResponse.Text` is documented as raw bytes-as-string, "never parsed", with I14 explicitly deferred to `internal/core/classify` (Phase B) | `internal/ports/provider.go:20-35` |
| `ports.EmbedResponse` is `{Vector []float32; Model string}` with **no `Dim` field** — "the dimension is `len(Vector)`" | `internal/ports/provider.go:47-62` |
| `ports.Clock` is consumed by `internal/brain`, "reads the clock once at the start of an operation and passes the resulting `time.Time` down as a plain argument" | `internal/ports/clock.go:6-20` |
| `ports.IDGen` exists and returns a UUID v4 string | `internal/ports/clock.go:22-29` |
| `internal/ports` today holds exactly four `.go` files: `clock.go`, `unitrepo.go`, `provider.go`, `doc.go` | glob of `internal/**/*.go` |
| `sqlite.UnitRepo` is the SQLite `ports.UnitRepo`; `LiveByIDs` filters **positively** (`AND status = ?` bound to `unit.StatusPool`) and reorders to the caller's `ids` | `internal/store/sqlite/unitrepo.go:81-119`, esp. `:91`, `:94`, `:112-117` |
| `sqlite.NewUnitRepo(v *Vault)` reaches `v.db`, an unexported field — so every store type Phase B adds must live in package `sqlite` too | `internal/store/sqlite/unitrepo.go:23-31`; `internal/store/sqlite/open.go:20` |
| Timestamps are persisted as `time.RFC3339` text (`unitTimeLayout`) | `internal/store/sqlite/unitrepo.go:41`, `:231-233` |
| `internal/store/sqlite` legitimately imports `internal/core/unit` — the store→core direction is allowed and already used | `internal/store/sqlite/unitrepo.go:14` |
| `test/support/repocontract.RunUnitRepo(t, newRepo)` is the shared, untagged contract suite run by L2 (`memrepo`) and L3 (`sqlite`) | `test/support/repocontract/repocontract.go:31` and its package doc `:1-15` |
| `fakeprovider.New(t, dir, ids...)` is an **ordered script keyed on case id**; an unscripted `Complete` fails immediately, an unused scripted id fails at cleanup; `SeenPrompts()` records prompts without them ever being the key | `test/support/fakeprovider/fakeprovider.go:50-59`, `:62-84`, `:89-91` |
| `fakeprovider.NewEmbeddingFake(model)` returns a `*Fake` that **carries no `Complete` script** — one capture test therefore needs two distinct `*Fake` values, one per port | `test/support/fakeprovider/embed.go:24-28` |
| The fake embedder's vectors are `fakeEmbeddingDim = 8` elements of `float32(h.Sum32()%10000)/10000` — every component in `[0,1)`, **never normalized, never semantic** | `test/support/fakeprovider/embed.go:14`, `:41-50` |
| `goldenset.ClassifyExpected` has no top-level `event_at`/`due_at`; dates appear only inside the opaque `structured_data` | `test/support/goldenset/types.go:166-178`; `testdata/classify/format.md:36` |
| `goldenset.RecallExample` carries `units[].{id,type,content,status}` and `queries[].{query,expected_unit_ids}` — **no vectors, no lexical ranking** | `test/support/goldenset/types.go:31-35`, `:73-78`, `:100-103` |
| `DecodeStrict` rejects unknown fields and enforces each type's `Validate()` | `test/support/goldenset/loader.go:24-47` (read via `types.go`'s `Validator` contract at `:23`) |
| `casesDirMustBeEmpty` is a rules-as-data map: `recall: true, classify: true, llm: false` | `test/conformance/golden_sets_test.go:259-263`, with `assertCasesDirEmptiness` at `:271-299` |
| `internal/config.DocumentedProviderTypes` is `["anthropic","openai","ollama","whisper_cpp"]`; `checkTaskProviders` already rejects a task naming an undeclared provider | `internal/config/validate.go:190`, `:172-181` |
| `DocumentedTaskNames` includes `capture_processing`, `relation_evaluation` and `embedding` | `internal/config/validate.go:194-202` |
| `internal/brain` contains exactly one file, `doc.go`, with a package comment and no code | `internal/brain/doc.go` (4 lines); glob of `internal/**/*.go` |
| `internal/core/classify/` **does not exist** | glob of `internal/**/*.go` — no path under it |
| `docs/06-harness.md` §1's tree already lists `classify/` (PR 1 of Phase A added the line) | `docs/06-harness.md:27` |
| `internal/core/recall/doc.go` still carries its "Pending conformance anchor" paragraph | `internal/core/recall/doc.go:5-14` |
| `internal/ports/doc.go` and `internal/core/unit/doc.go` no longer carry theirs | `internal/ports/doc.go` (9 lines, no anchor paragraph); glob confirms `unit/doc.go` exists, and Phase A task 2.4 records its removal as `[x]` (`m1a-substrate/tasks.md:196-210`) |

### 1.2 The gates Phase B has to move

| Claim | How it was verified |
|---|---|
| `test/conformance/pending_symbols.txt` holds exactly **two** tracked symbols, `recall.VectorQuery` and `recall.VectorIndex` | `test/conformance/pending_symbols.txt:6-7`; lines 1–5 are the comment header `pending-red.sh:31` strips |
| `i21_vector_search_filters_on_model_test.go` is the **only** file left carrying `//go:build pendingimpl` | `Grep` for `go:build pendingimpl` over the tree: two `.go` hits — `i21_…:1` (a real build tag) and `tree_scan_test.go:4` (**a comment**, verified by reading `tree_scan_test.go:1-8`; the file is untagged) |
| That comment in `tree_scan_test.go:4-7` is **stale**: it says I03 is "still `//go:build pendingimpl` until PR 3 promotes it", and PR 3 promoted it | `tree_scan_test.go:3-7` versus `test/conformance/i03_units_never_deleted_test.go:40` (no build tag on that file) |
| I21 requires `recall.VectorQuery` and `recall.VectorIndex` to be **structs** each carrying an exported **string-kind** `Model` field | `i21_vector_search_filters_on_model_test.go:60-77`, `:81-96` |
| I21's own "Assumed shape" comment says a `VectorIndex` is **scoped to one model**, and that a two-model vault is "two `VectorIndex` values, one per model, **never one index serving both**" | `i21_vector_search_filters_on_model_test.go:52-57` |
| I21's comment also states the reflective test proves the invariant is *expressible*, not enforced, and that the promoting PR still owes a behavioural test | `i21_…:36-50` |
| `pending-red.sh` runs `go test -c -tags pendingimpl` **first** and fails when that build **succeeds** — before it ever reads the symbols file. No empty-list short-circuit | `scripts/pending-red.sh:9-19` versus `:31` |
| `pending-red` is in `check-all`'s prerequisite list, has its own target, and is named in the Makefile header | `Makefile:39`, `:93-95`, `:13` |
| CI declares a `pending-red` job that runs `make pending-red` | `.github/workflows/ci.yml:107-115` |
| `docs/06-harness.md` describes the gate in §6's table and in §8 point 5 | `docs/06-harness.md:245`, `:349`, `:355` |
| `.golangci.yml`'s `run.build-tags` comment explains why `pendingimpl` is excluded | `.golangci.yml:20-24` |
| `pending-red` is a **required status context in `main`'s branch ruleset** | **Not verified in this session** — no shell, no `gh`. Asserted by `m1a-substrate/design.md` D8 and its §7 risk 5. Treated as true because acting on it is safe and not acting on it is not; §2 D10 gives the operator the check to run |
| `depguard`'s `core-purity` allows `internal/core/**` only `$gostd` and `github.com/rengo/nooma/internal/core`, and explicitly denies `os` | `.golangci.yml:52-62`, `:80-81`. There is **no** `$test`/`!$test` selector on `files:`, so as written it covers `_test.go` under `internal/core` too |
| `forbidigo` bans `time.Now`/`Since`/`Until`/`rand.`/`uuid.`/`os.Getenv` **by call pattern** and is scoped to `internal/core/` alone | `.golangci.yml:101-115`, `:122-124` |
| The core coverage floor measures **only test binaries under `internal/core/...`** — an L2 test in `test/conformance` contributes nothing | `scripts/core-coverage.sh:56` (`-coverpkg=./internal/core/... ./internal/core/...`) |
| The floor is 90, compared as `covered*100 < FLOOR*total` so 89.9 % fails | `scripts/core-coverage.sh:45`, `:111` |
| `make check` does not run `cover`; `check-all` and CI's `coverage` job do | `Makefile:36` vs `:39`, `:85-87`; `ci.yml:125-133` |
| `docs-sync.sh` fires on `^internal/core/` only, and is satisfied by touching `docs/02-cognitive-core.md` or by the `no-spec-change` label | `scripts/docs-sync.sh:45-51`, `:53-62` |
| The D9 presence guard (`core_exported_decls_have_tests_test.go`) and the store clock guard (`store_no_direct_clock_read_test.go`) already exist and run untagged in the fast loop | glob of `test/**/*.go` |

### 1.3 The schema, the ADRs, and doc 02

| Claim | How it was verified |
|---|---|
| `unit_embeddings` is `(unit_id PK → units ON DELETE CASCADE, model NOT NULL, dim NOT NULL, embedding BLOB NOT NULL, created_at NOT NULL)`, with `idx_unit_embeddings_model` | `internal/store/sqlite/migrations/0002_learning_and_search.sql:74-81`; identical in `docs/03-data-model.md:206-213` |
| The BLOB is documented as "dim × float32, little-endian, L2-normalized on write" | `0002_learning_and_search.sql:78` |
| `units_fts` is `fts5(content, content='units', content_rowid='rowid')` with three sync triggers that fire on INSERT / DELETE / **every** UPDATE | `0002_learning_and_search.sql:84-86`, `:118-127` |
| The triggers genuinely run — insert, update and archival re-index at the same rowid, delete removes | `test/integration/fts5_search_test.go:104-113`, `:130-148`, `:165-198`, `:214-231` (each carries a watched-RED transcript) |
| FTS5 must be registered per connection, and `sqlite.Open` does it in `initConn`, so "a connection without FTS5 registered is unrepresentable" | `internal/store/sqlite/open.go:164-169`; `docs/03-data-model.md:288-298` |
| `relations` has `UNIQUE (from_unit_id, to_unit_id, type)` and defaults `strength 0.5`, `confidence 0.5`, `created_by 'system'` | `0001_core_tables.sql:30-40` |
| `relation_thresholds` declares `min_confidence_to_persist REAL NOT NULL DEFAULT 0.3` and `min_confidence_to_surface REAL NOT NULL DEFAULT 0.5`, and migration 0002 **seeds no rows** | `0002_learning_and_search.sql:30-35` — the file contains no `INSERT` |
| `relations` has **no** "uncertain" column — the band is derivable from `confidence`, not stored | `0001_core_tables.sql:30-40`; doc 02 §4's band definition at `docs/02-cognitive-core.md:102-105` |
| `decision_log` is `(id, action, rationale, context DEFAULT '{}', occurred_at)` with an index on `occurred_at`; its DDL comment gives `'capture.classify'` as the example action | `0001_core_tables.sql:95-102` |
| `units.weight` defaults to `1.0` and `units.weight_decay_rate` to `0.01` in SQL | `0001_core_tables.sql:11-12` |
| `timers` and `triggers` tables exist and nothing in the tree writes to them | `0001_core_tables.sql:42-70`; glob shows no `internal/core/prospection` or `internal/scheduler` file beyond `doc.go` |
| ADR-0010 fixes RRF with `k = 60`, `score(d) = Σ 1/(k + rank_i(d))`, 1-indexed ranks, a single term for a doc in one list | `docs/adr/0010-hybrid-recall-fusion.md:32-42` |
| ADR-0010 requires `k` **and each list's relative weight** to be named constants, not literals | `0010:48-49` |
| ADR-0010 states `bm25()` returns **negative** values with no fixed bound, so vector and lexical scores are incomparable | `0010:19-22`. **The sign convention was not executed here** — no shell. PR 9's L3 test is what confirms `ORDER BY bm25(units_fts)` ascending is best-first |
| ADR-0010's stated cost is that RRF discards magnitude, and its stated mitigation is the judge seeing full candidate **content**, not scores | `0010:65-69` |
| ADR-0012 decides brute-force dot product over an in-memory index, vectors unit-normalized on write, index loaded at vault open and "must not be paid per request" | `docs/adr/0012-vector-proximity-search.md:59-63`, `:95-96` |
| ADR-0012's memory table: 1,000 → 3 MB / 0.88 ms; 10,000 → 29 MB / 8.30 ms; 50,000 → 146 MB / 43.12 ms; 500,000 → 1.4 GB / 434.57 ms; index load 42 ms per 10,000 | `0012:45-55` |
| That table is arithmetically consistent with **`float32`**: 10,000 × 768 × 4 B = 29.3 MiB against its "29 MB" | recomputed here; the same check is `m1a-substrate/design.md` §1's own row |
| ADR-0012 does **not** name the in-memory layout its measurement used (flat buffer vs slice-of-slices) | `0012:28-55` — read in full; no such statement exists |
| ADR-0012 says the recall interface hides the mechanism: `internal/core/recall` receives ranked lists and fuses; replacing the implementation must not touch the core | `0012:70-73` |
| ADR-0003's amendment moves "never mix models" from schema-enforced to **query-enforced**, and says it "belongs in the conformance suite" | `docs/adr/0003-embeddings.md:80-92` |
| Doc 02 §5's classification taxonomy is thirteen values, including `chitchat`, `out_of_scope`, `recall`, `correction`, `timer`, `recurring_reminder` | `docs/02-cognitive-core.md:113-115` |
| Doc 02 §5 names six orthogonal resolution fields, explicitly "orthogonal fields, not types" | `docs/02-cognitive-core.md:118-123` |
| Doc 02 §5's robustness clause: "a malformed field degrades to null (that resolution is ignored), it never brings down the whole classification" | `docs/02-cognitive-core.md:124-125` |
| Doc 02 §5's injected context is "active self-beliefs, local date + user timezone, open check-ins" | `docs/02-cognitive-core.md:116-117` |
| Doc 02 §5's numbered pipeline never mentions embedding at all — the "persist then embed" order comes from the proposal's diagram, not from doc 02 | `docs/02-cognitive-core.md:110-140` read in full; `m1-capture-recall/proposal.md:257-267` is where the order appears |
| Doc 02 §5's product rule: "Nooma captures with what it has, decides on its own, leaves an auditable trace, and only asks when ambiguity blocks it" | `docs/02-cognitive-core.md:142-144` |
| Doc 02 §2: classify assigns `weight` and λ; type orients direction; type acts as a prior when the self-model is empty | `docs/02-cognitive-core.md:53-57` |
| Doc 02 §8: "**A timer is NEVER a unit**: no weight, no decay, no graph, no belief derivation" | `docs/02-cognitive-core.md:210-212` |
| Doc 02 §11: "Every automatic decision with an **effect** is recorded with its reasoning", and "Pull: everything is recorded and explorable in the activity UI" | `docs/02-cognitive-core.md:266-270` |
| Doc 02 §13 lists RRF `k` = 60 and has **no** `recall_top_k`, no fusion weights, no dedup candidate bound | `docs/02-cognitive-core.md:291-310` — read row by row |
| Doc 02 §13 lists "λ per type (`weight_decay_rate`) | prior per type, base 0.01/day" but enumerates no per-type table | `docs/02-cognitive-core.md:295` |
| Doc 02 §12 claims `units.confidence` for the perception gate (Q2's closed answer), so Phase B writes NULL | proposal §8 Q2 at `m1-capture-recall/proposal.md:525-530`; `unit.Unit.Confidence` is already `*float64` "always nil" (`unit.go:30`) |
| `nooma doctor` is already documented as running "`PRAGMA integrity_check` + units↔embeddings↔fts consistency" | `docs/03-data-model.md:306-307` |
| `nooma.yml` has **no timezone key**, and the `config` table has no timezone column | `docs/01-architecture.md:164-216` read in full; `0002_learning_and_search.sql:61-70` |
| No test in this repository may reach a real LLM; `httptest` on an in-process loopback listener is not "the network" in §3's sense | `CLAUDE.md` non-negotiable #5; `docs/06-harness.md:152-154`; the precedent is `m1a-substrate/design.md` §6's own note, with `internal/providers/*/client_test.go` as the shipped example |

---

## 2. Decision record

### D1 — Classify's boundary is a **salvaging** decoder: `Decode(raw string) (Classification, error)`, pure, per-field optional

The provider port hands over raw text and nothing else (`provider.go:20-35`). I14 is therefore
a property of those bytes, and the whole of it lives in one pure function an L1 test can hammer.

**The shape.** Every field of `Classification` is optional at the type level — a pointer, or a
`json.RawMessage` for the opaque one — and the value carries its own report of what was lost:

```go
func Decode(raw string) (Classification, error)   // ErrNoFieldsSalvaged only

type Classification struct {
    Kind              *Kind            // the thirteen-member taxonomy — doc 02 §5
    NormalizedContent *string
    StructuredData    json.RawMessage
    Weight            *float64
    DecayRate         *float64
    EventAt           *time.Time       // D2
    DueAt             *time.Time       // D2
    NudgeOutcome       *NudgeOutcome
    RelationOutcome    *RelationOutcome
    StateOutcome       *StateOutcome
    TaskCheckinOutcome *TaskCheckinOutcome
    ListOp             *ListOp
    PersonRefStatus    *PersonRefStatus
    Degradations      []Degradation    // what was lost, and why
}

type Reason string
const (
    ReasonAbsent      Reason = "absent"
    ReasonWrongType   Reason = "wrong_type"
    ReasonUnknownEnum Reason = "unknown_enum"
    ReasonTruncated   Reason = "truncated"
    ReasonBadFormat   Reason = "bad_format"
)
type Degradation struct{ Field string; Reason Reason }
```

Pointers rather than zero values, for the reason `goldenset.ClassifyExpected` already recorded
for exactly this problem (`types.go:152-165`): with a plain `float64`, `"weight": null` and a
missing `"weight"` key both decode to `0.0`, indistinguishable from a case that genuinely means
zero. `Degradations` exists because I12 requires `brain` to write a rationale into
`decision_log`, and a decoder that discards *why* a field vanished forces the orchestrator to
guess.

**The mechanism, and why it is not `json.Unmarshal`.** `testdata/classify/format.md:92-94`
requires a **truncated-JSON** case in the corpus and says it is one of the three shapes that
prove I14. `encoding/json` rejects a truncated document wholesale — so `json.Unmarshal` into a
struct, or even into a `map[string]json.RawMessage`, degrades *every* field at once, which is
"it brings down the whole classification", the precise thing doc 02 §5 forbids
(`02:124-125`).

So `Decode` is built on a **salvaging streaming read**:

```go
// Salvage reads a JSON object's top-level members one at a time and returns
// every member it completed before the stream ended or went malformed.
func Salvage(raw []byte) (fields map[string]json.RawMessage, truncatedAfter bool)
```

It opens a `json.Decoder`, consumes the opening `{`, then loops `Token()` for a key and
`Decode(&json.RawMessage{})` for its value. When either errors — `io.ErrUnexpectedEOF`, a
`*json.SyntaxError`, anything — it stops and returns what it already has, flagging that the
stream ended early. A payload truncated halfway through its fourth field yields the first three
intact and marks the rest `ReasonTruncated`. That is literally "a malformed field degrades to
null and the rest survives", for the one shape the naive implementation cannot express.

| Option | Verdict |
|---|---|
| `json.Unmarshal` into the struct | Rejected — a truncated payload degrades every field at once, which the corpus is required to contain a case against |
| `json.Unmarshal` into `map[string]json.RawMessage`, then per-field | Rejected for the same reason: the outer unmarshal still rejects a truncated document. It handles wrong-type and unknown-enum correctly and nothing else |
| **Streaming salvage + a per-field table** (chosen) | Handles all three corpus shapes with one mechanism; the per-field pass is then trivially total |
| A hand-written tolerant JSON parser | Rejected — a second JSON implementation in a project whose core may import only stdlib, to buy nothing the streaming decoder does not already give |

**The floor.** `Salvage` returning zero completed members means the payload was not an object,
or was cut before its first value. There is no classification to degrade, because there are no
fields. `Decode` returns `ErrNoFieldsSalvaged`, and D9's routing gives that its own
`decision_log` action. This is not a violation of I14: I14 governs a *field*, and a payload with
no fields has none. Stated here because "truncated JSON degrades" and "an empty payload is an
error" both have to be true and a reader will otherwise think one contradicts the other.

**What happens to a degraded `type`.** `Kind` degrades like any other field — an unknown enum
value leaves `Kind == nil` and every other field intact, which is what an L1 test and the
corpus's unknown-enum case assert. What `brain` does *next* is a separate, named decision (D9):
there is no `unit.Type` to persist, so nothing is written and the refusal is logged. The
classification is not aborted; the *unit write* is declined. Keeping those two sentences apart
is what lets I14 stay a property of a pure function.

**Why a new package and not `core/unit`.** Already settled by the proposal (§4.1) and by Phase
A's `unit.Type` doc comment (`type.go:10-13`), which names `core/classify` as the home of the
thirteen-member vocabulary. `unit.Type` stays nine values; `classify.Kind` is thirteen; the
mapping is a partial method on `Kind`:

```go
func (k Kind) UnitType() (unit.Type, bool)   // false for chitchat, out_of_scope, recall,
                                             // correction, timer, recurring_reminder
```

`false` is where "a `chitchat` message produces no unit" is expressed, next to the decoder that
produced it rather than next to the definition of what a unit is. `core/classify` importing
`core/unit` is allowed — `depguard`'s `core-purity` allow-list is `github.com/rengo/nooma/internal/core`
(`.golangci.yml:62`), a prefix, and the arrow points away from nothing.

### D2 — The wire shape carries `event_at` and `due_at` as **top-level, separately-named** fields; `Classification` has no `created_at` at all

Doc 02 §5 says classify resolves "tomorrow"/"on Friday" against the injected local date, so the
provider returns absolute dates. I18 says the three timestamps are never interchanged. This
design makes that structural rather than careful:

- The wire shape has **three separately-named keys and one of them does not exist**:
  `event_at` and `due_at` are top-level; `created_at` is not a key the provider is ever asked
  for, because ingestion time is the orchestrator's fact, not the model's.
- `Classification` correspondingly has `EventAt` and `DueAt` and **no `CreatedAt` field**.
  I18's hardest third — "`created_at` was filled from something the model said" — is therefore
  unrepresentable, not merely tested.
- `classify.ToUnit` (D4) is the only place the three meet, and its L1 test drives it with three
  distinguishable instants.

**The alternative, and its cost.** Phase A's corpus example puts the date inside
`structured_data` (`testdata/classify/format.md:36`), and `goldenset.ClassifyExpected` has no
date fields (`types.go:166-178`). Keeping dates there would mean `brain` reaching into an
explicitly unschema'd blob to extract a governed column — `format.md:53` says
`structured_data`'s "shape varies by `expected.type` and is not fixed by a single schema in doc
02". Extracting a NOT-NULL-governed column from a deliberately opaque payload is the
two-sources-of-truth defect this project keeps naming, and it would move I18's enforcement into
an orchestrator where no L1 test reaches it.

**So the corpus type widens, in the PR that lands the classify corpus** (PR 7c):
`goldenset.ClassifyExpected` gains `EventAt *string` and `DueAt *string`, both optional, and
`testdata/classify/format.md`'s field table and its fenced example move the date out of
`structured_data`. The two must move together, because `TestHarness_GoldenSetFormatMatchesType`
decodes the fence into the Go type under `DecodeStrict` — that guard is what keeps the widening
honest rather than being a reason to avoid it. Widening a test-support type is not weakening a
conformance test; `docs/06-harness.md` §4's prohibition does not apply.

Dates are `*string` in the corpus (the recorded wire text) and `*time.Time` in
`Classification` (the parsed value), and the parse is where `ReasonBadFormat` comes from.
Accepted formats: RFC3339, and the date-only `2006-01-02` the example already uses — a
date-only value parses to midnight in the instant's own location, which is passed in, never
read from the OS.

### D3 — Missing `weight`/λ fall back to **one base prior**, pinned to migration 0001's column defaults; nine per-type numbers are not invented

`units.weight` and `units.weight_decay_rate` are `NOT NULL` (`0001:11-12`), so a degraded
`weight` cannot be persisted as null. Something must supply it.

Doc 02 §13 names the knob as "λ per type (`weight_decay_rate`) — prior per type, base 0.01/day"
(`02:295`) and enumerates no per-type table anywhere. Doc 02 §2 says type "orients the
direction" and the self-model "personalizes the value" — both of which are the **model's** job,
performed through the prompt, not a table in Go.

**Decision: `core/classify` declares exactly two constants, `PriorWeight = 1.0` and
`PriorDecayRate = 0.01`, matching migration 0001's own column `DEFAULT`s, and an L2 test pins
them to the migration text off disk.** This is Q1's closed shape, applied a second time: the
precedent helper is `migrationSQLText` in `test/conformance/i13_learning_signal_test.go:24-57`,
already reused by Phase A's `unit_status_ddl_test.go`.

| Option | Verdict |
|---|---|
| A nine-row per-type prior table in Go | Rejected — invents nine calibration numbers doc 02 §13 does not state, two milestones before anything can calibrate them. §13's row says "base 0.01/day", singular |
| Let the SQL `DEFAULT` supply it (omit the column from the INSERT) | Rejected — `sqlite.UnitRepo.Create` binds all fourteen columns explicitly (`unitrepo.go:46-54`) and R6.3 keeps that file closed; and a value that only exists in SQL is invisible to `core`'s own tests and to `decision_log` |
| **Two named constants pinned to the SQL defaults** (chosen) | One place, one number, a mechanized anti-drift check, and no invented calibration |

The comparison must be over **parsed floats**, not source text: the SQL reads `0.3`/`0.01` and
Go writes `0.30`/`0.01`. Stated because a string comparison would pass today and fail on the
first cosmetic edit.

Doc 02 §13 gains no new row here — the row already exists, and PR 7b's doc 02 delta names the
base value as the fallback rather than adding a knob.

### D4 — The capture pipeline is a clockless worker behind a one-line entry point, and the second `Now()` is made **unreachable**, then gated

`forbidigo` is scoped to `internal/core/` alone (`.golangci.yml:122-124`), so a `time.Now()` in
`internal/brain` is legal and no lint sees it. Proposal R9 names the real bug — not a stray
`time.Now`, but a **second** clock read mid-operation, so one decision sees two instants. This
design closes it in three layers, cheapest first.

**Layer 1 — the shape.** `brain` splits every service into a thin entry point that owns the
clock and a worker value that has no way to obtain one:

```go
// internal/brain/capture.go
type CaptureService struct {
    clock ports.Clock
    run   captureRunner        // every port EXCEPT the clock
}

// The only ports.Clock read in the whole package.
func (s *CaptureService) Capture(ctx context.Context, in CaptureInput) (CaptureResult, error) {
    return s.run.at(ctx, in, s.clock.Now())
}

type captureRunner struct {
    ids    ports.IDGen
    units  ports.UnitRepo
    embeds ports.EmbeddingRepo
    lex    ports.LexicalSearch
    rels   ports.RelationRepo
    log    ports.DecisionLog
    llm    ports.LLMProvider          // capture_processing
    judge  ports.LLMProvider          // relation_evaluation
    embed  ports.EmbeddingProvider
    index  *Index                     // D5
}

func (r captureRunner) at(ctx context.Context, in CaptureInput, now time.Time) (CaptureResult, error)
```

`captureRunner` has no `Clock` field and no method that returns one. Every function below `at`
takes `now time.Time`. A second instant requires *adding a field*, which is a reviewable act,
not a one-line slip. `RecallService` takes the same shape.

**Layer 2 — an L2 tree scan, in the shape m1a already shipped twice.** No non-test file under
`internal/brain/**` may contain `time.Now(`: the instant enters through `ports.Clock` or not at
all. This mirrors `store_no_direct_clock_read_test.go` exactly, so the pattern and its
temporary-break discipline already exist.

**Layer 3 — an L2 AST scan that catches the actual bug class.** Walk every non-test file under
`internal/brain/**` with `go/ast` and fail on either:

1. **more than one `Now()` call expression in a single file**, or
2. **any `Now()` call inside a `FuncDecl` whose signature already takes a `time.Time`
   parameter.**

(2) is the one that matters: a helper that was *handed* the instant and reaches for a fresh one
anyway is precisely R9. It is ~90 lines of L2, it runs in the fast loop, and it is
mechanizable without heuristics because both facts are syntactic.

**Its honest limitation, announced in its own doc comment** (the precedent is
`golden_sets_test.go:164-176`, which announces its own literal-substring proxy): it does not
catch two `Now()` reads in two different files that belong to one logical operation. Nothing
short of a call-graph analysis does. What it catches is the dominant form, and it converts
proposal R9 from "a review property, stated so it is known to be one" into a gate — which is
`docs/06-harness.md` §6's own precedence rule: if a rule can be an automated gate, it is a gate.

**The pipeline, with each step's home.** `spec.md` R4.1–R4.7 state what must be true; this is
where each decision lands:

```
CaptureService.Capture(in)
  now := clock.Now()                                          brain, ONCE
  ├ prompt := classify.BuildPrompt(in.Text, beliefs, now)     core/classify   pure
  ├ resp   := llm.Complete({Prompt, Task:"capture_processing"})  ports.LLMProvider
  ├ c, err := classify.Decode(resp.Text)                      core/classify   I14
  ├ route on c.Kind                                            brain           D9
  ├ u      := classify.ToUnit(c, ids.New(), now, priors)      core/classify   I18
  ├ units.Create(u)                        ports.UnitRepo  →  units_fts syncs by trigger
  ├ ev     := embed.Embed({u.Content})                        ports.EmbeddingProvider
  ├ embeds.Put({u.ID, ev.Model, ev.Vector, now})              ports.EmbeddingRepo   D8
  ├ index.Add(u.ID, normalized)                               brain
  ├ cand   := recallSvc.candidates(u.Content, exclude u.ID)   D5
  ├ jr     := judge.Complete({..., Task:"relation_evaluation"})  ports.LLMProvider
  ├ j, _   := relation.DecodeJudgment(jr.Text)                core/relation   D7
  ├ v      := relation.Decide(*j.Confidence, thresholds)      core/relation   I08
  ├ rels.Upsert(...)                                          ports.RelationRepo   I07
  └ log.Record(...) at every step with an effect              ports.DecisionLog    I12
```

`classify.ToUnit(c Classification, id string, now time.Time, p Priors) (unit.Unit, error)` is
pure and is where I18 lands: `CreatedAt = UpdatedAt = LastTouchedAt = now`, `EventAt = c.EventAt`,
`DueAt = c.DueAt`, `Status = unit.StatusPool`, `Confidence = nil` (Q2), `Weight`/`WeightDecayRate`
from `c` or from D3's priors. It returns an error only when `c.Kind` maps to no `unit.Type`, so
the caller cannot forget to check.

**Two properties the scripted fake gives for free**, worth naming so tests are written to expect
them. One capture makes **two** `Complete` calls, so a test scripts two case ids; and the judge
is **not called when the candidate list is empty**, because asking a model to compare against
nothing is a wasted call and a wasted `decision_log` row. `fakeprovider`'s unscripted-call
failure (`fakeprovider.go:66-69`) enforces the second for free.

**The timezone.** Doc 02 §5 injects "local date + user timezone" and `docs/06-harness.md` §2
forbids reading it from the OS **inside `core`**. There is no timezone key in `nooma.yml` and no
column for it (§1.3). Rather than inventing config: **the user's zone travels inside the
`time.Time` the clock returns.** The real `Clock` adapter returns `time.Now()`, whose `Location`
is `time.Local`; `BuildPrompt` renders `now.Format(...)` and `now.Location().String()`. The core
reads no environment, the instant is still singular, and no config schema moves. The reversal
criterion is explicit: a server-hosted vault whose process zone is not the user's — which is
multi-tenant, deferred out of v1 by ADR-0005. Test clocks fix a `Location` so prompt assertions
are stable.

**`BuildPrompt`'s belief parameter is always empty in M1**, and it exists anyway.
`classify.Belief{Facet, Content string}` is a projection of `self_beliefs`; nothing in M1 reads
that table (`derive` is M2, seeding M4), so `brain` passes `nil`. The parameter is what makes
M2's capture→derive→inject cycle a wiring change instead of a prompt rewrite. Open check-ins
(M3) get no parameter at all, because unlike beliefs there is no table to read them from yet —
an empty slice and an absent concept are different, and the design does not blur them.

### D5 — Two legs, one fusion, and I02 enforced **once**, at the `LiveByIDs` boundary

The symbols I21 pins (`i21_…:60-96`) and the ones this design adds around them:

```go
package recall

type VectorQuery struct {
    Model  string      // I21 — string kind, exported. Which model's space this query lives in
    Vector []float32   // unit-normalized
    K      int
}

type VectorIndex struct {
    Model   string       // I21 — one index, one model
    IDs     []string
    Vectors [][]float32  // parallel to IDs; every row len == Dim
}

type Scored struct{ ID string; Score float32 }

func NewVectorIndex(model string, ids []string, vecs [][]float32) (VectorIndex, error)
func Search(idx VectorIndex, q VectorQuery) ([]Scored, error)   // D6

func Normalize(v []float32) ([]float32, error)   // ErrZeroVector
func Tokenize(text string) []string              // the lexical leg's decision, D5 below
func Fuse(lists ...[]string) []string            // RRF, ADR-0010

const (
    RRFK            = 60   // ADR-0010, doc 02 §13 — already listed
    RecallTopK      = 20   // NEW §13 row
    WeightVector    = 1.0  // NEW §13 row — ADR-0010:48 requires the weights be constants
    WeightLexical   = 1.0  // NEW §13 row
)
```

**The vector leg.** `Search` over an index the vault-open path loaded. **One index, one model** —
I21's own "Assumed shape" comment says so in as many words (`i21_…:52-57`), and `Search` returns
`ErrModelMismatch` when `q.Model != idx.Model`. A two-model vault is two indexes, produced by
`EmbeddingRepo.LoadIndex(ctx, model)`.

> **Conflict surfaced — C1. `spec.md` R2.3 asks for a single index holding two models' entries.**
> Its wording is "when `VectorQuery.Model` names one model and `VectorIndex` holds entries from
> more than one model … the top-K selection considers only entries whose model matches". That
> requires a per-entry model, which is exactly the mixed index I21's anchor comment says must
> never exist. **This design follows the anchor**, because a conformance test written before the
> implementation is not reshaped to fit a spec — `docs/06-harness.md` §4 gives two exits, fixing
> the code or changing doc 02 plus its ADR, and neither is "widen the index". R2.3's *scenario*
> is satisfied unchanged on this shape: build a model-`a` index and a model-`b` index where a
> model-`b` entry outscores every model-`a` entry, query with model `a`, and assert the
> model-`b` entry never appears. **R2.3's wording needs a follow-up correction**; flagged here per
> `CLAUDE.md` non-negotiable #1 rather than silently satisfied by a different mechanism.

**The lexical leg**, split across the doc-06 §1 line rather than placed by instinct:

- `recall.Tokenize(text) []string` is **core**. What words the lexical leg searches for is a
  recall-quality decision the golden corpus pins, and it is pure.
- Rendering those tokens as FTS5 `MATCH` syntax is **`store/sqlite`**. `docs/06-harness.md` §1
  says "the cognitive core does not know that SQLite … exist", and emitting FTS5 query syntax is
  knowing it exists. The adapter quotes each token and joins with `OR`.

That split also removes a whole runtime failure class: raw user text handed to `MATCH` is an
FTS5 *query expression*, and `what about "ana"?` or a trailing `AND` is a syntax error, not a
zero-result search.

```sql
SELECT u.id
FROM units_fts
JOIN units u ON u.rowid = units_fts.rowid
WHERE units_fts MATCH ? AND u.status = 'pool'
ORDER BY bm25(units_fts)
LIMIT ?
```

`ORDER BY bm25(...)` **ascending**, because ADR-0010:20-22 records that `bm25()` returns
negative values — more negative is a better match. Not executed in this session; PR 9's L3 test
is what confirms it, and that test is written before the query.

**Where I02 is enforced, and why in exactly one place.** Both legs return ids. `brain` then
calls `ports.UnitRepo.LiveByIDs(ids)`, which already filters positively on `status = 'pool'`
(`sqlite/unitrepo.go:91,94`) and is pinned by a contract case answered twice. That single call
materializes the units the fused ranking needs anyway — for the response and for the judge's
prompt — so the filter costs nothing extra.

| Option | Verdict |
|---|---|
| Index only `status='pool'` units | Rejected — makes the in-memory index a second source of truth for `units.status` with no transactional link to the column. The first missed eviction surfaces a superseded unit in a live read, which is the exact failure I02 exists to prevent, reintroduced by its own mitigation |
| Filter inside each leg separately | Rejected — two filters is two places to get I02 wrong, and the SQL leg's own `status = 'pool'` predicate stays anyway (it keeps the leg's K meaningful), so this would be three |
| **One filter at the `LiveByIDs` boundary, for both legs, before fusion** (chosen) | I02 holds for the whole mechanism by one shipped, contract-tested call. The SQL predicate above is belt-and-braces, not the enforcement |

The accepted cost, named: a non-live id returned by either leg consumes one of its K slots, so
the fused list can be shorter than K. In Phase B this is unreachable — nothing in Phase B ever
leaves `pool` (no archive, no consolidation; `SetStatus` has no Phase B caller) — and when M2's
`archive` lands it becomes a real, bounded shrink, not a correctness bug.

**Fusion.** `Fuse` is variadic because ADR-0010's formula generalizes and because "documents
appearing in only one list contribute a single term" is naturally expressed over N lists; Phase
B always passes exactly two, vector first.

**Ties break deterministically, and this is not optional.** `score(d) = Σ w_i/(k + rank_i(d))`
produces exact float ties for symmetric cases, and `make test` runs `-shuffle=on`
(`Makefile:48`). **Rule: higher score first; on a tie, the id that appeared earliest across the
lists in argument order; on a further tie, lexicographic by id.** `spec.md` §9 leaves this to
design; an unspecified tiebreak is the bug the shuffled suite finds on a Tuesday.

**Two new §13 knobs beyond the one the spec names, and one of them is this design's own scope.**

- `recall_top_k = 20` — `spec.md` R2.5 requires the row and requires one K for both legs. 20
  rather than 5 because with a small K the two legs frequently share at most one id and RRF
  degenerates into concatenation, which makes the fusion golden prove nothing; and because
  ADR-0012's per-query cost is O(vault), not O(K), so K is nearly free on the vector side.
- `dedup_candidate_k = 5` — **invented by this design**, declared as such under
  `docs/06-harness.md` §6's precedence rule. ADR-0010's own mitigation for RRF's magnitude loss
  is that the judge sees *full candidate content* (`0010:65-69`); with `RecallTopK = 20` per leg
  the fused list can hold 40 units, and 40 full unit bodies in one prompt is a real latency and
  cost. An unbounded judge prompt is a defect discovered in production, so the bound is named
  now. It lives in `core/relation`, because it bounds what the judge is asked about.

Both numbers are unmeasured, and §13 is exactly the table where that is admitted and later
corrected. `WeightVector`/`WeightLexical` are 1.0 and exist because ADR-0010:48 requires each
list's relative weight to be a named constant; `docs/06-harness.md` §7 then requires them in
§13, so §13 gains four rows in PR 8b.

### D6 — The proximity loop lives in `core/recall`, over a row-per-unit layout, and its cost is ADR-0012's own table plus 2 %

```go
func Search(idx VectorIndex, q VectorQuery) ([]Scored, error)
```

Pure Go, stdlib only, no `context`, no ports — testable at L1 with no database, which ADR-0012
names as its own payoff (`0012:82-83`). Three refusals rather than three silent wrong answers:

- `ErrModelMismatch` when `q.Model != idx.Model` — I21's behavioural half, made the only way to
  reach the loop at all.
- `ErrDimMismatch` when `len(q.Vector) != len(idx.Vectors[0])` — a shorter dot product would
  still produce a number.
- `ErrZeroVector` from `Normalize` — a zero vector has no direction, and dividing by its norm
  yields `NaN`, which sorts arbitrarily and silently.

**Layout.** `Vectors [][]float32`, one row per unit, validated ragged-free by `NewVectorIndex`.
The alternative is a flat `[]float32` with a stride: one allocation, better locality, likely
faster. It is not chosen, and the reason is that **ADR-0012 does not name the layout its
measurement used** (§1.3), so the flat form buys an unquantified speedup at the cost of a stride
that can desynchronize from the id slice with nothing checking it. The row-per-unit form matches
the shape rows arrive in from SQL and makes a ragged row a construction-time error. ADR-0012
already fixed the reversal path: "the replacement lands behind the recall interface, so the
cognitive core does not change" (`0012:120-121`) — `Search`'s signature does not move.

**Cost, at the vault sizes the ADR itself names** (`0012:45-55`), with this layout's overhead
recomputed here rather than assumed:

| Units | ADR's vector RAM | This layout adds | Total | ADR's p95 |
|---|---|---|---|---|
| 1,000 | 3 MB | ~76 KB | ~3.1 MB | 0.88 ms |
| 10,000 | 29 MB | ~760 KB | ~30 MB | 8.30 ms |
| 50,000 | 146 MB | ~3.8 MB | ~150 MB | 43.12 ms |

The overhead is a 24-byte slice header plus a 16-byte string header and a 36-byte UUID per unit
— 76 bytes, against 3,072 bytes of `float32` at dim 768. **2.5 %.** Immaterial against a
decision whose stated ceiling is a memory number (`0012:91-94`), and stated numerically so
nobody has to wonder.

Index load is paid once at vault open — 42 ms per 10,000 vectors (`0012:55`) — and never per
request, which `spec.md` R3.2 states as a MUST NOT.

**Normalization is applied at the storage boundary, by the adapter, calling the core's pure
function.** This reconciles two artifacts that disagree:
`m1a-substrate/design.md` D7 says normalization "is a pure function in `core/recall` applied
before the store writes"; `spec.md` R3.1 says it "is a storage-boundary obligation, not
something callers are trusted to have already done". Both are satisfied by
`internal/store/sqlite` importing `internal/core/recall` and calling `recall.Normalize`
immediately before encoding the BLOB — the store→core import direction is already established
(`sqlite/unitrepo.go:14`). `brain` does **not** normalize: one place, per
`docs/06-harness.md` §7. Normalizing an already-normalized vector is a no-op within float
tolerance, so a future second caller cannot corrupt anything by being careful.

**The BLOB codec** is `math.Float32bits` + `binary.LittleEndian`, matching `0002:78`'s own
comment, in `internal/store/sqlite`. `dim` is written as `len(vector)`: the *column* keeps the
redundancy its DDL calls deliberate (`0002:77`), the *Go value* does not, which is Phase A's
own `EmbedResponse` reasoning (`provider.go:53-56`) applied consistently.

### D7 — The judge: three bands, a nil-row fallback, an upsert, and a `duplicate` that is recorded rather than acted on

```go
package relation

const (
    DefaultMinConfidenceToPersist = 0.30   // pinned to 0002:33's DEFAULT 0.3
    DefaultMinConfidenceToSurface = 0.50   // pinned to 0002:34's DEFAULT 0.5
    DedupCandidateK               = 5      // D5
)

type Thresholds struct{ Persist, Surface float64 }

// Resolve is Q1's closed answer: nil means relation_thresholds holds no row
// for this type, and relation type is open text, so no seed can be exhaustive.
func Resolve(row *Thresholds) Thresholds

type Verdict int
const (
    Discard   Verdict = iota   // conf <  Persist                    — I08
    Uncertain                  // Persist <= conf < Surface          — I09, storage half
    Asserted                   // conf >= Surface
)
func Decide(confidence float64, t Thresholds) Verdict

type Outcome string
const (OutcomeNew Outcome = "new"; OutcomeDuplicate = "duplicate"; OutcomeRelated = "related")

type Judgment struct {
    Outcome      *Outcome
    TargetUnitID *string
    Type         *string     // open text — doc 02 §4
    Strength     *float64
    Confidence   *float64
    Degradations []classify.Degradation
}
func DecodeJudgment(raw string) (Judgment, error)
```

**Where threshold resolution lives** (`spec.md` §9 leaves it to design): the *lookup* is
`brain`'s — it is a repository read — and the *fallback* is `core/relation`'s pure `Resolve`.
`brain` reads the row (or gets none) and hands `*Thresholds` to `Resolve`; the core never
touches `relation_thresholds`. That keeps R5.1's purity MUST intact and puts Q1's constants
where Q1 said, in exactly one place.

Phase B reads `relation_thresholds` and always finds it empty, because migration 0002 seeds no
rows (§1.3) and nothing writes it before M5. The read exists anyway, because a fallback that is
never exercised against a real absent-row lookup is a fallback nobody has run.

**Boundary semantics, stated because doc 02 contains two readings.** §4 says "below this, it is
not even stored" (persist) and "above this, it is asserted without asking" (surface) — a literal
reading makes `conf == Surface` neither. The same paragraph then writes the band as
`[persist, surface)`, which partitions the line with no gap. **The band notation wins:**
`conf >= Surface` is `Asserted`. L1 hammers both boundaries exactly, which is also what
`spec.md` R5.1's scenario demands.

**Uncertainty is not persisted, because it is derivable.** `relations` has no uncertain column
(§1.3). A relation is uncertain when its stored `confidence < Surface` for its type — which is
why Q1's constants matter beyond capture: M3's digest resolves the same thresholds at read time
to decide what to ask about. No schema change, no migration, and I09's storage half is satisfied
by storing the confidence honestly.

**I07 is an upsert, in SQL.** `INSERT INTO relations (…) VALUES (…) ON CONFLICT
(from_unit_id, to_unit_id, type) DO UPDATE SET strength = excluded.strength, confidence =
excluded.confidence` against the constraint at `0001:39`. A second judge run over the same
triple updates rather than erroring or duplicating.

**Direction is not canonicalized.** `(A,B,same_topic)` and `(B,A,same_topic)` are two distinct
rows under that constraint, and doc 02 §4 says relations are *directed edges*. M1 writes exactly
one row, in the direction the judge names (new unit → candidate). Canonicalizing would require
knowing which relation types are symmetric, and `type` is open text — the same fact that killed
seeding in Q1. The cost is real and named: M2's `connect` could later produce the mirror edge as
a second row. Recorded as a risk with an M2 owner, not solved here.

**What `duplicate` does: it is recorded and not acted on.** By the time the judge answers, the
unit is already persisted (D8's ordering). The tempting responses are all out of bounds:
superseding the new unit would overload `superseded`, whose one documented meaning is "replaced
insight" (doc 02 §1, and I20 is specifically about insights); reviving the existing one is a
weight write on the decay path, which proposal §3.3 lists as an explicit M2 non-goal. **So a
`duplicate` verdict writes a `duplicate`-typed relation from the new unit to the existing one,
and a `decision_log` row saying plainly that the duplicate was recorded and not merged.** This
is Q3a's principle applied to a case Q3a did not cover: a system that silently merged two
memories with a mechanism it cannot yet undo is worse than one that records the observation and
says so.

**Discards are logged, though they have no effect.** `spec.md` R5.4 requires it and doc 02 §11's
own framing supports it: the glass box exists to answer "why did you do that", and "I considered
linking these two and decided not to" is exactly the question a user asks about a link that is
not there. I12's MUST is about effectful decisions; logging a non-effectful one does not weaken
it.

**`DecodeJudgment` reuses `classify.Salvage`.** The judge's response is JSON from the same class
of provider with the same failure modes, and re-implementing the salvage loop would give one
project two tolerant decoders that can disagree. `core/relation` imports `core/classify` — legal
under `core-purity`'s prefix allow (`.golangci.yml:62`), and the direction matches the pipeline:
the judge runs after classify. The alternative — a fourth core package `jsonsalvage` — adds a
line to `docs/06-harness.md` §1's tree and a package for one function, when both consumers are
two steps of the same pipeline and I14 is documented as *classify's* invariant. **Reversal
criterion, named:** a third consumer outside the capture pipeline. M2's `derive` is one, and it
is one milestone away, so this is a decision with a short expected life and that is fine.

### D8 — The unit is persisted **before** it is embedded, the half-synced unit is real, and it is named rather than prevented

The failure: the unit row commits, then `EmbeddingProvider.Embed` fails or the embedding write
fails. The unit exists, is lexically findable (the FTS trigger fired inside the same
`INSERT`, `0002:118-120`), and is invisible to the vector leg.

The alternative that eliminates it is genuinely available and was worked through: `Embed` needs
only the *text*, not the unit id, so classify → embed → **one transaction writing both rows**
would make a unit and its embedding atomic.

**It is rejected, on doc 02's own product rule.** §5 says "Nooma captures with what it has,
decides on its own, leaves an auditable trace, and only asks when ambiguity blocks it"
(`02:142-144`). Under the atomic ordering, a local Ollama being down means **the user's thought
is refused**. That converts a degraded secondary index into a lost memory, in a product whose
first promise is that capture never fails. A personal brain must not decline to remember
something because a search index was unavailable.

| Option | Verdict |
|---|---|
| Classify → embed → one transaction for both rows | Rejected — an embedding-provider outage refuses the capture entirely. Trades a degraded index for a lost thought |
| Persist → embed → write embedding, gap unmentioned | Rejected — this is the failure the brief says must be named rather than discovered |
| **Persist → embed → write embedding, gap named, logged, returned, and repairable** (chosen) | Capture survives the outage; the gap is in the glass box, in the caller's result, and has an owner |

Three mechanisms, none of them a new port with no consumer:

1. **The result says so.** `CaptureResult` carries `Embedded bool`. A caller — Phase C's HTTP
   route, the CLI — can tell "stored and searchable" from "stored, semantic search pending".
2. **The glass box says so.** A failed embed writes `capture.embedding.failed` with the provider
   error in `context`. Doc 02 §11's pull surface then already shows it.
3. **The repair already exists on the roadmap.** ADR-0003's amendment turns `reindex` into "an
   ordinary `UPDATE` loop … resumable and incremental" (`0003:83-85`) — the same loop that
   backfills a missing row. And `nooma doctor` is *already documented* as checking
   "units↔embeddings↔fts consistency" (`docs/03-data-model.md:306-307`), so detection is an
   existing v1 promise, not a new obligation this design invents.

This design ships **no** consistency-query method in Phase B, deliberately: `UnembeddedLive` or
similar would be a port method whose only caller is a test, which is the shape
`m1a-substrate` D7 rejected for `TranscriptionProvider`. The obligation is recorded for whoever
ships `doctor`'s consistency check.

**FTS needs no application code at all.** Migration 0002's three triggers maintain `units_fts`
from `units`, they fire on the `INSERT` inside `UnitRepo.Create`, and `test/integration/fts5_search_test.go`
proves all three run against a real vault, each with a watched-RED transcript. Phase B adds a
*query*, never a write. This is a genuine saving and it is worth naming, because a plan that
budgets for FTS synchronization budgets for work that was done in the harness.

**`decision_log` rows are written outside any transaction, after the fact.** A crash between the
unit commit and the log write loses the log entry, not the unit — the correct direction to fail
in for an audit trail that must also record refusals, which by definition have no transaction to
join.

**The new ports, and why each method has a real caller:**

```go
// internal/ports/embeddingrepo.go
type Embedding struct{ UnitID, Model string; Vector []float32; At time.Time }
type EmbeddingRepo interface {
    Put(ctx context.Context, e Embedding) error                            // upsert on unit_id (PK)
    LoadIndex(ctx context.Context, model string) (recall.VectorIndex, error)
}

// internal/ports/lexicalsearch.go
type LexicalSearch interface {
    SearchLexical(ctx context.Context, tokens []string, k int) ([]string, error)
}

// internal/ports/relationrepo.go
type RelationRepo interface {
    Upsert(ctx context.Context, r relation.Relation) error
    ByUnit(ctx context.Context, unitID string) ([]relation.Relation, error)
    ThresholdsFor(ctx context.Context, relType string) (*relation.Thresholds, error)  // nil, nil when absent
}

// internal/ports/decisionlog.go
type DecisionLog interface {
    Record(ctx context.Context, d Decision) error
    Since(ctx context.Context, t time.Time, limit int) ([]Decision, error)
}
```

`Put` upserts because `unit_id` is the primary key and M6's `reindex` must replace a row.
`ByUnit`'s consumer is Phase C's read-only units route, one phase away. `Since`'s consumer is
doc 02 §11's own "Pull: everything is recorded and explorable in the activity UI" — an audit log
with no read path is a write-only log, so the read half is part of the port's definition, not a
test affordance. `ThresholdsFor` returning `(nil, nil)` for an absent row is what feeds
`relation.Resolve`, and it is where Q1's "no row" case actually originates.

**No `Delete*`-prefixed method anywhere**, which keeps `test/conformance/i03_units_never_deleted_test.go`'s
strengthened prefix set (`{Delete, Remove, Purge, Drop, Destroy}`, `i03:88`) satisfied. That
check reflects over `ports.UnitRepo` only, but the convention is the project's and the tree scan
for `DELETE FROM units` covers every new file automatically.

**Phase A's files stay closed** (`spec.md` R6.3): every port above is a **new file** in
`internal/ports`, every implementation a **new file** in `internal/store/sqlite`, and
`unitrepo.go` / `provider.go` / `sqlite/unitrepo.go` are not edited. `store_api.golden` gains
additions only, regenerated with `make store-api-golden` — a different target from
`make schema-golden`, and forgetting it turns `make check` red immediately.

### D9 — `decision_log`'s contract, and exactly what Q3a's refusal writes

```go
// internal/ports/decisionlog.go
type DecisionAction string

const (
    ActionCaptureClassify          DecisionAction = "capture.classify"
    ActionCaptureUnparseable       DecisionAction = "capture.classify.unparseable"
    ActionCaptureUnclassifiable    DecisionAction = "capture.classify.unclassifiable"
    ActionCaptureDiscarded         DecisionAction = "capture.discarded"
    ActionCaptureUnitCreated       DecisionAction = "capture.unit.created"
    ActionCaptureEmbeddingFailed   DecisionAction = "capture.embedding.failed"
    ActionCaptureHookDeferred      DecisionAction = "capture.hook.deferred"
    ActionCaptureDedupJudged       DecisionAction = "capture.dedup.judged"
    ActionRelationPersisted        DecisionAction = "relation.persisted"
    ActionRelationDiscarded        DecisionAction = "relation.discarded"
    ActionRelationDuplicateRecorded DecisionAction = "relation.duplicate.recorded"
)
func AllDecisionActions() []DecisionAction

type Decision struct {
    ID         string
    Action     DecisionAction
    Rationale  string           // plain English, doc 02 §11
    Context    json.RawMessage  // JSON; the column defaults to '{}'
    OccurredAt time.Time
}
```

A **defined string type with a closed vocabulary**, following `unit.Status`'s pattern
(`status.go:14`, `:33-35`) for the same reason: `Action` cannot be a bare literal at a call
site, the whole vocabulary is greppable across Go and SQL, and `'capture.classify'` is exactly
what `0001:97`'s own DDL comment names. `AllDecisionActions()` is a function returning a fresh
slice, never an exported var — Phase A's D1 reasoning, unchanged.

The vocabulary lives in `internal/ports`, beside the port that consumes it, because
`internal/brain` cannot host it (`ports` would then import `brain`, a cycle) and `internal/core`
must not (nothing in core knows an action exists).

**I12's "never from `internal/core`" half is already lint-enforced, and this design does not add
a test for it.** `depguard`'s `core-purity` allow-list is `$gostd` plus
`github.com/rengo/nooma/internal/core` (`.golangci.yml:60-62`) — `internal/ports` is not on it,
so no file under `internal/core/**` can import the `DecisionLog` port at all. Verified, and
worth naming: it is one of the few places where an invariant is free.

**Reads are not logged.** Doc 02 §11 says "every automatic decision with an **effect**". A
recall changes nothing in the vault, and logging every query would drown the surface whose whole
point is that "relief must not turn into an auditing chore" (`02:270-272`). There is no
`recall.answered` action.

**Q3a's honest refusal, written out.** `spec.md` R4.6/R4.7 state what must be true; this is the
row.

*A `timer` or `recurring_reminder` classification:*

- **zero `units` rows.** Doc 02 §8 is titled "Ephemeral timers — infrastructure, NOT memory" and
  states in bold "A timer is NEVER a unit" (`02:210-212`). An unarmed timer is still a timer.
  This also keeps I04 true rather than untested.
- **zero `timers` rows, zero `triggers` rows** — proposal §3.3's explicit non-goal.
- **exactly one `decision_log` row**: `action = capture.hook.deferred`,
  `rationale = "classified as timer; M1 arms no timer and cannot fire one, so nothing was scheduled"`,
  `context = {"kind":"timer","classification":{…},"reason":"prospection_not_implemented","milestone":"M3"}`.
  The classification is embedded verbatim so the record is a truthful account of what was
  understood, not merely of what was refused.
- **a caller-visible refusal**: `CaptureResult{Stored: false, Deferred: &Deferred{Kind, Message}}`,
  with `Message` in plain words. Phase C's route and CLI render it; Phase B's job is that the
  refusal is *representable and distinguishable* from success, which a bare `error` would not be
  — a `timer` classification is not a failure.

> **Conflict surfaced — C2.** `spec.md` R4.6 states as a **MUST NOT** that capture writes a
> `units` row for these two types; `spec.md` §9's open-items list then says the spec "does not
> decide it". The spec is internally in tension. This design decides it — **no unit** — on doc
> 02 §8's own bold sentence, which is the document that governs behavior, and pins it with a new
> L2 test (below). Recorded rather than resolved silently.

*An ambiguous person reference (`person_ref_status: "ambiguous"`):*

- **a `pool` unit is created**, never `incomplete` — Q3a's closed reasoning: an `incomplete` unit
  is invisible to every live read surface (I02) and, with `expire_incomplete` in M2, immortal.
- **two `decision_log` rows**, because two decisions happened: `capture.unit.created`, and
  `capture.hook.deferred` with `context.kind = "ambiguous_person_ref"` and a rationale saying the
  unit was stored complete rather than held, because nothing can promote it before M2.

**A new conformance test this design adds, declared as its own scope.** `docs/06-harness.md` §4's
table lists I04 ("A timer is never a unit") and no test for it exists — glob confirms. Phase B is
the first change that can produce a `timer` classification, so it is the first change where I04
is testable, and Q3a's whole argument rests on it. PR 10c adds
`test/conformance/i04_timer_never_a_unit_test.go`. No doc 06 edit is needed: I04 is already in
the §4 table (`06:175`).

**I06 is out of scope and says so.** `spec.md` R4.7 requires the suite to name it explicitly
rather than pass in silence. The conformance file carries a paragraph stating that I06 ("an
`incomplete` unit has no embedding until promoted") is not exercised because Phase B creates no
`incomplete` unit, so "no test fails" must not be read as "I06 holds" — the same honesty
`core-coverage.sh:102-105` shows when it reports "armed but vacuous" instead of a bare OK.

### D10 — Retiring `pending-red`: both symbols land together, and the **ruleset context must drop first**

`pending_symbols.txt` holds exactly two lines, `recall.VectorQuery` and `recall.VectorIndex`
(`:6-7`), and `i21_vector_search_filters_on_model_test.go:1` is the last `pendingimpl` file.
So the PR that creates those two symbols is the last promotion in the project's history, and it
retires the gate.

**Both together, no partial split.** A partial promotion is *mechanically* available —
`pending-red.sh` checks line by line (`:31-67`), so one could create `VectorQuery`, delete its
line, and leave the tag on. It is rejected: the test file references both symbols, so it cannot
be untagged until both exist, and the intermediate state ships a conformance test still tagged
`pendingimpl` for a symbol that already exists — the "instructions already carried out" defect
`m1a-substrate` D8 named. Stated so a reviewer knows the split was considered, not missed.

**The retirement, as a diff.** Eleven edits, ten of them in the repository:

| # | Edit | Located at |
|---|---|---|
| 1 | Add `internal/core/recall/{vector.go,fuse.go,tokenize.go}` + L1 tests | new |
| 2 | Delete `internal/core/recall/doc.go`'s "Pending conformance anchor" paragraph | `recall/doc.go:5-14` |
| 3 | Drop `//go:build pendingimpl` from I21's test, and rewrite its "Promotion:" paragraph, which instructs the reader to do what the PR is doing | `i21_…:1`, `:36-43` |
| 4 | Add I21's **behavioural** test — the anchor proves expressibility only, by its own admission | `i21_…:45-50` |
| 5 | Delete `test/conformance/pending_symbols.txt` | — |
| 6 | Delete `scripts/pending-red.sh` | — |
| 7 | `Makefile`: drop `pending-red` from `check-all`, delete the target, fix the header comment | `Makefile:39`, `:93-95`, `:13` |
| 8 | `.github/workflows/ci.yml`: delete the `pending-red` job | `ci.yml:107-115` |
| 9 | `docs/06-harness.md`: §6's table row and §8 point 5 move to past tense — the harness *was* proven by watching it fail, and the gate retired when its last anchor was promoted | `06:245`, `:349`, `:355` |
| 10 | `CLAUDE.md`'s Workflow section, which lists `pending-red` among `check-all`'s gates | `CLAUDE.md`, Workflow |
| 11 | `.golangci.yml`'s `run.build-tags` comment, which explains a `pendingimpl` exclusion that no longer has a file | `.golangci.yml:20-24` |

While there, fix the stale comment at `tree_scan_test.go:3-7`, which still says I03 is
"`//go:build pendingimpl` until PR 3 promotes it" — PR 3 promoted it (§1.2).

**The sequencing, which is the part no artifact records correctly.**
`m1a-substrate` D8 and its §7 risk 5 say the required status context must drop "or every future
merge to `main` blocks on a check that never posts". That is true and it **understates the
problem by one PR**:

> Steps 6 and 8 are in PR 8a's own diff, so the `pending-red` job does not exist on that PR's
> head and **never posts a status**. If `pending-red` is still a required context in `main`'s
> ruleset, **PR 8a itself cannot merge** — it blocks on a check its own diff deleted. It is not
> only future merges; it is this one.

So the order is:

1. **Remove `pending-red` from `main`'s branch ruleset required status checks, on GitHub.** Then
   and only then,
2. merge PR 8a.

Between (1) and (2) `main` is briefly unprotected against a red `pending-red` — harmless,
because the gate's entire purpose expires at (2), and because no other PR should be in flight
across that window (see §5's recommendation that PR 8a goes **first** in Phase B, which makes
the window empty by construction).

Neither step is expressible in a Makefile or a workflow file. The operator should confirm the
current required contexts before acting rather than trusting this document:
`gh api repos/rengo/nooma/rulesets` and then the ruleset's own id. **This design could not run
that command** (§1.2) and does not claim to know the answer.

**The golden-corpus red/green pairs travel with their own PRs.** `casesDirMustBeEmpty` is
`{recall: true, classify: true, llm: false}` (`golden_sets_test.go:259-263`) and
`assertCasesDirEmptiness` `t.Errorf`s on the first non-`.gitkeep` entry in a directory still
marked `true` — untagged, inside `make check`'s fast loop. So adding the first case file to
`classify/` or `recall/` turns `main` red unless the same PR flips that directory's entry. PR 7c
flips `classify`; PR 8c flips `recall`. Whichever lands second leaves all three entries `false`,
at which point the map has stopped expressing an asymmetry: **that PR replaces it with a plain
"every `cases/` must be non-empty" assertion** rather than shipping a rules-as-data structure
whose rule is now uniform.

### D11 — The ≥ 90 % floor is met by construction, and the corpus tests are deliberately **L2**

The floor's mechanics decide more than its number. `make cover` runs
`go test -coverprofile -coverpkg=./internal/core/... ./internal/core/...`
(`core-coverage.sh:56`), so **only test binaries under `internal/core/...` contribute**. An L2
test in `test/conformance` that drives `classify.Decode` over the whole corpus adds exactly
zero. It never runs in `make check` (`Makefile:36`), so the fast loop cannot catch a shortfall.

**The corpus tests are L2, and that is not a workaround.** `depguard`'s `core-purity` denies
`os` to `internal/core/**` (`.golangci.yml:80-81`) with **no `$test` selector on its `files:`
list**, so as written an L1 test inside `internal/core/classify` cannot `os.ReadDir` the corpus.
The split this forces is the one `nooma-testing`'s own decision gate would pick anyway:

- **L1** — hand-written tables over payload literals *in the test file*. No filesystem, no
  `os`, every arm reachable. **These carry the entire numerator.**
- **L2** — `test/conformance/i14_classify_field_degrades_to_null_test.go` drives the same pure
  functions over the real `testdata/classify/cases/` corpus, where reading files is ordinary.
  This is the invariant proof and the regression detector.

The design does not rest on the unverified lint detail (`m1a-substrate` §7 risk 7 flags it as
un-executed and this session could not execute it either): if `depguard` covers core test files,
the split is mandatory; if it does not, the split is still right, because a test that touches
the filesystem and could have been pure is misplaced by `docs/06-harness.md` §3's own rule.

**Four structural properties, not four reminders:**

1. **The decoder is table-driven.** One `fieldSpec{name, assign}` per wire field, and one
   salvage loop over the table. Statement count is **O(1) in the number of fields**, not
   O(fields): adding a field adds a table row (data, zero statements) and a three-line assigner.
   This is the single biggest lever — it turns "13 fields × 3 malformation shapes" from 39
   branches into one loop plus thirteen trivial functions.
2. **Enum decoding is one generic function.** `decodeEnum[T ~string](raw json.RawMessage, all []T) (*T, error)`
   serves `Kind` and all six orthogonal fields, so there is one set of arms, not seven. The six
   orthogonal vocabularies therefore need `AllX()` and **no `ParseX`** — fewer exported symbols,
   less surface to cover, and the vocabulary still closed.
3. **No unreachable arm.** No `default: panic(...)` over a closed vocabulary, because an
   unreachable statement is an uncovered statement by construction. Every unmatched input
   returns an error a table row can trigger. This is `m1a-substrate` D9 point 2, restated
   because `classify` is where it would first be violated.
4. **Every table test asserts its own completeness.** The taxonomy test iterates `AllKinds()` and
   fails if the expectation map's size differs from `len(AllKinds())`; the verdict test does the
   same over its three bands plus both boundaries. A value added later without an expectation
   fails loudly instead of defaulting to a passing arm.

**And two things already in the tree do work here for free.** `core_exported_decls_have_tests_test.go`
fires in the fast loop on every new exported symbol in `classify`/`recall`/`relation` — a proxy,
announced as one, but it catches the dominant R1 failure. And every core PR ships its L1 tables
in the same commit as the code, with `make cover`'s **number** read rather than its exit code
(Phase A task 2.7's precedent, which is why the floor has never surprised this chain).

`core/relation` and `core/recall` are small and enumerable; `core/classify` is the one at risk,
and property 1 is what keeps it from being.

---

## 3. Package layout and dependency map

```
internal/core/classify/               NEW package — pure, stdlib only          PR 7
  doc.go            (docs/06-harness.md §1's tree already lists classify/ — Phase A PR 1)
  kind.go           Kind, the 13 members, AllKinds, ParseKind, Kind.UnitType()
  outcomes.go       the six orthogonal vocabularies + their AllX() functions
  salvage.go        Salvage — the streaming, truncation-tolerant object reader   D1
  decode.go         Decode, the fieldSpec table, decodeEnum[T]                   D1, D11
  classification.go Classification, Degradation, Reason
  prior.go          PriorWeight, PriorDecayRate                                  D3
  tounit.go         ToUnit(c, id, now, priors) (unit.Unit, error)                D4, I18
  prompt.go         BuildPrompt(text, beliefs, now) string; Belief
  *_test.go         L1 tables over inline payload literals — the floor's numerator
      imports: encoding/json, errors, fmt, strings, time, internal/core/unit

internal/core/recall/                                                          PR 8
  doc.go            loses its "Pending conformance anchor" paragraph            D10
  vector.go         VectorQuery, VectorIndex, NewVectorIndex, Search, Normalize  D5, D6
  fuse.go           Fuse, RRFK, RecallTopK, WeightVector, WeightLexical          D5
  tokenize.go       Tokenize — what the lexical leg searches for                 D5
      imports: errors, fmt, math, sort, strings, unicode

internal/core/relation/                                                        PR 11
  doc.go
  thresholds.go     Thresholds, Resolve, the two Default… constants, DedupCandidateK   D7, Q1
  verdict.go        Verdict, Decide                                              D7, I08
  relation.go       Relation, CreatedBy
  judgment.go       Judgment, Outcome, DecodeJudgment                            D7
      imports: encoding/json, errors, fmt, time, internal/core/classify

internal/ports/                       new FILES only — Phase A's files untouched (spec R6.3)
  embeddingrepo.go  Embedding, EmbeddingRepo{Put, LoadIndex}                     PR 9
  lexicalsearch.go  LexicalSearch{SearchLexical}                                 PR 9
  decisionlog.go    DecisionAction + vocabulary, Decision, DecisionLog           PR 10
  relationrepo.go   RelationRepo{Upsert, ByUnit, ThresholdsFor}                  PR 11
      imports: context, encoding/json, time, internal/core/{unit,recall,relation}

internal/store/sqlite/                new FILES only — widens store_api.golden
  embeddingrepo.go  Put (ON CONFLICT unit_id), LoadIndex(model), the BLOB codec  PR 9
  search.go         SearchLexical — MATCH + bm25 + positive status='pool'        PR 9
  decisionlog.go    Record, Since                                               PR 10
  relationrepo.go   Upsert (ON CONFLICT from,to,type), ByUnit, ThresholdsFor     PR 11
      imports internal/core/recall (Normalize at the storage boundary — D6)

internal/brain/                       first code in this package, ever
  capture.go        CaptureService (the only ports.Clock read) + captureRunner   PR 10
  recall.go         RecallService — the one fused mechanism, D5                  PR 10
  index.go          Index — the in-memory VectorIndex holder, loaded at open     PR 10
  result.go         CaptureInput, CaptureResult, Deferred                        PR 10

test/support/memrepo/         + a fake per new port, deep-copying, mutex-guarded
test/support/repocontract/    + a contract suite per new port, run at L2 and L3
test/support/goldenset/       + ClassifyExpected.{EventAt,DueAt}  (D2)           PR 7c
                              + RecallExample vectors and lexical_ranking (§4)   PR 8c
test/conformance/             I02, I04(new), I07, I08, I12, I14, I18, I21, the
                              two brain clock guards, the two migration pins
testdata/classify/cases/      real cases, incl. truncated / wrong-type / unknown-enum
testdata/recall/cases/        real cases, incl. distractor / near-dup / disagreement
```

**Dependency-rule check.** `internal/core/classify` imports `internal/core/unit` and stdlib;
`internal/core/relation` imports `internal/core/classify` — both inside `core-purity`'s
`github.com/rengo/nooma/internal/core` allow prefix (`.golangci.yml:62`). No core package
imports `context`, `internal/ports`, or anything else. `internal/ports` importing three core
packages is legal and already established (`unitrepo.go:8`); the arrow points away from the
core, so no cycle exists. `internal/store/sqlite` importing `internal/core/recall` is the same
direction `unitrepo.go:14` already takes. `sqlite-containment` is untouched — nothing new
imports `database/sql` or the driver outside `internal/store` (`.golangci.yml:88-97`).

**`docs-sync.yml` fires on `^internal/core/` only** (`docs-sync.sh:45`), so **PRs 7, 8 and 11 are
the only Phase B PRs the gate forces a doc 02 delta on**, and each has a real one (D1's
field-by-field degradation into §5; D5's knobs into §5.2 and §13; D7's fallback into §4). PR 10
touches no core file and is therefore *not* forced — it carries Q3a's §5 hooks note anyway,
because the behaviour is a fact about the brain rather than an adapter detail. Stated so nobody
reads "not required" as "not wanted". **No Phase B PR should need `no-spec-change`.**

`docs/06-harness.md` §1's tree gains no line: `classify`, `recall` and `relation` are all
already listed (`06:26-31`).

---

## 4. Interfaces and fixture formats this change fixes

Beyond the Go declarations already given in §2, two **fixture formats** move, and both are
load-bearing enough to be decided here rather than during apply.

### 4.1 `goldenset.ClassifyExpected` gains two optional date fields (D2)

```go
EventAt *string `json:"event_at,omitempty"`
DueAt   *string `json:"due_at,omitempty"`
```

`testdata/classify/format.md`'s field table and its fenced example move the date out of
`structured_data` in the same PR, because `TestHarness_GoldenSetFormatMatchesType` decodes the
fence into the type under `DecodeStrict`.

### 4.2 `goldenset.RecallExample` gains vectors and a stated lexical ranking — and the reason is a real defect, found by reading the fake

`testdata/recall/format.md:98-107` requires the corpus to contain a **distractor**, a
**near-duplicate pair**, and a **lexical/vector disagreement**, and `expected_unit_ids` is "the
expected fused order". None of that is producible today:

- The only embedder any test may use is `fakeprovider.NewEmbeddingFake`, whose vectors are
  `float32(fnv32a(i‖text) % 10000) / 10000` — **eight components, all in `[0,1)`, never
  normalized, and a hash of the whole string** (`embed.go:14`, `:41-50`). Every pair of
  documents therefore has a high, essentially arbitrary cosine, and the vector ranking carries
  no relationship whatever to content. A golden order pinned against it would pin a hash.
- The lexical leg needs FTS5, which needs SQLite, which is L3 — the fusion corpus is L2.

| Option | Verdict |
|---|---|
| Drive the corpus with `NewEmbeddingFake` | Rejected — the "expected fused order" would be a property of FNV-1a, not of recall. A green test proving nothing is worse than no test |
| Give the fake a bag-of-words hashing embedder | Rejected — it makes the vector leg approximately equal to the lexical leg, so a **lexical/vector disagreement case becomes unauthorable**, and that is one of the three properties `format.md` requires |
| Test fusion at L1 only, over two hand-written id lists | Insufficient alone — ADR-0010 wants the golden set to be "the regression detector for recall quality", not for one function |
| **State both rankings' inputs in the case file** (chosen) | The case author writes the vectors and the lexical ranking, so a genuine disagreement is authorable; `Search` and `Fuse` — the two functions the ADRs care about — both run for real, at L2, with no database |

```go
type RecallUnit struct {
    ID, Type, Content, Status string
    Vector []float32 `json:"vector,omitempty"`
}
type RecallQuery struct {
    Query            string
    Vector           []float32 `json:"vector,omitempty"`
    LexicalRanking   []string  `json:"lexical_ranking,omitempty"`
    ExpectedUnitIDs  []string  `json:"expected_unit_ids"`
}
```

`Validate` gains a cross-field check the loader **can** mechanize, unlike the ones `format.md`
currently lists as unmechanized: if any unit carries a vector then all must, the query must, and
every vector must have the same length. Dimension 4–8 by hand keeps the files readable.

The loop is closed at L3: PR 9c's integration test asserts the **real** FTS5 leg produces the
`lexical_ranking` a case states, for at least one case. Without that, `lexical_ranking` is a
number an author made up; with it, it is a recording.

---

## 5. The chain, with the split lines drawn before any code exists

Phase A's own retrospective is the reason these lines are drawn now: PR 2a shipped at **2.6×**
its estimate because its split was considered only once the diff existed, and the chain then
measured 1.77×, 1.70×, 1.42×, 1.74× and 1.87× on slices whose lines *were* drawn first
(`m1a-substrate/tasks.md` §PR 2a, PR 3, PR 4, PR 5, PR 6). Sixteen merges against the
proposal's five rows is the honest projection, not pessimism.

| # | PR | Content | Est. |
|---|---|---|---|
| **8a** | `feat/core-recall-vector` | `VectorQuery`/`VectorIndex`/`NewVectorIndex`/`Search`/`Normalize` + L1; **promote I21, add its behavioural test, retire `pending-red` (D10)**; doc 02 §5.2 | ~380 |
| 8b | `feat/core-recall-fuse` | `Fuse`, `Tokenize`, the four constants; doc 02 §13's four rows | ~200 |
| 8c | `feat/recall-corpus` | `goldenset` vector/lexical widening + `format.md`; `testdata/recall/cases/`; flip `casesDirMustBeEmpty["recall"]` | ~250 |
| 7a | `feat/core-classify-decode` | `Kind`, the six vocabularies, `Salvage`, `Decode`, the field table + L1 | ~330 |
| 7b | `feat/core-classify-unit` | `ToUnit` (I18), the priors + their migration pin, `BuildPrompt`; doc 02 §5.1 | ~280 |
| 7c | `feat/classify-corpus` | `ClassifyExpected` widening + `format.md`; `testdata/classify/cases/` incl. the three broken shapes; flip `casesDirMustBeEmpty["classify"]`; I14 at L2 | ~250 |
| 9a | `feat/ports-embedding` | `ports.EmbeddingRepo` + `ports.LexicalSearch` + memrepo fakes + contract cases | ~280 |
| 9b | `feat/store-embedding` | The BLOB codec, `Put`, `LoadIndex(model)`, L3 over a two-model vault (I21's storage half); `store_api.golden` | ~380 |
| 9c | `feat/store-search` | The FTS5 leg, the positive filter, `bm25` ordering, the corpus's `lexical_ranking` confirmed at L3 | ~220 |
| 10a | `feat/ports-decisionlog` | `ports.DecisionLog` + the action vocabulary + store impl + memrepo + contract | ~250 |
| 10b | `feat/brain-capture` | `CaptureService`, `captureRunner`, `Index`, `RecallService`; the two clock guards; I12/I18/I02 at L2 | ~400 |
| 10c | `feat/brain-hooks` | Q3a's refusal path, `CaptureResult.Deferred`, **the new I04 conformance test**; doc 02 §5's hooks note | ~200 |
| 11a | `feat/core-relation` | `Thresholds`/`Resolve`/`Decide` + L1 boundaries + the migration pin at L2; doc 02 §4 | ~250 |
| 11b | `feat/ports-relation` | `ports.RelationRepo` + the SQLite upsert + memrepo + contract; I07 at L2 | ~300 |
| 11c | `feat/relation-judge` | `DecodeJudgment`, the judge call wired into capture, the `duplicate` handling; I08 at L2 | ~280 |

**Dependencies**: `8a → 8b → 8c`; `7a → 7b → 7c`; `9a → 9b`, `9a → 9c`; `(7c, 9b, 9c, 10a) → 10b → 10c`;
`11a → 11b → 11c`, and `11c → 10b`. PR 8 depends only on Phase A's PR 2, which shipped.

**PR 8a goes first, and the reason is D10.** It is the only PR carrying the ruleset change, and
running it first makes the window during which `main` lacks the `pending-red` context empty —
no other PR is in flight across it. Every subsequent Phase B PR then merges into a repository
where the gate simply does not exist.

---

## 6. Test matrix

| What | Level | Where | PR |
|---|---|---|---|
| `Salvage` over truncated / wrong-type / unknown-enum / non-object payloads | L1 | `internal/core/classify/` | 7a |
| `Decode`: each field degrades independently; the six orthogonal fields degrade independently of each other and of `type` (I14) | L1 | `internal/core/classify/` | 7a |
| `Kind` is thirteen members; `Kind.UnitType()` is false for the six non-persisting outcomes | L1 | `internal/core/classify/` | 7a |
| `ToUnit`: three distinguishable instants prove `created_at`/`event_at`/`due_at` are never crossed (I18) | L1 | `internal/core/classify/` | 7b |
| `PriorWeight`/`PriorDecayRate` equal migration 0001's `units.weight` / `weight_decay_rate` `DEFAULT`s, compared as parsed floats | L2 | `test/conformance/`, via `migrationSQLText` | 7b |
| I14 over the real corpus, each broken case's `Reason` matching the shape its id names | L2 | `test/conformance/` | 7c |
| `Search`: hand-computed dot-product ranking; `ErrModelMismatch`; `ErrDimMismatch`; `ErrZeroVector` | L1 | `internal/core/recall/` | 8a |
| I21 reflective anchor, **promoted, untagged** | L2 | `test/conformance/` | 8a |
| I21 behavioural: a model-`b` entry outscoring every model-`a` entry never appears in a model-`a` query | L1 | `internal/core/recall/` | 8a |
| `Fuse`: ADR-0010's formula by hand, an id in one list only, and the tie-break rule | L1 | `internal/core/recall/` | 8b |
| The recall corpus: `Search` over stated vectors + `Fuse` over the stated lexical ranking equals `expected_unit_ids` | L2 | `test/conformance/` | 8c |
| `EmbeddingRepo` and `LexicalSearch` contracts, against the in-memory fakes | L2 | `test/conformance/` → `repocontract` | 9a |
| The **same** contracts, against a real temporary vault | L3 | `test/integration/` → `repocontract` | 9b, 9c |
| A stored vector's L2 norm is 1 within tolerance; `dim == len(vector)`; the little-endian round trip | L3 | `test/integration/` | 9b |
| `LoadIndex(model)` over a two-model vault returns one model's rows only (I21, storage half) | L3 | `test/integration/` | 9b |
| The FTS leg returns only the `pool` unit from a four-status fixture sharing vocabulary (I02, storage half) | L3 | `test/integration/` | 9c |
| The real FTS leg reproduces a corpus case's stated `lexical_ranking` | L3 | `test/integration/` | 9c |
| `DecisionLog` contract; the action vocabulary is closed and every constant is reachable | L2 | `test/conformance/` | 10a |
| A capture reads the clock exactly once — a fake `Clock` that fails on the second call | L2 | `test/conformance/` | 10b |
| No non-test file under `internal/brain/**` contains `time.Now(` | L2 | `test/conformance/` | 10b |
| No file under `internal/brain/**` holds two `Now()` calls, and no `Now()` sits in a function that already takes a `time.Time` (R9's real bug class) | L2 | `test/conformance/`, `go/ast` | 10b |
| A capture with an effect leaves exactly one `decision_log` row per decision (I12) | L2 | `test/conformance/` | 10b |
| A `superseded` and an `incomplete` unit are absent from the fused output (I02) | L2 | `test/conformance/` | 10b |
| **I04 — a `timer` classification leaves `units` empty** (new conformance test) | L2 | `test/conformance/` | 10c |
| An ambiguous person reference produces a `pool` unit and two `decision_log` rows; I06 named as out of scope in prose | L2 | `test/conformance/` | 10c |
| `Decide`: three bands plus both boundaries exactly (`conf == Persist` stores; `conf == Surface` asserts) | L1 | `internal/core/relation/` | 11a |
| `DefaultMinConfidenceTo{Persist,Surface}` equal migration 0002's `relation_thresholds` `DEFAULT`s, as parsed floats (Q1) | L2 | `test/conformance/`, via `migrationSQLText` | 11a |
| `RelationRepo` contract, fake at L2 and real vault at L3; a second upsert over one triple leaves one row (I07) | L2 + L3 | `test/conformance/`, `test/integration/` | 11b |
| A candidate below `min_confidence_to_persist` leaves no `relations` row and one `decision_log` row (I08) | L2 | `test/conformance/` | 11c |
| The judge is **not** called when the candidate list is empty — enforced free by the scripted fake | L2 | `test/conformance/` | 11c |

No L4 row: Phase B adds no subcommand and no route, so there is nothing compiled to drive
(`spec.md` R4.8, R7.1). No test reaches a real provider — every LLM response comes from
`testdata/llm/cases/` through `fakeprovider`, and every embedding from `NewEmbeddingFake` or
from a corpus-stated vector.

---

## 7. Risks this design accepts

| # | Risk | Position |
|---|---|---|
| 1 | **The ruleset context is a claim this session could not verify.** `pending-red` being a required status check on `main` comes from `m1a-substrate/design.md`, not from an executed query | Accepted, and the direction of the error is safe: if it is *not* a required context, step (1) of D10 is a no-op. If it is and we skip it, PR 8a cannot merge. The operator confirms with `gh api` before acting; this document does not claim to know |
| 2 | **`bm25()`'s sign convention was not executed.** `ORDER BY bm25(units_fts)` ascending rests on ADR-0010:20-22's assertion that the values are negative | Accepted, and closed by PR 9c's L3 test, which is written before the query and observed failing. This is exactly the class of fact `docs/06-harness.md` §3 says only SQLite can disprove |
| 3 | **The fake embedder cannot rank.** Its vectors are a hash of the whole string, unnormalized, eight-dimensional (`embed.go:41-50`) | Closed structurally by §4.2: the corpus states its own vectors, and `NewEmbeddingFake` stays what it is — a determinism device for pipeline tests, never a ranking device. Named here so nobody re-discovers it by writing a green test that proves nothing |
| 4 | **The half-synced unit is real.** A unit persists, its embedding does not, and it is lexically findable but semantically invisible | Accepted deliberately (D8), on doc 02 §5's product rule. Surfaced in `CaptureResult.Embedded`, in a `capture.embedding.failed` glass-box row, and repaired by `reindex` (M6), whose detection half `nooma doctor` already promises (`03:306-307`) |
| 5 | **`spec.md` R2.3 asks for a mixed-model index** that I21's own anchor comment says must never exist | Resolved as C1 in D5 in the anchor's favour, with R2.3's scenario satisfied unchanged. R2.3's *wording* needs a follow-up correction — flagged, not silently satisfied |
| 6 | **`spec.md` R4.6 forbids a `units` row for a timer while `spec.md` §9 says the question is undecided** | Resolved as C2 in D9 on doc 02 §8's bold sentence, and pinned by a new I04 conformance test rather than by prose |
| 7 | **`dedup_candidate_k` and `recall_top_k` are unmeasured numbers.** No data exists to calibrate either | Accepted, and this is what §13 is for. Both are single named constants in one place, so calibration is a one-line change when real usage arrives — which is `docs/06-harness.md` §7's entire argument for the table |
| 8 | **`core/relation` importing `core/classify` for `Salvage` is a directional smell.** It reads as "relations depend on classification" | Accepted with a named reversal criterion (D7): a third consumer outside the capture pipeline moves it to its own package. M2's `derive` is that consumer, one milestone out, so this decision has a short expected life and is cheap to reverse |
| 9 | **Two fixture-format widenings** (`ClassifyExpected`, `RecallExample`) touch Phase A test-support types | Accepted. Neither is a conformance test, so `docs/06-harness.md` §4's prohibition does not apply, and `TestHarness_GoldenSetFormatMatchesType` keeps each type and its `format.md` in lockstep — the guard is the reason the widening is safe, not a reason to avoid it |
| 10 | **The AST clock guard does not catch two `Now()` reads across two files** in one logical operation | Accepted and announced in its own doc comment, following `golden_sets_test.go:164-176`'s proxy-announcement precedent. It catches the dominant form and converts proposal R9 from a review property into a gate, which is `docs/06-harness.md` §6's precedence rule doing its job |
| 11 | **Relation direction is not canonicalized**, so M2's `connect` could produce a mirror edge as a second row | Accepted (D7), with the owner named as M2. Canonicalizing needs a symmetry classification over open text, which is the same fact that killed seeding in Q1 |
| 12 | **Sixteen merges against the proposal's five rows.** Phase B is roughly 4,300 budgeted lines | Accepted, with every split line drawn above **before any code exists** — the single correction Phase A's 2.6× outlier earned. The measured band remains 1.3×–2.2×, and these estimates should be read through it |
| 13 | **`depguard`'s coverage of `_test.go` under `internal/core` is still unverified**, one chain later | Accepted, and D11 is written so the answer does not matter: the L1/L2 split it forces is the split `docs/06-harness.md` §3's own rule would pick anyway |

---

## 8. What this design does not decide

- **Q3b — is `/recall` standalone or routed through classify.** Phase C's. What this design fixes
  regardless: `brain.RecallService` is the one mechanism both entrances call, so Q3b decides an
  entrance, never a second implementation. ADR-0010's "a bias here propagates into the entire
  relation graph" is what makes that non-negotiable.
- **Q3c — how a correction finds its referent**, and with it the whole of `feat/corrections`
  (I03's correction half, I13's first `learning_signals` write, `ports.SignalRepo`). Moved to
  Phase C by `spec.md` §0 and honoured here.
- **The HTTP and CLI shapes of `CaptureResult.Deferred`.** Phase B fixes that the refusal is
  representable and distinguishable from success; how it is rendered is Phase C's.
- **`nooma doctor`'s units↔embeddings↔fts consistency check**, whose need D8 names and whose
  home is Phase C or M6. No Phase B port method exists for it, because it would have no caller.
- **The per-type λ table** doc 02 §13 gestures at. D3 ships one base prior and refuses to invent
  nine numbers; M5 is the milestone that calibrates them.
- **A vault-configured timezone.** D4 carries the zone inside the clock's instant and names the
  reversal criterion (a hosted vault whose process zone is not the user's), which is multi-tenant
  and out of v1 per ADR-0005.
- **Whether `internal/ports` gets its own exported-surface golden.** Phase B takes it from four
  files to eight, which is `m1a-substrate` §8's own stated revisit trigger ("when `ports` holds
  more than four interfaces"). Considered and deferred once more: every new port lands its
  contract suite in the same PR, which is a stronger guard than a golden, and adding a second
  golden format in the change that also retires a gate is one moving part too many. **The
  trigger is now met, so this is a deliberate deferral, not an oversight** — Phase C should
  decide it.
- **The in-memory index's eviction path.** Nothing in Phase B leaves `status = 'pool'`, so D5's
  `LiveByIDs` boundary makes eviction unnecessary rather than merely unimplemented. M2's
  `archive` phase is where the question becomes real, and the answer there is still "filter at
  the boundary", not "maintain a second copy of `units.status`".
