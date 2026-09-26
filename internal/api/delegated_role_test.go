package api

import (
	"encoding/json"
	"net/http"
	"testing"

	"brrpbrrp/internal/db"
)

// onlyCapability assigns an account exactly one management capability, on top of
// the ordinary reading and sending grants, so a test can prove what that one
// capability reaches and what it does not.
func onlyCapability(t *testing.T, s *Server, handler http.Handler, username, capability string) (string, *http.Cookie) {
	t.Helper()
	_ = handler
	roleID := customRole(t, s, username+"-role", 25, `["channel.read","message.send","`+capability+`"]`)
	id, cookie := memberAccount(t, s, username)
	if _, err := s.store.Exec(`DELETE FROM role_assignments WHERE user_id=? AND role_id=?`, id, db.MemberRoleID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.store.Exec(`INSERT INTO role_assignments(user_id,role_id,created_at) VALUES(?,?,?)`, id, roleID, db.Now()); err != nil {
		t.Fatal(err)
	}
	return id, cookie
}

// TestASingleCapabilityRoleReachesExactlyItsOwnSurface covers the R2-06 half that
// source review caught: the broad manager roles worked, but a role holding one
// capability had no usable path — invitations were only inside People, the audit log
// was only behind the settings authority, role assignment needed the full account
// list, and every tab issued requests its capability could not make.
//
// The required behavior is two-sided. Each granted function is reachable with only
// the data it needs, and every function outside the grant is refused rather than
// quietly satisfied by a wider surface.
func TestASingleCapabilityRoleReachesExactlyItsOwnSurface(t *testing.T) {
	s, handler := testServer(t)
	setupAdmin(t, s, handler)
	// Enough accounts for an invitation and an assignment to have a target.
	_, inviter := onlyCapability(t, s, handler, "inviter", "invite.manage")
	var roleManagerID string
	roleManagerID, roleManager := onlyCapability(t, s, handler, "roles", "role.manage")
	_, auditor := onlyCapability(t, s, handler, "auditor", "audit.read")
	// An ordinary member is the assignment target: it sits below every delegated role.
	memberAccount(t, s, "ordinary")

	for _, account := range []struct {
		name       string
		cookie     *http.Cookie
		capability string
	}{
		{"inviter", inviter, "invite.manage"},
		{"roles", roleManager, "role.manage"},
		{"auditor", auditor, "audit.read"},
	} {
		granted := capabilitiesOf(t, handler, account.cookie)
		if !granted[account.capability] {
			t.Fatalf("%s does not hold %s: %v", account.name, account.capability, granted)
		}
		for _, absent := range []string{"settings.manage", "member.manage", "operator.backup", "operator.credential_recovery"} {
			if granted[absent] {
				t.Fatalf("%s holds %s as well: %v", account.name, absent, granted)
			}
		}
	}

	// The invitation surface is reachable with invite.manage alone.
	if w := request(t, handler, "POST", "/api/admin/invitations", map[string]any{"hours": 24, "role_ids": []string{}}, inviter); w.Code != http.StatusCreated {
		t.Fatalf("an invitation-only manager could not create an invitation: %d %s", w.Code, w.Body.String())
	}
	if w := request(t, handler, "GET", "/api/admin/invitations", nil, inviter); w.Code != http.StatusOK {
		t.Fatalf("an invitation-only manager could not list invitations: %d %s", w.Code, w.Body.String())
	}

	// The role assignment workflow is reachable with role.manage alone, through the
	// scoped account list rather than the full one.
	summary := request(t, handler, "GET", "/api/admin/accounts/summary", nil, roleManager)
	if summary.Code != http.StatusOK {
		t.Fatalf("a role manager could not read the account summary: %d %s", summary.Code, summary.Body.String())
	}
	var records []map[string]any
	if err := json.Unmarshal(summary.Body.Bytes(), &records); err != nil {
		t.Fatalf("account summary: %v", err)
	}
	if len(records) == 0 {
		t.Fatal("the account summary listed no accounts")
	}
	// Every field is one the assignment decision uses. Credential, moderation and
	// session state stay behind member.manage.
	allowed := map[string]bool{"id": true, "username": true, "display_name": true, "is_admin": true, "is_owner": true, "role_ids": true, "assignable": true}
	for _, record := range records {
		for key := range record {
			if !allowed[key] {
				t.Fatalf("the account summary returned %q, which assignment does not need", key)
			}
		}
	}
	// A role manager assigns through the role endpoint, and the change is recorded.
	// The target is an ordinary member, below the manager's own position: authority is
	// delegated downward only, and a peer at the same position is refused.
	target := ""
	for _, record := range records {
		if record["username"] == "ordinary" {
			target, _ = record["id"].(string)
			if record["assignable"] != true {
				t.Fatal("an ordinary member was reported as unassignable")
			}
		}
	}
	if target == "" || target == roleManagerID {
		t.Fatal("the account summary did not list the ordinary member")
	}
	if w := request(t, handler, "PUT", "/api/admin/users/"+target+"/roles", map[string]any{"role_ids": []string{db.MemberRoleID}}, roleManager); w.Code != http.StatusOK {
		t.Fatalf("a role manager could not assign a role: %d %s", w.Code, w.Body.String())
	}
	var assigned int
	if err := s.store.QueryRow(`SELECT count(*) FROM role_assignments WHERE user_id=? AND role_id=?`, target, db.MemberRoleID).Scan(&assigned); err != nil {
		t.Fatal(err)
	}
	if assigned != 1 {
		t.Fatalf("the assignment was not stored: %d", assigned)
	}
	var audits int
	if err := s.store.QueryRow(`SELECT count(*) FROM audit_logs WHERE action='user.roles_changed' AND actor_id=?`, roleManagerID).Scan(&audits); err != nil {
		t.Fatal(err)
	}
	if audits != 1 {
		t.Fatalf("role assignment audit records = %d, want 1", audits)
	}

	// The audit-only role reads the log and nothing else.
	if w := request(t, handler, "GET", "/api/admin/audit-logs", nil, auditor); w.Code != http.StatusOK {
		t.Fatalf("an audit-only manager could not read the log: %d %s", w.Code, w.Body.String())
	}
	for _, forbidden := range []struct {
		name, method, path string
		body               any
	}{
		{"the full account list", http.MethodGet, "/api/admin/users", nil},
		{"server settings", http.MethodGet, "/api/admin/settings", nil},
		{"server health", http.MethodGet, "/api/admin/health", nil},
		{"server diagnostics", http.MethodGet, "/api/admin/diagnostics", nil},
		{"a backup archive", http.MethodPost, "/api/admin/backup", map[string]any{"include_uploads": true}},
		{"invitations", http.MethodGet, "/api/admin/invitations", nil},
	} {
		// Each capability is tried against every other surface: what the account was
		// not granted must not be reachable through the surface it was.
		for _, account := range []struct {
			name   string
			cookie *http.Cookie
		}{{"inviter", inviter}, {"roles", roleManager}, {"auditor", auditor}} {
			if forbidden.name == "invitations" && account.name == "inviter" {
				continue
			}
			if forbidden.name == "a backup archive" && account.name != "auditor" {
				// Backup needs a recent strong sign-in as well; the refusal either way.
				continue
			}
			w := request(t, handler, forbidden.method, forbidden.path, forbidden.body, account.cookie)
			if w.Code != http.StatusForbidden {
				t.Fatalf("%s reached %s: %d %s", account.name, forbidden.name, w.Code, w.Body.String())
			}
		}
	}
}
