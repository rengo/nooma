-- config gains the user's night (ADR-0030): when quiet hours end, and the
-- time, the evening before, an event reminder is moved to when its event
-- is at or before that end. NULL is "never chosen": prospection.
-- ResolveReminderPrefs supplies the default. See docs/03-data-model.md's
-- "System config" block — this file must reproduce those two columns
-- exactly. Published: never edit this file again, write 0008 instead
-- (CLAUDE.md §7 conventions).

ALTER TABLE config ADD COLUMN quiet_hours_end TEXT;      -- HH:MM local, 00:01 to 12:00
ALTER TABLE config ADD COLUMN evening_reminder_at TEXT;  -- HH:MM local, quiet end to 23:59
