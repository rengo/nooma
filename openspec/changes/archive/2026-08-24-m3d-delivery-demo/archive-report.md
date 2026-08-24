# Archive Report — m3d-delivery-demo

**Change**: m3d-delivery-demo (last of M3's four: m3a → m3b → m3c → **m3d**)
**Date Archived**: 2026-08-24
**Status**: Complete — all 8 implementation PRs merged, every task checked
**Main Branch HEAD at Archive**: 61cb18e (merge of PR #231)

---

## Artifacts Archived

`spec.md` (R0–R7.1, **R5.2 corrected 2026-08-24**), `design.md` (§1–§8), `tasks.md` (all tasks
checked, findings J1–J27, the verification pass), and this report.

## What shipped

Nooma speaks first. A trigger comes due and is pushed or held for the morning digest by its
interrupt level; a timer fires and its text is worded at delivery, saying so when it is late; the
digest is assembled once a day under a care gate; and an answer coming back resolves the check-in it
answers.

| # | PR | Branch | Merge |
|---|---|---|---|
| — | [#223](https://github.com/rengo/nooma/pull/223) | `plan/m3d-delivery-artifacts` | `f0974a3` |
| 1 | [#224](https://github.com/rengo/nooma/pull/224) | `feat/scheduler-proactive-tick` | `fac38bb` |
| 2 | [#225](https://github.com/rengo/nooma/pull/225) | `feat/ports-trigger-delivery` | `c4efe25` |
| 3 | [#226](https://github.com/rengo/nooma/pull/226) | `feat/brain-push-delivery` | `d54d28a` |
| 4 | [#227](https://github.com/rengo/nooma/pull/227) | `feat/brain-digest-assembly` | `525dcdf` |
| 5 | [#228](https://github.com/rengo/nooma/pull/228) | `feat/brain-timer-fire-rephrase` | `65dddaf` |
| 6 | [#229](https://github.com/rengo/nooma/pull/229) | `feat/brain-checkin-nudge-task` | `16fec0c` |
| 7 | [#230](https://github.com/rengo/nooma/pull/230) | `feat/brain-checkin-relation-state` | `cb7d989` |
| 8 | [#231](https://github.com/rengo/nooma/pull/231) | `feat/serve-wiring-and-demo` | `61cb18e` |

Eight PRs where the umbrella sketched seven — PR 2 is finding **J1**'s own. `m3e`'s threshold (nine
PRs or 3,200 lines) was measured at eight and ~2,750 and **not crossed**.

## Verification Gate: PASS

`make check-all` green. `internal/core` untouched by this change: `m3a` shipped every gate it runs.

Four claims verified by **mutation** rather than assertion — the shared scheduler guard, the
held-digest rows, the signal/delete ordering, and `deliver`'s success reporting.

---

## The findings that matter

### Three needed a decision that was not mine

**J1 — the umbrella's plan could not be built.** `ports.TriggerRepo` could not express delivery at
all: writing `surfaced_at`, finding fired-but-undelivered triggers, and resolving a check-in were
none of `Create`/`Due`/`Fire`/`Expire`. Found by reading the port against the spec rather than by
implementing into it. Four methods and a fourth vocabulary got their own PR.

**J5 — two config keys control nothing, and have since M0.** Nothing in this repository parses a
cron expression. `schedules.consolidate` and `schedules.proactive_check` sit in a real `nooma.yml`,
look like they control scheduling, and are read by nobody. `ProactiveCheckInterval` is a constant
beside `ConsolidationHour`, which is the existing shape — **the pre-existing gap is the finding**,
and fixing it is either a parser or a deprecation, each its own work unit.

**J22 — I03's sweep and I10 had contradicted each other for two milestones.** The sweep forbids any
removal-prefixed method on every ports repository interface; `m2c` widened it to include
`RelationRepo`; I10 requires exactly such a method. Nothing had ever needed to delete a relation, so
nobody noticed. **Owner ruling: `RelationRepo` leaves the sweep**, with the reason recorded at the
sweep. Renaming the method to slip past the prefix set was rejected — a check satisfied by a synonym
is a check nobody should trust.

### Two defects this change introduced and fixed itself

**J9 — a failed send counted as a delivery.** `deliver` returned `error`, and the caller counted a
delivery whenever it returned nil — which it does after *recording* a failure. Every other test
passed through the bug: the happy path sends and counts one, the digest path never reaches
`deliver`. **The count was wrong only in the case the count exists for.**

**J26 — a fired timer was never sent.** The timer path recorded its `decision_log` row, wrote
`rendered_text`, and had no `Send` at all. `renderTimer`'s tests assert what it *returns*;
`CheckReport` counts a *firing*. **No test below the demo asked whether the user received anything.**

Both are the same shape, and it is the report's most useful line: **a layer that only ever asserts
its own return value cannot notice that nobody called it.**

### One error in this change's own spec

**J21 — R5.2 had I10 backwards**, reading *"a denied relation is weakened, never deleted"* where
`docs/06-harness.md:250` and doc 02 §331 both say rejecting **deletes** it. The correction is in
`spec.md` with what it originally said. The lesson is small and repeatable: **an invariant quoted
from memory into a spec is a claim, and the source is one `rg` away.**

### Behaviour and gaps no artifact had stated

| # | Finding |
|---|---|
| **J3** | `MaxDigestDeferrals` had no counter anywhere. Derived from `decision_log`, which makes the audit trail load-bearing rather than decorative — verified by suppressing the rows |
| **J11** | `m3a` left "is an empty digest sent?" explicitly to `m3d`. **Not sent**: a message every morning saying nothing happened is one people learn to ignore, and then the one that matters arrives in that shape |
| **J12** | `LatestEnergy` must read the most recent row that HAS energy, not the most recent row — `m2`'s load watcher writes NULL energy by design, so the naive query reports "no reading" exactly when the care gate matters |
| **J16** | An empty provider answer is a failed rephrasing. R4.1's "failure" reads as a returned error; whitespace is neither an error nor a wording |
| **J17** | The delay caveat lives in two places deliberately — the prompt asks for it, the fallback appends it. Appending to a rephrasing already asked would say it twice |
| **J18** | `snooze` resolves nothing. R5.2's "resolves" reads as total over the vocabulary and is not: snooze is neither engaged nor declined, and forcing it records an answer the user did not give |
| **J19** | A channel-less digest was marking items delivered — worse than the push path's equivalent, because it surfaces every carried item at once. Found by `nooma check`, whose nil channel is by design |
| **J25** | Design §3.8's shutdown order was written without reading `serve.go`'s. The poller-first half shipped; the server/scheduler pair is JD-7-01's and was left alone |

### The one that keeps recurring

**J10, J20 — and G22 before them.** Three fixtures over two changes seeded a "stale" trigger at an
offset that is only stale at *some* hours: `DeliverableFrom` shifts a `fire_at` inside quiet hours to
that day's 07:00. J10's sweep caught its own fixture through the coverage guard it carried
("a sweep that only reaches one regime is a sample"); J20's two were caught by CI at 01:45 UTC.

**A fixture whose meaning depends on the wall clock is fragile even when the assertion looks
absolute.** Where a fixture can inject a clock it must; only the ones the shipped binary owns are
stuck with the real one, and those are the ones to write two-sided.

---

## Method notes

**Sweeps over `AllX()` beat tables**, again: every vocabulary mapping in this change iterates its
own, so a member added later fails a loop pass instead of being silently unhandled.

**Assert orderings as orderings.** I10's signal-before-delete went into one event log compared as a
sequence, not two independent facts. Verified by inverting it.

**The demo is not a formality.** It found J26, which nothing below it could have.

**Prove falsifiability by mutation when the claim is about a guard** — done four times here, and the
two defects above are what happens where it was not done.

---

## Final State

`AllDecisionActions()` = **40**. `internal/scheduler` runs two jobs with separate guards.
`ports.TriggerRepo` has eight methods; `ports.TimerRepo`'s `Fire` carries the delivered wording;
`ports.StateRepo` reads energy; `ports.RelationRepo` can delete, by I10 and outside I03's sweep.
`serve` starts the channel and the tick and joins the poller first.

**Exit criterion discharged**: `test/e2e/m3_demo_test.go` — one vault, one simulated day, a pushed
trigger, a held one arriving in the morning digest, a timer worded at delivery that says it was late,
and nothing delivered during quiet hours except the timer.

**Deliberately open, and named in `docs/05-build-plan.md` rather than left to be noticed**: a timer's
list and cancel from chat (M4's surface — doc 02 §8 promises both and `ports.TimerRepo` declares
neither, because a method with no caller is what this repository refuses to ship), and naming *which*
relation an inbound confirmation answers (`m3e`, needing a pending-question store — J24).
