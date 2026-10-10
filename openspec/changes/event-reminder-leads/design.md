# Design — event-reminder-leads

How [ADR-0029](../../../docs/adr/0029-event-reminders-follow-the-users-leads.md) is built. The
ADR is the product decision (client, relayed by the PM on 2026-10-10); this file decides HOW.
There is no `proposal.md`/`spec.md`: the ADR's numbered decision is the requirement list, cited
here as A1-A7.

## 1. Ground truth (read at `30d9f6c`)

| Claim | Where |
|---|---|
| A dated event arms one trigger `EventLeadDays` (7) before, clamped to now | `internal/core/prospection/arm.go` `datedTrigger`, `clampToNow` |
| A correction keeps exactly one armed trigger, decided by `Follow` over `datedTrigger`/`recurringTrigger` | `internal/core/prospection/follow.go`; `internal/brain/correction_reminder.go` |
| A date-only `event_at` decodes to midnight in the clock's zone | `internal/core/classify/decode.go` `assignTime` |
| The text repair of a failed reminder step reads `fire_at + lead_days` | `correction_reminder.go` `followTriggerText` |
| Vault settings live on the `config` singleton, read by `ConfigRepo.Load`, defaulted by core `Resolve*` | `internal/store/sqlite/configrepo.go`; migration 0002 |
| `m4e2-admin` (planned, unbuilt) writes settings through typed setters and logs `config.updated` | `openspec/changes/m4e2-admin/design.md` §3.1 |
| No path expires a unit's triggers on archive | `rg triggers internal/brain` |

## 2. Decisions

**D1. `Arm` returns the set.** `Arm(c, prefs, now) ([]Plan, bool)`: one `Plan` per row to write,
never empty (a refusal is one `ArmNothing` plan). A `Plan` stays one trigger, so every per-row
writer (`arm`, `armedTrigger`, `armRationale`, `moveReminder`) is unchanged in shape. Rejected: a
`Plan` carrying a slice of firings — it moves the loop into every writer.

**D2. `prospection.ReminderPrefs{TimedLeads []time.Duration; DateOnlyAt TimeOfDay}`**, with
`DefaultReminderPrefs()` built from named constants (§13 rows). Arm takes it as a parameter, so
core stays pure and PR 3 only changes where brain gets it from.

**D3. Event firings** (A1-A3): date-only iff `event_at`, in `now`'s location, is 00:00:00.000.
Date-only: the day before at `DateOnlyAt`, built on the wall clock in that zone. Timed:
`event_at - lead` for each lead, absolute durations ("24 hours before" across a DST change is 24
hours). Keep the firings at or after `now` (`clampToNow`'s own boundary), sorted ascending. None
left and the event ahead: one `Immediate` plan at `now`. Event at or before now: `already_past`,
unchanged.

**D4. Lead in the payload.** `TriggerPayload.LeadMinutes` (`lead_minutes`, omitted when 0),
written for one-shot event plans: the nominal gap `About - FireAt` before any clamp, exact for
date-only too. `lead_days` becomes `omitempty` and stays recurring-only. The text repair adds the
candidate `fire_at + lead_minutes`; an `Immediate` plan writes no lead, so its repair still
relies on the unit's previous `event_at` (doc 02's existing residual).

**D5. `Follow` reconciles a set** (A4). `Following{Plans []Plan; Carry []string; Expire
[]string}`, `Carry[i]` the live trigger moved onto `Plans[i]` or `""` to create. Recurring live:
unchanged. Otherwise live (in `(fire_at, id)` order) pairs positionally with the plans in fire
order; surplus live expire; surplus plans are created. **A correction never creates an at-once
reminder**: when the only plan is `Immediate` and nothing is live, the result arms nothing. An
`Immediate` plan carried onto a live trigger already due keeps that trigger's `fire_at`, so a
retry writes nothing. Pure and positional, so the same correction again reaches the same set.

**D6. The reply names every reminder.** `brain.Armed` keeps the earliest firing in
`FireAt`/`Immediate` and adds `Later []time.Time`. `phrase.Set.Lead` decides "the day before" by
calendar day rather than by a 24-48 h gap (a date-only reminder is 15 h before midnight and is
"the day before"), and leads are joined with a new `And` phrase per language. The HTTP and CLI
outputs keep reporting the earliest firing.

**D7. Preferences in the vault** (A5, PR 3). Migration 0006 adds `config.event_reminder_leads
TEXT` (JSON array of minutes, e.g. `[1440,120]`) and `config.date_only_reminder_at TEXT`
(`HH:MM`), both nullable with no default: `NULL` is "never chosen". `VaultConfig` carries the raw
strings; core `ResolveReminderPrefs(leads, at *string) ReminderPrefs` parses and validates
(`ParseEventReminderLeads`, `ParseTimeOfDay`, the A5 bounds as §13 constants), falling back per
field. The future writer validates with the same parsers, m4e2's `Resolve(v) == v` rule.

**D8. Brain reads the preferences at arm time.** `CaptureService.WithReminderPrefs(ports.ConfigRepo)`
(`ActivityService.WithUnits`'s precedent) rather than an 18th constructor parameter that ~35
test call sites would carry; capture and correction call `Load` once per operation, only when a
plan is about to be computed for an event, and a `Load` error fails the capture loudly (no
silent default). Unset, the defaults apply. A wiring test pins that `serve` sets it.

**D9. Re-arm on preference change** (A6) is not built here: it needs the first writer. It is
"for each event unit still ahead with an armed one-shot trigger, run `Follow`", recorded with
the existing `correction.reminder.*` rows' shape under a new action family. Sized in the report.

## 3. Chain (stacked to main)

| PR | Branch | Content | Forecast impl+docs |
|---|---|---|---|
| 1 | `docs/event-reminder-leads-plan` | ADR-0029 (Proposed), this design, tasks | ~190 |
| 2 | `feat/reply-names-every-reminder` | D6: the reply counts the day on the calendar and names every firing it is given | ~60 |
| 3 | `feat/event-reminder-leads` | D1-D5 with the defaults; doc 02 §5/§7/§13, doc 06 I29/I31, doc 07 | ~390 |
| 4 | `feat/reminder-preferences` | D7-D8; migration 0006, schema golden, doc 02 §13, doc 03 | ~250 |

Link 2 lands first so link 3's reminders are announced correctly from the moment they exist.

## 4. Tests (written first, watched red)

- L2 `TestI31_EventRemindersAtTheLeads` (new invariant, doc 06 §4): a timed event 3 days out
  arms two triggers at -24 h and -2 h; one 20 h out arms only -2 h; one 1 h out arms one at once;
  a date-only event arms one at 09:00 the day before.
- L2 `TestI29_CorrectedDateMovesTheReminder` widened: the set follows; a correction whose leads
  are all behind with nothing armed arms nothing; a failed second write is repaired by a retry.
- L1 in `prospection`: firing table (DST day, midnight in another zone is timed, duplicates
  impossible), `Follow` pairing and the at-once rule; PR 3: parser bounds, `Resolve` fallback.
- L3 PR 3: `ConfigRepo.Load` reads both columns; migration golden.

Mutation probes, recorded in each PR body: drop the "skip past leads" filter; flip the
date-only midnight test; let `Follow` create an at-once plan; drop the `fire_at + lead_minutes`
repair candidate; PR 3: accept a 0-minute lead, ignore the stored preference.
