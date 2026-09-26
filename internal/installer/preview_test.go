package installer

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"testing"
	"time"

	"brrpbrrp/internal/app"
)

// Opt-in local visual fixture. It never installs a service or changes device trust.
func TestManualReadyPreview(t *testing.T) {
	if os.Getenv("BRRP_READY_PREVIEW") != "1" {
		t.Skip("set BRRP_READY_PREVIEW=1 for a ten-minute local visual fixture")
	}
	w := certWizard(t)
	w.preview = true
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	w.host = listener.Addr().String()
	w.origin = "http://" + w.host
	handler := w.handler()
	server := &http.Server{Handler: http.HandlerFunc(func(out http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/install/info" && r.Header.Get("Authorization") == "Bearer "+w.token && r.Host == w.host {
			reply(out, 200, map[string]any{"os": "windows", "version": app.Version, "paths": w.paths, "addresses": []string{"192.168.1.5"}, "preview": true, "privilege_error": "", "state": w.state, "can_update": true})
			return
		}
		handler.ServeHTTP(out, r)
	})}
	defer server.Close()
	go server.Serve(listener)
	fmt.Println("READY_PREVIEW", w.origin+"/install#"+w.token)
	time.Sleep(10 * time.Minute)
}
