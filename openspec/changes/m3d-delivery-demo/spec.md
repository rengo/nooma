# Spec — M3 Phase D: delivery, check-ins, and the demo

Delta spec for `m3d-delivery-demo`, the last of M3's four chained changes (`m3a` → `m3b` → `m3c` →
**`m3d`**). Derived from `openspec/changes/m3-mouth-telegram/proposal.md` §3.2, §4.2–§4.5, §5 and
its 2026-08-19 owner rulings, and from the three archived phases' own handoffs.

`m3a` decided **when** to speak. `m3b` decided **what** comes due and wrote it down. `m3c` gave the
binary a mouth and an ear but never started it. **`m3d` makes it speak first** — and then listens to
the answer.

Every owner question the umbrella raised was ruled on 2026-08-19. **This change opens no new
question that a design can answer for itself**; §8 records the one it must decide and why it is a
design decision rather than an owner's.

---

## Scope boundary (binding)

**In**: `internal/brain/` (delivery, digest assembly, fire-time rephrasing, the four check-in
resolutions), `internal/scheduler/` (the `proactive_check` tick), `internal/ports/` (the
`StateRepo` widening R5.3 names), `cmd/nooma/` (wiring), `docs/02-cognitive-core.md`, `docs/05-build-plan.md`.

**Out**: `internal/channels/**` — `m3c` shipped the channel and this change only *starts* it.
`internal/store/sqlite/**` beyond `StateRepo`'s widening. No new table.

### R0 — `internal/core` changes only where a behavioural number lands

**MUST**: `internal/core/**` is modified only to add named constants that `docs/02-cognitive-core.md`
§13 will name, and their tests. No new decision function: `m3a` shipped every gate this change runs
(`Route`, `DigestDue`, `LowEnergy`, `Carry`, `DelayCaveat`, `TriggerVerdict`, `TimerVerdict`).

**Rationale**: `m3a`'s whole purpose was to decide before delivering. If `m3d` needs a new gate,
either `m3a` missed one — which is a finding, not a licence — or the delivery layer is deciding
something it should be reading. Adding a *constant* is different: a number the brain compares
against belongs in core and in §13 by the calibration rule, and `m3d` introduces at least one (the
digest hour is already `prospection.DigestHour`; the anti-starvation unit is already
`MaxDigestDeferrals`).

**Verified by**: the existing `depguard` `core-purity` rule, plus each PR's own diff.

---

## 1. The proactive tick — PR `feat/scheduler-proactive-tick`

### R1.1 — the scheduler runs a `proactive_check` job on the configured cadence

**MUST**: `internal/scheduler` gains a second cron job, driven by `schedules.proactive_check`
(`internal/config`'s existing key, `*/5 * * * *` by default — `docs/01-architecture.md:227`), which
calls one injected `ProactiveChecker` exactly as the consolidation job calls its `Consolidator`.

**MUST**: the two jobs share the scheduler's existing overlap guard shape but **not one slot** — a
five-minute check must not be skipped because a nightly consolidation is running, and a
consolidation must not be skipped because a check is. Each job has its own guard.

**MUST**: a `proactive_check` pass that fails is logged and the next tick still fires. A pass that
panics does not take the process down. Both are the consolidation job's own posture (`m2d`).

**Verified by**: L2 with the scheduler's existing fake timer — a tick fires the checker; a second
tick during a running pass is skipped and logged; a consolidation running does not skip a check.

### R1.2 — §13's row 918 splits, and its prose is corrected rather than left implying coverage

**MUST**: `docs/02-cognitive-core.md` §13's row 918 splits so the `proactive_check` half names its
own constant and can be checked by `calibration_doc_test.go`.

**MUST**: the consolidation half's prose is corrected to state **why it is not checked** — the gate's
regex matches `internal/core/…` only and never reaches `internal/scheduler.ConsolidationHour`
(proposal §4.3). Splitting the row does not make it checkable, and the row currently implies it does.

**MUST NOT**: `ConsolidationHour` moves into `internal/core` — owner ruling 5 records that as a
separate work unit outside M3.

**Verified by**: L2 — `calibration_doc_test.go` checks the new row; a second test asserts the
corrected prose names the blind spot rather than a fix.

---

## 2. Push delivery — PR `feat/brain-push-delivery`

### R2.1 — a fired trigger routed to push is delivered immediately, through the channel

**MUST**: for each trigger the due scan fires, `brain` resolves its `interrupt_level` through
`prospection.ResolveInterrupt`, asks `Interrupt.Route()`, and for `RoutePush` sends the trigger's
text through `ports.Channel.Send` **in the same pass**.

**MUST**: `surfaced_at` is written **only after** the send succeeds. A trigger the user never saw
must not be recorded as delivered — `m3b` left `surfaced_at` NULL for exactly this, and this
requirement is what closes it.

**MUST**: a send failure is recorded and the trigger stays `fired` with `surfaced_at` NULL, so a
later pass can deliver it. It is not retried inside the pass.

**Verified by**: L2 with `fakechannel` — a push-routed trigger produces one `Send` and one
`surfaced_at`; a failing `Send` produces no `surfaced_at` and one `decision_log` row.

### R2.2 — quiet hours defer a push, and the deferral is recomputed rather than stored (I16)

**MUST**: a push whose instant falls in quiet hours is **not** sent, writes no `surfaced_at`, and is
re-evaluated on the next pass — `m3a`'s `TriggerVerdict` already returns `VerdictDefer` for it and
`m3b`'s scan already writes nothing.

**MUST**: the timer is the one exception. `prospection.TimerVerdict` passes `deferInQuietHours =
false`, so a due timer fires and is delivered inside quiet hours. **This is I16's own exception and
the only one.**

**MUST**: nothing persists a "deferred" state. The deferral is arithmetic over `fire_at` and `now`,
recomputed every pass (`m3a` §3.1), so an item resurfaces by the clock moving and not by a row.

**Verified by**: L2 — a trigger swept across the quiet-hours boundary is deferred inside it and
delivered on the first pass outside; a timer at the same instants is delivered throughout.

---

## 3. The digest — PR `feat/brain-digest-assembly`

### R3.1 — the digest is assembled once daily, from what the day accumulated

**MUST**: at the first pass at or after `prospection.DigestHour` on a day with no digest yet
(`prospection.DigestDue`), `brain` assembles one digest from every trigger that is `fired` with
`surfaced_at` NULL and was routed to digest, sends it as one message, and marks every included
trigger `surfaced_at`.

**MUST**: the digest is one `Send`, not one per item. A digest delivered as N messages is N pushes
wearing a different name.

**MUST**: the day boundary is `now`'s own location — `DigestDue` already builds it with `time.Date`
in that zone, and this requirement is that nothing above it re-derives the boundary differently.

**Verified by**: L2 — two passes on one day produce one digest; a pass the next day produces
another; a pass before `DigestHour` produces none.

### R3.2 — the care gates decide what the digest carries, and `focus.Priority` orders it

**MUST**: `prospection.Carry` decides what is delivered and what is held, given the low-energy
reading, the adjacency map and the instant. `brain` supplies its inputs and persists its outcome; it
re-derives none of the rule.

**MUST**: the importance ordering is `focus.Priority` over the `focus.Candidate` each item carries —
owner ruling 4. `ports.UnitRepo.LiveFocusCandidates` (shipped by `m3b`, with no caller until now) is
where the candidates come from.

**MUST**: a `pattern_based` trigger has no source unit, so its `DigestItem.Candidate` is nil, and
`Carry` already substitutes the zero `focus.Candidate` for it. Nothing above may pass a
`triggers.unit_id` of NULL to `LiveFocusCandidates` — `m3b`'s port doc comment states this as the
caller's obligation and this is the caller.

**Verified by**: L2 — a low-energy day carries `LowEnergyDigestSize` items and holds the rest; a
held item's deferral count rises; an item held `MaxDigestDeferrals` times is delivered regardless.

### R3.3 — the energy reading comes from `current_state`, and its absence is not a low reading

**MUST**: `ports.StateRepo` gains a read for the most recent `current_state` energy reading with its
`recorded_at`, and `brain` passes it to `prospection.LowEnergy` as an `*EnergyReading`.

**MUST**: **nil is not low.** `LowEnergy` already treats a missing or stale reading as not-low
because the gate suppresses delivery, and suppressing on absence would make a vault with no
check-ins silently stop speaking. This requirement is that the read returns nil rather than a zero
value for "no reading".

**Verified by**: L2/L3 — a vault with no `current_state` row digests normally; a reading older than
`EnergyReadingMaxAgeHours` is ignored; a fresh low reading engages the gate.

---

## 4. Fire-time rephrasing — PR `feat/brain-timer-fire-rephrase`

### R4.1 — a timer's text is worded at delivery, and the stored request is never overwritten

**MUST**: when a timer fires, `brain` calls the LLM to rephrase `action_text` into `rendered_text`,
and delivers the rephrasing. `action_text` is not modified — doc 02 §8's "the request is stored
verbatim and only worded at delivery time", which `m3b` implemented by leaving `rendered_text` NULL.

**MUST**: a rephrasing failure delivers `action_text` verbatim rather than failing the delivery. The
user asked for a reminder; a model outage is not a reason to withhold it.

**MUST**: a timer with NULL `action_text` — the generic nudge — is delivered as a generic nudge with
no LLM call at all. There is nothing to rephrase.

**Verified by**: L2 with `fakeprovider` — a rephrased delivery; a provider failure delivering the
verbatim text and recording the degradation; a generic nudge making zero provider calls.

### R4.2 — a late delivery says so, at and above `DelayCaveatMinutes`

**MUST**: when the overdue duration satisfies `prospection.DelayCaveat`, the delivered text mentions
the delay. Below it, the lateness is the scheduler's own granularity and is not mentioned.

**MUST**: `DelayCaveat` decides; the render picks the words. Doc 02 §7's own division of labour, and
nothing in `brain` re-derives the threshold.

**Verified by**: L2 — a sweep across the boundary, asserting the caveat appears at and above it and
not below.

---

## 5. Check-ins — PRs `feat/brain-checkin-nudge-task` and `feat/brain-checkin-relation-state`

### R5.1 — a delivered nudge that is answered resolves its trigger

**MUST**: an inbound capture classified with a `nudge_outcome` resolves the most recent
fired-and-surfaced-but-unanswered trigger: `responded_at` is written and `resolution` is set from
the outcome (`engaged | declined | self_healed`).

**MUST**: an answer with no open check-in to resolve is recorded and changes nothing. A user saying
"done" out of the blue is not an error.

**Verified by**: L2 — the four `classify.AllNudgeOutcomes()` members, each resolving; an answer with
nothing open writing one row and no transition.

### R5.2 — `task_checkin_outcome`, `relation_outcome` and `state_outcome` each resolve their own thing

**MUST**: a `task_checkin_outcome` resolves an open task check-in. A `relation_outcome` confirms or
denies a derived relation (**I10**: a denied relation is weakened, never deleted). A
`state_outcome` confirms or denies the load hypothesis `m2`'s `pattern_eval` opened, writing the
answer into `current_state`.

**MUST**: each is exhaustive over its own `classify.AllXOutcomes()` vocabulary, iterated rather than
listed, so a member added later fails a test.

**Verified by**: L2 per vocabulary; L3 for the `current_state` write.

### R5.3 — `StateRepo` widens for the answer, and nothing is deleted (I10)

**MUST**: `ports.StateRepo` gains what R3.3's read and R5.2's write need, and no method whose name
begins `Delete`, `Remove`, `Purge`, `Drop` or `Destroy`.

**Verified by**: the I03 sweep, which already covers `ports.StateRepo`.

---

## 6. Wiring — PR `feat/serve-channel-scheduler-wiring`

### R6.1 — `runServe` starts the channel and the proactive tick, and stops them in a defined order

**MUST**: `runServe` constructs the channel through `m3c`'s `wireChannel`, starts its runner, and
starts the scheduler's `proactive_check` job.

**MUST**: shutdown stops accepting new work before it stops in-flight work: the channel's poller
stops, then the scheduler drains, then the server closes. A pass that is mid-delivery finishes or is
cancelled — it is never left having sent a message without recording it.

**MUST**: a vault with Telegram disabled starts and runs exactly as it does today. The channel is
optional; the tick is not.

**Verified by**: L4 — `serve` with Telegram disabled behaves unchanged; `serve` with a fake Telegram
delivers.

---

## 7. The demo — PR `feat/m3-demo`

### R7.1 — the build plan's own demo bullet, executable

**MUST**: `test/e2e/` runs the demo `docs/05-build-plan.md` names for M3, against a fake Telegram
server and a fake provider: a capture arms a trigger, a pass fires and delivers it, a timer fires
and is rephrased, a digest is assembled at the digest hour, and a check-in answer resolves what was
delivered — with `decision_log` telling the whole story.

**MUST**: no test in this change touches the network or a real LLM. `m3c`'s host-literal scan
already covers Telegram; the provider corpus covers the rest.

---

## 8. The one decision this spec leaves to the design

**What a push and a digest message actually say.** Every other open question was ruled on
2026-08-19. This one is a design decision rather than an owner's because it has no branch a reviewer
could disagree with on principle: the text is rendered from data the brain already has, and the
requirement that binds it is R2.1's and R3.1's — one `Send` per delivery, carrying what was decided.

The design must state the shape and pin it with a test, because "the wording is obvious" is how a
delivery surface ends up with five sentence templates nobody can find.

---

## Exit criterion (this change's own success condition, and M3's)

A vault, a fake Telegram server, and one simulated day: a message arrives and arms a trigger; the
trigger comes due and is **pushed** because its interrupt level is high; a second, quieter one is
**held for the digest** and delivered at the digest hour in one message; a timer fires and its text
is **worded at delivery**, mentioning the delay because it was late; the user answers one of them and
the check-in **resolves**. `decision_log` tells all of it, and nothing was delivered during quiet
hours except the timer.
