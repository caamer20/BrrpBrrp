package api

import (
	"net/http"
	"strings"
	"testing"

	"brrpbrrp/internal/db"
	"brrpbrrp/internal/ws"
)

// implicitMember creates an account with a session and no role assignment at all, so it
// holds only what the default role grants. The role API can produce this state: the
// Member role is the default role, and removing the assignment from an account leaves it
// relying on the implicit grant the authorization engine applies to every account.
func implicitMember(t *testing.T, s *Server, handler http.Handler, admin *http.Cookie, username string) (string, *http.Cookie) {
	t.Helper()
	id, cookie := memberAccount(t, s, username)
	if w := request(t, handler, "PUT", "/api/admin/users/"+id+"/roles", map[string]any{"role_ids": []string{}}, admin); w.Code != http.StatusOK {
		t.Fatalf("clearing %s's assignments: %d %s", username, w.Code, w.Body.String())
	}
	var rows int
	if err := s.store.QueryRow(`SELECT count(*) FROM role_assignments WHERE user_id=?`, id).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 0 {
		t.Fatalf("%s still holds %d explicit role assignments", username, rows)
	}
	return id, cookie
}

// defaultRole reads the installation's default role through the API.
func defaultRole(t *testing.T, handler http.Handler, admin *http.Cookie) db.Role {
	t.Helper()
	roles := readJSON[[]db.Role](t, request(t, handler, "GET", "/api/roles", nil, admin))
	for _, role := range roles {
		if role.IsDefault {
			return role
		}
	}
	t.Fatal("no default role is listed")
	return db.Role{}
}

// withoutCapability returns a permission list with one capability removed, which is the
// narrowest edit a role change can make.
func withoutCapability(permissions []string, drop string) []string {
	kept := make([]string, 0, len(permissions))
	for _, permission := range permissions {
		if permission != drop {
			kept = append(kept, permission)
		}
	}
	return kept
}

// TestRemovingReadFromTheImplicitDefaultRoleEndsTheStreamsItAdmitted is the reproduced
// R3-02 defect. An account that holds the default role without an assignment row was
// omitted from role-change stream revocation, so a download admitted under the
// capability kept streaming after the capability was removed and a fresh request for the
// same file was already refused.
//
// It also covers the permitted side with two controls: an account that keeps channel.read
// through a role of its own is not interrupted, and a change to that role invalidates its
// connections without touching an account that does not hold it.
func TestRemovingReadFromTheImplicitDefaultRoleEndsTheStreamsItAdmitted(t *testing.T) {
	s, handler := testServer(t)
	admin := setupAdmin(t, s, handler)
	implicitID, implicit := implicitMember(t, s, handler, admin, "implicit")
	readerID, reader := memberAccount(t, s, "reader")
	readersRole := customRole(t, s, "readers", 20, `["channel.read","message.send"]`, readerID)

	// A file in a public channel both accounts may read, and one open response each.
	attachmentID, size := sharedArchive(t, s, handler, admin)
	implicitStream, implicitHead := startDownload(t, s, handler, implicit, attachmentID)
	readerStream, readerHead := startDownload(t, s, handler, reader, attachmentID)

	// Narrowing the default role is what revokes the implicit holder's read capability.
	role := defaultRole(t, handler, admin)
	if !strings.Contains(strings.Join(role.Permissions, ","), "channel.read") {
		t.Fatalf("the default role does not grant channel.read: %v", role.Permissions)
	}
	narrowed := withoutCapability(role.Permissions, "channel.read")
	if w := request(t, handler, "PATCH", "/api/roles/"+role.ID, map[string]any{"name": role.Name, "position": role.Position, "permissions": narrowed}, admin); w.Code != http.StatusOK {
		t.Fatalf("narrow the default role: %d %s", w.Code, w.Body.String())
	}

	// The stream the implicit holder was admitted for stops, which is the boundary the
	// defect crossed: bytes already written cannot be recalled, but the server stops
	// sending the rest.
	if delivered := drain(t, implicitStream, implicitHead); delivered >= size {
		t.Fatalf("an implicit default-role holder received the whole file after channel.read was removed: %d of %d bytes", delivered, size)
	}
	// The control: the reader keeps channel.read through its own role, so its response is
	// finished rather than interrupted.
	if delivered := drain(t, readerStream, readerHead); delivered != size {
		t.Fatalf("an account that kept channel.read received %d of %d bytes", delivered, size)
	}
	// A new request states the same decision the stream boundary made.
	if w := request(t, handler, "GET", "/api/uploads/"+attachmentID, nil, implicit); w.Code != http.StatusNotFound {
		t.Fatalf("the implicit holder still reached the file: %d %s", w.Code, w.Body.String())
	}
	if w := request(t, handler, "GET", "/api/uploads/"+attachmentID, nil, reader); w.Code != http.StatusOK {
		t.Fatalf("the account that kept channel.read lost the file: %d %s", w.Code, w.Body.String())
	}

	// Event invalidation follows the same effective-role recipient list. The role change
	// is applied to the custom role, whose only holder is the reader.
	implicitClient := &ws.Client{ID: db.ID(), UserID: implicitID, SessionHash: activeSessionHash(t, s, implicitID), Out: make(chan ws.Queued, 8)}
	if !s.hub.Add(implicitClient) {
		t.Fatal("the hub refused the implicit holder's connection")
	}
	defer s.hub.Remove(implicitClient)
	readerClient := &ws.Client{ID: db.ID(), UserID: readerID, SessionHash: activeSessionHash(t, s, readerID), Out: make(chan ws.Queued, 8)}
	if !s.hub.Add(readerClient) {
		t.Fatal("the hub refused the reader's connection")
	}
	defer s.hub.Remove(readerClient)
	implicitClient.Send("presence.update", map[string]string{"state": "online"})
	readerClient.Send("presence.update", map[string]string{"state": "online"})
	implicitQueued := <-implicitClient.Out
	readerQueued := <-readerClient.Out
	if w := request(t, handler, "PATCH", "/api/roles/"+readersRole, map[string]any{"name": "readers", "position": 20, "permissions": []string{"channel.read"}}, admin); w.Code != http.StatusOK {
		t.Fatalf("narrow the readers role: %d %s", w.Code, w.Body.String())
	}
	if !readerClient.Stale(readerQueued) {
		t.Fatal("a role change did not invalidate a connection that holds the role")
	}
	if implicitClient.Stale(implicitQueued) {
		t.Fatal("a change to a role an account does not hold invalidated its connection")
	}
	// The reader still reads the channel, so the narrowing role did not end anything.
	if w := request(t, handler, "GET", "/api/uploads/"+attachmentID, nil, reader); w.Code != http.StatusOK {
		t.Fatalf("the reader lost the file after an unrelated role changed: %d %s", w.Code, w.Body.String())
	}
}
