# Design — digest-stale-reminders

A fix change. The client report (2026-10-14) and the PM's read-only evidence are the requirement;
[ADR-0032](../../../docs/adr/0032-a-reminder-says-what-is-true-now.md) (Proposed) records the
product decisions, cited here as P1-P5. There is no `proposal.md`/`spec.md` yet: the PM owns
them. Line numbers are at `efa0a1c`.

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

Every one fired minutes after capture, through a vault with no channel, and waited in
`Undelivered` until the first digest after Telegram was enabled.

## 2. Root causes

### Defect 1 — past events delivered; a backlog dumped

- **Why `fire_at` is minutes after capture: pre-ADR-0029 rows, not a live bug.** All seven carry
  `lead_days: 7` and no `lead_minutes`/`event_at`: the binary before #317 armed one trigger
  `EventLeadDays` (7) before the event and clamped it to now (`arm.go@8ec6576:12, :188, :208`;
  rationale text `capture.go@8ec6576:1078`). Every event less than 7 days out fired at once. The
  client's binary was rebuilt between 16:43:03Z and 16:45:54Z on 10-13: the rows armed after carry
  `lead_minutes`, `event_at` and `user_leads_minutes` and sit at the user's leads. Main arms
  correctly (I33/I34).
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
- **Why it came at 11:51 local, not 07:00**: no digest had ever been sent, so `DigestDue` was
  already true at the first pass with a channel.

### Defect 2 — frozen, raw text

- `renderDigest` (`digest.go:341-357`) and `renderPush` (`deliver.go:167-175`) print
  `payload.action`, written at arm time. A correction rewrites the text of **armed** triggers only
  (`correction_reminder.go:43`, `ArmedForUnit`); a fired-undelivered one keeps the old text.
  A correction that changes only the text touches no trigger at all, still on main
  (`correction_reminder.go:39-41` returns unless `event_at` moved). The content correction at
  19:35:53 therefore never reached `480914d7`, which fired at 19:37:46.
- Nothing frames it as a reminder: the line is the user's capture sentence.

**Separate defect, not in this change (flag to PM):** the correction at 2026-10-10T19:35:53Z wrote
the correction sentence itself as the unit's content (`previous` "Tengo un vuelo el 2026-10-11 a
las 09:00.", `next` "Es a las 08:00.") and left `event_at` at 09:00 local. That is the correction
extraction, after `fix-unit-correction-form` and `fix-correction-date-follows` merged. Rendering
from the unit (D5) will faithfully show that broken unit; the unit is what needs fixing.

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
  correction, then **fired** at 20:30:11 (undelivered). The 21:04:50 correction looked only at
  armed rows (`correction_reminder.go:43`), found none, and armed `4269326a`. Both waited in
  `Undelivered`. On main the same happens: `Follow` sees only armed rows, and a fired-undelivered
  one keeps its old date and old text.
- `fix-correction-date-follows` put fired triggers out of scope as "history"; one that never
  reached the user is not history.

### The push path

Same defects: `renderPush` uses the frozen text and English literals; a push that fails or has no
channel is never retried by push and falls to the ungated digest; an at-once row is never
checked against its event. Timers are not affected by 1, 2 or 4 (a timer fired without a channel
is lost, not piled up — noted, not changed); their text is already re-worded at delivery.

## 3. Decisions

**D1. One gate, `prospection.ReminderVerdict`** (P1). Pure:

```go
type Reminder struct {
    FireAt    time.Time
    About     time.Time  // the instant it reminds of; zero when unknown
    Current   *time.Time // the unit's instant now; nil when not an event/task or unknown
    Recurring bool
}
func (q QuietHours) ReminderVerdict(r Reminder, now time.Time) (Verdict, LapseReason)
```

In order: one-shot with `About` and `Current` both known and different → `Stale`/`superseded`;
`About` (else `Current`) at or before `now` → `Stale`/`passed`; else `q.TriggerVerdict(FireAt,
now)` (`Stale` there is `overdue`). `About` is resolved in brain from the payload: `event_at`,
else `fire_at + lead_minutes`, else `fire_at + lead_days` days for a recurring row, else the
unit's current instant (legacy at-once rows). `EventReminderVerdict` becomes this with `Current`
nil, so I34 holds unchanged. The scan reads the unit of a one-shot trigger (`UnitRepo.ByID`)
before judging it. remind-me-reminders' task reminders use `due_at` as `Current` (§5).

**D2. The gate runs on every delivery attempt.** At fire time (replacing `triggerVerdict`), on
the push retry (D3), in the digest and in Today's mirror, through one brain helper
`deliverable(ctx, []DueTrigger, now)` so the four cannot disagree.

**D3. Undelivered one-shot reminders are retried by push** (P2). After the due loop, the scan
reads `Undelivered`; for each one-shot row: `Deliver` → `deliver()` (send, then `Surface`);
`Defer`/`Pending` → nothing; `Stale` → lapse (D4). No destination → nothing and no row (the
existing `deliver.go:92` posture). The digest carries only recurring rows. A transport outage
writes one `check.delivery.failed` per pass per reminder, bounded by the 6 h window.

**D4. Lapse is a state transition.** New `TriggerRepo.Lapse(id)`: `fired` with `surfaced_at`
NULL → `expired`, `fired_at` kept (it fired; it was never delivered). One row
`check.trigger.lapsed` with `{id, fire_at, verdict: stale, reason}`. Nothing is deleted. No
migration: `status` has no CHECK and `expired` already exists.

**D5. Render from the unit** (P4). `phrase.Set` gains `Reminder` ("Reminder: %s — %s" /
"Recordatorio: %s — %s"), `DigestLine` ("%s — %s"), `DigestHeaderOne/Many` ("Coming up:" /
"Lo que viene:" family, no "for today"), `OneMore`, `RelationQuestion` (two %s), `Late`,
`SomethingYouAsked`. The text is the unit's current `content`; the instant is `About`, in the
scan clock's zone, through `Set.Time`. A missing unit falls back to `payload.action`. Due rows of
one unit collapse to one line or one push, and every one of them is surfaced by that send.
Today's mirror uses the same line builder.

**D6. Re-plan after a lapse** (P3). For a lapsed one-shot row whose unit is an event still ahead,
not superseded, with no armed one-shot trigger: `prospection.FollowSent(eventAt, nil, sent, prefs,
level, now)` and the existing `reminderSet.apply` with a new `checkCause` (`check.reminder.armed`;
moved/cancelled cannot happen with no live row). `FollowSent` never creates an at-once reminder.
**"Sent" means delivered**: `FiredForUnit` (`triggerrepo.go:264`) adds `surfaced_at IS NOT
NULL`, so an undelivered row no longer covers a lead in the preference re-arm either (I35's "what
went out is never sent again" keeps its meaning; what never went out is no longer counted).

**D7. Language on the unit** (P5). Migration adds `units.language TEXT NULL`, backfilled with
`json_extract(context,'$.language')` from each unit's `capture.classify` row. Capture writes it
(empty → NULL). `UnitRepo.LatestLanguage()` reads the newest unit's non-null language. A push
renders with `phrase.For(unit.language)`; the digest with `LatestLanguage`; unknown → fallback.
Corrections do not change it.

## 4. Behaviour in plain terms

- A reminder about something that already happened is never sent, however it got stuck.
- A reminder that could not go out is tried again on the next check while it is still on time.
  Once it is late it is dropped, and the event gets the reminders the user's leads still have
  ahead. On the client's vault, the first check after deploy drops all seven: the three flights
  that departed send nothing. Tomorrow's dentist gets its 2-hour reminder. The 10-15 flight gets
  its 24-hour reminder 9 minutes later (the 16:45 duplicate unit already has its own). The 10-16
  flight gets its leads, moved out of the night.
- A reminder reads "Recordatorio: Tengo una cita con el dentista… — jue 15 oct, 11:00", using the
  unit's text and time as they are now. One unit gives one line.
- The digest and every reminder speak Spanish to a user who writes in Spanish.

## 5. Interactions with remind-me-reminders (#337)

- Numbering: #337 takes ADR-0031, migration 0008 and I36; this change takes ADR-0032, the next
  migration and I37. Whichever merges second renumbers. Both add a `units` column, so both touch
  the schema golden.
- Its task reminders must write `event_at` (the `About`) in the payload. Otherwise a legacy-style
  row resolves through `Current`, which D1 reads as `due_at` for a task unit.
- Its quiet-hours exemption for asked reminders must live in the verdict D1 calls. If it does not,
  the retry and the fire-time paths disagree.
- Both add `phrase.Set` fields. That is a textual conflict only.

## 6. Chain (stacked to main)

| PR | Branch | Content | Forecast impl+docs |
|---|---|---|---|
| 1 | `plan/digest-stale-reminders` | ADR-0032 (Proposed), this design, tasks | ~260 |
| 2 | `fix/undelivered-reminder-gate` | D1-D4; doc 02 §7/§11, doc 03 `triggers`, doc 06 I37, doc 07 | ~330 |
| 3 | `fix/lapsed-reminder-replans` | D6; doc 02 §7 | ~150 |
| 4 | `fix/reminder-renders-the-unit` | D5 with both languages' sentences, rendered in the fallback | ~260 |
| 5 | `feat/proactive-language` | D7; migration, schema golden, doc 02 §5, doc 03 `units` | ~220 |

Link 2 is what stops a stale reminder from being sent. Links 4-5 need ADR-0032 accepted.

## 7. Tests (written first, watched red)

- L2 `TestI37_AnUndeliveredReminderIsJudgedAgain`: the client's seven rows, as fixtures in
  memrepo at 2026-10-14T14:51:36Z with a channel. Nothing past is sent. Each row lapses with its
  reason. The superseded row lapses even with its event ahead. A row within its window is pushed
  on the next pass, not by the digest.
- L2 I34 kept green unchanged. I35 widened: a fired, undelivered row covers no lead.
- L1: `ReminderVerdict` table (superseded, passed, overdue, defer, boundaries, legacy `About`
  resolution); `phrase` completeness keeps both languages total.
- L3: `Lapse` precondition in the repo contract; the migration backfill; `LatestLanguage`.
- Link 3, L2: a lapsed reminder for an event ahead arms its leads still ahead, never one at once.
- Link 5, L2: a Spanish unit's reminder and a Spanish-last vault's digest render in Spanish.

Mutation probes, recorded in each PR body: drop the gate from the digest; drop the superseded
check; let the retry fall to the digest; count an undelivered row as sent; render
`payload.action`; ignore the unit's language.
