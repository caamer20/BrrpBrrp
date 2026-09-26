-- Canonical direct pairs retain stable participant identifiers after physical
-- account erasure. They identify history, never grant access; current accepted
-- membership and active identity are checked for every read/write.
CREATE TABLE direct_pairs (
    conversation_id TEXT PRIMARY KEY REFERENCES conversations(id) ON DELETE CASCADE,
    user_low TEXT NOT NULL,
    user_high TEXT NOT NULL,
    UNIQUE(user_low,user_high),
    CHECK(user_low < user_high)
);
CREATE INDEX direct_pairs_high ON direct_pairs(user_high);
CREATE TABLE direct_requests (
    conversation_id TEXT PRIMARY KEY REFERENCES conversations(id) ON DELETE CASCADE,
    requester_id TEXT NOT NULL,
    recipient_id TEXT NOT NULL,
    state TEXT NOT NULL CHECK(state IN ('pending','accepted','rejected')),
    introduction TEXT NOT NULL DEFAULT '' CHECK(length(introduction)<=500),
    requested_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    retry_after TEXT NOT NULL DEFAULT ''
);
CREATE INDEX direct_requests_sender ON direct_requests(requester_id,requested_at);
CREATE INDEX direct_requests_recipient ON direct_requests(recipient_id,state);
CREATE TABLE user_blocks (
    blocker_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    blocked_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at TEXT NOT NULL,
    PRIMARY KEY(blocker_id,blocked_id),
    CHECK(blocker_id<>blocked_id)
);
CREATE TABLE contact_preferences (
    user_id TEXT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    allow_requests INTEGER NOT NULL DEFAULT 1 CHECK(allow_requests IN (0,1)),
    updated_at TEXT NOT NULL
);
CREATE TABLE direct_preferences (
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    conversation_id TEXT NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
    hidden INTEGER NOT NULL DEFAULT 0 CHECK(hidden IN (0,1)),
    updated_at TEXT NOT NULL,
    PRIMARY KEY(user_id,conversation_id)
);
CREATE TABLE conversation_reads (
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    conversation_id TEXT NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
    last_read_message_id TEXT NOT NULL DEFAULT '',
    last_read_at TEXT NOT NULL,
    PRIMARY KEY(user_id,conversation_id)
);
ALTER TABLE attachments ADD COLUMN staged_conversation_id TEXT REFERENCES conversations(id) ON DELETE CASCADE;
