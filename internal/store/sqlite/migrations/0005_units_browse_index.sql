-- One index for LiveBrowsePage's keyset-paginated live-unit read (m4b
-- design §3.2). Without it, every browse page is a full table scan plus a
-- sort. See docs/03-data-model.md "Core tables" — this file must reproduce
-- that section exactly (schema_doc_test, R4.3). Published: never edit this
-- file again, write 0006 instead (CLAUDE.md §7 conventions).

CREATE INDEX idx_units_live_browse ON units(status, created_at, id);
