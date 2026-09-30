package api

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"brrpbrrp/internal/db"
)

// V14-02. A member's export is the server's own copy of their history, and a copy the
// server holds is a copy the server is responsible for.
//
// Two defects, each reproduced before it was fixed. Both were the same mistake — the
// delivery path asked "may this account export?" instead of "may this account read
// what the file contains?" — in the two places it can be asked.

// A built export stops being deliverable when the access that produced it is
// withdrawn.
//
// The review's shape: a member's group export, built while access existed, then the
// membership removed through the real endpoint. History returned 404 and the download
// returned 200 with the removed group's text still in the body.
func TestAnExportStopsDeliveringWhatTheAccountCanNoLongerRead(t *testing.T) {
	t.Setenv("BRRP_ENABLE_PRIVATE_CONVERSATIONS", "1")
	s, handler := testServer(t)
	setupAdmin(t, s, handler)
	_, alice := namedUser(t, s, "export_alice")
	aliceID := memberIDOf(t, s, "export_alice")
	_, _ = namedUser(t, s, "export_bob")
	bobID := memberIDOf(t, s, "export_bob")

	// A group with two accepted members and a message, so it has a history worth
	// exporting and the removal below is a real authority change rather than a guess.
	group := readJSON[map[string]any](t, request(t, handler, "POST", "/api/groups",
		map[string]any{"name": "the export group"}, alice))
	groupID := group["id"].(string)
	invite(t, handler, alice, groupID, bobID)
	acceptGroupInvitationFor(t, s, handler, bobID, groupID)
	if w := request(t, handler, "POST", "/api/channels/"+groupID+"/messages",
		map[string]any{"content": "group text that will be revoked", "idempotency_key": "export-group-1"},
		alice); w.Code != 201 {
		t.Fatalf("the group message was refused: %d %s", w.Code, w.Body.String())
	}

	_ = exportFor(t, s, handler, alice, "export-key-withdraw")
	exportID := latestExportID(t, s, aliceID)
	if served := heldExportText(t, s, exportID); !strings.Contains(served, "group text that will be revoked") {
		t.Fatal("the export does not carry the group, so the withdrawal below would prove nothing")
	}

	// Ownership moves so the *other* member can remove her — the real product path, not
	// a database edit. A fixture that wrote the membership away would bypass the very
	// route the export check depends on.
	if w := request(t, handler, "POST", "/api/groups/"+groupID+"/transfer",
		map[string]any{"new_owner_id": bobID}, alice); w.Code != 200 {
		t.Fatalf("transfer ownership: %d %s", w.Code, w.Body.String())
	}
	bobSession := sessionFor(t, s, bobID)
	if w := request(t, handler, "POST", "/api/groups/"+groupID+"/members/"+aliceID+"/remove",
		map[string]any{}, bobSession); w.Code >= 300 {
		t.Fatalf("remove the member: %d %s", w.Code, w.Body.String())
	}

	// History first. The export question only means something if history is actually
	// refused, and a test that skipped this would pass with a working export check and
	// a broken membership removal.
	if w := request(t, handler, "GET", "/api/conversations/"+groupID+"/messages", nil, alice); w.Code != 404 {
		t.Fatalf("the removed member still read group history: %d", w.Code)
	}

	markPrivileged(t, s, alice)
	download := request(t, handler, "GET", "/api/me/export/"+exportID, nil, alice)
	if download.Code != http.StatusForbidden {
		t.Fatalf("the download returned %d, want 403: %s", download.Code, download.Body.String())
	}
	if strings.Contains(download.Body.String(), "group text that will be revoked") {
		t.Fatal("THE REFUSAL ITSELF CARRIED THE REMOVED GROUP'S TEXT")
	}
}

// The permitted control: an account whose access has not changed still receives its
// own file. A fix that simply stopped delivering exports would pass the refusal above.
func TestAnExportIsStillDeliveredWhenAccessIsUnchanged(t *testing.T) {
	t.Setenv("BRRP_ENABLE_PRIVATE_CONVERSATIONS", "1")
	s, handler := testServer(t)
	setupAdmin(t, s, handler)
	_, alice := namedUser(t, s, "export_keep_alice")
	aliceID := memberIDOf(t, s, "export_keep_alice")
	_, _ = namedUser(t, s, "export_keep_bob")
	bobID := memberIDOf(t, s, "export_keep_bob")

	direct := readJSON[map[string]any](t, request(t, handler, "POST", "/api/directs",
		map[string]any{"user_id": bobID}, alice))
	request(t, handler, "POST", "/api/directs/"+direct["id"].(string)+"/accept", map[string]any{},
		sessionFor(t, s, bobID))
	request(t, handler, "POST", "/api/channels/"+direct["id"].(string)+"/messages",
		map[string]any{"content": "still mine to read", "idempotency_key": "export-keep-1"}, alice)

	_ = exportFor(t, s, handler, alice, "export-key-keep")
	markPrivileged(t, s, alice)
	download := request(t, handler, "GET", "/api/me/export/"+latestExportID(t, s, aliceID), nil, alice)
	if download.Code != 200 {
		t.Fatalf("an unchanged account could not download its own export: %d %s", download.Code, download.Body.String())
	}
	if !strings.Contains(download.Body.String(), "still mine to read") {
		t.Fatal("the delivered export is missing content the account can read")
	}
}

// A build on a server with private conversations disabled does not contain them.
//
// The route's gate covers the request. This covers the *build*, which runs after the
// response and could still find private rows, because the selection queried the
// conversation table directly rather than asking the question the interface asks.
func TestAnExportContainsNoPrivateConversationWhenTheFeatureIsOff(t *testing.T) {
	s, handler := testServer(t)
	setupAdmin(t, s, handler)
	s.config.PrivateConversations = false
	_, alice := namedUser(t, s, "export_off_alice")
	aliceID := memberIDOf(t, s, "export_off_alice")
	_, _ = namedUser(t, s, "export_off_bob")
	bobID := memberIDOf(t, s, "export_off_bob")

	// A real accepted conversation and a real message, seeded through the model because
	// with the feature off the route is refused — which is the point: the build must not
	// find what the interface cannot show.
	seedAcceptedConversation(t, s, aliceID, bobID, "a private line with the feature off", "export-off-1")

	_ = exportFor(t, s, handler, alice, "export-key-feature-off")
	if served := heldExportText(t, s, latestExportID(t, s, aliceID)); strings.Contains(served, "a private line with the feature off") {
		t.Fatal("AN EXPORT ON A SERVER WITH PRIVATE CONVERSATIONS OFF CONTAINED A PRIVATE CONVERSATION")
	}
}

// An export built before the scope column existed is refused rather than served. An
// export whose scope cannot be established is one whose authorization cannot be, and
// "we cannot tell what this contains" is not a reason to hand it over.
func TestAnExportWithNoRecordedScopeIsRefusedRatherThanServed(t *testing.T) {
	t.Setenv("BRRP_ENABLE_PRIVATE_CONVERSATIONS", "1")
	s, handler := testServer(t)
	setupAdmin(t, s, handler)
	_, alice := namedUser(t, s, "export_noscope_alice")
	aliceID := memberIDOf(t, s, "export_noscope_alice")

	_ = exportFor(t, s, handler, alice, "export-key-noscope")
	exportID := latestExportID(t, s, aliceID)
	if _, err := s.store.Exec(`UPDATE data_exports SET scope='' WHERE id=?`, exportID); err != nil {
		t.Fatal(err)
	}
	markPrivileged(t, s, alice)
	download := request(t, handler, "GET", "/api/me/export/"+exportID, nil, alice)
	if download.Code != http.StatusGone {
		t.Fatalf("an export with no recorded scope was served with %d, want 410: %s", download.Code, download.Body.String())
	}
	if strings.Contains(download.Body.String(), `"conversations"`) {
		t.Fatal("the refusal body carried export content")
	}
}

// acceptGroupInvitationFor finds the pending invitation and accepts it through the
// supported route. Seeding the membership row directly would establish state without
// establishing that the route works.
func acceptGroupInvitationFor(t *testing.T, s *Server, h http.Handler, userID, conversationID string) {
	t.Helper()
	var invitationID string
	if err := s.store.QueryRowContext(t.Context(),
		`SELECT id FROM group_invitations WHERE invitee_id=? AND conversation_id=?`, userID, conversationID).Scan(&invitationID); err != nil {
		t.Fatal(err)
	}
	if w := request(t, h, "POST", "/api/group-invitations/"+invitationID+"/accept", map[string]any{},
		sessionFor(t, s, userID)); w.Code >= 300 {
		t.Fatalf("accept the invitation: %d %s", w.Code, w.Body.String())
	}
}

// seedAcceptedConversation writes one accepted private conversation with a message,
// through the model rather than a route, for fixtures where the route under test is
// deliberately unavailable.
func seedAcceptedConversation(t *testing.T, s *Server, firstID, secondID, content, key string) string {
	t.Helper()
	conversationID := db.ID()
	now := db.Now()
	if _, err := s.store.Exec(`INSERT INTO conversations(id,kind,state,created_at,updated_at) VALUES(?,'direct','active',?,?)`,
		conversationID, now, now); err != nil {
		t.Fatal(err)
	}
	for _, member := range []string{firstID, secondID} {
		if _, err := s.store.Exec(`INSERT INTO conversation_members(conversation_id,user_id,state,joined_at,updated_at) VALUES(?,?,'accepted',?,?)`,
			conversationID, member, now, now); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.store.Exec(`INSERT INTO messages(id,conversation_id,user_id,content,idempotency_key,created_at) VALUES(?,?,?,?,?,?)`,
		db.ID(), conversationID, firstID, content, key, now); err != nil {
		t.Fatal(err)
	}
	return conversationID
}

// heldExportText reads the file the server is holding — the thing actually at issue.
func heldExportText(t *testing.T, s *Server, id string) string {
	t.Helper()
	data, err := os.ReadFile(s.exportPath(id))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func latestExportID(t *testing.T, s *Server, userID string) string {
	t.Helper()
	var id string
	if err := s.store.QueryRowContext(t.Context(),
		`SELECT id FROM data_exports WHERE user_id=? ORDER BY created_at DESC LIMIT 1`, userID).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

// Erasing an account removes its export files.
//
// The row cascades with the account, so a cleanup written afterwards has nothing to
// name: an erased member's export would sit in the data directory with no record of
// whose it was and no sweep that could ever reach it, because the sweep works from the
// table. This is a copy of somebody's history that outlived the account by accident.
func TestErasingAnAccountRemovesItsExportFiles(t *testing.T) {
	t.Setenv("BRRP_ENABLE_PRIVATE_CONVERSATIONS", "1")
	s, handler := testServer(t)
	admin := setupAdmin(t, s, handler)
	_, alice := namedUser(t, s, "export_erased_alice")
	aliceID := memberIDOf(t, s, "export_erased_alice")

	_ = exportFor(t, s, handler, alice, "export-key-erased")
	exportID := latestExportID(t, s, aliceID)
	if _, err := os.Stat(s.exportPath(exportID)); err != nil {
		t.Fatalf("the export file was never written: %v", err)
	}
	// The owned account cannot be erased; erase the member's.
	if w := request(t, handler, "DELETE", "/api/admin/users/"+aliceID, map[string]any{"confirm": "export_erased_alice"}, admin); w.Code != 200 {
		t.Fatalf("erase the account: %d %s", w.Code, w.Body.String())
	}
	if _, err := os.Stat(s.exportPath(exportID)); !os.IsNotExist(err) {
		t.Fatalf("AN ERASED ACCOUNT'S EXPORT FILE SURVIVED IN THE DATA DIRECTORY: %v", err)
	}
	var rows int
	if err := s.store.QueryRowContext(t.Context(), `SELECT count(*) FROM data_exports WHERE id=?`, exportID).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 0 {
		t.Fatal("the export row survived the account it belonged to")
	}
}

// Finished copies are bounded, and the bound is the account's and the server's.
//
// A seven-day lifetime is a statement about how long a copy may exist, not about how
// many may exist. An account that ran one export a minute would otherwise accumulate
// an unbounded number of files, and every one of them is a copy of somebody's history
// the server is holding.
func TestFinishedExportsAreBoundedPerAccount(t *testing.T) {
	t.Setenv("BRRP_ENABLE_PRIVATE_CONVERSATIONS", "1")
	s, handler := testServer(t)
	setupAdmin(t, s, handler)
	_, alice := namedUser(t, s, "export_bound_alice")

	// Six exports, one more than the per-account allowance.
	for i := 0; i < maxReadyExportsPerAccount+3; i++ {
		_ = exportFor(t, s, handler, alice, "export-bound-key-"+string(rune('a'+i)))
	}
	var ready int
	if err := s.store.QueryRowContext(t.Context(),
		`SELECT count(*) FROM data_exports WHERE state='ready'`).Scan(&ready); err != nil {
		t.Fatal(err)
	}
	if ready > maxReadyExportsPerAccount {
		t.Fatalf("an account holds %d finished exports, bound is %d", ready, maxReadyExportsPerAccount)
	}
	// And the *files* follow the rows. A bound on the table with the files left behind
	// is a bound on nothing.
	entries, err := os.ReadDir(filepath.Join(s.config.DataDir, "exports"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) > maxReadyExportsPerAccount {
		t.Fatalf("the data directory holds %d export files for one account, bound is %d", len(entries), maxReadyExportsPerAccount)
	}
}

// The reclaim takes the oldest, so an account is not left holding only its most
// recent export while older ones are kept for some other reason.
func TestTheReclaimTakesTheOldestExports(t *testing.T) {
	t.Setenv("BRRP_ENABLE_PRIVATE_CONVERSATIONS", "1")
	s, handler := testServer(t)
	setupAdmin(t, s, handler)
	_, alice := namedUser(t, s, "export_oldest_alice")

	const first = "export-oldest-key-aaa"
	_ = exportFor(t, s, handler, alice, first)
	firstID := latestExportID(t, s, memberIDOf(t, s, "export_oldest_alice"))
	for i := 0; i < maxReadyExportsPerAccount+1; i++ {
		_ = exportFor(t, s, handler, alice, "export-oldest-key-"+string(rune('b'+i)))
	}
	var exists int
	if err := s.store.QueryRowContext(t.Context(),
		`SELECT count(*) FROM data_exports WHERE id=?`, firstID).Scan(&exists); err != nil {
		t.Fatal(err)
	}
	if exists != 0 {
		t.Fatal("the reclaim kept the oldest export and dropped a newer one")
	}
}

// An export file with no row behind it is swept, on the same grace period as an
// unclaimed upload. Without this, a crash between reclaiming a row and unlinking its
// file leaves a copy of somebody's history that nothing can ever reach again.
func TestAnOrphanedExportFileIsSwept(t *testing.T) {
	t.Setenv("BRRP_ENABLE_PRIVATE_CONVERSATIONS", "1")
	s, handler := testServer(t)
	setupAdmin(t, s, handler)

	directory := filepath.Join(s.config.DataDir, "exports")
	if err := os.MkdirAll(directory, 0700); err != nil {
		t.Fatal(err)
	}
	orphan := filepath.Join(directory, "orphan-export.json")
	if err := os.WriteFile(orphan, []byte(`{"conversations":[]}`), 0600); err != nil {
		t.Fatal(err)
	}
	// Aged past the grace, as a file abandoned by a crash a day ago would be.
	old := time.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(orphan, old, old); err != nil {
		t.Fatal(err)
	}
	removed, err := s.sweepOrphanExports(24 * time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if removed != 1 {
		t.Fatalf("the sweep removed %d orphaned exports, want 1", removed)
	}
	if _, err := os.Stat(orphan); !os.IsNotExist(err) {
		t.Fatal("THE ORPHANED EXPORT FILE SURVIVED THE SWEEP")
	}
	_ = handler
}

// A file younger than the grace is left alone: it may belong to an export whose row
// committed moments ago and whose file is still being written, and unlinking it would
// break a build in progress.
func TestAFreshExportFileIsNotSwept(t *testing.T) {
	t.Setenv("BRRP_ENABLE_PRIVATE_CONVERSATIONS", "1")
	s, handler := testServer(t)
	setupAdmin(t, s, handler)
	directory := filepath.Join(s.config.DataDir, "exports")
	if err := os.MkdirAll(directory, 0700); err != nil {
		t.Fatal(err)
	}
	fresh := filepath.Join(directory, "fresh-export.json")
	if err := os.WriteFile(fresh, []byte(`{}`), 0600); err != nil {
		t.Fatal(err)
	}
	if removed, err := s.sweepOrphanExports(24 * time.Hour); err != nil {
		t.Fatal(err)
	} else if removed != 0 {
		t.Fatal("the sweep removed a file young enough to be a build in progress")
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Fatalf("a fresh export file was removed: %v", err)
	}
	_ = handler
}
