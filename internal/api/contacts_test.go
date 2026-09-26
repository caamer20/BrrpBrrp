package api

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"brrpbrrp/internal/db"
)

func TestPrivateContactSurfacesRemainGated(t *testing.T) {
	s, h := testServer(t)
	admin := setupAdmin(t, s, h)
	peer, _ := memberAccount(t, s, "reader")
	for _, path := range []string{"/api/directs", "/api/me/contact-preferences", "/api/me/blocks"} {
		if w := request(t, h, "GET", path, nil, admin); w.Code != 409 {
			t.Fatalf("disabled private surface %s: %d", path, w.Code)
		}
	}
	if w := request(t, h, "POST", "/api/directs", map[string]string{"user_id": peer}, admin); w.Code != 409 {
		t.Fatalf("disabled creation: %d", w.Code)
	}
	var count int
	if err := s.store.QueryRow(`SELECT count(*) FROM direct_pairs`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("disabled direct work committed: %d %v", count, err)
	}
}

func TestContactPreferencesBlocksAndIntroductionsAreAccountScoped(t *testing.T) {
	s, h, admin := directTestServer(t)
	aID, a := memberAccount(t, s, "alice")
	bID, b := memberAccount(t, s, "brenda")
	if got := readJSON[map[string]bool](t, request(t, h, "GET", "/api/me/contact-preferences", nil, b)); !got["allow_requests"] {
		t.Fatal("new accounts do not allow bounded requests")
	}
	if w := request(t, h, "PUT", "/api/me/contact-preferences", map[string]bool{"allow_requests": false}, b); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	if w := request(t, h, "POST", "/api/directs", map[string]string{"user_id": bID}, a); w.Code != 404 {
		t.Fatalf("contact opt-out bypassed: %d %s", w.Code, w.Body.String())
	}
	if w := request(t, h, "PUT", "/api/me/contact-preferences", map[string]any{"allow_requests": true, "user_id": bID}, admin); w.Code != 400 {
		t.Fatalf("admin changed someone else's preference: %d", w.Code)
	}
	if w := request(t, h, "PUT", "/api/me/contact-preferences", map[string]bool{}, b); w.Code != 400 {
		t.Fatalf("missing explicit preference accepted: %d", w.Code)
	}
	if w := request(t, h, "PUT", "/api/me/contact-preferences", map[string]bool{"allow_requests": true}, b); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	if w := request(t, h, "POST", "/api/directs", map[string]string{"user_id": bID, "introduction": strings.Repeat("界", 501)}, a); w.Code != 400 {
		t.Fatalf("oversized introduction accepted: %d", w.Code)
	}
	w := request(t, h, "POST", "/api/directs", map[string]string{"user_id": bID, "introduction": strings.Repeat("界", 500)}, a)
	if w.Code != 201 {
		t.Fatal(w.Body.String())
	}
	d := readJSON[directConversation](t, w)
	if w := request(t, h, "PUT", "/api/me/blocks/"+aID, map[string]bool{}, b); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	if w := request(t, h, "PUT", "/api/me/blocks/"+aID, map[string]bool{}, b); w.Code != 200 {
		t.Fatal("repeated block failed")
	}
	if own := readJSON[[]db.User](t, request(t, h, "GET", "/api/me/blocks", nil, b)); len(own) != 1 || own[0].ID != aID {
		t.Fatalf("own block list: %+v", own)
	}
	if other := readJSON[[]db.User](t, request(t, h, "GET", "/api/me/blocks", nil, admin)); len(other) != 0 {
		t.Fatalf("administrator discovered another account's blocks: %+v", other)
	}
	if w := request(t, h, "DELETE", "/api/me/blocks/"+aID, map[string]bool{}, admin); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	if own := readJSON[[]db.User](t, request(t, h, "GET", "/api/me/blocks", nil, b)); len(own) != 1 {
		t.Fatal("administrator removed another account's block")
	}
	if w := request(t, h, "POST", "/api/directs/"+d.ID+"/accept", map[string]bool{}, b); w.Code != 409 {
		t.Fatalf("blocked pending request accepted: %d", w.Code)
	}
	if w := request(t, h, "POST", "/api/directs", map[string]string{"user_id": bID}, a); w.Code != 404 {
		t.Fatalf("blocked sender contacted recipient: %d", w.Code)
	}
	if w := request(t, h, "POST", "/api/directs", map[string]string{"user_id": aID}, b); w.Code != 404 {
		t.Fatalf("reverse contact bypassed block: %d", w.Code)
	}
}

func TestContactMutationRechecksCurrentSessionAndSendCapability(t *testing.T) {
	s, h, _ := directTestServer(t)
	aID, a := memberAccount(t, s, "alice")
	bID, _ := memberAccount(t, s, "brenda")
	if _, err := s.store.Exec(`DELETE FROM sessions WHERE user_id=?`, aID); err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodPost, "/api/directs", bytes.NewBufferString(`{"user_id":"`+bID+`"}`))
	r.Header.Set("Content-Type", "application/json")
	r = r.WithContext(withSession(r.Context(), hashToken(a.Value)))
	w := httptest.NewRecorder()
	s.createDirectConversation(w, r, db.User{ID: aID})
	if w.Code != 401 {
		t.Fatalf("ended session created request: %d %s", w.Code, w.Body.String())
	}
	_, c := memberAccount(t, s, "charlie")
	if _, err := s.store.Exec(`UPDATE roles SET permissions='["channel.read"]' WHERE is_default=1`); err != nil {
		t.Fatal(err)
	}
	if w := request(t, h, "POST", "/api/directs", map[string]string{"user_id": bID}, c); w.Code != 403 {
		t.Fatalf("revoked send capability created request: %d", w.Code)
	}
	var count int
	if err := s.store.QueryRow(`SELECT count(*) FROM direct_pairs`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("unauthorized requests persisted: %d %v", count, err)
	}
}

func TestRemovingIdentityCancelsPendingIntroductionsOnly(t *testing.T) {
	s, h, _ := directTestServer(t)
	aID, a := memberAccount(t, s, "alice")
	bID, b := memberAccount(t, s, "brenda")
	cID, _ := memberAccount(t, s, "charlie")
	accepted := requestDirect(t, h, a, bID)
	acceptDirect(t, h, b, accepted.ID)
	pending := requestDirect(t, h, a, cID)
	tx, err := s.store.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if err := cancelPendingDirectRequests(t.Context(), tx, aID); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	var state, intro string
	if err := s.store.QueryRow(`SELECT state,introduction FROM direct_requests WHERE conversation_id=?`, pending.ID).Scan(&state, &intro); err != nil || state != "rejected" || intro != "" {
		t.Fatalf("pending intro retained: %q %q %v", state, intro, err)
	}
	if err := s.store.QueryRow(`SELECT state,introduction FROM direct_requests WHERE conversation_id=?`, accepted.ID).Scan(&state, &intro); err != nil || state != "accepted" || intro != "" {
		t.Fatalf("identity cleanup changed accepted state or retained auxiliary authored text: %q %q %v", state, intro, err)
	}
}
