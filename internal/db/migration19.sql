-- Named community threads (ADR 0017).
--
-- A thread narrows which messages a view shows; it does not duplicate them. There is
-- no thread message table, no thread membership and no thread read cursor: a thread is
-- a channel conversation with a subject, and every rule that already governed a channel
-- governs it unchanged.
--
-- `messages.thread_id` is empty for an unthreaded message. Adding the column rather
-- than re-parenting rows under a synthetic root is deliberate: a message's identity and
-- its link do not change, so an old link keeps resolving.

ALTER TABLE messages ADD COLUMN thread_id TEXT;

CREATE TABLE threads (
    id TEXT PRIMARY KEY,
    -- A thread lives inside a channel conversation and inherits its authorization
    -- wholesale; there is no separate access rule to keep in step with the channel's.
    conversation_id TEXT NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
    -- The message the thread is about. It is what a reader sees first, so it is the
    -- thread's subject; deleting it tombstones the thread rather than leaving replies
    -- attached to nothing.
    root_message_id TEXT NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
    -- A bounded label, supplied by the person who started the thread. It is never used
    -- for lookup, so a rename changes nothing anyone linked to.
    title TEXT NOT NULL CHECK(length(title) BETWEEN 1 AND 120),
    -- 'active', 'archived' or 'locked'. Archived hides the thread from the channel
    -- list and stops replies; locked stops replies and leaves everything else alone.
    -- Neither is a deletion, and neither changes access.
    state TEXT NOT NULL DEFAULT 'active' CHECK(state IN ('active','archived','locked')),
    created_by TEXT REFERENCES users(id) ON DELETE SET NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE INDEX threads_channel ON threads(conversation_id, state, created_at DESC);
CREATE UNIQUE INDEX threads_root ON threads(root_message_id);

-- Replies carry the thread they belong to. The root is the message it was started
-- from, so every message in a thread is reachable by one filter.
CREATE INDEX messages_thread ON messages(thread_id, id) WHERE thread_id IS NOT NULL;
-- The channel's own listing excludes thread replies, so a busy thread does not bury
-- the channel it lives in.
CREATE INDEX messages_channel_unthreaded ON messages(conversation_id, id) WHERE thread_id IS NULL;

-- Following is a personal subscription, not read state: it decides what reaches an
-- account's activity and nothing else. Bounded on the write, so following cannot grow
-- a table without limit.
CREATE TABLE thread_follows (
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    thread_id TEXT NOT NULL REFERENCES threads(id) ON DELETE CASCADE,
    created_at TEXT NOT NULL,
    PRIMARY KEY(user_id, thread_id)
);

CREATE INDEX thread_follows_thread ON thread_follows(thread_id);

-- `thread_reply` is a new activity source (ADR 0017). The CHECK that enumerates the
-- sources is what makes an unknown kind impossible rather than merely unhandled, and
-- SQLite cannot widen one in place, so the table is rebuilt the way migrations 11, 17
-- and 18 rebuilt theirs: every row carried across, nothing invented, nothing dropped.
-- Adding the kind to the table rather than to the Go constant alone is deliberate — a
-- source the schema refuses is a write that fails at commit, which is exactly how
-- this would otherwise be discovered.
CREATE TABLE account_activity_rebuilt (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    source_kind TEXT NOT NULL CHECK(source_kind IN ('message','request','group_message','thread_reply')),
    source_id TEXT NOT NULL,
    conversation_id TEXT NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
    actor_id TEXT REFERENCES users(id) ON DELETE SET NULL,
    created_at TEXT NOT NULL,
    acknowledged_at TEXT NOT NULL DEFAULT '',
    interruptible INTEGER NOT NULL DEFAULT 1 CHECK(interruptible IN (0,1))
);

INSERT INTO account_activity_rebuilt(id,user_id,source_kind,source_id,conversation_id,actor_id,created_at,acknowledged_at,interruptible)
    SELECT id,user_id,source_kind,source_id,conversation_id,actor_id,created_at,acknowledged_at,interruptible
    FROM account_activity;

DROP TABLE account_activity;
ALTER TABLE account_activity_rebuilt RENAME TO account_activity;

-- The indexes are recreated because dropping the table took them with it, and the
-- identity index is what keeps a retried reply from producing a second interruption
-- for the same follower.
CREATE UNIQUE INDEX account_activity_identity
    ON account_activity(user_id, source_kind, source_id);
CREATE INDEX account_activity_recent ON account_activity(user_id, id DESC);
CREATE INDEX account_activity_unacknowledged
    ON account_activity(user_id, created_at) WHERE acknowledged_at='';
CREATE INDEX account_activity_dispatch ON account_activity(id) WHERE interruptible=1;
