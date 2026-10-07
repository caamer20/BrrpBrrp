package api

import (
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"time"

	"brrpbrrp/internal/authz"
	"brrpbrrp/internal/db"
	"brrpbrrp/internal/limits"
	"golang.org/x/crypto/bcrypt"
)

// errUnknownRole is the one invitation failure that is the client's mistake
// rather than a refusal of authority, so the two answer with different statuses.
var errUnknownRole = errors.New("That role does not exist.")

func (s *Server) createInvitation(w http.ResponseWriter, r *http.Request, user db.User) {
	var input struct {
		Hours   int      `json:"hours"`
		RoleIDs []string `json:"role_ids"`
		// A bounded multi-use invitation. The default of one use is what every
		// invitation issued before this did, so an invitation created without this
		// field behaves exactly as it always has.
		MaxUses int `json:"max_uses"`
	}
	if !decode(w, r, &input) {
		return
	}
	if input.Hours < 1 || input.Hours > 168 {
		problem(w, 400, "Invitations last between one hour and seven days.")
		return
	}
	// Bounded on the write. An invitation nobody can stop admitting people with is a
	// standing grant hidden in a link, so the ceiling is a policy, not a default.
	if input.MaxUses < 0 || input.MaxUses > 50 {
		problem(w, 400, "An invitation can admit between one and fifty people.")
		return
	}
	token, err := randomToken()
	if err != nil {
		s.failed(w, err)
		return
	}
	id, expires := db.ID(), db.Timestamp(time.Now().Add(time.Duration(input.Hours)*time.Hour))
	tx, err := s.store.BeginTx(r.Context(), nil)
	if err != nil {
		s.failed(w, err)
		return
	}
	defer tx.Rollback()
	// The invitation hands out roles, so the granter's authority is re-read in
	// the transaction that creates it.
	principal, err := s.requireCapabilityInTx(r.Context(), tx, user, authz.InviteManage)
	if err != nil {
		s.authorityFailed(w, err)
		return
	}
	grants, err := s.invitationGrants(r, tx, principal, input.RoleIDs)
	if err != nil {
		// An unknown role is a client mistake, not a refusal of authority.
		if errors.Is(err, errUnknownRole) {
			problem(w, 400, err.Error())
			return
		}
		problem(w, 403, err.Error())
		return
	}
	// `revoked_at=''` is a string literal, and the `?` in `expires_at>?` is part of
	// that literal as far as the driver is concerned: the statement binds seven
	// arguments to six placeholders, and the sixth — the admission bound — is silently
	// dropped. Every invitation was therefore written with the column default, so a
	// two-person invitation admitted everybody. The bound is now bound, and the
	// outstanding-count predicate takes a parameter rather than a second timestamp
	// smuggled inside a string.
	result, err := tx.Exec(`INSERT INTO invitations(id,token_hash,created_by,created_at,expires_at,max_uses) SELECT ?,?,?,?,?,? WHERE (SELECT count(*) FROM invitations WHERE used_at IS NULL AND revoked_at='' AND expires_at>?)<?`, id, hashToken(token), user.ID, db.Now(), expires, boundedUses(input.MaxUses), db.Now(), maxOutstandingInvitations)
	if err != nil {
		s.failed(w, err)
		return
	}
	if count, _ := result.RowsAffected(); count == 0 {
		problem(w, 409, "Revoke an unused invitation before creating another.")
		return
	}
	now := db.Now()
	for _, role := range grants {
		if _, err := tx.Exec(`INSERT OR IGNORE INTO invitation_roles(invitation_id,role_id,created_at) VALUES(?,?,?)`, id, role.ID, now); err != nil {
			s.failed(w, err)
			return
		}
	}
	if err := s.auditTx(r.Context(), tx, user.ID, "invitation.created", id); err != nil {
		s.failed(w, err)
		return
	}
	if err := tx.Commit(); err != nil {
		s.failed(w, err)
		return
	}
	respond(w, 201, map[string]any{"id": id, "url": s.config.BaseURL + "/#invite=" + token, "expires_at": expires, "roles": roleNames(grants), "role_ids": roleIDs(grants)})
}

// invitationGrants validates the roles an invitation may hand out. It applies
// the same delegation rules an ordinary grant goes through, so an invitation can
// never promise authority its author does not hold. An unknown role is a client
// mistake; holding too little authority is a refusal.
func (s *Server) invitationGrants(r *http.Request, tx *sql.Tx, actor authz.Principal, requested []string) ([]db.Role, error) {
	if len(requested) == 0 {
		return nil, nil
	}
	catalog, err := s.store.Roles(r.Context(), tx)
	if err != nil {
		return nil, err
	}
	known := make(map[string]db.Role, len(catalog))
	for _, role := range catalog {
		known[role.ID] = role
	}
	grants := make([]db.Role, 0, len(requested))
	seen := make(map[string]bool, len(requested))
	for _, id := range requested {
		if seen[id] {
			continue
		}
		seen[id] = true
		role, ok := known[id]
		if !ok {
			return nil, errUnknownRole
		}
		// The default role is held without an assignment, so every account
		// receives it whether or not the invitation asks for it.
		if role.IsDefault {
			continue
		}
		if !actor.CanActOnRole(authz.RoleFrom(role)) {
			return nil, errors.New("You cannot assign the " + role.Name + " role.")
		}
		if err := actor.CanGrantCapabilities(authz.Capabilities(role.Permissions)); err != nil {
			return nil, err
		}
		grants = append(grants, role)
	}
	return grants, nil
}

func roleNames(roles []db.Role) []string {
	names := make([]string, 0, len(roles))
	for _, role := range roles {
		names = append(names, role.Name)
	}
	return names
}

func roleIDs(roles []db.Role) []string {
	ids := make([]string, 0, len(roles))
	for _, role := range roles {
		ids = append(ids, role.ID)
	}
	return ids
}

func (s *Server) invitations(w http.ResponseWriter, r *http.Request, user db.User) {
	rows, err := s.store.Query(`SELECT id,created_at,expires_at,max_uses,uses FROM invitations WHERE used_at IS NULL AND revoked_at='' AND expires_at>? ORDER BY created_at DESC`, db.Now())
	if err != nil {
		s.failed(w, err)
		return
	}
	defer rows.Close()
	type summary struct {
		ID        string `json:"id"`
		CreatedAt string `json:"created_at"`
		ExpiresAt string `json:"expires_at"`
		// The admission bounds are part of what the invitation *is*, so they are
		// returned rather than hidden: an operator deciding whether to reissue needs to
		// see how much of it is left.
		MaxUses int      `json:"max_uses"`
		Uses    int      `json:"uses"`
		Roles   []string `json:"roles"`
		RoleIDs []string `json:"role_ids"`
	}
	// The rows are collected before the role lookup runs, because the store holds
	// one connection and a second query inside an open cursor would wait on it.
	entries := []summary{}
	for rows.Next() {
		var entry summary
		if err := rows.Scan(&entry.ID, &entry.CreatedAt, &entry.ExpiresAt, &entry.MaxUses, &entry.Uses); err != nil {
			s.failed(w, err)
			return
		}
		entries = append(entries, entry)
	}
	if err := rows.Err(); err != nil {
		s.failed(w, err)
		return
	}
	for i, entry := range entries {
		roles, err := s.store.InvitationRoles(r.Context(), s.store, entry.ID)
		if err != nil {
			s.failed(w, err)
			return
		}
		entries[i].Roles = roleNames(roles)
		entries[i].RoleIDs = roleIDs(roles)
	}
	respond(w, 200, entries)
}

func (s *Server) revokeInvitation(w http.ResponseWriter, r *http.Request, user db.User) {
	if !s.mutateReply(w, r, user, authz.InviteManage, func(tx *sql.Tx, principal authz.Principal) error {
		if _, err := tx.ExecContext(r.Context(), `UPDATE invitations SET revoked_at=?,revoked_by=? WHERE id=? AND revoked_at=''`, db.Now(), user.ID, r.PathValue("id")); err != nil {
			return err
		}
		return s.auditTx(r.Context(), tx, user.ID, "invitation.revoked", r.PathValue("id"))
	}) {
		return
	}
	respond(w, 200, map[string]bool{"ok": true})
}

func (s *Server) acceptInvitation(w http.ResponseWriter, r *http.Request) {
	// A valid invitation spends nothing: fifty people may accept one, and they often do
	// it from the same room. What is counted is a token that admits nobody — the one
	// thing here that can be guessed.
	address, open := s.guestOpen(w, r)
	if !open {
		return
	}
	var input struct {
		Token       string `json:"token"`
		Username    string `json:"username"`
		Password    string `json:"password"`
		DisplayName string `json:"display_name"`
	}
	if !decode(w, r, &input) {
		return
	}
	if len(input.Token) != 43 || !usernamePattern.MatchString(input.Username) || strings.EqualFold(input.Username, "everyone") || !validPassword(input.Password) || len(input.DisplayName) > 80 {
		problem(w, 400, "Choose a valid username and a 12–72 byte password.")
		return
	}
	if !s.acquirePasswordSlot(w) {
		return
	}
	defer s.releasePasswordSlot()
	hash, err := bcrypt.GenerateFromPassword([]byte(input.Password), 12)
	if err != nil {
		s.failed(w, err)
		return
	}
	tx, err := s.store.BeginTx(r.Context(), nil)
	if err != nil {
		s.failed(w, err)
		return
	}
	defer tx.Rollback()
	// The invitation is read before it is consumed only to learn what it promised
	// and who promised it. The single-use update below stays the only admission.
	var invitationID string
	var inviter sql.NullString
	// Every way an invitation stops working answers identically, so a link cannot be
	// used to find out which: expired, revoked, exhausted and never-existed all read
	// the same. The use count is consumed by the UPDATE's own predicate, so two people
	// racing for the last remaining use cannot both take it.
	if err := tx.QueryRow(`SELECT id,created_by FROM invitations WHERE token_hash=? AND revoked_at='' AND expires_at>? AND (used_at IS NULL OR (max_uses>0 AND uses<max_uses))`, hashToken(input.Token), db.Now()).
		Scan(&invitationID, &inviter); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			s.guestFailed(address)
			problem(w, 400, "This invitation has expired or has already been used.")
			return
		}
		s.failed(w, err)
		return
	}
	// A single-use invitation is consumed by setting used_at; a bounded multi-use one
	// is consumed by counting. Doing both in one statement keeps the two paths from
	// disagreeing about what "one more person" means.
	result, err := tx.Exec(`UPDATE invitations SET uses=uses+1,used_at=CASE WHEN uses+1>=max_uses THEN ? ELSE used_at END
		WHERE id=? AND revoked_at='' AND (used_at IS NULL OR uses<max_uses)`, db.Now(), invitationID)
	if err != nil {
		s.failed(w, err)
		return
	}
	if count, _ := result.RowsAffected(); count != 1 {
		s.guestFailed(address)
		problem(w, 400, "This invitation has expired or has already been used.")
		return
	}
	var count int
	if err := tx.QueryRow(`SELECT count(*) FROM users WHERE is_deleted=0`).Scan(&count); err != nil {
		s.failed(w, err)
		return
	}
	if count >= limits.Accounts {
		problem(w, 409, "This server has reached its account limit.")
		return
	}
	id, now := db.ID(), db.Now()
	_, err = tx.Exec(`INSERT INTO users(id,username,display_name,password_hash,created_at,updated_at) VALUES(?,?,?,?,?,?)`, id, strings.ToLower(input.Username), strings.TrimSpace(input.DisplayName), string(hash), now, now)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint") {
			problem(w, 409, "That username is already in use.")
		} else {
			s.failed(w, err)
		}
		return
	}
	// The account receives the default role in the same transaction that creates
	// it, so it never resolves with undefined authority.
	if err := db.AssignAccountRole(r.Context(), tx, id, db.MemberRoleID); err != nil {
		s.failed(w, err)
		return
	}
	granted, err := s.applyInvitationGrants(r, tx, id, invitationID, inviter)
	if err != nil {
		s.failed(w, err)
		return
	}
	if granted {
		if _, err := db.BumpAuthorizationRevision(r.Context(), tx); err != nil {
			s.failed(w, err)
			return
		}
	}
	if err := tx.Commit(); err != nil {
		s.failed(w, err)
		return
	}
	if err := s.newSessionFor(w, r, id); err != nil {
		s.failed(w, err)
		return
	}
	s.hub.Broadcast("user.updated", map[string]string{"id": id})
	respond(w, 201, map[string]bool{"ok": true})
}

// applyInvitationGrants hands a new account the roles its invitation promised.
// The inviter's authority is resolved again here, so an invitation that outlived
// a demotion, a deletion or an erase cannot still deliver what its author lost;
// anything no longer grantable is dropped and the account keeps the default role
// it always receives. It reports whether any role was actually added, so the
// authorization revision is bumped only when authority changed.
func (s *Server) applyInvitationGrants(r *http.Request, tx *sql.Tx, userID, invitationID string, inviter sql.NullString) (bool, error) {
	if !inviter.Valid {
		return false, nil
	}
	promised, err := s.store.InvitationRoles(r.Context(), tx, invitationID)
	if err != nil || len(promised) == 0 {
		return false, err
	}
	account, err := scanUser(tx.QueryRow(`SELECT `+userColumns+` FROM users WHERE id=? AND is_banned=0 AND is_deactivated=0`, inviter.String))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return false, err
	}
	actor, err := s.authz.Resolve(r.Context(), tx, account)
	if err != nil {
		return false, err
	}
	granted := false
	now := db.Now()
	for _, role := range promised {
		if !actor.CanActOnRole(authz.RoleFrom(role)) {
			continue
		}
		if err := actor.CanGrantCapabilities(authz.Capabilities(role.Permissions)); err != nil {
			continue
		}
		if _, err := tx.Exec(`INSERT OR IGNORE INTO role_assignments(user_id,role_id,assigned_by,created_at) VALUES(?,?,?,?)`, userID, role.ID, inviter.String, now); err != nil {
			return false, err
		}
		// The legacy column and the administrator role must never disagree.
		if role.Key == "admin" {
			if _, err := tx.Exec(`UPDATE users SET is_admin=1, updated_at=? WHERE id=?`, now, userID); err != nil {
				return false, err
			}
		}
		granted = true
	}
	return granted, nil
}

// boundedUses maps the requested admission count onto the stored representation. One
// is the floor: "admit nobody" is not an invitation, and silently making one would be a
// surprise, so a request for zero or less is stored as the single use every invitation
// before this behaved as.
func boundedUses(requested int) int {
	if requested < 1 {
		return 1
	}
	return requested
}

// maxOutstandingInvitations bounds how many unused invitations one installation may
// hold at once. It is a ceiling on standing links, enforced where the invitation is
// created rather than by a later sweep, and it reads the installation's own limit so a
// refusal cannot name a number the check does not apply.
const maxOutstandingInvitations = limits.OutstandingInvitations
