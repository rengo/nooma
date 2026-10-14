# Proposal — remind-me-reminders: a reminder the user asks for is a reminder

## Why

On the client's vault (main at efa0a1c, 2026-10-14), "recordame comprar café mañana a las 10" was
classified `task` with `due_at` 2026-10-15T13:00:00Z (10:00 at -03:00). **No trigger was armed**,
and the reply was "Anotado.". The model read it right. A task arms nothing (ADR-0029 point 7),
and nothing in the system carries "the user asked to be reminded".

## What changes

The client's decisions of 2026-10-14, NQ1 included, are
[ADR-0031](../../../docs/adr/0031-reminders-the-user-asks-for.md) points 1-8, accepted the same
day. In short:

- **Asked, from the user's own words.** A named time is reminded 15 minutes before (a preference
  edited on `/ui/preferences`). A day alone is reminded that day at 09:00. On an event, the asked
  lead is added to its 24 h and 2 h.
- **Repeating.** An asked repetition is a recurring task at the named time. "recordame X" with
  no day or time asks back, and "No" makes it daily.
- **Stopping.** A reminder stops only when the user says so in chat. A bare "listo" asks once
  whether to keep going.
- **Night and reply.** A named time rings through quiet hours, and the reply says exactly when.

PM decisions, binding:

- Late lead with the time still ahead: remind at once.
- Any kind with a date can carry the request.
- Timers stay as they are.
- **P1:** no link that creates a repeating reminder reaches main before the chat stop exists.
- **P2:** recurring reminders nobody asked for are re-armed after each occurrence too, as doc 02
  §7 already promises, with no double fire and no burst. That is a change for them, and R10 says
  so.
- **P3:** a bare "no" to "¿seguís o borramos?" deletes.
- **P4:** quoted or reported speech is a known limit, pinned by a test.
- **P5:** a stop is durable state on the unit, never inferred from the request flag.
- **P6:** only a new explicit ask brings a stopped reminder back, as a new unit.
- **P7:** a stop binds without asking only on one unambiguous candidate; a tie asks, with a
  list capped at 5.

## Out of scope

A reminder for a unit already stored; listing reminders, or cancelling timers, from chat (`m4f`);
the lead by chat; quoted speech (P4); units captured before this change.

## Review Workload Forecast

| Field | Value |
|---|---|
| Changed lines (impl + docs) | ~2,400 across 9 implementation links (~2,500 test lines apart) |
| 400-line budget risk | High as one PR |
| Chained PRs | Yes: stacked to main, each link under 400 |
| Decision needed before apply | None. ADR-0031 is accepted |

The plan is a chain of four PRs, each under 400 lines: (1) ADR-0031, this proposal, `spec.md`;
(2) `design.md` and `tasks.md`, one-shot links 1-4; (3) `design-repeating.md`, links 5-9; (4)
`tasks-repeating.md`.

**Link 2 delivers the first usable thing**: "recordame X a las H", "…mañana", and "recordame" on
an event.
