package api

import (
	"net/http"
	"strings"
	"testing"

	"brrpbrrp/internal/db"
)

func TestAccountRemovalPreservesConversationAndEndsIdentity(t *testing.T) {
	s, h := testServer(t)
	admin := setupAdmin(t, s, h)
	enrollMFA(t, h, admin, "correct horse battery")
	id, member := memberAccount(t, s, "departing")
	channel := textChannelID(t, s)
	created := request(t, h, "POST", "/api/channels/"+channel+"/messages", messageInput{Content: "keep the conversation", IdempotencyKey: "departing-message"}, member)
	if created.Code != 201 {
		t.Fatal(created.Body.String())
	}
	message := readJSON[db.Message](t, created)
	reply := readJSON[db.Message](t, request(t, h, "POST", "/api/channels/"+channel+"/messages", messageInput{Content: "a surviving reply", IdempotencyKey: "surviving-reply", ReplyToID: message.ID}, admin))
	if _, err := s.store.Exec(`INSERT INTO conversation_members(conversation_id,user_id,state,joined_at,updated_at) VALUES(?,?,'accepted',?,?)`, channel, id, db.Now(), db.Now()); err != nil {
		t.Fatal(err)
	}
	w := request(t, h, "POST", "/api/admin/users/"+id+"/anonymize", map[string]string{"confirm": "departing"}, admin)
	if w.Code != 200 {
		t.Fatalf("remove: %d %s", w.Code, w.Body.String())
	}
	removed, err := s.store.Account(t.Context(), s.store, id)
	if err != nil {
		t.Fatal(err)
	}
	if !removed.IsDeleted || !removed.IsDeactivated || removed.IsAdmin || removed.DisplayName != "Deleted member" || removed.Username == "departing" {
		t.Fatalf("identity not removed: %+v", removed)
	}
	for _, table := range []string{"sessions", "user_mfa", "recovery_codes", "role_assignments", "conversation_members"} {
		var n int
		if err := s.store.QueryRow("SELECT count(*) FROM "+table+" WHERE user_id=?", id).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 0 {
			t.Fatalf("%s left %d identity records", table, n)
		}
	}
	var receipts int
	s.store.QueryRow(`SELECT count(*) FROM message_operations WHERE account_id=?`, id).Scan(&receipts)
	if receipts != 0 {
		t.Fatal("removed identity kept operation metadata")
	}
	kept := readJSON[db.Message](t, request(t, h, "GET", "/api/messages/"+message.ID, nil, admin))
	if kept.Content != "keep the conversation" || kept.UserID != id {
		t.Fatalf("history damaged: %+v", kept)
	}
	keptReply := readJSON[db.Message](t, request(t, h, "GET", "/api/messages/"+reply.ID, nil, admin))
	if keptReply.ReplyToID != message.ID {
		t.Fatal("reply continuity lost")
	}
	if w := request(t, h, "GET", "/api/state", nil, member); w.Code != 401 {
		t.Fatalf("old session still active: %d", w.Code)
	}
	if w := request(t, h, "PATCH", "/api/admin/users/"+id, map[string]any{"is_deactivated": false, "confirm": removed.Username}, admin); w.Code != 409 {
		t.Fatalf("reactivation: %d %s", w.Code, w.Body.String())
	}
	if _, err := s.store.Exec(`UPDATE users SET is_deactivated=0 WHERE id=?`, id); err == nil {
		t.Fatal("storage allowed reactivation")
	}
	var audits int
	s.store.QueryRow(`SELECT count(*) FROM audit_logs WHERE action='user.anonymized' AND target_id=?`, id).Scan(&audits)
	if audits != 1 {
		t.Fatalf("audit count %d", audits)
	}
	// Erasing retained content is a separate, explicitly confirmed action.
	if w := request(t, h, "DELETE", "/api/admin/users/"+id, map[string]string{"confirm": removed.Username}, admin); w.Code != 200 {
		t.Fatalf("explicit erase: %d %s", w.Code, w.Body.String())
	}
	if w := request(t, h, "GET", "/api/messages/"+message.ID, nil, admin); w.Code != 404 {
		t.Fatalf("erased content still exists: %d", w.Code)
	}
	if w := request(t, h, "GET", "/api/messages/"+reply.ID, nil, admin); w.Code != 200 {
		t.Fatal("erasing author deleted another author's reply")
	}
}

func TestAccountRemovalRequiresCurrentConfirmationAndAuthority(t *testing.T) {
	s, h := testServer(t)
	admin := setupAdmin(t, s, h)
	enrollMFA(t, h, admin, "correct horse battery")
	id, member := memberAccount(t, s, "keepme")
	for _, tc := range []struct {
		name, confirm string
		cookie        *http.Cookie
		want          int
	}{
		{"wrong confirmation", "wrong", admin, 400},
		{"member forbidden", "keepme", member, 403},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := request(t, h, "POST", "/api/admin/users/"+id+"/anonymize", map[string]string{"confirm": tc.confirm}, tc.cookie)
			if w.Code != tc.want {
				t.Fatalf("%d %s", w.Code, w.Body.String())
			}
		})
	}
	account, err := s.store.Account(t.Context(), s.store, id)
	if err != nil || account.IsDeleted {
		t.Fatal("refused removal changed identity")
	}
	adminID := sessionUserID(t, s, admin)
	if w := request(t, h, "POST", "/api/admin/users/"+adminID+"/anonymize", map[string]string{"confirm": "admin"}, admin); w.Code != 403 {
		t.Fatalf("self removal: %d %s", w.Code, w.Body.String())
	}
	// All updates, including the identity change and its audit, roll back together.
	if _, err := s.store.Exec(`CREATE TRIGGER lifecycle_audit_failure BEFORE INSERT ON audit_logs WHEN NEW.action='user.anonymized' BEGIN SELECT RAISE(FAIL,'audit unavailable'); END`); err != nil {
		t.Fatal(err)
	}
	w := request(t, h, "POST", "/api/admin/users/"+id+"/anonymize", map[string]string{"confirm": "keepme"}, admin)
	if w.Code != 500 {
		t.Fatalf("audit failure: %d %s", w.Code, w.Body.String())
	}
	account, err = s.store.Account(t.Context(), s.store, id)
	if err != nil || account.IsDeleted || !strings.EqualFold(account.Username, "keepme") {
		t.Fatal("failed transaction did not preserve account")
	}
	if w := request(t, h, "GET", "/api/state", nil, member); w.Code != 200 {
		t.Fatal("rollback ended the member session")
	}
}
