package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"brrpbrrp/internal/db"
)

func directTestServer(t *testing.T) (*Server, http.Handler, *http.Cookie) {
	t.Helper()
	t.Setenv("BRRP_ENABLE_PRIVATE_CONVERSATIONS", "true")
	s, h := testServer(t)
	return s, h, setupAdmin(t, s, h)
}

func requestDirect(t *testing.T, h http.Handler, actor *http.Cookie, peer string) directConversation {
	t.Helper()
	w := request(t, h, "POST", "/api/directs", map[string]string{"user_id": peer, "introduction": "Hello, may we chat?"}, actor)
	if w.Code != 201 && w.Code != 200 {
		t.Fatalf("direct request: %d %s", w.Code, w.Body.String())
	}
	return readJSON[directConversation](t, w)
}

func acceptDirect(t *testing.T, h http.Handler, recipient *http.Cookie, id string) directConversation {
	t.Helper()
	w := request(t, h, "POST", "/api/directs/"+id+"/accept", map[string]bool{}, recipient)
	if w.Code != 200 {
		t.Fatalf("accept request: %d %s", w.Code, w.Body.String())
	}
	return readJSON[directConversation](t, w)
}

type directTestRequest struct {
	method, path string
	body         any
	cookie       *http.Cookie
}

func concurrentDirectRequests(t *testing.T, h http.Handler, inputs ...directTestRequest) []*httptest.ResponseRecorder {
	t.Helper()
	start := make(chan struct{})
	results := make([]*httptest.ResponseRecorder, len(inputs))
	var work sync.WaitGroup
	for i, input := range inputs {
		body, err := json.Marshal(input.body)
		if err != nil {
			t.Fatal(err)
		}
		r := httptest.NewRequest(input.method, input.path, bytes.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Origin", "https://localhost:8080")
		r.AddCookie(input.cookie)
		work.Add(1)
		go func() { defer work.Done(); <-start; w := httptest.NewRecorder(); h.ServeHTTP(w, r); results[i] = w }()
	}
	close(start)
	work.Wait()
	return results
}

func TestDirectConsentIsCanonicalExplicitAndNeverAdministrative(t *testing.T) {
	s, h, admin := directTestServer(t)
	aID, a := memberAccount(t, s, "alice")
	bID, b := memberAccount(t, s, "brenda")
	results := concurrentDirectRequests(t, h, directTestRequest{"POST", "/api/directs", map[string]string{"user_id": bID, "introduction": "From Alice"}, a}, directTestRequest{"POST", "/api/directs", map[string]string{"user_id": aID, "introduction": "From Brenda"}, b})
	created := 0
	var id string
	for _, w := range results {
		if w.Code == 201 {
			created++
		} else if w.Code != 200 {
			t.Fatalf("crossed requests: %d %s", w.Code, w.Body.String())
		}
		d := readJSON[directConversation](t, w)
		if id != "" && id != d.ID {
			t.Fatal("crossed requests created different pairs")
		}
		id = d.ID
		if d.CanRead || d.CanWrite || d.RequestState != "pending" {
			t.Fatalf("crossed requests implicitly granted consent: %+v", d)
		}
	}
	if created != 1 {
		t.Fatalf("created %d canonical pairs", created)
	}
	var requester, recipient string
	if err := s.store.QueryRow(`SELECT requester_id,recipient_id FROM direct_requests WHERE conversation_id=?`, id).Scan(&requester, &recipient); err != nil {
		t.Fatal(err)
	}
	sender, receiver := a, b
	if requester == bID {
		sender, receiver = b, a
	}
	if w := request(t, h, "POST", "/api/directs/"+id+"/accept", map[string]bool{}, sender); w.Code != 403 {
		t.Fatalf("requester accepted itself: %d %s", w.Code, w.Body.String())
	}
	if w := request(t, h, "POST", "/api/directs/"+id+"/accept", map[string]bool{}, admin); w.Code != 404 {
		t.Fatalf("administrator accessed an unrelated request: %d %s", w.Code, w.Body.String())
	}
	if w := request(t, h, "GET", "/api/directs", nil, admin); w.Code != 200 || strings.Contains(w.Body.String(), id) {
		t.Fatalf("administrator discovered a pair: %d %s", w.Code, w.Body.String())
	}
	for _, cookie := range []*http.Cookie{a, b} {
		if w := request(t, h, "GET", "/api/conversations/"+id+"/messages", nil, cookie); w.Code != 404 {
			t.Fatalf("pending request granted history: %d %s", w.Code, w.Body.String())
		}
	}
	d := acceptDirect(t, h, receiver, id)
	if !d.CanRead || !d.CanWrite || d.RequestState != "accepted" || d.State != "active" {
		t.Fatalf("accepted pair did not become usable: %+v", d)
	}
	if again := acceptDirect(t, h, receiver, id); again.ID != id {
		t.Fatal("duplicate accept changed pair")
	}
	if w := request(t, h, "POST", "/api/directs/"+id+"/reject", map[string]bool{}, receiver); w.Code != 409 {
		t.Fatalf("reject erased accepted history: %d %s", w.Code, w.Body.String())
	}
	set := request(t, h, "PUT", "/api/directs/"+id+"/visibility", map[string]bool{"hidden": true}, sender)
	if set.Code != 200 || !readJSON[directConversation](t, set).Hidden {
		t.Fatal("could not hide own conversation")
	}
	others := readJSON[[]directConversation](t, request(t, h, "GET", "/api/directs", nil, receiver))
	if len(others) != 1 || others[0].Hidden {
		t.Fatal("hiding changed another account's preference")
	}
	peer := recipient
	if requester != aID {
		peer = aID
	}
	if reopened := requestDirect(t, h, sender, peer); reopened.ID != id || reopened.Hidden || !reopened.CanRead {
		t.Fatalf("reopening created new work: %+v", reopened)
	}
}

func TestDirectRequestsHaveDurableLimitsAndRejectionCooldown(t *testing.T) {
	s, h, _ := directTestServer(t)
	senderID, sender := memberAccount(t, s, "sender")
	peers := []string{}
	cookies := []*http.Cookie{}
	ids := []string{}
	for i := 0; i < 11; i++ {
		id, cookie := memberAccount(t, s, fmt.Sprintf("recipient%d", i))
		peers = append(peers, id)
		cookies = append(cookies, cookie)
	}
	for i := 0; i < 5; i++ {
		ids = append(ids, requestDirect(t, h, sender, peers[i]).ID)
	}
	if w := request(t, h, "POST", "/api/directs", map[string]string{"user_id": peers[5]}, sender); w.Code != 429 {
		t.Fatalf("pending limit bypassed: %d %s", w.Code, w.Body.String())
	}
	if d := requestDirect(t, h, sender, peers[0]); d.ID != ids[0] {
		t.Fatal("duplicate consumed another pending request")
	}
	// A real reopen removes every in-memory limiter as a possible explanation.
	s.Close()
	if err := s.store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err := db.Open(s.config.DBFile)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	restarted, err := New(s.config, store, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(restarted.Close)
	h = restarted.Handler()
	if w := request(t, h, "POST", "/api/directs", map[string]string{"user_id": peers[5]}, sender); w.Code != 429 {
		t.Fatalf("restart reset pending limit: %d %s", w.Code, w.Body.String())
	}
	for i, id := range ids {
		if w := request(t, h, "POST", "/api/directs/"+id+"/reject", map[string]bool{}, cookies[i]); w.Code != 200 {
			t.Fatal(w.Body.String())
		}
	}
	if w := request(t, h, "POST", "/api/directs", map[string]string{"user_id": peers[0]}, sender); w.Code != 429 || w.Header().Get("Retry-After") == "" {
		t.Fatalf("rejection cooldown bypassed: %d %s", w.Code, w.Body.String())
	}
	if w := request(t, h, "POST", "/api/directs", map[string]string{"user_id": senderID}, cookies[0]); w.Code != 429 {
		t.Fatalf("reversing the pair bypassed cooldown: %d %s", w.Code, w.Body.String())
	}
	for i := 5; i < 10; i++ {
		d := requestDirect(t, h, sender, peers[i])
		if w := request(t, h, "POST", "/api/directs/"+d.ID+"/reject", map[string]bool{}, cookies[i]); w.Code != 200 {
			t.Fatal(w.Body.String())
		}
	}
	if w := request(t, h, "POST", "/api/directs", map[string]string{"user_id": peers[10]}, sender); w.Code != 429 {
		t.Fatalf("rejections reset daily limit: %d %s", w.Code, w.Body.String())
	}
	old := db.Timestamp(time.Now().Add(-25 * time.Hour))
	if _, err := store.Exec(`UPDATE direct_requests SET requested_at=?,retry_after=? WHERE requester_id=?`, old, old, senderID); err != nil {
		t.Fatal(err)
	}
	d := requestDirect(t, h, sender, peers[0])
	if d.ID != ids[0] || d.RequestState != "pending" {
		t.Fatalf("expired cooldown did not reuse canonical pair: %+v", d)
	}
}

func TestDirectIncomingInboxIsBounded(t *testing.T) {
	s, h, _ := directTestServer(t)
	target, _ := memberAccount(t, s, "recipient")
	for i := 0; i < 21; i++ {
		_, sender := memberAccount(t, s, fmt.Sprintf("sender%d", i))
		w := request(t, h, "POST", "/api/directs", map[string]string{"user_id": target}, sender)
		want := 201
		if i == 20 {
			want = 429
		}
		if w.Code != want {
			t.Fatalf("incoming request %d: %d %s", i, w.Code, w.Body.String())
		}
	}
	var count int
	if err := s.store.QueryRow(`SELECT count(*) FROM direct_requests WHERE recipient_id=? AND state='pending'`, target).Scan(&count); err != nil || count != 20 {
		t.Fatalf("incoming cap did not hold: %d %v", count, err)
	}
}

func TestDirectHistorySurvivesBlockingAndPeerErasure(t *testing.T) {
	s, h, admin := directTestServer(t)
	aID, a := memberAccount(t, s, "alice")
	bID, b := memberAccount(t, s, "brenda")
	d := requestDirect(t, h, a, bID)
	acceptDirect(t, h, b, d.ID)
	w := request(t, h, "POST", "/api/conversations/"+d.ID+"/messages", messageInput{Content: "retained history", IdempotencyKey: "direct-history-1"}, b)
	if w.Code != 201 {
		t.Fatalf("accepted send: %d %s", w.Code, w.Body.String())
	}
	if w := request(t, h, "PUT", "/api/me/blocks/"+bID, map[string]bool{}, a); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	for _, cookie := range []*http.Cookie{a, b} {
		ds := readJSON[[]directConversation](t, request(t, h, "GET", "/api/directs", nil, cookie))
		if len(ds) != 1 || !ds[0].CanRead || ds[0].CanWrite || !ds[0].Blocked {
			t.Fatalf("block history contract: %+v", ds)
		}
	}
	if w := request(t, h, "GET", "/api/conversations/"+d.ID+"/messages", nil, admin); w.Code != 404 {
		t.Fatalf("administrator read direct history: %d %s", w.Code, w.Body.String())
	}
	if w := request(t, h, "DELETE", "/api/me/blocks/"+bID, map[string]bool{}, a); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	ds := readJSON[[]directConversation](t, request(t, h, "GET", "/api/directs", nil, a))
	if len(ds) != 1 || !ds[0].CanWrite || ds[0].Count != 1 {
		t.Fatalf("unblock changed consent/history/unread: %+v", ds)
	}
	if _, err := s.store.Exec(`DELETE FROM users WHERE id=?`, bID); err != nil {
		t.Fatal(err)
	}
	ds = readJSON[[]directConversation](t, request(t, h, "GET", "/api/directs", nil, a))
	if len(ds) != 1 || !ds[0].CanRead || ds[0].CanWrite || !ds[0].Peer.IsDeleted {
		t.Fatalf("erased peer lost remaining member history: %+v", ds)
	}
	if d := requestDirect(t, h, a, bID); !d.CanRead || d.ID != ds[0].ID {
		t.Fatal("could not reopen retained history after peer erasure")
	}
	var members int
	if err := s.store.QueryRow(`SELECT count(*) FROM conversation_members WHERE conversation_id=? AND user_id=? AND state='accepted'`, d.ID, aID).Scan(&members); err != nil || members != 1 {
		t.Fatal("erasure removed remaining membership")
	}
}

func TestDirectAcceptBlockRaceNeverAllowsBlockedContact(t *testing.T) {
	s, h, _ := directTestServer(t)
	aID, a := memberAccount(t, s, "alice")
	bID, b := memberAccount(t, s, "brenda")
	d := requestDirect(t, h, a, bID)
	results := concurrentDirectRequests(t, h, directTestRequest{"POST", "/api/directs/" + d.ID + "/accept", map[string]bool{}, b}, directTestRequest{"PUT", "/api/me/blocks/" + aID, map[string]bool{}, b})
	if results[1].Code != 200 || (results[0].Code != 200 && results[0].Code != 409) {
		t.Fatalf("race: accept %d block %d", results[0].Code, results[1].Code)
	}
	ds := readJSON[[]directConversation](t, request(t, h, "GET", "/api/directs", nil, a))
	if len(ds) != 1 || ds[0].CanWrite || !ds[0].Blocked {
		t.Fatalf("accept/block race allowed contact: %+v", ds)
	}
	if ds[0].RequestState == "rejected" && (ds[0].CanRead || ds[0].Introduction != "") {
		t.Fatal("blocking pending request retained introduction or granted history")
	}
	if w := request(t, h, "DELETE", "/api/me/blocks/"+aID, map[string]bool{}, b); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	after := readJSON[[]directConversation](t, request(t, h, "GET", "/api/directs", nil, a))
	if after[0].RequestState != ds[0].RequestState {
		t.Fatal("unblocking silently accepted a request")
	}
}
