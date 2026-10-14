# Design — digest-stale-reminders

A fix change. The client report (2026-10-14) and the PM's read-only evidence are the requirement;
[ADR-0032](../../../docs/adr/0032-a-reminder-says-what-is-true-now.md) (Accepted by the client,
2026-10-14) records the product decisions, cited here as P1-P5. Lines are at `7d2b98a`.

## 1. Evidence (read-only, `~/.nooma/pablo.nooma/nooma.db`, 2026-10-14)

All seven digest rows have `lead_days: 7` and the rationale "firing at once because that horizon
was already behind". Their units' events now: dentist ×2 and one flight on 10-15, one on 10-16,
and three on 10-11/10-12, which are **past**. `480914d7`'s unit text is now "Es a las 08:00.";
`dad3c936` and `4269326a` belong to **one** unit. Each fired minutes after capture with no channel.

## 2. Root causes

**Defect 1 — past events delivered, a backlog dumped.**

- **Pre-ADR-0029 rows, not a live arming bug.** The binary before #317 armed 7 days ahead,
  clamped to now (`internal/core/prospection/arm.go@8ec6576:12, :188, :208`;
  `capture.go@8ec6576:1078`). The client's binary was rebuilt on 10-13, 16:43-16:45Z.
- **Judged once.** The verdict runs only on armed rows (`check.go:116`), and "never late"
  needs `lead_minutes > 0` (`check.go:412-418`). Doc 02 (`:1338-1341`) understates the reach.
- **Fired is not delivered.** No channel (`deliver.go:92-97`; `serve.go:123`; `nooma check`,
  `wiring.go:314`), no conversation (`:101-112`) or a failed send (`:115-119`) leaves the row
  `fired` with `surfaced_at` NULL. No pass retries a push (`deliver.go:21`).
- **The digest sends the backlog ungated** (`digest.go:71, :108, :118, :152`), and Today mirrors
  it (`today.go:145-165`).

**Defect 2 — frozen, raw text.**

- `renderDigest` (`digest.go:341-357`) and `renderPush` (`deliver.go:167-175`) print
  `payload.action`.
- A correction updates armed rows only (`correction_reminder.go:43`), and only when `event_at`
  moved (`:39-41`).
- **Follow-up, its own change:** the 2026-10-10T19:35:53Z correction stored "Es a las 08:00." as
  the unit's text and left its time at 09:00.

**Defect 3 — English.**

- Replies follow the message's language: `capture.go:280` keeps it and `channels/reply.go:35`
  uses it.
- The language is stored only in the `capture.classify` row (`capture.go:764-773`).
- The digest and push sentences are English literals (`digest.go:349, :368, :378, :394-399`;
  `deliver.go:170, :173`). ADR-0022 and doc 02 §5 (`:539-543`) name the gap.

**Defect 4 — two reminders for one unit.** `dad3c936` was moved by a correction, then fired
undelivered. The next correction saw no armed row (`correction_reminder.go:43`) and armed
`4269326a`. Main does the same.

**The push path** has defects 1, 2 and 3. Timers are not affected.

## 3. Decisions

**D1. One gate, `prospection.ReminderVerdict`** (P1), pure:
`(q QuietHours) ReminderVerdict(r Reminder, now) (Verdict, LapseReason)`. `Reminder` is
`{FireAt; About time.Time; FromEvent bool; Current *time.Time; Newer, Waiting bool}`.

1. `superseded` → `Stale`. This holds when `FromEvent` is set and `About` ≠ `Current`, both
   truncated to the second, or when `About` is unknown and `Newer` is set (D8).
2. `About` (else `Current`) at or before `now` → `Stale`/`passed`.
3. `Waiting` (a recurring row waiting for its digest, held rows included) → `Deliver`. ADR-0009's
   window belongs to a push.
4. Otherwise `q.TriggerVerdict(FireAt, now)`, whose `Stale` is `overdue`. Quiet hours still answer
   `Defer` first (`staleness.go:92-94`).

How `About` is resolved:

- one-shot, from the payload `event_at` (`FromEvent`);
- otherwise `fire_at + lead_minutes` when `lead_minutes > 0`;
- recurring: `NextOccurrence(rule, anchor, fire_at)` (`recurrence.go:88`, pure), because
  `armedPayload` writes no `event_at` for `ArmRecurring` (`capture.go:978`);
- otherwise unknown.

The recurring case assumes a period longer than the lead, which is true today: L1 table.
`Current` is the unit's `event_at` (a task unit's `due_at`, §5) via `UnitRepo.ByID`. A nil port
or `ErrUnitNotFound` means unknown. Any other read error aborts the pass.
`EventReminderVerdict` is D1 with `Current` nil, so I34 is unchanged.

**D2. Where it runs.** At fire time (`check.go:412-418`), in the reminder step (D6) and in the
digest, through one brain helper. Today runs it read-only (I27) and counts after it
(`today.go:149`).

**D3. Delivery attempts** (P2). A pass *can deliver* when its channel is non-nil and its
conversation is non-empty.

- **When it can:**
  - every undelivered one-shot row is judged in the reminder step;
  - `Deliver` is pushed (link 5), skipping rows this pass already fired or attempted;
  - only recurring rows reach the digest.
- **Send and record failures:**
  - a `Surface` conflict is skipped, not fatal (`deliver.go:121-126`);
  - if `Send` succeeds and `Surface` then fails, the id joins a set held across passes, so the
    retry skips it until restart;
  - a failing transport writes one `check.delivery.failed` per reminder per outage: an
    in-process set, cleared on success. That is not about 70 rows (one every 5 min for 6 h).
- **Concurrency:** no two passes with a channel run at once. Both commands take the vault lock
  (`cmd/nooma/check.go:67`, `serve.go:84`), and the proactive job is single-flight
  (`scheduler.go:72-108`).
- **When it cannot** (nil channel, `nooma check`, or no conversation):
  - a one-shot row is judged only for `passed`, which expires or lapses it with no re-plan;
  - every other one-shot row stays as it is and writes nothing;
  - no conversation adds one loud `check.delivery.failed` per due reminder per process
    (non-negotiable 7);
  - Today lists the armed one-shot reminders due by `now + 24 h`, read-only, so a UI-only vault
    still sees them (PM).
- **Bound:** at most one armed row per lead per event still ahead, never re-created.
- **I15 changes** (doc 02, ADR-0032 amending ADR-0009), and its row text names it. With no
  destination, an overdue one-shot trigger is left untouched; one with no unit is kept until a
  channel exists. Its test (`i15_…_test.go:168`) gets a channel case and a nil-channel case.

**D4. Lapse.** `TriggerRepo.Lapse(id)` moves a `fired` row with `surfaced_at` NULL to `expired`,
keeping `fired_at`. Doc 03, I15's comment and `due_scan_status_vocabulary_test.go` name the
combination. Each lapse writes `check.trigger.lapsed {id, fire_at, verdict, reason}`, the
reason being `passed`, `superseded`, `overdue` or `covered`.

**D5. Render from the unit** (P4). The message is the unit's current `content`, verbatim, plus
`About` in the clock's zone via `Set.Time`. A date-only event (ADR-0029's midnight test) uses
the new `Set.Date`.

- A date correction rewrites the text's date (`correction/follow.go:170`). A text that still
  disagrees is a defect in the unit (§2).
- `payload.action` and `followTriggerText` (`correction_reminder.go:191-208`) remain, but only as
  the fallback when the unit cannot be read.
- New `phrase.Set` fields in `en`/`es`: `Reminder`, `DigestLine`, `DigestHeaderOne/Many`,
  `OneMore`, `RelationQuestion`, `Late`, `SomethingYouAsked`, `Date`.
- The `fired` rows sent for one unit in one pass become one message, each surfaced. Armed
  siblings are untouched.

**D6. The reminder step: re-plan before lapse** (P3), when the pass can deliver. After the due
loop and **before** the digest:

1. Stale one-shot rows are collected, both armed (`overdue`) and undelivered (`overdue`,
   `superseded`). `passed` rows are retired at once.
2. Each unit with collected rows, whose current instant is ahead, gets
   `CatchUp(eventAt, live, sent, prefs, interrupt, now)` (pure). `live` is the unit's armed
   one-shot rows. `sent` is the unit's `status = 'fired'` rows: delivered, or undelivered and not
   lapsed, so a row still in flight covers its lead.
3. The plan is written first: `FollowSent`'s leads, carrying `live`, through `reminderSet.apply`
   with `checkCause` (`check.reminder.armed/moved/cancelled`).
4. Only then are the collected rows lapsed or expired.

A crash or abort anywhere leaves the stale rows un-lapsed. The next pass recomputes the unit, and
the rows written so far come back as `live`, so it carries them and creates nothing twice. No
candidate is held only in memory. Dry run computes the same plan from the pass's verdicts
(an overlay of its own decisions) and writes nothing.

**Catch-up** (link 4). Every lead is behind, nothing in `sent` is about this instant, and no
catch-up is armed or in flight. It produces **one** at-once plan through ADR-0030's
`deliverAt`: now, or quiet end when that is before the event. Otherwise
`check.reminder.refused` (`quiet_hours`, written by its own writer, not `apply`), and the rows
lapse.

- **Exactly one delivered.** `armCatchUp` creates id `catch-up:<unit>:<About UTC
  RFC3339>:<n>`, where `n` is 1 + the catch-ups already made for that instant, then records
  `armed`. `ErrTriggerExists` (`ports/triggerrepo.go:337`) means another pass took `n`, so
  nothing is recorded. A catch-up that lapses undelivered may be followed by attempt `n+1`
  while the event is ahead. Once one is delivered, it is in `sent`, and none follows.
- `catch_up` (`TriggerPayload.CatchUp`, `payloadJSON`) is cleared by `moveReminder` when the
  instant changes.

In link 3, before the catch-up exists, a unit owed only a catch-up is **held**: its rows are
neither sent nor lapsed until link 4 or `passed`.

**D7. Language on the unit** (P5). A migration adds `units.language TEXT NULL` and backfills it
in one grouped pass. The earliest `capture.classify` row by `rowid` wins
(`decision_log` has no `seq`; `0001_core_tables.sql:95-101`); a later row is a correction's
re-classification:
`WITH f AS (SELECT json_extract(context,'$.unit_id') u, MIN(rowid) r FROM decision_log WHERE
action='capture.classify' GROUP BY 1) UPDATE units SET language =
NULLIF(json_extract(d.context,'$.language'),'') FROM f JOIN decision_log d ON d.rowid = f.r
WHERE f.u = units.id` (SQLite 3.53).

- Capture writes it, empty as NULL. Corrections never change it.
- `UnitRepo.LatestLanguage()` returns the newest unit's non-null language, archived included.
- A push uses its unit's language, else the latest, else the fallback. The digest uses the
  latest, else the fallback.

**D8. Legacy rows** (the client's seven, `About` unknown). An undelivered one is `superseded`
(`Newer`) when its unit has an armed one-shot row. Every such pair is a legacy fired row next to
a lead armed later by a correction or re-arm.

**Common.** `CheckReport` gains `TriggersLapsed`, `RemindersArmed`, `RemindersMoved`,
`RemindersCancelled` and `RemindersRefused` (I12, `cmd/nooma/check.go:122`). The five actions go
into `AllDecisionActions`, doc 02 §11 and `ui/activitylabel.go`.

## 4-5. Client's vault; #337

After link 4 the first check drops all seven. The departed flights send nothing. The dentist gets
its 2-hour lead, and the 10-15 flight its 24-hour lead. The 10-16 flight gets its leads, out of
the night.

#337 takes ADR-0031, migration 0008 and I36. This change takes ADR-0032, the next migration and
I37; whichever merges second renumbers. #337's task reminders must write `event_at`, because D1
reads `due_at` as `Current`, and its quiet-hours exemption sits inside D1's `TriggerVerdict`.

## 6. Chain (stacked to main; each link leaves main consistent)

| PR | Branch | Content | Impl+docs |
|---|---|---|---|
| 2 | `fix/never-remind-of-the-past` | D1 `passed` only, recurring by `NextOccurrence` (no `overdue` change: armed rows keep today's verdict, the digest ignores `overdue`), D4; doc 02 §7/§11, doc 03, I37 | ~320 |
| 3 | `fix/late-reminders-replan` | D1 rest, D8, D6 without the catch-up (held), `sent` = `status = 'fired'` | ~390 |
| 4 | `fix/no-destination-and-catch-up` | D3 cannot-deliver branch, Today's list, catch-up attempts, I15 amended | ~380 |
| 5 | `fix/push-retries-undelivered` | D3 can-deliver branch: push, skips, bounds, recurring-only digest | ~200 |
| 6 | `fix/reminder-renders-the-unit` | D5 | ~260 |
| 7 | `feat/proactive-language` | D7 | ~220 |

## 7. Tests (written first, watched red)

- **L1:**
  - `ReminderVerdict`: every reason; `FromEvent` versus minute leads; second precision;
    `Waiting`; recurring `About` by `NextOccurrence` (the period is longer than the lead);
  - `CatchUp`: every branch, including attempt `n+1` and one already delivered.
- **L2 `TestI37_*`:**
  - link 2: the client's seven rows, so the past is never sent;
  - link 3: D8; the re-plan, then an **abort mid-pass** (a failing `Lapse`) followed by a clean pass
    creates nothing twice; a waiting birthday is carried; dry run equals the real scan;
  - link 4: one catch-up across passes; an undelivered one gets exactly one successor; a nil
    channel touches no row ahead and Today lists them;
  - link 5: one failure row per outage;
  - link 7: Spanish.
- **Kept or widened:** I15 amended, I34 unchanged, I35 widened (a row in flight covers; a lapsed
  one does not), I27 held.
- **L3:** `Lapse`, `status = 'fired'` coverage, the grouped backfill, `LatestLanguage`.
- **Mutation probes, per PR:**
  - drop each reason;
  - `overdue` on waiting rows;
  - lapse before re-plan;
  - drop the attempt number;
  - count a lapsed row as sent;
  - render `payload.action`.
