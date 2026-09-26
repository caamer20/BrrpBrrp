-- Personal notification preferences affect interruption, not history, unread
-- counts, mention records, or permission to read a channel.
CREATE TABLE channel_notification_preferences (
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    channel_id TEXT NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
    muted INTEGER NOT NULL DEFAULT 0 CHECK(muted IN (0,1)),
    updated_at TEXT NOT NULL,
    PRIMARY KEY(user_id, channel_id)
);

CREATE INDEX channel_notification_preferences_muted
    ON channel_notification_preferences(channel_id, user_id) WHERE muted=1;
