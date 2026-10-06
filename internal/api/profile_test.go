package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"brrpbrrp/internal/db"
)

// W-27. Before this route existed the only way to change a display name or an avatar
// color was PATCH /api/admin/users/{id}: a member with a typo in their own name had to
// ask an administrator to fix it. These tests pin the route's own rules — the two fields
// it takes, the validation it shares with the administrator's route, the account it
// touches, and the event it emits — and the refusal for an account that is no longer its
// holder's to change.

// workspaceSnapshot is the part of GET /api/state these tests read. The account list is
// where one member sees another member's name, so it is how a rename is shown to have
// reached the community rather than only the account itself.
type workspaceSnapshot struct {
	Users []db.User `json:"users"`
}

func TestAMemberChangesTheirOwnDisplayNameAndAvatarColor(t *testing.T) {
	s, handler := testServer(t)
	setupAdmin(t, s, handler)
	memberID, memberCookie := memberAccount(t, s, "renamer")
	_, observerCookie := memberAccount(t, s, "observer")

	w := request(t, handler, "PATCH", "/api/me", map[string]string{"display_name": "  Renamed  ", "avatar_color": "#1a2b3c"}, memberCookie)
	if w.Code != 200 {
		t.Fatalf("rename: %d %s", w.Code, w.Body.String())
	}
	updated := readJSON[db.User](t, w)
	if updated.DisplayName != "Renamed" {
		t.Fatalf("the response carries %q, want the trimmed name", updated.DisplayName)
	}
	if updated.AvatarColor != "#1a2b3c" {
		t.Fatalf("the response carries colour %q", updated.AvatarColor)
	}
	// It changes the account's own identity and nothing else. This route carries no
	// capability, so the fields it writes are the whole statement: a rename is not a
	// route into authority, access or another account.
	if updated.IsAdmin || updated.IsOwner || updated.IsBanned || updated.IsDeactivated || updated.IsDeleted {
		t.Fatalf("a rename changed something other than the name: %+v", updated)
	}
	// The account reads its own new name back.
	me := readJSON[db.User](t, request(t, handler, "GET", "/api/me", nil, memberCookie))
	if me.DisplayName != "Renamed" || me.AvatarColor != "#1a2b3c" {
		t.Fatalf("the account reads back %q / %q", me.DisplayName, me.AvatarColor)
	}
	// And the community does: another member's snapshot carries the new name, which is
	// what the user.updated event tells every connected client to fetch.
	snapshot := readJSON[workspaceSnapshot](t, request(t, handler, "GET", "/api/state", nil, observerCookie))
	seen := false
	for _, user := range snapshot.Users {
		if user.ID != memberID {
			continue
		}
		seen = true
		if user.DisplayName != "Renamed" || user.AvatarColor != "#1a2b3c" {
			t.Fatalf("another member sees %q / %q", user.DisplayName, user.AvatarColor)
		}
	}
	if !seen {
		t.Fatal("the renamed account is missing from another member's snapshot")
	}
}

func TestARenameRefusesExactlyWhatTheAdministratorRouteRefuses(t *testing.T) {
	s, handler := testServer(t)
	setupAdmin(t, s, handler)
	_, memberCookie := memberAccount(t, s, "careful")
	before := readJSON[db.User](t, request(t, handler, "GET", "/api/me", nil, memberCookie))

	if w := request(t, handler, "PATCH", "/api/me", map[string]string{"display_name": strings.Repeat("n", 81)}, memberCookie); w.Code != 400 {
		t.Fatalf("an 81-byte display name: %d %s", w.Code, w.Body.String())
	}
	if w := request(t, handler, "PATCH", "/api/me", map[string]string{"avatar_color": "blue"}, memberCookie); w.Code != 400 {
		t.Fatalf("a colour that is not six-digit hex: %d %s", w.Code, w.Body.String())
	}
	// A body that tries to name an account is refused at the field level: the route takes
	// two fields and there is no id to point somewhere else.
	if w := request(t, handler, "PATCH", "/api/me", map[string]string{"id": "somebody-else", "display_name": "Taken"}, memberCookie); w.Code != 400 {
		t.Fatalf("a body naming an id: %d %s", w.Code, w.Body.String())
	}
	// An empty change is not an error and moves nothing.
	if w := request(t, handler, "PATCH", "/api/me", map[string]string{}, memberCookie); w.Code != 200 {
		t.Fatalf("an empty change: %d %s", w.Code, w.Body.String())
	}
	afterEmpty := readJSON[db.User](t, request(t, handler, "GET", "/api/me", nil, memberCookie))
	if afterEmpty.DisplayName != before.DisplayName || afterEmpty.AvatarColor != before.AvatarColor {
		t.Fatalf("an empty change moved %q / %q", afterEmpty.DisplayName, afterEmpty.AvatarColor)
	}
	// An empty or blank display name is a real choice, not a validation error: it means
	// "show my username", which is what the client already falls back to.
	if w := request(t, handler, "PATCH", "/api/me", map[string]string{"display_name": "   "}, memberCookie); w.Code != 200 {
		t.Fatalf("clearing the display name: %d %s", w.Code, w.Body.String())
	}
	after := readJSON[db.User](t, request(t, handler, "GET", "/api/me", nil, memberCookie))
	if after.DisplayName != "" {
		t.Fatalf("a cleared display name reads back as %q", after.DisplayName)
	}
	if after.AvatarColor != before.AvatarColor || after.Username != before.Username {
		t.Fatal("a change moved a field it did not name")
	}
}

func TestARenameLeavesEveryOtherAccountAlone(t *testing.T) {
	s, handler := testServer(t)
	setupAdmin(t, s, handler)
	_, memberCookie := memberAccount(t, s, "renamer")
	bystanderID, bystanderCookie := memberAccount(t, s, "bystander")

	// The bystander's name is set through the same route, so a second rename that leaked
	// into another row would be visible as the bystander's own record changing under it.
	if w := request(t, handler, "PATCH", "/api/me", map[string]string{"display_name": "Bystander", "avatar_color": "#abcdef"}, bystanderCookie); w.Code != 200 {
		t.Fatalf("bystander rename: %d %s", w.Code, w.Body.String())
	}
	if w := request(t, handler, "PATCH", "/api/me", map[string]string{"display_name": "Renamed", "avatar_color": "#111111"}, memberCookie); w.Code != 200 {
		t.Fatalf("rename: %d %s", w.Code, w.Body.String())
	}
	bystander := readJSON[db.User](t, request(t, handler, "GET", "/api/me", nil, bystanderCookie))
	if bystander.ID != bystanderID || bystander.DisplayName != "Bystander" || bystander.AvatarColor != "#abcdef" {
		t.Fatalf("the bystander's own record moved: %+v", bystander)
	}
}

func TestADeactivatedAccountCannotRenameItself(t *testing.T) {
	s, handler := testServer(t)
	admin := setupAdmin(t, s, handler)
	memberID, memberCookie := memberAccount(t, s, "departing")

	// The administrator deactivates the account through the supported route. That ends
	// the account's sessions, so the rename from its old cookie is refused before any
	// handler runs — which is the answer its holder gets.
	if w := request(t, handler, "PATCH", "/api/admin/users/"+memberID, map[string]any{"is_deactivated": true, "confirm": "departing"}, admin); w.Code != 200 {
		t.Fatalf("deactivate: %d %s", w.Code, w.Body.String())
	}
	if w := request(t, handler, "PATCH", "/api/me", map[string]string{"display_name": "Still Here"}, memberCookie); w.Code != 401 {
		t.Fatalf("a deactivated account renamed itself: %d %s", w.Code, w.Body.String())
	}
	var name string
	if err := s.store.QueryRow(`SELECT display_name FROM users WHERE id=?`, memberID).Scan(&name); err != nil {
		t.Fatal(err)
	}
	if name != "" {
		t.Fatalf("the deactivated account's name is now %q", name)
	}

	// The handler's own check is the half the middleware cannot do for it: a deactivation
	// that commits between authentication and the transaction reaches the row inside it.
	// It cannot be produced over HTTP — a deactivated account has no session left to send
	// with — so the branch is exercised directly, with the request the middleware would
	// have refused earlier.
	deactivated := db.User{ID: memberID, Username: "departing", IsDeactivated: true}
	recorder := httptest.NewRecorder()
	attempt := httptest.NewRequest(http.MethodPatch, "/api/me", strings.NewReader(`{"display_name":"Still Here"}`))
	attempt.Header.Set("Content-Type", "application/json")
	s.updateMe(recorder, attempt, deactivated)
	if recorder.Code != 403 {
		t.Fatalf("the handler's own deactivation check answered %d: %s", recorder.Code, recorder.Body.String())
	}
	if err := s.store.QueryRow(`SELECT display_name FROM users WHERE id=?`, memberID).Scan(&name); err != nil {
		t.Fatal(err)
	}
	if name != "" {
		t.Fatalf("the direct attempt wrote %q", name)
	}
}

func TestARenameAnnouncesItselfOnTheAccountSocket(t *testing.T) {
	s, handler := testServer(t)
	setupAdmin(t, s, handler)
	memberID, memberCookie := memberAccount(t, s, "announcer")

	server := httptest.NewServer(handler)
	defer server.Close()
	socket := dialSocket(t, s, server, memberCookie)

	if w := request(t, handler, "PATCH", "/api/me", map[string]string{"display_name": "Announced"}, memberCookie); w.Code != 200 {
		t.Fatalf("rename: %d %s", w.Code, w.Body.String())
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	event := readEvent(t, ctx, socket, "user.updated")
	if !eventMentions(t, event, memberID) {
		t.Fatalf("the event does not name the account: %s", string(event.Data))
	}
}
