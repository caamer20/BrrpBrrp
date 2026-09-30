-- Delegated moderation, expiring timeouts and bounded invitations (ADR 0018).
--
-- A timeout suspends an account until a time and restores exactly what it had. That
-- is why `restores` is stored: expiry replays the recorded capabilities rather than
-- re-deriving them, so an account whose role was removed *during* the timeout returns
-- to what it had before the timeout and not to a default.

CREATE TABLE account_timeouts (
    user_id TEXT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    -- The capabilities the account held when the timeout was applied. Expiry puts
    -- these back and nothing else: a timeout is not a role change that happens to be
    -- undone, because an account that was banned before the timeout must not come back.
    restores TEXT NOT NULL,
    reason TEXT NOT NULL DEFAULT '',
    -- The account that applied it, kept for the audit trail rather than for the
    -- member's own view, which says only that they are suspended and until when.
    applied_by TEXT REFERENCES users(id) ON DELETE SET NULL,
    applied_at TEXT NOT NULL,
    expires_at TEXT NOT NULL
);

CREATE INDEX account_timeouts_expiry ON account_timeouts(expires_at);

-- Assignment is a claim about who is expected to look at a report, not a grant. The
-- review path still checks the reviewer's own capability, so an assignment to someone
-- who has since lost it is a stale claim rather than a way to review.
ALTER TABLE message_reports ADD COLUMN assigned_to TEXT REFERENCES users(id) ON DELETE SET NULL;
ALTER TABLE message_reports ADD COLUMN assigned_at TEXT NOT NULL DEFAULT '';
CREATE INDEX message_reports_assigned ON message_reports(assigned_to, status);

-- Bounded, expiring, revocable invitations. The table already exists from migration 2
-- — a hashed token, one use, an expiry, deleted to revoke — so this extends it rather
-- than creating a second one. A second table would mean two kinds of invitation, two
-- redemption paths, and two places where a revoked link could still work.
--
-- `max_uses` is the number of people the invitation admits, and the redemption
-- predicate is `uses < max_uses`, so a row with the value 0 admits nobody. Migration
-- 20 backfills the rows that predate the column: they were single-use, and they are
-- marked used rather than left at zero, so an existing installation's outstanding
-- invitations keep working exactly as they did. The token stays hashed; nothing here
-- stores the secret.
ALTER TABLE invitations ADD COLUMN max_uses INTEGER NOT NULL DEFAULT 0 CHECK(max_uses >= 0);
ALTER TABLE invitations ADD COLUMN uses INTEGER NOT NULL DEFAULT 0 CHECK(uses >= 0);
ALTER TABLE invitations ADD COLUMN entry_role TEXT NOT NULL DEFAULT 'member';
-- A revocation is recorded rather than performed by deletion, so a redeemed
-- invitation's history is not lost with the row and the audit trail can say an
-- invitation was withdrawn rather than never existed.
ALTER TABLE invitations ADD COLUMN revoked_at TEXT NOT NULL DEFAULT '';
ALTER TABLE invitations ADD COLUMN revoked_by TEXT REFERENCES users(id) ON DELETE SET NULL;
-- The adoption set: rows written before this migration were single-use, and a
-- redeemed one already has used_at set. Marking it used keeps the two facts
-- consistent; an outstanding one is left at zero, which the redemption guard treats as
-- exhausted only if it is also past used_at, so it keeps admitting exactly one person.
UPDATE invitations SET uses=1 WHERE used_at IS NOT NULL AND uses=0;

CREATE INDEX invitations_live ON invitations(expires_at) WHERE revoked_at='' AND used_at IS NULL;
