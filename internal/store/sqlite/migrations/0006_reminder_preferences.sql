-- config gains the user's event reminder preferences (ADR-0029). NULL is
-- "never chosen": prospection.ResolveReminderPrefs supplies the default, so
-- a later change of default reaches everyone who never chose. See
-- docs/03-data-model.md's "System config" block — this file must reproduce
-- those two columns exactly. Published: never edit this file again, write
-- 0007 instead (CLAUDE.md §7 conventions).

ALTER TABLE config ADD COLUMN event_reminder_leads TEXT;   -- JSON array of minutes, e.g. [1440,120]
ALTER TABLE config ADD COLUMN date_only_reminder_at TEXT;  -- HH:MM local
