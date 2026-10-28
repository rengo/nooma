-- config gains whether goal_stagnation_days is the user's own (doc 02 §9,
-- ADR-0055): 1 once it is saved from Admin, a pin the learn pass never
-- overwrites; 0 while it is the default or what learning set. Every row before
-- this migration is unpinned: no learner had run, and an earlier Admin save is
-- not distinguishable from the default it may equal. See docs/03-data-model.md's
-- System config section — this file must reproduce that column exactly.
-- Published: never edit this file again, write 0016 instead (CLAUDE.md §7).

ALTER TABLE config ADD COLUMN goal_stagnation_pinned INTEGER NOT NULL DEFAULT 0;
