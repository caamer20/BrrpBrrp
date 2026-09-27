package api

import (
	"net/http"
	"testing"

	"brrpbrrp/internal/db"
)

// R8-01: staging-time upload authority is not publication authority. Reproduced defect:
// a member could stage a destination-bound file, lose message.upload, and still publish
// the file by claiming it in a new send. These tests pin the claim-time capability
// re-check and its replay/restoration boundaries in both conversation scopes.

// messagesInConversation counts by conversation_id because DM messages deliberately
// have no channel projection: a channel_id count cannot see them at all.
func messagesInConversation(t *testing.T, s *Server, conversationID string) int {
	t.Helper()
	var count int
	if err := s.store.QueryRow(`SELECT count(*) FROM messages WHERE conversation_id=?`, conversationID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

func operationReceiptExists(t *testing.T, s *Server, accountID, key string) bool {
	t.Helper()
	var count int
	if err := s.store.QueryRow(`SELECT count(*) FROM message_operations WHERE account_id=? AND idempotency_key=?`, accountID, key).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count != 0
}

func attachmentClaimedMessage(t *testing.T, s *Server, attachmentID string) string {
	t.Helper()
	var messageID string
	if err := s.store.QueryRow(`SELECT COALESCE(message_id,'') FROM attachments WHERE id=?`, attachmentID).Scan(&messageID); err != nil {
		t.Fatal(err)
	}
	return messageID
}

func mentionRows(t *testing.T, s *Server) int {
	t.Helper()
	var count int
	if err := s.store.QueryRow(`SELECT count(*) FROM mentions`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

func setRolePermissions(t *testing.T, h http.Handler, admin *http.Cookie, role db.Role, permissions []string) {
	t.Helper()
	w := request(t, h, "PATCH", "/api/roles/"+role.ID, map[string]any{"name": role.Name, "position": role.Position, "permissions": permissions}, admin)
	if w.Code != http.StatusOK {
		t.Fatalf("role change: %d %s", w.Code, w.Body.String())
	}
}

func TestStagedAttachmentClaimRechecksUploadCapability(t *testing.T) {
	t.Run("channel", func(t *testing.T) {
		s, h := testServer(t)
		admin := setupAdmin(t, s, h)
		senderID, sender := memberAccount(t, s, "claim_member")
		_, peer := memberAccount(t, s, "claim_peer")
		channel := textChannelID(t, s)
		stagedClaimScenario(t, s, h, admin, senderID, sender, peer, channel, "/api/channels/"+channel+"/messages", "claim_peer")
	})
	t.Run("direct", func(t *testing.T) {
		s, h, admin, aid, a, _, b, id := privatePipeline(t)
		stagedClaimScenario(t, s, h, admin, aid, a, b, id, "/api/conversations/"+id+"/messages", "")
	})
}

func stagedClaimScenario(t *testing.T, s *Server, h http.Handler, admin *http.Cookie, senderID string, sender, peer *http.Cookie, conversation, sendPath, mentionName string) {
	t.Helper()
	role := defaultRole(t, h, admin)

	// Stage a destination-bound file while the capability is held.
	staged := readJSON[db.Attachment](t, directUpload(t, h, sender, conversation))

	// Revoke the upload capability. Controls from the reproduction: a fresh
	// upload is refused, and an ordinary text send (no claim) stays permitted.
	setRolePermissions(t, h, admin, role, withoutCapability(role.Permissions, "message.upload"))
	defer setRolePermissions(t, h, admin, role, role.Permissions)
	if w := directUpload(t, h, sender, conversation); w.Code != http.StatusForbidden {
		t.Fatalf("upload after revocation: %d %s", w.Code, w.Body.String())
	}
	if w := request(t, h, "POST", sendPath, messageInput{Content: "text stays permitted", IdempotencyKey: "claim-text-control-key"}, sender); w.Code != http.StatusCreated {
		t.Fatalf("text send after revocation: %d %s", w.Code, w.Body.String())
	}

	content := "publish the staged file"
	if mentionName != "" {
		content += " @" + mentionName
	}
	before := messagesInConversation(t, s, conversation)

	// The reviewed defect: claiming the staged file must now be denied, and the
	// denial must commit nothing — no message, no receipt, no claim, no mention.
	denied := messageInput{Content: content, IdempotencyKey: "claim-denied-key-1", AttachmentIDs: []string{staged.ID}}
	if w := request(t, h, "POST", sendPath, denied, sender); w.Code != http.StatusForbidden {
		t.Fatalf("staged claim survived revocation: %d %s", w.Code, w.Body.String())
	}
	if got := messagesInConversation(t, s, conversation); got != before {
		t.Fatalf("denied send created messages: before=%d after=%d", before, got)
	}
	if operationReceiptExists(t, s, senderID, "claim-denied-key-1") {
		t.Fatal("denied send claimed the operation key")
	}
	if claimed := attachmentClaimedMessage(t, s, staged.ID); claimed != "" {
		t.Fatalf("denied send claimed the staged file into %s", claimed)
	}
	if rows := mentionRows(t, s); rows != 0 {
		t.Fatalf("denied send recorded %d mentions", rows)
	}
	if w := request(t, h, "GET", "/api/uploads/"+staged.ID, nil, peer); w.Code != http.StatusNotFound {
		t.Fatalf("denied staged file readable by the peer: %d", w.Code)
	}

	// The denial never claimed the key: retry while revoked is refused again.
	if w := request(t, h, "POST", sendPath, denied, sender); w.Code != http.StatusForbidden {
		t.Fatalf("denied retry while revoked: %d", w.Code)
	}
	if operationReceiptExists(t, s, senderID, "claim-denied-key-1") {
		t.Fatal("denied retry claimed the operation key")
	}

	// Restore the capability: the previously denied key sends exactly once, its
	// replay returns the same message, and the file is claimed by that message.
	setRolePermissions(t, h, admin, role, role.Permissions)
	restored := request(t, h, "POST", sendPath, denied, sender)
	if restored.Code != http.StatusCreated {
		t.Fatalf("restored send: %d %s", restored.Code, restored.Body.String())
	}
	created := readJSON[db.Message](t, restored)
	if replay := request(t, h, "POST", sendPath, denied, sender); replay.Code != http.StatusOK || readJSON[db.Message](t, replay).ID != created.ID {
		t.Fatalf("replay after restoration: %d %s", replay.Code, replay.Body.String())
	}
	if got := messagesInConversation(t, s, conversation); got != before+1 {
		t.Fatalf("restored send and replay created %d messages, want %d", got, before+1)
	}
	if claimed := attachmentClaimedMessage(t, s, staged.ID); claimed != created.ID {
		t.Fatalf("restored send claimed file into %q, want %q", claimed, created.ID)
	}
	if mentionName != "" && mentionRows(t, s) != 1 {
		t.Fatalf("restored sending mention content recorded %d mentions, want 1", mentionRows(t, s))
	}
	if w := request(t, h, "GET", "/api/uploads/"+staged.ID, nil, peer); w.Code != http.StatusOK {
		t.Fatalf("published authorized file unreadable by the peer: %d", w.Code)
	}
}

// The claim-time guard must not rewrite history: an already acknowledged send
// replays from its receipt while read access remains, even after the author has
// lost the upload capability.
func TestAcknowledgedAttachmentSendReplaysAfterUploadRevocation(t *testing.T) {
	t.Run("channel", func(t *testing.T) {
		s, h := testServer(t)
		admin := setupAdmin(t, s, h)
		_, sender := memberAccount(t, s, "replay_member")
		_, peer := memberAccount(t, s, "replay_peer")
		channel := textChannelID(t, s)
		acknowledgedReplayScenario(t, s, h, admin, sender, peer, channel, "/api/channels/"+channel+"/messages")
	})
	t.Run("direct", func(t *testing.T) {
		s, h, admin, _, a, _, b, id := privatePipeline(t)
		acknowledgedReplayScenario(t, s, h, admin, a, b, id, "/api/conversations/"+id+"/messages")
	})
}

func acknowledgedReplayScenario(t *testing.T, s *Server, h http.Handler, admin *http.Cookie, sender, peer *http.Cookie, conversation, sendPath string) {
	t.Helper()
	role := defaultRole(t, h, admin)
	staged := readJSON[db.Attachment](t, directUpload(t, h, sender, conversation))
	// The peer's control file is staged before the revocation as well, because a
	// revoked account can no longer stage new uploads at all.
	other := readJSON[db.Attachment](t, directUpload(t, h, peer, conversation))
	send := messageInput{Content: "authorized attachment", IdempotencyKey: "claim-acknowledged-key", AttachmentIDs: []string{staged.ID}}
	created := request(t, h, "POST", sendPath, send, sender)
	if created.Code != http.StatusCreated {
		t.Fatalf("authorized send: %d %s", created.Code, created.Body.String())
	}
	message := readJSON[db.Message](t, created)

	setRolePermissions(t, h, admin, role, withoutCapability(role.Permissions, "message.upload"))

	// The acknowledged replay is answered from the receipt, before the new-claim
	// guard; it returns the message without new side effects.
	replay := request(t, h, "POST", sendPath, send, sender)
	if replay.Code != http.StatusOK {
		t.Fatalf("acknowledged replay after revocation: %d %s", replay.Code, replay.Body.String())
	}
	if readJSON[db.Message](t, replay).ID != message.ID {
		t.Fatal("acknowledged replay returned a different message")
	}
	if got := messagesInConversation(t, s, conversation); got != 1 {
		t.Fatalf("acknowledged replay created %d messages, want 1", got)
	}
	// The previously published authorized file stays readable while conversation
	// read access remains; upload revocation is not retroactive unpublishing.
	if w := request(t, h, "GET", "/api/uploads/"+staged.ID, nil, peer); w.Code != http.StatusOK {
		t.Fatalf("published file unreadable after revocation: %d", w.Code)
	}
	// A new send claiming a staged file is refused: the guard is real, only the
	// replay bypasses it.
	if w := request(t, h, "POST", sendPath, messageInput{Content: "peer claim without capability", IdempotencyKey: "claim-peer-denied-key", AttachmentIDs: []string{other.ID}}, peer); w.Code != http.StatusForbidden {
		t.Fatalf("new claim after revocation: %d", w.Code)
	}
}

// Deleting the message must not make its key reusable, and that answer must also
// survive the author losing the upload capability: the receipt guard runs first.
func TestDeletedAttachmentSendReplayStaysNonRecreatingAfterUploadRevocation(t *testing.T) {
	t.Run("channel", func(t *testing.T) {
		s, h := testServer(t)
		admin := setupAdmin(t, s, h)
		_, sender := memberAccount(t, s, "spent_member")
		channel := textChannelID(t, s)
		deletedReplayScenario(t, s, h, admin, sender, channel, "/api/channels/"+channel+"/messages")
	})
	t.Run("direct", func(t *testing.T) {
		s, h, admin, _, a, _, _, id := privatePipeline(t)
		deletedReplayScenario(t, s, h, admin, a, id, "/api/conversations/"+id+"/messages")
	})
}

func deletedReplayScenario(t *testing.T, s *Server, h http.Handler, admin, sender *http.Cookie, conversation, sendPath string) {
	t.Helper()
	role := defaultRole(t, h, admin)
	staged := readJSON[db.Attachment](t, directUpload(t, h, sender, conversation))
	send := messageInput{Content: "soon deleted", IdempotencyKey: "claim-spent-key", AttachmentIDs: []string{staged.ID}}
	created := request(t, h, "POST", sendPath, send, sender)
	if created.Code != http.StatusCreated {
		t.Fatalf("send: %d %s", created.Code, created.Body.String())
	}
	message := readJSON[db.Message](t, created)
	if w := request(t, h, "DELETE", "/api/messages/"+message.ID, map[string]string{"confirm": message.ID}, sender); w.Code != http.StatusOK {
		t.Fatalf("delete: %d %s", w.Code, w.Body.String())
	}
	setRolePermissions(t, h, admin, role, withoutCapability(role.Permissions, "message.upload"))
	if w := request(t, h, "POST", sendPath, send, sender); w.Code != http.StatusConflict {
		t.Fatalf("replay of deleted send after revocation: %d %s", w.Code, w.Body.String())
	}
	if got := messagesInConversation(t, s, conversation); got != 0 {
		t.Fatalf("spent-key replay recreated %d messages", got)
	}
}

// The claim-time guard does not loosen destination binding: with the capability
// held, an unscoped (community) staged file still cannot be claimed into a DM,
// and a DM-staged file cannot be claimed into a channel.
func TestAttachmentClaimKeepsDestinationBindingUnderCapabilityCheck(t *testing.T) {
	s, h, _, _, a, _, _, id := privatePipeline(t)
	channel := textChannelID(t, s)
	unscoped := readJSON[db.Attachment](t, directUpload(t, h, a, ""))
	if w := request(t, h, "POST", "/api/conversations/"+id+"/messages", messageInput{Content: "claim community bytes privately", IdempotencyKey: "claim-unscoped-into-dm", AttachmentIDs: []string{unscoped.ID}}, a); w.Code != http.StatusBadRequest {
		t.Fatalf("unscoped upload claimed into DM: %d %s", w.Code, w.Body.String())
	}
	private := readJSON[db.Attachment](t, directUpload(t, h, a, id))
	if w := request(t, h, "POST", "/api/channels/"+channel+"/messages", messageInput{Content: "claim private bytes publicly", IdempotencyKey: "claim-dm-into-channel", AttachmentIDs: []string{private.ID}}, a); w.Code != http.StatusBadRequest {
		t.Fatalf("DM-staged upload claimed into a channel: %d %s", w.Code, w.Body.String())
	}
	if got := messagesInConversation(t, s, id); got != 0 {
		t.Fatalf("refused claims created %d DM messages", got)
	}
	if attachmentClaimedMessage(t, s, unscoped.ID) != "" || attachmentClaimedMessage(t, s, private.ID) != "" {
		t.Fatal("refused cross-destination claim still claimed a file")
	}
}
