package api

import (
	"net/http"
	"testing"

	"brrpbrrp/internal/db"
)

func TestPrivateMessageEditDoesNotRevealExistenceToOutsiders(t *testing.T) {
	s, handler, admin, _, author, _, recipient, id := privatePipeline(t)
	_, outsider := memberAccount(t, s, "outside_private_pair")
	created := request(t, handler, "POST", "/api/conversations/"+id+"/messages", messageInput{Content: "private content", IdempotencyKey: "private-edit-oracle"}, author)
	if created.Code != http.StatusCreated {
		t.Fatal(created.Body.String())
	}
	message := readJSON[db.Message](t, created)
	body := map[string]string{"content": "attempted change"}
	for _, cookie := range []*http.Cookie{outsider, admin} {
		hidden := request(t, handler, "PATCH", "/api/messages/"+message.ID, body, cookie)
		missing := request(t, handler, "PATCH", "/api/messages/"+db.ID(), body, cookie)
		if hidden.Code != http.StatusNotFound || hidden.Code != missing.Code || hidden.Body.String() != missing.Body.String() {
			t.Fatalf("private edit existence oracle: existing=%d %s missing=%d %s", hidden.Code, hidden.Body.String(), missing.Code, missing.Body.String())
		}
	}
	if w := request(t, handler, "PATCH", "/api/messages/"+message.ID, body, recipient); w.Code != http.StatusForbidden {
		t.Fatalf("accepted recipient edited another author: %d %s", w.Code, w.Body.String())
	}
	if w := request(t, handler, "PATCH", "/api/messages/"+message.ID, body, author); w.Code != http.StatusOK {
		t.Fatalf("author control failed: %d %s", w.Code, w.Body.String())
	}
}
