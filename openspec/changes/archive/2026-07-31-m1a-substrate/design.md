# Design — M1 Phase A: the substrate

Technical design for `m1a-substrate`, the first of the three chained changes
[`m1-capture-recall/proposal.md`](../m1-capture-recall/proposal.md) §5 splits M1 into. Scope is
that document's **Phase A table only** — PRs 1 through 6. Phase B (`m1b-pipeline`) and Phase C
(`m1c-surface`) are out.

This design settles what Phase A's shapes are: the unit vocabulary, the repository port, the
provider ports, the in-memory fake, and the mechanics of three gates that have been armed and
vacuous since the harness landed. It does not restate requirements — that is `spec.md`, written
in parallel.

**A scope note, stated before anything rests on it.** Phase A's own table includes PR 4
(`feat/store-unitrepo`) and PR 6 (`feat/providers-http`), so Phase A does ship code capable of
writing a unit and of speaking HTTP to a model. What it ships no *caller* for: no pipeline, no
route, no CLI command reaches any of it. "Nothing in Phase A writes a unit" is true of the
running binary and false of the code, and the difference matters when reading §6's test matrix —
every Phase A behaviour is proven by a test, never by a user path.

---

## 1. Ground truth this design was verified against

Every row was checked by reading the named file at the named line in this session. **This
session had no shell**, so nothing here was verified by running a command; where the proposal or
the M0 design records a *measured* result that could only come from execution, the row says so
and attributes it rather than re-asserting it as if freshly observed.

| Claim | How it was verified |
|---|---|
| `internal/ports` declares exactly two interfaces, `Clock` and `IDGen`, and nothing else | `internal/ports/clock.go` — the whole file is 29 lines; `internal/ports/doc.go` adds only a package comment |
| Every package under `internal/core/` contains only a `doc.go` | glob of `internal/**/*.go`: `unit`, `recall`, `relation`, `weight`, `focus`, `consolidation`, `prospection`, `selfmodel`, `learning` each contribute exactly one file |
| I01's promoted test requires `unit.Status` to be **string-kind** | `test/conformance/i01_focus_never_persisted_test.go:63-66` — `var zero unit.Status; reflect.TypeOf(zero).Kind() != reflect.String` is an error |
| I01 calls `unit.AllStatuses()` — a function, not a package-level slice | same file, line 70 |
| I03 requires `ports.UnitRepo` to be an **interface**, with **≥1 method**, none named `Delete*` | `test/conformance/i03_units_never_deleted_test.go:50-67` |
| I03's second half is a tree scan for `DELETE FROM units` over `internal/` and `cmd/`, identifier-tail aware | same file, lines 70-111 (`containsUnitsDeleteStatement` rejects `units_fts`) |
| I21 requires `recall.VectorQuery` and `recall.VectorIndex` to be **structs** with exported string-kind `Model` fields | `test/conformance/i21_vector_search_filters_on_model_test.go:60-96` |
| `tree_scan_test.go` is `//go:build pendingimpl` and its only export is `scanGoTree`, used by I01 and I03 | `test/conformance/tree_scan_test.go:1`, `:29` |
| `repoRootFromCaller`, the other helper I01/I03 need, is **already untagged** | `test/conformance/store_api_test.go:87` — no build tag on that file. Nothing beyond `tree_scan_test.go` needs untagging |
| `pending-red.sh` fails when the package **compiles**, and that check runs before the symbols file is read | `scripts/pending-red.sh:9-19` (build + failure mode 1) versus `:31` (`tracked_syms=$(grep ...)`) |
| Untagging `scanGoTree` alone fails lint as `unused` | **Not re-measured here.** `m1-capture-recall/proposal.md` §4.7 records it as measured; the mechanism is consistent with `.golangci.yml:39` (`unused` enabled) and `:25-27` (`pendingimpl` deliberately absent from `run.build-tags`) |
| `depguard`'s `core-purity` allows `internal/core/**` only `$gostd` and `internal/core` | `.golangci.yml:52-62`. There is **no** `$test`/`!$test` selector on its `files:` list, so as written it covers `_test.go` files under `internal/core` too |
| `forbidigo` bans `time.Now`/`Since`/`Until` **by call pattern**, not the `time` import | `.golangci.yml:104-109` — `^time\.Now$` etc. A `time.Time` field in `internal/core` is legal |
| `forbidigo` is scoped to `internal/core/` by one exclusion rule | `.golangci.yml:122-124` (`path-except: internal/core/`) |
| The core coverage floor measures **only test binaries under `internal/core/...`** | `scripts/core-coverage.sh:56` — `go test -coverprofile -coverpkg=./internal/core/... ./internal/core/...`. An L2 test in `test/conformance` exercising core contributes **nothing** to the floor |
| The floor is armed and vacuous today, and exits 0 at `total=0` with a distinct message | `scripts/core-coverage.sh:102-105` |
| `make check` does not run the floor; `make check-all` and CI's `coverage` job do | `Makefile:36` vs `:39`, `:85-87`; `.github/workflows/ci.yml:125-133` |
| `docs-sync.sh` fires on `^internal/core/` only | `scripts/docs-sync.sh:45-51`. A PR touching only `internal/ports`, `internal/store`, `internal/providers`, `internal/config` or `test/` never trips it |
| `store_api.golden` walks `internal/store/**` **recursively**, skipping `_test.go`, and now renders exported `var`/`const` as well as funcs and types | `test/conformance/store_api_test.go:47`, `:107-114`, `:167-185` |
| `units.status` is `TEXT NOT NULL DEFAULT 'pool'` with the comment `pool|archived|superseded|incomplete` | `internal/store/sqlite/migrations/0001_core_tables.sql:10`; identical in `docs/03-data-model.md:27` |
| `units.type`'s DDL comment lists exactly nine values | `0001_core_tables.sql:8` — `task|mental_load|event|knowledge|procedural|emotional|list|structured_ref|insight`, matching `docs/02-cognitive-core.md:12-13` |
| doc 02's **classification** taxonomy is thirteen values, a different vocabulary | `docs/02-cognitive-core.md:102-104` — adds `chitchat`, `out_of_scope`, `recall`, `correction`, `timer`, `recurring_reminder`, and omits `structured_ref`/`insight` |
| `units.confidence` exists, is nullable, and doc 02 never mentions it | `0001_core_tables.sql:18` (via doc 03:35); no hit for `units.confidence` anywhere in doc 02 |
| Migration 0002's own comment says archived units stay in `units_fts` and that only `superseded`/`incomplete` are excluded — while prescribing the positive `status = 'pool'` filter, which also excludes archived | `0002_learning_and_search.sql:103-110`. The two halves of that comment cannot both be true; D2 resolves it |
| `DocumentedProviderTypes` is `["anthropic","ollama","whisper_cpp"]`, and its comment claims it mirrors doc 01 | `internal/config/validate.go:165-168` |
| `checkTasks` validates only the task **name**, never that its `provider` names a declared entry | `internal/config/validate.go:155-163` |
| doc 01 binds `embedding: { provider: ... }` — a literal ellipsis that decodes to the Go string `"..."` | `docs/01-architecture.md:199`; the decode behaviour is `m0-skeleton/design.md` §1's measured row |
| doc 01's `providers:` block has four entries and three distinct types; `whisper_local` is the only `whisper_cpp` | `docs/01-architecture.md:175-191` |
| The build plan requires an `openai` implementation | `docs/05-build-plan.md:48` |
| doc 01 names four provider interfaces, of which M1 needs two | `docs/01-architecture.md:225-226` — `LLMProvider`, `MultimodalProvider`, `TranscriptionProvider`, `EmbeddingProvider` |
| ADR-0012's memory table is arithmetically consistent with **`float32`**, not `float64` | 10,000 × 768 × 4 B = 29.3 MiB against the ADR's "29 MB"; 1,000 → 2.93 MiB against "3 MB"; 500,000 → 1.43 GiB against "1.4 GB". `float64` would double every row. `docs/adr/0012-vector-proximity-search.md:47-53` |
| ADR-0012 puts fusion and selection in `internal/core/recall` over a value handed to it, and hides the mechanism from the core | `0012:59-73` |
| ADR-0003's amendment moves invariant (2) from schema-enforced to **query-enforced** (`searches filter on model`) | `docs/adr/0003-embeddings.md:80-92` |
| `testdata/llm/format.md` flags `prompt` as fragile as a replay key, and does not solve it | `testdata/llm/format.md:42`, and `test/support/goldenset/types.go:213-219` repeats the warning on the field itself |
| `goldenset.Load`/`DecodeStrict` reject unknown fields and enforce required fields; `LLMExample` carries exactly one of `response`/`error` | `test/support/goldenset/loader.go:24-47`, `types.go:227-248` |
| `assertCasesDirIsEmpty` fails on the first non-`.gitkeep` entry, untagged, inside `make check` | `test/conformance/golden_sets_test.go:253-275`; the file has no build tag |
| `test/support/*` packages are imported by both untagged L2 and `integration`-tagged L3 | `test/conformance/schema_doc_test.go:10` and `test/integration/schema_golden_test.go:20` both import `test/support/schema` |
| `test/conformance` already has an untagged helper that reads migration SQL off disk | `test/conformance/i13_learning_signal_test.go:24-57` (`migrationSQLText`), with its own non-empty-corpus guard |
| No non-test file under `internal/store/**` references `time.Now` today | grep over `internal/`: the only hits are `txlock_integration_test.go:105,107` (a `_test.go` file) and two comments |
| §13's calibration table contains no knob Phase A introduces | `docs/02-cognitive-core.md:280-299` — the nearest, `recall_top_k`, belongs to Phase B |

---

## 2. Decision record

### D1 — `unit.Status` is a defined string type; `AllStatuses` is a function; validity is a boundary property

I01's conformance test was written first and it already decided the kind: a `unit.Status` whose
`reflect.Kind()` is not `reflect.String` fails
(`i01_focus_never_persisted_test.go:63-66`). Under `docs/06-harness.md` §4 a failing conformance
test has exactly two exits — fix the code, or change doc 02 *and* its ADR — and "make `Status` an
`int`" is neither. So the shape is settled; what follows is why the test is right anyway, and what
has to sit beside the type because a string type cannot carry it alone.

| Option | Pro | Con |
|---|---|---|
| **Defined string type** (chosen) | Prints as itself in an error, a log line and a `decision_log` row; binds to a `TEXT` column with no conversion table; the whole vocabulary stays greppable across Go and SQL | `Status("focus")` compiles — the type does not make an invalid value unrepresentable |
| Integer enum + a lookup table | An unmapped value is caught at the boundary by construction | Fails I01's promoted test; adds a second vocabulary (int ↔ text) that must be kept in sync with a `TEXT` column, and a `4` in a log line means nothing |
| A struct wrapping an unexported string | Genuinely unrepresentable outside the package | Fails I01's `reflect.String` check; cannot be scanned or bound without custom marshalling on both sides |

The type therefore does not enforce validity, and this design refuses to pretend otherwise. Three
things do:

- **`ParseStatus(string) (Status, error)`** is the sole entry point from untrusted text — a
  database row, a JSON body, a CLI flag. It returns `ErrUnknownStatus` naming the value and the
  vocabulary. Nothing else converts a `string` to a `Status`.
- **`AllStatuses() []Status`** is a *function returning a fresh slice*, not an exported `var`.
  An exported slice is mutable by any importer (`unit.AllStatuses[0] = "focus"` would defeat
  I01's vocabulary check from outside the package). The function form is also what I01 already
  calls.
- **The vocabulary has one source of truth in Go and one in SQL, and an L2 test pins them
  together.** `test/conformance` gains a test that reads migration 0001's `units.status` column
  comment through the existing `migrationSQLText` helper
  (`i13_learning_signal_test.go:24-57`) and asserts it names exactly the members of
  `AllStatuses()`. That is the same shape as the I13 precedent and it closes the only drift this
  design can see between the Go vocabulary and the schema's documented domain.

`unit.Type` follows the identical pattern — `AllTypes()`, `ParseType`, `ErrUnknownType` — for the
reasons in D4.

### D2 — The live predicate is `Status.IsLive()`: no clock, no arguments, and it names a documented contradiction

`internal/core` may not read the clock and may not import `ports`
(`.golangci.yml:52-62`, `:104-109`), so the question the launch brief asks — where does the
live/archived predicate live, given the caller holds the `Clock` — has a sharp answer: **liveness
is not a function of time at all.** It is a property of the status value:

```go
func (s Status) IsLive() bool { return s == StatusPool }
```

The temptation is to fold "archived because its effective weight decayed below the threshold"
into the same predicate. That is a different function, it takes `now time.Time` as a parameter,
it lives in `internal/core/weight`, and it is M2. Keeping the two apart is what stops a
time-dependent decision from leaking into a filter that every read surface calls.

A method, not a free function: the predicate and the vocabulary are one concept, and `s.IsLive()`
is reachable from any `Status` value without importing a second name.

**The contradiction this decision has to name.** Migration 0002's comment block
(`:103-110`) says archived units are "NOT excluded from read surfaces; only superseded/incomplete
are excluded" and, in the same breath, that doc 02 prescribes doing that "positively
(`status = 'pool'`)". Those cannot both hold: the positive filter excludes `archived` too. Doc 02
§1 has the same shape of wording. This design takes the **positive filter as the operative rule**
— `IsLive()` is `== StatusPool`, and every read surface filters positively — because that is the
half that is mechanized, that the partial index
(`0001_core_tables.sql:25-28`, `WHERE status = 'pool'`) already assumes, and that doc 02 states as
the invariant. The archived-units-stay-in-`units_fts` half remains true and is unaffected: FTS
indexing is not a read surface, which is exactly why the triggers must not be "optimized" to skip
statuses. PR 2's doc 02 delta says this in one sentence so the next reader does not have to
re-derive it.

### D3 — Transitions are an unexported table behind one exported validator; `core/unit` decides, `brain/` refuses

Legal transitions, derived from doc 02 §1 (`incomplete` is promoted or expired), §2
(`cold→warm/hot by a strong resurface`), and I20 (`the previous insight becomes superseded`):

| From | To |
|---|---|
| `pool` | `archived`, `superseded` |
| `archived` | `pool` |
| `incomplete` | `pool`, `archived` |
| `superseded` | — (terminal) |

Two entries need their reasoning recorded. **`incomplete → archived` is expiry's landing
status**: doc 02 says an unresolved `incomplete` unit is "expired after 24 h", the vocabulary has
no `expired` member, and I03 forbids deletion — so the only status left is `archived`. Doc 02 §1
gains that sentence in PR 2; inventing a fifth status would mean a new migration, a doc 03 edit
and a regenerated golden, for a transition M2 performs and M1 never reaches. **A self-transition
is illegal**, not a no-op: the validator is called only where a change is intended, and permitting
`pool → pool` would let the brain write an `UPDATE` that changes nothing while emitting a
`decision_log` row claiming an effect (I12).

Shape:

| Option | Verdict |
|---|---|
| Exported `map[Status][]Status` | Rejected — mutable by any importer, the same defect that made `AllStatuses` a function |
| A `switch` inside one function | Workable, but the legal set is data, and M0's D10 already established that in this codebase rules-as-data beats rules-as-control-flow |
| **Unexported table + one exported validator** (chosen) | The table is data and cannot be reached from outside; exactly one exported entry point, per `docs/06-harness.md` §7's "one place" rule |

```go
func ValidateTransition(from, to Status) error   // nil, or ErrIllegalTransition / ErrUnknownStatus
```

One function, not a `CanTransitionTo` boolean beside it: two exported spellings of one fact are
two things that can drift. Boolean callers write `err == nil`.

**Who refuses.** `core/unit` *decides* and returns an error value; `brain/` is the only layer that
*acts* — it returns the error to its caller and logs the refusal. The store never re-validates: a
port that re-checks the core's decision is a second source of truth, and the moment the two
disagree there is no rule for which wins. A SQL `CHECK` constraint was considered for the same
job and rejected on the same ground, plus it would need a new migration to express a rule that is
already expressible in the language the decision is made in.

The write path is `ByID → ValidateTransition → SetStatus(from: current, …)`, and D5's `from`
parameter is what makes the gap between the read and the write atomic.

### D4 — The persisted type vocabulary is not classify's taxonomy, and they live in different packages

`units.type`'s DDL comment names nine values; doc 02 §5's classification taxonomy names thirteen,
adding `chitchat`, `out_of_scope`, `recall`, `correction`, `timer`, `recurring_reminder` and
dropping the two derived types. Folding all thirteen into `unit.Type` would make
`unit.Type("chitchat")` a legal thing to persist into a column whose documented domain excludes
it.

So: **`unit.Type` carries the nine persisted values only** (Phase A), and classify's taxonomy is
its own vocabulary inside `internal/core/classify` (Phase B), with a partial mapping from the
second to the first. That mapping is where "a `chitchat` message produces no unit" is expressed,
and it belongs next to the decoder that produces it — not next to the definition of what a unit
is. This also keeps `core/unit`'s surface narrow, which matters because it is the package I01's
tree scan reads.

### D5 — `ports.UnitRepo`: five methods, no status parameter, no removal verb, and the instant always arrives as data

```go
type UnitRepo interface {
    Create(ctx context.Context, u unit.Unit) error
    ByID(ctx context.Context, id string) (unit.Unit, error)
    LiveByIDs(ctx context.Context, ids []string) ([]unit.Unit, error)
    UpdateContent(ctx context.Context, id, content string, at time.Time) error
    SetStatus(ctx context.Context, id string, from, to unit.Status, at time.Time) error
}

var (
    ErrUnitNotFound   = errors.New("unit not found")
    ErrUnitExists     = errors.New("unit already exists")
    ErrStatusConflict = errors.New("unit is not in the expected status")
)
```

Four shape decisions, each doing work:

**Deletion is unrepresentable in three layers, not one.** The interface declares no removal
method — that is the port's own shape, and it is what the promoted I03 reflection check pins
(`i03:57-67`). That check only catches names beginning with `Delete`, so `Purge`, `Remove` and
`Drop` would slip past it; the layer that actually closes the gap is I03's *second* half, the
tree scan for `DELETE FROM units` over all of `internal/` and `cmd/` — whatever a removal method
were called, it must eventually emit that statement. PR 3 additionally **strengthens** the prefix
set to `{Delete, Remove, Purge, Drop, Destroy}`. Strengthening a conformance test is allowed;
weakening one is the thing `docs/06-harness.md` §4 forbids.

**There is no `List(status)` method, and that is I02 made structural.** A status parameter is
precisely how a live read surface accidentally becomes a non-live one. Every read method is named
for what it returns: `LiveByIDs` cannot be asked for anything but `status = 'pool'`, and `ByID` is
the deliberate, single any-status escape hatch that corrections and audit need. When M2 needs
archived units, it gets `ListArchived`, not a parameter. The cost is N methods where one
parameterized method would do; it is accepted for exactly one property — no call site can widen a
filter by passing a different argument.

**`LiveByIDs` returns units in the caller's `ids` order, omitting what is absent or not live.**
Recall's fused ranking is the order that matters, and an unspecified order over a map-backed fake
would be a bug the race-and-shuffle suite finds on a Tuesday. The contract test asserts the
ordering against both implementations.

**No method reads a clock; every timestamp arrives as data** — inside the `unit.Unit` value for
`Create`, as an explicit `at time.Time` for the partial updates. `forbidigo` is scoped to
`internal/core` (`.golangci.yml:122-124`), so a `time.Now()` inside an adapter is legal and no
lint would catch a second clock read mid-operation — the proposal names this R9 and calls it a
review property. Making the instant a parameter converts the store's half of R9 into a
compile-time property; PR 4 adds the matching L2 tree scan (§6) so the store cannot reach for a
clock it no longer needs.

`SetStatus` takes `from` as an optimistic-concurrency precondition, not as a validation: the
legality decision already happened in `core/unit`. The vault lock guarantees one writer *process*
(M0 D4), not one goroutine, and Phase C's HTTP surface is concurrent within that process — so the
guard is real, it costs one parameter, and it turns a lost update into `ErrStatusConflict`.

Values, never pointers, in and out: a returned `*unit.Unit` would hand a caller a handle on the
fake's internal state, and the first test that mutated it would corrupt the next.

### D6 — The fake lives in `test/support/memrepo`, and a shared contract suite is the only thing keeping it honest

**Placement.** Ruled out, each for a mechanical reason:

| Candidate | Why not |
|---|---|
| `internal/store/memory` | Widens `testdata/schema/store_api.golden` (`store_api_test.go:47`, `:107`) with a test double — the golden exists so a store-surface widening is reviewable, and padding it with fakes is exactly the noise that teaches reviewers to skim it |
| `internal/ports` | Ships a test double in the production binary and widens the surface that every package imports |
| A `_test.go` inside `test/conformance` | Unreachable from L3, L4, and `internal/brain`'s own tests |
| **`test/support/memrepo`** (chosen) | The established precedent: `test/support/schema` is imported by both untagged L2 (`schema_doc_test.go:10`) and `integration`-tagged L3 (`schema_golden_test.go:20`); `test/support/goldenset`'s own doc comment states the intent — a support package "M1's real implementation can depend on later without `internal/core` ever seeing test-only code" |

**A correction to the brief's premise, because it changes nothing but should be said.** L1 cannot
use this fake and must not. `depguard`'s `core-purity` rule covers every file under
`internal/core/**` with no `$test` selector (`.golangci.yml:52-62`), so an L1 test inside
`internal/core/unit` cannot import `internal/ports` — and it has no reason to, because an L1 test
covers a pure function that takes no repository. The consumers are L2 (`test/conformance`),
`internal/brain`'s package tests, and L3/L4. If that lint detail turns out to behave differently
in practice, the placement does not change: the argument above stands on the golden, the binary
and reachability, not on depguard.

Package `memrepo` exports `NewUnits() *Units`, mutex-guarded (the suite runs `-race -shuffle=on`,
`Makefile:48`) and deep-copying on the way in and out so no test can reach its interior.

**Staying honest against the SQLite implementation that arrives in PR 4** is the harder half, and
a fake with its own bespoke tests proves nothing about the adapter. The mechanism:

```go
// test/support/repocontract  (untagged, so both L2 and L3 can import it)
func RunUnitRepo(t *testing.T, newRepo func(t *testing.T) ports.UnitRepo)
```

One suite, two callers: PR 3 runs it against `memrepo.Units` at **L2**, PR 4 runs the *same
function* against a real temporary vault at **L3**. Every behaviour the port promises —
`ErrUnitNotFound`, `ErrUnitExists`, `ErrStatusConflict`, `LiveByIDs`'s ordering and its exclusion
of `archived`/`superseded`/`incomplete`, `UpdateContent` leaving every other column alone — is
asserted once and answered twice. The standing rule this creates, and it is the whole point:
**a PR that widens `ports.UnitRepo` adds the contract case and the fake's implementation in that
same PR**, or the fake and the adapter start drifting the moment the second implementation lags.

Strict TDD orders it: the contract suite is written and watched failing before either
implementation exists.

### D7 — Two provider interfaces, raw text only, and `openai` reconciled by documenting it before declaring it

doc 01 names four provider interfaces. **Phase A defines two.**

```go
type LLMRequest  struct{ Prompt, Task string }
type LLMResponse struct{ Text, Model string }
type LLMProvider interface {
    Complete(ctx context.Context, req LLMRequest) (LLMResponse, error)
}

type EmbedRequest  struct{ Text string }
type EmbedResponse struct{ Vector []float32; Model string }
type EmbeddingProvider interface {
    Embed(ctx context.Context, req EmbedRequest) (EmbedResponse, error)
}
```

- **`LLMResponse.Text` is raw bytes-as-string, never a parsed classification.** I14's degradation
  rule is "a pure function of the bytes" (proposal §4.1) and lives in `core/classify`. A provider
  that parsed JSON would move I14 into an adapter, where it is untestable at L1 and would have to
  be re-proved once per provider.
- **`Model` is what actually answered, not what was asked for.** I21 filters vector search on the
  model that produced the vector; keying on the requested name would be a second source of truth
  the moment a provider substitutes a model.
- **`[]float32`.** ADR-0012's own memory table is arithmetically float32 (§1's row): 10,000 × 768
  × 4 B is the ADR's "29 MB". `float64` would silently double the residency figure the ADR chose
  the whole approach on.
- **No `Dim` field.** It is `len(Vector)`. A stored dimension beside a slice is a second source of
  truth with nothing keeping them equal.
- **Normalization is not the port's promise.** ADR-0012 unit-normalizes on write; that is a pure
  function in `core/recall` applied before the store writes, Phase B. A port that promised
  normalized vectors would make every provider responsible for a decision the core owns.
- **`TranscriptionProvider` and `MultimodalProvider` are not defined.** An interface with no
  implementation and no consumer is the same "shape with no semantics" the proposal §1 indicts
  `providers:` for having been for a milestone.

**`whisper_cpp` and `openai`, reconciled.** They are two different mismatches:

*`openai` is missing from the documented list.* `DocumentedProviderTypes` is
`["anthropic","ollama","whisper_cpp"]` and its own comment claims it mirrors doc 01
(`validate.go:165-168`); the build plan requires an openai implementation (`05:48`). The fix is
ordered, and the order is load-bearing rather than cosmetic: **PR 1 adds an `openai` entry to doc
01's `providers:` block; PR 6 adds `"openai"` to the Go list.** Reversed, the Go comment's claim
would be false for the interval between two merges — and no gate would say so, because
`TestValidate`'s round-trip only checks that every listed type validates. PR 1 also replaces
`embedding: { provider: ... }` with `{ provider: local_llama }`, the only declared entry ADR-0003
says can embed. Neither edit disturbs M0's config↔doc gate: it compares key *paths* and unions
field names across `providers:` entries, and an openai entry introduces no field name the existing
four do not already use (`config.go:52-59`).

*`whisper_cpp` is a documented type with no M1 interface.* It implements `TranscriptionProvider`
and is bound to `audio_transcription`, which M1 does not resolve. It stays a valid type: a config
that names it must keep loading (`spec R5.2`'s stance, carried forward). The consequence is a
boundary PR 6 must not cross: **the new task→provider check verifies that a task's `provider`
names a declared entry, and nothing more.** It must not check that the named provider's type
implements the interface the task needs — that check requires interfaces M1 does not define, and
doc 01's own example config would fail it. A type↔task compatibility matrix is a real idea for a
later milestone and is recorded here as deliberately not-now.

**The fake provider's replay key (proposal R8).** `testdata/llm/format.md:42` warns that `prompt`
is fragile because classify's real prompt is built from beliefs and the clock. The fake is
therefore constructed from an **ordered script of case ids**; each `Complete` call pops the next
id, loads it through `goldenset.Load`, and returns its `response` — or its `error` as a Go error.
Keying on the case id is what `format.md` recommends; making it a *script* rather than a lookup
buys two properties for free: a pipeline that makes an unscripted extra call fails immediately,
and a test that scripts more calls than the pipeline makes fails at cleanup. Keying by task name
was rejected (two classify calls in one test are indistinguishable), and keying by prompt or a
prompt hash was rejected by `format.md`'s own warning. The fake **records every prompt it saw**,
so a test can still assert that beliefs and the local date reached the prompt — the recorded
`prompt` field stays documentation and becomes assertable, without ever being the lookup key.

The fake `EmbeddingProvider` derives a deterministic vector from a hash of the input text and
reports a model name fixed at construction, so two fakes with different model names can populate
the two-model vault I21 exists for (Phase B, PR 9).

### D8 — The pendingimpl promotion sequence, and the rule that retires the gate

Build tags are additive: an untagged file compiles into every build, including
`-tags pendingimpl`. That single fact generates the whole sequence.

| PR | Creates | Untags | Drops from `pending_symbols.txt` | State of `make pending-red` after |
|---|---|---|---|---|
| 2 | `unit.Status`, `unit.AllStatuses` | `i01_focus_never_persisted_test.go` **and** `tree_scan_test.go` | 2 lines | green — 3 symbols still undefined |
| 3 | `ports.UnitRepo` | `i03_units_never_deleted_test.go` | 1 line | green — 2 symbols still undefined |
| 8 (Phase B) | `recall.VectorQuery`, `recall.VectorIndex` | `i21_vector_search_filters_on_model_test.go` | 2 lines — the file is now empty | **fails**, unless that PR retires the gate |

Why the helper's tag drops in PR 2 and not on its own: untagging `scanGoTree` alone leaves it with
no untagged caller and lint reports `unused: func scanGoTree is unused` (proposal §4.7, measured);
leaving it tagged while promoting I01 breaks the *untagged* build with `undefined: scanGoTree`,
which is `make check` red, not merely `make pending-red`. Binding the two together is the only
ordering that is green at every commit. After PR 2 the helper is untagged and therefore still
visible to the still-tagged I03 — the additive-tags property working in the sequence's favour.
Nothing else needs untagging: `repoRootFromCaller`, the other helper both tests use, has been in
the untagged `store_api_test.go` all along.

**The terminal rule, stated as a rule rather than a schedule.** `pending-red.sh` runs
`go test -c -tags pendingimpl` first and fails when it *succeeds* (`:9-19`), before it ever reads
the symbols file (`:31`). There is no empty-list short-circuit. So:

> **Whichever PR removes the last line from `pending_symbols.txt` retires the gate in that same
> PR.** It is bound to the last promotion, not to PR 8's identity — if the owner reorders, the
> retirement travels.

Retirement is five edits and one that is not in the repository:
delete `scripts/pending-red.sh` and `test/conformance/pending_symbols.txt`; drop `pending-red`
from `Makefile:39`'s `check-all` and its target block (`:93-95`) and from the header comment at
`:12`; remove the `pending-red` job from `ci.yml:107-115`; update `docs/06-harness.md` §6's table
row and §8 point 5, and `CLAUDE.md`'s Workflow section, which both name the gate. **And remove
`pending-red` from the branch ruleset's required status checks on GitHub** — a required context
whose job no longer exists never posts, and a context that never posts never becomes satisfied, so
leaving it registered blocks every future merge to `main` forever. That is the same failure mode
M0's design §7 records for un-registered matrix legs, running in the opposite direction, and no
artifact in this repository currently records it.

One more untracked step, once per promotion: `internal/core/unit/doc.go:5-14`,
`internal/ports/doc.go:9-18` and `internal/core/recall/doc.go:5-14` each carry a
"Pending conformance anchor" paragraph instructing the reader to do exactly what that PR is doing.
Each promotion PR deletes its own paragraph, or the tree keeps shipping instructions that have
already been carried out.

### D9 — The ≥90 % core coverage floor is met by making the domains enumerable, and guarded by a cheap proxy in the fast loop

The floor's mechanics decide the design more than the number does. `make cover` runs
`go test -coverprofile -coverpkg=./internal/core/... ./internal/core/...`
(`core-coverage.sh:56`), so **only test binaries under `internal/core/...` contribute**. An L2
conformance test that exercises `unit.ValidateTransition` through the fake adds nothing to the
floor; a core package tested only from L2 reads as 0 %. Nothing records this today, and it is the
first thing an implementer would get wrong.

Three structural properties, not three reminders:

1. **Every exported function in `internal/core/unit` is total over a small, enumerable domain.**
   `Status` has four members, so `ValidateTransition`'s domain is the 4×4 matrix, `IsLive`'s is
   four cases, `ParseStatus`'s is four valid inputs plus a handful of invalid ones. Exhaustive
   table tests are possible *because* D1 closed the vocabulary — that is the closure's second
   payoff after I01.
2. **No unreachable arm.** A `default: panic(...)` in a switch over a closed vocabulary is an
   uncovered statement by construction. Every unmatched input returns an error the table can
   trigger instead.
3. **The matrix test is driven by `AllStatuses()` and asserts its own completeness.** It iterates
   `AllStatuses()` × `AllStatuses()` and looks each pair up in an expectation map, failing if the
   map's size is not `len(AllStatuses())²`. A status added later without a matching expectation
   fails loudly rather than defaulting to "illegal" — D10's non-empty-corpus guard applied to a
   matrix.

That still leaves the proposal's R1 — the floor never runs in `make check` (`Makefile:36`), so the
fast loop cannot catch it. Moving `cover` into `check` was considered and rejected: the Makefile
header states the fast/full split deliberately, and Phase A is not the change that revisits it.
Instead PR 2 adds a **proxy that does run in the fast loop**: an untagged L2 test asserting that
every directory under `internal/core/` holding an exported top-level declaration also holds at
least one `_test.go`, and that each exported name appears in that directory's test sources.

Its honesty is part of the decision. It is a *proxy*, not the floor: an identifier mentioned only
in a comment satisfies it, and it says nothing about branch coverage. It catches exactly one
thing — a new exported core symbol shipped with no L1 test at all — which is R1's dominant failure
mode. It is announced as a proxy in its own doc comment, the way
`golden_sets_test.go:164-176` announces its literal-substring proxy. Until PR 2 lands it finds
zero declarations and reports "armed but vacuous", mirroring `core-coverage.sh:102-105` word for
word rather than passing with a bare OK.

---

## 3. Package layout and dependency map

```
internal/core/unit/               PR 2 — pure, stdlib only
  ├── doc.go                      loses its pending-anchor paragraph
  ├── status.go                   Status, the four constants, AllStatuses, ParseStatus, IsLive
  ├── transition.go               the unexported table, ValidateTransition, the sentinels
  ├── type.go                     Type, the nine constants, AllTypes, ParseType
  ├── unit.go                     the Unit struct
  └── *_test.go                   L1 tables — the floor's whole numerator for Phase A

internal/ports/                   PR 3, PR 5 — no build tag, no golden
  ├── doc.go                      loses its pending-anchor paragraph (PR 3)
  ├── unitrepo.go                 UnitRepo + ErrUnitNotFound / ErrUnitExists / ErrStatusConflict
  └── provider.go                 LLMProvider, EmbeddingProvider and their request/response types
      imports: context, time, errors, internal/core/unit

internal/store/sqlite/            PR 4 — widens testdata/schema/store_api.golden
  └── unitrepo.go                 the UnitRepo implementation; positive status='pool' filter

internal/providers/               PR 6
  ├── anthropic/  openai/  ollama/    HTTP clients, tested against httptest
  └── (no whisper adapter — D7)

internal/config/                  PR 6 — validate.go gains the task→provider reference check
                                        and "openai" in DocumentedProviderTypes
cmd/nooma/                        PR 6 — tasks: routing, resolved once at startup

test/support/memrepo/             PR 3 — the in-memory UnitRepo (D6)
test/support/repocontract/        PR 3 — RunUnitRepo, called from L2 and L3
test/support/fakeprovider/        PR 5 — the scripted replay fake (D7)
```

Dependency-rule check: `internal/core/unit` imports `context`? No — the core takes no context.
It imports `time`, `errors`, `encoding/json`, and nothing else; `forbidigo` bans `time.Now`, not
the package (`.golangci.yml:104-109`). `internal/ports` importing `internal/core/unit` is legal —
`core-purity` scopes to files *under* `internal/core`, and the arrow points away from the core, so
no cycle exists. `sqlite-containment` is untouched: nothing new imports `database/sql` outside
`internal/store`. `docs-sync.yml` fires on `^internal/core/` only (`docs-sync.sh:45`), so **PR 2
is the only Phase A PR that needs a doc 02 delta** — and it has three real ones (D2's positive
filter, D3's transition table, D3's expiry landing status). PRs 3–6 touch no core file; the
proposal's §5 rows suggesting otherwise for PR 3 can be read as optional.

`store_api.golden` is regenerated in **PR 4 only**, via `make store-api-golden` — a different
target from `make schema-golden`, and forgetting it turns `make check` red immediately.

`docs/06-harness.md` §1's tree gains no line in Phase A: `unit`, `recall` and `relation` are
already listed. `core/classify` is Phase B's addition, carried in PR 1's docs sweep.

---

## 4. Interfaces this change fixes

```go
// internal/core/unit
type Status string
const (
    StatusPool       Status = "pool"
    StatusArchived   Status = "archived"
    StatusSuperseded Status = "superseded"
    StatusIncomplete Status = "incomplete"
)
func AllStatuses() []Status
func ParseStatus(s string) (Status, error)
func (s Status) IsLive() bool
func ValidateTransition(from, to Status) error
var ErrUnknownStatus, ErrIllegalTransition error

type Type string   // nine members, AllTypes / ParseType / ErrUnknownType — D4

type Unit struct {
    ID              string
    Type            Type
    Content         string
    Status          Status
    Weight          float64
    WeightDecayRate float64
    LastTouchedAt   time.Time
    StructuredData  json.RawMessage   // nil when the column is NULL
    Source          string
    EventAt         *time.Time        // nil is not the zero time — I18
    DueAt           *time.Time
    Confidence      *float64          // always nil in v1 — proposal §8 Q2
    CreatedAt       time.Time
    UpdatedAt       time.Time
}
```

Nullable columns are pointers and `json.RawMessage`, never `sql.NullX`: `database/sql` is denied
inside `internal/core` (`.golangci.yml:76`), and the pointer form is the precedent
`goldenset.ClassifyExpected` already set for exactly this reason — "absent" and "a legitimate
zero" must not decode to the same value (`test/support/goldenset/types.go:152-165`).

Timestamps are `time.Time` in the core; the `TEXT` encoding doc 03 specifies is the SQLite
adapter's business, decided in PR 4.

---

## 5. Data flow

Phase A wires nothing. What it fixes is the shape the Phase B pipeline will pour through:

```
brain (Phase B)                    core (Phase A)              ports (Phase A)
  clock.Now() ── once ──┐
                        ├──→ unit.ValidateTransition(from,to)
                        │        └─ error, or nil
                        └──→ Unit{…, at} ──────────────────→ UnitRepo.Create
                                                             UnitRepo.SetStatus(from,to,at)
                                                                 │
                                       memrepo.Units ────────────┤  same contract suite
                                       sqlite.UnitRepo ──────────┘  (repocontract.RunUnitRepo)

cmd/nooma (PR 6)
  tasks: name ──→ resolved at startup ──→ ports.LLMProvider  (never resolved at capture time)
```

The one property worth reading off this diagram: the instant enters at the top, once, and travels
as a value. Nothing below `brain` can obtain a different one.

---

## 6. Test matrix

| What | Level | Where | PR |
|---|---|---|---|
| `Status` vocabulary, `ParseStatus`, `IsLive`, the 4×4 transition matrix, `Type`/`ParseType` | L1 | `internal/core/unit/` | 2 |
| I01 — `focus` is in no vocabulary and no `Status` literal in the tree | L2 (promoted, untagged) | `test/conformance/` | 2 |
| `AllStatuses()` equals migration 0001's `units.status` documented domain | L2 | `test/conformance/`, via `migrationSQLText` | 2 |
| Every exported decl under `internal/core/` has a sibling `_test.go` naming it (D9's proxy) | L2 | `test/conformance/` | 2 |
| I03 — no removal method on `UnitRepo`, no `DELETE FROM units` in the tree | L2 (promoted, untagged) | `test/conformance/` | 3 |
| The `UnitRepo` contract, against the in-memory fake | L2 | `test/conformance/` → `repocontract.RunUnitRepo` | 3 |
| The **same** contract, against a real temporary vault | L3 | `test/integration/` → `repocontract.RunUnitRepo` | 4 |
| `LiveByIDs` omits `archived`/`superseded`/`incomplete` and preserves `ids` order | L2 + L3 | a contract case, answered twice | 3, 4 |
| No non-test file under `internal/` or `cmd/` imports `test/support/` | L2 | `test/conformance/` | 3 |
| No non-test file under `internal/store/**` references `time.Now` (R9's store half) | L2 | `test/conformance/` | 4 |
| `store_api.golden` regenerates clean after `make store-api-golden` | L2 | existing test | 4 |
| The scripted fake: over-run fails immediately, under-run fails at cleanup, a recorded `error` surfaces as a Go error | L1 | `test/support/fakeprovider/` | 5 |
| `assertCasesDirIsEmpty` inverted into a non-empty-corpus guard | L2 | `test/conformance/golden_sets_test.go` | 5 |
| Anthropic / OpenAI / Ollama clients against an in-process `httptest` server | L1 | `internal/providers/*/` | 6 |
| A task naming an undeclared provider fails validation | L1 | `internal/config/` | 6 |

An in-process `httptest` listener is not "the network" that `docs/06-harness.md` §3 forbids —
the same distinction M0's design §8 drew for `serve`'s ephemeral loopback port. No test in Phase A
reaches a real provider; every response comes from `testdata/llm/cases/` or from a handler the
test itself wrote.

---

## 7. Risks this design accepts

| # | Risk | Position |
|---|---|---|
| 1 | **PR 2 will exceed the 400-line ceiling.** It carries `Status`, `Type`, `Unit`, transitions, exhaustive L1 tables, the I01 promotion, the `tree_scan_test.go` untag, two new L2 guards and a doc 02 delta | Accepted, with the split line drawn in advance rather than discovered during apply: **2a** = `Status` + `AllStatuses` + `ParseStatus` + `IsLive` + the I01 promotion + the `tree_scan_test.go` untag + the vocabulary/DDL guard; **2b** = `Type` + `Unit` + transitions + D9's presence guard. The untag must travel with 2a, because that is the PR that promotes I01 |
| 2 | **`unit.Status("focus")` compiles.** The type does not make an invalid value unrepresentable | Accepted and stated in D1. Validity is a boundary property enforced by `ParseStatus`, the closed `AllStatuses()` vocabulary, and I01's promoted vocabulary check. The alternative shapes that would close it fail the conformance test that was written first |
| 3 | **The fake and the SQLite repo can still diverge on anything the contract suite does not assert** | The contract suite is the only mechanism, and it is honest about that. The standing rule — widen the port and the contract in the same PR — is what keeps it from decaying. A port widening that lands without a contract case is a review failure with no gate behind it |
| 4 | **D9's presence guard is a proxy, not the floor.** A name mentioned in a comment satisfies it | Accepted and announced in its own doc comment, following `golden_sets_test.go:164-176`'s precedent. It catches the dominant R1 failure — an exported core symbol with no L1 test — and claims nothing more. `make cover` remains the real gate, in `check-all` and CI |
| 5 | **Retiring `pending-red` (Phase B) must also drop the required status context in the GitHub ruleset**, or every future merge to `main` blocks on a check that never posts | Recorded in D8 as part of the retirement, not as follow-up. It is a GitHub-side change no Makefile or workflow file can make |
| 6 | **Doc 02 does not name the status an expired `incomplete` unit lands in.** D3 writes `archived` into doc 02 §1 | The owner accepts a doc 02 edit in PR 2, or names a different landing status. Leaving it unstated means M2 invents one under time pressure, and the only alternatives are a fifth status (new migration, doc 03 edit, regenerated golden) or a deletion (I03) |
| 7 | **`depguard`'s `files: ["**/internal/core/**"]` carries no `$test` selector**, so this design assumes it covers `_test.go` files under the core. Not executable in this session | If the assumption is wrong, D6's placement does not change — it rests on the store golden, the production binary, and reachability from L3/L4, not on depguard. The only affected sentence is the parenthetical about why L1 cannot use the fake |
| 8 | **Adding `"openai"` to `DocumentedProviderTypes` before doc 01 documents it** would make `validate.go:165-168`'s own comment false, and no gate would report it | The PR 1 → PR 6 dependency in proposal §5 is load-bearing, not sequencing convenience. Stated in D7 so it survives a reordering |
| 9 | **Q2 (`units.confidence`) is still open.** `Unit.Confidence` is `*float64` and Phase A never sets it | Safe under the recommended answer (doc 02 claims the column, v1 writes NULL) and under Q2-A. Only Q2-B (drop the column) would force a struct edit, and the proposal recommends against it |
| 10 | **Two new L2 guards and one new support package are scope this phase invents**, beyond the proposal's PR contents | Accepted under `docs/06-harness.md` §6's precedence rule — a rule a machine can execute is a gate, not a skill. Each closes a named proposal risk (R1, R9's store half) or a two-source-of-truth this design created by closing a vocabulary. Together they are roughly 200 lines across PRs 2, 3 and 4 |

---

## 8. What this design does not decide

- **The tolerant decoder** (I14) and classify's own vocabulary — Phase B, PR 7.
- **`VectorQuery`, `VectorIndex`, top-K and RRF fusion** — Phase B, PR 8. Their reflective shape
  is already pinned by the pendingimpl I21 test; their behaviour is not.
- **The `TEXT` encoding for timestamps** the SQLite adapter writes — PR 4's business, bounded by
  doc 03.
- **Whether `internal/ports` gets its own exported-surface golden.** Considered: it would make
  every port widening reviewable the way the store's does. Not adopted — the port surface is small,
  every widening already carries a contract case, and the I03 reflection check is a partial golden
  for `UnitRepo` already. Revisit when `ports` holds more than four interfaces.
- **A type↔task compatibility matrix** in config validation (D7) — a later milestone.
- **Everything gated on the proposal's open questions**: the relation-thresholds fallback (Q1),
  the recall entrance (Q3b), correction referent resolution (Q3c). None of them touches Phase A,
  which is why Phase A could be designed first.
- **`internal/brain`'s services.** Phase A ships no orchestration; the one-clock-read-per-operation
  rule is stated here as the port's shape, and its brain-side half is Phase B's.
