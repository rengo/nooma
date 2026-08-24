# Tasks — M3 Phase D: delivery, check-ins, and the demo

Implementation task list for `m3d-delivery-demo`, derived from `spec.md` (R0–R7.1) and `design.md`
(§1–§8), both read in full before this document. Design §4 fixes the slicing — **eight PRs**,
stacked, ~2,750 budgeted impl+docs lines.

Chain strategy **`stacked-to-main`**, delivery **`auto-chain`**. Order: `1 → 2 → 3 → 4 → 5`, then
`6 → 7` (the check-in pair, which needs 2), then `8` (needs everything).

**Strict TDD is active.** Every behavioural task states the two-commit RED/GREEN shape, the
conformance/L1 test commit is strictly ahead of the implementation commit in every PR, and **every
RED/L2/L3 task carries a `Mutation:` line** naming the code change that must make it fail.

---

## Findings — spec/design disagreements and gaps found in this session

**J1 — the umbrella's seven-PR sketch for `m3d` cannot be built, because `ports.TriggerRepo` cannot
express delivery.** `m3b` shipped `Create`, `Due`, `Fire`, `Expire`. Spec R2.1 must write
`surfaced_at`, R3.1 must find fired-but-undelivered triggers, and R5.1 must find and resolve an open
check-in. **None is expressible.** Design §3.2 adds four methods and a fourth vocabulary
(`TriggerResolution`), and this task list gives them their own PR — `feat/ports-trigger-delivery`,
new relative to proposal §5.1's list. Found by reading the port against the spec, not by
implementing into it.

**J2 — `m3b` left `surfaced_at` NULL deliberately and said so; `m3d` is what fills it.** Not a
disagreement, recorded because it reads like an omission in `m3b`'s diff and is not. `m3b`'s
`TriggerRepo.Fire` doc comment states it: *"surfaced_at is untouched and stays NULL — 'pending
delivery' (migration 0001:52) is m3d's to close."*

**J3 — nothing persists a digest deferral count, and `MaxDigestDeferrals` needs one.** `m3a` shipped
the constant and `Carry` consumes a count it is given; no artifact says where the count lives.
Design §3.4 derives it from `decision_log` rather than adding a column, and states the cost. Recorded
because "the constant exists" reads as "the mechanism exists".

**J4 — proposal §5's `m3e` threshold is measured and not crossed.** *"If the forecast exceeds nine
PRs or 3,200 lines, the check-in pair splits off as `m3e-checkins`."* Eight PRs, ~2,750 lines. The
check-ins stay, and this is reported rather than silently assumed.

---

## PR 1 — `feat/scheduler-proactive-tick` (~300 impl+docs)

- [x] **1.1** Commit 1 (RED): `internal/scheduler/proactive_test.go` — over the existing fake timer:
      a tick calls the injected `ProactiveChecker`; a second tick while one is running is skipped and
      logged; **a running consolidation does NOT skip a proactive tick**.
      **Red**: `undefined: scheduler.ProactiveChecker`.
      Requirement: R1.1.
      **Mutation**: share one guard between the two jobs — the third assertion fails, and it is the
      one that matters: a shared guard lets a nightly pass suppress twelve hours of checks.
- [x] **1.2** Commit 2 (GREEN): the second cron job, its own guard, `Deps.ProactiveCheck`, failure
      and panic posture matching the consolidation job's.
      Requirement: R1.1.
- [x] **1.3** `docs/02-cognitive-core.md` §13 row 918 splits; the `proactive_check` half names its
      constant, the consolidation half's prose states **why it is unchecked** (the gate's regex never
      reaches `internal/scheduler`).
      Requirement: R1.2; owner ruling 5.
      **Mutation**: write the consolidation half as though splitting fixed it — the new L2 test
      asserting the prose names the blind spot fails.
- [x] **1.4** Purity/lint. Verify (PR-level): `make check-all`. Target ≤300.

**J5 — nothing in this repository parses a cron expression, and two config keys look like they
control scheduling while controlling nothing.** Spec R1.1 says the tick is "driven by
`schedules.proactive_check`". It cannot be: `internal/scheduler` hardcodes `ConsolidationHour = 3`
and `runCron` computes its next instant from that constant, never from `schedules.consolidate` —
which is decoded by `internal/config` and read by **nobody**, and has been since M0. Adding a cron
parser for one five-minute interval the docs already fix would be inventing a configuration surface
nobody asked for, so `ProactiveCheckInterval` is a constant beside `ConsolidationHour`, exactly as
the existing shape does it. **The pre-existing gap is the finding**: `schedules.consolidate` and
`schedules.proactive_check` are inert keys in a real config file, and a user editing one would
change nothing and be told nothing. Recorded for the owner rather than fixed here — fixing it is
either a parser or a deprecation, and both are their own work unit.

**J6 — §13's row 918 blamed the wrong thing, and splitting it proved that rather than fixing it.**
The row said the consolidation half is unchecked because the Default cell's leading `03:00` parses
as `03`, and that splitting the row so the half "can be checked" was M3's job. Split, the half still
cannot be checked: `calibration_doc_test.go`'s regex matches `internal/core/…` only and never reaches
`internal/scheduler` at any value. Proposal §4.3 had already spotted this; what this PR adds is the
correction landing in the row itself, plus a test that keeps it corrected. **A row which is neither
verified nor marked reads as coverage**, which is the state both scheduler rows were in.

---

## PR 2 — `feat/ports-trigger-delivery` (~350 impl+docs) — **J1's own PR**

- [x] **2.1** Commit 1 (RED): `test/support/repocontract/triggerrepo.go` (extend) — `Surface` writes
      `surfaced_at` and the row leaves `Undelivered` and enters `Delivered`; `Resolve` writes
      `responded_at` and `resolution` under a surfaced-and-unanswered precondition, returning the
      named conflict otherwise; `AllTriggerResolutions()` pinned to migration `0001:54`'s comment.
      **Red**: `undefined: ports.TriggerResolution` and the four methods.
      Requirement: design §3.2, §3.3.
      **Mutation**: let `Resolve` succeed on an unsurfaced trigger — a check-in could resolve
      something the user never saw.
- [x] **2.2** Commit 2 (GREEN): `internal/ports/triggerrepo.go` + `memrepo` + `internal/store/sqlite`,
      the precondition in the `UPDATE`'s `WHERE` and nowhere else (`m3b` §3.6's rule).
      Requirement: design §3.2.
- [x] **2.3** `testdata/schema/store_api.golden` regenerated.
- [x] **2.4** L3: the four methods over a real vault; `SELECT DISTINCT resolution` yields only
      vocabulary members — the constraint the schema does not carry.
      **Mutation**: map an outcome to a literal outside the vocabulary — only this test fails.
- [x] **2.5** Purity/lint. Verify (PR-level). Target ≤350.

**J7 — widening a port breaks every hand-written fake that implements it, and this one broke three
inside `internal/brain`.** `conflictingTriggers`, `failingTriggers` and `emptyTriggers` (m3b PR 5b's
own fixtures for the conflict arm) stopped compiling the moment `ports.TriggerRepo` grew four
methods. They gain the four as no-ops with a comment saying why — they are about the due scan, not
about delivery. **Not a defect, and worth recording as a cost**: the alternative to hand-written
fakes is `memrepo`, which those tests deliberately do not use because they need a repository that
fails on command. The cost of that choice is paid every time the port widens, and it is small; the
finding is that it is not zero, and a fourth widening would pay it again.

---

## PR 3 — `feat/brain-push-delivery` (~350 impl+docs)

- [x] **3.1** Commit 1 (RED): `internal/brain/deliver_test.go` — a fired trigger with a push-routed
      interrupt produces exactly one `Send` and one `Surface`; a digest-routed one produces neither;
      a **failing** `Send` produces no `Surface` and one `decision_log` row.
      **Red**: `CheckService` sends nothing today.
      Requirement: R2.1.
      **Mutation**: write `Surface` before the `Send` — the failing-send case then records a
      delivery the user never saw, which is the whole reason `m3b` left the column NULL.
- [x] **3.2** Commit 2 (GREEN): the delivery step inside `checkRunner.at`, gated by `commit` so
      `--dry-run` suppresses sends (design §3.1, D5).
      Requirement: R2.1; design §3.1.
- [x] **3.3** Commit 1 (RED, I16): `test/conformance/i16_*_test.go` (extend) — a trigger **swept**
      across the quiet-hours boundary is deferred inside and delivered on the first pass outside; a
      timer at the same instants is delivered throughout.
      Requirement: R2.2.
      **Mutation**: pass `deferInQuietHours = true` for timers — the timer half fails at every swept
      instant inside the window. **Sweep, not sample**: `m3b`'s G16 and G22 both came from sampling
      a boundary that has two regimes.
- [x] **3.4** Purity/lint. Verify (PR-level). Target ≤350.

**J8 — `ports.DueTrigger` could not carry a delivery's text, and `m3b` had a stated reason for that
which no longer holds.** `m3b` §3.1 made the read shape narrow deliberately: *"the core decision
consumes `(fireAt, now)`, so there is no core struct to reach for."* A payload would have been a
field with no reader. Delivery is a reader. `DueTrigger` gains `Payload`, and the alternative — a
second query per trigger to fetch text the first query already had in hand — is worse in the way
that matters. Recorded because the field looks like scope creep and is the opposite: the narrow
shape was correct exactly until this PR.

**J9 — the first implementation reported a failed send as a delivery, and only the failed-send test
caught it.** `deliver` returned `error`, and `fireAndDeliver` counted a delivery whenever it returned
nil — which it does after *recording* a failure, since a recorded failure is not an error the pass
stops for. `deliver` now reports whether the message actually went out. **Every other test in the PR
passed through the bug**: the happy path sends and counts one, the digest path never reaches
`deliver` at all. The count was only wrong in the case the count exists for.

**J10 — I16's sweep caught its own fixture before it caught any code.** Written with a fixed
`fire_at`, every swept hour outside quiet hours was past `TriggerStalenessHours` and so returned
`VerdictStale`, not `VerdictDeliver` — the sweep covered seven hours inside the window and **zero**
outside. Its own closing guard ("a sweep that only reaches one regime is a sample") failed the test
rather than letting it pass with seven-tenths of the claim untested. The fixture now moves `fire_at`
with the swept hour, so the only thing changing across the sweep is the window.

---

## PR 4 — `feat/brain-digest-assembly` (~400, watch the ceiling)

- [x] **4.1** Commit 1 (RED): one digest per day — two passes after `DigestHour` on one day produce
      one `Send`; a pass the next day produces another; a pass before the hour produces none.
      Requirement: R3.1.
      **Mutation**: drop `DigestDue`'s last-digest check — the same-day case sends twice, and at a
      five-minute cadence that is 288 messages a day (design §6's amplification row).
- [x] **4.2** Commit 2 (GREEN): assembly from `Undelivered`, one `Send`, `Surface` per included item.
      Requirement: R3.1.
- [x] **4.3** Commit 1 (RED, I09): `Carry`'s outcome persisted — a low-energy day carries
      `LowEnergyDigestSize` and holds the rest; a held item's count rises; an item held
      `MaxDigestDeferrals` times is carried **regardless of energy**.
      Requirement: R3.2; **J3**.
      **Mutation**: count deferrals in memory — the anti-starvation leg passes until a restart, so
      the test drives the count through the `decision_log` read rather than through the service's
      own state.
- [x] **4.4** Commit 2 (GREEN): `LiveFocusCandidates` for the ordering (owner ruling 4), the
      `decision_log`-derived deferral count (design §3.4), a nil `Candidate` for `pattern_based`.
      Requirement: R3.2.
      **Mutation**: pass a NULL `unit_id` to `LiveFocusCandidates` — `m3b`'s port doc comment names
      this as the caller's obligation and this is the caller.
- [x] **4.5** `ports.StateRepo` gains the energy read; **nil is not low**.
      Requirement: R3.3.
      **Mutation**: return a zero `EnergyReading` instead of nil for "no reading" — a vault with no
      check-ins silently stops speaking, and only the no-reading case fails.
- [x] **4.6** Purity/lint. Verify (PR-level). Target ≤400. **If measured lines exceed 400**, design
      §4's cut applies: assembly and send (4a) from the care gates and deferral counting (4b).
      **Report before splitting.**

**J11 — `m3a` left one question explicitly for `m3d`, and it is now decided: an empty digest is not
sent.** `Carry`'s own doc comment says *"Carry takes no position on whether an empty result is
delivered … Whether a digest carrying nothing is sent at all is m3d's decision."* **Decided: not
sent.** A message every morning saying nothing happened is a message people learn to ignore — and
then the one that matters arrives in the shape they learned to ignore. Recorded as this PR's own
decision rather than as an implementation detail, because it is the question `m3a` deliberately did
not answer.

**J12 — `LatestEnergy` must read the most recent row that HAS an energy value, not the most recent
row.** `current_state.energy` is nullable and `m2`'s load watcher writes rows with it NULL **by
design** (`OpenHypothesis` sets energy NULL). `ORDER BY recorded_at DESC LIMIT 1` alone would return
a hypothesis row and report "no reading" on a vault that has one — every time a hypothesis was
opened after the user last answered, which is exactly when the care gate matters. The `WHERE energy
IS NOT NULL` is the whole fix and would have been invisible without reading `m2`'s writer.

**J13 — the deferral count makes `decision_log` load-bearing rather than decorative, and a mutation
proves it.** Suppressing the `check.digest.held` rows leaves every behavioural assertion green and
fails only the count leg: a held item would reset its own patience every morning and never reach
`MaxDigestDeferrals`. Design §3.4 chose the derivation over a column; what this PR adds is that the
choice has a test which fails when the rows stop being written, so nobody can later "tidy up" the
held rows as noise.

**J14 — PR 4 measured under budget and design §4's pre-drawn cut was not taken.** Reported rather
than assumed: the cut was assembly-and-send from care-gates-and-counting.

---

## PR 5 — `feat/brain-timer-fire-rephrase` (~300 impl+docs)

- [x] **5.1** Commit 1 (RED): with `fakeprovider` — a fired timer's `action_text` is rephrased into
      `rendered_text` and the rephrasing is delivered; `action_text` is **unchanged**; a provider
      failure delivers `action_text` verbatim and records the degradation; a NULL `action_text`
      makes **zero** provider calls.
      Requirement: R4.1.
      **Mutation**: overwrite `action_text` with the rephrasing — doc 02 §8's "stored verbatim" is
      lost and the user's own words are gone for good.
- [x] **5.2** Commit 2 (GREEN): the rephrasing call, its fallback, the generic-nudge short circuit.
- [x] **5.3** Commit 1 (RED): the delay caveat **swept** across `DelayCaveatMinutes`, appearing at
      and above it and not below.
      Requirement: R4.2.
      **Mutation**: make the comparison strict — the boundary instant fails, and `m3a`'s F6 already
      decided this boundary is inclusive on purpose.
- [x] **5.4** Purity/lint. Verify (PR-level). Target ≤300.

**J15 — `TimerRepo.Fire` grew the parameter `m3b`'s finding G2 predicted, and grew it rather than
gaining a sibling method.** G2 resolved a spec/design disagreement in the design's favour on the
grounds that `rendered_text` had *"no caller [that] would ever pass a value through"*, and said
plainly: *"m3d's fire-time rephrasing adds the write when it has a real caller."* This is that
caller. The parameter beat a separate `Render(id, text)` for the same reason `fired_at` is written
BY `Fire` and not after it — one statement moving `status`, `surfaced_at` and `rendered_text`
together makes a fired row with no delivered wording **unrepresentable**, where two statements would
make it merely unlikely.

**J16 — an empty provider answer counts as a failed rephrasing, and no artifact said so.** R4.1
covers "a rephrasing failure" and the natural reading is a returned error. A provider that answers
with whitespace has returned no error and worded nothing; delivering that would be worse than
delivering nothing, since the user would get a blank where their own reminder should be. Treated as
the failure path, which delivers `action_text` verbatim and records the degradation.

**J17 — the delay caveat lives in two places, deliberately, and the split is the finding.** A
successful rephrasing is ASKED (in the prompt) to acknowledge the delay in its own sentence; the
fallback paths APPEND one. Appending to a rephrasing that was already asked for it would say the
same thing twice, in two voices. Neither spec nor design named the interaction — R4.2 says "the
delivered text mentions the delay" and is silent on which layer does it when there are two.

---

## PR 6 — `feat/brain-checkin-nudge-task` (~350 impl+docs)

- [x] **6.1** Commit 1 (RED): a capture carrying a `nudge_outcome` resolves the most recent open
      check-in — `responded_at` and `resolution` written — **iterated over
      `classify.AllNudgeOutcomes()`**, never listed; an answer with nothing open writes one row and
      changes nothing.
      Requirement: R5.1.
      **Mutation**: hand-write the outcome switch — a fifth member added later is silently unhandled;
      the iterated test fails on the loop pass with no expectation.
- [x] **6.2** Commit 2 (GREEN): the resolution path in the capture pipeline, ambiguity resolved to
      the most recent with the choice recorded (design §3.6, D4).
- [x] **6.3** The same for `task_checkin_outcome`, iterated over its own `AllX()`.
- [x] **6.4** Purity/lint. Verify (PR-level). Target ≤350.

**J18 — `snooze` resolves nothing, and spec R5.2's wording does not cover it.** R5.2 says *"a
`task_checkin_outcome` resolves an open task check-in"*. Two of the three do. **Snooze means "ask me
later"**: the check-in is neither engaged nor declined, and `ports.AllTriggerResolutions()` has no
member for it — deliberately, since the vocabulary is what a nudge *ended as*. Forcing snooze into
`declined` would record an answer the user did not give; forcing it into `engaged` is worse. It
leaves the check-in open, so the next digest or push asks again, which is what was requested.
Recorded because "resolves" reads as total over the vocabulary and is not.

**J19 — a channel-less digest was marking items delivered, and `nooma check` is what found it.** The
push path already declines to `Surface` anything when `r.channel == nil`; the digest did not, and
the asymmetry was worse there: a channel-less digest surfaces **every carried item at once**,
recording a delivery nobody received and removing them from tomorrow's digest forever. Found by the
CLI test, because `wireCheck` passes a nil channel by design (a subcommand that messaged the user as
a side effect of being run manually would be a surprise) — so the one caller that exercises the
nil-channel path is a subcommand nobody thought of as a delivery test.

**J20 — G22's wall-clock fragility recurred at a different offset, in two more fixtures.** Both the
CLI dry-run test and the L4 demo seeded a "stale" trigger at `TriggerStalenessHours + 2` hours ago.
`DeliverableFrom` shifts a `fire_at` that fell inside quiet hours to that day's 07:00, so that offset
is only reliably overdue at *some* hours of the day — at others the trigger is deliverable, fires,
and the expiry assertion fails. Both now seed two days back, which is stale from any shift, and both
were verified under `TZ=UTC` and `TZ=Asia/Tokyo`. **Third occurrence of the same class**: a fixture
whose meaning depends on the wall clock is fragile even when the assertion looks absolute.

---

## PR 7 — `feat/brain-checkin-relation-state` (~400 impl+docs)

- [ ] **7.1** Commit 1 (RED, **I10**): a denied relation is **weakened, never deleted** — asserted
      against the relation's own row, plus the I03-shaped reflection check that `RelationRepo`
      offers no removal verb.
      Requirement: R5.2; I10.
      **Mutation**: delete the relation on denial — I10's own invariant, and the reflection half
      cannot catch a `DELETE` in SQL, which is why the row assertion exists beside it.
- [ ] **7.2** Commit 2 (GREEN): `relation_outcome` resolution.
- [ ] **7.3** `state_outcome` resolution writing into `current_state`; `ports.StateRepo` widened
      (R5.3), no removal-prefixed method.
- [ ] **7.4** Purity/lint. Verify (PR-level). Target ≤400.

---

## PR 8 — `feat/serve-wiring-and-demo` (~300 impl+docs)

- [ ] **8.1** Commit 1 (RED): `runServe` starts the channel and the tick; **a vault with Telegram
      disabled starts and runs unchanged**.
      Requirement: R6.1.
      **Mutation**: make the channel required — a disabled-Telegram vault fails to start, which is
      every existing deployment.
- [ ] **8.2** Commit 2 (GREEN): the wiring, and shutdown in the order poller → scheduler → server
      (design §3.8, D6).
      **Mutation**: stop the server first — a pass mid-delivery can send without recording, because
      `Surface` needs the store the shutdown just closed.
- [ ] **8.3** Commit 1 (RED, L4): **the exit criterion** — one simulated day against a fake Telegram
      and a fake provider: a capture arms; a push is delivered; a quieter one is held and arrives in
      the digest at the digest hour; a timer fires, is worded at delivery and mentions its delay; an
      answer resolves the check-in. `decision_log` tells all of it, and **nothing is delivered during
      quiet hours except the timer**.
      Requirement: R7.1; the Exit criterion.
      **Mutation**: drop any one of the five acts — the demo is the only place they are asserted
      together, and together is the claim.
- [ ] **8.4** `docs/05-build-plan.md` — M3's own success criteria ticked.
- [ ] **8.5** Purity/lint. Verify (PR-level): `make check-all`. Target ≤300.

---

## Review Workload Forecast

| Field | Value |
|---|---|
| Estimated changed lines | ~2,750 impl+docs across 8 PRs |
| 400-line budget risk | **High for PRs 4 and 7** (400 each); Medium for 2, 3, 6 (350) |
| Suggested split | PR 4 only, pre-drawn (design §4). Report before splitting |
| `m3e` threshold | **Measured, not crossed** — 8 PRs and ~2,750 lines against 9 and 3,200 (**J4**) |
| Delivery strategy | `auto-chain` |
| Chain strategy | `stacked-to-main` |

---

## Traceability

| Spec section | Requirements | Tasks |
|---|---|---|
| §0 | R0 | every PR's purity task |
| §1 | R1.1, R1.2 | 1.1–1.4 |
| §2 | R2.1, R2.2 | 3.1–3.4 (port in 2.1–2.5) |
| §3 | R3.1–R3.3 | 4.1–4.6 |
| §4 | R4.1, R4.2 | 5.1–5.4 |
| §5 | R5.1–R5.3 | 6.1–6.4, 7.1–7.4 |
| §6 | R6.1 | 8.1–8.2 |
| §7 | R7.1 | 8.3 |
