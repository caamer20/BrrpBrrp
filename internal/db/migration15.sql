-- Durable account activity, private notification preferences and typed push
-- destinations (ADR 0015).
--
-- Nothing that exists today is changed or reinterpreted. Unread counts and the
-- outstanding request count stay derived, so they cannot drift from history, and
-- `channel_notification_preferences` stays channel-scoped and stays about
-- interruption rather than history. This adds the one thing that was missing: a
-- durable record that something happened, addressed to an account, that survives
-- the recipient being offline.
--
-- `account_activity` stores identifiers, a kind and a timestamp. It never stores a
-- second copy of message text or of a request introduction: a row is a pointer to
-- something that already exists and is read under that thing's own authorization.
-- A private push payload is derived from this row at dispatch and is likewise
-- content-free by construction (ADR 0015, "What a private notification may say").

CREATE TABLE account_activity (
    id TEXT PRIMARY KEY,
    -- The account this is addressed to. Cascading on erasure is correct: this is
    -- the erased account's own record of what it was told.
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    -- 'message', 'request', 'group_message' or later sources. Paired with
    -- `source_id`, which is a message id or a request attempt id -- never a
    -- conversation id alone, so a pair that renews its request produces two
    -- distinct events for one conversation.
    source_kind TEXT NOT NULL CHECK(source_kind IN ('message','request','group_message')),
    source_id TEXT NOT NULL,
    -- Provenance only, exactly as the evidence tables store it. It is never
    -- returned to a reader: an identifier is not permission to browse a
    -- conversation, and this table is readable by its own recipient.
    conversation_id TEXT NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
    -- Who did it. Cleared by erasure so no record of an erased account's
    -- identity remains, while the row itself stays for the recipient.
    actor_id TEXT REFERENCES users(id) ON DELETE SET NULL,
    created_at TEXT NOT NULL,
    -- Acknowledgment is durable, per account and monotonic. It is a separate fact
    -- from unread (derived) and from delivery (a device fact the service worker
    -- owns), and it is what the bounded-storage pass reclaims first.
    acknowledged_at TEXT NOT NULL DEFAULT '',
    -- 'push' when this row may interrupt the recipient, 'none' when it only
    -- appears in the list. A mute moves a row here rather than deleting it: muting
    -- suppresses interruption, not history (ADR 0015, "Preference precedence").
    interruptible INTEGER NOT NULL DEFAULT 1 CHECK(interruptible IN (0,1))
);

-- One row per event per recipient. This is what makes a retried send, a replayed
-- request, or a recomputation idempotent rather than duplicating an interruption.
CREATE UNIQUE INDEX account_activity_identity
    ON account_activity(user_id, source_kind, source_id);

-- The list is read newest first and paginated on id, so this is the access path.
CREATE INDEX account_activity_recent ON account_activity(user_id, id DESC);
-- The bounded-storage pass reclaims acknowledged rows first and only ever
-- unacknowledged ones as a last resort, so it needs the acknowledgment index.
CREATE INDEX account_activity_unacknowledged
    ON account_activity(user_id, created_at) WHERE acknowledged_at='';
-- Eligibility is recomputed at dispatch, not at enqueue, so a queued notice cannot
-- disclose activity the recipient lost access to while it waited.
CREATE INDEX account_activity_dispatch ON account_activity(id) WHERE interruptible=1;

-- Private notification preferences. Account-level and per kind, because a direct
-- conversation has no channel to attach a preference to, and one account's
-- treatment of a stranger's request is not a property of a channel they have no
-- membership in. Separate from channel_notification_preferences, which stays
-- channel-scoped and stays about interruption.
CREATE TABLE private_notification_preferences (
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    -- 'direct_message', 'incoming_request' or later kinds.
    kind TEXT NOT NULL CHECK(kind IN ('direct_message','incoming_request')),
    -- 0 permits an interruption, 1 suppresses it. Neither affects the activity
    -- row, the unread count or whether the conversation can be read: a mute is
    -- about being interrupted, not about permission.
    muted INTEGER NOT NULL DEFAULT 0 CHECK(muted IN (0,1)),
    -- Whether a push names the sender. The default is 0: a lock-screen preview is
    -- the one place where private text most easily leaks, so the quieter choice is
    -- the default and the account opts in (ADR 0015).
    show_sender INTEGER NOT NULL DEFAULT 0 CHECK(show_sender IN (0,1)),
    updated_at TEXT NOT NULL,
    PRIMARY KEY(user_id, kind)
);

-- A blocked pair's activity is written but never delivered, and the blocked pair
-- itself is how delivery is refused. This index serves that per-dispatch check.
CREATE INDEX private_notification_preferences_muted
    ON private_notification_preferences(user_id) WHERE muted=1;
