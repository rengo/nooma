# Design — event-reminder-delivery

How [ADR-0030](../../../docs/adr/0030-event-reminders-are-pushed-and-respect-the-night.md) is
built. The ADR is the product decision (client, relayed by the PM on 2026-10-13); this file
decides HOW. As for `event-reminder-leads`, the ADR's numbered decision is the requirement list,
cited here as B1-B6. Stacked on `event-reminder-leads` (#314 → #316 → #317 → #318).

## 1. Ground truth (read at `8759165`)

| Claim | Where |
|---|---|
| A dated event's reminders are planned by one function, used by capture and correction alike | `internal/core/prospection/reminders.go` `eventReminders`; `follow.go` `Follow` |
| A due trigger routes push vs digest by `Interrupt.Route()` alone | `internal/brain/deliver.go` `fireAndDeliver` |
| Quiet hours are two constants, `[0, 7)`, read by `InQuietHours`, `DeliverableFrom`, `TriggerVerdict` | `internal/core/prospection/quiethours.go`, `staleness.go` |
| The digest is due at `DigestHour` (7), asserted `>= QuietHoursEndHour` | `internal/core/prospection/digest.go` |
| A one-shot reminder stores `payload.lead_minutes`, `fire_at + lead_minutes` = its event | ADR-0029 D4 |
| A persisted unit whose plan refuses writes no `capture.arm.refused` row | `internal/brain/capture.go` (the `refuseArm` comment) |
| Reminder preferences are nullable `config` columns resolved by `ResolveReminderPrefs` | migration 0006; `prefs.go` |

## 2. Decisions

**D1. Arm time** (ADR-0030, options table). `fire_at` is stored already adjusted; the due scan's
quiet-hours deferral stays as the backstop for a vault that was off.

**D2. `ReminderPrefs` grows `QuietEnd` and `EveningAt`** (`TimeOfDay`). Defaults: quiet end
`QuietHoursEndHour`:00, evening `DefaultEveningReminderHour` (22):00, a new §13 row. Quiet start
stays `QuietHoursStartHour` (0): the client set only the end.

**D3. One adjustment, `deliverAt(nominal, eventAt, now, prefs)`** (B2-B4), applied in
`eventReminders` to every lead still ahead and to the at-once instant:

1. `nominal` outside `[00:00, QuietEnd)` local → `nominal`.
2. Else `q` = that day's quiet end. `q` before the event → `q`.
3. Else the day before at `EveningAt`.
4. The result behind `now` → `now` when `now` is outside quiet hours (it is before the event,
   since the event is ahead); otherwise dropped.

Every result is before the event by construction: step 2 checks it, and steps 1 and 3 are at or
before `nominal`. Plans landing on one instant collapse to one. `LeadMinutes` is
`event - fire_at` of the adjusted instant, so ADR-0029 D4's repair candidate `fire_at +
lead_minutes` still names the event; a plan pulled to `now` stays `Immediate` with no lead.
When nothing is left and something was dropped, the result is `ArmNothing` with the new refusal
`RefusalQuietHours` (`quiet_hours`).

**D4. `Follow` is unchanged in code**: it calls `eventReminders`, so a correction re-arms
adjusted (I29). A `quiet_hours` refusal is `!arms` and expires every live reminder, each
recorded by the existing `correction.reminder.*` rows.

**D5. Capture records the drop.** A persisted unit whose plan is `RefusalQuietHours` writes one
`capture.arm.refused` row (`why`, `unit_id`) and stays `OutcomeStored`. Only this reason: an
`already_past` event is self-explanatory on its own unit, which is why `refuseArm` skips it.

**D6. Push regardless of interrupt** (B1). Core `RouteTrigger(recurring bool, i Interrupt)
Route`: a one-shot trigger is `RoutePush`; a recurring one is `i.Route()`, unchanged.
`fireAndDeliver` (commit and dry run) calls it with `t.RecurrenceRule != nil`.

**D7. Never late at delivery** (B4). Core `EventReminderVerdict(fireAt, eventAt, now)`: `now`
at or after `eventAt` → `VerdictStale`, otherwise `TriggerVerdict`. The scan uses it for a
one-shot trigger with `lead_minutes > 0`, `eventAt = fire_at + lead_minutes`; stale expires
through the existing `check.trigger.expired` row. An at-once reminder (no lead) has no known
event and keeps `TriggerVerdict`: it is due the minute it is armed.

**D8. Preferences in the vault** (B5, link 3). Migration 0007 adds `config.quiet_hours_end` and
`config.evening_reminder_at`, `TEXT` nullable. `ResolveReminderPrefs` takes them; quiet end
valid `00:01`-`12:00`, evening valid from the resolved quiet end to `23:59`, else its default.

**D9. The scan reads quiet end** (B5, link 3). A `QuietHours{End TimeOfDay}` value with
`Contains`/`DeliverableFrom` and the methods `TriggerVerdict`, `EventReminderVerdict` and
`DigestDue`; the package functions of the same names keep their one-night signatures as
`DefaultQuietHours()`'s, so I16 (which pins `InQuietHours`' one-parameter shape) and every
existing caller are untouched. `DigestDue` is due at the later of `DigestHour` and quiet end.
`CheckService.WithQuietHours(ports.ConfigRepo)` reads `Load` once per scan into the runner's
`night`, like `WithReminderPrefs`; unset, the defaults. `deliverAt` uses the same `QuietHours`
so the arm-time window and the scan's cannot drift apart. Both `nooma check` and serve's pass
are wired with it, so a manual scan never fires what serve would still hold.

## 3. Chain (stacked)

| PR | Branch | Content | Forecast impl+docs |
|---|---|---|---|
| 1 | `docs/event-reminder-delivery-plan` | ADR-0029 → Accepted, ADR-0030 (Accepted), this design, tasks | ~170 |
| 2 | `feat/event-reminder-delivery` | D1-D7 at the defaults; doc 02 §7/§13, doc 06 I34, doc 07 | ~300 |
| 3 | `feat/quiet-hours-preferences` | D8-D9; migration 0007, schema golden, doc 02 §7/§13, doc 03 | ~250 |

## 4. Tests (written first, watched red)

- L2 `TestI34_EventRemindersArePushedAndNeverLate` (new invariant, doc 06 §4): ADR-0030's worked
  examples table, row by row, through capture; a low-interrupt and a degraded event reminder are
  pushed when due; a reminder whose event has started is expired, not sent.
- L2 I29 widened: a correction onto a 05:00 instant re-arms at 07:00 and 22:00 the day before.
- L1: `deliverAt` table (each step, both boundaries, a zone other than UTC); collapse;
  `RouteTrigger`; `EventReminderVerdict`; link 3: parsers, `Resolve` fallback, `QuietHours`,
  `DigestDue` with a later quiet end.

Mutation probes, recorded in each PR body: make step 2's check `!q.After(event)`; drop step 3;
pull to now inside quiet hours; route one-shot by interrupt; drop the never-late check; link 3:
ignore the stored quiet end in the scan.
