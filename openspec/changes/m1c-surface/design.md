# Design — M1 Phase C: the surface

Technical design for `m1c-surface`, the third and last of the three chained changes
[`m1-capture-recall/proposal.md`](../m1-capture-recall/proposal.md) §5 splits M1 into. Phase A
(`m1a-substrate`) and Phase B (`m1b-pipeline`) are **complete**: the capture pipeline runs, the
relation judge runs, and nothing a user can reach calls either of them.

This design settles what Phase B deliberately left open and what the proposal scheduled here: how
`core/recall` grows a fusion that keeps its scores, where the margin gate lives and what "ask"
looks like as a Go value, how a correction finds its referent and which column it writes, how
ADR-0016's audit-before-edit ordering is made structural, what the HTTP and CLI surfaces are, and
— PRs 15, 16 and 17 — `nooma init`'s two provider paths, `nooma doctor`'s structured-JSON quality
gate, and the OpenAI embeddings client without which the milestone's judged path cannot run.

It is written against [`spec.md`](spec.md). Where this design and that document disagree, the
disagreement is in §2 with the evidence for both sides. It does not restate requirements.

**Scope**: the umbrella proposal's §6 Phase C table, now six rows — PR 12 `feat/corrections`, PR 13
`feat/httpapi-capture-recall`, PR 14 `feat/cli-capture-demo`, PR 15 `feat/init-provider-paths`,
PR 16 `feat/doctor-quality-gate`, PR 17 `feat/openai-embeddings`. **PR 14 is last in time despite
its number** (`proposal.md:485-489`: `(13,15,16,17) → 14`): it is the demo, the demo is what tags
`v0.1.0`, and a demo that needs a provider configured by hand, against a provider nothing checked,
with a leg that silently does not run, is the exit criterion being walked around rather than met.
§6 encodes that ordering.

**A scope note, stated before anything rests on it.** Phase C is the first change in this milestone
where a human being can drive the brain. Every Phase A and Phase B behaviour was proven by a test
and by nothing else; from PR 13d onward there is a compiled binary to point at. That changes what
"done" means: a green suite is necessary and no longer sufficient, and §6's last row is a demo run
by hand because the proposal's own success criteria end with one.

**Revision history, because two of this document's revisions changed decisions rather than
polish.**

- **Revision 2** corrected six defects found by a fresh-context reviewer with a shell, who
  re-executed roughly twenty of §1's `file:line` citations. Rows carrying those corrections are
  marked ✎.
- **Revision 3** folds in three owner rulings: **C9** — build the OpenAI embeddings client, as PR
  17; **C6** — a correction writes one field, D3 stands and `spec.md` R1.8 is overruled; **C8** —
  `nooma capture` is an HTTP client, D11 stands. It adds **D17** (the client) and **D18** (the
  mechanism that makes C9's failure shape visible if it recurs), and specifies the two things C8's
  ruling asked for: what the CLI does when no server is running, and how it gets the token off a
  non-loopback bind.
- **Revision 4** discharges the reconciliation obligation revision 3's §9 set for itself.
  `spec.md` revision 3 has landed and carries every ruling; **C5 and C7 are closed by it**, and
  §2's reconciliation notes record three items its landing created — an unreachable requirement
  clause, an internal level disagreement, and a citation drift. It decides `spec.md` §10's open
  item on which PR performs the embedding binding (D15), states which of *configured*, *fit* and
  *effective* each D18 mechanism answers, and files **one new conflict, C10**, which it does
  **not** resolve: the spec asked for the outcome D18b delivers and forbade the port surface that
  delivers half of it.
- **Revision 5** closes C10 and N2 against `spec.md` revision 4, which adopted this design's
  recommended branch on both. **No conflict this design filed remains open.** Three places moved —
  C10's entry, §8's risk row 0 and §9 — plus D18b's own blocked-row note, which would otherwise
  have become the stale note C10's closing paragraph warns about. Nothing else changed.

---

## 1. Ground truth this design was verified against

Every row was checked by reading the named file at the named line, or by a `Glob`/`Grep` over the
working tree. **This session had no shell**, so nothing here was verified by running a command;
rows marked ✎ carry a correction or a confirmation from the gate reviewer's audit, which did. Rows
that could not be verified at all say so in plain words — this chain has already shipped one
requirement asserting "verified present" about something that was not, and one corpus fixture whose
central number no engine ever produced.

### 1.1 The code Phase C builds on, as it actually exists

| Claim | How it was verified |
|---|---|
| `recall.Fuse(lists ...[]string) []string` computes RRF scores into a local `scores` map and **discards it**: built at `:56`, written at `:63`, read only inside the sort comparator at `:73-77`, and the function returns `ids` | `internal/core/recall/fuse.go:55-83` |
| `Fuse`'s tie-break is already three-level and already documented: higher score, then earliest list index, then lexicographic | `fuse.go:49-54`, `:71-80` |
| `TestFuse_BreaksTiesDeterministically` exercises the earliest-list and lexicographic levels **separately**, and `TestFuse_ReproducesADR0010ByHand` pins score-derived ordering | `internal/core/recall/fuse_test.go` — confirmed by the gate reviewer's audit ✎ |
| `test/conformance/recall_corpus_test.go` asserts **id order only**; it never reads a score value, and `Fuse` has none to give it | `test/conformance/recall_corpus_test.go` — confirmed by the gate reviewer's audit ✎ |
| `RRFK = 60`, `RecallTopK = 20`, `WeightVector = WeightLexical = 1.0` are declared constants; `fuseWeight(i)` maps list index → weight | `fuse.go:9`, `:14`, `:22-25`, `:33-42` |
| `recall.Scored` is `{ID string; Score float32}` and is the **vector leg's** dot-product result, not a fusion result | `internal/core/recall/vector.go:32-36`, `:95-98` |
| `brain.RecallService.Candidates(ctx, content, vector, model, excludeID) ([]unit.Unit, error)` fuses two id lists and resolves them through **one** `LiveByIDs` call | `internal/brain/recall.go:51-90`, esp. `:83`, `:85` |
| `RecallService` holds `index`, `lex`, `units` — **no `ports.EmbeddingProvider`**, so it cannot embed a query of its own | `internal/brain/recall.go:25-34` |
| `captureRunner` constructs a fresh `RecallService` per capture rather than holding one | `internal/brain/capture.go:305` |
| `ports.UnitRepo` declares exactly five methods; `ByID` is documented as "at any status — the deliberate escape hatch **corrections** and audit need" | `internal/ports/unitrepo.go:30-57`, esp. `:35-38` |
| `UpdateContent(ctx, id, content string, at time.Time)` is the only update method; its SQL sets `content` and `updated_at` and nothing else | `internal/ports/unitrepo.go:45-48`; `internal/store/sqlite/unitrepo.go:122-131` |
| `units.event_at` and `units.due_at` are nullable `TEXT`; `units.content` is `NOT NULL` | `0001_core_tables.sql:9`, `:16-17` |
| `learning_signals` exists with `target_id TEXT` carrying "NO FK: the signal outlives the target's deletion", and `signal_type`'s comment enumerates eleven values beginning with `correction` | `0002_learning_and_search.sql:8-19` |
| Nothing in the tree writes `learning_signals`: no `ports.SignalRepo`, no `memrepo` fake, no `repocontract` suite | glob of `internal/**/*.go` and `test/support/**/*.go` |
| `ports.DecisionAction` holds twelve members; **four have no production caller** — `capture.classify.unparseable`, `capture.classify.unclassifiable`, `capture.discarded`, `capture.dedup.judged` | `internal/ports/decisionlog.go:32-45`; `Grep` over `*.go`: every hit outside that file is in `test/support/repocontract/decisionlog.go` |
| `captureRunner.at` returns a plain Go error for the four non-persisting kinds that are not `timer`/`recurring_reminder` — `chitchat`, `out_of_scope`, `recall`, `correction` | `internal/brain/capture.go:165-168`, and its own doc comment at `:134-139` |
| `CaptureResult.Deferred`'s doc comment states "non-nil **exactly when** `Stored` is false" — a claim Phase C breaks the moment a second non-storing outcome exists | `internal/brain/result.go:63-66` |
| `classify.Classification` carries `NormalizedContent *string`, `EventAt *time.Time`, `DueAt *time.Time` and no field naming which of them a correction means | `internal/core/classify/classification.go:25-48` |
| `Kind.UnitType()` returns `false` for `correction` — a correction never becomes a unit | `internal/core/classify/kind.go:47-66` |
| The corpus case `correction-not-friday` carries **both** `normalized_content` ("Dentist appointment is on the 15th, not the 14th") **and** `event_at`, and labels neither | `testdata/classify/cases/correction-not-friday.json:6`, `:9` |
| `httpapi.Handler(version string)` mounts exactly two routes, `GET /{$}` and `GET /ui`, and an unknown path is a 404 by deliberate choice | `internal/httpapi/server.go:19-33` |
| `cmd/nooma`'s dispatch table holds five commands and is the source of truth for usage; "a command appears here only when it works" | `cmd/nooma/main.go:37-68` |
| `runServe` resolves the vault, validates config, **decides the binding**, takes the write lock, opens SQLite, and passes only a version string to `httpapi.Handler` | `cmd/nooma/serve.go:41-94` |
| `vaultlock.Acquire` is exclusive and `runServe` holds it for the process's lifetime | `cmd/nooma/serve.go:70-78` |
| **`runStatus` never takes the write lock**, and says so in its own words: *"It is read-only in the strong sense: it never takes the write lock (R8.4). An implementation that acquired in order to inspect would make `nooma status` useless against a running instance."* It uses `vaultlock.ReadHolder` | `cmd/nooma/status.go:27-30`, `:55` ✎ |
| **Only `ollama` ships an `EmbeddingProvider`.** `internal/providers/` holds `anthropic/client.go`, `openai/client.go`, `ollama/client.go` and **`ollama/embed.go`** | glob of `internal/providers/**/*.go` ✎ |
| `ollama.Client.Embed` is a **method on the same `Client`** that implements `Complete` — one client type per provider package, one method per port. That is the shape PR 6 established and PR 17 follows | `internal/providers/ollama/embed.go:33` |
| `ollama.Embed` returns `ports.EmbedResponse{Vector: parsed.Embeddings[0], Model: parsed.Model}` — the **response's** echoed model, not the request's — and treats an empty vector array as a Go error rather than a zero-value response | `ollama/embed.go:66-70`, `:29-32` |
| `openai.Client` is `{baseURL, apiKey, model string; httpClient *http.Client}` with `var _ ports.LLMProvider = (*Client)(nil)`, a `defaultBaseURL`, and an overridable `baseURL` so tests point at `httptest` | `internal/providers/openai/client.go:16-41`, `:26` |
| The openai chat client sets `Authorization: Bearer <key>` | `internal/providers/openai/client.go:78` |
| `config.DocumentedTaskNames` holds seven tasks, including `embedding`; `DocumentedProviderTypes` is a list of provider **types**, and the `providers:` map is keyed by entry name, so one type may appear more than once | `internal/config/validate.go:185-202` |
| `test/support/memrepo` holds fakes for units, embeddings, lexical, decision log and relations; `test/support/repocontract` holds four contract suites | glob of `test/support/**/*.go` |

### 1.2 The gates Phase C has to satisfy

| Claim | How it was verified |
|---|---|
| `depguard`'s `core-purity` allows `internal/core/**` only `$gostd` and `github.com/rengo/nooma/internal/core` — a prefix, so a new core package may import `core/recall` and `core/classify` | `.golangci.yml:52-62`; already exercised by `core/relation` importing `core/classify` |
| `forbidigo` bans `time.Now`/`rand.`/`uuid.`/`os.Getenv` inside `internal/core/` only | `.golangci.yml:101-124` |
| `docs-sync.sh` fires on `^internal/core/` **per pull request** | `scripts/docs-sync.sh:45-62`, and **C9** of `m1b-pipeline/tasks.md` — the record of this gate firing three times in one chain |
| The `internal/core` coverage floor is 90 % and measures only test binaries under `internal/core/...` | `scripts/core-coverage.sh:45`, `:56`, `:111` |
| **`make pending-red` no longer exists.** Retired in `714934e` once I21 was promoted — verified by execution during the gate review ✎. **Strict TDD is still mandatory** (`CLAUDE.md` non-negotiable #4) but is now discipline backed by review, not a Makefile target | gate reviewer's audit ✎ |
| `docs/06-harness.md` §1's package tree lists **ten** `core/` packages and **does not list `correction/`** ✎ | `docs/06-harness.md:21-53` |
| `docs/06-harness.md` §4's invariant table is headed "Initial extraction" and ends at **I21** | `docs/06-harness.md:167-192` |
| `docs/06-harness.md` §4 names I03 and I13 as structural tests: "a `DELETE FROM units` outside the migrations" and "the migration declares no FK on `learning_signals.target_id`" | `docs/06-harness.md:196-198` |
| `store_api.golden` is regenerated with `make store-api-golden`, a different target from `make schema-golden` | `m1b-pipeline/tasks.md` 9b.3, 11b.3 |

### 1.3 The documents that govern

| Claim | How it was verified |
|---|---|
| Doc 02 §5 step 4 already carries Q3c's full answer: an identifier wins where the caller has one; chat resolves by running step 2's recall and gating on a **ratio** of the top two scores; the gate is "a pure function of the scored candidates: no LLM, no I/O, no clock"; and it "needs `internal/core/recall` to expose a fusion that keeps its scores" | `docs/02-cognitive-core.md:170-193` |
| Doc 02 §5 step 4 also carries ADR-0016: "The row is written first — if it fails, the edit does not happen" | `02:187-193` |
| Doc 02 §13 **already lists** `correction_referent_margin` = 1.5, beside `recall_top_k`, both RRF weights and `dedup_candidate_k` | `02:496-500` |
| Doc 02 §5 step 2: "Same mechanism serves both answering a `recall` and finding connection candidates" | `02:152-153` |
| Doc 02 §5's product rule: "Nooma captures with what it has… and **only asks when ambiguity blocks it**" | `02:204-206` |
| Doc 02 §5.1: an **optional** field's absence "is the ordinary case, not a loss"; only a required field's absence is reported | `02:274-278` |
| Doc 02 §5.1: a wrong-shaped value and an out-of-vocabulary value "are recorded as different events… one is a formatting failure, the other a vocabulary failure" | `02:241-245` |
| Doc 02 §9 names `correction` as a `learning_signals` type with a positive/negative/neutral valence and a target, and repeats "No FK to the target" | `02:410-415` |
| Doc 02 §11: "Every automatic decision with an effect is recorded with its reasoning" — at **`02:449-452`**, inside §11, which begins at line 449 ✎ | revision 1 cited `02:266-270`, which is §5.1 |
| ADR-0016 fixes: the pre-image goes in `decision_log.context`, **in the same row**, written first, with the exact keys left to PR 12 and doc 02 §5 as their home | `docs/adr/0016-correction-pre-image.md:58-70`, `:84-86` |
| ADR-0010 requires `k` and each list's relative weight to be named constants, and states RRF's own cost is that it discards magnitude | `docs/adr/0010-hybrid-recall-fusion.md:48-49`, `:65-69` |
| ADR-0007's decision is bind-time only, its header reads "**Enables**: M4 (tolerable unresolved through M0–M3)", and **no request-time token check exists anywhere in the tree** | `docs/adr/0007-http-auth.md:7`, `:29-36`; `Grep` over `*.go` |
| ADR-0007's UI half is a **cookie handshake**, tied to the server-rendered UI, which is M4 | `0007:41-53` |
| `docs/01-architecture.md`'s CLI table lists **nine** commands, `capture` absent, and four of the nine are unbuilt — so it is a **promise table**, not a record of what exists ✎ | `docs/01-architecture.md:141-153` |
| `docs/01-architecture.md` describes the HTTP surface only as "Exposes an HTTP API on `localhost:7777`" — no route-level promise ✎ | `docs/01-architecture.md:101` |
| `docs/01-architecture.md` frames `nooma doctor` as reporting "provider unreachable → how to install it" — unreachability is already its own category | `docs/01-architecture.md:155-157` |
| `docs/03-data-model.md` already promises `nooma doctor` runs "`PRAGMA integrity_check` + units↔embeddings↔fts consistency" | `docs/03-data-model.md:306-307` (via `m1b-pipeline/design.md` §1.3) |
| **`proposal.md` §6 now carries six Phase C rows**, adding **PR 17 `feat/openai-embeddings` (~200)**, with the note "*a degradation designed for an outage was absorbing a gap in the build plan*" and "Anthropic publishes no embeddings API, so OpenAI is the provider" | `m1-capture-recall/proposal.md:424-450` ✎ |
| **The dependency line is now** `6 → 15`, `(5,6) → 16`, **`6 → 17 → 15`**, `(13,15,16,17) → 14`, and the proposal carries the reason: `internal/config/validate.go:177` rejects a `tasks.<name>.provider` naming an absent provider, so "the wizard cannot offer a complete Cloud path before a complete Cloud path exists" | `proposal.md:450-460`, `:495-499` ✎ |
| **`spec.md` revision 3 has landed** and carries every ruling: R1.8 is single-field, R3.1 is the HTTP client, R2.9–R2.12 specify ADR-0017's request-time auth into PR 13, §6 is PR 17, and R1.2/R2.1/R2.2 are reworded for contract. Its sections renumbered — the old §6–§9 are now §7–§10 | read in full for this revision ✎ |
| `config.Provider` carries an `Endpoint` field (`yaml:"endpoint"`) on a union struct whose doc comment says "no single entry populates all of them" — so an `openai` entry may carry one, and `openai.NewClient` already falls back to `defaultBaseURL` when `baseURL` is empty | `internal/config/config.go:47-59`; `internal/providers/openai/client.go:33-41` ✎ |
| `spec.md` R8.1 assigns R6.3's Cloud-vault-embeds test to **L4**, while R6.3's own "Verified by" clause names **L2** | `spec.md:1071-1090`, `:1175-1177` ✎ |
| `spec.md` R7.3 permits edits to Phase A/B files only for R1.2, R1.3, R1.7 and R6.1; R7.4 enumerates the `internal/ports` files that stay untouched and sanctions exactly two edits — `unitrepo.go`'s two methods and `decisionlog.go`'s two actions. **`embeddingrepo.go` appears in neither the permission nor the enumeration** | `spec.md:1130-1152` ✎ |

---

## 2. Conflicts

Ten disagreements have been found inside this change's inputs. **Eight are closed** — five by owner
rulings, three by `spec.md`'s own revisions. Their evidence is kept and each resolution recorded,
per this project's practice of never deleting the disagreement that preceded a decision. The
remaining two are gaps in doc 02 that the rulings tell us how to close. **None is open.**

Three further items are **reconciliation notes** rather than conflicts — nothing disagreed about
what should happen, but `spec.md` revision 3's landing left a clause unreachable, a level assigned
twice, and two quotations pointing at text this design had since rewritten. They are §2.1, and two
of the three are now closed too.

**A resolved conflict is edited to say so, in the same pass that learns it.** This project has been
bitten by the opposite twice: `m1b-pipeline/tasks.md`'s own C2 and C3 are both *"already resolved
in `spec.md` itself; `design.md`'s own note describes the pre-correction wording as if still
live"*. C10 below was very nearly the third — see its own closing note.

### C1 — `nooma init`'s wizard and `nooma doctor`'s gate were claimed by Phase C in prose with no row in its table. **RESOLVED — scheduled as PRs 15 and 16.**

**Evidence for "in scope"**: `proposal.md` §3.2 items 14–15, verbatim: *"They belong to **Phase C**
(`m1c-surface`): both are CLI surface, both need providers to exist first."*

**Evidence for "not in the chain"** (as it stood): §6's Phase C table held three rows, none naming
`init` or `doctor`, and the dependency line closed the phase at PR 14.

**Resolution, owner, 2026-08-02 (PR #101)**: both are real rows now, and PR 14's edge became
`(13,15,16) → 14` — since widened again by PR 17. The proposal states the principle it extracted:
*"an item is scheduled when it is a row in a table that something reads; §3.2 is prose that nothing
executes."* Revision 1 of this design drafted them as conditional `14c`/`14d` **after** the demo; a
tasks phase cutting from it would have misordered the chain.

### C2 — Nothing in doc 02 says whether a correction's `normalized_content` is the referent's replacement body

**Side A — it is.** `NormalizedContent` is the unit's content everywhere else in the pipeline (doc
02 §5.1's table: when it degrades, "the unit has nothing to store or embed", `02:256`).

**Side B — it is not.** Q3c-iii's justification says `UpdateContent` "cannot write the `event_at`
that the corpus case expects". And the case's `normalized_content`, *"Dentist appointment is on the
15th, not the 14th"*, is a normalization of the correction **utterance**: it still carries "not the
14th", which is commentary about an edit rather than a memory.

**Position.** A gap in the governing document, closed by C6's ruling below and written into doc 02
§5 step 4 by PR 12c (D13).

### C3 — The proposal names two different tests for I13, and only one has an M1 producer

**Side A — the correction signal**, §2's success criteria and §3.4's row ("the first *write* into
`learning_signals`"). `spec.md` R1.10 agrees.

**Side B — the relation rejection**, §6 item 5, whose subject has no M1 producer by its own
parenthesis and whose invariant (I10) §3.3 lists as out of M1.

**Position.** Side A. D6 proves I13's behavioural half the way `docs/06-harness.md:198` frames it —
no foreign key — with an L3 case recording a signal whose `target_id` names a unit that never
existed. No rejection path is built.

### C4 — ADR-0007 demands a token to *start* an exposed server and nothing checks it on a *request*. **RESOLVED — ADR-0017 plus middleware, inside PR 13.**

**Evidence the gap was sanctioned**: ADR-0007's "**Enables**: M4 (tolerable unresolved through
M0–M3)", and a Decision section entirely about binding.

**Evidence it stopped being tolerable**: "tolerable" was measured against `GET /` and a UI
placeholder. Phase C mounts `POST /capture`, which writes the user's memory.

**Resolution, owner, 2026-08-02**: **ADR-0017 and the middleware both land inside PR 13** — the PR
that mounts the routes — so no commit exists where `POST /capture` is mounted and unprotected.
ADR-0007 is **not** edited and **not** superseded. D10 makes the guarantee structural rather than a
promise about commit sequence.

### C5 — `spec.md` R1.2's "`Fuse` itself is unchanged" vs. D1's reimplementation. **CLOSED by `spec.md` revision 3.**

R1.2's prior wording said `Fuse` itself is unchanged; D1 makes it a projection of `FuseScored` so
ADR-0010's formula has one implementation. The exported *behaviour* — signature, ordering,
three-level tie-break — is byte-identical.

R1.2 now reads "`Fuse`'s exported signature and behaviour (its returned ordering, including its
three-level tie-break) are unchanged" and adds a MUST NOT saying the requirement "**MUST NOT** be
read as forbidding `Fuse`'s own implementation from changing internally… two independent
implementations of the same `Σ w_i/(k + rank_i)` formula are two places for ADR-0010's own named
bias to live". That is D1's argument in the spec's own words. Nothing left to reconcile.

### C6 — What a correction writes. **RESOLVED — one field. D3 stands; `spec.md` R1.8 is overruled.**

**Spec side (overruled).** R1.8: *"for each of `{content, event date, due date}` present (non-nil)
in the correction's classification, capture calls the matching … method."*

**Design side (adopted).** `normalized_content` is **almost always present** — doc 02 §5.1 makes
its absence one of the two unsurvivable degradations — so under R1.8 there is no such thing as a
date-only correction: **every** correction would overwrite the referent's body with the model's
normalization of the correction utterance. For the corpus case that is survivable. For *"no, it's
Ana not Anna"* it is not: a unit reading "Meeting with Anna on Tuesday" becomes "It's Ana, not
Anna", and the memory is gone. ADR-0016 makes that recoverable in principle, and nothing reads the
pre-image back until M4.

**Position as filed** — kept verbatim because `spec.md`'s Conflict 3 quotes it as "design.md's own
words": *"This design recommends [the one-field] rule (dated fields win; content only when the
correction resolved no date; two dates ask), because it takes the write that requires no inference
and refuses the one that requires an unlicensed and destructive one."* It was filed as a
recommendation, not a unilateral resolution.

**Owner ruling, 2026-08-03**: dates win, content is the no-date fallback, two dates ask. **The
accepted cost is stated plainly rather than mitigated**: a correction that moves a date leaves the
body saying the old one until the user corrects the text too. That is a stale memory; the
alternative was a destroyed one. `spec.md` R1.8 is rewritten in full, with the same rule, its own
accepted-cost paragraph and three scenarios.

### C7 — `spec.md` R2.1/R2.2 named `CaptureResult.Stored`; D8 replaces it with a closed `Outcome` vocabulary. **CLOSED by `spec.md` revision 3.**

R2.1's prior enumeration described today's struct; R2.2's MUST was phrased around `Stored: false`.
Phase C adds four more non-storing outcomes, at which point `result.go:63-66`'s own documented
invariant is false.

Both are now "rewritten for contract, not shape". R2.1 requires that the route map "every distinct
outcome the capture pipeline can produce… to a response a caller can programmatically tell apart
from every other outcome", explicitly not mandating "the Go type, field names, or HTTP status
codes", and requires a completeness test "failing loudly if a new outcome is ever added with no
mapping" — which is D8's `AllCaptureOutcomes()` total switch, arrived at from the other direction.
Nothing left to reconcile.

### C8 — How `nooma capture` reaches the vault. **RESOLVED — HTTP client. D11 stands.**

**Spec side (overruled).** R3.1: *"opens the vault directly — the same way `status`/`doctor` already
do, with no running `nooma serve` instance required"*, and *"holds the vault's single-writer lock"*.

**Design side (adopted), and the cited precedent does not transfer.** `runStatus` says so in its own
words: *"It is read-only in the strong sense: it never takes the write lock… An implementation that
acquired in order to inspect would make `nooma status` useless against a running instance"*
(`status.go:27-30`). Status and doctor are **readers**; capture is a **writer**. `runServe` holds
the exclusive lock for its whole lifetime, and serving is the normal deployment state — so opening
the vault directly fails in the common case in order to work in the rare one. It would also
duplicate `serve`'s entire wiring in a second place.

**Position as filed** — kept verbatim because `spec.md`'s Conflict 4 quotes it as "design.md's own
words": *"The precedent R3.1 cites does not transfer… A lock-taking `nooma capture` would refuse
every time the product was running normally."* And its own cost, as filed: *"`nooma capture`
requires a running server and fails with a message saying so."*

**Owner ruling, 2026-08-03**: the CLI posts to the running server. D11 specifies what it does when
none is running and how it gets the token off a non-loopback bind. `spec.md` R3.1 is rewritten in
full and now carries all three MUSTs plus the MUST NOT against opening the vault or taking its
lock.

### C9 — "Cloud is the path that must work", and there was no cloud embedder. **RESOLVED — PR 17 builds it.**

**Evidence.** `internal/providers/` holds three chat clients and **exactly one embedder**,
`ollama/embed.go`. `config.DocumentedTaskNames` includes `embedding`; `spec.md` R4.2 requires the
Cloud path to produce a configuration "immediately usable by the capture pipeline"; `proposal.md`
§3.2 states "M1 is judged on the cloud path running end to end".

**Why it survived planning, which the proposal now carries verbatim**: *"a degradation designed for
an outage was absorbing a gap in the build plan, and a mechanism that turns a missing component into
a quieter product is the hardest kind of omission to see."* Nothing was broken. Every capture
succeeded. The pipeline worked — on one leg, silently, in the exact configuration the milestone is
judged on.

**Owner ruling, 2026-08-03**: **PR 17 `feat/openai-embeddings` (~200)**, and the chain now reads
**`6 → 17 → 15`**, `(13,15,16,17) → 14`. Anthropic publishes no embeddings API, so OpenAI is the
provider. D17 designs the client; **D18 designs the mechanism that makes this failure shape visible
if it recurs**, which is the part of the ruling that outlives this instance.

**The `17 → 15` edge, derived twice.** Revision 3 of this design flagged it as an implicit
dependency the proposal's graph did not state: PR 15's wizard would otherwise write an `embedding`
binding that is a lie until 17 exists. It was derived independently and about twenty minutes
earlier by `sdd-spec` from `internal/config/validate.go:177`, and it is now committed to
`proposal.md:450-460` with the sharper statement of the same fact — a wizard shipped before the
cloud embedder has two options, "write an embedding binding that fails validation, or write no
embedding binding at all and hand the user the silently one-legged vault", and both are the bug PR
17 closes. The design cites the proposal rather than restating it, and drops its own risk row: two
independent derivations of one missing edge is as close to confirmation as a planning artifact
gets.

### C10 — `spec.md` R6.3 asked that a permanently-unembedded vault be statable; R7.3/R7.4 forbade the port surface that states half of it. **RESOLVED by `spec.md` revision 4 — the method is sanctioned, and both D18b rows ship.**

**Side A — the outcome is required.** R6.3: *"A Cloud vault whose captures come back unembedded is
something a test can state, not something a user discovers by wondering why search feels thin"*,
with a MUST NOT against satisfying it by "a code comment, a doc note, or a step in a manual
verification checklist alone".

**Side B — the mechanism is forbidden.** D18b's second `doctor` row needs
`ports.EmbeddingRepo.CountLiveWithoutEmbedding` — a method on a Phase B file. R7.3's MUST NOT
permits edits to already-delivered `internal/ports/**` files only for "R1.2's scored fusion, R1.3's
gate, R1.7's two new `UnitRepo` methods, and R6.1's new provider client", and R7.4 enumerates the
`ports` files that stay untouched, sanctioning exactly two edits — `unitrepo.go`'s methods and
`decisionlog.go`'s actions. **`embeddingrepo.go` is in neither list.** Nor is there a way around
it: `doctor` lives in `cmd/nooma`, and `sqlite-containment` confines `database/sql` to
`internal/store`, so the count cannot be read without *some* store surface.

**Why this is a real gap and not pedantry.** R6.3 and R7.3 were written for different purposes and
neither anticipated the other. The spec asks for the outcome and forbids one of the mechanisms that
produces it — and the half it forbids is the *runtime* half, the one that answers "did this vault
actually end up with vectors" rather than "is a provider bound".

**What is genuinely at stake, priced both ways.** R6.3's own two MUSTs are satisfiable **without**
the port method — a conformance test that a Cloud-configured capture comes back embedded, plus a
reading of the wizard-written `tasks:` block. So D18b's second row is this design's *addition*
beyond the spec, not a requirement of it.

| Resolution | Cost |
|---|---|
| **Add one clause to R7.3/R7.4** naming `EmbeddingRepo`'s count method, exactly as R1.7's two `UnitRepo` methods are already named | One sentence in the spec. The design keeps 16b's second row, which discharges `m1b` D8's explicitly deferred obligation ("a port method whose only caller is a test" — `doctor` is now a non-test caller) and delivers half of the units↔embeddings↔fts consistency check `docs/03-data-model.md:306-307` already promises |
| **Drop D18b's second row** | 16b ships row 1 and the shared-list guard only; ~90 lines lighter; the *effective* question is then answered at build time by R6.3's conformance test alone and never at runtime, so a vault that stops embedding after the wizard ran has nothing that states it |

**This design's recommendation was the first**, filed as a recommendation. The disagreement was
between two requirements inside one document, so it was the spec's to settle, not this design's.

**Resolution — `spec.md` revision 4, which took the first branch and went further than it was
asked to.** R6.3 now carries a third MUST that *mandates* the method rather than merely permitting
it: "`ports.EmbeddingRepo` gains a method reporting how many live units hold no embedding — e.g.
`CountLiveWithoutEmbedding` — and `nooma doctor` gains a check that reports the count, so a vault
already in the wild, not only a build-time test, can state whether its live units are embedded."
R7.3 names it among its sanctioned additions, and R7.4 now enumerates **three** sanctioned edits to
existing interfaces — `unitrepo.go`, `decisionlog.go`, `embeddingrepo.go` — "and the only three",
with a MUST NOT against its own list ever again reading as exhaustive while being incomplete.

**The authority is a debt, not a new exception, and the spec says so.** `m1b-pipeline/design.md`
D8 declined this exact method with a named recipient: *"This design ships **no** consistency-query
method in Phase B, deliberately: `UnembeddedLive` or similar would be a port method whose only
caller is a test… **The obligation is recorded for whoever ships `doctor`'s consistency check**"*
(`:790-793`). `docs/03-data-model.md:306-307` had already promised the check as a v1 commitment
predating this change. Phase C ships `doctor`'s consistency check in PR 16, so Phase B's own stated
condition — no caller but a test — discharges itself. R7.3 puts it plainly: naming the exception
"corrects that omission, it does not grant a new one."

**What this changes here**: nothing in the design's shape. Both D18b rows ship in **16b** at the
estimate already priced, and the L2+L3 split of the count's own tests is now R6.3's own "Verified
by" wording, including archived units being excluded from it.

> **This entry was two hours from becoming `m1b-pipeline`'s C2 and C3 for the third time.** Both of
> those are conflict notes that outlived the correction to the document they cited, and both were
> found by the next phase rather than by review. This one was found the same way — `sdd-tasks`
> caught it while cutting the chain and logged it as its own C1. **A stale conflict note is worse
> than no note**: it tells the next reader something is broken when it has been fixed, and the next
> reader has no way to tell which without re-deriving the whole disagreement. The rule this project
> keeps re-learning is that the artifact which files a conflict owns closing it, in the same pass
> that learns of the resolution — which is why the evidence above is kept and the status line
> rewritten, rather than the entry being deleted.

---

## 2.1 Reconciliation notes

Not conflicts: nothing disagrees about what should happen. Three artefacts of `spec.md` revision
3's landing, recorded so the next phase does not re-derive them.

**N1 — R6.2's fallback clause is now unreachable, and `spec.md` §10's open item is decided.** R6.2
says "This spec does not mandate which PR's diff performs the binding — PR 15's wizard may already
be written generically against whichever Cloud-capable embedder exists at build time, or this PR
may extend PR 15's wizard logic directly; either satisfies this requirement", and §10 carries the
same as an open item. **Under `6 → 17 → 15` the second option is impossible** — 17 lands first, so
there is no PR 15 wizard for it to extend — and the first is unnecessary, because the embedder
exists by the time the wizard is written. D15 decides it: **PR 15's diff performs the binding, and
PR 17 touches no wizard code at all.** R6.2's neighbouring sentence — "Before this PR exists, the
Cloud path honestly serves six of the seven" — likewise describes a chain state that no longer
occurs; it is harmless, and the spec may drop or date it.

**N2 — R6.3 and R8.1 assigned the same test two different levels. CLOSED by `spec.md` revision 4,
which adopted this note's own answer.** R6.3's "Verified by" said **L2**; R8.1's level table put
"the Cloud-vault-embeds test (R6.3)" under **L4**. The design satisfied both, because they prove
different things and both are cheap once D17's `endpoint` passthrough exists. R6.3's "Verified by"
now says exactly that: L2 for the observable distinction and the `CountLiveWithoutEmbedding` check
(against a `repocontract` fake and, at L3, a real vault), and "L4 — R8.1 separately requires the
same observable distinction proven once more, end to end… **the two levels are not duplicates of
each other** — L2 proves the distinction exists in the pipeline, L4 proves it survives being wired
together for real." Recorded as closed rather than deleted, for the reason C10's closing note
gives.

**N3 — `spec.md`'s Conflicts 3 and 4 quote design text that revision 3 rewrote.** Conflict 3 quotes
*"This design recommends [the one-field] rule…"* and Conflict 4 quotes *"A lock-taking `nooma
capture` would refuse every time the product was running normally"* and *"requires a running server
and fails with a message saying so"*. All three sentences were revision 2's; revision 3 rewrote C6
and C8 into their resolved form and the quoted wording went with it. **Revision 4 restores all
three verbatim** inside C6's and C8's new "Position as filed" paragraphs, so the spec's citations
resolve again without the spec changing. The positions never differed — only the sentences moved.

---

## 3. Decision record

### D1 — `core/recall` gains `FuseScored`; `Fuse` becomes a projection of it, and ADR-0010's surface grows by exactly one function

Doc 02 §5 step 4 states the requirement verbatim: the gate "needs `internal/core/recall` to expose a
fusion that keeps its scores instead of only its ranked identifiers" (`02:184-186`). `Fuse` computes
those scores today and throws them away.

```go
package recall

// FusedCandidate is one id and the RRF score that ranked it.
type FusedCandidate struct {
    ID    string
    Score float64
}

// FuseScored is the fusion; Fuse is its projection onto ids.
func FuseScored(lists ...[]string) []FusedCandidate
func Fuse(lists ...[]string) []string
```

**`Fuse` is reimplemented in terms of `FuseScored`, not left alone** (C5). Two functions computing
`Σ w_i/(k + rank_i)` independently are two places for ADR-0010's bias to live, and ADR-0010's own
argument is that a bias here propagates into the whole relation graph. `Fuse` keeps its exact
signature, ordering and three-level tie-break.

**What proves the refactor, stated precisely.** ✎

- **Order** is proven by the pre-existing suites, *unedited*: `fuse_test.go`'s
  `TestFuse_BreaksTiesDeterministically` exercises the earliest-list and lexicographic levels
  separately, and `TestFuse_ReproducesADR0010ByHand` pins score-derived ordering.
- **Magnitude is not proven by anything that exists.** `recall_corpus_test.go` asserts id order
  only, and `Fuse` has no scores to expose. So **a `FuseScored` whose magnitudes are wrong while the
  order stays right is invisible to both suites** — exactly the failure D2's gate is exposed to,
  because a ratio is sensitive to magnitude, not rank. The proof of magnitude is the **new
  hand-computed L1 table in 12a**.

This is C11/C12's shape one layer up: a fixture verified only against itself is a restatement, and a
suite that cannot see the quantity it is supposed to protect is not protecting it.

**`Score` is `float64`, and it is not `recall.Scored`.** `Scored` means *a dot product*
(`vector.go:32-36`). Reusing it would give one type two meanings, and the `float32` would compress
exactly where the gate needs resolution: at `k = 60`, first-on-both scores `2/61` and second-on-both
`2/62`, a difference of 5×10⁻⁴ a ratio must see.

| Option | Verdict |
|---|---|
| A variant flag or second parameter on `Fuse` | Rejected — a function whose return type depends on an argument is two functions sharing a name |
| Return `map[string]float64` beside the ranked ids | Rejected — the order is the answer; an unordered map plus an ordered slice gives ties two chances to disagree |
| Reuse `recall.Scored` | Rejected — one type, two meanings, and a `float32` where the consumer compares near-ties |
| **`FusedCandidate` + `FuseScored`, `Fuse` as its projection** (chosen) | One implementation of ADR-0010's formula, one tie-break, an additive surface |

**One property this adds, load-bearing for D2:** every id `FuseScored` returns has a **strictly
positive** score, because an id appears only if present in at least one list and every term
`w_i/(k + rank_i)` is positive for `w_i > 0`, `k = 60`, `rank_i ≥ 1`. So D2's ratio can never divide
by zero. Proven by an L1 property test that names the constants the bound depends on, so a future
negative weight breaks it loudly.

### D2 — The margin gate lives in a new `internal/core/correction`, and "ask" is `(value, bool)`

```go
package correction   // internal/core/correction — NEW

// ReferentMargin is docs/02-cognitive-core.md §13's correction_referent_margin.
const ReferentMargin = 1.5

//   len(cands) == 0                     -> "", false   (nothing to correct)
//   len(cands) == 1                     -> id,  true    (no ambiguity to gate)
//   cands[0].Score/cands[1].Score >= m   -> id,  true
//   otherwise                           -> "", false   (ask)
func Referent(cands []recall.FusedCandidate, margin float64) (string, bool)
```

**Only the top two participate** — `spec.md` R1.3's own MUST, pinned by a table case whose third
candidate would flip the result if it were allowed to.

**Why `(value, bool)` and not an outcome enum.** `Kind.UnitType()` and `relation.ThresholdsFor`'s
`(nil, nil)` answer this twice already. A third vocabulary (`Resolved | Ambiguous | NoCandidates`)
would let the audit row say *why* it asked — but the caller already holds every fact it would carry:
it passed `cands` in, so `len(cands) == 0` distinguishes "nothing found" from "too close", and the
two scores the rationale quotes are `cands[0]` and `cands[1]`. A vocabulary that only restates its
own input is a third source of truth.

**The boundary is inclusive, pinned at exactly 1.5.** Doc 02 says the system asks when the top two
are "closer together than" the margin — a strict inequality, so *at* the margin it acts. `m1b` D7
learned this on `conf == Surface`; the L1 table drives `1.4999`, `1.5` and `1.5001`.

**The margin is a parameter; the constant is the caller's default** — `relation.Decide`'s shape. A
margin ≤ 1 makes the gate vacuous; stated in the doc comment, not guarded in code, because the only
producer is a `const` in the same package and a guard would be a branch no test can reach.

**Why a new package**, at the cost of a line in `docs/06-harness.md` §1's tree (`spec.md` §10 —
renumbered from §9 — leaves the package to design):

| Home | Verdict |
|---|---|
| `core/recall` | Rejected — it would declare a constant named for corrections and a function about referents. Its subject is "given a query and an index, what ranks where" |
| `core/classify` | Rejected — classify owns "what did the model say"; which unit that answer refers to is not in the response |
| `core/unit` | Rejected — it is I01's tree-scan subject, and `m1b` D1 argued for keeping that subject narrow |
| **`internal/core/correction`** (chosen) | It holds two real decisions: the gate (D2) and the edit plan (D3). A package for one function is a smell; a package for the two decisions doc 02 §5 step 4 states is a section of the document made executable |

**The margin is computed over the *live* candidates, after `LiveByIDs`, never before.** If the
top-scoring id is `superseded` it is dropped — and a ratio computed before the drop would gate the
*surviving* top candidate against a score belonging to a unit nobody can correct. D9 names the
method that keeps scores and units paired across the filter.

### D3 — A correction writes exactly one field, and the plan stays a slice on purpose

**C6 is ruled. This is the rule:**

| The classification resolved | The plan holds |
|---|---|
| `event_at`, and not `due_at` | one `event_at` edit |
| `due_at`, and not `event_at` | one `due_at` edit |
| neither date, and `normalized_content` survived | one `content` edit |
| **both** dates | nothing — ask |
| neither date and no content | nothing — ask |

```go
package correction

type Field string
const (
    FieldContent Field = "content"
    FieldEventAt Field = "event_at"
    FieldDueAt   Field = "due_at"
)
func AllFields() []Field

// Edit is one field and its new value. Exactly one accessor reports true.
type Edit struct{ /* unexported */ }

func NewContentEdit(s string) Edit
func NewEventAtEdit(t time.Time) Edit
func NewDueAtEdit(t time.Time) Edit

func (e Edit) Field() Field
func (e Edit) Content() (string, bool)
func (e Edit) EventAt() (time.Time, bool)
func (e Edit) DueAt() (time.Time, bool)

// PlanEdit decides which fields a correction changes. false means there is
// nothing unambiguous to write, and the caller asks instead.
func PlanEdit(c classify.Classification) ([]Edit, bool)
```

Writing `event_at` from `Classification.EventAt` requires no inference: the field means the same
thing on both sides. Writing `content` from `NormalizedContent` requires C2's inference — that the
model's normalization of the correction *utterance* is the referent's new *body* — so it is taken
only when it is the only thing on offer. **The accepted cost, owner-accepted and unmitigated**: a
correction that moves a date leaves the body naming the old one until the user corrects the text.

**`PlanEdit` still returns `[]Edit`, and keeping that shape after the ruling is deliberate.** The
slice was introduced in revision 2 *before* C6 was ruled, precisely so the ruling would cost one
pure function's body and its L1 table — not the port, not the pre-image shape (whose
`previous`/`next` are objects keyed by column name), not `dispatchEdits`, not the audit ordering.
**It bought exactly that, and the bill came in at one function.** Collapsing it to a single `Edit`
now would re-hardcode the answer into four call sites and make the next ruling expensive again —
which is the whole reason a design should absorb an open question in one place rather than spread
it. The invariant "the plan holds exactly one element" lives in `PlanEdit`'s L1 table, where a
ruling can be read and changed, rather than in a type where it would have to be re-derived.

**Why `Edit` is opaque with per-field accessors.** A struct with `Content string` beside `At
time.Time` and a `Field` tag is the shape that writes the wrong column. With unexported state and
three accessors whose names match the three repo methods one-for-one, a crossed wiring is a name
mismatch a reader sees and a `false` return an L1 test catches. The completeness table iterates
`AllFields()` and asserts exactly one accessor reports true per field.

**`PlanEdit` takes the whole `Classification`, not three values.** Passing `(c.NormalizedContent,
c.EventAt, c.DueAt)` positionally puts two `*time.Time` arguments side by side, which is I18's exact
failure mode with nothing guarding it. The cost is that `core/correction` imports `core/classify` —
`m1b` D7's accepted smell, with the same reversal criterion: a producer of corrections that is not a
classification.

### D4 — `ports.UnitRepo` gains two per-field update methods, non-nullable, and the one risk a signature cannot close is pinned by the contract

```go
UpdateEventAt(ctx context.Context, id string, eventAt time.Time, at time.Time) error
UpdateDueAt(ctx context.Context, id string, dueAt time.Time, at time.Time) error
```

Q3c-iii implemented unchanged: not one `UpdateFields(patch)`, so **I18 stays structural**. Each
rewrites exactly the field it names plus `UpdatedAt`, leaves every other column untouched, and
returns `ErrUnitNotFound` when the id is unknown (`spec.md` R1.7).

**The dates are values, not pointers.** A `*time.Time` would let a caller clear a column, and "the
user removed the date" is a correction M1 cannot express: `classify` cannot distinguish an absent
date from a deliberately removed one, so `PlanEdit` can never produce a nil-dated edit. A nilable
parameter would ship a branch with no producer. Reversal criterion: an explicit "clear the date"
instruction in the wire shape, which is M4's UI.

**The residual risk, named because a name cannot close it.** Both methods take two `time.Time`
arguments, so a call site *can* swap the new value with the audit timestamp. It is closed the only
way left: the shared `repocontract` case drives each with **two distinguishable instants** and
asserts both columns independently, answered twice — `memrepo` and `internal/store/sqlite`, in the
same PR. C11's lesson is why "twice" is the operative word.

**Phase A's `unitrepo.go` files are edited, and that is a change of rule, not a violation.** `m1b`'s
R6.3 froze them *for Phase B*; a method added to an existing interface cannot be a new file.
Consequence, budgeted: every implementer breaks until it implements both. `store_api.golden` gains
lines and modifies none. No `Delete`/`Remove`/`Purge`/`Drop`/`Destroy` prefix, so I03's reflection
stays satisfied.

### D5 — ADR-0016's ordering is structural: one function reaches the update methods, downstream of the audit write in its own statement list

No Go signature enforces statement order, so this is three layers, in the shape `m1b` D4 used for
the second clock read.

**Layer 1 — one door.**

```go
// internal/brain/correction.go

// applyWithPreImage is the ONLY path in this package to a ports.UnitRepo
// update method. ADR-0016: the row is written first; if it fails, no edit runs.
func (r correctionRunner) applyWithPreImage(ctx context.Context, target unit.Unit,
    plan []correction.Edit, ref referentSource, now time.Time) error {

    if err := r.recordPreImage(ctx, target, plan, ref, now); err != nil {
        return err                       // ADR-0016: the edit does not happen
    }
    return r.dispatchEdits(ctx, target.ID, plan, now)
}

// dispatchEdits loops the plan; each iteration is a total switch over
// correction.Field whose arm reads the accessor named after the column its
// method writes.
func (r correctionRunner) dispatchEdits(ctx context.Context, id string,
    plan []correction.Edit, now time.Time) error
```

**One pre-image row covers the whole plan** — ADR-0016's "in the same row that records the
correction decision". Under C6's ruling the plan holds one element, so the row names one field; the
shape does not change if a later milestone widens it.

**Layer 2 — an L2 AST guard, mechanizing both halves.** Walk every non-test file under `internal/`
except `internal/store/**` and `internal/ports/**` (which *declare* and *implement* the methods
rather than calling them) and fail on either:

1. a call to `UpdateContent`, `UpdateEventAt` or `UpdateDueAt` outside `dispatchEdits`; or
2. `applyWithPreImage`'s body holding the `dispatchEdits` call at a statement index **lower than**
   the `recordPreImage` call, or holding either zero times.

Both facts are syntactic and checkable with `go/ast`; the file sits beside
`brain_no_direct_clock_read_test.go` and `brain_single_clock_read_test.go`. ~140 lines of L2. The
gate reviewer confirmed the approach is implementable against the current tree.

**Its honest limitation, announced in its own doc comment**, following
`golden_sets_test.go:164-176`: it proves the *call* is ordered, not that the row written *contains*
the pre-image. That is Layer 3.

**Layer 3 — the behavioural test ADR-0016 names** (`0016:84-86`, `spec.md` R1.9): a `DecisionLog`
fake whose `Record` fails, a correction driven against it, and an assertion that `ByID` returns the
unit unchanged field by field. The RED-first test of the slice.

**The direction of failure, chosen deliberately and stated because it inverts D8's.** `m1b` D8 wrote
decision rows *after* the fact, arguing that a crash between the unit commit and the log write
"loses the log entry, not the unit". Here the direction inverts, and it must: the edit is
destructive, so the survivable failure is an audit row describing a correction that did not land.
The trail becomes **over-inclusive, never under-inclusive**, and a retry re-reads current values so
the second pre-image is accurate.

**The pre-image's shape, which ADR-0016 explicitly leaves to this PR** (`0016:68-70`, `spec.md`
§10):

```json
{
  "unit_id": "u-8f1c…",
  "fields": ["event_at"],
  "previous": { "event_at": "2026-08-14T09:00:00-03:00" },
  "next":     { "event_at": "2026-08-15T09:00:00-03:00" },
  "referent": { "source": "recall", "score": 0.0328, "runner_up_score": 0.0164, "margin": 2.0 }
}
```

`previous` and `next` are objects keyed by column name, so a reader — and M4's undo surface — never
consults a tag to know what a value means. `referent.source` is `"recall"` or `"explicit"`; the
three score keys are **omitted** on the explicit path rather than written as zeros, because a zero
score is a claim and an absent key is the truth. `previous.event_at` is `null` when the column was
empty. Doc 02 §5 step 4 gains this shape, which is where ADR-0016 says it belongs.

**Two new `DecisionAction` members**, extending the closed vocabulary its completeness test pins:

```go
ActionCorrectionApplied   DecisionAction = "correction.applied"    // carries the pre-image
ActionCorrectionAmbiguous DecisionAction = "correction.ambiguous"  // the gate asked
```

`correction.ambiguous` records a decision with no vault effect, which `m1b` D7 already settled for
relation discards: *"I changed nothing, and here is what I was choosing between"* is exactly the
question a user asks about a correction that appeared to do nothing.

### D6 — `ports.SignalRepo`, and I13's behavioural half proven by a target that never existed

```go
// internal/ports/signalrepo.go — NEW FILE

type SignalType string   // the eleven members 0002:10 enumerates, beginning with correction
type Valence string      // positive | negative | neutral
type TargetKind string   // unit | trigger | belief | relation
func AllSignalTypes() []SignalType
func AllValences() []Valence

type Signal struct {
    ID             string
    Type           SignalType
    Valence        Valence
    TargetKind     *TargetKind
    TargetID       *string          // NO FK — I13
    DecisionAction *DecisionAction
    RelationType   *string
    Magnitude      *float64
    Context        json.RawMessage
    OccurredAt     time.Time
}

type SignalRepo interface {
    Record(ctx context.Context, s Signal) error
    Since(ctx context.Context, t time.Time, limit int) ([]Signal, error)
}
```

A `ports` DTO defined in `ports`, following C13's resolved precedent (`ports.Embedding`,
`ports.Decision`, `ports.Relation`). No `Delete*`-prefixed method.

**The whole vocabulary is declared though M1 produces one member.** A closed vocabulary is what
makes an out-of-vocabulary value detectable at all — doc 02 §5.1's own argument applied to the write
side. Ten of the eleven have no M1 producer, and that is a *vocabulary*, not ten unbuilt features.

**`Since` is part of the port, not a test affordance**: the invariant is that a signal **outlives**
its target, and "outlives" is only observable by reading signals back.

**I13's behavioural half needs no deletion.** Nothing in M1 deletes anything, and no M1 surface
rejects a relation (C3). The invariant's mechanical content is *no foreign key*, exactly as
`docs/06-harness.md:198` frames it. So the L3 case records a signal whose `TargetID` names a unit id
that **was never created**, against a vault with `foreign_keys=on`, and asserts the row persists and
reads back. C11's lesson: a promise only the store can keep lives at L3.

**The correction signal's fields, decided rather than defaulted** (`spec.md` §10 leaves the valence
to design):

- `Type = correction`, `TargetKind = unit`, `TargetID = <the corrected unit's id>`.
- `Valence = negative`. Doc 02 §9 calls the loop a prediction-error loop and §4 calls explicit user
  corrections "the strongest learning signal". Neutral is the shape of "no information", which is
  the one thing a correction is not.
- `Magnitude = nil`. No document defines a scale, and Q2's lesson is that occupying a column with an
  invented semantics costs more than leaving it null for its real consumer (M5).
- `DecisionAction = nil`. The bucket a correction is evidence *against* is whichever decision
  produced the corrected value, and M1 cannot know which — `decision_log` has no unit index.
  `Context` carries `{unit_id, fields, decision_id}`, where `decision_id` is the
  `correction.applied` row this signal accompanies: the real link M5 can follow.

The signal is written **after** the edits: it records something that happened, and a signal for an
edit that failed would teach M5's loop from an event that did not occur.

### D7 — One correction path, two entrances, and no clock-owning shell that nobody calls

**Both entrances arrive through capture**, and not for convenience: a correction's *what* always
comes from classify. There is no entrance that skips classify, so the only difference between the
API caller and the chat caller is **whether they also know which unit**:

```go
type CaptureInput struct {
    Text       string
    Channel    string
    ReferentID string   // optional; meaningful only when the classification is a correction
}
```

`ReferentID` wins wherever it is non-empty (doc 02 §5 step 4, `spec.md` R1.5), and when it wins
**recall does not run at all** — no embedding call, no fusion, no gate; an instrumented index that
fails the test if queried proves it. The unit is fetched with `UnitRepo.ByID`, whose own doc comment
names corrections as the reason it exists. An unknown explicit id is an **error**, never a silent
fallback to recall — a fallback would defeat the caller's explicit intent.

**The asymmetry this creates, stated rather than discovered.** `ByID` returns a unit at any status;
the recall path returns only `pool` units. So an explicit id can correct an archived or superseded
unit and an inferred one cannot. That is the right asymmetry — a caller naming an id has looked at
that unit; an inference over live candidates should never resurrect a non-live one.

**The correction path is a clockless worker owned by `captureRunner`, with no service shell.**

```go
type correctionRunner struct {
    units   ports.UnitRepo
    log     ports.DecisionLog
    signals ports.SignalRepo
    ids     ports.IDGen
    recall  *RecallService
}

func (r correctionRunner) at(ctx context.Context, in CaptureInput,
    c classify.Classification, now time.Time) (*Correction, error)
```

It receives `now` from `captureRunner.at`, which received it from the single
`CaptureService.Capture` clock read. There is deliberately **no `CorrectionService` with its own
`ports.Clock`**: it would have no caller. Reversal criterion: M4's UI offering "edit this unit" with
a value and no message to classify.

**Why a separate type at all**: `capture.go` is already 763 lines; the correction path needs
`ports.SignalRepo`, which capture does not; and a separate value is drivable at L2 without scripting
a classify call, which is what makes D5's RED-first audit-failure test cheap.

```
correctionRunner.at(in, c, now)
  ├ in.ReferentID != ""  → units.ByID(id)                       explicit; unknown id → error
  └ otherwise            → recall.ScoredFor(ctx, in.Text)       D9
                           → correction.Referent(cands, ReferentMargin)   D2
                           → false → decision_log correction.ambiguous, return Asked
  ├ correction.PlanEdit(c)                                       D3
  │    → false → decision_log correction.ambiguous, return Asked
  ├ applyWithPreImage(target, plan, ref, now)                    D5 — audit, THEN the edit
  └ signals.Record(correction signal)                            D6, I13
```

`Create` is never called on this path, and neither is `SetStatus` — the `correctionRunner` holds no
method that could.

### D8 — `CaptureResult` becomes a tagged union over a closed outcome vocabulary, and three orphan actions get their callers

```go
type CaptureOutcome string
const (
    OutcomeStored    CaptureOutcome = "stored"      // a unit was persisted
    OutcomeDeferred  CaptureOutcome = "deferred"    // timer / recurring_reminder — Q3a
    OutcomeDiscarded CaptureOutcome = "discarded"   // chitchat / out_of_scope
    OutcomeRecalled  CaptureOutcome = "recalled"    // a recall, answered
    OutcomeCorrected CaptureOutcome = "corrected"   // a correction, applied
    OutcomeAsked     CaptureOutcome = "asked"       // a correction whose referent was ambiguous
)
func AllCaptureOutcomes() []CaptureOutcome

type CaptureResult struct {
    Outcome    CaptureOutcome
    UnitID     string          // Stored
    Embedded   bool            // Stored
    Candidates []string        // Stored
    Deferred   *Deferred       // Deferred
    Recalled   []unit.Unit     // Recalled
    Correction *Correction     // Corrected | Asked
}
```

**`Stored bool` is replaced, not joined** (C7). Keeping both gives one fact two sources that can
disagree. The cost is priced: Phase B tests asserting `Stored: true/false` are edited in the PR that
lands `Outcome` — edits to assertions about a renamed field, never a weakened conformance claim.

**What this buys the route.** The handler's status mapping becomes a **total switch** over
`AllCaptureOutcomes()`, with an L2 test that iterates the vocabulary and fails if any member has no
mapping. A new outcome cannot be added later without the route noticing.

**Three orphan actions get callers, and it is this phase's job because it is this phase's problem.**
`capture.discarded`, `capture.classify.unparseable` and `capture.classify.unclassifiable` have been
declared and unwired since PR 10a. Today the paths they name return bare Go errors, which is
invisible; once there is an HTTP route, every outcome must map to a status code and "unknown error"
is not one.

- `chitchat` / `out_of_scope` → `OutcomeDiscarded`, one `capture.discarded` row, HTTP 200. A demo
  where "hello" returns 500 is a bug the demo would find.
- `Decode` returning `ErrNoFieldsSalvaged` → `capture.classify.unparseable`, HTTP 502.
- `c.Kind == nil` → `capture.classify.unclassifiable`, HTTP 502.

`capture.dedup.judged` stays an orphan for C14b's recorded reason.

### D9 — `RecallService.ScoredFor` and `ForText` are the one mechanism both entrances call, and the argument is the **raw** text

```go
type RecallService struct {
    index *Index
    lex   ports.LexicalSearch
    units ports.UnitRepo
    embed ports.EmbeddingProvider   // NEW — /recall must embed its own query
}

// ScoredFor embeds text, runs both legs, fuses, filters to live units, and
// returns the survivors paired with their fused scores, in fused order. The
// bool reports whether the vector leg ran.
func (s *RecallService) ScoredFor(ctx context.Context, text string) ([]ScoredUnit, bool, error)
func (s *RecallService) ForText(ctx context.Context, text string) ([]unit.Unit, bool, error)

type ScoredUnit struct {
    Unit  unit.Unit
    Score float64
}
```

**The argument is `in.Text`, never `c.NormalizedContent`, and this is forced rather than preferred.**
`/recall` is standalone (Q3b): it never calls classify, so it has only the raw query. If capture's
`recall` branch searched the *normalized* text, the two entrances would answer the same user
question differently whenever normalization changed a word — which is its job. The L2 test that pins
I22 drives both entrances with one string and compares the ordered ids.

**Scores survive the live filter by a join, not a second fusion.** `LiveByIDs` returns survivors in
the caller's id order, so `ScoredFor` builds an `id → score` map from `FuseScored` and walks the
returned units — one pass, order and score preserved, non-live ids dropped with their scores.

**An embedding failure degrades to the lexical leg alone; it does not refuse the read.** `m1b` D8's
product rule applied to the read path. The `bool` is the degradation flag (rendered as
`semantic_leg_available`), and **both entrances degrade identically because both call the same
method**, so I22 holds under degradation too. **This is also the state C9 would have put every Cloud
vault in permanently, which is why D18 exists**: a degradation flag that is always false is
indistinguishable from a product that simply searches badly.

**`captureRunner` holds one `RecallService`** instead of constructing one per capture
(`capture.go:305`): `correctionRunner` needs the same instance, and two services over one `Index`
would be two places to get eviction wrong when M2 adds it.

### D10 — The HTTP surface: three route families, a total status mapping, and ADR-0017's middleware inside `Handler`

```
POST /capture        {"text": "...", "source": "api", "unit_id": "..."}   → 201 | 200 | 4xx | 5xx
POST /recall         {"query": "..."}                                     → 200
GET  /units/{id}                                                          → 200 | 404
GET  /units?ids=a,b,c                                                     → 200
```

**`POST /capture` is the only write route, and the correction override rides on it.** A separate
`POST /units/{id}/correct` was rejected: it still needs a `text` to classify, which makes it
`/capture` with the id moved into the path — a second route for one operation.

**The status mapping is a total switch over `AllCaptureOutcomes()`**: `stored` → 201, every other
outcome → 200 with a body naming what happened; provider failures → 502, store failures → 500. A
`deferred` timer is **not** an error status: Q3a's argument is that the refusal is an honest answer.

**`POST /recall`, not `GET /recall?q=`** — a divergence from `spec.md` R2.4's illustrative "e.g.
`GET /recall`", which §10 leaves to design. A query is user memory content: a `GET` puts it in
access logs, shell history and the browser address bar. R2.7's property — no route writes
`decision_log` — holds regardless of method and is tested over all four routes.

**The unit routes read through `LiveByIDs`, never `ByID`.** A non-live id and an unknown id return
the **same** 404 body, so the surface does not leak the existence of a non-live unit through its
error shape. **The collection route is `GET /units?ids=…`, and that is the whole of it**: it maps
1:1 onto the single live read method, needs no pagination or ordering rule, and has an immediate
consumer — rendering the `candidates` a capture result returns.

**ADR-0017 and the middleware, and why "same PR" is not the mechanism.** C4's resolution requires
that no commit exist where `POST /capture` is mounted and unprotected. A commit-ordering promise is
not enforceable, so the guard is structural:

```go
type Deps struct {
    Version string
    Capture *brain.CaptureService
    Recall  *brain.RecallService
    Token   string   // "" means no token is configured — reachable only on loopback
}

func Handler(d Deps) http.Handler   // builds BOTH muxes; callers cannot reach the inner one
```

`Handler` builds an open mux for `GET /{$}` and `GET /ui`, and a guarded mux for the API. Every API
route is registered through one helper that applies `requireToken(d.Token)`, and the route table is
**one slice used both to register and to test** — so a new API route is guarded by construction, and
the L2 completeness test iterates the same slice.

**What the middleware does when no token is configured — the ordinary development case.** It is a
**no-op**: an empty `Token` means there is no secret to present, and that state is reachable only on
a loopback bind, because `DecideBinding` refuses to return an address for a non-loopback bind with
no `auth_token_env` or an unset variable (`binding.go:41-52`). That implication is not left as
prose: `httpapi.ResolveToken(cfg, lookup) (string, bool)` reads the same variable `DecideBinding`
checked, and an L2 test sweeps the same truth table `binding_test.go` already uses, asserting that
**for every combination where `DecideBinding` succeeds and `ResolveToken` returns `""`, the bind is
loopback**. One read, one source.

When a token *is* configured, every API request must carry `Authorization: Bearer <token>`, compared
with `crypto/subtle.ConstantTimeCompare`. **A missing token and a wrong token produce byte-identical
responses** — same status, same body, same headers — because a response that tells the two apart is
an oracle for the token's *existence* independently of its value. `spec.md` R2.11 states this as a
MUST NOT and requires the completeness test to assert the two responses are identical, not merely
that both are unauthorized; the test compares them to each other rather than each to a literal.

**ADR-0017's own scope.** It covers the **API header** only. The UI's authentication is ADR-0007's
cookie handshake, tied to the server-rendered UI, and the UI is M4 — so `/ui` and `/` stay open in
M1 and ADR-0017 says so in its Consequences, naming M4 as the owner. ADR-0007 is neither edited nor
superseded; ADR-0017 records that it discharges the request-time half ADR-0007 deferred.

### D11 — `nooma capture` is an HTTP client of the running server, and what it does when there is none

C8 is ruled: the CLI posts to the running server.

| Option | Verdict |
|---|---|
| The CLI opens the vault and runs `brain.CaptureService` itself (`spec.md` R3.1) | Rejected — `runServe` holds the exclusive lock for its lifetime and serving is the normal deployment state, so this fails in the common case to work in the rare one. It also duplicates `serve`'s whole wiring |
| The CLI opens the vault **without** the write lock | Rejected outright — two writers to one SQLite vault is what the lock exists to prevent |
| Try the lock, fall back to posting | Rejected — one command, two code paths, two failure modes, and two different auth semantics selected by invisible state |
| **The CLI posts to the running server** (chosen) | One writer, one wiring site, one server-side path for both entrances. Reading `nooma.yml` for the address takes no lock, exactly as `status` reads the vault without one |

**Where it posts.** The CLI resolves the vault (`config.ResolveVault`), loads `nooma.yml`, and
builds the URL from `server.bind` and `server.http_port`. **A wildcard bind is not a dial address**:
`0.0.0.0` and `::` are what a server listens on, not what a client connects to, so the CLI dials
`127.0.0.1` when the configured bind is a wildcard and the configured host otherwise. Stated because
a literal `http://0.0.0.0:7777` works on some stacks and not others, which is the worst kind of bug.

**What it does when no server is running.** It does **not** fall back to opening the vault — that is
C8's rejected option, and stating the refusal here is what keeps it rejected. It distinguishes three
cases, and the diagnosis costs nothing because `vaultlock.ReadHolder` is the same free read `status`
already performs:

| Lock holder | Dial | Message, and exit status |
|---|---|---|
| not held | fails | `no nooma server is running for vault <path> (expected at http://127.0.0.1:7777) — start one with 'nooma serve'`, exit 1 |
| held by pid N | fails | `a process (pid N) holds vault <path> but nothing answered at <addr> — check server.bind and server.http_port in nooma.yml`, exit 1 |
| either | succeeds | the ordinary path |

The second message is the one that earns the lock read: without it, a user whose `bind` moved gets
told "no server is running" while one is, and looks in the wrong place.

**How it gets the token.** From the same place the server does: `httpapi.ResolveToken(cfg,
os.LookupEnv)` (D10), reading the `server.auth_token_env` variable *name* out of `nooma.yml` and its
value out of the environment. One function, three readers — `serve`'s `Deps`, the middleware, and
the CLI — so the client and the server cannot disagree about whether a token exists or which
variable holds it.

- Loopback bind, no `auth_token_env`: no header, no friction. This is the development case.
- `auth_token_env` set and the variable set: `Authorization: Bearer <value>` on the request.
- `auth_token_env` set and the variable **unset**: the CLI **refuses before sending**, naming the
  variable — `DecideBinding`'s own decide-first-act-second shape (`binding.go:24-27`: "a server that
  binds and then complains has already exposed the port"). A client that sends first and discovers
  the 401 afterwards has already put the user's memory on the wire unauthenticated.

**No `nooma recall` subcommand** (`spec.md` R3.2 does not require one). The demo's "what do you know
about X?" is a capture whose classification is `recall`, answered by the same service `/recall`
calls — the sharpest demonstration of "one mechanism, two entrances". Accepted cost: asking spends
one classify call. Reversal criterion: an offline ask path, which is M4's.

### D12 — `internal/ports` gets no exported-surface golden, and the deferral stops here

`m1b` §8 handed this decision to Phase C, having deferred it once with its own trigger already met.
**Decided: no golden.** The one thing a golden catches that a contract suite does not is an
accidental *removal* — and every port here has at least two implementations and a shared contract
suite, so a removed method is a compile error before it is a diff. What a golden adds is a file to
regenerate in every port PR and a second golden format beside `store_api.golden`. The trigger is
retired rather than moved: **a port with only one implementation** is what would make a golden earn
its keep, and this project's contract rule says such a port should not exist.

### D13 — Each PR's documentation delta is assigned up front, because `docs-sync` fires per pull request

C9 of `m1b-pipeline/tasks.md` is the record of this gate failing three PRs in one afternoon. Phase C
has a sharper version: doc 02 §5 step 4 and §13 **already carry** Q3c's decision, so the obvious
deltas are written and a PR with nothing left to say reaches for `no-spec-change`. `spec.md` R1.13
licenses exactly that for PR 12; this design **agrees with the mechanism and finds it unnecessary**,
because each core-touching slice has a genuine delta.

| PR | Touches `internal/core/**` | Documentation gains |
|---|---|---|
| 12a | `core/recall` | doc 02 §5.2 — the scored fusion as a named output of the same mechanism, and why every fused score is strictly positive |
| 12b | `core/correction` (new) | doc 02 §5 step 4 — the gate's boundary is inclusive, applied to the **live** candidates; `docs/06-harness.md` §1's tree gains `correction/` |
| 12c | `core/correction` | doc 02 §5 step 4 — **which column a correction writes** (C2/C6's closed gap), and that two dated fields ask |
| 12f | no core file | not forced; carries ADR-0016's settled `context` shape into §5 step 4 anyway |
| 13b | no core file | **`docs/adr/0017-http-request-auth.md`** is new, and `docs/adr/README.md`'s index gains its row |
| 17 | no core file | doc 01's provider list already carries `openai` (Phase A PR 1); no delta |
| 15 | no core file | doc 01's `nooma init` row is already accurate; no delta |
| 16a | no core file | doc 01's `nooma doctor` row gains the quality gate as a named check |
| 16b | no core file | doc 03:306-307 already promises the units↔embeddings consistency check; 16b delivers half of it, and the doc line is corrected to say which half exists |
| 14a | no core file | **`docs/01-architecture.md`'s CLI table gains `nooma capture`** ✎ — the table already lists four unbuilt commands, so it is a promise table, and `capture`'s absence means doc 01 never promised the command. `01:101` makes no route-level promise, so §5's routes need no doc 01 edit |

`docs/06-harness.md` §4's invariant table also moves twice — see D14.

### D14 — Two new invariants are registered before their tests are written

| # | Invariant | Doc 02 | Lands in |
|---|---|---|---|
| I22 | One recall mechanism: the same text answered through capture and through `/recall` returns the same ordered result | §5 step 2 | 13a |
| I23 | A correction's pre-image is recorded before its edit is applied; a failed audit write leaves the unit untouched | §5 step 4 | 12f |

`nooma-testing`'s execution step 2 requires registering an invariant in `docs/06-harness.md` §4
before writing its test. Adding rows to a table headed "Initial extraction" is what that heading
invites; the alternative — conformance tests naming no registered invariant — makes the registry
incomplete on the first change that needed it.

### D15 — `nooma init`'s two provider paths, and a type that cannot hold a secret

PR 15. `cmd/nooma/init.go`'s `defaultConfig()` ships `providers:`/`tasks:` fully commented today —
M0 built the placeholder, not the wizard.

**Two paths, exactly** (`spec.md` R4.1): **Cloud (recommended)** and **Ollama**. The embedded
llama.cpp option is not offered — ADR-0002: "The embedded option is discarded."

**The no-secret guarantee is a type, not a rule** (`spec.md` R4.3 asks for a structural guarantee):

```go
// EnvVarName is the NAME of an environment variable — never its value.
type EnvVarName string

// NewEnvVarName rejects anything that is not a POSIX-shaped variable name:
// ^[A-Z_][A-Z0-9_]*$.
func NewEnvVarName(s string) (EnvVarName, error)

type providerChoice struct {
    Type      string       // anthropic | openai | ollama
    Model     string
    APIKeyEnv EnvVarName   // the only credential-adjacent field, and it cannot hold a credential
    BaseURL   string       // ollama only
}

func renderProviders(choices []providerChoice) string   // the yml renderer, and its whole input
```

A real API key cannot be spelled as an environment-variable name — `sk-ant-api03-…` and `sk-proj-…`
both carry lowercase letters and hyphens, which the constructor rejects. So the renderer's input
type is *incapable* of carrying a secret rather than trusted not to. That is non-negotiable #7's
"structural, not warnings" in the same shape Q3c-iii used for I18, and it is L1-testable with a
table of real-shaped keys. The key value the Cloud path collects interactively goes to `.env` at
`0o600` through a function the yml renderer cannot reach.

**What the Cloud path binds, now that PR 17 exists.** Two `providers:` entries of type `openai` —
one carrying the chat model, one carrying the embedding model — because a provider entry holds one
`model` and a chat model is not an embedding model. That is legal: the `providers:` map is keyed by
entry name and a type may appear more than once (`validate.go:185`). `tasks:` then binds
`capture_processing` and `relation_evaluation` to the first and `embedding` to the second.

**Which PR's diff writes the `embedding` binding — `spec.md` §10's open item, decided: PR 15's.**
R6.2 left two options open; **`6 → 17 → 15` closes one of them outright** (17 lands first, so there
is no PR 15 wizard for 17 to extend) and makes the other unnecessary (the embedder exists by the
time the wizard is written, so nothing has to be written "generically against whichever
Cloud-capable embedder exists at build time"). So the wizard names the binding directly and **PR 17
touches no `cmd/nooma` file at all** — which is also what keeps PR 17 at ~200 lines and reviewable
as one thing.

**The wizard writes `endpoint` for every provider entry it creates, including the cloud ones.**
`config.Provider` already carries the field (`config.go:56`) and its doc comment says the struct is
a union "no single entry populates all of them", so this widens no schema. It is written empty by
default — `openai.NewClient` falls back to `defaultBaseURL` when `baseURL` is `""`
(`client.go:33-36`) — and it exists so a **test** can point a wizard-written vault at a loopback
`httptest` server. That is what makes `spec.md` R6.3's L4 form reachable at all: an L4 test drives
the compiled binary, so it cannot inject `fakeprovider`, and R6.4 forbids calling the real endpoint.
Without an overridable endpoint in the config, R6.3-at-L4 and R6.4 cannot both hold. See N2.

**The Ollama path** binds the same three tasks to one `ollama` entry, or to two if the user names a
distinct embedding model — `ollama/embed.go` and `ollama/client.go` are methods on one `Client`, so
the shape is identical.

**What neither path binds**: `chat`, `belief_derivation`, `audio_transcription` and
`image_description` have no M1 consumer. A binding with no reader is the same defect as a port
method with no caller — and D18's coverage check reads the same list of consumed tasks, so an
unbound task the binary *does* consume becomes a failure rather than a silence.

**Nothing here calls a provider.** `nooma init` writes configuration; the first live call happens
when the vault is used. Every wizard test drives scripted input and none opens a connection.

### D16 — `nooma doctor`'s quality gate: zero degradations, unreachable ≠ unsuitable, and a no-op by construction

PR 16a. One new row in `doctorChecks`'s existing `{name, run}` table — not a new command, not a
rewrite of `runDoctor`'s accumulate-every-failure loop (`spec.md` R5.1).

**Ruling on `spec.md` R5.4 — validity means zero degradations. Upheld, with two refinements.**
(Accepted by the owner as written.)

I14's decoder is deliberately tolerant, so "did not error" is satisfied by a response that degraded
every field but one. A gate built on the tolerant decoder's bare non-error return would pass
precisely the providers it exists to reject, and ADR-0002's "verifies the returned JSON validates
against the expected schema" demands the stricter reading. So: a live response is valid only when
`classify.Decode` (or `relation.DecodeJudgment`) reports **zero** `Degradation` entries.

*Refinement 1 — the bar is tight but not absurd, and doc 02 says why.* §5.1 already states that
"only the required fields' absence is reported; an optional field's absence is the ordinary case,
not a loss" (`02:274-278`).

*Refinement 2 — the report names which kind of failure.* §5.1: a wrong-shaped value and an
out-of-vocabulary value "are recorded as different events… one is a formatting failure, the other a
vocabulary failure, and §9's learning loop should not confuse them" (`02:241-245`). They call for
different user advice — a JSON-mode setting versus a different model — so the failure line names the
field, the reason, and the task.

*And the count is honest*: `k of n prompts produced clean JSON`, failing when `k < n`. Each corpus
prompt is sent **once** — a retry that turns a flaky provider green is worse information than a
single honest sample.

**Ruling on `spec.md` R5.6 — a no-op when no provider is configured. Upheld, and made structural.**

A freshly `init`ed vault configures no provider, and `test/e2e/doctor_test.go`'s
`TestDoctorOnAHealthyVault` already asserts a fresh vault reports zero failures. **The gate iterates
the vault's configured `tasks:` bindings, so "no bindings" means "zero iterations" — a no-op by
construction, not an `if len(tasks) == 0 { return ok }` early return.** An explicit branch is both
an arm no test can make a decision about and an invitation to a future "warn if empty". The report
line states the count — `llm quality: ok (0 tasks configured)` — so a user can tell "passed" from
"did not run", the distinction `core-coverage.sh`'s "armed but vacuous" wording already models.

**The gate covers only tasks whose answer is JSON**: `capture_processing` and `relation_evaluation`.
`embedding` is bound to a provider whose response is a vector, not text, and there is no production
decoder to judge it with — sending a classify prompt to an embeddings endpoint would test nothing.
Embedding fitness is a different question, and D18 is where it is asked.

**Unreachable is not unsuitable, and doc 01 already says so.** A transport error is reported as
*unreachable*, in doc 01's own existing category ("provider unreachable → how to install it",
`01:155-157`), never as a JSON-fitness verdict. A model cannot be judged bad at JSON on the strength
of a network that never delivered the question. The live call carries a bounded timeout.

**Attribution is per task** (`spec.md` R5.2), matching `runDoctor`'s existing per-check
independence. **The corpus is the prompts, never the recorded answers** (R5.3). **No test calls a
real provider** (R5.5): the decision logic is proven at L1/L2 against `fakeprovider` with scripted
responses covering a clean pass and each I14 degradation shape.

### D17 — The OpenAI embeddings client (PR 17): one method on the existing `Client`, no new corpus

C9 is ruled. The shape is not open — PR 6 built three HTTP clients and `ollama/embed.go` is the
embedder among them, so this is the same shape one more time.

```go
// internal/providers/openai/embed.go
type embedRequest struct {
    Model string `json:"model"`
    Input string `json:"input"`
}

// embedResponse mirrors OpenAI's POST /v1/embeddings shape: data is an array
// of objects, one per input, each carrying the vector under "embedding".
type embedResponse struct {
    Model string `json:"model"`
    Data  []struct {
        Embedding []float32 `json:"embedding"`
    } `json:"data"`
}

func (c *Client) Embed(ctx context.Context, req ports.EmbedRequest) (ports.EmbedResponse, error)

var _ ports.EmbeddingProvider = (*Client)(nil)
```

**A method on the existing `Client`, not a second type.** `ollama.Client` implements both `Complete`
and `Embed` (`ollama/embed.go:33`), and `openai.Client` already carries `baseURL`, `apiKey`, `model`
and `httpClient` with an overridable base URL for `httptest` (`client.go:16-41`). One `Client` per
provider package, one method per port. The chat-vs-embedding *model* difference is handled where it
belongs — in configuration: two `providers:` entries of type `openai`, each with its own `model`,
bound to different tasks (D15). A second Go type would put a configuration fact into the type
system.

**Four behaviours copied deliberately from `ollama/embed.go`, because they are decisions and not
boilerplate:**

1. **The returned `Model` is the response's echoed model, not the request's.** `unit_embeddings.model`
   is what I21 filters on, so it must name the model that actually produced the vector. If OpenAI
   echoes a more specific id than was requested, that specific id is what gets stored, and I21 keeps
   working because both the write and the query read the same field.
2. **An empty `data` array is a Go error, never a zero-value `EmbedResponse`.** A caller cannot be
   handed a valid-looking response carrying no vector — `ollama/embed.go:66-68`'s own reasoning.
3. **A non-200 status is an error carrying the body.** OpenAI's quota and model-not-found messages
   are the useful part, and they arrive in the body.
4. **`Authorization: Bearer <key>`**, the same header the chat client sets (`client.go:78`), from the
   same `apiKey` field.

**What it deliberately does not do.** It does not send OpenAI's optional `dimensions` parameter:
`ports.EmbedResponse` has no `Dim` field ("the dimension is `len(Vector)`"), `dim` is written as
`len(vector)` at the storage boundary, and a truncation knob with no §13 row and no consumer is
scope. Reversal criterion: ADR-0012's memory table becoming a real constraint for a large vault,
which is `reindex` territory (M6). It also does not normalize — `internal/store/sqlite` calls
`recall.Normalize` at the storage boundary for every embedding (`m1b` D6), and normalizing an
already-normalized vector is a no-op within tolerance. Stated so nobody "optimizes" the store's call
away on the grounds that OpenAI already returns unit vectors.

**The test path, and why it adds nothing to `testdata/llm/`.** The corpus's cases carry a `prompt`
and a recorded text `response` decoded by `classify.Decode`; a vector is not text, and D16's quality
gate iterates that corpus expecting JSON. **Putting an embedding case in it would give the gate
something it cannot judge.** So:

- **The client** is proven at **L2 against `httptest`** — request path `/v1/embeddings`, the
  `Authorization` header, the request body's `model`/`input`, the response decode, the empty-`data`
  error, the non-200 error, and the echoed-model rule. That is `openai/client_test.go`'s existing
  shape, and `m1a`'s design already established that an in-process loopback listener is not "the
  network" in `docs/06-harness.md` §3's sense.
- **Everything downstream** keeps using `fakeprovider.NewEmbeddingFake`, which stays exactly what
  `m1b` risk 3 says it is: a determinism device, never a ranking device. PR 17 adds no fixture and
  changes no corpus.

**The base URL is `Client.baseURL`, and that is what makes R6.3's L4 form possible.** `Embed` posts
to `c.baseURL + "/v1/embeddings"`, and `NewClient` already defaults an empty `baseURL` to
`https://api.openai.com` (`client.go:33-36`). `cmd/nooma`'s wiring passes `config.Provider.Endpoint`
straight through for every HTTP provider type, so a vault whose `openai` entry carries an
`endpoint` is served by that address — which is how an L4 test points the compiled binary at a
loopback `httptest` server without touching the network (D15, N2). **PR 17 adds no config field and
no wizard code**; it consumes a field that already exists.

**The honest limit, named rather than discovered.** The response shape above is authored from
OpenAI's published API and **confirmed by nothing that runs in CI** — no test may reach the real
endpoint, so an `httptest` fixture written by the same author who wrote the decoder is C12's shape
exactly: a fixture verified only against itself. The first real confirmation is a human running
`nooma doctor` against a live key. Two things make that acceptable rather than reckless: the failure
is loud (a shape mismatch yields a decode error or the empty-`data` error, not a plausible wrong
vector), and **D18 is what makes a silently missing vector impossible to ship**. The test file's doc
comment states this, and PR 17's description repeats it.

### D18 — Making the gap visible if it recurs: one list, three readers, and two `doctor` rows

C9's ruling asks for more than the client. The failure was not "OpenAI has no client" — it was that
**a degradation built for a provider outage silently absorbed a hole in the build plan**, and
nothing anywhere could state it. `CaptureResult.Embedded` was already false on every capture, and no
surface added those falses up. A user would have inferred it from search feeling thin.

**Three questions, and no mechanism answers more than one.** Stating this first, because conflating
them is how C9 survived planning — `EmbeddingProvider` was a real port with a real implementation
(*fit*, for Ollama) and `tasks:` was a real routing mechanism (*configured*, in principle), and
nobody asked the third question at all.

| Question | Asks | Answered by | What it cannot see |
|---|---|---|---|
| **Configured** | is a provider bound to every task this binary asks for? | D18a's shared list + its L2 guard; D18b row 1 (a pure config read) | whether the bound provider can do the job, or whether it did |
| **Fit** | does the bound provider produce usable answers? | D16's quality gate, for the JSON tasks; R6.1's `httptest` client test, for the embedder's wire shape | whether it is bound at all, or what the vault actually contains |
| **Effective** | did this vault actually end up with vectors? | D18b row 2 (`CountLiveWithoutEmbedding`, at runtime) and `spec.md` R6.3's test (at build time) | why not — it reports a number, not a cause |

**D18b row 1 reads bindings, not implementations, and its doc comment says so.** It would pass on a
`tasks.embedding` entry naming a provider type that has no embedder — it checks that a task has *a*
provider, never that the provider can embed. Under `6 → 17 → 15` that state is unreachable in this
chain (`validate.go:177` rejects a binding to an absent provider, and the embedder exists before
the wizard runs), but the check does not know that, and a later milestone adding a fourth provider
type could reach it. Announcing the limit in the check's own comment follows
`golden_sets_test.go:164-176`'s proxy-announcement precedent; the *fit* question is D16's and R6.1's,
and the *effective* question is row 2's.

Three mechanisms, cheapest and most structural first.

**18a — one list, three readers, and a test that they are the same list.** The fact that went
missing is *"which tasks does this binary actually ask a provider for"*. Today it is implicit in
three places that never meet: `serve`'s wiring resolves `tasks.*` into ports, `init`'s wizard writes
`tasks:` bindings, and nothing checks the two agree. So:

```go
// cmd/nooma/tasks.go
// tasksM1Consumes names every task this binary resolves to a provider at
// startup. It is the single source for three readers: serve's wiring, init's
// wizard, and doctor's coverage check.
var tasksM1Consumes = []string{"capture_processing", "relation_evaluation", "embedding"}
```

- `serve` resolves exactly these into `CaptureService`/`RecallService`'s ports.
- `init`'s Cloud and Ollama paths bind exactly these (D15).
- `doctor`'s coverage check reports exactly these (18b).
- **An L2 test asserts all three read the list rather than restating it**, and that every member is
  in `config.DocumentedTaskNames`.

This is D10's guarded-route pattern applied to configuration: one slice, registered from and tested
against. A future milestone that starts consuming `belief_derivation` adds one string, and the
wizard that must bind it and the check that must report it both move with it. **The gap becomes
hard to represent rather than merely detectable**, which is the strongest form available.

Its honest limit: it proves the three readers agree, not that a bound provider works. That is 18b
and D16.

**18b — two `doctor` rows, because they answer two different questions.** PR 16b adds them, and they
are separate rows rather than clauses of D16's gate for three reasons: neither makes a provider
call, so both run offline and instantly while the quality gate goes to the network; D16's gate asks
"is this configured provider good at JSON" while these ask "is there a provider at all" and "did the
vault actually get indexed"; and `spec.md` R5.1 scopes the quality gate to *one* new row, so folding
three behaviours into it would give one report line three readings.

*Row 1 — task coverage (a pure configuration read).*

| Vault state | Report |
|---|---|
| no providers configured at all | `ok (no providers configured)` — a fresh vault has nothing to say, and `TestDoctorOnAHealthyVault` stays green unchanged |
| providers configured, every member of `tasksM1Consumes` bound | `ok` |
| providers configured, a member unbound | **FAIL**, naming the task and what degrades — for `embedding`: *"capture will store units with no vector and recall will run on its lexical leg alone"* — **superseded during `16b`, see the note below; this row is kept as written because it is what was prescribed** |

The distinction between "nothing is configured" and "something is configured and a leg is missing"
is the whole check. A fresh vault is not broken; a Cloud vault with no embedder is, and it is broken
in a way that never raises an error. **This row is what would have caught C9 before a single capture
ran.**

> **Superseded during `16b` — the row above prescribes a FAIL string the implementation did not
> ship, and this note is the correction. The row is left as written, per `openspec/README.md`'s
> lifecycle rule: a change directory is a historical record and is not edited to match what
> shipped.** This follows C10's own precedent above — the evidence is kept and the status
> rewritten, rather than the entry rewritten to look right in hindsight.
>
> What it prescribed for `embedding` — *"capture will store units with no vector and recall will
> run on its lexical leg alone"* — describes `m1b` D8's degradation for a provider **outage** after
> wiring already succeeded, which is D18's *fit* question. This row answers D18's *configured*
> question. Under `13d`'s fail-closed `wireBrain`, `resolveTaskProviders` is all-or-nothing, so an
> unbound task never reaches D8's soft degradation: nothing is captured at all, and both routes
> answer `503`. A reader of the prescribed text would have concluded their captures were landing
> without semantic search — a deferrable recall-quality problem — and deferred a total outage.
> **This check exists because a silent degradation hid a build-plan gap for two milestones;
> shipping that wording would have repeated the failure inside the check's own diagnostic.**
>
> The shipped consequence is also one shared sentence for every member of `tasksM1Consumes` rather
> than one per task, because `wireBrain`'s all-or-nothing resolution makes the outcome identical
> whichever member is missing. The authority is `cmd/nooma/doctor.go`'s `taskCoverageConsequence`;
> `tasks.md`'s Conflicts §C21.1 filed the correction and recorded it for whoever next revised this
> document.

*Row 2 — vault coverage (one SQL count).* `ports.EmbeddingRepo` gains
`CountLiveWithoutEmbedding(ctx) (int, error)`, a `LEFT JOIN` from `units` where `status = 'pool'`
and `unit_embeddings.unit_id IS NULL`. Zero → `ok`; above zero → **FAIL**, naming the count:
*"N live units have no embedding; semantic recall cannot reach them"*.

This is the first half of the units↔embeddings↔fts consistency check `docs/03-data-model.md:306-307`
**already promises** and `m1b` D8 deferred with an explicit condition: *"this design ships no
consistency-query method in Phase B, deliberately: `UnembeddedLive` or similar would be a port
method whose only caller is a test."* That condition is now discharged — `doctor` is a real,
non-test caller. The fts half stays M6's, and 16b's doc 03 delta says which half exists rather than
letting the promise read as fully kept.

> **This row was blocked by C10 and is not any more.** The method is an addition to a Phase B
> `internal/ports` file, which `spec.md` R7.3/R7.4 did not sanction while R6.3 asked for the
> outcome it produces. `spec.md` revision 4 closed that: R6.3 now **mandates** the method and the
> `doctor` check that reports it, R7.3 names it among its sanctioned additions, and R7.4 lists it
> as the third and last sanctioned edit to an existing interface. Both rows ship in 16b at the
> estimate §6 already carries. R6.3's own "Verified by" fixes the levels: the count is proven at L2
> against a `repocontract`-shared fake and at L3 against a real vault holding embedded and
> unembedded live units, with archived units excluded from it.

**Why not a `decision_log` sample instead.** Counting recent `capture.embedding.failed` rows was
considered: it needs no new port method (`DecisionLog.Since` exists), and it would give `doctor` its
first real reader for the glass box. It loses on precision — it samples a window rather than
answering "is my vault indexed", and it cannot see units that were never embedded because no
provider was ever bound, which is C9's actual shape. Recorded because it is the obvious alternative
and because it becomes the right mechanism the day `doctor` wants a *trend* rather than a *state*.

---

## 4. Package layout and dependency map

```
internal/core/recall/
  fuse.go            + FusedCandidate, FuseScored; Fuse becomes its projection    D1   12a

internal/core/correction/            NEW package — pure, stdlib + core only
  doc.go             (docs/06-harness.md §1's tree gains the line — 12b)
  referent.go        ReferentMargin, Referent(cands, margin) (string, bool)       D2   12b
  edit.go            Field, AllFields, Edit + 3 constructors + 3 accessors        D3   12c
  plan.go            PlanEdit(classify.Classification) ([]Edit, bool)             D3   12c
      imports: time, internal/core/recall, internal/core/classify

internal/ports/
  unitrepo.go        + UpdateEventAt, UpdateDueAt (an EDIT to a Phase A file)     D4   12d
  signalrepo.go      NEW — SignalType, Valence, TargetKind, Signal, SignalRepo    D6   12e
  decisionlog.go     + ActionCorrectionApplied, ActionCorrectionAmbiguous         D5   12f
  embeddingrepo.go   + CountLiveWithoutEmbedding                                  D18  16b

internal/store/sqlite/
  unitrepo.go        + the two UPDATEs (an EDIT to a Phase A file)                D4   12d
  signalrepo.go      NEW — Record, Since                                          D6   12e
  embeddingrepo.go   + the LEFT JOIN count                                        D18  16b

internal/brain/
  recall.go          + embed field, ScoredUnit, ScoredFor, ForText                D9   13a
  correction.go      NEW — correctionRunner, applyWithPreImage, dispatchEdits     D5,D7 12f/12g
  capture.go         + the correction / recall / discarded routing                D8   12g, 13a
  result.go          CaptureOutcome, CaptureResult reshaped, Correction           D8   12g

internal/httpapi/
  server.go          Handler(Deps); the two muxes; the guarded route slice        D10  13b
  auth.go            requireToken, ResolveToken                                   D10  13b
  capture.go         POST /capture; the total status switch                       D10  13b
  recall.go          POST /recall; GET /units/{id}; GET /units?ids=               D10  13c

internal/providers/openai/
  embed.go           NEW — Embed on the existing Client; ports.EmbeddingProvider  D17  17

cmd/nooma/
  serve.go           wires providers, repos, Index, services, token into Handler  D10  13d
  tasks.go           NEW — tasksM1Consumes, the one list three readers share      D18  13d
  capture.go         NEW subcommand — an HTTP client of the running server        D11  14a
  init.go            EnvVarName, providerChoice, renderProviders, the two paths    D15  15
  doctor.go          + the quality-gate row (16a) and the two coverage rows (16b)  D16,D18

docs/adr/0017-http-request-auth.md   NEW (C4's resolution)                        D10  13b

test/support/memrepo/       + Signals fake; + the embedding count                       12e, 16b
test/support/repocontract/  + signalrepo.go; + the two UpdateAt cases; + the count      12d, 12e, 16b
test/conformance/           I02(route), I03(correction half), I12, I13, I18, I22, I23,
                            the audit-before-edit AST guard, the outcome-mapping,
                            guarded-route and shared-task-list completeness tests
testdata/classify/cases/    + one due-date correction case                              12c
```

**Dependency-rule check.** `internal/core/correction` imports `internal/core/recall`,
`internal/core/classify` and stdlib — inside `core-purity`'s prefix allow, pointing the direction the
pipeline runs. It imports no `context`, no `internal/ports`, and reads no clock. `internal/httpapi`
gains `internal/brain`, `internal/core/unit` and `crypto/subtle`. `internal/providers/openai` gains
nothing beyond what its chat client already imports. `sqlite-containment` is untouched: nothing
outside `internal/store` gains `database/sql`.

---

## 5. Wire shapes this change fixes

### 5.1 `POST /capture`

```json
{ "text": "no, the dentist is on the 15th, not the 14th", "source": "api", "unit_id": "u-8f1c…" }
```

`source` defaults to `"api"` when absent — it is `units.source`, the caller's fact. `unit_id` is
optional and ignored unless the classification is a correction; ignored rather than rejected,
deliberately.

```json
{ "outcome": "corrected",
  "correction": { "unit_id": "u-8f1c…", "fields": ["event_at"], "referent": "explicit" } }
{ "outcome": "asked",
  "correction": { "question": "Which one did you mean?", "candidates": [ … ] } }
{ "outcome": "stored", "unit_id": "u-a03…", "embedded": true, "candidates": ["u-91…"] }
{ "outcome": "deferred", "kind": "timer", "message": "timers aren't wired up yet — …" }
{ "outcome": "discarded" }
{ "outcome": "recalled", "units": [ … ], "semantic_leg_available": true }
```

`outcome` is always present and is the discriminator.

### 5.2 `POST /recall`

```json
{ "query": "when is the dentist?" }
→ { "units": [ … ], "semantic_leg_available": true }
```

The same `units` rendering and the same flag as an `outcome: recalled` capture, because they are the
same answer from the same method (D9) — a shared renderer, not two that agree today.

### 5.3 A rendered unit

`id`, `type`, `content`, `status`, `weight`, `event_at`, `due_at`, `created_at`, `updated_at`. Not
`weight_decay_rate`, not `confidence`, not `structured_data`. A field is rendered when something can
be done with it.

### 5.4 Authentication

`Authorization: Bearer <token>` on every API route when `server.auth_token_env` names a set
variable, sent by the HTTP client and by `nooma capture` alike (D10, D11). No token configured → no
header required, reachable only on a loopback bind. A 401 carries no detail distinguishing "absent"
from "wrong".

### 5.5 `POST /v1/embeddings` (outbound, PR 17)

```json
{ "model": "text-embedding-3-small", "input": "the dentist is on the 15th" }
→ { "model": "text-embedding-3-small", "data": [ { "embedding": [0.01, -0.02, …] } ] }
```

The stored `unit_embeddings.model` is the **response's** `model`, and `dim` is `len(embedding)`.
Authored from the published API and confirmed by no test that runs in CI — D17 states the limit.

---

## 6. The chain, with the split lines drawn before any code exists

Phase A shipped 16 merges against 6 planned rows; Phase B shipped 15 against 5, with one slice
missing its estimate by 2.6× **because the proof obligation, not the implementation, sets the size**.
Phase C's proposal rows are six. **Seventeen merges** is the honest projection, and the lines are
drawn now rather than when a diff exists.

**PR 14 is last in time despite its number** — `proposal.md:485-489`'s `(13,15,16,17) → 14`. The
table is in merge order.

| # | PR | Content | Est. |
|---|---|---|---|
| 12a | `feat/core-recall-scored` | `FusedCandidate`, `FuseScored`, `Fuse` as its projection; the **hand-computed magnitude table** and the strict-positivity property; the pre-existing suites pass unedited; doc 02 §5.2 | ~320 |
| 12b | `feat/core-correction-referent` | New package; `ReferentMargin`, `Referent`; L1 over 0/1/n candidates, the three ratios at the boundary, and the non-participating third candidate; doc 06 §1's tree line; doc 02 §5 step 4 | ~330 |
| 12c | `feat/core-correction-plan` | `Field`, `Edit`, constructors and accessors, `PlanEdit` with C6's ruled rule; the `AllFields` completeness table; one new corpus case for a due-date correction; doc 02 §5 step 4 | ~400 |
| 12d | `feat/ports-unit-fields` | `UpdateEventAt`/`UpdateDueAt` on the port, `memrepo`, `sqlite`, the two-distinguishable-instants contract cases at L2 **and** L3; `store_api.golden` | ~380 |
| 12e | `feat/ports-signalrepo` | `ports.SignalRepo` + the three vocabularies + `memrepo` + `sqlite` + contract at L2/L3; **I13's behavioural half**; `store_api.golden` | ~400 |
| 12f | `feat/brain-correction-apply` | `correctionRunner`, `applyWithPreImage`, `dispatchEdits`, the two new actions, the pre-image shape, the correction signal; **I23 + the AST guard**; doc 02 §5 step 4 | ~430 |
| 13a | `feat/brain-recall-fortext` | `RecallService` gains the embedder; `ScoredFor`/`ForText`; capture's `recall` and `discarded` routing; the three orphan actions wired; **I22** | ~380 |
| 12g | `feat/brain-correction-route` | Referent resolution over `ScoredFor`, the ambiguous path, `CaptureOutcome`, `CaptureResult` reshaped, capture's `correction` routing; I03's correction half at L2 | ~400 |
| 13b | `feat/httpapi-capture` | **ADR-0017**, `requireToken`/`ResolveToken`, `Handler(Deps)`'s two muxes and the guarded route slice, `POST /capture`, the total status switch, both completeness tests | ~450 |
| 13c | `feat/httpapi-recall-units` | `POST /recall`, `GET /units/{id}` and `GET /units?ids=` through `LiveByIDs`, the shared unit renderer, R2.7's no-decision-row property | ~330 |
| 13d | `feat/serve-wiring` | `cmd/nooma/serve.go` wires config→providers→repos→`Index`→services→token→`Handler`; **`tasksM1Consumes` and its first reader (D18a)**; the first L4 over a real capture and recall | ~420 |
| 17 | `feat/openai-embeddings` | `Embed` on `openai.Client`, the `httptest` L2 (request shape, echoed model, empty `data`, non-200), the `ports.EmbeddingProvider` assertion, and the `Endpoint`→`baseURL` passthrough in the wiring. **Depends only on Phase A's PR 6** and can land at any earlier point, but **must precede 15** — `proposal.md:450-460` | ~200 |
| 15 | `feat/init-provider-paths` | `EnvVarName` and its L1 table, `providerChoice`, `renderProviders`, the Cloud and Ollama paths binding `tasksM1Consumes` incl. **`embedding` (D15's decision on `spec.md` §10's open item)**, `TestFreshVaultIsLoadable` extended to a populated vault, L4 for both paths | ~400 |
| 16a | `feat/doctor-quality-gate` | One new `doctorChecks` row; zero-degradation validity, per-reason reporting, per-task attribution, unreachable≠unsuitable, the zero-iteration no-op; L1/L2 over `fakeprovider`; `TestDoctorOnAHealthyVault` stays green | ~420 |
| 16b | `feat/doctor-coverage` | **D18's two rows** — task coverage (pure config read) and vault coverage (`CountLiveWithoutEmbedding` + store + contract + L3); **D18a's third reader and the shared-list L2 guard**; doc 03:306-307's promise corrected to name which half exists. **Both rows ship** — C10 closed, `spec.md` R6.3/R7.3/R7.4 sanction the method | ~330 |
| 14a | `feat/cli-capture` | `nooma capture` as an HTTP client, `ResolveToken`'s third reader, the wildcard-bind dial rule, the three no-server messages; doc 01's CLI table gains the row; L4 | ~350 |
| 14b | `feat/demo` | The demo walked end to end by hand, `docs/05-build-plan.md`'s M1 section closed, `CLAUDE.md`'s status line | ~180 |

**Dependencies**: `12a → 12b → 12c`; `12d` and `12e` independent of the core slices and of each
other; `(12c,12d,12e) → 12f → 12g`; `12a → 13a`, and `13a → 12g` for `ScoredFor` — **13a lands
before 12g**, the one ordering that does not follow its own numbering, called out for the reason
`m1b`'s C1 exists. Then `(12g,13a) → 13b → 13c → 13d`; `13d → 16a`; **`13d → 17 → 15`**;
`(15,16a) → 16b`; and `(13d,15,16a,16b,17) → 14a → 14b`.

The `17 → 15` edge is the proposal's own (`proposal.md:495-499`), with its reason at `:450-460`;
C9 records that this design derived it independently and one step later than `sdd-spec` did. It is
no longer a design-side flag and no longer a risk.

**12a goes first** — the only slice with no dependency, and it unblocks both 12b and 13a. **17 can
go at any time before 15**, including first, since Phase A's PR 6 shipped; a parallel worktree
could take it while 12a is in review.

---

## 7. Test matrix

Strict TDD is mandatory (`CLAUDE.md` non-negotiable #4) and is **no longer backed by a Makefile
target**: `make pending-red` was retired in `714934e`. Every row below is written before its
implementation and observed failing for the right reason; the discipline is now carried by review
and by the commit sequence (`spec.md` **R8.2**, renumbered from R7.2 when the spec inserted §6 for
PR 17). Worth stating plainly, because a retired gate is easily read as a retired rule — the rule
outlived the gate that once proved it.

| What | Level | Where | PR |
|---|---|---|---|
| `FuseScored`: each returned **score magnitude** equals `Σ w_i/(RRFK + rank_i)` computed by hand — the only proof of magnitude that exists | L1 | `internal/core/recall/` | 12a |
| Every fused score is strictly positive, incl. last-rank-in-one-list | L1 | `internal/core/recall/` | 12a |
| `Fuse` still returns exactly what it returned: `fuse_test.go`'s tie-break fixture and `recall_corpus_test.go` pass **unedited** (order only — they cannot see magnitude) | L1 + L2 | existing | 12a |
| `Referent`: zero, one and many candidates; ratios at `1.4999`, `1.5`, `1.5001`; a margin ≤ 1; a third candidate that would flip the answer if it participated | L1 | `internal/core/correction/` | 12b |
| `AllFields()` completeness: for each `Field`, exactly one accessor reports true | L1 | `internal/core/correction/` | 12c |
| `PlanEdit`: every row of D3's ruled table, incl. both-dates → false, no-content → false, and **a date-carrying correction leaving content untouched** | L1 | `internal/core/correction/` | 12c |
| `PlanEdit` over the corpus's correction cases produces the field each case implies | L2 | `test/conformance/` | 12c |
| `UpdateEventAt`/`UpdateDueAt` write their own column and `updated_at`, driven with **two distinguishable instants**, touching nothing else (I18) | L2 + L3 | `repocontract` via `memrepo` and `sqlite` | 12d |
| A signal whose `target_id` names a unit that was never created persists and reads back (**I13**) | L3 | `internal/store/sqlite/` | 12e |
| `SignalRepo` contract; `AllSignalTypes`/`AllValences` closed and complete | L2 | `test/conformance/` | 12e |
| **I23** — a correction whose `DecisionLog.Record` fails leaves the unit unchanged field by field | L2 | `test/conformance/` | 12f |
| No file under `internal/` outside `store`/`ports` calls an `Update*` method except `dispatchEdits`, and `dispatchEdits` is called only after `recordPreImage` in statement order | L2 | `test/conformance/`, `go/ast` | 12f |
| A successful correction writes exactly one `correction.applied` row whose `context.previous` equals what `ByID` returned before the edit | L2 | `test/conformance/` | 12f |
| A correction emits exactly one `correction` signal, `negative`, targeting the unit | L2 | `test/conformance/` | 12f |
| **I22** — one text through `CaptureService` (classified `recall`) and through `RecallService.ForText` returns the same ordered ids | L2 | `test/conformance/` | 13a |
| An embedding failure on the read path returns the lexical leg alone with the degradation flag set, through **both** entrances | L2 | `test/conformance/` | 13a |
| An explicit `unit_id` bypasses recall entirely — an instrumented index that fails if queried; an unknown explicit id errors and edits nothing | L2 | `test/conformance/` | 12g |
| An ambiguous referent writes `correction.ambiguous`, no `UnitRepo` update, and returns `OutcomeAsked` | L2 | `test/conformance/` | 12g |
| **I03's correction half** — a correction is an UPDATE: the unit count is unchanged and the id survives; no `Create`, no `SetStatus` | L2 | `test/conformance/` | 12g |
| The margin is computed after the live filter: a `superseded` top scorer is dropped and the ratio recomputed over the survivors (I02) | L2 | `test/conformance/` | 12g |
| Every `AllCaptureOutcomes()` member maps to a status code — the mapping is total | L2 | `test/conformance/` | 13b |
| Every route in the guarded slice returns 401 without a token when one is configured, and 200 with it; **the no-token and wrong-token responses are compared to each other and are byte-identical** (R2.11's oracle MUST NOT) | L2 | `internal/httpapi/` | 13b |
| For every `(bind, auth_token_env, env set?)` combination where `DecideBinding` succeeds and `ResolveToken` returns `""`, the bind is loopback | L2 | `internal/httpapi/` | 13b |
| `POST /capture` over `httptest`: each outcome's body and status | L2 | `internal/httpapi/` | 13b |
| `GET /units/{id}` returns the **same** 404 shape for a `superseded` unit and an unknown id (I02) | L2 | `internal/httpapi/` | 13c |
| No route writes a `decision_log` row (R2.7), asserted over all four | L2 | `internal/httpapi/` | 13c |
| The compiled binary: `serve`, a real `POST /capture`, a `POST /recall` that finds it | L4 | `test/e2e/` | 13d |
| `openai.Client.Embed` against `httptest`: path, `Authorization`, request `model`/`input`, response decode, **the echoed model is what is returned**, empty `data` → error, non-200 → error carrying the body | L2 | `internal/providers/openai/` | 17 |
| A provider entry's `endpoint` reaches the client as its `baseURL`, and an empty one falls back to the provider's default | L2 | `cmd/nooma/` | 17 |
| **R6.3, the *effective* question at build time, level 1 of 2 (N2)** — a wired pipeline with `tasks.embedding` bound returns `Embedded: true`; the same pipeline with it unbound returns `Embedded: false`, so the two are distinguishable | L2 | `test/conformance/` | 15 |
| **R6.3, level 2 of 2 (N2)** — a wizard-written Cloud vault whose `openai` entries point at a loopback `httptest` server, driven through the compiled binary, leaves a `unit_embeddings` row | L4 | `test/e2e/` | 15 |
| `NewEnvVarName` rejects real-shaped API keys and accepts real variable names | L1 | `cmd/nooma/` | 15 |
| A wizard run with a scripted key: the literal key appears nowhere in `nooma.yml`, and `.env` holds it at `0o600` | L4 | `test/e2e/` | 15 |
| A wizard-populated vault decodes and validates through `config.Decode`/`Validate`, both paths, and binds every member of `tasksM1Consumes` | L2 | `cmd/nooma/` | 15 |
| The gate: a clean response passes; each I14 degradation shape fails, and the report names the field, the reason and the task | L2 | `cmd/nooma/` | 16a |
| The gate: one task passing and another failing are reported separately | L2 | `cmd/nooma/` | 16a |
| The gate with zero configured tasks reports `ok (0 tasks configured)` and zero failures; `TestDoctorOnAHealthyVault` stays green unchanged | L2 + L4 | `cmd/nooma/`, `test/e2e/` | 16a |
| The gate on a transport error reports **unreachable**, never a JSON-fitness verdict | L2 | `cmd/nooma/` | 16a |
| **D18a** — `serve`'s wiring, `init`'s wizard and `doctor`'s coverage check all read `tasksM1Consumes`, and every member is in `config.DocumentedTaskNames` | L2 | `test/conformance/` | 16b |
| **D18b row 1** — a vault with providers but no `embedding` binding FAILS coverage naming the degradation; a vault with no providers at all reports `ok (no providers configured)` | L2 | `cmd/nooma/` | 16b |
| **D18b row 2** — `CountLiveWithoutEmbedding` over a vault holding embedded and unembedded live units returns the unembedded count, and archived units do not count | L2 + L3 | `repocontract`, `internal/store/sqlite/` | 16b |
| The compiled binary: `nooma capture` against a running `nooma serve`; its three no-server/wrong-address messages; its refusal when `auth_token_env` names an unset variable | L4 | `test/e2e/` | 14a |
| The demo, by hand: two captures (API + CLI), one ask, one correction — and no timer | — | — | 14b |

No test reaches a real provider or the network: every LLM response comes from `testdata/llm/`
through `fakeprovider`, every embedding from `NewEmbeddingFake` or a corpus-stated vector, every
provider client from `httptest`, and every wizard run from scripted input.

---

## 8. Risks this design accepts

| # | Risk | Position |
|---|---|---|
| 0 | ~~**C10 is open**~~ — **CLOSED.** `spec.md` revision 4 sanctions `EmbeddingRepo`'s consistency method in R6.3, R7.3 and R7.4, on the debt `m1b-pipeline/design.md:790-793` recorded and `docs/03-data-model.md:306-307` already promised | No longer a risk. Kept as a row rather than deleted, because the escalation was correct and the record should show a conflict that was filed, escalated and closed — not a gap in the numbering. Both D18b rows ship in 16b at the estimate already priced |
| 1 | **PR 17's wire shape is confirmed by no test that runs in CI** | Accepted and named in D17. No test may reach the real endpoint, so the `httptest` fixture is authored from the published API by the same author who wrote the decoder — C12's shape. Mitigated by loudness (a shape mismatch is a decode error or the empty-`data` error, never a plausible wrong vector) and by D18, which makes a silently missing vector unshippable. First real confirmation is a human running `doctor` with a live key, and PR 17's description says so |
| 2 | **A correction that moves a date leaves the body stale** | Owner-accepted under C6, stated plainly rather than mitigated. A stale memory is recoverable by a second correction; a body overwritten with the correction utterance is not, until M4 reads the pre-image back |
| 3 | **`nooma capture` requires a running server** | Owner-accepted under C8. The three-message diagnosis (D11) is what keeps the failure legible; an offline mode is additive and named |
| 4 | **Two `time.Time` parameters on `UpdateEventAt`/`UpdateDueAt` can be swapped** | Accepted as the residual I18 risk no signature closes, pinned by a contract case answered twice with distinguishable instants |
| 5 | **The AST guard proves call order, not row content** | Accepted and announced in the guard's own doc comment; the content half is a behavioural L2 test in the same PR |
| 6 | **An audit row can describe a correction that did not land** | Accepted, and it is the chosen direction: over-inclusive beats under-inclusive when the edit is destructive — the inverse of `m1b` D8's direction, for D8's own reason |
| 7 | **D18a proves three readers agree, not that a bound provider works** | Accepted and stated in D18. It closes the configuration half of C9's shape; D16 closes the fitness half and D18b row 2 closes the outcome half. No single mechanism covers all three, and pretending one did is how C9 happened |
| 8 | **D18b row 2 fails on a vault mid-backfill** | Accepted: a nonzero count is exactly what a half-indexed vault should report, and the message says what it means rather than implying corruption. When M6's `reindex` lands, a resumable loop is the fix and this row is how a user knows to run it |
| 9 | **ADR-0017 adds an auth path this milestone did not budget** | Accepted: it is C4's owner resolution, ~150 lines including the truth-table test, and it is the difference between a write route the LAN can reach and one it cannot |
| 10 | **`/ui` and `/` stay open when a token is configured** | Accepted and scoped in ADR-0017: ADR-0007 ties the UI to a cookie handshake and the UI is M4 |
| 11 | **`CaptureResult` is reshaped, so Phase B tests are edited** (C7) | Accepted: every edit is to an assertion about a renamed field, never to a conformance claim |
| 12 | **`SignalType` declares eleven members with one producer** | Accepted: a vocabulary mirroring migration 0002's own comment, pinned by a completeness test |
| 13 | **Seventeen merges against six proposal rows** | Accepted, with every split line drawn before any code exists. Phase A/B measured 1.3×–2.2×, one slice at 2.6× on **test** surface; 12f, 13b, 12g and 16a are the rows to read through that band |
| 14 | **`docs-sync` has fewer genuine doc 02 deltas to give than Phase B did** | Accepted and pre-assigned in D13; each core slice has a real delta, so `no-spec-change` is not needed |
| 15 | **`nooma doctor` now makes live network calls at runtime** | Accepted: non-negotiable #5 binds tests, not the binary. Bounded by a timeout, and a transport failure is reported as unreachable rather than as a verdict on the model |
| 16 | **Strict TDD lost its Makefile gate** when `pending-red` was retired in `714934e` | Accepted and named: the rule is unchanged and is now carried by review and by each PR's commit sequence. Stated in §7 so no slice reads a retired gate as a retired obligation |
| 17 | **The spec assigns R6.3's test two levels** (R6.3 says L2, R8.1 says L4) | Accepted, and satisfied both ways rather than chosen between — N2. Both are cheap once D15's `endpoint` passthrough exists, and they prove different things. Named so the choice is not made silently by whoever writes the test |
| 18 | **`spec.md` R6.2's fallback clause describes a chain state that can no longer occur** | Accepted and recorded as N1. Harmless as written; the design decides the open item it left (PR 15's diff performs the binding) rather than carrying the ambiguity into tasks |

---

## 9. What this design does not decide

- **Nothing. No conflict filed by this design remains open.** C10 was the last one, and `spec.md`
  revision 4 closed it by sanctioning `ports.EmbeddingRepo`'s consistency method in R6.3, R7.3 and
  R7.4 — the branch this design recommended, grounded in the debt `m1b-pipeline/design.md:790-793`
  named and the promise `docs/03-data-model.md:306-307` already made. Both D18b rows ship in 16b,
  at the estimate already priced. The two doc 02 gaps (C2, C3) are closed by rulings and land as
  prose in the PRs D13 assigns.
- **The reconciliation obligation revision 3 set for itself is discharged.** `spec.md` revision 3
  landed and was read in full against this design. **C5 and C7 are closed by it** — R1.2 now
  constrains `Fuse`'s exported contract rather than its body, and R2.1/R2.2 are rewritten for
  contract rather than for today's struct. C6, C8 and C9's rulings are rendered in the spec exactly
  as this design carries them. Three artefacts of the landing are recorded as §2.1's notes rather
  than absorbed: R6.2's now-unreachable fallback clause (**N1**, and §10's open item is decided in
  D15), R6.3-vs-R8.1's double level assignment (**N2**, satisfied both ways), and the two
  quotations in the spec's Conflicts 3 and 4 that revision 3 had rewritten (**N3**, restored
  verbatim so the citations resolve).
- **The spec's renumbering breaks nothing that is still standing.** Its old §6–§9 became §7–§10 when
  §6 was inserted for PR 17. This design carried **five** affected references and all five are
  fixed: four `spec.md` §9 → §10 (D2's package placement, D5's pre-image shape, D6's valence, D10's
  wire shapes) and one `spec.md` R7.2 → **R8.2** (§7's strict-TDD note, which under the new
  numbering would otherwise have pointed at the `docs-sync` requirement instead — a citation that
  still resolves to a real requirement and says the wrong thing, which is the worst kind).
  Every other citation this design makes is to §1–§5, whose R-numbers did not move; the one
  §5-adjacent hazard, old R5.7 (corpus coverage) becoming R5.8 while a new R5.7 took its number,
  is not hit — this design cites R5.1, R5.2, R5.3, R5.4 and R5.6 only.
- **Undo.** ADR-0016 records the pre-image and explicitly does not offer it back; no surface reads
  `decision_log` until M4. D5's keys are chosen so that surface has something to read.
- **The UI's authentication.** ADR-0007's cookie handshake, deferred to M4 by ADR-0017 with M4 named
  as the owner.
- **A relation-rejection path**, and with it I10 and the `relation_reject` signal (C3).
- **`GET /units` as a full collection.** `GET /units?ids=` is decided; a listing route needs
  pagination and ordering rules whose first real consumer is M4's UI.
- **Multi-field corrections.** C6 ruled one field. `PlanEdit` returning `[]Edit` keeps the door open
  at zero cost, but a *transactional* multi-field edit is a further decision with a further cost —
  `ports.UnitRepo` has no transaction concept — and it belongs with the surface that can express one.
- **The fts half of doc 03's consistency promise.** 16b delivers units↔embeddings; units↔fts is M6's,
  and 16b's doc delta says which half exists rather than letting the promise read as fully kept.
- **The in-memory index's eviction path.** Phase C still leaves nothing `pool`, so D2's `LiveByIDs`
  boundary keeps eviction unnecessary rather than merely unimplemented.
- **An embedding-model change path.** PR 17 makes two models representable in one vault (I21 already
  covers the read side), but `reindex` is M6's and no Phase C surface migrates an existing vault from
  one embedding model to another.
