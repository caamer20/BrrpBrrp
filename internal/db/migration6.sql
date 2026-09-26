-- M-C: the shared conversation descriptor, its membership table and deletion-safe send
-- receipts (ADR 0010 / D-11).
--
-- Additive and forward-only. A channel conversation's id is its channel id, so legacy
-- message ids, deep links, search hits, unread state and the existing authorization path
-- are unchanged. messages.conversation_id is added nullable and backfilled; channel_id
-- stays NOT NULL and authoritative for channel conversations, and the compatibility reader
-- is COALESCE(conversation_id, channel_id). Tightening either column requires the
-- rowid-preserving table rebuild that ADR 0010 places in a later stage, with foreign keys
-- disabled outside the transaction.
CREATE TABLE IF NOT EXISTS conversations (
    id TEXT PRIMARY KEY,
    kind TEXT NOT NULL CHECK(kind IN ('channel','direct','group')),
    channel_id TEXT UNIQUE REFERENCES channels(id) ON DELETE CASCADE,
    state TEXT NOT NULL DEFAULT 'active',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS conversation_members (
    conversation_id TEXT NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    state TEXT NOT NULL CHECK(state IN ('pending','accepted','left','removed')),
    invited_by TEXT REFERENCES users(id) ON DELETE SET NULL,
    joined_at TEXT,
    updated_at TEXT NOT NULL,
    PRIMARY KEY(conversation_id, user_id)
);
CREATE INDEX IF NOT EXISTS conversation_members_user ON conversation_members(user_id, state);

-- One conversation per channel, created by the schema rather than by application code: the
-- migration backfills the channels that already exist, and this trigger creates the mapping for
-- every channel inserted afterwards, in the same transaction as the channel. Neither path lets a
-- channel exist without its conversation, so no request has to repair the mapping.
INSERT INTO conversations(id, kind, channel_id, state, created_at, updated_at)
    SELECT id, 'channel', id, 'active', created_at, updated_at FROM channels
    WHERE id NOT IN (SELECT id FROM conversations);
CREATE TRIGGER IF NOT EXISTS channels_conversation AFTER INSERT ON channels BEGIN
    INSERT INTO conversations(id, kind, channel_id, state, created_at, updated_at)
        VALUES(new.id, 'channel', new.id, 'active', new.created_at, new.updated_at);
END;

-- Every message gains its conversation scope. This is an UPDATE of one indexed column: ids,
-- rowids, reply and attachment references and content are untouched, so the FTS index (which
-- is keyed on rowid and rebuilt from content) stays valid.
ALTER TABLE messages ADD COLUMN conversation_id TEXT;
UPDATE messages SET conversation_id = channel_id WHERE conversation_id IS NULL;
CREATE INDEX IF NOT EXISTS messages_conversation ON messages(conversation_id, id DESC);

-- Deletion-safe send receipts. The operation identity is (account, key); the destination is
-- part of the intent digest, so reusing a key in another conversation or with other content
-- conflicts instead of silently returning unrelated work. No foreign key on conversation_id
-- or result_message_id: the receipt must survive the deletion of the content it acknowledges,
-- which is the whole point of it.
CREATE TABLE IF NOT EXISTS message_operations (
    account_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    idempotency_key TEXT NOT NULL,
    conversation_id TEXT NOT NULL,
    intent_digest TEXT NOT NULL DEFAULT '',
    state TEXT NOT NULL DEFAULT 'committed' CHECK(state IN ('committed','deleted')),
    result_message_id TEXT,
    created_at TEXT NOT NULL,
    expires_at TEXT NOT NULL,
    PRIMARY KEY(account_id, idempotency_key)
);
CREATE INDEX IF NOT EXISTS message_operations_expiry ON message_operations(expires_at);
CREATE INDEX IF NOT EXISTS message_operations_result ON message_operations(result_message_id);

-- Messages that predate the migration get a receipt with an empty digest: the intent was not
-- recorded, so a replay is answered from the message itself and a missing message is a
-- non-recreating conflict. Nothing is inferred from content that may no longer exist.
INSERT INTO message_operations(account_id, idempotency_key, conversation_id, intent_digest, state, result_message_id, created_at, expires_at)
    SELECT user_id, idempotency_key, COALESCE(conversation_id, channel_id), '', 'committed', id, created_at, created_at
    FROM messages
    WHERE id NOT IN (SELECT result_message_id FROM message_operations WHERE result_message_id IS NOT NULL);

-- Erasing the content marks the receipt's outcome in the same transaction, so a later retry
-- can never recreate a message the server has deleted or purged.
CREATE TRIGGER IF NOT EXISTS message_operations_deleted AFTER DELETE ON messages BEGIN
    UPDATE message_operations SET state='deleted', result_message_id=NULL WHERE result_message_id=old.id;
END;
