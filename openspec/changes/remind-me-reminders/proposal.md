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
- **Stopping.** A reminder stops only when the user says "no me lo recuerdes más" in chat. A
  daily rings every day until then.
- **Night and reply.** A named time rings through quiet hours, and the reply says exactly when.

PM decisions, binding:

- Late lead with the time still ahead: remind at once.
- Any kind with a date can carry the request.
- Timers stay as they are.
- **P1:** no link that creates a repeating reminder reaches main before the chat stop exists.
- **P2:** recurring reminders nobody asked for are re-armed after each occurrence too, as doc 02
  §7 already promises, with no double fire and no burst. That is a change for them, and R10 says
  so.
- **P3:** deferred with the done handling (below).
- **P4:** quoted or reported speech is out of scope; under ADR-0033 it is measured, not pinned.
- **P5:** a stop is durable state on the unit, never inferred from the request flag.
- **P6:** only a new explicit ask brings a stopped reminder back, as a new unit, except for the
  10-minute undo ADR-0033 decides.
- **P7:** a stop binds without asking only when the model's choice from the numbered candidate
  list names one unambiguous candidate; a tie or `unclear` asks, with a list capped at 5.

## Detection changed (client, 2026-10-14)

"La IA entiende y el código actúa." ADR-0033 (Accepted, 2026-10-14, option A) replaces the phrase lists with fields
the classify call returns. The code still binds, confirms and degrades. Everything the user
sees is unchanged, except the undo after a stop. The client also asked that a reminder say a
short title ("Recordatorio: Reunión con Juan — mié 14 oct, 22:30") instead of the content with
its date twice (spec R12), and that every real-model recording be made in one session.

## Deferred (client, 2026-10-14)

Done handling: a bare "listo" or "ya lo hice" on a reminder, the "¿seguís o borramos?"
question, its done-for-today mark, and old link 9 ("cuando me recuerde tomar la pastilla no voy
a decir 'listo'"). "listo" keeps M3's behaviour. This is a follow-up, to be planned when
the client asks for it.

## Out of scope

A reminder for a unit already stored; listing reminders, or cancelling timers, from chat (`m4f`);
the lead by chat; quoted speech (P4); units captured before this change.

## Review Workload Forecast

| Field | Value |
|---|---|
| Changed lines (impl + docs) | ~2,580 across 9 remaining implementation links: 1a, 1a′ and 2-8 (link 1 merged; old link 9 deferred) (~2,720 test lines apart), the sums of the link tables in `tasks.md` and `tasks-repeating.md` |
| 400-line budget risk | High as one PR |
| Chained PRs | Yes: stacked to main, each link under 400 |
| Decision needed before apply | None. ADR-0031 and ADR-0033 are accepted by the client |

The plan is a chain of three PRs, each under 400 lines: #350 (ADR-0033, this proposal,
`spec.md`); #356 (`design.md` and `tasks.md`, links 1a, 1a′ and 2-4); #357
(`design-repeating.md` and `tasks-repeating.md`, links 5-8).

**Link 2 delivers the first usable thing**: "recordame X a las H", "…mañana", and "recordame" on
an event.
