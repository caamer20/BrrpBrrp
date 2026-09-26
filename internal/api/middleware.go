package api

import (
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
