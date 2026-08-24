# Archive Report — M3: the mouth (Telegram + prospection)

**Change**: m3-mouth-telegram — the umbrella proposal over four phase changes
**Date Archived**: 2026-08-24
**Status**: Complete — all four phases implemented, verified and archived
**Main Branch HEAD at Archive**: 61cb18e

---

## What M3 was, and what it is now

Before M3, Nooma answered when spoken to. It captured, it recalled, it consolidated at night — and
it never said anything first. Doc 02 described triggers, timers, a digest, quiet hours and four
kinds of check-in, and none of it existed: `triggers` and `timers` were tables M0 created and
nothing used.

**Nooma now speaks first.** A dated event or a reminder arms something at capture. A pass every five
minutes decides what has come due, expires what is past its window, fires what is not, and either
pushes it immediately or holds it for a morning digest gated on how the user is doing. A timer's
text is worded at delivery and says so when it is late. A message arriving from Telegram becomes an
ordinary capture and gets an answer, and an answer coming back closes the check-in it answers.

## The four phases

| Phase | Archived | What it decided |
|---|---|---|
| [`m3a-prospection`](../2026-08-22-m3a-prospection/) | 2026-08-22 | **When** to speak — every gate, pure, before a row was written anywhere |
| [`m3b-trigger-timer`](../2026-08-21-m3b-trigger-timer/) | 2026-08-21 | **What** comes due, and writing it down |
| [`m3c-telegram`](../2026-08-23-m3c-telegram/) | 2026-08-23 | A mouth and an ear — built, and deliberately not started |
| [`m3d-delivery-demo`](../2026-08-24-m3d-delivery-demo/) | 2026-08-24 | Speaking first, and hearing the answer |

**33 pull requests**, #193 through #231. Each phase archived with its own report; this one is about
what the four have to say together.

## The success criteria, discharged

The proposal's §2 asked for six things. All six run:

- A dated event captured today arms a trigger, and the trigger fires at its lead time.
- An overdue trigger expires rather than firing late (I15) — swept, not sampled.
- Nothing is delivered during quiet hours except a timer (I16) — swept over all 24 hours.
- A digest is assembled once a day, gated on energy, with an anti-starvation bound.
- A message from an allowed chat becomes a capture and gets a reply; one from anywhere else becomes
  nothing, audibly.
- `nooma check` and the five-minute pass both run the same decisions.

**Two items on the build plan's own M3 list are deliberately open**, and `docs/05-build-plan.md`
names them rather than leaving them to be discovered: a timer's list and cancel from chat (M4's
surface), and naming *which* relation an inbound confirmation answers (`m3e`, needing a
pending-question store).

---

## What the four phases found, together

Sixty-odd findings were recorded across the phases: F1–F10, G1–G22, H1–H13, J1–J27. Their own
reports carry them. What follows is what only the whole milestone shows.

### Planning artifacts were wrong in ways only implementation could find — repeatedly

Every phase found at least one requirement it could not satisfy as written, and in each case the
conflict was **structural rather than a typo**:

- **`m3b` G6** — the contract `m3b` shipped could not run at L3, because `triggers.unit_id` has a
  foreign key and the in-memory fake enforces none. L2 was green and silent.
- **`m3c` H1** — the spec's message type omitted an id its own R4.1 needed.
- **`m3d` J1** — the umbrella's seven-PR plan could not be built: the port could not express
  delivery at all.
- **`m3d` J21** — a spec quoted an invariant **backwards**.

The pattern: a planning artifact describes what should be true, and only building against it reveals
what it left unsaid. Recording each as a finding rather than silently correcting it is what made the
next phase able to read the trail.

### Two invariants had contradicted each other for two milestones

**`m3d` J22.** I03's reflection sweep forbids any removal-prefixed method on every ports repository
interface. I10 requires that rejecting a relation deletes it. `m2c` widened the sweep to include
`RelationRepo` and nobody noticed, because nothing had ever needed to delete a relation.

It took the first change that did. The owner's ruling narrowed the sweep to what I03 actually claims
— its own name says **units** — and the alternative, renaming the method to slip past the prefix
set, was rejected: a check satisfied by a synonym is a check nobody should trust.

### Sweeps found what samples could not, four times

`m3b`'s G16 (an overdue trigger inside quiet hours defers rather than expires), G22 and `m3d`'s J10
and J20 (three fixtures whose "staleness" only held at some hours of the day) are one finding met
four times.

**A fixture whose meaning depends on the wall clock is fragile even when the assertion looks
absolute.** Where a fixture can inject a clock it must; the ones the shipped binary owns are stuck
with the real one and have to be written two-sided — asserting what happens inside the window *and*
outside it, never one and a skip.

G22 is the sharpest: **G16 was recorded as a finding and still bit twice.** A documented interaction
is not a guarded one.

### Two defects shipped and were caught by the next slice, not by review

**`m3b` G14** — retiring the timer refusal left an undated timer falling through to `classify.ToUnit`
and failing the whole capture. A user typing "remind me to call the dentist" with no time got a 500,
on `main`, until `m3b`'s own next PR swept 78 cells and found it.

**`m3d` J26** — a fired timer was never sent. Every test below L4 passed, because `renderTimer`'s
tests assert what it returns and `CheckReport` counts a firing.

Both are the same shape, and it is the milestone's most reusable line: **a layer that only ever
asserts its own return value cannot notice that nobody called it.** The demo is not a formality.

### Storage facts were pinned by observation, not inference

`m3b` recorded what SQLite actually does with a non-finite REAL: **NaN bound as a parameter is
stored as SQL NULL**, so a NaN interrupt level and a degraded one are indistinguishable once
written, while ±Inf survives verbatim. Non-numeric TEXT stays TEXT. None of that was guessed.

`m3c` recorded the token leak the obvious code has — Telegram puts the bot token in the URL path and
`net/http` puts the URL in `*url.Error`, so `fmt.Errorf("%w", err)` writes it into every transport
error — and closed it with a helper whose doc comment states plainly that it is a denylist.

### R0 was an experiment, and it passed

`m3c` was the first real channel since doc 02 line 653 claimed *"nothing in the decision layer names
a channel."* Verified directly: zero files under `internal/core` changed across all five of its PRs.
The boundary was in the right place, and two parsing scans keep it that way.

---

## Method notes for M4

**Iterate vocabularies, never list them.** Every `AllX()` in this codebase exists so a test can
sweep it; a hand-written switch over today's members compiles unchanged the day a member is added.
This found G14 and G16 directly.

**Prove falsifiability by mutation when the claim is about a guard.** Roughly a dozen mutations were
made, run and reverted across M3. The two shipped defects are both in places where that was not
done.

**Assert orderings as orderings.** I10's signal-before-delete went into one event log compared as a
sequence. Two independent assertions would have passed with the order reversed.

**Name what a test cannot prove, inside the test.** M3 has half a dozen of these: the port contracts
cannot observe "the row is unchanged" after a refused transition; no fixture built from four
statuses can distinguish I02's positive filter from an exclusion list; the L4 scope scan reads the
tree rather than a diff.

**Ask the owner before writing the design, not after.** `m3c` asked its one open question up front
and archived the day it finished. `m3b` discovered its question during implementation and waited for
a ruling with the change already merged.

---

## Final State

`internal/core/prospection` (every gate, pure), `internal/channels` + `internal/channels/telegram`,
`ports.Channel`, `ports.TriggerRepo` (eight methods), `ports.TimerRepo`, `ports.StateRepo` widened,
`ports.UnitRepo.LiveFocusCandidates`, `brain.CheckService`, `brain`'s arming and check-in paths, the
scheduler's second job, and `nooma check`.

**`AllDecisionActions()` = 40. `AllCaptureOutcomes()` = 7. No migration in the entire milestone** —
`triggers` and `timers` were correct as M0 left them, index included.

**Next**: M4, the mirror — the complete UI. And `m3e` if the two open items are wanted, which is the
owner's call rather than a queued task.
