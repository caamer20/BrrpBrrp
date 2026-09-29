-- Consenting groups on the shared conversation model (ADR 0016).
--
-- A group is a `conversations` row with kind='group' and members in
-- `conversation_members`. The schema has said 'group' since migration 6 and every
-- access check is written against membership rather than against "is this a direct
-- pair", so this adds two tables and no second message store, no second
-- authorization path and no second set of counters.
--
-- `group_owners` is a conversation-level row rather than a column on the inviter's
-- membership, because ownership governs three things — who may invite, who may
-- remove, and who may transfer — and inferring it from "who created this" would make
-- a transfer meaningless.

CREATE TABLE group_owners (
    conversation_id TEXT PRIMARY KEY REFERENCES conversations(id) ON DELETE CASCADE,
    -- The owner is always an accepted member. That is a property of the transition
    -- rather than of the schema, because a CHECK cannot read another table, and it
    -- is enforced in the same transaction as every change to the owner.
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    updated_at TEXT NOT NULL
);

-- A group name is the one piece of content a group has. It is length-bounded and
-- stripped, and it is a label rather than a permission: joining does not depend on
-- it and it is never used for lookup.
--
-- The ten-member and ten-invitation bounds live in the code that enforces them in
-- the accepting transaction, because a CHECK cannot count rows in another table and
-- a trigger would have to re-derive the count on every path. They are stated in
-- ADR 0016 rather than left implicit.
CREATE TABLE group_details (
    conversation_id TEXT PRIMARY KEY REFERENCES conversations(id) ON DELETE CASCADE,
    name TEXT NOT NULL CHECK(length(name) BETWEEN 1 AND 80)
);

-- Group invitations are durable, not memory: a restart cannot resurrect a revoked or
-- expired one, and a replayed acceptance is the same acceptance rather than a second
-- membership. `state` is 'pending' until the recipient acts; the row is removed when
-- it does, which is what makes a revoked invitation genuinely gone rather than
-- merely marked.
CREATE TABLE group_invitations (
    id TEXT PRIMARY KEY,
    conversation_id TEXT NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
    inviter_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    invitee_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    state TEXT NOT NULL DEFAULT 'pending' CHECK(state IN ('pending','accepted','rejected','revoked','expired')),
    created_at TEXT NOT NULL,
    expires_at TEXT NOT NULL,
    responded_at TEXT NOT NULL DEFAULT '',
    -- Replay protection. One open invitation per (group, invitee): a second
    -- invitation to the same person is the first one, not a duplicate, so a
    -- retried send cannot produce two acceptances to reconcile.
    CHECK(inviter_id <> invitee_id)
);

CREATE UNIQUE INDEX group_invitations_open
    ON group_invitations(conversation_id, invitee_id) WHERE state='pending';
-- The membership table already has (conversation_id, user_id) as its key, so an
-- accepted member is unique by construction. This index serves the per-invitee
-- lookup the accept path does.
CREATE INDEX group_invitations_invitee ON group_invitations(invitee_id, state);
CREATE INDEX group_invitations_expiry ON group_invitations(expires_at) WHERE state='pending';

-- One open invitation per invitee per group is enforced above. This index serves the
-- per-account sent bound, which is counted rather than constrained because the
-- bound is a rate rather than an invariant (ADR 0016, "Bounds").
CREATE INDEX group_invitations_inviter ON group_invitations(inviter_id, created_at);
