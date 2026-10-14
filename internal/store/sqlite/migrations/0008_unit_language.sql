-- units gains the language its capture was written in (ADR-0032 point 5). NULL is
-- "not known": the classification named none, or the unit predates this column
-- and no capture.classify row recorded one. See docs/03-data-model.md's units
-- block — this file must reproduce that column exactly. Published: never edit
-- this file again, write 0009 instead (CLAUDE.md §7 conventions).

ALTER TABLE units ADD COLUMN language TEXT;  -- 'en' | 'es' | NULL

-- Backfill from the capture's own classification. A correction re-classifies, so
-- a unit can have several capture.classify rows and the earliest by rowid is the
-- capture's (decision_log has no sequence column). An empty language is not one.
WITH first AS (
  SELECT json_extract(context, '$.unit_id') AS unit_id, MIN(rowid) AS row
  FROM decision_log
  WHERE action = 'capture.classify'
  GROUP BY 1
)
UPDATE units
SET language = NULLIF(json_extract(d.context, '$.language'), '')
FROM first JOIN decision_log d ON d.rowid = first.row
WHERE first.unit_id = units.id;
