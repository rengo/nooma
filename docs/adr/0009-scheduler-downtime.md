# ADR-0009 — Scheduler semantics under downtime

- **Status**: Accepted
- **Date**: 2026-07-27
- **Supersedes**: —
- **Superseded by**: —
- **Enables**: M2

## Context

This is a **new** problem created by Nooma's topology, and it does not exist in an always-on
service: the binary runs on a laptop that closes, on a Raspberry that reboots, on a machine
that is powered off over the weekend.

Questions that must be answered before writing the scheduler:

- If the 03:00 consolidation did not run because the machine was asleep, what happens at
  startup?
- A trigger due at 10:00 with the machine powering on at 14:00 — does it fire?
- An ephemeral "remind me in 15 minutes" timer that expired eight hours ago — what does it do?

Answering this badly produces the worst possible bug in an assistant: **nudges that arrive late
and out of context**. A "leave at 10" reminder sounding at 14:00 is not a late nudge, it is
noise that teaches the user to ignore notifications. And once they ignore them, the entire
proactive lobe stops being worth anything.

## Options evaluated

| Option | Pro | Con |
|---|---|---|
| Fire everything overdue at startup | Nothing is lost | An avalanche of obsolete nudges. Destroys trust |
| Discard everything overdue | Zero noise | The night's consolidation is lost, along with nudges that did matter |
| Catch-up with a staleness gate per kind | Recovers what is still useful, discards what is not | Thresholds must be chosen and tested |

## Decision

**Catch-up at startup, with a staleness gate and different thresholds per kind of work.** The
general criterion: *background work is recovered, user-facing nudges expire*.

### Consolidation — always recovered

At startup, if `config.consolidation_last_run_at` is more than 24 h old, consolidation is
queued with a **120-second delay** so it does not compete with startup (opening the vault,
migrations, connecting channels).

It is recovered because it is internal work: archiving cold things and connecting related ones
is still correct even if done late. It bothers nobody.

### `time_based` triggers — they expire

At startup and on every `proactive_check`, overdue triggers are evaluated:

- Overdue by **≤ `trigger_staleness_hours` (default 6)** → fires normally, passing through
  quiet hours and the digest/push logic.
- Overdue by **more** → `status = 'expired'`, with a `decision_log` entry explaining it was
  discarded for age. **Never `fired`.**

Expiry is not silent: it stays auditable in the glass box, and the source unit remains alive in
the pool. The nudge was lost, not the memory.

### Ephemeral timers — they expire faster

Timers are infrastructure, not memory ([`../02-cognitive-core.md`](../02-cognitive-core.md)
§8), and their value is purely temporal:

- Overdue by **≤ `timer_staleness_hours` (default 3)** → delivered, and the `rendered_text`
  **mentions the delay explicitly**. "Two hours ago you asked me to remind you to turn off the
  stove" is useful and honest. "Turn off the stove" out of time is dangerous.
- Overdue by **more** → `status = 'cancelled'`, with a note in `decision_log`.

### Quiet hours always win

None of this catch-up skips quiet hours `[00:00, 07:00)` local. A 03:00 startup defers
deliveries until waking. The only exception remains the one already defined for urgent push
during valid hours.

The three thresholds (`120s`, `6h`, `3h`) are calibratable defaults and live in the calibration
table of [`../02-cognitive-core.md`](../02-cognitive-core.md) §13.

## Consequences

### What it enables

- The brain can be powered off without corrupting its behavior. That is a requirement of the
  topology, not a feature.
- The staleness gate is a **pure function** over `(fire_at, now, kind, threshold)`: it gets
  tested entirely without a scheduler, without a real clock, and without a database. The three
  questions in the Context section become three tests.

### What it costs

- Nudges are deliberately lost. A conscious tradeoff: trust in the channel is worth more than
  delivery completeness.
- Three more thresholds to calibrate, with initial values chosen by judgment rather than data.

### Reversal criteria

Real usage showing that 6 h discards nudges the user did want (raise the threshold), or that it
delivers nudges that were already noise (lower it). The mechanism does not change; the numbers
do, and that is what calibratable means.
