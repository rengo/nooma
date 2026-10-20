-- The chat a message is read in, stored in the vault (ADR-0041, superseding
-- ADR-0036's in-memory log): what the user wrote, what Nooma answered, and
-- what it pushed, each with the units it concerned. Append-only: a turn is
-- never changed or removed, and the read takes the last turns rather than
-- pruning the older ones. seq is the chat's order, two turns at one instant
-- included. See docs/03-data-model.md's conversation_turns block — this file
-- must reproduce it exactly. Published: never edit this file again, write the
-- next migration instead (CLAUDE.md §7 conventions).

CREATE TABLE conversation_turns (
  seq          INTEGER PRIMARY KEY,         -- append order
  conversation TEXT NOT NULL,               -- the channel's chat id
  said_at      TEXT NOT NULL,
  speaker      TEXT NOT NULL,               -- user|nooma|push
  text         TEXT NOT NULL,
  unit_ids     TEXT NOT NULL DEFAULT '[]'   -- JSON array of units(id); deliberately NOT a foreign key
);

-- The read: a chat's last turns.
CREATE INDEX idx_conversation_turns_chat ON conversation_turns(conversation, seq);
