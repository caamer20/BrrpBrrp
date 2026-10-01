-- Admit group-message interruption preferences (ADR 0016).
--
-- Migration 15 constrained `private_notification_preferences.kind` to
-- 'direct_message' and 'incoming_request' — the kinds that existed then — while its
-- own comment said "or later kinds". Migration 17 widened `account_activity.source_kind`
-- for the group kind but did not widen this table, and the preference route accepted
-- `group_message` and then failed its write against the CHECK. A member could not mute
-- group-message interruptions at all, and the failure surfaced as a 500 rather than as
-- the 400 an unknown kind earns. Found by the private-state recovery rehearsal when it
-- began setting a preference before the snapshot (review 17, V17-05).
--
-- SQLite cannot alter a CHECK in place. The table is rebuilt with the wider set,
-- carrying every row across, the same rebuild discipline migration 17 used for the
-- activity table. The partial index used by the dispatch-time mute check is recreated
-- with the table it belongs to.

CREATE TABLE private_notification_preferences_rebuilt (
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    kind TEXT NOT NULL CHECK(kind IN ('direct_message','incoming_request','group_message')),
    muted INTEGER NOT NULL DEFAULT 0 CHECK(muted IN (0,1)),
    show_sender INTEGER NOT NULL DEFAULT 0 CHECK(show_sender IN (0,1)),
    updated_at TEXT NOT NULL,
    PRIMARY KEY(user_id, kind)
);

INSERT INTO private_notification_preferences_rebuilt(user_id,kind,muted,show_sender,updated_at)
    SELECT user_id,kind,muted,show_sender,updated_at FROM private_notification_preferences;

DROP TABLE private_notification_preferences;
ALTER TABLE private_notification_preferences_rebuilt RENAME TO private_notification_preferences;

CREATE INDEX private_notification_preferences_muted
    ON private_notification_preferences(user_id) WHERE muted=1;
