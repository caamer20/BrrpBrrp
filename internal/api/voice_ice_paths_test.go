package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"brrpbrrp/internal/db"
)

// The ICE configuration a client is handed is the only thing that decides whether a
// call has a path at all. These cases go through the real join handler and read the
// real acceptance payload, because the defect they guard is a payload that looks
// right in a struct and is wrong in the bytes a browser receives.

// iceURLs pulls the URLs out of an acceptance payload's ice_servers, so a test
// asserts on what was offered rather than on the shape of what was offered.
func iceURLs(t *testing.T, accepted map[string]any) []string {
	t.Helper()
	raw, present := accepted["ice_servers"]
	if !present {
		t.Fatalf("the acceptance carried no ice_servers field: %#v", accepted)
	}
	servers, ok := raw.([]any)
	if !ok {
		t.Fatalf("ice_servers was not a list: %#v", raw)
	}
	var urls []string
	for _, entry := range servers {
		server, ok := entry.(map[string]any)
		if !ok {
			t.Fatalf("an ICE server was not an object: %#v", entry)
		}
		list, _ := server["urls"].([]any)
		for _, url := range list {
			if text, ok := url.(string); ok {
				urls = append(urls, text)
			}
		}
	}
	return urls
}

// voiceChannelWith creates a voice channel for a join test.
func voiceChannelWith(t *testing.T, handler http.Handler, admin *http.Cookie) db.Channel {
	t.Helper()
	return readJSON[db.Channel](t, request(t, handler, "POST", "/api/channels", db.Channel{Name: "town hall", Type: "voice", MaxVoiceUsers: 4}, admin))
}

// TestNoVendorSTUNIsOfferedWithoutAnOperatorChoice is the privacy half of the
// contract. The server used to hand every call a hardcoded public STUN server, which
// sends a call's address discovery to a third party in a product whose promise is
// that it needs no vendor account, and leaves an offline installation pointing at a
// name it cannot resolve.
func TestNoVendorSTUNIsOfferedWithoutAnOperatorChoice(t *testing.T) {
	s, server, handler := voiceTestServer(t)
	admin := setupAdmin(t, s, handler)
	channel := voiceChannelWith(t, handler, admin)

	conn := voiceSocket(t, s, server, admin)
	accepted := joinVoiceSocket(t, conn, channel.ID)

	urls := iceURLs(t, accepted)
	for _, url := range urls {
		if strings.Contains(url, "google.com") {
			t.Fatalf("the server offered a vendor STUN server the operator never chose: %s", url)
		}
	}
	if len(urls) != 0 {
		t.Fatalf("with no STUN server configured, a call was offered %v", urls)
	}
	if accepted["force_relay"] == true {
		t.Fatal("a call was forced onto a relay that is not configured")
	}
}

// TestTheForcedRelayFlagReachesTheClient proves the switch is not server-side only.
// Without it on the wire the browser cannot honour it, and "the operator asked for
// relayed calls" would be a claim about a payload nobody sends.
func TestTheForcedRelayFlagReachesTheClient(t *testing.T) {
	s, server, handler := voiceTestServer(t)
	admin := setupAdmin(t, s, handler)
	configureTURNForTest(t, handler, s, admin)
	settings := s.Settings()
	settings.ForceRelay = true
	saveSettings(t, handler, s, admin, settings)

	channel := voiceChannelWith(t, handler, admin)
	conn := voiceSocket(t, s, server, admin)
	accepted := joinVoiceSocket(t, conn, channel.ID)

	if accepted["force_relay"] != true {
		t.Fatalf("a server told to force the relay told the client %v", accepted["force_relay"])
	}
	if accepted["relay_available"] != true {
		t.Fatalf("a configured relay reported as unavailable: %v", accepted["relay_available"])
	}
	// The relay must actually be offered, or a forced-relay client has nothing to
	// force onto and the call cannot connect at all.
	var relayed bool
	for _, url := range iceURLs(t, accepted) {
		if strings.HasPrefix(url, "turn:") || strings.HasPrefix(url, "turns:") {
			relayed = true
		}
	}
	if !relayed {
		t.Fatalf("forcing the relay offered no relay: %v", iceURLs(t, accepted))
	}
}

// TestAForcedRelayNeedsARelayToForce is the failure an operator would otherwise only
// discover at the next call: the flag silently doing nothing. It is refused at the
// point of the change instead, where the cause is visible.
func TestAForcedRelayNeedsARelayToForce(t *testing.T) {
	s, _, handler := voiceTestServer(t)
	admin := setupAdmin(t, s, handler)
	settings := s.Settings()
	settings.ForceRelay = true
	recorder := trySettings(t, handler, s, admin, settings)
	if recorder.Code != 400 {
		t.Fatalf("forcing the relay with no TURN server was accepted with %d, want 400", recorder.Code)
	}
	if !strings.Contains(recorder.Body.String(), "TURN") {
		t.Fatalf("the refusal did not say what is missing: %s", recorder.Body.String())
	}
}

// TestAMalformedSTUNServerIsRefusedAtThePointOfTheChange keeps a typo from
// presenting later as a call that sometimes fails. ICE reports an unusable server as
// a network fault, so the operator would never connect it to the setting.
func TestAMalformedSTUNServerIsRefusedAtThePointOfTheChange(t *testing.T) {
	s, _, handler := voiceTestServer(t)
	admin := setupAdmin(t, s, handler)
	for _, bad := range []string{"https://stun.example.com", "not a url", "stun.example.com:3478", "turn:stun.example.com:3478"} {
		settings := s.Settings()
		settings.STUNServer = bad
		recorder := trySettings(t, handler, s, admin, settings)
		if recorder.Code != 400 {
			t.Fatalf("the STUN server %q was accepted with %d, want 400", bad, recorder.Code)
		}
	}
}

// TestTheOperatorsSTUNServerReachesTheClient is the functional half: a configured
// STUN server reaches the browser, as the operator wrote it.
func TestTheOperatorsSTUNServerReachesTheClient(t *testing.T) {
	s, server, handler := voiceTestServer(t)
	admin := setupAdmin(t, s, handler)
	settings := s.Settings()
	settings.STUNServer = "stun:stun.example.com:3478"
	saveSettings(t, handler, s, admin, settings)

	channel := voiceChannelWith(t, handler, admin)
	conn := voiceSocket(t, s, server, admin)
	accepted := joinVoiceSocket(t, conn, channel.ID)

	urls := iceURLs(t, accepted)
	if len(urls) != 1 || urls[0] != "stun:stun.example.com:3478" {
		t.Fatalf("the operator's STUN server did not reach the client unchanged: %v", urls)
	}
}

// TestADirectCallIsNotForcedByDefault is the control for the flag: a call with a
// relay configured but no requirement must still be allowed a direct path, because
// forcing every call through the server costs bandwidth and adds latency for nothing.
func TestADirectCallIsNotForcedByDefault(t *testing.T) {
	s, server, handler := voiceTestServer(t)
	admin := setupAdmin(t, s, handler)
	configureTURNForTest(t, handler, s, admin)

	channel := voiceChannelWith(t, handler, admin)
	conn := voiceSocket(t, s, server, admin)
	accepted := joinVoiceSocket(t, conn, channel.ID)

	if accepted["force_relay"] != false {
		t.Fatalf("a call was forced onto the relay without being asked to: %v", accepted["force_relay"])
	}
	if accepted["relay_available"] != true {
		t.Fatalf("a configured relay reported as unavailable: %v", accepted["relay_available"])
	}
}

// voiceTestServer is testServer plus a real listener, because a socket cannot be
// dialled against an http.Handler. It is the same server the REST calls go through,
// so a test never observes a payload the product does not actually send.
func voiceTestServer(t *testing.T) (*Server, *httptest.Server, http.Handler) {
	t.Helper()
	s, handler := testServer(t)
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return s, server, handler
}

// saveSettings persists settings through the real operator endpoint, so a test
// cannot assert on a configuration the product would refuse to accept.
func saveSettings(t *testing.T, handler http.Handler, s *Server, admin *http.Cookie, settings db.Settings) {
	t.Helper()
	if recorder := request(t, handler, "PUT", "/api/admin/settings", map[string]any{"settings": settings}, admin); recorder.Code != 200 {
		t.Fatalf("the settings were refused with %d: %s", recorder.Code, recorder.Body.String())
	}
}

// trySettings attempts the same save and hands back the recorder, so a test can
// assert on a refusal instead of failing inside the helper.
func trySettings(t *testing.T, handler http.Handler, s *Server, admin *http.Cookie, settings db.Settings) *httptest.ResponseRecorder {
	t.Helper()
	return request(t, handler, "PUT", "/api/admin/settings", map[string]any{"settings": settings}, admin)
}

// configureTURNForTest turns on a relay with a routable-looking advertised host, so
// the relay path can be observed without a real TURN deployment. It is the operator
// endpoint, not SQL: the point is that the product can be configured this way.
func configureTURNForTest(t *testing.T, handler http.Handler, s *Server, admin *http.Cookie) {
	t.Helper()
	settings := s.Settings()
	settings.TURN = true
	settings.TURNExternalIP = "203.0.113.10"
	settings.TURNAdvertisedHost = "turn.example.com"
	settings.TURNPort = 3478
	saveSettings(t, handler, s, admin, settings)
}
