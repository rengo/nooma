-- A question the brain asked and is waiting on. See docs/03-data-model.md
-- and docs/adr/0027-pending-question-store.md. Published: never edit this
-- file again, write 0005 instead (CLAUDE.md §7 conventions, non-negotiable
-- in spirit).

CREATE TABLE pending_questions (
  id           TEXT PRIMARY KEY,
  kind         TEXT NOT NULL,   -- relation (the only member m3e writes)
  relation_id  TEXT NOT NULL,   -- relations(id); deliberately NOT a foreign key
  created_at   TEXT NOT NULL,   -- when consolidation decided to ask
  asked_at     TEXT,            -- NULL = not yet surfaced in a digest
  resolved_at  TEXT,            -- NULL = still open
  resolution   TEXT             -- confirmed|rejected|expired; NULL while open
);

-- The digest's queue: unasked, oldest first.
CREATE INDEX idx_pending_questions_unasked ON pending_questions(created_at)
  WHERE asked_at IS NULL AND resolved_at IS NULL;

-- The disambiguation pool and the expiry sweep: asked, unanswered.
CREATE INDEX idx_pending_questions_open ON pending_questions(asked_at)
  WHERE asked_at IS NOT NULL AND resolved_at IS NULL;
