# Tasks — M3 Phase B: trigger/timer ports, store, arming, due scan

Implementation task list for `m3b-trigger-timer`, derived from `spec.md` (R0–R6.1) and `design.md`
(§1–§12, plus the F1 reconciliation note and the 2026-08-21 owner decisions), both read in full
before this document, and matched to `m3a-prospection/tasks.md`'s granularity per instruction.
Design §7 fixes the slicing — **eight PRs**, stacked, ~1,620 budgeted impl+docs lines — treated as
authoritative over spec's own six-PR numbering where the two disagree.

Chain strategy **`stacked-to-main`**, delivery strategy **`auto-chain`** (design §7). Order:
`1 → 2 → 3` (store slice, 3 independent of everything after it); `4a → 4b` (capture slice, needs
1+2); `5a → 5b` (scan slice, needs 1+2); `6` (needs 5).

**Strict TDD is active.** Every behavioral task states the two-commit RED/GREEN shape. **Inside
every PR the conformance/L1 test commit is strictly ahead of the implementation commit** —
`sdd-verify` reads the PR's `git log` and reports an inversion as CRITICAL. Every PR touching
`internal/core/**` carries its `docs/02-cognitive-core.md` delta in the same PR. **For every
RED/L2/L3 task below, a `Mutation:` line names the code change that must make the test fail** — a
task without one is not considered checkable from the tree alone.

---

## Findings — spec/design disagreements found in this session (report, don't paper over)

**G1 — design §3.6's pipeline diagram is stale after its own F1 correction.** §3.6's ASCII pseudocode
still reads `Deliver → Fire(id, now) + check.trigger.fired` for a trigger, and §3.6's prose lists
`check.trigger.fired` among the "nine `DecisionAction` members added". Both contradict §3.3's
*corrected* table (`VerdictDeliver` → trigger stays `armed`, no write) and spec R5.3's own MUST ("no
`decision_log` row was written for it this pass"). The F1 reconciliation note fixed §3.3's table but
never propagated into §3.6. **Resolved in F1's own favor**: `check.trigger.fired` is not a producer
in `m3b`; PR 5a ships four due-scan actions, not five (`check.trigger.expired`, `check.timer.fired`,
`check.timer.cancelled`, `check.conflict_skipped`). Combined with the four arming actions (PR
4a/4b) and the one removed member (`ActionCaptureHookDeferred`), `AllDecisionActions()` goes from
24 to **31**, not design's stated 32. Task 5a.1 states this in the PR itself.

**G2 — spec R1.2 asks `TimerRepo`'s transition method to "accept `rendered_text` and `surfaced_at`
as optional write parameters"; design's own interface snippet ships `Fire(ctx, id, at) error` with
neither.** Resolved in design's favor, on design's own stated grounds (`UpdateEventAt`'s refusal of
a parameter with no caller, §3.1): `at` is written to `surfaced_at` directly (a timer's `Fire` *is*
its delivery, R5.2's own claim), and `rendered_text` is simply never touched by this PR's `UPDATE` —
achieving "stays NULL" without inventing a parameter no `m3b` call site would ever pass a value
through. `m3d`'s fire-time rephrasing adds the write when it has a real caller, the same trade
design takes for M4's `dismissed` transition (§3.1, Option B).

**G3 — spec R0 ("no file under `internal/core/**` is created or modified") conflicts with design's
own plan to ship `prospection.AllVerdicts()` inside `internal/core/prospection/staleness.go` in PR
5a** (§3.3 layer 3, §12 Risk F). **Resolved in design's favor**: `AllVerdicts()` states no new
decision — it is a completeness accessor over a vocabulary `m3a` already shipped, structurally
identical to `classify.AllKinds()`/`ports.AllDecisionActions()`, and it is what makes §3.3's own
Risk-A mitigation layer able to fail on its own violation. This is the one `internal/core` line in
the whole change; PR 5a carries its `docs/02-cognitive-core.md` §7 delta in the same PR per the
non-negotiable ordering rule. `spec.md` R0 is not edited by this artifact; the deviation is recorded
here for owner awareness, the same posture `m3a`'s F1–F9 took toward `design.md`.

**G4 — task 1.3's placement of the vocabulary tests contradicts design §8's own table.**
Task 1.3 puts `AllTriggerStatuses()`/`AllTriggerKinds()`'s migration-comment pin inside
`test/support/repocontract/triggerrepo.go`; design §8's testing table puts it at
`test/conformance/` (PR 1). **Resolved in design's favour.** The vocabularies are properties of
`internal/ports`, not of any implementation of the port, so running the pin once at L2 proves
exactly as much as running it again per implementation would, and `migrationSQLText`
(`i13_learning_signal_test.go:24`) already lives in `test/conformance`. Shipped as
`test/conformance/trigger_timer_vocabulary_ddl_test.go`, which also holds the fresh-slice check
`AllDecisionActions`'s doc comment argues for. Task 1.1's `RunTriggerRepoContract(t, repo, clock)`
signature went the same way: the house shape is `Run<Port>(t, newRepo func(t) ports.<Port>)` —
every one of the seven existing suites — and no case in this contract reads a clock, so a `clock`
parameter would have had no reader. Shipped as `RunTriggerRepo` / `RunTimerRepo`.

**G5 — design §3.1's claim that "the I03 scan stays scoped to `ports.UnitRepo`" is stale.**
`test/conformance/i03_units_never_deleted_test.go:118-137` has swept **seven** ports interfaces
since `m2c` PR 3, and its own comment states the list's claim as "every ports repository
interface". Leaving `TriggerRepo` and `TimerRepo` out would have falsified that claim the moment
they landed, so both were added to `sweptPortsRepoTypes` — a strengthening, which
`docs/06-harness.md` §4 permits (weakening is what it forbids). The contract suite's own
reflection check stays: it runs per implementation, this one runs over the declaration, and the
redundancy is stated in both places rather than left to look accidental.

**G6 — the contract suite PR 1 shipped could not run at L3 as written, and the schema is why.**
`triggers.unit_id REFERENCES units(id) ON DELETE CASCADE` (`0001:43`) and the vault opens with
`foreign_keys=on`, so every contract case that created a trigger for a fixture unit id failed with
`FOREIGN KEY constraint failed` the moment the real repository stopped being a no-op. Neither
`spec.md` nor `design.md` names the FK; the in-memory fake has no notion of one, so L2 was green
and silent about it. **Resolved by the house pattern that already exists for exactly this**:
`RunTriggerRepo` now takes a `repocontract.TriggerHarness` (`ports.TriggerRepo` plus
`EnsureUnit(t, id)`), the shape `repocontract.EmbeddingHarness` has carried since `m1a` — and whose
own doc comment names this failure mode in advance: "without this hook the suite would pass at L2
and be impossible to run at L3 — which is not a contract, it is a fake's opinion". `RunTimerRepo`
takes no harness, and the asymmetry is the schema's own: `timers` carries no `unit_id`, because a
timer is never a unit (I04).

**G7 — `triggers.payload`'s JSON keys were unspecified, and are now pinned to the column comment.**
Design §3.1 fixes `TriggerPayload{ActionText, Rationale, LeadDays}` as the Go shape and doc 02 §7
pins `payload.lead_days` as a stored key, but nothing named the other two. Migration `0001:48`'s own
comment — "JSON (action, rationale, lead_days…)" — is the only other source of truth, so
`payloadJSON` follows it: `action`, `rationale`, `lead_days`. `ActionText` maps to `action`
deliberately rather than to `action_text`; `timers.action_text` is a different table's column, not
this key's namesake. Asserted against the stored bytes, `anchorJSON`'s own discipline.

**G8 — PR 2 measured 463 implementation-and-docs lines against design §7's ~250 budget, so it was
cut in two.** Not an exception claimed, a measurement acted on: design §7 already set that posture
for PR 5a ("apply that cut if its own forecast exceeds 400"), and the same rule read against the
same number gives the same answer here. The cut is the natural one — **PR 2a
`feat/store-trigger`** (~280 impl+docs: the trigger repository, `anchorJSON`, `payloadJSON`, the
`interrupt_level` round trip, the `TriggerHarness` retrofit) and **PR 2b `feat/store-timer`**
(~150: the timer repository and its own asymmetries). Both regenerate the golden, each for its own
rows. Tasks 2.1–2.4 belong to 2a, 2.5–2.6 to 2b, and 2.7/2.8 are done once per PR. The chain is now
nine PRs, which was already design §7's own named contingency shape, reached one slice earlier than
it expected.

---

## Owner-review items carried forward (design §11 / "Owner decisions — 2026-08-21" — not reopened)

| # | Item | Decided default | PR / Task |
|---|---|---|---|
| R1 | Transitions take no `to` parameter | Accepted as designed | PR 1, tasks 1.1–1.5 |
| R2 | Status vocabularies live in `internal/ports` | Accepted as designed | PR 1, task 1.3 |
| R3 | `ActionCaptureHookDeferred` deleted, not kept read-only | Deleted | PR 4a, task 4a.1 |
| R4 | Four `decision_log` actions for arming | Four | PR 4a/4b |
| R5 | `LiveFocusCandidates` ships with no production caller | Ships in PR 3 | PR 3 |
| R6 | Eight PRs, ninth named as contingency | Eight, watch PR 5a | §7 note below PR 5a |
| Q1 | `nooma check --dry-run` | **Yes, in scope** — suppresses the effect, never branches the logic | PR 6, tasks 6.3–6.4 |
| Q2 | Unbounded `Due`, no `timers` index | Accepted for v1, no migration | PR 2, task 2.6 (named, not mitigated) |
| Risk A | No `CHECK` constraint on status columns | Accepted; the L3 `SELECT DISTINCT` test is the constraint | PR 5a, task 5a.8 (see below) |

---

## PR 1 — `feat/ports-trigger-timer` (~250 impl+docs)

Depends on nothing outside this change. Ships both ports, three vocabularies + `AllX()`, write/read
shapes, sentinels, `memrepo` fakes, `repocontract` shared suites.

- [x] **1.1** Commit 1 (RED): `test/support/repocontract/triggerrepo.go` (new) — a shared
      `RunTriggerRepoContract(t, repo, clock)`: Create+Due round trip (armed trigger appears in
      `Due` at/after `fire_at`, absent before); the conflict scenario (spec R1.1's own: stored
      status `expired`, call the `armed→fired` transition, expect the named conflict error, row
      unchanged); `Expire` moves `armed→expired` and the row leaves `Due`; **no
      `Delete`/`Remove`/`Purge`/`Drop`/`Destroy`-prefixed method**, asserted via
      `reflect.TypeOf((*ports.TriggerRepo)(nil)).Elem()`'s own `NumMethod()`/`Method(i).Name`, never
      a hand-typed list of the four method names.
      **Red**: `undefined: ports.TriggerRepo`, `ports.Trigger`, `ports.DueTrigger`,
      `ports.ErrTriggerStatusConflict`, `memrepo.NewTriggerRepo`.
      Stub: minimal interface + a `memrepo` fake whose every method is a no-op/zero-value — compiles;
      the Create+Due round trip fails first (empty slice).
      Requirement: R1.1.
      **Mutation**: replace the reflection scan's prefix set `{Delete,Remove,Purge,Drop,Destroy}`
      with the empty set — a hypothetical sixth `DeleteBy…` method would then compile and pass
      undetected; also flip `memrepo`'s `Due` filter from `status == armed` to "return everything" —
      the expired-row-absent-from-Due assertion must fail.
- [x] **1.2** Commit 2 (GREEN): implement `internal/ports/triggerrepo.go` (`TriggerStatus`,
      `TriggerKind`, `AllTriggerStatuses()`, `AllTriggerKinds()`, `Trigger`, `TriggerPayload`,
      `DueTrigger`, `TriggerRepo`, sentinels — design §3.1's exact shape) and
      `test/support/memrepo/triggers.go` (the `armed` precondition enforced under a mutex,
      `UnitRepo`'s own fake pattern).
      Verify: `go test ./test/support/repocontract/... ./test/support/memrepo/...`.
      Requirement: R1.1; design §3.1, §3.2.
- [x] **1.3** Commit 1 (RED): `repocontract/triggerrepo.go` (continued) — `AllTriggerStatuses()`
      returns exactly `armed|fired|dismissed|expired` in migration `0001:42-58`'s own comment
      order; `AllTriggerKinds()` returns exactly `time_based|event_based|pattern_based` — pinned to
      the literal comment text, `relation.AllCreatedBy`'s own shape against `0001:37`.
      **Not a missing-symbol red** (1.2 already compiles) — disclosed per `m2a` C9; the substantive
      assertion itself is red until the vocabulary/order match.
      Requirement: design §3.2; **R2**.
      **Mutation**: reorder one member in the Go-side slice — fails; the test also asserts
      `len(AllTriggerStatuses()) == 4` so a silently-dropped member is caught independent of order.
- [x] **1.4** Commit 1 (RED): `test/support/repocontract/timerrepo.go` (new) — mirrors 1.1 for
      `TimerRepo`: Create+Due, `pending→cancelled` conflict scenario, `Cancel` drops from `Due`,
      the same reflection-based no-forbidden-prefix check, `AllTimerStatuses()` pinned to
      `pending|fired|cancelled` at `0001:61-70`.
      **Red**: `undefined: ports.TimerRepo`, `ports.Timer`, `ports.DueTimer`, `memrepo.NewTimerRepo`.
      Stub: zero-value stubs — compiles; Create+Due round trip fails first.
      Requirement: R1.2.
      **Mutation**: identical shape to 1.1's — reflection-emptying and always-return-all.
- [x] **1.5** Commit 2 (GREEN): implement `internal/ports/timerrepo.go` (`Create`/`Due`/`Fire`/
      `Cancel`, no `interrupt_level` field — `timers` has none) and `test/support/memrepo/timers.go`.
      `Fire` is 3-arg per **G2**'s resolution: `at` writes `surfaced_at`; `rendered_text` untouched.
      Verify: `go test ./test/support/repocontract/... ./test/support/memrepo/...`.
      Requirement: R1.2; design §3.1; **G2**.
- [x] **1.6** `repocontract/triggerrepo.go` (continued) — R1.3's Go-level half: `Create` with
      `InterruptLevel: nil` → `Due` returns `nil`; `Create` with a pointer to `0.37` → `Due` returns
      a **freshly allocated** pointer to `0.37`, never the same pointer (a caller mutating the
      returned value must not corrupt the fake's own store).
      `Trigger.InterruptLevel *float64` is the field name — the port takes the **already-converted**
      float, never a `prospection.Interrupt` (`interruptColumn` lives in `internal/brain`, per
      design §3.4; `ports` does not import `prospection.Interrupt`).
      Requirement: R1.3.
      **Mutation**: have `memrepo` store the caller's pointer directly instead of copying the float
      — a test that mutates the pointer after `Create` and re-reads via `Due` must observe the
      mutation and fail the "unaffected" assertion.
- [x] **1.7** Purity/lint: `golangci-lint run` (`ports-purity` — `internal/ports` imports only
      `internal/core/prospection` for `Rule`/`Anchor`, no `internal/store` import).
      Requirement: `nooma-core` hard rules 1–2; design §4.
- [x] Verify (PR-level): `make check-all`; confirm diff touches only
      `internal/ports/{triggerrepo,timerrepo}.go`, `test/support/memrepo/{triggers,timers}.go`,
      `test/support/repocontract/{triggerrepo,timerrepo}.go`. No `docs/02-cognitive-core.md` delta
      (no `internal/core` touch here). Target ≤250 impl+docs lines.

---

## PR 2a — `feat/store-trigger` (~280 impl+docs) — see finding G8 for the cut

Depends on PR 1. Ships both SQLite repos, `anchorJSON`, `store_api.golden` regenerated. **L3 owns
§3.4's round trip** — both the `interrupt_level` NULL↔degraded contract and `recurrence_anchor`'s
lowercase-key stored TEXT.

- [x] **2.1** Commit 1 (RED): `internal/store/sqlite/triggerrepo_integration_test.go` (build tag
      `integration`) — `repocontract.RunTriggerRepoContract` run against a real migrated vault
      (`unitrepo_integration_test.go`'s own pattern); a raw-SQL fixture inserting `interrupt_level`
      as NaN/+Inf/-Inf directly (bypassing the repo), read back through `Due`, asserted against the
      **exact `*float64` observed in this session** — pinned, not guessed (design §3.4: "whatever
      comes back is asserted, not assumed").
      **Red**: `undefined: sqlite.NewTriggerRepo`.
      Stub: a constructor whose every method is a no-op — compiles; the Create+Due round trip fails
      first.
      Requirement: R1.1, R1.3, R2.1.
      **Mutation**: this task's own discipline is the guard — asserting a value chosen in advance
      instead of the value actually read would silently stop tracking a future SQLite storage-format
      change; the fixture must assert the observed bytes, named as the check itself.
- [x] **2.2** Commit 2 (GREEN): implement `internal/store/sqlite/triggerrepo.go` — `Create` INSERTs
      per `Trigger`/`TriggerPayload` (JSON-marshalled payload); `Due` SELECTs
      `WHERE status = 'armed' AND fire_at IS NOT NULL AND fire_at <= ?` (matches
      `idx_triggers_status_fire`); `Fire`/`Expire` as single `UPDATE ... WHERE id = ? AND status =
      'armed'` + `requireRowAffected(res, ports.ErrTriggerStatusConflict)` — `UnitRepo.SetStatus`'s
      own shape, **the `armed` precondition living in the `WHERE` clause and nowhere else** (load-
      bearing for PR 5b).
      Verify: `go test -tags=integration ./internal/store/sqlite/... -run TriggerRepo`.
      Requirement: R1.1, R1.3, R2.1; design §3.1, §3.4, §3.6.
- [x] **2.3** Commit 1 (RED): `triggerrepo_integration_test.go` (continued) — `recurrence_anchor`'s
      **stored TEXT**: `Create` with `RecurrenceAnchor: &prospection.Anchor{Month: time.September,
      Day: 4}`, then a raw `SELECT recurrence_anchor FROM triggers WHERE id = ?` asserted to equal
      the literal string `{"month":9,"day":4}` — never `{"Month":9,"Day":4}`, Go's default.
      **Red**: without `anchorJSON`, `Create` either fails to marshal (no encoder) or marshals with
      capitalized keys — the raw-TEXT assertion fails first.
      Requirement: design §3.4 (the `anchorJSON` detail).
      **Mutation**: delete the private `anchorJSON` type and marshal `prospection.Anchor` directly —
      the stored-TEXT assertion fails, while a test asserting only the round-tripped `Anchor` struct
      would still pass (Go's `json.Unmarshal` is case-insensitive on read). This is the exact
      "defence that protects the result but not the probe" this task is written to close: the
      assertion is against raw bytes, not the round-tripped struct.
- [x] **2.4** Commit 2 (GREEN): implement a private `anchorJSON{Month int `json:"month"`; Day int
      `json:"day"`}` in `triggerrepo.go`; `Create` converts `*prospection.Anchor` → `*anchorJSON`
      before marshalling, `Due`'s unmarshal path converts back.
      Verify: `go test -tags=integration ./internal/store/sqlite/... -run Anchor`.
      Requirement: design §3.4.
- [ ] **2.5** Commit 1 (RED): `internal/store/sqlite/timerrepo_integration_test.go` — mirrors 2.1 for
      `TimerRepo`: contract run against a real vault; `Fire`'s single-statement write of
      `status='fired', surfaced_at=?` verified via a raw read after the call; a raw `SELECT
      rendered_text` after `Fire` confirms it is still `NULL` (**G2**'s resolution — untouched, not
      defaulted).
      **Red**: `undefined: sqlite.NewTimerRepo`.
      Stub: zero-value constructor — compiles; Due round trip fails first.
      Requirement: R1.2, R2.1; **G2**.
- [ ] **2.6** Commit 2 (GREEN): implement `internal/store/sqlite/timerrepo.go` — `Create`; `Due`
      (`WHERE status = 'pending' AND fire_at <= ?`, full scan — `timers` carries **no index**, **Q2**
      accepted, named not mitigated); `Fire` (single `UPDATE status='fired', surfaced_at=?` under
      `from='pending'`); `Cancel` (`status='cancelled'` under `from='pending'`, no timestamp column).
      Verify: `go test -tags=integration ./internal/store/sqlite/... -run TimerRepo`.
      Requirement: R1.2, R2.1; design §3.6 (Risk E, Q2).
- [ ] **2.7** `testdata/schema/store_api.golden` — regenerate; diff limited to the two new
      repositories' `type`/`func`/`var` lines, no existing row changed (spec R2.1's own MUST).
      Verify: `make store-api-golden && git diff --stat testdata/schema/store_api.golden`.
      Requirement: R2.1.
      **Mutation**: hand-edit the golden instead of regenerating — `store_api_test.go`'s own
      regeneration-diff gate (inside `make check-all`) fails on any drift from the tool's output.
- [ ] **2.8** Purity/lint: `golangci-lint run`.
      Requirement: `nooma-core` hard rules; design §4.
- [x] Verify (PR-level, 2a): `make check-all`; confirm diff touches only
      `internal/store/sqlite/triggerrepo{,_integration_test}.go`,
      `testdata/schema/store_api.golden`, and the `TriggerHarness` retrofit in
      `test/support/{repocontract,memrepo}` plus its L2 wiring (finding G6). No `docs/02-cognitive-core.md` delta (store is I/O, not
      core). Target ≤250 impl+docs lines.

---

## PR 3 — `feat/ports-store-focus-candidates` (~120 impl+docs)

Independent of everything after it. Ships `LiveFocusCandidates` + its positive-`pool` SQL (I02),
M2's carry-over (owner ruling 4, umbrella §3.3).

- [ ] **3.1** Commit 1 (RED): `test/support/repocontract/unitrepo.go` (extend) — `LiveFocusCandidates`
      given a mixed id set (one `pool`, one `superseded`, one `incomplete`, one absent id) returns
      only the `pool` row's five `focus.Candidate` fields, exactly matching the unit's own stored
      values; an empty id set returns an empty slice, never an error; ordered by id (design's own
      tie-break, not a ranking).
      **Red**: `undefined: ports.UnitRepo.LiveFocusCandidates`.
      Stub: add the method, `memrepo` returning `nil, nil` unconditionally — compiles; the mixed-id
      case fails first (expects one row, gets zero).
      Requirement: R3.1.
      **Mutation**: change the filter from `status == "pool"` (positive, I02) to `status !=
      "superseded" && status != "incomplete"` (negative) — the fixture, which seeds both excluded
      statuses among the mix, cannot distinguish the two on its own; recorded as the fixture's own
      named limit rather than assumed complete, per the "guard entered from underneath" concern —
      a third live status added later by M4 would pass the negative-list implementation silently.
- [ ] **3.2** Commit 2 (GREEN): implement `LiveFocusCandidates` in `memrepo` and in
      `internal/store/sqlite/unitrepo.go` (`WHERE status = 'pool' AND id IN (...)`, positive filter
      per I02, `ORDER BY id`).
      Verify: `go test ./test/support/repocontract/...` and
      `go test -tags=integration ./internal/store/sqlite/... -run FocusCandidates`.
      Requirement: R3.1; design §3.7; I02.
- [ ] **3.3** `internal/store/sqlite/unitrepo_integration_test.go` (extend) — 3.1's fixture against
      a real migrated vault.
      Requirement: R3.1.
- [ ] **3.4** `testdata/schema/store_api.golden` — regenerate; diff limited to `UnitRepo`'s widened
      method set.
      Verify: `make store-api-golden`.
      Requirement: R2.1's regeneration discipline, applied here.
- [ ] **3.5** Purity/lint: `golangci-lint run` (`ports-purity` — `internal/ports` now imports
      `internal/core/focus`, already exercised by `unitrepo.go`'s other core imports).
      Requirement: design §4.
- [ ] Verify (PR-level): `make check-all`; confirm diff touches only `internal/ports/unitrepo.go`,
      `internal/store/sqlite/{unitrepo,unitrepo_integration_test}.go`,
      `testdata/schema/store_api.golden`, `test/support/{memrepo,repocontract}/unitrepo.go`. No
      `docs/02-cognitive-core.md` delta. Target ≤120 impl+docs lines.

---

## PR 4a — `feat/brain-arm-at-capture` (~280 impl+docs)

Depends on PR 1+2. **Non-negotiable commit order inside this PR**: (1) retirement, (2) I04
test-rewrite (RED), (3) arming implementation (GREEN, wires `TriggerRepo`/`TimerRepo`).

- [ ] **4a.1** Commit 1 (retirement): delete `timerHookRefusal` (`capture.go:296-322`) and its call
      site (`:178-183`); delete `brain.OutcomeDeferred`, `brain.Deferred`, `CaptureResult.Deferred`
      (`result.go`); delete `ports.ActionCaptureHookDeferred` and its `AllDecisionActions()` entry
      (**R3**); narrow the switches in `internal/httpapi/capture.go` and `cmd/nooma/capture.go` to
      compile without the deleted outcome.
      Verify: `go build ./...` — this commit is not green on the whole suite by design (`capture_test.go`
      fixtures asserting `OutcomeDeferred` are expected red until 4a.3, not fixed in this commit).
      Requirement: design §3.5 ("its own commit").
- [ ] **4a.2** Commit 2 (RED, I04 strengthened — R4.4): rewrite
      `test/conformance/i04_timer_never_a_unit_test.go` — **delete lines 36-46's vacuity paragraph
      entirely**, not amend; assert via the real `TriggerRepo`/`TimerRepo` fakes wired into a test
      `CaptureService`: zero `units` rows, **exactly one `timers` row**, `CaptureResult.Outcome ==
      brain.OutcomeArmed` for a `timer` classification with a resolved `DueAt` after `now`; a
      structural sub-test iterates `reflect.TypeOf((*ports.TimerRepo)(nil)).Elem()`'s
      `NumMethod()`/`Method(i).Type.In/.Out` asserting none names `unit.Unit`, `*unit.Unit`, or
      `[]unit.Unit` — **never a hand-checked list of the interface's four method names**, the
      "correct guard entered from underneath" risk this shape closes.
      **Red**: `undefined: brain.OutcomeArmed` — the row-count sub-test fails to compile; the
      reflection sub-test already passes vacuously today (disclosed, `m2a` C9: `TimerRepo` already
      has no such method, and stays true).
      Stub: add `brain.OutcomeArmed` to `CaptureOutcome`/`AllCaptureOutcomes` with no producer yet —
      compiles; the row-count case fails first.
      Requirement: R4.4, R4.1.
      **Mutation**: revert the doc-comment deletion — no code mutation catches prose; the task's own
      completion check is `git diff` for this commit showing lines 36-46 removed, named explicitly
      here so it is checkable from the tree rather than assumed done.
- [ ] **4a.3** Commit 3 (GREEN): implement arming — `captureRunner.at` gains `triggers
      ports.TriggerRepo`, `timers ports.TimerRepo` (no clock field — `Arm(c, now)` reuses the
      instant the pipeline already read, satisfying `brain_single_clock_read_test.go` by
      construction); replaces the deleted fork with `prospection.Arm(classification, now)`; on
      `(Plan, true)`, persists via `TriggerRepo.Create`/`TimerRepo.Create` per `Plan.What`, calling
      `interruptColumn(Plan.Interrupt)` (new unexported helper, design §3.4) before
      `TriggerRepo.Create`; sets `CaptureResult{Outcome: OutcomeArmed, Armed: &Armed{What:
      Plan.What, ID: <created id>, FireAt: Plan.FireAt}}`.
      Verify: `go test ./internal/brain/... ./test/conformance/... -run I04`.
      Requirement: R4.1, R4.4; design §3.4, §3.5.
- [ ] **4a.4** `internal/brain/interrupt_test.go` (new, L1 white-box) — `interruptColumn ∘
      prospection.ResolveInterrupt` is the identity over `{nil, 0.0, PushThreshold, 1.0, a degraded
      resolution}` — no SQLite (design §3.4's L1 assignment).
      **Red/stub**: folded into 4a.3's own red/green split — disclosed rather than inventing a
      commit with no distinct red, since `interruptColumn` has no caller until arming exists.
      Requirement: design §3.4 (§8's L1 row).
      **Mutation**: make `interruptColumn` write `0.0` instead of SQL `NULL` for a degraded
      `Interrupt` — the identity breaks at the `nil` case, since re-resolving `0.0` reports
      `Degraded() == false`.
- [ ] **4a.5** `internal/httpapi/capture.go`, `cmd/nooma/capture.go` — restore totality with a real
      `OutcomeArmed` branch (HTTP: 200 naming the armed id/fire time; CLI: a one-line confirmation).
      Requirement: design §3.5 (the "compile-time-visible retirement" property, closed).
- [ ] **4a.6** `test/conformance/i18_arm_persists_distinct_instants_test.go` (new) — R4.3's own
      scenario: a classification whose `DueAt`, `EventAt`, `CreatedAt` are three distinct instants;
      after capture, the persisted `timers.fire_at` (`timer` case) equals `*DueAt` bit-for-bit, and
      the persisted `triggers.fire_at` (dated `event` case) derives from `EventAt`, never
      `CreatedAt` — read back from the fakes, not compared only against the in-memory `Plan`.
      Requirement: R4.3.
      **Mutation**: swap the `Create` call's `FireAt` argument for `classification.CreatedAt` — the
      three-distinct-instants fixture catches it; two distinct instants would not.
- [ ] **4a.7** `docs/02-cognitive-core.md` §5 step 5 amendment: replace the M1-era "not wired up yet"
      note with what arming does per `Kind`, cross-referencing `Arm`'s table (`m3a` §3.7, unchanged).
      Requirement: `CLAUDE.md` non-negotiable #1 (behavior-change delta, this PR touches no
      `internal/core`).
- [ ] **4a.8** `docs/02-cognitive-core.md` §8 amendment: the request text is stored verbatim and
      worded only at fire time (`m3d`'s); `rendered_text` stays NULL through this change (**G2**).
      Requirement: design §3.5 (`ArmTimer` row's contract).
- [ ] **4a.9** Purity/lint: `golangci-lint run` (`brain-boundary` — one `Now()` in the capture path).
      Requirement: `docs/06-harness.md`'s single-clock-read rule.
- [ ] Verify (PR-level): `make check-all`; confirm diff touches only
      `internal/brain/{capture,result,interrupt}{,_test}.go`, `internal/httpapi/capture.go`,
      `cmd/nooma/capture.go`, `internal/ports/decisionlog.go`,
      `test/conformance/{i04_timer_never_a_unit,i18_arm_persists_distinct_instants}_test.go`,
      `docs/02-cognitive-core.md`. **`git log` for this PR must read: retirement → I04 rewrite (red)
      → arming (green)** — CRITICAL if inverted. Target ≤280 impl+docs lines.

---

## PR 4b — `feat/brain-arm-refusal-audit` (~110 impl+docs)

Depends on PR 4a.

- [ ] **4b.1** Commit 1 (RED): `test/conformance/capture_arm_refusal_audit_test.go` (new) —
      table-driven over `classify.AllKinds()` (thirteen members) × {dated future, dated past,
      undated} × {rule present, absent}: `RefusalNoDate`/`RefusalAlreadyPast` → exactly one
      `capture.arm.refused` row with `Context.why` set and a distinct `Rationale` substring per
      member; `RefusalKindNotArming`/`RefusalNoKind` → zero additional rows (design's derived rule:
      a refusal writes a row exactly when the capture would otherwise leave no trace at all); a
      successful arm → exactly one `capture.armed.{timer,trigger,recurring_trigger}` row with
      `Plan.Why == RefusalNone`. The test asserts `len(classify.AllKinds()) == 13` at its own top.
      **Red**: `undefined: ports.ActionCaptureArmRefused` — every refused cell fails first (expects
      one row, gets zero; 4a shipped only the effect-arm path).
      Stub: add the constant with no writer — compiles; `RefusalNoDate` cell fails first.
      Requirement: R4.2.
      **Mutation**: replace the `classify.AllKinds()` iteration with a hand-written five-`Kind`
      subset — a fourteenth `Kind` added later would silently not appear; the `len() == 13` guard
      catches a silent narrowing of the iterated set itself.
- [ ] **4b.2** Commit 2 (GREEN): implement the `capture.arm.refused` write in `captureRunner.at`'s
      `(_, false)` branch, gated to `RefusalNoDate`/`RefusalAlreadyPast` only.
      Verify: `go test ./test/conformance/... -run CaptureArmRefusalAudit`.
      Requirement: R4.2; design §3.5.
- [ ] **4b.3** `docs/02-cognitive-core.md` §11 amendment: state the derived rule verbatim (refusal
      writes exactly when the capture would otherwise be traceless), not a fixed four-row table.
      Requirement: design §3.5.
- [ ] **4b.4** Purity/lint: `golangci-lint run`.
- [ ] Verify (PR-level): `make check-all`; confirm diff touches only `internal/brain/capture.go`,
      `internal/ports/decisionlog.go`, `test/conformance/capture_arm_refusal_audit_test.go`,
      `docs/02-cognitive-core.md`. Target ≤110 impl+docs lines.

---

## PR 5a — `feat/brain-due-scan` (~340 impl+docs, watch the 400 ceiling)

Depends on PR 1+2. **Finding G1's own correction governs this PR's action count and pipeline**: only
four due-scan `DecisionAction` members ship here (`check.trigger.expired`, `check.timer.fired`,
`check.timer.cancelled`, `check.conflict_skipped` — `check.trigger.fired` has no producer in `m3b`).

- [ ] **5a.1** Commit 1: `internal/core/prospection/staleness.go` — add `AllVerdicts() []Verdict`
      (four members, declared order — the `AllKinds`/`AllDecisionActions`/`AllCaptureOutcomes`
      pattern, twelve lines). **G3**: the one `internal/core` line in `m3b`, against R0's literal
      text; resolved because this states no new decision (§3.3 layer 3's own completeness accessor).
      Ships with its own inline existence check (`len(AllVerdicts()) == 4`) — the trivial-red case
      `m2a` C9 covers.
      Requirement: design §3.3 (layer 3); **G3**.
- [ ] **5a.2** Commit 2 (RED): `internal/brain/check_test.go` — `triggerTransition`/`timerTransition`,
      **iterated over `prospection.AllVerdicts()`**, never a hand-written switch: `VerdictPending`→
      `(_, false)` both; `VerdictDefer`→`(_, false)` trigger, and `timerTransition(VerdictDefer)`
      (the unreachable case per `TimerVerdict`) still returns a defined `(_, false)` rather than
      panicking; `VerdictStale`→`(TriggerStatusExpired, true)` / `(TimerStatusCancelled, true)`;
      `VerdictDeliver`→`(_, false)` trigger (**F1/G1**, corrected) / `(TimerStatusFired, true)`
      timer. The test asserts `len(prospection.AllVerdicts()) > 0` at its top.
      **Red**: `undefined: brain.triggerTransition/timerTransition`.
      Stub: both return `("", false)` unconditionally — compiles; `VerdictStale` fails first.
      Requirement: design §3.3 (layer 3); R5.2, R5.3.
      **Mutation**: hand-write the test's own four-case switch instead of iterating `AllVerdicts()`
      — a fifth `Verdict` added later to `staleness.go` compiles and is silently unchecked by a
      hand-written switch; the iterated test's fifth loop pass fails on the undefined mapping.
- [ ] **5a.3** Commit 3 (GREEN): implement `triggerTransition`/`timerTransition` per the corrected
      table; implement `CheckService`/`checkRunner` mirroring `ConsolidateService`'s split (one
      `ports.Clock` on `CheckService`, clockless `checkRunner.at(ctx, now)`); pipeline **per §3.6 as
      corrected by G1**: trigger `Stale`→`Expire`+`check.trigger.expired`; trigger
      `Pending`/`Defer`/`Deliver`→no write, no row; timer `Stale`→`Cancel`+`check.timer.cancelled`;
      timer `Deliver`→`Fire`+`check.timer.fired`; timer `Pending`→no write, no row.
      Verify: `go test ./internal/brain/...`.
      Requirement: R5.1, R5.2, R5.3; design §3.3 (corrected), §3.6 (corrected, **G1**).
- [ ] **5a.4** `test/conformance/i15_trigger_expires_not_fires_test.go` (extend, behavioural half) —
      a trigger swept across the staleness window through a real `checkRunner.at` over `memrepo`
      fakes: `expired` past the window and **never** `fired` at any swept instant.
      Requirement: R5.3 (I15 behavioural half).
      **Mutation**: route `VerdictStale`→`Fire` instead of `Expire` — the sweep fails at every
      instant past the window, not only at one sampled point.
- [ ] **5a.5** `test/conformance/check_effect_completeness_test.go` (new, I12 both directions) — for
      every `Verdict` **iterated via `AllVerdicts()`** crossed with {trigger, timer}: a writing
      outcome (`Stale`, timer-`Deliver`) writes **exactly one** row; a non-writing outcome
      (`Pending`, `Defer`, trigger-`Deliver`) writes **zero** — I12's symmetric half.
      Requirement: R5.1–R5.3; I12.
      **Mutation**: any branch writing a row for `VerdictDefer` or trigger-`VerdictDeliver` — caught
      by the zero-row assertion on exactly those cells.
- [ ] **5a.6** `docs/02-cognitive-core.md` §7 amendment: the due-scan's shape (one clock read,
      delegates every decision to `m3a`'s gates); the corrected trigger-`Deliver` behavior (**F1**:
      stays armed, no row — nothing in `m3b` can surface a fired trigger); `AllVerdicts()` as the
      completeness accessor.
      Requirement: R0 (this PR's `internal/core` touch, **G3** — same-PR delta per the ordering rule);
      design §3.3, §3.6.
- [ ] **5a.7** `test/integration/due_scan_status_vocabulary_test.go` (new, L3, Risk A's own
      mitigation) — after a scan over a real migrated vault seeded with fixtures across every
      `Verdict`, `SELECT DISTINCT status FROM triggers` and `FROM timers` return only members of
      `AllTriggerStatuses()`/`AllTimerStatuses()` — the constraint the schema does not carry (§1,
      no `CHECK`).
      Requirement: Risk A (accepted, no migration — "the L3 test is the constraint").
      **Mutation**: map `VerdictStale`→`"stale"` in `triggerTransition` instead of
      `TriggerStatusExpired` — every other test in the suite stays green; only this L3 test fails,
      exactly the RED design names as the one to watch.
- [ ] **5a.8** Purity/lint: `golangci-lint run` (`brain-boundary` — one `Now()` in `check.go`).
      Requirement: `docs/06-harness.md`'s single-clock-read rule.
- [ ] Verify (PR-level): `make check-all`; confirm diff touches only
      `internal/core/prospection/staleness{,_test}.go`, `internal/brain/check{,_test}.go`,
      `test/conformance/{i15_trigger_expires_not_fires,check_effect_completeness}_test.go`,
      `test/integration/due_scan_status_vocabulary_test.go`, `docs/02-cognitive-core.md`. Target
      ≤340 impl+docs lines. **If measured lines threaten 400**, design's own pre-drawn fallback
      applies: split at the trigger half (5a.1–5a.2 partial, 5a.3's trigger branch, 5a.4) | the
      timer half (5a.2's remaining cases, 5a.3's timer branch), as a ninth PR
      `feat/brain-due-scan-timers` — report before splitting, per house convention (**R6**).

---

## PR 5b — `feat/brain-due-scan-conflict` (~90 impl+docs)

Depends on PR 5a. **R6/R7** (proposal), double-firing guard.

- [ ] **5b.1** Commit 1 (RED): `internal/brain/check_test.go` (extend) — pre-seed a trigger already
      `expired`, feed a due window that would have matched it had it still been `armed` (simulating
      a race where `Due` returned it before a concurrent writer moved it); assert `checkRunner.at`
      does not abort, records exactly one `check.conflict_skipped` row, and still processes a second,
      unrelated due row in the same pass.
      **Red**: `checkRunner.at` (5a.3) propagates the conflict error today and aborts the whole
      pass — the "second row still processed" assertion fails.
      Requirement: R5.4.
      **Mutation**: revert the conflict arm to propagate instead of record-and-continue — the
      second, unrelated row's own row-count assertion fails.
- [ ] **5b.2** Commit 2 (GREEN): implement the conflict arm in `checkRunner.at` — catch
      `ErrTriggerStatusConflict`/`ErrTimerStatusConflict`, record `check.conflict_skipped`, continue;
      any other error still aborts (`persistArchiveTransitions`'s verbatim shape).
      Verify: `go test ./internal/brain/... -run Conflict`.
      Requirement: R5.4; design §3.6.
- [ ] **5b.3** Commit 1 (RED, L3): `test/integration/due_scan_concurrent_test.go` (build tag
      `integration`) — two goroutines running `checkRunner.at` against **one real migrated vault**
      (SQLite's own `_txlock=immediate` DSN — a `memrepo` fake proves nothing about the race),
      `go test -race`, over one armed trigger past its staleness window: exactly one `expired` row,
      exactly one `check.trigger.expired` row, exactly one `check.conflict_skipped` row.
      **Red**: before 5b.2, both goroutines abort the whole pass on the loser's unhandled error —
      the exactly-one-`conflict_skipped` assertion fails.
      Requirement: R5.4 (L3, "checked to be able to run it").
      **Mutation**: run this identical test body against `memrepo` instead of real SQLite — the
      race claim is unfalsifiable there; **the "correct guard entered from underneath" risk this
      task is written to close** — a race test passing against a fake proves nothing about the SQL
      `WHERE`-clause precondition it claims to guard, which is why this task is pinned to L3 and not
      folded into 5a's L2 suite.
- [ ] **5b.4** `docs/02-cognitive-core.md` §11 amendment: a scan-time transition conflict is
      recorded and skipped, never fatal to the pass.
      Requirement: R5.4; design §3.6.
- [ ] **5b.5** Purity/lint: `golangci-lint run`.
- [ ] Verify (PR-level): `make check-all`; confirm diff touches only `internal/brain/check{,_test}.go`,
      `internal/ports/decisionlog.go`, `test/integration/due_scan_concurrent_test.go`,
      `docs/02-cognitive-core.md`. Target ≤90 impl+docs lines.

---

## PR 6 — `feat/cli-check` (~220 impl+docs, widened for Q1's `--dry-run`)

Depends on PR 5a+5b.

- [ ] **6.1** Commit 1 (RED): `cmd/nooma/check_test.go` — `runCheck`'s flag/lock/open shape mirroring
      `runConsolidate`'s own test: a nonexistent vault fails with `ResolveVault`'s own error; a held
      lock fails with `vaultlock.InUseError` naming the holder; more than one positional argument is
      refused before anything is opened.
      **Red**: `undefined: runCheck` (name confirmed at apply time, `runConsolidate`'s own pattern).
      Stub: `func runCheck(args []string) error { return nil }` — compiles; nonexistent-vault case
      fails first.
      Requirement: R6.1.
      **Mutation**: make the >1-argument case a silent no-op (uses the first arg) instead of a
      refusal — caught directly by the fixture's specific "too many arguments" error assertion.
- [ ] **6.2** Commit 2 (GREEN): implement `cmd/nooma/check.go` — flags, `config.ResolveVault`,
      `loadVaultConfig`, `vaultlock.Acquire`, `sqlite.Open`, `wireCheck` (new in
      `cmd/nooma/wiring.go`), run `CheckService.Check(ctx)`, render `renderConsolidateReport`'s
      posture (one unconditional scanned-count line, then one line per non-empty outcome, silence
      otherwise). Registered via `init()`, `consolidate.go:19-29`'s exact pattern. **No pre-lock
      provider resolution, no `--phase`-style flag** — design §3.8's two named differences, stated
      in a doc comment against reflexive copy-paste.
      Verify: `go test ./cmd/nooma/... -run Check`.
      Requirement: R6.1; design §3.8.
- [ ] **6.3** Commit 1 (RED, Q1): `cmd/nooma/check_test.go` (continued) — `--dry-run`, seeded with a
      due timer + a stale trigger: `nooma check --dry-run` reports the same counts and the same
      "would fire"/"would expire" lines as a wet run's own report, and a subsequent real `nooma
      check` (no flag) against the **same, untouched** vault still finds both rows in their original
      `armed`/`pending` status — proving the dry run took the identical decision path and merely
      suppressed the write.
      **Red**: `undefined:` the `--dry-run` flag / the commit-suppressing parameter; `runCheck`
      always commits today — the "vault unchanged after `--dry-run`" half fails first.
      Stub: flag parsed and ignored — compiles; that assertion fails first.
      Requirement: Q1 (owner decision 2026-08-21 — "the flag suppresses the effect, it does not
      branch the logic").
      **Mutation**: implement `--dry-run` as a second, independently-derived report function
      instead of gating `checkRunner`'s own persistence calls — this task's own fixture (identical
      report content between dry and wet runs) is shaped to catch exactly that divergence.
- [ ] **6.4** Commit 2 (GREEN): thread a `commit bool` into `checkRunner.at(ctx, now, commit)` that
      gates only `Fire`/`Expire`/`Cancel`/`record` — verdict evaluation and report construction are
      identical in both modes, satisfying Q1 literally.
      Verify: `go test ./cmd/nooma/... -run DryRun` and `go test ./internal/brain/... -run
      CheckDryRun`.
      Requirement: Q1; design §3.6, extended by this task beyond its original snippet — recorded
      here as this task's own scope addition, not folded silently into PR 5a.
- [ ] **6.5** `test/e2e/check_demo_test.go` (new, L4) — a real migrated vault seeded with one
      due-not-stale timer and one overdue-past-threshold trigger; run the compiled `nooma check`
      binary; read `decision_log` and assert the timer fired, the trigger expired, both carrying a
      human-readable `Rationale` — the change's own exit criterion, executable; a source-tree grep
      in the same test file asserts no file under `internal/channels/**`/`internal/scheduler/**` and
      no Telegram credential/token/chat id anywhere in this PR's diff.
      Requirement: R6.1; spec's Exit criterion; §9's Telegram-boundary note.
      **Mutation**: seed only the due timer, drop the trigger fixture — the "expired" half becomes
      unfalsifiable; both fixtures are required together for exactly this reason.
- [ ] **6.6** `docs/01-architecture.md:157-161` command-table amendment: one new row after `nooma
      consolidate`, per design §3.8's exact text.
      Requirement: R6.1.
- [ ] **6.7** Purity/lint: `golangci-lint run` (confirm `--dry-run` is a plain boolean, no untrusted
      vocabulary string routed through it).
      Requirement: design §9 (the one applicable threat-matrix row).
- [ ] Verify (PR-level): `make check-all`; confirm diff touches only
      `cmd/nooma/{check,check_test,wiring,main}.go`, `internal/brain/check{,_test}.go` (the
      `commit bool` widening), `test/e2e/check_demo_test.go`, `docs/01-architecture.md`. Target
      ≤220 impl+docs lines (widened from design's 180 to cover Q1's `--dry-run`, reported here).

---

## Review Workload Forecast

| Field | Value |
|-------|-------|
| Estimated changed lines | ~1,700 budgeted impl+docs across 8 PRs (design §7, PR 6 widened +40 for Q1); test lines not separately budgeted but historically ran *under* budget on this project's `prospection`-only PRs (e.g. `m3a` PR 2: 143 measured vs 370 budgeted) — `m3b`'s store/I-O PRs have no equivalent precedent, tracked per PR |
| 400-line budget risk | **Medium overall.** PR 5a is the one PR at genuine risk (budgeted 340, design's own stated concern); PR 1/2 (~250 each), PR 4a (~280), PR 6 (~220) comfortably under; PR 3/4b/5b (≤120) low risk |
| Chained PRs recommended | Yes — eight links, already a chain by design |
| Suggested split | PR 5a's fallback is pre-drawn by design (task 5a's own PR-level verify note): trigger half \| timer half, as a ninth PR `feat/brain-due-scan-timers` — apply only if measured lines threaten 400, report before splitting |
| Delivery strategy | `auto-chain` |
| Chain strategy | `stacked-to-main` |

Decision needed before apply: No
Chained PRs recommended: Yes
Chain strategy: stacked-to-main
400-line budget risk: Medium

### Suggested Work Units

| Unit | Goal | Likely PR | Focused test command | Runtime harness | Rollback boundary |
|------|------|-----------|----------------------|-----------------|-------------------|
| 1 | `TriggerRepo`/`TimerRepo` ports, vocabularies, fakes | PR 1 | `go test ./test/support/repocontract/...` | N/A — no L3/L4 row of its own, PR 2 owns it | Delete both port files + fakes; nothing depends on them yet |
| 2 | SQLite implementations, `anchorJSON`, golden | PR 2 | `go test -tags=integration ./internal/store/sqlite/... -run TriggerRepo\|TimerRepo` | `internal/store/sqlite/*_integration_test.go` against a real migrated vault | Delete both repo files; depends on PR 1 only |
| 3 | `LiveFocusCandidates`, positive-`pool` SQL | PR 3 | `go test -tags=integration ./internal/store/sqlite/... -run FocusCandidates` | Same integration harness as PR 2 | Delete the method + its SQL; independent of everything after it |
| 4a | Arming at capture, I04 strengthened, I18 persisted | PR 4a | `go test ./test/conformance/... -run I04` | `test/e2e` unaffected until PR 6 | Revert restores `timerHookRefusal`; retired symbols return with it (one commit) |
| 4b | Refusal audit trail | PR 4b | `go test ./test/conformance/... -run CaptureArmRefusalAudit` | N/A — folded into PR 6's L4 | Delete the one write site; PR 4a unaffected |
| 5a | `CheckService`, transition mappings, `AllVerdicts()` | PR 5a | `go test ./internal/brain/... -run Transition` | `test/integration/due_scan_status_vocabulary_test.go` (L3, Risk A) | Delete `check.go`; armed rows sit inert, untouched by anything else |
| 5b | Conflict handling, concurrency proof | PR 5b | `go test ./internal/brain/... -run Conflict` | `test/integration/due_scan_concurrent_test.go` (`-race`) | Delete the conflict arm; PR 5a's happy path unaffected |
| 6 | `nooma check`, `--dry-run`, wiring | PR 6 | `go test ./cmd/nooma/... -run Check` | `test/e2e/check_demo_test.go` — the milestone's own exit criterion | Delete `check.go`/wiring; PR 5a/5b's service is still callable from tests |

---

## Traceability

| Spec section | Requirements | Tasks |
|---|---|---|
| R0 (core purity) | R0 | see **G3** — one named, argued exception |
| §1 `TriggerRepo`/`TimerRepo` | R1.1–R1.3 | 1.1–1.7, 1.4–1.5 (see **R1**, **R2**, **G2**) |
| §2 SQLite implementations | R2.1 | 2.1–2.8 |
| §3 Focus-candidate query | R3.1 | 3.1–3.5 (see **R5**) |
| §4 Arming at capture | R4.1–R4.4 | 4a.1–4a.9, 4b.1–4b.4 (see **R3**, **R4**) |
| §5 Due-scan runner | R5.1–R5.4 | 5a.1–5a.8, 5b.1–5b.5 (see **G1**, **F1**, Risk A) |
| §6 `nooma check` | R6.1 | 6.1–6.7 (see Q1, Q2) |
| §7 Out of scope | — | not tasked; `m3c`/`m3d`'s own |
| Exit criterion | — | task 6.5 |

---

## Handoffs to `m3c`/`m3d` (design §11/§12, carried forward)

- **`m3d` owns real trigger delivery.** A `VerdictDeliver` trigger stays `armed` under `m3b` (**F1**);
  `m3d` supplies `Interrupt.Route()`'s push/digest split and quiet-hours-aware routing, then calls
  `TriggerRepo.Fire` for the first time in the chain — the method ships in PR 1 with no production
  caller in `m3b`, `LiveFocusCandidates`'s own precedent (**R5**).
- **`m3d`'s fire-time rephrasing** is the first production caller of a `rendered_text` write on
  `TimerRepo` — **G2**'s resolution (no parameter in `m3b`) means `m3d` widens `Fire`'s signature or
  adds a `MarkRendered` method when it has that real caller, the same trade design took for M4's
  `dismissed` transition.
- **Risk E (no `timers` index)** is unmitigated in `m3b` (**Q2**, accepted for v1) and becomes hot
  the moment `m3d`'s tick runs every five minutes; revisit if M4's activity view makes the scan hot.
- **`check.trigger.fired`** (**G1**) is not a member of `AllDecisionActions()` after `m3b`; `m3d` adds
  it as its own vocabulary member when real trigger delivery lands, with its own producer from day
  one — never re-adding a bucket the glass box could not show, R3's own argument applied forward.

---

## Reconciliation notes carried forward from design.md (not reopened here)

- **F1** (design, corrected in §3.3, dated 2026-08-21): a `VerdictDeliver` trigger stays `armed`;
  only a timer fires. Governs task 5a.2–5a.5, 5a.7 and **G1** above.
- **Owner decisions, 2026-08-21**: Risk A accepted (task 5a.7 is the constraint); Q1 accepted, shaping
  tasks 6.3–6.4; R1/R2 accepted as designed (tasks 1.1–1.5); Q2 accepted for v1 (task 2.6, named
  not mitigated).
