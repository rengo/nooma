# ADR-0043 — A follow-up that asks to be reminded reminds of the item it names

- **Status**: Accepted (by the client through the PM, 2026-10-19: "recordámelo" refers to the
  item it follows; make the code act on that reading)
- **Date**: 2026-10-19
- **Supersedes**: [ADR-0036](0036-a-chat-message-is-read-in-its-conversation.md) point 3, in
  part: a bound follow-up with `remind_request: true` is not corrected, it is reminded of
- **Superseded by**: —
- **Enables**: M4

## Context

Recorded against the prompt that shows each turn's time (ADR-0042), "recordámelo" right after
"tengo que comprar el regalo de Ana mañana" came back as a `correction` of that task (referent
`A`) with `remind_request: true`. That is the right reading: the message refers to the item it
follows. The code then took the correction path, which returns before `remind_request` is
read. The task's text was rewritten, no reminder was armed, and the reply said "Corregido.".
Read before as a new task, the same message armed a reminder on a duplicate unit.

## Decision

1. **A chat follow-up bound to a listed item whose reading carries `remind_request: true` makes
   that item an asked reminder.** The unit is marked asked (`remind_me`, `UnitRepo.SetRemindMe`)
   and armed by the same `prospection.Arm` an asked capture uses (ADR-0031), on the unit as it
   stands: no unit is stored, and nothing else on it is rewritten.
2. **Only the asked reminder is armed**, so an event's own leads are never armed twice, and one
   already armed at the same instant is not armed again.
3. **The answer is the asked reminder's** ("Anotado para el sáb 17 oct. Te aviso ese día a las
   09:00."). An item with no date is still marked asked, and the answer says no reminder was set
   and why ("Anotado, pero no puse aviso: …"), with a `capture.arm.refused` row naming the unit.

## Consequences

- A remind-me that also changes the item ("es a las 11, recordámelo") is reminded of as the item
  stands, and the change is not applied. The prompt is unchanged, so no recording is affected.
- The ADR-0031 re-plan reads the asked mark as it does for an asked capture.
