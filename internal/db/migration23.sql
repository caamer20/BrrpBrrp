-- A thread survives its root (ADR 0017, "A thread whose root is gone").
--
-- Migration 19 declared `root_message_id ... ON DELETE CASCADE` and wrote in a
-- comment that deleting the root "tombstones the thread rather than leaving replies
-- attached to nothing". The cascade did the opposite: deleting the root deleted the
-- *thread row*, and the replies — acknowledged, read, sometimes linked to — were left
-- carrying a `thread_id` that resolved to nothing. They were absent from the thread
-- (404), absent from the channel listing, and still returned by search with a link
-- that 404s. A member who deleted one message lost the rest of the conversation with
-- no indication that it had gone.
--
-- The root is what a thread is *about*, not what it *is*. A subject can be withdrawn
-- and the discussion can stand: that is what a forum thread is, and it is what a
-- reader who has read forty replies is owed.
--
-- `root_message_id` becomes optional and `root_deleted_at` records when the subject
-- went, so a thread can say its subject was removed rather than presenting an
-- unexplained empty first line. It is not a content retention field: the root's
-- *text* is gone with the row, and nothing here keeps a copy of it.
--
-- The table is rebuilt rather than altered, because SQLite cannot drop a foreign key
-- or widen a NOT NULL column in place, and a cascade is the whole problem. Every row
-- is carried across, nothing invented, nothing dropped.

CREATE TABLE threads_rebuilt (
    id TEXT PRIMARY KEY,
    -- A thread lives inside a channel conversation and inherits its authorization
    -- wholesale; there is no separate access rule to keep in step with the channel's.
    conversation_id TEXT NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
    -- The message the thread is about. It is nullable because deleting the subject
    -- tombstones the thread rather than removing it; `root_deleted_at` says when.
    root_message_id TEXT REFERENCES messages(id) ON DELETE SET NULL,
    -- A bounded label, supplied by the person who started the thread. It is never used
    -- for lookup, so a rename changes nothing anyone linked to.
    title TEXT NOT NULL CHECK(length(title) BETWEEN 1 AND 120),
    -- 'active', 'archived' or 'locked'. Archived hides the thread from the channel
    -- list and stops replies; locked stops replies and leaves everything else alone.
    -- Neither is a deletion, and neither changes access.
    state TEXT NOT NULL DEFAULT 'active' CHECK(state IN ('active','archived','locked')),
    created_by TEXT REFERENCES users(id) ON DELETE SET NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    -- Set when the subject message is deleted. Empty for a thread that still has one,
    -- and for a thread from a build before this column existed.
    root_deleted_at TEXT
);

INSERT INTO threads_rebuilt(id,conversation_id,root_message_id,title,state,created_by,created_at,updated_at,root_deleted_at)
    SELECT id,conversation_id,root_message_id,title,state,created_by,created_at,updated_at,'' FROM threads;

DROP TABLE threads;
ALTER TABLE threads_rebuilt RENAME TO threads;

-- Recreated because dropping the table took them with it. The unique root index is
-- partial now: a tombstoned thread has no root, and several of them may, so a plain
-- unique index would forbid the second.
CREATE INDEX threads_channel ON threads(conversation_id, state, created_at DESC);
CREATE UNIQUE INDEX threads_root ON threads(root_message_id) WHERE root_message_id IS NOT NULL;
