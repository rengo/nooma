-- unit_embeddings is keyed by (unit_id, model): a unit holds one vector per
-- model, so a write under one model keeps the unit's vector under another
-- (I21, ADR-0058, superseding ADR-0003's amendment in part). A reindex then
-- never takes away the index the running model reads, and a return to an
-- earlier model costs nothing. SQLite cannot change a primary key in place,
-- so the table is rebuilt: every row is copied first, then the old table is
-- dropped and the copy renamed. Nothing is lost; the model index and the
-- cascade from units are recreated as they were. See docs/03-data-model.md's
-- Search section — this file must reproduce that table exactly.
-- Published: never edit this file again, write 0017 instead (CLAUDE.md §7).

CREATE TABLE unit_embeddings_per_model (
  unit_id   TEXT NOT NULL REFERENCES units(id) ON DELETE CASCADE,
  model     TEXT NOT NULL,       -- e.g. 'nomic-embed-text'
  dim       INTEGER NOT NULL,    -- redundant with the blob length, and worth the redundancy
  embedding BLOB NOT NULL,       -- dim × float32, little-endian, L2-normalized on write
  created_at TEXT NOT NULL,
  PRIMARY KEY (unit_id, model)
);

INSERT INTO unit_embeddings_per_model (unit_id, model, dim, embedding, created_at)
SELECT unit_id, model, dim, embedding, created_at FROM unit_embeddings;

DROP TABLE unit_embeddings;

ALTER TABLE unit_embeddings_per_model RENAME TO unit_embeddings;

CREATE INDEX idx_unit_embeddings_model ON unit_embeddings(model);
