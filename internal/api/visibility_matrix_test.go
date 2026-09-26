package api

import (
	"net/http"
	"testing"

	"brrpbrrp/internal/db"
)

// TestReadSurfacesAgreeOnTheAccessMatrix is the F-04 regression: the SQL
// visibility condition, the policy engine and the access inspector must agree, so
// no read surface may be more permissive than a direct read.
func TestReadSurfacesAgreeOnTheAccessMatrix(t *testing.T) {
	s, handler := restrictedServer(t)
	admin := setupAdmin(t, s, handler)
	insiderID, insider := memberAccount(t, s, "insider")
	_, outsider := memberAccount(t, s, "outsider")

	if w := request(t, handler, "POST", "/api/categories", map[string]any{"name": "restricted-only"}, admin); w.Code != http.StatusCreated {
		t.Fatalf("category: %d %s", w.Code, w.Body.String())
	}
	channel := readJSON[db.Channel](t, request(t, handler, "POST", "/api/channels",
		db.Channel{Name: "vault", Type: "text", Category: "restricted-only", MaxVoiceUsers: 8, Visibility: "private"}, admin))
	if channel.ID == "" {
		t.Fatal("private channel was not created")
	}
	grant := map[string]any{"visibility": "private", "user_ids": []string{insiderID}}
	if w := request(t, handler, "PUT", "/api/channels/"+channel.ID+"/access", grant, admin); w.Code != 200 {
		t.Fatalf("grant: %d %s", w.Code, w.Body.String())
	}
	message := readJSON[db.Message](t, request(t, handler, "POST", "/api/channels/"+channel.ID+"/messages",
		messageInput{Content: "matrix marker", IdempotencyKey: "matrix-1"}, admin))
	if message.ID == "" {
		t.Fatal("message was not stored")
	}

	readSurfaces := func(cookie *http.Cookie) (channels int, hits int, counted bool, historyCode int) {
		t.Helper()
		listed := readJSON[[]db.Channel](t, request(t, handler, "GET", "/api/channels", nil, cookie))
		for _, candidate := range listed {
			if candidate.ID == channel.ID {
				channels++
			}
		}
		hits = len(readJSON[[]db.Message](t, request(t, handler, "GET", "/api/search?q=matrix", nil, cookie)))
		counts := readJSON[[]unreadCount](t, request(t, handler, "GET", "/api/unread", nil, cookie))
		for _, count := range counts {
			if count.ChannelID == channel.ID {
				counted = true
			}
		}
		historyCode = request(t, handler, "GET", "/api/channels/"+channel.ID+"/messages", nil, cookie).Code
		return channels, hits, counted, historyCode
	}

	// The insider reaches every surface.
	if channels, hits, counted, historyCode := readSurfaces(insider); channels != 1 || hits != 1 || !counted || historyCode != http.StatusOK {
		t.Fatalf("insider matrix: channels=%d hits=%d counted=%v history=%d", channels, hits, counted, historyCode)
	}
	// The outsider reaches none of them: a direct read is denied, so no derived
	// surface may report the channel, its content, its activity or its count.
	if channels, hits, counted, historyCode := readSurfaces(outsider); channels != 0 || hits != 0 || counted || historyCode != http.StatusNotFound {
		t.Fatalf("outsider matrix: channels=%d hits=%d counted=%v history=%d", channels, hits, counted, historyCode)
	}

	// The category name is shared workspace structure, but its count is not: a
	// category holding only a channel the caller cannot open reports no count.
	categoriesFor := func(cookie *http.Cookie) int {
		t.Helper()
		for _, category := range readJSON[[]db.Category](t, request(t, handler, "GET", "/api/categories", nil, cookie)) {
			if category.Name == "restricted-only" {
				return category.Channels
			}
		}
		return -1
	}
	if count := categoriesFor(insider); count != 1 {
		t.Fatalf("the insider's category count = %d, want 1", count)
	}
	if count := categoriesFor(outsider); count != 0 {
		t.Fatalf("the outsider's category count = %d, want 0: it discloses a restricted channel", count)
	}
	// The same scoping applies to the bootstrap snapshot.
	for _, category := range readJSON[struct {
		Categories []db.Category `json:"categories"`
	}](t, request(t, handler, "GET", "/api/state", nil, outsider)).Categories {
		if category.Name == "restricted-only" && category.Channels != 0 {
			t.Fatalf("state disclosed %d restricted channels in a category", category.Channels)
		}
	}
}

// TestAccessGrantedThroughTheDefaultRoleIsHonouredByEveryRead proves the SQL
// condition uses the roles the policy engine resolved, including the default role
// an account holds without an assignment. It used to require an explicit
// role_assignments row, so such an account was refused history the policy engine
// would have allowed.
func TestAccessGrantedThroughTheDefaultRoleIsHonouredByEveryRead(t *testing.T) {
	s, handler := restrictedServer(t)
	admin := setupAdmin(t, s, handler)
	unassignedID, unassigned := memberAccount(t, s, "unassigned")
	// An account with no role assignment at all: the default role is the only
	// authority it has.
	if _, err := s.store.Exec(`DELETE FROM role_assignments WHERE user_id=?`, unassignedID); err != nil {
		t.Fatal(err)
	}
	channel := readJSON[db.Channel](t, request(t, handler, "POST", "/api/channels",
		db.Channel{Name: "role-vault", Type: "text", MaxVoiceUsers: 8, Visibility: "private"}, admin))
	grant := map[string]any{"visibility": "private", "role_ids": []string{db.MemberRoleID}}
	if w := request(t, handler, "PUT", "/api/channels/"+channel.ID+"/access", grant, admin); w.Code != 200 {
		t.Fatalf("grant: %d %s", w.Code, w.Body.String())
	}
	message := readJSON[db.Message](t, request(t, handler, "POST", "/api/channels/"+channel.ID+"/messages",
		messageInput{Content: "default role marker", IdempotencyKey: "default-role-1"}, admin))

	if w := request(t, handler, "GET", "/api/channels/"+channel.ID+"/messages", nil, unassigned); w.Code != http.StatusOK {
		t.Fatalf("an account holding the granted role read no history: %d %s", w.Code, w.Body.String())
	}
	if hits := readJSON[[]db.Message](t, request(t, handler, "GET", "/api/search?q=default", nil, unassigned)); len(hits) != 1 || hits[0].ID != message.ID {
		t.Fatalf("search disagreed with the direct read: %+v", hits)
	}
	listed := readJSON[[]db.Channel](t, request(t, handler, "GET", "/api/channels", nil, unassigned))
	found := false
	for _, candidate := range listed {
		if candidate.ID == channel.ID {
			found = true
		}
	}
	if !found {
		t.Fatal("the channel list disagreed with the direct read")
	}
}

// TestRemovingChannelReadRemovesEveryChannel proves the SQL condition and the
// policy engine both consult channel.read. A stored condition that only checked
// visibility would keep listing public channels to an account whose role no
// longer includes reading at all.
func TestRemovingChannelReadRemovesEveryChannel(t *testing.T) {
	s, handler := testServer(t)
	setupAdmin(t, s, handler)
	_, member := memberAccount(t, s, "reader")
	if len(readJSON[[]db.Channel](t, request(t, handler, "GET", "/api/channels", nil, member))) == 0 {
		t.Fatal("the member saw no channels to begin with")
	}
	if _, err := s.store.Exec(`UPDATE roles SET permissions='["message.send"]' WHERE is_default=1`); err != nil {
		t.Fatal(err)
	}
	if channels := readJSON[[]db.Channel](t, request(t, handler, "GET", "/api/channels", nil, member)); len(channels) != 0 {
		t.Fatalf("an account without channel.read still listed %d channels", len(channels))
	}
	if counts := readJSON[[]unreadCount](t, request(t, handler, "GET", "/api/unread", nil, member)); len(counts) != 0 {
		t.Fatalf("an account without channel.read still received %d unread counts", len(counts))
	}
}
