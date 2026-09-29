-- Add the group activity kind (ADR 0016).
--
-- Migration 15 constrained `account_activity.source_kind` to the two kinds that
-- existed then. A group message is its own kind rather than a flag on a message, so
-- a group activity row is distinguishable from a direct one without inferring it
-- from the conversation. An applied migration is not edited to retrofit a live
-- database, so the constraint is replaced here instead.
--
-- SQLite cannot alter a CHECK in place. The table is rebuilt with the wider set,
-- carrying every row across, and the same rebuild discipline migration 11 used for
-- the shared conversation mapping.

CREATE TABLE account_activity_rebuilt (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    source_kind TEXT NOT NULL CHECK(source_kind IN ('message','request','group_message')),
    source_id TEXT NOT NULL,
    conversation_id TEXT NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
    actor_id TEXT REFERENCES users(id) ON DELETE SET NULL,
    created_at TEXT NOT NULL,
    acknowledged_at TEXT NOT NULL DEFAULT '',
    interruptible INTEGER NOT NULL DEFAULT 1 CHECK(interruptible IN (0,1))
);

INSERT INTO account_activity_rebuilt(id,user_id,source_kind,source_id,conversation_id,actor_id,created_at,acknowledged_at,interruptible)
    SELECT id,user_id,source_kind,source_id,conversation_id,actor_id,created_at,acknowledged_at,interruptible FROM account_activity;

DROP TABLE account_activity;
ALTER TABLE account_activity_rebuilt RENAME TO account_activity;

-- The indexes are recreated because dropping the table took them with it. They are
-- the same definitions as migration 15, and the identity index is what keeps a
-- retried send or a replayed request from producing a second interruption.
CREATE UNIQUE INDEX account_activity_identity
    ON account_activity(user_id, source_kind, source_id);
CREATE INDEX account_activity_recent ON account_activity(user_id, id DESC);
CREATE INDEX account_activity_unacknowledged
    ON account_activity(user_id, created_at) WHERE acknowledged_at='';
CREATE INDEX account_activity_dispatch ON account_activity(id) WHERE interruptible=1;
