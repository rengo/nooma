# Tasks — M3 Phase A: prospection (pure)

Implementation task list for `m3a-prospection`, derived from `spec.md` (R0–R6.1) and `design.md`
(§1–§12), both read in full before this document. Design §7 fixes the slicing — **seven PRs**,
stacked, ~2,470 budgeted impl+docs lines — and is treated as authoritative over spec's own PR
numbering and function names where the two disagree; every disagreement found is recorded below
as a Finding (F1–F5), per this project's own `m2b`/`m2d` precedent ("report, don't paper over"),
not silently resolved.

Chain strategy **`stacked-to-main`**, delivery strategy **`auto-chain`** (design §7, matching
`m1`/`m2`'s own precedent). Seven PRs in the linear order design tables them: `1 → 2` (staleness
needs `DeliverableFrom`); `3` independent; `4 → 7` (`Arm` reads both new classify fields);
`6 → 7` (`Arm` calls `NextOccurrence`); `5` independent of `3`/`4`.

**Strict TDD is active.** Every behavioral task states the two-commit shape this repo's own
precedent established: **commit 1** is the test plus a stub with the final signature returning
zero values (red for the right reason, C14-guarded — a zero-value stub must fail on a
length/presence assertion before any content check); **commit 2** is the implementation (green).
Where a genuine red is structurally impossible (a constant-relation check whose both operands
already exist), this document says so explicitly, per `m2a` C9's convention.

Every PR runs `make check-all` before opening — L3, the `internal/core` coverage floor (R12,
which bites `m3a` hardest per design §8), the cross-compile matrix, L4. `docs-sync.yml` fires on
`internal/core/`; **every one of these seven PRs touches it**, and each carries a genuine doc 02
delta below — none should need `no-spec-change`.

---

## Findings — spec/design disagreements (report, don't paper over)

**F1 — R1.1/R1.2's staleness formula is naive; design's is not, and this document follows
design.** Spec states `overdue = now.Sub(fireAt)` directly. Design §3.3 shows this formula
silently expires every trigger armed between 00:00 and 01:00, every night, because the 7-hour
quiet window is longer than the 6-hour staleness threshold — invisible in either §13 row read
alone. Design's `TriggerVerdict` computes overdue from `DeliverableFrom(fireAt)` instead, with a
worked six-row boundary table and the property "no trigger armed anywhere inside quiet hours is
ever Stale at the end hour." **Resolved in design's favor** — it is derived, not asserted, and
directly targets design's own Risk A. Tasks 2.1–2.2 test the corrected formula, not spec's literal
text; flagged for owner awareness since it changes what spec's stated MUST clauses read as.

**F2 — function names differ throughout.** `EvaluateTriggerStaleness`/`EvaluateTimerStaleness` →
`TriggerVerdict`/`TimerVerdict`; `ResolveInterruptLevel`/`DecideDelivery` → `ResolveInterrupt`/
`Interrupt.Route()`; `DecideArming` → `Arm`. Design's shapes are also structurally stronger in one
case (F2 continued in the delivery-split PR): an opaque `Interrupt` type whose `degraded` flag
survives a future `PushThreshold` recalibration, versus spec's bare-float `DecideDelivery`, which
any caller could bypass. **Resolved in design's favor** per the explicit instruction that design
owns the slicing; tasks use design's exact signatures, the traceability table below maps every
task back to its spec requirement ID regardless of the name change.

**F3 — `classify.Classification.RecurrenceRule *Rule` cannot type-check as written.** Design §3.8
declares this field inside `classify.Classification`, but `Rule` is declared in
`internal/core/prospection/recurrence.go` (PR 6). Design §4's own dependency map states
"`classify` does not import `prospection`... there is no cycle" — a `classify` field literally
typed `*prospection.Rule` would create exactly that cycle. **Not resolved by this document**: task
4.3 flags it explicitly and requires confirming classify's existing closed-enum field pattern
(mirroring the six orthogonal fields already decoded, e.g. `Kind`) before naming the stub type —
`RecurrenceRule` almost certainly needs a package-local `classify` type, decoded independently,
with PR 7's `Arm` doing the string-based conversion into `prospection.Rule` at its own call site
(the legal direction: `prospection` imports `classify`, never the reverse).

**F4 — design's `Carry` merges two spec requirements into one function.** Spec's R4.2 (the
low-energy importance gate) and R4.3 (anti-starvation, a standalone deferral-count predicate) are
two independent single-candidate functions in spec. Design's `Carry` (§3.5) is one function over
the whole item list, whose three-step rule (`!lowEnergy` → all; `Deferrals >= MaxDigestDeferrals`
→ carry regardless; else rank and truncate) subsumes both. **Resolved in design's favor** — the
composition is what makes "regardless of rank" and "in addition to the truncation, never inside
it" (design's own wording) provable as one property instead of two functions whose composition a
caller could get wrong. Task 5.6 tests every spec R4.2/R4.3 scenario as a case inside `Carry`'s own
table.

**F5 — spec R5.2 wants an exported, unclamped lead-time function; design's `lead(t)` is an
unexported, now-clamped helper inside `arm.go`.** Spec's scenario explicitly requires *"no clamping
at this layer"* and independent testability. Design's `lead()` clamps to `now` because an event
captured two days before it happens must not arm five days in the past. **Reconciled, not in
conflict, once read as two layers**: task 7.1–7.2 ship an exported, unclamped lead-time function
satisfying spec R5.2 literally; task 7.3–7.4's `Arm` calls it and applies design's own now-clamp
at Arm's call site, satisfying design's table row 3 ("A dated event... arms nothing" / the
already-past clamp). Neither requirement is weakened; they compose.

---

## Owner-review items carried forward (design §11 — decided defaults, ship if the owner is silent)

| # | Item | PR | Task |
|---|---|---|---|
| R1 | I16's push exception is the timer, not a threshold | PR 1 | 1.6, 1.7 |
| R2 | `MaxDigestDeferrals = 3`, weakest derivation in the design | PR 5 | 5.8, 5.11 |
| R3 | Out-of-range `interrupt_level` reuses `ReasonBadFormat` | PR 4 | 4.1, 4.2, 4.8 |
| R4 | `DelayCaveatMinutes >= 3×` tick — relation deferred to `m3d` #1 (no schedule default exists) | PR 2 | 2.9 (documents the deferral) |
| R5 | Classify gains `recurrence_rule` as well as `interrupt_level` | PR 4 | 4.3, 4.4, 4.8 |

---

## PR 1 — `feat/core-prospection-quiet-hours` (~320 impl+docs)

Depends on nothing outside this change. Ships `quiethours.go` — `InQuietHours`, `DeliverableFrom`,
2 constants. I16 pure half.

- [x] **1.1** Commit 1 (RED): `internal/core/prospection/quiethours_test.go` — `InQuietHours`
      boundary table: `00:00:00`→true, `06:59:59`→true, `07:00:00`→false, `23:59:59`→false; a
      fixed non-UTC `Location` test double proving two `time.Time` values denoting the same
      instant but carrying different `Location`s (one reads `06:30` local, one reads `08:30`)
      judge differently — the zone travels with the instant, never a global clock (spec R2.1's own
      scenario).
      **Red**: `undefined: prospection.QuietHoursStartHour/EndHour`, `undefined:
      prospection.InQuietHours`.
      Stub: the two consts (0, 7); `func InQuietHours(now time.Time) bool { return false }` —
      compiles; the `00:00:00` case expects `true`, fails first.
      Requirement: R2.1.
- [x] **1.2** Commit 2 (GREEN): implement `InQuietHours` via `now.Hour()` in `now.Location()`.
      Verify: `go test ./internal/core/prospection/...`.
      Requirement: R2.1; design §3.1.
- [x] **1.3** Commit 1 (RED): `quiethours_test.go` (continued) — `DeliverableFrom`: `t` outside
      quiet hours → `t` unchanged; `t` inside quiet hours → that day's `QuietHoursEndHour` instant,
      same `Location`; `t == QuietHoursEndHour` exactly → `t` unchanged (end is exclusive, matching
      `InQuietHours`'s own boundary).
      **Red**: `undefined: prospection.DeliverableFrom`.
      Stub: `func DeliverableFrom(t time.Time) time.Time { return t }` — compiles; the
      inside-quiet-hours case expects a shifted instant, fails first.
      Requirement: design §3.1/§3.3 (Risk A's mitigation) — flagged: spec's own R1.1 does not name
      this function; see Finding F1.
- [x] **1.4** Commit 2 (GREEN): implement `DeliverableFrom` via `time.Date` with out-of-range
      fields, never `AddDate` (house pattern, `consolidation.NextDailyRun`'s own discipline).
      Verify: `go test ./internal/core/prospection/... -run DeliverableFrom`.
      Requirement: design §3.1/§3.3.
- [x] **1.5** `test/conformance/i16_quiet_hours_test.go` (new) — sweep the whole `[00:00, 24:00)`
      window in a fixed `Location`, asserting `InQuietHours` against the boundary table; assert
      `InQuietHours` takes no "kind" parameter — it structurally cannot special-case a timer even
      if a caller wanted it to (this PR's own half of "the timer is the only exception"; the real
      composed proof is PR 2's `TimerVerdict` test, cross-referenced here).
      **Not a missing-symbol red**: `InQuietHours` already compiles and passes (task 1.2) —
      disclosed per `m2a` C9.
      Requirement: R2.1 (I16 row, `docs/06-harness.md` §4).
- [x] **1.6** `docs/02-cognitive-core.md` §7 amendment: state the timer exception explicitly in the
      push bullet — an explicit user instruction (a timer) outranks the quiet-hours policy window;
      an inferred trigger does not, and is always deferred. Owner-review **R1**.
      Requirement: design §3.2 (decision C).
- [x] **1.7** `docs/06-harness.md:256` amendment: I16's row gains the four words naming the
      exception.
      Requirement: design §3.2; **R1**.
- [x] **1.8** §13: row 913 (`Quiet hours`) **splits into two** — `quiet_hours_start_hour` (0,
      `prospection.QuietHoursStartHour`) and `quiet_hours_end_hour` (7,
      `prospection.QuietHoursEndHour`) — because a Default cell starting with `[` fails the gate's
      anchored numeric parse.
      Requirement: R0; design §3.1.
- [x] **1.9** Purity/lint: `golangci-lint run` (`core-purity` — imports only `time` beyond stdlib;
      `forbidigo` — no `time.Now`/`rand.*`/`os.Getenv`).
      Requirement: `nooma-core` hard rules 1–2.
- [x] Verify (PR-level): `make check-all`; confirm diff touches only
      `internal/core/prospection/quiethours{,_test}.go`, `test/conformance/i16_quiet_hours_test.go`,
      `docs/02-cognitive-core.md`, `docs/06-harness.md`. Target ≤320 impl+docs lines.

---

## PR 2 — `feat/core-prospection-staleness` (~370 impl+docs)

Depends on PR 1 (`DeliverableFrom`). Ships `staleness.go` — `Verdict`, `TriggerVerdict`,
`TimerVerdict`, `DelayCaveat`, 3 constants. I15 pure half. See **Finding F1**: the formula tested
here is design's `DeliverableFrom`-based one, not spec's literal `now.Sub(fireAt)`.

- [x] **2.1** Commit 1 (RED): `internal/core/prospection/staleness_test.go` — `TriggerVerdict`
      exercised through design §3.3's own worked six-row boundary table (`00:30`/pass `03:00` →
      Defer; `00:30`/pass `07:00` → Deliver, overdue 0; `00:00`/pass `07:00` → Deliver; `20:00`/pass
      next-day `10:00` → Stale; `23:30`/pass next-day `07:00` → Stale, 7.5h overdue; `06:00`/pass
      `06:05` → Defer, then pass `07:00` → Deliver, overdue 0); not-yet-due → Pending;
      `overdue == TriggerStalenessHours` exactly → Deliver (strict `>` only); the property "no
      trigger armed anywhere in `[QuietHoursStartHour, QuietHoursEndHour)` is ever Stale at the end
      hour," swept across the whole window.
      **Red**: `undefined: prospection.Verdict`, the four `Verdict*` constants, `undefined:
      prospection.TriggerVerdict`.
      Stub: `type Verdict string`; the four consts; `func TriggerVerdict(fireAt, now time.Time)
      Verdict { return "" }` — compiles; the not-yet-due case expects `VerdictPending`, fails
      first.
      Requirement: R1.1, resolved per **Finding F1**; I16-ordering (quiet hours evaluated before
      staleness).
- [x] **2.2** Commit 2 (GREEN): implement `TriggerVerdict` via an unexported
      `verdict(fireAt, from, stalenessHours, now)` helper — `from = DeliverableFrom(fireAt)`; order:
      Pending → (trigger-only) Defer via `InQuietHours(now)` → Stale (strict `>`) → Deliver.
      Verify: `go test ./internal/core/prospection/...`.
      Requirement: R1.1; design §3.3.
- [x] **2.3** Commit 1 (RED): `staleness_test.go` (continued) — `TimerVerdict`: not-yet-due →
      Pending; `fireAt` 2h before `now`, `TimerStalenessHours = 3` → Deliver (spec R1.2's own
      scenario); Deliver even while `InQuietHours(now)` is true — the direct proof of "the timer is
      the only push exception" (PR 1 task 1.5's forward reference).
      **Red**: `undefined: prospection.TimerVerdict`.
      Stub: `func TimerVerdict(fireAt, now time.Time) Verdict { return "" }` — compiles; the
      Deliver-inside-quiet-hours case fails first.
      Requirement: R1.2, resolved per **Finding F2** (`TimerVerdict` shares `Verdict` rather than a
      separate Deliver/Cancel pair — `brain` maps `VerdictStale` to `cancelled`, per design §3.3).
- [x] **2.4** Commit 2 (GREEN): implement `TimerVerdict` via the same helper, `from = fireAt`
      directly — no `DeliverableFrom` shift, no quiet-hours check.
      Verify: `go test ./internal/core/prospection/...`.
      Requirement: R1.2; design §3.3.
- [x] **2.5** Commit 1 (RED): `staleness_test.go` (continued) — `DelayCaveat`: overdue far below
      `DelayCaveatMinutes` → false (spec R1.3's "a few seconds late" scenario); `overdue ==
      DelayCaveatMinutes` exactly → **true** (inclusive `>=` — Finding F6); one minute above → true.
      **Red**: `undefined: prospection.DelayCaveatMinutes`, `undefined: prospection.DelayCaveat`.
      Stub: `const DelayCaveatMinutes = 15`; `func DelayCaveat(overdue time.Duration) bool { return
      true }` — compiles; the below-threshold case expects `false`, fails first.
      Requirement: R1.3.
- [x] **2.6** Commit 2 (GREEN): implement `DelayCaveat` — `overdue >= DelayCaveatMinutes*time.Minute`.
      Verify: `go test ./internal/core/prospection/...`.
      Requirement: R1.3; design §3.3.
- [x] **2.7** `test/conformance/i15_trigger_expires_not_fires_test.go` (new) — for every `fireAt`
      swept across the staleness window, `TriggerVerdict` never returns `VerdictDeliver` once
      genuinely past the window, and core returns `VerdictStale`, never a status the schema knows
      (design §3.3's own I12 note).
      Requirement: R1.1 (I15 row, `docs/06-harness.md` §4).
- [x] **2.8** `docs/02-cognitive-core.md` §7 + ADR-0009 cross-reference amendment: staleness counts
      from the first deliverable instant (`DeliverableFrom`), not from `fire_at` directly, and why
      (the 7h-quiet/6h-staleness interaction); the delay-caveat threshold and its three-tick
      derivation.
      Requirement: design §3.3 (Risk A's doc-facing half).
- [x] **2.9** §13: row 920 (`trigger_staleness_hours`) amended with `prospection.
      TriggerStalenessHours`; row 921 (`timer_staleness_hours`) amended with `prospection.
      TimerStalenessHours`; new row `delay_caveat_minutes` (15, chosen — three shipped
      `proactive_check` ticks). Note **R4**: the `DelayCaveatMinutes >= 3×` tick relation cannot be
      asserted here — `internal/config/defaults.go` declares no schedule default — documented as
      deferred to `m3d` #1 in the row's own comment.
      Requirement: R0; design §3.3.
- [x] **2.10** Purity/lint: `golangci-lint run`.
      Requirement: `nooma-core` hard rules 1–2.
- [x] Verify (PR-level): `make check-all`; confirm diff touches only
      `internal/core/prospection/staleness{,_test}.go`,
      `test/conformance/i15_trigger_expires_not_fires_test.go`, `docs/02-cognitive-core.md`. Target
      ≤370 impl+docs lines — measured 143 (124 `staleness.go` + 19 `docs/02-cognitive-core.md`), no
      split needed. **If the boundary table plus the property sweep run long**, split at
      `TriggerVerdict` (the trigger-side, `DeliverableFrom`-composed half) | `TimerVerdict` +
      `DelayCaveat` (the timer-only half, structurally independent of the trigger's own shift) —
      not pre-drawn by design, report before splitting.

---

## PR 3 — `feat/core-prospection-delivery-split` (~300 impl+docs)

Independent of PR 1/2/4. Ships `delivery.go` — `Interrupt`, `ResolveInterrupt`, `Route`, 2
constants. See **Finding F2**: this PR ships design's opaque `Interrupt` type, not spec's bare
`ResolveInterruptLevel`/`DecideDelivery` pair.

- [x] **3.1** Commit 1 (RED): `internal/core/prospection/delivery_test.go` — `ResolveInterrupt`:
      `nil` → `{DefaultInterruptLevel, degraded: true}`; `NaN`/`+Inf`/`-Inf`/`-0.1`/`1.1` → the same
      degraded default (five shapes, individually — spec R3.1's own enumeration); an in-range value
      passes through non-degraded; the zero value `Interrupt{}` also reports `Degraded() == true`
      (design's "forgotten initialisation is safe" property) — ordered so the `nil` case (expects
      degraded) runs before any coincidentally-matching zero-value case.
      **Red**: `undefined: prospection.DefaultInterruptLevel`, `undefined: prospection.Interrupt`,
      `undefined: prospection.ResolveInterrupt`.
      Stub: `const DefaultInterruptLevel = 0.0`; `type Interrupt struct{ level float64; degraded
      bool }`; `func ResolveInterrupt(level *float64) Interrupt { return Interrupt{} }` — compiles;
      the `nil` case fails first (C14 guard).
      Requirement: R3.1, resolved per **Finding F2**.
- [x] **3.2** Commit 2 (GREEN): implement `ResolveInterrupt` — `nil`, non-finite, or outside
      `[0,1]` → `{DefaultInterruptLevel, true}`; else → `{level, false}`.
      Verify: `go test ./internal/core/prospection/...`.
      Requirement: R3.1; design §3.4.
- [x] **3.3** Commit 1 (RED): `delivery_test.go` (continued) — `Route()`: a degraded `Interrupt` →
      `RouteDigest` regardless of `level`, even a corrupt level far above `PushThreshold`; `level ==
      PushThreshold` exactly → `RoutePush` (spec R3.2's inclusive boundary); one ulp below →
      `RouteDigest`; `1.0` → `RoutePush`; the composed scenario `ResolveInterrupt(nil).Route()` →
      `RouteDigest`, spec R3.2's own scenario, proven end to end.
      **Red**: `undefined: prospection.RoutePush/RouteDigest`, `undefined: (Interrupt).Route`.
      Stub: the two `Route` consts; `func (i Interrupt) Route() Route { return RoutePush }` —
      compiles; the degraded-always-digest case fails first.
      Requirement: R3.2.
- [x] **3.4** Commit 2 (GREEN): implement `Route()` — degraded short-circuit first, then `level >=
      PushThreshold`.
      Verify: `go test ./internal/core/prospection/...`.
      Requirement: R3.2; design §3.4.
- [x] **3.5** `delivery_test.go` (continued) — `Level()`/`Degraded()` accessor round-trip for every
      constructed `Interrupt`; an in-package structural assertion that `Interrupt`'s fields are
      unexported, so no caller outside this package can construct a non-degraded `Interrupt` with an
      out-of-range level.
      **Not a missing-symbol red**: `Level`/`Degraded` already compile (task 3.2) — disclosed per
      `m2a` C9.
      Requirement: design §3.4 (the type-safety argument).
- [x] **3.6** `docs/02-cognitive-core.md` §7 amendment: the degradation path (a degraded
      classification never produces a push); the NULL↔degraded round trip `brain` must persist
      (`m3a`'s contract to state, `m3b`'s to implement); the tone exemption (`Route() == RoutePush`
      is the "urgent push is NOT softened" exemption).
      Requirement: design §3.4.
- [x] **3.7** §13: row 912 (`Push threshold`) amended with `prospection.PushThreshold`; new row
      `default_interrupt_level` (0.0, chosen — "no claim was made," behaviourally inert below the
      push threshold).
      Requirement: R0; design §3.4.
- [x] **3.8** Purity/lint: `golangci-lint run`.
      Requirement: `nooma-core` hard rules 1–2.
- [x] Verify (PR-level): `make check-all`; confirm diff touches only
      `internal/core/prospection/delivery{,_test}.go`, `docs/02-cognitive-core.md`. Target ≤300
      impl+docs lines.

---

## PR 4 — `feat/classify-prospection-fields` (~380 impl+docs)

Touches `internal/core/classify`, not `prospection`. Independent of PR 1–3; feeds PR 7. See
**Finding F3**: `RecurrenceRule`'s exact classify-side type is not fully specified by design and
must be confirmed against classify's existing closed-enum pattern before task 4.3's stub is named.

- [x] **4.1** Commit 1 (RED): `internal/core/classify/classification_test.go` (extend) —
      `InterruptLevel`: absent → `nil`, no `Reason` (the six existing fields' own "absence is
      ordinary" pattern); present, valid `[0,1]` → the float; present, out of `[0,1]` → `nil` +
      `ReasonBadFormat` (owner-review **R3**: reuse, not a new `Reason`).
      **Red**: the fixture references `Classification.InterruptLevel`, which does not exist —
      package fails to compile.
      Stub: add `InterruptLevel *float64` to `Classification`; no `fieldSpecs` row yet — compiles;
      the out-of-range fixture expects `ReasonBadFormat`, the decoder never reads the field, no
      `Reason` is ever emitted, fails first.
      Requirement: design §3.8; **R3**.
- [x] **4.2** Commit 2 (GREEN): `decode.go` — add the `fieldSpecs` row for `interrupt_level` with a
      range-checking assigner (JSON number → `*float64`; non-finite or outside `[0,1]` → `nil` +
      append `ReasonBadFormat`).
      Verify: `go test ./internal/core/classify/...`.
      Requirement: design §3.8; **R3**.
- [x] **4.3** Commit 1 (RED): `classification_test.go` (continued) — `RecurrenceRule`: absent →
      `nil`, no `Reason`; `"yearly"`/`"monthly"` → the decoded value; unknown text (`"weekly"`) →
      `nil` + `ReasonUnknownEnum`, matching the six orthogonal enum fields' own degradation.
      **Before writing the stub**: confirm classify's existing closed-enum field pattern (e.g. how
      `Kind` or another orthogonal field is typed) and name `RecurrenceRule`'s type accordingly — a
      package-local `classify` type, never `*prospection.Rule` (**Finding F3** — that would create
      the import cycle design §4 says does not exist).
      **Red**: the fixture references the not-yet-declared field/type.
      Stub: add the field per the confirmed pattern; no `fieldSpecs` row yet — compiles; the
      `"yearly"` case fails first.
      Requirement: design §3.8; **R5**.
- [x] **4.4** Commit 2 (GREEN): `decode.go` — add the `fieldSpecs` row for `recurrence_rule`,
      reusing `assignEnum` against `{"yearly", "monthly"}`.
      Verify: `go test ./internal/core/classify/...`.
      Requirement: design §3.8; **R5**.
- [x] **4.5** `prompt.go`: widen the field list to include `interrupt_level` and `recurrence_rule`
      (§5 step 1's list); state doc 02 §7's guidance for the model verbatim (`interrupt_level ∈
      [0,1]`; `recurrence_rule` closed vocabulary).
      Requirement: design §3.8.
- [x] **4.6** `testdata/classify/format.md` + golden corpus: widen with the two new fields; extend
      fixtures covering present, absent, out-of-range, and unknown-enum for both.
      Verify: `go test ./internal/core/classify/... -run Golden` (or the suite's actual golden-test
      name).
      Requirement: design §3.8.
- [x] **4.7** `docs/02-cognitive-core.md` §5 step 1 amendment: add `interrupt_level` and
      `recurrence_rule` to the enumerated orthogonal fields.
      Requirement: design §3.8.
- [x] **4.8** `docs/02-cognitive-core.md` §5.1's degradable-field table: two new rows —
      `interrupt_level` (degrades via `ReasonBadFormat`, widening its doc comment per **R3**) and
      `recurrence_rule` (degrades via `ReasonUnknownEnum`, per **R5**).
      Requirement: design §3.8; **R3**, **R5**.
- [x] **4.9** Purity/lint: `golangci-lint run`.
      Requirement: `nooma-core` hard rules 1–2.
- [x] Verify (PR-level): `make check-all`; confirm diff touches only
      `internal/core/classify/{classification,decode,prompt}{,_test}.go`,
      `testdata/classify/format.md` and its golden corpus, `docs/02-cognitive-core.md`. Target ≤380
      impl+docs lines — **Medium-High risk**, 0.95× the ceiling before code is written. **If the
      golden-corpus regeneration runs long**, split at `InterruptLevel` (tasks 4.1–4.2, half of
      4.5/4.8) | `RecurrenceRule` (tasks 4.3–4.4, the other half, plus Finding F3's type
      resolution) — not pre-drawn by design, report before splitting.

      **Diff-scope deviation, found at apply time**: the stated scope omitted
      `test/support/goldenset/types.go`. `goldenset.ClassifyExpected` needed
      `InterruptLevel`/`RecurrenceRule` fields before any golden-corpus case file could carry
      either name past `Load`'s `DisallowUnknownFields` gate — task 4.6's own instruction to
      "widen [...] the golden corpus" is unsatisfiable without it. `make check-all` stayed
      green; impl+docs landed at 115 lines (see apply-progress), well under the 380 budget even
      with this file included.

---

## PR 5 — `feat/core-prospection-digest-gates` (~400 impl+docs)

Independent of PR 3/4. Ships `digest.go` — `DigestDue`, `LowEnergy`, `Carry`, 5 constants; the
first `internal/core/focus` importer outside its own package. See **Finding F4**: `Carry` merges
spec R4.2 and R4.3 into one function.

- [x] **5.1** Commit 1 (RED): `internal/core/prospection/digest_test.go` — `DigestDue`: `nil`
      `lastDigestAt` + `now` at/after `DigestHour` → true (spec R4.1's nil-prior scenario); `now`
      before `DigestHour` same day → false; `lastDigestAt` at today's `DigestHour` instant, `now`
      later same day → false; `lastDigestAt` from yesterday, `now` at/after today's `DigestHour` →
      true (downtime is normal — one digest owed after three days off, never three).
      **Red**: `undefined: prospection.DigestHour`, `undefined: prospection.DigestDue`.
      Stub: `const DigestHour = 7`; `func DigestDue(lastDigestAt *time.Time, now time.Time) bool {
      return false }` — compiles; the nil-prior case expects `true`, fails first.
      Requirement: R4.1.
- [x] **5.2** Commit 2 (GREEN): implement `DigestDue` — local hour `>= DigestHour` AND
      (`lastDigestAt` nil OR strictly before today's `DigestHour` instant), built with `time.Date`.
      Verify: `go test ./internal/core/prospection/...`.
      Requirement: R4.1; design §3.5.
- [x] **5.3** `digest_test.go` (continued) — the relation `DigestHour >= QuietHoursEndHour`,
      computed from both named constants.
      **Not a missing-symbol red**: both constants already exist by this point in the chain
      (PR 1 merged) — disclosed per `m2a` C9.
      Requirement: design §3.5.
- [x] **5.4** Commit 1 (RED): `digest_test.go` (continued) — `LowEnergy`: `nil` reading → false
      ("no observation is not an observation of depletion," spec R4.2's own resolution); `Level <
      LowEnergyMax` and `RecordedAt` within `EnergyReadingMaxAgeHours` → true; `Level ==
      LowEnergyMax` exactly → false (strict `<`); a reading older than
      `EnergyReadingMaxAgeHours` → false regardless of `Level` (the "recent" half).
      **Red**: `undefined: prospection.LowEnergyMax/EnergyReadingMaxAgeHours`, `undefined:
      prospection.EnergyReading`, `undefined: prospection.LowEnergy`.
      Stub: the two consts; `type EnergyReading struct{ Level float64; RecordedAt time.Time }`;
      `func LowEnergy(r *EnergyReading, now time.Time) bool { return true }` — compiles; the `nil`
      case expects `false`, fails first.
      Requirement: R4.2 (the resolution half — the `pattern_based`/no-priority answer lives in
      `Carry`, task 5.6).
- [x] **5.5** Commit 2 (GREEN): implement `LowEnergy` — `nil` → false; else `Level < LowEnergyMax`
      AND `now.Sub(r.RecordedAt) <= EnergyReadingMaxAgeHours*time.Hour`.
      Verify: `go test ./internal/core/prospection/...`.
      Requirement: R4.2; design §3.5.
- [x] **5.6** Commit 1 (RED): `digest_test.go` (continued) — `Carry`: `!lowEnergy` → every item
      carried, nothing held (spec R4.2's own "at/above energy, every candidate passes"); fewer
      items than `LowEnergyDigestSize` under `lowEnergy` → all carried; more items than
      `LowEnergyDigestSize`, none at `MaxDigestDeferrals` → exactly the top `LowEnergyDigestSize` by
      `focus.Rank` carried, the rest held (R4.2's truncation half); `Deferrals ==
      MaxDigestDeferrals-1` held again by rank; `Deferrals == MaxDigestDeferrals` carried
      "regardless of rank," even ranked last (spec R4.3's own scenario); a fresh candidate
      (`Deferrals == 0`) never force-carries; a `DigestItem` with `Candidate == nil` (`pattern_based`,
      no source unit) enters `focus.Rank` as the zero `focus.Candidate`, ranked last
      deterministically by ID, never a nil dereference (R4.2's own explicit obligation).
      **Red**: `undefined: prospection.LowEnergyDigestSize/MaxDigestDeferrals`, `undefined:
      prospection.DigestItem`, `undefined: prospection.Carry`.
      Stub: `const LowEnergyDigestSize = focus.DefaultSize / 2`; `const MaxDigestDeferrals = 3`;
      `type DigestItem struct{ ID string; Candidate *focus.Candidate; Deferrals int }`; `func
      Carry(items []DigestItem, adjacency map[string]float64, lowEnergy bool, now time.Time)
      (carry, held []DigestItem) { return nil, nil }` — compiles; the `!lowEnergy`-carries-everything
      case expects `len(carry) == len(items)`, fails first.
      Requirement: R4.2, R4.3 — see **Finding F4**.
- [x] **5.7** Commit 2 (GREEN): implement `Carry` per design's three-step rule: `!lowEnergy`
      short-circuit; `MaxDigestDeferrals` force-carry (added regardless of rank, never inside the
      truncation); rank the remainder via `focus.Rank` (zero `focus.Candidate` for a nil
      `Candidate`) and carry the top `LowEnergyDigestSize`.
      Verify: `go test ./internal/core/prospection/...` (now imports `internal/core/focus`).
      Requirement: R4.2, R4.3; design §3.5.
- [x] **5.8** `digest_test.go` (continued) — the relation `MaxDigestDeferrals <
      consolidation.LoadCooldownDays`, computed from both named constants (owner-review **R2**'s
      upper bound).
      **Not a missing-symbol red**: `consolidation.LoadCooldownDays` ships in `m2b` — disclosed per
      `m2a` C9.
      Requirement: design §3.5; **R2**.
- [x] **5.9** `digest_test.go` (continued) — `LowEnergyDigestSize == focus.DefaultSize / 2`, a
      `go/constant`-level equality (written as that expression, not the literal `3`).
      **Not a missing-symbol red**: same as 5.8.
      Requirement: design §3.5.
- [x] **5.10** `docs/02-cognitive-core.md` §7 amendment: the cadence; the care gate's two-part shape
      ("low" and "recent"); the anti-starvation rule (one deferral = one day, forced delivery
      regardless); the `pattern_based`-trigger answer (ranked last via the zero-Candidate score, no
      special case).
      Requirement: design §3.5.
- [x] **5.11** §13: five new rows — `digest_hour` (7, derived); `low_energy_max` (0.5, chosen,
      midpoint of an uncalibrated `[0,1]` scale); `energy_reading_max_age_hours` (24, derived from
      the digest cycle); `low_energy_digest_size` (3, derived as `focus.DefaultSize/2`);
      `max_digest_deferrals` (3, chosen inside the band `(1, load_cooldown_days)` — **R2**).
      Requirement: R0; design §3.5.
- [x] **5.12** Purity/lint: `golangci-lint run` (`core-purity` — first import of
      `internal/core/focus` from outside `focus` itself; confirm `depguard` allows the core-to-core
      edge, per design §4's own claim).
      Requirement: `nooma-core` hard rule 1; design §4 (Risk C).
- [x] Verify (PR-level): `make check-all`; confirm diff touches only
      `internal/core/prospection/digest{,_test}.go`, `docs/02-cognitive-core.md`. Target ≤400
      impl+docs lines — **at the ceiling before code is written, highest risk in this chain**. **If
      `Carry`'s own test table or its four-part doc-02 amendment run long**, split at `DigestDue` +
      `LowEnergy` (the two single-reading gates) | `Carry` (the whole-list composition, and where
      the `focus` import lands) — not pre-drawn by design, report before splitting.

---

## PR 6 — `feat/core-prospection-recurrence` (~350 impl+docs)

Independent; feeds PR 7. Ships `recurrence.go` — `NextOccurrence`, `Rule`, `Anchor`, 1 constant.
I17 pure half.

- [x] **6.1** Commit 1 (RED): `internal/core/prospection/recurrence_test.go` — an ordinary anchor
      (15 March / day 10) across several years/months as a sanity baseline (spec R5.1's own
      baseline); yearly `{Feb, 29}` evaluated from a leap year toward a non-leap year → **28 Feb**
      (design's clamp rule, chosen over Go's own `Mar 3` rollover and over "skip months lacking the
      day"); monthly `{day: 31}` toward a 30-day month → that month's last day; determinism — the
      same `(rule, anchor, after)` computed twice returns the same instant; every returned instant
      carries `RecurrenceAnchorHour` regardless of `after`'s own hour (never inherited).
      **Red**: `undefined: prospection.RuleYearly/RuleMonthly`, `undefined: prospection.Anchor`,
      `undefined: prospection.RecurrenceAnchorHour`, `undefined: prospection.NextOccurrence`.
      Stub: `type Rule string`; the two consts; `type Anchor struct{ Month time.Month; Day int }`;
      `const RecurrenceAnchorHour = 12`; `func NextOccurrence(rule Rule, anchor Anchor, after
      time.Time) time.Time { return after }` — compiles; the ordinary-baseline case expects a later
      instant, fails first.
      Requirement: R5.1 — note: design's parameter is named `after`, spec's `firedAt`; kept as
      `after` per design's own stated reason (the caller may mean "the arming instant," not only
      "the last occurrence").
- [x] **6.2** Commit 2 (GREEN): implement `NextOccurrence` via `time.Date` with out-of-range day
      fields (never `AddDate`), clamping to the target month's last day.
      Verify: `go test ./internal/core/prospection/...`.
      Requirement: R5.1; design §3.6.
- [x] **6.3** `recurrence_test.go` (continued) — the anchor-idempotence property: occurrence *N*
      re-derived from the anchor *N* times equals occurrence *N* computed directly — never
      "advance from the previous occurrence" (design's Rule 2, rejecting the drift bug where
      advancing 29 Feb by a year gives 28 Feb forever). Fixtures fix both the anchor and `after` as
      literals — no test computes a year from anything but a literal (design §12 Risk E).
      Requirement: design §3.6 (Rule 2); Risk E.
- [x] **6.4** `recurrence_test.go` (continued) — the two mandatory zone fixtures (design §8):
      `America/Havana` (spring-forward at local midnight) and `Pacific/Apia` (2011-12-30 does not
      exist); a third fixed-offset zone as the control. `import _ "time/tzdata"` in the test file
      only (tzdata stays out of the shipped binary — this repo cross-compiles for Windows,
      ADR-0013).
      Requirement: design §3.6, §8; R5.1.
- [x] **6.5** `test/conformance/i17_recurrence_same_unit_test.go` (new) — I17's arithmetic half
      only (the "same unit" half is a caller obligation, spec R5.1's own scoping): every occurrence
      carries the anchor's own month/day, clamped, never the previous occurrence's.
      Requirement: R5.1 (I17 row, `docs/06-harness.md` §4).
- [x] **6.6** `docs/02-cognitive-core.md` §7 amendment: the clamp rule (never overflow, never skip)
      and the anchor-idempotence rule (always re-derive, never advance from the previous
      occurrence).
      Requirement: design §3.6.
- [x] **6.7** §13: new row `recurrence_anchor_hour` (12, derived — local noon; midnight rejected
      because a DST gap can normalise it onto the previous calendar date).
      Requirement: R0; design §3.6.
- [x] **6.8** Purity/lint: `golangci-lint run`.
      Requirement: `nooma-core` hard rules 1–2.
- [x] Verify (PR-level): `make check-all`; confirm diff touches only
      `internal/core/prospection/recurrence{,_test}.go`,
      `test/conformance/i17_recurrence_same_unit_test.go`, `docs/02-cognitive-core.md`. Target
      ≤350 impl+docs lines.

---

## PR 7 — `feat/core-prospection-arming` (~350 impl+docs)

Depends on PR 4 (classify fields) and PR 6 (`NextOccurrence`). Ships `arm.go` — `Arm`, `Plan`, 1
constant. See **Finding F5**: ships an exported, unclamped lead-time function (spec R5.2) plus
`Arm`'s own internal now-clamp (design), as two composed layers rather than one.

- [x] **7.1** Commit 1 (RED): `internal/core/prospection/arm_test.go` — the standalone lead-time
      function: `eventAt` nine days out → `fireAt` exactly two days out (spec R5.2's own scenario);
      `eventAt - EventLeadDays*24h` landing in the past → returned **unclamped** (spec's explicit
      "no clamping at this layer").
      **Red**: `undefined: prospection.EventLeadDays`, `undefined: prospection.<lead-time func —
      name confirmed at apply time>`.
      Stub: `const EventLeadDays = 7`; the lead-time func returning `eventAt` unchanged — compiles;
      the nine-days-out case fails first.
      Requirement: R5.2, resolved per **Finding F5**.
- [x] **7.2** Commit 2 (GREEN): implement the lead-time arithmetic via `time.Date` with a negative
      day offset (house pattern), no clamp.
      Verify: `go test ./internal/core/prospection/...`.
      Requirement: R5.2; design §3.7.
- [x] **7.3** Commit 1 (RED): `arm_test.go` (continued) — `Arm`: the four-way outcome table from
      every `classify.Kind` (design §3.7's table) — `timer` with a resolved `DueAt` after `now` →
      `ArmTimer`, `FireAt = *DueAt` (never reading `EventAt` — I18's pure half); dated `event` after
      `now` → `ArmTrigger`, `FireAt` = the clamped lead-time result; `recurring_reminder` with
      `EventAt` + `RecurrenceRule` both present → `ArmRecurring`, anchor from `EventAt`'s own
      month/day, `FireAt = lead(NextOccurrence(...))`; `recurring_reminder` with `EventAt` present
      but `RecurrenceRule` degraded → `ArmTrigger` for the one-shot dated occurrence (capture
      honoured, recurrence not invented); everything else, or undated/past-dated → `ArmNothing`,
      `false`, with a reason distinct from a non-arm-worthy `Kind` (spec R6.1's own "distinct
      outcome" requirement); `Plan.Interrupt` always resolved via `ResolveInterrupt`, never left
      unset.
      **Red**: `undefined: prospection.ArmNothing/ArmTimer/ArmTrigger/ArmRecurring`, `undefined:
      prospection.Plan`, `undefined: prospection.Arm`.
      Stub: the four `Armament` consts; `type Plan struct{ What Armament; FireAt time.Time;
      LeadDays int; Rule Rule; Anchor Anchor; Interrupt Interrupt }`; `func Arm(c
      classify.Classification, now time.Time) (Plan, bool) { return Plan{}, false }` — compiles;
      the `timer` case fails first.
      Requirement: R6.1, resolved per **Finding F2** (`DecideArming`→`Arm`) and **Finding F3**
      (`RecurrenceRule`'s classify-side value converted into `prospection.Rule` at this call site).
- [x] **7.4** Commit 2 (GREEN): implement `Arm` per design's five-row table; `DueAt` read only for
      `timer`, `EventAt` read only for `event`/`recurring_reminder` (I18); the now-clamp applied
      only at this call site, composing task 7.2's unclamped function.
      Verify: `go test ./internal/core/prospection/...` (now imports `internal/core/classify`).
      Requirement: R6.1; design §3.7.
- [x] **7.5** `arm_test.go` (continued) — I18 as a property of `Arm`'s body: for a fixture carrying
      distinct `EventAt`/`DueAt`/`CreatedAt`, `Arm` never reads `CreatedAt` and reads exactly one of
      `EventAt`/`DueAt` per `Kind`, never both; a sweep over every `classify.Kind` confirms no
      function in this file returns `unit.Unit`, `*unit.Unit`, or `[]unit.Unit`.
      **Not a missing-symbol red**: `Arm` already compiles and passes (task 7.4) — disclosed per
      `m2a` C9.
      Requirement: R6.1 (I18 half, the `unit.Unit`-absence property).
- [x] **7.6** `internal/core/classify/prompt.go`: state that a timer's instant lives in `due_at`,
      never `event_at`.
      Requirement: design §3.7 (decision 1).
- [x] **7.7** `docs/02-cognitive-core.md` §5 step 5 amendment: retire the M1 note deferring
      timer/trigger arming; state what `Arm` does for each `Kind`.
      Requirement: design §3.7.
- [x] **7.8** `docs/02-cognitive-core.md` §7 amendment: the lead-time clamp rule (arming at `now` is
      honest for an event whose lead window has already partly elapsed); the timer's
      `due_at`-not-`event_at` rule (cross-referencing task 7.6).
      Requirement: design §3.7 (decisions 1–2).
- [x] **7.9** §13: row 914 (`Event lead time`) amended with `prospection.EventLeadDays`; confirm
      row 899's note that `focus.UrgencyLeadDays` is a separate knob stays true and is now
      checkable on both ends (no new row).
      Requirement: R0; design §3.7.
- [x] **7.10** Purity/lint: `golangci-lint run` (`core-purity` — `prospection` now imports
      `internal/core/classify`; confirm no cycle, i.e. `classify` imports `prospection` nowhere in
      the tree after this PR).
      Requirement: `nooma-core` hard rule 1; design §4.
- [x] Verify (PR-level): `make check-all`; confirm diff touches only
      `internal/core/prospection/arm{,_test}.go`, `internal/core/classify/prompt.go`,
      `docs/02-cognitive-core.md`. Target ≤350 impl+docs lines. **Chain's last link** — after merge,
      `rg` over `internal/ports`, `internal/store`, `internal/brain`, `internal/scheduler`,
      `internal/channels` confirms none references any `internal/core/prospection` symbol yet (the
      spec's own exit criterion).

---

## Review Workload Forecast

| Field | Value |
|-------|-------|
| Estimated changed lines | ~2,470 budgeted impl+docs across 7 PRs (design §7); test lines not separately budgeted by design but historically 1.3×–4.3× of impl+docs on this project — tracked per PR, not against the 400 ceiling |
| 400-line budget risk | **High for PR 5** (400, at the ceiling pre-code); **Medium-High for PR 2** (370) and **PR 4** (380); **Low-Medium for PR 1** (320), **PR 3** (300), **PR 6/PR 7** (350 each) |
| Chained PRs recommended | Yes — seven links, already a chain by design |
| Suggested split | No pre-drawn split lines exist for PR 2, PR 4, or PR 5 (unlike `m2b`'s precedent) — each PR's own task list above names its natural split boundary as a fallback, to be used only if measured lines threaten 400, reported before splitting rather than split silently |
| Delivery strategy | `auto-chain` |
| Chain strategy | `stacked-to-main` |

Decision needed before apply: No
Chained PRs recommended: Yes
Chain strategy: stacked-to-main
400-line budget risk: Medium

### Suggested Work Units

| Unit | Goal | Likely PR | Focused test command | Runtime harness | Rollback boundary |
|------|------|-----------|----------------------|-----------------|-------------------|
| 1 | Quiet hours + `DeliverableFrom`, I16 pure half | PR 1 | `go test ./internal/core/prospection/... -run QuietHours` | `test/conformance/i16_quiet_hours_test.go` | Delete `quiethours{,_test}.go`; no importer exists yet |
| 2 | Staleness gates + delay caveat, I15 pure half | PR 2 | `go test ./internal/core/prospection/... -run Verdict` | `test/conformance/i15_trigger_expires_not_fires_test.go` | Delete `staleness{,_test}.go`; depends on PR 1 only |
| 3 | Push/digest split | PR 3 | `go test ./internal/core/prospection/... -run Interrupt` | N/A — no conformance row of its own; covered by R3.2's scenario | Delete `delivery{,_test}.go`; no dependents in this chain |
| 4 | Classify widening (`interrupt_level`, `recurrence_rule`) | PR 4 | `go test ./internal/core/classify/...` | Golden-corpus regression | Revert the two decoder rows + prompt lines; `prospection` unaffected until PR 7 |
| 5 | Digest gates, first `focus` importer | PR 5 | `go test ./internal/core/prospection/... -run Carry` | N/A — no conformance row; I09 is `m3d`'s | Delete `digest{,_test}.go`; the only PR touching `focus` coupling, cleanly reversible |
| 6 | Recurrence, I17 pure half | PR 6 | `go test ./internal/core/prospection/... -run NextOccurrence` | `test/conformance/i17_recurrence_same_unit_test.go` | Delete `recurrence{,_test}.go`; no dependents until PR 7 |
| 7 | Arming decision, I18 first real load | PR 7 | `go test ./internal/core/prospection/... -run Arm` | N/A — I18's behavioral half is `m3b`'s; this PR's exit `rg` scan is structural | Delete `arm{,_test}.go` + the prompt line; `m3a`'s own exit criterion becomes uncheckable until re-added |

---

## Traceability

| Spec section | Requirements | Tasks |
|---|---|---|
| §1 Staleness (I15) | R1.1–R1.3 | 2.1–2.9 (see Finding F1) |
| §2 Quiet hours (I16) | R2.1 | 1.1–1.8 |
| §3 Delivery split | R3.1, R3.2 | 3.1–3.7 (see Finding F2) |
| §4 Digest gates | R4.1–R4.3 | 5.1–5.11 (see Finding F4) |
| §5 Recurrence + lead time | R5.1, R5.2 | 6.1–6.7, 7.1–7.2 (see Finding F5) |
| §6 Arming | R6.1 | 7.3–7.9 (see Finding F2, F3) |
| R0 — purity and calibration, cross-cutting | R0 | 1.9, 2.10, 3.8, 4.9, 5.12, 6.8, 7.10 (purity); 1.8, 2.9, 3.7, 4.8, 5.11, 6.7, 7.9 (§13 rows) |
| Doc 02 amendments (§4.2's seven gates) | — | 1.6–1.7, 2.8, 3.6, 4.7–4.8, 5.10, 6.6, 7.7–7.8 |
| Owner-review items | R1–R5 | see the table above |
| §7 "What this spec does not require" | — | not tasked; `m3b`/`m3c`/`m3d`'s own scope |

---

## Handoffs to `m3b`/`m3d` (design §11/§12, carried forward)

- **F3's resolution** (classify's `RecurrenceRule` type and its conversion into `prospection.Rule`
  at `Arm`'s call site) is `m3a`'s own to confirm at apply time; `m3b`'s arming-at-capture PR
  consumes `Arm`'s `Plan` output only, never the classify-side type directly.
- **R4** (`DelayCaveatMinutes >= 3×` tick relation) is an `m3d` #1 L2 test obligation, documented
  in PR 2's §13 row rather than asserted here.
- **Q1** (does a digest carrying nothing get published?) stays undecided — `Carry`'s two return
  slices take no position; `m3d`'s digest assembly decides.
- The NULL↔degraded round trip for `triggers.interrupt_level` (PR 3, task 3.6) is `m3a`'s contract
  to state and `m3b`'s to implement at the store layer.

---

## Reconciliation note — 2026-08-20

Two disagreements beyond F1–F5, found on a second pass over `spec.md` and `design.md` and resolved
before apply.

- **F6 — the delay-caveat boundary.** Resolved **inclusive**. Tasks 2.5 and 2.6 above were
  corrected: the RED case at exactly `DelayCaveatMinutes` now expects `true`, and the GREEN
  implementation is `overdue >= DelayCaveatMinutes*time.Minute`. `design.md` §3.3 was corrected to
  match; `spec.md` R1.3 already read inclusive and did not change.
- **F7 — `internal/core/classify` inside `m3a`.** `spec.md`'s scope box and R3.1 declared the
  package out of scope while PR 4 ships it. `spec.md` was corrected; the task list, which already
  carried PR 4 in full, did not change.

---

## Reconciliation note — 2026-08-20 (F8, found at apply time)

**F8 — `Interrupt`'s degraded field is inverted from the design's snippet.** `design.md` §3.4's
illustrative Go declares `type Interrupt struct { level float64; degraded bool }`, and this
document's own task 3.1 stub repeats it. PR 3 ships `confirmed bool` instead, with
`Degraded()` returning `!confirmed`. Recorded here because F1-F7 set the convention that a
deviation from a planning artifact is written down, not just implemented.

**Why the inversion.** With `degraded bool`, an `Interrupt` that never passed through
`ResolveInterrupt` reports `Degraded() == false` — it claims a provenance it does not have.
Three consequences, of which only the first is harmless:

1. *Routing is unaffected.* A zero-value `Interrupt{}` carries `level == 0.0`, below
   `PushThreshold`, so it routes to the digest under either polarity. Judgment Day's first judge
   verified this and was right to.
2. *An in-package literal is not.* `Interrupt{level: 0.9}` written by hand reports itself
   non-degraded and routes to **push**, having never been validated. Today `prospection` is one
   author's package; PRs 5 and 7 add files to it.
3. *The audit trail is not.* `Degraded()` does not only feed routing — doc 02 §7, as amended by
   this same PR, makes it decide persistence: `brain` writes `triggers.interrupt_level` as `NULL`
   exactly when the resolution degraded. Under the old polarity a forgotten resolution would
   persist `0.0` as a **claimed** value, which is the precise distinction §5.1 warns about ("a
   degraded weight is not a zero weight") written into the database.

With `confirmed bool`, `confirmed` is set only inside `ResolveInterrupt` on a validated in-range
value, so the zero value and every hand-written literal are degraded by construction. The
property the design stated is preserved; only the field expressing it changed.

**F8's own scope note.** This is not a correction to `design.md` §3.4's decision — that decision
is implemented faithfully. It corrects the snippet that illustrated it.

---

## Judgment Day note — PR 4, 2026-08-21

Round 1 on frozen target `0c76021`. Both judges independently found the same CRITICAL; the
findings below are recorded here because two of them are about how this change was *made*, not
only about what it shipped.

- **JD-4-01 (CRITICAL, both judges, fixed).** `{"interrupt_level": null}` decoded to a claimed
  `0.0` with no degradation. `Salvage` stores any decodable value under its key, so an explicit
  null is present rather than absent, and `json.Unmarshal` accepts null for a non-pointer
  destination without error. Fixed by reading into a `*float64`; the three states — absent,
  degraded, claimed 0.0 — were re-verified distinct by decoding all three.
- **JD-4-02 (WARNING, both judges, base-only, NOT fixed here).** `assignFloat` carries the
  identical shape for `weight` and `decay_rate` and has since M1. `classification.go`'s own doc
  comment has described this exact defect since M1, and `goldenset.ClassifyExpected` fixed it on
  the fixture side — the decoder never did. Worse than JD-4-01's instance, because doc 02 records
  that a λ of 0 never decays and so §6's archiving pass can never reach the unit, while the row
  violates no NOT NULL constraint. **Its own work unit**, with its own conformance test and doc
  02 delta, rather than buried in a PR named for two prospection fields.
- **JD-4-03 (WARNING, judge B, fixed).** `classification.go`'s citation of
  `goldenset/types.go:152-165` went stale — this PR's own widening of that file shifted the
  block to 210-217. The comment now also records the necessary-but-not-sufficient point: a
  pointer *field* does not help unless the *assigner* reads into a pointer.
- **JD-4-04 (WARNING, judge B, recorded not re-done).** Task 4.6's golden-corpus widening was
  reported as having no genuine RED available, citing `m2a` C9. That framing does not hold:
  adding a fixture carrying the new keys *before* widening `goldenset.ClassifyExpected` would
  fail `Load`'s `DisallowUnknownFields`, which is a real, mechanically-detectable red. C9 covers
  a check whose operands both already exist and therefore cannot fail; this was not that. The
  commits are not being rewritten, and the misapplied precedent is recorded so the next slice
  does not inherit it.
- **JD-4-05 (SUGGESTION, judge B).** `{"recurrence_rule": null}` degrades safely — `""` matches
  no vocabulary member, so `ReasonUnknownEnum` is recorded and the field stays nil — but the
  label describes an explicit null imprecisely. No data is miscoded, so it is pinned as-is with
  the imprecision stated rather than a sixth `Reason` invented for it (owner-review R3's own
  argument).

---

## Reconciliation note — 2026-08-21 (F9, found by Judgment Day on PR 7)

**F9 — spec R6.1 and design §3.7 disagree about a past-dated recurring reminder.** R6.1's MUST
reads *"a dated `event` or `recurring_reminder` whose instant is at or before `now` arms
nothing"*, with no exception for a rule-bearing recurrence. Design §3.7's table applies that
refusal only to the one-shot rows, and its "three decisions" note scopes it to *"a dated event"*.
`Arm` follows the design; no test pinned which reading governed.

**Resolved in the design's favour, and the spec's phrasing is the error.** A birthday's `event_at`
is the birth date — always in the past, usually by decades. Applying the refusal to a rule-bearing
recurring reminder would make the feature refuse every input it exists to serve.

The distinction the spec's wording loses: a **one-shot instant** is a thing that happens once and
can be over, and arming a nudge for it afterwards is doc 02 §5.1's refusal pointing the other way.
A **recurrence's `event_at` is an anchor** — a month and a day the next occurrence is re-derived
from, with its year discarded. The same date is a spent instant without a rule and a live anchor
with one, and `TestArm_RecurringIgnoresHowOldItsAnchorIs` asserts both halves in one test so the
boundary is visible rather than inferred.

**Also pinned while resolving it**: which frame the anchor's month and day are read in. `classify`
may decode an RFC3339 `event_at` carrying its own offset, while `NextOccurrence` builds occurrences
in `now`'s location — two frames, and near midnight they disagree on the calendar date. The anchor
is read in the **event's own zone**, because an anniversary is a calendar date rather than an
instant: "4 September" means 4 September wherever the person later happens to be, and the
occurrence is materialised in the zone they are in now. `TestArm_AnchorIsTheDateAsStated` pins it.

**F10 — the `unit.Unit` scan cannot be an L1 test.** Spec R6.1 lists it under *"Verified by: L1"*,
and depguard's `core-purity` rule forbids importing `os` anywhere under `internal/core`, its own
tests included. A scan that reads the package's files therefore cannot live beside them. It ships
as `test/conformance/i04_arming_never_produces_a_unit_test.go` instead. The rule is right and the
spec's layer assignment was optimistic; nothing about the assertion changed, only where it runs.
