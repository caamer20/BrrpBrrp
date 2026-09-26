package installer

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"runtime"
	"strings"
	"sync"
	"time"

	"brrpbrrp/internal/app"
	"brrpbrrp/web"
)

type Options struct{ Preview, NoBrowser bool }
type State struct {
	Phase      string       `json:"phase"`
	Message    string       `json:"message"`
	Error      string       `json:"error"`
	Plan       *Plan        `json:"plan,omitempty"`
	Registered bool         `json:"registered"`
	Removal    *RemovalPlan `json:"removal,omitempty"`
}
type wizard struct {
	mu                  sync.Mutex
	ctx                 context.Context
	cancel              context.CancelFunc
	token, origin, host string
	preview             bool
	paths               Paths
	state               State
	install             func(context.Context, Plan, progress) error
	start               func(context.Context, Plan) error
	detect              func(context.Context, Paths) (*RemovalPlan, error)
	remove              func(context.Context, RemovalPlan, progress) error
	removal             *RemovalPlan
	privilege           func() error
	trust               func(context.Context, []byte) error
	repair              func(context.Context, Plan) error
	update              func(context.Context, Plan) error
}

func Run(ctx context.Context, options Options) error {
	ctx, cancel := context.WithTimeout(ctx, time.Hour)
	defer cancel()
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return err
	}
	defer listener.Close()
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return err
	}
	w := &wizard{ctx: ctx, cancel: cancel, token: base64.RawURLEncoding.EncodeToString(secret), host: listener.Addr().String(), preview: options.Preview, paths: defaultPaths(), state: State{Phase: "welcome"}, install: install, start: startInstallation}
	w.detect, w.remove = detectInstallation, uninstall
	w.privilege = nativeCheck
	w.trust, w.repair = nativeTrustCertificate, repairInstallationCertificate
	w.update = updateInstallation
	w.origin = "http://" + w.host
	if !options.Preview {
		if p := existingInstallation(w.paths); p != nil {
			w.state = State{Phase: "resume", Message: "Continue your existing installation", Plan: p, Registered: true}
		}
	}
	server := &http.Server{Handler: w.handler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 45 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 8192}
	address := w.origin + "/install#" + w.token
	fmt.Println("BrrpBrrp setup:", address)
	if options.Preview {
		fmt.Println("Preview mode: installation and account changes are disabled.")
	}
	fmt.Println("Keep this window open during setup. An installed background service keeps running after setup closes.")
	if !options.NoBrowser {
		go func() {
			if err := openBrowser(address); err != nil {
				fmt.Println("Open the setup address above in a browser on this computer.")
			}
		}()
	}
	go func() {
		<-ctx.Done()
		stop, done := context.WithTimeout(context.Background(), 5*time.Second)
		defer done()
		_ = server.Shutdown(stop)
	}()
	err = server.Serve(listener)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func (w *wizard) handler() http.Handler {
	assets := http.HandlerFunc(func(out http.ResponseWriter, r *http.Request) {
		web.ServeAsset(out, r, strings.TrimPrefix(r.URL.Path, "/"))
	})
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/install/info", w.info)
	mux.HandleFunc("GET /api/install/status", func(out http.ResponseWriter, r *http.Request) {
		w.mu.Lock()
		defer w.mu.Unlock()
		reply(out, 200, w.state)
	})
	mux.HandleFunc("POST /api/install/check", w.check)
	mux.HandleFunc("POST /api/install/apply", w.apply)
	mux.HandleFunc("POST /api/install/retry", w.retry)
	mux.HandleFunc("POST /api/install/update", w.updateApplication)
	mux.HandleFunc("GET /api/install/uninstall-plan", w.removalPlan)
	mux.HandleFunc("POST /api/install/uninstall", w.removeInstallation)
	mux.HandleFunc("POST /api/install/account", w.account)
	mux.HandleFunc("GET /api/install/certificate", w.certificate)
	mux.HandleFunc("GET /api/install/certificate-info", w.certificateInfo)
	mux.HandleFunc("POST /api/install/trust-certificate", w.certificateAction)
	mux.HandleFunc("POST /api/install/repair-certificate", w.certificateAction)
	mux.HandleFunc("POST /api/install/open-tray", func(out http.ResponseWriter, r *http.Request) {
		w.mu.Lock()
		defer w.mu.Unlock()
		if w.preview || w.state.Phase != "complete" || w.state.Plan == nil {
			problem(out, 409, "Finish installing first.")
			return
		}
		if err := nativeOpenTray(*w.state.Plan); err != nil {
			problem(out, 500, err.Error())
			return
		}
		reply(out, 200, map[string]bool{"opened": true})
	})
	mux.HandleFunc("POST /api/install/close", func(out http.ResponseWriter, r *http.Request) {
		w.mu.Lock()
		busy := w.state.busy()
		w.mu.Unlock()
		if busy {
			problem(out, 409, "Wait for the current step to finish.")
			return
		}
		reply(out, 200, map[string]bool{"closed": true})
		go func() { time.Sleep(150 * time.Millisecond); w.cancel() }()
	})
	return http.HandlerFunc(func(out http.ResponseWriter, r *http.Request) {
		out.Header().Set("Cache-Control", "no-store")
		out.Header().Set("Referrer-Policy", "no-referrer")
		out.Header().Set("X-Content-Type-Options", "nosniff")
		out.Header().Set("X-Frame-Options", "DENY")
		out.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
		remote, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil || !net.ParseIP(remote).IsLoopback() || r.Host != w.host {
			problem(out, 403, "Setup is only available on this computer.")
			return
		}
		if strings.HasPrefix(r.URL.Path, "/api/") {
			if subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte("Bearer "+w.token)) != 1 {
				problem(out, 403, "This setup session has expired. Open the address shown in the setup window.")
				return
			}
			if origin := r.Header.Get("Origin"); (origin != "" && origin != w.origin) || (r.Method != "GET" && origin != w.origin) {
				problem(out, 403, "The setup request came from another site.")
				return
			}
			mux.ServeHTTP(out, r)
			return
		}
		if r.Method != "GET" && r.Method != "HEAD" {
			out.WriteHeader(405)
			return
		}
		if r.URL.Path == "/" {
			http.Redirect(out, r, "/install", http.StatusTemporaryRedirect)
			return
		}
		if r.URL.Path == "/install" {
			data, err := fs.ReadFile(web.Assets(), "index.html")
			if err != nil {
				problem(out, 500, "The setup interface is missing from this build.")
				return
			}
			out.Header().Set("Content-Type", "text/html; charset=utf-8")
			page := strings.ReplaceAll(string(data), `<link rel="manifest" href="/manifest.webmanifest" />`, "")
			_, _ = io.WriteString(out, page)
			return
		}
		// Do not register the chat app's service worker on this privileged origin.
		if !strings.HasPrefix(r.URL.Path, "/assets/") && !strings.HasPrefix(r.URL.Path, "/icons/") && r.URL.Path != "/favicon.svg" {
			http.NotFound(out, r)
			return
		}
		assets.ServeHTTP(out, r)
	})
}

func (w *wizard) info(out http.ResponseWriter, r *http.Request) {
	privilege := ""
	if err := w.privilege(); err != nil {
		privilege = err.Error()
	}
	w.mu.Lock()
	state := w.state
	paths := w.paths
	w.mu.Unlock()
	reply(out, 200, map[string]any{"os": runtime.GOOS, "version": app.Version, "paths": paths, "addresses": localIPs(), "preview": w.preview, "privilege_error": privilege, "state": state, "can_update": state.Plan != nil && canUpdate(*state.Plan)})
}

func (w *wizard) check(out http.ResponseWriter, r *http.Request) {
	var input Settings
	if !readJSON(out, r, &input) {
		return
	}
	w.mu.Lock()
	paths, busy := w.paths, w.state.busy()
	w.mu.Unlock()
	if busy {
		problem(out, 409, "Wait for the current operation to finish.")
		return
	}
	p, err := makePlan(input, paths, localIPs())
	if err == nil {
		err = checkDestination(&p)
	}
	if err != nil {
		problem(out, 400, err.Error())
		return
	}
	reply(out, 200, p)
}

func (w *wizard) apply(out http.ResponseWriter, r *http.Request) {
	if w.preview {
		problem(out, 403, "Preview mode cannot install or modify this computer.")
		return
	}
	if err := w.privilege(); err != nil {
		problem(out, 403, err.Error())
		return
	}
	var input Settings
	if !readJSON(out, r, &input) {
		return
	}
	w.mu.Lock()
	if w.state.Phase != "welcome" && w.state.Phase != "removed" && !(w.state.Phase == "error" && !w.state.Registered) {
		w.mu.Unlock()
		problem(out, 409, "Installation has already started.")
		return
	}
	p, err := makePlan(input, w.paths, localIPs())
	if err == nil {
		err = checkDestination(&p)
	}
	if err != nil {
		w.mu.Unlock()
		problem(out, 400, err.Error())
		return
	}
	w.state = State{Phase: "installing", Message: "Preparing your server", Plan: &p}
	w.mu.Unlock()
	go w.perform(p, false)
	reply(out, 202, map[string]bool{"started": true})
}

func (w *wizard) retry(out http.ResponseWriter, r *http.Request) {
	if w.preview {
		problem(out, 403, "Preview mode cannot start a service.")
		return
	}
	w.mu.Lock()
	if !w.state.Registered || (w.state.Phase != "error" && w.state.Phase != "resume") {
		w.mu.Unlock()
		problem(out, 409, "There is no stopped installation to resume.")
		return
	}
	p := *w.state.Plan
	w.state.Phase, w.state.Error = "installing", ""
	w.mu.Unlock()
	go w.perform(p, true)
	reply(out, 202, map[string]bool{"started": true})
}

func (w *wizard) perform(p Plan, resume bool) {
	ctx, cancel := context.WithTimeout(w.ctx, 2*time.Minute)
	defer cancel()
	report := func(message string) { w.mu.Lock(); w.state.Message = message; w.mu.Unlock() }
	var err error
	if !resume {
		err = w.install(ctx, p, report)
	}
	w.mu.Lock()
	w.state.Registered = resume || err == nil || existingInstallation(w.paths) != nil
	w.mu.Unlock()
	if err == nil {
		report("Registering and starting the background service")
		err = w.start(ctx, p)
	}
	var required bool
	if err == nil {
		report("Checking the server and database")
		required, err = waitReady(ctx, p)
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if err != nil {
		w.state.Phase = "error"
		w.state.Error = err.Error()
		return
	}
	w.state.Phase = "account"
	if !required {
		w.state.Phase = "complete"
	}
	w.state.Message = "Your server is running"
}

func (w *wizard) certificate(out http.ResponseWriter, r *http.Request) {
	w.mu.Lock()
	p := w.state.Plan
	w.mu.Unlock()
	if p == nil || p.Settings.Mode != "network" {
		problem(out, 404, "No local certificate is available.")
		return
	}
	data, _, err := app.TrustCertificate(certificateConfig(*p))
	if err != nil {
		problem(out, 404, "The server has not created its certificate yet.")
		return
	}
	out.Header().Set("Content-Type", "application/x-x509-ca-cert")
	out.Header().Set("Content-Disposition", `attachment; filename="BrrpBrrp-local-ca.cer"`)
	_, _ = out.Write(data)
}

func readJSON(out http.ResponseWriter, r *http.Request, input any) bool {
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		problem(out, 415, "Send JSON to the setup service.")
		return false
	}
	r.Body = http.MaxBytesReader(out, r.Body, 16384)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(input); err != nil {
		problem(out, 400, "The setup request is invalid or too large.")
		return false
	}
	if decoder.Decode(new(any)) != io.EOF {
		problem(out, 400, "Send one setup request at a time.")
		return false
	}
	return true
}
func reply(out http.ResponseWriter, status int, value any) {
	out.Header().Set("Content-Type", "application/json")
	out.WriteHeader(status)
	_ = json.NewEncoder(out).Encode(value)
}
func problem(out http.ResponseWriter, status int, message string) {
	reply(out, status, map[string]string{"error": message})
}
