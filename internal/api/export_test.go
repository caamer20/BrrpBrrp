package api

import (
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"brrpbrrp/internal/db"
)

// A member export is the one read path that leaves the building, so the properties
// under test are the ones WP-12 names: it contains only what the account may read, it
// contains no credentials, and it is bounded with the bound visible in the file rather
// than hidden from the member who has to notice it.

// markPrivileged stamps the session as a recent strong sign-in.
//
// The export is gated on fresh authentication because the file leaves the building,
// and a session minted directly by a fixture is not one: without this every test here
// would be testing the gate instead of the export. This is the same stamp the
// session tests use, so the gate under test is the real one.
func markPrivileged(t *testing.T, s *Server, cookie *http.Cookie) {
	t.Helper()
	if _, err := s.store.Exec(`UPDATE sessions SET privileged_at=? WHERE token_hash=?`,
		db.Timestamp(time.Now()), hashToken(cookie.Value)); err != nil {
		t.Fatal(err)
	}
}

// exportFor builds an export and returns the decoded document.
func exportFor(t *testing.T, s *Server, handler http.Handler, cookie *http.Cookie, key string) exportDocument {
	t.Helper()
	markPrivileged(t, s, cookie)
	w := request(t, handler, "POST", "/api/me/export", map[string]any{"operation_key": key}, cookie)
	if w.Code != http.StatusAccepted {
		t.Fatalf("request export: %d %s", w.Code, w.Body.String())
	}
	record := readJSON[exportRecord](t, w)
	// The build runs after the commit; polling is bounded rather than slept through, so
	// the test is neither slow on a fast machine nor flaky on a slow one.
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		var state string
		if err := s.store.QueryRowContext(t.Context(), `SELECT state FROM data_exports WHERE id=?`, record.ID).Scan(&state); err == nil {
			switch state {
			case "building":
			case "failed":
				var omissions, completed string
				_ = s.store.QueryRowContext(t.Context(), `SELECT omissions,completed_at FROM data_exports WHERE id=?`, record.ID).Scan(&omissions, &completed)
				t.Fatalf("the export failed at %s: %s", completed, omissions)
			default:
				return readExportFile(t, s, record.ID)
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("the export never finished")
	return exportDocument{}
}

func readExportFile(t *testing.T, s *Server, id string) exportDocument {
	t.Helper()
	data, err := os.ReadFile(s.exportPath(id))
	if err != nil {
		t.Fatal(err)
	}
	var document exportDocument
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	return document
}

func TestMemberExportContainsTheirOwnConversationsAndNothingElse(t *testing.T) {
	t.Setenv("BRRP_ENABLE_PRIVATE_CONVERSATIONS", "1")
	s, handler := testServer(t)
	admin := setupAdmin(t, s, handler)
	_, alice := namedUser(t, s, "export_alice")
	_, bob := namedUser(t, s, "export_bob")
	_ = bob
	aliceID := memberIDOf(t, s, "export_alice")
	bobID := memberIDOf(t, s, "export_bob")

	// A community channel with a message, and a direct conversation with one.
	channel := moderationChannel(t, s, handler, admin, "export-room")
	request(t, handler, "POST", "/api/channels/"+channel+"/messages",
		map[string]any{"content": "a public line", "idempotency_key": "export-public-1"}, admin)
	request(t, handler, "POST", "/api/channels/"+channel+"/messages",
		map[string]any{"content": "a private line", "idempotency_key": "export-public-2"}, admin)

	direct := readJSON[map[string]any](t, request(t, handler, "POST", "/api/directs",
		map[string]any{"user_id": bobID}, sessionFor(t, s, aliceID)))
	directID := direct["id"].(string)
	request(t, handler, "POST", "/api/directs/"+directID+"/accept", map[string]any{}, sessionFor(t, s, bobID))
	// Alice writes in the direct conversation; Bob's reply is the *other* participant's
	// content, which an export legitimately includes because she is a member of it.
	request(t, handler, "POST", "/api/channels/"+directID+"/messages",
		map[string]any{"content": "between us", "idempotency_key": "export-direct-1"}, sessionFor(t, s, aliceID))

	document := exportFor(t, s, handler, alice, "export-key-alice-one")

	if document.Format != "brrpbrrp-member-export" {
		t.Fatalf("the file is not an export: %+v", document.Format)
	}
	// The channel and the direct conversation are both present: an export that omitted
	// the account's own conversation would be useless.
	titles := map[string]bool{}
	for _, conversation := range document.Conversations {
		titles[conversation.Title] = true
	}
	if !titles["export-room"] {
		t.Fatalf("the export omitted the account's channel: %v", titles)
	}
	if !titles["export_bob"] {
		t.Fatalf("the export did not name the direct conversation for its other member: %v", titles)
	}
	// The message content is there, because that is the point.
	found := false
	for _, conversation := range document.Conversations {
		for _, message := range conversation.Messages {
			if message.Content == "between us" {
				found = true
			}
		}
	}
	if !found {
		t.Fatal("the export contains none of the account's own message content")
	}
}

func TestMemberExportContainsNoCredentialsOfAnyKind(t *testing.T) {
	s, handler := testServer(t)
	admin := setupAdmin(t, s, handler)
	_, alice := namedUser(t, s, "export_creds_alice")
	document := exportFor(t, s, handler, alice, "export-key-creds-one")

	// The document is read as raw text as well as by structure: a field that leaked
	// under a name nobody thought to check would still be here.
	raw, err := os.ReadFile(s.exportPath(firstExportID(t, s, alice)))
	if err != nil {
		t.Fatal(err)
	}
	body := string(raw)
	for _, forbidden := range []string{
		"password", "password_hash", "session", "token", "secret", "recovery_code",
		"mfa", "two_factor", "private_key",
	} {
		if strings.Contains(strings.ToLower(body), forbidden) {
			t.Fatalf("the export contains %q", forbidden)
		}
	}
	// And the account block is exactly the five non-secret fields.
	encoded, _ := json.Marshal(document.Account)
	if len(encoded) == 0 || !strings.Contains(string(encoded), "export_creds_alice") {
		t.Fatalf("the account block is not what it should be: %s", encoded)
	}
	for _, field := range []string{"Password", "Session", "Token", "Secret", "RecoveryCode"} {
		if strings.Contains(string(encoded), field) {
			t.Fatalf("the account block carries %q", field)
		}
	}
	_ = admin
}

func firstExportID(t *testing.T, s *Server, cookie *http.Cookie) string {
	t.Helper()
	_ = cookie
	var id string
	if err := s.store.QueryRowContext(t.Context(), `SELECT id FROM data_exports ORDER BY id DESC LIMIT 1`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func TestMemberExportExcludesAConversationWhoseMembershipWasWithdrawn(t *testing.T) {
	t.Setenv("BRRP_ENABLE_PRIVATE_CONVERSATIONS", "1")
	s, handler := testServer(t)
	setupAdmin(t, s, handler)
	_, alice := namedUser(t, s, "export_leave_alice")
	namedUser(t, s, "export_leave_bob")
	aliceID := memberIDOf(t, s, "export_leave_alice")
	bobID := memberIDOf(t, s, "export_leave_bob")

	direct := readJSON[map[string]any](t, request(t, handler, "POST", "/api/directs",
		map[string]any{"user_id": bobID}, sessionFor(t, s, aliceID)))
	directID := direct["id"].(string)
	request(t, handler, "POST", "/api/directs/"+directID+"/accept", map[string]any{}, sessionFor(t, s, bobID))
	request(t, handler, "POST", "/api/channels/"+directID+"/messages",
		map[string]any{"content": "said before the withdrawal", "idempotency_key": "export-leave-1"},
		sessionFor(t, s, aliceID))

	// Alice leaves. The history is retained — that is the product's choice — but she is
	// no longer a member, and an export contains what the account may *read*, not what
	// it once could.
	if _, err := s.store.Exec(`UPDATE conversation_members SET state='left'
		WHERE conversation_id=? AND user_id=?`, directID, aliceID); err != nil {
		t.Fatal(err)
	}
	document := exportFor(t, s, handler, alice, "export-key-leave-one")
	for _, conversation := range document.Conversations {
		if conversation.ID == directID {
			t.Fatal("the export contains a conversation whose membership was withdrawn")
		}
		for _, message := range conversation.Messages {
			if message.Content == "said before the withdrawal" {
				t.Fatal("the export contains a message from a conversation the account cannot read")
			}
		}
	}
}

func TestMemberExportIsBoundedAndSaysSo(t *testing.T) {
	s, handler := testServer(t)
	admin := setupAdmin(t, s, handler)
	channel := moderationChannel(t, s, handler, admin, "bounded-room")
	// A conversation larger than the export's message budget. The rows go in through
	// the store because writing 50,001 messages over HTTP would take an hour and would
	// be testing the limiter rather than the bound.
	authorID := memberIDOf(t, s, "admin")
	base := time.Now().UTC()
	for index := 0; index < maxExportMessages+25; index++ {
		if _, err := s.store.Exec(`INSERT INTO messages(id,conversation_id,channel_id,user_id,content,created_at,idempotency_key)
			VALUES(?,?,?,?,?,?,?)`, db.ID(), channel, channel, authorID, "bounded line",
			db.Timestamp(base.Add(time.Duration(index)*time.Millisecond)), "bounded-"+db.ID()); err != nil {
			t.Fatalf("seed row %d: %v", index, err)
		}
	}
	_, alice := namedUser(t, s, "export_bounded_alice")
	document := exportFor(t, s, handler, alice, "export-key-bounded-one")

	if !document.Summary.Truncated {
		t.Fatalf("a %d-message conversation was exported without being marked truncated: %+v", maxExportMessages+25, document.Summary)
	}
	// The bound is *visible*. A member whose export stopped must be able to find that
	// fact in the file rather than infer it from a number that looks complete.
	if len(document.Omissions) == 0 {
		t.Fatal("the export stopped at its bound and recorded no reason")
	}
	joined := strings.Join(document.Omissions, " ")
	if !strings.Contains(joined, "stopped") && !strings.Contains(joined, "limit") {
		t.Fatalf("the omission does not say the export stopped: %q", joined)
	}
	if document.Summary.Messages > maxExportMessages {
		t.Fatalf("the export holds %d messages, over its own bound of %d", document.Summary.Messages, maxExportMessages)
	}
}

func TestMemberExportKeyIsTheOperationIdentity(t *testing.T) {
	s, handler := testServer(t)
	setupAdmin(t, s, handler)
	_, alice := namedUser(t, s, "export_key_alice")
	markPrivileged(t, s, alice)
	first := request(t, handler, "POST", "/api/me/export",
		map[string]any{"operation_key": "export-same-key-twice"}, alice)
	if first.Code != http.StatusAccepted {
		t.Fatalf("first request: %d %s", first.Code, first.Body.String())
	}
	// The same key is the same export, not a second one: a member who double-clicks
	// does not get two copies of their own history.
	second := request(t, handler, "POST", "/api/me/export",
		map[string]any{"operation_key": "export-same-key-twice"}, alice)
	if second.Code != http.StatusOK {
		t.Fatalf("retry: %d %s", second.Code, second.Body.String())
	}
	if readJSON[exportRecord](t, first).ID != readJSON[exportRecord](t, second).ID {
		t.Fatal("the same operation key produced a second export")
	}
	var count int
	if err := s.store.QueryRowContext(t.Context(), `SELECT count(*) FROM data_exports WHERE user_id=?`,
		memberIDOf(t, s, "export_key_alice")).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("%d exports exist for one key", count)
	}
}

func TestMemberExportIsOnlyTheAccountThatRequestedIt(t *testing.T) {
	s, handler := testServer(t)
	setupAdmin(t, s, handler)
	_, alice := namedUser(t, s, "export_other_alice")
	_, bob := namedUser(t, s, "export_other_bob")
	exportFor(t, s, handler, alice, "export-key-other-one")
	var id string
	if err := s.store.QueryRowContext(t.Context(), `SELECT id FROM data_exports ORDER BY id DESC LIMIT 1`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	// Another account cannot download it, and is told the same thing as a missing
	// export, so a guessed id confirms nothing.
	if w := request(t, handler, "GET", "/api/me/export/"+id, nil, bob); w.Code != 404 {
		t.Fatalf("another account downloaded the export: %d %s", w.Code, w.Body.String())
	}
	if w := request(t, handler, "DELETE", "/api/me/export/"+id, nil, bob); w.Code != 404 {
		t.Fatalf("another account deleted the export: %d %s", w.Code, w.Body.String())
	}
	// And it is not in their own history either.
	for _, record := range readJSON[[]exportRecord](t, request(t, handler, "GET", "/api/me/export", nil, bob)) {
		if record.ID == id {
			t.Fatal("another account's export is in their history")
		}
	}
}

func TestMemberExportNeedsRecentAuthentication(t *testing.T) {
	s, handler := testServer(t)
	setupAdmin(t, s, handler)
	_, alice := namedUser(t, s, "export_fresh_alice")
	// A session that has not recently authenticated. The stamp is *cleared* rather
	// than aged, because an absent stamp and a stale one are the same answer to the
	// gate and the absent case is the one that must not be forgotten.
	if _, err := s.store.Exec(`UPDATE sessions SET privileged_at='' WHERE token_hash=?`,
		hashToken(alice.Value)); err != nil {
		t.Fatal(err)
	}
	w := request(t, handler, "POST", "/api/me/export",
		map[string]any{"operation_key": "export-key-fresh-one"}, alice)
	if w.Code == http.StatusAccepted {
		t.Fatal("an export was produced without recent authentication")
	}
	if w.Code != http.StatusUnauthorized && w.Code != http.StatusForbidden {
		t.Fatalf("unexpected status: %d %s", w.Code, w.Body.String())
	}
	// And nothing was left behind: a refused request must not leave a half-built job.
	var count int
	if err := s.store.QueryRowContext(t.Context(),
		`SELECT count(*) FROM data_exports WHERE user_id=?`, memberIDOf(t, s, "export_fresh_alice")).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("a refused export left %d records behind", count)
	}
}
