# Design — ui-preferences

The user changes their reminder and quiet-hours preferences from a Preferences page in the UI.
The rules are ADR-0029 (leads, date-only time) and ADR-0030 (quiet end, evening time), both
Accepted. The brief came from the PM on 2026-10-13. The client confirmed ADR-0029 point 6: a
preference change re-arms the armed one-shot reminders of every event still ahead. This page is
the "first writer" that point 6 ships with. The Telegram chat intent comes later.

## 1. Ground truth

| Claim | Where |
|---|---|
| Four nullable `config` columns: `event_reminder_leads` (JSON minutes), `date_only_reminder_at`, `quiet_hours_end`, `evening_reminder_at` (`HH:MM`) | migrations 0006, 0007; `ports.VaultConfig` |
| Defaults 24 h + 2 h, 09:00, 07:00, 22:00. Quiet hours *start* at 00:00 and are not a preference | `prospection.DefaultReminderPrefs`, `QuietHoursStartHour` |
| Validation: 1-5 leads, 1 min to 30 days each, no duplicates; time `00:00`-`23:59`; quiet end `00:01`-`12:00`; evening from the quiet end to `23:59` | `prospection.Parse*` |
| Capture and correction read the preferences at arm time, so new values reach new arms with no code | `brain.reminderPrefs` |
| A correction re-arms a unit's set through `prospection.Follow` with `correction.reminder.*` rows | `brain/correction_reminder.go` |
| `ConfigRepo` has no setter; m4e2 (unbuilt) plans `config.updated` shaped `{fields, previous, next}` | `ports/configrepo.go`; m4e2 design §3.1 |
| `QuietHours.DigestDue` sent a second digest when the quiet end moved past the hour today's digest went out | `prospection/digest.go` |

No migration and no ADR. Doc 02 §7 and §11 change, doc 06 §4 gains I35, doc 07 gains the page.

## 2. Decisions

**D1. One digest per local date.** `DigestDue` checks the last send against the start of today's
local date instead of today's due time. ADR-0030 says a later quiet end *delays* the digest. It
never says the digest is sent twice. (PR 1.)

**D2. One port setter, `ConfigRepo.SetReminderPrefs(ctx, ports.ReminderPrefsUpdate, at)`.** The
update holds four `*string` fields, and nil leaves a column as it is. It is one lazy UPSERT with
`COALESCE(excluded.col, col)` per column, so the four values are written atomically. The evening
time is validated against the quiet end, so the two must land together. m4e2's
one-setter-per-field rule is for independent knobs. These four are one form with a cross-field
rule, and that is why there is one setter.

**D3. Only changed fields are written.** A field counts as changed when the submitted value
differs from the value **in force** (m4e2 §3.1, finding 6), or when the stored value is corrupt.
Submitting the defaults on a vault that never chose any writes nothing. `NULL` keeps meaning
"never chosen" (ADR-0029). Leads are compared as a set and stored in descending order. The
order the user typed them in changes nothing.

**D4. `brain.PreferencesService`**, with `Preferences(ctx)` (the values in force) and
`Save(ctx, Preferences) (SaveResult, error)`. `Save` validates every field with the core parsers
before any write. The evening time is judged against the *submitted* quiet end. Each invalid
field is reported in `*PreferencesError`, which wraps `prospection.ErrInvalidReminderPref`, and
nothing is written. Every field is judged in the same save, so a refusal names all the wrong
ones at once, a malformed evening included when the quiet end is wrong too. The order then follows m4e2 §3.1: write, then record `config.updated` with
`{fields, previous, next}`, where previous and next are the values in force. Leads are recorded
as readable text ("24 hours, 2 hours"), so `/ui/activity` shows them without a template. If the
log write fails after the config write landed, the error is `*WriteLandedError{Record: true}`.
`LeadUnit` is a closed set: `ParseLeadUnit` refuses any other unit, and `Minutes()` of a unit
outside it is 0, a lead core always refuses, so an unknown unit can never be saved as minutes.

**D5. Re-arm (ADR-0029 point 6).** After a valid save, `Save` walks
`TriggerRepo.ArmedUnitIDs` (new read: distinct `unit_id` of armed clocked triggers). For each
unit it reads `UnitRepo.ByID`, which includes archived units, because their triggers still fire.
It keeps the unit if it is an `event` with `event_at` after now and none of its armed triggers is
recurring (birthdays are point 7). For each kept unit it runs `prospection.Follow` with the new
preferences and the interrupt level of the unit's earliest armed trigger (the one a reminder
created for the unit inherits; doc 02 §7 states it), then applies the result through the same
helper the correction uses. The helper is now parameterised by action family, so the rows
are `config.reminder.moved|armed|cancelled`, recorded before each trigger write (ADR-0016).

Re-arm runs when a save **changes a value in force**, or when the re-arm of an earlier change is
still pending. A save that changes nothing, with nothing pending, never creates, moves or
re-sends a reminder. Running `Follow` again over the same preferences is *not* a no-op: it only
sees live triggers, so a lead whose evening-pulled reminder already went out is planned again
at once (leads 6 h and 2 h, an event at 05:00, the 22:00 reminder sent, a save at 22:30 would
pull the 23:00 one to 22:30 and arm a new 23:00).

Pending is read from the decision log. A re-arm that completes records
`config.reminders.followed`. A re-arm is pending when the newest `config.updated` row is newer
(by insertion sequence) than the newest `config.reminders.followed` row. If a re-arm stops part
way, `Save` returns `ErrRemindersNotFollowed`, the saved values stand, and the next save (the page
says "Save again to finish") completes the re-arm even if it changes nothing. The re-arm is
deterministic over what is armed, so finishing it does not repeat what the first attempt
already did. Known gap: if the `config.updated` row itself could not be written *and* the re-arm
failed, nothing marks the re-arm pending. That needs two writes to fail in one save.

Two consequences come straight from point 4, and the page copy does not hide them. When every
new lead of an event is already behind, its armed reminder is pulled to now. When a new lead is
still ahead, a reminder is armed for it even if a reminder for an older lead already went out.

**D6. Five actions: `config.updated`, `config.reminder.{moved,armed,cancelled}` and
`config.reminders.followed`.** They are
appended to the vocabulary, and `ActivityFamilies` yields one `config` family. m4e2 reuses
`config.updated` for its admin fields.

**D7. UI: `GET /ui/preferences` and `POST /ui/preferences`.** These are guarded and
cross-origin-protected like `/ui/beliefs`, and a nav entry is added. The page has one form. The
leads editor is one row per lead (a whole number and a unit: minutes, hours or days) with a
**Remove** button per row and an **Add a lead** button, up to 5. Remove and Add are htmx
buttons (`type="button"`, `name="op"`) that post the form and get back the leads fieldset with the
edits kept. They **save nothing**. An empty row counts as no lead, and one spare empty row is always
shown while there is room, so adding a lead works without JavaScript too. The times are `<input type="time">`. An invalid save
re-renders with each field's message and keeps what was typed. A valid save re-renders with the
saved values and "Preferences saved." On `HX-Request` the response is a fragment, otherwise the
full page. The save button uses the pending pattern from #328. One line of copy says that a
change also moves the reminders already set for upcoming events.

**D8. Messages restate the rule, with core's constants interpolated.** Example: "Each lead is
from 1 minute to 30 days, at most 5, no two the same." The UI never re-implements the rule.
Core decides, and the UI names the field. An amount that is not a whole number, or an unknown
unit, is refused by the UI. The UI still submits the times to `Save`, with no leads (which core
always refuses, so nothing is written), so the same submit reports every wrong field.

## 3. Chain (stacked to main)

| PR | Branch | Content |
|---|---|---|
| 1 | `fix/digest-once-per-date` | D1 + doc 02; this design and tasks (#329) |
| 2 | `feat/reminder-prefs-save` | D2-D4, D6 (`config.updated`); doc 02 §11 (#330) |
| 3 | `feat/reminder-prefs-rearm` | D5, D6 (`config.reminder.*`, `config.reminders.followed`), I35; doc 02 §7, doc 06 §4 (#331) |
| 4 | `feat/ui-preferences` | D7-D8; wiring and UI gates; doc 07 |

## 4. Tests

- L1 core: the digest per date (PR 1).
- L1 brain over memrepo: validation per field with no write, the effective-value no-op, the
  absent-row default no-op, the corrupt-value repair, write-then-record, `WriteLandedError`,
  re-arm move/arm/cancel, past and recurring events skipped, an unchanged save repairing.
- L3: `SetReminderPrefs` touches only the columns it names; `ArmedUnitIDs`.
- L2: `TestI35_APreferenceChangeReArmsEventsAhead`; the UI gates (entrances whitelist,
  cross-origin body, mux wiring, read views write nothing).
- L1 ui over stubs: render, invalid input, add and remove, saved notice, pending attributes.
