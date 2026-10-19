-- units gains an event's own reminder leads (I40, ADR-0038): a JSON array of
-- whole minutes before event_at, as config.event_reminder_leads holds the
-- user's. NULL follows the user's preferences, so every unit from before keeps
-- the reminders it always had. Only a correction writes it. See
-- docs/03-data-model.md's units block — this file must reproduce that column
-- exactly. Published: never edit this file again, write the next migration
-- instead (CLAUDE.md §7 conventions).

ALTER TABLE units ADD COLUMN reminder_leads TEXT;  -- JSON array of minutes, or NULL: the user's leads
