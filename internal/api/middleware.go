package api

import (
	"bufio"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"time"

	"brrpbrrp/internal/authz"
	"brrpbrrp/internal/db"
)

type authenticatedHandler func(http.ResponseWriter, *http.Request, db.User)

// routePolicy declares the authority a route requires before its handler runs.
// Route-level checks answer "may this account act on this surface at all";
// resource-level checks inside the handler answer "may it act on this one".
type routePolicy struct {
	// Capability is the minimum grant the account must hold. An empty capability
	// means the route only needs an authenticated, active account, and the
	// handler must itself scope whatever it returns.
	Capability authz.Capability
	// FreshAuth additionally requires a strong authentication inside the recent
	// window, because the action can defeat a privacy boundary rather than merely
	// change content. It also requires an enrolled second factor of an operator
	// account, so an action that can reach every private boundary is never
	// protected by a password alone (WP-05).
	FreshAuth bool
	// Note records why a non-obvious policy is correct, so the inventory reads as
	// a review document rather than a list of names.
	Note string
	// BeyondCommunity marks a route a moderation timeout must not close. A community
	// timeout suspends an account's ability to participate in the community; it is not
	// an installation ban, and it must not read as one by denying private history,
	// direct conversations or a private call. An installation ban is a separate
	// authority with its own rules and its own routes (ADR 0018, completion review 14,
	// V14-03).
	//
	// The default is closed — a timeout stops the account everywhere — because a new
	// route should be suspended until somebody has said which kind it is. Getting that
	// wrong in this direction is recoverable; getting it wrong in the other direction
	// is a community moderator holding somebody's private conversations hostage.
	BeyondCommunity bool
	// MixedConversation marks a route that serves *both* community channels and
	// private conversations under the same path, so the middleware cannot know which
	// kind it is answering without resolving the conversation itself.
	//
	// A timeout's scope is per-destination, not per-path: the same URL is a community
	// channel for one id and a private conversation for another. Such a route lets a
	// suspended account through, and the handler decides — it already resolves the
	// conversation to answer at all, and it refuses a suspended caller for a channel
	// (see `refuseSuspendedCommunity`).
	//
	// Declaring it is a deliberate act. A route that is only community must not carry
	// it, because that would let a timed-out member keep reading channels; a route
	// that is only private should use BeyondCommunity instead, which needs no
	// per-request work.
	MixedConversation bool
}

// administrativeCapabilities reach administration surfaces. Mutations behind
// them keep the stricter per-account limit that administrator-only routes had
// before roles existed; ordinary conversations keep their own send limits.
var administrativeCapabilities = map[authz.Capability]bool{
	authz.ChannelManage:              true,
	authz.CategoryManage:             true,
	authz.RoleManage:                 true,
	authz.InviteManage:               true,
	authz.MemberManage:               true,
	authz.SettingsManage:             true,
	authz.OperatorBackup:             true,
	authz.OperatorCredentialRecovery: true,
	authz.OperatorOwnershipTransfer:  true,
}

func (s *Server) protect(policy routePolicy, next authenticatedHandler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, err := s.authenticate(r)
		if err != nil {
			problem(w, http.StatusUnauthorized, "Please sign in again.")
			return
		}
		principal, err := s.resolvePrincipal(r.Context(), s.store, user)
		if err != nil {
			s.failed(w, err)
			return
		}
		// An inactive account holds nothing — banned, deactivated or inside a
		// moderation timeout — and the check belongs here rather than in each handler.
		// Relying on the capability check alone would let a route that declares no
		// capability (a plain channel list, a search) answer a suspended account,
		// which is the one thing a timeout exists to prevent.
		//
		// A ban and a timeout are answered differently on purpose: a banned or
		// deactivated account's session is over, so 401 sends it back to sign in; a
		// timeout is a decision about what the account may do and its session is still
		// valid, so 403 with an explanation is the honest answer.
		// A ban or deactivation ends the session everywhere: the account is out, and
		// 401 says so rather than implying a permission problem.
		if principal.Inactive {
			problem(w, http.StatusUnauthorized, "Please sign in again.")
			return
		}
		// A moderation timeout is a *community* decision. On a community route it is a
		// 403 with an explanation, because the session is still valid and the person is
		// still signed in — 401 would send them through a sign-in they do not need.
		//
		// On a route marked BeyondCommunity it does not apply at all. The previous
		// version set a header and then returned, so the handler never ran and the
		// client received an empty 200: a private conversation list with no
		// conversations, a DM that silently vanishes. A 200 that is indistinguishable
		// from success and is not success is worse than the 403 it replaced.
		if principal.Suspended && !policy.BeyondCommunity && !policy.MixedConversation {
			problem(w, http.StatusForbidden, "This account is suspended until a moderation timeout expires. Contact a moderator if you think this is a mistake.")
			return
		}
		if policy.Capability != "" && !principal.Allowed(policy.Capability) {
			problem(w, http.StatusForbidden, "Your role does not include "+string(policy.Capability)+".")
			return
		}
		if policy.FreshAuth && !s.requireRecentAuthentication(w, r, user) {
			return
		}
		if policy.FreshAuth && !s.requireEnrolledSecondFactor(r.Context(), w, user) {
			return
		}
		limit := 240
		if r.Method == http.MethodGet {
			limit = 1200
		}
		if !s.limiter.allow("user:"+user.ID+":"+r.Method, limit, time.Minute) {
			w.Header().Set("Retry-After", "60")
			problem(w, http.StatusTooManyRequests, "Too many requests. Try again shortly.")
			return
		}
		if administrativeCapabilities[policy.Capability] && r.Method != http.MethodGet && !s.limiter.allow("privileged:"+user.ID, 100, time.Minute) {
			problem(w, 429, "Too many administrative changes. Try again in a minute.")
			return
		}
		// The session hash travels with the request so a write transaction can
		// prove this session still exists when it commits.
		hash := ""
		if cookie, err := r.Cookie(cookieName); err == nil {
			hash = hashToken(cookie.Value)
		}
		next(w, withPrincipal(r.WithContext(withSession(r.Context(), hash)), principal), user)
	}
}

func (s *Server) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Permissions-Policy", "camera=(), microphone=(self), geolocation=()")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' blob: data: https:; media-src 'self' blob: https:; connect-src 'self'; object-src 'none'; base-uri 'none'; frame-ancestors 'none'; form-action 'self'")
		// Only a deployment whose browser-facing origin is real HTTPS gets the transport
		// pin; Config.HSTS explains why a locally issued certificate must not receive one.
		// The value is a plain six-month max-age, without includeSubDomains or preload.
		if s.config.HSTS() {
			w.Header().Set("Strict-Transport-Security", "max-age=15552000")
		}
		if strings.HasPrefix(r.URL.Path, "/api/") {
			w.Header().Set("Cache-Control", "no-store")
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead && r.Method != http.MethodOptions {
			origin := r.Header.Get("Origin")
			if (origin != "" && origin != s.config.BaseURL) || r.Header.Get("Sec-Fetch-Site") == "cross-site" || (origin == "" && !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json")) {
				problem(w, http.StatusForbidden, "Request origin is not allowed.")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// measure records one request's outcome against its route pattern.
//
// It sits inside the authentication wrapper so a refused request is still counted: a route
// that answers 401 for everybody is a fact an operator needs, and it is invisible if only
// admitted requests are measured.
func (s *Server) measure(pattern string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		done := s.metrics.Begin(pattern)
		recorder := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next(recorder, r)
		done(recorder.status)
	}
}

// statusRecorder remembers the status the handler wrote. Nothing else is wrapped: a
// ResponseWriter that also intercepts the body is a ResponseWriter that breaks streaming
// and hijacking, which is what the websocket route needs.
type statusRecorder struct {
	http.ResponseWriter
	status  int
	written bool
}

func (w *statusRecorder) WriteHeader(status int) {
	if !w.written {
		w.status = status
		w.written = true
	}
	w.ResponseWriter.WriteHeader(status)
}

func (w *statusRecorder) Write(body []byte) (int, error) {
	w.written = true
	return w.ResponseWriter.Write(body)
}

// Hijack lets the websocket upgrade through. Without it the one route that needs to stop
// being an HTTP request at all would fail, and the failure would look like a broken socket
// rather than a broken metrics wrapper.
func (w *statusRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hijacker, ok := w.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, http.ErrNotSupported
	}
	return hijacker.Hijack()
}

func (s *Server) clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return "unknown"
	}
	if !s.config.BehindProxy || !s.trustedIP(host) {
		return host
	}
	forwarded := strings.Split(r.Header.Get("X-Forwarded-For"), ",")
	// Walk from the trusted peer toward the client; ignore spoofed left entries.
	for i := len(forwarded) - 1; i >= 0; i-- {
		candidate := strings.TrimSpace(forwarded[i])
		if net.ParseIP(candidate) == nil {
			return host
		}
		host = candidate
		if !s.trustedIP(candidate) {
			break
		}
	}
	return host
}

func (s *Server) trustedIP(value string) bool {
	ip, err := netip.ParseAddr(value)
	if err != nil {
		return false
	}
	for _, prefix := range s.config.TrustedProxies {
		if prefix.Contains(ip.Unmap()) {
			return true
		}
	}
	return false
}

type rateBucket struct {
	count int
	until time.Time
}
type rateLimiter struct {
	sync.Mutex
	buckets map[string]rateBucket
}

func newRateLimiter() *rateLimiter { return &rateLimiter{buckets: make(map[string]rateBucket)} }

func (l *rateLimiter) allow(key string, limit int, window time.Duration) bool {
	l.Lock()
	defer l.Unlock()
	now := time.Now()
	if len(l.buckets) > 4096 {
		for k, b := range l.buckets {
			if now.After(b.until) {
				delete(l.buckets, k)
			}
		}
		if len(l.buckets) > 4096 {
			return false
		}
	}
	b := l.buckets[key]
	if now.After(b.until) {
		b = rateBucket{until: now.Add(window)}
	}
	b.count++
	l.buckets[key] = b
	return b.count <= limit
}
