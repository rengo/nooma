-- units gains the short, date-free title a reminder says for it (spec R12,
-- remind-me-reminders). NULL is "no title": the unit predates this column, or
-- the model gave none, or the one it gave failed the check in
-- classify.ResolveTitle. There is no backfill, since a title cannot be derived
-- from the content without the model. See docs/03-data-model.md's units block —
-- this file must reproduce that column exactly. Published: never edit this file
-- again, write the next migration instead (CLAUDE.md §7 conventions).

ALTER TABLE units ADD COLUMN title TEXT;  -- the reminder's name, or NULL
