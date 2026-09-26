-- Account removal can retain conversation continuity without retaining a login/profile.
-- Existing accounts and message rowids are unchanged. Erasing messages remains a separate,
-- explicitly confirmed action; these tombstones cannot be reactivated or made privileged.
ALTER TABLE users ADD COLUMN is_deleted INTEGER NOT NULL DEFAULT 0 CHECK(is_deleted IN (0,1));
CREATE TRIGGER users_tombstone_cannot_reactivate BEFORE UPDATE ON users
WHEN OLD.is_deleted=1 AND (NEW.is_deleted<>1 OR NEW.is_deactivated<>1 OR NEW.is_admin<>0 OR NEW.is_owner<>0)
BEGIN
    SELECT RAISE(ABORT, 'a removed account cannot be reactivated');
END;
