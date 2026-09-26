package api

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strings"

	"brrpbrrp/internal/authz"
	"brrpbrrp/internal/db"
)

// restrictedChannelsEnabled reports whether restricted community channels may be
// created or changed through the API. The enforcement code is always live; this
// switch keeps an incomplete privacy surface from being exposed to users before
// the privacy gate passes. It defaults to off.
func (s *Server) restrictedChannelsEnabled() bool { return s.config.RestrictedChannels }

func (s *Server) requireRestrictedChannels(w http.ResponseWriter) bool {
	if s.restrictedChannelsEnabled() {
		return true
	}
	problem(w, http.StatusConflict, "Restricted channels are disabled on this server until the privacy checks are complete. Set BRRP_ENABLE_RESTRICTED_CHANNELS=true to enable them.")
	return false
}

// categories returns durable categories in display order. Names are shared
// workspace structure; the per-category channel count is scoped to the caller.
func (s *Server) listCategories(w http.ResponseWriter, r *http.Request, user db.User) {
	categories, err := s.store.Categories(r.Context(), s.store, viewer(principalOf(r)))
	if err != nil {
		s.failed(w, err)
		return
	}
	respond(w, 200, categories)
}

func (s *Server) createCategory(w http.ResponseWriter, r *http.Request, user db.User) {
	var input struct {
		Name     string `json:"name"`
		Position *int   `json:"position"`
	}
	if !decode(w, r, &input) {
		return
	}
	name, err := db.NormalizeCategoryName(input.Name)
	if err != nil {
		problem(w, 400, "Use a category name of 1-60 characters.")
		return
	}
	tx, err := s.store.BeginTx(r.Context(), nil)
	if err != nil {
		s.failed(w, err)
		return
	}
	defer tx.Rollback()
	if _, err := s.requireCapabilityInTx(r.Context(), tx, user, authz.CategoryManage); err != nil {
		s.authorityFailed(w, err)
		return
	}
	position := 0
	if input.Position != nil {
		position = *input.Position
	} else {
		if err := tx.QueryRow(`SELECT COALESCE(max(position)+1,0) FROM categories`).Scan(&position); err != nil {
			s.failed(w, err)
			return
		}
	}
	if position < 0 || position > 1000 {
		problem(w, 400, "Choose a category position from 0 to 1000.")
		return
	}
	id, now := db.ID(), db.Now()
	if _, err := tx.Exec(`INSERT INTO categories(id,name,position,created_at,updated_at) VALUES(?,?,?,?,?)`, id, name, position, now, now); err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint") {
			problem(w, 409, "A category with that name already exists.")
			return
		}
		s.failed(w, err)
		return
	}
	if err := s.auditTx(r.Context(), tx, user.ID, "category.created", id); err != nil {
		s.failed(w, err)
		return
	}
	if err := tx.Commit(); err != nil {
		s.failed(w, err)
		return
	}
	s.publishCategoryChange("A category was created.")
	respond(w, 201, db.Category{ID: id, Name: name, Position: position})
}

func (s *Server) updateCategory(w http.ResponseWriter, r *http.Request, user db.User) {
	var input struct {
		Name     string `json:"name"`
		Position *int   `json:"position"`
	}
	if !decode(w, r, &input) {
		return
	}
	name, err := db.NormalizeCategoryName(input.Name)
	if err != nil {
		problem(w, 400, "Use a category name of 1-60 characters.")
		return
	}
	tx, err := s.store.BeginTx(r.Context(), nil)
	if err != nil {
		s.failed(w, err)
		return
	}
	defer tx.Rollback()
	if _, err := s.requireCapabilityInTx(r.Context(), tx, user, authz.CategoryManage); err != nil {
		s.authorityFailed(w, err)
		return
	}
	var previous string
	var position int
	if err := tx.QueryRow(`SELECT name,position FROM categories WHERE id=?`, r.PathValue("id")).Scan(&previous, &position); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			problem(w, 404, "Category not found.")
			return
		}
		s.failed(w, err)
		return
	}
	if input.Position != nil {
		if *input.Position < 0 || *input.Position > 1000 {
			problem(w, 400, "Choose a category position from 0 to 1000.")
			return
		}
		position = *input.Position
	}
	if _, err := tx.Exec(`UPDATE categories SET name=?,position=?,updated_at=? WHERE id=?`, name, position, db.Now(), r.PathValue("id")); err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint") {
			problem(w, 409, "A category with that name already exists.")
			return
		}
		s.failed(w, err)
		return
	}
	// channels.category stays the denormalized display name; a rename never
	// changes a channel's visibility or access grants.
	if name != previous {
		if _, err := tx.Exec(`UPDATE channels SET category=?,updated_at=? WHERE trim(category)=?`, name, db.Now(), previous); err != nil {
			s.failed(w, err)
			return
		}
	}
	if err := s.auditTx(r.Context(), tx, user.ID, "category.updated", r.PathValue("id")); err != nil {
		s.failed(w, err)
		return
	}
	if err := tx.Commit(); err != nil {
		s.failed(w, err)
		return
	}
	s.publishCategoryChange("A category changed.")
	respond(w, 200, db.Category{ID: r.PathValue("id"), Name: name, Position: position})
}

func (s *Server) deleteCategory(w http.ResponseWriter, r *http.Request, user db.User) {
	if !s.mutateReply(w, r, user, authz.CategoryManage, func(tx *sql.Tx, principal authz.Principal) error {
		var channels int
		if err := tx.QueryRow(`SELECT count(*) FROM channels c JOIN categories ON trim(c.category)=categories.name WHERE categories.id=?`, r.PathValue("id")).Scan(&channels); err != nil {
			return err
		}
		if channels != 0 {
			return refuse(409, "Move or delete this category's channels first.")
		}
		result, err := tx.Exec(`DELETE FROM categories WHERE id=?`, r.PathValue("id"))
		if err != nil {
			return err
		}
		if affected, _ := result.RowsAffected(); affected == 0 {
			return refuse(404, "Category not found.")
		}
		return s.auditTx(r.Context(), tx, user.ID, "category.deleted", r.PathValue("id"))
	}) {
		return
	}
	s.publishCategoryChange("A category was removed.")
	respond(w, 200, map[string]bool{"ok": true})
}

func (s *Server) publishCategoryChange(reason string) {
	s.invalidateWorkspace(reason)
}

// roleView is a role plus what the caller may do with it. The client shows the
// same limits the engine enforces, so it does not offer a control that would
// always be refused.
type roleView struct {
	db.Role
	Grantable bool `json:"grantable"`
	Editable  bool `json:"editable"`
	Deletable bool `json:"deletable"`
}

func (s *Server) listRoles(w http.ResponseWriter, r *http.Request, user db.User) {
	roles, err := s.store.Roles(r.Context(), s.store)
	if err != nil {
		s.failed(w, err)
		return
	}
	principal := principalOf(r)
	views := make([]roleView, 0, len(roles))
	for _, role := range roles {
		resolved := authz.RoleFrom(role)
		views = append(views, roleView{
			Role:      role,
			Grantable: principal.CanActOnRole(resolved) && principal.CanGrantCapabilities(authz.Capabilities(role.Permissions)) == nil,
			Editable:  principal.CanActOnRole(resolved),
			Deletable: principal.CanDeleteRole(resolved, role.Members),
		})
	}
	respond(w, 200, views)
}

func (s *Server) createRole(w http.ResponseWriter, r *http.Request, user db.User) {
	var input struct {
		Name        string   `json:"name"`
		Position    int      `json:"position"`
		Permissions []string `json:"permissions"`
	}
	if !decode(w, r, &input) {
		return
	}
	tx, err := s.store.BeginTx(r.Context(), nil)
	if err != nil {
		s.failed(w, err)
		return
	}
	defer tx.Rollback()
	// Authority is re-resolved inside the transaction, so a role that was
	// revoked while this request was being prepared cannot create another role.
	principal, err := s.requireCapabilityInTx(r.Context(), tx, user, authz.RoleManage)
	if err != nil {
		s.authorityFailed(w, err)
		return
	}
	role, _, err := s.authz.CreateRole(r.Context(), tx, principal, input.Name, input.Position, authz.Capabilities(input.Permissions))
	if err != nil {
		problem(w, 403, err.Error())
		return
	}
	if err := s.auditTx(r.Context(), tx, user.ID, "role.created", role.ID); err != nil {
		s.failed(w, err)
		return
	}
	if err := s.commitAuthorityChange(r.Context(), tx, "A role was created.", user.ID); err != nil {
		s.failed(w, err)
		return
	}
	respond(w, 201, role)
}

func (s *Server) updateRole(w http.ResponseWriter, r *http.Request, user db.User) {
	var input struct {
		Name        string   `json:"name"`
		Position    int      `json:"position"`
		Permissions []string `json:"permissions"`
	}
	if !decode(w, r, &input) {
		return
	}
	tx, err := s.store.BeginTx(r.Context(), nil)
	if err != nil {
		s.failed(w, err)
		return
	}
	defer tx.Rollback()
	// The role and the actor's authority are both read inside the transaction:
	// a role edited to sit above the actor, or an actor whose role was revoked,
	// must be decided on what the commit will actually see.
	principal, err := s.requireCapabilityInTx(r.Context(), tx, user, authz.RoleManage)
	if err != nil {
		s.authorityFailed(w, err)
		return
	}
	role, err := s.store.Role(r.Context(), tx, r.PathValue("id"))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			problem(w, 404, "Role not found.")
			return
		}
		s.failed(w, err)
		return
	}
	if _, err = s.authz.UpdateRole(r.Context(), tx, principal, role, input.Name, input.Position, authz.Capabilities(input.Permissions)); err != nil {
		problem(w, 403, err.Error())
		return
	}
	// The effective holders are listed inside the transaction that changes the role, so the
	// audience told to refetch is read from the state being committed. The default role
	// applies to accounts without an assignment, so an implicit holder is included.
	holders, err := s.roleHoldersInTx(r.Context(), tx, role)
	if err != nil {
		s.failed(w, err)
		return
	}
	if err := s.auditTx(r.Context(), tx, user.ID, "role.updated", role.ID); err != nil {
		s.failed(w, err)
		return
	}
	if err := s.commitAuthorityChange(r.Context(), tx, "A role changed.", holders...); err != nil {
		s.failed(w, err)
		return
	}

	// A role's capabilities decide which channels its holders may read, and a role can
	// be granted on any number of channels, so the streams of the accounts that hold
	// it end with the change. Accounts that do not hold it keep what they were
	// already reading.
	s.endUnreadableStreams(r.Context(), holders)
	s.enforceVoiceAccess(r.Context(), user.ID, "A role changed.")
	respond(w, 200, map[string]bool{"ok": true})
}

func (s *Server) deleteRole(w http.ResponseWriter, r *http.Request, user db.User) {
	tx, err := s.store.BeginTx(r.Context(), nil)
	if err != nil {
		s.failed(w, err)
		return
	}
	defer tx.Rollback()
	principal, err := s.requireCapabilityInTx(r.Context(), tx, user, authz.RoleManage)
	if err != nil {
		s.authorityFailed(w, err)
		return
	}
	role, err := s.store.Role(r.Context(), tx, r.PathValue("id"))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			problem(w, 404, "Role not found.")
			return
		}
		s.failed(w, err)
		return
	}
	// The holders are listed before the delete, because afterwards there is nothing
	// left to list and their authority is still moving.
	holders, err := s.roleHoldersInTx(r.Context(), tx, role)
	if err != nil {
		s.failed(w, err)
		return
	}
	if _, err := s.authz.DeleteRole(r.Context(), tx, principal, tx, role); err != nil {
		problem(w, 403, err.Error())
		return
	}
	if err := s.auditTx(r.Context(), tx, user.ID, "role.deleted", role.ID); err != nil {
		s.failed(w, err)
		return
	}
	if err := s.commitAuthorityChange(r.Context(), tx, "A role was removed.", holders...); err != nil {
		s.failed(w, err)
		return
	}
	s.endUnreadableStreams(r.Context(), holders)
	s.enforceVoiceAccess(r.Context(), user.ID, "A role was removed.")
	respond(w, 200, map[string]bool{"ok": true})
}

// roleHoldersInTx lists the effective holders of a role inside the transaction that
// is changing the role itself. Effective, not assigned: the default role applies to
// every account without an assignment row, so those accounts must be in the list
// that loses its streams.
func (s *Server) roleHoldersInTx(ctx context.Context, q db.Queryer, role db.Role) ([]string, error) {
	holders, err := s.store.EffectiveRoleHolders(ctx, q, role)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(holders))
	for _, holder := range holders {
		ids = append(ids, holder.ID)
	}
	return ids, nil
}

func (s *Server) userRoles(w http.ResponseWriter, r *http.Request, user db.User) {
	target, err := scanUser(s.store.QueryRow(`SELECT `+userColumns+` FROM users WHERE id=?`, r.PathValue("id")))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			problem(w, 404, "Account not found.")
			return
		}
		s.failed(w, err)
		return
	}
	roles, err := s.store.AssignedRoles(r.Context(), s.store, target.ID)
	if err != nil {
		s.failed(w, err)
		return
	}
	if roles == nil {
		roles = []db.Role{}
	}
	respond(w, 200, map[string]any{"user": target, "roles": roles})
}

// accountSummary is the minimum a role manager needs to assign a role: who the
// account is and which roles it already holds. Credential, session, ban and
// activity state are deliberately absent, because assigning a role does not require
// reading any of them, and the wider account list stays behind member.manage. A
// scoped surface like this is how a single-capability role gets a usable workflow
// without widening what any one account may read.
type accountSummary struct {
	ID          string   `json:"id"`
	Username    string   `json:"username"`
	DisplayName string   `json:"display_name"`
	IsAdmin     bool     `json:"is_admin"`
	IsOwner     bool     `json:"is_owner"`
	RoleIDs     []string `json:"role_ids"`
	Assignable  bool     `json:"assignable"`
}

// accountSummaries lists active accounts for role assignment. It is the account-list
// surface that role.manage alone can reach, and it returns no more than the decision
// in front of it needs.
func (s *Server) accountSummaries(w http.ResponseWriter, r *http.Request, user db.User) {
	principal := principalOf(r)
	rows, err := s.store.Query(`SELECT u.id,u.username,u.display_name,u.is_admin,u.is_owner,COALESCE(group_concat(ra.role_id),'')
        FROM users u LEFT JOIN role_assignments ra ON ra.user_id=u.id
        WHERE u.is_banned=0 AND u.is_deactivated=0
        GROUP BY u.id ORDER BY u.username`)
	if err != nil {
		s.failed(w, err)
		return
	}
	defer rows.Close()
	summaries := make([]accountSummary, 0)
	for rows.Next() {
		var summary accountSummary
		var roles string
		if err := rows.Scan(&summary.ID, &summary.Username, &summary.DisplayName, &summary.IsAdmin, &summary.IsOwner, &roles); err != nil {
			s.failed(w, err)
			return
		}
		summary.RoleIDs = []string{}
		if roles != "" {
			summary.RoleIDs = strings.Split(roles, ",")
		}
		// An account the caller may not assign to is marked rather than hidden, so the
		// interface can show it and refuse the action for the same reason the server
		// would.
		summaries = append(summaries, summary)
	}
	if err := rows.Err(); err != nil {
		s.failed(w, err)
		return
	}
	for index := range summaries {
		// Whether the caller may hand this account a role is the engine's decision,
		// reported so the interface does not offer an action that would be refused.
		rank, err := s.authz.Rank(r.Context(), s.store, summaries[index].ID)
		if err != nil {
			s.failed(w, err)
			return
		}
		summaries[index].Assignable = summaries[index].IsOwner == false && (principal.IsOwner || principal.Position() > rank)
	}
	respond(w, 200, summaries)
}

// setUserRoles replaces an account's role set. It applies each change through the
// policy service so escalation, hierarchy and last-administrator guards are the
// same ones the engine enforces everywhere else.
func (s *Server) setUserRoles(w http.ResponseWriter, r *http.Request, user db.User) {
	var input struct {
		RoleIDs []string `json:"role_ids"`
	}
	if !decode(w, r, &input) {
		return
	}
	target, err := scanUser(s.store.QueryRow(`SELECT `+userColumns+` FROM users WHERE id=?`, r.PathValue("id")))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			problem(w, 404, "Account not found.")
			return
		}
		s.failed(w, err)
		return
	}
	tx, err := s.store.BeginTx(r.Context(), nil)
	if err != nil {
		s.failed(w, err)
		return
	}
	defer tx.Rollback()
	// Role assignment is the most direct way to hand out authority, so the
	// actor's own authority is read inside the transaction rather than reused
	// from the route guard.
	principal, err := s.requireCapabilityInTx(r.Context(), tx, user, authz.RoleManage)
	if err != nil {
		s.authorityFailed(w, err)
		return
	}
	// The target is read again inside the committing transaction. The read before
	// it only established that the account exists; it is not authority. A request
	// paused there resumes against the account as it is committed, so a transfer
	// that made this target the owner is refused instead of having its roles
	// rewritten on a stale record.
	target, err = s.store.Account(r.Context(), tx, target.ID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			problem(w, 404, "Account not found.")
			return
		}
		s.failed(w, err)
		return
	}
	// Roles are revoked and granted from here, so the owner's authority is not
	// reachable: it moves only through the ownership ceremony.
	if target.IsDeleted {
		problem(w, http.StatusConflict, "A removed account cannot receive roles.")
		return
	}
	if err := authz.RefuseOwnerChange(target); err != nil {
		problem(w, 403, err.Error())
		return
	}
	catalog, err := s.store.Roles(r.Context(), tx)
	if err != nil {
		s.failed(w, err)
		return
	}
	wanted := make(map[string]bool, len(input.RoleIDs))
	for _, id := range input.RoleIDs {
		wanted[id] = true
	}
	known := make(map[string]db.Role, len(catalog))
	for _, role := range catalog {
		known[role.ID] = role
	}
	for id := range wanted {
		if _, ok := known[id]; !ok {
			problem(w, 400, "That role does not exist.")
			return
		}
	}
	assigned, err := s.store.AssignedRoleIDs(r.Context(), tx, target.ID)
	if err != nil {
		s.failed(w, err)
		return
	}
	current := make(map[string]bool, len(assigned))
	for _, id := range assigned {
		current[id] = true
	}
	// Removals run before grants so a swap cannot momentarily leave the
	// installation without its last administrator.
	for id := range current {
		if wanted[id] {
			continue
		}
		if _, err := s.authz.RevokeRole(r.Context(), tx, principal, target, known[id]); err != nil {
			problem(w, 403, err.Error())
			return
		}
	}
	for id := range wanted {
		if current[id] {
			continue
		}
		if _, err := s.authz.GrantRole(r.Context(), tx, principal, target, known[id]); err != nil {
			problem(w, 403, err.Error())
			return
		}
	}
	if err := s.auditTx(r.Context(), tx, user.ID, "user.roles_changed", target.ID); err != nil {
		s.failed(w, err)
		return
	}
	if err := s.commitAuthorityChange(r.Context(), tx, "Your roles changed.", target.ID); err != nil {
		s.failed(w, err)
		return
	}

	// A demoted member loses the streams with the role: the voice connection and any
	// file the account is still downloading.
	s.deliveries.revokeAccount(target.ID)
	s.enforceVoiceAccess(r.Context(), user.ID, "Your roles changed.", target.ID)
	respond(w, 200, map[string]bool{"ok": true})
}

// designateOwner completes the ownership ceremony or transfers ownership. It is
// an operator ceremony: it requires an administrator before any owner exists, and
// only the owner afterwards.
func (s *Server) designateOwner(w http.ResponseWriter, r *http.Request, user db.User) {
	var input struct {
		Username string `json:"username"`
		Confirm  string `json:"confirm"`
	}
	if !decode(w, r, &input) {
		return
	}
	target, err := scanUser(s.store.QueryRow(`SELECT `+userColumns+` FROM users WHERE username=?`, strings.ToLower(input.Username)))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			problem(w, 404, "Account not found.")
			return
		}
		s.failed(w, err)
		return
	}
	if input.Confirm != target.Username {
		problem(w, 400, "Confirm the account username to change ownership.")
		return
	}
	tx, err := s.store.BeginTx(r.Context(), nil)
	if err != nil {
		s.failed(w, err)
		return
	}
	defer tx.Rollback()
	// Ownership moves on the authority the commit observes, and the target is
	// re-read too: it must still be an active account when ownership lands.
	principal, err := s.requireCapabilityInTx(r.Context(), tx, user, authz.OperatorOwnershipTransfer)
	if err != nil {
		s.authorityFailed(w, err)
		return
	}
	target, err = s.store.Account(r.Context(), tx, target.ID)
	if err != nil {
		s.failed(w, err)
		return
	}
	if _, err := s.authz.DesignateOwner(r.Context(), tx, principal, target); err != nil {
		problem(w, 403, err.Error())
		return
	}
	if err := s.auditTx(r.Context(), tx, user.ID, "owner.designated", target.ID); err != nil {
		s.failed(w, err)
		return
	}
	// Every administrator's operator tier changes with this, so every session refetches.
	// The change is workspace-wide, so the signal is addressed to every connection and the
	// event carries no content.
	if err := s.commitAuthorityChange(r.Context(), tx, "The installation owner changed."); err != nil {
		s.failed(w, err)
		return
	}
	respond(w, 200, map[string]bool{"ok": true})
}
