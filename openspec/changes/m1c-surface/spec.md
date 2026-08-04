# Spec — M1c: the surface

Delta specification for the `m1c-surface` change, the third of three chained SDD changes that
split `openspec/changes/m1-capture-recall/proposal.md` (owner decision, 2026-07-30, proposal §8
Q5). This document states what MUST be true of the repository after this change is applied, in
testable form. It does not prescribe how (that is `design.md`'s job).

Sources verified by direct reading, not inferred: `openspec/changes/m1-capture-recall/proposal.md`
as updated by PR #101 and PR #102 (§3.2 items 11/14/15, §3.4, §5's now-six-row Phase C table and
its three closing notes, §8 Q3b/Q3c/Q4/Q5, and the merged `6 → 17 → 15` dependency line, `e70c435`)
— re-read in full for this revision, not worked from a summary — `openspec/changes/m1c-surface/design.md`
(this change's own design, whose reconciliation against this spec found the two wording
divergences and one overruling revision 4 corrected — §2's C5, C6, C7, C8 — and, this revision,
filed and correctly declined to resolve C10, a disagreement between this spec's own R6.3 and
R7.3/R7.4), `openspec/changes/m1b-pipeline/design.md:790-793` (the deferred `EmbeddingRepo`
consistency method, and its own named recipient — "whoever ships `doctor`'s consistency check"),
`openspec/changes/m1b-pipeline/spec.md` (format and rigor this document matches),
`docs/02-cognitive-core.md` §1, §4, §5 (step 4 and its M1 note), §9, §11, §13, `docs/06-harness.md`
§1–§7, `docs/03-data-model.md` (`units`, `learning_signals`, `relation_thresholds`, and §"Operational
properties of the vault" at `03:306-307` — `nooma doctor`'s pre-existing units↔embeddings↔fts
consistency promise), `docs/adr/0002-default-llm-preset.md`, `docs/adr/0007-http-auth.md`,
`docs/adr/0016-correction-pre-image.md`, `testdata/llm/format.md`, and the tree as it exists
today: `internal/core/recall/fuse.go`, `internal/core/classify/{kind.go,tounit.go,
classification.go}`, `internal/core/unit/{unit.go,transition.go}`, `internal/brain/
{capture.go,recall.go,result.go}`, `internal/ports/{unitrepo.go,decisionlog.go,relationrepo.go}`,
`internal/httpapi/{server.go,binding.go}`, `cmd/nooma/{main.go,init.go,status.go,doctor.go}`,
`test/e2e/doctor_test.go`, `test/conformance/*.go`, `testdata/classify/cases/correction-not-friday.json`,
`openspec/changes/m1b-pipeline/tasks.md` (C11, C12), and the git log of this repository
(`877f647`, `522abd8`, `24f48a3`, `e70c435`).

Two facts about the tree, verified before writing a single requirement, that shape this
document more than anything else:

1. **Phase B is already built, further than the umbrella proposal describes it.** `internal/brain/
   capture.go` already runs the full pipeline through the relation judge (PR 11c), including the
   Q3a routing fork for `timer`/`recurring_reminder` and the ambiguous-person-reference path. A
   `correction`- or `recall`-classified capture today reaches `classify.ToUnit`, which returns
   `ErrNoUnitType` for both (verified: `classify.Kind.UnitType()` maps `correction` and `recall`
   to no `unit.Type`), and that error propagates as a plain Go error out of `Capture` — neither
   Kind has a routing fork yet. This spec is the one that adds both forks.
2. **Doc 02 already carries Q3c's answer.** `docs/02-cognitive-core.md` §5 step 4 (the margin as a
   ratio, ADR-0016's pre-image sentence) and §13 (`correction_referent_margin` = 1.5) already hold
   the prose PR 12 implements, landed by dedicated docs commits (`877f647`, `522abd8`) ahead of any
   Phase C code. This is unlike every `m1b-pipeline` core PR, each of which carried its own doc 02
   delta in the same PR. §1 below states what this means for PR 12's `docs-sync` obligation, and
   `## Conflicts` states the tension it creates with proposal §4.8's blanket claim.

---

## 0. Scope boundary

> Phase C is the surface: in-place corrections with referent resolution (I03, I13), the HTTP
> routes for capture/recall/read-only units, and the CLI capture command the demo runs through.

**Honour, do not reopen, the four CLOSED decisions this change depends on** (umbrella proposal
§8, owner decisions 2026-08-02 unless noted):

- **Q3b** (2026-08-02): `/recall` is standalone — it embeds the query text and runs both legs, no
  classify call on the read path. `internal/brain/recall.go`'s `RecallService.Candidates` already
  implements the mechanism; this change is the first to expose it as an HTTP entrance and as
  capture's own `type: recall` routing.
- **Q3c-i** (2026-08-02): which unit a correction edits is resolved by an explicit `unit_id`
  override when the caller has one, and otherwise by hybrid recall against the correction text
  gated on the **ratio** of the top two fused scores (`correction_referent_margin`, default 1.5).
- **Q3c-ii** (2026-08-02, [ADR-0016](../../../docs/adr/0016-correction-pre-image.md)): the values a
  correction is about to overwrite are written to `decision_log.context` before the overwrite; a
  failed audit write blocks the edit.
- **Q3c-iii** (2026-08-02): `ports.UnitRepo` gains one update method per correctable field
  (`UpdateEventAt`, `UpdateDueAt`, alongside the existing `UpdateContent`) — never a single
  `UpdateFields(patch)`.
- **Q4** (2026-08-02): classify's corpus already covers the deferred-hook taxonomy values
  (`timer`, `recurring_reminder`) and `chitchat`/`out_of_scope` — already satisfied by
  `m1b-pipeline`'s PR 7; this change adds no new classify corpus obligation.

**Every requirement below is bounded by the umbrella proposal's Phase C PR table (§5), as PR #101
and PR #102 widened it to six rows**: PR 12 (`feat/corrections`), PR 13
(`feat/httpapi-capture-recall`), PR 14 (`feat/cli-capture-demo`), PR 15 (`feat/init-provider-paths`),
PR 16 (`feat/doctor-quality-gate`), PR 17 (`feat/openai-embeddings`).

**PR 17 exists for the same reason PRs 15 and 16 do: a degradation absorbed a gap instead of
surfacing it.** `docs/06-harness.md`'s own D8 degradation design lets a capture succeed with no
embedding when the embedder is down — correct for an outage. `internal/providers/` holds three chat
clients and exactly one embedder, `ollama/embed.go`, so a Cloud-configured vault (PR 15) had nothing
to embed with, and D8's degradation absorbed that silently: every capture stored, every unit came
back with no vector, every recall ran on its lexical leg alone — a demo that works and is visibly
worse than the product it demonstrates. §6 below specifies PR 17's client and, per its own §6.3, a
requirement that makes this exact gap detectable if it ever recurs, rather than leaving it to a user
wondering why search feels thin.

> **Scope note, added after `13d` shipped.** The narrative above — *"every capture stored, every
> unit came back with no vector, every recall ran on its lexical leg alone"* — was accurate of the
> codebase at the time it was written, and remains accurate for the **outage** case: a bound
> embedding provider that fails at runtime still degrades exactly that way (`m1b` D8, and D18's
> *fit* question, which §6.1's client and §5's quality gate address). It no longer describes the
> **never-bound** case. `13d`'s fail-closed `wireBrain` resolves every member of `tasksM1Consumes`
> or none, so a vault with no embedder bound captures nothing at all: `POST /capture` and `POST
> /recall` both answer `503`. That state is D18's *configured* question, which §6.3's `doctor` row
> answers — see `tasks.md`'s Conflicts §C21.1 and `cmd/nooma/doctor.go`'s
> `taskCoverageConsequence` for the shipped wording.

**PR 15 and PR 16 did not exist when this spec's first revision was written.** That revision found
proposal §3.2 items 14 and 15 (`nooma init`'s Cloud/Ollama paths, `nooma doctor`'s structured-JSON
quality gate) asserted in prose to belong to Phase C while naming no PR in the then-three-row Phase
C table, and recorded the gap as a conflict rather than silently absorbing the items into PR 14 or
dropping them (`## Conflicts`, Conflict 1). The owner resolved it by scheduling the items as PRs 15
and 16, not by excluding them — the opposite of the exclusion this spec's own §6/§8 boundary
requirements previously assumed. §4 and §5 below now specify PR 15 and PR 16 in full; `## Conflicts`
Conflict 1 is updated with the resolution rather than rewritten, per this project's own recorded
practice of naming a decision rather than erasing the disagreement that preceded it.

This change does not implement anything from `m1-capture-recall/proposal.md` §3.3's explicit
non-goals (`effective_weight`, consolidation, triggers, timers actually firing, self-model
derivation, the learning pass *consuming* a signal, Telegram, `reindex`, perception), and does not
build an undo surface for a correction — ADR-0016 names that gap explicitly and this spec follows
it (§6 below).

---

## 1. Corrections (PR 12 — `feat/corrections`)

Traced to `docs/02-cognitive-core.md` §4 (the duplicate/correction distinction), §5 step 4
(corrections), §9 (I13), §13 (`correction_referent_margin`), and
[ADR-0016](../../../docs/adr/0016-correction-pre-image.md).

### R1.1 — Capture routes a `correction` classification away from `classify.ToUnit`

**MUST**: `internal/brain`'s capture orchestration, on a classification whose `Kind` is
`classify.KindCorrection`, does not call `classify.ToUnit` — verified: `ToUnit` returns
`ErrNoUnitType` for this Kind today, since `Kind.UnitType()` maps `correction` to no `unit.Type`.
It forks to the correction path (R1.5/R1.6 below) before `ToUnit` is ever reached, mirroring the
routing pattern `m1b-pipeline` R4.6 already established for the timer/recurring_reminder refusal.

**MUST NOT**: a `correction` classification ever call `ports.UnitRepo.Create` — a correction edits
an existing unit, doc 02 §5 step 4's own "in place"; it never persists a new one.

**Verified by**: L2 — a conformance test driving a correction-classified capture through
`fakeprovider`/`memrepo`, asserting `Create` is never called for it.

### R1.2 — `internal/core/recall` exposes a fusion that keeps its scores, additive to `Fuse`

**MUST**: `internal/core/recall` exposes a pure fusion function returning the fused candidates
together with their RRF scores, not only their ranked ids — doc 02 §5 step 4, verbatim: "it
therefore needs `internal/core/recall` to expose a fusion that keeps its scores instead of only
its ranked identifiers." Verified: `Fuse` (`fuse.go:55`) computes `scores` into a local map and
discards it, returning `[]string`.

**MUST**: this is an addition to `core/recall`'s surface — `Fuse`'s exported signature and
behaviour (its returned ordering, including its three-level tie-break) are unchanged, and it stays
the mechanism `internal/brain/recall.go`'s `RecallService.Candidates` already calls; ADR-0010's
surface grows, per proposal §8 Q3c-i's own framing.

**MUST NOT** be read as forbidding `Fuse`'s own implementation from changing internally — a prior
revision of this requirement said "`Fuse` itself is unchanged," worded strictly enough to forbid
reimplementing `Fuse` as a projection over the scored fusion this requirement adds, which this spec
does not intend to forbid: two independent implementations of the same `Σ w_i/(k + rank_i)` formula
are two places for ADR-0010's own named bias to live, and a single implementation with `Fuse` as a
thin projection closes that risk. What this requirement actually constrains is the **exported
contract** — signature, ordering, tie-break — never the function body.

**MUST NOT**: this function perform any I/O, read a clock, or call an LLM — the same purity
`Fuse` and every other `core/recall` function already holds.

**Verified by**: L1 — a table test asserting the returned per-id scores equal `Fuse`'s own RRF
formula (`score(d) = Σ w_i/(RRFK + rank_i(d))`) for the same input lists, and that the returned
ordering matches `Fuse`'s.

### R1.3 — The correction referent margin gate is a pure function over the top two scored candidates, gated by a ratio

**MUST**: a pure function in `internal/core` (the exact package — `core/recall` or a new
`core/correction` — is design's choice; the decision-gate table proposal §4.1 fixed for Phase B's
packages predates Q3c and names none for this decision) takes R1.2's scored candidates and
returns either **pick** (naming the top candidate) or **ask** (no referent chosen), as follows —
doc 02 §5 step 4:

- Zero scored candidates → **ask**.
- Exactly one scored candidate → **pick**, naming it — there is no second score for it to be
  "closer together" than.
- Two or more scored candidates → compute the ratio of the highest score to the second-highest
  (only the top two participate; a third or later candidate never affects the decision). A ratio
  **strictly less than** `correction_referent_margin` (default 1.5) returns **ask** — doc 02's own
  wording, "closer together than," is a strict inequality. A ratio **greater than or equal to**
  the margin returns **pick**, naming the top candidate.

**MUST NOT**: the gate use the absolute difference between the top two scores — doc 02 §5 step 4
explicitly rejects this: RRF's compression at `k = 60` makes an absolute gap ambiguous depending on
how many legs a candidate appeared on, while a ratio does not.

**MUST NOT**: the gate perform any I/O, call an LLM, or read a clock — doc 02: "The gate is a pure
function of the scored candidates: no LLM, no I/O, no clock."

**Verified by**: L1 — a table test covering: zero candidates → ask; one candidate → pick; a ratio
above the margin → pick; a ratio below the margin → ask; a ratio exactly equal to the margin →
pick (the boundary, pinned inclusive on the pick side per doc 02's "closer together than" being a
strict inequality); a three-or-more-candidate case where the third candidate's score would flip
the answer if it were allowed to participate, asserting it does not.

**Scenario**:
- GIVEN two scored candidates whose fused scores have a ratio of exactly 1.5
  (`correction_referent_margin`'s default)
- WHEN the gate evaluates them
- THEN it returns pick, naming the higher-scoring candidate — the ratio is not "closer together
  than" the margin, it equals it

### R1.4 — `correction_referent_margin` is a single named constant matching doc 02 §13

**MUST**: `correction_referent_margin` (default 1.5) is a single named constant in exactly one
place, not a literal repeated at call sites — `docs/06-harness.md` §7's calibratable-number rule.
Doc 02 §13 already carries this row (verified above); this PR's obligation is that the Go constant
equals it.

**Verified by**: L1 — a test asserting the constant's value is 1.5 and is referenced (not
re-literaled) by R1.3's gate; review confirming it matches doc 02 §13's row.

### R1.5 — An explicit `unit_id` override wins wherever the caller has one

**MUST**: capture's input carries an optional explicit target-unit identifier (the exact
mechanism — a new `CaptureInput` field or an equivalent — is design's choice); when a `correction`
classification arrives with this identifier set, capture uses it directly as the referent and does
**not** run R1.2/R1.3's recall-and-gate at all — Q3c-i's closed decision, verbatim: "An explicit
`unit_id` wins wherever the caller has one."

**MUST**: when the explicit identifier names no existing unit (`ports.UnitRepo.ByID` returns
`ErrUnitNotFound`), the correction fails with an error rather than silently falling back to
recall-based resolution — a silent fallback would defeat the caller's explicit intent, which is
the entire reason the override exists.

**Verified by**: L2 — a conformance test driving a correction capture carrying an explicit target
unit id, asserting the referent unit is updated directly with no recall call made (an instrumented
fake `RecallService`/index that fails the test if queried); a second test asserting an unknown
explicit id returns an error and edits nothing.

### R1.6 — Chat-path referent resolution runs hybrid recall against the correction text; "ask" edits nothing

**MUST**: when no explicit identifier is given (R1.5), capture runs hybrid recall — the same
vector/lexical legs `RecallService.Candidates` already runs — against the correction's classified
text, fused by R1.2's scored fusion, gated by R1.3.

**MUST**: when the gate returns **ask**, capture edits no unit and creates no unit; it returns a
result distinguishable from every other capture outcome this change defines (an ordinary stored
capture, the timer/recurring_reminder refusal, a discarded chitchat/out-of-scope classification, a
recall answer, and a resolved correction) — carrying enough information for a caller to ask the
user which unit was meant. This spec does not mandate the exact response shape or type; R1.8 below
reuses this same distinguishable-ask outcome for its own ambiguous-field case.

**MUST**: when the gate returns **pick**, capture proceeds to R1.8/R1.9 against the named unit.

**MUST NOT**: a correction, resolved or not, ever create a new unit — doc 02 §5 step 4 has no
branch that persists one.

**Verified by**: L2 — two conformance tests: one where a single strong recall match resolves and
edits the target unit; one where ambiguous recall results (ratio below the margin) leave every
unit untouched and return the ask-shaped result.

**Scenario**:
- GIVEN a vault holding two units about different dentist appointments, whose recall scores
  against the correction text "no, it's the 15th, not the 14th" fall within
  `correction_referent_margin` of each other
- WHEN the correction is captured with no explicit unit id
- THEN neither unit is edited, no new unit is created, and the caller receives a result naming
  the ambiguity rather than a silent guess

### R1.7 — Q3c-iii: one update method per correctable field on `ports.UnitRepo`

**MUST**: `ports.UnitRepo` gains `UpdateEventAt` and `UpdateDueAt`, alongside the existing
`UpdateContent` — Q3c-iii's closed decision, verbatim: "not one `UpdateFields(patch)`... no
signature is capable of writing the wrong date." Each new method follows `UpdateContent`'s own
contract shape: it rewrites exactly the one field it names plus `UpdatedAt`, leaves every other
column unchanged, and returns `ErrUnitNotFound` if no unit with the given id exists.

**MUST NOT**: `ports.UnitRepo` gain any combined patch-shaped update method (a struct or map of
optional fields) for corrections — this is Q3c-iii's structural core: "no argument that could mean
something else."

**Verified by**: L2 — a contract test (extending the existing `unitrepo_memrepo_test.go` shape)
asserting each new method updates only its one named column and leaves the rest of the unit
unchanged; review of `ports.UnitRepo`'s declared method set confirming no combined-patch method
exists.

### R1.8 — A correction writes exactly ONE field: dates win, content is the no-date fallback, two dates is an ask

**Overruled and rewritten.** This requirement's prior revision mandated a write for each of
`{content, event date, due date}` present in the classification. Because `normalized_content` is
present in nearly every classification (doc 02 §5.1 names its absence one of only two unsurvivable
degradations), that rule meant **every** correction overwrote the referent's body with the
correction utterance itself: a unit reading "Meeting with Anna on Tuesday" became "It's Ana, not
Anna" — the thing the unit was there to remember was gone. Owner decision, recorded here rather
than silently applied (`## Conflicts`, Conflict 3).

**MUST**: capture computes an edit plan for the correction's classification and applies **at most
one** field write to the referent unit, chosen by this rule:

| The classification resolved | The correction writes |
|---|---|
| `event_at`, and not `due_at` | `event_at` only |
| `due_at`, and not `event_at` | `due_at` only |
| neither date, and `normalized_content` survived | `content` only |
| **both** `event_at` and `due_at` | nothing — ask |
| neither date and no content | nothing — ask |

Dates win over content whenever either date is present — writing `event_at` from
`Classification.EventAt` requires no inference (the field means the same thing on both sides of the
pipeline), while writing `content` from `NormalizedContent` requires inferring that the model's
normalization of the correction *utterance* is the referent's new *body*, which this spec licenses
only when there is nothing else to write.

**MUST**: when the plan resolves to nothing — both dates present, or neither a date nor content
survived — capture edits no unit and returns the same ask-shaped result R1.6 already establishes
for an ambiguous referent: the correction asks rather than guesses which field to change, the same
way it asks rather than guesses which unit.

**MUST NOT**: `content` be written on any correction that also resolved a date, even though
`normalized_content` may also have survived decoding alongside it — this is the rule's entire
point.

**Accepted cost, stated rather than hidden**: a correction that moves a date leaves the referent's
body stale — the unit's content still reads whatever it read before, while its `event_at` or
`due_at` now carries the corrected value. Inconsistent but useful beats consistent but empty: a
stale body is still findable and still says approximately the right thing, while an unconditionally
overwritten one loses content the correction never touched. `ADR-0016`'s pre-image preserves the
previous value of the one field this PR's plan actually changes either way, so the staleness is at
minimum visible in the audit trail even where a future correction has not yet fixed it in the unit
itself.

**MUST NOT**: the correction path call `SetStatus`, or any `Delete`/`Remove`/`Purge`/`Drop`/
`Destroy`-prefixed method, on the referent unit — a correction edits content and dates, doc 02 §5
step 4's "in place," never a state transition (I03's own structural check, restated as the
property this PR's orchestration must uphold at the call-site level, per `m1b-pipeline`'s own R4.1
precedent).

**Verified by**: L1 — a table test over the edit-planning decision, covering all five rows above,
including the two-dates-present and no-date-no-content ask cases; L2 — a conformance test asserting
a date-only correction leaves the referent's content byte-for-byte unchanged, a content-only
correction (no date resolved) leaves both dates unchanged, and at most one `Update*` call reaches
`ports.UnitRepo` per correction (a fake-repo-instrumented test failing if a second `Update*` call
occurs).

**Scenario**:
- GIVEN a unit whose content reads "Meeting with Anna on Tuesday" and a correction classified with
  `normalized_content = "It's Ana, not Anna"` and no date fields resolved
- WHEN the correction applies
- THEN the unit's content is overwritten to "It's Ana, not Anna" — dates are absent, so content is
  the fallback; the accepted cost this requirement states, not a hidden defect

- GIVEN the same unit and a correction classified with `event_at` resolved to the 15th and
  `normalized_content` also present
- WHEN the correction applies
- THEN only `event_at` changes; the unit's content still reads "Meeting with Anna on Tuesday" —
  dates win, and the body goes stale rather than being silently overwritten

- GIVEN a correction whose classification resolves both `event_at` and `due_at`
- WHEN the correction is evaluated
- THEN no field is written, and the caller receives the same ask-shaped result an ambiguous
  referent produces

### R1.9 — The ADR-0016 pre-image is written before any overwrite, in the same `decision_log` row

**MUST**: before any `Update*` call runs, capture writes one `decision_log` row (I12) whose
`context` carries at minimum: the target unit's id, the previous value of every field about to
change, and the confidence that selected the referent (or an explicit-override marker, when R1.5's
path was taken) — ADR-0016's decision, verbatim.

**MUST**: this pre-image row is written **first**; if writing it fails, no `Update*` call runs —
ADR-0016: "The row is written first. If it fails, the `UPDATE` does not happen," the same ordering
`capture.embedding.failed` and `capture.dedup.failed` already use in `m1b-pipeline`, where an
audit-write failure propagates rather than being swallowed.

**MUST NOT**: any field on the referent unit change if the pre-image write failed.

**Verified by**: L2 — a conformance test asserting the `decision_log` write happens before any
repository `Update*` call (an instrumented fake ordering check); a second test with a `DecisionLog`
fake configured to fail `Record`, asserting the target unit's content and dates are byte-for-byte
unchanged afterward.

**Scenario**:
- GIVEN a correction resolved to a target unit, and a `DecisionLog.Record` call configured to fail
- WHEN capture attempts the correction
- THEN no `Update*` call reaches `ports.UnitRepo`, and the target unit's stored content and dates
  are unchanged

### R1.10 — The `correction` learning signal (I13) is emitted after a successful correction

**MUST**: after R1.9's pre-image write and every `Update*` call from R1.8 have both succeeded,
capture writes a `learning_signals` row via a new port (`ports.SignalRepo` or equivalent — this
spec does not mandate the exact method signature, following `m1b-pipeline`'s own restraint for
`DecisionLog`/`RelationRepo` in its R4.5/R5.3) with `signal_type = "correction"`,
`target_kind = "unit"`, `target_id` = the referent unit's id — I13's own wording: "the first *write*
into `learning_signals`."

**MUST**: this is the first PR in the whole `m1-capture-recall` umbrella that writes to
`learning_signals` at all — `m1b-pipeline`'s own R8.1 explicitly deferred every write to this
table to corrections.

**MUST NOT**: this signal row require the target unit to exist at write time, or be joined against
`units` in a way that would defeat `learning_signals.target_id`'s deliberate absence of a foreign
key — already structural in the schema (migration 0002, verified: "NO FK: the signal outlives the
target's deletion"); this PR's obligation is not to defeat that structural fact with application
logic that behaves as if the FK existed.

**Verified by**: L2 — a conformance test asserting a successful correction leaves exactly one
`learning_signals`-shaped row (via the fake `SignalRepo`) naming `signal_type = "correction"` and
the referent's id; the existing `i13_learning_signal_test.go` DDL check is unaffected — this PR is
the first to exercise the write path that test's schema fact protects, not a change to the schema
itself.

### R1.11 — I03 still holds: a correction never deletes

**MUST**: no code path this PR adds calls `DELETE FROM units`, or any `UnitRepo` method whose name
begins `Delete`/`Remove`/`Purge`/`Drop`/`Destroy` — already enforced tree-wide by the existing
`test/conformance/i03_units_never_deleted_test.go`, restated here as the property this PR's new
code must not violate.

**Verified by**: `i03_units_never_deleted_test.go`, unchanged, passing against this PR's new files.

### R1.12 — `internal/store/sqlite` implements the new port surface; `store_api.golden` is regenerated

**MUST**: `internal/store/sqlite` provides SQLite implementations of `UpdateEventAt`,
`UpdateDueAt` (R1.7) and the new `SignalRepo` port (R1.10) — a correction must work against a real
vault for the demo (umbrella proposal §2's own success criterion: "a correction edits its
referenced unit in place"), not only against `memrepo`.

**MUST**: `testdata/schema/store_api.golden` is regenerated (`make store-api-golden`) to include
this PR's new exported surface — the same obligation `m1a-substrate` R4.4 and `m1b-pipeline` R3.5
already established.

**MUST NOT**: this PR add a new migration, or modify `0001_core_tables.sql` or
`0002_learning_and_search.sql` — `learning_signals` and its no-FK `target_id` column already exist
(migration 0002, verified above); this PR is repository methods against existing tables, not
schema.

**Verified by**: L3 — a test running a correction's `Update*` calls and `SignalRepo` write against
a real temporary SQLite vault and reading the rows back; `TestHarness_StoreAPIUnchanged` against
the regenerated golden; `git diff` over `internal/store/sqlite/migrations/` is empty for this PR.

### R1.13 — Doc 02 already carries this PR's delta; the `docs-sync` exception

**MUST NOT**: this PR be required to add new prose to `docs/02-cognitive-core.md` §4, §5 step 4,
or §13 — verified above by direct reading: that text (the margin-as-ratio rule, ADR-0016's
pre-image sentence, the `correction_referent_margin` row) already exists in the committed doc 02,
landed by dedicated docs commits (`877f647`, `522abd8`) ahead of this PR's code, unlike every
`m1b-pipeline` core PR, each of which carried its own doc 02 delta in the same PR.

**MUST**: this PR still touches `docs/02-cognitive-core.md` if implementation surfaces a wording
gap the already-merged prose left open — ADR-0016 itself names one: "this ADR does not fix its
exact keys... doc 02 §5 is where the settled shape belongs" (the pre-image's `context` JSON shape).
If no such gap surfaces, this PR may carry `no-spec-change` truthfully — the one core-touching PR
in this milestone licensed to do so, and only because its delta already landed elsewhere. See
`## Conflicts` for the tension this creates with proposal §4.8's blanket claim.

**Verified by**: `docs-sync.yml` on GitHub; review of whether the pre-image's `context` shape
needed a wording update.

### R1.14 — Core purity and coverage hold for the new `core/recall`/`core/correction` surface

**MUST**: every file this PR adds under `internal/core/**` imports only the standard library and
its own package, calls none of `time.Now`, `time.Since`, `time.Until`, `rand.*`, `uuid.*`,
`os.Getenv`, and holds ≥ 90% statement coverage.

**Verified by**: `golangci-lint run`; `scripts/core-coverage.sh`.

---

## 2. HTTP surface (PR 13 — `feat/httpapi-capture-recall`)

Traced to `docs/02-cognitive-core.md` §1 (I02), §5 (capture), §11 (I12), and proposal §8 Q3b.

### R2.1 — A capture route wires the existing `CaptureService`, and every distinct outcome maps to a distinguishable response

**Rewritten for contract, not shape.** This requirement's prior revision enumerated
`CaptureResult`'s fields as they exist today (`Stored`, `UnitID`, `Embedded`, `Candidates`,
`Deferred`) — describing the current struct rather than mandating a behaviour, and by Phase C's own
close this struct grows past a single boolean-plus-pointer shape (R1.6's ask outcome, R2.3's recall
outcome, chitchat/out-of-scope's discard outcome). Restated as a contract:

**MUST**: `internal/httpapi` exposes a route (e.g. `POST /capture`) that accepts a message body
(text, an optional channel, and an optional explicit target-unit identifier per R1.5) and calls
`brain.CaptureService.Capture` unchanged.

**MUST**: the route maps every distinct outcome the capture pipeline can produce — at minimum: a
unit stored, a timer/recurring-reminder refusal, a chitchat/out-of-scope classification discarded,
a recall answered, a correction applied, and a correction left ambiguous (asking) — to a response a
caller can programmatically tell apart from every other outcome. This spec does not mandate the
Go type, field names, or HTTP status codes that implement this — only that the mapping is total
(every outcome capture can produce has a response) and that no two distinct outcomes are
indistinguishable at this boundary.

**MUST**: the route's request/response handling carries no business decision of its own — decoding
the body and encoding the result is `internal/httpapi`'s only job here; every decision (classify,
persist, embed, recall, judge, correct) already lives in `brain`/`core`, per `docs/06-harness.md`
§1's dependency rule restated for this new adapter.

**Verified by**: L2 — a handler test (using `memrepo`/`fakeprovider`) driving one ordinary capture
through the route and asserting the response reflects a successfully-stored outcome; a completeness
test iterating every outcome the capture pipeline can produce and asserting the route's mapping
covers each one, failing loudly if a new outcome is ever added with no mapping; L4 — the compiled
binary answering a real HTTP capture request during `nooma serve`.

### R2.2 — The capture route surfaces Q3a's refusal in plain words, never a silent success

**Rewritten for contract, not shape**, for the same reason as R2.1: this requirement's prior
revision named `Stored: false` and `Deferred.Message` as the mechanism, describing today's Go
shape rather than the behaviour it protects.

**MUST**: when a capture is refused rather than stored (the timer/recurring_reminder refusal, Q3a,
`m1b-pipeline` R4.6), the route's response is distinguishable from every other outcome's response
(R2.1) and carries the refusal's plain-words message verbatim — Q3a's own wording, "tells the
caller 'not yet' in plain words" — never merely an HTTP status code standing in for the message.

**Verified by**: L2 — a route test driving a timer-classified message and asserting the response's
refusal-shaped field is set and its message text matches the refusal's plain-words message
verbatim.

### R2.3 — Capture's `type: recall` classification routes to the standalone recall mechanism instead of failing

**MUST**: `internal/brain`'s capture orchestration, on a classification whose `Kind` is
`classify.KindRecall`, does not attempt `classify.ToUnit` (which today returns `ErrNoUnitType` for
this Kind, per R1.1's own verification for `correction`) — it instead runs the same hybrid-recall
mechanism R2.4 exposes over the classification's text/embedding and returns those results as the
capture's own result, mirroring the routing pattern `m1b-pipeline`'s R4.6 established for the
timer refusal (a `Kind`-based fork inside capture's orchestration, before `ToUnit` is ever called).

**MUST NOT**: a `type: recall` capture ever persist a unit — Q3b's own framing: recall is an
entrance to the existing mechanism, never a write.

**Verified by**: L2 — a conformance test driving a recall-classified capture through
`fakeprovider`/`memrepo`, asserting no `ports.UnitRepo.Create` call occurs and the returned result
carries the same fused candidate ordering the standalone route (R2.4) produces for identical text.

### R2.4 — A standalone recall route exists, with no classify call on the read path

**MUST**: `internal/httpapi` exposes a route (e.g. `GET /recall`) that accepts a query string,
embeds it via `ports.EmbeddingProvider`, and calls the same `brain.RecallService.Candidates`
capture already uses (`internal/brain/recall.go`) — Q3b's closed decision, verbatim: "`/recall`
takes a query string, embeds it, runs both legs, fuses, returns units... no classify call on the
read path."

**MUST NOT**: this route call `ports.LLMProvider.Complete` with `capture_processing`, or any
`classify.*` function — a query is embedded and searched, never classified.

**Verified by**: L2 — a route test asserting no LLM completion call occurs (a `fakeprovider`
configured with zero scripted `capture_processing` cases still succeeds); L4 — a real HTTP recall
over a compiled binary.

### R2.5 — Capture-`recall` and the standalone `/recall` route return the same answer for the same text (Q3b's conformance-shaped property)

**MUST**: for the same input text (and therefore the same embedding, under the same deterministic
fixture provider), the fused candidate ordering R2.3's capture-time recall path returns is
identical to the fused candidate ordering R2.4's standalone route returns. Both paths are the same
call into `brain.RecallService.Candidates` over the same `(content, vector, model)` inputs, so this
is a property of routing consistency, not two mechanisms — proposal §8 Q3b's own text: "capture-
`recall` and `/recall` must return the same answer for the same text. That is a conformance-shaped
property worth its own test," named there as unbuilt because `/recall` itself is Phase C's to
build.

**Verified by**: L2 — a conformance test seeding `memrepo`/`fakeprovider` with the same corpus,
driving one capture classified `recall` and one standalone `/recall` call over identical text, and
asserting the two ordered candidate-id lists are equal.

**Scenario**:
- GIVEN a vault holding several existing units and the query text "what do you know about the
  dentist"
- WHEN that text is submitted once through capture (classified as `recall`) and once through the
  standalone `/recall` route
- THEN both return the identical fused ordering of live candidate units

### R2.6 — Read-only unit routes filter positively on `status = pool` (I02)

**MUST**: `internal/httpapi` exposes read-only routes over units (e.g. `GET /units/{id}`, and a
list-shaped route) that resolve through `ports.UnitRepo.LiveByIDs` or an equivalent positively-
filtered read — never `ports.UnitRepo.ByID` exposed directly over HTTP, since `ByID` is the
deliberate any-status escape hatch `internal/ports/unitrepo.go`'s own doc comment reserves for
corrections and audit (R1.5's own use of it), not a public read surface.

**MUST**: a request naming a unit whose status is not `pool` (`archived`, `superseded`,
`incomplete`) returns the same "not found" response an unknown id would produce — I02's
positive-filter framing applied at the HTTP boundary: a live read surface must not leak the
existence of a non-live unit through its error shape either.

**Verified by**: L2 — a route test seeding one `pool` unit and one `archived` unit, asserting only
the `pool` unit's route responds successfully and the archived unit's by-id request returns the
same not-found shape an unknown id would.

**Scenario**:
- GIVEN a vault holding one `pool` unit and one `archived` unit
- WHEN the read-only unit route is queried for the archived unit's id
- THEN the response is the same "not found" shape a nonexistent id would produce

### R2.7 — GET routes write no `decision_log` row

**MUST**: no route this PR adds writes to `decision_log` — recall and the read-only unit routes
are reads, and doc 02 §4's own reasoning ("a judgment that decided nothing writes nothing... the
same reason a read writes no row") applies at the HTTP boundary the same way `m1b-pipeline`'s own
`recall_writes_no_decision_test.go` already proves for `RecallService` at the brain layer.

**Verified by**: L2 — a route test asserting `DecisionLog.Record` is never called for a `GET
/recall` or `GET /units/{id}` request (an instrumented fake that fails the test if `Record` is
invoked).

### R2.8 — L4 walks capture and recall through the compiled binary

**MUST**: `test/e2e` gains at least one test that starts the compiled `nooma serve` binary against
a real, migrated, fixture-configured vault, posts a capture, and issues a recall that finds it —
the umbrella proposal's own success-criteria demo shape (§2: "capture through the API and through
the CLI, then ask 'what do you know about X?' and get a real recall"), proven once at the binary
level per `docs/06-harness.md` §3's L4 definition.

**Verified by**: `go test ./test/e2e/... -tags e2e`.

### R2.9 — Request-time auth (ADR-0017) lands in the same PR that mounts `POST /capture`, so no commit ever mounts it unprotected

**Owner decision on the design's C4, added to this spec.** ADR-0007 requires
`server.auth_token_env` for a non-loopback bind and the server refuses to listen without it
(`internal/httpapi/binding.go`'s `DecideBinding`) — but nothing in the tree validates that token
per request: `internal/httpapi/server.go`'s `Handler` mounts its routes with no middleware at all.
That was tolerable while the only routes were `GET /` and a UI placeholder — ADR-0007's own header
says so, "Enables: M4 (tolerable unresolved through M0–M3)" — and stops being tolerable the moment
this PR mounts `POST /capture`, which writes the user's memory.

**MUST**: this PR authors a new ADR (ADR-0017) recording the decision that every API route
requires a bearer token whenever one is configured, and implements the per-request middleware that
enforces it, in the same PR that mounts `POST /capture` (R2.1) — so that no commit in this
change's history ever has a write route mounted without the check already in place.

**MUST NOT**: ADR-0007 be edited — it is `Accepted`. ADR-0017 discharges the request-time half
ADR-0007's own header names as deferred; it does not supersede or contradict what ADR-0007 decided
about binding.

**Verified by**: `docs/adr/0017-http-request-auth.md` exists and `docs/adr/README.md`'s index
carries its row; `git diff --name-only` for this PR shows the new ADR and the middleware landing
together, never the middleware in a later PR.

### R2.10 — The middleware is a no-op reachable only on a loopback bind

**MUST**: when no token is configured (`server.auth_token_env` unset, or the environment variable
it names unset), the middleware performs no check and every request passes through unmodified —
the ordinary loopback-development case, where there is no secret to present.

**MUST**: this no-op state is reachable only when the effective bind is loopback. `DecideBinding`
already refuses to return a listen address for a non-loopback bind with no `auth_token_env`
configured or an unset variable (`binding.go`) — the middleware's own no-op condition and the
bind-time refusal condition must read the same fact, so bind-time and request-time can never
disagree about whether a token exists.

**MUST NOT**: the middleware and `DecideBinding` derive "is a token configured" from two different
reads of configuration or environment — one read, shared, is what keeps the two decisions from
drifting apart.

**Verified by**: L2 — a test sweeping the same bind/token truth table `binding_test.go` already
exercises, asserting that for every combination where the server would actually start, the
middleware is a no-op only when the effective bind is loopback.

### R2.11 — When a token is configured, every API route requires it, compared without leaking a timing oracle

**MUST**: every route mounted under the API surface — present in this PR and any later PR — requires
`Authorization: Bearer <token>` whenever a token is configured, compared using a constant-time
comparison (e.g. `crypto/subtle.ConstantTimeCompare`), never a plain string equality that could leak
timing information about a partial match.

**MUST**: the set of routes this requirement covers is declared once and consumed both by route
registration and by this requirement's own completeness test — a new API route added in a later PR
is guarded by construction, not by a developer remembering to add middleware to it, the same "one
slice, two consumers" shape this milestone's own completeness tests already use elsewhere (e.g.
`m1b-pipeline`'s outcome-vocabulary completeness tests).

**MUST NOT**: a missing token and an incorrect token produce distinguishable responses — an error
message (or a status code) that tells a caller "you sent no token" apart from "you sent the wrong
token" is an oracle an attacker can use to probe for the token's existence independently of its
value.

**Verified by**: L2 — a completeness test iterating the declared route set, asserting every entry
returns an unauthorized response with no token and with a wrong token, and that both responses are
identical; review confirming the comparison is constant-time.

### R2.12 — ADR-0017's own scope is the API's bearer-token header only; the UI stays open in M1

**MUST NOT**: this PR implement ADR-0007's cookie-handshake UI authentication — that authentication
belongs to the server-rendered UI, which ADR-0007 itself ties to M4 (ADR-0008). `GET /` and `GET
/ui` stay open in M1 exactly as M0 left them; this PR's middleware guards the API surface only.

**Verified by**: review — no cookie-setting or session code path exists in this PR; `GET /` and
`GET /ui` remain reachable without a token regardless of whether one is configured.

---

## 3. CLI surface (PR 14 — `feat/cli-capture-demo`)

Traced to proposal §3.2 item 11 (`cmd/nooma` gains a `capture` subcommand) and §2's demo criterion.

**PR 14 is last in time despite its number** (proposal §5's dependency list, as PR #101 and PR #102
restate it: `(13,15,16,17) → 14`) — it is the demo, and the demo is M1's exit criterion, so PR 13's
auth (R2.9–R2.12), PR 15's Cloud path (§4 below), PR 16's quality gate (§5 below), and PR 17's
embedding client (§6 below) all exist before PR 14 walks through them. This section's own
requirements are unchanged by that reordering; only their place in the chain moved. R3.1 above adds
one more precondition beyond dependency order: PR 14's own demo requires a *running* `nooma serve`
process, since `nooma capture` is now an HTTP client of it.

### R3.1 — `nooma capture` is an HTTP client of the running server, never a second direct-vault writer

**Overruled and rewritten.** This requirement's prior revision had `nooma capture` open the vault
directly, citing `status`/`doctor`'s precedent for CLI commands that work without a running server.
**That precedent does not transfer.** `cmd/nooma/status.go`'s `runStatus` says, in its own words,
"It is read-only in the strong sense: it never takes the write lock," and calls
`vaultlock.ReadHolder` — it reads who holds the lock, it never takes it. `status`/`doctor` are
readers; `capture` is a writer, and `nooma serve` holds the vault's exclusive write lock for its
entire lifetime. A `nooma capture` that opened the vault directly would fail every time the product
was doing the one thing it is meant to do most of the time: running. Owner decision, recorded here
rather than silently applied (`## Conflicts`, Conflict 4).

**MUST**: `cmd/nooma` gains a `capture` subcommand, added to `main.go`'s `commands` map following
`init`/`status`/`doctor`/`serve`'s own dispatch-table convention, that sends `POST /capture` (R2.1)
against a running `nooma serve` instance — reading the vault's `nooma.yml` only to resolve the
bind address, which takes no lock — and prints a human-readable summary of the response.

**MUST**: when no server is reachable at the resolved address, `nooma capture` fails with a message
that says so in plain words — never a silent hang, and never a fallback to opening the vault
directly.

**MUST**: when the resolved bind is not loopback, `nooma capture` reads the token from the same
`server.auth_token_env`-named environment variable the server itself reads (R2.10/R2.11) and sends
it as the request's `Authorization: Bearer` header — the same credential, read the same way, never
a second source of truth for whether a token exists.

**MUST NOT**: `nooma capture` open the vault's database directly, or take (or attempt to take) the
vault's write lock — that is `nooma serve`'s exclusive resource, and two writers to one SQLite vault
is exactly what the lock exists to prevent.

**Verified by**: L2 — a test driving the subcommand against an `httptest`-backed fake server and
asserting the request/response shape and the no-server-reachable failure message; L4 — a
compiled-binary test starting `nooma serve` against a real vault, then running `nooma capture
"<text>"` against it, and asserting a unit was persisted (read back via a direct store read or a
subsequent status-shaped check).

### R3.2 — The demo captures via the CLI and finds it via recall

**MUST**: the demo (umbrella proposal §2: "capture through the API and through the CLI, then ask
'what do you know about X?' and get a real recall over what was captured") is exercised by at
least one L4 test that captures via `nooma capture`, and finds the captured content through a
recall — either a subsequent `nooma capture` classified `recall` (reusing R2.3's routing) or the
HTTP `/recall` route from R2.8, since this spec does not mandate a `nooma recall` subcommand —
proposal §3.2 item 11 names only "a `capture` subcommand" for `cmd/nooma`, not a recall one.

**Verified by**: L4.

### R3.3 — The demo must not be shown a timer

**MUST**: no case in this PR's own demo script or fixture corpus asks the CLI to capture a `timer`
or `recurring_reminder` message — Q3a's own closing sentence, restated by `docs/02-cognitive-core.md`
§5's M1 note: "the demo must not be shown a timer." Restated here as PR 14's obligation, since PR
14 is the PR that walks the demo end to end.

**Verified by**: review of the demo script/fixtures this PR adds.

---

## 4. Provider configuration (PR 15 — `feat/init-provider-paths`)

Traced to `m1-capture-recall/proposal.md` §3.2 item 14, `docs/adr/0002-default-llm-preset.md`'s
Decision, and CLAUDE.md non-negotiable #7. Read first, verified before writing a requirement:
`cmd/nooma/init.go`'s `defaultConfig()` ships `providers:`/`tasks:` fully commented today, with no
interactive step — M0 built the placeholder, not the wizard.

### R4.1 — `nooma init` offers exactly two first-class provider paths: Cloud and Ollama

**MUST**: `nooma init` gains a step — interactive or flag-driven; this spec does not mandate which
— offering exactly two first-class paths, Cloud (recommended) and Ollama, that replaces the
commented `providers:`/`tasks:` placeholder with a real, populated block for both, plus matching
`tasks:` bindings covering the seven documented tasks (`config.DocumentedTaskNames`) the chosen
path can serve.

**MUST NOT**: this PR offer, or resurrect, the embedded llama.cpp option — ADR-0002's Decision,
verbatim: "The embedded option is discarded."

**Verified by**: L2 — a wizard-flow test driving each path with scripted input and asserting the
resulting `nooma.yml` carries a `providers:` entry and matching `tasks:` bindings; L4 — a
compiled-binary run of `nooma init` through each path against a real filesystem target.

### R4.2 — Cloud is the path that must work; Ollama is offered but is not this change's own exit criterion

**MUST**: the Cloud path, once completed, produces a vault whose `providers:`/`tasks:`
configuration is immediately usable by the capture pipeline PR 13/14's own demo drives — owner
direction, `proposal.md` §3.2, verbatim: "Cloud is the path that must work... M1 is judged on the
cloud path running end to end."

**MUST**: the Ollama path, once completed, also produces a valid, loadable `nooma.yml`/`.env` pair
— `internal/providers/ollama` already exists (Phase A, `m1a-substrate`) and stays supported; the
wizard must not offer a path it cannot actually configure end to end at the config-writing level.

**MUST NOT**: PR 14's own L4 demo (R2.8, R3.2) depend on the Ollama path succeeding — only the
Cloud path is required to carry the demo. This asymmetry is deliberate, not an oversight: owner
direction, verbatim, "the local-model story has comments pending and is deliberately not the
priority — the ollama client exists and stays supported, but M1 is judged on the cloud path running
end to end."

**Verified by**: L4 — the demo test (R3.2) runs only against a vault the Cloud path configured;
review confirming no PR 14 test depends on an Ollama-configured vault.

### R4.3 — `nooma init` never writes a secret; the guarantee is structural, not a runtime scrub

**MUST**: the function or type that renders the `providers:` block into `nooma.yml` accepts no
parameter capable of carrying a raw credential value — only the provider type string and the
environment-variable NAME string (`api_key_env`) it points at. This is a structural, type-level
guarantee — CLAUDE.md non-negotiable #7's own framing, "safe defaults are structural, not
warnings" — the same shape PR 12's R1.7 already used for Q3c-iii: a signature incapable of
carrying the wrong thing, rather than a value trusted not to be misused.

**MUST**: any key value the wizard collects interactively during the Cloud path is written only to
`.env`, at the `0o600` permission M0's `populateVault` already establishes for that file (verified:
`cmd/nooma/init.go`'s `populateVault`, `os.WriteFile(..., 0o600)`) — never passed to, or reachable
from, the code path that writes `nooma.yml`.

**MUST**: `nooma.yml`'s own header comment continues to state this guarantee in the config this PR
writes, not only in M0's placeholder — verified: `defaultConfig()`'s existing header already says
"Secrets are never written here... a credential is always referenced by the NAME of an environment
variable."

**Verified by**: L1/L2 — a review-level signature check (the same category R1.7's "no combined
patch method" check uses) confirming the `providers:`-block-rendering function's declared
parameters contain no field typed to hold a raw secret; L4 — a test running the Cloud wizard path
with a scripted API key value as interactive input, then asserting the literal key string appears
nowhere in the written `nooma.yml` while `.env` carries it (or the wizard's own output instructs the
user to set it).

**Scenario**:
- GIVEN a user completing the Cloud wizard path and supplying an API key value when prompted
- WHEN `nooma init` finishes and the resulting vault is inspected
- THEN `nooma.yml` contains `api_key_env: ANTHROPIC_API_KEY` (or the equivalent name for the
  chosen provider), never the key's own value, and the value itself is written only to `.env`

### R4.4 — The written configuration decodes and validates through the real loader, unchanged

**MUST**: the `providers:`/`tasks:` blocks this PR writes decode and validate through
`config.Decode`/`cfg.Validate` exactly as `defaultConfig()`'s existing commented placeholder is
proven to do today — verified: `cmd/nooma/init.go`'s own doc comment names `TestFreshVaultIsLoadable`
as the test that would fail if `defaultConfig()` stopped decoding or validating; this PR extends
that same obligation to a populated configuration, not a commented one.

**MUST**: every task the wizard binds names a provider present in the `providers:` map it also
wrote — `internal/config/validate.go`'s `checkTaskProviders` (Phase A) is what this PR's own output
must satisfy, not a new check this PR invents.

**Verified by**: L2 — `TestFreshVaultIsLoadable`-shaped coverage extended to a wizard-populated
vault for both paths.

### R4.5 — No test in this PR touches the network or a real provider

**MUST NOT**: any L1, L2, L3, or L4 test this PR adds call a real Anthropic/OpenAI/Ollama endpoint
— the wizard's own interactive flow is driven by scripted input in every test. `nooma init` itself
makes no provider call at all (it only writes configuration); only a human running it for real, and
only later when the vault is actually used, reaches a live provider.

**Verified by**: review — every wizard test in this change drives scripted input, none opens a
network connection.

---

## 5. The doctor quality gate (PR 16 — `feat/doctor-quality-gate`)

Traced to `m1-capture-recall/proposal.md` §3.2 item 15, `docs/adr/0002-default-llm-preset.md`'s
Decision and Consequences, and `testdata/llm/format.md`. Read first, verified before writing a
requirement: `cmd/nooma/doctor.go` already exists (M0) with a `doctorChecks []doctorCheck` table
of five checks (configuration, permissions, database integrity, schema version, bind) and a
report loop (`runDoctor`) that accumulates every failure rather than stopping at the first — this
PR is an addition to that table, not a new command.

### R5.1 — `doctorChecks` gains a structured-JSON quality check, sending the fixed prompt set to each task's configured provider

**Revised (project/quality-gate-sends-stub-prompts; openspec Conflicts §C24)**: "sends
`testdata/llm/cases/`'s recorded `prompt` text" below described the shape actually shipped by
`16a-i`, and that shape is the defect this revision closes — a live OpenAI key found `checkLLMQuality`
failing 21 of 21 prompts, every one a formatting failure, because that recorded `prompt` field was a
60-84 byte fake-replay identifier, not classify's real ~1550-byte prompt. See R5.3 below for the
corrected requirement.

**MUST**: `cmd/nooma/doctor.go`'s `doctorChecks` slice gains one new `doctorCheck` entry (following
the existing `{name, run}` shape verbatim) whose `run` function, for each task the vault's `tasks:`
configuration binds, builds the live prompt through the same function production calls —
`classify.BuildPrompt` for `capture_processing`, `brain.JudgePrompt` for `relation_evaluation` — from
`testdata/llm/cases/`'s recorded `message` (and, for `relation_evaluation`, `candidates`) fields, sends
it to the provider configured for that task, and decodes the live response with the production
decoder that task's shape already has — `classify.Decode` for `capture_processing`-tasked prompts,
`relation.DecodeJudgment` for `relation_evaluation`-tasked prompts — ADR-0002's Decision, verbatim:
"it sends a fixed set of classify and judge prompts to the provider configured for each task, and
verifies the returned JSON validates against the expected schema."

**MUST NOT**: this PR introduce a new command, rewrite `runDoctor`'s report loop, or change how any
of the five existing checks run — it is one new row in an existing table.

**Verified by**: L2 — a test asserting `doctorChecks` grew by exactly one entry and every existing
check's behavior is unchanged; review of the new entry's shape against the existing four.

### R5.2 — A failure names the provider unsuitable for that task, never in general

**MUST**: when a task's configured provider fails the gate (R5.4 below), the reported failure names
the task the failure applies to — ADR-0002, verbatim: "doctor reports the provider as unsuitable
for that specific task, not in general — a model can be excellent at chat and bad at JSON, and the
user has to see that distinction."

**MUST NOT**: a failure on one task's prompt set cause another task's check to be skipped, or be
folded into a single collapsed verdict for "the provider" — matching `runDoctor`'s own existing
independence across checks (verified: its loop accumulates `failed++` per check, no early return).

**Verified by**: L2 — a test scripting one task's provider to fail and a different task's provider
to pass, asserting the report names the failing task specifically and the passing task separately.

**Scenario**:
- GIVEN a provider configured for `capture_processing` that reliably produces valid classify JSON,
  and the same or a different provider configured for `relation_evaluation` that does not
- WHEN `nooma doctor` runs the quality gate
- THEN the report shows `capture_processing` passing and `relation_evaluation` failing, naming
  `relation_evaluation` specifically — never one verdict covering both tasks

### R5.3 — The gate builds the real production prompt from the corpus's `message`/`candidates` fields, never a separately recorded prompt string, and never compares against `response`/`expected`

**Revised (project/quality-gate-sends-stub-prompts; openspec Conflicts §C24) — the original text
below was the requirement that shipped the defect, kept here struck through in spirit but not in
fact (this document does not edit an Accepted ADR's own words, only this PR's own prior
requirement text) so the correction is traceable.** The original `prompt` field could not be both a
stable replay key (`fakeprovider.Fake` selects by case id, never by prompt content — spec R5.2's own
precedent for "never by matching the live prompt text") and a genuine elicitor of the recorded
`response` — sending it live sent a stub, and a real provider answered in prose because prose is a
perfectly reasonable reply to 60-84 bytes with no format instruction. One field could not do both
jobs; splitting the corpus into `message` (the raw text a real builder needs) and letting the gate
call that real builder is the fix, not a schema tweak on top of the old shape.

**MUST**: the gate builds its own live request through the same function production calls —
`classify.BuildPrompt(message, nil, now)` for a `capture_processing`-tagged case,
`brain.JudgePrompt(unit.Unit{Content: message}, candidates)` for a `relation_evaluation`-tagged case
— reading `message` (and, for `relation_evaluation`, `candidates`) from `testdata/llm/cases/`'s
corpus, the same corpus `m1b-pipeline` already built and populated — ADR-0002's Consequences,
verbatim: "The gate's prompt corpus is the same one that feeds the test golden files: written once,
used in two places." (That claim now holds for `message`/`candidates`, not for a `prompt` field this
corpus no longer carries — see `testdata/llm/format.md`'s own note.)

**MUST NOT**: the gate compare the live response against the corpus case's own `response` field, or
against `testdata/classify/cases/`'s `expected` field. Both are tied to one specific past recording
of one specific `provider`/`model` pair (`testdata/llm/format.md`'s own per-case fields, verified)
and are not ground truth for a *different*, live provider's own new answer — ADR-0002's "the
provider configured for each task" means the same fixed prompts go to whichever provider is
actually configured, not only the one that originally recorded them.

**MUST NOT**: the gate send a corpus case's raw `message` (or, for `relation_evaluation`, raw
candidate content) directly as the live request, bypassing `classify.BuildPrompt`/`brain.JudgePrompt`
— that is exactly the shape of the original defect, one layer further in: a short, format-instruction-free
string a real provider is free to answer in prose.

**Verified by**: L1/L2 — a test asserting the check's request text equals
`classify.BuildPrompt`'s/`brain.JudgePrompt`'s own output for the corpus case's `message`/`candidates`,
built through the same call the test makes (so a future change to either builder is picked up
automatically rather than compared against a stale hardcoded string); and that no assertion in the
check's decision logic reads that case's `response` or an `expected` field from
`testdata/classify/cases/`.

### R5.4 — Validity means zero degradations, not merely a decodable response — upheld, with two refinements

**Upheld as flagged, with two refinements design's own reconciliation added.** This requirement's
prior revision correctly derived the zero-degradations bar from a real tension between I14's
tolerant decoder and ADR-0002's "verifies... validates" wording, rather than assuming one side.

**MUST**: a live response is judged valid only if decoding it reports **no** `Degradation` entries
(via `classify.Decode`) or no missing required judgment fields (via `relation.DecodeJudgment`) for
the fields the corpus case's task expects populated. Reusing I14's tolerant decoder by its bare
non-error return would be too weak a bar: I14 exists precisely so a malformed field never aborts
decoding (`m1b-pipeline` R1.2), so "did not error" is satisfied by a response that degraded every
field but one. ADR-0002's own wording — "verifies the returned JSON validates against the expected
schema" — demands the stricter reading: a provider whose JSON needs I14's tolerance to be usable at
all is exactly the "bad at JSON" case this gate exists to catch.

**Refinement 1 — the bar is tight but not absurd; doc 02 already says why.** Doc 02 §5.1: "only the
required fields' absence is reported; an optional field's absence is the ordinary case, not a
loss." So a live response that legitimately omits an optional field is **not** a degradation for
this gate's purposes — "zero degradations" means zero recorded `Degradation` entries, not "every
field, required or not, must be present."

**Refinement 2 — the report names which kind of failure, because doc 02 treats them as different
events.** Doc 02 §5.1: a wrong-shaped value and an out-of-vocabulary value "are recorded as
different events… one is a formatting failure, the other a vocabulary failure, and §9's learning
loop should not confuse them."

**MUST**: a failure's report names the field, the `Reason` (`ReasonWrongType`/`ReasonTruncated`
distinct from `ReasonUnknownEnum`), and the task — never a single collapsed "bad JSON" verdict. A
formatting failure and a vocabulary failure call for different advice (a JSON-mode setting versus a
different model), and a gate that merges them throws away the distinction doc 02 itself draws.

**MUST**: each corpus prompt is sent **once** per gate run, never retried — a retry that turns a
flaky provider green is worse information than a single honest sample, and the sample size is the
corpus itself, not the number of attempts.

**MUST**: the report states the count in the form "`k` of `n` prompts produced clean JSON," and the
task fails when `k < n` — the count is what lets a reader tell "every prompt was clean" from "most
were" without re-deriving it from a list of failure lines.

**MUST NOT**: this check re-derive its own JSON validator — it calls the same `classify.Decode`/
`relation.DecodeJudgment` Phase B already built and I14's conformance suite already proves correct
against known-good and known-bad fixtures, independently of this PR. A hand-rolled validator written
fresh for `doctor` would repeat this milestone's own recorded shape (`m1b-pipeline/tasks.md` C11,
C12: "a fixture verified only against itself is not a fixture, it is a restatement") — here, a
validator checked only against its own author's idea of "valid JSON," never against the production
decoder every other test in this codebase is already held to.

**Verified by**: L1/L2 — a table test over scripted `fakeprovider` responses covering a clean pass
(zero degradations), a response with only an optional field absent (still a clean pass, per
Refinement 1), and each I14 degradation shape (truncated, wrong-typed, unknown-enum, asserting the
report names the correct `Reason` per Refinement 2); a test asserting the corpus is sent exactly
once per case with no retry; a test asserting the report's count line matches `k`/`n`.

### R5.5 — The live call is `nooma doctor`'s own runtime behavior, never a test that touches the network

**MUST**: this check's decision logic (per-task attribution, degradation counting, the failure
message shape) is proven at L1/L2 against `fakeprovider` with scripted responses — never against a
real network call, per CLAUDE.md non-negotiable #5.

**MUST**: the check's actual live behavior — sending the corpus's prompts to the real,
user-configured provider — is `nooma doctor`'s own runtime behavior when a user runs it against a
live vault with real credentials, the same category as the existing `checkPermissions`/
`checkIntegrity` checks, which already perform real I/O no `make check`/`make check-all` target
exercises with a live provider.

**MUST NOT**: any L1, L2, L3, or L4 test this PR adds call a real provider.

**Verified by**: review — every test for this check goes through `fakeprovider`.

### R5.6 — The gate is a no-op, not a failure, when no provider is configured for a task — upheld, made structural

**Upheld as flagged, made structural rather than an early return.** This requirement's prior
revision correctly derived the no-op requirement from `test/e2e/doctor_test.go`'s existing
`TestDoctorOnAHealthyVault` — a fresh, provider-less vault must still report `doctor` healthy.

**MUST**: for a task the vault's `tasks:` configuration does not bind to any provider, the gate
reports nothing — neither pass nor fail — for that task. A freshly `init`ed vault (M0's
`defaultConfig()`, verified: `providers:`/`tasks:` ship fully commented) configures no provider at
all, and `test/e2e/doctor_test.go`'s existing `TestDoctorOnAHealthyVault` already asserts a fresh
vault reports `doctor` healthy with zero failures — a gate that failed on an absent provider would
regress that existing, passing L4 test.

**MUST**: the no-op is structural — the gate iterates the vault's configured `tasks:` bindings, so
zero bindings means zero iterations, not a branch such as `if len(tasks) == 0 { return ok }`. An
explicit early-return branch is both an arm no test can meaningfully drive (there is no decision to
make when there is nothing to iterate) and an invitation for a later "warn if empty" to grow inside
it; iterating an empty set already does the right thing without a branch to maintain.

**MUST**: the report states the count even at zero — e.g. "`llm quality: ok (0 tasks configured)`"
— so a reader can tell "passed" from "did not run," the same distinction `scripts/core-coverage.sh`'s
own "armed but vacuous" framing already names elsewhere in this project.

**Verified by**: L2 — a check-decision test over a configuration with no `tasks:` entries, asserting
the check reports zero failures and its report line states the zero count; a review confirming no
`len(tasks) == 0`-shaped early-return branch exists in the check's own control flow;
`test/e2e/doctor_test.go`'s `TestDoctorOnAHealthyVault` stays green against a freshly `init`ed vault
with no change to that test required by this PR.

### R5.7 — Unreachable is not unsuitable

**MUST**: a transport error while sending a prompt — connection refused, DNS failure, a timed-out
request — is reported as the provider being **unreachable**, in `doc 01`'s own existing category
("provider unreachable → how to install it") — never folded into, or reported alongside, a
JSON-fitness verdict (R5.4). A model cannot be judged bad at JSON on the strength of a network that
never delivered the question.

**MUST**: the live call carries a bounded timeout, so a single unreachable provider cannot make
`nooma doctor` hang.

**Verified by**: L2 — a test scripting a transport-level failure for one task's provider and
asserting the report names it unreachable, distinct in wording and in category from a
JSON-degradation failure (R5.4); review confirming the live call is timeout-bounded.

### R5.8 — Corpus coverage for every task the gate checks; this PR closes any gap

**MUST**: for every task this gate checks (`capture_processing`, `relation_evaluation`),
`testdata/llm/cases/` holds at least one case tagged with that `task` — verified against the corpus
as it exists at the start of this PR, not assumed. Where a task this gate must cover has no existing
case, this PR adds it, following the same "written once, used in two places" discipline
`m1b-pipeline` already established for `capture_processing`.

**Verified by**: `test/support/goldenset.Load` successfully loads every relevant case; review
confirming both tasks have coverage before this PR's own check is written against them.

---

## 6. OpenAI embeddings (PR 17 — `feat/openai-embeddings`)

Traced to `m1-capture-recall/proposal.md` §5's PR 17 row and its closing note (added by proposal
PR #102), and ADR-0002's "the `nooma init` wizard offers two first-class paths: Cloud
(recommended) and Ollama," restated by §3.2 as "Cloud is the path that must work." Read first,
verified before writing a requirement: `internal/providers/` holds `anthropic/client.go`,
`openai/client.go`, `ollama/client.go`, and exactly one file implementing
`ports.EmbeddingProvider` — `ollama/embed.go`. A Cloud-configured vault had no embedder to bind
`tasks.embedding` to.

### R6.1 — An OpenAI embeddings client implements `ports.EmbeddingProvider`, in the shape `m1a-substrate`'s HTTP clients already established

**MUST**: `internal/providers/openai` gains an embeddings client implementing
`ports.EmbeddingProvider` — the same port `ollama/embed.go` already implements, following the same
client shape `m1a-substrate` PR 6 already established for its three chat clients (anthropic,
openai, ollama).

**MUST NOT**: this PR modify `ports.EmbeddingProvider`'s own interface — the port already exists
(Phase A); this PR adds an implementation, not a new contract.

**Verified by**: L2 — an `httptest`-backed test driving the client against a scripted HTTP
response, asserting it returns a vector shaped like the port's existing embed-response type.

### R6.2 — PR 15's diff performs the binding: `6 → 17 → 15` settles the question this requirement previously left open

**Settled, not a hedge.** A prior revision of this requirement left open which PR's diff binds
`tasks.embedding` to this PR's client — "PR 15's wizard may already be written generically... or
this PR may extend PR 15's wizard logic directly; either satisfies this requirement." The
proposal's dependency line, as merged (`e70c435`), now reads `6 → 17 → 15`: PR 17 (this PR) lands
**before** PR 15. The second option is therefore impossible — there is no PR 15 wizard yet for
this PR to extend — and the first is simply what happens rather than a choice this spec still
needs to leave open: PR 15's wizard is written against an embedder that already exists.

**MUST**: once this PR and PR 15 (§4 above) have both landed, a vault configured through `nooma
init`'s Cloud path has `tasks.embedding` bound to this PR's OpenAI embeddings client — closing the
gap the proposal's own PR 17 note names: "Anthropic publishes no embeddings API, so OpenAI is the
provider."

**MUST**: PR 15's own diff performs this binding. This PR (PR 17) touches no `cmd/nooma` file —
its own scope is the client (R6.1) and the visibility requirement (R6.3), never the wizard.

**MUST**: PR 15's own R4.1 obligation — `tasks:` bindings "covering the seven documented tasks...
the chosen path can serve" — is satisfied for `embedding` specifically once PR 15 lands, since PR
17 already exists in the tree by construction of this ordering, and PR 15 never writes a binding
`checkTaskProviders` would reject.

**Verified by**: L2 — a wizard-flow test (extending R4.1's own coverage) asserting a freshly
`init`ed Cloud vault's `tasks:` block includes an `embedding` entry; `git diff --name-only` for
this PR (PR 17) contains no path under `cmd/nooma/`.

### R6.3 — A Cloud vault whose captures come back unembedded is something a test can state, not something a user discovers by wondering why search feels thin

**MUST**: at least one conformance test asserts that a capture against a Cloud-configured vault
(PR 15's Cloud path, with `embedding` bound per R6.2) produces an embedded unit — not merely that
capture succeeds, which `m1b-pipeline`'s own D8 degradation design already guarantees regardless of
whether any embedder exists at all. This is the requirement that closes the gap this PR exists for:
a degradation designed for a provider outage was silently absorbing the absence of a cloud embedder
entirely, and a future regression that drops the `embedding` binding again must fail this test
loudly, not degrade the demo's recall quietly a second time.

**MUST**: the same freshly `init`ed Cloud vault a reader can inspect (R6.2's own verification)
states which tasks are bound in `nooma.yml` directly — `embedding` present once this PR exists is
this PR's own visible proof the gap closed, readable without running a capture at all.

**MUST**: `ports.EmbeddingRepo` gains a method reporting how many live units hold no embedding —
e.g. `CountLiveWithoutEmbedding` — and `nooma doctor` gains a check that reports the count, so a
vault already in the wild, not only a build-time test, can state whether its live units are
embedded. This is not a new exception; it is a debt `m1b-pipeline` recorded and named its own
recipient for. `m1b-pipeline/design.md:790-793` declined to ship a consistency-query method in
Phase B "deliberately: `UnembeddedLive` or similar would be a port method whose only caller is a
test... **The obligation is recorded for whoever ships `doctor`'s consistency check**," and
`docs/03-data-model.md:306-307` already promises, as an existing v1 commitment predating this
change entirely, that "`nooma doctor` runs `PRAGMA integrity_check` + units↔embeddings↔fts
consistency." Phase B's own stated condition — that the method needed a real caller, not a test —
discharges itself here: `doctor` is that caller. R7.3 and R7.4 name this method as their own
sanctioned exception, below.

**MUST NOT**: this requirement be satisfied by a code comment, a doc note, or a step in a manual
verification checklist alone — a silent, permanently-degraded recall is exactly the failure mode
this requirement exists to make loud instead.

**Verified by**: L2 — the conformance test named above, proving the distinction is *observable*: a
wired pipeline with `tasks.embedding` bound returns an embedded unit, and the same pipeline
without it does not; the `CountLiveWithoutEmbedding`-backed `doctor` check, proven at L2 against a
`repocontract`-shared fake and at L3 against a real vault holding both embedded and unembedded
live units (archived units excluded from the count, per I02's own read-side filter). L4 — R8.1
separately requires the same observable distinction proven once more, end to end, through the
compiled binary against a wizard-written Cloud vault; the two levels are not duplicates of each
other — L2 proves the distinction exists in the pipeline, L4 proves it survives being wired
together for real.

### R6.4 — No test in this PR touches the network or a real provider

**MUST NOT**: any L1, L2, L3, or L4 test this PR adds call a real OpenAI endpoint — every test
goes through `httptest`'s local fake server (for the client itself, R6.1) or `fakeprovider` (for
anything exercising the capture pipeline, R6.3), the same discipline every other PR in this change
already states.

**Verified by**: review — every test for this PR goes through `httptest`/`fakeprovider`.

---

## 7. Cross-cutting constraints

### R7.1 — No test in this change touches the network or a real LLM/embedding provider

**MUST NOT**: any L1, L2, L3, or L4 test added by PRs 12–17 open a network connection or call a
real provider — CLAUDE.md non-negotiable #5; `docs/06-harness.md` §3. This includes PR 16's own
quality-gate check (R5.5), PR 15's wizard (R4.5), and PR 17's embeddings client (R6.4): their
decision logic is tested against `fakeprovider`/`httptest`/scripted input, their live behavior is a
runtime property, never a test.

**Verified by**: review — every provider-facing test in this change goes through
`test/support/fakeprovider` or `httptest`.

### R7.2 — Only PR 12 touches `internal/core/**` in this change, and its `docs-sync` obligation is already discharged

**MUST**: PR 13, PR 14, PR 15, PR 16, and PR 17 are not required to touch
`docs/02-cognitive-core.md` — none adds code under `internal/core/**` (PR 13's Kind-based routing
fork and its auth middleware both live in `internal/brain`/`internal/httpapi`, the same layers
`m1b-pipeline`'s R4.6 already placed the timer-refusal fork in; PR 14, PR 15, PR 16, and PR 17 are
CLI/`cmd/nooma`/`internal/providers` wiring only, reusing the production decoders PR 12/
`m1b-pipeline` already built rather than adding new `core` code, per R5.4).

**MUST**: PR 12 is the only core-touching PR in this change, and R1.13 states its `docs-sync`
treatment in full — it may need no new prose, since doc 02 already carries Q3c's answer.

**Verified by**: `git diff --name-only` per PR; `docs-sync.yml` on GitHub for PR 12.

### R7.3 — Phase A and Phase B's delivered surfaces are not modified beyond additions

**MUST NOT**: any PR in this change modify `internal/core/unit/**`, `internal/core/classify/**`,
`internal/core/relation/**`, or any file under `internal/core/recall/**`/`internal/ports/**`/
`internal/store/sqlite/**`/`internal/providers/**` that Phase A or Phase B already delivered,
beyond what R1.2's scored fusion, R1.3's gate, R1.7's two new `UnitRepo` methods, R6.1's new
provider client, and R6.3's new `EmbeddingRepo` consistency method require — those land as
additions (new functions in existing files where R1.2/R1.7/R6.3 name them, new files for
`SignalRepo` and the OpenAI embeddings client), not edits to Phase A/B's existing exported
behavior. R1.2's own MUST NOT already states that this constrains exported behavior, not a
function's implementation.

**`internal/ports/embeddingrepo.go`'s exception is a debt discharged, not a scope carved.**
`m1b-pipeline` deliberately shipped no consistency-query method against this exact file, and named
its own recipient: `m1b-pipeline/design.md:790-793` — "This design ships **no** consistency-query
method in Phase B, deliberately: `UnembeddedLive` or similar would be a port method whose only
caller is a test... **The obligation is recorded for whoever ships `doctor`'s consistency
check**." `docs/03-data-model.md:306-307` already promises, independent of this change, that
`nooma doctor` checks "units↔embeddings↔fts consistency." Phase B's stated condition — no caller
but a test — discharges itself once `doctor` (R6.3) is that caller. This requirement was written
without knowledge of that deferral; naming the exception here corrects that omission, it does not
grant a new one.

**Verified by**: `git diff --name-only` per PR; `store_api.golden`'s diff (R1.12) shows only
additions.

### R7.4 — Existing `internal/ports` files are not otherwise modified; new ports land as new files

**MUST**: the new `SignalRepo` port (R1.10) is a new file. `internal/ports/decisionlog.go`,
`internal/ports/relationrepo.go`, `internal/ports/provider.go`, `internal/ports/clock.go`, and
`internal/ports/lexicalsearch.go` are not touched by this change. `internal/ports/unitrepo.go`
gains R1.7's two new methods, `internal/ports/decisionlog.go` gains R1.9's two new
`DecisionAction` members, and `internal/ports/embeddingrepo.go` gains R6.3's new consistency
method (`R7.3`'s own citation of `m1b-pipeline/design.md:790-793` and
`docs/03-data-model.md:306-307` is this exception's authority) — three sanctioned edits to
existing interfaces, and the only three.

**MUST NOT**: this enumeration be read as covering only the five untouched files while leaving
`embeddingrepo.go`'s status implicit — a prior revision of this requirement did exactly that,
naming five files as untouched and two edits as sanctioned while never mentioning
`embeddingrepo.go` at all, so the list read as exhaustive without being complete. Every
`internal/ports/*.go` file existing before this change is now named above, either as untouched or
as carrying a named, sanctioned edit.

**Verified by**: `git diff --name-only`; a directory listing of `internal/ports/*.go` as it stood
before this change, cross-checked against this requirement's own enumeration for completeness.

---

## 8. Test levels

### R8.1 — Level assignment for this change

**MUST**: the scored fusion (R1.2), the margin gate (R1.3), and the edit-planning decision (R1.8)
are **L1** — pure functions, no database, no network, no clock.

**MUST**: the correction orchestration (R1.1, R1.5, R1.6, R1.8, R1.9, R1.10, R1.11), the capture
and recall HTTP routing (R2.1–R2.3, R2.5–R2.7), the standalone recall route (R2.4), the auth
middleware (R2.9–R2.12), the CLI-as-HTTP-client logic (R3.1), the wizard-flow tests (R4.1, R4.3,
R4.4), the quality-gate decision logic (R5.1, R5.2, R5.3, R5.4, R5.6, R5.7, R5.8), the embeddings
client's own request/response shape (R6.1), and the observable-embedding property R6.3 states at
build time (both the conformance test and the `doctor` coverage check's decision logic) are **L2**
(`test/conformance/`, untagged, or an equivalent `cmd/nooma`/`internal/httpapi`-scoped unit test
for CLI/HTTP-layer logic) — exercised against `memrepo`/`fakeprovider`/`httptest`/scripted stdin,
per `docs/06-harness.md` §3's L2 definition.

**MUST**: the SQLite implementation of the new port surface (R1.12, and R6.3's
`EmbeddingRepo.CountLiveWithoutEmbedding`) is **L3** (`integration` tag) — it requires a real
migrated SQLite vault.

**MUST**: the compiled-binary capture/recall walk (R2.8), the CLI capture command and demo (R3.1,
R3.2), the wizard's full CLI flow for both provider paths (R4.1, R4.2, R4.3), and the
Cloud-vault-embeds property proven once more end to end through the compiled binary (R6.3) are
**L4** (`e2e` tag) — this is not a second requirement to prove the same fact R6.3's L2 test
already proves; R6.3 itself states what each level proves: L2 that the distinction is observable
in the wired pipeline, L4 that it survives being wired together for real, against a
wizard-written vault.

**MUST NOT**: any L4 test this change adds for PR 16 call a real provider — R5.5/R5.6 already state
why: the gate's live call is runtime behavior, not something CI exercises, and `TestDoctorOnAHealthyVault`
(R5.6) is the existing L4 coverage this PR must not regress, not new L4 coverage this PR is required
to add for the gate's own live call.

**Verified by**: file placement and build tags, per `docs/06-harness.md` §3.

### R8.2 — Every new test is observed failing for the right reason first (Strict TDD)

**MUST**: each requirement's test in this spec is written before its implementation and observed
failing with the expected message or compiler error — non-negotiable #4, and Strict TDD Mode,
which is active for this project and not inferred, applies to every requirement in §1–§6 above.

**MUST NOT**: a failing test be weakened to pass. The two legitimate exits are fixing the code or
changing the governing document (doc 02, plus its ADR if affected) in the same PR.

**Verified by**: the commit sequence within each PR — a work-unit commit contains the test and the
code that satisfies it together.

---

## 9. Boundaries this change must not cross

### R9.1 — No PR in this change builds an undo surface, a version/history table, or a `corrects` edge

**MUST NOT**: any PR in this change persist a previous value anywhere other than R1.9's
`decision_log.context` pre-image, or build any mechanism to read that pre-image back and reverse a
correction — ADR-0016's own stated gap: "Undo is **not** built by this decision... no surface does
that until M4."

**Verified by**: `git diff --name-only` over the full chain contains no new table, no
`internal/core/correction/versions*` (or equivalent) path, and no route or command that reads a
pre-image back as a reversal.

### R9.2 — No PR implements anything from the umbrella proposal's explicit non-goals

**MUST NOT**: any PR in this change compute or persist `effective_weight`, priority, focus,
hysteresis (I05, I19 — M2); implement any consolidation phase, including `connect`'s reuse of
R1.2's scored fusion (M2); arm, evaluate, or fire a trigger or timer (I04, I15–I17 — M3); derive a
self-belief (M2); consume the new `correction` learning signal (M5 — this change *emits* it, per
I13, and consumes nothing); add a Telegram channel; implement `nooma reindex` as a command (M6); or
touch perception/`measurements` (v2, ADR-0005).

**Verified by**: `git diff --name-only` over the full chain contains no path under
`internal/core/consolidation/**` (beyond its existing `doc.go`), `internal/core/prospection/**`,
`internal/core/selfmodel/**`, `internal/core/learning/**`, `internal/channels/telegram/**`, or a
`nooma reindex` subcommand.

### R9.3 — PR 14 must not absorb PR 15's, PR 16's, or PR 17's scope; each is its own PR with its own requirements

**MUST NOT**: PR 14 (`feat/cli-capture-demo`) implement any part of `nooma init`'s Cloud/Ollama
paths (§4 above), `nooma doctor`'s structured-JSON quality gate (§5 above), or the OpenAI
embeddings client (§6 above) — this spec's own earlier revisions found each of these named or
required with no PR of their own at the time, and declared each a conflict rather than resolving
it by folding the item into PR 14's demo scope (`## Conflicts`, Conflict 1). The owner's
resolutions scheduled them as PRs 15, 16, and 17, each with its own requirements above — the
exclusion this requirement previously stated is superseded by that scheduling, but the boundary it
was protecting stays: PR 14 walks the demo through configuration, a quality gate, and an
embeddings client PRs 15, 16, and 17 already built, it does not build any of them itself.

**MUST**: PR 14 depends on PR 13, PR 15, PR 16, and PR 17 completing first (`proposal.md` §5's
dependency line: `(13,15,16,17) → 14`) — PR 14's own demo (R3.1, R3.2) runs against a running
`nooma serve` instance (PR 13's routes and auth) over a vault PR 15's Cloud path configured, with
PR 17's embedding client bound so the demo's recall is not silently lexical-only, diagnosed healthy
in the sense §5's gate can prove (though R5.6 makes running the gate itself optional for the demo,
since a fresh vault's absence of a provider is a no-op, not a failure, until PR 15 configures one).

**Verified by**: `git diff --name-only` for PR 14 contains no change to `cmd/nooma/init.go`'s
provider-wizard logic, `cmd/nooma/doctor.go`'s `doctorChecks`, or `internal/providers/openai/`'s
embeddings client; the proposal's own dependency graph for this change.

---

## 10. Open items this spec deliberately leaves to design

- **The exact port method signatures for `ports.SignalRepo`, `UpdateEventAt`, and `UpdateDueAt`**
  (R1.7, R1.10) — design's choice, following the structural precedent `ports.UnitRepo` and
  `ports.RelationRepo` already set (no `Delete*`-prefixed method for `SignalRepo`, per CLAUDE.md
  non-negotiable #6).
- **Which package inside `internal/core` hosts the margin gate function and the edit-planning
  function** (R1.3, R1.8) — `core/recall`/a new `core/correction` package, or another placement;
  either satisfies this spec as long as the functions stay pure and the package respects
  `depguard`'s core-purity rule.
- **The exact JSON shape of `decision_log.context` for the correction pre-image** (R1.9) — ADR-0016
  itself defers this: "this ADR does not fix its exact keys... doc 02 §5 is where the settled shape
  belongs." PR 12 discovers it; R1.13 states the resulting `docs-sync` treatment.
- **The `valence` value the `correction` learning signal carries** — doc 02 §9 lists `valence` as
  positive/negative/neutral per signal type but does not state which applies to `correction`
  specifically; design's choice, informed by doc 02 §13's own "exact values get calibrated with
  real usage" framing.
- **HTTP request/response JSON shapes** for the capture, recall, and read-only unit routes (§2) —
  this spec states what each route MUST do, not its wire schema. This now explicitly includes the
  auth error response's exact body (R2.11 mandates only that a missing and a wrong token look
  identical, not the exact bytes).
- **The exact declared route table ADR-0017's middleware and its completeness test share** (R2.9,
  R2.11) — design's choice of shape (a slice, a map, a generated list), as long as registration and
  the completeness test consume the same one.
- **Whether a `nooma recall` CLI subcommand exists** — R3.2 does not require one; if design adds
  one, it is a compatible addition to this spec, not a contradiction of it.
- **Whether `nooma init`'s provider step is interactive prompting, flags, or both** (R4.1) — design's
  choice; this spec states what the resulting configuration must satisfy, not how the user supplies
  the answers.
- **The exact wording and format of `nooma doctor`'s new quality-gate report line** (R5.1, R5.2,
  R5.4, R5.6) — design's choice, following `doctorCheck`'s existing `{name, run}` shape and
  `runDoctor`'s existing `FAIL`/`ok` line format; R5.6 requires the line to state a task count and
  R5.4 requires it to name a field/reason/task, but not the exact phrasing.
- **Whether PR 16 needs new `testdata/llm/cases/` entries beyond what already exists** (R5.8) — this
  spec requires the coverage, not a specific count; PR 16 verifies the corpus as it stands at PR 16's
  own start, per `m1b-pipeline`'s own precedent of building corpus cases in the PR that first needs
  them.
- **The exact method signature of `ports.EmbeddingRepo`'s consistency method** (R6.3) — this spec
  names it `CountLiveWithoutEmbedding` (or equivalent) and requires it to count live units with no
  embedding; the exact signature and its SQL shape are design's choice, in the same restraint this
  spec already applies to `ports.SignalRepo` and the two `UpdateEventAt`/`UpdateDueAt` methods.
- **Whether `nooma doctor`'s new coverage check lands in PR 16's own diff or PR 17's** (R6.3) —
  this spec requires the check and the port method to exist by the end of this change; it does not
  mandate which PR's diff adds the `doctorChecks` row, since `cmd/nooma/doctor.go` is PR 16's file
  and `internal/ports/embeddingrepo.go`/`internal/store/sqlite/embeddingrepo.go` are naturally PR
  17's, and a PR that touches both is not forbidden by anything else in this spec.

---

## Conflicts

Five contradictions have been found inside the governing inputs across this spec's revisions.
None was resolved by picking a side; each was recorded with the evidence for each side, per this
milestone's own established practice (`m1-capture-recall/proposal.md` §5's closing note already
records two earlier ones outside this document: `spec.md` R2.3 vs. `design.md` D3 on `incomplete →
archived`, and the PR-12-phase-placement contradiction between the proposal's own §5 table and its
§8 closing paragraph). Conflicts 1, 3, 4, and 5 have since been resolved; their evidence is kept
and each resolution is recorded rather than the conflict being deleted, matching how the proposal
itself treats a closed question — the disagreement stays visible, dated, and attributed. **Conflict
2 stands open** — the one contradiction remaining in this document as of this revision. Conflicts
3, 4, and 5 originate in `design.md`'s own reconciliation against this spec (its C6, C8, and C10
respectively), not in a fresh reading of the proposal — recorded here because a divergence between
this spec and its own design is exactly the shape a conflict entry exists to name, regardless of
which document surfaced it first. Conflict 5 is distinct from 3 and 4 in one respect worth naming:
`design.md`'s C10 was a disagreement between two of this spec's *own* requirements (R6.3 and
R7.3/R7.4), not between this spec and the design — the design correctly declined to resolve it
("picking a side would be exactly what this section exists to prevent," `design.md:151-152`) and
escalated it, exactly as this spec's own practice would ask of a sub-agent finding a contradiction
rather than quietly choosing one side.

### Conflict 1 — Proposal §3.2 items 14–15 claimed Phase C, but no PR in §5's then-three-row Phase C table named them. **RESOLVED — scheduled as PRs 15 and 16, not excluded.**

**Evidence for "in scope"** (unchanged): `m1-capture-recall/proposal.md` §3.2, on items 14 (`nooma
init`'s Cloud/Ollama first-class paths) and 15 (`nooma doctor`'s structured-JSON quality gate),
stated plainly: "They belong to Phase C (`m1c-surface`): both are CLI surface, both need providers
to exist first, and a wizard offering to configure a provider before any provider exists would be
offering a promise." This was unambiguous ownership language, not a hedge.

**Evidence for "not in the chain"** (as it stood before PR #101): §5's Phase C PR table listed
exactly three rows — PR 12 (`feat/corrections`), PR 13 (`feat/httpapi-capture-recall`), PR 14
(`feat/cli-capture-demo`) — and none named `init` or `doctor`. The proposal's own dependency list
for the then-fourteen-PR chain contained no PR whose content could plausibly be items 14/15 — every
PR's content was stated explicitly in §5, and none mentioned the init wizard or the doctor gate.

**This spec's own first revision resolved it locally, not by picking a side**: it was scoped by its
own governing instructions to the three PRs §5's table named at the time, matching their stated
content exactly, and therefore implemented neither item — recording the gap rather than absorbing
either into PR 14 (the only remaining Phase C PR with any spare budget) or dropping them silently,
which would have repeated the exact failure mode ADR-0002's own history already names in this
proposal's §3.2 ("Neither appeared in any milestone's bullets... They surfaced because the owner
asked an ordinary product question").

**The owner's resolution, recorded in `proposal.md` §5 itself** (dated 2026-08-02, PR #101):
*"They became PRs 15 and 16 on 2026-08-02, and the fix for the general case is the same one this
section already argued for: a decision is scheduled when it is a row in a table something reads,
not when a paragraph says where it belongs."* The proposal's own closing note on this credits the
mechanism that surfaced it: *"Surfaced by `sdd-spec` while scoping Phase C, which declared it as a
conflict instead of resolving it unilaterally, and confirmed independently before the owner
decided."*

**What changed in this document as a result**: §4 (PR 15) and §5 (PR 16) above now specify both
items in full, at the same rigor as every other PR in this change. R9.3 (formerly this document's
own R6.3, then R8.3) is inverted — it no longer excludes the items, it forbids PR 14 from absorbing
PRs 15/16/17's own scope (extended again once PR 17 was scheduled, per Conflict 1's own reasoning
applied a second time), which is a different boundary protecting the same underlying concern (a
decision is scheduled as a row in a table, not folded into whichever PR happens to have budget
left). The exclusion this spec previously assumed is superseded; the instinct not to silently
absorb the items into PR 14 was correct and is preserved.

### Conflict 2 — Proposal §4.8 ("No M1 core PR should need `no-spec-change`") vs. doc 02 already carrying PR 12's delta

**Evidence for §4.8's claim**: `m1-capture-recall/proposal.md` §4.8 states: "**No M1 core PR should
need [`no-spec-change`].** If one does, that is a signal the PR is not actually implementing a
behavior doc 02 describes." Every `m1b-pipeline` core PR (7, 8, 11) honored this — each added
genuine new prose to doc 02 in the same PR (`m1b-pipeline/spec.md` R1.7, R2.10, R5.6), and that
spec's own R6.2 restates the rule for Phase B without exception.

**Evidence for the tension**: `docs/02-cognitive-core.md` §5 step 4 and §13, read directly for this
spec (not inferred), already carry the referent-margin-as-ratio rule, the ADR-0016 pre-image
sentence, and the `correction_referent_margin = 1.5` row — all landed by dedicated docs commits
(`877f647`, `522abd8`, both in this repository's own git history) before PR 12's code exists. PR 12
touches `internal/core/recall` (R1.2's scored fusion, R1.3's gate) and, per the behavior described
above, may have no undocumented behavior left to write down — the exact opposite of what §4.8
frames as "a signal the PR is not actually implementing a behavior doc 02 describes." Here, the PR
implements a behavior doc 02 describes so completely that describing it again would be restating,
not documenting.

**Resolution recorded here, not applied silently**: R1.13 grants PR 12 a narrow, named exception —
it may carry `no-spec-change` truthfully if implementation surfaces no wording gap, and this spec
does not extend that exception to PR 13, PR 14, PR 15, PR 16, or PR 17 (R7.2), nor does it assert
§4.8's blanket claim is wrong for the rest of the milestone. The exception is local to the one PR
whose doc delta already landed elsewhere, and it is recorded as an exception rather than silently
treated as the norm. Unchanged by PR #101 or PR #102 — PRs 15, 16, and 17 touch `cmd/nooma/` and
`internal/providers/` only, never `internal/core/**`, so they were never candidates for this
exception in the first place.

### Conflict 3 — `spec.md` R1.8 wrote every field the classification carried; `design.md` D3 wrote exactly one. **RESOLVED — one field. Owner decision.**

**Evidence for "every field"** (this spec's prior revision): R1.8, verbatim: *"for each of
`{content, event date, due date}` present (non-nil) in the correction's classification, capture
calls the matching `UpdateContent`/`UpdateEventAt`/`UpdateDueAt` method on the referent unit."*
R1.9 matched it — "the previous value of **every field** about to change" — as does ADR-0016's own
wording.

**Evidence for "exactly one"** (`design.md` C6, D3): `normalized_content` is present in nearly
every classification — doc 02 §5.1 names its absence one of only two unsurvivable degradations —
so under the "every field" rule there is no such thing as a date-only correction: **every**
correction overwrites the referent's body with the model's normalization of the correction
utterance. For *"no, it's Ana not Anna"* that means a unit reading "Meeting with Anna on Tuesday"
becomes "It's Ana, not Anna" — the memory the correction was about is gone. ADR-0016 makes that
recoverable in principle, but nothing reads the pre-image back until M4, so in M1 it is recoverable
the way a backup nobody can restore is recoverable.

**Position, design.md's own words**: *"This design recommends [the one-field] rule (dated fields
win; content only when the correction resolved no date; two dates ask), because it takes the write
that requires no inference and refuses the one that requires an unlicensed and destructive one."*
Filed as a recommendation, not a unilateral resolution — design explicitly left it to the owner.

**Owner decision**: one field. Dates win; content is the no-date fallback; two dates present is an
ask, not a guess. The accepted cost is real and is now stated in the requirement itself, not left
implicit: a correction that moves a date leaves the body stale, and ADR-0016 preserves the previous
value of the field that *is* written either way.

**What changed in this document as a result**: R1.8 is rewritten in full above, with its own
accepted-cost paragraph and three scenarios covering the date-wins, content-fallback, and
two-dates-ask cases. No other requirement in this change depended on the "every field" rule — R1.9
(the pre-image) already used plural language ("every field about to change") that holds unchanged
under a plan of exactly one edit, and R1.7 (the per-field update methods) is unaffected either way,
since the methods it mandates are called at most once per correction now rather than up to three
times.

### Conflict 4 — `spec.md` R3.1 required `nooma capture` to open the vault directly and hold the write lock; `design.md` D11 made it an HTTP client. **RESOLVED — HTTP client. Owner decision.**

**Evidence for "opens the vault directly"** (this spec's prior revision): R3.1, verbatim: *"opens
the vault directly — the same way `status`/`doctor` already do, with no running `nooma serve`
instance required,"* and *"holds the vault's single-writer lock the same way any write path must."*
The precedent cited was `status`/`doctor`'s own CLI convention of working without a running server.

**Evidence the precedent does not transfer** (`design.md` C8, verified independently against the
tree rather than taken on the design's word): `cmd/nooma/status.go:27`'s own comment states, "It is
read-only in the strong sense: it never takes the write lock," and `runStatus` calls
`vaultlock.ReadHolder` — it reads who holds the lock, it never takes it. `status` and `doctor` are
readers; `capture` is a writer. `runServe` holds the vault's exclusive lock for its entire
lifetime, and serving is the product's normal deployment state (doc 01's Layer 2 makes the served
UI the lean-back surface), so a lock-taking `nooma capture` would fail precisely when the product
was doing what it is meant to do most of the time.

**Position, design.md's own words**: *"The precedent R3.1 cites does not transfer... A lock-taking
`nooma capture` would refuse every time the product was running normally."* Design recommended the
HTTP-client shape and named its own cost plainly: "`nooma capture` requires a running server and
fails with a message saying so."

**Owner decision**: `nooma capture` does `POST /capture` against the running server. Two
consequences specified in R3.1 above as a direct result: capture needs the server running, and —
per R2.9–R2.11's auth requirements — it needs the token whenever the bind is not loopback.

**What changed in this document as a result**: R3.1 is rewritten in full above. The open item this
spec previously carried — "whether `nooma capture` can run concurrently with a running `nooma
serve` instance" — is removed from `## 10`, not left dangling: the question is answered by
construction once capture is itself an HTTP client of the server rather than a second writer
contending for the same lock.

### Conflict 5 — R6.3 required a permanently-unembedded Cloud vault be statable; R7.3/R7.4 forbade the only port surface that could state its runtime half. **RESOLVED — the exception was a debt Phase B recorded, not a new one. `EmbeddingRepo` named in both requirements.**

**This conflict is different in kind from 1, 3, and 4.** It is not the owner overruling a spec
position, nor the owner scheduling a gap prose had claimed but no PR built. It is a disagreement
between two requirements inside this document itself, filed by `design.md` as its own C10 and
**deliberately left unresolved there**: *"One, C10, is open and this design does not resolve it: it
is a disagreement between two requirements inside `spec.md` itself, and picking a side would be
exactly what this section exists to prevent"* (`design.md:150-152`). Escalating rather than
resolving was the correct call — a design document is not the place to settle what its own spec
requires.

**Side A — the outcome is required.** R6.3, unchanged in this revision's intent: "A Cloud vault
whose captures come back unembedded is something a test can state, not something a user discovers
by wondering why search feels thin." Read literally against the tree, satisfying the *runtime* half
of that sentence — a vault already deployed, not only a build-time conformance test — needs a way
to ask a live vault whether its live units are embedded, and nothing in `internal/ports` could
answer that question before this revision.

**Side B — the mechanism was forbidden.** R7.3's prior revision permitted edits to already-delivered
`internal/ports/**` files only for "R1.2's scored fusion, R1.3's gate, R1.7's two new `UnitRepo`
methods, and R6.1's new provider client," and R7.4 enumerated the `ports` files that stay untouched,
sanctioning exactly two edits (`unitrepo.go`, `decisionlog.go`) — neither list named
`internal/ports/embeddingrepo.go`, the one file a runtime coverage check would need to grow.

**Resolution, from evidence in Phase B, not a new exception.** `m1b-pipeline/design.md:790-793`
recorded this exact deferral by name, with its own stated condition and its own named recipient:
*"This design ships **no** consistency-query method in Phase B, deliberately: `UnembeddedLive` or
similar would be a port method whose only caller is a test, which is the shape `m1a-substrate` D7
rejected for `TranscriptionProvider`. **The obligation is recorded for whoever ships `doctor`'s
consistency check.**"* And `docs/03-data-model.md:306-307` already promises, independent of this
change and predating it, that "`nooma doctor` runs `PRAGMA integrity_check` + units↔embeddings↔fts
consistency" — an existing v1 commitment, not a new one this spec invents. Phase B's own stated
condition for withholding the method — that it would have no caller but a test — discharges itself
the moment `doctor` exists as a real caller, which is exactly what R6.3 (and PR 16) makes true. R7.3
and R7.4 were written without knowledge of that deferral, before Phase B's own design document was
read for this purpose; naming the exception is a correction of that omission, not a new carve-out
being granted.

**What changed in this document as a result**: R6.3 gains a `MUST` naming
`ports.EmbeddingRepo.CountLiveWithoutEmbedding` (or equivalent) and the `nooma doctor` check that
uses it, with both authorities cited inline. R7.3 gains the same exception and the same citation,
framed explicitly as "a debt discharged, not a scope carved." R7.4's file enumeration is corrected
in the same edit — see the next paragraph, filed as its own defect rather than folded silently into
this one.

**A second, independent defect, found while resolving this conflict, not by `design.md`'s own
audit.** R7.4's prior revision enumerated the untouched `internal/ports` files as `decisionlog.go`,
`relationrepo.go`, `provider.go`, `clock.go`, and `lexicalsearch.go`, and named two sanctioned edits
— `unitrepo.go` and `decisionlog.go`. `embeddingrepo.go` appeared in neither list. The enumeration
read as exhaustive over every file in the package and was not: a reader checking R7.4 alone, without
also reading R7.3's prose MUST NOT, would have found no mention of `embeddingrepo.go` at all and
could reasonably have concluded it was untouched by omission rather than by oversight. R7.4 is
rewritten above to name every `internal/ports/*.go` file that existed before this change, each
either as untouched or as carrying a named, sanctioned edit — an enumeration is only worth having if
it is actually complete.

**No conflict remains open in this document as of this revision, except Conflict 2** — which was
filed as open and stands open, unchanged by this revision's other edits.
