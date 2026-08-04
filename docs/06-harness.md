# 06 — Build harness

This document defines **how Nooma gets built**: layout, dependency rules, test taxonomy,
automated gates, and conventions. It does not describe what the brain does (that is
[`02-cognitive-core.md`](02-cognitive-core.md)) but what must be true of every line written.

Guiding principle: **rules a machine does not execute are not rules, they are intentions.**
Everything this document declares is either verified by CI, or explicitly accepted as human
discipline — and it says which.

---

## 1. The dependency rule

The cognitive core does not know that SQLite, HTTP, Telegram, or an LLM exist. It takes data,
returns decisions. Everything else is an adapter around it.

This is not architectural purism: it is the precondition for the brain's decisions — the only
hard part of this project — to be testable in milliseconds and without a network.

```
nooma/
├── cmd/nooma/              # main + CLI. The only place that does wiring
├── internal/
│   ├── core/               # THE BRAIN. Pure functions. Zero I/O
│   │   ├── unit/           # the unit, types, status transitions
│   │   ├── classify/       # the classification taxonomy, tolerant decoding
│   │   ├── weight/         # effective_weight, decay, boost, spreading
│   │   ├── focus/          # priority, the two focuses, hysteresis
│   │   ├── recall/         # RRF fusion
│   │   ├── relation/       # persist/surface thresholds, uncertain band
│   │   ├── correction/     # referent gate, edit plan (doc 02 §5 step 4)
│   │   ├── consolidation/  # the decision logic of each of the 8 phases
│   │   ├── prospection/    # staleness, quiet hours, digest vs push, recurrence
│   │   ├── selfmodel/      # beliefs, current_state
│   │   └── learning/       # signals → knob adjustment
│   ├── ports/              # interfaces: repos, providers, channels, Clock, IDGen
│   ├── brain/              # services orchestrating core + ports (capture, consolidate…)
│   ├── store/              # SQLite: migrations, repos, vec, fts, lockfile
│   ├── providers/          # anthropic, openai, ollama, whisper
│   ├── channels/           # telegram, whatsapp (disabled)
│   ├── httpapi/            # handlers, auth (ADR-0007)
│   ├── ui/                 # templ + htmx + embedded assets (ADR-0008)
│   ├── scheduler/          # in-process cron, boot catch-up (ADR-0009)
│   └── config/             # yml + .env, vault resolution
├── docs/
├── test/
│   ├── conformance/        # L2: doc-02 invariants, no build tag
│   ├── integration/        # L3: migrations, FTS5, transactions, lockfile (tag: integration)
│   └── e2e/                # L4: the compiled binary end to end (tag: e2e)
├── testdata/               # golden sets and fixtures
├── scripts/                # verification scripts invoked by make targets
└── .github/workflows/
```

### `core/` vs `brain/`

The distinction that makes this work:

- **`core/`** holds the **decision**: "does this overdue trigger fire or expire?", "which units
  enter the focus?", "does this relation get persisted?". Data in, data out.
- **`brain/`** holds the **orchestration**: read from the repo, call `core`, write the result,
  record in `decision_log`, publish the event to the channel.

The 8 consolidation phases live split: each phase's logic in `core/consolidation`, the pass
that runs them in order and persists in `brain/`.

### How it is enforced

In `golangci-lint`, not in code review:

- **`depguard`** — `internal/core/**` may not import `internal/store`, `internal/providers`,
  `internal/httpapi`, `database/sql`, `net/http`, or any external dependency that is not pure
  computation stdlib.
- **`forbidigo`** — inside `internal/core/**`, `time.Now`, `time.Since`, `rand.`, `uuid.New`,
  and `os.Getenv` are forbidden. See §2.

A PR that violates the dependency rule never reaches review: the lint does not pass.

---

## 2. The clock is a port

**No function in `core/` calls `time.Now()`.** The current instant arrives as a plain
parameter. Same for identifiers and any source of randomness.

```go
// internal/ports
type Clock interface { Now() time.Time }
type IDGen interface { New() string }
```

**`core/` does not import `ports` either.** The ports are consumed by `brain/`, which reads
the clock **once** at the start of an operation and passes the resulting `time.Time` down into
the core:

```go
// brain/ reads the clock once...
now := s.clock.Now()
// ...and core decides as a pure function of its arguments
decision := prospection.EvaluateTrigger(trigger, now, thresholds)
```

This is stricter than "inject a Clock" and better for two reasons. A core function is a pure
function of its arguments, with nothing to stub. And one decision gets exactly **one** instant:
a core that held a `Clock` could call `Now()` twice mid-decision and compare two different
"nows" — a real bug class that this rule makes unrepresentable.

The `depguard` allow-list enforces it: `internal/core/**` may import the standard library and
its own packages, nothing else.

This is **not** a style preference. Look at how much of Nooma's behavior is a function of time:

decay and `effective_weight` · temporal urgency in priority · `incomplete` units expiring at
24 h · trigger `fire_at` · the boot staleness gate (6 h / 3 h) · quiet hours · event lead time
· yearly and monthly recurrence · digest cadence · `goal_stagnation_days` · the learning
module's cooldown · the incremental `learn` checkpoint

That is nearly everything. A `time.Now()` scattered through the core turns every one of those
behaviors into something you can only test by waiting.

And there is a concrete consequence already written into the plan: **the M2 demo is "a vault
with simulated weeks of data"**. That demo is literally impossible without an injected clock.
It is not a testing convenience — it is a product requirement we already committed to.

The user's timezone follows the same rule: it is vault data passed in as a parameter, never
read from the operating system inside the core.

---

## 3. Test taxonomy

Four levels, with different costs and triggers. The rule: **a test lives at the cheapest level
where it still proves something real.**

| Level | Where | Build tag | Touches | When it runs |
|---|---|---|---|---|
| **L1 — Pure** | next to the `core/` code | none | nothing | Always. Milliseconds |
| **L2 — Conformance** | `test/conformance/` | none | `core` + `brain` with fakes | Always |
| **L3 — Integration** | `test/integration/` | `integration` | a real temporary SQLite vault | In CI and on demand |
| **L4 — Smoke E2E** | `test/e2e/` | `e2e` | the compiled binary | In CI, before release |

- **L1** covers the pure functions. Hard coverage floor: **≥ 90 % in `internal/core/`**. There
  is no global coverage floor — global coverage is a metric you satisfy by writing useless
  getter tests.
- **L2** is the level this project needs and almost no project has. See §4.
- **L3** verifies what only SQLite can disprove: migrations, FTS5 registration and
  synchronization, transactions, the single-writer lockfile. Each test starts from an empty
  temporary vault.
- **L4** compiles the binary and walks the user path: `nooma init`, `nooma serve`, a capture
  via API, a recall, `nooma doctor`, `nooma export`.

No level calls an LLM or an external API. Ever. Providers are served from fixtures (§5). A test
that depends on the network is a test that will fail on a Tuesday with nobody having touched
anything, and three weeks later the team has learned to ignore a red CI.

---

## 4. Conformance: `02-cognitive-core.md` made executable

The README declares doc 02 the source of truth for behavior. Today it is prose. Prose nobody
executes drifts from the code — always, without exception, in every project.

The conformance suite turns the **hard invariants** of doc 02 into tests. Each test carries in
its name the section it verifies, and **it is written before the implementation that satisfies
it.**

A hard invariant is a binary claim about behavior, independent of the calibratable numbers.
Initial extraction:

| # | Invariant | Doc 02 |
|---|---|---|
| I01 | `status='focus'` does not exist. Focus is a query, never persisted | §3 |
| I02 | Every LIVE read surface excludes `superseded` and `incomplete` | §1 |
| I03 | Nothing is deleted: archiving is a state transition. No path emits `DELETE` on `units` | §1 |
| I04 | A timer is never a unit: no weight, no decay, no graph, no beliefs | §8 |
| I05 | `effective_weight` is computed on read; decay is not written on every read | §2 |
| I06 | An `incomplete` unit has no embedding until promoted | §1, doc 03 |
| I07 | A relation is unique per `(from, to, type)` | §4 |
| I08 | Confidence < `min_confidence_to_persist` → the relation is not stored | §4 |
| I09 | The `[persist, surface)` band → stored **and** asked about in the digest | §4 |
| I10 | Rejecting a relation deletes it **and** emits `relation_reject` before deleting | §4, §9 |
| I11 | The 8 consolidation phases run in order, and `learn` is always last | §6 |
| I12 | Every automatic decision with an effect writes to `decision_log` | §11 |
| I13 | A `learning_signal` outlives the deletion of its target (no FK) | §9 |
| I14 | A malformed `classify` field degrades to null; it never aborts the classification | §5 |
| I15 | A trigger overdue past the threshold → `expired`, never `fired` | ADR-0009 |
| I16 | Nothing is delivered during quiet hours except the defined push exception | §7 |
| I17 | Firing a recurring trigger creates the next one pointing at the **same** unit | §7 |
| I18 | `event_at`, `created_at`, and `due_at` are never interchanged | §1 |
| I19 | A challenger must beat the incumbent by more than `hysteresis_margin` | §3 |
| I20 | One active insight per metric; the previous one becomes `superseded` | §12, doc 03 |
| I21 | Every vector search filters on `model`; embeddings from two models never compare | §5 |
| I22 | Capture's own recall entrance and the standalone `/recall` route are one mechanism, called with the same raw text, never `normalized_content` | §5 |
| I23 | A correction's pre-image is recorded before its edit is applied; a failed audit write leaves the unit untouched | §5 step 4 |

Four of these are better verified with a structural test than a behavioral one:

- **I01** — a test that fails if the literal `"focus"` appears as a status value in the tree.
- **I03** — a test that fails if a `DELETE FROM units` appears outside the migrations.
- **I13** — a test verifying the migration declares no FK on `learning_signals.target_id`.
- **I23** — a `go/ast` test asserting `applyWithPreImage` is the only function that reaches any
  `UnitRepo.Update*` method, and that `recordPreImage` precedes `dispatchEdits` in its statement
  order. A behavioral test proves the ordering holds on the paths it exercises; this one proves no
  other path exists. It also refuses to pass vacuously — it fails if it scans no files, if
  `applyWithPreImage` is absent, or if either call is missing — and refuses to guess, failing
  loudly when the two calls sit in one statement or inside a closure where their order cannot be
  read.

They are ugly tests and they are worth gold: they are exactly the invariants somebody will
break without noticing eight months from now, with the best intentions.

**Maintenance rule**: if a conformance test fails, there are two legitimate exits — fix the
code, or change doc 02 **and** the corresponding ADR in the same PR. Weakening the test so it
passes is not one of the two.

---

## 5. Golden sets and fixtures

Three data artifacts built **before** the code that consumes them:

### `testdata/recall/` — the recall golden set (ADR-0010)

A small corpus of units with realistic content, a set of queries, and the expected order of
the fused result. It is the regression detector for recall quality: without it, "recall got
worse" is a feeling; with it, it is a test that fails with a diff.

It feeds all three consumers of hybrid recall: answering a `recall`, finding dedup candidates
during capture, and finding pairs during the `connect` phase.

### `testdata/classify/` — the JSON gate corpus (ADR-0002)

Input messages with their expected structured output, covering the complete taxonomy of doc 02
§5 and the orthogonal resolution fields. **Written once, used in two places**: the capture
pipeline tests, and the `nooma doctor` provider quality gate. Sharing the corpus is
deliberate — what we test is what we ask the user to validate on their machine.

It must include deliberately broken cases: truncated JSON, a field with the wrong type, an
unknown enum. Those are the ones that prove I14. It must also include at least one otherwise
well-formed response wrapped in a markdown code fence — doc 02 §5.1's preamble-tolerance rule,
confirmed against a live OpenAI key — since a fence is not one of I14's three malformed shapes: a
fenced-but-clean object must decode with **zero** degradations, not a recorded one.

### `testdata/llm/` — recorded responses

Real responses from each provider, recorded once and replayed by a fake provider. They allow
testing the full pipeline without network and without cost.

---

## 6. CI gates

All of the following runs on every PR and blocks the merge:

| Gate | What it verifies |
|---|---|
| `golangci-lint` | Includes `depguard` and `forbidigo` — §1 and §2 are not optional. Runs against every build-tagged file too (`.golangci.yml`'s `run.build-tags`), not only the default untagged build — a linter that only ever sees L1/L2 code would silently exempt `test/integration/**` and any future `e2e` file from every rule in this table. `pendingimpl`-tagged files (`test/conformance/`) were a deliberate exception while I01/I03/I21 anchored to symbols that did not exist yet, so linting them would have reported those as permanent errors — they stayed out of `run.build-tags` until each anchor was promoted (§8 point 5). I21, the last one, was promoted in `m1b-pipeline` PR 8a; no file carries the `pendingimpl` tag today |
| `go vet` | — |
| L1 + L2 tests with `-race` | The core and the invariants |
| L3 tests with `-race` | Migrations and store against real SQLite |
| `internal/core/` coverage | ≥ 90 %, hard floor |
| `templ generate` leaves a clean tree | The committed `_templ.go` files are current (ADR-0008) |
| Schema golden | Apply all migrations from scratch and compare the resulting schema against a versioned dump |
| docs↔code sync | See below |

**Schema golden** deserves a note: it is the gate that stops the real schema from drifting away
from [`03-data-model.md`](03-data-model.md). A migration that changes a table without updating
the dump fails CI, and the dump's diff is what gets reviewed in the PR. The committed dump lives
at `testdata/schema/structure.golden` (the structural projection compared against doc 03) and
`testdata/schema/ddl.golden` (the raw, normalized DDL, self-diff only); `make schema-golden`
regenerates both from the embedded migrations.

**docs↔code sync**: the README already declares the rule ("either the code gets fixed or the
doc gets updated in the same PR"). The executable version: a PR touching `internal/core/**`
must also touch `docs/02-cognitive-core.md`, or carry the `no-spec-change` label. The label
exists because there are legitimate changes that do not alter the specification — refactors,
performance. But applying it is a conscious act that gets recorded, not an oversight.

**L4 (e2e) and the cross-compilation matrix run on every PR too**, per
[ADR-0013](adr/0013-cross-compile-targets.md) — the matrix across seven targets: `linux` on
`amd64`/`arm64`/`arm`, `darwin` on `amd64`/`arm64`, `windows` on `amd64`/`arm64`. They live in
`main.yml` rather than `ci.yml` because they are the slow jobs, and each matrix leg posts its own
status check.

What does **not** run on every PR: driver benchmarks. Those run before release.

A distinction the matrix earns its keep by not blurring: cross-compilation proves the code
**builds** for a target, never that it **behaves** there. Platform behavior needs a test that names
the platform, which is why `integration (windows)` and `e2e (windows)` run the L3 and L4 suites on
`windows-latest`. The distinction was not theoretical: `windows/amd64` cross-compiled green from
the day the harness landed, while the SQLite store could not open a vault on Windows at all.

Those two are **separate jobs, never matrix legs of the Linux ones**. Matrixing a job renames the
check it posts, so `integration` and `e2e` — both required contexts — would stop posting under the
names the ruleset waits for, and every merge to `main` would block on a check that never arrives.
They also invoke `go test` directly instead of their `make` targets, because `make` is not on PATH
on GitHub's Windows runners; the duplication is deliberate and is the only one in the workflows.

### Three layers: gates, `CLAUDE.md`, and skills

Part of Nooma's development is done with AI assistants. That does not change the rules, but it
does add two places worth putting them before the gate catches them:

| Layer | What it holds | Who it governs | When it acts |
|---|---|---|---|
| **CI gates** | Everything a machine can verify | Everyone | After the fact |
| **`CLAUDE.md`** (root) | The non-negotiables, always loaded | The assistant, every session | Before the fact |
| **`.claude/skills/`** | Decision procedures CI cannot see | The assistant, by trigger | Before the fact |

**Precedence rule: if a rule can be an automated gate, it is a gate — not a skill.** A skill is
an instruction to a model, and therefore probabilistic; a lint is deterministic. Turning a gate
into a skill downgrades a guarantee into a suggestion.

The argument that settles it: Nooma is AGPL and expects third-party contributions. A skill
governs the assistant of whoever has it loaded. CI governs everyone — the external
contributor, a different model, and the same assistant with a compacted context.

What does belong in skills is what CI **cannot** see: whether the conformance test was written
before the implementation, which level a test should have gone to, whether a new decision
belongs in `core/` or `brain/`, and — the most dangerous gap — that weakening a conformance
test to make it pass **also makes CI pass**.

Current skills: `nooma-core` (dependency rule and injected clock) and `nooma-testing`
(taxonomy and conformance discipline). **Skills point at these docs, they do not restate
them**: a skill that copies content drifts, and then there are two sources of truth fighting
each other again.

---

## 7. Conventions

- **Commits**: conventional commits. A commit is a reviewable unit of work: the change, its
  tests, and its doc together. No "wip" or "fix tests" as separate commits.
- **PRs**: soft ceiling of 400 changed lines. Above that, split into chained PRs. A PR nobody
  can genuinely review is a PR that gets approved without review.
- **Branches**: trunk-based, short-lived.
- **ADRs**: every architectural decision is a new ADR. An `Accepted` ADR is never edited.
- **Migrations**: SQL embedded with `go:embed`, versioned with `PRAGMA user_version`, applied
  when opening the vault. **A published migration is never modified** — write the next one.
  There are real users' vaults on the other side.
- **Calibratables**: every number in the doc 02 §13 table is a named constant in exactly one
  place. A threshold literal buried in a function is a number nobody will be able to calibrate
  when real data arrives.
- **Language**: everything in the repository is in English — code, identifiers, comments,
  commit messages, docs, skills, and UI copy. Nooma is an AGPL project that expects community
  contributions; English is the entry condition, not a preference. UI internationalization is
  deferred, not ruled out.

---

## 8. Definition of Done for the harness

The harness is ready when, over a repo with no business logic:

1. `go build ./...` works on a clean machine without installing extra tools.
2. `golangci-lint run` passes, and an import of `internal/store` inside `internal/core`
   **fails**.
3. A `time.Now()` inside `internal/core` **fails** the lint.
4. The four test levels run with their tags, even if nearly empty.
5. There was at least one conformance test per structural invariant (I01, I03, I13), watched
   failing red because there was no implementation yet.
6. CI runs every gate in §6 and blocks the merge.
7. Migrations apply from scratch and the schema golden matches
   [`03-data-model.md`](03-data-model.md).
8. `LICENSE` with AGPL-3.0 ([ADR-0004](adr/0004-license.md)).

Point 5 is the one usually skipped and the one that matters: the harness is not proven until
you have watched it fail for the right reason.

---

## 9. Build order

The harness and the ADR-0001 spike overlap: the spike needs a repo to live in, and the release
CI needs the spike's result. The sequence that unties the knot:

1. **Minimal repo**: `go.mod`, empty layout, lint with the rules from §1 and §2, CI running
   lint and L1. No logic.
2. **ADR-0001 spike** against the seven criteria in the ADR. It lives on a branch and **is not
   merged**: it is an experiment, and its output is an ADR, not production code.
3. **Close ADR-0001** — accepted, or superseded by the one that picks the fallback.
4. **Rest of the harness**: migrations, schema golden, the four test levels, the golden sets
   empty but with their format defined, the complete CI.
5. **M0** as laid out in [`05-build-plan.md`](05-build-plan.md).

Step 2 is the only place in the whole project where code is written to be thrown away. It
should be thrown away: a spike that gets merged stops being a spike and becomes debt with good
PR.
