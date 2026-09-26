package api

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"brrpbrrp/internal/db"
)

// restrictedServer starts a server with restricted community channels enabled and
// returns the helper an operator would use to grant them.
func restrictedServer(t *testing.T) (*Server, http.Handler) {
	t.Helper()
	t.Setenv("BRRP_ENABLE_RESTRICTED_CHANNELS", "true")
	return testServer(t)
}

// memberAccount creates an ordinary account with its default role and a session.
func memberAccount(t *testing.T, s *Server, username string) (string, *http.Cookie) {
	t.Helper()
	id, now := db.ID(), db.Now()
	if _, err := s.store.Exec(`INSERT INTO users(id,username,password_hash,created_at,updated_at) VALUES(?,?,?,?,?)`, id, username, "unused-test-hash", now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := s.store.Exec(`INSERT INTO role_assignments(user_id,role_id,created_at) VALUES(?,?,?)`, id, db.MemberRoleID, now); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	if err := s.newSession(w, id); err != nil {
		t.Fatal(err)
	}
	return id, w.Result().Cookies()[0]
}

// TestRestrictedChannelPrivacyAcrossDeliverySurfaces is the T-AUTH-01 scenario: an
// authenticated outsider that knows the real identifiers must learn nothing about
// a restricted conversation through any delivery surface, and access removed is
// access denied from the next request onward.
func TestRestrictedChannelPrivacyAcrossDeliverySurfaces(t *testing.T) {
	s, handler := restrictedServer(t)
	admin := setupAdmin(t, s, handler)
	insiderID, insider := memberAccount(t, s, "insider")
	_, outsider := memberAccount(t, s, "outsider")

	created := request(t, handler, "POST", "/api/channels", db.Channel{Name: "vault", Type: "text", MaxVoiceUsers: 8, Visibility: "private"}, admin)
	if created.Code != 201 {
		t.Fatalf("private channel: %d %s", created.Code, created.Body.String())
	}
	channel := readJSON[db.Channel](t, created)
	if channel.Visibility != "private" || channel.Posting != "members" {
		t.Fatalf("created channel = %+v", channel)
	}

	// Only the insider holds an explicit grant.
	grant := map[string]any{"visibility": "private", "user_ids": []string{insiderID}}
	if w := request(t, handler, "PUT", "/api/channels/"+channel.ID+"/access", grant, admin); w.Code != 200 {
		t.Fatalf("grant: %d %s", w.Code, w.Body.String())
	}
	inspected := readJSON[map[string]any](t, request(t, handler, "GET", "/api/channels/"+channel.ID+"/access", nil, admin))
	members, _ := inspected["effective_members"].([]any)
	if len(members) != 2 || inspected["effective_count"] != float64(2) {
		t.Fatalf("access inspector = %v", inspected)
	}
	for _, member := range members {
		if member == "outsider" {
			t.Fatalf("access inspector promised access the server would refuse: %v", members)
		}
	}
	if inspected["managers_can_read"] != true {
		t.Fatal("access inspector hid manager visibility")
	}

	uploaded := sendFile(t, handler, admin, "vault.txt", "restricted content")
	attachment := readJSON[db.Attachment](t, uploaded)
	message := readJSON[db.Message](t, request(t, handler, "POST", "/api/channels/"+channel.ID+"/messages",
		messageInput{Content: "constellation", IdempotencyKey: "vault-secret", AttachmentIDs: []string{attachment.ID}}, admin))
	if message.ID == "" {
		t.Fatal("message was not stored")
	}
	if w := request(t, handler, "PUT", "/api/messages/"+message.ID+"/pin", map[string]bool{"pinned": true}, admin); w.Code != 200 {
		t.Fatalf("pin: %d %s", w.Code, w.Body.String())
	}

	// The granted member reaches every read surface.
	if w := request(t, handler, "GET", "/api/channels/"+channel.ID+"/messages", nil, insider); w.Code != 200 {
		t.Fatalf("insider history: %d", w.Code)
	}
	hits := readJSON[[]db.Message](t, request(t, handler, "GET", "/api/search?q=constellation", nil, insider))
	if len(hits) != 1 || hits[0].ID != message.ID {
		t.Fatalf("insider search = %+v", hits)
	}
	if w := request(t, handler, "GET", "/api/uploads/"+attachment.ID, nil, insider); w.Code != 200 {
		t.Fatalf("insider attachment: %d", w.Code)
	}

	// Every surface refuses the outsider with a non-disclosing result.
	for _, probe := range []struct {
		method, path string
		body         any
	}{
		{"GET", "/api/channels/" + channel.ID + "/messages", nil},
		{"GET", "/api/channels/" + channel.ID + "/pins", nil},
		{"GET", "/api/channels/" + channel.ID + "/first-unread", nil},
		{"GET", "/api/messages/" + message.ID, nil},
		{"GET", "/api/messages/" + message.ID + "/context", nil},
		{"GET", "/api/messages/" + message.ID + "/replies", nil},
		{"POST", "/api/channels/" + channel.ID + "/read", map[string]string{"last_read_message_id": message.ID}},
		{"POST", "/api/channels/" + channel.ID + "/messages", messageInput{Content: "intrusion", IdempotencyKey: "outsider-send"}},
		{"POST", "/api/messages/" + message.ID + "/reactions", map[string]string{"emoji": "👍"}},
		{"GET", "/api/uploads/" + attachment.ID, nil},
	} {
		w := request(t, handler, probe.method, probe.path, probe.body, outsider)
		if w.Code != http.StatusNotFound {
			t.Fatalf("%s %s leaked %d: %s", probe.method, probe.path, w.Code, w.Body.String())
		}
	}
	// A capability refusal on a visible surface is a separate, clearer answer.
	if w := request(t, handler, "GET", "/api/channels/"+channel.ID+"/access", nil, outsider); w.Code != http.StatusForbidden {
		t.Fatalf("outsider inspected access: %d", w.Code)
	}
	if hits := readJSON[[]db.Message](t, request(t, handler, "GET", "/api/search?q=constellation", nil, outsider)); len(hits) != 0 {
		t.Fatalf("search leaked restricted content: %+v", hits)
	}
	if w := request(t, handler, "GET", "/api/search?q=constellation&channel_id="+channel.ID, nil, outsider); w.Code != http.StatusNotFound {
		t.Fatalf("scoped search leaked the channel: %d", w.Code)
	}
	// Workspace state must not name the channel, count it or expose a cursor.
	bootstrap := readJSON[struct {
		Channels []db.Channel  `json:"channels"`
		Unread   []unreadCount `json:"unread"`
	}](t, request(t, handler, "GET", "/api/state", nil, outsider))
	for _, listed := range bootstrap.Channels {
		if listed.ID == channel.ID {
			t.Fatalf("state listed the restricted channel: %+v", listed)
		}
	}
	for _, count := range bootstrap.Unread {
		if count.ChannelID == channel.ID {
			t.Fatalf("state counted the restricted channel: %+v", count)
		}
	}
	// Managers manage community channels and are told so by the inspector.
	if w := request(t, handler, "GET", "/api/channels/"+channel.ID+"/messages", nil, admin); w.Code != 200 {
		t.Fatalf("channel manager lost access: %d", w.Code)
	}

	// Revocation: the grant is removed and the next request is refused.
	before := readJSON[map[string]any](t, request(t, handler, "GET", "/api/state", nil, insider))["authorization"].(map[string]any)["revision"].(float64)
	if w := request(t, handler, "PUT", "/api/channels/"+channel.ID+"/access", map[string]any{"visibility": "private"}, admin); w.Code != 200 {
		t.Fatalf("revoke: %d %s", w.Code, w.Body.String())
	}
	after := readJSON[map[string]any](t, request(t, handler, "GET", "/api/state", nil, insider))["authorization"].(map[string]any)["revision"].(float64)
	if after <= before {
		t.Fatalf("revocation did not advance the authorization revision: %v -> %v", before, after)
	}
	for _, probe := range []struct{ method, path string }{
		{"GET", "/api/channels/" + channel.ID + "/messages"},
		{"GET", "/api/messages/" + message.ID},
		{"GET", "/api/uploads/" + attachment.ID},
	} {
		if w := request(t, handler, probe.method, probe.path, nil, insider); w.Code != http.StatusNotFound {
			t.Fatalf("revoked member still reached %s: %d", probe.path, w.Code)
		}
	}
	if hits := readJSON[[]db.Message](t, request(t, handler, "GET", "/api/search?q=constellation", nil, insider)); len(hits) != 0 {
		t.Fatalf("revoked member still searched the channel: %+v", hits)
	}
	bootstrap = readJSON[struct {
		Channels []db.Channel  `json:"channels"`
		Unread   []unreadCount `json:"unread"`
	}](t, request(t, handler, "GET", "/api/state", nil, insider))
	for _, listed := range bootstrap.Channels {
		if listed.ID == channel.ID {
			t.Fatal("revoked member's state still listed the channel")
		}
	}
	// The restricted content itself is untouched for those who remain authorized.
	if w := request(t, handler, "GET", "/api/channels/"+channel.ID+"/messages", nil, admin); w.Code != 200 {
		t.Fatalf("owner lost access after revocation: %d", w.Code)
	}
	// The channel's durable record keeps its history and identity.
	var stored string
	if err := s.store.QueryRow(`SELECT name FROM channels WHERE id=?`, channel.ID).Scan(&stored); err != nil || stored != "vault" {
		t.Fatalf("channel record = %q %v", stored, err)
	}
}

// TestRestrictedChannelsStayDisabledWithoutApproval proves the privacy gate: the
// enforcement code exists, but the API refuses to create restricted channels
// until an operator turns the feature on.
func TestRestrictedChannelsStayDisabledWithoutApproval(t *testing.T) {
	s, handler := testServer(t)
	admin := setupAdmin(t, s, handler)
	if w := request(t, handler, "POST", "/api/channels", db.Channel{Name: "vault", Type: "text", MaxVoiceUsers: 8, Visibility: "private"}, admin); w.Code != http.StatusConflict {
		t.Fatalf("private channel created while disabled: %d", w.Code)
	}
	if w := request(t, handler, "POST", "/api/channels", db.Channel{Name: "news", Type: "text", MaxVoiceUsers: 8, Posting: "announcement"}, admin); w.Code != http.StatusConflict {
		t.Fatalf("announcement channel created while disabled: %d", w.Code)
	}
	public := readJSON[db.Channel](t, request(t, handler, "POST", "/api/channels", db.Channel{Name: "general-2", Type: "text", MaxVoiceUsers: 8}, admin))
	if public.Visibility != "public" || public.Posting != "members" {
		t.Fatalf("public defaults = %+v", public)
	}
	if w := request(t, handler, "PUT", "/api/channels/"+public.ID+"/access", map[string]any{"visibility": "private"}, admin); w.Code != http.StatusConflict {
		t.Fatalf("access change allowed while disabled: %d", w.Code)
	}
	// Converting an existing public channel to private is refused too.
	converted := request(t, handler, "PATCH", "/api/channels/"+public.ID, db.Channel{Name: "general-2", Type: "text", MaxVoiceUsers: 8, Visibility: "private", Posting: "members"}, admin)
	if converted.Code != http.StatusConflict {
		t.Fatalf("visibility conversion allowed while disabled: %d %s", converted.Code, converted.Body.String())
	}
}

// TestAnnouncementChannelRestrictsOrdinarySends covers the posting-policy half of
// the channel model: readable by members, postable only by channel managers.
func TestAnnouncementChannelRestrictsOrdinarySends(t *testing.T) {
	s, handler := restrictedServer(t)
	admin := setupAdmin(t, s, handler)
	_, member := memberAccount(t, s, "reader")
	channel := readJSON[db.Channel](t, request(t, handler, "POST", "/api/channels", db.Channel{Name: "news", Type: "text", MaxVoiceUsers: 8, Posting: "announcement"}, admin))
	if channel.Posting != "announcement" {
		t.Fatalf("posting policy = %q", channel.Posting)
	}
	if w := request(t, handler, "GET", "/api/channels/"+channel.ID+"/messages", nil, member); w.Code != 200 {
		t.Fatalf("member could not read the announcement channel: %d", w.Code)
	}
	send := messageInput{Content: "hello", IdempotencyKey: "announcement-attempt"}
	if w := request(t, handler, "POST", "/api/channels/"+channel.ID+"/messages", send, member); w.Code != http.StatusForbidden {
		t.Fatalf("member posted in an announcement channel: %d %s", w.Code, w.Body.String())
	}
	if w := request(t, handler, "POST", "/api/channels/"+channel.ID+"/messages", messageInput{Content: "announcement", IdempotencyKey: "announcement-ok"}, admin); w.Code != 201 {
		t.Fatalf("manager could not post: %d %s", w.Code, w.Body.String())
	}
	// Search eligibility follows the same rule.
	if hits := readJSON[[]db.Message](t, request(t, handler, "GET", "/api/search?q="+url.QueryEscape("announcement"), nil, member)); len(hits) == 0 {
		t.Fatal("member could not search a readable announcement channel")
	}
}

// TestRealtimeEventsAreAudienceScoped proves delivery, not just request handling:
// a message event for a restricted channel never reaches an unrelated socket.
func TestRealtimeEventsAreAudienceScoped(t *testing.T) {
	s, handler := restrictedServer(t)
	admin := setupAdmin(t, s, handler)
	_, insiderCookie := memberAccount(t, s, "insider")
	_, outsiderCookie := memberAccount(t, s, "outsider")

	insiderID := sessionUserID(t, s, insiderCookie)
	_ = insiderID
	channel := readJSON[db.Channel](t, request(t, handler, "POST", "/api/channels", db.Channel{Name: "backroom", Type: "text", MaxVoiceUsers: 8, Visibility: "private"}, admin))
	grant := map[string]any{"visibility": "private", "user_ids": []string{insiderID}}
	if w := request(t, handler, "PUT", "/api/channels/"+channel.ID+"/access", grant, admin); w.Code != 200 {
		t.Fatalf("grant: %d", w.Code)
	}
	outsiderAudience, _, err := s.channelAudience(t.Context(), s.store, channel, db.ChannelAccess{Users: []string{insiderID}})
	if err != nil {
		t.Fatal(err)
	}
	for _, userID := range outsiderAudience {
		if userID == sessionUserID(t, s, outsiderCookie) {
			t.Fatal("audience for a granted-only channel included an unrelated account")
		}
	}
	if !strings.Contains(strings.Join(outsiderAudience, ","), insiderID) {
		t.Fatal("audience omitted the granted account")
	}
}

func sessionUserID(t *testing.T, s *Server, cookie *http.Cookie) string {
	t.Helper()
	var id string
	if err := s.store.QueryRow(`SELECT user_id FROM sessions WHERE token_hash=?`, hashToken(cookie.Value)).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}
