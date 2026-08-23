# Archive Report — m3a-prospection

**Change**: m3a-prospection (first of M3's chained changes: **m3a** → m3b → m3c → m3d)
**Date Archived**: 2026-08-22
**Mode**: hybrid (openspec files + Engram observations)
**Status**: Complete — all 7 implementation PRs merged plus one Judgment Day escalation PR, all 73
tasks checked
**Main Branch HEAD at Archive**: d05b3e1

**Archived late, and that is worth saying rather than hiding.** `m3a` finished on 2026-08-21 and
`m3b` was built and archived on top of it before this report was written. Nothing was lost — the
artifacts sat complete in `openspec/changes/m3a-prospection/` the whole time — but for a day the
active-changes directory claimed work was in flight that was not. The lesson is the same one F1–F10
kept teaching this change: the record is part of the work, not a thing done after it.

---

## Artifacts Archived

Moved to `openspec/changes/archive/2026-08-22-m3a-prospection/`:

- `spec.md` — Requirements R0–R6.1
- `design.md` — §1–§12 plus owner rulings
- `tasks.md` — the seven-PR task list, all 73 tasks checked, findings F1–F10, the PR 4 Judgment Day
  note, and three reconciliation notes
- `archive-report.md` — this report

## What shipped

`internal/core/prospection` — the brain's decision logic for *when to speak*, pure, with no I/O and
no clock of its own. Every gate M3 needs, decided in core and tested there, before a single row was
written anywhere.

| # | PR | Branch | Merge |
|---|---|---|---|
| 1 | [#194](https://github.com/rengo/nooma/pull/194) | `feat/core-prospection-quiet-hours` | `8cafbad` |
| 2 | [#195](https://github.com/rengo/nooma/pull/195) | `feat/core-prospection-staleness` | `045fda2` |
| 3 | [#196](https://github.com/rengo/nooma/pull/196) | `feat/core-prospection-delivery-split` | `06bbb0f` |
| 4 | [#197](https://github.com/rengo/nooma/pull/197) | `feat/classify-prospection-fields` | `625d678` |
| — | [#198](https://github.com/rengo/nooma/pull/198) | `fix/classify-explicit-null-required-floats` | `1bf33f4` |
| 5 | [#199](https://github.com/rengo/nooma/pull/199) | `feat/core-prospection-digest-gates` | `97340a4` |
| 6 | [#200](https://github.com/rengo/nooma/pull/200) | `feat/core-prospection-recurrence` | `80a3fce` |
| 7 | [#201](https://github.com/rengo/nooma/pull/201) | `feat/core-prospection-arming` | `aa4538a` |

Planning artifacts landed separately: [#193](https://github.com/rengo/nooma/pull/193) (M3's
umbrella plus `m3a`'s three) and [#202](https://github.com/rengo/nooma/pull/202) (`m3b`'s).

**What is in the package**: `QuietHoursStartHour`/`QuietHoursEndHour`/`InQuietHours`/
`DeliverableFrom`; `TriggerStalenessHours`/`TimerStalenessHours`/`Verdict`/`TriggerVerdict`/
`TimerVerdict`/`DelayCaveat`; `Interrupt`/`ResolveInterrupt`/`PushThreshold`/`Route`;
`Rule`/`Anchor`/`NextOccurrence`/`RecurrenceAnchorHour`; `Carry` and the digest gates;
`Armament`/`Refusal`/`Plan`/`Arm`/`LeadTime`/`EventLeadDays`. `internal/core/classify` widened by
two decoded fields, `interrupt_level` and `recurrence_rule`.

`AllVerdicts()` was added later, by `m3b` PR 5a — the one `internal/core` line that change shipped,
recorded there as its finding G3.

## Verification Gate: PASS

`make check-all` green throughout, and `internal/core` coverage held at **100%** against the 90%
floor while the package grew — `m3a` is the largest single addition `internal/core` has taken.

**Purity held**: `internal/core/prospection` imports the standard library and its own packages and
nothing else. It reads no clock — every instant arrives as a parameter — and no timezone: the user's
zone is carried by the `time.Time` values themselves.

## Task Completion Gate: PASS

All 73 tasks checked. Strict TDD across all seven PRs.

---

## Findings — F1 through F10

Ten disagreements and gaps recorded rather than papered over. `tasks.md` carries them in full, with
three reconciliation notes and a Judgment Day note. What a future reader needs:

### The two that changed what the system does

**F9 — spec R6.1 would have made recurring reminders refuse every input they exist to serve.** R6.1's
MUST reads *"a dated `event` or `recurring_reminder` whose instant is at or before `now` arms
nothing"*, with no exception for a rule-bearing recurrence. **A birthday's `event_at` is the birth
date — always in the past, usually by decades.** Design §3.7 scoped the refusal to the one-shot rows
and `Arm` follows the design, but no test pinned which reading governed until Judgment Day on PR 7
asked.

Resolved in the design's favour; the spec's phrasing is the error. The distinction its wording loses:
a **one-shot instant** is a thing that happens once and can be over, and arming a nudge for it
afterwards is doc 02 §5.1's refusal pointing the other way. A **recurrence's `event_at` is an
anchor** — a month and a day the next occurrence is re-derived from, with its year discarded. The
same date is a spent instant without a rule and a live anchor with one.
`TestArm_RecurringIgnoresHowOldItsAnchorIs` asserts both halves in one test, so the boundary is
visible rather than inferred.

**Pinned while resolving it**: which frame the anchor's month and day are read in. `classify` may
decode an RFC3339 `event_at` carrying its own offset while `NextOccurrence` builds occurrences in
`now`'s location — two frames that disagree on the calendar date near midnight. The anchor is read in
the **event's own zone**, because an anniversary is a calendar date rather than an instant: "4
September" means 4 September wherever the person later happens to be.

**F8 — `Interrupt`'s degraded field is inverted from the design's snippet, deliberately.** Design
§3.4 declares `degraded bool`; PR 3 ships `confirmed bool` with `Degraded()` returning `!confirmed`.
Under the design's polarity, an `Interrupt` that never passed through `ResolveInterrupt` reports
`Degraded() == false` — it claims a provenance it does not have. Routing survives that (a zero value
carries `level == 0.0`, below `PushThreshold`), but two things do not: a hand-written
`Interrupt{level: 0.9}` would route to **push** having never been validated, and the audit trail
would persist a forgotten resolution as a **claimed** `0.0` rather than NULL — which is precisely
the distinction doc 02 §5.1 warns about, written into the database. With `confirmed`, the zero value
and every literal are degraded by construction.

### The defect a review found, and the one it refused to bury

**JD-4-01 (CRITICAL, both judges).** `{"interrupt_level": null}` decoded to a **claimed `0.0`** with
no degradation recorded. `Salvage` stores any decodable value under its key, so an explicit null is
*present* rather than absent, and `json.Unmarshal` accepts null for a non-pointer destination without
error. Fixed by reading into a `*float64`; the three states — absent, degraded, claimed `0.0` — were
re-verified distinct by decoding all three.

**JD-4-02 (WARNING, both judges) — the same defect, older and worse, deliberately NOT fixed in that
PR.** `assignFloat` carries the identical shape for `weight` and `decay_rate`, and has since M1.
`classification.go`'s own doc comment has described this exact defect since M1; `goldenset` fixed it
on the fixture side and the decoder never did. It is worse than JD-4-01's instance because doc 02
records that a λ of 0 never decays, so §6's archiving pass can never reach such a unit, while the row
violates no `NOT NULL` constraint. Given **its own work unit** ([#198](https://github.com/rengo/nooma/pull/198))
with its own conformance test and doc 02 delta, rather than buried in a PR named for two prospection
fields.

**JD-4-04 (WARNING) — a misapplied precedent, recorded rather than rewritten.** Task 4.6's
golden-corpus widening was reported as having no genuine RED available, citing `m2a` C9. That framing
does not hold: adding a fixture carrying the new keys *before* widening `goldenset.ClassifyExpected`
would fail `Load`'s `DisallowUnknownFields`, which is a real, mechanically-detectable red. C9 covers
a check whose operands both already exist and therefore cannot fail; this was not that. The commits
were not rewritten, and the misapplied precedent is written down so the next slice does not inherit
it.

### Artifact disagreements resolved on their merits

| # | Disagreement | Resolution |
|---|---|---|
| **F1** | Spec R1.1/R1.2's staleness formula is naive; design's is not | Design's favour — measured from the first *deliverable* instant, because the quiet-hours window (7h) is longer than `trigger_staleness_hours` (6), so a naive measure would expire every trigger armed between 00:00 and 01:00, every night |
| **F2** | Function names differ throughout between spec and design | Design's names |
| **F3** | `classify.Classification.RecurrenceRule *Rule` cannot type-check as written | Its own vocabulary in `classify`, converted at `Arm`'s call site |
| **F4** | Design's `Carry` merges two spec requirements into one function | Design's favour |
| **F5** | Spec R5.2 wants an exported unclamped lead-time function; design's `lead(t)` is unexported | Exported `LeadTime`, with the clamp a layer above it — see below |
| **F6** | The delay-caveat boundary, strict or inclusive | **Inclusive**, and the asymmetry with staleness's strict comparison is deliberate: expiring is unrecoverable, delivering late is not, so the boundary belongs on the cheap side |
| **F7** | Spec declared `internal/core/classify` out of scope while PR 4 ships it | Spec corrected |
| **F10** | Spec R6.1 lists the `unit.Unit` scan as L1; depguard forbids `os` under `internal/core`, tests included | Ships as `test/conformance/i04_arming_never_produces_a_unit_test.go` — the rule is right and the spec's layer assignment was optimistic |

**F5's own consequence, found in PR 7 and worth the space**: `LeadTime` alone is not enough. An event
captured two days before it happens has a horizon five days in the past, and arming there hands the
staleness gate something born expired. `clampToNow` is the layer the lead time deliberately does not
have.

---

## Handoffs, and how they landed

`tasks.md` recorded four handoffs to `m3b`/`m3d`. Three are now discharged:

- **F3's resolution** — `m3b` consumes `Arm`'s `Plan` only, never the classify-side type. Held.
- **The NULL↔degraded round trip** for `triggers.interrupt_level` — stated here, implemented by `m3b`
  as `interruptColumn` in `internal/brain`, with an L1 round-trip test proving it is
  `ResolveInterrupt`'s exact inverse. Discharged.
- **R4** (`DelayCaveatMinutes >= 3×` tick relation) — still `m3d`'s L2 obligation. Open.
- **Q1** (does a digest carrying nothing get published?) — still undecided; `Carry` takes no
  position. `m3d`'s digest assembly decides. Open.

**What `m3b` found in this package's behaviour, recorded here because it belongs to `m3a`'s gates**:
an overdue trigger inside quiet hours is **deferred, not expired**, because `verdict` evaluates quiet
hours before staleness — an item is never declared stale during a window in which it was refused
delivery. Correct, deliberate, and stated in none of `m3a`'s artifacts. `m3b` recorded it as G16 and
amended doc 02 §7. It then bit `m3b` a second time (its G22) in a test reading a wall clock. **A
documented interaction is not a guarded one.**

---

## Method notes carried forward

**A pure package earns its tests being pure.** Every gate here is a function of its inputs and one
instant, so every test is L1 and fast, and the conformance suite (I15, I16, I17) checks the
invariants rather than re-implementing the formulas. `i16_quiet_hours_test.go` had to be corrected
once (commit `5da3bd4`) for exactly that: an L2 gate that recomputes the L1 function it guards agrees
with a broken implementation by construction. `i15`'s sweep was written to that lesson.

**Judgment Day earned its cost on PR 4.** Two judges independently found the same CRITICAL, and two
of the five findings were about *how the change was made* rather than what it shipped — a stale
citation and a misapplied precedent. Both were recorded rather than quietly fixed.

**A defect found in passing gets its own work unit.** JD-4-02 was older and worse than the finding
that surfaced it, and burying it in a PR named for two prospection fields would have made it
unreviewable. It became [#198](https://github.com/rengo/nooma/pull/198).

---

## Final State

`internal/core/prospection` is complete for M3's purposes and unchanged since, except for
`AllVerdicts()` added by `m3b`. `internal/core` coverage is 100%.

**Exit criterion discharged**: `Arm` decides what every classification arms, structurally proven
never to produce a `unit.Unit`, with the scan running at L2 for F10's reason.

**Next**: `m3c` — the Telegram transport. `m3b` is archived at
`openspec/changes/archive/2026-08-21-m3b-trigger-timer/`; `m3-mouth-telegram`, M3's umbrella
proposal, stays active until `m3d` closes.
