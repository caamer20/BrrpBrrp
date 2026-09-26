-- Session management (WP-05).
--
-- Sessions become first-class objects an account can list and revoke, and carry
-- the timestamp of the last strong authentication so privileged actions can
-- require a recent one.
--
-- Compatibility contract: existing sessions keep working and keep their expiry.
-- The new columns default to empty, which means "unknown client" and "not
-- recently authenticated"; neither grants anything.

ALTER TABLE sessions ADD COLUMN last_seen_at TEXT NOT NULL DEFAULT '';
ALTER TABLE sessions ADD COLUMN user_agent TEXT NOT NULL DEFAULT '';
ALTER TABLE sessions ADD COLUMN privileged_at TEXT NOT NULL DEFAULT '';
CREATE INDEX sessions_user_seen ON sessions(user_id, last_seen_at);

-- Existing live sessions are treated as seen at their creation time rather than
-- as never seen, so a session list is not misleading about age.
UPDATE sessions SET last_seen_at = created_at WHERE last_seen_at = '';
