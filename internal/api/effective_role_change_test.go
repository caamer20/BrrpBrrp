package api

import (
	"encoding/json"
	"net/http"
	"testing"

	"brrpbrrp/internal/db"
)

// accountWithRole creates an account holding exactly one role: the member assignment
// every account is created with is replaced, so the test controls the effective set.
func accountWithRole(t *testing.T, s *Server, username, roleID string) (string, *http.Cookie) {
	t.Helper()
	id, cookie := memberAccount(t, s, username)
	if _, err := s.store.Exec(`DELETE FROM role_assignments WHERE user_id=?`, id); err != nil {
		t.Fatal(err)
	}
	if _, err := s.store.Exec(`INSERT INTO role_assignments(user_id,role_id,created_at) VALUES(?,?,?)`, id, roleID, db.Now()); err != nil {
		t.Fatal(err)
	}
	return id, cookie
}

// roleAccount creates an account holding one custom role with the given capabilities,
// for actors whose exact authority the test needs to control.
func roleAccount(t *testing.T, s *Server, username string, position int, permissions ...string) (string, *http.Cookie) {
	t.Helper()
	encoded, err := json.Marshal(permissions)
	if err != nil {
		t.Fatal(err)
	}
	roleID := customRole(t, s, username+"-role", position, string(encoded))
	return accountWithRole(t, s, username, roleID)
}

// memberCapabilities is the default role's grant, which an account with no assignment
// resolves with. The permitted control has to hold every one of these to be allowed to
// drop an account onto the fallback.
func memberCapabilities(t *testing.T, s *Server) []string {
	t.Helper()
	roles, err := s.store.Roles(t.Context(), s.store)
	if err != nil {
		t.Fatal(err)
	}
	for _, role := range roles {
		if role.ID == db.MemberRoleID {
			return role.Permissions
		}
	}
	t.Fatal("the member role is missing from the catalog")
	return nil
}

// The reproduced defect: an actor holding role.manage but not the capabilities of the
// default role could empty a restricted account's assignment list, and the implicit
// default fallback then handed that account message.send — a capability the actor could
// never have granted directly. The individual revocation succeeded; the effective
// authority change is what has to be authorized.
func TestEmptyingTheLastAssignmentCannotGrantWhatTheActorCannot(t *testing.T) {
	s, handler := testServer(t)
	setupAdmin(t, s, handler)
	channelID := textChannelID(t, s)

	readOnly := customRole(t, s, "read only", 5, `["channel.read"]`)
	_, manager := roleAccount(t, s, "role-manager", 25, "channel.read", "role.manage")
	targetID, target := accountWithRole(t, s, "restricted", readOnly)

	// The starting state the change would alter: the target may read and may not send.
	if granted := capabilitiesOf(t, handler, target); granted["message.send"] {
		t.Fatalf("the read-only fixture already holds message.send: %v", granted)
	}
	if w := request(t, handler, "POST", "/api/channels/"+channelID+"/messages",
		messageInput{Content: "not permitted", IdempotencyKey: "read-only-send-1"}, target); w.Code != http.StatusForbidden {
		t.Fatalf("the read-only target could send: %d %s", w.Code, w.Body.String())
	}

	// The escalation: remove the only assignment, letting the default role apply.
	emptied := request(t, handler, "PUT", "/api/admin/users/"+targetID+"/roles",
		map[string]any{"role_ids": []string{}}, manager)
	if emptied.Code != http.StatusForbidden {
		t.Fatalf("emptying the last assignment answered %d: %s", emptied.Code, emptied.Body.String())
	}
	// The refusal is real: no capability was gained and no action became possible.
	if granted := capabilitiesOf(t, handler, target); granted["message.send"] {
		t.Fatalf("the refused change still granted message.send: %v", granted)
	}
	if w := request(t, handler, "POST", "/api/channels/"+channelID+"/messages",
		messageInput{Content: "still not permitted", IdempotencyKey: "read-only-send-2"}, target); w.Code != http.StatusForbidden {
		t.Fatalf("the refused change made a send succeed: %d %s", w.Code, w.Body.String())
	}
	// The assignment survives, so the account is still where it was.
	var assignments int
	if err := s.store.QueryRow(`SELECT count(*) FROM role_assignments WHERE user_id=? AND role_id=?`, targetID, readOnly).Scan(&assignments); err != nil {
		t.Fatal(err)
	}
	if assignments != 1 {
		t.Fatalf("the refused change removed the assignment: %d", assignments)
	}
	// A retry is refused the same way: the key was never claimed by a partial change.
	if w := request(t, handler, "PUT", "/api/admin/users/"+targetID+"/roles",
		map[string]any{"role_ids": []string{}}, manager); w.Code != http.StatusForbidden {
		t.Fatalf("a repeated escalation answered %d: %s", w.Code, w.Body.String())
	}
}

// The permitted control: an actor who does hold the default role's capabilities may
// drop an account onto the fallback, and the result is real authority, not a refusal
// that happens to look safe.
func TestEmptyingTheLastAssignmentWorksWhenTheFallbackIsGrantable(t *testing.T) {
	s, handler := testServer(t)
	setupAdmin(t, s, handler)
	channelID := textChannelID(t, s)

	readOnly := customRole(t, s, "read only permitted", 5, `["channel.read"]`)
	permissions := append([]string{}, memberCapabilities(t, s)...)
	permissions = append(permissions, "role.manage")
	_, manager := roleAccount(t, s, "full-role-manager", 25, permissions...)
	targetID, target := accountWithRole(t, s, "restricted-permitted", readOnly)

	emptied := request(t, handler, "PUT", "/api/admin/users/"+targetID+"/roles",
		map[string]any{"role_ids": []string{}}, manager)
	if emptied.Code != http.StatusOK {
		t.Fatalf("a grantable fallback was refused: %d %s", emptied.Code, emptied.Body.String())
	}
	if granted := capabilitiesOf(t, handler, target); !granted["message.send"] {
		t.Fatalf("the permitted change did not grant the fallback: %v", granted)
	}
	if w := request(t, handler, "POST", "/api/channels/"+channelID+"/messages",
		messageInput{Content: "permitted now", IdempotencyKey: "fallback-send-1"}, target); w.Code != http.StatusCreated {
		t.Fatalf("the permitted fallback could not send: %d %s", w.Code, w.Body.String())
	}
}

// A replacement is the same rule: the capabilities the requested set adds must be
// grantable, while the ones it only removes do not need to be re-granted.
func TestRoleReplacementAuthorizesOnlyWhatItAdds(t *testing.T) {
	s, handler := testServer(t)
	setupAdmin(t, s, handler)

	// The actor holds the ordinary posting capabilities but not invite.manage.
	_, manager := roleAccount(t, s, "replacement-manager", 25, "channel.read", "message.send", "message.react", "role.manage")
	readOnly := customRole(t, s, "replace read only", 5, `["channel.read"]`)
	withInvites := customRole(t, s, "replace invites", 4, `["channel.read","invite.manage"]`)
	targetID, target := accountWithRole(t, s, "replace-target", withInvites)

	// Shrinking is allowed: the target loses invite.manage and gains nothing.
	shrunk := request(t, handler, "PUT", "/api/admin/users/"+targetID+"/roles",
		map[string]any{"role_ids": []string{readOnly}}, manager)
	if shrunk.Code != http.StatusOK {
		t.Fatalf("a pure reduction was refused: %d %s", shrunk.Code, shrunk.Body.String())
	}
	if granted := capabilitiesOf(t, handler, target); granted["invite.manage"] {
		t.Fatalf("the reduction left invite.manage: %v", granted)
	}

	// Growing into a capability the actor does not hold is refused.
	grown := request(t, handler, "PUT", "/api/admin/users/"+targetID+"/roles",
		map[string]any{"role_ids": []string{withInvites}}, manager)
	if grown.Code != http.StatusForbidden {
		t.Fatalf("a replacement granting invite.manage answered %d: %s", grown.Code, grown.Body.String())
	}
	// A multi-role replacement that swaps one grantable role for another, keeping the
	// actor's own capabilities, still works.
	otherRead := customRole(t, s, "replace other read", 3, `["channel.read","message.react"]`)
	swapped := request(t, handler, "PUT", "/api/admin/users/"+targetID+"/roles",
		map[string]any{"role_ids": []string{readOnly, otherRead}}, manager)
	if swapped.Code != http.StatusOK {
		t.Fatalf("a grantable multi-role replacement was refused: %d %s", swapped.Code, swapped.Body.String())
	}
	// And the actor's own authority is unchanged: it still cannot grant invite.manage.
	if granted := capabilitiesOf(t, handler, manager); granted["invite.manage"] {
		t.Fatalf("the actor gained invite.manage: %v", granted)
	}
}

// The default role's own edit is the same boundary: raising its floor is a grant to
// every unassigned account, so it needs the same delegability as any other grant.
func TestEditingTheDefaultRoleRaisesItsFloorOnlyForGrantableCapabilities(t *testing.T) {
	s, handler := testServer(t)
	// The setup administrator is the installation owner in this fixture, so it may
	// raise the floor.
	owner := setupAdmin(t, s, handler)
	defaults := defaultRole(t, handler, owner)
	if defaults.ID == "" {
		t.Fatal("the default role has no id")
	}

	// An edit to the floor has to keep its whole capability set grantable by the actor,
	// so a manager that does not hold the floor's own capabilities cannot raise it —
	// and cannot lower it either, which is the conservative side of the same rule.
	_, manager := roleAccount(t, s, "default-editor", 25, "channel.read", "message.send", "role.manage")
	raised := append(append([]string{}, defaults.Permissions...), "invite.manage")
	refused := request(t, handler, "PATCH", "/api/roles/"+defaults.ID,
		map[string]any{"name": defaults.Name, "position": defaults.Position, "permissions": raised}, manager)
	if refused.Code != http.StatusForbidden {
		t.Fatalf("a default-role escalation answered %d: %s", refused.Code, refused.Body.String())
	}

	// The owner may, and the account with no assignment resolves with it.
	permissions := append([]string{}, memberCapabilities(t, s)...)
	permissions = append(permissions, "invite.manage")
	permitted := request(t, handler, "PATCH", "/api/roles/"+defaults.ID,
		map[string]any{"name": defaults.Name, "position": defaults.Position, "permissions": permissions}, owner)
	if permitted.Code != http.StatusOK {
		t.Fatalf("the owner's default-role edit was refused: %d %s", permitted.Code, permitted.Body.String())
	}
	// An account with no assignment row holds the floor: the capability it gained is
	// the one the owner just granted it.
	member, cookie := memberAccount(t, s, "floor-member")
	if _, err := s.store.Exec(`DELETE FROM role_assignments WHERE user_id=?`, member); err != nil {
		t.Fatal(err)
	}
	if granted := capabilitiesOf(t, handler, cookie); !granted["invite.manage"] {
		t.Fatalf("the raised floor did not reach an unassigned account: %v", granted)
	}
}
