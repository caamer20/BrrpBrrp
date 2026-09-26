CREATE TABLE IF NOT EXISTS schema_migrations (version INTEGER PRIMARY KEY);

CREATE TABLE IF NOT EXISTS users (
    id TEXT PRIMARY KEY,
    username TEXT NOT NULL UNIQUE COLLATE NOCASE,
    display_name TEXT NOT NULL DEFAULT '',
    password_hash TEXT NOT NULL,
    is_admin INTEGER NOT NULL DEFAULT 0,
    is_banned INTEGER NOT NULL DEFAULT 0,
    ban_reason TEXT NOT NULL DEFAULT '',
    avatar_color TEXT NOT NULL DEFAULT '#89a8f4',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    last_seen_at TEXT
);
CREATE TABLE IF NOT EXISTS sessions (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash TEXT NOT NULL UNIQUE,
    created_at TEXT NOT NULL,
    expires_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS sessions_user ON sessions(user_id);
CREATE INDEX IF NOT EXISTS sessions_expiry ON sessions(expires_at);

CREATE TABLE IF NOT EXISTS channels (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    type TEXT NOT NULL CHECK(type IN ('text','voice')),
    category TEXT NOT NULL DEFAULT '',
    position INTEGER NOT NULL DEFAULT 0,
    topic TEXT NOT NULL DEFAULT '',
    max_voice_users INTEGER NOT NULL DEFAULT 8 CHECK(max_voice_users BETWEEN 1 AND 10),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS channels_order ON channels(type, position);

CREATE TABLE IF NOT EXISTS messages (
    id TEXT PRIMARY KEY,
    channel_id TEXT NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    content TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    edited_at TEXT,
    idempotency_key TEXT NOT NULL,
    embed_json TEXT,
    embed_status TEXT NOT NULL DEFAULT 'pending',
    UNIQUE(user_id, idempotency_key)
);
CREATE INDEX IF NOT EXISTS messages_channel ON messages(channel_id, id DESC);
CREATE INDEX IF NOT EXISTS messages_user ON messages(user_id);
CREATE INDEX IF NOT EXISTS messages_created ON messages(created_at);
CREATE VIRTUAL TABLE IF NOT EXISTS message_fts USING fts5(content, content='messages', content_rowid='rowid');
CREATE TRIGGER IF NOT EXISTS messages_insert AFTER INSERT ON messages BEGIN
    INSERT INTO message_fts(rowid, content) VALUES(new.rowid, new.content);
END;
CREATE TRIGGER IF NOT EXISTS messages_update AFTER UPDATE OF content ON messages BEGIN
    INSERT INTO message_fts(message_fts, rowid, content) VALUES('delete', old.rowid, old.content);
    INSERT INTO message_fts(rowid, content) VALUES(new.rowid, new.content);
END;
CREATE TRIGGER IF NOT EXISTS messages_delete AFTER DELETE ON messages BEGIN
    INSERT INTO message_fts(message_fts, rowid, content) VALUES('delete', old.rowid, old.content);
END;

CREATE TABLE IF NOT EXISTS attachments (
    id TEXT PRIMARY KEY,
    message_id TEXT REFERENCES messages(id) ON DELETE CASCADE,
    uploader_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    file_name TEXT NOT NULL,
    stored_name TEXT NOT NULL UNIQUE,
    file_size INTEGER NOT NULL,
    mime_type TEXT NOT NULL,
    created_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS attachments_message ON attachments(message_id);
CREATE TABLE IF NOT EXISTS reactions (
    message_id TEXT NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    emoji TEXT NOT NULL,
    created_at TEXT NOT NULL,
    PRIMARY KEY(message_id, user_id, emoji)
);
CREATE TABLE IF NOT EXISTS mentions (
    message_id TEXT NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
    channel_id TEXT NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
    target_user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    actor_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at TEXT NOT NULL,
    PRIMARY KEY(message_id, target_user_id)
);
CREATE INDEX IF NOT EXISTS mentions_target ON mentions(target_user_id, channel_id);
CREATE TABLE IF NOT EXISTS channel_reads (
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    channel_id TEXT NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
    last_read_message_id TEXT NOT NULL DEFAULT '',
    last_read_at TEXT NOT NULL,
    PRIMARY KEY(user_id, channel_id)
);
CREATE TABLE IF NOT EXISTS push_subscriptions (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    endpoint TEXT NOT NULL UNIQUE,
    p256dh TEXT NOT NULL,
    auth TEXT NOT NULL,
    created_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS push_user ON push_subscriptions(user_id);
CREATE TABLE IF NOT EXISTS settings (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS audit_logs (
    id TEXT PRIMARY KEY,
    actor_id TEXT REFERENCES users(id) ON DELETE SET NULL,
    action TEXT NOT NULL,
    target_id TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS audit_created ON audit_logs(created_at);
