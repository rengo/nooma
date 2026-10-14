# Proposal — remind-me-reminders: a reminder the user asks for is a reminder

## Why

On the client's vault (main at efa0a1c, 2026-10-14), the Telegram message "recordame comprar café
mañana a las 10" was classified `task`, with `due_at` 2026-10-15T13:00:00Z (10:00 at -03:00) and
content "Recordarme comprar café el 2026-10-15 a las 10:00.". **No trigger was armed**, and the
reply was "Anotado.".

The model read the message correctly. The client's 2026-10-09 rule puts something to get done in
`task`, and ADR-0029 point 7 says a task arms nothing. Nothing in the system carries "the user
asked to be reminded". The one thing the user asked for is the one thing Nooma did not do, and
the reply did not say so.

## What changes (client decisions, 2026-10-14, binding; PM defaults marked)

1. **Asking** is read from the user's own words ("recordame…", "avisame…", "remind me…"), not
   from the model's reading of them. Any kind with a date can carry it (PM).
2. **A named time** ("recordame X a las H"): one reminder before H, at its own lead, which is
   separate from the event leads. The default is **15 minutes**, editable on `/ui/preferences`.
   If the lead is already past and H is still ahead, Nooma reminds at once (PM).
3. **A day with no time** ("recordame X mañana"): that day at **09:00**, the date-only hour.
4. **On an event**: the asked lead is **added** to the event's 24 h and 2 h.
5. **No day and no time** ("recordame X"): Nooma asks back, "¿Algún día y hora en particular?".
   - A time or relative answer ("mañana a las 10", "en 1 semana") sets the reminder.
   - "No" makes it **daily at 09:00**.
   - Which question is being answered is decided by the m3e pending-question pattern, never by
     the model.
6. **Recurring** ("recordame tomar pastillas todos los días a las 9pm"): a recurring **task**
   that reminds every day at 21:00.
7. **Stopping is chat-first**: the user may never open the UI.
   - **An explicit stop** ("no me lo recuerdes más", also "listo, no me lo recuerdes más") stops
     the reminder for good.
   - **A bare "listo" / "ya lo hice"** on a daily or recurring reminder is ambiguous. The first
     time, Nooma asks "¿Querés que te lo siga recordando o borramos el recordatorio?". The answer
     is read from phrase lists. "Seguí" keeps the reminder and marks it, so from then on a bare
     "listo" on it means done for today: no question, and it keeps recurring. "Borralo" stops it.
   - A reminder that became daily through "No" (5) follows the same rule.
   - When it is unclear which reminder is meant, Nooma asks which. That is never decided by the
     model.
8. **Quiet hours**: a time the user named rings at that time even inside quiet hours, recurring
   reminders included (a daily 06:30 rings at 06:30 when the silence ends at 08:00). A time
   Nooma chose (09:00) keeps ADR-0030's night rules.
9. **Delivery**: as in ADR-0030, the reminder is pushed whatever its interrupt level and is never
   sent after its time.
10. **The reply** names exactly when Nooma will remind: "Anotado para el jue 15 oct 10:00. Te
    aviso 15 minutos antes.". When nothing was set, the reply says why.
11. **Corrections**: correcting the time moves the reminder.
12. **Lead changes**: changing the lead moves the reminders already armed.
13. **Not changed**: timers (PM), recurring reminders that were not asked for, and events and
    tasks that were not asked about.

## Out of scope

- Asking for a reminder about a unit already stored.
- Listing reminders from chat.
- Cancelling timers from chat (`m4f`).
- Changing the lead by chat.
- Re-reading units captured before this change.

## Open product questions

None. The client answered NQ1 on 2026-10-14 (decision 7).

## Review Workload Forecast

| Field | Value |
|---|---|
| Estimated changed lines (impl + docs) | ~2,200 across 9 implementation links (~2,300 test lines, counted apart) |
| 400-line budget risk | High as one PR |
| Chained PRs recommended | Yes: stacked to main, each link under 400 |
| Decision needed before apply | ADR-0031 accepted |

This plan is itself a two-link chain: this file, `spec.md` and ADR-0031 (what the client
accepts), then `design.md` and `tasks.md`. The implementation links are in `tasks.md`.
**Link 2 delivers the first usable thing**: "recordame X a las H", "…mañana", and "recordame" on
an event.
