-- Stage M-A: ownership, roles, role assignments and the committed
-- authorization revision.
--
-- Compatibility contract: every existing administrator keeps the authority it
-- had before this migration, ordinary members gain no new privileged
-- capability, and no ownership is inferred. An owner is designated only by an
-- explicit operator ceremony after the upgrade.

ALTER TABLE users ADD COLUMN is_owner INTEGER NOT NULL DEFAULT 0;
CREATE UNIQUE INDEX users_single_owner ON users(is_owner) WHERE is_owner = 1;

CREATE TABLE roles (
    id TEXT PRIMARY KEY,
    role_key TEXT NOT NULL UNIQUE,
    name TEXT NOT NULL,
    position INTEGER NOT NULL,
    is_default INTEGER NOT NULL DEFAULT 0,
    is_system INTEGER NOT NULL DEFAULT 0,
    permissions TEXT NOT NULL DEFAULT '[]',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE role_assignments (
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role_id TEXT NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
    assigned_by TEXT REFERENCES users(id) ON DELETE SET NULL,
    created_at TEXT NOT NULL,
    PRIMARY KEY(user_id, role_id)
);
CREATE INDEX role_assignments_role ON role_assignments(role_id);

CREATE TABLE authorization_state (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    revision INTEGER NOT NULL,
    updated_at TEXT NOT NULL
);

-- Stage M-B: durable categories, channel visibility, access grants and posting
-- policy.
--
-- Compatibility contract: existing channels stay public and members-postable,
-- keep their IDs, order, history and links, and remain reachable by every
-- active member. channels.category stays the denormalized category name so
-- existing reads and clients are unaffected; the categories table becomes the
-- durable, ordered object and is the authority for renames.

CREATE TABLE categories (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    position INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

ALTER TABLE channels ADD COLUMN visibility TEXT NOT NULL DEFAULT 'public' CHECK (visibility IN ('public', 'private'));
ALTER TABLE channels ADD COLUMN posting TEXT NOT NULL DEFAULT 'members' CHECK (posting IN ('members', 'announcement'));
ALTER TABLE channels ADD COLUMN authorization_revision INTEGER NOT NULL DEFAULT 1;

CREATE TABLE channel_role_access (
    channel_id TEXT NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
    role_id TEXT NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
    granted_by TEXT REFERENCES users(id) ON DELETE SET NULL,
    created_at TEXT NOT NULL,
    PRIMARY KEY(channel_id, role_id)
);

CREATE TABLE channel_user_access (
    channel_id TEXT NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    granted_by TEXT REFERENCES users(id) ON DELETE SET NULL,
    created_at TEXT NOT NULL,
    PRIMARY KEY(channel_id, user_id)
);

INSERT INTO categories(id, name, position, created_at, updated_at)
SELECT 'cat-' || lower(hex(randomblob(16))), grouped.name, grouped.position,
       strftime('%Y-%m-%dT%H:%M:%fZ', 'now'), strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
FROM (
    SELECT trim(category) AS name, min(position) AS position
    FROM channels
    WHERE trim(category) <> ''
    GROUP BY trim(category)
) AS grouped
ORDER BY grouped.position, grouped.name;

INSERT INTO roles(id, role_key, name, position, is_default, is_system, permissions, created_at, updated_at) VALUES
    ('role-admin', 'admin', 'Administrator', 80, 0, 1,
     '["channel.read","message.send","message.react","message.upload","message.pin","message.delete_any","mention.everyone","voice.connect","voice.speak","voice.share_screen","voice.moderate","member.manage","channel.manage","category.manage","role.manage","invite.manage","settings.manage","audit.read"]',
     strftime('%Y-%m-%dT%H:%M:%fZ', 'now'), strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    ('role-moderator', 'moderator', 'Moderator', 60, 0, 1,
     '["channel.read","message.send","message.react","message.upload","message.pin","message.delete_any","voice.connect","voice.speak","voice.share_screen","voice.moderate","audit.read"]',
     strftime('%Y-%m-%dT%H:%M:%fZ', 'now'), strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    ('role-member', 'member', 'Member', 10, 1, 1,
     '["channel.read","message.send","message.react","message.upload","voice.connect","voice.speak","voice.share_screen"]',
     strftime('%Y-%m-%dT%H:%M:%fZ', 'now'), strftime('%Y-%m-%dT%H:%M:%fZ', 'now'));

-- Administrators keep administrator authority; everyone else becomes a member.
-- No role is silently granted the operator tier.
INSERT INTO role_assignments(user_id, role_id, created_at)
SELECT id, CASE WHEN is_admin = 1 THEN 'role-admin' ELSE 'role-member' END,
       strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
FROM users;

INSERT INTO authorization_state(id, revision, updated_at)
VALUES (1, 1, strftime('%Y-%m-%dT%H:%M:%fZ', 'now'));
