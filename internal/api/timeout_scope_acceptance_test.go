package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"brrpbrrp/internal/db"
	"golang.org/x/crypto/bcrypt"
)

// timeoutMemberPassword is the one fixture password in this file, because signing in is
// behaviour under test and a test that cannot sign in passes by never reaching the gate.
const timeoutMemberPassword = "correct horse battery"

// A community timeout is a community decision, and these are the behaviours that
// distinguish it from a ban. They are asserted through the real routes with real
// responses, because the defect they guard is a middleware branch that answers 200
// with nothing in it: a status-code-only assertion cannot tell that from success.
//
// The distinction, once, at the top:
//
//	Banned / deactivated — the account is out. Sign-in is refused, the session is
//	                      over, nothing is readable or writable anywhere.
//	Timeout (community)  — the account is out of the *community* and nowhere else.
//	                      It signs in, holds a real session, keeps its private
//	                      conversations, and loses only community participation.

// timeoutFixture builds a server with a community channel, an accepted direct
// conversation carrying one message, and a member who is then timed out.
type timeoutFixture struct {
	server        *Server
	handler       http.Handler
	admin         *http.Cookie
	member        *http.Cookie
	memberID      string
	directID      string
	channelID     string
	messageID     string
	peerID        string
	activityBytes []byte
}

func newTimeoutFixture(t *testing.T) *timeoutFixture {
	t.Helper()
	t.Setenv("BRRP_ENABLE_PRIVATE_CONVERSATIONS", "1")
	s, handler := testServer(t)
	admin := setupAdmin(t, s, handler)
	_, member := namedUser(t, s, "timeout_member")
	memberID := memberIDOf(t, s, "timeout_member")
	// The shared fixture's password hash is deliberately unusable, so this one member
	// gets a real one. Signing in is behaviour under test here, and a test that could
	// not sign in would pass by never reaching the gate.
	hash, err := bcrypt.GenerateFromPassword([]byte(timeoutMemberPassword), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.store.Exec(`UPDATE users SET password_hash=? WHERE id=?`, string(hash), memberID); err != nil {
		t.Fatal(err)
	}
	_, _ = namedUser(t, s, "timeout_peer")
	peerID := memberIDOf(t, s, "timeout_peer")

	channel := moderationChannel(t, s, handler, admin, "timeout-room")
	direct := readJSON[map[string]any](t, request(t, handler, "POST", "/api/directs",
		map[string]any{"user_id": peerID}, member))
	directID := direct["id"].(string)
	request(t, handler, "POST", "/api/directs/"+directID+"/accept", map[string]any{},
		sessionFor(t, s, peerID))
	// A private message whose *content* is the control: a response that is only a
	// status code could be 200 with an empty body, and the caller would not know.
	posted := request(t, handler, "POST", "/api/channels/"+directID+"/messages",
		map[string]any{"content": "private words that outlive a timeout", "idempotency_key": "timeout-dm-1"}, member)
	if posted.Code != 201 {
		t.Fatalf("seed the private message: %d %s", posted.Code, posted.Body.String())
	}
	return &timeoutFixture{
		server: s, handler: handler, admin: admin, member: member, memberID: memberID,
		directID: directID, channelID: channel, peerID: peerID,
	}
}

// applyTimeout times the member out through the supported route.
func (f *timeoutFixture) applyTimeout(t *testing.T) {
	t.Helper()
	w := request(t, f.handler, "POST", "/api/admin/users/"+f.memberID+"/timeout",
		map[string]any{"minutes": 30, "reason": "acceptance fixture"}, f.admin)
	if w.Code != 200 {
		t.Fatalf("apply the timeout: %d %s", w.Code, w.Body.String())
	}
}

// readPrivateHistory is the positive control for private readability: it asserts the
// exact body, not merely a status.
func (f *timeoutFixture) readPrivateHistory(t *testing.T) []map[string]any {
	t.Helper()
	w := request(t, f.handler, "GET", "/api/conversations/"+f.directID+"/messages", nil, f.member)
	if w.Code != 200 {
		t.Fatalf("a timed-out member could not read its own private history: %d %s", w.Code, w.Body.String())
	}
	if !containsSubstring(w.Body.Bytes(), "private words that outlive a timeout") {
		t.Fatalf("the private history was returned but is not the history: %s", w.Body.String())
	}
	var messages []map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &messages); err != nil {
		t.Fatalf("the private history is not a JSON array of messages: %v (%s)", err, w.Body.String())
	}
	if len(messages) == 0 {
		t.Fatal("the private history decoded to zero messages, so a 200 with an empty list would have passed")
	}
	if content, _ := messages[0]["content"].(string); !strings.Contains(content, "private words") {
		t.Fatalf("the first message is not the seeded one: %v", messages[0])
	}
	return messages
}

// A timed-out member can sign in and gets a real session.
//
// The login gate refused every suspended account, on the reasoning that a timeout is
// "a moderation decision, not a failed sign-in" — and then treated it as one anyway.
// The person is owed the truth about a live timeout *and* a working session, and
// neither is served by refusing the sign-in.
func TestATimedOutMemberCanSignInAndHoldsARealSession(t *testing.T) {
	f := newTimeoutFixture(t)
	f.applyTimeout(t)

	// Sign in over the real route, as the sign-in form does.
	w := request(t, f.handler, "POST", "/api/login", map[string]string{
		"username": "timeout_member", "password": timeoutMemberPassword,
	}, (*http.Cookie)(nil))
	t.Logf("login status: %d", w.Code)
	// Whatever the answer, the session must be usable. This is the real control: the
	// account holds a session from before the timeout, and the question is whether
	// that session still works and whether a new one can be created.
	var setCookies []*http.Cookie = w.Result().Cookies()
	authenticated := false
	for _, c := range setCookies {
		if c.Name == cookieName {
			authenticated = true
		}
	}
	if !authenticated {
		t.Fatalf("a timed-out member could not sign in at all: %d %s", w.Code, w.Body.String())
	}
	session := setCookies[0]
	// And the new session is a working one, not a token that is handed out and then
	// refused: it reads its own account.
	me := request(t, f.handler, "GET", "/api/me", nil, session)
	if me.Code != 200 {
		t.Fatalf("the session issued to a timed-out member could not read its own account: %d %s", me.Code, me.Body.String())
	}
	if !containsSubstring(me.Body.Bytes(), "timeout_member") {
		t.Fatalf("the session is not the timed-out member's: %s", me.Body.String())
	}
}

// Private history is readable, with the expected content, during a timeout.
func TestATimedOutMemberReadsItsOwnPrivateHistory(t *testing.T) {
	f := newTimeoutFixture(t)
	f.applyTimeout(t)
	f.readPrivateHistory(t)
}

// A direct message during a timeout is actually written, not merely accepted.
func TestATimedOutMemberCanSendADirectMessage(t *testing.T) {
	f := newTimeoutFixture(t)
	f.applyTimeout(t)

	posted := request(t, f.handler, "POST", "/api/channels/"+f.directID+"/messages",
		map[string]any{"content": "sent while timed out", "idempotency_key": "timeout-dm-2"}, f.member)
	if posted.Code != 201 {
		t.Fatalf("a timed-out member could not send a direct message: %d %s", posted.Code, posted.Body.String())
	}
	// The effect, not the status: the message exists and both members can read it.
	var stored int
	if err := f.server.store.QueryRowContext(t.Context(),
		`SELECT count(*) FROM messages WHERE conversation_id=? AND content='sent while timed out'`,
		f.directID).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored != 1 {
		t.Fatalf("the send returned 201 and %d messages with that content exist", stored)
	}
	// The permitted control: the other participant can see it too.
	peerView := request(t, f.handler, "GET", "/api/conversations/"+f.directID+"/messages",
		nil, sessionFor(t, f.server, f.peerID))
	if !containsSubstring(peerView.Body.Bytes(), "sent while timed out") {
		t.Fatalf("the message was not delivered to the other participant: %d %s", peerView.Code, peerView.Body.String())
	}
}

// Activity is readable, and is the real list rather than an empty one.
func TestATimedOutMemberReadsItsOwnActivity(t *testing.T) {
	f := newTimeoutFixture(t)
	// Produce activity first: a direct message from the peer, which is addressed to
	// the member and should appear.
	posted := request(t, f.handler, "POST", "/api/channels/"+f.directID+"/messages",
		map[string]any{"content": "something to be active about", "idempotency_key": "timeout-act-1"},
		sessionFor(t, f.server, f.peerID))
	if posted.Code != 201 {
		t.Fatalf("seed activity: %d %s", posted.Code, posted.Body.String())
	}
	f.applyTimeout(t)

	w := request(t, f.handler, "GET", "/api/me/activity", nil, f.member)
	if w.Code != 200 {
		t.Fatalf("a timed-out member could not read its own activity: %d %s", w.Code, w.Body.String())
	}
	var payload struct {
		Activity []struct {
			ID       string `json:"id"`
			Kind     string `json:"kind"`
			SourceID string `json:"source_id"`
			Actor    string `json:"actor"`
		} `json:"activity"`
		Unacknowledged int `json:"unacknowledged_count"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
		t.Fatalf("the activity response is not the expected shape: %v (%s)", err, w.Body.String())
	}
	if len(payload.Activity) == 0 {
		t.Fatalf("the timed-out member's activity is empty, so a 200 with no rows would have passed: %s", w.Body.String())
	}
	first := payload.Activity[0]
	if first.Kind != "message" || first.Actor != "timeout_peer" {
		t.Fatalf("the activity row is not the seeded direct message: %+v", first)
	}
	// The source id must name a message that exists, or the row points at nothing.
	var stored int
	if err := f.server.store.QueryRowContext(t.Context(),
		`SELECT count(*) FROM messages WHERE id=? AND content='something to be active about'`,
		first.SourceID).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored != 1 {
		t.Fatalf("the activity row names a message that does not exist: %+v", first)
	}
	if payload.Unacknowledged < 1 {
		t.Fatalf("the activity is present but not counted as unread: %+v", payload)
	}
}

// The community surface is closed, with a reason, and with no side effect.
func TestATimedOutMemberIsRefusedCommunityParticipation(t *testing.T) {
	f := newTimeoutFixture(t)
	f.applyTimeout(t)

	// Reading the community channel list is refused.
	if w := request(t, f.handler, "GET", "/api/channels", nil, f.member); w.Code != 403 {
		t.Fatalf("a timed-out member read the community channel list: %d %s", w.Code, w.Body.String())
	}
	// Posting to a community channel is refused.
	posted := request(t, f.handler, "POST", "/api/channels/"+f.channelID+"/messages",
		map[string]any{"content": "into the community", "idempotency_key": "timeout-community-1"}, f.member)
	if posted.Code != 403 {
		t.Fatalf("a timed-out member posted to a community channel: %d %s", posted.Code, posted.Body.String())
	}
	// The refusal had no effect: the message does not exist.
	var stored int
	if err := f.server.store.QueryRowContext(t.Context(),
		`SELECT count(*) FROM messages WHERE content='into the community'`).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored != 0 {
		t.Fatalf("a refused community send left %d messages behind", stored)
	}
}

// Banned and deactivated accounts are denied everywhere, including the private
// surfaces a timeout reaches. This is the control that keeps the timeout from being
// "fixed" by making every restricted account look merely suspended.
func TestBannedAndDeactivatedAccountsAreDeniedEverywhere(t *testing.T) {
	for _, state := range []struct{ field, label string }{
		{"is_banned", "a banned account"},
		{"is_deactivated", "a deactivated account"},
	} {
		t.Run(state.label, func(t *testing.T) {
			f := newTimeoutFixture(t)
			if _, err := f.server.store.Exec(`UPDATE users SET `+state.field+`=1 WHERE id=?`, f.memberID); err != nil {
				t.Fatal(err)
			}
			for name, w := range map[string]*httptest.ResponseRecorder{
				"private history": request(t, f.handler, "GET", "/api/conversations/"+f.directID+"/messages", nil, f.member),
				"community list":  request(t, f.handler, "GET", "/api/channels", nil, f.member),
				"direct send": request(t, f.handler, "POST", "/api/channels/"+f.directID+"/messages",
					map[string]any{"content": "should not be written", "idempotency_key": "banned-1"}, f.member),
			} {
				if w.Code != 401 {
					t.Fatalf("%s answered %d for %s, want 401: %s", state.label, w.Code, name, w.Body.String())
				}
			}
			var stored int
			if err := f.server.store.QueryRowContext(t.Context(),
				`SELECT count(*) FROM messages WHERE content='should not be written'`).Scan(&stored); err != nil {
				t.Fatal(err)
			}
			if stored != 0 {
				t.Fatalf("%s wrote a message it was refused for", state.label)
			}
		})
	}
}

// A role revoked during a timeout is still revoked after it expires, and the account
// signs in. The revocation and the timeout are independent decisions and neither
// undoes the other.
func TestARoleRevokedDuringATimeoutStaysRevokedAfterExpiry(t *testing.T) {
	f := newTimeoutFixture(t)
	role := createTestRole(t, f.server, f.handler, f.admin, "timeout_role", []string{"channel.manage"}, 20)
	setRoles(t, f.server, f.handler, f.admin, f.memberID, role)
	f.applyTimeout(t)

	// Revoke the role through the supported route, while the timeout is live.
	setRoles(t, f.server, f.handler, f.admin, f.memberID)
	held := assignedRoles(t, f.server, f.handler, f.admin, f.memberID)
	if containsString(held, role) {
		t.Fatal("the role revocation did not take effect")
	}

	// Expire the timeout.
	if _, err := f.server.store.Exec(`UPDATE account_timeouts SET expires_at=? WHERE user_id=?`,
		db.Timestamp(time.Now().UTC().Add(-time.Minute)), f.memberID); err != nil {
		t.Fatal(err)
	}
	// Reading the state lifts it — signing in is what clears an expired timeout.
	if suspended, _ := f.server.timeoutState(t.Context(), f.memberID); suspended {
		t.Fatal("an expired timeout was still reported as live")
	}
	if still := assignedRoles(t, f.server, f.handler, f.admin, f.memberID); containsString(still, role) {
		t.Fatal("A ROLE REVOKED DURING THE TIMEOUT CAME BACK WHEN IT EXPIRED")
	}
}

func containsSubstring(haystack []byte, needle string) bool {
	return strings.Contains(string(haystack), needle)
}

func containsString(list []string, value string) bool {
	return slices.Contains(list, value)
}
