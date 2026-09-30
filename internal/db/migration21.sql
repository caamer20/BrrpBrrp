-- A member's own export, and the record of what it could not include (ADR 0019b).
--
-- The file a member downloads is on their machine; what the server keeps is a record
-- of what was produced, when, how much, and — importantly — what it could not include.
-- Without that last part an export that stopped at its bound is indistinguishable from
-- a complete one, and a member cannot tell the difference from the outside.

CREATE TABLE data_exports (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    operation_key TEXT NOT NULL,
    -- The state of the job: 'building', 'ready' or 'failed'.
    state TEXT NOT NULL DEFAULT 'building' CHECK(state IN ('building','ready','failed')),
    created_at TEXT NOT NULL,
    completed_at TEXT NOT NULL DEFAULT '',
    -- What the file contains and, more importantly, what it does not. `omissions` is a
    -- human-readable list of reasons the export stopped or skipped something.
    message_count INTEGER NOT NULL DEFAULT 0,
    attachment_count INTEGER NOT NULL DEFAULT 0,
    conversation_count INTEGER NOT NULL DEFAULT 0,
    bytes_written INTEGER NOT NULL DEFAULT 0,
    truncated INTEGER NOT NULL DEFAULT 0 CHECK(truncated IN (0,1)),
    omissions TEXT NOT NULL DEFAULT '',
    -- A produced file has a lifetime and is removed on its own. The server keeping a
    -- copy of one account's history indefinitely is a liability that grows with every
    -- export ever run.
    expires_at TEXT NOT NULL DEFAULT '',
    completed_by TEXT REFERENCES users(id) ON DELETE SET NULL,
    -- A fresh-authentication gate records when it was satisfied, because an export
    -- leaves the building and is the one read path that does.
    fresh_auth_at TEXT NOT NULL DEFAULT '',
    UNIQUE(user_id, operation_key)
);

CREATE INDEX data_exports_pending ON data_exports(user_id, state);
-- The expiry sweep is bounded by this index rather than by scanning the table.
CREATE INDEX data_exports_expiry ON data_exports(expires_at) WHERE expires_at<>'';
