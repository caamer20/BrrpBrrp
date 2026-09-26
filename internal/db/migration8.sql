-- Channel slow mode is disabled by default. Its durable clock belongs to the
-- account and channel, not to a message row: deleting a message must not reset it.
ALTER TABLE channels ADD COLUMN slow_mode_seconds INTEGER NOT NULL DEFAULT 0
    CHECK(slow_mode_seconds BETWEEN 0 AND 21600);

CREATE TABLE channel_slow_mode_sends (
    channel_id TEXT NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    last_sent_at TEXT NOT NULL,
    PRIMARY KEY(channel_id, user_id)
);

INSERT INTO channel_slow_mode_sends(channel_id, user_id, last_sent_at)
    SELECT channel_id, user_id, MAX(created_at) FROM messages
    GROUP BY channel_id, user_id;
