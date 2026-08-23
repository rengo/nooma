# Design — M3 Phase A: prospection (pure)

Technical design for `m3a-prospection`, the first of the four chained changes
[`m3-mouth-telegram/proposal.md`](../m3-mouth-telegram/proposal.md) §5 splits M3 into. Scope is
that document's **m3a block only**: `internal/core/prospection` in full, plus the one
`internal/core/classify` widening owner ruling 1 requires. `m3b` (ports, store, arming at capture,
due scan), `m3c` (channel port and Telegram adapter) and `m3d` (tick, delivery, check-ins, demo)
are out.

`m3a` ships a package that is `doc.go`-only today
(`internal/core/prospection/doc.go:1-4`) and touches exactly one other: `internal/core/classify`.
**Zero ports, zero store, zero brain, zero I/O, no migration.**

**This design is mostly not about code.** Proposal §4.2 names **seven behaviours doc 02 gestures
at and does not define**, and six of the seven fall inside `m3a`'s pure boundary. §3 of this
document decides each one, shows the derivation, names the Go constant, and gives the
`docs/02-cognitive-core.md` §13 row it lands in with its default and its PR. Where a number is
**chosen** rather than **derived**, the row says so — that distinction is load-bearing, because a
design that presents a judgement call as a derivation is harder to overturn later than one that
flags it.

It does not restate requirements — that is `spec.md`, written concurrently by `sdd-spec` and never
read or edited here. It does not edit `docs/`; it *describes* the doc 02 amendments each PR makes,
because `CLAUDE.md` non-negotiable #1 requires the doc delta to ship in the same PR as the code.

> **Three things this design decides that the umbrella proposal did not anticipate**, each flagged
> for owner review rather than applied silently:
> 1. **I16's "defined push exception" is defined here for the first time, and it is not a
>    threshold** — it is the *timer* (§3.2). Doc 02 §7 as written defers pushes during quiet hours;
>    the exception named in `docs/06-harness.md:256` and proposal §2 has no definition anywhere in
>    the tree.
> 2. **Classify gains a second field, `recurrence_rule`** (§3.6). Ruling 1 widened classify for
>    `interrupt_level`; `monthly` recurrence has exactly the same "no producer" defect (R1) and
>    doc 02 §5.1 forbids reading it out of `structured_data`.
> 3. **`m3a` is seven PRs, not six** (§7). The classify widening is its own slice.

---

## 1. Ground truth this design was verified against

Every row was read at the named file and line in this session.

| Claim | Verified at |
|---|---|
| `internal/core/prospection` is a single 4-line `doc.go` with no declarations; its package comment already states the charter *"decides staleness, quiet hours, digest vs push and recurrence"* | `internal/core/prospection/doc.go:1-4` |
| `triggers` carries `interrupt_level REAL` **nullable, no DEFAULT, no CHECK**; `unit_id` is nullable "NULL for pattern_based"; `payload TEXT NOT NULL DEFAULT '{}'` documented as "action, rationale, lead_days…" | `internal/store/sqlite/migrations/0001_core_tables.sql:44`, `:48`, `:49` |
| `timers` has `pending\|fired\|cancelled` and **no `interrupt_level` column** | `0001_core_tables.sql:61-70` |
| The calibration gate matches **only** `internal/core/<pkg>.<Symbol>` and parses the Default cell with `^-?\d+(?:\.\d+)?` anchored at the start | `test/conformance/calibration_doc_test.go:35`, `:46` |
| A §13 row naming **two** core constants is a hard error — "this gate reads one number per row… Split the row" | `calibration_doc_test.go:234-238` |
| `calibrationMinSymbols = 21` is a **floor**, not an equality; adding rows cannot fail it | `calibration_doc_test.go:29`, `:138-145` |
| §13's Quiet-hours row reads `[00:00, 07:00) local` — no leading number, so it parses to `""` and would fail loudly the day it named a constant | `docs/02-cognitive-core.md:913`, gate at `calibration_doc_test.go:250-255` |
| §13 rows 912/914/920/921 (`Push threshold` 0.70, `Event lead time` 7 days, `trigger_staleness_hours` 6, `timer_staleness_hours` 3) name no constant and are therefore unchecked today | `docs/02-cognitive-core.md:912`, `:914`, `:920`, `:921` |
| Doc 02 §7's push bullet states quiet hours **inside** the push branch: *"immediate push… skipping cadence and gates. Quiet hours [00:00, 07:00) local time: deferred and resurfacing on waking."* | `docs/02-cognitive-core.md:772-774` |
| I16's harness row says "except the defined push exception" — and **nothing in doc 02 defines one** | `docs/06-harness.md:256` |
| I15/I16/I17 already have rows in `docs/06-harness.md` §4, so no invariant-table row is added — only tests | `docs/06-harness.md:255-257` |
| `ports.Clock.Now()` returns a `time.Time`, and the port is consumed by `internal/brain`, never by core; its doc comment names "quiet hours, staleness gates, recurrence" as the reason | `internal/ports/clock.go:5-20` |
| The zone travels inside the instant, and there is deliberately no timezone key in configuration | `docs/02-cognitive-core.md:600-610`; mechanism precedent `internal/core/classify/prompt.go:28-43` |
| `depguard`'s `core-purity` allows `internal/core/**` only `$gostd` and `internal/core`, so `prospection` may import `classify` and `focus` and may not import `ports` | recorded from `.golangci.yml:47-77` in `m2a-weight-focus/design.md:86` |
| `focus.Rank(cs []Candidate, adjacency map[string]float64, now time.Time) []Ranked` exists, with a three-level total tie-break and a documented NaN posture | `internal/core/focus/rank.go:74-100` |
| `focus.Candidate` carries `DueAt *time.Time` and **no `EventAt`**; `unit.Unit` carries **both** | `internal/core/focus/priority.go:158-166`, `internal/core/unit/unit.go:28-29` |
| `focus.Priority` is multiplicative in `e = weight.Effective(...)`, so a zero-valued Candidate scores **exactly 0** | `internal/core/focus/priority.go:232-242` |
| `focus.UrgencyLeadDays = 7` is deliberately a **separate knob** from prospection's Event lead time despite the identical value | `internal/core/focus/priority.go:11-18` |
| `focus.DefaultSize` is §13's `focus_size` = 7, "a human attention bound, 7±2" | `docs/02-cognitive-core.md:904` |
| `consolidation.LoadCooldownDays = 7`, `consolidation.CatchUpStalenessHours = 24` | `docs/02-cognitive-core.md:911`, `internal/core/consolidation/schedule.go:14` |
| The house pattern for a calibratable is an **untyped** constant, deliberately not a `time.Duration`, because the gate reads the bare number | `internal/core/consolidation/schedule.go:8-14` |
| The house pattern for building a local instant is `time.Date` with out-of-range fields, **never** `AddDate` — with the Havana and Apia/Kiritimati anomalies worked out | `internal/core/consolidation/schedule.go:66-118` |
| Classify's decoder is a 13-row table; adding a field is one row plus a three-line assigner | `internal/core/classify/decode.go:33-49` |
| Classify's degradation vocabulary has five `Reason` values; `ReasonBadFormat` covers "the JSON type was right, the value is not one this field reads" | `internal/core/classify/classification.go:57-72` |
| Every classify field is a pointer so "absent" and "a legitimate zero" cannot collapse | `internal/core/classify/classification.go:21-24` |
| Classify refuses to invent per-type constant tables where doc 02 names none — "exactly two numbers here, not eighteen" | `internal/core/classify/prior.go:9-19` |
| `structured_data` is "opaque to the brain and stays opaque" | `docs/02-cognitive-core.md:562` |
| `schedules.proactive_check` is decoded but has **no Go default constant** — `internal/config/defaults.go` names no schedule; the `*/5 * * * *` default exists only in `docs/01-architecture.md:227` and a test fixture | `internal/config/config.go:86-89`, `internal/config/defaults.go`, `internal/config/decode_test.go:34` |
| `internal/core/focus` still has no importer outside its own package | grep over `internal/`; `m3a` PR 5 becomes the first |

---

## 2. What `m3a` decides, in one paragraph

`core/prospection` owns **when a pending nudge may be delivered** (quiet hours), **when it is too
late to deliver it** (the two staleness gates), **which lane it takes** (push versus digest),
**what a digest carries on a depleted day** (the care gate and anti-starvation), **when the next
occurrence of a recurring nudge is** (calendar arithmetic), and **what a classification arms**.
Every one is a pure function over data; the current instant arrives as a named `now time.Time`
parameter on every function that needs it and on none that does not. Nothing in the package returns
a status string the schema knows (`expired`, `cancelled`, `fired`) — core returns a *verdict* and
`brain` names the transition, which is what keeps I12's "log from `brain`, never from `core`" a
property of the API rather than a rule somebody remembers.

---

## 3. The six behaviours doc 02 does not define, decided

Proposal §4.2 lists seven. Six are `m3a`'s. The seventh — *"important ones"* — is §3.4 here, and
owner ruling 4 already fixed its mechanism (`focus.Priority`); what remained undefined was its
*shape*, and that is decided below.

### 3.1 Quiet hours — the representation, and why the §13 row must split

Doc 02 §13's Default cell is `[00:00, 07:00) local`. `calibration_doc_test.go:46` anchors its
number parser at the start of the cell, so that cell yields `""`, and `:250-255` turns an empty
parse into a hard failure the moment the row names a constant. `:234-238` separately forbids one
row naming two constants. **Two rows is therefore the only representable shape**, and it happens
to be the shape the code wants anyway:

```go
// QuietHoursStartHour is the local hour at which quiet hours open, inclusive.
const QuietHoursStartHour = 0
// QuietHoursEndHour is the local hour at which quiet hours close, exclusive.
const QuietHoursEndHour = 7

// InQuietHours reports whether now's local wall clock falls in
// [QuietHoursStartHour, QuietHoursEndHour).
func InQuietHours(now time.Time) bool
```

Untyped int constants, not `time.Duration` and not `time.Time` — `consolidation.CatchUpStalenessHours`'s
own doc comment gives the reason verbatim (`schedule.go:8-14`): a `Duration` holds
`25200000000000`, not `7`, and the gate reads the bare number.

**The zone is not a parameter and not a config key.** `now.Hour()` reads the wall clock in
`now.Location()`, and the location travels inside the instant `ports.Clock.Now()` produced
(`docs/02-cognitive-core.md:600-610`; the same mechanism `classify/prompt.go:28-43` documents for
capture). A test `Clock` fixing a `Location` is what makes every assertion in this package stable.

**The window is half-open on purpose**, matching doc 02's own `[00:00, 07:00)` notation: 07:00:00.000
is not quiet. That boundary is load-bearing twice over — §3.4's digest hour and §3.2's
`DeliverableFrom` both land exactly on it.

**Resurfacing is "the first pass at or after `QuietHoursEndHour`", not an 07:00 alarm.** There is
no scheduled wake-up job, and core could not hold one. A deferred item is simply an item whose
gate said *defer* last pass and says *deliver* this pass. Two consequences worth writing down:

- **Quiet-hours deferral needs no persisted state.** It is recomputed every pass from `fire_at` and
  `now`. Only §3.4's low-energy deferral is counted, and it is counted for a different reason.
- **The worst case is one tick of lateness** — at the shipped `*/5` cadence, five minutes. Proposal
  §4.5 already priced this and it is the intended semantics.

#### §13 rows (PR 1)

| Knob | Default | Status |
|---|---|---|
| `quiet_hours_start_hour` (`internal/core/prospection.QuietHoursStartHour`) | 0 | **replaces row 913**, half of the split |
| `quiet_hours_end_hour` (`internal/core/prospection.QuietHoursEndHour`) | 7 | **replaces row 913**, the other half |

### 3.2 I16's "defined push exception" — it is the timer, not a threshold

Doc 02 §7 puts quiet hours *inside* the push bullet (`02:772-774`): a push skips "cadence and
gates", and then quiet hours defer it. `docs/06-harness.md:256` and proposal §2 both promise an
exception. **Nothing in the tree defines one.** This is an eighth undefined behaviour, discovered
by reading the gate, and it must be closed here because it is exactly the branch I16 tests.

| Option | Verdict |
|---|---|
| **A. No exception — quiet hours are absolute** | Safest, and makes I16 trivially structural. But it leaves `docs/06-harness.md:256`'s own wording false and deletes a promise two documents make |
| **B. A second, higher `interrupt_level` threshold** | **Rejected outright.** Ruling 1 makes the *model* the producer of `interrupt_level`. A second threshold means a hallucinated 0.95 wakes the user at 03:00 — ADR-0009's stake, inverted. It is also a safe default expressed as a number rather than as structure, which `CLAUDE.md` non-negotiable #7 refuses |
| **C. The exception is the *timer*** — chosen | A timer's instant was set by the user, in the user's own words, at capture ("remind me in 15 min"). A trigger's instant was *inferred* by the brain from a dated event, a lead time, or a pattern. **An explicit instruction outranks a policy window; an inference does not.** Needs no number, cannot be reached by a model hallucination, and makes I16 non-vacuous |

**Decision: C.** Concretely:

- `TriggerVerdict` applies the quiet-hours gate. **Every** trigger defers, whatever its
  `interrupt_level`. The cost of deferring a genuinely urgent inferred nudge is bounded by seven
  hours and is recoverable; the cost of one wrong 03:00 wake-up is neither — that is ADR-0009's
  own argument, applied to the one branch it did not reach.
- `TimerVerdict` does not apply it. A timer is never deferred, which is also why a timer never
  accrues policy delay and needs no `DeliverableFrom` shift (§3.3).
- Doc 02 §7's push bullet is amended in PR 1 to state the exception explicitly, and
  `docs/06-harness.md:256`'s I16 row gains the four words that name it. **Owner-review item R1.**

This also settles a second §7 clause with no extra machinery: *"Mutually exclusive with the digest
(no double delivery)"*. `Route` (§3.4) is a total function into a two-member closed vocabulary, and
timers are not routed at all — they are always pushed, which the schema already implies by giving
`timers` no `interrupt_level` column (`0001_core_tables.sql:61-70`).

### 3.3 Staleness — and the seven-hour window that would have expired a whole hour of the calendar every night

Doc 02 §13 fixes `trigger_staleness_hours = 6` and quiet hours at seven hours long. Read naively —
overdue measured as `now - fire_at` — **every trigger armed between 00:00 and 01:00 expires before
the user wakes, every night, forever.** Seven is greater than six. Nothing in the tree says this
out loud; it falls out of two rows of §13 that were written years apart.

The fix is not to move either number. It is to say what "overdue" measures:

```go
// DeliverableFrom returns the first instant at or after t at which a trigger
// may actually be delivered: t itself when t is outside quiet hours, and that
// day's QuietHoursEndHour otherwise.
func DeliverableFrom(t time.Time) time.Time

// Verdict is the whole delivery gate's output for one pending item.
type Verdict string

const (
	VerdictPending Verdict = "pending" // fire_at is still in the future
	VerdictDefer   Verdict = "defer"   // deliverable later today — quiet hours
	VerdictStale   Verdict = "stale"   // past its staleness window
	VerdictDeliver Verdict = "deliver"
)

func TriggerVerdict(fireAt, now time.Time) Verdict
func TimerVerdict(fireAt, now time.Time) Verdict
```

Both delegate to one unexported `verdict(fireAt time.Time, from time.Time, stalenessHours int, now
time.Time) Verdict`, evaluated in this order — and the order is the design:

1. `now.Before(fireAt)` → **Pending**. Not yet due.
2. trigger only: `InQuietHours(now)` → **Defer**. *Quiet hours are evaluated before staleness, so
   an item is never expired during a window in which it was refused delivery.*
3. `now.Sub(from) > stalenessHours` → **Stale**.
4. → **Deliver**.

where `from = DeliverableFrom(fireAt)` for a trigger and `from = fireAt` for a timer.

**Why only the *first* quiet-hours segment is excluded, and why that is the correct rule rather
than an approximation of a harder one.** A full "deliverable-time accumulator" — subtracting every
quiet-hours segment between `fire_at` and `now` — is computable and is *wrong*. Once an item has
had real deliverable time and was not delivered, the system was down or broken, and ADR-0009's
staleness gate is precisely the rule for "we were down": *"nudges that arrive late and out of
context… teaches the user to ignore notifications."* So:

> **Staleness counts from the first instant the item could have been delivered, and real time
> thereafter — because after that instant every further delay is the system's fault or the world's,
> never policy's.**

Worked boundaries, all at `TriggerStalenessHours = 6`:

| `fire_at` | Pass at | `DeliverableFrom` | Overdue | Verdict |
|---|---|---|---|---|
| 00:30 | 03:00 | 07:00 | — | **Defer** (rule 2 fires first) |
| 00:30 | 07:00 | 07:00 | 0 | **Deliver**, no caveat |
| 00:00 (the worst case the naive reading killed) | 07:00 | 07:00 | 0 | **Deliver** |
| 20:00 | next day 10:00 (downtime) | 20:00 | 14 h | **Stale** |
| 23:30 | next day 07:00 (downtime) | 23:30 | 7.5 h | **Stale** — correct: it had 30 deliverable minutes and the system missed them |
| 06:00 | 06:05 | 06:00 (07:00? no — 06:00 *is* quiet) → 07:00 | — | **Defer**, then Deliver at 07:00 with overdue 0 |

The last row is the property worth asserting as a test rather than trusting: **no trigger armed
anywhere inside `[QuietHoursStartHour, QuietHoursEndHour)` is ever `Stale` at
`QuietHoursEndHour`.** It holds for any quiet-hours width, including a future recalibration, and
it is the whole reason `DeliverableFrom` exists.

**The comparison is strict** (`> stalenessHours`, not `>=`), matching `consolidation.CatchUpDue`'s
own documented convention (`schedule.go:19-26`) and ADR-0009's "more than" wording. Exactly six
hours overdue is still delivered.

**Timers cancel, triggers expire — and core says neither.** `VerdictStale` is one value; `brain`
maps it to `expired` on a trigger (I15) and `cancelled` on a timer (doc 02 §8's own vocabulary).
Core owning the status strings would put a schema vocabulary inside a pure decision and would give
`prospection` two nearly-identical verdict types for one decision.

#### The delay caveat — behaviour §4.2 #7

ADR-0009 requires a delivered-but-late nudge to "mention the delay explicitly". At what overdue
amount? Proposal §4.2 states the failure mode itself: *"A timer three minutes late that announces
'three minutes ago you asked me…' is noise of a different kind."*

The threshold is derived from what the delay *is*. Below some amount, the lateness is the
scheduler's own granularity and is not a fact about the user's world at all. The shipped tick is
`*/5 * * * *` (`docs/01-architecture.md:227`), so one tick of lateness is mechanical, always, for
every delivery.

```go
// DelayCaveatMinutes is the overdue amount at which a delivery must say so.
const DelayCaveatMinutes = 15

// DelayCaveat reports the fact; the render layer picks the words. The
// comparison is inclusive: exactly DelayCaveatMinutes late already caveats.
func DelayCaveat(overdue time.Duration) bool { return overdue >= DelayCaveatMinutes*time.Minute }
```

**Chosen, with its band derived:** the threshold must be strictly greater than the largest delay
attributable to the scheduler. At the shipped default that is one tick (5 min). 15 is **three
ticks**, which keeps the caveat silent under ordinary jitter, one missed tick and a retry, and
still keeps it silent for a user who slows the tick to `*/15` — the first cadence at which a
2-tick threshold would caveat every single delivery. The relation to assert is
`DelayCaveatMinutes >= 3 × the default proactive_check period`; **it cannot be asserted in `m3a`**,
because `internal/config/defaults.go` declares no schedule default today (§1). It lands as an L2
test in `m3d` #1 (`feat/scheduler-proactive-tick`), the PR that first gives the tick a Go home.
Recorded as **risk R4** rather than left to be rediscovered.

**The comparison is inclusive (`>=`), and that is a decision, not the default.** Two reasons, both
worth writing down because the strict `>` is what an unexamined hand types:

1. **The two errors cost different amounts.** A caveat that was not strictly necessary costs the
   user one clause of politeness. A *missing* caveat on a genuinely late message is the exact
   failure ADR-0009 exists to prevent — a nudge that arrives out of context and teaches the user to
   ignore notifications. When one error is cheap and the other is the one the invariant was written
   for, the boundary belongs on the cheap side.
2. **It matches the repository's own convention for a permissive-side boundary.** Doc 02 §7's
   delivery split reads `interrupt_level >= 0.7 → immediate push` (`02:772`), inclusive, against
   §13's own `Push threshold (interrupt_level) | 0.70` row (`02:912`) — the threshold *is* the first
   value that qualifies, not the last that does not.

**And it is deliberately the opposite of `verdict`'s own comparison above**, which stays strictly
`>`: there the inclusive side is the *destructive* one — at exactly the staleness window an item is
not yet past it, and expiring is unrecoverable while delivering late is not. Same principle
(the boundary goes where being wrong is cheap), opposite direction, and the two are stated together
so the next reader harmonises neither into the other.

`DelayCaveat` returns a `bool`, not a sentence. This is doc 02 §7's own division of labour, stated
there for tone — *"the brain passes the fact (`loaded`), the render layer picks the words"* — and
applied here unchanged.

#### §13 rows (PR 2)

| Knob | Default | Status |
|---|---|---|
| `trigger_staleness_hours` (`internal/core/prospection.TriggerStalenessHours`) | 6 | **row 920 amended** — gains a constant, value unchanged |
| `timer_staleness_hours` (`internal/core/prospection.TimerStalenessHours`) | 3 | **row 921 amended** — gains a constant, value unchanged |
| `delay_caveat_minutes` (`internal/core/prospection.DelayCaveatMinutes`) | 15 — minutes; three shipped `proactive_check` ticks, so scheduler granularity never produces a caveat | **new row**, chosen with a derived band |

### 3.4 The delivery split, `interrupt_level`, and its degradation path

Owner ruling 1: classify emits `interrupt_level` per message, degrading to a single named constant,
and **a degraded classification never produces a push**. This section supplies the mechanism that
makes the second half structural rather than arithmetic.

```go
// PushThreshold is doc 02 §7's own comparison: >= 0.70 is a push.
const PushThreshold = 0.70

// DefaultInterruptLevel fills a degraded or unreadable interrupt_level.
const DefaultInterruptLevel = 0.0

// Interrupt is a resolved interrupt level. Its fields are unexported, so the
// only way to obtain one is ResolveInterrupt, and a degraded one cannot be
// constructed with Route() == RoutePush.
type Interrupt struct {
	level    float64
	degraded bool
}

// ResolveInterrupt reads what classify (or the triggers.interrupt_level column)
// supplied. nil, or a value outside [0,1], degrades.
func ResolveInterrupt(level *float64) Interrupt

func (i Interrupt) Level() float64 { return i.level }
func (i Interrupt) Degraded() bool { return i.degraded }

type Route string

const (
	RoutePush   Route = "push"
	RouteDigest Route = "digest"
)

// Route is total into a two-member vocabulary: doc 02 §7's "mutually exclusive
// (no double delivery)" as a type rather than as a rule.
func (i Interrupt) Route() Route {
	if i.degraded {
		return RouteDigest
	}
	if i.level >= PushThreshold {
		return RoutePush
	}
	return RouteDigest
}
```

**Two independent guards, and only one of them is a number.** `DefaultInterruptLevel <
PushThreshold` makes a degraded value route to the digest arithmetically; the `degraded` short-circuit
makes it route to the digest *even if a future recalibration lowers `PushThreshold` below the
default*. Ruling 1's promise survives a recalibration it never anticipated. The zero value
`Interrupt{}` also routes to the digest, so a forgotten initialisation is safe rather than loud.

**Out-of-range degrades, it does not clamp.** `triggers.interrupt_level` carries no `CHECK`
constraint (`0001_core_tables.sql:48`), so core cannot vouch for a value read back from it — the
identical boundary `focus.Priority` documents for adjacency (`priority.go:207-222`). Clamping 1.7
to 1.0 would manufacture a push out of a corrupt number; degrading refuses to reason about it. JSON
cannot carry NaN or ±Inf, so `[0,1]` is a total test over every value that can arrive.

**Why `DefaultInterruptLevel = 0.0`, and why that is not the sentinel doc 02 §5.1 warns about.**
Below `PushThreshold` every value is behaviourally identical in v1: the split is the only consumer,
and ruling 4 makes the digest's own ordering `focus.Priority`, not `interrupt_level`. So the number
is chosen for **auditability**, not for behaviour, and `0.0` reads as *no claim was made*. Doc 02
§5.1's warning — *"a degraded weight is not a zero weight"* (`02:563`) — is honoured by structure,
not by an invented sentinel value: `Interrupt.Degraded()` carries the distinction in memory, and
**`brain` persists `triggers.interrupt_level` as NULL when the resolution degraded**, which is what
the column's nullability is for. The round trip is exact: NULL ↔ degraded, in both directions, and
an auditor reading the glass box can always tell a claimed 0.0 from an absent one. That contract is
`m3a`'s to state and `m3b`'s to implement.

**Tone.** Doc 02 §7's *"TONE softens when the user is loaded… Urgent push is NOT softened"* needs no
new machinery: `LowEnergy` (§3.5) is the fact, and `Route() == RoutePush` is the exemption.

#### §13 rows (PR 3)

| Knob | Default | Status |
|---|---|---|
| Push threshold (`internal/core/prospection.PushThreshold`) | 0.70 | **row 912 amended** — gains a constant, value unchanged |
| `default_interrupt_level` (`internal/core/prospection.DefaultInterruptLevel`) | 0.0 — fills a degraded or out-of-range `interrupt_level`; behaviourally inert below the push threshold, chosen so an audit reads "no claim" | **new row**, chosen |

### 3.5 The digest — cadence, the care gate, and anti-starvation

Owner ruling 2: **once daily at a fixed morning hour**, its own §13 row, distinct from
consolidation's 03:00, one deferral = one day.

#### The hour is derived, not chosen

```go
// DigestHour is the local hour at which the daily digest becomes due.
const DigestHour = 7

// DigestDue reports whether a digest is owed at now, given the last one.
func DigestDue(lastDigestAt *time.Time, now time.Time) bool
```

Quiet hours end at 07:00 and that is already fixed by doc 02 §13. Therefore:

- a digest hour **before** 07:00 is a digest that is born deferred, every single day — the cadence
  would be decorative and the real delivery hour would be 07:00 anyway;
- a digest hour **after** 07:00 opens a dead window in which quiet-hours-deferred pushes have
  resurfaced and the digest has not, so the user's first morning contact is the one lane the
  design says should be rarer.

**The only hour at which the digest is both a morning digest and never born deferred is exactly the
hour quiet hours end.** `DigestHour = QuietHoursEndHour = 7`, and — following
`focus.UrgencyLeadDays`'s own precedent (`priority.go:11-18`) — they are **two constants and two
§13 rows**, because one is a delivery window's edge and the other is a cadence, and collapsing two
knobs because they agree today is how a calibration table becomes un-tunable. The relation
`DigestHour >= QuietHoursEndHour` is asserted as an L1 test.

Ruling 2's "distinct from consolidation's 03:00" holds by construction and for a reason worth
recording: 03:00 is *inside* quiet hours, which is why consolidation can run there and why delivery
cannot.

`DigestDue` is written so that downtime is a normal case (ADR-0014's own posture): due iff `now`'s
local hour is at or past `DigestHour` **and** `lastDigestAt` is nil or strictly before today's
`DigestHour` instant in `now.Location()`. A vault that was off for three days owes exactly one
digest, not three.

#### The care gate — "important ones", ruling 4's mechanism given a shape

Doc 02 §7: *"if `current_state.energy` is low (recent reading), it holds back non-urgent items and
only lets important ones through."* Three undefined words: **low**, **recent**, **important**.

```go
// EnergyReading is one current_state row as the gate sees it. Both fields are
// required: doc 02 §7's gate is "low (recent reading)", two conditions.
type EnergyReading struct {
	Level      float64
	RecordedAt time.Time
}

const LowEnergyMax = 0.5
const EnergyReadingMaxAgeHours = 24

// LowEnergy reports doc 02 §7's own two-part condition. A nil reading is not
// low: no observation is not an observation of depletion.
func LowEnergy(r *EnergyReading, now time.Time) bool
```

**`LowEnergyMax = 0.5` — chosen, and argued.** `energy` is declared on `[0,1]` (doc 02 §10) with no
calibration data behind it. The midpoint is the only point on such a scale that is not an
invention, and the repository has taken exactly this reading twice before: `weight_threshold = 0.5`
and `fuse.go`'s neutral `1.0` leg weights (*"start at the simplest form when no calibration data
exists"*, `m2a design §3.1`). The comparison is **strict** (`Level < LowEnergyMax`): the gate
suppresses delivery, so the burden of proof is on *low*, and exactly the midpoint is not low. Same
convention as `CatchUpDue` and `ExpireIncomplete`.

**`EnergyReadingMaxAgeHours = 24` — derived from the cadence.** The digest is once daily (ruling
2), so its input must be no older than one digest cycle; a reading from two digests ago would hold
items back on a day it never observed. It coincides with `incomplete_expiry_hours` and
`catch_up_staleness_hours` (both 24) **by coincidence, not by relation** — the §13 row says so, in
row 890's own words and for the same reason.

**"Important" — ruling 4, given the only shape `focus.Priority` admits.** An *absolute* priority
cut is impossible: `focus.Priority` is homogeneous of degree 1 in effective weight
(`m2a design §3.1`), so a fixed threshold on it means something different for every vault. That is
the same argument that made `hysteresis_margin` relative (m2a D8). The gate is therefore a
**relative truncation**:

```go
// LowEnergyDigestSize is how many items a low-energy digest carries.
// Half the human attention bound, by the same reading that puts LowEnergyMax
// at the midpoint of the [0,1] energy scale.
const LowEnergyDigestSize = focus.DefaultSize / 2 // = 3

// MaxDigestDeferrals is how many consecutive digests may hold one item back
// before it is carried regardless. Ruling 2: one deferral = one day.
const MaxDigestDeferrals = 3

type DigestItem struct {
	ID        string           // trigger id
	Candidate *focus.Candidate // nil for a trigger with no source unit
	Deferrals int              // digests that already held it back
}

// Carry splits pending items into what this digest delivers and what it holds.
func Carry(items []DigestItem, adjacency map[string]float64, lowEnergy bool, now time.Time) (carry, held []DigestItem)
```

`Carry`'s rule, in order:

1. `!lowEnergy` → carry everything.
2. `item.Deferrals >= MaxDigestDeferrals` → carry, **regardless of rank**. That is what
   "regardless" means, and it is carried *in addition to* the truncation, never inside it.
3. otherwise: rank every remaining item with `focus.Rank` and carry the top `LowEnergyDigestSize`.

**`LowEnergyDigestSize = focus.DefaultSize / 2` is written as that expression in Go, not as `3`.**
The derivation is then in the code rather than in a comment about the code, and a recalibration of
`focus_size` carries it. §13 documents the resulting value, `3`, which is what the calibration gate
compares (`go/constant` values, `calibration_doc_test.go:175-182`).

**The `pattern_based`-trigger case falls out of the arithmetic instead of needing a rule.**
Proposal §3.3 flagged it: `triggers.unit_id` is nullable, so a pattern watcher has no source unit
and no priority, and the gate must not nil-dereference. `Carry` enters such an item into
`focus.Rank` as the **zero `focus.Candidate` carrying only its own ID**. Every term of
`focus.Priority` is multiplicative in `e = weight.Effective(0, 0, …)` = 0, so its score is exactly
`0.0` and `Rank`'s three-level tie-break (`rank.go:29-35`) orders it last, deterministically, by
ID. **A "still on this goal, or shall we let it rest?" nudge is therefore the first thing a
depleted user stops being asked** — which is the behaviour doc 02 §7 asks for, reached with no
special case, no nil check inside the ranking, and no invented number.

**`MaxDigestDeferrals = 3` — chosen, inside a derived band, and it is the weakest derivation in
this design.** The band:

- **> 1**, or the care gate is a one-day delay wearing the word *anti-starvation*.
- **< `consolidation.LoadCooldownDays` (7)**. The load watcher will not re-open a state hypothesis
  for seven days after one resolves (`02:794-801`). If an item could be suppressed for the whole
  cooldown, a low-energy reading could silence a nudge across exactly the window in which the brain
  refuses to re-ask about the energy that is silencing it. The user would never get the chance to
  correct the state suppressing their own digest.

Within `(1, 7)`, 3 is the value the same halving reading picks. The relation
`MaxDigestDeferrals < consolidation.LoadCooldownDays` is asserted as an L1 test in `prospection`'s
own package (core may import core), following m2a's `ReviveGain × WeightCeiling > weight_threshold`
precedent: the number is chosen, the *relation* is pinned, so a later recalibration cannot silently
break the promise. **Owner-review item R2.**

#### §13 rows (PR 5)

| Knob | Default | Status |
|---|---|---|
| `digest_hour` (`internal/core/prospection.DigestHour`) | 7 — derived: the only hour at which the daily digest is both a morning digest and never born inside quiet hours | **new row**, derived |
| `low_energy_max` (`internal/core/prospection.LowEnergyMax`) | 0.5 — the midpoint of an uncalibrated [0,1] scale; strict comparison | **new row**, chosen |
| `energy_reading_max_age_hours` (`internal/core/prospection.EnergyReadingMaxAgeHours`) | 24 — one digest cycle; coincides with `incomplete_expiry_hours` by coincidence, not by relation, and no test ties them | **new row**, derived |
| `low_energy_digest_size` (`internal/core/prospection.LowEnergyDigestSize`) | 3 — declared as `focus.DefaultSize / 2` | **new row**, derived |
| `max_digest_deferrals` (`internal/core/prospection.MaxDigestDeferrals`) | 3 — chosen inside the band `1 < n < load_cooldown_days`; the upper bound is asserted | **new row**, chosen |

### 3.6 Recurrence — 29 February and day 31, decided

Doc 02 §7: `recurrence_rule` (`yearly` | `monthly`) + `recurrence_anchor` `{month, day}`, and on
firing "the next one is created automatically pointing at the SAME source unit" (I17).

```go
type Rule string

const (
	RuleYearly  Rule = "yearly"
	RuleMonthly Rule = "monthly"
)

// Anchor is doc 02 §7's recurrence_anchor. Month is ignored by RuleMonthly.
type Anchor struct {
	Month time.Month
	Day   int
}

// RecurrenceAnchorHour is the local wall clock an anniversary lands on.
const RecurrenceAnchorHour = 12

// NextOccurrence returns the first occurrence strictly after `after`, always
// re-derived from the anchor and never advanced from the previous occurrence.
func NextOccurrence(rule Rule, anchor Anchor, after time.Time) time.Time
```

**Two rules, and they answer R10 between them.**

**Rule 1 — clamp to the last day of the target month.** Never overflow, never skip.

| Case | Result | Why not the alternative |
|---|---|---|
| `yearly`, anchor `{Feb, 29}`, non-leap year | **28 Feb** | — |
| `yearly`, anchor `{Feb, 29}`, leap year | **29 Feb** | — |
| `monthly`, anchor day 31 | Jan 31 → Feb 28/29 → Mar 31 → Apr 30 → May 31 | — |
| Go's own `time.Date` normalisation | **Rejected**: `Feb 31` becomes `Mar 3`. A monthly reminder wanders forward and can skip a month entirely; a February birthday lands in March |
| "Skip months that lack the day" | **Rejected**: a day-31 monthly reminder would fire seven times a year and never in February |

**Rule 2 — always re-derive from the anchor, never advance from the previous occurrence.** This is
what makes rule 1 safe: advancing 29 Feb by one year gives 28 Feb, and advancing *that* gives 28
Feb forever — the anniversary silently drifts off its own date after one leap cycle, and the same
happens to a day-31 monthly reminder after its first February. Re-deriving gives an invariant worth
asserting directly, and it is I17's arithmetic half:

> **The anchor is idempotent.** Occurrence *N* computed from the anchor is the same instant however
> many times the trigger has re-armed, and re-arming is therefore a pure function of
> `(rule, anchor, now)` — never of the trigger's own history.

**`RecurrenceAnchorHour = 12` — derived, and midnight is rejected on evidence already in the
tree.** Doc 02 §5.1 turns a bare calendar date into local midnight (`02:567-572`), and the obvious
move is to reuse it. It is unsafe here, and `consolidation.NextDailyRun`'s own doc comment
(`schedule.go:66-112`) is why: a DST gap at local midnight normalises **backward** — Havana's
spring-forward maps local 00:00 to 23:00 *the previous evening*. An anniversary would then carry
the previous calendar date, be deferred as quiet-hours-adjacent, and nudge a day early, once a
year, in whichever zone puts its transition at midnight. Noon is off by eleven hours from every
transition of less than twelve, and the only known ≥12 h transitions delete the whole calendar date
(Pacific/Apia skipped 2011-12-30, Pacific/Kiritimati 1994-12-31 — both recorded in
`schedule.go:100-107`), where no instant on that date exists and forward normalisation is the only
available answer and the safe one.

The distinction is that §5.1's midnight rule optimises for *"the same vault in two zones classifies
identically"*, and recurrence optimises for *"the nudge lands on the right calendar day in the
user's own zone"*. Different criteria, different instants, both stated.

Construction follows `NextDailyRun`'s discipline verbatim: build with `time.Date` and out-of-range
day fields, **never** `AddDate` on a wall clock (`schedule.go:80-92` records both defects that
produced that rule).

#### §13 rows (PR 6)

| Knob | Default | Status |
|---|---|---|
| `recurrence_anchor_hour` (`internal/core/prospection.RecurrenceAnchorHour`) | 12 — local noon; midnight is rejected because a DST gap can normalise it onto the previous calendar date | **new row**, derived |

### 3.7 Arming — what a classification arms, and where the numbers come from

```go
// EventLeadDays is doc 02 §7's notification horizon.
const EventLeadDays = 7

type Armament string

const (
	ArmNothing   Armament = "nothing"
	ArmTimer     Armament = "timer"
	ArmTrigger   Armament = "trigger"
	ArmRecurring Armament = "recurring_trigger"
)

type Plan struct {
	What      Armament
	FireAt    time.Time
	LeadDays  int    // EventLeadDays for triggers, 0 for timers
	Rule      Rule   // "" unless What == ArmRecurring
	Anchor    Anchor // zero unless What == ArmRecurring
	Interrupt Interrupt
}

// Arm decides what one classification arms at one instant. It is the same
// shape classify.Kind.UnitType() already has: a decision about a value.
func Arm(c classify.Classification, now time.Time) (Plan, bool)
```

| `c.Kind` | Condition | Plan |
|---|---|---|
| `timer` | `c.DueAt != nil` and after `now` | `ArmTimer`, `FireAt = *c.DueAt` |
| `recurring_reminder` | `c.EventAt != nil` **and** `c.RecurrenceRule != nil` | `ArmRecurring`, anchor `{EventAt.Month(), EventAt.Day()}`, `FireAt = lead(NextOccurrence(rule, anchor, now))` |
| `recurring_reminder` | `c.EventAt != nil`, rule degraded | `ArmTrigger` for the dated occurrence — **the capture is honoured, the recurrence is not invented** |
| `event` | `c.EventAt != nil` and after `now` | `ArmTrigger`, `FireAt = lead(*c.EventAt)` |
| anything else, or undated, or dated in the past | — | `ArmNothing`, `false` |

`lead(t)` = `time.Date(y, m, d-EventLeadDays, hh, mm, ss, ns, loc)`, **clamped to be at or after
`now`**.

**Three decisions inside that table.**

1. **A `timer` carries its instant in `due_at`, never `event_at`** — I18 forbids interchanging
   them, so the choice must be stated rather than inferred. `event_at` is *when a thing happens in
   the world*, and a timer has no world event; `due_at` is *when this is owed*, and a timer is owed
   at its fire instant. There is no collision with `focus.UrgencyRamp`, which reads `due_at` on
   units, because **a timer is never a unit** (I04, doc 02 §8). The classify prompt states it, so
   PR 7 carries a `testdata/classify/format.md` delta alongside the doc 02 §5 step-5 amendment.
2. **The lead time is clamped, not offset.** An event captured two days before it happens would
   otherwise arm a trigger five days in the past and be expired by §3.3 on the very next pass. The
   lead time is a *notification horizon*, and the system is not late for an event it only just
   learned about. Arming at `now` is the honest reading.
3. **A dated event at or before `now` arms nothing.** Doc 02 §5.1 already refuses to arm on a
   degraded date (*"arming a trigger on a guessed date is worse than not arming one"*, `02:564`);
   arming a nudge for something already over is the same failure in a different direction.

`Arm` reads `EventAt` for events, `DueAt` for timers, and `CreatedAt` never — I18 as a property of
the function's own body, asserted in PR 7's test.

`Arm` also performs the one conversion §3.8 leaves to it: `c.RecurrenceRule` is a
`classify.RecurrenceRule`, and `Plan.Rule` is a `prospection.Rule`, so the mapping between the two
happens **here**, at the call site, because the legal import edge runs `prospection → classify` and
never back (§4). The mapping is total over what can arrive — classify has already degraded anything
outside `{yearly, monthly}` to `nil` — and its `nil` arm is the rule-degraded row of the table
above, which arms the dated occurrence as a one-shot trigger.

**`Arm` takes `classify.Classification`, not a local mirror of it.** The rejected alternative is a
`prospection.Armable` struct that `brain` fills from a `Classification`: it would be a second
source of truth for a shape doc 02 §5 already fixes, and it would drift the first time classify
gains a field. `prospection → classify` is a legal core-to-core edge (§1) and there is no cycle:
classify does not import prospection, and §3.4's degradation default deliberately lives in
`prospection` so that it stays that way.

#### §13 rows (PR 7)

| Knob | Default | Status |
|---|---|---|
| Event lead time (`internal/core/prospection.EventLeadDays`) | 7 | **row 914 amended** — gains a constant, value unchanged; row 899's note that `focus.UrgencyLeadDays` is a *separate* knob with the same value stays true and is now checkable on both ends |

### 3.8 The classify widening — ruling 1, and the second field it implies

```go
type Classification struct {
	// …
	InterruptLevel *float64        // doc 02 §7, [0,1]
	RecurrenceRule *RecurrenceRule // doc 02 §7's closed vocabulary — classify's own
}

// RecurrenceRule is doc 02 §7's recurrence vocabulary as classify decodes it.
// It is declared in classification.go beside the field that carries it — it is
// a capture field, not one of outcomes.go's six orthogonal resolutions — in
// exactly the shape outcomes.go uses for each of those six: a ~string type,
// its members, and an AllX() the decoder matches against. There is
// deliberately no ParseX; decodeEnum serves all of them (design D11 point 2).
type RecurrenceRule string

const (
	RecurrenceRuleYearly  RecurrenceRule = "yearly"
	RecurrenceRuleMonthly RecurrenceRule = "monthly"
)

// AllRecurrenceRules returns a fresh slice of the vocabulary, in doc 02's
// declared order.
func AllRecurrenceRules() []RecurrenceRule
```

Two `fieldSpecs` rows (`decode.go:33-49`), one assigner each. Both are optional, so their absence is
the ordinary case and is not reported (`decode.go:71-80`):

```go
{"interrupt_level", false, assignInterruptLevel},
{"recurrence_rule", false, assignEnum(AllRecurrenceRules,
	func(c *Classification, v *RecurrenceRule) { c.RecurrenceRule = v })},
```

`recurrence_rule` reuses `assignEnum` unchanged. `interrupt_level` needs its own assigner rather
than `assignFloat`, which returns only `ReasonWrongType`: the range check is the whole point of the
row.

**Why `RecurrenceRule` is classify's own type and not `*prospection.Rule`.** `Rule` is declared in
`internal/core/prospection` (§4, `recurrence.go`, PR 6), and §4's dependency map runs
`prospection → classify`, never back. A `classify` field typed `*prospection.Rule` would be that
reverse edge — the import cycle §4 states does not exist — and Go would refuse to compile the pair.
The two vocabularies carry the same two strings by construction, and **PR 7's `Arm` maps one onto
the other at its own call site** (§3.7), which is the legal direction. Nothing is lost across that
boundary: a `recurrence_rule` classify could not decode is already `nil` and already means "no
recurrence was claimed", which is exactly the row of §3.7's table that arms a one-shot trigger.

**`interrupt_level` out of `[0,1]` degrades with `ReasonBadFormat`.** The existing vocabulary is
five values (`classification.go:57-72`) and `ReasonBadFormat` already means *the JSON type was
right and the value is not one this field reads* — its doc comment names a date as the example, and
this widens the comment, not the vocabulary. The rejected alternative is a sixth `Reason`
(`ReasonOutOfRange`): it is a real distinction, but §9's learning loop has no separate use for it
today, and doc 02 §5.1's own justification for distinguishing reasons is that the loop *"should not
confuse them"* — a distinction with no consumer is a vocabulary addition smuggled in. **Owner-review
item R3.**

**`recurrence_rule` is a closed vocabulary, so it reuses `assignEnum` and degrades with
`ReasonUnknownEnum`** exactly as the six orthogonal fields do — which is also why it must be a
`classify`-side `~string` type: `assignEnum[T ~string]` matches against an `AllX()` of its own
package's members. Why it must exist at all: `monthly` has **no producer anywhere in the tree** —
the identical defect R1 records for `interrupt_level` —
and doc 02 §5.1 forbids the cheap alternative, because `structured_data` is *"opaque to the brain
and stays opaque"* (`02:562`). Deriving `yearly` from the kind would be inventing calibration in the
one place doc 02 says the model decides, which is `prior.go:9-19`'s refusal restated.

**Neither field is ever filled by a per-type table.** `prior.go`'s two-numbers-not-eighteen rule
applies verbatim.

Doc 02 deltas in PR 4: §5 step 1's field list, **§5.1's degradable-field table gains two rows**
(ruling 1 names one; the second is this design's addition), and `testdata/classify/format.md` plus
the golden corpus widen with them.

---

## 4. Package layout, and how `now` travels

```
internal/core/prospection/
├── doc.go          (exists — package comment unchanged; it already states this charter)
├── quiethours.go   QuietHoursStartHour, QuietHoursEndHour, InQuietHours, DeliverableFrom   PR 1
├── staleness.go    TriggerStalenessHours, TimerStalenessHours, DelayCaveatMinutes,
│                   Verdict, TriggerVerdict, TimerVerdict, DelayCaveat                      PR 2
├── delivery.go     PushThreshold, DefaultInterruptLevel, Interrupt, ResolveInterrupt,
│                   Route                                                                   PR 3
├── digest.go       DigestHour, LowEnergyMax, EnergyReadingMaxAgeHours,
│                   LowEnergyDigestSize, MaxDigestDeferrals, EnergyReading,
│                   DigestItem, DigestDue, LowEnergy, Carry                                 PR 5
├── recurrence.go   Rule, Anchor, RecurrenceAnchorHour, NextOccurrence                      PR 6
└── arm.go          EventLeadDays, Armament, Plan, Arm                                      PR 7
```

**Dependency map.** `prospection` imports `time`, `internal/core/classify` (PR 7) and
`internal/core/focus` (PR 5). It imports no `ports`, no `store`, no `providers`, nothing external —
`depguard`'s `core-purity` rule enforces it and `forbidigo` denies `time.Now`/`rand`/`os.Getenv` by
call pattern (§1). No cycle exists: `classify` and `focus` import neither `prospection` nor each
other's dependents.

**That edge is one-way, and §3.8 is where it would have been broken.** `classify.Classification`
declares its own `RecurrenceRule` vocabulary; typing that field `*prospection.Rule` instead would
make `classify` import `prospection` while `prospection` imports `classify` — the cycle this map
denies, and one Go rejects at compile time rather than at review. The conversion between the two
vocabularies belongs to `Arm` (§3.7), on the `prospection` side of the edge.

`m3a` PR 5 is **the first importer `internal/core/focus` has ever had outside its own package**
(§1). It discharges the *pure* half of M2's carry-over; the `UnitRepo` query and its `ORDER BY`
remain `m3b`'s (proposal §5.1's `feat/ports-store-focus-candidates`).

**How `now` travels.** Every function that needs the instant takes it as a trailing `now
time.Time`, and no function that does not need it takes one. `brain` reads `ports.Clock` **once**
per pass and hands the same instant to every gate — `brain_single_clock_read_test.go` requires it,
and proposal §4.5 already priced its one consequence (an item crossing 07:00 mid-pass is judged by
the pass's start instant, so the worst case is one extra tick of deferral). `NextOccurrence` takes
`after` rather than `now` because the value it needs is "the last occurrence or the arming instant,
whichever the caller means", and naming it `now` would hide that.

---

## 5. Data flow

```
                        classify.Classification            ports.Clock.Now()  ← the only clock read
                                  │                                 │
                                  ▼                                 │
   PR 7   prospection.Arm(c, now) ──────────────────────────────────┤
                                  │                                 │
                                  ▼                                 │
                          Plan{What, FireAt, Rule, Anchor, Interrupt}
                                  │                                 │
                    ══════════════╪═════ m3b persists ══════════════╪══════
                                  ▼                                 │
                        one pending trigger / timer                 │
                                  │                                 │
   PR 1+2  ─────────────► TriggerVerdict(fireAt, now) ◄─────────────┤
                          TimerVerdict(fireAt, now)                 │
                                  │                                 │
             ┌──────────┬─────────┼───────────┬──────────┐          │
             ▼          ▼         ▼           ▼          │          │
          Pending    Defer      Stale      Deliver       │          │
                    (quiet)   (I15/§8)        │          │          │
                                              ▼          │          │
   PR 3                          Interrupt.Route()       │          │
                                    │        │           │          │
                              RoutePush   RouteDigest    │          │
                                    │        │           │          │
   PR 5                             │        ▼           │          │
                                    │   DigestDue(last, now) ───────┤
                                    │        │                      │
                                    │   LowEnergy(reading, now) ◄───┘
                                    │        │
                                    │   Carry(items, adj, low, now) ──► focus.Rank
                                    │        │
                                    ▼        ▼
   PR 6                     DelayCaveat(overdue)      held → Deferrals+1
                                    │
                    ══════════ m3c/m3d publish ══════════
                                    │
                        NextOccurrence(rule, anchor, now) ──► the next armed trigger (I17)
```

Everything above the `m3b persists` line and below it is pure. `m3a` ships only the boxes; the
lines crossing the double rules are `m3b`, `m3c` and `m3d`.

---

## 6. File changes

| File | Action | What |
|---|---|---|
| `internal/core/prospection/quiethours.go` | Create | PR 1 — §3.1 |
| `internal/core/prospection/staleness.go` | Create | PR 2 — §3.3 |
| `internal/core/prospection/delivery.go` | Create | PR 3 — §3.4 |
| `internal/core/prospection/digest.go` | Create | PR 5 — §3.5 |
| `internal/core/prospection/recurrence.go` | Create | PR 6 — §3.6 |
| `internal/core/prospection/arm.go` | Create | PR 7 — §3.7 |
| `internal/core/prospection/*_test.go` | Create | L1, one per file above |
| `internal/core/classify/classification.go` | Modify | PR 4 — two fields, plus classify's own `RecurrenceRule` vocabulary (§3.8) |
| `internal/core/classify/decode.go` | Modify | PR 4 — two `fieldSpecs` rows, one range-checking assigner |
| `internal/core/classify/prompt.go` | Modify | PR 4 — the two fields; PR 7 — a timer's instant is `due_at` |
| `testdata/classify/format.md`, golden corpus | Modify | PR 4, PR 7 |
| `docs/02-cognitive-core.md` | Modify | §5, §5.1, §7, §13 — one delta per PR (§7 below) |
| `docs/06-harness.md` | Modify | PR 1 only — I16's row names the exception (§3.2) |
| `test/conformance/i15_trigger_expires_not_fires_test.go` | Create | PR 2 |
| `test/conformance/i16_quiet_hours_test.go` | Create | PR 1 |
| `test/conformance/i17_recurrence_same_unit_test.go` | Create | PR 6 |

**No migration. No `ports`. No `store`. No `brain`. No `store_api.golden` change.** `triggers` and
`timers` have existed since M0 and `m3a` writes to neither.

---

## 7. The seven PRs

Chain `stacked-to-main`, delivery `auto-chain`. Every PR: **the test commit precedes the
implementation commit** (proposal §6; `sdd-verify` reads the PR's `git log` and reports an inversion
as CRITICAL). Estimates are budgets against the 400-line implementation+docs ceiling, not
predictions — proposal §5.1 records this project's measured 1.3×–4.3× multipliers.

| # | Branch | Content | Doc 02 delta | §13 | Est. |
|---|---|---|---|---|---|
| 1 | `feat/core-prospection-quiet-hours` | `InQuietHours`, `DeliverableFrom`, 2 constants. **I16 pure half** | §7: quiet hours are evaluated before staleness and are absolute for triggers; **the exception is a timer**; resurfacing is the first pass at or after the end hour. Plus `06-harness.md:256` | row 913 **splits into two** | ~320 |
| 2 | `feat/core-prospection-staleness` | `Verdict`, `TriggerVerdict`, `TimerVerdict`, `DelayCaveat`, 3 constants. **I15 pure half** | §7 + ADR-0009 reference: staleness counts from the first deliverable instant; the caveat threshold | 920, 921 amended; +1 new | ~370 |
| 3 | `feat/core-prospection-delivery-split` | `Interrupt`, `ResolveInterrupt`, `Route`, 2 constants | §7: the degradation path, NULL ↔ degraded, tone exemption | 912 amended; +1 new | ~300 |
| 4 | `feat/classify-prospection-fields` | `InterruptLevel`, `RecurrenceRule`, 2 `fieldSpecs` rows, range check, prompt + golden corpus | §5 step 1's field list; **§5.1's table gains two rows** | — | ~380 |
| 5 | `feat/core-prospection-digest-gates` | `DigestDue`, `LowEnergy`, `Carry`, 5 constants; first `focus` importer | §7: cadence, the care gate's shape, anti-starvation, the unitless-trigger answer | +5 new | ~400 |
| 6 | `feat/core-prospection-recurrence` | `NextOccurrence`, `Rule`, `Anchor`, 1 constant. **I17 pure half** | §7: the clamp rule and anchor idempotence | +1 new | ~350 |
| 7 | `feat/core-prospection-arming` | `Arm`, `Plan`, 1 constant | §5 step 5 (M1 note retired for timers/triggers); §7: the lead-time clamp; a timer's instant is `due_at` | 914 amended | ~350 |

Seven PRs, ~2,470 budgeted lines. **Proposal §5.1 planned six at ~2,000**; the seventh is PR 4,
split out because ruling 1's classify widening and the arming decision are two reviewable units
(a decoder contract plus a golden corpus, and a decision table) that together clear 400 lines on
their own.

Order is linear and each PR builds: 1 → 2 (staleness needs `DeliverableFrom`), 3 independent,
4 → 7 (`Arm` reads both new fields), 6 → 7 (`Arm` calls `NextOccurrence`), 5 independent of 3/4.
Stacked in the order tabled.

**`m3c-telegram` unblocks at this document's review**, not at PR 7's merge (proposal §5): the shape
of the thing it will be asked to send is `Verdict` + `Route` + `DelayCaveat`, all fixed above.

---

## 8. Testing strategy

| Layer | What | Where |
|---|---|---|
| **L1** | Every function above, at its boundaries. Fake instants only — no test in `internal/core` may read a real clock (`nooma-testing` hard rule 2) | `internal/core/prospection/*_test.go` |
| **L1** | The three constant relations: `DigestHour >= QuietHoursEndHour`; `MaxDigestDeferrals < consolidation.LoadCooldownDays`; `LowEnergyDigestSize == focus.DefaultSize / 2` | same |
| **L2** | **I15** — a trigger past its staleness window yields `VerdictStale` and never `VerdictDeliver`, for every `fire_at` in the window | `test/conformance/i15_…` (PR 2) |
| **L2** | **I16** — no `TriggerVerdict` returns `VerdictDeliver` while `InQuietHours(now)`, swept across the whole window; and the timer exception is asserted as the *only* one | `test/conformance/i16_…` (PR 1) |
| **L2** | **I17**'s arithmetic half — anchor idempotence, and every occurrence carries the anchor's own month/day clamped, never the previous occurrence's | `test/conformance/i17_…` (PR 6) |
| **L2** | The calibration gate picks up 13 new/amended rows automatically (`calibration_doc_test.go`) — no new test, but `make check-all` is mandatory before every PR (R12) | existing |
| **L3 / L4** | **None.** `m3a` touches no SQLite, no binary, no adapter | — |

**Fixtures express boundaries as multiples of the constants, never as literals** — `focus`'s own
discipline (`priority.go:137-140`), so a recalibration needs no fixture edit.

**Two zone fixtures are mandatory, not optional**, and both are named here so they are not
discovered: `America/Havana` (spring-forward at local midnight, §3.6's rejection of midnight) and
`Pacific/Apia` (2011-12-30 does not exist). A third, an ordinary fixed-offset zone, is the control.

**The `internal/core` coverage floor (≥90 %) bites `m3a` hardest** of the four slices (R12), and
`make check` does not run `scripts/core-coverage.sh`. `make check-all` before every PR,
structurally.

---

## 9. Threat matrix

**N/A** — `m3a` introduces no routing, no shell command, no subprocess, no VCS/PR automation, no
executable-file classification and no process integration. It is seven pure Go files and a decoder
table row. The boundaries that would need the matrix (`getUpdates` polling, `allowed_chat_ids`
enforcement, the `bot_token_env` read) are `m3c`'s, and proposal §9 R5/R7/R8 already names them for
that change.

---

## 10. Migration / rollout

**No migration.** `triggers` and `timers` are unchanged since `0001_core_tables.sql` (M0), `m3a`
writes no rows, and `testdata/schema/store_api.golden` is untouched. No feature flag: nothing in
`m3a` is reachable from a running binary until `m3b` calls it. **Rollback is deleting a package
nothing imports.**

---

## 11. Owner-review items and open questions

Numbered so `sdd-tasks` and the owner can answer them by reference. None blocks `sdd-tasks`; each
has a decided default that ships if the owner is silent.

| # | Item | Decided default | What a different answer costs |
|---|---|---|---|
| **R1** | **I16's push exception is the timer, not a threshold** (§3.2). This defines a promise `docs/06-harness.md:256` and proposal §2 both make and neither defines, and it means a `interrupt_level = 0.95` trigger still waits until 07:00 | Timer-only exception; doc 02 §7 and harness I16 amended in PR 1 | Option A (no exception at all) is a one-line change and a harness-row edit. Option B (a second threshold) is rejected on ADR-0009's own grounds and would need a superseding argument, not a preference |
| **R2** | **`MaxDigestDeferrals = 3`** (§3.5) is the weakest derivation in this design: the band `1 < n < load_cooldown_days` is derived, the value inside it is chosen | 3 | Any value in `(1, 7)` is a one-constant change plus its §13 row. The asserted upper-bound relation is what must not move |
| **R3** | **Out-of-range `interrupt_level` reuses `ReasonBadFormat`** rather than adding a sixth `Reason` (§3.8) | Reuse, widening the doc comment | A sixth reason is ~15 lines plus a doc 02 §5.1 delta, and is the better answer the day §9's learning loop distinguishes them |
| **R4** | `DelayCaveatMinutes >= 3 × the default proactive_check period` **cannot be asserted in `m3a`** — `internal/config/defaults.go` declares no schedule default (§1) | The relation is documented in PR 2 and asserted as an L2 test in `m3d` #1 | Adding the config default in `m3a` would pull `internal/config` into a pure slice |
| **R5** | **Classify gains `recurrence_rule`** as well as `interrupt_level` (§3.8), widening ruling 1 by one field | Ships in PR 4 | Declining it means `monthly` has no producer and `m3a` #7 arms every `recurring_reminder` as a one-shot trigger — R1 repeated for a second field |
| **Q1** | Should a digest that carries *nothing* (every item held) be published at all, or is silence the delivery? `Carry` returns two slices and takes no position; the choice is `m3d`'s digest assembly | Not decided here — recorded so `m3d`'s design does not have to rediscover it | — |
| **Q2** | Doc 02 §7's lead time is "policy per event class", and `m3a` ships one horizon for every event. Migrating it to a self-model preference is doc 02's own deferred decision (`02:766-767`) | One constant, deferred unchanged | — |

---

## 12. Risks this design adds or sharpens

| # | Risk | Mitigation |
|---|---|---|
| A | **The 7-hour quiet window versus the 6-hour staleness gate** (§3.3). Without `DeliverableFrom`, an entire hour of the calendar silently expires every night. It is invisible in both §13 rows and in every prose reading of ADR-0009 | `DeliverableFrom`, plus the L2 sweep asserting *no* trigger armed inside quiet hours is stale at the end hour — written as a property over the whole window, not as three examples |
| B | **`m3a` names 13 constants where proposal §4.3 anticipated 5.** §13 grows by 9 rows | Every row derived or explicitly marked chosen in §3; the calibration gate's `calibrationMinSymbols` is a floor (`calibration_doc_test.go:29`) and cannot fail on additions |
| C | **PR 5 makes `prospection` depend on `focus`**, a package with no importers until now. A `focus` recalibration now moves the digest | Deliberate and stated: it is ruling 4's own instruction. The coupling is exactly one call (`focus.Rank`) and one derived constant, both asserted |
| D | **`docs-sync` fires only once a PR is open** (proposal R13), and all seven PRs touch `internal/core/**` | Every PR in §7 has a genuine doc 02 delta. No `m3a` PR should need `no-spec-change` |
| E | **Recurrence tests that use the real calendar rot.** A fixture written "next 29 Feb" passes differently in 2027 and 2028 | Every recurrence fixture fixes both the anchor and the `after` instant explicitly; no test computes a year from anything but a literal |

---

## Reconciliation note — 2026-08-19

This document and `spec.md` were written concurrently and never read each other. `tasks.md`,
written last against both, recorded five disagreements as Findings F1–F5. Each was reconciled
against those recorded resolutions on 2026-08-19; `spec.md` carries the mirror note.

- **F1 — staleness formula.** This document's `DeliverableFrom`-based measurement (§3.3) stands; it
  is derived and it closes Risk A. `spec.md`'s R1.1, which measured `overdue` from `fireAt`
  directly, was corrected to it. Nothing changed here.
- **F2 — function names.** `TriggerVerdict`/`TimerVerdict`, `ResolveInterrupt`/`Interrupt.Route`
  and `Arm` (§3.3, §3.4, §3.7) stand; `spec.md` was aligned to them, reasoning included. Nothing
  changed here.
- **F3 — `classify.Classification.RecurrenceRule` could not compile.** §3.8 typed it `*Rule`, a
  `prospection` type, while §4's dependency map runs `prospection → classify`. **Corrected here**:
  §3.8 now declares a `classify`-local `RecurrenceRule` vocabulary following the package's own
  `assignEnum`/`AllX()` pattern, states in one sentence why the field is not typed across the
  boundary, and hands the conversion to `Arm` at its PR 7 call site; §3.7 and §4 now say so
  explicitly, and §6's file table names the added vocabulary.
- **F4 — `Carry`.** This document's whole-list composition (§3.5) stands; `spec.md`'s two
  independent per-candidate predicates were merged into it, both requirements kept observable.
  Nothing changed here.
- **F5 — lead time.** No real conflict: `lead()` stays unexported and clamped inside `arm.go`
  (§3.7), and `spec.md`'s R5.2 now states the exported unclamped function and this clamp as two
  layers PR 7 ships together. Nothing changed here.

## Reconciliation note — 2026-08-20

A second pass over the same pair surfaced two further disagreements. Both are recorded rather than
fixed silently.

- **F6 — the delay-caveat boundary direction. Corrected here.** §3.3's `DelayCaveat` returned
  `overdue > DelayCaveatMinutes*time.Minute`, so a delivery exactly at the threshold carried no
  caveat, while `spec.md` R1.3's MUST reads *"at or above it, one is"*. The comparison is now
  inclusive (`>=`). The argument is asymmetric cost: an unnecessary caveat costs the user one
  clause of politeness, while a missing caveat on a genuinely late delivery is precisely the
  out-of-context nudge ADR-0009 exists to prevent. When the two errors cost different amounts the
  boundary belongs on the cheap side, which is also doc 02 §7's own convention for the push
  threshold (`interrupt_level >= 0.70`, inclusive).
  **The contrast with staleness is deliberate and must not be "harmonized".** `verdict`'s staleness
  comparison stays strictly `>`: there the inclusive side is the destructive one — at exactly the
  window the item has not yet passed it, and expiring is unrecoverable while delivering is not.
  Same principle, opposite direction.
- **F7 — `internal/core/classify`'s place in `m3a`.** §5's scope line and §7's PR 4 were already
  correct; `spec.md` still carried a "not in scope" statement about the package, written before
  this document placed the work. Corrected in `spec.md`. Nothing changed here.
