# ADR-0043 — A follow-up that asks to be reminded reminds of the item it names

- **Status**: Accepted (by the client through the PM, 2026-10-19: "recordámelo" refers to the
  item it follows; make the code act on that reading. And 2026-10-20: "es a las 11,
  recordámelo" must not drop the change; apply it first, then remind of the corrected item)
- **Date**: 2026-10-19
- **Supersedes**: [ADR-0036](0036-a-chat-message-is-read-in-its-conversation.md) point 3, in
  part: a bound follow-up with `remind_request: true` is not only corrected, it is reminded of
- **Superseded by**: [ADR-0044](0044-a-chat-answer-names-the-item-it-is-about.md), points 3 and 4 in
  part (the answer names every ring the item has)
- **Enables**: M4

## Context

Recorded against the prompt that shows each turn's time (ADR-0042), "recordámelo" right after
"tengo que comprar el regalo de Ana mañana" came back as a `correction` of that task (referent
`A`) with `remind_request: true`. That is the right reading: the message refers to the item it
follows. The code then took the correction path, which returns before `remind_request` is
read. The task's text was rewritten, no reminder was armed, and the reply said "Corregido.".
Read before as a new task, the same message armed a reminder on a duplicate unit.

The same recordings show the model also sets `remind_request: true` beside a change of the
event's leads ("avisame 2 horas antes", `classify-followup-dos-horas-antes`), where the user
asked for leads, not for an asked reminder.

## Decision

1. **A chat follow-up bound to a listed item whose reading carries `remind_request: true` makes
   that item an asked reminder.** The unit is marked asked (`remind_me`, `UnitRepo.SetRemindMe`)
   and armed by the same `prospection.Arm` an asked capture uses (ADR-0031): no unit is stored.
2. **Only the asked reminder is armed**, so an event's own leads are never armed twice, and one
   already armed at the same instant is not armed again.
3. **The answer is the asked reminder's** ("Anotado para el sáb 17 oct. Te aviso ese día a las
   09:00."). An item that cannot be reminded (no date, or too close for the asked reminder) is
   still marked asked, and the answer says no reminder was set and why ("Anotado, pero no puse
   aviso: …"), with a `capture.arm.refused` row naming the unit.
4. **A remind-me that names a new instant for the item moves it first.** The reading's
   `event_at` or `due_at`, when it is not the one the unit already holds (`correction.MovesDate`;
   a repeated instant is an echo, I30), is applied through the existing explicit-referent
   correction path, and the asked reminder is then armed on the moved unit. The answer names
   both in the existing sentences ("Movido al mar 20 oct, 11:00. Te aviso 15 minutos antes."). A
   correction that asks instead of applying is answered as it is and arms nothing: the item is
   not where the user said. With no new instant nothing on the unit is rewritten: the reading
   restates its item's text, and a restatement cannot be told from an edit.
5. **Leads win.** A bound reading that carries `reminder_leads_minutes` says when to be
   reminded: it takes the correction path, which sets the event's own leads (I40), and a
   `remind_request` beside it is ignored. No asked reminder is added and the item is not marked
   asked.

## Consequences

- The prompt is unchanged, so no recording is affected.
- A remind-me that changes only the item's wording ("es con Pedro, recordámelo") is reminded of
  with its wording unchanged; correcting the wording takes its own message.
- The ADR-0031 re-plan reads the asked mark as it does for an asked capture.
