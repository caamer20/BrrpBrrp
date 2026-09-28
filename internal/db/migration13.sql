-- Private message evidence: one voluntarily submitted, immutable snapshot of a
-- single currently readable accepted-direct message (ADR 0013).
--
-- This is not a second message store and not a route into a conversation. It holds
-- no conversation id, no membership, no attachment reference and no history: only
-- the selected message's bounded text, the two account ids that describe it, and
-- the reporter's own explanation. A reviewer gains nothing here beyond the snapshot
-- the reporter chose to disclose.
--
-- It is a separate table from `message_reports` rather than a new kind of row in it
-- on purpose. The community table requires a channel and cascades on channel
-- deletion, and its review policy is channel-scoped read access plus
-- `message.delete_any`. A private message has no channel projection, and widening
-- that table's SELECT would put private rows in front of reviewers whose only grant
-- is community moderation. The mechanics (expiry, pagination, resolve/reopen,
-- bounded cleanup) are shared code; the disclosure boundary is not.
--
-- The source message has no foreign key on purpose: deleting the message does not
-- erase evidence a reporter already submitted. Identities are cleared rather than
-- cascaded, so a removed account leaves no identity attached to a record that must
-- still be reviewable. Expiry is 90 days from creation and is never extended by a
-- resolve or reopen, so the column is set once and only ever read.
CREATE TABLE private_message_reports (
    id TEXT PRIMARY KEY,
    message_id TEXT NOT NULL,
    conversation_id TEXT NOT NULL,
    reporter_id TEXT REFERENCES users(id) ON DELETE SET NULL,
    author_id TEXT REFERENCES users(id) ON DELETE SET NULL,
    reason TEXT NOT NULL CHECK(length(reason) BETWEEN 1 AND 2000),
    snapshot_text TEXT NOT NULL CHECK(length(snapshot_text) <= 4000),
    status TEXT NOT NULL DEFAULT 'open' CHECK(status IN ('open','resolved')),
    review_note TEXT NOT NULL DEFAULT '' CHECK(length(review_note) <= 2000),
    reviewed_by TEXT REFERENCES users(id) ON DELETE SET NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    expires_at TEXT NOT NULL
);

-- One open report per reporter per message, so a concurrent retry deduplicates
-- against the original evidence instead of replacing it. Same shape and same
-- partial-index trick as the community table, deliberately.
CREATE UNIQUE INDEX private_message_reports_active_reporter_message
    ON private_message_reports(reporter_id, message_id) WHERE status='open';
CREATE INDEX private_message_reports_queue ON private_message_reports(status, id DESC);
CREATE INDEX private_message_reports_expiry ON private_message_reports(expires_at);

-- The per-account submission bound is durable, unlike the community limiter, which
-- is an in-memory sliding window a restart resets. Private evidence is disclosed
-- data with a storage cost, so the limit that protects the account is recorded where
-- a restart cannot clear it. The counter is a window start plus the number of
-- submissions in that window; the account row is removed with the account.
CREATE TABLE private_report_rate_limits (
    user_id TEXT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    window_started_at TEXT NOT NULL,
    submissions INTEGER NOT NULL CHECK(submissions >= 0)
);
