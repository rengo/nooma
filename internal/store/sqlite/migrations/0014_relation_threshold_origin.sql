-- relation_thresholds gains who set a row and when the learn pass last changed
-- it (doc 02 §9, ADR-0053). origin is 'learned' or 'user': a user row is a pin
-- the learn pass never overwrites. adjusted_at is the learn pass's last change,
-- NULL when it never changed the row; the rule reads only verdicts after it and
-- waits relation_cooldown_days from it. See docs/03-data-model.md's Learning
-- section — this file must reproduce those columns exactly. Published: never
-- edit this file again, write 0015 instead (CLAUDE.md §7 conventions).
--
-- Every row before this migration was seeded or set by hand, never by a learn
-- pass, which had not run: it is 'learned' with no adjusted_at, so the learn
-- pass may move it and counts every verdict.

ALTER TABLE relation_thresholds ADD COLUMN origin TEXT NOT NULL DEFAULT 'learned'
  CHECK (origin IN ('learned', 'user'));
ALTER TABLE relation_thresholds ADD COLUMN adjusted_at TEXT;
