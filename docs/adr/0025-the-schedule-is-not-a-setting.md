# ADR-0025 — The schedule is not a setting: a key nobody reads is retired, not parsed

- **Status**: Accepted
- **Date**: 2026-08-31
- **Supersedes**: —
- **Superseded by**: —
- **Enables**: M3e

## Context

`nooma.yml` has carried two keys since M0:

```yaml
schedules:
  consolidate: "0 3 * * *"        # nightly
  proactive_check: "*/5 * * * *"  # scan for due triggers + urgent push
```

They were decoded into `config.Schedules` and **read by nobody**. Nothing in this repository
parses a cron expression at all. The schedule that actually runs is two constants:

| Constant | Value |
|---|---|
| `internal/scheduler.ConsolidationHour` | `3` — 03:00 local, daily |
| `internal/scheduler.ProactiveCheckInterval` | `5 * time.Minute` |

This is `m3d` finding **J5**, recorded at the time as *"the pre-existing gap is the finding, and
fixing it is either a parser or a deprecation, each its own work unit"*. This ADR is that work
unit.

Three constraints shaped it.

**The documentation actively invited the keys.** `docs/01-architecture.md` printed the block
above inside its `nooma.yml` example, with comments explaining what each expression did. A user
who copied it got a file that loaded, looked configured, and changed nothing. That is the most
expensive failure a configuration surface has — the same one `decode.go`'s own comment names as
the reason unknown keys are rejected at all: *"the user's next experience is fixing a typo,
restarting, and observing no change at all"*.

**The decoder is strict, so deleting the fields is not free.** `yaml.Strict()` rejects unknown
keys by design (spec R3.2). Removing `Schedules` from the struct without more would turn every
vault that copied the documented example into a vault that will not start, reporting an unknown
key — which reads as *"you made a typo"*, not *"this setting is gone"*.

**The values are already documented and calibrated.** Both constants are rows in
[`02-cognitive-core.md`](../02-cognitive-core.md) §13, with their reasoning written down.
`ProactiveCheckInterval`'s row states it is bounded from both sides: longer, and an item deferred
out of quiet hours waits past 07:00 by up to that long every morning; shorter, and a personal
vault does arithmetic it has no reason to do. These are calibrated numbers, not preferences.

## Options evaluated

| Option | Real tradeoff |
|---|---|
| **Parse the cron expressions** | Honours the documented surface literally. Costs a cron dependency — the fourth direct dependency in a `go.mod` that has three — or a hand-written parser, and brings timezone and DST handling as new surface, for a personal binary that needs exactly two schedules |
| **Typed scalars instead of cron** (`consolidation_hour: 3`) | Avoids the parser and could move the constants into `internal/core`, where §13's calibration gate would finally reach them. But it renames a documented surface and folds two work units into one, and it still exposes numbers whose own documentation says they are bounded from both sides |
| **Retire the keys** ✅ | The schema stops lying. Costs the loss of a configurability that never existed, and requires the load error to explain itself rather than report a typo |

The asymmetric variant — the hour configurable, the interval not — was considered and rejected:
the case it serves, a machine asleep at 03:00, is already served by
[ADR-0009](0009-scheduler-downtime.md)'s boot catch-up, which runs the missed pass on startup.

## Decision

**Retire `schedules.consolidate` and `schedules.proactive_check`.** The `Schedules` type and the
`Config.Schedules` field are removed, and the block is removed from
[`01-architecture.md`](../01-architecture.md)'s example.

**A retired key is not a mistyped one, and the load error says which happened.** `decode.go`
carries a `retiredKeys` table. When a strict decode fails, the document is re-read permissively
and, if it carries a retired key, the error names the key, states the values that are now fixed
(03:00 local, every 5 minutes), points here, and names the one control that does exist:
`consolidation_enabled` in the vault's `config` table, which suppresses the nightly pass and the
boot catch-up together.

That re-read runs **on the error path only**, and it re-reads the document rather than matching
on the decoder's error text — the distinction between a retired key and a typo must not depend on
a dependency's wording.

## Consequences

### What it enables

- The `nooma.yml` schema and `docs/01-architecture.md` describe something true. The L2 gate of
  spec R9.1 (`test/conformance/config_doc_test.go`) compares the two key sets in both directions,
  so this can no longer drift back apart silently.
- A vault carrying the old block gets a sentence that tells it what to delete, what runs instead,
  and where to read why — rather than a caret under a key name.
- The rule this repository already applies to ports — a method with no caller is not shipped —
  now visibly applies to configuration keys too.

### What it costs

- **The schedule cannot be changed.** A user who wants consolidation at 05:00 has no supported
  way to ask for it. ADR-0009's boot catch-up is the answer for a machine that was off, and it is
  not an answer for a machine that is on and busy at 03:00.
- Every retired key needs an entry in `retiredKeys` to keep its explanatory error. A future
  removal that forgets one degrades silently to the generic unknown-key message.
- §13's two calibration rows stay outside the calibration gate's reach, which matches
  `internal/scheduler…`, not `internal/core…`. That is a separate recorded work unit (owner
  ruling 5) and this ADR does not close it.

### Reversal criteria

Real evidence that a fixed 03:00 harms someone whose machine is *awake* at 03:00 — a laptop under
load every night, a shared machine, a user in a timezone the local-hour assumption serves badly.
The ADR that supersedes this one should introduce a typed scalar and move the constant into
`internal/core`, closing §13's gate gap in the same change. It should not introduce a cron parser:
the two schedules this binary runs do not justify the grammar.
