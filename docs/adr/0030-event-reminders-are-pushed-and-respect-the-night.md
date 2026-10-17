# ADR-0030 — Event reminders are pushed during the day, held through the night, and never late

- **Status**: Accepted
- **Date**: 2026-10-13
- **Supersedes**: —
- **Superseded by**: [ADR-0034](0034-a-recurring-reminder-rings-on-its-day.md), point 6's recurring timing and routing only (a recurring reminder rings on its day and is pushed)
- **Enables**: M4 (the settings page and the chat intent that edit these preferences)

## Context

ADR-0029 arms a dated event's reminders at the user's leads: 24 hours and 2 hours before a
timed event, the day before at 09:00 for a date-only one. It says when a reminder is *armed*,
not how it *arrives*, and doc 02 §7 still decides arrival from the classification's
`interrupt_level`: below 0.7 a due reminder waits for the next morning's digest, and anything
due inside quiet hours `[00:00, 07:00)` waits for 07:00. Two consequences the client rejected:

- A dentist reminder two hours before, captured with a low interrupt level, reaches the user
  in tomorrow's digest — after the appointment.
- A 05:00 flight's 2-hour reminder is due at 03:00, is deferred to 07:00, and arrives two hours
  after the plane left.

The client decided delivery in product terms on 2026-10-13. This ADR fixes the rule, when it is
applied, and where its two times live.

## Options evaluated

**When the quiet-hours adjustment is applied**

| Option | Real tradeoff |
|---|---|
| At delivery: `fire_at` stays the nominal lead, the due scan shifts it | Cannot move a reminder *earlier*: the scan reads triggers whose `fire_at` is already behind, so a 03:00 reminder that should have gone out at 22:00 the evening before is invisible until 03:00. The reply ("I'll remind you at…") and the glass box would also name an instant the reminder never fires at |
| **At arm time: `fire_at` is stored already shifted** ✅ | The stored instant is the true one, so the reply, `decision_log` and `/ui` all say what will happen. A correction re-arms through the same function capture uses (`prospection.Follow`, I29), so a moved date and a fresh capture cannot disagree. A preference change re-arms through that same decision (ADR-0029 point 6). The due scan keeps its quiet-hours deferral as the backstop for a vault that was off |

**Where quiet end and the evening time live**

| Option | Real tradeoff |
|---|---|
| Constants in `internal/core` | Exactly what ADR-0029 ruled out for reminder settings: nothing the user can change |
| **Two nullable columns on the `config` singleton row** ✅ | ADR-0029's own choice for the leads, for the same reasons: one read, one `Resolve*`, and `NULL` means "never chosen" |

## Decision

**An event reminder is pushed when it is due, is never sent during quiet hours, and never
arrives after its event.** An *event reminder* is a one-shot reminder of a dated event (every
trigger without a recurrence rule).

1. **Pushed.** A due event reminder is pushed on the channel at once, whatever its
   `interrupt_level`, a degraded one included. It never waits for the digest. The
   interrupt-level split, and its "a degraded reading never pushes" guarantee, keep governing
   every other trigger.
2. **Quiet hours.** Quiet hours are `[00:00, quiet end)` local, quiet end the user's preference
   (default **07:00**). A reminder whose nominal instant (its lead before the event) falls in
   quiet hours is armed **at that night's quiet end** when that is still before the event.
3. **The evening before.** When the event is at or before that quiet end, the reminder is armed
   instead **the evening before at the user's evening time** (default **22:00**): earlier, never
   later.
4. **Never late.** A reminder whose adjusted instant is already behind the arming instant is
   armed at once when the arming instant is outside quiet hours and before the event. When it is
   not — the capture happens inside the quiet hours that run into the event — no allowed instant
   exists: the reminder is not armed, and a capture records why (`capture.arm.refused`, reason
   `quiet_hours`). At delivery, a reminder whose event has already started is expired with a
   `check.trigger.expired` row instead of sent. Two reminders the adjustment lands on the same
   instant are armed once. The at-once reminder of ADR-0029 point 3 follows the same rule.
5. **Preferences.** `config.quiet_hours_end` and `config.evening_reminder_at`, `HH:MM`,
   nullable; `NULL`, a missing row or an invalid value resolve to the default. Valid: a quiet end
   from `00:01` to `12:00`; an evening time from the quiet end to `23:59`. Both are read at arm
   time by capture and correction, and quiet end also by the due scan, so the scan defers every
   trigger to the user's quiet end and the daily digest is not due before it.
6. **Not changed.** A timer keeps its quiet-hours exemption (an explicit instruction outranks the
   window). A recurring reminder keeps its timing and its interrupt-level routing: it is not an
   event reminder in this sense, and this decision does not need it to be. A task's `due_at`
   still arms nothing.

**Worked examples** (defaults; the conformance test states each one):

| Event | Reminder | Nominal | Delivered | Why |
|---|---|---|---|---|
| Flight 05:00 | 24 h | 05:00 the day before | 07:00 the day before | quiet; quiet end is before the event |
| Flight 05:00 | 2 h | 03:00 | 22:00 the evening before | quiet; the event is before quiet end |
| Flight 08:00 | 2 h | 06:00 | 07:00 | quiet; quiet end is before the event |
| Flight 07:00 | 2 h | 05:00 | 22:00 the evening before | quiet; the event is *at* quiet end |
| Dentist 10:00 | 2 h | 08:00 | 08:00, pushed | daytime |
| Flight 05:00, captured 02:00 | — | — | nothing armed, recorded | no allowed instant before it |

**Vaults with reminders already armed** keep their `fire_at`; they are pushed rather than
digested from this change on, and the next correction of their date re-arms them shifted.

## Consequences

### What it enables

- A reminder reaches the user before the thing it is about, in every case the rule can satisfy,
  and says so when it cannot.
- The settings page and chat intent of M4 edit two more preferences through the same path as the
  leads.

### What it costs

- A low-interrupt event reminder interrupts during the day. That is the client's decision: an
  appointment is not digest material.
- Moving quiet end later than the digest hour delays the digest to quiet end. `digest_hour`
  stays a calibration constant; the user's quiet end only ever pushes it later.
- A change of these preferences reaches already-armed reminders only when ADR-0029 point 6's
  re-arm ships; until then the scan's deferral and the never-late expiry hold the line.

### Reversal criteria

Users reporting that daytime pushes for low-importance events are noise would reopen point 1,
most likely as a per-event or per-lead opt-out rather than a return to interrupt routing.
