-- Community message reports retain only the selected message's bounded text
-- evidence. They are never a route into DMs/groups or a channel the reviewer
-- cannot currently read. Deleting the source message does not erase evidence;
-- deleting the channel removes it because no channel permission can survive.
CREATE TABLE message_reports (
    id TEXT PRIMARY KEY,
    message_id TEXT NOT NULL,
    channel_id TEXT NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
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
CREATE UNIQUE INDEX message_reports_active_reporter_message
    ON message_reports(reporter_id, message_id) WHERE status='open';
CREATE INDEX message_reports_queue ON message_reports(status, id DESC);
CREATE INDEX message_reports_channel ON message_reports(channel_id, status, id DESC);
CREATE INDEX message_reports_expiry ON message_reports(expires_at);
