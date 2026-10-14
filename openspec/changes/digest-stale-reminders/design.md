# Design — digest-stale-reminders

A fix change. The client report (2026-10-14) and the PM's read-only evidence are the requirement;
[ADR-0032](../../../docs/adr/0032-a-reminder-says-what-is-true-now.md) (Accepted by the client,
2026-10-14) records the product decisions, cited here as P1-P5. Lines are at `7d2b98a`.

## 1. What happened on the client's vault

Read-only queries of `~/.nooma/pablo.nooma/nooma.db`, 2026-10-14:

| Trigger | Payload | Unit's event now | In the digest |
|---|---|---|---|
| `aaf29508` | `lead_days: 7`, "firing at once because that horizon was already behind" | 10-15 14:00Z | "dentista el viernes a las 10" |
| `cc80398a` | same shape | 10-15 14:00Z | "Tengo una cita con el dentista…" |
| `480914d7` | same; `action` "…10-11 a las 09:00." | 10-11 12:00Z (**past**) | frozen text; unit text is now "Es a las 08:00." |
| `dad3c936` | same; moved by a correction to "…10-12 a las 08:00." | 10-12 13:00Z (**past**) | frozen text |
| `4269326a` | same; armed by a later correction of the **same unit** | 10-12 13:00Z (**past**) | "…a París…10:00." (duplicate unit) |
| `de28990a`, `17152cfa` | same shape | 10-15, 10-16 | raw sentences |

Each fired minutes after capture, with no channel, and waited for the first digest.

## 2. Root causes

### Defect 1 — past events delivered; a backlog dumped

- **Why `fire_at` is minutes after capture: pre-ADR-0029 rows, not a live bug.** All seven carry
  `lead_days: 7` and no `lead_minutes`/`event_at`: the binary before #317 armed one trigger
  `EventLeadDays` (7) before the event and clamped it to now (`arm.go@8ec6576:12, :188, :208`;
  rationale text `capture.go@8ec6576:1078`). Every event less than 7 days out fired at once. The
  client's binary was rebuilt on 10-13 between 16:43Z and 16:45Z; later rows sit at the leads.
- **A trigger is judged once.** `check.go:116` runs the verdict only on `Due` (armed) rows. The
  "never late" check (`check.go:412-418`) applies only to a one-shot row with `lead_minutes > 0`,
  so neither a legacy row nor an at-once row is ever checked against its event. Doc 02 §7 states
  this exception and is wrong about its reach (`docs/02-cognitive-core.md:1338-1341`: "only a vault
  that is off between arming and the event" — a vault without a channel is on).
- **Fired is not delivered, and nothing re-judges an undelivered row.** With no channel
  (`deliver.go:92-97`; serve with Telegram disabled, `serve.go:123`; `nooma check`, which always
  passes a nil channel, `wiring.go:314`) or no conversation (`deliver.go:101-112`) or a failed send
  (`deliver.go:115-119`), a one-shot trigger stays `fired` with `surfaced_at` NULL.
- **The only reader of that backlog is the digest, and it has no gate.** `digest.go:71` reads
  `Undelivered`, `:108`/`:118` rank all of it, `:152` sends all of it. `deliver.go:21`'s "a later
  pass picks it up" means tomorrow's digest: no pass retries a push. `today.go:145-165` mirrors the
  same list on `/ui`.

### Defect 2 — frozen, raw text

- `renderDigest` (`digest.go:341-357`) and `renderPush` (`deliver.go:167-175`) print
  `payload.action`, written at arm time. A correction rewrites the text of **armed** triggers only
  (`correction_reminder.go:43`, `ArmedForUnit`); a fired-undelivered one keeps the old text.
  A correction that changes only the text touches no trigger at all, still on main
  (`correction_reminder.go:39-41` returns unless `event_at` moved). The content correction at
  19:35:53 therefore never reached `480914d7`, which fired at 19:37:46. Nor is it framed as a
  reminder: the line is the user's capture sentence.

**Follow-up, its own change** (client, 2026-10-14): the 2026-10-10T19:35:53Z correction stored
its own sentence "Es a las 08:00." as the unit's content and left `event_at` at 09:00 local.
Rendering from the unit (D5) will show that broken unit faithfully.

### Defect 3 — English proactive messages

- Chat replies: the classifier returns `language` per message (ADR-0022), `capture.go:280` keeps
  it for the reply, `channels/reply.go:35` renders with `phrase.For(result.Language)`.
- It is never stored on the unit: `capture.go:764-773` writes it only into the
  `capture.classify` row. A proactive message has no message to read, so the digest header
  (`digest.go:349`, `plural` `:394-399`), "One more —" (`:368`), the question (`:378`), the push
  fallback and the late caveat (`deliver.go:170, :173`) are English literals outside `phrase`.
  ADR-0022 "What it costs" and doc 02 §5 (`:539-543`) record the gap.

### Defect 4 — two reminders for one unit

- Not a re-arm leaving an armed trigger alive. `dad3c936` was **moved** by the 20:26:12
  correction, then **fired** at 20:30:11 (undelivered). The 21:04:50 correction saw only armed
  rows (`correction_reminder.go:43`), found none, and armed `4269326a`. Main does the same;
  `fix-correction-date-follows` called fired rows "history", but an undelivered one is not.

### The push path

Same defects: frozen text and English literals in `renderPush`, a failed or channel-less push
falling to the ungated digest. Timers are not affected (noted, not changed).

## 3. Decisions

**D1. One gate, `prospection.ReminderVerdict`** (P1), pure:
`(q QuietHours) ReminderVerdict(r Reminder, now) (Verdict, LapseReason)`, where `Reminder` is
`{FireAt; About time.Time (zero = unknown); Current *time.Time; Recurring, Waiting bool}`.

1. One-shot, `About` and `Current` known, different after truncating both to the second →
   `Stale`/`superseded`.
2. `About` (one-shot: else `Current`) at or before `now` → `Stale`/`passed`.
3. `Waiting` (a recurring row waiting for its digest) → `Deliver`. ADR-0009's window is a
   push's; a birthday fired at 14:05 waits for tomorrow's 08:00 digest, as do rows the
   low-energy gate holds (`digest.go:197`).
4. Else `q.TriggerVerdict(FireAt, now)`; its `Stale` is `overdue`. Quiet hours still answer
   `Defer` before staleness (`staleness.go:92-94`).

`About` comes from the payload. It is `event_at` when present, else `fire_at + lead_minutes` when
`lead_minutes > 0`, else unknown. `fire_at + lead_days` is never used: a clamped recurring row's
sum is not its occurrence, so a recurring row without `event_at` is never `passed`. `Current` is
the unit's `event_at` (`due_at` for a task unit, §5), read with `UnitRepo.ByID`. A nil `units`
port or `ErrUnitNotFound` gives `Current` nil; any other read error aborts the pass like every
repository error. `EventReminderVerdict` becomes D1 with `Current` nil, so I34 is unchanged.

**D2. Where the gate runs.** At fire time (replacing `triggerVerdict`, `check.go:412-418`), in
the push retry (D3) and in the digest. Today's mirror runs it read-only (I27): it hides what the
digest would drop, and its counts (`today.go:149`) are taken after the gate. Everything goes
through one brain helper, so the paths cannot disagree.

**D3. Undelivered one-shot rows are retried by push** (P2). After the due loop, the scan reads
`Undelivered` and judges each one-shot row: `Deliver` sends it, `Stale` lapses it (D4), and
anything else is left. Rows this pass already fired or attempted are skipped, so there is one
send and one failure row per pass. If `Send` succeeds and `Surface` then fails, the id goes into
a set the `CheckService` keeps across passes, and the retry skips it until restart. That bounds
the resend to once per process. A `Surface` conflict (`ErrTriggerStatusConflict`) means another
pass recorded the send: it is skipped, not fatal (`deliver.go:121-126` aborts today). Only
recurring rows reach the digest, so one-shot rows are never listed in a digest or on Today.
Lapsing a `passed` recurring row in the digest pass keeps it out of `Undelivered`.

No two passes with a channel run at once: both commands take the vault lock
(`cmd/nooma/check.go:67`, `serve.go:84`), the proactive job is single-flight
(`scheduler.go:72-108`), and `nooma check` has no channel. The conflict handling is defence.

**D3a. A nil channel judges only the past** (P3). With no channel (serve without Telegram,
`nooma check`), a one-shot row that is `passed` or `superseded` is expired or lapsed. Every other
one-shot row is left untouched and writes no row. This keeps D6 reachable when a channel appears
days later. A channel with no conversation keeps today's loud `check.delivery.failed` at fire
time (`deliver.go:101-112`, non-negotiable 7), and its retry is skipped silently, so that is one
row per reminder. The pile is bounded: armed rows for events still ahead, at most one per lead,
never re-created. This changes I15 for one-shot rows. Its test passes a nil channel
(`i15_trigger_expires_not_fires_test.go:168`), so it gets a channel and a nil-channel case,
amended through doc 02 §7 and ADR-0032 (ADR-0009 amended).

**D4. Lapse is a state transition.** `TriggerRepo.Lapse(id)` moves a `fired` row with
`surfaced_at` NULL to `expired`, keeping `fired_at`. The combination `expired` + `fired_at` means
"fired, never delivered". Doc 03, I15's comment and the status-vocabulary test
(`due_scan_status_vocabulary_test.go`) name it. One `check.trigger.lapsed` row records
`{id, fire_at, verdict, reason}`. Reasons are `passed`, `superseded`, `overdue` and `covered`. No
migration.

**D5. Render from the unit** (P4). The line is the unit's current `content`, verbatim, plus
`About` in the scan clock's zone, through `Set.Time`. A date-only event (midnight local, ADR-0029's
test) renders with a new `Set.Date` and no time. The content is the user's words. A date
correction rewrites the date in it (`correction/follow.go:170`), so the two agree.
When they do not, the unit is wrong (the follow-up in §2), and the instant shown is the truth
being reminded of. `payload.action` and `followTriggerText` (`correction_reminder.go:191-208`)
stay as they are, but only as the fallback when the unit cannot be read. New `phrase.Set` fields
in `en`/`es`: `Reminder`, `DigestLine`, `DigestHeaderOne/Many` (no "for today"), `OneMore`,
`RelationQuestion`, `Late`, `SomethingYouAsked`, `Date`. The rows being sent for one unit in one
pass (all `fired`) become one message, and each is surfaced. Armed siblings are not touched.

**D6. Re-plan once per event** (P3). A one-shot row that goes stale as `overdue` (armed rows
expired, fired rows lapsed) or as `superseded` adds its unit to the pass's re-plan set. `passed`
never adds it. After both loops, the scan acts once per unit if all of these hold:

- the unit is an event whose current instant is ahead;
- it has no armed one-shot row;
- it has no undelivered row the gate would still `Deliver` or `Defer`.

It then calls the new pure `prospection.CatchUp(eventAt, sent, prefs, interrupt, now)`:

1. Leads still ahead are planned as `FollowSent(eventAt, nil, sent, …)` plans them.
2. If none is ahead and nothing in `sent` is about this instant, it plans **one** at-once
   reminder through ADR-0030's `deliverAt`. That gives `now` outside quiet hours, or quiet end
   when quiet end comes before the event; otherwise the result is `ArmNothing`/`quiet_hours`,
   recorded as `check.reminder.refused`. Only a `superseded` lapse can reach the quiet-hours
   branches from the scan. An `overdue` row is `Defer`red there and never goes stale at night.
3. Otherwise nothing is planned.

Leads go through the existing `reminderSet.apply` with a new `checkCause`. The at-once catch-up
does not: `armReminder` records before `Create` and draws `ids.New()`
(`correction_reminder.go:221-229`). So a dedicated `armCatchUp` creates the row with id
`catch-up:<unit_id>:<About in UTC, RFC3339>` and payload `catch_up: true`. Only after `Create`
succeeds does it record `check.reminder.armed`. The existing `ports.ErrTriggerExists`
(`ports/triggerrepo.go:234, :337`; sqlite `:131`, memrepo `:60`, repo contract `:134`) means
already claimed: nothing is recorded and nothing is sent. The residual is that a crash between
`Create` and the record leaves the row without its trail row. Its payload rationale still says
why.

- **One per event and instant, ever.** That covers several lapsed leads, several passes and
  retries. A catch-up that lapses is never re-planned (`catch_up` is checked). Neither is one
  that ends undelivered: it had ADR-0009's push window. An event corrected away and back to the
  same instant gets no second catch-up, but its leads still ahead are armed.
- **The flag is carried.** `moveReminder` keeps `catch_up` when a correction moves the row.
  `TriggerPayload.CatchUp` and `payloadJSON` encode it.
- **After it goes out,** the unit's other undelivered rows lapse as `covered`.
- **"Sent" means delivered.** `FiredForUnit` (`sqlite/triggerrepo.go:261-265`) adds
  `surfaced_at IS NOT NULL`, so a lapsed row never covers a lead, here or in I35's re-arm.

**D7. Language on the unit** (P5). A migration adds `units.language TEXT NULL` and backfills it
from the **earliest** `capture.classify` row of each unit. Later rows re-classify corrections:
`UPDATE units SET language = (SELECT NULLIF(json_extract(context,'$.language'),'') FROM
decision_log WHERE action='capture.classify' AND json_extract(context,'$.unit_id')=units.id
ORDER BY occurred_at, seq LIMIT 1)`. Capture writes it, empty as NULL, and a correction never
changes it. `UnitRepo.LatestLanguage()` returns the non-null language of the newest unit by
`created_at`, archived units included: it is the person's language, not the unit's state. A
push uses its unit's language, else `LatestLanguage`, else the fallback. The digest uses
`LatestLanguage`, else the fallback.

**Dry run** reaches every verdict and the re-plan, writes nothing and counts the same effects.
`CheckReport` gains `TriggersLapsed`/`RemindersArmed`/`RemindersRefused` (I12 arithmetic,
`cmd/nooma/check.go:122`). The three new actions go into `AllDecisionActions`, doc 02 §11 and
`ui/activitylabel.go`.

## 4. Behaviour in plain terms

ADR-0032's decision, applied to the client's vault: the first check after deploy drops all seven.
The departed flights send nothing. The dentist gets its 2-hour reminder, the 10-15 flight its
24-hour one, and the 10-16 flight its leads, moved out of the night.

## 5. Interactions with remind-me-reminders (#337)

#337 takes ADR-0031, migration 0008 and I36. This change takes ADR-0032, the next migration and
I37. Whichever merges second renumbers. Its task reminders must write `event_at` (D1 reads
`due_at` as `Current`), and its quiet-hours exemption must live inside D1's `TriggerVerdict`.

## 6. Chain (stacked to main; every link leaves main consistent)

| PR | Branch | Content | Impl+docs |
|---|---|---|---|
| 1 | `plan/digest-stale-reminders` | ADR-0032, this design, tasks | this PR |
| 2 | `fix/never-remind-of-the-past` | D1 `passed` only, D4, `FiredForUnit`; the gate at fire time, in the digest and Today; doc 02 §7/§11, doc 03, doc 06 I37 | ~300 |
| 3 | `fix/late-reminders-replan` | D1 `superseded`/`overdue`, D3a, D6 with the catch-up, I15 amended; doc 02 §5/§7/§11 | ~380 |
| 4 | `fix/push-retries-undelivered` | D3: the retry, the one-shot rows out of the digest, the conflict and the sent-set | ~170 |
| 5 | `fix/reminder-renders-the-unit` | D5 | ~260 |
| 6 | `feat/proactive-language` | D7; migration, golden, doc 02 §5, doc 03 | ~220 |

Link 2 lapses only the past; link 3 brings `overdue` and the re-plan together; until link 4 the
gated digest stays the re-attempt path.

## 7. Tests (written first, watched red)

- L1 `ReminderVerdict`: each reason; second-precision `superseded`; `Waiting`; `About`
  resolution with a zero lead and with a clamped recurring row. L1 `CatchUp`: every branch.
- L2 `TestI37_*` (link 2): the client's seven rows: the past is never sent, the four ahead stay.
  Link 3: they lapse and re-plan as §4 says; a superseded fired row re-plans the new instant;
  two lapsed leads in one pass, then three more passes, give one catch-up; a viable sibling
  blocks it; a nil channel touches no row ahead; a recurring row waiting 18 h is carried. Link 4:
  a failed push is retried once per pass.
- L2: I15 amended, I34 unchanged, I35 widened, I27 held. L3: `Lapse`, backfill, `LatestLanguage`.
- Mutation probes, per PR: drop each reason; `overdue` on waiting rows; a random catch-up id;
  re-plan a lapsed catch-up; count an undelivered row as sent; render `payload.action`.
