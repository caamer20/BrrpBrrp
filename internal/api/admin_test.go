package api

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"brrpbrrp/internal/app"
	"brrpbrrp/internal/backup"
	"brrpbrrp/internal/db"
)

func TestAdministrationAndRestore(t *testing.T) {
	s, handler := testServer(t)
	admin := setupAdmin(t, s, handler)
	memberID, member := ordinaryUser(t, s)
	for _, path := range []string{"/api/admin/users", "/api/admin/settings", "/api/admin/health", "/api/admin/audit-logs"} {
		if w := request(t, handler, "GET", path, nil, member); w.Code != 403 {
			t.Fatalf("member accessed %s: %d", path, w.Code)
		}
	}
	created := request(t, handler, "POST", "/api/admin/users", map[string]any{"username": "new_member", "password": "a private initial passphrase", "display_name": "New member"}, admin)
	if created.Code != 201 {
		t.Fatal(created.Body.String())
	}
	user := readJSON[db.User](t, created)
	if w := request(t, handler, "POST", "/api/login", map[string]string{"username": user.Username, "password": "a private initial passphrase"}, nil); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	me := readJSON[db.User](t, request(t, handler, "GET", "/api/me", nil, admin))
	for _, input := range []map[string]any{{"is_admin": false, "confirm": "admin"}, {"is_banned": true, "confirm": "admin"}} {
		if w := request(t, handler, "PATCH", "/api/admin/users/"+me.ID, input, admin); w.Code != 400 {
			t.Fatalf("self lockout: %d", w.Code)
		}
	}
	if w := request(t, handler, "PATCH", "/api/admin/users/"+memberID, map[string]any{"is_banned": true, "confirm": "wrong"}, admin); w.Code != 400 {
		t.Fatal("ban accepted without confirmation")
	}
	if w := request(t, handler, "PATCH", "/api/admin/users/"+memberID, map[string]any{"is_banned": true, "confirm": "member"}, admin); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	if w := request(t, handler, "GET", "/api/me", nil, member); w.Code != 401 {
		t.Fatal("banned session remained active")
	}
	settings := s.Settings()
	settings.Purge = true
	settings.RetentionDays = 30
	if w := request(t, handler, "PUT", "/api/admin/settings", map[string]any{"settings": settings}, admin); w.Code != 400 {
		t.Fatal("purge enabled without confirmation")
	}
	if w := request(t, handler, "PUT", "/api/admin/settings", map[string]any{"settings": settings, "confirm": settings.ServerName}, admin); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	channels, _ := s.channels()
	var channelID string
	for _, channel := range channels {
		if channel.Type == "text" {
			channelID = channel.ID
			break
		}
	}
	message := readJSON[db.Message](t, request(t, handler, "POST", "/api/channels/"+channelID+"/messages", messageInput{Content: "restore searchable keepsake", IdempotencyKey: "backup-1"}, admin))
	attachmentID, name := db.ID(), db.ID()
	if err := os.WriteFile(filepath.Join(s.config.UploadDir, name), []byte("keepsake"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.store.Exec(`INSERT INTO attachments(id,message_id,uploader_id,file_name,stored_name,file_size,mime_type,created_at) VALUES(?,?,?,?,?,?,?,?)`, attachmentID, message.ID, me.ID, "note.txt", name, 8, "text/plain", db.Now()); err != nil {
		t.Fatal(err)
	}
	// An operator action that can reach every private boundary needs an enrolled
	// second factor on top of a recent sign-in (WP-05, D-10).
	if w := request(t, handler, "POST", "/api/admin/backup", map[string]bool{"include_uploads": true}, admin); w.Code != 403 {
		t.Fatalf("unenrolled operator archived the server: %d %s", w.Code, w.Body.String())
	}
	enrollMFA(t, handler, admin, "correct horse battery")
	response := request(t, handler, "POST", "/api/admin/backup", map[string]bool{"include_uploads": true}, admin)
	if response.Code != 201 {
		t.Fatal(response.Body.String())
	}
	result := readJSON[struct {
		ID  string `json:"id"`
		URL string `json:"url"`
	}](t, response)
	if w := request(t, handler, "GET", result.URL, nil, member); w.Code != 401 {
		t.Fatal("backup exposed")
	}
	source, err := os.ReadFile(filepath.Join(s.config.DataDir, "backups", result.ID+".zip"))
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("BRRP_DATA_DIR", t.TempDir())
	destination, err := app.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if err := destination.Prepare(); err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(destination.DataDir, "restore.zip")
	if err := os.WriteFile(archive, source, 0600); err != nil {
		t.Fatal(err)
	}
	if err := backup.Restore(context.Background(), destination, archive); err != nil {
		t.Fatal(err)
	}
	restored, err := db.Open(destination.DBFile)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	var sessions, found int
	if err := restored.QueryRow(`SELECT count(*) FROM sessions`).Scan(&sessions); err != nil || sessions != 0 {
		t.Fatal("restore retained sessions", err)
	}
	if err := restored.QueryRow(`SELECT count(*) FROM message_fts WHERE message_fts MATCH 'keepsake'`).Scan(&found); err != nil || found != 1 {
		t.Fatal("restore lost search", err)
	}
	if data, err := os.ReadFile(filepath.Join(destination.UploadDir, name)); err != nil || string(data) != "keepsake" {
		t.Fatal("restore lost attachment", err)
	}
	if err := backup.Restore(context.Background(), destination, archive); err == nil {
		t.Fatal("restore overwrote existing data")
	}
}

func TestKickRevokesSessionsAndPushSubscriptions(t *testing.T) {
	s, handler := testServer(t)
	admin := setupAdmin(t, s, handler)
	memberID, member := ordinaryUser(t, s)
	if _, err := s.store.Exec(`INSERT INTO push_subscriptions(id,user_id,endpoint,p256dh,auth,created_at) VALUES(?,?,?,?,?,?)`, db.ID(), memberID, "https://push.example.com/member", "key", "auth", db.Now()); err != nil {
		t.Fatal(err)
	}
	path := "/api/admin/users/" + memberID + "/kick"
	if response := request(t, handler, "POST", path, map[string]string{"confirm": "wrong"}, admin); response.Code != 400 {
		t.Fatalf("kick without confirmation: %d", response.Code)
	}
	if response := request(t, handler, "GET", "/api/me", nil, member); response.Code != 200 {
		t.Fatal("unconfirmed kick revoked the session")
	}
	if response := request(t, handler, "POST", path, map[string]string{"confirm": "member"}, admin); response.Code != 200 {
		t.Fatalf("kick: %d %s", response.Code, response.Body.String())
	}
	if response := request(t, handler, "GET", "/api/me", nil, member); response.Code != 401 {
		t.Fatal("kicked session remained active")
	}
	var count int
	if err := s.store.QueryRow(`SELECT count(*) FROM push_subscriptions WHERE user_id=?`, memberID).Scan(&count); err != nil || count != 0 {
		t.Fatalf("kicked member retained push subscriptions: count=%d err=%v", count, err)
	}
}

// An offline host action is recorded without an actor, because no account
// performed it. The audit surface must say so instead of presenting it as the
// owner's own action or as an account that was later erased.
func TestAuditLogNamesOfflineHostActions(t *testing.T) {
	s, handler := testServer(t)
	admin := setupAdmin(t, s, handler)
	me := readJSON[db.User](t, request(t, handler, "GET", "/api/me", nil, admin))
	if _, err := s.store.Exec(`INSERT INTO audit_logs(id,actor_id,action,target_id,created_at) VALUES(?,NULL,'owner.recovered_offline',?,?)`, db.ID(), me.ID, db.Now()); err != nil {
		t.Fatal(err)
	}
	entries := readJSON[[]map[string]string](t, request(t, handler, "GET", "/api/admin/audit-logs", nil, admin))
	recovered, offline := false, false
	for _, entry := range entries {
		switch entry["action"] {
		case "owner.recovered_offline":
			recovered = true
			if entry["actor"] != "host operator (offline)" {
				t.Fatalf("offline recovery was attributed to %q", entry["actor"])
			}
		case "user.created_offline":
			offline = true
		}
	}
	if !recovered {
		t.Fatal("the offline recovery was missing from the audit page")
	}
	if offline {
		t.Fatal("the fixture contains an offline account creation this test did not make")
	}
}
