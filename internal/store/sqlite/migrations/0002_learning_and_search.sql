-- Learning, Measurements, System config and Search. See
-- docs/03-data-model.md's "Learning", "Measurements (perception phase)",
-- "System config" and "Search: embeddings + FTS5" sections — this file
-- must reproduce them exactly (spec R3.8, the schema golden R4.3 compares
-- the two). Published: never edit this file again, write 0003 instead
-- (CLAUDE.md §7 conventions, non-negotiable in spirit).

CREATE TABLE learning_signals (
  id              TEXT PRIMARY KEY,
  signal_type     TEXT NOT NULL,   -- correction|nudge_ack|nudge_ignored|nudge_engaged|nudge_declined|belief_delete|belief_edit|relation_reject|relation_confirm|state_confirmed|state_denied
  valence         TEXT NOT NULL,   -- positive|negative|neutral
  target_kind     TEXT,            -- unit|trigger|belief|relation
  target_id       TEXT,            -- NO FK: the signal outlives the target's deletion
  decision_action TEXT,            -- decision bucket evidenced
  relation_type   TEXT,            -- only for relation_reject/confirm
  magnitude       REAL,
  context         TEXT NOT NULL DEFAULT '{}',
  occurred_at     TEXT NOT NULL
);
CREATE INDEX idx_learning_signals_occurred ON learning_signals(occurred_at);

CREATE TABLE learning_state (                    -- incremental pass checkpoint
  id                 INTEGER PRIMARY KEY DEFAULT 1,
  last_run_at        TEXT,
  signals_consumed   INTEGER NOT NULL DEFAULT 0,
  goal_calibrated_at TEXT,                       -- goal cadence cooldown
  updated_at         TEXT NOT NULL
);

CREATE TABLE relation_thresholds (
  id                         TEXT PRIMARY KEY,
  relation_type              TEXT NOT NULL UNIQUE,
  min_confidence_to_persist  REAL NOT NULL DEFAULT 0.3,
  min_confidence_to_surface  REAL NOT NULL DEFAULT 0.5
);

CREATE TABLE calibration (                       -- generic auto-tuned knobs
  key        TEXT PRIMARY KEY,                   -- e.g. 'goal_stagnation_days'
  value      REAL NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE TABLE measurements (
  id             TEXT PRIMARY KEY,
  entity         TEXT NOT NULL DEFAULT 'self',   -- self|car|pet|…
  domain         TEXT NOT NULL,                  -- health|finance|fitness|… (a column, not a table)
  metric_key     TEXT NOT NULL,
  value          REAL,
  unit           TEXT,                           -- mg/dL, kg, ARS
  ref_range_low  REAL,
  ref_range_high REAL,
  measured_at    TEXT NOT NULL,                  -- EVENT time (≠ created_at)
  metadata       TEXT,
  ref_unit_id    TEXT REFERENCES units(id) ON DELETE SET NULL,  -- structured_ref anchor
  created_at     TEXT NOT NULL,
  updated_at     TEXT NOT NULL
);
CREATE INDEX idx_measurements_metric ON measurements(domain, metric_key, measured_at);
CREATE INDEX idx_measurements_ref_unit ON measurements(ref_unit_id);

CREATE TABLE config (
  id                        INTEGER PRIMARY KEY DEFAULT 1,  -- singleton: ALWAYS WHERE id=1
  weight_threshold          REAL NOT NULL DEFAULT 0.5,
  hysteresis_margin         REAL NOT NULL DEFAULT 0.05,
  consolidation_enabled     INTEGER NOT NULL DEFAULT 1,
  goal_stagnation_days      INTEGER NOT NULL DEFAULT 21,
  mental_load_threshold     INTEGER NOT NULL DEFAULT 7,
  consolidation_last_run_at TEXT,                -- NULL = never ran
  updated_at                TEXT NOT NULL
);

-- Embeddings: an ordinary table. No virtual table, no extension.
-- Proximity search is a dot product in Go over an in-memory index (ADR-0012).
CREATE TABLE unit_embeddings (
  unit_id   TEXT PRIMARY KEY REFERENCES units(id) ON DELETE CASCADE,
  model     TEXT NOT NULL,       -- e.g. 'nomic-embed-text'
  dim       INTEGER NOT NULL,    -- redundant with the blob length, and worth the redundancy
  embedding BLOB NOT NULL,       -- dim × float32, little-endian, L2-normalized on write
  created_at TEXT NOT NULL
);
CREATE INDEX idx_unit_embeddings_model ON unit_embeddings(model);

-- Lexical: FTS5 synchronized with units.content via triggers
CREATE VIRTUAL TABLE units_fts USING fts5(
  content, content='units', content_rowid='rowid'
);

-- Sync triggers: keep units_fts current with units.content. External-content
-- FTS5 tables are not maintained automatically (docs/03-data-model.md's own
-- "Search" section states this in prose; this is its DDL, R9.1/R9.2).
--
-- The literal string 'delete' as the first value in an INSERT INTO
-- units_fts(...) below is FTS5's own documented special-command syntax for
-- removing one row from an EXTERNAL-CONTENT table (units_fts is declared
-- content='units': it stores no content of its own, so an ordinary DELETE
-- statement against it does not apply). Read as a plain INSERT it looks
-- like nonsense; it is not one.
--
-- units_fts_au issues a delete-then-insert pair, never an UPDATE, because
-- external-content FTS5 tables have no UPDATE path at all — that
-- restriction is exactly why there are three triggers here and not one.
--
-- Archival is an UPDATE (units.status -> 'archived'), never a DELETE
-- (CLAUDE.md non-negotiable #6), so units_fts_au fires on archival too and
-- re-indexes the SAME content at the SAME rowid: an archived unit STAYS in
-- units_fts. That is correct, not a leak — docs/02-cognitive-core.md's
-- units are "cold, weight ~= 0" once archived, NOT excluded from read
-- surfaces; only superseded/incomplete units are excluded, and doc 02
-- prescribes doing that positively (status = 'pool') AT QUERY TIME, never
-- at index time.
--
-- Do NOT "optimize" any of these three triggers to skip
-- superseded/incomplete rows. units_fts is content='units',
-- content_rowid='rowid': it MUST mirror units row-for-row. A trigger that
-- silently skipped some units rows would break that rowid correspondence,
-- and FTS5 would then return rowids that either do not resolve to any
-- units row or resolve to the WRONG one.
CREATE TRIGGER units_fts_ai AFTER INSERT ON units BEGIN
  INSERT INTO units_fts(rowid, content) VALUES (new.rowid, new.content);
END;
CREATE TRIGGER units_fts_ad AFTER DELETE ON units BEGIN
  INSERT INTO units_fts(units_fts, rowid, content) VALUES ('delete', old.rowid, old.content);
END;
CREATE TRIGGER units_fts_au AFTER UPDATE ON units BEGIN
  INSERT INTO units_fts(units_fts, rowid, content) VALUES ('delete', old.rowid, old.content);
  INSERT INTO units_fts(rowid, content) VALUES (new.rowid, new.content);
END;
