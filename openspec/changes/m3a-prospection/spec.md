# Spec — M3a: prospection (pure)

Delta specification for `m3a-prospection`, the first of four chained changes splitting
`openspec/changes/m3-mouth-telegram/proposal.md` (the umbrella; m3a has no proposal.md of its
own, per §5's chain). States what MUST be true of the repository after this change is applied,
in testable form. It does not prescribe how (that is `design.md`, run in parallel per the
umbrella §10).

Sources: umbrella §3.2 item 1, §4.1, §4.2, §4.3, §4.4, §4.5, §5's `m3a` paragraph, §5.1's six
`m3a` rows, §8's Owner rulings table (2026-08-19); `docs/02-cognitive-core.md` §5, §5.1, §7, §13;
`docs/06-harness.md` §4 (I15, I16, I17, I18); ADR-0009.

## Scope boundary (binding)

> `m3a` is `internal/core/prospection` in full: both staleness gates, quiet hours, the
> delivery split, the digest gates, recurrence, lead time, arming — **plus the one
> `internal/core/classify` widening owner ruling 1 requires** (PR 4), because a field the
> model produces has to be decoded somewhere and `classify` is the package that decodes.
> All pure — the instant arrives as a parameter. Zero ports, zero I/O, zero `time.Now()`. No
> adapter, no store, no scheduler code in this slice. Depends on nothing.

Seven PRs, per `design.md` §7, which owns the slicing: `feat/core-prospection-quiet-hours` (PR 1),
`-staleness` (PR 2), `-delivery-split` (PR 3), `feat/classify-prospection-fields` (PR 4),
`-digest-gates` (PR 5), `-recurrence` (PR 6), `-arming` (PR 7). The umbrella §5.1 anticipated six;
design split the classify widening into a slice of its own, and the PR label on each section below
follows design's numbering. The package already exists as a `doc.go` stub
(`internal/core/prospection/doc.go:1`) and is already listed in `docs/06-harness.md` §1's tree — no
preflight PR.

`internal/core/prospection` may import `internal/core/classify` (for `classify.Kind` and the six
orthogonal fields, as arming's input shape) and `internal/core/focus` (for the digest's importance
predicate) — both are `internal/core` siblings, not ports. It imports no `internal/ports`,
`internal/store`, `internal/brain`, or `internal/scheduler` symbol.

**Not this change**: `TriggerRepo`/`TimerRepo`, the due-scan runner, `decision_log` writes,
quiet-hours *deferral* and *resurfacing* as effects, the digest *query* (`focus.Priority`'s
production caller), the Telegram adapter, the scheduler tick, and `nooma check`/`serve` wiring —
all `m3b`/`m3c`/`m3d`. This document proves every requirement against a repo-constructed input,
with no database, no network, and no real clock.

### R0 — General purity, across all seven PRs

**MUST**: no file in `internal/core/prospection` calls `time.Now`, `time.Since`, `time.Until`,
`rand.*`, `uuid.*`, or `os.Getenv`. Every function taking an instant takes it as a `time.Time`
parameter named `now`.

**MUST**: every calibrated number this change introduces is a named constant in exactly one
place in `internal/core/prospection`, and lands a `docs/02-cognitive-core.md` §13 row in the PR
that introduces it (per the umbrella §4.3's five named rows).

**Verified by**: L2, the existing `core-purity` `depguard`/`forbidigo` gate; L1 for every
individual requirement below.

### A note on what this spec does NOT invent

Per the umbrella §4.2, five of the seven doc-02-silent behaviours are `m3a`'s: the delay-caveat
threshold, the digest cadence hour, the digest's low-energy/importance thresholds,
anti-starvation's deferral count, and 29 Feb/day-31 recurrence resolution. Unlike `m2a` (which
invented formulas in the spec itself and paid a four-point reconciliation cost against a
concurrent design), **this document states the observable property each gate MUST satisfy and
names the constant sdd-design owes, without asserting the constant's value.** Where doc 02 or the
umbrella already gives a number (both staleness hours, quiet hours' two bounds, the push
threshold, event lead days), this document states it directly — those five are not open.

---

## 1. Staleness gates (I15, pure half) — PR 2 `feat/core-prospection-staleness`

Traced to ADR-0009 ("a pure function over `(fire_at, now, kind, threshold)`… tested entirely
without a scheduler, without a real clock, and without a database") and `docs/06-harness.md`
I15's row.

### R1.1 — `TriggerVerdict` decides deliver vs. stale, never both, and never touches an armed trigger not yet due

**MUST**: `internal/core/prospection` exposes a pure function computing, from `fireAt, now
time.Time`, a single verdict for one armed trigger. A not-yet-due trigger (`now.Before(fireAt)`)
produces **no delivery decision** — it stays `armed`. For a due trigger, `overdue` is measured from
**the first instant at which that trigger could actually have been delivered** (R2.1's own
function), and from real time thereafter — not from `fireAt` itself. Quiet hours are the system
refusing to deliver; time spent inside that refusal is not time the item was late.
`overdue <= TriggerStalenessHours * time.Hour` → **Deliver**;
`overdue > TriggerStalenessHours * time.Hour` → **Stale**, never `Fired`.

**MUST**: the quiet-hours gate is evaluated **before** the staleness gate, so an item is never
declared stale during a window in which its delivery was refused.

**MUST**: no trigger whose `fireAt` falls anywhere inside `[QuietHoursStartHour,
QuietHoursEndHour)` is **Stale** at `QuietHoursEndHour` the same morning. This is the property the
measurement rule above exists for, and it is why `overdue = now.Sub(fireAt)` is not merely a
simplification but wrong: the quiet window is seven hours (R2.1) and the staleness threshold is
six, so measured from `fireAt` every trigger armed between 00:00 and 01:00 would expire before the
user woke, every night. Stated as a property rather than as three examples, it survives a
recalibration of either number.

**MUST**: `TriggerStalenessHours == 6`, a named constant — doc 02 §13's existing "6" row gains
this Go home (umbrella §4.3, PR 2).

**MUST**: the boundary is inclusive on the deliver side — overdue by *exactly*
`TriggerStalenessHours` delivers; a nanosecond more is stale.

**Scenario: an event trigger overdue by more than the threshold goes stale, never fires**
- GIVEN `fireAt` 7 hours before `now`, both outside quiet hours, `TriggerStalenessHours = 6`
- WHEN the staleness gate evaluates it
- THEN it returns **Stale**, and no verdict this gate can produce is ever `Fired` for this input

**Scenario: a trigger armed inside quiet hours is deliverable when they end, not expired**
- GIVEN `fireAt` at 00:00 local and `now` at 07:00 local the same day, `QuietHoursEndHour = 7`,
  `TriggerStalenessHours = 6`
- WHEN the staleness gate evaluates it
- THEN it returns **Deliver** with an `overdue` of zero — seven hours of wall clock elapsed and
  none of it counted, because none of it was deliverable

**Verified by**: L1 — not-yet-due → no delivery decision; inside quiet hours → deferred, never
stale; `overdue == threshold` → Deliver (boundary, inclusive); `overdue == threshold + 1ns` →
Stale; and the property above swept across the whole `[QuietHoursStartHour, QuietHoursEndHour)`
window rather than sampled.

### R1.2 — `TimerVerdict` mirrors R1.1 with the timer's own threshold, and without the quiet-hours gate

**MUST**: the same shape and the same verdict vocabulary as R1.1, with `TimerStalenessHours == 3`.

**MUST**: a timer's `overdue` is measured from `fireAt` directly. A timer is the one exception to
quiet hours — its instant was set by the user, in the user's own words, at capture — so a timer is
never deferred, never accrues policy delay, and therefore needs no "first deliverable instant"
shift.

**MUST**: the verdict is one vocabulary shared with R1.1, and none of its members is a status the
schema knows. Core reports *stale*; `brain` names the transition — `expired` on a trigger (I15),
`cancelled` on a timer (doc 02 §8's own `pending|fired|cancelled`). Two nearly identical verdict
types for one decision, or a schema vocabulary inside a pure function, are both refused here.

**Scenario: a timer overdue by less than the threshold is still deliverable**
- GIVEN `fireAt` 2 hours before `now`, `TimerStalenessHours = 3`
- WHEN the gate evaluates it
- THEN it returns **Deliver**

**Scenario: a timer due inside quiet hours is delivered anyway**
- GIVEN `fireAt` and `now` both at 03:00 local, inside `[QuietHoursStartHour, QuietHoursEndHour)`
- WHEN the gate evaluates it
- THEN it returns **Deliver** — the exception is the timer itself, not a level, not a threshold

**Verified by**: L1 — the same boundary table as R1.1, with the timer's threshold; plus the
inside-quiet-hours delivery above, which is the direct proof that the exception exists and is the
only one.

### R1.3 — a delay-caveat predicate exists for a delivered-but-overdue timer

**MUST**: `internal/core/prospection` exposes a pure predicate deciding, given a **Deliver**
timer's `overdue` duration, whether the fire-time rendering (`m3d`'s job) must mention the delay
explicitly (ADR-0009: *"a timer three minutes late that announces… is noise of a different
kind"*). The predicate is gated by a named threshold distinct from `TimerStalenessHours` itself.

**sdd-design owes**: the threshold's name, value, and its `docs/02-cognitive-core.md` §13 row
(umbrella §4.2's seventh row — *"At what overdue amount the caveat appears"*). This spec states
only the property: below the threshold, no caveat is required; at or above it, one is.

**Scenario: a timer delivered a few seconds late requires no caveat**
- GIVEN a **Deliver** decision with `overdue` far below the (design-owed) caveat threshold
- WHEN the predicate is evaluated
- THEN it returns `false`

**Verified by**: L1 — a boundary table once design names the constant; this spec's own test
obligation is limited to the below-threshold/at-threshold shape, not the number.

---

## 2. Quiet hours (I16, pure half) — PR 1 `feat/core-prospection-quiet-hours`

Traced to doc 02 §7 (`[00:00, 07:00)` local) and §4.4 ("the zone travels inside the instant").

### R2.1 — `InQuietHours` reads only the instant's own `Location`, never a configured zone

**MUST**: `internal/core/prospection` exposes `InQuietHours(now time.Time) bool`, true for
`now`'s local (`now.Location()`) clock hour in `[QuietHoursStartHour, QuietHoursEndHour)` and
false otherwise. It takes no second parameter for a timezone, and no file in the package reads
`config` or the environment for one — per §4.4, "no config key, no ADR, no open question".

**MUST**: `QuietHoursStartHour == 0` and `QuietHoursEndHour == 7`, two named constants — the
split doc 02 §13's `Quiet hours` row into (umbrella §4.3), because a Default cell starting with
`[` fails `calibration_doc_test.go`'s anchored numeric parse.

**MUST**: the interval is half-open — the start bound is inclusive, the end bound is exclusive.
`06:59:59` in the instant's zone is inside quiet hours; `07:00:00` is not.

**MUST**: alongside `InQuietHours`, the same package exposes a pure function returning, for a given
instant, the **first instant at or after it at which delivery is permitted** — that instant itself
when it falls outside quiet hours, and that day's `QuietHoursEndHour` in the instant's own
`Location` otherwise. It ships here, with quiet hours, rather than with the staleness gate, because
it is quiet hours' own arithmetic; R1.1's `overdue` is defined in terms of it, which is what makes
"no trigger armed inside quiet hours is ever stale at the end hour" a property of composition
rather than a coincidence of two constants.

**Scenario: the same wall-clock instant is judged differently in two zones**
- GIVEN two `time.Time` values denoting the same instant, one carrying a `Location` where the
  local clock reads `06:30` and one where it reads `08:30`
- WHEN `InQuietHours` evaluates each
- THEN the first returns `true` and the second returns `false` — the zone travels with the
  instant, never with a global clock

**Verified by**: L1 — `00:00:00` → true; `06:59:59` → true; `07:00:00` → false; `23:59:59` →
false; a fixed non-UTC `Location` test double proving no environment read; and for the
first-deliverable-instant function, an instant outside quiet hours returned unchanged, one inside
shifted to that day's `QuietHoursEndHour` in the same `Location`, and one exactly at
`QuietHoursEndHour` returned unchanged (the end bound is exclusive on both functions or on
neither).

---

## 3. Delivery split — push vs. digest (I16, delivery gate) — PR 3 `feat/core-prospection-delivery-split`

Traced to doc 02 §7 (`interrupt_level >= 0.7` → push; below → digest; mutually exclusive) and
owner ruling 1 (Q1, Option A: classify emits `interrupt_level`, degrading to a single named
constant; a degraded classification never produces a push).

### R3.1 — `ResolveInterrupt` fills an absent or unparseable reading with a named default strictly below the push threshold

**MUST**: `internal/core/prospection` exposes `ResolveInterrupt(level *float64) Interrupt`,
mirroring `consolidation.ResolveWeightThreshold`'s posture toward an unusable input: `nil` resolves
to `DefaultInterruptLevel`. A non-finite (`NaN`, `±Inf`) or finite-but-outside-`[0,1]` value is
treated identically to `nil` — a value core cannot interpret is never trusted as-is, and is never
clamped into range, because clamping `1.7` to `1.0` would manufacture a push out of a corrupt
number. Any other value passes through unchanged.

**MUST**: the returned `Interrupt` records **that** the resolution degraded, separately from the
level it resolved to, and `ResolveInterrupt` is the only way to obtain one: its fields are
unexported, so no caller outside the package can construct a non-degraded `Interrupt` carrying an
out-of-range level, and its zero value reports itself as degraded rather than as a claimed `0.0`.

**MUST**: `DefaultInterruptLevel < PushThreshold` (R3.2), unconditionally — this is what makes
ruling 1's "a degraded classification never produces a push" a provable property rather than a
convention. The `degraded` flag is the second, independent guard on the same promise: the
inequality alone would fail the day `PushThreshold` were recalibrated below the default, and the
flag would not.

**sdd-design owes**: `DefaultInterruptLevel`'s exact value and its `docs/02-cognitive-core.md`
§13 row. Unlike `classify.PriorWeight`/`PriorDecayRate`, no schema `DEFAULT` exists to pin it
against — `triggers.interrupt_level` (`0001_core_tables.sql:48`) carries no `DEFAULT` clause — so
design chooses the number rather than reading it off the migration.

**MUST**: `internal/core/classify` gains an `InterruptLevel *float64` field, with the prompt,
decoder and golden corpus widened to match — owner ruling 1's own instruction, and `design.md`
§7 gives it a PR of its own (PR 4, `feat/classify-prospection-fields`). An earlier revision of
this document declared that package outside `m3a`'s boundary; that was written before the
design placed the work, and it left ruling 1 decided with nowhere to execute. The resolution
principle: `prospection` **consumes** the field and `classify` **produces** it, so a slice that
owns the consumer and disowns the producer ships a gate whose input never arrives.

**Verified by**: L1 — `nil` → `DefaultInterruptLevel`, degraded; `NaN`, `+Inf`, `-Inf`, `-0.1`,
`1.1` → the same degraded default, each asserted individually; an in-range value passes through
non-degraded; the zero `Interrupt` reports itself degraded; the constant inequality against
`PushThreshold` computed from both named constants, never repeated literals.

### R3.2 — the resolved interrupt's own `Route` splits push vs. digest at the threshold, mutually exclusively

**MUST**: R3.1's `Interrupt` exposes `Route() Route`, a two-member closed vocabulary
(`RoutePush` | `RouteDigest`), computing `level >= PushThreshold` → `RoutePush`; otherwise →
`RouteDigest`. `PushThreshold == 0.70`, a named constant — doc 02 §13's existing "0.70" row gains
this Go home (umbrella §4.3, PR 3).

**MUST**: a degraded `Interrupt` routes to `RouteDigest` **regardless of the level it carries**,
short-circuiting the comparison entirely — including a corrupt level far above `PushThreshold`.

**MUST**: the routing decision is reachable only through a resolved `Interrupt`, never as a bare
float a caller could compute for itself. Ruling 1's promise is then structural: there is no
in-package path from an unresolved number to `RoutePush`.

**MUST**: `Route` is total and returns exactly one of the two members for any `Interrupt` — there
is no third outcome and no path that yields both, which is I16's mutual-exclusivity made a
property of the return type rather than a convention two call sites must each honour.

**MUST**: the boundary is inclusive on the push side — `level == PushThreshold` → `RoutePush`.

**Scenario: a degraded classification never reaches Push**
- GIVEN a classification whose `interrupt_level` field failed to decode (`nil`)
- WHEN `ResolveInterrupt(nil).Route()` is evaluated
- THEN the result is `RouteDigest` — never `RoutePush`, twice over: by R3.1's inequality and by
  the degraded short-circuit, either of which alone would suffice

**Verified by**: L1 — `level == PushThreshold` → `RoutePush` (boundary); one ulp below →
`RouteDigest`; `1.0` → `RoutePush`; a degraded `Interrupt` carrying a level above the threshold →
`RouteDigest`; the composed degraded-path scenario above, driven through both functions together
rather than asserted as a separate fact about the constants.

---

## 4. Digest gates — PR 5 `feat/core-prospection-digest-gates`

Traced to doc 02 §7 ("with a cadence… two care gates… anti-starvation") and owner rulings 2 and
4.

### R4.1 — a digest-cadence gate exists, once daily, at an hour distinct from consolidation's

**MUST**: `internal/core/prospection` exposes a pure predicate deciding, given the last digest
delivery instant (or its absence) and `now`, whether a digest is due — true at most once per
local calendar day, at a fixed hour. The hour is a named constant distinct from
`internal/scheduler.ConsolidationHour` (03:00), per owner ruling 2 ("distinct from consolidation's
03:00 so the two passes never contend for one instant").

**sdd-design owes**: the constant's name, its hour value, and its `docs/02-cognitive-core.md`
§13 row (umbrella §4.2's second row — the demo asks for a *morning* digest, so the hour, not only
the cadence, is design's to choose).

**MUST**: a `nil` "last delivered" input (no digest has ever gone out) is due immediately once the
cadence hour is reached — the same "no prior state" posture `focus`'s hysteresis takes for an
empty incumbent.

**Verified by**: L1 — once design names the hour: not-yet-due before it on the same day; due at
it; already-delivered-today not due again; a `nil` prior delivery due at the first qualifying
instant.

### R4.2 — the low-energy care gate holds back non-urgent items and lets important ones through, over `focus.Priority`

**MUST**: `internal/core/prospection` exposes a pure predicate reporting whether the user is in the
low-energy state doc 02 §7's gate reads, from a `current_state` energy reading (or its absence) and
`now`. Doc 02's "low (recent reading)" is two conditions, not one: a reading older than a named
recency bound does not gate anything, whatever its level. An **absent** reading is not low — no
observation is not an observation of depletion — mirroring R3.1's posture toward a value core
cannot interpret.

**MUST**: the package exposes a pure function taking the whole set of pending digest items, that
low-energy fact, and `now`, and splitting it into what this digest carries and what it holds. At or
above the energy threshold every item is carried; below it, only the most important survive.

**MUST**: "important" is ordered by `focus.Priority` (through `focus.Rank`). Ordering by
`interrupt_level` instead is explicitly rejected (owner ruling 4) — no function in this package
computes a digest gate from `interrupt_level`.

**MUST**: the low-energy cut is a **bounded item count**, not an absolute score threshold.
`focus.Priority` is homogeneous of degree 1 in effective weight, so a fixed cut-off on the score
itself would mean something different in every vault — the same argument that made
`hysteresis_margin` relative in `m2a`. The bound is a named constant.

**MUST**: this requirement and R4.3 are observable as **one composed function over the whole
list**, not as two independent single-candidate predicates whose composition each caller would have
to get right. Both stay individually observable: every scenario either requirement states is a case
of that one function's own behaviour, and "carried regardless of rank" (R4.3) is provable only
where the ranking and the override live together.

**MUST**: for a `pattern_based` trigger with no source unit (`triggers.unit_id` nullable,
`0001_core_tables.sql:44`) — and therefore no `focus.Priority` to compute — the function has a
defined answer rather than a nil dereference. Per owner ruling 4 and umbrella §3.3's own naming
of this gap.

**sdd-design owes**: the energy threshold's name and value, the recency bound's name and value, the
carried-item bound's name and value, their §13 rows, and the `pattern_based`-with-no-priority
answer (carry unconditionally, hold unconditionally, or an answer that falls out of the ranking
arithmetic — design's choice, stated explicitly rather than left to an implementer's judgment
call).

**Verified by**: L1 — for the energy predicate: an absent reading → not low; a level below the
threshold with a recent reading → low; exactly at the threshold → not low; a stale reading → not
low whatever its level. For the carry function: at/above energy → every item carried, nothing held;
below energy with fewer items than the bound → all carried; below energy with more → exactly the
top-ranked bound carried and the rest held; the `pattern_based`/no-priority item reaching design's
stated answer, not a panic.

### R4.3 — anti-starvation: one deferral is one day, and a bounded number of deferrals forces delivery regardless of the other gates

**MUST**: each digest item carries its consecutive deferral count (an integer, incremented once per
digest cycle the item was held back — one deferral corresponds to exactly one day, per owner ruling
2's stated anti-starvation unit) into R4.2's carry function, and an item whose count has reached a
named bound is carried on this cycle **regardless of rank**, whatever R4.2's truncation alone would
have decided.

**MUST**: the forced carry is *in addition to* the truncation, never inside it — an item forced by
starvation does not consume one of the low-energy digest's ranked slots. This is the half of
"regardless" a per-candidate predicate cannot state, and it is why R4.2 and R4.3 are one function
(R4.2's composition MUST).

**MUST**: the deferral count needed to force delivery is a named constant.

**sdd-design owes**: the constant's name, its value, and its §13 row (umbrella §4.2's fourth row
— "how many deferrals an item survives before it is delivered regardless").

**Scenario: an item held back long enough surfaces even on a low-energy day**
- GIVEN a candidate held back by R4.2's low-energy gate on the (design-owed) maximum number of
  consecutive prior cycles, ranked last of all pending items
- WHEN the carry function evaluates the list on the next cycle, still on a low-energy day
- THEN that candidate is carried, independent of what the ranked truncation alone would have
  decided, and it does not displace a higher-ranked item from the truncation

**Verified by**: L1 — once design names the constant: a count below it → the item defers to R4.2's
own ranked answer; at or above it → carried regardless of rank, asserted with the item ranked last;
a fresh candidate (zero deferrals) never force-carries on its first cycle.

---

## 5. Recurrence and lead time — PR 6 `feat/core-prospection-recurrence`, PR 7 `-arming`

Traced to doc 02 §7 (`recurrence_rule` `yearly`/`monthly` + `recurrence_anchor`; lead time
default 7 days) and I17.

### R5.1 — `NextOccurrence` computes the next fire instant for a recurring trigger, pointing at the same source unit

**MUST**: `internal/core/prospection` exposes a pure function computing the next occurrence from
`(rule, anchor, after time.Time)` — the instant parameter is named `after` rather than `firedAt`
because the caller may mean the arming instant and not only the previous occurrence — for
`rule ∈ {yearly, monthly}` and `anchor = {month, day}`
(`yearly`) or `{day}` (`monthly`). The function returns only the next fire instant — I17's "same
unit" half is a caller obligation (the re-armed trigger row carries the original `unit_id`
unchanged), proven at `m3d` where the row is actually constructed; this function has no unit-id
parameter to get wrong.

**MUST**: the function is total over every valid `(rule, anchor)` pair — it never errors and
never panics for a syntactically valid anchor, including the two edge inputs below.

**Scenario: a yearly anchor at 29 February, evaluated toward a non-leap year**
- GIVEN `rule = yearly`, `anchor = {month: 2, day: 29}`, `after` in a leap year
- WHEN `NextOccurrence` computes the following year's occurrence and that year is not a leap year
- THEN it returns a single, deterministic date within that year — no error, no panic. **The exact
  resolution (clamped to 28 February, or rolled forward to 1 March) is an sdd-design obligation**
  (umbrella §4.2's sixth row), recorded with its own `docs/02-cognitive-core.md` §7 amendment, not
  invented here.

**Scenario: a monthly anchor at day 31, evaluated toward a 30-day month**
- GIVEN `rule = monthly`, `anchor = {day: 31}`, `after` in a 31-day month
- WHEN `NextOccurrence` computes the following month's occurrence and that month has 30 days
- THEN it returns a single, deterministic date within that month — same resolution obligation as
  above, stated once and applying to both edges rather than two separate rules

**MUST**: whichever resolution design chooses, it is applied consistently — the same
`(rule, anchor)` pair, evaluated repeatedly toward the same target month/year, always returns the
same instant (determinism), and the chosen rule is the one place this behaviour is decided, not
one judgment call per call site.

**Verified by**: L1 — an ordinary anchor (e.g. 15 March / day 10) across several years/months as
a sanity baseline; the two edge scenarios above, pinned to design's chosen resolution once named;
a determinism check re-running the same input twice.

### R5.2 — lead time computes a dated event's `fire_at` a fixed number of days before `event_at`

**MUST**: `internal/core/prospection` exposes a pure function computing `fireAt = eventAt -
EventLeadDays * 24h` from `(eventAt time.Time)`. `EventLeadDays == 7`, a named constant — doc 02
§13's existing "7 days" row gains this Go home (umbrella §4.3, PR 7 — the arming PR, where the constant is first read). This is a **separate**
constant from `focus.UrgencyLeadDays` (also 7 by coincidence — `m2a`'s own recorded distinction:
one is prospection's notification horizon, the other is the ranking's).

**MUST**: an `eventAt` less than `EventLeadDays` away from — or already before — some reference
instant is not this function's concern to gate; it always returns `eventAt - 7d`, whatever that
instant is. Whether an already-past `fire_at` arms at all is `m3b`'s (arming at capture) or R6.1's
concern below, not lead time's own arithmetic.

**MUST**: this arithmetic and the arming clamp are **two composed layers, not one rule, and PR 7
ships both**. This function is exported and unclamped, so the subtraction is testable on its own
and means one thing everywhere. R6.1's `Arm` calls it and then clamps the result to be at or after
`now`, because an event captured two days before it happens must not arm a trigger five days in the
past and be declared stale on the very next pass (R1.1). The clamp lives at `Arm`'s call site and
never inside this function: the lead time is a *notification horizon*, and "the system is not late
for an event it only just learned about" is an arming decision, stated once, not an invisible
property of a subtraction.

**Scenario: lead time computed for an event nine days out**
- GIVEN `eventAt` nine days after a reference instant
- WHEN the lead-time function computes `fireAt`
- THEN `fireAt` is exactly two days after the reference instant (`9 - 7`)

**Verified by**: L1 — the arithmetic identity above; a case where `eventAt - 7d` lands in the
past, asserting the function still returns it (no clamping at this layer); and, in R6.1's own
tests, the same input reaching `Arm` and coming back clamped to `now` — the two layers asserted
separately, so neither can quietly absorb the other.

---

## 6. Arming — PR 7 `feat/core-prospection-arming`

Traced to doc 02 §5 step 5 (hooks arm triggers/timers) and §5.1's degradation table (dated fields
degrade to "no date… nothing is armed for it"), plus owner ruling 1's R1 answer.

### R6.1 — `Arm` maps a classification to what gets armed, or to nothing

**MUST**: `internal/core/prospection` exposes a pure function taking a `classify.Classification`
(or the minimal subset of its fields — `Kind`, `EventAt`, `DueAt`, and the decoded
`RecurrenceRule` field PR 4 adds) and `now time.Time`, returning an arming plan: for
`Kind = timer`, arm a timer at the
requested offset; for a dated `event`, arm a `time_based` trigger at R5.2's `fireAt`; for
`recurring_reminder`, arm a `time_based` trigger carrying `recurrence_rule` +
`recurrence_anchor`; for every other `Kind`, arm nothing.

The rule is a **decoded field**, never `structured_data`. Doc 02 §5.1 says `structured_data`
"is opaque to the brain and stays opaque" (`docs/02-cognitive-core.md:562`), and a recurrence
rule the arming decision must read is by definition not opaque to it.

**MUST**: a classification whose relevant date field degraded to absent (`EventAt == nil` for an
`event`, or the equivalent for `recurring_reminder`) arms **nothing**, and the function reports
this as a distinct outcome from "not an arm-worthy kind" rather than silently falling through the
same branch — doc 02 §5.1's own rule: *"arming a trigger on a guessed date is worse than not
arming one."*

**MUST**: `Arm` reads `Classification.EventAt` for an `event`'s arming instant and never `DueAt`,
and vice versa for any capture kind that arms from `DueAt` — I18's pure half, the first path in
this package that reads one of the two fields to compute a `fire_at`.

**MUST**: a dated `event` or `recurring_reminder` whose instant is at or before `now` arms
**nothing**. Arming a nudge for something already over is doc 02 §5.1's own refusal in a different
direction. This is distinct from R5.2's clamp, which applies when the *lead time* — not the event —
has already partly elapsed: an event still ahead of `now` arms at `now`; an event behind `now` arms
not at all.

**MUST**: `Arm` consumes ruling 1's answer for `interrupt_level` at the point of arming: the plan's
interrupt field is R3.1's `ResolveInterrupt` applied to whatever the classification carried, and it
is always populated — an armed trigger's interrupt level is never left unset when a classification
supplied nothing, and the resolved value carries its own degraded flag rather than a bare number a
later caller could misread.

**Scenario: a `timer` kind with a resolved offset arms a timer, not a trigger**
- GIVEN a `Classification{Kind: timer, …}` with a parsed offset
- WHEN `Arm` evaluates it
- THEN the plan arms a timer (never a `triggers` row), matching doc 02 §8's "a timer is NEVER a
  unit" — this function does not construct a unit-shaped value either way

**Scenario: an `event` classification with a degraded `EventAt` arms nothing, and says why**
- GIVEN a `Classification{Kind: event, EventAt: nil}` (the date failed to decode)
- WHEN `Arm` evaluates it
- THEN the plan is "no arming", and the reported reason distinguishes this from a `chitchat`
  classification's own "no arming" — different causes, both auditable once `m3b`'s
  `decision_log` write consumes this plan

**Verified by**: L1 — the four-way outcome table (`timer`, dated `event`, `recurring_reminder`,
everything else) driven from every `classify.Kind` member; a degraded-date `event`/
`recurring_reminder` input arming nothing with a distinct reason; the `interrupt_level` resolution
composed with R3.1; an assertion that no function in this file returns `unit.Unit`,
`*unit.Unit`, or `[]unit.Unit` (mirroring `m2a` R1.3's read-only-package property, applied here to
prove arming plans a trigger/timer, never a unit).

---

## 7. What this spec does not require

Matching the umbrella §5's `m3a` row and §3.2: the `TriggerRepo`/`TimerRepo` ports, their SQLite
implementations, the due-scan runner, `decision_log` writes (I12), quiet-hours *deferral* as a
persisted effect and its waking-resurfacing (I16's behavioural half), the digest's
`focus.Priority`-producing query and its `pattern_based`-safe caller, the Telegram adapter, the
scheduler tick, `nooma check`, and `serve` wiring are all `m3b`/`m3c`/`m3d`. No requirement above
depends on any of them existing; every scenario is provable against a repo-constructed input, with
no database, no network, and no real clock.

## Exit criterion (this change's own success condition)

Every gate named in umbrella §4.2 that falls to `m3a` — the delay-caveat threshold (§1), the
digest cadence hour and its low-energy/importance thresholds and anti-starvation count (§4), and
the recurrence edge-case resolution (§5) — has, by the time this change closes: a named constant
under `internal/core/prospection` (or an explicit, reviewed decision that the behaviour is a rule
rather than a number, for recurrence's edge resolution), a `docs/02-cognitive-core.md` §13 row
where the behaviour is numeric, and a conformance test proving the property this document states
— with no file under `internal/ports`, `internal/store`, `internal/brain`, `internal/scheduler`,
or `internal/channels` referencing any symbol this package exports.

---

## Reconciliation note — 2026-08-19

This document and `design.md` were written concurrently and never read each other. `tasks.md`,
written last against both, recorded five disagreements as Findings F1–F5. This document was
corrected against those recorded resolutions on 2026-08-19; `design.md` carries the mirror note.

- **F1 — staleness formula (correctness, not wording).** R1.1 measured `overdue` as
  `now.Sub(fireAt)`, which expires every trigger armed between 00:00 and 01:00 before the user
  wakes, because the quiet window (7 h) is longer than `trigger_staleness_hours` (6). R1.1 now
  measures from the first deliverable instant, states the quiet-hours-before-staleness ordering,
  and states the no-trigger-armed-in-quiet-hours-is-stale-at-07:00 property. R2.1 gained the
  first-deliverable-instant function it is computed from. Design's formula won.
- **F2 — function names.** `EvaluateTriggerStaleness`/`EvaluateTimerStaleness` → `TriggerVerdict`/
  `TimerVerdict` (one shared verdict vocabulary, no schema status strings in core);
  `ResolveInterruptLevel`/`DecideDelivery` → `ResolveInterrupt`/`Interrupt.Route`, with the opaque
  type's `degraded` flag stated as the second guard on ruling 1; `DecideArming` → `Arm`. Design's
  names and shapes won, and the reasoning around them was rewritten, not only the symbols.
- **F3 — `classify.Classification.RecurrenceRule`'s type.** Resolved inside `design.md` §3.8/§4;
  this document names no classify-side type and needed no change.
- **F4 — `Carry`.** R4.2's per-candidate importance threshold and R4.3's standalone deferral
  predicate are now one composed function over the whole item list, with a bounded item count in
  place of an absolute score cut and "carried regardless of rank, in addition to the truncation"
  stated where it is provable. Both requirements stay separately observable. Design won.
- **F5 — lead time.** R5.2 now states the two-layer composition explicitly: the exported,
  unclamped subtraction stays unclamped and independently testable, and `Arm` applies the now-clamp
  at its own call site. Neither side was weakened; they were never in conflict.
- **PR numbering (recorded in `tasks.md`'s preamble, not as a Finding).** Section labels and the
  slicing paragraph now follow `design.md` §7's seven PRs, which `tasks.md` treats as authoritative
  over this document's original six.

## Reconciliation note — 2026-08-20

A second pass over the same pair surfaced two disagreements `tasks.md` had not caught. Both are
recorded here rather than fixed silently.

- **F6 — the delay-caveat boundary direction.** R1.3 already read *"at or above it, one is"*;
  `design.md` §3.3 implemented `overdue > DelayCaveatMinutes`, so exactly at the threshold the two
  documents disagreed. **Resolved inclusive**, in this document's favour: `design.md` and
  `tasks.md` 2.5/2.6 were corrected to `>=`. The argument is asymmetric cost — an unnecessary
  caveat costs one clause of politeness, a missing caveat on a genuinely late delivery is the
  out-of-context nudge ADR-0009 exists to prevent. Nothing changed here.
- **F7 — whether `internal/core/classify` is inside `m3a` at all.** The scope box and §R3.1's
  "MUST NOT (flagged)" paragraph both declared that package out of scope, while `design.md` §7
  ships it as PR 4 under owner ruling 1. **Corrected here**: the scope box now names the widening,
  R3.1's paragraph is a MUST naming PR 4, and R6.1's field list routes recurrence through the
  decoded `RecurrenceRule` field rather than through `structured_data` — which doc 02 §5.1 says is
  "opaque to the brain and stays opaque" (`docs/02-cognitive-core.md:562`).
