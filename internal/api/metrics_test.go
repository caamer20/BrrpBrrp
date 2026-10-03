package api

import (
	"encoding/json"
	"net/http"
	"testing"
)

// metricsBody is the shape the endpoint promises. It is decoded into maps rather than a
// struct so a field that disappears is a failure here rather than a compile error
// somewhere else: an operator's dashboard and the probe script both read these by name.
func metricsBody(t *testing.T, handler http.Handler, cookie *http.Cookie) map[string]any {
	t.Helper()
	w := request(t, handler, "GET", "/api/admin/metrics", nil, cookie)
	if w.Code != 200 {
		t.Fatalf("metrics: %d %s", w.Code, w.Body.String())
	}
	body := map[string]any{}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode metrics: %v", err)
	}
	return body
}

func TestMetricsRequireOperatorAuthorityAndCarryNoMessageContent(t *testing.T) {
	s, handler := testServer(t)
	if w := request(t, handler, "GET", "/api/admin/metrics", nil, nil); w.Code != 401 {
		t.Fatalf("unauthenticated metrics: %d, want 401", w.Code)
	}
	admin := setupAdmin(t, s, handler)

	// A member without settings.manage is refused, and the refusal is on the *route*
	// rather than on the handler's own judgement. The member is created by the
	// administrator and signs in for itself, because that is the only way an account with
	// the ordinary role comes to exist.
	w := request(t, handler, "POST", "/api/admin/users", map[string]any{
		"username": "metrics_member",
		"password": "a private initial passphrase",
	}, admin)
	if w.Code != 201 {
		t.Fatalf("create member: %d %s", w.Code, w.Body.String())
	}
	login := request(t, handler, "POST", "/api/login", map[string]string{
		"username": "metrics_member",
		"password": "a private initial passphrase",
	}, nil)
	if login.Code != 200 {
		t.Fatalf("member sign-in: %d %s", login.Code, login.Body.String())
	}
	member := login.Result().Cookies()[0]
	if w := request(t, handler, "GET", "/api/admin/metrics", nil, member); w.Code != 403 {
		t.Fatalf("member reading metrics: %d, want 403", w.Code)
	}

	// The administrator reads it, and what it holds is counters and figures — never the
	// content of anything a member wrote.
	body := metricsBody(t, handler, admin)
	for _, key := range []string{"version", "uptime_seconds", "pid", "routes", "process", "media", "storage", "jobs", "connections"} {
		if _, ok := body[key]; !ok {
			t.Fatalf("metrics body has no %q: %+v", key, body)
		}
	}
	routes, ok := body["routes"].([]any)
	if !ok {
		t.Fatalf("routes is %T, want a list", body["routes"])
	}
	// The scrape itself is a request, so the route it lives on must be in the list: a
	// metrics endpoint that does not count itself is a metrics endpoint with a hole where
	// its own cost should be.
	found := false
	for _, entry := range routes {
		route, _ := entry.(map[string]any)
		if route["route"] == "GET /api/admin/metrics" {
			found = true
		}
		// A label that contains a UUID-shaped segment is a label built from a request
		// path, which is how one busy channel turns this map into a memory leak.
		label, _ := route["route"].(string)
		for _, segment := range splitPath(label) {
			if looksLikeAnID(segment) {
				t.Fatalf("route label %q carries what looks like an identifier", label)
			}
		}
	}
	if !found {
		t.Fatalf("the metrics route does not count itself: %+v", routes)
	}
	process, _ := body["process"].(map[string]any)
	if process == nil {
		t.Fatalf("process section is missing")
	}
	for _, key := range []string{"reported", "resident_bytes", "cpu_seconds", "goroutines", "go_heap_bytes", "go_sys_bytes", "go_gc_cycles"} {
		if _, ok := process[key]; !ok {
			t.Fatalf("process section has no %q: %+v", key, process)
		}
	}
	media, _ := body["media"].(map[string]any)
	for _, key := range []string{"voice_users", "forwarded_packets", "forwarded_bytes", "dropped_packets"} {
		if _, ok := media[key]; !ok {
			t.Fatalf("media section has no %q: %+v", key, media)
		}
	}
}

func splitPath(label string) []string {
	out := []string{}
	current := ""
	for _, char := range label {
		if char == '/' || char == ' ' {
			if current != "" {
				out = append(out, current)
			}
			current = ""
			continue
		}
		current += string(char)
	}
	if current != "" {
		out = append(out, current)
	}
	return out
}

// A path segment that is an opaque identifier: long, and made of hex or uuid punctuation.
// Route patterns carry `{id}` instead, which is the whole point of measuring patterns.
func looksLikeAnID(segment string) bool {
	if len(segment) < 24 {
		return false
	}
	runes := 0
	digits := 0
	letters := 0
	for _, char := range segment {
		switch {
		case char >= '0' && char <= '9':
			digits++
			runes++
		case (char >= 'a' && char <= 'f') || (char >= 'A' && char <= 'F'):
			letters++
			runes++
		case char == '-':
			runes++
		default:
			return false
		}
	}
	return runes == len(segment) && digits > 0 && letters > 0 && len(segment) >= 32
}

// The counters move. A metrics endpoint that reports zeros while serving requests is worse
// than no endpoint, because it is believed.
func TestTheCountersMoveWithTheTraffic(t *testing.T) {
	s, handler := testServer(t)
	admin := setupAdmin(t, s, handler)
	before := routeTotal(t, metricsBody(t, handler, admin), "GET /api/me")
	for range 3 {
		if w := request(t, handler, "GET", "/api/me", nil, admin); w.Code != 200 {
			t.Fatalf("me: %d %s", w.Code, w.Body.String())
		}
	}
	after := routeTotal(t, metricsBody(t, handler, admin), "GET /api/me")
	if after < before+3 {
		t.Fatalf("GET /api/me counted %d, want at least %d", after, before+3)
	}

	// A refused request is counted too: an endpoint that answers 401 for everybody is a
	// fact an operator needs, and it is invisible if only admitted requests are measured.
	before = routeTotal(t, metricsBody(t, handler, admin), "GET /api/state")
	if w := request(t, handler, "GET", "/api/state", nil, nil); w.Code != 401 {
		t.Fatalf("unauthenticated state: %d, want 401", w.Code)
	}
	body := metricsBody(t, handler, admin)
	if after := routeTotal(t, body, "GET /api/state"); after < before+1 {
		t.Fatalf("the refusal was not counted: %d, want at least %d", after, before+1)
	}
	if classes := routeClasses(t, body, "GET /api/state"); classes["4xx"] == 0 {
		t.Fatalf("the refusal was not classified: %+v", classes)
	}
}

func routeTotal(t *testing.T, body map[string]any, pattern string) int64 {
	t.Helper()
	for _, entry := range body["routes"].([]any) {
		route, _ := entry.(map[string]any)
		if route["route"] == pattern {
			total, _ := route["total"].(float64)
			return int64(total)
		}
	}
	return 0
}

func routeClasses(t *testing.T, body map[string]any, pattern string) map[string]float64 {
	t.Helper()
	for _, entry := range body["routes"].([]any) {
		route, _ := entry.(map[string]any)
		if route["route"] != pattern {
			continue
		}
		classes, _ := route["status_class"].(map[string]any)
		out := map[string]float64{}
		for name, count := range classes {
			value, _ := count.(float64)
			out[name] = value
		}
		return out
	}
	return map[string]float64{}
}
