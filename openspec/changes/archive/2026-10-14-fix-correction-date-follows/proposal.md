# Proposal — fix-correction-date-follows: a corrected date carries what was derived from it

## Why

Observed by the client on a real vault, main at ed1f2d0, 2026-10-09:

1. "dentista el viernes a las 10" was stored with content "dentista el 2026-10-16 a las 10",
   `event_at` 2026-10-16T10:00Z, and a trigger armed for it.
2. The unit page's correction form ("en realidad es hoy viernes 9 a las 10") wrote one
   `correction.applied` changing only `event_at` to 2026-10-09T10:00Z.
3. The unit page now reads "dentista el 2026-10-16 a las 10" beside Event 2026-10-09 10:00, and
   the trigger armed for the old date was not moved.

Doc 02 §5 step 4 called the stale body an accepted cost and said nothing about the trigger.

## What changes (client-approved decisions)

1. After a correction changes a unit's event or due date, the unit's text does not contradict
   it: it shows the new date, or wording that carries no stale date. Free capture is otherwise
   unchanged.
2. The unit ends up with exactly one live reminder consistent with the new date, armed by the
   same rule a fresh capture uses (including firing at once when the lead horizon is already
   behind). If the new date is already past, the reminder is cancelled and none is armed.
3. Activity shows the correction (previous → next, including any text change) and the reminder
   being moved, armed or cancelled.
4. It applies to every correction path: unit form, API with `unit_id`, free capture corrections.
5. Nothing is deleted: cancelling is a state transition. Existing units are not migrated.

## Out of scope

- Re-wording a date the stored text names in a form other than the one capture is asked to
  write (see design §2).
- Triggers that already fired. They are history; only an armed reminder is live.
