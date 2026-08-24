# Design — M3 Phase D: delivery, check-ins, and the demo

Technical design for `m3d-delivery-demo`, derived from `spec.md` (R0–R7.1) and the umbrella
proposal's §4.2–§4.5 and 2026-08-19 owner rulings, all read in full before this document. The three
archived phases' handoffs are treated as binding.

---

## 1. Ground truth this design was verified against

Read at authoring time:

- **`internal/core/prospection` is complete for M3.** `Route`, `DigestDue`, `LowEnergy`, `Carry`,
  `DelayCaveat`, `TriggerVerdict`, `TimerVerdict`, `AllVerdicts`, plus `DigestHour = 7`,
  `LowEnergyMax = 0.5`, `EnergyReadingMaxAgeHours = 24`, `LowEnergyDigestSize = focus.DefaultSize/2`,
  `MaxDigestDeferrals = 3`. **This change adds no gate**, which is R0.
- **`brain.CheckService`** already reads the clock once and runs the due scan
  (`internal/brain/check.go`), with `CheckRequest{DryRun}` and `CheckReport`.
- **`ports.TriggerRepo` has four methods**: `Create`, `Due`, `Fire`, `Expire`. It can neither write
  `surfaced_at` nor find an unanswered trigger — see **§3.2**, which is this design's largest
  addition and was not named by any artifact.
- **`ports.Channel`** (`m3c`): `Name`, `Receive`, `Confirm`, `Send`, `Close`. `Send` has had no
  caller since it shipped.
- **`ports.UnitRepo.LiveFocusCandidates`** (`m3b` PR 3) has had no caller since it shipped. This is
  it.
- **`internal/scheduler`** runs one cron job over `Consolidator`, with an overlap guard, a boot
  catch-up and a timer seam. `schedules.proactive_check` is a config key with no job.
- **`ports.StateRepo`**: `OpenHypothesis`, `LastHypothesisAt`. No energy read, no resolution write.
- **`classify`** already decodes `nudge_outcome`, `task_checkin_outcome`, `relation_outcome`,
  `state_outcome`, each with its `AllX()`.

---

## 2. What `m3d` decides, in one paragraph

A proactive pass reads the clock once, runs `m3b`'s due scan, and then does what `m3b` deliberately
did not: sends. A fired trigger goes out immediately or waits for the digest, by `Route`. A fired
timer is worded at delivery. Once a day the digest is assembled from what accumulated, gated by
`Carry`. An inbound answer resolves the check-in it answers. `runServe` starts the poller and the
tick, and stops them in an order that never leaves a message sent and unrecorded.

---

## 3. Decisions

### 3.1 Delivery extends the due scan rather than following it

| Option | Verdict |
|---|---|
| **A. A separate `DeliverService`** run after `CheckService` | **Rejected.** Two services means two clock reads or an instant passed between them, and the second is the shape `brain_single_clock_read_test.go` exists to prevent. It also splits one decision across two audit trails: "fired" and "delivered" would be different passes with different instants |
| **B. `CheckService` grows a delivery step** — chosen | The scan already knows which triggers it just fired and with what verdict. Delivery is what to do with that, in the same pass, under the same instant. `CheckReport` grows counts; `CheckRequest` grows nothing — a dry run already suppresses every write, and a send is a write |

**`--dry-run` therefore suppresses sends too**, and that follows from Q1's own ruling ("suppresses
the effect, never branches the logic") without a new decision. A dry run that sent messages while
writing nothing would be the worst of both.

### 3.2 `TriggerRepo` widens by three methods, and no artifact said so

**This is the change's largest unplanned addition, found by reading the port against the spec.**
`m3b` shipped `Create`, `Due`, `Fire`, `Expire`. Spec R2.1 needs to write `surfaced_at`; R3.1 needs
the same for a digest's items; R5.1 needs to find an unanswered check-in and resolve it. None is
expressible today.

```go
// Surface records that id reached the user at at. Separate from Fire
// because firing and delivering are different facts with different
// failure modes — m3b left surfaced_at NULL precisely so this could be
// the thing that fills it.
Surface(ctx context.Context, id string, at time.Time) error

// Delivered returns every trigger that fired and reached the user and
// has no answer yet — doc 02 §5's "open check-ins", read for the digest's
// exclusion and for a check-in's resolution.
Delivered(ctx context.Context) ([]DueTrigger, error)

// Resolve records the user's answer: responded_at and resolution, under
// a surfaced-and-unanswered precondition.
Resolve(ctx context.Context, id string, resolution TriggerResolution, at time.Time) error
```

`TriggerResolution` is a fourth vocabulary in `internal/ports` (`engaged | declined | self_healed`,
migration `0001:54`), with its `AllTriggerResolutions()` and its migration-comment pin — the shape
the other three already have.

**`Resolve` takes a `to` parameter where `Fire` and `Expire` do not**, and the asymmetry is
deliberate: `resolution` is a *value* the user supplied, not a status the transition implies. `m3b`
§3.1 rejected a `to` parameter because every call site had exactly one legal value; here every call
site has three, and they come from `classify`'s own vocabulary.

### 3.3 The digest reads what was fired, not what is due

A digest item is a trigger that **fired and was not pushed**. So the digest's source is `Delivered`'s
complement: fired, `surfaced_at` NULL. That is one more read, and it is deliberately not a new port
method — `Due` returns armed rows and a fourth read would be a status parameter by another name
(`UnitRepo`'s own rule). Instead `Delivered` is paired with:

```go
// Undelivered returns every trigger that fired and has not reached the
// user. Named for what it returns, like every other read on this port.
Undelivered(ctx context.Context) ([]DueTrigger, error)
```

Four new methods, then, and the port doc comment says why each is named for its result rather than
parameterised by status.

### 3.4 Deferral counting has nowhere to live, and that is a real problem

`prospection.MaxDigestDeferrals = 3` requires knowing **how many digests an item has been held
through**. Nothing persists that. Three options:

| Option | Verdict |
|---|---|
| A column on `triggers` | **Rejected.** A migration, for a counter that is meaningless outside a digest |
| A `decision_log` count — count `check.digest.held` rows naming the trigger | **Chosen.** The rows already have to be written (I12: every effect writes one), the count is derivable, and it needs no schema change. `DecisionLog.Since` already reads by time |
| In-memory | **Rejected.** A restart resets every item's patience, which is exactly the starvation the constant exists to bound |

The cost is stated: counting rows is O(rows since the window) per digest. The window is bounded by
`MaxDigestDeferrals` digests, i.e. days, and a personal vault's `decision_log` is small. If it ever
is not, the column becomes a migration with a reason.

### 3.5 What a delivery says

Spec §8 leaves this to the design. Three shapes, one file, pinned by a test:

- **A push**: the trigger's `payload.action_text`, with the delay caveat appended when
  `DelayCaveat` says so.
- **A timer**: its `rendered_text` when the rephrasing succeeded, its `action_text` otherwise, the
  generic nudge when both are absent, plus the same caveat rule.
- **A digest**: one header naming the count, then one line per item. Held items are not mentioned —
  a digest that listed what it withheld would defeat the low-energy gate it came from.

All three live in `internal/brain/render.go` with a table test over their inputs, because "the
wording is obvious" is how a delivery surface ends up with five templates nobody can find.

### 3.6 Check-in resolution matches the most recent open one, and says so when it cannot

An answer carries an outcome, not an id. So resolution is: read `Delivered`, take the most recent,
apply. **Ambiguity is not resolved by guessing** — if there are several open and the answer names
none, the most recent is chosen and the `decision_log` row records that it was a choice among N.
The alternative, asking, is a second conversational turn this change does not build.

An answer with nothing open writes one row and changes nothing (R5.1).

### 3.7 The tick and its guard

`internal/scheduler` gains a second job with its **own** overlap guard. Sharing one guard would let a
nightly consolidation suppress twelve hours of proactive checks, which is the opposite of both jobs'
purpose. The scheduler's existing timer seam and log discipline are unchanged.

### 3.8 Shutdown order

Poller first, then scheduler drain, then server close. The reason is R6.1's own: the poller is what
accepts new work, and stopping it first bounds what the drain has to finish. A pass mid-delivery is
cancelled through its context, and because `Surface` is written only after a successful `Send`, a
cancelled pass leaves a trigger fired-and-undelivered, which the next pass picks up.

---

## 4. The eight PRs

| # | Branch | Content | Impl+docs |
|---|---|---|---|
| 1 | `feat/scheduler-proactive-tick` | The second job, its guard, §13 row 918's split and prose | ~300 |
| 2 | `feat/ports-trigger-delivery` | `Surface`, `Delivered`, `Undelivered`, `Resolve`, `TriggerResolution` + SQLite + contract | ~350 |
| 3 | `feat/brain-push-delivery` | Route, send, `surfaced_at`, quiet-hours deferral (I16) | ~350 |
| 4 | `feat/brain-digest-assembly` | `DigestDue`, `Carry`, `LiveFocusCandidates`, the deferral count (I09) | ~400 |
| 5 | `feat/brain-timer-fire-rephrase` | The LLM call, the fallback, the caveat | ~300 |
| 6 | `feat/brain-checkin-nudge-task` | `nudge_outcome`, `task_checkin_outcome` | ~350 |
| 7 | `feat/brain-checkin-relation-state` | `relation_outcome` (I10), `state_outcome`, `StateRepo` | ~400 |
| 8 | `feat/serve-wiring-and-demo` | `runServe` wiring, shutdown order, the L4 demo | ~300 |

**Eight PRs, ~2,750 budgeted impl+docs lines.** Proposal §5's threshold — *"if the forecast exceeds
nine PRs or 3,200 lines, the check-in pair splits off as `m3e-checkins`"* — is **measured and not
crossed**, and PR 2 is new relative to the umbrella's seven-PR sketch because §3.2 found a port that
cannot express what the spec requires.

**PR 4 is the one at risk** at 400. Its pre-drawn cut: the digest's assembly and send (4a) from the
care gates and deferral counting (4b).

---

## 5. Testing strategy

| Layer | What | PR |
|---|---|---|
| L2 | The tick fires; overlap per job, not shared | 1 |
| L2/L3 | The four new `TriggerRepo` methods, contract + SQLite | 2 |
| L2 | Push vs digest by `Route`; `surfaced_at` only after a successful `Send` | 3 |
| L2 | **I16 swept across the quiet-hours boundary**, trigger deferred and timer not | 3 |
| L2 | One digest per day; `Carry`'s outcome persisted; **I09** anti-starvation at `MaxDigestDeferrals` | 4 |
| L2 | Rephrasing, its failure falling back to verbatim, the generic nudge making zero calls | 5 |
| L2 | Each check-in vocabulary **iterated via its `AllX()`**, never listed | 6, 7 |
| L2 | **I10**: a denied relation is weakened, never deleted | 7 |
| L4 | The simulated day — the exit criterion | 8 |

---

## 6. Threat matrix

| Boundary | Assessment |
|---|---|
| **Outbound message content** | A delivery carries the user's own stored text plus fixed sentences. Nothing a third party supplies reaches a `Send` — an unadmitted message never becomes a capture (`m3c` R3.1) |
| **LLM input** | The rephrasing call sends `action_text`, which is the user's own words. Same surface `classify` already has |
| **Inbound** | Unchanged from `m3c`: allow-list at receipt |
| **Denial of service** | Unchanged and still unmitigated: an allowed chat drives LLM calls. `m3c` §8 names it |
| **Delivery amplification** | **New.** A bug in the digest's day boundary could send one digest per pass — 288 messages a day. Bounded by `DigestDue`'s own last-digest check, and asserted by "two passes on one day produce one digest" |

---

## 7. Owner-review items

Every umbrella question was ruled on 2026-08-19. These are this design's own, decided:

| # | Item | Decided |
|---|---|---|
| D1 | Delivery extends `CheckService` rather than a second service | §3.1 |
| D2 | `TriggerRepo` widens by four methods | §3.2, §3.3 |
| D3 | Deferral count derived from `decision_log`, not a column | §3.4 |
| D4 | An ambiguous check-in answer resolves the most recent and records that it chose | §3.6 |
| D5 | `--dry-run` suppresses sends | §3.1 |
| D6 | Shutdown order: poller, scheduler, server | §3.8 |

---

## 8. Risks

| # | Risk | Posture |
|---|---|---|
| A | The deferral count is O(rows) per digest | Accepted; bounded by days on a personal vault. The column is available with a reason if it ever is not |
| B | A digest that fails to send holds every item another day | Accepted: the items stay undelivered and the next digest carries them, which is the same path a deferral takes |
| C | `Delivered` is unbounded | Accepted, and the same posture `LiveDecayStates` already carries |
| D | PR 4 is budgeted at the ceiling | Cut pre-drawn (§4) |
