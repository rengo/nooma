# Tasks — m3e: the pending question

Implementation task list for `m3e-pending-question`, derived from `spec.md` (R1–R10) and
`design.md` (§1–§12), both read in full before this document, matched to `m3a-prospection` and
`m3b-trigger-timer`'s own tasks-file granularity per instruction. Design §7 fixes the slicing —
**six PRs**, stacked, ~1,260 budgeted impl+docs lines — treated as authoritative over any
disagreement with spec's own wording, per that same precedent ("design owns the slicing").

Chain strategy **`stacked-to-main`** (design §7's own explicit choice). Order: `1 → 2 → 3 → 4 → 5
→ 6`, linear. PR 1 must land first and alone — proposal §9's own asymmetry: the migration does
not roll back, and every PR after it is cleanly revertible on its own.

**Strict TDD is active.** Every behavioral task states the two-commit RED/GREEN shape. **Inside
every PR the conformance/L1/L2 test commit is strictly ahead of the implementation commit** —
`sdd-verify` reads the PR's `git log` and reports an inversion as CRITICAL. Every PR touching
`internal/core/**` carries its `docs/02-cognitive-core.md` delta in the same PR (`CLAUDE.md`
non-negotiable #1). For load-bearing RED tasks, a `Mutation:` line names the code change that
must make the test fail — a task without one for those cases is not considered checkable from the
tree alone.

---

## Findings — spec/design disagreements found in this session (report, don't paper over)

**F1 — spec R5 and design §3.6 disagree about the confirm signal's ordering, and the disagreement
is total, not cosmetic.** Spec R5's MUST reads: *"the signal is emitted before the `Upsert`,
mirroring `RejectRelation`'s own ordering discipline... the worst case of a signal for a raise
that then failed is a learning signal ahead of a retryable write, never evidence for something
that reverted."* Design §3.6 ships the opposite, argued at length: *"the signal is emitted AFTER
the raise, which is the OPPOSITE of I10's order, for the same underlying reason pointed the other
way... the effect is idempotent — `ConfirmedConfidence` applied twice is `ConfirmedConfidence`
applied once — so re-running the raise costs nothing, while a signal emitted for a raise that then
failed is evidence the learning module would tune on forever."* **Resolved in design's favor**:
design's reasoning directly engages the property that makes I10's before-ordering correct there
(an irreversible delete) and shows it does not transfer to an idempotent raise. Tasks 6.3–6.4
below ship design's AFTER ordering. Flagged because it reverses a literal spec MUST rather than
merely renaming something, per `m3b` G21's own posture toward an unmet MUST.

**F2 — spec R1 and design §3.4 disagree about which package computes the Uncertain verdict, and
design's own rejected-alternatives table shows the reasoning explicitly.** Spec R1's MUST reads:
*"computed by the caller from the same `resolvedThresholds` it already reads... never inside
`core/consolidation` or `core/relation`."* Design §3.4 rejects exactly that option (*"the same
decision computed twice in two packages, and a future edit to `Decide`'s boundary would have to
find both — the 'one rule in two languages' drift"*) and instead ships `ProposedRelation.Band`, a
field computed **inside** `ProposeRelation` — inside `core/consolidation`, the package spec R1
names as forbidden. **Resolved in design's favor**: `ProposeRelation` already computes
`relation.Decide` today and discards it (`connect.go:174-176`, per design §1's own verified
ground truth); the field costs one line and closes a real double-maintenance hazard. Tasks
2.3–2.4 ship `Band` inside `core/consolidation`, not a `brain`-side recomputation.

**F3 — spec R5's `ConfirmedConfidence` signature and design §3.3's shipped signature differ.**
Spec: *"a pure function computing the confirmed confidence from `(current, floor float64)
float64`."* Design §3.3's code: `func ConfirmedConfidence(current float64, t Thresholds) float64`
— a `relation.Thresholds` struct, not a bare `floor float64`. **Resolved in design's favor**, per
this document's own instruction to use design's exact fixed signatures: `t.Surface` is the field
design's body reads, and the struct carries `Persist` alongside `Surface` for no cost to this
function's one comparison. Task 2.1 tests the struct-taking form.

**F4 — spec R3 and design §3.5 disagree about which column orders the digest's queue, found
while implementing PR 5.** Spec R3's MUST reads: *"exactly one — the oldest by `asked_at` — is
appended"*, and its own scenario is written over *"two `pending_questions` rows open at digest
time, asked at different instants."* Design §3.5 ships the opposite key: *"the tie-break is
`created_at`, then `id` — FIFO, not confidence."* **Resolved in design's favor**, and the
disagreement is not a preference — spec R3's wording is unreachable. The digest reads
`Unasked` (queued rows), whose `asked_at` is NULL by construction, and `MarkAsked`'s own
`asked_at IS NULL` precondition means a question is asked exactly once and never re-rendered, so
two questions "asked at different instants" can never compete for one digest line. Ordering a
set of NULLs by `asked_at` orders nothing. Tasks 5.1–5.2 ship `created_at`, then `id`, which is
also what `ports.PendingQuestionRepo.Unasked`'s own doc comment already promised when PR 3
shipped it. Spec R3's scenario maps onto the shipped behaviour as *two queued questions, created
at different instants → the older is asked; the newer stays queued for a later digest*, which is
what `TestDigest_AsksExactlyOneQuestionOldestFirst` asserts.

No other disagreement was found: R2 (state machine), R3's cap itself, R4 (disambiguation), R6
(reject), R7 (expiry), R8 (unmatched), R9 (stale comment) and R10 (doc sync) are consistent
between the two artifacts as read.

---

## Owner-review items carried forward (design §11 — decided defaults, ship if the owner is silent)

| # | Item | Decided default | PR / Task |
|---|---|---|---|
| R1 | Q1's ruling ships inside ADR-0027 as a `Related decision`, not `0028` | Inside `0027` | PR 1, task 1.5 |
| R2 | `capture.go`'s own relation judge creates no question | Consolidate's path only (proposal §3.2) | Not tasked — explicit non-goal, proposal §3.4 |
| R3 | `ProposedRelation` gains `Band` | The field, inside `core/consolidation` | PR 2, tasks 2.3–2.4 (see **F2**) |
| R4 | The confirm path's signal is emitted after the raise | After | PR 6, tasks 6.3–6.4 (see **F1**) |
| R5 | `RejectRelation` narrows to `(ctx, relationID, now)` | Narrowed | PR 6, tasks 6.5–6.6 |
| Q1 (design §11) | The queued (unasked) pool is unbounded | Not decided — no second bound in `m3e` | Named in PR 5's digest tasks, not mitigated |
| Q2 (design §11) | Does a re-ask ever happen after expiry | No | Not tasked — nothing creates a second question |

---

## PR 1 — `feat/store-pending-questions-migration` (~180 impl+docs)

Depends on nothing outside this change. Forward-only and inert — nothing reads the table until
PR 3. **Must land first and alone** (proposal §9's rollback asymmetry).

- [ ] **1.1** `internal/store/sqlite/migrations/0004_pending_questions.sql` (new) — table
      `pending_questions(id TEXT PRIMARY KEY, kind TEXT NOT NULL, relation_id TEXT NOT NULL,
      created_at TEXT NOT NULL, asked_at TEXT, resolved_at TEXT, resolution TEXT)`; partial index
      `idx_pending_questions_unasked` on `(created_at)` `WHERE asked_at IS NULL AND resolved_at IS
      NULL`; partial index `idx_pending_questions_open` on `(asked_at)` `WHERE asked_at IS NOT
      NULL AND resolved_at IS NULL`. **No `CHECK` constraint** on `kind`/`resolution` (design §3.1
      — no table in this schema carries one, and `kind` is designed to grow). **No foreign key**
      on `relation_id` (design §3.1's three-rejected-behaviors table — CASCADE deletes the audit
      of a deletion, RESTRICT strands an emitted signal, SET NULL destroys the one column the
      table exists for).
      Requirement: R1, R2 (spec); ADR-0027.
- [ ] **1.2** `docs/03-data-model.md` — add the `pending_questions` table section, matching `0004`
      exactly (column names, types, both partial indexes, the no-FK note).
      Requirement: R10.
- [ ] **1.3** `test/conformance/schema_doc_test.go` — extend the anchor list with
      `pending_questions`'s entry.
      Requirement: R10; design §3.1.
      **Mutation**: remove the new anchor row without removing the table from `docs/03` — the
      anchor test must fail on the drift, proving the anchor is load-bearing rather than
      decorative.
- [ ] **1.4** Regenerate `internal/store/sqlite/testdata/schema/*.golden` via its make target;
      confirm the diff is limited to the one new table's rows — never hand-edited.
      Verify: `make store-schema-golden && git diff --stat internal/store/sqlite/testdata/schema/`.
      Requirement: R2; design §3.1.
- [ ] **1.5** `docs/adr/0027-pending-question-store.md` (new), `Accepted` — Decision: a question
      the brain asked lives in a dedicated `pending_questions` store while it waits for an
      answer, not `triggers` and not `decision_log` (proposal §5). Alternatives: `triggers`
      (resolution vocabulary `engaged|declined|self_healed` would misdescribe a confirmation;
      single `unit_id` cannot hold two endpoints) and `decision_log` (the glass box is not a state
      store; no queryable worklist for M4). Consequences: migration `0004`, forward-only; `kind`
      column carrying one member; no FK on `relation_id`; no `Delete`-prefixed method. **A
      `Related decision` section carries Q1's ruling** (`confirmed_floor := the relation type's
      own min_confidence_to_surface`) rather than a separate `0028` — owner-review **R1**'s
      decided default.
      Requirement: ADR-0027; proposal §5.
- [ ] **1.6** `docs/adr/README.md` — add the `0027` index row.
      Requirement: ADR-0027.
- [ ] Verify (PR-level): `make check-all`; confirm diff touches only
      `internal/store/sqlite/migrations/0004_pending_questions.sql`, `docs/03-data-model.md`,
      `test/conformance/schema_doc_test.go`, `internal/store/sqlite/testdata/schema/*.golden`,
      `docs/adr/0027-pending-question-store.md`, `docs/adr/README.md`. No `docs/02-cognitive-core.md`
      delta (no `internal/core` touch). Target ≤180 impl+docs lines.

---

## PR 2 — `feat/core-relation-confirmed-confidence` (~120 impl+docs)

Independent of PR 1's runtime effect; ordered after it only by the migration's own must-land-first
rule. Ships `ConfirmedConfidence`, `ProposedRelation.Band`, `connect.go:151`'s correction. Triggers
`docs-sync` (proposal R7).

- [ ] **2.1** Commit 1 (RED): `internal/core/relation/confirm_test.go` (new) — three properties,
      swept over `c` and `t` including NaN: (a) `Decide(ConfirmedConfidence(c, t), t) == Asserted`
      for every input — *confirming always leaves the band*; (b)
      `ConfirmedConfidence(ConfirmedConfidence(c, t), t) == ConfirmedConfidence(c, t)` —
      idempotent; (c) `ConfirmedConfidence(c, t) >= c` for every non-NaN `c` — confirming never
      lowers a confidence.
      **Red**: `undefined: relation.ConfirmedConfidence`.
      Stub: `func ConfirmedConfidence(current float64, t Thresholds) float64 { return current }` —
      compiles; property (a) fails first for `c` below `t.Surface`.
      Requirement: R5 (spec), resolved per **Finding F3** (takes `Thresholds`, not a bare
      `floor float64`).
      **Mutation**: swap the body to `return math.Max(current, t.Surface)` — the NaN sweep in
      property (a) must fail, since `math.Max` propagates NaN while the shipped `!(current >
      t.Surface)` form lands NaN on the floor (design §3.3's own stated reason for the unusual
      form).
- [ ] **2.2** Commit 2 (GREEN): implement `internal/core/relation/confirm.go` — exactly design
      §3.3's body: `if !(current > t.Surface) { return t.Surface }; return current`. No I/O, no
      `time.Time` parameter, imports nothing but its own package (non-negotiable #3).
      Verify: `go test ./internal/core/relation/...`.
      Requirement: R5; design §3.3.
- [ ] **2.3** Commit 1 (RED): `internal/core/consolidation/connect_test.go` (extend) —
      `ProposeRelation` returns `Band ∈ {Uncertain, Asserted}` and never `Discard` on `ok == true`,
      table-driven over the band boundaries (`m3a`'s own boundary-table style).
      **Red**: `undefined: ProposedRelation.Band`.
      Stub: add the field, always assign the zero value — compiles; the boundary case expecting
      `Uncertain` fails first.
      Requirement: R1 (spec), resolved per **Finding F2** (`Band` lives inside `core/consolidation`,
      not recomputed by `brain`).
      **Mutation**: assign `Band` from a second, independently-written `Decide`-like comparison
      instead of the value `ProposeRelation` already computes at `connect.go:174-176` — a future
      edit to `Decide`'s own boundary would silently desync the two; this task's fixture pins
      `Band` against `relation.Decide`'s own output, not a re-derived literal.
- [ ] **2.4** Commit 2 (GREEN): assign `Band` from the `relation.Decide` call `ProposeRelation`
      already makes and today discards (`connect.go:164-186`).
      Verify: `go test ./internal/core/consolidation/...`.
      Requirement: R1; design §3.4.
- [ ] **2.5** `internal/core/consolidation/connect.go:150-151` — correct the stale doc comment
      (*"the asking is M3's"*) to name `m3e`, in this PR (R9's own MUST: same PR as the code that
      discharges it).
      Requirement: R9.
- [ ] **2.6** `docs/02-cognitive-core.md` §4 amendment — `confirmed_floor` reads as an alias for
      the relation type's own `min_confidence_to_surface` (Q1's ruling), not an undefined term.
      Requirement: R10; non-negotiable #1 (same-PR doc delta for an `internal/core` change).
- [ ] **2.7** Purity/lint: `golangci-lint run` (`core-purity` — `confirm.go` imports nothing beyond
      its own package; `forbidigo` — no `time.Now`/`rand.*`).
      Requirement: `nooma-core` hard rules 1–2.
- [ ] Verify (PR-level): `make check-all`; confirm diff touches only
      `internal/core/relation/confirm{,_test}.go`,
      `internal/core/consolidation/connect{,_test}.go`, `docs/02-cognitive-core.md`. Target ≤120
      impl+docs lines.

---

## PR 3 — `feat/ports-store-pending-questions` (~300 impl+docs — genuine risk, see below)

Depends on PR 1 (migration). Ships the port, both vocabularies, `RelationRepo.ByID`, the SQLite
implementation with both joins and the `WHERE EXISTS` insert guard, memrepo + repocontract,
`store_api.golden`. **L3 owns N1** (design §3.4/§3.1) and the FK's deliberate absence.

- [ ] **3.1** Commit 1 (RED): `test/support/repocontract/pendingquestionrepo.go` (new) —
      `RunPendingQuestionRepo(t, newRepo)`: `Create`+`Unasked` round trip; `Create` with an unknown
      `relation_id` returns `ErrRelationNotFound` and inserts nothing (N1's structural guard);
      `MarkAsked` moves a row from `Unasked` into `Open`, precondition `asked_at IS NULL` in the
      `WHERE` clause only; `Confirm`/`Reject`/`Expire` each require `resolved_at IS NULL`, set
      `resolved_at` + their own resolution literal in one statement; `Open` orders most-recently-
      asked first; **no `Delete`/`Remove`/`Purge`/`Drop`/`Destroy`-prefixed method**, asserted via
      `reflect.TypeOf((*ports.PendingQuestionRepo)(nil)).Elem()`'s own `NumMethod()`/`Method(i).Name`
      (I03, mirroring `m3b`'s own trigger/timer reflection sweep).
      **Red**: `undefined: ports.PendingQuestionRepo`, `ports.PendingQuestion`,
      `ports.RelationQuestion`, `memrepo.NewPendingQuestionRepo`.
      Stub: minimal interface + a no-op `memrepo` fake — compiles; the `Create`+`Unasked` round
      trip fails first.
      Requirement: R1, R2.
      **Mutation**: replace the reflection scan's forbidden-prefix set with the empty set — a
      hypothetical `DeleteExpired` method would then compile and pass undetected.
- [ ] **3.2** Commit 2 (GREEN): implement `internal/ports/pendingquestionrepo.go` per design
      §3.2's exact shape — `QuestionKind` (`QuestionKindRelation`), `AllQuestionKinds()`;
      `QuestionResolution` (`QuestionConfirmed`/`QuestionRejected`/`QuestionExpired`),
      `AllQuestionResolutions()`; `PendingQuestion{ID, Kind, RelationID, CreatedAt}` (write shape,
      no `asked_at`/`resolved_at`/`resolution` field — a row born already-answered is
      unrepresentable); `RelationQuestion{ID, RelationID, RelationType, FromUnitID, ToUnitID,
      FromContent, ToContent, CreatedAt, AskedAt *time.Time}` (read shape); the seven-method
      `PendingQuestionRepo` interface (`Create`, `Unasked`, `Open`, `MarkAsked`, `Confirm`,
      `Reject`, `Expire` — no method with no caller, no method taking a `resolution` parameter);
      `ErrQuestionNotFound`, `ErrQuestionStatusConflict`. `test/support/memrepo/pendingquestions.go`
      — the fake, preconditions enforced under a mutex (`UnitRepo`'s own fake pattern).
      Verify: `go test ./test/support/repocontract/... ./test/support/memrepo/...`.
      Requirement: R1, R2; design §3.2.
- [ ] **3.3** `repocontract/pendingquestionrepo.go` (continued) — `AllQuestionKinds()`/
      `AllQuestionResolutions()` pinned to `0004`'s own column comment vocabulary, with `len()`
      assertions guarding against a silently dropped member.
      Requirement: R2; design §3.1 (the vocabulary-pin discipline `m3b`'s G4 established).
      **Mutation**: reorder or drop one member in the Go-side slice — fails on the pinned
      comment-order match, independent of the `len()` guard.
- [ ] **3.4** Commit 1 (RED): `test/support/repocontract/relationrepo.go` (extend) —
      `RelationRepo.ByID` returns the WHOLE row (`Strength`, `CreatedBy`, `CreatedAt` included, not
      only `Confidence`) or `ErrRelationNotFound` for an unknown id.
      **Red**: `undefined: ports.RelationRepo.ByID`.
      Stub: add the method returning the zero `Relation` unconditionally — compiles; the
      known-id case fails first (all fields zero instead of the fixture's own values).
      Requirement: design §3.2 (the confirm path's full-row need, I07).
- [ ] **3.5** Commit 2 (GREEN): implement `ByID` in `test/support/memrepo/relations.go` and
      `internal/store/sqlite/relationrepo.go`.
      Verify: `go test ./test/support/repocontract/...`.
      Requirement: design §3.2.
- [ ] **3.6** Commit 1 (RED): `internal/store/sqlite/pendingquestionrepo_integration_test.go`
      (build tag `integration`) — `RunPendingQuestionRepo` against a real migrated vault; a
      dedicated case asserting `Create` against an unknown `relation_id` returns
      `ErrRelationNotFound` and the raw table stays empty (N1's guard, proven at the one place it
      can fail); a raw-SQL fixture proving the FK's deliberate absence — reject a relation through
      a raw delete and confirm the question row **survives**, resolved-or-not, with `relation_id`
      still readable (design §3.1's own stated purpose).
      **Red**: `undefined: sqlite.NewPendingQuestionRepo`.
      Stub: a constructor whose every method is a no-op — compiles; the `Create`+`Unasked` round
      trip fails first.
      Requirement: R1, R2; design §3.1, §3.4 (N1).
      **Mutation**: drop the `WHERE EXISTS(SELECT 1 FROM relations WHERE id = ?)` guard from
      `Create`'s `INSERT` — the unknown-`relation_id` case must then insert successfully instead
      of returning `ErrRelationNotFound`, the exact regression N1 exists to catch.
- [ ] **3.7** Commit 2 (GREEN): implement `internal/store/sqlite/pendingquestionrepo.go` —
      `Create` via `INSERT ... SELECT ... WHERE EXISTS(SELECT 1 FROM relations WHERE id = ?)`,
      `ErrRelationNotFound` on zero rows affected; `Unasked`/`Open` both inner-join `relations`
      (R8's mitigation — a question whose relation is gone is skipped, not failed, on either read);
      `MarkAsked`/`Confirm`/`Reject`/`Expire` as single `UPDATE ... WHERE id = ? AND <precondition>`
      + `requireRowAffected(res, ports.ErrQuestionStatusConflict)`.
      Verify: `go test -tags=integration ./internal/store/sqlite/... -run PendingQuestion`.
      Requirement: R1, R2, R8; design §3.1, §3.2.
- [ ] **3.8** L3: `EXPLAIN QUERY PLAN` on `Unasked` and `Open` confirms both partial indexes are
      used.
      Requirement: design §3.1 (the two indexes' own purpose).
- [ ] **3.9** `test/conformance/i03_units_never_deleted_test.go` — extend `sweptPortsRepoTypes`
      with `PendingQuestionRepo` (`m3b`'s own precedent for a freshly added port); confirm no
      `Delete`-prefixed method is found, `RelationRepo.Delete` staying the one carve-out (2026-08-24
      owner ruling).
      Requirement: R2 (spec — I03, "no carve-out" for the new port).
- [ ] **3.10** `testdata/schema/store_api.golden` — regenerate; diff limited to
      `PendingQuestionRepo`'s new `type`/`func` lines plus `RelationRepo.ByID`.
      Verify: `make store-api-golden && git diff --stat testdata/schema/store_api.golden`.
      Requirement: R2.1's regeneration discipline, applied here.
- [ ] **3.11** Purity/lint: `golangci-lint run` (`ports-purity` — `internal/ports` imports no new
      package; `internal/store` remains the only importer of `internal/store/sqlite`).
      Requirement: `nooma-core` hard rules 1–2; design §4.
- [ ] Verify (PR-level): `make check-all`; confirm diff touches only
      `internal/ports/{pendingquestionrepo,relationrepo}.go`,
      `internal/store/sqlite/{pendingquestionrepo,relationrepo}{,_integration_test}.go`,
      `test/support/memrepo/{pendingquestions,relations}.go`,
      `test/support/repocontract/{pendingquestionrepo,relationrepo}.go`,
      `test/conformance/i03_units_never_deleted_test.go`, `testdata/schema/store_api.golden`. No
      `docs/02-cognitive-core.md` delta (ports/store are I/O, not core). Target ≤300 impl+docs
      lines — **genuine risk, flagged by design §7**. **If measured lines threaten 400**, apply
      design's own pre-drawn cut: port-plus-fakes (tasks 3.1–3.5) \| SQLite-plus-golden (tasks
      3.6–3.11), as two PRs — report before splitting, per house convention.

---

## PR 4 — `feat/brain-queue-relation-question` (~110 impl+docs)

Depends on PR 2 (`Band`) and PR 3 (the port). Ships the Uncertain branch in `judgeAndPersistPair`,
one `decision_log` action, wiring. **I09's storing half; `test/conformance/i09_*.go` is created
here as RED and turned GREEN in PR 5** (design §8's own split).

- [x] **4.1** Commit 1 (RED): `internal/brain/consolidate_test.go` (extend) — a judged pair with
      `Band == Uncertain` writes exactly one `pending_questions` row (`kind = relation`,
      `relation_id` = the persisted relation's id) in the same pass as `Upsert` +
      `ActionConnectRelationPersisted`; a `Band == Asserted` pair writes none; the write is
      recorded by `ActionConnectQuestionCreated` (I12, one row per effect).
      **Red**: `undefined: ports.ActionConnectQuestionCreated`.
      Stub: add the constant with no writer; `consolidateRunner` gains an unused `questions`
      field — compiles; the Uncertain-writes-a-question case fails first (zero rows, one
      expected).
      Requirement: R1 (spec).
      **Mutation**: gate the branch on `proposed.Band == relation.Asserted` (inverted) instead of
      `!= relation.Uncertain` — an Uncertain pair then writes nothing and an Asserted one writes a
      question; both halves of the fixture must fail.
- [x] **4.2** `test/conformance/i09_uncertain_band_asked_test.go` (new) — the invariant's first
      real conformance test, both halves per design §8: (a) a judgment landing in
      `[Persist, Surface)` stores the relation AND produces exactly one `pending_questions` row —
      **satisfiable by this PR alone**; (b) its next due digest's rendered text names both
      endpoints — **genuinely RED until PR 5** ships the digest's second source and
      `renderDigest`'s question line. Both subtests live in this one file from this PR forward;
      part (b) staying red at the end of this PR is expected and disclosed, not a defect.
      Requirement: I09 (`docs/06-harness.md`); R1, R3.
      **Owner-consulted**: part (b) is written as a real `t.Fatal` (not `t.Skip`) — `go test
      ./...` / `make check-all` are expected to report this one subtest FAIL until PR 5 lands,
      per this task's own "genuinely RED" wording; confirmed with the owner over `t.Skip`
      (keeps every PR green, but has no precedent in `test/conformance` and contradicts "stays
      red"). Disclose in PR 4's own description.
- [x] **4.3** Commit 2 (GREEN): implement the branch in `judgeAndPersistPair`, after the existing
      `Upsert` and its `ActionConnectRelationPersisted` row, exactly per design §3.4's snippet:
      `if proposed.Band != relation.Uncertain { return nil }`; construct
      `ports.PendingQuestion{ID: r.ids.New(), Kind: ports.QuestionKindRelation, RelationID: rel.ID,
      CreatedAt: now}`; `r.questions.Create(ctx, q)`; record `ActionConnectQuestionCreated`.
      `consolidateRunner` reads no clock — `now` is the instant `ConsolidateService` already
      handed down (`brain_single_clock_read_test.go` satisfied by construction).
      Verify: `go test ./internal/brain/... -run Consolidate` and
      `go test ./test/conformance/... -run I09` (part (a) subtests pass; part (b) stays red,
      confirmed by name in the test output).
      Requirement: R1; design §3.4.
- [x] **4.4** `cmd/nooma/wiring.go` — wire `questions ports.PendingQuestionRepo` into
      `consolidateRunner`'s constructor.
      Requirement: design §4 (layout table).
- [x] **4.5** Purity/lint: `golangci-lint run` (`brain-boundary` — no new clock read).
      Requirement: `docs/06-harness.md`'s single-clock-read rule.
- [x] Verify (PR-level): `make check-all` — green except
      `TestI09_QuestionIsNotYetNamedInTheDigest`, deliberately red per task 4.2's own
      "Owner-consulted" note above (disclose in the PR description). Diff also touches, beyond
      this task's own idealized list — every caller of `NewConsolidateService`'s widened
      signature, mechanical one-argument additions only:
      `internal/scheduler/scheduler_test.go`, `test/e2e/consolidation_demo_test.go`,
      `test/integration/consolidate_expire_incomplete_test.go`,
      `test/integration/consolidate_pattern_eval_load_test.go`,
      `test/support/repocontract/decisionlog.go` (the `AllDecisionActions` count, all five m3e
      members at once rather than incrementally per PR). Also fixed, found only by
      `make check-all`'s own L3 leg (not `make check`): four migration-0004 fallout fixes left
      over from PR 1 — `test/integration/migrate_test.go` and
      `test/integration/concurrent_open_test.go`'s hardcoded `PRAGMA user_version`/
      `VersionError.BinaryVersion` literals (3→4), and
      `test/integration/schema_golden_anchor_test.go`'s hand-written required-object list
      (missing `pending_questions` + its two partial indexes). No `docs/02-cognitive-core.md`
      delta (`internal/brain` is not `internal/core`). Target ≤110 impl+docs lines — not
      re-measured against this wider actual diff; revisit before opening the PR.

---

## PR 5 — `feat/brain-digest-asks` (~300 impl+docs — genuine risk, see below)

Depends on PR 3 (the port) and PR 4 (questions exist to ask about). Ships the digest's second item
source, the one-question cap (Q2), the low-energy rule, the FIFO tie-break, `renderDigest`'s
question paragraph, the expiry sweep (Q4). **I09's asking half — `i09_*.go` turns GREEN here.**

- [x] **5.1** Commit 1 (RED): `internal/brain/digest_test.go` (extend) — zero open questions →
      the digest is unchanged from today's trigger-only shape; N queued questions → exactly one
      surfaced, the **oldest by `created_at`, then `id`** (FIFO, never confidence — design §3.5's
      own rationale: ranking by confidence would ask first about the relation closest to
      asserting itself anyway); `MarkAsked` called exactly once per digest; a digest with zero
      trigger items and one open relation question is **still sent** — the "an empty digest is not
      sent" rule is about `Carry`'s own items having nothing to say, and a pending question is not
      nothing to say (R3's own MUST); a low-energy digest carries **zero** questions and the queue
      is untouched.
      **Red**: `undefined:` the second item source / `nextQuestion` helper on `checkRunner`.
      Stub: a helper returning `nil` unconditionally — compiles; the one-queued-question-surfaces
      case fails first.
      Requirement: R3 (spec).
      **Mutation**: read `pending_questions` sorted by `id` alone, dropping the `created_at`
      tie-break — a fixture with two questions created in the same millisecond but distinct ids in
      reverse creation order must still surface the actually-oldest one; catches an
      id-only-sort regression the FIFO property depends on.
- [x] **5.2** Commit 2 (GREEN): restructure `assembleDigest` per design §3.5's pipeline —
      `expireStaleQuestions` runs BEFORE anything is sent; `question := nil; if !low { question =
      r.nextQuestion(ctx) }`; the "0 items AND no question" early-return check widens to include
      `question == nil`; `channel.Send(renderDigest(carry, pending, question))`; on success,
      `questions.MarkAsked(...)` alongside `triggers.Surface(...)`; record
      `ActionCheckDigestQuestionAsked`.
      Verify: `go test ./internal/brain/... -run Digest`.
      Requirement: R3; design §3.5.
- [x] **5.3** Commit 1 (RED): `digest_test.go` (continued) — `renderDigest(carry, pending,
      question)` appends the exact line shape naming both endpoints, matching doc 02 §4's own
      wording: `"One more — I linked \"<from>\" with \"<to>\". Are they related?"`; both
      `FromContent`/`ToContent` truncated to `questionSnippetRunes` (rune-aware, ellipsis on
      truncation — `units.content` is unbounded).
      **Red**: `undefined: questionSnippetRunes`; `renderDigest`'s signature does not yet accept a
      question argument.
      Requirement: R3's line-shape MUST.
- [x] **5.4** Commit 2 (GREEN): implement `renderDigest`'s question paragraph and
      `questionSnippetRunes` (brain-side, not core — `calibration_doc_test.go` matches only
      `internal/core/<pkg>.<Symbol>`, `digestHistoryDays` the shipped precedent for a brain-side
      bound needing no §13 row).
      Verify: `go test ./internal/brain/... -run RenderDigest`.
      Requirement: R3; design §3.5.
- [x] **5.5** Commit 1 (RED): `digest_test.go` (continued) — `expireStaleQuestions`: a question
      surfaced in `MaxDigestDeferrals - 1` digests stays open (still asked, still unresolved);
      surfaced in `MaxDigestDeferrals` digests transitions to `resolution = expired`, `resolved_at`
      set — a state transition, never a delete (non-negotiable #6); an expired question is
      excluded from both this task's own render check and R4's disambiguation pool (forward
      reference to PR 6).
      **Red**: `undefined: expireStaleQuestions`.
      Stub: a no-op returning `(0, nil)` — compiles; the `MaxDigestDeferrals`-surfaced case fails
      first (still open, expected expired).
      Requirement: R7 (spec).
      **Mutation**: count surfaces from a wall-clock window instead of
      `decision_log`'s own `ActionCheckDigestSent` rows — a fixture whose digests were sent on
      three consecutive mornings but with an artificially shifted `now` between the count and the
      assertion must still expire correctly only under the row-count form, never a duration form.
- [x] **5.6** Commit 2 (GREEN): implement `expireStaleQuestions(ctx, history, now, commit)` per
      design §3.5 — count is `len(rows where Action == ActionCheckDigestSent && OccurredAt.After(
      *q.AskedAt))` over the `history` slice this pass already read
      (`digestHistoryDays = MaxDigestDeferrals + 2`, no additional read); `>= MaxDigestDeferrals`
      expires; runs before the send, so the digest going out now is not counted against the
      question it may be carrying; `!commit` (`nooma check --dry-run`) counts and writes nothing.
      Record `ActionCheckQuestionExpired` per expired question (I12).
      Verify: `go test ./internal/brain/... -run Expire`.
      Requirement: R7; design §3.5.
- [x] **5.7** `test/conformance/i09_uncertain_band_asked_test.go` — confirm part (b) is now GREEN:
      a question's next due digest names both endpoints, end to end from `judgeAndPersistPair`
      through `assembleDigest`.
      Requirement: I09; R1, R3.
- [x] **5.8** L3 (`test/integration/` or equivalent): `SELECT DISTINCT resolution FROM
      pending_questions` after a real pass yields only `AllQuestionResolutions()` members — the
      constraint the schema does not carry (`m3b`'s Risk A posture, applied to this new table).
      Requirement: R2 (vocabulary pin, no `CHECK`).
- [x] **5.9** `checkRunner` gains one field, `questions ports.PendingQuestionRepo`,
      **nil-tolerant** exactly as `channel`/`units`/`state` already are — a pass without it still
      fires and expires, it simply asks nothing.
      Requirement: design §3.5.
- [x] **5.10** `docs/02-cognitive-core.md` §4 amendment — the digest's asking mechanism: at most
      one relation question per digest, appended after `prospection.Carry`'s ranked items (Q2);
      the low-energy gate; the FIFO tie-break; the expiry rule (Q4) as a state transition, never a
      delete.
      Requirement: R10; non-negotiable #1.
- [x] **5.11** `docs/06-harness.md` — correct the I09 row if its wording needs to name the
      surfacing mechanism (spec R10's conditional clause).
      Requirement: R10.
- [x] **5.12** `cmd/nooma/wiring.go` — wire `questions` into `checkRunner`'s constructor.
      Requirement: design §4.
- [x] **5.13** Purity/lint: `golangci-lint run` (`brain-boundary`).
      Requirement: `nooma-core` hard rules 1–2.
- [x] Verify (PR-level): `make check-all` — **fully green**, including L3, the schema-golden
      regeneration diff, the `internal/core` coverage floor (99%), the seven-target matrix and L4.
      `TestI09_QuestionIsNotYetNamedInTheDigest`, PR 4's deliberately-red conformance test, is
      closed here as task 5.7 planned.
      **Measured: 311 impl+docs lines** (`+293/-18`, tests counted separately at `+772/-34`) —
      under the 400 ceiling and close to the ~300 budget, so design §7's pre-drawn cut was **not
      applied**. It was nonetheless preserved in the commit shape: the four commits are
      `RED asking → GREEN asking → RED expiry → GREEN expiry`, and the asking half is green on
      its own, so the cut stays available as a clean `git` boundary if review asks for it.
      **Deviations from this section's own idealized list, all reported rather than papered over:**
      - `internal/ports/decisionlog.go` is **untouched**: PR 4 already added all five m3e
        `DecisionAction` members at once (its own disclosed deviation), so
        `ActionCheckDigestQuestionAsked` and `ActionCheckQuestionExpired` were already in the
        vocabulary and in `AllDecisionActions`.
      - `docs/06-harness.md` is **untouched** (task 5.11's own clause is conditional: *"correct
        the I09 row **if** its wording needs to name the surfacing mechanism"*). The row reads
        *"The `[persist, surface)` band → stored **and** asked about in the digest | §4"* and
        already names it; the §4 pointer now resolves to the two paragraphs this PR added there.
        Editing it would have been churn, not sync.
      - `test/support/repocontract/pendingquestionrepo.go` gained a case (+45): `Unasked`'s
        `created_at`-then-`id` order was the one half of the port's contract nothing asserted —
        `Open`'s order had a case, `Unasked`'s did not — and the FIFO tie-break task 5.1's own
        `Mutation:` line names lives in the repository, not in `brain`. Asserted at the layer it
        lives in, so it covers `memrepo` and real SQLite at once.
      - The mechanical fallout of `NewCheckService`'s widened signature: `cmd/nooma/wiring.go`
        (both call sites — `wireCheck` gets the real repo too, since what keeps that subcommand
        silent is its nil channel, not a missing repository) plus one-argument additions in
        `test/conformance/{i15,i16,check_effect_completeness}_*.go`,
        `test/integration/due_scan_{concurrent,status_vocabulary}_test.go` and
        `test/e2e/m3_demo_test.go`.
      - `renderDigest` gained a shape this section did not anticipate: with zero carried items it
        **drops the item header entirely** rather than writing *"Here are 0 things for today"*.
        R3's own "a question alone is still sent" MUST is what makes that case reachable, and the
        header counts `Carry`'s output. The question sentence itself is written once
        (`questionLine`), so the appended and standalone forms cannot drift into two different
        questions.
      - Task 5.8's L3 test reaches `confirmed` and `rejected` by calling the repository directly,
        not through a real pass: the check-in path that will call them is PR 6's, and a
        vocabulary test that asserted only `expired` would leave two thirds of the vocabulary
        unchecked until then. Stated in the test's own doc comment.

---

## PR 6 — `feat/brain-relation-checkin` (~250 impl+docs)

Depends on PR 2 (`ConfirmedConfidence`), PR 3 (`RelationRepo.ByID`, the port) and PR 5 (questions
exist and can already be asked). Ships `resolveRelationCheckIn`, `ConfirmRelation`,
`RejectRelation`'s narrowing, `recordRelationCheckIn`. **I10 reachable with a real relation on the
end; `relation_confirm`'s first emission anywhere in this tree.**

- [ ] **6.1** Commit 1 (RED): `internal/brain/checkin_test.go` (extend) —
      `resolveRelationCheckIn`, mirroring `resolveCheckIn`'s own test shape: zero open questions +
      either outcome value → one `decision_log` row (`ActionCaptureRelationCheckInUnmatched`) with
      `open_relation_questions: 0`, no `RejectRelation`/confirm-path call whatsoever (R8, both
      outcome values identically); one open question → trivially resolved,
      `open_relation_questions: 1`; two-or-more open questions → the **most recently asked** one
      resolved, the count recorded (R4).
      **Red**: `resolveRelationCheckIn` today only writes one audit row naming `m3e` and returns
      (`checkin.go:108-122`) — every fixture in this task fails against that shape.
      Stub: a signature matching design's snippet, returning `nil` unconditionally after writing
      nothing — compiles; the zero-open-questions case fails first (wrong action name / wrong
      `Context` shape).
      Requirement: R4, R8 (spec).
      **Mutation**: change the disambiguation pick from `open[0]` (design's `Open` ordering:
      most-recent-asked first) to the LAST element of the slice — a three-open-questions fixture
      whose most-recent is not the slice's last element must fail.
- [ ] **6.2** Commit 2 (GREEN): implement `resolveRelationCheckIn` exactly per design §3.6's
      snippet — reads `c.RelationOutcome`, `r.questions.Open(ctx)`; `len(open) == 0` →
      `recordRelationCheckIn(ctx, now, ActionCaptureRelationCheckInUnmatched, "", "", *outcome,
      0)`; else `target := open[0]`, switch on `*c.RelationOutcome` to `ConfirmRelation`/
      `RejectRelation`, then `recordRelationCheckIn(ctx, now,
      ActionCaptureRelationCheckInResolved, target.ID, target.RelationID, *outcome, len(open))`.
      Verify: `go test ./internal/brain/... -run RelationCheckIn`.
      Requirement: R4, R8; design §3.6.
- [ ] **6.3** Commit 1 (RED): `checkin_test.go` (continued) — `ConfirmRelation`: reads the relation
      via `RelationRepo.ByID`, `ThresholdsFor`, applies `relation.ConfirmedConfidence`; persists
      through `Upsert` with **only `Confidence` replaced** (`Strength`/`CreatedBy`/`CreatedAt`
      travel back unchanged); `SignalRelationConfirm` emitted **AFTER** the raise (design §3.6's
      flagged reversal of I10's order — **Finding F1**); the `pending_questions` row resolved
      `confirmed`. Scenario: a relation already above the floor is a no-op raise (`GREATEST` at
      its own floor) but the signal is still emitted — the signal records the confirmation, not
      the delta (spec R5's own scenario).
      **Red**: `undefined: ConfirmRelation`.
      Stub: a no-op returning `nil` — compiles; the emit-after-persist ordering assertion (checks
      `Upsert` called strictly before `signals.Record`) fails first.
      Requirement: R5 (spec); design §3.6, **Finding F1**.
      **Mutation**: emit the signal BEFORE the `Upsert` call instead of after — the ordering
      assertion, which asserts call sequence rather than merely "both happened", must fail; this
      is the mutation that would make the test pass under spec R5's original (superseded) wording,
      named explicitly so the test is checkable against the ruling actually shipped.
- [ ] **6.4** Commit 2 (GREEN): implement `ConfirmRelation` exactly per design §3.6's snippet —
      `rel, err := r.rels.ByID(ctx, q.RelationID)`; `row, err := r.rels.ThresholdsFor(ctx,
      rel.Type)`; `rel.Confidence = relation.ConfirmedConfidence(rel.Confidence,
      relation.Resolve(row))`; `err = r.rels.Upsert(ctx, rel)` (I07: revises in place); `err =
      r.signals.Record(ctx, ports.Signal{ID: r.ids.New(), Type: ports.SignalRelationConfirm,
      Valence: ports.ValencePositive, TargetKind: &relationTarget, TargetID: &rel.ID, OccurredAt:
      now})`.
      Verify: `go test ./internal/brain/... -run ConfirmRelation`.
      Requirement: R5; design §3.6.
- [ ] **6.5** Commit 1 (RED): `checkin_test.go` (continued) — `RejectRelation` narrows to
      `(ctx context.Context, relationID string, now time.Time) error` (owner-review **R5**);
      `checkin_test.go:221`'s existing assertions extended by one argument in the call expression;
      I10's ordering stays unchanged (signal emitted before delete).
      **Red**: the existing test's call expression does not compile against the narrowed
      signature until this task's fixture is updated in lock-step with 6.6's implementation —
      recorded as the shape of this particular RED (a signature narrowing, not a missing symbol).
      Requirement: R6 (spec); design §3.6.
- [ ] **6.6** Commit 2 (GREEN): narrow `RejectRelation`'s parameter list to `(ctx, relationID
      string, now)` — it reads only `rel.ID` today (`checkin.go:130-148`), and a parameter with no
      reader is refused (`UpdateEventAt`'s own doc-comment rule). `resolveRelationCheckIn`'s
      rejected branch resolves the `pending_questions` row (`resolution = rejected`) after
      `RejectRelation` returns successfully.
      Verify: `go test ./internal/brain/... -run Reject`.
      Requirement: R6; design §3.6.
      **Mutation**: resolve the `pending_questions` row BEFORE calling `RejectRelation` instead of
      after — a fixture whose `RejectRelation` fails mid-way must show the question still open
      (retry-safe); reversing the order makes a failed reject silently close its own question.
- [ ] **6.7** `internal/brain/checkin.go` — `recordRelationCheckIn` beside `recordCheckIn`, and a
      `questionDetail{question_id, relation_id, resolution}` context shape beside `checkDetail`
      (design §3.7) — a distinct `Context` shape from `recordCheckIn`'s own
      `{trigger_id, resolution, open_check_ins}`, per m2c §7.5's rule that effects split when their
      Context shapes differ (design's own argument, applied one level down from the proposal's
      argument against reusing `triggers`).
      Requirement: design §3.7.
- [ ] **6.8** `internal/ports/decisionlog.go` — add `ActionCaptureRelationCheckInResolved`
      (`"capture.relation_checkin.resolved"`) and `ActionCaptureRelationCheckInUnmatched`
      (`"capture.relation_checkin.unmatched"`); extend `AllDecisionActions()`.
      Requirement: design §3.7.
- [ ] **6.9** `captureRunner` gains one field, `questions ports.PendingQuestionRepo` (`signals` is
      already present, `RejectRelation`'s own existing field). `cmd/nooma/wiring.go` wires it.
      Requirement: design §3.6.
- [ ] **6.10** `test/conformance/` (new, threat-matrix §9's one applicable row — untrusted inbound
      text → an irreversible delete) — an unknown/undecodable outcome resolves nothing and deletes
      nothing; a `rejected` outcome with nothing open writes one row and deletes nothing (R8); a
      `rejected` outcome with three open questions deletes exactly **one** relation and records
      `open_relation_questions: 3`.
      Requirement: design §9 (the applicable threat-matrix case); R6, R8.
- [ ] **6.11** `test/e2e/` (L4) — the proposal's own demo, end to end: a vault with one
      Uncertain-band relation produces a digest naming both endpoints; a reply of "yes, they're
      related" raises its confidence via `GREATEST(current, min_confidence_to_surface)` and emits
      `relation_confirm`; a reply of "no" emits `relation_reject` and deletes it, signal first.
      Requirement: proposal §2 (Demo); spec's own Exit criterion.
- [ ] **6.12** `docs/02-cognitive-core.md` §5 amendment — state the store-based (never
      model-based) disambiguation rule for `relation_outcome` (Q3): the pending-question store
      resolves which relation, the classify prompt is never widened to inject open check-ins.
      Requirement: R10; non-negotiable #1.
- [ ] **6.13** Purity/lint: `golangci-lint run` (`brain-boundary`).
      Requirement: `nooma-core` hard rules 1–2.
- [ ] Verify (PR-level): `make check-all`; confirm diff touches only
      `internal/brain/{checkin,capture}{,_test}.go`, `internal/brain/check.go` (the
      `questionDetail` addition), `internal/ports/decisionlog.go`, `cmd/nooma/wiring.go`,
      `test/conformance/*.go` (the threat-matrix case), `test/e2e/*.go`,
      `docs/02-cognitive-core.md`. Target ≤250 impl+docs lines.

---

## Traceability

| Spec section | Requirement | Tasks |
|---|---|---|
| R1 — recorded on Uncertain | R1 | 4.1–4.3 (see **Finding F2**) |
| R2 — state machine, never deleted | R2 | 3.1–3.3, 3.9, 5.8 |
| R3 — digest surfaces one, after ranked items | R3 | 5.1–5.4, 5.10–5.11 |
| R4 — disambiguation mirrors `resolveCheckIn` | R4 | 6.1–6.2 |
| R5 — confirm raises confidence, emits signal | R5 | 2.1–2.2, 6.3–6.4 (see **Finding F1**, **F3**) |
| R6 — reject reaches a real relation | R6 | 6.5–6.6 |
| R7 — expiry after `MaxDigestDeferrals` | R7 | 5.5–5.6 |
| R8 — unmatched answer resolves nothing | R8 | 6.1–6.2 |
| R9 — stale comment corrected | R9 | 2.5 |
| R10 — doc sync | R10 | 1.2, 2.6, 5.10–5.11, 6.12 |
| I09 — first real conformance test | — | 4.2, 5.7 |
| §9 threat matrix — inbound text to delete | — | 6.10 |
| Exit criterion | — | 6.11 |

---

## Review Workload Forecast

| Field | Value |
|-------|-------|
| Estimated changed lines | ~1,260 budgeted impl+docs across 6 PRs (design §7: 180 + 120 + 300 + 110 + 300 + 250); test lines not separately budgeted by design, tracked per PR against `m3a`/`m3b`'s own historical 1.3×–4.3× multiplier, not against the 400-line ceiling |
| 400-line budget risk | **Medium overall.** PR 3 and PR 5 are each budgeted ~300 — **High risk pre-code**, per design §7's own flag, each with a pre-drawn fallback split named in its own PR section above; PR 1 (~180), PR 2 (~120), PR 4 (~110), PR 6 (~250) are Low-Medium risk |
| Chained PRs recommended | **Yes** — six links, already a chain by design, and the cached session `delivery_strategy: single-pr` cannot hold: aggregate ~1,260 lines exceeds both the repo's own 400-line-per-PR soft ceiling (`docs/06-harness.md` §7) and the session's 800-line stop-and-ask threshold by a wide margin |
| Suggested split | No PR individually needs a pre-emptive split at the budgeted estimate; PR 3's and PR 5's own pre-drawn fallback cuts (named in their sections above) apply only if measured lines threaten 400, reported before splitting rather than split silently — `m3a`/`m3b`'s own house convention |
| Delivery strategy (cached) | `single-pr` — in tension with the above, as design §7 already stated before this artifact existed |
| Chain strategy | `stacked-to-main` (design §7's own explicit choice, matching `m3a`/`m3b`'s precedent for this repo) |

Decision needed before apply: Yes
Chained PRs recommended: Yes
Chain strategy: stacked-to-main
400-line budget risk: Medium

**Why "Decision needed" is Yes despite `auto-chain`-style reasoning above**: the session's cached
`delivery_strategy` is `single-pr`, and per the `sdd-tasks` skill's own rule table, `single-pr`
resolves to `Decision needed before apply: Yes` — the orchestrator must require an explicit
`size:exception` before `sdd-apply` starts oversized work, or the owner re-rules the delivery
strategy to `auto-chain`/`ask-on-risk` now that this forecast exists (proposal R5's own framing:
"the strategy is the owner's to re-rule once the forecast exists").

### Suggested Work Units

| Unit | Goal | Likely PR | Focused test command | Runtime harness | Rollback boundary |
|------|------|-----------|----------------------|-----------------|-------------------|
| 1 | Migration, doc 03, ADR-0027 | PR 1 | `go test ./test/conformance/... -run SchemaDoc` | Schema-golden regeneration-diff gate (`make check-all`) | Migration is forward-only and does not roll back; the table stays inert and unread until PR 3 — this is the one PR whose rollback is asymmetric, by design |
| 2 | `ConfirmedConfidence`, `Band`, stale-comment fix | PR 2 | `go test ./internal/core/relation/... -run ConfirmedConfidence` | N/A — pure core function, no runtime scenario | Delete `confirm{,_test}.go`; revert `Band` field and its one call site; `connect.go:151`'s comment reverts independently |
| 3 | Port, SQLite impl, `RelationRepo.ByID` | PR 3 | `go test -tags=integration ./internal/store/sqlite/... -run PendingQuestion` | `pendingquestionrepo_integration_test.go` against a real migrated vault | Delete both repo files + port; PR 1's table sits unread, exactly as before this PR |
| 4 | Queue a question on Uncertain | PR 4 | `go test ./test/conformance/... -run I09` | `test/conformance/i09_uncertain_band_asked_test.go` (part (a) only until PR 5) | Delete the branch in `judgeAndPersistPair`; existing rows are read by nothing until PR 5 |
| 5 | Digest asks, expiry sweep | PR 5 | `go test ./internal/brain/... -run Digest` | `test/conformance/i09_uncertain_band_asked_test.go` (both halves green) | Delete the additive branch in `assembleDigest`; the trigger-only digest is restored exactly; queued questions stop being asked and never expire — inert, not corrupt |
| 6 | Disambiguation, confirm, reject-with-a-real-relation | PR 6 | `go test ./internal/brain/... -run RelationCheckIn` | `test/e2e/*.go` — the proposal's own demo | Reverts to today's recorded-and-ignored row; questions asked before the revert stay open and expire on schedule (PR 5's sweep) |

---

## Handoffs (design §11/§12, carried forward)

- **Owner-review R2** (design §3.4, N2): `capture.go`'s own relation judge creates no question in
  this change — explicit non-goal (proposal §3.4). Widening it is named future work, not inherited
  silently.
- **Design §11 Q1**: the queued (unasked) pool is unbounded — one question per digest against an
  unbounded producer. Not mitigated in `m3e`; a `created_at`-based queue expiry is named as future
  work with its own cost (a second §13-adjacent number for a pressure nothing has measured).
- **Design §12 N4**: `ConfirmRelation`'s read-modify-write races the nightly connect pass inside
  one `nooma serve` process. Bounded by `vaultlock` across processes; the worst in-process case is
  one confidence revision losing to another, no deletion, no lost question — `ConfirmedConfidence`'s
  idempotence (task 2.1 property (b)) means a repeat answer corrects it.

## Key Learnings

1. Spec and design disagreed on the confirm signal's ordering (before vs. after the raise); design's reasoning about idempotence was more thorough and was followed.
2. Spec and design disagreed on which package computes the Uncertain-band verdict; design's `ProposedRelation.Band` field avoids recomputing `relation.Decide` in two packages.
3. The `ConfirmedConfidence` function signature in spec (`floor float64`) differs from design's shipped form (`t Thresholds`); design's exact signature was used per the task-writing instruction.
4. Two of the six PRs (port/store and digest-asks) are individually budgeted near the 400-line soft ceiling and each carries a pre-drawn fallback split named in design.md itself.
5. The `i09_uncertain_band_asked_test.go` conformance file is intentionally created RED in PR 4 and turned GREEN in PR 5, mirroring the split between storing and asking.
