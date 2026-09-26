ALTER TABLE users ADD COLUMN is_deactivated INTEGER NOT NULL DEFAULT 0;
ALTER TABLE messages ADD COLUMN reply_to_id TEXT REFERENCES messages(id) ON DELETE SET NULL;
CREATE INDEX messages_reply ON messages(reply_to_id);
CREATE TABLE message_pins (
    message_id TEXT PRIMARY KEY REFERENCES messages(id) ON DELETE CASCADE,
    pinned_by TEXT REFERENCES users(id) ON DELETE SET NULL,
    created_at TEXT NOT NULL
);
CREATE TABLE invitations (
    id TEXT PRIMARY KEY,
    token_hash TEXT NOT NULL UNIQUE,
    created_by TEXT REFERENCES users(id) ON DELETE SET NULL,
    created_at TEXT NOT NULL,
    expires_at TEXT NOT NULL,
    used_at TEXT
);
CREATE TABLE user_mfa (
    user_id TEXT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    secret TEXT NOT NULL DEFAULT '',
    pending_secret TEXT NOT NULL DEFAULT '',
    pending_until TEXT NOT NULL DEFAULT '',
    last_counter INTEGER NOT NULL DEFAULT -1
);
CREATE TABLE recovery_codes (
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    code_hash TEXT NOT NULL,
    PRIMARY KEY(user_id, code_hash)
);
