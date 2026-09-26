-- The migration runner pins one connection and disables foreign_keys before
-- BEGIN. DROP TABLE with enforcement enabled would cascade-delete message
-- dependants. Every rowid is copied explicitly for the external-content FTS index.
CREATE TABLE messages_new (
    id TEXT PRIMARY KEY,
    conversation_id TEXT NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
    channel_id TEXT REFERENCES channels(id) ON DELETE CASCADE,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    content TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    edited_at TEXT,
    idempotency_key TEXT NOT NULL,
    embed_json TEXT,
    embed_status TEXT NOT NULL DEFAULT 'pending',
    reply_to_id TEXT REFERENCES messages(id) ON DELETE SET NULL,
    UNIQUE(user_id, idempotency_key),
    CHECK(channel_id IS NULL OR channel_id=conversation_id)
);

INSERT INTO messages_new(rowid,id,conversation_id,channel_id,user_id,content,created_at,edited_at,idempotency_key,embed_json,embed_status,reply_to_id)
    SELECT rowid,id,COALESCE(conversation_id,channel_id),channel_id,user_id,content,created_at,edited_at,idempotency_key,embed_json,embed_status,reply_to_id FROM messages;
DROP TABLE messages;
ALTER TABLE messages_new RENAME TO messages;

CREATE INDEX messages_channel ON messages(channel_id,id DESC);
CREATE INDEX messages_conversation ON messages(conversation_id,id DESC);
CREATE INDEX messages_user ON messages(user_id);
CREATE INDEX messages_created ON messages(created_at);
CREATE INDEX messages_reply ON messages(reply_to_id);

CREATE TRIGGER messages_insert AFTER INSERT ON messages BEGIN
    INSERT INTO message_fts(rowid,content) VALUES(new.rowid,new.content);
END;
CREATE TRIGGER messages_update AFTER UPDATE OF content ON messages BEGIN
    INSERT INTO message_fts(message_fts,rowid,content) VALUES('delete',old.rowid,old.content);
    INSERT INTO message_fts(rowid,content) VALUES(new.rowid,new.content);
END;
CREATE TRIGGER messages_delete AFTER DELETE ON messages BEGIN
    INSERT INTO message_fts(message_fts,rowid,content) VALUES('delete',old.rowid,old.content);
END;
CREATE TRIGGER message_operations_deleted AFTER DELETE ON messages BEGIN
    UPDATE message_operations SET state='deleted',result_message_id=NULL WHERE result_message_id=old.id;
END;

-- A nullable projection is not permission to attach private content to a public
-- channel. Bind the projection to the descriptor on every new or moved message.
CREATE TRIGGER messages_scope_insert BEFORE INSERT ON messages
WHEN NOT EXISTS(SELECT 1 FROM conversations c WHERE c.id=NEW.conversation_id AND
    ((c.kind='channel' AND c.channel_id=NEW.channel_id AND c.id=NEW.channel_id) OR
     (c.kind IN ('direct','group') AND c.channel_id IS NULL AND NEW.channel_id IS NULL)))
BEGIN SELECT RAISE(ABORT,'message conversation scope is invalid'); END;
CREATE TRIGGER messages_scope_update BEFORE UPDATE OF conversation_id,channel_id ON messages
WHEN NOT EXISTS(SELECT 1 FROM conversations c WHERE c.id=NEW.conversation_id AND
    ((c.kind='channel' AND c.channel_id=NEW.channel_id AND c.id=NEW.channel_id) OR
     (c.kind IN ('direct','group') AND c.channel_id IS NULL AND NEW.channel_id IS NULL)))
BEGIN SELECT RAISE(ABORT,'message conversation scope is invalid'); END;

-- Descriptor edits cannot turn an existing channel's history into private
-- content, or private content into a community projection.
CREATE TRIGGER conversations_message_scope_update BEFORE UPDATE OF kind,channel_id ON conversations
WHEN EXISTS(SELECT 1 FROM messages m WHERE m.conversation_id=OLD.id AND NOT
    ((NEW.kind='channel' AND NEW.channel_id IS NOT NULL AND m.channel_id IS NOT NULL AND NEW.channel_id=m.channel_id AND NEW.id=m.channel_id) OR
     (NEW.kind IN ('direct','group') AND NEW.channel_id IS NULL AND m.channel_id IS NULL)))
BEGIN SELECT RAISE(ABORT,'conversation change would invalidate message scope'); END;
