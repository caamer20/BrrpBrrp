package api

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"time"

	"brrpbrrp/internal/db"
	"brrpbrrp/internal/ws"
)

// recentAuthenticationWindow is how long a confirmed password or second factor
// stays fresh enough to authorize an action that can defeat a privacy boundary.
// The window is short on purpose: these actions are rare, and a long one would
// make "recent" meaningless.
const recentAuthenticationWindow = 15 * time.Minute

// sessionView is the client-facing session record. It never carries token
// material, and it states whether the session is inside the privileged window so
// the client can ask for a password before offering a sensitive action.
type sessionView struct {
	db.Session
	RecentlyAuthenticated bool `json:"recently_authenticated"`
}

func (s *Server) sessionViews(r *http.Request, userID string) ([]sessionView, error) {
	current := currentSessionHash(r)
	sessions, err := s.store.Sessions(r.Context(), s.store, userID, current)
	if err != nil {
		return nil, err
	}
	views := make([]sessionView, 0, len(sessions))
	for _, session := range sessions {
		views = append(views, sessionView{Session: session, RecentlyAuthenticated: authenticationIsRecent(session.PrivilegedAt, time.Now())})
	}
	return views, nil
}

// authenticationIsRecent reports whether a strong authentication happened inside
// the privileged window. An empty or unparsable timestamp is never recent.
func authenticationIsRecent(privileged string, now time.Time) bool {
	if privileged == "" {
		return false
	}
	at, err := time.Parse("2006-01-02T15:04:05.000000000Z", privileged)
	if err != nil {
		return false
	}
	age := now.Sub(at)
	return age >= 0 && age <= recentAuthenticationWindow
}

// recentAuthenticationInTx answers the freshness question through a caller-supplied
// query interface, so a handler that already holds a transaction asks inside it.
// The store holds a single connection: a helper that reached for the store while a
// transaction was open would wait for the connection its own caller is holding.
func (s *Server) recentAuthenticationInTx(ctx context.Context, q db.Queryer, hash string) (bool, error) {
	if hash == "" {
		return false, nil
	}
	privileged, err := s.store.PrivilegedAt(ctx, q, hash)
	if err != nil {
		return false, err
	}
	return authenticationIsRecent(privileged, time.Now()), nil
}

// secondFactorEnrolledInTx answers the enrollment half of the privileged policy
// through the same query interface.
func (s *Server) secondFactorEnrolledInTx(ctx context.Context, q db.Queryer, userID string) (bool, error) {
	var enrolled bool
	err := q.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM user_mfa WHERE user_id=? AND secret!='')`, userID).Scan(&enrolled)
	return enrolled, err
}

// requirePrivilegedSecurityPolicyInTx applies the strong-authentication policy of a
// security-policy change inside the committing transaction. The caller has already
// established that the actor is the owner; this adds the freshness of the session
// that made the request and, while the policy is on, the actor's own enrolled
// factor, all read through the transaction rather than through the store.
func (s *Server) requirePrivilegedSecurityPolicyInTx(ctx context.Context, tx *sql.Tx, userID string, privilegedAccount bool) error {
	recent, err := s.recentAuthenticationInTx(ctx, tx, sessionHashOf(ctx))
	if err != nil {
		return err
	}
	if !recent {
		return refuse(http.StatusForbidden, fmt.Sprintf("Confirm your password to continue: this action needs a sign-in from the last %s.", recentAuthenticationWindow))
	}
	if !s.Settings().RequirePrivilegedMFA || !privilegedAccount {
		return nil
	}
	enrolled, err := s.secondFactorEnrolledInTx(ctx, tx, userID)
	if err != nil {
		return err
	}
	if !enrolled {
		return refuse(http.StatusForbidden, "Enroll two-factor authentication before using this action. Your recovery codes and the offline server command remain available if you lose your authenticator.")
	}
	return nil
}

// requireRecentAuthentication gates an action that needs a fresh proof of
// identity rather than merely a valid session.
func (s *Server) requireRecentAuthentication(w http.ResponseWriter, r *http.Request, user db.User) bool {
	hash := currentSessionHash(r)
	if hash == "" {
		problem(w, http.StatusUnauthorized, "Please sign in again.")
		return false
	}
	recent, err := s.recentAuthenticationInTx(r.Context(), s.store, hash)
	if err != nil {
		s.failed(w, err)
		return false
	}
	if !recent {
		problem(w, http.StatusForbidden, fmt.Sprintf("Confirm your password to continue: this action needs a sign-in from the last %s.", recentAuthenticationWindow))
		return false
	}
	return true
}

func currentSessionHash(r *http.Request) string {
	cookie, err := r.Cookie(cookieName)
	if err != nil {
		return ""
	}
	return hashToken(cookie.Value)
}

// requireEnrolledSecondFactor enforces the privileged enrollment policy: an
// account that can exercise operator-tier authority must have a second factor
// before it does, because those actions can reach every private boundary on the
// server. Ordinary members are never blocked from working by this check.
func (s *Server) requireEnrolledSecondFactor(ctx context.Context, w http.ResponseWriter, user db.User) bool {
	if !s.Settings().RequirePrivilegedMFA {
		return true
	}
	if !user.IsAdmin && !user.IsOwner {
		return true
	}
	enrolled, err := s.secondFactorEnrolledInTx(ctx, s.store, user.ID)
	if err != nil {
		s.failed(w, err)
		return false
	}
	if enrolled {
		return true
	}
	problem(w, http.StatusForbidden, "Enroll two-factor authentication before using this action. Your recovery codes and the offline server command remain available if you lose your authenticator.")
	return false
}

// listSessions shows an account its own sessions.
func (s *Server) listSessions(w http.ResponseWriter, r *http.Request, user db.User) {
	views, err := s.sessionViews(r, user.ID)
	if err != nil {
		s.failed(w, err)
		return
	}
	respond(w, 200, views)
}

// revokeSession signs one device out. Revoking the caller's own session also
// clears the cookie, so the browser cannot keep using a session the server has
// already removed.
func (s *Server) revokeSession(w http.ResponseWriter, r *http.Request, user db.User) {
	tx, err := s.store.BeginTx(r.Context(), nil)
	if err != nil {
		s.failed(w, err)
		return
	}
	defer tx.Rollback()
	if _, err := s.authorizeInTx(r.Context(), tx, user); err != nil {
		s.authorityFailed(w, err)
		return
	}
	hash, err := s.store.DeleteSession(r.Context(), tx, user.ID, r.PathValue("id"))
	if errors.Is(err, sql.ErrNoRows) {
		problem(w, 404, "Session not found.")
		return
	}
	if err != nil {
		s.failed(w, err)
		return
	}
	// The revocation commits with the invalidation of exactly that session's
	// connections, so a queued event resolved under the old session is dropped even
	// while the slower stream and socket cleanup below is still running. The account's
	// other devices are not addressed and keep working.
	if err := s.commitAuthorityChangeFor(r.Context(), tx, "That session was signed out.", ws.Audience{SessionHashes: []string{hash}}); err != nil {
		s.failed(w, err)
		return
	}
	// The sockets, media and file streams bound to that session end here, not at
	// the next request: a revoked session must not keep receiving events or bytes.
	s.hub.Disconnect(user.ID, hash)
	s.deliveries.revokeSession(hash)
	if hash == currentSessionHash(r) {
		s.clearCookie(w)
		s.leaveVoice(user.ID, "")
	}
	s.audit(user.ID, "session.revoked", r.PathValue("id"))
	respond(w, 200, map[string]bool{"ok": true})
}

// revokeOtherSessions signs every other device out and keeps the current one.
func (s *Server) revokeOtherSessions(w http.ResponseWriter, r *http.Request, user db.User) {
	hash := currentSessionHash(r)
	tx, err := s.store.BeginTx(r.Context(), nil)
	if err != nil {
		s.failed(w, err)
		return
	}
	defer tx.Rollback()
	if _, err := s.authorizeInTx(r.Context(), tx, user); err != nil {
		s.authorityFailed(w, err)
		return
	}
	removed, err := s.store.DeleteOtherSessions(r.Context(), tx, user.ID, hash)
	if err != nil {
		s.failed(w, err)
		return
	}
	// Every removed session's connections are invalidated with the commit, so an event
	// queued for a signed-out device is dropped at the commit rather than when the
	// disconnect below runs. The account is addressed as a whole because the removals are
	// counted, not listed: the invalidation must cover every device that was just ended.
	if err := s.commitAuthorityChangeFor(r.Context(), tx, "Your other sessions were signed out.", ws.Audience{UserIDs: []string{user.ID}}); err != nil {
		s.failed(w, err)
		return
	}
	// Disconnect by user rather than by hash: every remaining client for this
	// account must reconnect and re-authenticate under the survivor. Streams
	// admitted by the sessions that were just removed stop too, and the surviving
	// session's own streams keep going.
	s.hub.Disconnect(user.ID, "")
	s.deliveries.revokeAccountExceptSession(user.ID, hash)
	s.audit(user.ID, "session.revoked_others", user.ID)
	respond(w, 200, map[string]any{"revoked": removed})
}

// reauth confirms the account's password (and second factor when enrolled) and
// marks the current session as freshly authenticated.
func (s *Server) reauth(w http.ResponseWriter, r *http.Request, user db.User) {
	var input struct {
		Password string `json:"password"`
		Code     string `json:"code"`
	}
	if !decode(w, r, &input) {
		return
	}
	if !s.checkPassword(w, r, user.ID, input.Password) {
		return
	}
	hash := currentSessionHash(r)
	if hash == "" {
		problem(w, http.StatusUnauthorized, "Please sign in again.")
		return
	}
	tx, err := s.store.BeginTx(r.Context(), nil)
	if err != nil {
		s.failed(w, err)
		return
	}
	defer tx.Rollback()
	if _, err := s.authorizeInTx(r.Context(), tx, user); err != nil {
		s.authorityFailed(w, err)
		return
	}
	var enrolled bool
	if err := tx.QueryRow(`SELECT EXISTS(SELECT 1 FROM user_mfa WHERE user_id=? AND secret!='')`, user.ID).Scan(&enrolled); err != nil {
		s.failed(w, err)
		return
	}
	if enrolled {
		if !s.limiter.allow("mfa:"+user.ID, 10, 5*time.Minute) {
			problem(w, 429, "Too many verification attempts. Try again in five minutes.")
			return
		}
		valid, err := consumeFactor(tx, user.ID, input.Code)
		if err != nil {
			s.failed(w, err)
			return
		}
		if !valid {
			problem(w, 401, "The code is incorrect or has already been used.")
			return
		}
	}
	if err := db.MarkSessionPrivileged(r.Context(), tx, hash); err != nil {
		s.failed(w, err)
		return
	}
	if err := tx.Commit(); err != nil {
		s.failed(w, err)
		return
	}
	s.audit(user.ID, "session.reauthenticated", user.ID)
	respond(w, 200, map[string]any{"recently_authenticated": true, "second_factor_required": enrolled})
}
