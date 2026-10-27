-- learning_signals gains nothing: this migration types the relation signals
-- written before M5 (doc 02 §9, ADR-0052). A relation_reject or
-- relation_confirm from then carries no relation_type, so the learner, which
-- counts a verdict against its type, could not count it. No schema change; see
-- docs/03-data-model.md's Learning section. Published: never edit this file
-- again, write 0014 instead (CLAUDE.md §7 conventions).
--
-- Only an untyped signal is touched (relation_type IS NULL), and no row is
-- inserted, so the backfill can neither duplicate a signal nor rewrite one
-- that already has its type. Every signal it types keeps a note of where the
-- type came from (backfilled_from).

-- A graph delete (relation.rejected, #398) recorded the whole relation before
-- deleting it: its type, maker and the confidence the user judged. The
-- signal was emitted just before the delete (I10), so it exists for every
-- such row whose delete went through. The earliest row by rowid wins if a
-- retried delete wrote two.
WITH rejected AS (
  SELECT json_extract(context, '$.relation_id') AS relation_id, MIN(rowid) AS row
  FROM decision_log
  WHERE action = 'relation.rejected'
  GROUP BY 1
)
UPDATE learning_signals
SET relation_type = json_extract(d.context, '$.previous.type'),
    context = json_object(
      'created_by', json_extract(d.context, '$.previous.created_by'),
      'confidence', json_extract(d.context, '$.previous.confidence'),
      'backfilled_from', d.id)
FROM rejected JOIN decision_log d ON d.rowid = rejected.row
WHERE learning_signals.signal_type = 'relation_reject'
  AND learning_signals.relation_type IS NULL
  AND learning_signals.target_id = rejected.relation_id
  AND json_extract(d.context, '$.previous.type') IS NOT NULL;

-- A confirm left its relation alive, so the relation names its type and
-- maker. The confidence the user judged is not recoverable (the confirm
-- raised it) and is left out rather than guessed.
UPDATE learning_signals
SET relation_type = r.type,
    context = json_object('created_by', r.created_by, 'backfilled_from', 'relations')
FROM relations r
WHERE learning_signals.signal_type = 'relation_confirm'
  AND learning_signals.relation_type IS NULL
  AND learning_signals.target_id = r.id;

-- A reject answered in chat cannot be typed: its relation is gone and its
-- decision row (capture.relation_checkin.resolved) names no type. It stays
-- untyped, and the learner does not count it.
