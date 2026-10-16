-- units gains whether the user's own words asked to be reminded of it, and
-- config gains the user's remind-me lead (ADR-0031, remind-me-reminders). An
-- existing unit reads 0: nobody asked, and its reminders are the ones it always
-- had. NULL in config.remind_me_lead_minutes is "never chosen":
-- prospection.ResolveReminderPrefs supplies the default of 15 minutes, so a later
-- change of default reaches everyone who never chose. See docs/03-data-model.md's
-- units and "System config" blocks — this file must reproduce those columns
-- exactly. Published: never edit this file again, write the next migration
-- instead (CLAUDE.md §7 conventions).

ALTER TABLE units ADD COLUMN remind_me INTEGER NOT NULL DEFAULT 0;  -- 1: the user asked to be reminded
ALTER TABLE config ADD COLUMN remind_me_lead_minutes INTEGER;       -- whole minutes, 1 to 43200
