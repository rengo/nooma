-- current_state gains a discriminator between a user-reported reading and a
-- consolidation-written hypothesis. See docs/03-data-model.md's
-- "Core tables" section, current_state block — this file must reproduce
-- that column exactly (spec R3.8, the schema golden R4.3 compares the
-- two). Published: never edit this file again, write 0004 instead
-- (CLAUDE.md §7 conventions, non-negotiable in spirit).

ALTER TABLE current_state ADD COLUMN source TEXT NOT NULL DEFAULT 'user';  -- user|consolidation
