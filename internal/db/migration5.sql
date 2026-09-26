-- Invite-time role selection: an invitation may carry the role grants its
-- recipient receives on acceptance.
--
-- Compatibility contract: an invitation created before this migration carries no
-- roles and its recipient receives exactly the default role, which is what every
-- new account received before. Nothing about an existing invitation changes.
--
-- The rows here are a promise, not an authority: the inviter's delegation
-- authority is validated when the invitation is created and re-validated when it
-- is accepted, so an invitation that outlives its author's authority cannot hand
-- out what that account no longer holds. Deleting a role drops it from every
-- pending invitation through the cascade.

CREATE TABLE invitation_roles (
    invitation_id TEXT NOT NULL REFERENCES invitations(id) ON DELETE CASCADE,
    role_id TEXT NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
    created_at TEXT NOT NULL,
    PRIMARY KEY(invitation_id, role_id)
);

CREATE INDEX invitation_roles_role ON invitation_roles(role_id);
